//go:build windows

package main

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/netip"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/Luase233/zerotier-mihomo-gateway/internal/divert"
	"github.com/Luase233/zerotier-mihomo-gateway/internal/gateway"
	"github.com/Luase233/zerotier-mihomo-gateway/internal/preflight"
	"github.com/Luase233/zerotier-mihomo-gateway/internal/probe"
	"github.com/Luase233/zerotier-mihomo-gateway/internal/settings"
	"golang.org/x/sys/windows"
)

const version = "0.1.0-experimental"

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	if err := run(log); err != nil {
		log.Error("stopped", "error", err)
		os.Exit(1)
	}
}

func run(log *slog.Logger) error {
	if len(os.Args) < 2 {
		return errors.New("usage: zt-gateway check|probe|capture|guard|proxy -config gateway.json")
	}
	mode := os.Args[1]
	if mode != "check" && mode != "probe" && mode != "capture" && mode != "guard" && mode != "proxy" {
		return errors.New("unknown mode")
	}
	fs := flag.NewFlagSet(mode, flag.ContinueOnError)
	configPath := fs.String("config", "gateway.json", "configuration path")
	duration := fs.Duration("duration", 0, "capture/proxy duration; zero runs until stopped")
	readyFile := fs.String("ready-file", "", "atomic readiness JSON path")
	guardPID := fs.Uint("guard-pid", 0, "independent guard process required by proxy mode")
	guardReadyFile := fs.String("guard-ready-file", "", "readiness proof from the independent guard")
	if err := fs.Parse(os.Args[2:]); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("unexpected arguments")
	}
	if *duration < 0 {
		return errors.New("duration cannot be negative")
	}
	if (mode == "guard" || mode == "proxy") && *duration != 0 {
		return errors.New("guard and proxy must not expire automatically; stop with the reviewed script after phone default route is OFF")
	}
	c, err := settings.Load(*configPath)
	if err != nil {
		return err
	}
	state, err := preflight.Check(c, mode == "guard" || mode == "proxy")
	if err != nil {
		return err
	}
	log.Info("preflight", "mode", mode, "version", version, "source", c.SourceIP, "return_interface", c.InterfaceIndex, "nat_objects", len(state.NAT), "admin", state.Admin)
	if mode == "check" || mode == "probe" {
		ctx, cancel := context.WithTimeout(context.Background(), 35*time.Second)
		defer cancel()
		p, e := probe.Run(ctx, c.Proxy, c.DNS)
		json.NewEncoder(os.Stdout).Encode(struct {
			Version string          `json:"version"`
			System  preflight.State `json:"system"`
			Proxy   probe.Result    `json:"proxy"`
		}{version, state, p})
		return e
	}
	if !state.Admin {
		return errors.New("capture also requires administrator privileges")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if *duration > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, *duration)
		defer cancel()
	}
	if mode == "guard" {
		h, e := divert.OpenGuard(c.WinDivertDLL, c.SourceIP)
		if e != nil {
			return e
		}
		defer h.Close()
		if e = writeReady(*readyFile, mode, c); e != nil {
			return e
		}
		log.Info("guard_ready", "filter", "ip and ip.SrcAddr == "+c.SourceIP, "priority", -1000)
		<-ctx.Done()
		return nil
	}
	var guard windows.Handle
	if mode == "proxy" {
		if *guardPID == 0 || uint64(*guardPID) > uint64(^uint32(0)) {
			return errors.New("proxy requires a live independent -guard-pid")
		}
		guard, err = verifyGuard(uint32(*guardPID), *guardReadyFile, c)
		if err != nil {
			return err
		}
		defer windows.CloseHandle(guard)
	}
	h, err := divert.Open(c.WinDivertDLL, c.SourceIP, mode == "capture")
	if err != nil {
		return err
	}
	defer h.Close()
	var g *gateway.Gateway
	if mode == "proxy" {
		g, err = gateway.New(gateway.Config{Source: netip.MustParseAddr(c.SourceIP), Proxy: c.Proxy, DNS: c.DNS, MTU: c.MTU, MaxFlows: c.MaxFlows, IdleTimeout: time.Duration(c.IdleTimeoutSeconds) * time.Second, Log: log, WritePacket: func(packet []byte) error { return h.Send(packet, c.InterfaceIndex) }})
		if err != nil {
			return err
		}
		defer g.Close()
	}
	if err = writeReady(*readyFile, mode, c); err != nil {
		return err
	}
	log.Info("ready", "mode", mode, "ipv4_only", true, "dns_override", c.DNS)
	var packets, bytes, rejected atomic.Uint64
	var dropAll atomic.Bool
	errCh := make(chan error, 1)
	go func() {
		buf := make([]byte, divert.MaxPacketSize)
		for {
			n, meta, e := h.Receive(buf)
			if e != nil {
				errCh <- e
				return
			}
			count := packets.Add(1)
			bytes.Add(uint64(n))
			if dropAll.Load() {
				rejected.Add(1)
				continue
			}
			if mode == "capture" {
				if count <= 30 {
					log.Info("packet", "length", n, "egress_if", meta.IfIndex, "impostor", meta.Impostor, "summary", packetSummary(buf[:n]))
				}
				continue
			}
			if e = g.Inject(buf[:n]); e != nil {
				rejected.Add(1)
				if !errors.Is(e, gateway.ErrPacketRejected) {
					log.Error("inject_failed", "error", e)
				}
			}
		}
	}()
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	guardTicker := time.NewTicker(time.Second)
	defer guardTicker.Stop()
	guardFailed := false
	for {
		select {
		case <-ctx.Done():
			log.Info("stopping", "packets", packets.Load(), "bytes", bytes.Load())
			if g != nil {
				g.Close()
			}
			return h.Close()
		case e := <-errCh:
			return e
		case <-guardTicker.C:
			if guard != 0 && !guardFailed {
				status, e := windows.WaitForSingleObject(guard, 0)
				if e != nil || status != uint32(windows.WAIT_TIMEOUT) {
					// Do not close the last interception handle on guard failure:
					// retain it and consume/drop all subsequent client packets.
					guardFailed = true
					dropAll.Store(true)
					log.Error("guard exited; proxy retains interception and drops client packets; close phone default route before Stop.ps1")
				}
			}
		case <-ticker.C:
			if g != nil {
				log.Info("stats", "gateway", g.Stats(), "dropped", rejected.Load())
			} else {
				log.Info("stats", "packets", packets.Load(), "bytes", bytes.Load())
			}
		}
	}
}

