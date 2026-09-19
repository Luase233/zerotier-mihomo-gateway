//go:build windows

package divert

import (
	"encoding/binary"
	"errors"
	"fmt"
	"net/netip"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

type nativeFunctions struct {
	open, receive, send, shutdown, close, checksums, getParam uintptr
}

// Handle owns a single NETWORK_FORWARD handle. Receive and Send may run
// concurrently. Close interrupts Receive and waits for outstanding calls.
type Handle struct {
	dll       windows.Handle
	native    windows.Handle
	functions nativeFunctions
	client    netip.Addr
	sniff     bool
	guard     bool
	mu        sync.Mutex
	closed    bool
	active    sync.WaitGroup
	closeOnce sync.Once
	closeErr  error
}

// Open loads the caller's explicitly selected WinDivert.dll and opens only the
// exact source IPv4 filter at NETWORK_FORWARD. Opening requires elevation and
// may load the official WinDivert kernel driver. sniff=true leaves traffic
// unchanged and disables injection. sniff=false consumes captured packets.
// In consuming mode, the caller must reject Windows NAT covering this source.
// Sniff mode may observe an existing setup, but NAT makes its coverage unreliable.
func Open(dllPath, source string, sniff bool) (*Handle, error) {
	return openHandle(dllPath, source, sniff, false)
}

// OpenGuard opens an independent kernel-drop handle for the same source. Its
// lower priority (-1000) lets a priority-0 proxy capture first; packets fall
// through to this guard when that proxy exits. The guard needs its own process
// to survive a proxy crash. It is NOT a persistent firewall across guard exit,
// reboot, or driver failure. The caller must reject conflicting NAT first.
func OpenGuard(dllPath, source string) (*Handle, error) {
	return openHandle(dllPath, source, false, true)
}

func openHandle(dllPath, source string, sniff, guard bool) (*Handle, error) {
	if unsafe.Sizeof(uintptr(0)) != 8 {
		return nil, errors.New("WinDivert adapter supports only 64-bit Windows processes")
	}
	filter, err := Filter(source)
	if err != nil {
		return nil, err
	}
	if !filepath.IsAbs(dllPath) || !strings.EqualFold(filepath.Base(dllPath), "WinDivert.dll") {
		return nil, errors.New("DLL path must be an absolute path to WinDivert.dll")
	}
	info, err := os.Stat(dllPath)
	if err != nil {
		return nil, fmt.Errorf("WinDivert DLL: %w", err)
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("WinDivert DLL path is not a regular file")
	}
	// Restrict dependencies to the selected DLL directory and Windows System32;
	// do not search the current working directory or PATH for an executable DLL.
	dll, err := windows.LoadLibraryEx(dllPath, 0,
		windows.LOAD_LIBRARY_SEARCH_DLL_LOAD_DIR|windows.LOAD_LIBRARY_SEARCH_SYSTEM32)
	if err != nil {
		return nil, fmt.Errorf("load WinDivert DLL: %w", err)
	}
	f := nativeFunctions{}
	procedures := []struct {
		name string
		to   *uintptr
	}{
		{"WinDivertOpen", &f.open}, {"WinDivertRecv", &f.receive},
		{"WinDivertSend", &f.send}, {"WinDivertShutdown", &f.shutdown},
		{"WinDivertClose", &f.close}, {"WinDivertHelperCalcChecksums", &f.checksums},
		{"WinDivertGetParam", &f.getParam},
	}
	for _, proc := range procedures {
		*proc.to, err = windows.GetProcAddress(dll, proc.name)
		if err != nil {
			_ = windows.FreeLibrary(dll)
			return nil, fmt.Errorf("resolve %s in WinDivert DLL: %w", proc.name, err)
		}
	}
	filterBytes := append([]byte(filter), 0)
	flags := uintptr(0)
	priority := int16(0)
	if sniff {
		flags = flagSniff | flagRecvOnly
	} else if guard {
		flags = flagDrop | flagRecvOnly
		priority = guardPriority
	}
	value, _, callErr := syscall.SyscallN(f.open,
		uintptr(unsafe.Pointer(&filterBytes[0])), layerNetworkForward, uintptr(priority), flags)
	runtime.KeepAlive(filterBytes)
	if value == ^uintptr(0) || value == 0 {
		_ = windows.FreeLibrary(dll)
		return nil, apiError("WinDivertOpen (requires administrator; verify official driver files)", callErr)
	}
	var major uint64
	r, _, callErr := syscall.SyscallN(f.getParam, value, 3, uintptr(unsafe.Pointer(&major)))
	if r == 0 || major != 2 {
		_, _, _ = syscall.SyscallN(f.shutdown, value, 3)
		_, _, _ = syscall.SyscallN(f.close, value)
		_ = windows.FreeLibrary(dll)
		if r == 0 {
			return nil, apiError("query WinDivert driver version", callErr)
		}
		return nil, fmt.Errorf("unsupported WinDivert driver major version %d; require 2.x", major)
	}
	client, _ := parseSource(source) // Filter already validated source.
	return &Handle{dll: dll, native: windows.Handle(value), functions: f, client: client, sniff: sniff, guard: guard}, nil
}

func (h *Handle) begin() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return os.ErrClosed
	}
	h.active.Add(1)
	return nil
}

