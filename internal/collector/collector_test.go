package collector

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net"
	"net/netip"
	"strings"
	"testing"
	"time"

	"network-traffic-visualizer/internal/counters"
	"network-traffic-visualizer/internal/packet"
	"network-traffic-visualizer/internal/sflow"
)

var quiet = slog.New(slog.NewTextHandler(io.Discard, nil))

func datagram(agent string, seq, uptime uint32, samples ...[]byte) []byte {
	b := &sflow.DatagramBuilder{Agent: netip.MustParseAddr(agent)}
	for _, s := range samples {
		b.Add(s)
	}
	return b.Bytes(seq, uptime)
}

func ethSample(rate uint32, frame int) []byte {
	h := packet.BuildFrame(netip.MustParseAddr("10.20.0.10"), netip.MustParseAddr("198.51.100.10"), packet.ProtoTCP, 50000, 443, frame, nil)
	return sflow.EncodeFlowSample(sflow.FlowSample{SamplingRate: rate, Input: sflow.Interface{Value: 3}, Output: sflow.Interface{Value: 1},
		RawHeader: &sflow.RawPacketHeader{Protocol: sflow.HeaderProtocolEthernet, FrameLength: uint32(frame), Header: h}}, false)
}

func TestNormalizeRawHeader(t *testing.T) {
	d, _ := sflow.Decode(datagram("10.0.0.2", 1, 1, ethSample(512, 600)))
	flows, _, st := Normalize(d, time.Unix(100, 0))
	if len(flows) != 1 || st != (NormalizeStats{}) {
		t.Fatalf("flows=%v stats=%+v", flows, st)
	}
	f := flows[0]
	if f.ExporterID != "10.0.0.2/0" || f.SrcIP != "10.20.0.10" || *f.DstPort != 443 || f.Protocol != "tcp" ||
		f.AddressFamily != "ipv4" || f.SampledPacketLength != 600 || f.EstimatedBytes != 600*512 ||
		*f.InputIfIndex != 3 || *f.OutputIfIndex != 1 || !f.ObservedAt.Equal(time.Unix(100, 0)) {
		t.Fatalf("got %+v", f)
	}
}

func TestNormalizeFallbacksAndRejects(t *testing.T) {
	sampledV6 := sflow.EncodeFlowSample(sflow.FlowSample{SamplingRate: 100,
		SampledIPv6:    &sflow.SampledIP{Length: 1300, Protocol: 17, Src: netip.MustParseAddr("2001:db8::1"), Dst: netip.MustParseAddr("fd00::1"), SrcPort: 443, DstPort: 5000},
		ExtendedSwitch: &sflow.ExtendedSwitch{SrcVLAN: 40}}, false)
	arp := make([]byte, 42)
	arp[12], arp[13] = 0x08, 0x06
	nonIP := sflow.EncodeFlowSample(sflow.FlowSample{SamplingRate: 100, RawHeader: &sflow.RawPacketHeader{Protocol: 1, FrameLength: 60, Header: arp}}, false)
	zeroRate := ethSample(0, 100)
	d, err := sflow.Decode(datagram("10.0.0.1", 1, 1, sampledV6, nonIP, zeroRate,
		sflow.EncodeCounterSample(sflow.CounterSample{SourceIDIndex: 1}, false)))
	if err != nil {
		t.Fatal(err)
	}
	flows, ctrs, st := Normalize(d, time.Now())
	if len(flows) != 1 || flows[0].AddressFamily != "ipv6" || *flows[0].VLAN != 40 || flows[0].EstimatedBytes != 130000 {
		t.Fatalf("sampled_ipv6 fallback: %+v", flows)
	}
	if st.NonIPSamples != 1 || st.InvalidSamplingRate != 1 || st.CounterSamplesNoIface != 1 || len(ctrs) != 0 {
		t.Fatalf("stats: %+v ctrs=%d", st, len(ctrs))
	}
}

type nopSink struct{}

func (nopSink) Publish(Batch) {}

func TestSequenceGapsAndRestarts(t *testing.T) {
	c := New(Options{Logger: quiet}, nopSink{})
	src := netip.MustParseAddrPort("10.0.0.2:6343")
	feed := func(seq, uptime uint32) {
		c.HandleDatagram(datagram("10.0.0.2", seq, uptime, ethSample(1, 64)), src, time.Now())
	}
	feed(1, 1_000_000)
	feed(2, 1_001_000)
	feed(5, 1_002_000) // 3 and 4 not (yet) seen
	if lost := c.Metrics().DatagramsLost.Load(); lost != 2 {
		t.Fatalf("lost=%d, want 2", lost)
	}
	feed(4, 1_002_500) // arrives late (reordering): no longer missing
	feed(1, 500)       // uptime went backwards: restart; 3 stays lost
	feed(2, 1_500)
	m := c.Metrics()
	if m.DatagramsLost.Load() != 1 || m.AgentRestarts.Load() != 1 {
		t.Fatalf("lost=%d restarts=%d", m.DatagramsLost.Load(), m.AgentRestarts.Load())
	}
}

