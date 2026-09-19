package settings

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/netip"
	"os"
	"path/filepath"
)

type Config struct {
	SourceIP           string `json:"source_ip"`
	WindowsIP          string `json:"windows_ip"`
	InterfaceIndex     uint32 `json:"interface_index"`
	Proxy              string `json:"proxy"`
	DNS                string `json:"dns"`
	MTU                uint32 `json:"mtu"`
	MaxFlows           int    `json:"max_flows"`
	IdleTimeoutSeconds int    `json:"idle_timeout_seconds"`
	WinDivertDLL       string `json:"windivert_dll"`
	File               string `json:"-"`
}

func Load(path string) (Config, error) {
	var c Config
	absolute, err := filepath.Abs(path)
	if err != nil {
		return c, err
	}
	f, err := os.Open(absolute)
	if err != nil {
		return c, err
	}
	defer f.Close()
	d := json.NewDecoder(io.LimitReader(f, 65536))
	d.DisallowUnknownFields()
	if err = d.Decode(&c); err != nil {
		return c, err
	}
	if err = d.Decode(new(any)); err != io.EOF {
		return c, errors.New("config must contain exactly one JSON object")
	}
	c.File = absolute
	if err = c.Validate(); err != nil {
		return c, err
	}
	if !filepath.IsAbs(c.WinDivertDLL) {
		c.WinDivertDLL = filepath.Join(filepath.Dir(absolute), c.WinDivertDLL)
	}
	return c, nil
}

func (c Config) Validate() error {
	s, err := netip.ParseAddr(c.SourceIP)
	if err != nil || !s.Is4() || !s.IsPrivate() {
		return errors.New("source_ip must be a private IPv4 address")
	}
	w, err := netip.ParseAddr(c.WindowsIP)
	if err != nil || !w.Is4() || !w.IsPrivate() || w == s {
		return errors.New("windows_ip must be a distinct private IPv4 address")
	}
	if !netip.PrefixFrom(w, 24).Contains(s) {
		return errors.New("prototype requires phone and Windows on the same /24")
	}
	if c.InterfaceIndex == 0 {
		return errors.New("interface_index must be positive")
	}
	if c.Proxy != "127.0.0.1:7897" {
		return errors.New("this prototype only permits the existing local proxy 127.0.0.1:7897")
	}
	dns, err := netip.ParseAddrPort(c.DNS)
	if err != nil || !dns.Addr().Is4() || !dns.Addr().IsGlobalUnicast() || dns.Addr().IsPrivate() || dns.Port() != 53 {
		return errors.New("dns must be an explicit public IPv4 address on port 53")
	}
	if c.MTU < 1280 || c.MTU > 1500 {
		return errors.New("mtu must be 1280..1500")
	}
	if c.MaxFlows < 1 || c.MaxFlows > 1024 {
		return errors.New("max_flows must be 1..1024")
	}
	if c.IdleTimeoutSeconds < 15 || c.IdleTimeoutSeconds > 600 {
		return errors.New("idle timeout must be 15..600 seconds")
	}
	if filepath.Base(c.WinDivertDLL) != "WinDivert.dll" {
		return fmt.Errorf("invalid WinDivert DLL filename")
	}
	return nil
}
