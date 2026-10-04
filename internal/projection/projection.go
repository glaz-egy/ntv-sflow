// Package projection builds API views (Globe, Home, Topology, Device,
// Flows) from one set of attributed flows per window. Globe and Home are
// two projections of the same data, so they are consistent by construction.
//
// It is source-agnostic: the mock engine feeds it now; the live collector
// pipeline will feed it later. Mirrors apps/web/src/lib/mock-backend/server.ts.
package projection

import (
	"sort"
	"strconv"
	"strings"
	"time"

	"network-traffic-visualizer/internal/aggregation"
	c "network-traffic-visualizer/internal/contract"
	"network-traffic-visualizer/internal/devices"
	"network-traffic-visualizer/internal/enrichment"
	"network-traffic-visualizer/internal/flow"
	"network-traffic-visualizer/internal/topology"
)

const (
	DefaultGlobeLimit    = 100
	DefaultHomeEdgeLimit = 250
)

var services = map[string]string{
	"tcp/443": "https", "udp/443": "quic", "tcp/445": "smb", "tcp/2049": "nfs", "tcp/22": "ssh",
	"tcp/8883": "mqtts", "tcp/32400": "plex", "tcp/873": "rsync", "tcp/8443": "https-alt",
	"udp/123": "ntp", "udp/53": "dns",
}

func DestinationNodeID(key string) string { return "dst:" + key }

type network struct {
	name       string
	vlanID     *int
	classifier *enrichment.Classifier
}

// Inventory is what the system knows about the network (metadata layers).
type Inventory struct {
	Registry       *devices.Registry
	Exporters      []flow.Exporter
	Links          []topology.Link
	Origin         topology.Origin
	CountryAnchors map[string][2]float64
	networks       []network
}

func NewInventory(reg *devices.Registry, nets []topology.Network, exporters []flow.Exporter, links []topology.Link, origin topology.Origin, anchors map[string][2]float64) (*Inventory, error) {
	inv := &Inventory{Registry: reg, Exporters: exporters, Links: links, Origin: origin, CountryAnchors: anchors}
	for _, n := range nets {
		cl, err := enrichment.NewClassifier([]string{n.CIDR})
		if err != nil {
			return nil, err
		}
		inv.networks = append(inv.networks, network{n.Name, n.VlanID, cl})
	}
	return inv, nil
}

// WanRates are boundary-interface counter rates (D-034); nil → sampled fallback.
type WanRates struct {
	DownloadBps, UploadBps float64
	IntervalSeconds        int
}

// Frame is one aggregate window. Ticks are seconds relative to Epoch.
type Frame struct {
	Flows         []aggregation.AttributedFlow
	Epoch         time.Time
	Tick          int // server time
	WindowEndTick int
	WindowSeconds int
	WAN           *WanRates
}

func (f *Frame) At(tick int) time.Time   { return f.Epoch.Add(time.Duration(tick) * time.Second) }
func (f *Frame) ts(tick int) c.Timestamp { return c.TS(f.At(tick)) }

func (f *Frame) Window() c.TimeWindow {
	return c.TimeWindow{Start: f.ts(f.WindowEndTick - f.WindowSeconds), End: f.ts(f.WindowEndTick)}
}

func (f *Frame) sampled(bytes float64, samples int) c.Measurement {
	return c.Measurement{
		Value: (bytes * 8) / float64(f.WindowSeconds), Unit: "bps", MeasurementKind: c.KindSampledEstimate,
		WindowSeconds: c.Ptr(f.WindowSeconds), SampleCount: c.Ptr(samples),
	}
}

func counter(bps float64, interval int) c.Measurement {
	return c.Measurement{Value: bps, Unit: "bps", MeasurementKind: c.KindCounter, IntervalSeconds: c.Ptr(interval)}
}

// ------------------------------------------------------------------ helpers

type rate struct {
	rx, tx   float64
	rxS, txS int
	last     *int
}

func maxTick(a *int, b *int) *int {
	if b != nil && (a == nil || *b > *a) {
		return b
	}
	return a
}

func (inv *Inventory) networkOf(addrs []string) *network {
	for _, a := range addrs {
		for i := range inv.networks {
			if in, _ := inv.networks[i].classifier.IsInternal(a); in {
				return &inv.networks[i]
			}
		}
	}
	return nil
}

func nodeTraffic(f *Frame) map[string]*rate {
	t := map[string]*rate{}
	add := func(id *string, rx bool, fl *aggregation.AttributedFlow) {
		if id == nil {
			return
		}
		e, ok := t[*id]
		if !ok {
			e = &rate{}
			t[*id] = e
		}
		if rx {
			e.rx += fl.EstimatedBytes
			e.rxS += fl.SampleCount
		} else {
			e.tx += fl.EstimatedBytes
			e.txS += fl.SampleCount
		}
		e.last = maxTick(e.last, fl.LastSeenAt)
	}
	internet := topology.InternetNodeID
	for i := range f.Flows {
		fl := &f.Flows[i]
		if fl.Scope == aggregation.ScopeTransit {
			continue
		}
		add(fl.SrcNodeID, false, fl)
		add(fl.DstNodeID, true, fl)
		if fl.Scope == aggregation.ScopeExternal {
			add(&internet, fl.Direction == aggregation.Outbound, fl)
		}
	}
	return t
}

