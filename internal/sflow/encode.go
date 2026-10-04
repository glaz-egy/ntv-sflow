package sflow

import (
	"encoding/binary"
	"net/netip"
)

// Encoding is used by the sFlow generator (cmd/sflow-gen) and tests.

type writer struct{ b []byte }

func (w *writer) u32(v uint32) { w.b = binary.BigEndian.AppendUint32(w.b, v) }
func (w *writer) u64(v uint64) { w.b = binary.BigEndian.AppendUint64(w.b, v) }
func (w *writer) addr(a netip.Addr) {
	if a.Is4() {
		w.u32(1)
		b := a.As4()
		w.b = append(w.b, b[:]...)
		return
	}
	w.u32(2)
	b := a.As16()
	w.b = append(w.b, b[:]...)
}
func (w *writer) opaque(p []byte) {
	w.u32(uint32(len(p)))
	w.b = append(w.b, p...)
	for len(w.b)%4 != 0 {
		w.b = append(w.b, 0)
	}
}

// record appends format, length, body.
func (w *writer) record(format uint32, body []byte) {
	w.u32(format)
	w.u32(uint32(len(body)))
	w.b = append(w.b, body...)
}

// EncodeFlowSample returns a complete sample (format, length, body).
func EncodeFlowSample(s FlowSample, expanded bool) []byte {
	var records [][2]any
	if h := s.RawHeader; h != nil {
		r := &writer{}
		r.u32(h.Protocol)
		r.u32(h.FrameLength)
		r.u32(h.Stripped)
		r.opaque(h.Header)
		records = append(records, [2]any{uint32(RecordRawPacketHeader), r.b})
	}
	for _, ip := range []*SampledIP{s.SampledIPv4, s.SampledIPv6} {
		if ip == nil {
			continue
		}
		r := &writer{}
		r.u32(ip.Length)
		r.u32(ip.Protocol)
		if ip.Src.Is4() {
			a, b := ip.Src.As4(), ip.Dst.As4()
			r.b = append(append(r.b, a[:]...), b[:]...)
		} else {
			a, b := ip.Src.As16(), ip.Dst.As16()
			r.b = append(append(r.b, a[:]...), b[:]...)
		}
		r.u32(ip.SrcPort)
		r.u32(ip.DstPort)
		r.u32(ip.TCPFlags)
		r.u32(ip.TOS)
		f := uint32(RecordSampledIPv4)
		if !ip.Src.Is4() {
			f = RecordSampledIPv6
		}
		records = append(records, [2]any{f, r.b})
	}
	if sw := s.ExtendedSwitch; sw != nil {
		r := &writer{}
		r.u32(sw.SrcVLAN)
		r.u32(sw.SrcPriority)
		r.u32(sw.DstVLAN)
		r.u32(sw.DstPriority)
		records = append(records, [2]any{uint32(RecordExtendedSwitch), r.b})
	}

	body := &writer{}
	body.u32(s.SequenceNumber)
	format := uint32(FormatFlowSample)
	if expanded {
		format = FormatFlowSampleExpanded
		body.u32(s.SourceIDType)
		body.u32(s.SourceIDIndex)
		body.u32(s.SamplingRate)
		body.u32(s.SamplePool)
		body.u32(s.Drops)
		body.u32(s.Input.Format)
		body.u32(s.Input.Value)
		body.u32(s.Output.Format)
		body.u32(s.Output.Value)
	} else {
		body.u32(s.SourceIDType<<24 | s.SourceIDIndex&0x00FFFFFF)
		body.u32(s.SamplingRate)
		body.u32(s.SamplePool)
		body.u32(s.Drops)
		body.u32(s.Input.Format<<30 | s.Input.Value&0x3FFFFFFF)
		body.u32(s.Output.Format<<30 | s.Output.Value&0x3FFFFFFF)
	}
	body.u32(uint32(len(records)))
	for _, rec := range records {
		body.record(rec[0].(uint32), rec[1].([]byte))
	}
	out := &writer{}
	out.record(format, body.b)
	return out.b
}

// EncodeCounterSample returns a complete counter sample with generic interface counters.
func EncodeCounterSample(s CounterSample, expanded bool) []byte {
	body := &writer{}
	body.u32(s.SequenceNumber)
	format := uint32(FormatCounterSample)
	if expanded {
		format = FormatCounterSampleExpanded
		body.u32(s.SourceIDType)
		body.u32(s.SourceIDIndex)
	} else {
		body.u32(s.SourceIDType<<24 | s.SourceIDIndex&0x00FFFFFF)
	}
	if g := s.Generic; g != nil {
		body.u32(1)
		r := &writer{}
		r.u32(g.IfIndex)
		r.u32(g.IfType)
		r.u64(g.IfSpeed)
		r.u32(g.IfDirection)
		r.u32(g.IfStatus)
		r.u64(g.InOctets)
		for _, v := range []uint32{g.InUcastPkts, g.InMulticastPkts, g.InBroadcastPkts, g.InDiscards, g.InErrors, g.InUnknownProtos} {
			r.u32(v)
		}
		r.u64(g.OutOctets)
		for _, v := range []uint32{g.OutUcastPkts, g.OutMulticastPkts, g.OutBroadcastPkts, g.OutDiscards, g.OutErrors, g.PromiscuousMode} {
			r.u32(v)
		}
		body.record(RecordGenericIfCounters, r.b)
	} else {
		body.u32(0)
	}
	out := &writer{}
	out.record(format, body.b)
	return out.b
}

// DatagramBuilder assembles encoded samples into datagrams.
type DatagramBuilder struct {
	Agent      netip.Addr
	SubAgentID uint32
	samples    [][]byte
	size       int
}

const datagramHeaderMax = 4 + 4 + 16 + 4 + 4 + 4 + 4

func (b *DatagramBuilder) Add(sample []byte) {
	b.samples = append(b.samples, sample)
	b.size += len(sample)
}

// Size is the encoded size if Bytes were called now.
func (b *DatagramBuilder) Size() int  { return datagramHeaderMax + b.size }
func (b *DatagramBuilder) Count() int { return len(b.samples) }

// Bytes encodes the datagram and resets the builder.
func (b *DatagramBuilder) Bytes(sequence, uptimeMs uint32) []byte {
	w := &writer{b: make([]byte, 0, b.Size())}
	w.u32(Version5)
	w.addr(b.Agent)
	w.u32(b.SubAgentID)
	w.u32(sequence)
	w.u32(uptimeMs)
	w.u32(uint32(len(b.samples)))
	for _, s := range b.samples {
		w.b = append(w.b, s...)
	}
	b.samples, b.size = nil, 0
	return w.b
}
