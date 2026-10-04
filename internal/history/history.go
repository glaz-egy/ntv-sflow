// Package history persists per-second flow observations and boundary counter
// rates, and rebuilds historical windows from them (Milestone E, D-059).
//
// What is stored is the pre-attribution observation (exporter + flow key +
// sample count + estimated bytes), the same input live windows are built
// from. A historical snapshot is therefore produced by the unchanged
// aggregation and projection packages, so Globe/Home/Flows look and behave
// exactly like live, only over a chosen time range.
//
// Stores: Memory (bounded, for mock mode and small setups) and ClickHouse
// (per-second rows with 1-minute and 1-hour rollups and TTL retention).
package history

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"network-traffic-visualizer/internal/flow"
)

// FlowRow is one exporter's observations of one unidirectional flow key
// during one interval (a second for raw rows; a minute/hour for rollups).
//
// Start uses the same labels as live windows: sim/collector second index t
// covers the labelled interval (t−1, t], so Start = epoch + (t−1)s (D-059).
type FlowRow struct {
	Start time.Time
	Obs   flow.WindowObservation // LastSampleAt is ignored on write
	// Internal flags are classified at write time with the configured
	// internal CIDRs. They only drive timeline totals; snapshots re-classify.
	SrcInternal, DstInternal bool
}

// CounterRow is a boundary-interface counter rate (D-034), already computed
// from octet deltas.
type CounterRow struct {
	At              time.Time // end of the polling interval
	ExporterID      string
	IfIndex         int
	RxBps, TxBps    float64
	IntervalSeconds float64
}

// MergedFlow is the sum of FlowRows for one exporter + flow key over a range.
type MergedFlow struct {
	Obs flow.WindowObservation // SampleCount/EstimatedBytes summed; SamplingRate = max
	// LastStart is the Start of the newest row with samples.
	LastStart time.Time
}

// Tier is one stored resolution and how long it is kept.
type Tier struct {
	Step      time.Duration
	Retention time.Duration
}

// TimelineBucket holds sampled-estimate bytes per scope for one step.
type TimelineBucket struct {
	Start                                      time.Time
	InboundBytes, OutboundBytes, InternalBytes float64
	TransitBytes                               float64
	InboundSamples, OutboundSamples            int
	InternalSamples, TransitSamples            int
}

// CounterBucket is the boundary counter rate for one bucket: per exporter
// the interval-weighted mean, summed across exporters (D-034).
type CounterBucket struct {
	Start        time.Time
	RxBps, TxBps float64
	// CoveredSeconds is the polled time inside the bucket (max across
	// exporters); less than the bucket length means a partial average.
	CoveredSeconds float64
}

// bucketFrom returns the start of the step-sized bucket (counted from
// start) that contains t. Buckets are relative to the range, so any step
// lines up with it.
func bucketFrom(start, t time.Time, step time.Duration) time.Time {
	n := t.Sub(start) / step
	if t.Before(start) && t.Sub(start)%step != 0 {
		n--
	}
	return start.Add(n * step)
}

// Store is the persistence boundary. Implementations must be safe for
// concurrent use. Ranges are half-open: [start, end).
type Store interface {
	WriteFlows(ctx context.Context, rows []FlowRow) error
	WriteCounters(ctx context.Context, rows []CounterRow) error
	// Flows merges rows of the given tier in [start, end) per exporter + key.
	Flows(ctx context.Context, tier Tier, start, end time.Time) ([]MergedFlow, error)
	// CounterSeries averages counter rates per bucket of step over [start, end).
	CounterSeries(ctx context.Context, start, end time.Time, step time.Duration) ([]CounterBucket, error)
	// Timeline sums bytes per scope in buckets of step (a multiple of
	// tier.Step), counting each flow key once per bucket (Dedup).
	Timeline(ctx context.Context, tier Tier, start, end time.Time, step time.Duration, dd Dedup) ([]TimelineBucket, error)
	// Coverage returns the oldest and newest stored raw row starts.
	Coverage(ctx context.Context) (earliest, latest time.Time, ok bool, err error)
	// Tiers lists the stored resolutions, finest first.
	Tiers() []Tier
	// Kind names the backend ("memory", "clickhouse").
	Kind() string
	Close() error
}

// Dedup picks one observation point per flow key, like
// aggregation.ChooseObservation (D-024): lowest rank, then lowest exporter
// id. Traffic seen by several exporters is never summed (CLAUDE.md rule 7).
type Dedup struct {
	ExternalRank map[string]int // exporter id → rank for non-internal flows
	InternalRank map[string]int // exporter id → rank for internal flows
	// Ranks of exporters missing from the maps (= number of policy roles).
	UnknownExternal, UnknownInternal int
}

// NewDedup ranks exporters by the observation policy role order.
func NewDedup(exporters []flow.Exporter, policy flow.ObservationPolicy) Dedup {
	d := Dedup{ExternalRank: map[string]int{}, InternalRank: map[string]int{},
		UnknownExternal: len(policy.External), UnknownInternal: len(policy.Internal)}
	for _, e := range exporters {
		d.ExternalRank[e.ID], d.InternalRank[e.ID] = len(policy.External), len(policy.Internal)
		for i, r := range policy.External {
			if r == e.Role {
				d.ExternalRank[e.ID] = i
				break
			}
		}
		for i, r := range policy.Internal {
			if r == e.Role {
				d.InternalRank[e.ID] = i
				break
			}
		}
	}
	return d
}

func (d Dedup) Rank(exporterID string, internal bool) int {
	if internal {
		if r, ok := d.InternalRank[exporterID]; ok {
			return r
		}
		return d.UnknownInternal
	}
	if r, ok := d.ExternalRank[exporterID]; ok {
		return r
	}
	return d.UnknownExternal
}

// better reports whether exporter a is preferred over b.
func (d Dedup) better(a, b string, internal bool) bool {
	ra, rb := d.Rank(a, internal), d.Rank(b, internal)
	if ra != rb {
		return ra < rb
	}
	return a < b
}

// ErrUnavailable is returned when history is disabled.
var ErrUnavailable = errors.New("history is not enabled")

// ParseRetention accepts Go durations plus a day suffix ("7d", "90d", "36h").
func ParseRetention(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	if strings.HasSuffix(s, "d") {
		n, err := strconv.Atoi(strings.TrimSuffix(s, "d"))
		if err != nil || n <= 0 {
			return 0, fmt.Errorf("invalid retention %q", s)
		}
		return time.Duration(n) * 24 * time.Hour, nil
	}
	d, err := time.ParseDuration(s)
	if err != nil || d <= 0 {
		return 0, fmt.Errorf("invalid retention %q (use e.g. 7d, 36h)", s)
	}
	return d, nil
}

func floorTo(t time.Time, step time.Duration) time.Time { return t.UTC().Truncate(step) }

func ceilTo(t time.Time, step time.Duration) time.Time {
	f := floorTo(t, step)
	if f.Before(t) {
		return f.Add(step)
	}
	return f
}