func (inv *Inventory) applyRates(n *c.HomeNode, f *Frame, tr *rate) {
	if tr == nil {
		return
	}
	rx, tx := f.sampled(tr.rx, tr.rxS), f.sampled(tr.tx, tr.txS)
	n.RxBps, n.TxBps = &rx, &tx
	if tr.last != nil {
		n.LastSeen = c.Ptr(f.ts(*tr.last))
	}
}

func (inv *Inventory) deviceNode(d *devices.Device, f *Frame, traffic map[string]*rate) c.HomeNode {
	addrs := make([]string, 0, len(d.Addresses))
	for _, a := range d.Addresses {
		addrs = append(addrs, a.Address)
	}
	net := inv.networkOf(addrs)
	status := "offline"
	if d.Online {
		status = "online"
	}
	n := c.HomeNode{ID: d.ID, Kind: "device", Label: d.DisplayName, DeviceType: d.Type, Status: status, Addresses: addrs}
	n.VlanID = d.VlanID
	if n.VlanID == nil && net != nil {
		n.VlanID = net.vlanID
	}
	if net != nil {
		n.NetworkName = c.Ptr(net.name)
	}
	inv.applyRates(&n, f, traffic[d.ID])
	return n
}

func (inv *Inventory) nodeFor(id string, f *Frame, traffic map[string]*rate, labelHint string) c.HomeNode {
	if d := inv.Registry.Get(id); d != nil {
		return inv.deviceNode(d, f, traffic)
	}
	if id == topology.InternetNodeID {
		n := c.HomeNode{ID: id, Kind: "internet", Label: "Internet", DeviceType: "internet", Status: "unknown", Addresses: []string{}}
		inv.applyRates(&n, f, traffic[id])
		return n
	}
	if strings.HasPrefix(id, "dst:") {
		label := labelHint
		if label == "" {
			label = id[4:]
		}
		return c.HomeNode{ID: id, Kind: "external_destination", Label: label, DeviceType: "internet", Status: "unknown", Addresses: []string{}}
	}
	// Unresolved internal endpoint: temporary identity, no fabricated metadata.
	addr := strings.TrimPrefix(id, "ep:")
	n := c.HomeNode{ID: id, Kind: "unresolved_endpoint", Label: addr, DeviceType: "unknown", Status: "unknown", Addresses: []string{addr}}
	if net := inv.networkOf([]string{addr}); net != nil {
		n.VlanID, n.NetworkName = net.vlanID, c.Ptr(net.name)
	}
	inv.applyRates(&n, f, traffic[id])
	return n
}

func (inv *Inventory) labelForNode(id string) (label, typ string, resolved bool) {
	if d := inv.Registry.Get(id); d != nil {
		return d.DisplayName, d.Type, true
	}
	return strings.TrimPrefix(id, "ep:"), "unknown", false
}

func (inv *Inventory) wanSummary(f *Frame) c.WanSummary {
	if f.WAN != nil {
		return c.WanSummary{
			Download: counter(f.WAN.DownloadBps, f.WAN.IntervalSeconds),
			Upload:   counter(f.WAN.UploadBps, f.WAN.IntervalSeconds),
			Basis:    "boundary_counter",
		}
	}
	var inB, outB float64
	var inS, outS int
	for i := range f.Flows {
		fl := &f.Flows[i]
		if fl.Scope != aggregation.ScopeExternal {
			continue
		}
		if fl.Direction == aggregation.Inbound {
			inB += fl.EstimatedBytes
			inS += fl.SampleCount
		} else {
			outB += fl.EstimatedBytes
			outS += fl.SampleCount
		}
	}
	return c.WanSummary{Download: f.sampled(inB, inS), Upload: f.sampled(outB, outS), Basis: "sampled_sum"}
}

func (inv *Inventory) observationPoints(flows []*aggregation.AttributedFlow) []c.ObservationPoint {
	names := map[string]string{}
	for _, e := range inv.Exporters {
		names[e.ID] = e.Name
	}
	points := map[string]*c.ObservationPoint{}
	for _, fl := range flows {
		for _, o := range fl.Observations {
			used := fl.Used.ExporterID == o.ExporterID
			if p, ok := points[o.ExporterID]; ok {
				p.UsedForAggregate = p.UsedForAggregate || used
				continue
			}
			name, ok := names[o.ExporterID]
			if !ok {
				name = o.ExporterID
			}
			points[o.ExporterID] = &c.ObservationPoint{
				ExporterID: o.ExporterID, ExporterName: name, InputIfIndex: o.InputIfIndex,
				OutputIfIndex: o.OutputIfIndex, UsedForAggregate: used, SamplingRate: o.SamplingRate,
			}
		}
	}
	out := make([]c.ObservationPoint, 0, len(points))
	for _, p := range points {
		out = append(out, *p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ExporterID < out[j].ExporterID })
	return out
}

