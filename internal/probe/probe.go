// Package probe checks the existing SOCKS listener without loading a driver.
package probe

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"time"

	"github.com/Luase233/zerotier-mihomo-gateway/internal/socks"
	"golang.org/x/net/dns/dnsmessage"
)

type Result struct {
	TCPDNS   bool   `json:"tcp_dns"`
	UDPDNS   bool   `json:"udp_dns"`
	HTTPS    bool   `json:"https"`
	EgressIP string `json:"egress_ip"`
}

func Run(parent context.Context, proxy, dns string) (Result, error) {
	var result Result
	c, err := socks.New(proxy)
	if err != nil {
		return result, err
	}
	target, err := netip.ParseAddrPort(dns)
	if err != nil {
		return result, err
	}
	_, err = resolve(parent, c, target, "example.com.", true)
	if err != nil {
		return result, fmt.Errorf("SOCKS TCP DNS: %w", err)
	}
	result.TCPDNS = true
	ip, err := resolve(parent, c, target, "api.ipify.org.", false)
	if err != nil {
		return result, fmt.Errorf("SOCKS UDP DNS: %w", err)
	}
	result.UDPDNS = true
	transport := &http.Transport{DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		return c.DialTCP(ctx, netip.AddrPortFrom(ip, 443))
	}, TLSHandshakeTimeout: 8 * time.Second, DisableKeepAlives: true}
	defer transport.CloseIdleConnections()
	client := http.Client{Transport: transport, Timeout: 12 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	req, err := http.NewRequestWithContext(parent, "GET", "https://api.ipify.org", nil)
	if err != nil {
		return result, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return result, fmt.Errorf("HTTPS through SOCKS: %w", err)
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 128))
	if err != nil {
		return result, err
	}
	if resp.StatusCode != 200 {
		return result, fmt.Errorf("egress endpoint returned %s", resp.Status)
	}
	egress, err := netip.ParseAddr(strings.TrimSpace(string(b)))
	if err != nil {
		return result, fmt.Errorf("invalid egress IP response")
	}
	result.HTTPS = true
	result.EgressIP = egress.String()
	return result, nil
}

func resolve(parent context.Context, c *socks.Client, target netip.AddrPort, name string, tcp bool) (netip.Addr, error) {
	ctx, cancel := context.WithTimeout(parent, 8*time.Second)
	defer cancel()
	var id [2]byte
	if _, err := rand.Read(id[:]); err != nil {
		return netip.Addr{}, err
	}
	n, err := dnsmessage.NewName(name)
	if err != nil {
		return netip.Addr{}, err
	}
	m := dnsmessage.Message{Header: dnsmessage.Header{ID: binary.BigEndian.Uint16(id[:]), RecursionDesired: true}, Questions: []dnsmessage.Question{{Name: n, Type: dnsmessage.TypeA, Class: dnsmessage.ClassINET}}}
	query, err := m.Pack()
	if err != nil {
		return netip.Addr{}, err
	}
	var response []byte
	if tcp {
		conn, e := c.DialTCP(ctx, target)
		if e != nil {
			return netip.Addr{}, e
		}
		defer conn.Close()
		conn.SetDeadline(time.Now().Add(7 * time.Second))
		framed := make([]byte, len(query)+2)
		binary.BigEndian.PutUint16(framed, uint16(len(query)))
		copy(framed[2:], query)
		if _, e = conn.Write(framed); e != nil {
			return netip.Addr{}, e
		}
		var size [2]byte
		if _, e = io.ReadFull(conn, size[:]); e != nil {
			return netip.Addr{}, e
		}
		response = make([]byte, binary.BigEndian.Uint16(size[:]))
		if _, e = io.ReadFull(conn, response); e != nil {
			return netip.Addr{}, e
		}
	} else {
		a, e := c.Associate(ctx)
		if e != nil {
			return netip.Addr{}, e
		}
		defer a.Close()
		stop := context.AfterFunc(ctx, func() { a.Close() })
		defer stop()
		if _, e = a.WriteTo(query, target); e != nil {
			return netip.Addr{}, e
		}
		buf := make([]byte, 65535)
		for {
			count, from, e := a.ReadFrom(buf)
			if e != nil {
				return netip.Addr{}, e
			}
			if from == target {
				response = append([]byte(nil), buf[:count]...)
				break
			}
		}
	}
	var answer dnsmessage.Message
	if err = answer.Unpack(response); err != nil {
		return netip.Addr{}, err
	}
	if answer.ID != m.ID || !answer.Response || answer.RCode != dnsmessage.RCodeSuccess || answer.Truncated {
		return netip.Addr{}, fmt.Errorf("invalid/unsuccessful DNS response")
	}
	for _, a := range answer.Answers {
		if v, ok := a.Body.(*dnsmessage.AResource); ok {
			return netip.AddrFrom4(v.A), nil
		}
	}
	return netip.Addr{}, fmt.Errorf("no IPv4 answer for %s", name)
}
