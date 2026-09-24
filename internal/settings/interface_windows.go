//go:build windows

package settings

import (
	"fmt"
	"net"
	"net/netip"
	"strings"
)

// ResolveInterface uses the configured private addresses as stable identity.
// Windows can assign a different interface index to the same ZeroTier adapter
// after a reboot; the index is only a hint and is never used without checking
// the actual interface, address, subnet, and operational state.
func ResolveInterface(c *Config) error {
	interfaces, err := net.Interfaces()
	if err != nil {
		return fmt.Errorf("enumerate network interfaces: %w", err)
	}
	var matches []uint32
	for _, iface := range interfaces {
		if iface.Flags&net.FlagUp == 0 || !strings.HasPrefix(iface.Name, "ZeroTier One [") {
			continue
		}
		addresses, err := iface.Addrs()
		if err != nil {
			return fmt.Errorf("read ZeroTier addresses: %w", err)
		}
		for _, address := range addresses {
			prefix, err := netip.ParsePrefix(address.String())
			if err != nil || !prefix.Addr().Is4() || prefix.Addr().String() != c.WindowsIP {
				continue
			}
			phone, _ := netip.ParseAddr(c.SourceIP)
			if prefix.Contains(phone) {
				matches = append(matches, uint32(iface.Index))
				break
			}
		}
	}
	index, err := uniqueInterface(matches)
	if err != nil {
		return err
	}
	c.InterfaceIndex = index
	return nil
}
