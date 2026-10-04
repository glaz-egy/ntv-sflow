// Package sflow decodes (and, for tests and the generator, encodes) sFlow
// version 5 datagrams: https://sflow.org/sflow_version_5.txt
//
// Input is untrusted UDP payload: lengths are validated before use, the
// decoder never panics (fuzzed), unknown sample/record formats are skipped
// via their length fields and counted, and nothing here knows about the UI.
package sflow

import (
	"errors"
	"net/netip"
)

const Version5 = 5

// Data formats (enterprise 0).
const (
	FormatFlowSample                   = 1
	FormatCounterSample                = 2
	FormatFlowSampleExpanded           = 3
	FormatCounterSampleExpanded        = 4
	RecordRawPacketHeader              = 1
	RecordSampledIPv4                  = 3
	RecordSampledIPv6                  = 4
	RecordExtendedSwitch               = 1001
	RecordGenericIfCounters            = 1
	HeaderProtocolEthernet             = 1
	HeaderProtocolIPv4                 = 11
	HeaderProtocolIPv6                 = 12
	maxSamplesPerDatagram              = 1024
	maxRecordsPerSample                = 128
	ifIndexUnknown              uint32 = 0x3FFFFFFF
)

// Error kinds are bounded strings usable as metric labels.
type DecodeError struct{ Kind string }

func (e *DecodeError) Error() string { return "sflow: " + e.Kind }

var (
	errTruncated      = &DecodeError{"truncated"}
	errLengthOverflow = &DecodeError{"length_overflow"}
	errVersion        = &DecodeError{"unsupported_version"}
	errAgentAddress   = &DecodeError{"invalid_agent_address"}
	errTooManySamples = &DecodeError{"too_many_samples"}
	errTooManyRecords = &DecodeError{"too_many_records"}
)

// ErrorKind returns the metric label for an error ("other" if not a DecodeError).
func ErrorKind(err error) string {
	var de *DecodeError
	if errors.As(err, &de) {
		return de.Kind
	}
	return "other"
}

type Datagram struct {
	AgentAddress   netip.Addr
	SubAgentID     uint32
	SequenceNumber uint32
	UptimeMs       uint32
	FlowSamples    []FlowSample
	CounterSamples []CounterSample
	Stats          DecodeStats
}

// DecodeStats counts things skipped inside an otherwise valid datagram.
type DecodeStats struct {
	UnknownSamples   int
	UnknownRecords   int
	MalformedSamples int
	MalformedRecords int
}

// Interface is an sFlow interface reference. Format 0 = single ifIndex,
// 1 = packet discarded, 2 = multiple interfaces.
type Interface struct {
	Format uint32
	Value  uint32
}

// IfIndex returns the ifIndex if the reference is a single known interface.
func (i Interface) IfIndex() (uint32, bool) {
	if i.Format != 0 || i.Value == ifIndexUnknown || i.Value == 0 {
		return 0, false
	}
	return i.Value, true
}

type FlowSample struct {
	SequenceNumber uint32
	SourceIDType   uint32
	SourceIDIndex  uint32
	SamplingRate   uint32
	SamplePool     uint32
	Drops          uint32
	Input          Interface
	Output         Interface
	RawHeader      *RawPacketHeader
	SampledIPv4    *SampledIP
	SampledIPv6    *SampledIP
	ExtendedSwitch *ExtendedSwitch
}

type RawPacketHeader struct {
	Protocol    uint32
	FrameLength uint32
	Stripped    uint32
	Header      []byte // aliases the datagram buffer
}

// SampledIP is the sampled_ipv4 / sampled_ipv6 record.
type SampledIP struct {
	Length   uint32
	Protocol uint32
	Src, Dst netip.Addr
	SrcPort  uint32
	DstPort  uint32
	TCPFlags uint32
	TOS      uint32
}

type ExtendedSwitch struct {
	SrcVLAN, SrcPriority, DstVLAN, DstPriority uint32
}

type CounterSample struct {
	SequenceNumber uint32
	SourceIDType   uint32
	SourceIDIndex  uint32
	Generic        *GenericInterfaceCounters
}

type GenericInterfaceCounters struct {
	IfIndex          uint32
	IfType           uint32
	IfSpeed          uint64
	IfDirection      uint32
	IfStatus         uint32 // bit 0 admin up, bit 1 oper up
	InOctets         uint64
	InUcastPkts      uint32
	InMulticastPkts  uint32
	InBroadcastPkts  uint32
	InDiscards       uint32
	InErrors         uint32
	InUnknownProtos  uint32
	OutOctets        uint64
	OutUcastPkts     uint32
	OutMulticastPkts uint32
	OutBroadcastPkts uint32
	OutDiscards      uint32
	OutErrors        uint32
	PromiscuousMode  uint32
}

func (g GenericInterfaceCounters) AdminUp() bool { return g.IfStatus&1 != 0 }
func (g GenericInterfaceCounters) OperUp() bool  { return g.IfStatus&2 != 0 }
