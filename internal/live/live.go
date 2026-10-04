// Package live turns collector output into the same projections the mock
// serves (CLAUDE.md DoD 8): normalized samples are bucketed per second,
// merged into a sliding window of WindowObservations, attributed by the
// shared aggregation package and projected by the shared projection package.
// Nothing downstream knows whether data is mock or live.
package live

import (
	"fmt"
	"io"
	"math"
	"sync"
	"sync/atomic"
	"time"

	"network-traffic-visualizer/internal/aggregation"
	"network-traffic-visualizer/internal/collector"
	c "network-traffic-visualizer/internal/contract"
	"network-traffic-visualizer/internal/counters"
	"network-traffic-visualizer/internal/devices"
	"network-traffic-visualizer/internal/enrichment"
	"network-traffic-visualizer/internal/flow"
	"network-traffic-visualizer/internal/inventory"
	"network-traffic-visualizer/internal/projection"
	"network-traffic-visualizer/internal/topology"
)

type Options struct {
	WindowSeconds  int
	UpdateInterval time.Duration
	// StaleAfter: no datagram for this long → status "stale".
	StaleAfter    time.Duration
	Epoch         time.Time
	Inventory     *inventory.Inventory
	InternalCIDRs []string
	Origin        topology.Origin
	Geo           enrichment.GeoLookup
	// MaxKeysPerSecond bounds memory per second bucket (0 = 200k).
	MaxKeysPerSecond int
	Now              func() time.Time
}

type bucket struct {
	order []string
	obs   map[string]*flow.WindowObservation
}

type wanRate struct {
	rate counters.InterfaceRate
}

type Source struct {
	opts Options
	inv  *projection.Inventory
	ctx  aggregation.Context

	mu            sync.Mutex
	buckets       map[int]*bucket
	exporters     []flow.Exporter
	exporterByID  map[string]flow.Exporter
	agentMap      map[string]string
	tracker       *counters.Tracker
	wan           map[string]wanRate // boundary exporter id → latest WAN rate
	lastDatagram  time.Time
	tick          int
	frame         *projection.Frame
	dirty         bool
	invDirty      bool
	lateSamples   atomic.Int64
	droppedKeys   atomic.Int64
	acceptedFlows atomic.Int64
}

