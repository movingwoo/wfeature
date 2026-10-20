package backend

import "testing"

// The range is inclusive at both ends and holds each side on its own, because a
// page's start, the CLI's -screen and a KTF descriptor are all read by it.
func TestValidScreenHoldsBothSides(t *testing.T) {
	for _, test := range []struct {
		width, height int
		want          bool
	}{
		{MinScreenSide, MinScreenSide, true},
		{MaxScreenSide, MaxScreenSide, true},
		{176, 220, true},
		{MinScreenSide - 1, 220, false},
		{176, MaxScreenSide + 1, false},
		{0, 0, false},
		{-240, 320, false},
	} {
		if got := ValidScreen(test.width, test.height); got != test.want {
			t.Errorf("ValidScreen(%d, %d) = %v, want %v", test.width, test.height, got, test.want)
		}
	}
}
