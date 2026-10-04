// Package aggregation turns window observations into attributed flows:
//  1. de-duplicate observation points (D-007, D-024)
//  2. classify internal/external from configured CIDRs (D-008)
//  3. resolve internal endpoints to device identities (D-009)
//  4. attach GeoIP/ASN metadata to external endpoints
//
// Mirrors apps/web/src/lib/mock-backend/pipeline/attribute.ts.
package aggregation

import (
	"sort"

	"network-traffic-visualizer/internal/devices"
	"network-traffic-visualizer/internal/enrichment"
	"network-traffic-visualizer/internal/flow"
)

type Scope string

const (
	ScopeInternal Scope = "internal"
	ScopeExternal Scope = "external"
	ScopeTransit  Scope = "transit"
)

type Direction string

const (
	Inbound  Direction = "inbound"
	Outbound Direction = "outbound"
)

type AttributedFlow struct {
	Key            string
	SrcIP, DstIP   string
	Protocol       flow.Protocol
	SrcPort        *int
	DstPort        *int
	ServicePort    *int
	Scope          Scope
	Direction      Direction // "" for internal/transit
	SrcNodeID      *string
	DstNodeID      *string
	InternalNodeID *string
	ExternalIP     *string
	Geo            enrichment.GeoRecord
	EstimatedBytes float64
	SampleCount    int
	Used           flow.WindowObservation
	Observations   []flow.WindowObservation // every exporter that saw it; values not summed
	LastSeenAt     *int
}

type Context struct {
	Classifier *enrichment.Classifier
	Registry   *devices.Registry
	Geo        enrichment.GeoLookup
	Exporters  []flow.Exporter
	Policy     flow.ObservationPolicy
}

// ChooseObservation picks the observation that counts for a flow: exporter
// role preference, ties by exporter id. Never the sum, never the maximum.
func ChooseObservation(candidates []flow.WindowObservation, exporters map[string]flow.Exporter, roles []flow.ExporterRole) flow.WindowObservation {
	rank := func(o flow.WindowObservation) int {
		e, ok := exporters[o.ExporterID]
		if !ok {
			return len(roles)
		}
		for i, r := range roles {
			if r == e.Role {
				return i
			}
		}
		return len(roles)
	}
	sorted := append([]flow.WindowObservation(nil), candidates...)
	sort.SliceStable(sorted, func(i, j int) bool {
		ri, rj := rank(sorted[i]), rank(sorted[j])
		if ri != rj {
			return ri < rj
		}
		return sorted[i].ExporterID < sorted[j].ExporterID
	})
	return sorted[0]
}

func servicePort(src, dst *int) *int {
	if src == nil || dst == nil {
		if src != nil {
			return src
		}
		return dst
	}
	if *src < *dst {
		return src
	}
	return dst
}

// Attribute groups observations by flow key (in first-seen order) and
// attributes each flow.
func Attribute(observations []flow.WindowObservation, ctx Context) []AttributedFlow {
	exporters := map[string]flow.Exporter{}
	for _, e := range ctx.Exporters {
		exporters[e.ID] = e
	}
	// Canonical order: results must not depend on arrival order (D-052).
	sortedObs := append([]flow.WindowObservation(nil), observations...)
	sort.SliceStable(sortedObs, func(i, j int) bool {
		ki, kj := sortedObs[i].Key(), sortedObs[j].Key()
		if ki != kj {
			return ki < kj
		}
		return sortedObs[i].ExporterID < sortedObs[j].ExporterID
	})
	var order []string
	groups := map[string][]flow.WindowObservation{}
	for _, o := range sortedObs {
		k := o.Key()
		if _, ok := groups[k]; !ok {
			order = append(order, k)
		}
		groups[k] = append(groups[k], o)
	}

	out := make([]AttributedFlow, 0, len(order))
	for _, key := range order {
		obs := groups[key]
		s := obs[0]
		srcInternal, ok1 := ctx.Classifier.IsInternal(s.SrcIP)
		dstInternal, ok2 := ctx.Classifier.IsInternal(s.DstIP)
		if !ok1 || !ok2 {
			continue // unparseable: dropped from views, never guessed
		}

		var scope Scope
		var dir Direction
		switch {
		case srcInternal && dstInternal:
			scope = ScopeInternal
		case !srcInternal && !dstInternal:
			scope = ScopeTransit
		default:
			scope = ScopeExternal
			if srcInternal {
				dir = Outbound
			} else {
				dir = Inbound
			}
		}
		roles := ctx.Policy.External
		if scope == ScopeInternal {
			roles = ctx.Policy.Internal
		}
		used := ChooseObservation(obs, exporters, roles)

		f := AttributedFlow{
			Key: key, SrcIP: s.SrcIP, DstIP: s.DstIP, Protocol: s.Protocol,
			SrcPort: s.SrcPort, DstPort: s.DstPort, ServicePort: servicePort(s.SrcPort, s.DstPort),
			Scope: scope, Direction: dir,
			EstimatedBytes: used.EstimatedBytes, SampleCount: used.SampleCount,
			Used: used, Observations: obs,
		}
		if srcInternal {
			f.SrcNodeID = ptr(ctx.Registry.NodeIDFor(s.SrcIP))
		}
		if dstInternal {
			f.DstNodeID = ptr(ctx.Registry.NodeIDFor(s.DstIP))
		}
		if scope == ScopeExternal {
			if f.SrcNodeID != nil {
				f.InternalNodeID = f.SrcNodeID
				f.ExternalIP = ptr(s.DstIP)
			} else {
				f.InternalNodeID = f.DstNodeID
				f.ExternalIP = ptr(s.SrcIP)
			}
			f.Geo = ctx.Geo.Lookup(*f.ExternalIP)
		}
		for _, o := range obs {
			if o.LastSampleAt != nil && (f.LastSeenAt == nil || *o.LastSampleAt > *f.LastSeenAt) {
				f.LastSeenAt = o.LastSampleAt
			}
		}
		out = append(out, f)
	}
	return out
}

func ptr[T any](v T) *T { return &v }