func portKey(p *int) string {
	if p == nil {
		return ""
	}
	return strconv.Itoa(*p)
}

func protocolShares(f *Frame, flows []*aggregation.AttributedFlow, limit int) []c.ProtocolShare {
	type g struct {
		f       *aggregation.AttributedFlow
		bytes   float64
		samples int
	}
	var order []string
	groups := map[string]*g{}
	for _, fl := range flows {
		k := string(fl.Protocol) + "/" + portKey(fl.ServicePort)
		e, ok := groups[k]
		if !ok {
			e = &g{f: fl}
			groups[k] = e
			order = append(order, k)
		}
		e.bytes += fl.EstimatedBytes
		e.samples += fl.SampleCount
	}
	sort.SliceStable(order, func(i, j int) bool {
		a, b := groups[order[i]], groups[order[j]]
		if a.bytes != b.bytes {
			return a.bytes > b.bytes
		}
		return order[i] < order[j]
	})
	if len(order) > limit {
		order = order[:limit]
	}
	out := make([]c.ProtocolShare, 0, len(order))
	for _, k := range order {
		e := groups[k]
		share := c.ProtocolShare{Protocol: string(e.f.Protocol), Port: e.f.ServicePort, Bps: f.sampled(e.bytes, e.samples)}
		if s, ok := services[k]; ok {
			share.Service = c.Ptr(s)
		}
		out = append(out, share)
	}
	return out
}

func externalFlows(f *Frame, sourceNodeID, protocol string) []*aggregation.AttributedFlow {
	var out []*aggregation.AttributedFlow
	for i := range f.Flows {
		fl := &f.Flows[i]
		if fl.Scope != aggregation.ScopeExternal {
			continue
		}
		if sourceNodeID != "" && (fl.InternalNodeID == nil || *fl.InternalNodeID != sourceNodeID) {
			continue
		}
		if protocol != "" && string(fl.Protocol) != protocol {
			continue
		}
		out = append(out, fl)
	}
	return out
}

func matchesDestination(fl *aggregation.AttributedFlow, key string) bool {
	grouping, _, ok := aggregation.ParseDestinationKey(key)
	if !ok || fl.ExternalIP == nil {
		return false
	}
	return aggregation.DestinationKey(grouping, *fl.ExternalIP, fl.Geo) == key
}

func destinationLabelForKey(f *Frame, key string) string {
	grouping, value, ok := aggregation.ParseDestinationKey(key)
	if !ok {
		return key
	}
	for i := range f.Flows {
		if matchesDestination(&f.Flows[i], key) {
			return aggregation.DestinationLabel(grouping, *f.Flows[i].ExternalIP, f.Flows[i].Geo)
		}
	}
	if grouping == "asn" && value != "unknown" {
		return "AS" + value
	}
	return value
}

func same[T comparable](vals []*T) *T {
	if len(vals) == 0 || vals[0] == nil {
		return nil
	}
	for _, v := range vals[1:] {
		if v == nil || *v != *vals[0] {
			return nil
		}
	}
	return vals[0]
}

func (inv *Inventory) buildDestinations(f *Frame, flows []*aggregation.AttributedFlow, grouping string) []c.GlobeDestination {
	type acc struct {
		flows      []*aggregation.AttributedFlow
		inB, outB  float64
		inS, outS  int
		sources    map[string]float64
		sourceList []string
		last       *int
	}
	var order []string
	groups := map[string]*acc{}
	for _, fl := range flows {
		key := aggregation.DestinationKey(grouping, *fl.ExternalIP, fl.Geo)
		g, ok := groups[key]
		if !ok {
			g = &acc{sources: map[string]float64{}}
			groups[key] = g
			order = append(order, key)
		}
		g.flows = append(g.flows, fl)
		if fl.Direction == aggregation.Inbound {
			g.inB += fl.EstimatedBytes
			g.inS += fl.SampleCount
		} else {
			g.outB += fl.EstimatedBytes
			g.outS += fl.SampleCount
		}
		if fl.InternalNodeID != nil {
			if _, seen := g.sources[*fl.InternalNodeID]; !seen {
				g.sourceList = append(g.sourceList, *fl.InternalNodeID)
			}
			g.sources[*fl.InternalNodeID] += fl.EstimatedBytes
		}
		g.last = maxTick(g.last, fl.LastSeenAt)
	}

	out := make([]c.GlobeDestination, 0, len(order))
	for _, key := range order {
		g := groups[key]
		first := g.flows[0]
		var ccs, cns, cities, orgs []*string
		var asns []*int
		var members []aggregation.WeightedGeo
		for _, fl := range g.flows {
			ccs = append(ccs, fl.Geo.CountryCode)
			cns = append(cns, fl.Geo.CountryName)
			cities = append(cities, fl.Geo.City)
			orgs = append(orgs, fl.Geo.Organization)
			asns = append(asns, fl.Geo.ASN)
			members = append(members, aggregation.WeightedGeo{Geo: fl.Geo, Weight: fl.EstimatedBytes})
		}
		d := c.GlobeDestination{
			Key: key, Grouping: grouping, Label: aggregation.DestinationLabel(grouping, *first.ExternalIP, first.Geo),
			CountryCode: same(ccs), City: same(cities), ASN: same(asns), Organization: same(orgs),
			InboundBps: f.sampled(g.inB, g.inS), OutboundBps: f.sampled(g.outB, g.outS),
			SourceDeviceCount: len(g.sources), SourceNodeIDs: []string{},
		}
		if grouping == "ip" {
			d.IP = first.ExternalIP
		}
		if d.CountryCode != nil {
			d.CountryName = same(cns)
		}
		if loc := aggregation.GroupLocation(grouping, members, inv.CountryAnchors); loc != nil {
			d.Location = &c.GeoLocation{Latitude: loc.Latitude, Longitude: loc.Longitude, Basis: loc.Basis}
		}
		srcs := append([]string(nil), g.sourceList...)
		sort.SliceStable(srcs, func(i, j int) bool {
			a, b := g.sources[srcs[i]], g.sources[srcs[j]]
			if a != b {
				return a > b
			}
			return srcs[i] < srcs[j]
		})
		if len(srcs) > 10 {
			srcs = srcs[:10]
		}
		d.SourceNodeIDs = append(d.SourceNodeIDs, srcs...)
		last := f.WindowEndTick
		if g.last != nil {
			last = *g.last
		}
		d.LastSeen = f.ts(last)
		out = append(out, d)
	}
	return out
}

