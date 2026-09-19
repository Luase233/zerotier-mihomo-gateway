package gateway

import (
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"net"
	"net/netip"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"gvisor.dev/gvisor/pkg/buffer"
	"gvisor.dev/gvisor/pkg/tcpip"
	"gvisor.dev/gvisor/pkg/tcpip/adapters/gonet"
	"gvisor.dev/gvisor/pkg/tcpip/header"
	"gvisor.dev/gvisor/pkg/tcpip/link/channel"
	"gvisor.dev/gvisor/pkg/tcpip/network/ipv4"
	"gvisor.dev/gvisor/pkg/tcpip/stack"
	"gvisor.dev/gvisor/pkg/tcpip/transport/tcp"
	"gvisor.dev/gvisor/pkg/tcpip/transport/udp"
)

// mockSOCKS is an independent loopback RFC 1928 server. It never contacts the
// requested Internet destination: CONNECT echoes streams and ASSOCIATE echoes
// correctly wrapped datagrams. This tests packets on both sides of a real TCP
// state machine rather than calling gateway handlers directly.
type mockSOCKS struct {
	l           net.Listener
	mu          sync.Mutex
	connections []io.Closer
	targets     chan netip.AddrPort
	wg          sync.WaitGroup
}

func newMock(t *testing.T) *mockSOCKS {
	t.Helper()
	l, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	m := &mockSOCKS{l: l, targets: make(chan netip.AddrPort, 32)}
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			m.mu.Lock()
			m.connections = append(m.connections, c)
			m.mu.Unlock()
			m.wg.Add(1)
			go func() { defer m.wg.Done(); defer c.Close(); m.serve(c) }()
		}
	}()
	t.Cleanup(func() {
		l.Close()
		m.mu.Lock()
		for _, c := range m.connections {
			c.Close()
		}
		m.mu.Unlock()
		m.wg.Wait()
	})
	return m
}

func (m *mockSOCKS) serve(c net.Conn) {
	c.SetDeadline(time.Now().Add(10 * time.Second))
	var greet [3]byte
	if _, err := io.ReadFull(c, greet[:]); err != nil || greet != [3]byte{5, 1, 0} {
		return
	}
	if _, err := c.Write([]byte{5, 0}); err != nil {
		return
	}
	var request [10]byte
	if _, err := io.ReadFull(c, request[:]); err != nil || request[0] != 5 || request[2] != 0 || request[3] != 1 {
		return
	}
	dst := netip.AddrPortFrom(netip.AddrFrom4([4]byte(request[4:8])), binary.BigEndian.Uint16(request[8:10]))
	switch request[1] {
	case 1:
		m.targets <- dst
		// BND.ADDR belongs to the proxy; it is deliberately not the destination.
		if _, err := c.Write([]byte{5, 0, 0, 1, 127, 0, 0, 1, 0, 1}); err != nil {
			return
		}
		io.Copy(c, c)
	case 3:
		u, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
		if err != nil {
			return
		}
		defer u.Close()
		m.mu.Lock()
		m.connections = append(m.connections, u)
		m.mu.Unlock()
		port := u.LocalAddr().(*net.UDPAddr).Port
		// Unspecified relay address is legal and must use the proxy's IP.
		if _, err := c.Write([]byte{5, 0, 0, 1, 0, 0, 0, 0, byte(port >> 8), byte(port)}); err != nil {
			return
		}
		done := make(chan struct{})
		go func() { defer close(done); io.Copy(io.Discard, c); u.Close() }()
		b := make([]byte, 65535)
		for {
			u.SetReadDeadline(time.Now().Add(10 * time.Second))
			n, peer, err := u.ReadFromUDP(b)
			if err != nil {
				break
			}
			if n < 10 || b[0] != 0 || b[1] != 0 || b[2] != 0 || b[3] != 1 {
				break
			}
			m.targets <- netip.AddrPortFrom(netip.AddrFrom4([4]byte(b[4:8])), binary.BigEndian.Uint16(b[8:10]))
			if _, err = u.WriteToUDP(b[:n], peer); err != nil {
				break
			}
		}
		c.Close()
		<-done
	}
}

