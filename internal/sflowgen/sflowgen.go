// Package sflowgen turns the deterministic mock traffic into real sFlow v5
// datagrams, so the collector can be exercised end to end without hardware.
// Each sampled packet the mock engine draws becomes one flow sample with an
// Ethernet/IP/L4 header; the boundary WAN interface emits counter samples.
package sflowgen

import (
	"net/netip"

	"network-traffic-visualizer/internal/flow"
	"network-traffic-visualizer/internal/mock"
	"network-traffic-visualizer/internal/packet"
	"network-traffic-visualizer/internal/sflow"
)

// MaxDatagramBytes keeps datagrams below a typical 1500-byte MTU.
const MaxDatagramBytes = 1400

type agent struct {
	exporter    flow.Exporter
	addr        netip.Addr
	datagramSeq uint32
	flowSeq     uint32
	counterSeq  uint32
	pool        uint32
	builder     sflow.DatagramBuilder
}

type Generator struct {
	engine *mock.Engine
	agents map[string]*agent
	order  []string
}

func New(engine *mock.Engine) *Generator {
	g := &Generator{engine: engine, agents: map[string]*agent{}}
	for _, e := range engine.Scenario.Inventory.Exporters {
		addr := netip.MustParseAddr(e.AgentAddress)
		g.agents[e.ID] = &agent{exporter: e, addr: addr, builder: sflow.DatagramBuilder{Agent: addr}}
		g.order = append(g.order, e.ID)
	}
	return g
}

func iface(p *int) sflow.Interface {
	if p == nil {
		return sflow.Interface{Value: 0x3FFFFFFF}
	}
	return sflow.Interface{Value: uint32(*p)}
}

func ipProto(p flow.Protocol) uint8 {
	switch p {
	case flow.TCP:
		return packet.ProtoTCP
	case flow.UDP:
		return packet.ProtoUDP
	case flow.ICMP:
		return packet.ProtoICMP
	}
	return 0
}

func (g *Generator) add(a *agent, sample []byte, uptimeMs uint32, out *[][]byte) {
	if a.builder.Count() > 0 && a.builder.Size()+len(sample) > MaxDatagramBytes {
		g.flush(a, uptimeMs, out)
	}
	a.builder.Add(sample)
}

func (g *Generator) flush(a *agent, uptimeMs uint32, out *[][]byte) {
	if a.builder.Count() == 0 {
		return
	}
	a.datagramSeq++
	*out = append(*out, a.builder.Bytes(a.datagramSeq, uptimeMs))
}

// Second returns all datagrams for sim second t.
func (g *Generator) Second(t int, uptimeMs uint32) [][]byte {
	var out [][]byte
	for _, o := range g.engine.SecondObservations(t) {
		if o.SampleCount == 0 {
			continue
		}
		a, ok := g.agents[o.ExporterID]
		if !ok {
			continue
		}
		// The mock uses a fixed packet size per conversation leg.
		packetBytes := int(o.EstimatedBytes) / (o.SampleCount * o.SamplingRate)
		src, dst := netip.MustParseAddr(o.SrcIP), netip.MustParseAddr(o.DstIP)
		header := packet.BuildFrame(src, dst, ipProto(o.Protocol), uint16(*o.SrcPort), uint16(*o.DstPort), packetBytes, nil)
		for k := 0; k < o.SampleCount; k++ {
			a.flowSeq++
			a.pool += uint32(o.SamplingRate)
			in := iface(o.InputIfIndex)
			fs := sflow.FlowSample{
				SequenceNumber: a.flowSeq, SourceIDIndex: in.Value & 0x00FFFFFF,
				SamplingRate: uint32(o.SamplingRate), SamplePool: a.pool,
				Input: in, Output: iface(o.OutputIfIndex),
				RawHeader: &sflow.RawPacketHeader{
					Protocol: sflow.HeaderProtocolEthernet, FrameLength: uint32(packetBytes), Header: header,
				},
			}
			g.add(a, sflow.EncodeFlowSample(fs, false), uptimeMs, &out)
		}
	}
	// Boundary interface counters on the mock's polling schedule.
	if t%mock.CounterIntervalSeconds == 0 {
		b := g.engine.BoundaryExporter()
		if a, ok := g.agents[b.ID]; ok && b.BoundaryIfIndex != nil {
			in, outOctets := g.engine.WanOctets(t)
			a.counterSeq++
			g.add(a, sflow.EncodeCounterSample(sflow.CounterSample{
				SequenceNumber: a.counterSeq, SourceIDIndex: uint32(*b.BoundaryIfIndex),
				Generic: &sflow.GenericInterfaceCounters{
					IfIndex: uint32(*b.BoundaryIfIndex), IfType: 6, IfSpeed: uint64(b.IfSpeedBps),
					IfDirection: 1, IfStatus: 3, InOctets: uint64(in), OutOctets: uint64(outOctets),
				},
			}, false), uptimeMs, &out)
		}
	}
	for _, id := range g.order {
		g.flush(g.agents[id], uptimeMs, &out)
	}
	return out
}
