package history

import (
	"context"
	"errors"
	"math"
	"sync"
	"testing"
	"time"

	"network-traffic-visualizer/internal/flow"
	"network-traffic-visualizer/internal/projection"
)

var t0 = time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)

func ip(v int) *int { return &v }

func row(sec int, exporter, src, dst string, sport, dport, samples int, bytes float64, srcIn, dstIn bool) FlowRow {
	return FlowRow{
		Start: t0.Add(time.Duration(sec) * time.Second),
		Obs: flow.WindowObservation{
			ExporterID: exporter, InputIfIndex: ip(3), SrcIP: src, DstIP: dst, Protocol: flow.TCP,
			SrcPort: ip(sport), DstPort: ip(dport), SamplingRate: 256, SampleCount: samples, EstimatedBytes: bytes,
		},
		SrcInternal: srcIn, DstInternal: dstIn,
	}
}

// storeContract is run against every Store implementation (Memory here,
// ClickHouse in clickhouse_test.go when a server is available).
func storeContract(t *testing.T, s Store) {
	ctx := context.Background()
	rows := []FlowRow{
		// Upload from 192.168.1.10, seen by the edge (boundary) and a core switch.
		row(0, "edge", "192.168.1.10", "203.0.113.5", 50000, 443, 2, 2000, true, false),
		row(0, "core", "192.168.1.10", "203.0.113.5", 50000, 443, 3, 3000, true, false),
		row(1, "edge", "192.168.1.10", "203.0.113.5", 50000, 443, 1, 1000, true, false),
		// Download over IPv6.
		row(1, "edge", "2001:db8::1", "fd00::10", 443, 51000, 4, 4000, false, true),
		// Internal backup.
		row(2, "core", "192.168.1.10", "192.168.1.20", 40000, 873, 5, 5000, true, true),
		// Outside the queried range.
		row(10, "edge", "192.168.1.10", "203.0.113.5", 50000, 443, 9, 9000, true, false),
	}
	if err := s.WriteFlows(ctx, rows); err != nil {
		t.Fatal(err)
	}
	counters := []CounterRow{
		{At: t0.Add(2 * time.Second), ExporterID: "edge", IfIndex: 1, RxBps: 100, TxBps: 10, IntervalSeconds: 2},
		{At: t0.Add(5 * time.Second), ExporterID: "edge", IfIndex: 1, RxBps: 400, TxBps: 40, IntervalSeconds: 3},
		{At: t0.Add(5 * time.Second), ExporterID: "edge2", IfIndex: 7, RxBps: 1000, TxBps: 100, IntervalSeconds: 5},
	}
	if err := s.WriteCounters(ctx, counters); err != nil {
		t.Fatal(err)
	}
	raw := s.Tiers()[0]

	t.Run("flows merge per exporter and key", func(t *testing.T) {
		got, err := s.Flows(ctx, raw, t0, t0.Add(5*time.Second))
		if err != nil {
			t.Fatal(err)
		}
		byKey := map[string]MergedFlow{}
		for _, m := range got {
			byKey[m.Obs.ExporterID+"|"+m.Obs.Key()] = m
		}
		if len(byKey) != 4 {
			t.Fatalf("want 4 merged rows, got %d: %+v", len(got), got)
		}
		up := byKey["edge|192.168.1.10|203.0.113.5|tcp|50000|443"]
		if up.Obs.SampleCount != 3 || up.Obs.EstimatedBytes != 3000 || !up.LastStart.Equal(t0.Add(time.Second)) {
			t.Errorf("edge upload: %+v", up)
		}
		if up.Obs.InputIfIndex == nil || *up.Obs.InputIfIndex != 3 || up.Obs.SamplingRate != 256 {
			t.Errorf("edge upload metadata: %+v", up.Obs)
		}
		down, ok := byKey["edge|2001:db8::1|fd00::10|tcp|443|51000"]
		if !ok || down.Obs.SampleCount != 4 {
			t.Errorf("IPv6 download missing or wrong: %+v (keys %v)", down, byKey)
		}
		if _, ok := byKey["core|192.168.1.10|192.168.1.20|tcp|40000|873"]; !ok {
			t.Errorf("internal flow missing")
		}
	})

	t.Run("timeline counts each flow once", func(t *testing.T) {
		dd := Dedup{ExternalRank: map[string]int{"edge": 0, "core": 1}, InternalRank: map[string]int{"core": 0, "edge": 1}, UnknownExternal: 3, UnknownInternal: 3}
		got, err := s.Timeline(ctx, raw, t0, t0.Add(4*time.Second), 2*time.Second, dd)
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 2 {
			t.Fatalf("want 2 buckets, got %+v", got)
		}
		b0, b1 := got[0], got[1]
		if !b0.Start.Equal(t0) || !b1.Start.Equal(t0.Add(2*time.Second)) {
			t.Fatalf("bucket starts: %v %v", b0.Start, b1.Start)
		}
		// Upload: edge preferred over core → 2000 + 1000, never 2000+3000+1000.
		if b0.OutboundBytes != 3000 || b0.OutboundSamples != 3 || b0.InboundBytes != 4000 || b0.InternalBytes != 0 {
			t.Errorf("bucket 0: %+v", b0)
		}
		if b1.InternalBytes != 5000 || b1.InternalSamples != 5 || b1.OutboundBytes != 0 {
			t.Errorf("bucket 1: %+v", b1)
		}
	})

	t.Run("counter series weights by interval and sums exporters", func(t *testing.T) {
		got, err := s.CounterSeries(ctx, t0, t0.Add(5*time.Second), 5*time.Second)
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 1 {
			t.Fatalf("want 1 bucket, got %+v", got)
		}
		// edge: (100*2 + 400*3)/5 = 280; edge2: 1000 → 1280.
		b := got[0]
		if math.Abs(b.RxBps-1280) > 1e-9 || math.Abs(b.TxBps-128) > 1e-9 || b.CoveredSeconds != 5 {
			t.Errorf("counter bucket: %+v", b)
		}
		got, _ = s.CounterSeries(ctx, t0, t0.Add(4*time.Second), 2*time.Second)
		if len(got) != 1 || !got[0].Start.Equal(t0) || got[0].RxBps != 100 {
			t.Errorf("counter at t0+2s belongs to the first 2s bucket: %+v", got)
		}
	})

	t.Run("coverage", func(t *testing.T) {
		e, l, ok, err := s.Coverage(ctx)
		if err != nil || !ok || !e.Equal(t0) || !l.Equal(t0.Add(10*time.Second)) {
			t.Errorf("coverage: %v %v %v %v", e, l, ok, err)
		}
	})
}

