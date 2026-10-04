package sflow

import (
	"encoding/binary"
	"encoding/hex"
	"errors"
	"net/netip"
	"reflect"
	"testing"
)

func be32(vs ...uint32) []byte {
	var b []byte
	for _, v := range vs {
		b = binary.BigEndian.AppendUint32(b, v)
	}
	return b
}

func cat(parts ...[]byte) []byte {
	var b []byte
	for _, p := range parts {
		b = append(b, p...)
	}
	return b
}

// handFixture is laid out field by field from sflow_version_5.txt,
// independently of the encoder.
func handFixture() []byte {
	header := []byte{0xde, 0xad, 0xbe, 0xef, 0x01} // 5 bytes → padded to 8
	rawRecord := cat(be32(1 /*ethernet*/, 1514 /*frame_length*/, 4 /*stripped*/, 5 /*header_length*/), header, []byte{0, 0, 0})
	switchRecord := be32(10, 0, 20, 0)
	flowBody := cat(
		be32(7),          // sequence_number
		be32(0<<24|3),    // source_id: ifIndex 3
		be32(512),        // sampling_rate
		be32(100000),     // sample_pool
		be32(2),          // drops
		be32(3),          // input ifIndex 3
		be32(0x3FFFFFFF), // output unknown
		be32(3),          // 3 records
		be32(1, uint32(len(rawRecord))), rawRecord,
		be32(1001, 16), switchRecord,
		be32(9<<12|1, 8), be32(0xAA, 0xBB), // enterprise 9 record: unknown, skipped
	)
	generic := cat(
		be32(1, 6), binary.BigEndian.AppendUint64(nil, 1_000_000_000), be32(1, 3),
		binary.BigEndian.AppendUint64(nil, 123456789), be32(1, 2, 3, 4, 5, 6),
		binary.BigEndian.AppendUint64(nil, 987654321), be32(7, 8, 9, 10, 11, 0),
	)
	counterBody := cat(be32(9, 0<<24|1, 1), be32(1, uint32(len(generic))), generic)
	return cat(
		be32(5),                       // version
		be32(1), []byte{192, 0, 2, 1}, // agent IPv4
		be32(0, 42, 3600000), // sub agent, sequence, uptime
		be32(3),              // 3 samples
		be32(1, uint32(len(flowBody))), flowBody,
		be32(2, uint32(len(counterBody))), counterBody,
		be32(0xFFF, 4), be32(0), // unknown sample format: skipped
	)
}

func TestDecodeHandFixture(t *testing.T) {
	if len(cat(be32(1, 6), make([]byte, 80))) != 88 {
		t.Fatal("generic counters must be 88 bytes")
	}
	d, err := Decode(handFixture())
	if err != nil {
		t.Fatal(err)
	}
	if d.AgentAddress.String() != "192.0.2.1" || d.SequenceNumber != 42 || d.UptimeMs != 3600000 {
		t.Fatalf("header: %+v", d)
	}
	if len(d.FlowSamples) != 1 || len(d.CounterSamples) != 1 {
		t.Fatalf("samples: %d flow, %d counter", len(d.FlowSamples), len(d.CounterSamples))
	}
	f := d.FlowSamples[0]
	if f.SamplingRate != 512 || f.SourceIDIndex != 3 || f.Drops != 2 {
		t.Fatalf("flow: %+v", f)
	}
	if in, ok := f.Input.IfIndex(); !ok || in != 3 {
		t.Fatalf("input: %+v", f.Input)
	}
	if _, ok := f.Output.IfIndex(); ok {
		t.Fatal("output 0x3FFFFFFF must be unknown")
	}
	if f.RawHeader == nil || f.RawHeader.FrameLength != 1514 || hex.EncodeToString(f.RawHeader.Header) != "deadbeef01" {
		t.Fatalf("raw header: %+v", f.RawHeader)
	}
	if f.ExtendedSwitch == nil || f.ExtendedSwitch.SrcVLAN != 10 || f.ExtendedSwitch.DstVLAN != 20 {
		t.Fatalf("switch: %+v", f.ExtendedSwitch)
	}
	g := d.CounterSamples[0].Generic
	if g == nil || g.IfIndex != 1 || g.IfSpeed != 1e9 || g.InOctets != 123456789 || g.OutOctets != 987654321 || g.InErrors != 5 || g.OutErrors != 11 || !g.AdminUp() || !g.OperUp() {
		t.Fatalf("generic: %+v", g)
	}
	if d.Stats.UnknownSamples != 1 || d.Stats.UnknownRecords != 1 {
		t.Fatalf("stats: %+v", d.Stats)
	}
}

