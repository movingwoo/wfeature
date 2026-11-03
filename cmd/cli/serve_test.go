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

// `run` speaks the page's key codes, and the page sends -6 and -7 for the soft
// keys on every platform.
func TestSharedKeyNamesAreThePageCodes(t *testing.T) {
	for _, platform := range []string{"ktf", "lgt", "skt"} {
		lookup := sharedKeyLookup(platform)
		for name, want := range map[string]int32{"soft1": -6, "soft2": -7, "fire": 148, "clear": 8, "call": 10, "5": '5'} {
			if code, ok := lookup(name); !ok || code != want {
				t.Errorf("%s %s = %d/%t, want %d", platform, name, code, ok, want)
			}
		}
	}
	for _, name := range []string{"soft3", "ez", "hangup"} {
		if _, ok := sharedKeyLookup("ktf")(name); !ok {
			t.Errorf("ktf refused %s", name)
		}
		if code, ok := sharedKeyLookup("skt")(name); ok {
			t.Errorf("skt sent %s as %d, which a MIDlet or a script reads as another key", name, code)
		}
	}
}
