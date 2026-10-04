// Package packet parses the truncated packet headers carried in sFlow raw
// packet header records (Ethernet / 802.1Q / IPv4 / IPv6 / TCP / UDP).
//
// Input is untrusted network data: every access is bounds-checked and the
// parser never panics. Missing layers are reported as absent, not guessed.
package packet

import (
	"encoding/binary"
	"errors"
	"net/netip"
)

const (
	EtherTypeIPv4 = 0x0800
	EtherTypeIPv6 = 0x86DD
	EtherTypeVLAN = 0x8100
	EtherTypeQinQ = 0x88A8

	ProtoICMP   = 1
	ProtoTCP    = 6
	ProtoUDP    = 17
	ProtoICMPv6 = 58
)

var (
	ErrTruncated = errors.New("packet: truncated header")
	ErrNotIP     = errors.New("packet: not an IP packet")
)

// Info is what the visualization pipeline needs from a sampled header.
type Info struct {
	VLAN       *uint16
	SrcIP      netip.Addr
	DstIP      netip.Addr
	IPProtocol uint8
	// IPLength is the IP total length (IPv4) or header+payload length (IPv6)
	// claimed by the packet; 0 if unknown.
	IPLength int
	SrcPort  *uint16
	DstPort  *uint16
	TCPFlags uint8
}

// ParseEthernet parses an Ethernet II frame header (with up to two VLAN tags).
func ParseEthernet(b []byte) (Info, error) {
	if len(b) < 14 {
		return Info{}, ErrTruncated
	}
	var info Info
	etherType := binary.BigEndian.Uint16(b[12:14])
	off := 14
	for tags := 0; (etherType == EtherTypeVLAN || etherType == EtherTypeQinQ) && tags < 2; tags++ {
		if len(b) < off+4 {
			return Info{}, ErrTruncated
		}
		if info.VLAN == nil { // outermost tag
			v := binary.BigEndian.Uint16(b[off:off+2]) & 0x0FFF
			info.VLAN = &v
		}
		etherType = binary.BigEndian.Uint16(b[off+2 : off+4])
		off += 4
	}
	var ip Info
	var err error
	switch etherType {
	case EtherTypeIPv4:
		ip, err = ParseIPv4(b[off:])
	case EtherTypeIPv6:
		ip, err = ParseIPv6(b[off:])
	default:
		return info, ErrNotIP
	}
	ip.VLAN = info.VLAN
	return ip, err
}

// ParseIPv4 parses an IPv4 header and, for the first fragment, L4 ports.
func ParseIPv4(b []byte) (Info, error) {
	if len(b) < 20 || b[0]>>4 != 4 {
		if len(b) >= 1 && b[0]>>4 != 4 {
			return Info{}, ErrNotIP
		}
		return Info{}, ErrTruncated
	}
	ihl := int(b[0]&0x0F) * 4
	if ihl < 20 {
		return Info{}, ErrNotIP
	}
	info := Info{
		IPProtocol: b[9],
		IPLength:   int(binary.BigEndian.Uint16(b[2:4])),
		SrcIP:      netip.AddrFrom4([4]byte(b[12:16])),
		DstIP:      netip.AddrFrom4([4]byte(b[16:20])),
	}
	fragOffset := binary.BigEndian.Uint16(b[6:8]) & 0x1FFF
	if fragOffset != 0 || len(b) < ihl {
		return info, nil // later fragments carry no L4 header
	}
	parseL4(&info, b[ihl:])
	return info, nil
}

// ParseIPv6 parses an IPv6 header, skipping common extension headers.
func ParseIPv6(b []byte) (Info, error) {
	if len(b) < 40 {
		if len(b) >= 1 && b[0]>>4 != 6 {
			return Info{}, ErrNotIP
		}
		return Info{}, ErrTruncated
	}
	if b[0]>>4 != 6 {
		return Info{}, ErrNotIP
	}
	info := Info{
		IPLength: 40 + int(binary.BigEndian.Uint16(b[4:6])),
		SrcIP:    netip.AddrFrom16([16]byte(b[8:24])),
		DstIP:    netip.AddrFrom16([16]byte(b[24:40])),
	}
	next := b[6]
	off := 40
	for hops := 0; hops < 8; hops++ {
		switch next {
		case 0, 43, 60: // hop-by-hop, routing, destination options
			if len(b) < off+2 {
				info.IPProtocol = next
				return info, nil
			}
			next, off = b[off], off+(int(b[off+1])+1)*8
		case 44: // fragment
			if len(b) < off+8 {
				info.IPProtocol = next
				return info, nil
			}
			fragOffset := binary.BigEndian.Uint16(b[off+2:off+4]) >> 3
			next, off = b[off], off+8
			if fragOffset != 0 {
				info.IPProtocol = next
				return info, nil
			}
		case 51: // authentication header
			if len(b) < off+2 {
				info.IPProtocol = next
				return info, nil
			}
			next, off = b[off], off+(int(b[off+1])+2)*4
		default:
			info.IPProtocol = next
			if off <= len(b) {
				parseL4(&info, b[off:])
			}
			return info, nil
		}
	}
	info.IPProtocol = next
	return info, nil
}

func parseL4(info *Info, b []byte) {
	switch info.IPProtocol {
	case ProtoTCP, ProtoUDP:
		if len(b) < 4 {
			return
		}
		sp, dp := binary.BigEndian.Uint16(b[0:2]), binary.BigEndian.Uint16(b[2:4])
		info.SrcPort, info.DstPort = &sp, &dp
		if info.IPProtocol == ProtoTCP && len(b) >= 14 {
			info.TCPFlags = b[13]
		}
	}
}
