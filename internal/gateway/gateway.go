// Package gateway is a bounded IPv4 TCP/UDP packet-to-SOCKS gateway.
// Its channel endpoint is an in-process object, not a Windows TUN adapter.
package gateway

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/netip"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Luase233/zerotier-mihomo-gateway/internal/socks"
	"github.com/xjasonlyu/tun2socks/v2/core"
	"github.com/xjasonlyu/tun2socks/v2/core/adapter"
	"gvisor.dev/gvisor/pkg/buffer"
	"gvisor.dev/gvisor/pkg/tcpip/header"
	"gvisor.dev/gvisor/pkg/tcpip/link/channel"
	"gvisor.dev/gvisor/pkg/tcpip/stack"
)

var ErrPacketRejected = errors.New("packet outside gateway IPv4 TCP/UDP allowlist")

type Config struct {
	Source      netip.Addr
	Proxy       string
	MTU         uint32
	DNS         string
	MaxFlows    int
	IdleTimeout time.Duration
	WritePacket func([]byte) error
	Log         *slog.Logger
}

type Snapshot struct {
	PacketsIn  uint64 `json:"packets_in"`
	PacketsOut uint64 `json:"packets_out"`
	Rejected   uint64 `json:"rejected"`
	Errors     uint64 `json:"errors"`
	TCPFlows   uint64 `json:"tcp_flows"`
	UDPFlows   uint64 `json:"udp_flows"`
	Active     int    `json:"active"`
}

type Gateway struct {
	cfg                                             Config
	dns                                             netip.AddrPort
	proxy                                           *socks.Client
	ep                                              *channel.Endpoint
	stack                                           *stack.Stack
	ctx                                             context.Context
	cancel                                          context.CancelFunc
	mu                                              sync.Mutex
	closed                                          bool
	flows                                           map[*flow]struct{}
	wg                                              sync.WaitGroup
	closeOnce                                       sync.Once
	in, out, rejected, failures, tcpFlows, udpFlows atomic.Uint64
}

type flow struct {
	mu      sync.Mutex
	closed  bool
	closers []io.Closer
	last    atomic.Int64
	ctx     context.Context
	cancel  context.CancelFunc
}

