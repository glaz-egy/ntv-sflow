package packet

import (
	"net/netip"
	"testing"
)

func TestRoundTripIPv4TCPWithVLAN(t *testing.T) {
	vlan := uint16(20)
	b := BuildFrame(netip.MustParseAddr("10.20.0.10"), netip.MustParseAddr("198.51.100.10"), ProtoTCP, 50000, 443, 600, &vlan)
	info, err := ParseEthernet(b)
	if err != nil {
		t.Fatal(err)
	}
	if info.SrcIP.String() != "10.20.0.10" || info.DstIP.String() != "198.51.100.10" || info.IPProtocol != ProtoTCP {
		t.Fatalf("got %+v", info)
	}
	if *info.SrcPort != 50000 || *info.DstPort != 443 || *info.VLAN != 20 || info.IPLength != 600-18 {
		t.Fatalf("got %+v", info)
	}
	if info.TCPFlags != 0x18 {
		t.Fatalf("tcp flags %x", info.TCPFlags)
	}
}

func TestRoundTripIPv6UDP(t *testing.T) {
	b := BuildFrame(netip.MustParseAddr("fd00:40::22"), netip.MustParseAddr("2001:db8:1::10"), ProtoUDP, 50001, 443, 1350, nil)
	info, err := ParseEthernet(b)
	if err != nil {
		t.Fatal(err)
	}
	if info.SrcIP.String() != "fd00:40::22" || info.IPProtocol != ProtoUDP || *info.DstPort != 443 || info.IPLength != 1336 {
		t.Fatalf("got %+v", info)
	}
}

func TestTruncatedHeadersKeepWhatIsKnown(t *testing.T) {
	b := BuildFrame(netip.MustParseAddr("10.0.0.1"), netip.MustParseAddr("10.0.0.2"), ProtoTCP, 1, 2, 100, nil)
	// Only Ethernet + IPv4: addresses known, ports unknown.
	info, err := ParseEthernet(b[:34])
	if err != nil || info.SrcIP.String() != "10.0.0.1" || info.SrcPort != nil {
		t.Fatalf("got %+v %v", info, err)
	}
	if _, err := ParseEthernet(b[:20]); err != ErrTruncated {
		t.Fatalf("want ErrTruncated, got %v", err)
	}
}

func TestNonIPAndFragments(t *testing.T) {
	arp := make([]byte, 42)
	arp[12], arp[13] = 0x08, 0x06
	if _, err := ParseEthernet(arp); err != ErrNotIP {
		t.Fatalf("ARP: %v", err)
	}
	b := BuildFrame(netip.MustParseAddr("10.0.0.1"), netip.MustParseAddr("10.0.0.2"), ProtoUDP, 1, 2, 100, nil)
	b[14+6] = 0x00
	b[14+7] = 0x10 // fragment offset != 0
	info, err := ParseEthernet(b)
	if err != nil || info.SrcPort != nil {
		t.Fatalf("later fragment must not yield ports: %+v %v", info, err)
	}
}

func TestIPv6ExtensionHeaders(t *testing.T) {
	b := BuildFrame(netip.MustParseAddr("2001:db8::1"), netip.MustParseAddr("2001:db8::2"), ProtoTCP, 7, 8, 200, nil)
	ip := append([]byte(nil), b[14:54]...)
	l4 := b[54:]
	ip[6] = 0 // next header: hop-by-hop
	hbh := []byte{ProtoTCP, 0, 0, 0, 0, 0, 0, 0}
	pkt := append(append(ip, hbh...), l4...)
	info, err := ParseIPv6(pkt)
	if err != nil || info.IPProtocol != ProtoTCP || *info.DstPort != 8 {
		t.Fatalf("got %+v %v", info, err)
	}
}

func FuzzParseEthernet(f *testing.F) {
	f.Add(BuildFrame(netip.MustParseAddr("10.0.0.1"), netip.MustParseAddr("10.0.0.2"), ProtoTCP, 1, 2, 100, nil))
	f.Add(BuildFrame(netip.MustParseAddr("::1"), netip.MustParseAddr("::2"), ProtoUDP, 1, 2, 100, nil))
	f.Fuzz(func(t *testing.T, b []byte) {
		_, _ = ParseEthernet(b) // must never panic
	})
}
