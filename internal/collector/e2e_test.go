package collector

import (
	"context"
	"io"
	"log/slog"
	"net"
	"sync"
	"testing"
	"time"

	"network-traffic-visualizer/internal/counters"
	"network-traffic-visualizer/internal/flow"
	"network-traffic-visualizer/internal/mock"
	"network-traffic-visualizer/internal/sflowgen"
)

type captureSink struct {
	mu       sync.Mutex
	flows    []flow.Sample
	counters []counters.Observation
}

func (c *captureSink) Publish(b Batch) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.flows = append(c.flows, b.Flows...)
	c.counters = append(c.counters, b.Counters...)
}

func (c *captureSink) snapshot() ([]flow.Sample, []counters.Observation) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]flow.Sample(nil), c.flows...), append([]counters.Observation(nil), c.counters...)
}

// sFlow generated from the mock, sent over real UDP, decoded and normalized
// by the collector must reproduce exactly the mock's sampled observations.
func TestEndToEndGeneratorToCollector(t *testing.T) {
	sc, err := mock.BuildScenario("default", 42)
	if err != nil {
		t.Fatal(err)
	}
	engine, err := mock.NewEngine(sc, 42)
	if err != nil {
		t.Fatal(err)
	}
	gen := sflowgen.New(engine)

	sink := &captureSink{}
	c := New(Options{Listen: "127.0.0.1:0", Workers: 4, QueueSize: 10_000, ReadBuffer: 8 << 20,
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}, sink)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go c.Run(ctx)
	addr, err := c.Addr(ctx)
	if err != nil {
		t.Fatal(err)
	}
	conn, err := net.Dial("udp", addr.String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	const tick = mock.StartTick + 5 // multiple of 5: includes a counter poll
	datagrams := gen.Second(tick, 1000)
	for _, d := range datagrams {
		if len(d) > sflowgen.MaxDatagramBytes {
			t.Fatalf("datagram %d bytes exceeds limit", len(d))
		}
		if _, err := conn.Write(d); err != nil {
			t.Fatal(err)
		}
		time.Sleep(50 * time.Microsecond) // stay friendly to the loopback buffer
	}

	// Expected from the engine, keyed by exporter agent + flow key.
	agentOf := map[string]string{}
	for _, e := range sc.Inventory.Exporters {
		agentOf[e.ID] = e.AgentAddress + "/0"
	}
	type agg struct {
		samples int
		bytes   float64
	}
	want := map[string]agg{}
	wantTotal := 0
	for _, o := range engine.SecondObservations(tick) {
		if o.SampleCount == 0 {
			continue
		}
		k := agentOf[o.ExporterID] + "|" + o.Key()
		a := want[k]
		a.samples += o.SampleCount
		a.bytes += o.EstimatedBytes
		want[k] = a
		wantTotal += o.SampleCount
	}

	deadline := time.Now().Add(5 * time.Second)
	var flows []flow.Sample
	var ctrs []counters.Observation
	for time.Now().Before(deadline) {
		flows, ctrs = sink.snapshot()
		if len(flows) >= wantTotal && len(ctrs) >= 1 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	m := c.Metrics()
	if m.DatagramsDropped.Load() != 0 || m.DatagramsInvalid.Load() != 0 {
		t.Fatalf("dropped=%d invalid=%d", m.DatagramsDropped.Load(), m.DatagramsInvalid.Load())
	}
	if int(m.DatagramsReceived.Load()) != len(datagrams) {
		t.Fatalf("received %d of %d datagrams", m.DatagramsReceived.Load(), len(datagrams))
	}

	got := map[string]agg{}
	for _, f := range flows {
		o := flow.WindowObservation{SrcIP: f.SrcIP, DstIP: f.DstIP, Protocol: f.Protocol, SrcPort: f.SrcPort, DstPort: f.DstPort}
		k := f.ExporterID + "|" + o.Key()
		a := got[k]
		a.samples++
		a.bytes += f.EstimatedBytes
		got[k] = a
		if f.EstimatedBytes != float64(f.SampledPacketLength*f.SamplingRate) {
			t.Fatalf("estimated_bytes must be length × rate: %+v", f)
		}
	}
	if len(got) != len(want) {
		t.Fatalf("flow keys: got %d want %d", len(got), len(want))
	}
	for k, w := range want {
		if got[k] != w {
			t.Errorf("%s: got %+v want %+v", k, got[k], w)
		}
	}

	in, out := engine.WanOctets(tick)
	if len(ctrs) != 1 || ctrs[0].ExporterID != "10.0.0.1/0" || ctrs[0].IfIndex != 1 ||
		ctrs[0].InOctets != uint64(in) || ctrs[0].OutOctets != uint64(out) || !ctrs[0].OperUp {
		t.Fatalf("counter observation: %+v", ctrs)
	}
	t.Logf("%d datagrams, %d flow samples, %d flow keys reproduced exactly", len(datagrams), len(flows), len(got))
}
