// Package socks implements the subset of RFC 1928 needed by the gateway.
// Connections are always made to the configured loopback proxy; there is no
// direct-connect path and no operating-system DNS resolution.
package socks

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/netip"
	"sync"
	"time"
)

const handshakeTimeout = 10 * time.Second

type Client struct{ proxy netip.AddrPort }

func New(proxy string) (*Client, error) {
	a, err := netip.ParseAddrPort(proxy)
	if err != nil || !a.Addr().Is4() || !a.Addr().IsLoopback() || a.Port() == 0 {
		return nil, errors.New("SOCKS proxy must be a literal IPv4 loopback address with nonzero port")
	}
	return &Client{proxy: a}, nil
}

func (c *Client) handshake(ctx context.Context, command byte, target netip.AddrPort) (net.Conn, netip.AddrPort, error) {
	d := net.Dialer{Timeout: handshakeTimeout, KeepAlive: 30 * time.Second}
	conn, err := d.DialContext(ctx, "tcp4", c.proxy.String())
	if err != nil {
		return nil, netip.AddrPort{}, err
	}
	ok := false
	defer func() {
		if !ok {
			conn.Close()
		}
	}()
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	defer stop()
	if err = conn.SetDeadline(time.Now().Add(handshakeTimeout)); err != nil {
		return nil, netip.AddrPort{}, err
	}
	if _, err = conn.Write([]byte{5, 1, 0}); err != nil {
		return nil, netip.AddrPort{}, err
	}
	var greeting [2]byte
	if _, err = io.ReadFull(conn, greeting[:]); err != nil {
		return nil, netip.AddrPort{}, err
	}
	if greeting != [2]byte{5, 0} {
		return nil, netip.AddrPort{}, errors.New("SOCKS server did not accept no-authentication method")
	}
	address, err := encodeAddress(target)
	if err != nil {
		return nil, netip.AddrPort{}, err
	}
	request := append([]byte{5, command, 0}, address...)
	if _, err = conn.Write(request); err != nil {
		return nil, netip.AddrPort{}, err
	}
	var response [3]byte
	if _, err = io.ReadFull(conn, response[:]); err != nil {
		return nil, netip.AddrPort{}, err
	}
	if response[0] != 5 || response[2] != 0 {
		return nil, netip.AddrPort{}, errors.New("invalid SOCKS response")
	}
	if response[1] != 0 {
		return nil, netip.AddrPort{}, fmt.Errorf("SOCKS request rejected, reply=%d", response[1])
	}
	bound, err := readAddress(conn)
	if err != nil {
		return nil, netip.AddrPort{}, err
	}
	if err = conn.SetDeadline(time.Time{}); err != nil {
		return nil, netip.AddrPort{}, err
	}
	if ctx.Err() != nil {
		return nil, netip.AddrPort{}, ctx.Err()
	}
	ok = true
	return conn, bound, nil
}

func (c *Client) DialTCP(ctx context.Context, target netip.AddrPort) (net.Conn, error) {
	conn, _, err := c.handshake(ctx, 1, target)
	return conn, err
}

type Association struct {
	control net.Conn
	udp     *net.UDPConn
	once    sync.Once
}

func (c *Client) Associate(ctx context.Context) (*Association, error) {
	control, relay, err := c.handshake(ctx, 3, netip.AddrPortFrom(netip.IPv4Unspecified(), 0))
	if err != nil {
		return nil, err
	}
	if relay.Addr().IsUnspecified() {
		relay = netip.AddrPortFrom(c.proxy.Addr(), relay.Port())
	}
	// A remote relay or hostname would exceed this program's loopback-only
	// upstream boundary. Mihomo on the same machine returns a local relay.
	if !relay.Addr().Is4() || !relay.Addr().IsLoopback() || relay.Port() == 0 {
		control.Close()
		return nil, fmt.Errorf("SOCKS UDP relay is not IPv4 loopback: %s", relay)
	}
	udp, err := net.DialUDP("udp4", nil, net.UDPAddrFromAddrPort(relay))
	if err != nil {
		control.Close()
		return nil, err
	}
	a := &Association{control: control, udp: udp}
	go func() { _, _ = io.Copy(io.Discard, control); _ = a.Close() }()
	return a, nil
}

func (a *Association) Close() error {
	a.once.Do(func() { a.control.Close(); a.udp.Close() })
	return nil
}

func (a *Association) WriteTo(payload []byte, target netip.AddrPort) (int, error) {
	if len(payload) > 65507-10 {
		return 0, errors.New("SOCKS UDP payload too large")
	}
	address, err := encodeAddress(target)
	if err != nil {
		return 0, err
	}
	packet := make([]byte, 3+len(address)+len(payload))
	copy(packet[3:], address)
	copy(packet[3+len(address):], payload)
	_, err = a.udp.Write(packet)
	if err != nil {
		return 0, err
	}
	return len(payload), nil
}

func (a *Association) ReadFrom(buf []byte) (int, netip.AddrPort, error) {
	packet := make([]byte, 65535)
	n, err := a.udp.Read(packet)
	if err != nil {
		return 0, netip.AddrPort{}, err
	}
	if n < 10 || packet[0] != 0 || packet[1] != 0 || packet[2] != 0 || packet[3] != 1 {
		return 0, netip.AddrPort{}, errors.New("unsupported or malformed SOCKS UDP packet (IPv4 and FRAG=0 required)")
	}
	source := netip.AddrPortFrom(netip.AddrFrom4([4]byte(packet[4:8])), binary.BigEndian.Uint16(packet[8:10]))
	if n-10 > len(buf) {
		return 0, netip.AddrPort{}, io.ErrShortBuffer
	}
	return copy(buf, packet[10:n]), source, nil
}

func encodeAddress(a netip.AddrPort) ([]byte, error) {
	if !a.Addr().Is4() {
		return nil, errors.New("only IPv4 SOCKS addresses are supported")
	}
	b := make([]byte, 7)
	b[0] = 1
	ip := a.Addr().As4()
	copy(b[1:5], ip[:])
	binary.BigEndian.PutUint16(b[5:7], a.Port())
	return b, nil
}

func readAddress(r io.Reader) (netip.AddrPort, error) {
	var kind [1]byte
	if _, err := io.ReadFull(r, kind[:]); err != nil {
		return netip.AddrPort{}, err
	}
	switch kind[0] {
	case 1:
		var b [6]byte
		if _, err := io.ReadFull(r, b[:]); err != nil {
			return netip.AddrPort{}, err
		}
		return netip.AddrPortFrom(netip.AddrFrom4([4]byte(b[:4])), binary.BigEndian.Uint16(b[4:])), nil
	case 4:
		var b [18]byte
		if _, err := io.ReadFull(r, b[:]); err != nil {
			return netip.AddrPort{}, err
		}
		return netip.AddrPortFrom(netip.AddrFrom16([16]byte(b[:16])), binary.BigEndian.Uint16(b[16:])), nil
	default:
		return netip.AddrPort{}, errors.New("SOCKS reply contains an unsupported address type; DNS lookup is deliberately disabled")
	}
}
