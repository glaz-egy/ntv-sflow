package history

import (
	"context"
	"fmt"
	"sync"
	"time"

	"network-traffic-visualizer/internal/flow"
	"network-traffic-visualizer/internal/projection"
)

// FrameBuilder turns merged observations into a projection frame with the
// source's current inventory and attribution context (live or mock).
type FrameBuilder interface {
	HistoryFrame(obs []flow.WindowObservation, start time.Time, seconds int, wan *projection.WanRates) (*projection.Frame, *projection.Inventory)
	ObservationDedup() Dedup
}

// RangeError is an invalid requested range (HTTP 400).
type RangeError struct{ msg string }

func (e *RangeError) Error() string { return e.msg }

const (
	// MaxSpan bounds a snapshot or timeline range.
	MaxSpan = 400 * 24 * time.Hour
	// bucketsPerTier: a tier is used while span/step stays within this
	// (1s → 6h, 1m → 15d, 1h → longer).
	bucketsPerTier = 21600
	// MaxTimelinePoints bounds timeline responses.
	MaxTimelinePoints = 720
	// counterCoverage: boundary counters are used for a snapshot's WAN
	// totals only when they cover this share of the range (else sampled sum).
	counterCoverage = 0.8
)

var niceSteps = []time.Duration{
	time.Second, 2 * time.Second, 5 * time.Second, 10 * time.Second, 15 * time.Second, 30 * time.Second,
	time.Minute, 2 * time.Minute, 5 * time.Minute, 10 * time.Minute, 15 * time.Minute, 30 * time.Minute,
	time.Hour, 2 * time.Hour, 3 * time.Hour, 6 * time.Hour, 12 * time.Hour, 24 * time.Hour,
}

// Service answers historical queries. Safe for concurrent use.
type Service struct {
	store   Store
	builder FrameBuilder
	now     func() time.Time

	mu     sync.Mutex
	cache  []cached // small LRU: replay asks for the same range from several views
	info   Info
	infoAt time.Time
}

type cached struct {
	start, end time.Time
	at         time.Time
	snap       Snapshot
}

// Snapshot is a historical window ready for projection.
type Snapshot struct {
	Frame *projection.Frame
	Inv   *projection.Inventory
	Step  time.Duration
}

// NewService: now is the source's clock (the mock may run faster than wall time).
func NewService(store Store, builder FrameBuilder, now func() time.Time) *Service {
	return &Service{store: store, builder: builder, now: now}
}

func (s *Service) Store() Store { return s.store }

// Resolve validates [start, end), picks the stored tier and aligns the range
// outwards to its step.
func (s *Service) Resolve(start, end time.Time) (Tier, time.Time, time.Time, error) {
	if !end.After(start) {
		return Tier{}, start, end, &RangeError{"end must be after start"}
	}
	if end.Sub(start) > MaxSpan {
		return Tier{}, start, end, &RangeError{fmt.Sprintf("range must not exceed %s", MaxSpan)}
	}
	if now := s.now(); end.After(now.Add(time.Second)) {
		return Tier{}, start, end, &RangeError{"end must not be in the future"}
	}
	tier := s.pickTier(start, end.Sub(start))
	return tier, floorTo(start, tier.Step), ceilTo(end, tier.Step), nil
}

func (s *Service) pickTier(start time.Time, span time.Duration) Tier {
	tiers := s.store.Tiers()
	now := s.now()
	for _, t := range tiers {
		if !start.Before(now.Add(-t.Retention)) && span <= t.Step*bucketsPerTier {
			return t
		}
	}
	// Older than every retention, or longer than every limit: coarsest tier.
	return tiers[len(tiers)-1]
}