func harness(t *testing.T, maxFlows int, idle time.Duration) (*Gateway, *stack.Stack, *mockSOCKS) {
	return harnessDropReturn(t, maxFlows, idle, nil)
}

func harnessDropReturn(t *testing.T, maxFlows int, idle time.Duration, drop func([]byte) bool) (*Gateway, *stack.Stack, *mockSOCKS) {
	t.Helper()
	m := newMock(t)
	phone := netip.MustParseAddr("10.147.20.2")
	link := channel.New(512, 1280, "")
	s := stack.New(stack.Options{NetworkProtocols: []stack.NetworkProtocolFactory{ipv4.NewProtocol}, TransportProtocols: []stack.TransportProtocolFactory{tcp.NewProtocol, udp.NewProtocol}})
	if err := s.CreateNIC(1, link); err != nil {
		t.Fatal(err)
	}
	if err := s.AddProtocolAddress(1, tcpip.ProtocolAddress{Protocol: ipv4.ProtocolNumber, AddressWithPrefix: tcpip.AddressWithPrefix{Address: tcpip.AddrFrom4(phone.As4()), PrefixLen: 24}}, stack.AddressProperties{}); err != nil {
		t.Fatal(err)
	}
	s.SetRouteTable([]tcpip.Route{{Destination: header.IPv4EmptySubnet, NIC: 1}})
	g, err := New(Config{Source: phone, Proxy: m.l.Addr().String(), MaxFlows: maxFlows, IdleTimeout: idle, WritePacket: func(b []byte) error {
		if drop != nil && drop(b) {
			return nil
		}
		p := stack.NewPacketBuffer(stack.PacketBufferOptions{Payload: buffer.MakeWithData(append([]byte(nil), b...))})
		defer p.DecRef()
		link.InjectInbound(header.IPv4ProtocolNumber, p)
		return nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			p := link.ReadContext(ctx)
			if p == nil {
				return
			}
			v := p.ToView()
			b := append([]byte(nil), v.AsSlice()...)
			v.Release()
			p.DecRef()
			_ = g.Inject(b)
		}
	}()
	t.Cleanup(func() {
		cancel()
		s.Close()
		link.Close()
		<-done
		g.Close()
		s.Wait()
	})
	return g, s, m
}

func destination(ip string, port uint16) tcpip.FullAddress {
	return tcpip.FullAddress{NIC: 1, Addr: tcpip.AddrFrom4(netip.MustParseAddr(ip).As4()), Port: port}
}

func expectTarget(t *testing.T, m *mockSOCKS, want string) {
	t.Helper()
	select {
	case got := <-m.targets:
		if got.String() != want {
			t.Fatalf("SOCKS target %s, want %s", got, want)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("missing SOCKS request")
	}
}

func TestTCPPacketsSOCKSHandshakeAndReturn(t *testing.T) {
	g, s, m := harness(t, 16, 5*time.Second)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, err := gonet.DialContextTCP(ctx, s, destination("203.0.113.7", 443), ipv4.ProtocolNumber)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	c.SetDeadline(time.Now().Add(5 * time.Second))
	payload := bytes.Repeat([]byte("packet-forwarding-"), 400)
	if _, err = c.Write(payload); err != nil {
		t.Fatal(err)
	}
	got := make([]byte, len(payload))
	if _, err = io.ReadFull(c, got); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatal("TCP payload corrupted")
	}
	expectTarget(t, m, "203.0.113.7:443")
	if g.Stats().PacketsIn == 0 || g.Stats().PacketsOut == 0 {
		t.Fatal("no bidirectional packets")
	}
}

func TestUDPAndPrivateDNSOriginalReplyAddress(t *testing.T) {
	_, s, m := harness(t, 16, 5*time.Second)
	for _, tc := range []struct {
		ip       string
		port     uint16
		upstream string
	}{{"203.0.113.8", 443, "203.0.113.8:443"}, {"10.23.0.1", 53, "1.1.1.1:53"}} {
		dst := destination(tc.ip, tc.port)
		c, err := gonet.DialUDP(s, nil, &dst, ipv4.ProtocolNumber)
		if err != nil {
			t.Fatal(err)
		}
		c.SetDeadline(time.Now().Add(5 * time.Second))
		payload := []byte("independent-udp-packet-roundtrip")
		if _, err = c.Write(payload); err != nil {
			t.Fatal(err)
		}
		got := make([]byte, 100)
		n, from, err := c.ReadFrom(got)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got[:n], payload) {
			t.Fatal("UDP payload corrupted")
		}
		if from.String() != net.JoinHostPort(tc.ip, fmtPort(tc.port)) {
			t.Fatalf("reply source %s does not preserve original destination", from)
		}
		expectTarget(t, m, tc.upstream)
		c.Close()
	}
}

