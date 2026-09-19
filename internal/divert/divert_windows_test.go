//go:build windows

package divert

import "testing"

// These calls stop at input validation: they never load a DLL or open a driver.
func TestOpenRejectsInvalidArgumentsBeforeLoading(t *testing.T) {
	for _, test := range []struct{ path, source string }{
		{"WinDivert.dll", "10.147.20.2"},
		{`C:\untrusted\other.dll`, "10.147.20.2"},
		{`C:\untrusted\WinDivert.dll`, "10.147.20.2 or true"},
	} {
		if h, err := Open(test.path, test.source, true); err == nil || h != nil {
			t.Fatalf("accepted invalid DLL/source: %+v", test)
		}
		if h, err := OpenGuard(test.path, test.source); err == nil || h != nil {
			t.Fatalf("guard accepted invalid DLL/source: %+v", test)
		}
	}
}

func TestGuardCannotReceiveOrInject(t *testing.T) {
	// Guard operations must fail before touching any native function pointer.
	h := &Handle{guard: true}
	if _, _, err := h.Receive(make([]byte, MaxPacketSize)); err == nil {
		t.Fatal("guard allowed receiving")
	}
	if err := h.Send(nil, 18); err == nil {
		t.Fatal("guard allowed injection")
	}
}
