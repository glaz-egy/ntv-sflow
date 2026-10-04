package mock

import (
	"math"

	"network-traffic-visualizer/internal/enrichment"
	"network-traffic-visualizer/internal/flow"
	"network-traffic-visualizer/internal/topology"
)

// Mirrors engine.ts.

const (
	WindowSeconds          = 5
	CounterIntervalSeconds = 5
	routerWanIf            = 1
	routerLanIf            = 2
	backgroundBps          = 400_000
)

type observer struct {
	exporter      flow.Exporter
	inputIfIndex  *int
	outputIfIndex *int
}

type legPlan struct {
	conv        *Conversation
	up          bool
	srcIP       string
	dstIP       string
	srcPort     int
	dstPort     int
	packetBytes int
	observers   []observer
	wan         int // +1 inbound, -1 outbound, 0 internal
}

func (p *legPlan) leg() string {
	if p.up {
		return "up"
	}
	return "down"
}

func (p *legPlan) profile() RateProfile {
	if p.up {
		return p.conv.Up
	}
	return p.conv.Down
}

// corePortForIP follows the *physical* topology (not scenario knowledge).
func corePortForIP(addr string) *int {
	key := enrichment.CanonicalIP(addr)
	var devID string
	for _, d := range Devices() {
		for _, a := range d.Addresses {
			if enrichment.CanonicalIP(a.Address) == key {
				devID = d.ID
			}
		}
	}
	if devID == "" {
		return nil
	}
	links := Topology()
	current := devID
	for hops := 0; hops < 8; hops++ {
		if port, ok := corePorts[current]; ok {
			return ip(port)
		}
		var parent *topology.Link
		for i := range links {
			if links[i].B == current {
				parent = &links[i]
				break
			}
		}
		if parent == nil {
			return nil
		}
		if parent.A == "dev_core_sw" {
			if port, ok := corePorts[current]; ok {
				return ip(port)
			}
			return nil
		}
		current = parent.A
	}
	return nil
}

func ProfileFactor(seed int, key string, p RateProfile, t int) float64 {
	gate := 1.0
	if p.Shape == "burst" || p.Shape == "periodic" {
		period := p.PeriodSeconds
		if period == 0 {
			period = 60
		}
		duty := p.Duty
		if duty == 0 {
			duty = 0.3
		}
		phase := int(math.Floor(KeyedUniform(seed, key, "phase") * float64(period)))
		pos := (((t + phase) % period) + period) % period
		if float64(pos) < duty*float64(period) {
			gate = 1
		} else if p.Shape == "burst" {
			gate = 0.02
		} else {
			gate = 0
		}
	}
	return gate * math.Max(0, 1+float64(p.Jitter*SmoothNoise(seed, key, t, 8)))
}

type Engine struct {
	Scenario    Scenario
	Seed        int
	plans       []legPlan
	router      flow.Exporter
	secondCache map[int][]flow.WindowObservation
	cumulative  [][2]float64 // in, out octets at the start of second i
}

func NewEngine(sc Scenario, seed int) (*Engine, error) {
	classifier, err := enrichment.NewClassifier(sc.Inventory.InternalCIDRs)
	if err != nil {
		return nil, err
	}
	e := &Engine{Scenario: sc, Seed: seed, secondCache: map[int][]flow.WindowObservation{}}
	var core *flow.Exporter
	for i, x := range sc.Inventory.Exporters {
		switch x.Role {
		case flow.RoleBoundary:
			e.router = sc.Inventory.Exporters[i]
		case flow.RoleCore:
			core = &sc.Inventory.Exporters[i]
		}
	}
	if e.router.ID == "" || core == nil {
		panic("mock inventory requires boundary and core exporters")
	}
	routerCore := corePorts["dev_router"]
	for ci := range sc.Conversations {
		conv := &sc.Conversations[ci]
		for _, up := range []bool{true, false} {
			src, dst := conv.Client, conv.Server
			if !up {
				src, dst = conv.Server, conv.Client
			}
			srcInt, _ := classifier.IsInternal(src)
			dstInt, _ := classifier.IsInternal(dst)
			wan := 0
			if srcInt && !dstInt {
				wan = -1
			} else if !srcInt && dstInt {
				wan = 1
			}
			srcCore, dstCore := ip(routerCore), ip(routerCore)
			if srcInt {
				srcCore = corePortForIP(src)
			}
			if dstInt {
				dstCore = corePortForIP(dst)
			}
			obs := []observer{{exporter: *core, inputIfIndex: srcCore, outputIfIndex: dstCore}}
			if wan != 0 {
				in, out := routerLanIf, routerWanIf
				if wan == 1 {
					in, out = routerWanIf, routerLanIf
				}
				obs = append(obs, observer{exporter: e.router, inputIfIndex: ip(in), outputIfIndex: ip(out)})
			}
			plan := legPlan{conv: conv, up: up, srcIP: src, dstIP: dst, observers: obs, wan: wan}
			if up {
				plan.srcPort, plan.dstPort = conv.ClientPort, conv.ServerPort
				plan.packetBytes = 600
			} else {
				plan.srcPort, plan.dstPort = conv.ServerPort, conv.ClientPort
				plan.packetBytes = 1350
			}
			if conv.PacketBytes != nil {
				if up {
					plan.packetBytes = conv.PacketBytes[0]
				} else {
					plan.packetBytes = conv.PacketBytes[1]
				}
			}
			e.plans = append(e.plans, plan)
		}
	}
	return e, nil
}

func (e *Engine) EffectiveTick(tick int) int {
	if s := e.Scenario.DataStopsAtTick; s != nil && tick > *s {
		return *s
	}
	return tick
}