// ---------------------------------------------------------------- queries

type GlobeQuery struct {
	Grouping     string
	SourceNodeID string
	Direction    string // inbound | outbound | both
	Protocol     string
	MinBps       float64
	Limit        int
}

func (inv *Inventory) Globe(f *Frame, q GlobeQuery) c.GlobeResponse {
	if q.Direction == "" {
		q.Direction = "both"
	}
	if q.Limit <= 0 {
		q.Limit = DefaultGlobeLimit
	}
	rank := func(d *c.GlobeDestination) float64 {
		switch q.Direction {
		case "inbound":
			return d.InboundBps.Value
		case "outbound":
			return d.OutboundBps.Value
		}
		return d.InboundBps.Value + d.OutboundBps.Value
	}
	all := inv.buildDestinations(f, externalFlows(f, q.SourceNodeID, q.Protocol), q.Grouping)
	kept := all[:0]
	for i := range all {
		if r := rank(&all[i]); r > 0 && r >= q.MinBps {
			kept = append(kept, all[i])
		}
	}
	sort.SliceStable(kept, func(i, j int) bool {
		a, b := rank(&kept[i]), rank(&kept[j])
		if a != b {
			return a > b
		}
		return kept[i].Key < kept[j].Key
	})
	truncated := 0
	if len(kept) > q.Limit {
		truncated = len(kept) - q.Limit
		kept = kept[:q.Limit]
	}
	return c.GlobeResponse{
		Window: f.Window(), ServerTime: f.ts(f.Tick),
		Origin: c.GlobeOrigin{
			Label: inv.Origin.Label, Latitude: inv.Origin.Latitude, Longitude: inv.Origin.Longitude, Precision: inv.Origin.Precision,
		},
		Grouping: q.Grouping, Destinations: append([]c.GlobeDestination{}, kept...),
		TruncatedCount: truncated, Summary: inv.wanSummary(f),
	}
}