func writeReady(path, mode string, c settings.Config) error {
	if path == "" {
		return nil
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(absolute), 0700); err != nil {
		return err
	}
	configHash, err := hashConfig(c)
	if err != nil {
		return err
	}
	start, err := processStart(windows.CurrentProcess())
	if err != nil {
		return err
	}
	b, err := json.Marshal(map[string]any{"pid": os.Getpid(), "mode": mode, "version": version, "source": c.SourceIP, "interface_index": c.InterfaceIndex, "ready_utc": time.Now().UTC().Format(time.RFC3339Nano), "config_sha256": configHash, "process_start_filetime": start})
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(absolute), "ready-*.tmp")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if _, err = f.Write(b); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, absolute)
}

func verifyGuard(pid uint32, readyPath string, c settings.Config) (windows.Handle, error) {
	if pid == uint32(os.Getpid()) {
		return 0, errors.New("guard must be an independent process")
	}
	h, err := windows.OpenProcess(windows.SYNCHRONIZE|windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return 0, err
	}
	failed := true
	defer func() {
		if failed {
			windows.CloseHandle(h)
		}
	}()
	buf := make([]uint16, 32768)
	size := uint32(len(buf))
	if err = windows.QueryFullProcessImageName(h, 0, &buf[0], &size); err != nil {
		return 0, err
	}
	self, err := os.Executable()
	if err != nil {
		return 0, err
	}
	if !strings.EqualFold(filepath.Clean(windows.UTF16ToString(buf[:size])), filepath.Clean(self)) {
		return 0, errors.New("guard process executable does not match this gateway")
	}
	if readyPath == "" {
		return 0, errors.New("guard readiness proof is required")
	}
	proof, err := os.ReadFile(readyPath)
	if err != nil {
		return 0, err
	}
	var ready struct {
		PID       uint32 `json:"pid"`
		Mode      string `json:"mode"`
		Source    string `json:"source"`
		Interface uint32 `json:"interface_index"`
		Config    string `json:"config_sha256"`
		Start     string `json:"process_start_filetime"`
	}
	if err = json.Unmarshal(proof, &ready); err != nil {
		return 0, err
	}
	wantHash, err := hashConfig(c)
	if err != nil {
		return 0, err
	}
	wantStart, err := processStart(h)
	if err != nil {
		return 0, err
	}
	if ready.PID != pid || ready.Mode != "guard" || ready.Source != c.SourceIP || ready.Interface != c.InterfaceIndex || ready.Config != wantHash || ready.Start != wantStart {
		return 0, errors.New("guard readiness does not match process, phone, interface and exact configuration")
	}
	status, err := windows.WaitForSingleObject(h, 0)
	if err != nil || status != uint32(windows.WAIT_TIMEOUT) {
		return 0, errors.New("guard process is not alive")
	}
	failed = false
	return h, nil
}

func hashConfig(c settings.Config) (string, error) {
	b, e := os.ReadFile(c.File)
	if e != nil {
		return "", e
	}
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:]), nil
}
func processStart(h windows.Handle) (string, error) {
	var created, exit, kernel, user windows.Filetime
	if e := windows.GetProcessTimes(h, &created, &exit, &kernel, &user); e != nil {
		return "", e
	}
	return strconv.FormatUint(uint64(created.HighDateTime)<<32|uint64(created.LowDateTime), 10), nil
}

func packetSummary(b []byte) string {
	if len(b) < 20 {
		return "short packet"
	}
	src := netip.AddrFrom4([4]byte(b[12:16]))
	dst := netip.AddrFrom4([4]byte(b[16:20]))
	ihl := int(b[0]&15) * 4
	if (b[9] == 6 || b[9] == 17) && len(b) >= ihl+4 && binary.BigEndian.Uint16(b[6:8])&0x1fff == 0 {
		return fmt.Sprintf("%s:%d -> %s:%d protocol=%d", src, binary.BigEndian.Uint16(b[ihl:]), dst, binary.BigEndian.Uint16(b[ihl+2:]), b[9])
	}
	return fmt.Sprintf("%s -> %s protocol=%d", src, dst, b[9])
}
