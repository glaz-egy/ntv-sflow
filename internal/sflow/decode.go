package sflow

import "net/netip"

// Decode parses one sFlow v5 datagram. An error means the datagram framing
// is unusable; malformed or unknown samples/records inside a well-framed
// datagram are skipped and counted in Datagram.Stats.
func Decode(b []byte) (*Datagram, error) {
	r := &reader{b: b}
	version, err := r.u32()
	if err != nil {
		return nil, err
	}
	if version != Version5 {
		return nil, errVersion
	}
	d := &Datagram{}
	if d.AgentAddress, err = readAddress(r); err != nil {
		return nil, err
	}
	for _, dst := range []*uint32{&d.SubAgentID, &d.SequenceNumber, &d.UptimeMs} {
		if *dst, err = r.u32(); err != nil {
			return nil, err
		}
	}
	n, err := r.u32()
	if err != nil {
		return nil, err
	}
	// Each sample needs at least 8 bytes of framing.
	if n > maxSamplesPerDatagram || uint64(n)*8 > uint64(r.remaining()) {
		return nil, errTooManySamples
	}
	for i := uint32(0); i < n; i++ {
		format, err := r.u32()
		if err != nil {
			return nil, err
		}
		length, err := r.u32()
		if err != nil {
			return nil, err
		}
		body, err := r.sub(length)
		if err != nil {
			return nil, err // framing broken: cannot find the next sample
		}
		enterprise, f := format>>12, format&0xFFF
		if enterprise != 0 {
			d.Stats.UnknownSamples++
			continue
		}
		switch f {
		case FormatFlowSample, FormatFlowSampleExpanded:
			s, err := decodeFlowSample(body, f == FormatFlowSampleExpanded, &d.Stats)
			if err != nil {
				d.Stats.MalformedSamples++
				continue
			}
			d.FlowSamples = append(d.FlowSamples, s)
		case FormatCounterSample, FormatCounterSampleExpanded:
			s, err := decodeCounterSample(body, f == FormatCounterSampleExpanded, &d.Stats)
			if err != nil {
				d.Stats.MalformedSamples++
				continue
			}
			d.CounterSamples = append(d.CounterSamples, s)
		default:
			d.Stats.UnknownSamples++
		}
	}
	return d, nil
}

func readAddress(r *reader) (netip.Addr, error) {
	typ, err := r.u32()
	if err != nil {
		return netip.Addr{}, err
	}
	switch typ {
	case 1:
		b, err := r.bytes(4)
		if err != nil {
			return netip.Addr{}, err
		}
		return netip.AddrFrom4([4]byte(b)), nil
	case 2:
		b, err := r.bytes(16)
		if err != nil {
			return netip.Addr{}, err
		}
		return netip.AddrFrom16([16]byte(b)), nil
	}
	return netip.Addr{}, errAgentAddress
}

func readFields(r *reader, dsts ...*uint32) error {
	for _, d := range dsts {
		v, err := r.u32()
		if err != nil {
			return err
		}
		*d = v
	}
	return nil
}