func fmtPort(p uint16) string { return strconv.Itoa(int(p)) }

func TestTCPRetransmitsLostReturnPacket(t *testing.T) {
	var dropped atomic.Bool
	_, s, _ := harnessDropReturn(t, 16, 15*time.Second, func(b []byte) bool {
		if len(b) < 40 || b[9] != 6 {
			return false
		}
		ipLen := int(b[0]&15) * 4
		if len(b) < ipLen+20 {
			return false
		}
		tcpLen := int(b[ipLen+12]>>4) * 4
		return len(b) > ipLen+tcpLen && dropped.CompareAndSwap(false, true)
	})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	c, err := gonet.DialContextTCP(ctx, s, destination("203.0.113.10", 443), ipv4.ProtocolNumber)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	c.SetDeadline(time.Now().Add(10 * time.Second))
	payload := bytes.Repeat([]byte("recover-loss-"), 1000)
	if _, err = c.Write(payload); err != nil {
		t.Fatal(err)
	}
	got := make([]byte, len(payload))
	if _, err = io.ReadFull(c, got); err != nil {
		t.Fatal(err)
	}
	if !dropped.Load() || !bytes.Equal(payload, got) {
		t.Fatal("TCP failed to recover a lost raw return packet")
	}
}

func TestUDPFragmentsAcrossSmallMTU(t *testing.T) {
	var returnedFragments atomic.Int32
	_, s, m := harnessDropReturn(t, 16, 5*time.Second, func(b []byte) bool {
		if len(b) >= 20 && b[9] == 17 && binary.BigEndian.Uint16(b[6:8])&0x3fff != 0 {
			returnedFragments.Add(1)
		}
		return false
	})
	dst := destination("203.0.113.13", 443)
	c, err := gonet.DialUDP(s, nil, &dst, ipv4.ProtocolNumber)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	c.SetDeadline(time.Now().Add(5 * time.Second))
	payload := bytes.Repeat([]byte("fragmented-udp-"), 600)
	if _, err = c.Write(payload); err != nil {
		t.Fatal(err)
	}
	got := make([]byte, 16384)
	n, err := c.Read(got)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(payload, got[:n]) || returnedFragments.Load() < 2 {
		t.Fatal("UDP payload failed to roundtrip through IPv4 fragmentation/reassembly")
	}
	expectTarget(t, m, "203.0.113.13:443")
}

