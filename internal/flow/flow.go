// Package flow holds the normalized flow domain: observations and the
// exporters that produced them. No protocol (sFlow) or transport types.
package flow

import (
	"strconv"
	"time"
)

// Protocol is the L4 protocol class used for filtering and display.
type Protocol string

const (
	TCP   Protocol = "tcp"
	UDP   Protocol = "udp"
	ICMP  Protocol = "icmp"
	Other Protocol = "other"
)

func ParseProtocol(s string) (Protocol, bool) {
	switch p := Protocol(s); p {
	case TCP, UDP, ICMP, Other:
		return p, true
	}
	return "", false
}

// ExporterRole describes where an exporter sits; used for observation de-dup (D-024).
type ExporterRole string

const (
	RoleBoundary ExporterRole = "boundary"
	RoleCore     ExporterRole = "core"
	RoleAccess   ExporterRole = "access"
)

type Exporter struct {
	ID              string
	Name            string
	AgentAddress    string
	Role            ExporterRole
	SamplingRate    int
	BoundaryIfIndex *int
	IfSpeedBps      float64
}

// ObservationPolicy lists exporter roles in preference order per scope.
type ObservationPolicy struct {
	External []ExporterRole
	Internal []ExporterRole
}

// WindowObservation is one aggregation window of sampled observations for a
// unidirectional flow key at one exporter. EstimatedBytes is
// Σ sampled_packet_length × sampling_rate — an estimate, not exact bytes.
type WindowObservation struct {
	ExporterID     string
	InputIfIndex   *int
	OutputIfIndex  *int
	SrcIP          string
	DstIP          string
	Protocol       Protocol
	SrcPort        *int
	DstPort        *int
	SamplingRate   int
	SampleCount    int
	EstimatedBytes float64
	// LastSampleAt is a window-relative tick (seconds); nil without samples.
	LastSampleAt *int
}

// Key identifies the unidirectional flow independent of the exporter.
func (o WindowObservation) Key() string {
	return o.SrcIP + "|" + o.DstIP + "|" + string(o.Protocol) + "|" + portText(o.SrcPort) + "|" + portText(o.DstPort)
}

func portText(p *int) string {
	if p == nil {
		return ""
	}
	return strconv.Itoa(*p)
}

// Sample is one normalized sampled packet (docs/DATA_MODEL.md §3
// FlowObservation) as produced by a collector. It is protocol-agnostic:
// sFlow today, NetFlow/IPFIX later.
type Sample struct {
	ObservedAt time.Time
	// ExporterID identifies the observation point: "<agent address>/<sub-agent id>".
	ExporterID       string
	AgentAddress     string
	DatagramSequence uint32
	SampleSequence   uint32
	InputIfIndex     *int
	OutputIfIndex    *int

	SrcIP         string
	DstIP         string
	AddressFamily string // ipv4 | ipv6
	SrcPort       *int
	DstPort       *int
	IPProtocol    int
	Protocol      Protocol
	VLAN          *int

	// SampledPacketLength is the original frame length reported by the exporter.
	SampledPacketLength int
	SamplingRate        int
	// EstimatedBytes = SampledPacketLength × SamplingRate. An estimate, never exact.
	EstimatedBytes float64
}

// ProtocolClass maps an IP protocol number to the display class.
func ProtocolClass(ipProto int) Protocol {
	switch ipProto {
	case 6:
		return TCP
	case 17:
		return UDP
	case 1, 58:
		return ICMP
	}
	return Other
}