func decodeFlowSample(r *reader, expanded bool, st *DecodeStats) (FlowSample, error) {
	var s FlowSample
	var err error
	if expanded {
		err = readFields(r, &s.SequenceNumber, &s.SourceIDType, &s.SourceIDIndex, &s.SamplingRate,
			&s.SamplePool, &s.Drops, &s.Input.Format, &s.Input.Value, &s.Output.Format, &s.Output.Value)
	} else {
		var source, in, out uint32
		err = readFields(r, &s.SequenceNumber, &source, &s.SamplingRate, &s.SamplePool, &s.Drops, &in, &out)
		s.SourceIDType, s.SourceIDIndex = source>>24, source&0x00FFFFFF
		s.Input = Interface{Format: in >> 30, Value: in & 0x3FFFFFFF}
		s.Output = Interface{Format: out >> 30, Value: out & 0x3FFFFFFF}
	}
	if err != nil {
		return s, err
	}
	n, err := r.u32()
	if err != nil {
		return s, err
	}
	if n > maxRecordsPerSample {
		return s, errTooManyRecords
	}
	for i := uint32(0); i < n; i++ {
		format, err := r.u32()
		if err != nil {
			return s, err
		}
		length, err := r.u32()
		if err != nil {
			return s, err
		}
		body, err := r.sub(length)
		if err != nil {
			return s, err
		}
		if format>>12 != 0 {
			st.UnknownRecords++
			continue
		}
		var recErr error
		switch format & 0xFFF {
		case RecordRawPacketHeader:
			h := &RawPacketHeader{}
			var hl uint32
			if recErr = readFields(body, &h.Protocol, &h.FrameLength, &h.Stripped, &hl); recErr == nil {
				h.Header, recErr = body.opaque(hl)
			}
			if recErr == nil {
				s.RawHeader = h
			}
		case RecordSampledIPv4, RecordSampledIPv6:
			ip := &SampledIP{}
			size := 4
			if format&0xFFF == RecordSampledIPv6 {
				size = 16
			}
			if recErr = readFields(body, &ip.Length, &ip.Protocol); recErr == nil {
				var src, dst []byte
				if src, recErr = body.bytes(size); recErr == nil {
					if dst, recErr = body.bytes(size); recErr == nil {
						ip.Src, _ = netip.AddrFromSlice(src)
						ip.Dst, _ = netip.AddrFromSlice(dst)
						recErr = readFields(body, &ip.SrcPort, &ip.DstPort, &ip.TCPFlags, &ip.TOS)
					}
				}
			}
			if recErr == nil {
				if size == 4 {
					s.SampledIPv4 = ip
				} else {
					s.SampledIPv6 = ip
				}
			}
		case RecordExtendedSwitch:
			sw := &ExtendedSwitch{}
			if recErr = readFields(body, &sw.SrcVLAN, &sw.SrcPriority, &sw.DstVLAN, &sw.DstPriority); recErr == nil {
				s.ExtendedSwitch = sw
			}
		default:
			st.UnknownRecords++
		}
		if recErr != nil {
			st.MalformedRecords++
		}
	}
	return s, nil
}

func decodeCounterSample(r *reader, expanded bool, st *DecodeStats) (CounterSample, error) {
	var s CounterSample
	var err error
	if expanded {
		err = readFields(r, &s.SequenceNumber, &s.SourceIDType, &s.SourceIDIndex)
	} else {
		var source uint32
		err = readFields(r, &s.SequenceNumber, &source)
		s.SourceIDType, s.SourceIDIndex = source>>24, source&0x00FFFFFF
	}
	if err != nil {
		return s, err
	}
	n, err := r.u32()
	if err != nil {
		return s, err
	}
	if n > maxRecordsPerSample {
		return s, errTooManyRecords
	}
	for i := uint32(0); i < n; i++ {
		format, err := r.u32()
		if err != nil {
			return s, err
		}
		length, err := r.u32()
		if err != nil {
			return s, err
		}
		body, err := r.sub(length)
		if err != nil {
			return s, err
		}
		if format != RecordGenericIfCounters { // includes enterprise != 0
			st.UnknownRecords++
			continue
		}
		g, err := decodeGeneric(body)
		if err != nil {
			st.MalformedRecords++
			continue
		}
		s.Generic = g
	}
	return s, nil
}

func decodeGeneric(r *reader) (*GenericInterfaceCounters, error) {
	g := &GenericInterfaceCounters{}
	var err error
	if err = readFields(r, &g.IfIndex, &g.IfType); err != nil {
		return nil, err
	}
	if g.IfSpeed, err = r.u64(); err != nil {
		return nil, err
	}
	if err = readFields(r, &g.IfDirection, &g.IfStatus); err != nil {
		return nil, err
	}
	if g.InOctets, err = r.u64(); err != nil {
		return nil, err
	}
	if err = readFields(r, &g.InUcastPkts, &g.InMulticastPkts, &g.InBroadcastPkts, &g.InDiscards, &g.InErrors, &g.InUnknownProtos); err != nil {
		return nil, err
	}
	if g.OutOctets, err = r.u64(); err != nil {
		return nil, err
	}
	if err = readFields(r, &g.OutUcastPkts, &g.OutMulticastPkts, &g.OutBroadcastPkts, &g.OutDiscards, &g.OutErrors, &g.PromiscuousMode); err != nil {
		return nil, err
	}
	return g, nil
}
