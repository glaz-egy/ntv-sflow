package counters

import (
	"strconv"
	"sync"
	"time"
)

// Observation is a normalized interface counter sample
// (docs/DATA_MODEL.md §4 InterfaceCounterObservation).
type Observation struct {
	ObservedAt   time.Time
	ExporterID   string
	AgentAddress string
	IfIndex      int
	IfSpeedBps   float64
	InOctets     uint64
	OutOctets    uint64
	InErrors     uint32
	OutErrors    uint32
	AdminUp      bool
	OperUp       bool
}

// InterfaceRate is a counter-derived rate (measurement_kind: counter).
type InterfaceRate struct {
	ExporterID      string
	IfIndex         int
	RxBps, TxBps    float64
	IntervalSeconds float64
	At              time.Time
}

// Tracker turns successive observations into rates per (exporter, ifIndex).
// sFlow generic counters are 64-bit, so a decrease is a reset, not a wrap.
type Tracker struct {
	mu          sync.Mutex
	maxInterval time.Duration
	prev        map[string]Observation
}

func NewTracker(maxInterval time.Duration) *Tracker {
	return &Tracker{maxInterval: maxInterval, prev: map[string]Observation{}}
}

// Update records o and returns a rate when one can be computed.
func (t *Tracker) Update(o Observation) (InterfaceRate, bool) {
	key := o.ExporterID + "#" + strconv.Itoa(o.IfIndex)
	t.mu.Lock()
	prev, ok := t.prev[key]
	t.prev[key] = o
	t.mu.Unlock()
	if !ok {
		return InterfaceRate{}, false
	}
	opt := Options{MaxIntervalSeconds: t.maxInterval.Seconds(), IfSpeedBps: o.IfSpeedBps}
	at := func(x Observation) float64 { return float64(x.ObservedAt.UnixNano()) / 1e9 }
	rx := Compute(&Reading{At: at(prev), Octets: float64(prev.InOctets), Width: 64}, Reading{At: at(o), Octets: float64(o.InOctets), Width: 64}, opt)
	tx := Compute(&Reading{At: at(prev), Octets: float64(prev.OutOctets), Width: 64}, Reading{At: at(o), Octets: float64(o.OutOctets), Width: 64}, opt)
	if !rx.OK || !tx.OK {
		return InterfaceRate{}, false
	}
	return InterfaceRate{ExporterID: o.ExporterID, IfIndex: o.IfIndex, RxBps: rx.Bps, TxBps: tx.Bps, IntervalSeconds: rx.IntervalSeconds, At: o.ObservedAt}, true
}