func TestMemoryStoreContract(t *testing.T) {
	storeContract(t, NewMemory(MemoryOptions{Retention: 24 * time.Hour, Now: func() time.Time { return t0.Add(time.Minute) }}))
}

func TestMemoryRetentionAndRowCap(t *testing.T) {
	now := t0.Add(100 * time.Second)
	m := NewMemory(MemoryOptions{Retention: 60 * time.Second, MaxRows: 3, Now: func() time.Time { return now }})
	ctx := context.Background()
	_ = m.WriteFlows(ctx, []FlowRow{
		row(10, "e", "10.0.0.1", "1.1.1.1", 1, 2, 1, 1, true, false), // older than retention
		row(50, "e", "10.0.0.1", "1.1.1.1", 1, 2, 1, 1, true, false),
		row(60, "e", "10.0.0.1", "1.1.1.1", 1, 2, 1, 1, true, false),
		row(70, "e", "10.0.0.1", "1.1.1.1", 1, 2, 1, 1, true, false),
		row(80, "e", "10.0.0.1", "1.1.1.1", 1, 2, 1, 1, true, false),
	})
	e, l, _, _ := m.Coverage(ctx)
	if !e.Equal(t0.Add(60*time.Second)) || !l.Equal(t0.Add(80*time.Second)) {
		t.Errorf("retention/cap: coverage %v..%v", e, l)
	}
}

type fakeBuilder struct{}

func (fakeBuilder) ObservationDedup() Dedup { return Dedup{} }

