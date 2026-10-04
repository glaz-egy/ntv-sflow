package history

import (
	"context"
	"sort"
	"sync"
	"time"
)

// Memory keeps raw per-second rows in process memory, bounded by age and
// row count. It is lost on restart. Used for mock mode, tests and setups
// without ClickHouse.
type Memory struct {
	retention time.Duration
	maxRows   int
	now       func() time.Time

	mu       sync.RWMutex
	seconds  []int64 // sorted unix seconds that have rows
	rows     map[int64][]FlowRow
	nrows    int
	counters []CounterRow // sorted by At
}

type MemoryOptions struct {
	Retention time.Duration // default 1h
	MaxRows   int           // default 1,000,000
	Now       func() time.Time
}

func NewMemory(opts MemoryOptions) *Memory {
	if opts.Retention <= 0 {
		opts.Retention = time.Hour
	}
	if opts.MaxRows <= 0 {
		opts.MaxRows = 1_000_000
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	return &Memory{retention: opts.Retention, maxRows: opts.MaxRows, now: opts.Now, rows: map[int64][]FlowRow{}}
}

func (m *Memory) Tiers() []Tier { return []Tier{{Step: time.Second, Retention: m.retention}} }

func (m *Memory) Close() error { return nil }
func (m *Memory) Kind() string { return "memory" }

func (m *Memory) WriteFlows(_ context.Context, rows []FlowRow) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, r := range rows {
		sec := r.Start.Unix()
		if _, ok := m.rows[sec]; !ok {
			i := sort.Search(len(m.seconds), func(i int) bool { return m.seconds[i] >= sec })
			m.seconds = append(m.seconds, 0)
			copy(m.seconds[i+1:], m.seconds[i:])
			m.seconds[i] = sec
		}
		r.Obs.LastSampleAt = nil
		m.rows[sec] = append(m.rows[sec], r)
		m.nrows++
	}
	m.prune()
	return nil
}

func (m *Memory) WriteCounters(_ context.Context, rows []CounterRow) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.counters = append(m.counters, rows...)
	sort.SliceStable(m.counters, func(i, j int) bool { return m.counters[i].At.Before(m.counters[j].At) })
	m.prune()
	return nil
}

// prune drops rows older than the retention and the oldest seconds beyond
// the row cap. Caller holds m.mu.
func (m *Memory) prune() {
	cutoff := m.now().Add(-m.retention).Unix()
	drop := 0
	for drop < len(m.seconds) && (m.seconds[drop] < cutoff || m.nrows > m.maxRows) {
		m.nrows -= len(m.rows[m.seconds[drop]])
		delete(m.rows, m.seconds[drop])
		drop++
	}
	m.seconds = m.seconds[drop:]
	c := 0
	for c < len(m.counters) && m.counters[c].At.Unix() < cutoff {
		c++
	}
	m.counters = m.counters[c:]
}

// span returns the stored seconds in [start, end). Caller holds m.mu.
func (m *Memory) span(start, end time.Time) []int64 {
	lo := sort.Search(len(m.seconds), func(i int) bool { return m.seconds[i] >= start.Unix() })
	hi := sort.Search(len(m.seconds), func(i int) bool { return m.seconds[i] >= end.Unix() })
	return m.seconds[lo:hi]
}

func (m *Memory) Flows(_ context.Context, _ Tier, start, end time.Time) ([]MergedFlow, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var order []string
	merged := map[string]*MergedFlow{}
	for _, sec := range m.span(start, end) {
		for _, r := range m.rows[sec] {
			k := r.Obs.ExporterID + "|" + r.Obs.Key()
			cur, ok := merged[k]
			if !ok {
				cur = &MergedFlow{Obs: r.Obs}
				cur.Obs.SampleCount, cur.Obs.EstimatedBytes = 0, 0
				merged[k] = cur
				order = append(order, k)
			}
			cur.Obs.SampleCount += r.Obs.SampleCount
			cur.Obs.EstimatedBytes += r.Obs.EstimatedBytes
			cur.Obs.SamplingRate = max(cur.Obs.SamplingRate, r.Obs.SamplingRate)
			if r.Obs.SampleCount > 0 && r.Start.After(cur.LastStart) {
				cur.LastStart = r.Start
			}
		}
	}
	out := make([]MergedFlow, 0, len(order))
	for _, k := range order {
		out = append(out, *merged[k])
	}
	return out, nil
}

