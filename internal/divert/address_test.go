package divert

import (
	"encoding/binary"
	"net/netip"
	"testing"
	"unsafe"
)

func TestWinDivertAddressABI(t *testing.T) {
	var a nativeAddress
	if unsafe.Sizeof(a) != 80 || unsafe.Offsetof(a.Flags) != 8 ||
		unsafe.Offsetof(a.IfIndex) != 16 || unsafe.Offsetof(a.SubIfIndex) != 20 {
		t.Fatal("Go structure differs from WinDivert 2.x WINDIVERT_ADDRESS")
	}
	// Check bitfield byte order, including a nontrivial destination interface.
	a.Flags = layerNetworkForward | addressOutbound | addressImpostor
	a.IfIndex = 0x12345678
	raw := unsafe.Slice((*byte)(unsafe.Pointer(&a)), 80)
	if raw[8] != 1 || raw[10] != 0x0a || binary.LittleEndian.Uint32(raw[16:20]) != 0x12345678 {
		t.Fatal("WinDivert bitfield/interface layout mismatch")
	}
	if got := a.metadata(); got.IfIndex != a.IfIndex || !got.Impostor || got.IPv6 {
		t.Fatalf("invalid metadata: %+v", got)
	}
}

func TestExactSourceFilter(t *testing.T) {
	filter, err := Filter("10.147.20.2")
	if err != nil || filter != "ip and ip.SrcAddr == 10.147.20.2" {
		t.Fatalf("filter=%q error=%v", filter, err)
	}
	for _, source := range []string{"", "0.0.0.0", "127.0.0.1", "255.255.255.255", "224.0.0.1", "169.254.1.2", "::1", "::ffff:10.147.20.2", "10.147.20.0/24", "10.147.20.2 or true"} {
		t.Run(source, func(t *testing.T) {
			if _, err := Filter(source); err == nil {
				t.Fatalf("accepted unsafe or unsupported source %q", source)
			}
		})
	}
}

func TestReturnInjectionBoundaries(t *testing.T) {
	client := netip.MustParseAddr("10.147.20.2")
	packet := make([]byte, 28)
	packet[0] = 0x45
	binary.BigEndian.PutUint16(packet[2:4], uint16(len(packet)))
	copy(packet[16:20], client.AsSlice())
	if err := validateReturnPacket(packet, client, 18); err != nil {
		t.Fatal(err)
	}
	if err := validateReturnPacket(packet, client, 0); err == nil {
		t.Fatal("accepted unspecified return interface")
	}
	packet[19] = 188
	if err := validateReturnPacket(packet, client, 18); err == nil {
		t.Fatal("accepted response to a different host")
	}
	packet[19] = 184
	for _, mutate := range []func([]byte) []byte{
		func(p []byte) []byte { return p[:19] },
		func(p []byte) []byte { p[0] = 0x65; return p },
		func(p []byte) []byte { p[0] = 0x44; return p },
		func(p []byte) []byte { p[0] = 0x4f; return p },
		func(p []byte) []byte { p[3]++; return p },
	} {
		copyPacket := append([]byte(nil), packet...)
		if err := validateReturnPacket(mutate(copyPacket), client, 18); err == nil {
			t.Fatal("accepted malformed or truncated return packet")
		}
	}
}
