//go:build windows

package preflight

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/Luase233/zerobridge-mihomo/internal/settings"
)

type State struct {
	Admin bool `json:"admin"`
	NAT   []struct {
		Name   string
		Prefix string
	} `json:"nat"`
	Forwarding      bool   `json:"forwarding"`
	DriverSignature string `json:"driver_signature"`
}

// Check executes only read-only Windows queries. It never loads the DLL.
func Check(c settings.Config, active bool) (State, error) {
	var state State
	iface, err := net.InterfaceByIndex(int(c.InterfaceIndex))
	if err != nil {
		return state, err
	}
	if iface.Flags&net.FlagUp == 0 {
		return state, fmt.Errorf("configured interface is down")
	}
	addresses, err := iface.Addrs()
	if err != nil {
		return state, err
	}
	found := false
	for _, a := range addresses {
		ip, network, e := net.ParseCIDR(a.String())
		if e == nil && ip.String() == c.WindowsIP && network.Contains(net.ParseIP(c.SourceIP)) {
			found = true
		}
	}
	if !found {
		return state, fmt.Errorf("interface %d does not have expected Windows/phone subnet", c.InterfaceIndex)
	}
	manifestPath := filepath.Join(filepath.Dir(c.WinDivertDLL), "manifest.json")
	b, err := os.ReadFile(manifestPath)
	if err != nil {
		return state, err
	}
	b = []byte(strings.TrimPrefix(string(b), "\ufeff"))
	var manifest struct {
		DLL    string `json:"dll_sha256"`
		Driver string `json:"driver_sha256"`
	}
	if err = json.Unmarshal(b, &manifest); err != nil {
		return state, err
	}
	driver := filepath.Join(filepath.Dir(c.WinDivertDLL), "WinDivert64.sys")
	for file, expected := range map[string]string{c.WinDivertDLL: manifest.DLL, driver: manifest.Driver} {
		content, e := os.ReadFile(file)
		if e != nil {
			return state, e
		}
		sum := sha256.Sum256(content)
		if len(expected) != 64 || !strings.EqualFold(hex.EncodeToString(sum[:]), expected) {
			return state, fmt.Errorf("runtime hash mismatch: %s", filepath.Base(file))
		}
	}
	// Single-quoted PowerShell literal, with embedded apostrophes escaped.
	quotedDriver := "'" + strings.ReplaceAll(driver, "'", "''") + "'"
	script := fmt.Sprintf(`$ErrorActionPreference='Stop'; $p=[Security.Principal.WindowsPrincipal]::new([Security.Principal.WindowsIdentity]::GetCurrent()); $i=Get-NetIPInterface -AddressFamily IPv4 -InterfaceIndex %d; [pscustomobject]@{admin=$p.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator);nat=@(Get-NetNat | ForEach-Object {[pscustomobject]@{Name=$_.Name;Prefix=[string]$_.InternalIPInterfaceAddressPrefix}});forwarding=([string]$i.Forwarding -eq 'Enabled');driver_signature=[string](Get-AuthenticodeSignature -LiteralPath %s).Status} | ConvertTo-Json -Compress -Depth 4`, c.InterfaceIndex, quotedDriver)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, filepath.Join(os.Getenv("SystemRoot"), "System32", "WindowsPowerShell", "v1.0", "powershell.exe"), "-NoProfile", "-NonInteractive", "-Command", script)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	// Go inherits Codex's PowerShell 7 module path. Windows PowerShell 5.1
	// cannot load those assemblies; use its own native modules for this query.
	for _, entry := range os.Environ() {
		if !strings.HasPrefix(strings.ToUpper(entry), "PSMODULEPATH=") {
			cmd.Env = append(cmd.Env, entry)
		}
	}
	cmd.Env = append(cmd.Env, "PSModulePath="+filepath.Join(os.Getenv("SystemRoot"), "System32", "WindowsPowerShell", "v1.0", "Modules"))
	out, err := cmd.Output()
	if err != nil {
		if exit, ok := err.(*exec.ExitError); ok {
			return state, fmt.Errorf("read-only Windows preflight failed: %w: %s", err, strings.TrimSpace(string(exit.Stderr)))
		}
		return state, fmt.Errorf("read-only Windows preflight failed: %w", err)
	}
	if err = json.Unmarshal(out, &state); err != nil {
		return state, fmt.Errorf("Windows preflight JSON: %w", err)
	}
	if state.DriverSignature != "Valid" {
		return state, fmt.Errorf("WinDivert driver Authenticode status is %s", state.DriverSignature)
	}
	if !state.Forwarding {
		return state, fmt.Errorf("ZeroTier forwarding is not enabled; no changes were made")
	}
	if active {
		if !state.Admin {
			return state, fmt.Errorf("administrator privileges required; no changes were made")
		}
		// Forward-layer WinDivert/NAT interaction is unsupported. Conservative
		// first release refuses every explicit WinNAT, never removes any itself.
		if len(state.NAT) > 0 {
			return state, fmt.Errorf("active gateway refuses existing Windows NAT objects; use reviewed scripts, never mix forward interception with NAT")
		}
	}
	return state, nil
}
