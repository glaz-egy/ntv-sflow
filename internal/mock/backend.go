package mock

import (
	"math"
	"sync"
	"time"

	"network-traffic-visualizer/internal/aggregation"
	c "network-traffic-visualizer/internal/contract"
	"network-traffic-visualizer/internal/counters"
	"network-traffic-visualizer/internal/devices"
	"network-traffic-visualizer/internal/enrichment"
	"network-traffic-visualizer/internal/flow"
	"network-traffic-visualizer/internal/history"
	"network-traffic-visualizer/internal/projection"
	"network-traffic-visualizer/internal/topology"
)

type Options struct {
	Seed     int
	Scenario string
	Speed    float64
	// Epoch is the wall-clock time of sim tick 0.
	Epoch time.Time
	// Optional overrides from runtime configuration.
	InternalCIDRs []string
	Origin        *topology.Origin
	// History receives each completed sim second (optional). The first
	// Advance also records the seconds before StartTick.
	History history.Sink
}

// Backend serves projections of the mock engine. Safe for concurrent use.
type Backend struct {
	opts   Options
	engine *Engine
	inv    *projection.Inventory
	ctx    aggregation.Context

	mu            sync.Mutex
	tick          int
	frame         *projection.Frame
	flushed       int // sim seconds ≤ flushed were handed to History
	lastCounterAt int // tick of the last recorded WAN counter poll
}

func NewBackend(opts Options) (*Backend, error) {
	sc, err := BuildScenario(opts.Scenario, opts.Seed)
	if err != nil {
		return nil, err
	}
	if len(opts.InternalCIDRs) > 0 {
		sc.Inventory.InternalCIDRs = opts.InternalCIDRs
	}
	if opts.Origin != nil {
		sc.Inventory.Origin = *opts.Origin
	}
	engine, err := NewEngine(sc, opts.Seed)
	if err != nil {
		return nil, err
	}
	classifier, err := enrichment.NewClassifier(sc.Inventory.InternalCIDRs)
	if err != nil {
		return nil, err
	}
	reg := devices.NewRegistry(sc.Inventory.Devices)
	inv, err := projection.NewInventory(reg, sc.Inventory.Networks, sc.Inventory.Exporters, sc.Inventory.Topology, sc.Inventory.Origin, CountryAnchors)
	if err != nil {
		return nil, err
	}
	return &Backend{
		opts: opts, engine: engine, inv: inv, tick: StartTick, lastCounterAt: -1,
		ctx: aggregation.Context{
			Classifier: classifier, Registry: reg, Geo: sc.Geo,
			Exporters: sc.Inventory.Exporters, Policy: sc.Inventory.Policy,
		},
	}, nil
}

// EpochForStart aligns tick StartTick with `now` (whole seconds), like the TS provider.
func EpochForStart(now time.Time) time.Time {
	return now.Truncate(time.Second).Add(-StartTick * time.Second)
}

func (b *Backend) Description() string { return b.engine.Scenario.Description }

func (b *Backend) SetTick(t int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if t < 0 {
		t = 0
	}
	b.tick = t
}

func (b *Backend) Tick() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.tick
}

// Advance sets the tick from wall-clock time and speed. Returns true when a
// new tick was reached.
func (b *Backend) Advance(now time.Time) bool {
	elapsed := now.Sub(b.opts.Epoch).Seconds() - StartTick
	t := StartTick + int(math.Floor(elapsed*b.opts.Speed))
	b.mu.Lock()
	if t <= b.tick {
		b.mu.Unlock()
		return false
	}
	b.tick = t
	flows, counters := b.recordLocked()
	b.mu.Unlock()
	if b.opts.History != nil {
		b.opts.History.RecordFlows(flows)
		b.opts.History.RecordCounters(counters)
	}
	return true
}

// recordLocked returns history rows for completed sim seconds (mock windows
// include the current tick) and a new WAN counter poll. Row starts use the
// window labels: second t is the interval (t−1, t] (D-059). Caller holds b.mu.
func (b *Backend) recordLocked() ([]history.FlowRow, []history.CounterRow) {
	if b.opts.History == nil {
		return nil, nil
	}
	var flows []history.FlowRow
	through := b.engine.EffectiveTick(b.tick)
	for t := b.flushed + 1; t <= through; t++ {
		start := b.opts.Epoch.Add(time.Duration(t-1) * time.Second)
		for _, o := range b.engine.SecondObservations(t) {
			if o.SampleCount == 0 {
				continue
			}
			srcIn, _ := b.ctx.Classifier.IsInternal(o.SrcIP)
			dstIn, _ := b.ctx.Classifier.IsInternal(o.DstIP)
			flows = append(flows, history.FlowRow{Start: start, Obs: o, SrcInternal: srcIn, DstInternal: dstIn})
		}
	}
	if through > b.flushed {
		b.flushed = through
	}
	var counters []history.CounterRow
	if _, cur := b.engine.WanCounterReadings(b.tick); cur.At != b.lastCounterAt {
		if w := b.wanRates(); w != nil {
			ex := b.engine.BoundaryExporter()
			counters = append(counters, history.CounterRow{
				At: b.opts.Epoch.Add(time.Duration(cur.At) * time.Second), ExporterID: ex.ID, IfIndex: derefOr(ex.BoundaryIfIndex, 0),
				RxBps: w.DownloadBps, TxBps: w.UploadBps, IntervalSeconds: float64(w.IntervalSeconds),
			})
		}
		b.lastCounterAt = cur.At
	}
	return flows, counters
}