func TestResolvePicksTierAndAligns(t *testing.T) {
	now := t0
	store := &tiered{Memory: NewMemory(MemoryOptions{}), tiers: []Tier{{time.Second, 7 * 24 * time.Hour}, {time.Minute, 90 * 24 * time.Hour}, {time.Hour, 365 * 24 * time.Hour}}}
	svc := &Service{store: store, now: func() time.Time { return now }}
	cases := []struct {
		start, end         time.Time
		step               time.Duration
		wantStart, wantEnd time.Time
	}{
		{now.Add(-5 * time.Minute), now, time.Second, now.Add(-5 * time.Minute), now},
		{now.Add(-6 * time.Hour), now, time.Second, now.Add(-6 * time.Hour), now},
		{now.Add(-7*time.Hour + 30*time.Second), now.Add(-30 * time.Second), time.Minute, now.Add(-7 * time.Hour), now},
		{now.Add(-8 * 24 * time.Hour), now.Add(-8*24*time.Hour + time.Hour), time.Minute, now.Add(-8 * 24 * time.Hour), now.Add(-8*24*time.Hour + time.Hour)},
		{now.Add(-30 * 24 * time.Hour), now, time.Hour, now.Add(-30 * 24 * time.Hour), now},
		{now.Add(-200 * 24 * time.Hour), now.Add(-199 * 24 * time.Hour), time.Hour, now.Add(-200 * 24 * time.Hour), now.Add(-199 * 24 * time.Hour)},
	}
	for _, c := range cases {
		tier, s, e, err := svc.Resolve(c.start, c.end)
		if err != nil || tier.Step != c.step || !s.Equal(c.wantStart) || !e.Equal(c.wantEnd) {
			t.Errorf("Resolve(%v, %v) = %v %v %v %v; want step %v %v..%v", c.start, c.end, tier.Step, s, e, err, c.step, c.wantStart, c.wantEnd)
		}
	}
	for _, bad := range [][2]time.Time{{now, now}, {now, now.Add(-time.Second)}, {now, now.Add(time.Hour)}, {now.Add(-500 * 24 * time.Hour), now}} {
		var re *RangeError
		if _, _, _, err := svc.Resolve(bad[0], bad[1]); !errors.As(err, &re) {
			t.Errorf("Resolve(%v, %v) should be a RangeError, got %v", bad[0], bad[1], err)
		}
	}
}

type tiered struct {
	*Memory
	tiers []Tier
}

func (s *tiered) Tiers() []Tier { return s.tiers }

func TestPickStep(t *testing.T) {
	cases := []struct {
		span, base, want time.Duration
	}{
		{time.Hour, time.Second, 5 * time.Second},
		{5 * time.Minute, time.Second, time.Second},
		{24 * time.Hour, time.Minute, 2 * time.Minute},
		{7 * 24 * time.Hour, time.Minute, 15 * time.Minute},
		{365 * 24 * time.Hour, time.Hour, 24 * time.Hour},
	}
	for _, c := range cases {
		if got := pickStep(c.span, c.base, MaxTimelinePoints); got != c.want {
			t.Errorf("pickStep(%v, %v) = %v, want %v", c.span, c.base, got, c.want)
		}
	}
}