// Receive returns one complete IPv4 packet. Use a MaxPacketSize buffer. A
// truncated receive is reported as an error and is never returned as success.
func (h *Handle) Receive(buffer []byte) (int, Metadata, error) {
	if h.guard {
		return 0, Metadata{}, errors.New("receiving is disabled on a kernel-drop guard handle")
	}
	if len(buffer) == 0 || uint64(len(buffer)) > uint64(^uint32(0)) {
		return 0, Metadata{}, errors.New("invalid receive buffer length")
	}
	if err := h.begin(); err != nil {
		return 0, Metadata{}, err
	}
	defer h.active.Done()
	var address nativeAddress
	var length uint32
	r, _, callErr := syscall.SyscallN(h.functions.receive, uintptr(h.native),
		uintptr(unsafe.Pointer(&buffer[0])), uintptr(len(buffer)),
		uintptr(unsafe.Pointer(&length)), uintptr(unsafe.Pointer(&address)))
	runtime.KeepAlive(buffer)
	if r == 0 {
		h.mu.Lock()
		closed := h.closed
		h.mu.Unlock()
		if closed {
			return 0, Metadata{}, os.ErrClosed
		}
		return 0, Metadata{}, apiError("WinDivertRecv", callErr)
	}
	if uint64(length) > uint64(len(buffer)) {
		return 0, Metadata{}, errors.New("WinDivert returned a truncated packet")
	}
	packet := buffer[:length]
	if err := validatePacket(packet); err != nil {
		return 0, Metadata{}, fmt.Errorf("received packet: %w", err)
	}
	source := netip.AddrFrom4([4]byte{packet[12], packet[13], packet[14], packet[15]})
	if source != h.client || address.Flags&0xff != layerNetworkForward || address.Flags&addressIPv6 != 0 {
		return 0, Metadata{}, errors.New("received packet does not match the configured source/layer")
	}
	return int(length), address.metadata(), nil
}

// Send injects a generated response through the specified egress interface.
// It only accepts packets addressed to the configured client. It updates the
// caller's packet checksums; fragmented packets must already carry a valid
// transport checksum calculated over the complete datagram by the IP stack.
// The caller must exclusively own packet until it
// returns. Success means accepted for injection, not acknowledged delivery.
func (h *Handle) Send(packet []byte, ifIndex uint32) error {
	if h.sniff || h.guard {
		return errors.New("injection is disabled on a capture-only or guard handle")
	}
	if err := validateReturnPacket(packet, h.client, ifIndex); err != nil {
		return err
	}
	if err := h.begin(); err != nil {
		return err
	}
	defer h.active.Done()
	address := nativeAddress{Flags: layerNetworkForward | addressOutbound, IfIndex: ifIndex}
	if err := h.prepareSendChecksums(packet, &address); err != nil {
		return err
	}
	var sent uint32
	r, _, callErr := syscall.SyscallN(h.functions.send, uintptr(h.native),
		uintptr(unsafe.Pointer(&packet[0])), uintptr(len(packet)),
		uintptr(unsafe.Pointer(&sent)), uintptr(unsafe.Pointer(&address)))
	runtime.KeepAlive(packet)
	if r == 0 {
		return apiError("WinDivertSend", callErr)
	}
	if int(sent) != len(packet) {
		return fmt.Errorf("WinDivertSend injected %d of %d bytes", sent, len(packet))
	}
	return nil
}

