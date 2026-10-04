package collector

import (
	"encoding/json"
	"io"
	"log/slog"
	"sort"
	"strconv"
	"sync"
	"time"

	"network-traffic-visualizer/internal/counters"
)

// FanOut publishes to several sinks.
type FanOut []Sink

func (f FanOut) Publish(b Batch) {
	for _, s := range f {
		s.Publish(b)
	}
}

// SummarySink logs a periodic per-exporter summary: sample rates, sampled
// throughput estimates and counter-derived interface rates. It is the
// default "is anything arriving?" view until aggregation lands.
type SummarySink struct {
	log      *slog.Logger
	tracker  *counters.Tracker
	mu       sync.Mutex
	exporter map[string]*exporterSummary
}

type exporterSummary struct {
	samples   int
	estimated float64
	rates     map[int]counters.InterfaceRate
}

func NewSummarySink(log *slog.Logger) *SummarySink {
	return &SummarySink{log: log, tracker: counters.NewTracker(5 * time.Minute), exporter: map[string]*exporterSummary{}}
}

func (s *SummarySink) get(id string) *exporterSummary {
	e, ok := s.exporter[id]
	if !ok {
		e = &exporterSummary{rates: map[int]counters.InterfaceRate{}}
		s.exporter[id] = e
	}
	return e
}

func (s *SummarySink) Publish(b Batch) {
	var rates []counters.InterfaceRate
	for _, o := range b.Counters {
		if r, ok := s.tracker.Update(o); ok {
			rates = append(rates, r)
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, f := range b.Flows {
		e := s.get(f.ExporterID)
		e.samples++
		e.estimated += f.EstimatedBytes
	}
	for _, r := range rates {
		s.get(r.ExporterID).rates[r.IfIndex] = r
	}
}

// Run logs a summary every interval until ctx is done.
func (s *SummarySink) Run(done <-chan struct{}, interval time.Duration) {
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-done:
			return
		case <-t.C:
			s.flush(interval)
		}
	}
}

func (s *SummarySink) flush(interval time.Duration) {
	s.mu.Lock()
	ids := make([]string, 0, len(s.exporter))
	for id := range s.exporter {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	type line struct {
		id      string
		samples int
		est     float64
		rates   []counters.InterfaceRate
	}
	var lines []line
	for _, id := range ids {
		e := s.exporter[id]
		l := line{id: id, samples: e.samples, est: e.estimated}
		for _, r := range e.rates {
			l.rates = append(l.rates, r)
		}
		sort.Slice(l.rates, func(i, j int) bool { return l.rates[i].IfIndex < l.rates[j].IfIndex })
		lines = append(lines, l)
		e.samples, e.estimated = 0, 0
	}
	s.mu.Unlock()
	if len(lines) == 0 {
		s.log.Info("collector summary: no sFlow received in interval", "interval", interval.String())
		return
	}
	for _, l := range lines {
		attrs := []any{
			"exporter", l.id,
			"flow_samples_per_s", float64(l.samples) / interval.Seconds(),
			// Sampled estimate, not exact throughput (D-005).
			"sampled_estimate_bps", l.est * 8 / interval.Seconds(),
		}
		for _, r := range l.rates {
			attrs = append(attrs, "if"+strconv.Itoa(r.IfIndex)+"_counter_rx_bps", r.RxBps, "if"+strconv.Itoa(r.IfIndex)+"_counter_tx_bps", r.TxBps)
		}
		s.log.Info("collector summary", attrs...)
	}
}

// DebugFlowSink writes normalized flow samples as JSON lines, bounded to
// maxPerSecond. Opt-in only: output contains internal addresses and peers
// (docs/SECURITY.md §5).
type DebugFlowSink struct {
	mu           sync.Mutex
	enc          *json.Encoder
	maxPerSecond int
	second       int64
	written      int
}

func NewDebugFlowSink(w io.Writer, maxPerSecond int) *DebugFlowSink {
	return &DebugFlowSink{enc: json.NewEncoder(w), maxPerSecond: maxPerSecond}
}

func (d *DebugFlowSink) Publish(b Batch) {
	d.mu.Lock()
	defer d.mu.Unlock()
	for i := range b.Flows {
		sec := b.ReceivedAt.Unix()
		if sec != d.second {
			d.second, d.written = sec, 0
		}
		if d.written >= d.maxPerSecond {
			return
		}
		d.written++
		_ = d.enc.Encode(b.Flows[i])
	}
}
