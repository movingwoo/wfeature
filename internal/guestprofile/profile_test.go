package guestprofile

import (
	"strings"
	"testing"
)

func TestFrameStringFallsBackToTheAddress(t *testing.T) {
	if got := (Frame{Address: 0x1234}).String(); got != "0x1234" {
		t.Fatalf("unnamed frame = %q, want 0x1234", got)
	}
	if got := (Frame{Address: 0x1234, Symbol: "a.one()V", Offset: 0x12}).String(); got != "a.one()V+0x12" {
		t.Fatalf("named frame = %q, want a.one()V+0x12", got)
	}
}

func TestProfileFoldedRendersOutermostFrameFirst(t *testing.T) {
	profile := Profile{Stacks: []Stack{{
		Frames: []Frame{
			{Address: 0x1000, Symbol: "a.leaf()V"},
			{Address: 0x2000, Symbol: "a.middle()V", Offset: 4},
			{Address: 0x3000},
		},
		Count: 7,
	}}}
	want := "0x3000;a.middle()V+0x4;a.leaf()V+0x0 7\n"
	if got := profile.Folded(); got != want {
		t.Fatalf("Folded() = %q, want %q", got, want)
	}
}

func TestUnnamedLeavesAreRankedByRegionRatherThanByInstruction(t *testing.T) {
	// A pure WIPI C game has no AOT bodies to resolve against, so every leaf
	// is an address. Ranking them one instruction at a time spread a single
	// hot loop over thousands of entries too small to read; the loop has to
	// come back as one row.
	loop := []uint32{0x107c7a, 0x107c7c, 0x107c7e, 0x107c80, 0x107c82}
	elsewhere := uint32(0x10a5c6)
	index := newRegionIndex(append(append([]uint32(nil), loop...), elsewhere))

	for _, address := range loop {
		if got := index.label(Frame{Address: address}); got != "0x107c7a-0x107c82" {
			t.Fatalf("label(%#x) = %q, want the whole loop", address, got)
		}
	}
	// A region of one address stays a plain address rather than an empty span.
	if got := index.label(Frame{Address: elsewhere}); got != "0x10a5c6" {
		t.Errorf("label(%#x) = %q, want the bare address", elsewhere, got)
	}
	// A gap wider than regionGap is a different piece of code, not a cold
	// branch inside this one.
	if index.label(Frame{Address: loop[0]}) == index.label(Frame{Address: elsewhere}) {
		t.Error("two hot spots 0x2900 apart were merged into one region")
	}
	// A name always wins: regions exist for the code that has none.
	if got := index.label(Frame{Address: loop[0], Symbol: "a.one()V"}); got != "a.one()V" {
		t.Errorf("label of a named frame = %q, want its symbol", got)
	}
	// An address no sample ever landed on is reported as itself.
	if got := index.label(Frame{Address: 0x200000}); got != "0x200000" {
		t.Errorf("label of an unsampled address = %q, want the bare address", got)
	}
}

func TestReportSaysWhyItHasNoStacksInsteadOfRepeatingTheRanking(t *testing.T) {
	// RVCT keeps no frame pointer in r7, so every stack is its leaf. A
	// "hottest stacks" section would then repeat the self-time ranking line
	// for line under a heading promising callers.
	profile := Profile{
		Samples: 10,
		Steps:   10000,
		Stacks:  []Stack{{Frames: []Frame{{Address: 0x1000}}, Count: 10, Share: 1}},
		Leaves:  []Leaf{{Symbol: "0x1000", Count: 10, Share: 1}},
	}
	report := profile.Report(10)
	if strings.Contains(report, "hottest stacks") {
		t.Errorf("report promises callers it does not have:\n%s", report)
	}
	if !strings.Contains(report, "no frame pointer in r7") {
		t.Errorf("report does not say why there are no callers:\n%s", report)
	}

	profile.Stacks[0].Frames = append(profile.Stacks[0].Frames, Frame{Address: 0x2000})
	if !strings.Contains(profile.Report(10), "hottest stacks") {
		t.Error("a profile that did walk callers lost its stacks section")
	}
}