// prepareSendChecksums takes a validated complete IPv4 packet or fragment.
// A first fragment contains a transport header but only part of its data; the
// default WinDivert helper therefore rejects it as truncated. Later fragments
// do not even contain that header. For both cases preserve the stack's checksum
// over the original datagram and calculate only this fragment's IP checksum.
func (h *Handle) prepareSendChecksums(packet []byte, address *nativeAddress) error {
	flags := uintptr(0)
	if binary.BigEndian.Uint16(packet[6:8])&0x3fff != 0 {
		// NO_ICMP_CHECKSUM | NO_ICMPV6_CHECKSUM | NO_TCP_CHECKSUM | NO_UDP_CHECKSUM.
		flags = 2 | 4 | 8 | 16
		// WinDivertSend independently fixes checksums whose metadata bits are
		// clear. Both bits must be set, including for non-initial fragments,
		// so the kernel never tries to recompute an incomplete transport segment.
		address.Flags |= addressTCPChecksum | addressUDPChecksum
	}
	r, _, callErr := syscall.SyscallN(h.functions.checksums,
		uintptr(unsafe.Pointer(&packet[0])), uintptr(len(packet)),
		uintptr(unsafe.Pointer(address)), flags)
	runtime.KeepAlive(packet)
	if r == 0 {
		return apiError("WinDivertHelperCalcChecksums", callErr)
	}
	return nil
}

// Close is idempotent. Capturing ceases when the handle is closed; this package
// does not provide a persistent fail-closed firewall after process exit.
func (h *Handle) Close() error {
	h.closeOnce.Do(func() {
		h.mu.Lock()
		h.closed = true
		h.mu.Unlock()
		// DROP handles have no receives to interrupt. Keep the filter fully
		// active until this explicit close; no partial shutdown is necessary.
		if h.guard {
			r, _, callErr := syscall.SyscallN(h.functions.close, uintptr(h.native))
			if r == 0 {
				h.closeErr = apiError("WinDivertClose guard", callErr)
			}
			if err := windows.FreeLibrary(h.dll); err != nil {
				h.closeErr = errors.Join(h.closeErr, fmt.Errorf("unload WinDivert DLL: %w", err))
			}
			return
		}
		r, _, callErr := syscall.SyscallN(h.functions.shutdown, uintptr(h.native), 3)
		if r == 0 {
			h.closeErr = apiError("WinDivertShutdown (pending native calls may remain until process exit)", callErr)
			_ = windows.CancelIoEx(h.native, nil)
			// A call admitted before Close might not have entered the DLL yet.
			// If shutdown failed, cancellation alone cannot guarantee it will
			// finish. Retain the DLL and handle until all calls finish instead
			// of hanging Close or unloading code still in use by another thread.
			go func() {
				h.active.Wait()
				_, _, _ = syscall.SyscallN(h.functions.close, uintptr(h.native))
				_ = windows.FreeLibrary(h.dll)
			}()
			return
		}
		h.active.Wait()
		r, _, callErr = syscall.SyscallN(h.functions.close, uintptr(h.native))
		if r == 0 {
			h.closeErr = errors.Join(h.closeErr, apiError("WinDivertClose", callErr))
		}
		if err := windows.FreeLibrary(h.dll); err != nil {
			h.closeErr = errors.Join(h.closeErr, fmt.Errorf("unload WinDivert DLL: %w", err))
		}
	})
	return h.closeErr
}

func apiError(operation string, err syscall.Errno) error {
	if err == 0 {
		return fmt.Errorf("%s failed without a Windows error code", operation)
	}
	return fmt.Errorf("%s: %w", operation, err)
}
