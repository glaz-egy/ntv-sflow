package live

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/netip"
	"reflect"
	"testing"
	"time"

	"network-traffic-visualizer/internal/collector"
	c "network-traffic-visualizer/internal/contract"
	"network-traffic-visualizer/internal/counters"
	"network-traffic-visualizer/internal/enrichment"
	"network-traffic-visualizer/internal/flow"
	"network-traffic-visualizer/internal/inventory"
	"network-traffic-visualizer/internal/mock"
	"network-traffic-visualizer/internal/projection"
	"network-traffic-visualizer/internal/sflowgen"
)

var epoch = time.Date(2026, 10, 3, 14, 45, 0, 0, time.UTC)

func demoSource(t *testing.T, now time.Time) *Source {
	t.Helper()
	inv, err := inventory.Load("../../configs/inventory.demo.yaml")
	if err != nil {
		t.Fatal(err)
	}
	s, err := New(Options{
		WindowSeconds: mock.WindowSeconds, Epoch: epoch, Inventory: inv,
		InternalCIDRs: mock.DefaultInternalCIDRs, Origin: mock.DefaultOrigin,
		Geo: enrichment.NewChainGeo(enrichment.NewOverrideGeo(inv.GeoOverrides), nil, 1000),
		Now: func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func normalize(t *testing.T, v any) any {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var out any
	json.Unmarshal(b, &out)
	// Server clocks legitimately differ between a mock tick and a live tick.
	if m, ok := out.(map[string]any); ok {
		delete(m, "server_time")
		delete(m, "generated_at")
	}
	return out
}

// CLAUDE.md DoD 8: the same traffic, delivered as real sFlow through the
// collector and the live aggregator, yields exactly the mock's API views.
func TestLiveFromSFlowEqualsMock(t *testing.T) {
	for _, scenario := range []string{"default", "heavy-download", "internal-backup"} {
		t.Run(scenario, func(t *testing.T) {
			const T = mock.StartTick + 17
			mb, err := mock.NewBackend(mock.Options{Seed: 42, Scenario: scenario, Speed: 1, Epoch: epoch})
			if err != nil {
				t.Fatal(err)
			}
			mb.SetTick(T)

			sc, _ := mock.BuildScenario(scenario, 42)
			engine, _ := mock.NewEngine(sc, 42)
			gen := sflowgen.New(engine)
			src := demoSource(t, epoch)
			col := collector.New(collector.Options{Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}, src)
			from := netip.MustParseAddrPort("10.0.0.1:6343")
			for sec := T - 12; sec <= T; sec++ {
				src.Advance(epoch.Add(time.Duration(sec) * time.Second))
				at := epoch.Add(time.Duration(sec)*time.Second + 400*time.Millisecond)
				for _, d := range gen.Second(sec, uint32(sec*1000)) {
					col.HandleDatagram(d, from, at)
				}
			}
			src.Advance(epoch.Add(time.Duration(T+1) * time.Second)) // window end = T

			lf, linv := src.Snapshot()
			mf, minv := mb.Snapshot()
			if lf.WindowEndTick != mf.WindowEndTick {
				t.Fatalf("window end: live %d mock %d", lf.WindowEndTick, mf.WindowEndTick)
			}
			cases := map[string][2]any{}
			for _, g := range []string{"asn", "ip", "country", "city"} {
				q := projection.GlobeQuery{Grouping: g}
				cases["globe "+g] = [2]any{linv.Globe(lf, q), minv.Globe(mf, q)}
			}
			cases["globe filtered"] = [2]any{
				linv.Globe(lf, projection.GlobeQuery{Grouping: "asn", SourceNodeID: "dev_pc01", Direction: "inbound", Protocol: "tcp"}),
				minv.Globe(mf, projection.GlobeQuery{Grouping: "asn", SourceNodeID: "dev_pc01", Direction: "inbound", Protocol: "tcp"}),
			}
			cases["destination"] = [2]any{linv.Destination(lf, "asn:13335", "", ""), minv.Destination(mf, "asn:13335", "", "")}
			cases["home"] = [2]any{linv.Home(lf, projection.HomeQuery{}), minv.Home(mf, projection.HomeQuery{})}
			hq := projection.HomeQuery{DestinationKey: "asn:13335", IncludeInactive: true}
			cases["home dest"] = [2]any{linv.Home(lf, hq), minv.Home(mf, hq)}
			cases["topology"] = [2]any{linv.Topology(lf), minv.Topology(mf)}
			cases["device"] = [2]any{linv.Device(lf, "dev_pc01", "asn"), minv.Device(mf, "dev_pc01", "asn")}
			cases["unresolved"] = [2]any{linv.Device(lf, "ep:10.30.0.77", "country"), minv.Device(mf, "ep:10.30.0.77", "country")}
			cases["flows"] = [2]any{linv.Flows(lf, projection.FlowQuery{Limit: 1000}), minv.Flows(mf, projection.FlowQuery{Limit: 1000})}
			for name, pair := range cases {
				got, want := normalize(t, pair[0]), normalize(t, pair[1])
				if !reflect.DeepEqual(got, want) {
					gb, _ := json.Marshal(got)
					wb, _ := json.Marshal(want)
					t.Errorf("%s differs:\nlive: %.600s\nmock: %.600s", name, gb, wb)
				}
			}
			g := linv.Globe(lf, projection.GlobeQuery{Grouping: "asn"})
			if g.Summary.Basis != "boundary_counter" || len(g.Destinations) == 0 {
				t.Fatalf("live summary should come from WAN counters: %+v", g.Summary)
			}
		})
	}
}

func sample(sec int, exporter string) flow.Sample {
	sp, dp := 50000, 443
	return flow.Sample{
		ObservedAt: epoch.Add(time.Duration(sec)*time.Second + 100*time.Millisecond), ExporterID: exporter, AgentAddress: "192.0.2.9",
		SrcIP: "10.99.0.5", DstIP: "203.0.113.200", Protocol: flow.TCP, SrcPort: &sp, DstPort: &dp,
		SampledPacketLength: 1000, SamplingRate: 100, EstimatedBytes: 100_000,
	}
}

func TestUnknownExporterLateSamplesAndStatus(t *testing.T) {
	now := epoch.Add(100 * time.Second)
	s, err := New(Options{Epoch: epoch, InternalCIDRs: []string{"10.0.0.0/8"}, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	if st := s.Status(); st.Collector.Status != "disconnected" || st.Live || st.Mode != "live" || st.Mock != nil {
		t.Fatalf("before any data: %+v", st)
	}
	s.Advance(now)
	s.Publish(collector.Batch{ReceivedAt: now, Flows: []flow.Sample{sample(98, "192.0.2.9/0"), sample(10, "192.0.2.9/0")}})
	s.Advance(now.Add(time.Second))
	f, inv := s.Snapshot()
	if len(f.Flows) != 1 || f.Flows[0].Used.ExporterID != "192.0.2.9/0" {
		t.Fatalf("flows: %+v", f.Flows)
	}
	if *f.Flows[0].InternalNodeID != "ep:10.99.0.5" {
		t.Fatal("empty inventory: internal endpoint must be unresolved, not dropped")
	}
	g := inv.Globe(f, projection.GlobeQuery{Grouping: "asn"})
	if len(g.Destinations) != 1 || g.Destinations[0].Location != nil || g.Summary.Basis != "sampled_sum" {
		t.Fatalf("no GeoIP / no counters: unknown location and sampled summary expected: %+v", g)
	}
	if g.Destinations[0].OutboundBps.Value != 100_000*8/5.0 {
		t.Fatalf("estimate: %v", g.Destinations[0].OutboundBps.Value)
	}
	if s.lateSamples.Load() != 1 {
		t.Fatalf("late samples: %d", s.lateSamples.Load())
	}
	if st := s.Status(); st.Collector.Status != "connected" || !st.Live {
		t.Fatalf("after data: %+v", st)
	}
	s.Advance(now.Add(time.Minute))
	if st := s.Status(); st.Collector.Status != "stale" || st.Live || st.Collector.LastDatagramAt == nil {
		t.Fatalf("after silence: %+v", st)
	}
	var obs = inv.Destination(f, "asn:unknown", "", "")
	if obs == nil || obs.ObservationPoints[0].ExporterName != "192.0.2.9" {
		t.Fatalf("discovered exporter should be named by agent address: %+v", obs)
	}
}

func TestWANCountersOnlyFromBoundaryInterface(t *testing.T) {
	inv, _ := inventory.Load("../../configs/inventory.demo.yaml")
	now := epoch.Add(50 * time.Second)
	s, _ := New(Options{Epoch: epoch, Inventory: inv, InternalCIDRs: mock.DefaultInternalCIDRs, Now: func() time.Time { return now }})
	obs := func(sec int, ifIndex int, in, out uint64, exporter string) counters.Observation {
		return counters.Observation{ObservedAt: epoch.Add(time.Duration(sec) * time.Second), ExporterID: exporter, AgentAddress: "x", IfIndex: ifIndex, InOctets: in, OutOctets: out, IfSpeedBps: 1e10}
	}
	s.Publish(collector.Batch{Counters: []counters.Observation{
		obs(40, 1, 0, 0, "10.0.0.1/0"), obs(45, 1, 6_250_000, 625_000, "10.0.0.1/0"), // WAN: 10 Mb/s in, 1 Mb/s out
		obs(40, 3, 0, 0, "10.0.0.2/0"), obs(45, 3, 1e9, 1e9, "10.0.0.2/0"), // core switch port: ignored
		obs(40, 2, 0, 0, "10.0.0.1/0"), obs(45, 2, 1e9, 1e9, "10.0.0.1/0"), // router LAN port: ignored
	}})
	s.Advance(now)
	f, invp := s.Snapshot()
	sum := invp.Globe(f, projection.GlobeQuery{Grouping: "asn"}).Summary
	if sum.Basis != "boundary_counter" || sum.Download.Value != 10_000_000 || sum.Upload.Value != 1_000_000 ||
		sum.Download.MeasurementKind != c.KindCounter || *sum.Download.IntervalSeconds != 5 {
		t.Fatalf("summary: %+v", sum)
	}
}

// Guards the parity test above: losing a single datagram must be visible.
func TestParityDetectsLostDatagram(t *testing.T) {
	const T = mock.StartTick + 17
	mb, _ := mock.NewBackend(mock.Options{Seed: 42, Scenario: "default", Speed: 1, Epoch: epoch})
	mb.SetTick(T)
	sc, _ := mock.BuildScenario("default", 42)
	engine, _ := mock.NewEngine(sc, 42)
	gen := sflowgen.New(engine)
	src := demoSource(t, epoch)
	col := collector.New(collector.Options{Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}, src)
	for sec := T - 6; sec <= T; sec++ {
		src.Advance(epoch.Add(time.Duration(sec) * time.Second))
		for i, d := range gen.Second(sec, uint32(sec*1000)) {
			if sec == T-1 && i == 2 {
				continue
			}
			col.HandleDatagram(d, netip.AddrPort{}, epoch.Add(time.Duration(sec)*time.Second))
		}
	}
	src.Advance(epoch.Add(time.Duration(T+1) * time.Second))
	lf, linv := src.Snapshot()
	mf, minv := mb.Snapshot()
	q := projection.HomeQuery{}
	if reflect.DeepEqual(normalize(t, linv.Home(lf, q)), normalize(t, minv.Home(mf, q))) {
		t.Fatal("a lost datagram went unnoticed: parity comparison is not sensitive")
	}
}
