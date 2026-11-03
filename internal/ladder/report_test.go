package ladder

import "testing"

func TestScreensReadsAsASentence(t *testing.T) {
	for count, want := range map[int]string{0: "screens", 1: "screen", 2: "screens"} {
		if got := Screens(count); got != want {
			t.Errorf("Screens(%d) = %q, want %q", count, got, want)
		}
	}
}

func TestOpeningMovedOnTellsTheTwoDefectsApart(t *testing.T) {
	for _, test := range []struct {
		moved int
		want  string
	}{
		{0, ", and it did not move on its own in the 1024 ticks after"},
		{1, ", over 1 screen its opening moved on to"},
		{3, ", over 3 screens its opening moved on to"},
	} {
		if got := OpeningMovedOn(test.moved, 1024); got != test.want {
			t.Errorf("OpeningMovedOn(%d, 1024) = %q, want %q", test.moved, got, test.want)
		}
	}
}
