package collector

import (
	"fmt"
	"io"
	"sort"
	"sync"
	"sync/atomic"
)

// Metrics are collector health counters (docs/OBSERVABILITY.md §1).
// Labels are bounded (error kinds), never per-IP.
type Metrics struct {
	DatagramsReceived   atomic.Int64
	DatagramsInvalid    atomic.Int64
	DatagramsDropped    atomic.Int64 // queue full (backpressure)
	DatagramsRejected   atomic.Int64 // source not in allow-list
	FlowSamples         atomic.Int64
	CounterSamples      atomic.Int64
	UnsupportedSamples  atomic.Int64
	UnsupportedRecords  atomic.Int64
	MalformedSamples    atomic.Int64
	MalformedRecords    atomic.Int64
	NonIPSamples        atomic.Int64
	InvalidSamplingRate atomic.Int64
	DatagramsLost       atomic.Int64 // estimated from agent sequence numbers (gauge)
	AgentRestarts       atomic.Int64
	AgentDrops          atomic.Int64 // drops reported by agents in flow samples
	QueueDepth          atomic.Int64
	ProcessingLagNanos  atomic.Int64 // last receive → normalized latency
	QueueCapacity       int64
	mu                  sync.Mutex
	decodeErrors        map[string]int64
}

func newMetrics(queueCap int) *Metrics {
	return &Metrics{QueueCapacity: int64(queueCap), decodeErrors: map[string]int64{}}
}

func (m *Metrics) decodeError(kind string) {
	m.DatagramsInvalid.Add(1)
	m.mu.Lock()
	m.decodeErrors[kind]++
	m.mu.Unlock()
}

// WritePrometheus writes the metrics in Prometheus text exposition format.
func (m *Metrics) WritePrometheus(w io.Writer) {
	counter := func(name, help string, v int64) {
		fmt.Fprintf(w, "# HELP %s %s\n# TYPE %s counter\n%s %d\n", name, help, name, name, v)
	}
	gauge := func(name, help string, v float64) {
		fmt.Fprintf(w, "# HELP %s %s\n# TYPE %s gauge\n%s %g\n", name, help, name, name, v)
	}
	counter("ntv_collector_datagrams_received_total", "UDP datagrams received.", m.DatagramsReceived.Load())
	counter("ntv_collector_datagrams_invalid_total", "Datagrams that failed to decode.", m.DatagramsInvalid.Load())
	counter("ntv_collector_datagrams_dropped_total", "Datagrams dropped because the ingestion queue was full.", m.DatagramsDropped.Load())
	counter("ntv_collector_datagrams_rejected_total", "Datagrams from sources outside collector.allowed_sources.", m.DatagramsRejected.Load())
	counter("ntv_collector_flow_samples_total", "Flow samples normalized.", m.FlowSamples.Load())
	counter("ntv_collector_counter_samples_total", "Interface counter samples normalized.", m.CounterSamples.Load())
	counter("ntv_collector_unsupported_samples_total", "Samples with unsupported formats (skipped).", m.UnsupportedSamples.Load())
	counter("ntv_collector_unsupported_records_total", "Records with unsupported formats (skipped).", m.UnsupportedRecords.Load())
	counter("ntv_collector_malformed_samples_total", "Malformed samples inside valid datagrams.", m.MalformedSamples.Load())
	counter("ntv_collector_malformed_records_total", "Malformed records inside valid samples.", m.MalformedRecords.Load())
	counter("ntv_collector_non_ip_samples_total", "Flow samples without IP information.", m.NonIPSamples.Load())
	counter("ntv_collector_invalid_sampling_rate_total", "Flow samples with sampling_rate 0 (dropped).", m.InvalidSamplingRate.Load())
	counter("ntv_collector_agent_restarts_total", "Agent restarts detected (uptime or sequence reset).", m.AgentRestarts.Load())
	counter("ntv_collector_agent_reported_drops_total", "Packet drops reported by agents in flow samples.", m.AgentDrops.Load())
	gauge("ntv_collector_datagrams_lost_estimate", "Datagrams missing according to agent sequence numbers (reordering-safe estimate).", float64(m.DatagramsLost.Load()))
	gauge("ntv_collector_ingestion_queue_depth", "Datagrams waiting to be decoded.", float64(m.QueueDepth.Load()))
	gauge("ntv_collector_ingestion_queue_capacity", "Ingestion queue capacity.", float64(m.QueueCapacity))
	gauge("ntv_collector_processing_lag_seconds", "Receive-to-normalized latency of the last datagram.", float64(m.ProcessingLagNanos.Load())/1e9)

	m.mu.Lock()
	kinds := make([]string, 0, len(m.decodeErrors))
	for k := range m.decodeErrors {
		kinds = append(kinds, k)
	}
	sort.Strings(kinds)
	fmt.Fprintf(w, "# HELP ntv_collector_decoder_errors_total Decode errors by kind.\n# TYPE ntv_collector_decoder_errors_total counter\n")
	for _, k := range kinds {
		fmt.Fprintf(w, "ntv_collector_decoder_errors_total{kind=%q} %d\n", k, m.decodeErrors[k])
	}
	m.mu.Unlock()
}