func New(opts Options) (*Source, error) {
	if opts.WindowSeconds <= 0 {
		opts.WindowSeconds = 5
	}
	if opts.UpdateInterval <= 0 {
		opts.UpdateInterval = time.Second
	}
	if opts.StaleAfter <= 0 {
		opts.StaleAfter = 3 * time.Duration(opts.WindowSeconds) * time.Second
	}
	if opts.MaxKeysPerSecond <= 0 {
		opts.MaxKeysPerSecond = 200_000
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.Inventory == nil {
		opts.Inventory = inventory.Empty()
	}
	if opts.Geo == nil {
		opts.Geo = enrichment.MapGeo{}
	}
	classifier, err := enrichment.NewClassifier(opts.InternalCIDRs)
	if err != nil {
		return nil, err
	}
	reg := devices.NewRegistry(opts.Inventory.Devices)
	pinv, err := projection.NewInventory(reg, opts.Inventory.Networks, opts.Inventory.Exporters,
		opts.Inventory.Links, opts.Origin, enrichment.CountryAnchors)
	if err != nil {
		return nil, err
	}
	s := &Source{
		opts: opts, inv: pinv, buckets: map[int]*bucket{},
		exporters: append([]flow.Exporter(nil), opts.Inventory.Exporters...), exporterByID: map[string]flow.Exporter{},
		agentMap: map[string]string{}, tracker: counters.NewTracker(10 * time.Minute), wan: map[string]wanRate{},
	}
	for agent, id := range opts.Inventory.AgentToExporter {
		s.agentMap[agent] = id
	}
	for _, e := range s.exporters {
		s.exporterByID[e.ID] = e
	}
	s.ctx = aggregation.Context{Classifier: classifier, Registry: reg, Geo: opts.Geo, Exporters: s.exporters, Policy: opts.Inventory.Policy}
	s.tick = s.tickAt(opts.Now())
	return s, nil
}

func (s *Source) tickAt(t time.Time) int {
	return int(math.Floor(t.Sub(s.opts.Epoch).Seconds()))
}

// exporterFor maps a collector identity ("agent/sub") to an exporter id,
// registering unknown agents so they still appear as observation points.
// Caller holds s.mu.
func (s *Source) exporterFor(agentID, agentAddress string) string {
	if id, ok := s.agentMap[agentID]; ok {
		return id
	}
	e := flow.Exporter{ID: agentID, Name: agentAddress, AgentAddress: agentAddress} // role unknown: lowest preference
	s.agentMap[agentID] = agentID
	s.exporters = append(s.exporters, e)
	s.exporterByID[e.ID] = e
	s.ctx.Exporters = s.exporters
	s.invDirty = true // projection inventory is replaced (not mutated) in Snapshot
	return agentID
}

// Publish implements collector.Sink.
func (s *Source) Publish(b collector.Batch) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if b.ReceivedAt.After(s.lastDatagram) {
		s.lastDatagram = b.ReceivedAt
	}
	oldest := s.tick - 1 - s.opts.WindowSeconds
	for i := range b.Flows {
		f := &b.Flows[i]
		sec := s.tickAt(f.ObservedAt)
		if sec <= oldest {
			s.lateSamples.Add(1) // outside every window still to be served
			continue
		}
		exp := s.exporterFor(f.ExporterID, f.AgentAddress)
		bk := s.buckets[sec]
		if bk == nil {
			bk = &bucket{obs: map[string]*flow.WindowObservation{}}
			s.buckets[sec] = bk
		}
		o := flow.WindowObservation{
			ExporterID: exp, InputIfIndex: f.InputIfIndex, OutputIfIndex: f.OutputIfIndex,
			SrcIP: f.SrcIP, DstIP: f.DstIP, Protocol: f.Protocol, SrcPort: f.SrcPort, DstPort: f.DstPort,
		}
		key := exp + "|" + o.Key()
		cur, ok := bk.obs[key]
		if !ok {
			if len(bk.obs) >= s.opts.MaxKeysPerSecond {
				s.droppedKeys.Add(1)
				continue
			}
			cur = &o
			bk.obs[key] = cur
			bk.order = append(bk.order, key)
		}
		cur.SampleCount++
		cur.EstimatedBytes += f.EstimatedBytes
		cur.SamplingRate = f.SamplingRate
		cur.LastSampleAt = &sec
		s.acceptedFlows.Add(1)
		if sec < s.tick {
			s.dirty = true // late but still inside the served window
		}
	}
	for _, o := range b.Counters {
		exp := s.exporterFor(o.ExporterID, o.AgentAddress)
		e := s.exporterByID[exp]
		if e.Role != flow.RoleBoundary || e.BoundaryIfIndex == nil || *e.BoundaryIfIndex != o.IfIndex {
			continue
		}
		o.ExporterID = exp
		if r, ok := s.tracker.Update(o); ok {
			s.wan[exp] = wanRate{rate: r}
		}
	}
}

// Advance moves the clock; returns true when a new window is available.
func (s *Source) Advance(now time.Time) bool {
	t := s.tickAt(now)
	s.mu.Lock()
	defer s.mu.Unlock()
	if t <= s.tick {
		return false
	}
	s.tick = t
	s.dirty = true
	for sec := range s.buckets {
		if sec < t-1-s.opts.WindowSeconds {
			delete(s.buckets, sec)
		}
	}
	return true
}

// window merges completed seconds (end−W, end]. Caller holds s.mu.
func (s *Source) window(end int) []flow.WindowObservation {
	var order []string
	merged := map[string]*flow.WindowObservation{}
	for sec := end - s.opts.WindowSeconds + 1; sec <= end; sec++ {
		bk := s.buckets[sec]
		if bk == nil {
			continue
		}
		for _, k := range bk.order {
			o := bk.obs[k]
			if m, ok := merged[k]; ok {
				m.SampleCount += o.SampleCount
				m.EstimatedBytes += o.EstimatedBytes
				m.SamplingRate = o.SamplingRate
				m.LastSampleAt = o.LastSampleAt
				continue
			}
			cp := *o
			merged[k] = &cp
			order = append(order, k)
		}
	}
	out := make([]flow.WindowObservation, 0, len(order))
	for _, k := range order {
		out = append(out, *merged[k])
	}
	return out
}

