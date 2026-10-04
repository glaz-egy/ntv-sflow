// Package counters converts interface counter readings into rates
// (docs/SFLOW.md §7): bps = delta_octets * 8 / delta_seconds.
package counters

type Reading struct {
	At     float64 // seconds, consistent per interface
	Octets float64
	Width  int // 32 or 64
}

type Reason string

const (
	FirstSample         Reason = "first_sample"
	NonPositiveInterval Reason = "non_positive_interval"
	StalePrevious       Reason = "stale_previous"
	CounterReset        Reason = "counter_reset"
)

type Options struct {
	// MaxIntervalSeconds: previous readings older than this are not used (0 = no limit).
	MaxIntervalSeconds float64
	// IfSpeedBps: deltas implying more than 110% of link speed are resets (0 = unknown).
	IfSpeedBps float64
}

type Rate struct {
	OK              bool
	Bps             float64
	IntervalSeconds float64
	Reason          Reason
}

const twoPow32 = 4294967296.0

func Compute(prev *Reading, cur Reading, opt Options) Rate {
	if prev == nil {
		return Rate{Reason: FirstSample}
	}
	interval := cur.At - prev.At
	if interval <= 0 {
		return Rate{Reason: NonPositiveInterval}
	}
	if opt.MaxIntervalSeconds > 0 && interval > opt.MaxIntervalSeconds {
		return Rate{Reason: StalePrevious}
	}
	delta := cur.Octets - prev.Octets
	if delta < 0 {
		// A 32-bit counter may wrap once; a 64-bit decrease means a reset.
		if cur.Width == 32 && prev.Octets < twoPow32 {
			delta += twoPow32
		} else {
			return Rate{Reason: CounterReset}
		}
	}
	bps := delta * 8 / interval
	if opt.IfSpeedBps > 0 && bps > opt.IfSpeedBps*1.1 {
		return Rate{Reason: CounterReset}
	}
	return Rate{OK: true, Bps: bps, IntervalSeconds: interval}
}