func (e *Engine) IsStale(tick int) bool {
	s := e.Scenario.DataStopsAtTick
	return s != nil && tick > *s
}

func (e *Engine) trueBytes(p *legPlan, t int) float64 {
	prof := p.profile()
	factor := ProfileFactor(e.Seed, p.conv.ID+"/"+p.leg(), prof, t)
	return (prof.Bps * factor) / 8
}

func (e *Engine) sampleSecond(t int) []flow.WindowObservation {
	if c, ok := e.secondCache[t]; ok {
		return c
	}
	var out []flow.WindowObservation
	for i := range e.plans {
		p := &e.plans[i]
		packets := e.trueBytes(p, t) / float64(p.packetBytes)
		for _, o := range p.observers {
			n := o.exporter.SamplingRate
			r := KeyedRand(e.Seed, p.conv.ID, p.leg(), o.exporter.ID, t)
			samples := Poisson(r, packets/float64(n))
			w := flow.WindowObservation{
				ExporterID: o.exporter.ID, InputIfIndex: o.inputIfIndex, OutputIfIndex: o.outputIfIndex,
				SrcIP: p.srcIP, DstIP: p.dstIP, Protocol: p.conv.Protocol,
				SrcPort: ip(p.srcPort), DstPort: ip(p.dstPort), SamplingRate: n,
				SampleCount: samples, EstimatedBytes: float64(samples * p.packetBytes * n),
			}
			if samples > 0 {
				w.LastSampleAt = ip(t)
			}
			out = append(out, w)
		}
	}
	e.secondCache[t] = out
	if len(e.secondCache) > 64 {
		oldest := t
		for k := range e.secondCache {
			if k < oldest {
				oldest = k
			}
		}
		delete(e.secondCache, oldest)
	}
	return out
}

// WindowObservations returns observations for (end−W, end], one per
// (exporter, flow key), in first-seen order; keys without samples omitted.
func (e *Engine) WindowObservations(endTick int) []flow.WindowObservation {
	end := e.EffectiveTick(endTick)
	var order []string
	merged := map[string]*flow.WindowObservation{}
	for t := end - WindowSeconds + 1; t <= end; t++ {
		for _, o := range e.sampleSecond(t) {
			if o.SampleCount == 0 {
				continue
			}
			key := o.ExporterID + "|" + o.Key()
			if prev, ok := merged[key]; ok {
				prev.SampleCount += o.SampleCount
				prev.EstimatedBytes += o.EstimatedBytes
				if o.LastSampleAt != nil {
					prev.LastSampleAt = o.LastSampleAt
				}
				continue
			}
			c := o
			merged[key] = &c
			order = append(order, key)
		}
	}
	out := make([]flow.WindowObservation, 0, len(order))
	for _, k := range order {
		out = append(out, *merged[k])
	}
	return out
}

func (e *Engine) cumulativeAt(t int) [2]float64 {
	if len(e.cumulative) == 0 {
		offset := math.Floor(KeyedUniform(e.Seed, "wan-offset") * 1e12)
		e.cumulative = append(e.cumulative, [2]float64{offset, math.Floor(offset / 3)})
	}
	for len(e.cumulative) <= t {
		s := len(e.cumulative) - 1
		prev := e.cumulative[s]
		inBytes, outBytes := 0.0, 0.0
		for i := range e.plans {
			p := &e.plans[i]
			if p.wan == 0 {
				continue
			}
			b := e.trueBytes(p, s)
			if p.wan == 1 {
				inBytes += b
			} else {
				outBytes += b
			}
		}
		bg := (backgroundBps / 8.0) * (1 + float64(0.3*SmoothNoise(e.Seed, "bg", s, 8)))
		e.cumulative = append(e.cumulative, [2]float64{
			prev[0] + math.Floor(inBytes+bg+0.5),
			prev[1] + math.Floor(outBytes+bg/2+0.5),
		})
	}
	if t < 0 {
		t = 0
	}
	return e.cumulative[t]
}

// CounterReading is a boundary (WAN) interface counter poll.
type CounterReading struct {
	At                  int
	InOctets, OutOctets float64
}

// WanCounterReadings returns the two most recent polls at or before tick;
// previous is nil before the second poll.
func (e *Engine) WanCounterReadings(tick int) (prev *CounterReading, cur CounterReading) {
	end := e.EffectiveTick(tick)
	at := int(math.Floor(float64(end)/CounterIntervalSeconds)) * CounterIntervalSeconds
	read := func(s int) CounterReading {
		c := e.cumulativeAt(s)
		return CounterReading{At: s, InOctets: c[0], OutOctets: c[1]}
	}
	if at-CounterIntervalSeconds >= 0 {
		r := read(at - CounterIntervalSeconds)
		prev = &r
	}
	return prev, read(at)
}

func (e *Engine) BoundaryExporter() flow.Exporter { return e.router }

// TrueBytes is exported for tests.
func (e *Engine) TrueBytes(convID string, up bool, t int) float64 {
	for i := range e.plans {
		if e.plans[i].conv.ID == convID && e.plans[i].up == up {
			return e.trueBytes(&e.plans[i], t)
		}
	}
	return 0
}

// SecondObservations returns the per-exporter sample counts for sim second t
// (used by the sFlow generator to emit real datagrams).
func (e *Engine) SecondObservations(t int) []flow.WindowObservation { return e.sampleSecond(t) }

// WanOctets returns the cumulative boundary-interface octet counters at the
// start of sim second t.
func (e *Engine) WanOctets(t int) (in, out float64) {
	c := e.cumulativeAt(t)
	return c[0], c[1]
}