func TestInvalidDatagramsAreCountedByKind(t *testing.T) {
	c := New(Options{Logger: quiet}, nopSink{})
	c.HandleDatagram([]byte{0, 0, 0, 4}, netip.AddrPort{}, time.Now())
	c.HandleDatagram([]byte{0, 0}, netip.AddrPort{}, time.Now())
	var buf bytes.Buffer
	c.Metrics().WritePrometheus(&buf)
	out := buf.String()
	for _, want := range []string{
		"ntv_collector_datagrams_invalid_total 2",
		`ntv_collector_decoder_errors_total{kind="unsupported_version"} 1`,
		`ntv_collector_decoder_errors_total{kind="truncated"} 1`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}

type blockingSink struct{ release chan struct{} }

func (b blockingSink) Publish(Batch) { <-b.release }

func runCollector(t *testing.T, opts Options, sink Sink) (*Collector, net.Conn, context.CancelFunc) {
	t.Helper()
	opts.Listen, opts.Logger = "127.0.0.1:0", quiet
	c := New(opts, sink)
	ctx, cancel := context.WithCancel(context.Background())
	go c.Run(ctx)
	addr, err := c.Addr(ctx)
	if err != nil {
		t.Fatal(err)
	}
	conn, err := net.Dial("udp", addr.String())
	if err != nil {
		t.Fatal(err)
	}
	return c, conn, cancel
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	for deadline := time.Now().Add(3 * time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		if cond() {
			return
		}
	}
	t.Fatal("condition not met")
}

func TestQueueFullDropsInsteadOfBlocking(t *testing.T) {
	sink := blockingSink{release: make(chan struct{})}
	c, conn, cancel := runCollector(t, Options{Workers: 1, QueueSize: 2}, sink)
	defer cancel()
	defer close(sink.release)
	for i := 0; i < 20; i++ {
		conn.Write(datagram("10.0.0.2", uint32(i+1), 1, ethSample(1, 64)))
	}
	waitFor(t, func() bool { return c.Metrics().DatagramsReceived.Load() == 20 })
	if d := c.Metrics().DatagramsDropped.Load(); d == 0 || d > 18 {
		t.Fatalf("expected drops with a full queue, got %d", d)
	}
}

func TestAllowedSources(t *testing.T) {
	c, conn, cancel := runCollector(t, Options{AllowedSources: []netip.Prefix{netip.MustParsePrefix("192.0.2.0/24")}}, nopSink{})
	defer cancel()
	conn.Write(datagram("10.0.0.2", 1, 1, ethSample(1, 64)))
	waitFor(t, func() bool { return c.Metrics().DatagramsRejected.Load() == 1 })
	if c.Metrics().FlowSamples.Load() != 0 {
		t.Fatal("rejected datagram must not be processed")
	}
}

func TestTrackerComputesCounterRates(t *testing.T) {
	tr := counters.NewTracker(time.Minute)
	base := time.Unix(1000, 0)
	o := counters.Observation{ExporterID: "x", IfIndex: 1, ObservedAt: base, InOctets: 1000, OutOctets: 0, IfSpeedBps: 1e9}
	if _, ok := tr.Update(o); ok {
		t.Fatal("first sample has no rate")
	}
	o.ObservedAt, o.InOctets, o.OutOctets = base.Add(10*time.Second), 126_000, 12_500
	r, ok := tr.Update(o)
	if !ok || r.RxBps != 100_000 || r.TxBps != 10_000 {
		t.Fatalf("got %+v", r)
	}
	o.ObservedAt, o.InOctets = base.Add(20*time.Second), 10 // reset
	if _, ok := tr.Update(o); ok {
		t.Fatal("counter reset must not produce a rate")
	}
}

func TestDebugFlowSinkIsBounded(t *testing.T) {
	var buf bytes.Buffer
	s := NewDebugFlowSink(&buf, 2)
	d, _ := sflow.Decode(datagram("10.0.0.2", 1, 1, ethSample(1, 64), ethSample(1, 64), ethSample(1, 64)))
	flows, _, _ := Normalize(d, time.Unix(5, 0))
	s.Publish(Batch{ReceivedAt: time.Unix(5, 0), Flows: flows})
	if n := strings.Count(buf.String(), "\n"); n != 2 {
		t.Fatalf("want 2 lines, got %d", n)
	}
}