// Snapshot rebuilds the window [start, end) (aligned to the tier step).
func (s *Service) Snapshot(ctx context.Context, start, end time.Time) (Snapshot, error) {
	tier, start, end, err := s.Resolve(start, end)
	if err != nil {
		return Snapshot{}, err
	}
	if snap, ok := s.cached(start, end); ok {
		return snap, nil
	}
	merged, err := s.store.Flows(ctx, tier, start, end)
	if err != nil {
		return Snapshot{}, err
	}
	// Rates are averaged over the window, but never over time without
	// stored data: before recording started or after the newest second.
	// Rows outside the stored range do not exist, so trimming the averaging
	// interval to it is exact even for minute/hour rollups. The response
	// window shows the trimmed range.
	// Coverage is read fresh (not the cached Info): rows newer than a stale
	// "latest" would otherwise be divided by too short an interval.
	fStart, fEnd := start, end
	if e, l, ok, err := s.store.Coverage(ctx); err != nil {
		return Snapshot{}, err
	} else if ok {
		l = l.Add(time.Second) // end of the newest stored second
		if fStart.Before(e) && fEnd.After(e) {
			fStart = e
		}
		if fEnd.After(l) && fStart.Before(l) {
			fEnd = l
		}
	}
	obs := make([]flow.WindowObservation, 0, len(merged))
	for _, m := range merged {
		o := m.Obs
		o.LastSampleAt = nil
		if o.SampleCount > 0 && !m.LastStart.IsZero() {
			// LastStart is a raw second start; like live, tick t labels the
			// end of second t, so the last sample's tick is start offset + 1.
			t := int(m.LastStart.Sub(fStart)/time.Second) + 1
			o.LastSampleAt = &t
		}
		obs = append(obs, o)
	}
	seconds := int(fEnd.Sub(fStart) / time.Second)
	wan, err := s.wanAverage(ctx, fStart, fEnd)
	if err != nil {
		return Snapshot{}, err
	}
	f, inv := s.builder.HistoryFrame(obs, fStart, seconds, wan)
	snap := Snapshot{Frame: f, Inv: inv, Step: tier.Step}
	s.store1(start, end, snap)
	return snap, nil
}

// wanAverage returns boundary counter rates averaged over the range, or nil
// when counters do not cover enough of it (projection then falls back to
// the sampled sum, D-034).
func (s *Service) wanAverage(ctx context.Context, start, end time.Time) (*projection.WanRates, error) {
	span := end.Sub(start)
	buckets, err := s.store.CounterSeries(ctx, start, end, span)
	if err != nil || len(buckets) == 0 {
		return nil, err
	}
	b := buckets[0]
	if b.CoveredSeconds < counterCoverage*span.Seconds() {
		return nil, nil
	}
	return &projection.WanRates{DownloadBps: b.RxBps, UploadBps: b.TxBps, IntervalSeconds: int(span / time.Second)}, nil
}

func (s *Service) cached(start, end time.Time) (Snapshot, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	for _, c := range s.cache {
		// Ranges ending recently may still receive rows: short TTL.
		if c.start.Equal(start) && c.end.Equal(end) && now.Sub(c.at) < 5*time.Second {
			return c.snap, true
		}
	}
	return Snapshot{}, false
}

func (s *Service) store1(start, end time.Time, snap Snapshot) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cache = append(s.cache, cached{start: start, end: end, at: time.Now(), snap: snap})
	if len(s.cache) > 16 {
		s.cache = s.cache[len(s.cache)-16:]
	}
}

// TimelinePoint is one bucket; nil rates mean no stored data.
type TimelinePoint struct {
	Start time.Time
	// Seconds the rates are averaged over: the step, or less for the bucket
	// holding the newest stored second.
	Seconds                     float64
	Data                        bool
	Inbound, Outbound, Internal float64 // sampled-estimate bps
	InboundSamples              int
	OutboundSamples             int
	InternalSamples             int
	// WAN counter rates (nil without boundary counters in the bucket).
	WanDownload, WanUpload *float64
	WanCoveredSeconds      float64
}

type Timeline struct {
	Start, End time.Time
	Step       time.Duration
	Points     []TimelinePoint
}