// Destination returns nil when the key is invalid or has no traffic.
func (inv *Inventory) Destination(f *Frame, key, sourceNodeID, protocol string) *c.GlobeDestinationDetail {
	grouping, _, ok := aggregation.ParseDestinationKey(key)
	if !ok {
		return nil
	}
	var flows []*aggregation.AttributedFlow
	for _, fl := range externalFlows(f, sourceNodeID, protocol) {
		if matchesDestination(fl, key) {
			flows = append(flows, fl)
		}
	}
	if len(flows) == 0 {
		return nil
	}
	dest := inv.buildDestinations(f, flows, grouping)[0]

	type srcAcc struct {
		inB, outB float64
		inS, outS int
	}
	type memAcc struct {
		f       *aggregation.AttributedFlow
		bytes   float64
		samples int
	}
	var srcOrder, memOrder []string
	sources := map[string]*srcAcc{}
	members := map[string]*memAcc{}
	for _, fl := range flows {
		id := *fl.InternalNodeID
		s, ok := sources[id]
		if !ok {
			s = &srcAcc{}
			sources[id] = s
			srcOrder = append(srcOrder, id)
		}
		if fl.Direction == aggregation.Inbound {
			s.inB += fl.EstimatedBytes
			s.inS += fl.SampleCount
		} else {
			s.outB += fl.EstimatedBytes
			s.outS += fl.SampleCount
		}
		m, ok := members[*fl.ExternalIP]
		if !ok {
			m = &memAcc{f: fl}
			members[*fl.ExternalIP] = m
			memOrder = append(memOrder, *fl.ExternalIP)
		}
		m.bytes += fl.EstimatedBytes
		m.samples += fl.SampleCount
	}
	sort.SliceStable(srcOrder, func(i, j int) bool {
		a, b := sources[srcOrder[i]], sources[srcOrder[j]]
		if ta, tb := a.inB+a.outB, b.inB+b.outB; ta != tb {
			return ta > tb
		}
		return srcOrder[i] < srcOrder[j]
	})
	sort.SliceStable(memOrder, func(i, j int) bool {
		a, b := members[memOrder[i]], members[memOrder[j]]
		if a.bytes != b.bytes {
			return a.bytes > b.bytes
		}
		return memOrder[i] < memOrder[j]
	})
	if len(memOrder) > 10 {
		memOrder = memOrder[:10]
	}

	detail := &c.GlobeDestinationDetail{
		Destination: dest, TopSources: []c.DestinationSource{}, Members: []c.DestinationMember{},
		TopProtocols: protocolShares(f, flows, 5), ObservationPoints: inv.observationPoints(flows),
	}
	for _, id := range srcOrder {
		s := sources[id]
		label, typ, resolved := inv.labelForNode(id)
		detail.TopSources = append(detail.TopSources, c.DestinationSource{
			NodeID: id, Label: label, DeviceType: typ, Resolved: resolved,
			InboundBps: f.sampled(s.inB, s.inS), OutboundBps: f.sampled(s.outB, s.outS),
		})
	}
	for _, addr := range memOrder {
		m := members[addr]
		detail.Members = append(detail.Members, c.DestinationMember{
			IP: addr, City: m.f.Geo.City, CountryCode: m.f.Geo.CountryCode, TotalBps: f.sampled(m.bytes, m.samples),
		})
	}
	return detail
}

type HomeQuery struct {
	FocusNodeID     string
	DestinationKey  string
	VlanID          *int
	DeviceTypes     []string
	Protocol        string
	MinBps          float64
	Limit           int
	IncludeInactive bool
	Scope           string // internal | external | both
}

