package main

import (
	"strings"
	"testing"
)

func TestServeRefusesTheOptionsItReplacesByName(t *testing.T) {
	// Each of these instructs the loop -serve replaces, so accepting one and
	// ignoring it would leave a caller wondering for an afternoon why their
	// scripted keys never arrived.
	for _, name := range []string{"-cheat", "-route", "-key", "-touch", "-park", "-framedir", "-ticks"} {
		reason := serveConflict(map[string]bool{name: true})
		if !strings.Contains(reason, name) {
			t.Errorf("%s alongside -serve was not refused by name: %q", name, reason)
		}
	}
	if reason := serveConflict(map[string]bool{"-play": true, "-screen": true}); reason != "" {
		t.Errorf("an option -serve has no quarrel with was refused: %q", reason)
	}
	if reason := serveConflict(nil); reason != "" {
		t.Errorf("a plain -serve was refused: %q", reason)
	}
}

func TestShootFrameSaysWhenThereIsNothingToCapture(t *testing.T) {
	if err := shootFrame(t.TempDir()+"/none.png", nil, 0, 0); err == nil {
		t.Fatal("capturing a screen that has never been drawn reported success")
	}
}