func (m *Memory) CounterSeries(_ context.Context, start, end time.Time, step time.Duration) ([]CounterBucket, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	type acc struct{ rx, tx, secs float64 }
	sums := map[int64]map[string]*acc{}
	var keys []int64
	for _, c := range m.counters {
		if !c.At.After(start) || c.At.After(end) {
			continue
		}
		bs := bucketFrom(start, c.At.Add(-time.Millisecond), step)
		byExp, ok := sums[bs.Unix()]
		if !ok {
			byExp = map[string]*acc{}
			sums[bs.Unix()] = byExp
			keys = append(keys, bs.Unix())
		}
		a := byExp[c.ExporterID]
		if a == nil {
			a = &acc{}
			byExp[c.ExporterID] = a
		}
		a.rx += c.RxBps * c.IntervalSeconds
		a.tx += c.TxBps * c.IntervalSeconds
		a.secs += c.IntervalSeconds
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })
	out := make([]CounterBucket, 0, len(keys))
	for _, k := range keys {
		b := CounterBucket{Start: time.Unix(k, 0).UTC()}
		for _, a := range sums[k] {
			if a.secs <= 0 {
				continue
			}
			b.RxBps += a.rx / a.secs
			b.TxBps += a.tx / a.secs
			b.CoveredSeconds = max(b.CoveredSeconds, a.secs)
		}
		out = append(out, b)
	}
	return out, nil
}

func (m *Memory) Timeline(_ context.Context, _ Tier, start, end time.Time, step time.Duration, dd Dedup) ([]TimelineBucket, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	type pick struct {
		row   FlowRow
		bytes float64
		n     int
	}
	// bucket start → flow key → exporter → summed row
	sums := map[int64]map[string]map[string]*pick{}
	var keys []int64
	for _, sec := range m.span(start, end) {
		bs := bucketFrom(start, time.Unix(sec, 0), step).Unix()
		byKey, ok := sums[bs]
		if !ok {
			byKey = map[string]map[string]*pick{}
			sums[bs] = byKey
			keys = append(keys, bs)
		}
		for _, r := range m.rows[sec] {
			k := r.Obs.Key()
			if byKey[k] == nil {
				byKey[k] = map[string]*pick{}
			}
			p := byKey[k][r.Obs.ExporterID]
			if p == nil {
				p = &pick{row: r}
				byKey[k][r.Obs.ExporterID] = p
			}
			p.bytes += r.Obs.EstimatedBytes
			p.n += r.Obs.SampleCount
		}
	}
	out := make([]TimelineBucket, 0, len(keys))
	for _, bs := range keys {
		b := TimelineBucket{Start: time.Unix(bs, 0).UTC()}
		for _, byExp := range sums[bs] {
			var best *pick
			for exp, p := range byExp {
				internal := p.row.SrcInternal && p.row.DstInternal
				if best == nil || dd.better(exp, best.row.Obs.ExporterID, internal) {
					best = p
				}
			}
			addToBucket(&b, best.row.SrcInternal, best.row.DstInternal, best.bytes, best.n)
		}
		out = append(out, b)
	}
	return out, nil
}

// addToBucket adds bytes by scope: internal→external is outbound (upload),
// external→internal inbound (download).
func addToBucket(b *TimelineBucket, srcInternal, dstInternal bool, bytes float64, samples int) {
	switch {
	case srcInternal && dstInternal:
		b.InternalBytes += bytes
		b.InternalSamples += samples
	case srcInternal:
		b.OutboundBytes += bytes
		b.OutboundSamples += samples
	case dstInternal:
		b.InboundBytes += bytes
		b.InboundSamples += samples
	default:
		b.TransitBytes += bytes
		b.TransitSamples += samples
	}
}

func (m *Memory) Coverage(_ context.Context) (time.Time, time.Time, bool, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if len(m.seconds) == 0 {
		return time.Time{}, time.Time{}, false, nil
	}
	return time.Unix(m.seconds[0], 0).UTC(), time.Unix(m.seconds[len(m.seconds)-1], 0).UTC(), true, nil
}