func (inv *Inventory) Home(f *Frame, q HomeQuery) c.HomeTrafficResponse {
	traffic := nodeTraffic(f)
	if q.Scope == "" {
		q.Scope = "both"
	}
	if q.Limit <= 0 {
		q.Limit = DefaultHomeEdgeLimit
	}
	destNode := ""
	if q.DestinationKey != "" {
		destNode = DestinationNodeID(q.DestinationKey)
	}
	nodeCache := map[string]c.HomeNode{}
	node := func(id string) c.HomeNode {
		if n, ok := nodeCache[id]; ok {
			return n
		}
		hint := ""
		if id == destNode && destNode != "" {
			hint = destinationLabelForKey(f, q.DestinationKey)
		}
		n := inv.nodeFor(id, f, traffic, hint)
		nodeCache[id] = n
		return n
	}
	matches := func(id string) bool {
		n := node(id)
		if q.VlanID != nil && (n.VlanID == nil || *n.VlanID != *q.VlanID) {
			return false
		}
		if len(q.DeviceTypes) > 0 {
			for _, t := range q.DeviceTypes {
				if t == n.DeviceType {
					return true
				}
			}
			return false
		}
		return true
	}

	type edgeAcc struct {
		source, target string
		scope          string
		fwdB, revB     float64
		fwdS, revS     int
		flows          []*aggregation.AttributedFlow
		last           *int
	}
	var order []string
	edges := map[string]*edgeAcc{}
	sourceIDs := map[string]bool{}

	for i := range f.Flows {
		fl := &f.Flows[i]
		if fl.Scope == aggregation.ScopeTransit {
			continue
		}
		if q.Protocol != "" && string(fl.Protocol) != q.Protocol {
			continue
		}
		if q.Scope != "both" && string(fl.Scope) != q.Scope {
			continue
		}
		var a, b string
		var forward bool
		if fl.Scope == aggregation.ScopeInternal {
			a, b = *fl.SrcNodeID, *fl.DstNodeID
			if b < a {
				a, b = b, a
			}
			forward = *fl.SrcNodeID == a
			if !matches(a) && !matches(b) {
				continue
			}
		} else {
			internal := *fl.InternalNodeID
			matchesDest := q.DestinationKey != "" && matchesDestination(fl, q.DestinationKey)
			if matchesDest {
				sourceIDs[internal] = true
			}
			a = internal
			b = topology.InternetNodeID
			if matchesDest {
				b = destNode
			}
			forward = fl.Direction == aggregation.Outbound
			if !matches(a) {
				continue
			}
		}
		if q.FocusNodeID != "" && a != q.FocusNodeID && b != q.FocusNodeID {
			continue
		}
		id := "e:" + a + "~" + b
		e, ok := edges[id]
		if !ok {
			e = &edgeAcc{source: a, target: b, scope: string(fl.Scope)}
			edges[id] = e
			order = append(order, id)
		}
		if forward {
			e.fwdB += fl.EstimatedBytes
			e.fwdS += fl.SampleCount
		} else {
			e.revB += fl.EstimatedBytes
			e.revS += fl.SampleCount
		}
		e.flows = append(e.flows, fl)
		e.last = maxTick(e.last, fl.LastSeenAt)
	}

	total := func(e *edgeAcc) float64 { return ((e.fwdB + e.revB) * 8) / float64(f.WindowSeconds) }
	ranked := order[:0:0]
	for _, id := range order {
		if t := total(edges[id]); t >= q.MinBps && t > 0 {
			ranked = append(ranked, id)
		}
	}
	sort.SliceStable(ranked, func(i, j int) bool {
		a, b := total(edges[ranked[i]]), total(edges[ranked[j]])
		if a != b {
			return a > b
		}
		return ranked[i] < ranked[j]
	})
	kept := ranked
	if len(kept) > q.Limit {
		kept = kept[:q.Limit]
	}

	resp := c.HomeTrafficResponse{
		Window: f.Window(), ServerTime: f.ts(f.Tick), Nodes: []c.HomeNode{}, Edges: []c.TrafficEdge{},
		TruncatedCount: len(ranked) - len(kept),
	}
	nodeIDs := map[string]bool{}
	for _, id := range kept {
		e := edges[id]
		last := f.WindowEndTick
		if e.last != nil {
			last = *e.last
		}
		resp.Edges = append(resp.Edges, c.TrafficEdge{
			ID: id, Source: e.source, Target: e.target, Scope: e.scope,
			ForwardBps: f.sampled(e.fwdB, e.fwdS), ReverseBps: f.sampled(e.revB, e.revS),
			TopProtocols: protocolShares(f, e.flows, 3), ObservationPoints: inv.observationPoints(e.flows),
			LastSeen: f.ts(last),
		})
		nodeIDs[e.source], nodeIDs[e.target] = true, true
	}
	if q.FocusNodeID != "" {
		nodeIDs[q.FocusNodeID] = true
	}
	if destNode != "" {
		nodeIDs[destNode] = true
	}
	if q.IncludeInactive {
		for _, d := range inv.Registry.Devices {
			if matches(d.ID) {
				nodeIDs[d.ID] = true
			}
		}
		nodeIDs[topology.InternetNodeID] = true
	}
	ids := make([]string, 0, len(nodeIDs))
	for id := range nodeIDs {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		resp.Nodes = append(resp.Nodes, node(id))
	}

	if q.DestinationKey != "" {
		srcs := make([]string, 0, len(sourceIDs))
		for id := range sourceIDs {
			srcs = append(srcs, id)
		}
		sort.Strings(srcs)
		resp.DestinationContext = &c.DestinationContext{
			Key: q.DestinationKey, Label: destinationLabelForKey(f, q.DestinationKey), SourceNodeIDs: srcs,
		}
	}

	var internalB float64
	var internalS int
	for i := range f.Flows {
		if f.Flows[i].Scope == aggregation.ScopeInternal {
			internalB += f.Flows[i].EstimatedBytes
			internalS += f.Flows[i].SampleCount
		}
	}
	wan := inv.wanSummary(f)
	wanTotal := wan.Download
	wanTotal.Value = wan.Download.Value + wan.Upload.Value
	if wan.Download.SampleCount != nil {
		up := 0
		if wan.Upload.SampleCount != nil {
			up = *wan.Upload.SampleCount
		}
		wanTotal.SampleCount = c.Ptr(*wan.Download.SampleCount + up)
	}
	online := 0
	for _, d := range inv.Registry.Devices {
		if d.Online {
			online++
		}
	}
	resp.Summary = c.HomeSummary{
		WanTotal: wanTotal, InternalEstimated: f.sampled(internalB, internalS),
		DeviceCount: len(inv.Registry.Devices), OnlineCount: online,
	}
	return resp
}

func (inv *Inventory) Topology(f *Frame) c.TopologyResponse {
	traffic := nodeTraffic(f)
	ids := map[string]bool{topology.InternetNodeID: true}
	for _, d := range inv.Registry.Devices {
		ids[d.ID] = true
	}
	resp := c.TopologyResponse{Nodes: []c.HomeNode{}, Links: []c.TopologyLink{}, GeneratedAt: f.ts(f.Tick)}
	for _, l := range inv.Links {
		if !ids[l.A] || !ids[l.B] {
			continue
		}
		resp.Links = append(resp.Links, c.TopologyLink{
			ID: l.ID, SourceNodeID: l.A, TargetNodeID: l.B, LinkType: l.LinkType, Evidence: l.Evidence,
			Confidence: l.Confidence, SourceInterface: l.AInterface, TargetInterface: l.BInterface,
		})
	}
	sorted := make([]string, 0, len(ids))
	for id := range ids {
		sorted = append(sorted, id)
	}
	sort.Strings(sorted)
	for _, id := range sorted {
		resp.Nodes = append(resp.Nodes, inv.nodeFor(id, f, traffic, ""))
	}
	return resp
}

