package collector

import (
	"net/netip"
	"strconv"
	"time"

	"network-traffic-visualizer/internal/counters"
	"network-traffic-visualizer/internal/flow"
	"network-traffic-visualizer/internal/packet"
	"network-traffic-visualizer/internal/sflow"
)

// NormalizeStats counts samples that could not become observations.
type NormalizeStats struct {
	NonIPSamples          int // no IP information (ARP, MPLS, unknown header protocol, ...)
	InvalidSamplingRate   int // sampling_rate == 0: cannot estimate, dropped
	CounterSamplesNoIface int // counter sample without generic interface counters
}

// ExporterID is the observation-point identity: agent address + sub-agent.
func ExporterID(agent netip.Addr, subAgent uint32) string {
	return agent.String() + "/" + strconv.FormatUint(uint64(subAgent), 10)
}

func ifIndexPtr(i sflow.Interface) *int {
	if v, ok := i.IfIndex(); ok {
		n := int(v)
		return &n
	}
	return nil
}

func portPtr(p *uint16) *int {
	if p == nil {
		return nil
	}
	n := int(*p)
	return &n
}

// Normalize converts a decoded datagram into domain observations.
// observedAt is the collector receive time: sFlow carries no wall-clock
// timestamp, only agent uptime (D-046).
func Normalize(d *sflow.Datagram, observedAt time.Time) ([]flow.Sample, []counters.Observation, NormalizeStats) {
	var st NormalizeStats
	exporter := ExporterID(d.AgentAddress, d.SubAgentID)
	agent := d.AgentAddress.String()
	samples := make([]flow.Sample, 0, len(d.FlowSamples))

	for _, fs := range d.FlowSamples {
		if fs.SamplingRate == 0 {
			st.InvalidSamplingRate++
			continue
		}
		s := flow.Sample{
			ObservedAt: observedAt, ExporterID: exporter, AgentAddress: agent,
			DatagramSequence: d.SequenceNumber, SampleSequence: fs.SequenceNumber,
			InputIfIndex: ifIndexPtr(fs.Input), OutputIfIndex: ifIndexPtr(fs.Output),
			SamplingRate: int(fs.SamplingRate),
		}
		ok := false
		// Prefer the raw header (most exporters send it); fall back to sampled_ipv4/6.
		if h := fs.RawHeader; h != nil {
			var info packet.Info
			var err error
			switch h.Protocol {
			case sflow.HeaderProtocolEthernet:
				info, err = packet.ParseEthernet(h.Header)
			case sflow.HeaderProtocolIPv4:
				info, err = packet.ParseIPv4(h.Header)
			case sflow.HeaderProtocolIPv6:
				info, err = packet.ParseIPv6(h.Header)
			default:
				err = packet.ErrNotIP
			}
			if err == nil && info.SrcIP.IsValid() {
				ok = true
				s.SrcIP, s.DstIP = info.SrcIP.String(), info.DstIP.String()
				s.IPProtocol = int(info.IPProtocol)
				s.SrcPort, s.DstPort = portPtr(info.SrcPort), portPtr(info.DstPort)
				if info.VLAN != nil {
					v := int(*info.VLAN)
					s.VLAN = &v
				}
				// frame_length: original frame length on the wire (D-045).
				s.SampledPacketLength = int(h.FrameLength)
			}
		}
		if !ok {
			ip := fs.SampledIPv4
			if ip == nil {
				ip = fs.SampledIPv6
			}
			if ip != nil && ip.Src.IsValid() {
				ok = true
				s.SrcIP, s.DstIP = ip.Src.Unmap().String(), ip.Dst.Unmap().String()
				s.IPProtocol = int(ip.Protocol)
				if ip.Protocol == packet.ProtoTCP || ip.Protocol == packet.ProtoUDP {
					sp, dp := int(ip.SrcPort), int(ip.DstPort)
					s.SrcPort, s.DstPort = &sp, &dp
				}
				s.SampledPacketLength = int(ip.Length)
			}
		}
		if !ok {
			st.NonIPSamples++
			continue
		}
		if s.VLAN == nil && fs.ExtendedSwitch != nil && fs.ExtendedSwitch.SrcVLAN != 0 {
			v := int(fs.ExtendedSwitch.SrcVLAN)
			s.VLAN = &v
		}
		s.AddressFamily = "ipv4"
		if a, err := netip.ParseAddr(s.SrcIP); err == nil && a.Is6() {
			s.AddressFamily = "ipv6"
		}
		s.Protocol = flow.ProtocolClass(s.IPProtocol)
		s.EstimatedBytes = float64(s.SampledPacketLength) * float64(s.SamplingRate)
		samples = append(samples, s)
	}

	obs := make([]counters.Observation, 0, len(d.CounterSamples))
	for _, cs := range d.CounterSamples {
		g := cs.Generic
		if g == nil {
			st.CounterSamplesNoIface++
			continue
		}
		obs = append(obs, counters.Observation{
			ObservedAt: observedAt, ExporterID: exporter, AgentAddress: agent,
			IfIndex: int(g.IfIndex), IfSpeedBps: float64(g.IfSpeed),
			InOctets: g.InOctets, OutOctets: g.OutOctets, InErrors: g.InErrors, OutErrors: g.OutErrors,
			AdminUp: g.AdminUp(), OperUp: g.OperUp(),
		})
	}
	return samples, obs, st
}