func TestTimelineStepKeepsPointLimitAfterAlignment(t *testing.T) {
	m := NewMemory(MemoryOptions{Retention: 24 * time.Hour, Now: func() time.Time { return t0.Add(time.Hour) }})
	svc := NewService(m, fakeBuilder{}, func() time.Time { return t0.Add(time.Hour) })
	// 10 min from an unaligned start: 30 s steps would need 21 buckets.
	tl, err := svc.Timeline(context.Background(), t0.Add(7*time.Second), t0.Add(607*time.Second), 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(tl.Points) > 20 || tl.Step != time.Minute {
		t.Errorf("step %v, %d points", tl.Step, len(tl.Points))
	}
}

func TestServiceTimelineFillsGapsAndUsesCounters(t *testing.T) {
	m := NewMemory(MemoryOptions{Retention: 24 * time.Hour, Now: func() time.Time { return t0.Add(time.Hour) }})
	ctx := context.Background()
	_ = m.WriteFlows(ctx, []FlowRow{
		row(0, "e", "10.0.0.1", "1.1.1.1", 1, 2, 1, 1000, true, false),
		row(11, "e", "10.0.0.1", "1.1.1.1", 1, 2, 1, 1000, true, false), // newest second: data ends at 12s
	})
	_ = m.WriteCounters(ctx, []CounterRow{{At: t0.Add(5 * time.Second), ExporterID: "e", RxBps: 80, TxBps: 8, IntervalSeconds: 5}})
	svc := NewService(m, fakeBuilder{}, func() time.Time { return t0.Add(time.Hour) })
	tl, err := svc.Timeline(ctx, t0, t0.Add(15*time.Second), 3)
	if err != nil {
		t.Fatal(err)
	}
	if tl.Step != 5*time.Second || len(tl.Points) != 3 {
		t.Fatalf("step %v, %d points", tl.Step, len(tl.Points))
	}
	p0, p1 := tl.Points[0], tl.Points[1]
	if !p0.Data || p0.Outbound != 1000*8/5.0 || p0.WanDownload == nil || *p0.WanDownload != 80 {
		t.Errorf("point 0: %+v", p0)
	}
	if p1.Data {
		t.Errorf("gap must be explicit without data: %+v", p1)
	}
	if p0.Seconds != 5 {
		t.Errorf("first bucket starts with the data: full 5 s, got %v", p0.Seconds)
	}
	// [10s,15s) holds data only up to 12s: averaged over 2s, not 5s.
	if p2 := tl.Points[2]; p2.Seconds != 2 || p2.Outbound != 1000*8/2.0 {
		t.Errorf("partial newest bucket: %+v", p2)
	}
}

func (fakeBuilder) HistoryFrame([]flow.WindowObservation, time.Time, int, *projection.WanRates) (*projection.Frame, *projection.Inventory) {
	return nil, nil
}

// failingStore fails the first n writes.
type failingStore struct {
	*Memory
	mu    sync.Mutex
	fails int
}

func (f *failingStore) WriteFlows(ctx context.Context, rows []FlowRow) error {
	f.mu.Lock()
	if f.fails > 0 {
		f.fails--
		f.mu.Unlock()
		return errors.New("boom")
	}
	f.mu.Unlock()
	return f.Memory.WriteFlows(ctx, rows)
}

func TestRecorderRetriesAndDropsWhenFull(t *testing.T) {
	store := &failingStore{Memory: NewMemory(MemoryOptions{Retention: 24 * time.Hour, Now: func() time.Time { return t0 }}), fails: 1}
	r := NewRecorder(store, RecorderOptions{QueueSize: 1, FlushInterval: 10 * time.Millisecond})
	r.RecordFlows([]FlowRow{row(0, "e", "10.0.0.1", "1.1.1.1", 1, 2, 1, 1, true, false)})
	r.RecordFlows([]FlowRow{row(1, "e", "10.0.0.1", "1.1.1.1", 1, 2, 1, 1, true, false)}) // queue full: dropped
	if r.queuedDropped.Load() != 1 {
		t.Fatalf("queue-full drop not counted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { r.Run(ctx); close(done) }()
	deadline := time.Now().Add(2 * time.Second)
	for r.writtenFlows.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	<-done
	if r.writtenFlows.Load() != 1 || r.writeErrors.Load() != 1 {
		t.Fatalf("written %d, errors %d", r.writtenFlows.Load(), r.writeErrors.Load())
	}
	if _, _, ok, _ := store.Coverage(context.Background()); !ok {
		t.Fatal("row not persisted after retry")
	}
}

// A window reaching before the first stored second is averaged over the
// covered part only, and the frame says which range that is.
func TestSnapshotClampsToStoredRange(t *testing.T) {
	m := NewMemory(MemoryOptions{Retention: 24 * time.Hour, Now: func() time.Time { return t0.Add(time.Hour) }})
	ctx := context.Background()
	var rows []FlowRow
	for sec := 100; sec < 160; sec++ {
		rows = append(rows, row(sec, "e", "10.0.0.1", "1.1.1.1", 1, 2, 1, 1000, true, false))
	}
	_ = m.WriteFlows(ctx, rows)
	b := &recordingBuilder{}
	svc := NewService(m, b, func() time.Time { return t0.Add(time.Hour) })
	if _, err := svc.Snapshot(ctx, t0, t0.Add(300*time.Second)); err != nil {
		t.Fatal(err)
	}
	if !b.start.Equal(t0.Add(100*time.Second)) || b.seconds != 60 {
		t.Errorf("window not clamped to stored data: start %v, %d s", b.start, b.seconds)
	}
}

type recordingBuilder struct {
	fakeBuilder
	start   time.Time
	seconds int
}

func (r *recordingBuilder) HistoryFrame(_ []flow.WindowObservation, start time.Time, seconds int, _ *projection.WanRates) (*projection.Frame, *projection.Inventory) {
	r.start, r.seconds = start, seconds
	return nil, nil
}

func TestTimelineFirstBucketAveragesFromFirstStoredSecond(t *testing.T) {
	m := NewMemory(MemoryOptions{Retention: 24 * time.Hour, Now: func() time.Time { return t0.Add(time.Hour) }})
	_ = m.WriteFlows(context.Background(), []FlowRow{
		row(57, "e", "10.0.0.1", "1.1.1.1", 1, 2, 1, 300, true, false),
		row(90, "e", "10.0.0.1", "1.1.1.1", 1, 2, 1, 600, true, false),
	})
	svc := NewService(m, fakeBuilder{}, func() time.Time { return t0.Add(time.Hour) })
	tl, err := svc.Timeline(context.Background(), t0, t0.Add(120*time.Second), 2)
	if err != nil {
		t.Fatal(err)
	}
	// [0,60s): recording starts at 57s → 3 s covered.
	if p := tl.Points[0]; p.Seconds != 3 || p.Outbound != 300*8/3.0 {
		t.Errorf("first bucket: %+v", p)
	}
}