// Device returns nil for unknown ids (unresolved endpoints only while seen).
func (inv *Inventory) Device(f *Frame, id, grouping string) *c.DeviceDetail {
	traffic := nodeTraffic(f)
	dev := inv.Registry.Get(id)
	seen := false
	for i := range f.Flows {
		fl := &f.Flows[i]
		if (fl.SrcNodeID != nil && *fl.SrcNodeID == id) || (fl.DstNodeID != nil && *fl.DstNodeID == id) {
			seen = true
			break
		}
	}
	if dev == nil && !(strings.HasPrefix(id, "ep:") && seen) {
		return nil
	}
	n := inv.nodeFor(id, f, traffic, "")

	type peerAcc struct {
		txB, rxB float64
		txS, rxS int
	}
	type destAcc struct {
		f         *aggregation.AttributedFlow
		outB, inB float64
		outS, inS int
	}
	var peerOrder, destOrder []string
	peers := map[string]*peerAcc{}
	dests := map[string]*destAcc{}
	for i := range f.Flows {
		fl := &f.Flows[i]
		isSrc := fl.SrcNodeID != nil && *fl.SrcNodeID == id
		isDst := fl.DstNodeID != nil && *fl.DstNodeID == id
		if fl.Scope == aggregation.ScopeInternal && (isSrc || isDst) {
			peer := *fl.SrcNodeID
			if isSrc {
				peer = *fl.DstNodeID
			}
			p, ok := peers[peer]
			if !ok {
				p = &peerAcc{}
				peers[peer] = p
				peerOrder = append(peerOrder, peer)
			}
			if isSrc {
				p.txB += fl.EstimatedBytes
				p.txS += fl.SampleCount
			} else {
				p.rxB += fl.EstimatedBytes
				p.rxS += fl.SampleCount
			}
		} else if fl.Scope == aggregation.ScopeExternal && fl.InternalNodeID != nil && *fl.InternalNodeID == id {
			key := aggregation.DestinationKey(grouping, *fl.ExternalIP, fl.Geo)
			d, ok := dests[key]
			if !ok {
				d = &destAcc{f: fl}
				dests[key] = d
				destOrder = append(destOrder, key)
			}
			if fl.Direction == aggregation.Outbound {
				d.outB += fl.EstimatedBytes
				d.outS += fl.SampleCount
			} else {
				d.inB += fl.EstimatedBytes
				d.inS += fl.SampleCount
			}
		}
	}
	sort.SliceStable(peerOrder, func(i, j int) bool {
		a, b := peers[peerOrder[i]], peers[peerOrder[j]]
		if ta, tb := a.txB+a.rxB, b.txB+b.rxB; ta != tb {
			return ta > tb
		}
		return peerOrder[i] < peerOrder[j]
	})
	sort.SliceStable(destOrder, func(i, j int) bool {
		a, b := dests[destOrder[i]], dests[destOrder[j]]
		if ta, tb := a.outB+a.inB, b.outB+b.inB; ta != tb {
			return ta > tb
		}
		return destOrder[i] < destOrder[j]
	})
	if len(peerOrder) > 10 {
		peerOrder = peerOrder[:10]
	}
	if len(destOrder) > 10 {
		destOrder = destOrder[:10]
	}

	detail := &c.DeviceDetail{
		Node: n, Macs: []string{}, TopInternalPeers: []c.DevicePeer{}, TopExternalDestinations: []c.DeviceExternalDestination{},
	}
	for _, pid := range peerOrder {
		p := peers[pid]
		label, typ, _ := inv.labelForNode(pid)
		detail.TopInternalPeers = append(detail.TopInternalPeers, c.DevicePeer{
			NodeID: pid, Label: label, DeviceType: typ, TxBps: f.sampled(p.txB, p.txS), RxBps: f.sampled(p.rxB, p.rxS),
		})
	}
	for _, key := range destOrder {
		d := dests[key]
		detail.TopExternalDestinations = append(detail.TopExternalDestinations, c.DeviceExternalDestination{
			Key: key, Label: aggregation.DestinationLabel(grouping, *d.f.ExternalIP, d.f.Geo),
			CountryCode: d.f.Geo.CountryCode, OutboundBps: f.sampled(d.outB, d.outS), InboundBps: f.sampled(d.inB, d.inS),
		})
	}

	if dev != nil {
		for _, a := range dev.Addresses {
			detail.Addresses = append(detail.Addresses, c.DeviceAddress{Address: a.Address, Family: a.Family, Source: a.Source})
		}
		detail.Macs = append(detail.Macs, dev.Macs...)
		detail.SSID, detail.Vendor, detail.Model = dev.SSID, dev.Vendor, dev.Model
	} else {
		fam := "ipv4"
		if strings.Contains(n.Addresses[0], ":") {
			fam = "ipv6"
		}
		detail.Addresses = []c.DeviceAddress{{Address: n.Addresses[0], Family: fam, Source: "flow"}}
	}
	if detail.Addresses == nil {
		detail.Addresses = []c.DeviceAddress{}
	}
	for _, l := range inv.Links {
		if l.B != id {
			continue
		}
		label, _, _ := inv.labelForNode(l.A)
		if l.A == topology.InternetNodeID {
			label = "Internet"
		}
		detail.Attachment = &c.DeviceAttachment{ViaNodeID: l.A, ViaLabel: label, Interface: l.AInterface, Evidence: l.Evidence}
		break
	}
	return detail
}

