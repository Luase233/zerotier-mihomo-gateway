package settings

import "testing"

func TestUniqueInterface(t *testing.T) {
	for _, test := range []struct {
		matches []uint32
		want    uint32
		fails   bool
	}{
		{[]uint32{19}, 19, false},
		{nil, 0, true},
		{[]uint32{18, 19}, 0, true},
	} {
		got, err := uniqueInterface(test.matches)
		if (err != nil) != test.fails || got != test.want {
			t.Fatalf("matches=%v: got index=%d, error=%v", test.matches, got, err)
		}
	}
}
