package packet

import (
	"encoding/binary"
	"net/netip"
)

// BuildFrame builds the first bytes of an Ethernet frame carrying an IPv4 or
// IPv6 packet of `frameLength` bytes with a TCP/UDP header. Used by the sFlow
// generator and tests; only headers are produced (no payload).
func BuildFrame(src, dst netip.Addr, proto uint8, srcPort, dstPort uint16, frameLength int, vlan *uint16) []byte {
	b := make([]byte, 0, 80)
	b = append(b, 0x02, 0, 0, 0, 0, 0x02, 0x02, 0, 0, 0, 0, 0x01) // dst, src MAC
	ipLen := frameLength - 14
	if vlan != nil {
		b = binary.BigEndian.AppendUint16(b, EtherTypeVLAN)
		b = binary.BigEndian.AppendUint16(b, *vlan&0x0FFF)
		ipLen -= 4
	}
	if src.Is4() {
		b = binary.BigEndian.AppendUint16(b, EtherTypeIPv4)
		h := make([]byte, 20)
		h[0] = 0x45
		binary.BigEndian.PutUint16(h[2:4], uint16(max(ipLen, 20)))
		h[8] = 64
		h[9] = proto
		s4, d4 := src.As4(), dst.As4()
		copy(h[12:16], s4[:])
		copy(h[16:20], d4[:])
		binary.BigEndian.PutUint16(h[10:12], ipv4Checksum(h))
		b = append(b, h...)
	} else {
		b = binary.BigEndian.AppendUint16(b, EtherTypeIPv6)
		h := make([]byte, 40)
		h[0] = 0x60
		binary.BigEndian.PutUint16(h[4:6], uint16(max(ipLen-40, 0)))
		h[6] = proto
		h[7] = 64
		s16, d16 := src.As16(), dst.As16()
		copy(h[8:24], s16[:])
		copy(h[24:40], d16[:])
		b = append(b, h...)
	}
	l4 := make([]byte, 8)
	if proto == ProtoTCP {
		l4 = make([]byte, 20)
		l4[12] = 0x50
		l4[13] = 0x18 // PSH|ACK
	}
	binary.BigEndian.PutUint16(l4[0:2], srcPort)
	binary.BigEndian.PutUint16(l4[2:4], dstPort)
	return append(b, l4...)
}

func ipv4Checksum(h []byte) uint16 {
	var sum uint32
	for i := 0; i < len(h); i += 2 {
		sum += uint32(binary.BigEndian.Uint16(h[i : i+2]))
	}
	for sum>>16 != 0 {
		sum = sum&0xFFFF + sum>>16
	}
	return ^uint16(sum)
}
