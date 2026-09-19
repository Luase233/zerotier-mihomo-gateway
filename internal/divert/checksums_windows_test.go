//go:build windows

package divert

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

// This loads only the checksum function from the hash-checked official DLL.
// It never resolves/calls WinDivertOpen and cannot install/open the driver.
func checksumDLLForTest(t *testing.T) *Handle {
	t.Helper()
	path, err := filepath.Abs(filepath.Join("..", "..", "runtime", "WinDivert.dll"))
	if err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		t.Skip("official runtime DLL is not installed; native checksum integration test skipped")
	}
	if err != nil {
		t.Fatal(err)
	}
	manifestBytes, err := os.ReadFile(filepath.Join(filepath.Dir(path), "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		Hash string `json:"dll_sha256"`
	}
	if err := json.Unmarshal([]byte(strings.TrimPrefix(string(manifestBytes), "\ufeff")), &manifest); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(content)
	if !strings.EqualFold(manifest.Hash, hex.EncodeToString(sum[:])) {
		t.Fatal("official runtime DLL hash mismatch")
	}
	dll, err := windows.LoadLibraryEx(path, 0, windows.LOAD_LIBRARY_SEARCH_DLL_LOAD_DIR|windows.LOAD_LIBRARY_SEARCH_SYSTEM32)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := windows.FreeLibrary(dll); err != nil {
			t.Error(err)
		}
	})
	proc, err := windows.GetProcAddress(dll, "WinDivertHelperCalcChecksums")
	if err != nil {
		t.Fatal(err)
	}
	return &Handle{functions: nativeFunctions{checksums: proc}}
}

func TestNativeFragmentChecksumsPreserveReassembledDatagram(t *testing.T) {
	h := checksumDLLForTest(t)
	for _, test := range []struct {
		name                       string
		protocol                   byte
		headerSize, checksumOffset int
	}{
		{"UDP", 17, 8, 6}, {"TCP", 6, 20, 16}, {"ICMP", 1, 8, 2},
	} {
		t.Run(test.name, func(t *testing.T) {
			const ipHeader = 20
			full := make([]byte, ipHeader+test.headerSize+9000)
			full[0], full[8], full[9] = 0x45, 64, test.protocol
			binary.BigEndian.PutUint16(full[2:4], uint16(len(full)))
			binary.BigEndian.PutUint16(full[4:6], 12345)
			copy(full[12:16], []byte{203, 0, 113, 11})
			copy(full[16:20], []byte{192, 168, 196, 184})
			segment := full[ipHeader:]
			switch test.protocol {
			case 17:
				binary.BigEndian.PutUint16(segment[0:2], 443)
				binary.BigEndian.PutUint16(segment[2:4], 43456)
				binary.BigEndian.PutUint16(segment[4:6], uint16(len(segment)))
			case 6:
				binary.BigEndian.PutUint16(segment[0:2], 443)
				binary.BigEndian.PutUint16(segment[2:4], 43456)
				segment[12], segment[13] = 0x50, 0x18
			case 1:
				segment[0] = 0 // Echo reply.
			}
			for i := test.headerSize; i < len(segment); i++ {
				segment[i] = byte(i*31 + 7)
			}
			// Compute the complete-datagram checksum just as the stack does
			// before fragmenting, then independently verify it below.
			address := nativeAddress{}
			if err := h.prepareSendChecksums(full, &address); err != nil {
				t.Fatal(err)
			}
			originalChecksum := binary.BigEndian.Uint16(segment[test.checksumOffset:])
			if originalChecksum == 0 {
				t.Fatal("fixture must have a nonzero transport checksum")
			}
			var reassembled []byte
			const fragmentPayload = 1256 // Multiple of 8, with room below MTU 1280.
			for offset := 0; offset < len(segment); offset += fragmentPayload {
				end := min(offset+fragmentPayload, len(segment))
				fragment := append(append([]byte(nil), full[:ipHeader]...), segment[offset:end]...)
				binary.BigEndian.PutUint16(fragment[2:4], uint16(len(fragment)))
				bits := uint16(offset / 8)
				if end < len(segment) {
					bits |= 0x2000
				}
				binary.BigEndian.PutUint16(fragment[6:8], bits)
				fragment[10], fragment[11] = 0, 0
				if offset == 0 {
					// Reproduce the old behavior: flags=0 rejects a first fragment.
					oldCopy := append([]byte(nil), fragment...)
					r, _, _ := syscall.SyscallN(h.functions.checksums, uintptr(unsafe.Pointer(&oldCopy[0])), uintptr(len(oldCopy)), 0, 0)
					runtime.KeepAlive(oldCopy)
					if r != 0 {
						t.Fatal("expected WinDivert 2.2.2 to reject full checksum calculation on first fragment")
					}
				}
				payloadBefore := append([]byte(nil), fragment[ipHeader:]...)
				address = nativeAddress{Flags: layerNetworkForward | addressOutbound, IfIndex: 18}
				if err := h.prepareSendChecksums(fragment, &address); err != nil {
					t.Fatalf("offset %d: %v", offset, err)
				}
				if !bytes.Equal(payloadBefore, fragment[ipHeader:]) {
					t.Fatalf("offset %d: transport data/checksum changed", offset)
				}
				if internetChecksum(fragment[:ipHeader]) != 0 {
					t.Fatalf("offset %d: invalid IPv4 checksum", offset)
				}
				if address.Flags&(addressIPChecksum|addressTCPChecksum|addressUDPChecksum) != addressIPChecksum|addressTCPChecksum|addressUDPChecksum {
					t.Fatal("metadata permits kernel transport checksum recalculation")
				}
				reassembled = append(reassembled, fragment[ipHeader:]...)
			}
			if !bytes.Equal(segment, reassembled) {
				t.Fatal("reassembled datagram differs")
			}
			if binary.BigEndian.Uint16(reassembled[test.checksumOffset:]) != originalChecksum {
				t.Fatal("full transport checksum was not preserved")
			}
			checksumInput := reassembled
			if test.protocol != 1 {
				pseudo := make([]byte, 12)
				copy(pseudo[:8], full[12:20])
				pseudo[9] = test.protocol
				binary.BigEndian.PutUint16(pseudo[10:], uint16(len(reassembled)))
				checksumInput = append(pseudo, reassembled...)
			}
			if internetChecksum(checksumInput) != 0 {
				t.Fatal("reassembled transport checksum is invalid")
			}
		})
	}
}

func internetChecksum(data []byte) uint16 {
	var sum uint32
	for len(data) >= 2 {
		sum += uint32(binary.BigEndian.Uint16(data))
		data = data[2:]
	}
	if len(data) != 0 {
		sum += uint32(data[0]) << 8
	}
	for sum>>16 != 0 {
		sum = (sum & 0xffff) + (sum >> 16)
	}
	return ^uint16(sum)
}