func TestEncoderMatchesHandFixtureLayout(t *testing.T) {
	// Re-encoding the decoded fixture's known parts must decode identically.
	d, _ := Decode(handFixture())
	b := &DatagramBuilder{Agent: d.AgentAddress}
	b.Add(EncodeFlowSample(d.FlowSamples[0], false))
	b.Add(EncodeCounterSample(d.CounterSamples[0], false))
	d2, err := Decode(b.Bytes(d.SequenceNumber, d.UptimeMs))
	if err != nil {
		t.Fatal(err)
	}
	d.Stats, d2.Stats = DecodeStats{}, DecodeStats{}
	if !reflect.DeepEqual(d, d2) {
		t.Fatalf("round trip mismatch:\n%+v\n%+v", d, d2)
	}
}

func TestRoundTripExpandedAndIPv6(t *testing.T) {
	s := FlowSample{
		SequenceNumber: 1, SourceIDType: 0, SourceIDIndex: 0x01FFFFFF, SamplingRate: 1024, SamplePool: 5,
		Input: Interface{0, 0x01FFFFFF}, Output: Interface{2, 3},
		SampledIPv6: &SampledIP{Length: 1300, Protocol: 17, Src: netip.MustParseAddr("2001:db8::1"),
			Dst: netip.MustParseAddr("fd00::2"), SrcPort: 443, DstPort: 50000},
	}
	b := &DatagramBuilder{Agent: netip.MustParseAddr("2001:db8::ff"), SubAgentID: 3}
	b.Add(EncodeFlowSample(s, true))
	b.Add(EncodeCounterSample(CounterSample{SequenceNumber: 2, SourceIDIndex: 9}, true))
	d, err := Decode(b.Bytes(1, 2))
	if err != nil {
		t.Fatal(err)
	}
	if d.AgentAddress.String() != "2001:db8::ff" || d.SubAgentID != 3 {
		t.Fatalf("agent: %+v", d)
	}
	got := d.FlowSamples[0]
	if !reflect.DeepEqual(got, s) {
		t.Fatalf("expanded flow sample mismatch:\n%+v\n%+v", got, s)
	}
	if _, ok := got.Output.IfIndex(); ok {
		t.Fatal("multiple-interface output must not be a single ifIndex")
	}
	if d.CounterSamples[0].Generic != nil || d.CounterSamples[0].SourceIDIndex != 9 {
		t.Fatalf("counter: %+v", d.CounterSamples[0])
	}
}

func TestRejectsBadFraming(t *testing.T) {
	good := handFixture()
	cases := map[string][]byte{
		"empty":            nil,
		"version 4":        cat(be32(4), good[4:]),
		"agent type 7":     cat(be32(5, 7), good[8:]),
		"too many samples": cat(good[:24], be32(1_000_000)),
		"sample length overflow": func() []byte {
			b := append([]byte(nil), good...)
			binary.BigEndian.PutUint32(b[32:], 0xFFFFFFF0)
			return b
		}(),
	}
	want := map[string]string{
		"empty": "truncated", "version 4": "unsupported_version", "agent type 7": "invalid_agent_address",
		"too many samples": "too_many_samples", "sample length overflow": "length_overflow",
	}
	for name, b := range cases {
		_, err := Decode(b)
		if err == nil || ErrorKind(err) != want[name] {
			t.Errorf("%s: got %v, want %s", name, err, want[name])
		}
	}
}

func TestMalformedRecordInsideValidSampleIsSkipped(t *testing.T) {
	// raw header record claims 200 header bytes but its record length is 16.
	rec := be32(1, 1514, 0, 200)
	body := cat(be32(1, 3, 256, 1, 0, 3, 4, 1), be32(1, uint32(len(rec))), rec)
	b := cat(be32(5, 1), []byte{10, 0, 0, 1}, be32(0, 1, 1, 1), be32(1, uint32(len(body))), body)
	d, err := Decode(b)
	if err != nil {
		t.Fatal(err)
	}
	if len(d.FlowSamples) != 1 || d.FlowSamples[0].RawHeader != nil || d.Stats.MalformedRecords != 1 {
		t.Fatalf("got %+v", d)
	}
}

func TestEveryTruncationIsSafe(t *testing.T) {
	good := handFixture()
	for i := 0; i < len(good); i++ {
		d, err := Decode(good[:i])
		if err == nil && d == nil {
			t.Fatalf("prefix %d: nil datagram without error", i)
		}
		if err != nil && !errors.As(err, new(*DecodeError)) {
			t.Fatalf("prefix %d: untyped error %v", i, err)
		}
	}
}

func FuzzDecode(f *testing.F) {
	f.Add(handFixture())
	b := &DatagramBuilder{Agent: netip.MustParseAddr("::1")}
	b.Add(EncodeFlowSample(FlowSample{SamplingRate: 1, SampledIPv4: &SampledIP{Src: netip.MustParseAddr("1.2.3.4"), Dst: netip.MustParseAddr("5.6.7.8")}}, true))
	f.Add(b.Bytes(1, 1))
	f.Fuzz(func(t *testing.T, data []byte) {
		d, err := Decode(data) // must never panic
		if err == nil && d == nil {
			t.Fatal("nil datagram without error")
		}
	})
}