type DeviceFilter struct {
	Type, Status, Search string
	VlanID               *int
}

func (inv *Inventory) Devices(f *Frame, q DeviceFilter) c.DeviceListResponse {
	traffic := nodeTraffic(f)
	search := strings.ToLower(q.Search)
	out := c.DeviceListResponse{Devices: []c.HomeNode{}}
	for i := range inv.Registry.Devices {
		n := inv.deviceNode(&inv.Registry.Devices[i], f, traffic)
		if q.Type != "" && n.DeviceType != q.Type || q.Status != "" && n.Status != q.Status {
			continue
		}
		if q.VlanID != nil && (n.VlanID == nil || *n.VlanID != *q.VlanID) {
			continue
		}
		if search != "" {
			hit := strings.Contains(strings.ToLower(n.Label), search)
			for _, a := range n.Addresses {
				hit = hit || strings.Contains(strings.ToLower(a), search)
			}
			if !hit {
				continue
			}
		}
		out.Devices = append(out.Devices, n)
	}
	sort.SliceStable(out.Devices, func(i, j int) bool { return out.Devices[i].ID < out.Devices[j].ID })
	return out
}

type FlowQuery struct {
	Source, Destination       string
	SourceNodeID, DestNodeID  string
	Protocol, Exporter, Scope string
	SrcPort, DstPort          *int
	Limit                     int
	// Offset skips the first matches (page cursor; ordering is by bytes, then key).
	Offset int
}

func (inv *Inventory) Flows(f *Frame, q FlowQuery) c.FlowSearchResponse {
	if q.Limit <= 0 {
		q.Limit = 100
	}
	eq := func(p *string, v string) bool { return v == "" || (p != nil && *p == v) }
	portEq := func(p, v *int) bool { return v == nil || (p != nil && *p == *v) }
	var matched []*aggregation.AttributedFlow
	for i := range f.Flows {
		fl := &f.Flows[i]
		if q.Source != "" && enrichment.CanonicalIP(fl.SrcIP) != enrichment.CanonicalIP(q.Source) ||
			q.Destination != "" && enrichment.CanonicalIP(fl.DstIP) != enrichment.CanonicalIP(q.Destination) ||
			!eq(fl.SrcNodeID, q.SourceNodeID) || !eq(fl.DstNodeID, q.DestNodeID) ||
			q.Protocol != "" && string(fl.Protocol) != q.Protocol ||
			q.Scope != "" && string(fl.Scope) != q.Scope ||
			!portEq(fl.SrcPort, q.SrcPort) || !portEq(fl.DstPort, q.DstPort) {
			continue
		}
		if q.Exporter != "" {
			seen := false
			for _, o := range fl.Observations {
				seen = seen || o.ExporterID == q.Exporter
			}
			if !seen {
				continue
			}
		}
		matched = append(matched, fl)
	}
	sort.SliceStable(matched, func(i, j int) bool {
		if matched[i].EstimatedBytes != matched[j].EstimatedBytes {
			return matched[i].EstimatedBytes > matched[j].EstimatedBytes
		}
		return matched[i].Key < matched[j].Key
	})
	resp := c.FlowSearchResponse{Window: f.Window(), Flows: []c.FlowRecord{}}
	matched = matched[min(q.Offset, len(matched)):]
	if len(matched) > q.Limit {
		resp.TruncatedCount = len(matched) - q.Limit
		resp.NextOffset = c.Ptr(q.Offset + q.Limit)
		matched = matched[:q.Limit]
	}
	for _, fl := range matched {
		r := c.FlowRecord{
			SrcIP: fl.SrcIP, DstIP: fl.DstIP, Protocol: string(fl.Protocol), SrcPort: fl.SrcPort, DstPort: fl.DstPort,
			Scope: string(fl.Scope), SrcNodeID: fl.SrcNodeID, DstNodeID: fl.DstNodeID,
			Bps: f.sampled(fl.EstimatedBytes, fl.SampleCount), ExporterID: fl.Used.ExporterID, SamplingRate: fl.Used.SamplingRate,
		}
		if fl.Direction != "" {
			r.Direction = c.Ptr(string(fl.Direction))
		}
		if fl.LastSeenAt != nil {
			r.LastSeen = c.Ptr(f.ts(*fl.LastSeenAt))
		}
		resp.Flows = append(resp.Flows, r)
	}
	return resp
}