// Timeline returns traffic per scope for [start, end) in at most maxPoints
// buckets (gaps are explicit points without data). An end in the future is
// clamped to now, so clients may ask for "the last hour" with their clock.
func (s *Service) Timeline(ctx context.Context, start, end time.Time, maxPoints int) (Timeline, error) {
	if maxPoints <= 0 || maxPoints > MaxTimelinePoints {
		maxPoints = MaxTimelinePoints
	}
	if now := s.now(); end.After(now) && start.Before(now) {
		end = now
	}
	tier, start, end, err := s.Resolve(start, end)
	if err != nil {
		return Timeline{}, err
	}
	step := pickStep(end.Sub(start), tier.Step, maxPoints)
	for {
		a, b := floorTo(start, step), ceilTo(end, step)
		next := nextStep(step, tier.Step)
		if int(b.Sub(a)/step) <= maxPoints || next == step {
			start, end = a, b
			break
		}
		step = next // alignment added a bucket
	}
	// The first and newest stored seconds bound the data: a bucket
	// straddling either edge is averaged over its covered part, not shown
	// as a drop in traffic.
	earliest, latest, haveData, err := s.store.Coverage(ctx)
	if err != nil {
		return Timeline{}, err
	}
	dataEnd := latest.Add(time.Second)
	buckets, err := s.store.Timeline(ctx, tier, start, end, step, s.builder.ObservationDedup())
	if err != nil {
		return Timeline{}, err
	}
	counters, err := s.store.CounterSeries(ctx, start, end, step)
	if err != nil {
		return Timeline{}, err
	}
	byStart := map[int64]TimelineBucket{}
	for _, b := range buckets {
		byStart[b.Start.Unix()] = b
	}
	cnt := map[int64]CounterBucket{}
	for _, b := range counters {
		cnt[b.Start.Unix()] = b
	}
	tl := Timeline{Start: start, End: end, Step: step}
	for t := start; t.Before(end); t = t.Add(step) {
		p := TimelinePoint{Start: t, Seconds: step.Seconds()}
		if haveData {
			from, to := t, t.Add(step)
			if from.Before(earliest) && to.After(earliest) {
				from = earliest
			}
			if from.Before(dataEnd) && to.After(dataEnd) {
				to = dataEnd
			}
			p.Seconds = to.Sub(from).Seconds()
		}
		secs := p.Seconds
		if b, ok := byStart[t.Unix()]; ok {
			p.Data = true
			p.Inbound, p.Outbound, p.Internal = b.InboundBytes*8/secs, b.OutboundBytes*8/secs, b.InternalBytes*8/secs
			p.InboundSamples, p.OutboundSamples, p.InternalSamples = b.InboundSamples, b.OutboundSamples, b.InternalSamples
		}
		if c, ok := cnt[t.Unix()]; ok {
			p.Data = true
			p.WanDownload, p.WanUpload = &c.RxBps, &c.TxBps
			p.WanCoveredSeconds = c.CoveredSeconds
		}
		tl.Points = append(tl.Points, p)
	}
	return tl, nil
}

// pickStep is the smallest nice step ≥ base that keeps the point count ≤ max.
func pickStep(span, base time.Duration, maxPoints int) time.Duration {
	for _, st := range niceSteps {
		if st >= base && st%base == 0 && span/st <= time.Duration(maxPoints) {
			return st
		}
	}
	return niceSteps[len(niceSteps)-1]
}

// nextStep is the next nice step above cur that is a multiple of base.
func nextStep(cur, base time.Duration) time.Duration {
	for _, st := range niceSteps {
		if st > cur && st%base == 0 {
			return st
		}
	}
	return cur
}

// Info describes what history is available.
type Info struct {
	Kind             string
	Earliest, Latest *time.Time
	Tiers            []Tier
}

// Info is cached for a few seconds: it is part of every status update.
func (s *Service) Info(ctx context.Context) (Info, error) {
	s.mu.Lock()
	if time.Since(s.infoAt) < 5*time.Second {
		info := s.info
		s.mu.Unlock()
		return info, nil
	}
	s.mu.Unlock()
	info := Info{Kind: s.store.Kind(), Tiers: s.store.Tiers()}
	e, l, ok, err := s.store.Coverage(ctx)
	if err != nil {
		return info, err
	}
	if ok {
		// Latest is the end of the newest stored second.
		l = l.Add(time.Second)
		info.Earliest, info.Latest = &e, &l
	}
	s.mu.Lock()
	s.info, s.infoAt = info, time.Now()
	s.mu.Unlock()
	return info, nil
}