func New(cfg Config) (*Gateway, error) {
	if !cfg.Source.Is4() || !cfg.Source.IsGlobalUnicast() {
		return nil, errors.New("source must be an IPv4 unicast address")
	}
	if cfg.WritePacket == nil {
		return nil, errors.New("WritePacket callback is required")
	}
	if cfg.MTU == 0 {
		cfg.MTU = 1280
	}
	if cfg.MTU < 576 || cfg.MTU > 9000 {
		return nil, errors.New("MTU must be between 576 and 9000")
	}
	if cfg.MaxFlows == 0 {
		cfg.MaxFlows = 256
	}
	if cfg.MaxFlows < 1 || cfg.MaxFlows > 4096 {
		return nil, errors.New("MaxFlows must be between 1 and 4096")
	}
	if cfg.IdleTimeout == 0 {
		cfg.IdleTimeout = 120 * time.Second
	}
	if cfg.IdleTimeout < time.Second {
		return nil, errors.New("IdleTimeout must be at least one second")
	}
	if cfg.DNS == "" {
		cfg.DNS = "1.1.1.1:53"
	}
	dns, err := netip.ParseAddrPort(cfg.DNS)
	if err != nil || !publicIPv4(dns.Addr()) || dns.Port() == 0 {
		return nil, errors.New("DNS must be a public IPv4 address and port")
	}
	if cfg.Log == nil {
		cfg.Log = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	proxy, err := socks.New(cfg.Proxy)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	g := &Gateway{cfg: cfg, dns: dns, proxy: proxy, ctx: ctx, cancel: cancel, flows: make(map[*flow]struct{})}
	g.ep = channel.New(512, cfg.MTU, "")
	// WFP/WinDivert may deliver transport checksums pending hardware offload.
	// IPv4 size/source/protocol checks are still performed before injection.
	g.ep.LinkEPCapabilities = stack.CapabilityRXChecksumOffload
	g.stack, err = core.CreateStack(&core.Config{LinkEndpoint: g.ep, TransportHandler: g, ICMPHandler: discardICMP{}})
	if err != nil {
		g.ep.Close()
		cancel()
		return nil, fmt.Errorf("create netstack: %w", err)
	}
	g.wg.Add(2)
	go g.output()
	go g.reap()
	return g, nil
}

type discardICMP struct{}

func (discardICMP) HandlePacket(adapter.Packet) bool { return true }

func publicIPv4(ip netip.Addr) bool {
	return ip.Is4() && ip.IsGlobalUnicast() && !ip.IsPrivate() && !ip.IsLoopback() && !ip.IsLinkLocalUnicast() && !netip.MustParsePrefix("100.64.0.0/10").Contains(ip) && ip.As4()[0] != 0 && ip.As4()[0] < 224
}

// Inject accepts an owned copy of a validated raw IPv4 packet. Rejection means
// DROP at the capture layer; it never authorizes direct Internet forwarding.
func (g *Gateway) Inject(packet []byte) error {
	if len(packet) < 20 || packet[0]>>4 != 4 {
		g.rejected.Add(1)
		return ErrPacketRejected
	}
	ihl := int(packet[0]&15) * 4
	length := int(binary.BigEndian.Uint16(packet[2:4]))
	source := netip.AddrFrom4([4]byte(packet[12:16]))
	target := netip.AddrFrom4([4]byte(packet[16:20]))
	if ihl < 20 || ihl > len(packet) || length < ihl || length > len(packet) || source != g.cfg.Source || (packet[9] != 6 && packet[9] != 17) {
		g.rejected.Add(1)
		return ErrPacketRejected
	}
	// A private carrier/router DNS address is allowed only for DNS, which is
	// replaced with the configured public resolver before SOCKS negotiation.
	// Private fragmented destinations are rejected rather than guessing ports.
	if !publicIPv4(target) {
		fragment := binary.BigEndian.Uint16(packet[6:8]) & 0x3fff
		if fragment != 0 || length < ihl+4 || binary.BigEndian.Uint16(packet[ihl+2:ihl+4]) != 53 {
			g.rejected.Add(1)
			return ErrPacketRejected
		}
	}
	g.mu.Lock()
	closed := g.closed
	g.mu.Unlock()
	if closed {
		return net.ErrClosed
	}
	p := stack.NewPacketBuffer(stack.PacketBufferOptions{Payload: buffer.MakeWithData(append([]byte(nil), packet[:length]...))})
	defer p.DecRef()
	g.in.Add(1)
	g.ep.InjectInbound(header.IPv4ProtocolNumber, p)
	return nil
}

func (g *Gateway) output() {
	defer g.wg.Done()
	for {
		p := g.ep.ReadContext(g.ctx)
		if p == nil {
			return
		}
		v := p.ToView()
		b := append([]byte(nil), v.AsSlice()...)
		v.Release()
		p.DecRef()
		if len(b) < 20 || b[0]>>4 != 4 || netip.AddrFrom4([4]byte(b[16:20])) != g.cfg.Source {
			g.rejected.Add(1)
			continue
		}
		if err := g.cfg.WritePacket(b); err != nil {
			g.failures.Add(1)
			if count := g.failures.Load(); count == 1 || count%1000 == 0 {
				g.cfg.Log.Error("return packet failed", "error", err, "total_errors", count)
			}
			continue
		}
		g.out.Add(1)
	}
}

func (g *Gateway) start(c net.Conn) *flow {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.closed || len(g.flows) >= g.cfg.MaxFlows {
		g.rejected.Add(1)
		c.Close()
		return nil
	}
	ctx, cancel := context.WithCancel(g.ctx)
	f := &flow{ctx: ctx, cancel: cancel, closers: []io.Closer{c}}
	f.touch()
	g.flows[f] = struct{}{}
	g.wg.Add(1)
	return f
}

func (f *flow) touch() { f.last.Store(time.Now().UnixNano()) }
func (f *flow) add(c io.Closer) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		c.Close()
		return false
	}
	f.closers = append(f.closers, c)
	return true
}
func (f *flow) close() {
	f.mu.Lock()
	if f.closed {
		f.mu.Unlock()
		return
	}
	f.closed = true
	closers := append([]io.Closer(nil), f.closers...)
	f.mu.Unlock()
	f.cancel()
	for _, c := range closers {
		c.Close()
	}
}
func (g *Gateway) finish(f *flow) {
	f.close()
	g.mu.Lock()
	delete(g.flows, f)
	g.mu.Unlock()
	g.wg.Done()
}

func targetOf(id stack.TransportEndpointID) (netip.AddrPort, error) {
	ip, ok := netip.AddrFromSlice(id.LocalAddress.AsSlice())
	if !ok || !publicIPv4(ip) || id.LocalPort == 0 {
		return netip.AddrPort{}, ErrPacketRejected
	}
	return netip.AddrPortFrom(ip, id.LocalPort), nil
}
func (g *Gateway) destination(id stack.TransportEndpointID) (netip.AddrPort, error) {
	if id.LocalPort == 53 {
		return g.dns, nil
	}
	target, err := targetOf(id)
	return target, err
}