// wanRates sums fresh boundary-interface counter rates (D-034). Caller holds s.mu.
func (s *Source) wanRates(now time.Time) *projection.WanRates {
	var total projection.WanRates
	n := 0
	for _, w := range s.wan {
		maxAge := 2*time.Duration(w.rate.IntervalSeconds*float64(time.Second)) + s.opts.StaleAfter
		if now.Sub(w.rate.At) > maxAge {
			continue
		}
		total.DownloadBps += w.rate.RxBps
		total.UploadBps += w.rate.TxBps
		if iv := int(math.Round(w.rate.IntervalSeconds)); iv > total.IntervalSeconds {
			total.IntervalSeconds = iv
		}
		n++
	}
	if n == 0 {
		return nil
	}
	return &total
}

// Snapshot implements httpapi.Source.
func (s *Source) Snapshot() (*projection.Frame, *projection.Inventory) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.invDirty {
		// Handlers may still be reading the previous inventory: copy, never mutate.
		cp := *s.inv
		cp.Exporters = append([]flow.Exporter(nil), s.exporters...)
		s.inv, s.invDirty = &cp, false
	}
	if s.frame != nil && !s.dirty {
		return s.frame, s.inv
	}
	end := s.tick - 1 // last completed second
	s.frame = &projection.Frame{
		Flows:         aggregation.Attribute(s.window(end), s.ctx),
		Epoch:         s.opts.Epoch,
		Tick:          s.tick,
		WindowEndTick: end,
		WindowSeconds: s.opts.WindowSeconds,
		WAN:           s.wanRates(s.opts.Epoch.Add(time.Duration(s.tick) * time.Second)),
	}
	s.dirty = false
	return s.frame, s.inv
}

// Status implements httpapi.Source.
func (s *Source) Status() c.StatusResponse {
	f, _ := s.Snapshot()
	s.mu.Lock()
	last := s.lastDatagram
	s.mu.Unlock()
	now := f.At(f.Tick)
	st := c.StatusResponse{
		Mode: "live", ServerTime: c.TS(now), LastAggregateAt: c.Ptr(c.TS(f.At(f.WindowEndTick))),
		UpdateIntervalSeconds: s.opts.UpdateInterval.Seconds(), WindowSeconds: float64(s.opts.WindowSeconds),
	}
	switch {
	case last.IsZero():
		st.Collector = c.CollectorStatus{Status: "disconnected"}
	case now.Sub(last) > s.opts.StaleAfter:
		st.Collector = c.CollectorStatus{Status: "stale", LastDatagramAt: c.Ptr(c.TS(last))}
	default:
		st.Live = true
		st.Collector = c.CollectorStatus{Status: "connected", LastDatagramAt: c.Ptr(c.TS(last))}
	}
	return st
}

// WritePrometheus exposes live-aggregation metrics (docs/OBSERVABILITY.md §2).
func (s *Source) WritePrometheus(w io.Writer) {
	s.mu.Lock()
	buckets := len(s.buckets)
	exporters := len(s.exporters)
	s.mu.Unlock()
	fmt.Fprintf(w, "# HELP ntv_live_samples_accepted_total Flow samples added to live windows.\n# TYPE ntv_live_samples_accepted_total counter\nntv_live_samples_accepted_total %d\n", s.acceptedFlows.Load())
	fmt.Fprintf(w, "# HELP ntv_live_samples_late_total Samples older than every served window (dropped).\n# TYPE ntv_live_samples_late_total counter\nntv_live_samples_late_total %d\n", s.lateSamples.Load())
	fmt.Fprintf(w, "# HELP ntv_live_keys_dropped_total Samples dropped by the per-second key limit.\n# TYPE ntv_live_keys_dropped_total counter\nntv_live_keys_dropped_total %d\n", s.droppedKeys.Load())
	fmt.Fprintf(w, "# HELP ntv_live_active_buckets Per-second buckets held in memory.\n# TYPE ntv_live_active_buckets gauge\nntv_live_active_buckets %d\n", buckets)
	fmt.Fprintf(w, "# HELP ntv_live_exporters Known exporters (configured + discovered).\n# TYPE ntv_live_exporters gauge\nntv_live_exporters %d\n", exporters)
}