func TestCloseCancelsActiveTCPAndUDP(t *testing.T) {
	g, s, m := harness(t, 16, 30*time.Second)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	tcpConn, err := gonet.DialContextTCP(ctx, s, destination("203.0.113.11", 443), ipv4.ProtocolNumber)
	if err != nil {
		t.Fatal(err)
	}
	defer tcpConn.Close()
	tcpConn.SetDeadline(time.Now().Add(5 * time.Second))
	tcpConn.Write([]byte("a"))
	var b [1]byte
	if _, err = io.ReadFull(tcpConn, b[:]); err != nil {
		t.Fatal(err)
	}
	expectTarget(t, m, "203.0.113.11:443")
	dst := destination("203.0.113.12", 443)
	udpConn, err := gonet.DialUDP(s, nil, &dst, ipv4.ProtocolNumber)
	if err != nil {
		t.Fatal(err)
	}
	defer udpConn.Close()
	udpConn.SetDeadline(time.Now().Add(5 * time.Second))
	udpConn.Write([]byte("b"))
	if _, err = udpConn.Read(b[:]); err != nil {
		t.Fatal(err)
	}
	expectTarget(t, m, "203.0.113.12:443")
	if g.Stats().Active != 2 {
		t.Fatal("test needs both active transports")
	}
	done := make(chan struct{})
	go func() { g.Close(); close(done) }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("gateway shutdown blocked with active flows")
	}
	if g.Stats().Active != 0 {
		t.Fatal("active flows remain after shutdown")
	}
}

func TestTCPDNSOverridesPrivateDestination(t *testing.T) {
	_, s, m := harness(t, 16, 5*time.Second)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, err := gonet.DialContextTCP(ctx, s, destination("10.23.0.1", 53), ipv4.ProtocolNumber)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	c.SetDeadline(time.Now().Add(5 * time.Second))
	if _, err = c.Write([]byte{0, 3, 1, 2, 3}); err != nil {
		t.Fatal(err)
	}
	var b [5]byte
	if _, err = io.ReadFull(c, b[:]); err != nil {
		t.Fatal(err)
	}
	expectTarget(t, m, "1.1.1.1:53")
}

func TestIdleExpiryAndFlowCap(t *testing.T) {
	g, s, m := harness(t, 1, time.Second)
	dst := destination("203.0.113.8", 443)
	c, err := gonet.DialUDP(s, nil, &dst, ipv4.ProtocolNumber)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	c.SetDeadline(time.Now().Add(5 * time.Second))
	c.Write([]byte("one"))
	var b [10]byte
	if _, err = c.Read(b[:]); err != nil {
		t.Fatal(err)
	}
	expectTarget(t, m, "203.0.113.8:443")
	dst2 := destination("203.0.113.9", 443)
	c2, err := gonet.DialUDP(s, nil, &dst2, ipv4.ProtocolNumber)
	if err != nil {
		t.Fatal(err)
	}
	defer c2.Close()
	c2.SetDeadline(time.Now().Add(250 * time.Millisecond))
	c2.Write([]byte("two"))
	if _, err = c2.Read(b[:]); err == nil {
		t.Fatal("flow cap did not reject second flow")
	}
	if got := g.Stats(); got.Active != 1 || got.Rejected == 0 {
		t.Fatalf("unexpected cap stats: %+v", got)
	}
	deadline := time.Now().Add(4 * time.Second)
	for g.Stats().Active != 0 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if g.Stats().Active != 0 {
		t.Fatal("idle flow not reaped")
	}
}

func TestSourceAndProtocolBoundary(t *testing.T) {
	g, _, _ := harness(t, 1, time.Second)
	makePacket := func(src, dst string, protocol byte) []byte {
		b := make([]byte, 28)
		b[0] = 0x45
		binary.BigEndian.PutUint16(b[2:4], 28)
		b[8] = 64
		b[9] = protocol
		s := netip.MustParseAddr(src).As4()
		d := netip.MustParseAddr(dst).As4()
		copy(b[12:16], s[:])
		copy(b[16:20], d[:])
		binary.BigEndian.PutUint16(b[22:24], 443)
		return b
	}
	for _, b := range [][]byte{nil, {0x60}, makePacket("10.147.20.3", "1.1.1.1", 17), makePacket("10.147.20.2", "192.168.1.1", 6), makePacket("10.147.20.2", "1.1.1.1", 1)} {
		if err := g.Inject(b); err == nil {
			t.Fatal("out-of-scope packet accepted")
		}
	}
}