func derefOr(p *int, d int) int {
	if p == nil {
		return d
	}
	return *p
}

// HistoryFrame implements history.FrameBuilder.
func (b *Backend) HistoryFrame(obs []flow.WindowObservation, start time.Time, seconds int, wan *projection.WanRates) (*projection.Frame, *projection.Inventory) {
	return &projection.Frame{
		Flows: aggregation.Attribute(obs, b.ctx), Epoch: start,
		Tick: seconds, WindowEndTick: seconds, WindowSeconds: seconds, WAN: wan,
	}, b.inv
}

// ObservationDedup implements history.FrameBuilder.
func (b *Backend) ObservationDedup() history.Dedup {
	return history.NewDedup(b.ctx.Exporters, b.ctx.Policy)
}

// Now is the sim clock (it runs faster than wall time when speed > 1).
func (b *Backend) Now() time.Time {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.opts.Epoch.Add(time.Duration(b.tick) * time.Second)
}

// Snapshot returns the current frame and the inventory to project it with.
func (b *Backend) Snapshot() (*projection.Frame, *projection.Inventory) {
	b.mu.Lock()
	defer b.mu.Unlock()
	end := b.engine.EffectiveTick(b.tick)
	if b.frame != nil && b.frame.Tick == b.tick {
		return b.frame, b.inv
	}
	if b.frame != nil && b.frame.WindowEndTick == end {
		f := *b.frame
		f.Tick = b.tick
		b.frame = &f
		return b.frame, b.inv
	}
	b.frame = &projection.Frame{
		Flows:         aggregation.Attribute(b.engine.WindowObservations(b.tick), b.ctx),
		Epoch:         b.opts.Epoch,
		Tick:          b.tick,
		WindowEndTick: end,
		WindowSeconds: WindowSeconds,
		WAN:           b.wanRates(),
	}
	return b.frame, b.inv
}

func (b *Backend) wanRates() *projection.WanRates {
	prev, cur := b.engine.WanCounterReadings(b.tick)
	ex := b.engine.BoundaryExporter()
	opt := counters.Options{MaxIntervalSeconds: CounterIntervalSeconds * 3, IfSpeedBps: ex.IfSpeedBps}
	var pIn, pOut *counters.Reading
	if prev != nil {
		pIn = &counters.Reading{At: float64(prev.At), Octets: prev.InOctets, Width: 64}
		pOut = &counters.Reading{At: float64(prev.At), Octets: prev.OutOctets, Width: 64}
	}
	rIn := counters.Compute(pIn, counters.Reading{At: float64(cur.At), Octets: cur.InOctets, Width: 64}, opt)
	rOut := counters.Compute(pOut, counters.Reading{At: float64(cur.At), Octets: cur.OutOctets, Width: 64}, opt)
	if !rIn.OK || !rOut.OK {
		return nil // first poll / reset: fall back to sampled sum (D-034)
	}
	return &projection.WanRates{DownloadBps: rIn.Bps, UploadBps: rOut.Bps, IntervalSeconds: int(rIn.IntervalSeconds)}
}

func (b *Backend) Status() c.StatusResponse {
	f, _ := b.Snapshot()
	b.mu.Lock()
	stale := b.engine.IsStale(b.tick)
	b.mu.Unlock()
	collector := "connected"
	if stale {
		collector = "stale"
	}
	last := c.TS(f.At(f.WindowEndTick))
	return c.StatusResponse{
		Mode: "mock", Live: !stale,
		Collector:             c.CollectorStatus{Status: collector, LastDatagramAt: c.Ptr(last)},
		LastAggregateAt:       c.Ptr(last),
		ServerTime:            c.TS(f.At(f.Tick)),
		UpdateIntervalSeconds: 1, WindowSeconds: WindowSeconds,
		Mock: &c.MockInfo{Seed: b.opts.Seed, Scenario: b.opts.Scenario, Speed: b.opts.Speed},
	}
}
