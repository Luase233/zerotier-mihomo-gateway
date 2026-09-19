// Package divert provides the narrow WinDivert interface needed by this gateway.
// It never changes routes, adapter settings, NAT, or firewall rules.
package divert

import (
	"encoding/binary"
	"errors"
	"fmt"
	"net/netip"
	"unsafe"
)

// MaxPacketSize is WINDIVERT_MTU_MAX from WinDivert 2.2.2.
const MaxPacketSize = 40 + 65535

const (
	layerNetworkForward = 1
	flagSniff           = 0x0001
	flagDrop            = 0x0002
	flagRecvOnly        = 0x0004
	guardPriority       = int16(-1000)
	addressOutbound     = uint32(1 << 17)
	addressImpostor     = uint32(1 << 19)
	addressIPv6         = uint32(1 << 20)
	addressIPChecksum   = uint32(1 << 21)
	addressTCPChecksum  = uint32(1 << 22)
	addressUDPChecksum  = uint32(1 << 23)
)

// Metadata describes a packet received at NETWORK_FORWARD. IfIndex is the
// destination/egress interface, NOT the interface on which a packet arrived.
type Metadata struct {
	IfIndex     uint32
	Impostor    bool
	IPv6        bool
	IPChecksum  bool
	TCPChecksum bool
	UDPChecksum bool
}

// nativeAddress implements WINDIVERT_ADDRESS from the version 2.2.2 header.
// The bitfield at byte 8 contains Layer (bits 0..7), Event (8..15), and flags.
// The network union starts at byte 16 and occupies 64 bytes in total.
// Source: https://github.com/basil00/WinDivert/blob/v2.2.2/include/windivert.h
type nativeAddress struct {
	Timestamp  int64
	Flags      uint32
	Reserved   uint32
	IfIndex    uint32
	SubIfIndex uint32
	Padding    [56]byte
}

// Fail compilation if the Go layout ever stops matching WinDivert's ABI.
var _ [80 - unsafe.Sizeof(nativeAddress{})]byte
var _ [unsafe.Sizeof(nativeAddress{}) - 80]byte
var _ [16 - unsafe.Offsetof(nativeAddress{}.IfIndex)]byte
var _ [unsafe.Offsetof(nativeAddress{}.IfIndex) - 16]byte

func (a nativeAddress) metadata() Metadata {
	return Metadata{
		IfIndex: a.IfIndex, Impostor: a.Flags&addressImpostor != 0,
		IPv6:        a.Flags&addressIPv6 != 0,
		IPChecksum:  a.Flags&addressIPChecksum != 0,
		TCPChecksum: a.Flags&addressTCPChecksum != 0,
		UDPChecksum: a.Flags&addressUDPChecksum != 0,
	}
}

// Filter validates a single source IPv4 address and constructs the exact-source
// forward filter. No caller-supplied filter text is accepted.
func Filter(source string) (string, error) {
	ip, err := parseSource(source)
	if err != nil {
		return "", err
	}
	return "ip and ip.SrcAddr == " + ip.String(), nil
}

func parseSource(source string) (netip.Addr, error) {
	ip, err := netip.ParseAddr(source)
	if err != nil || !ip.Is4() || !ip.IsGlobalUnicast() || ip.IsLoopback() {
		return netip.Addr{}, fmt.Errorf("source must be one unicast non-loopback IPv4 address: %q", source)
	}
	return ip, nil
}

func validatePacket(packet []byte) error {
	if len(packet) < 20 || len(packet) > 65535 || packet[0]>>4 != 4 {
		return errors.New("expected one complete IPv4 packet")
	}
	headerLength := int(packet[0]&0x0f) * 4
	if headerLength < 20 || headerLength > len(packet) {
		return errors.New("invalid IPv4 header length")
	}
	if int(binary.BigEndian.Uint16(packet[2:4])) != len(packet) {
		return errors.New("IPv4 total length does not match packet buffer")
	}
	return nil
}

func validateReturnPacket(packet []byte, client netip.Addr, ifIndex uint32) error {
	if ifIndex == 0 {
		return errors.New("return interface index must be nonzero")
	}
	if err := validatePacket(packet); err != nil {
		return err
	}
	destination := netip.AddrFrom4([4]byte{packet[16], packet[17], packet[18], packet[19]})
	if destination != client {
		return fmt.Errorf("refusing to inject packet to %s: expected client %s", destination, client)
	}
	return nil
}