func (g *Gateway) HandleTCP(c adapter.TCPConn) {
	f := g.start(c)
	if f == nil {
		return
	}
	g.tcpFlows.Add(1)
	go func() {
		defer g.finish(f)
		target, err := g.destination(c.ID())
		if err != nil {
			g.failures.Add(1)
			return
		}
		up, err := g.proxy.DialTCP(f.ctx, target)
		if err != nil {
			g.failures.Add(1)
			g.cfg.Log.Warn("TCP SOCKS connect failed", "target", target, "error", err)
			return
		}
		if !f.add(up) {
			return
		}
		g.cfg.Log.Debug("TCP flow opened", "target", target)
		done := make(chan error, 2)
		go func() { done <- copyStream(up, c, f) }()
		go func() { done <- copyStream(c, up, f) }()
		// Keep the reverse direction alive after a clean half-close.
		if err := <-done; err != nil {
			f.close()
		}
		<-done
	}()
}

func copyStream(dst, src net.Conn, f *flow) error {
	buf := make([]byte, 32*1024)
	for {
		n, err := src.Read(buf)
		if n > 0 {
			f.touch()
			written := 0
			for written < n {
				m, we := dst.Write(buf[written:n])
				written += m
				if we != nil {
					return we
				}
				if m == 0 {
					return io.ErrShortWrite
				}
				f.touch()
			}
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				if cw, ok := dst.(interface{ CloseWrite() error }); ok {
					_ = cw.CloseWrite()
				}
				return nil
			}
			return err
		}
	}
}

func (g *Gateway) HandleUDP(c adapter.UDPConn) {
	f := g.start(c)
	if f == nil {
		return
	}
	g.udpFlows.Add(1)
	go func() {
		defer g.finish(f)
		target, err := g.destination(c.ID())
		if err != nil {
			g.failures.Add(1)
			return
		}
		up, err := g.proxy.Associate(f.ctx)
		if err != nil {
			g.failures.Add(1)
			g.cfg.Log.Warn("UDP SOCKS associate failed", "target", target, "error", err)
			return
		}
		if !f.add(up) {
			return
		}
		g.cfg.Log.Debug("UDP flow opened", "target", target)
		done := make(chan struct{}, 2)
		go func() {
			defer func() { done <- struct{}{} }()
			b := make([]byte, 65535)
			for {
				n, e := c.Read(b)
				if e != nil {
					return
				}
				f.touch()
				if _, e = up.WriteTo(b[:n], target); e != nil {
					return
				}
			}
		}()
		go func() {
			defer func() { done <- struct{}{} }()
			b := make([]byte, 65535)
			for {
				n, source, e := up.ReadFrom(b)
				if e != nil {
					return
				}
				if source != target {
					g.rejected.Add(1)
					continue
				}
				f.touch()
				// c retains the original destination, including before DNS override.
				if _, e = c.Write(b[:n]); e != nil {
					return
				}
			}
		}()
		<-done
		f.close()
		<-done
	}()
}

func (g *Gateway) reap() {
	defer g.wg.Done()
	timer := time.NewTicker(time.Second)
	defer timer.Stop()
	for {
		select {
		case <-g.ctx.Done():
			return
		case now := <-timer.C:
			var expired []*flow
			g.mu.Lock()
			for f := range g.flows {
				if now.Sub(time.Unix(0, f.last.Load())) > g.cfg.IdleTimeout {
					expired = append(expired, f)
				}
			}
			g.mu.Unlock()
			for _, f := range expired {
				f.close()
			}
		}
	}
}

func (g *Gateway) Stats() Snapshot {
	g.mu.Lock()
	active := len(g.flows)
	g.mu.Unlock()
	return Snapshot{g.in.Load(), g.out.Load(), g.rejected.Load(), g.failures.Load(), g.tcpFlows.Load(), g.udpFlows.Load(), active}
}

func (g *Gateway) Close() error {
	g.closeOnce.Do(func() {
		g.mu.Lock()
		g.closed = true
		var flows []*flow
		for f := range g.flows {
			flows = append(flows, f)
		}
		g.mu.Unlock()
		g.cancel()
		for _, f := range flows {
			f.close()
		}
		g.stack.Close()
		g.ep.Close()
		g.wg.Wait()
		g.stack.Wait()
	})
	return nil
}
