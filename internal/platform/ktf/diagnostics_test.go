package ktf

import (
	"fmt"
	"strings"
	"testing"
)

func TestTraceRingRetainsRecentEventsInOrder(t *testing.T) {
	ring := traceRing{limit: 3}
	for _, event := range []string{"a", "b", "c", "d", "e"} {
		ring.record(diagEvent{text: event})
	}

	entries := ring.snapshot()
	if len(entries) != 3 {
		t.Fatalf("retained %d events, want 3", len(entries))
	}
	for index, want := range []string{"c", "d", "e"} {
		if entries[index].Event != want {
			t.Errorf("entry %d is %q, want %q", index, entries[index].Event, want)
		}
	}
	// Sequences are positions in the whole stream, so the trimmed prefix stays
	// visible as a gap rather than being renumbered from one.
	if entries[0].Sequence != 3 || entries[2].Sequence != 5 {
		t.Errorf("sequences are %d..%d, want 3..5", entries[0].Sequence, entries[2].Sequence)
	}
	if ring.total != 5 {
		t.Errorf("recorded total %d, want 5", ring.total)
	}
}

func TestTraceRingWithoutLimitRecordsNothing(t *testing.T) {
	ring := traceRing{}
	ring.record(diagEvent{text: "a"})
	if entries := ring.snapshot(); entries != nil {
		t.Fatalf("retained %d events with no limit, want none", len(entries))
	}
	if ring.total != 0 {
		t.Errorf("counted %d events with no limit, want 0", ring.total)
	}
}

func TestCountDiagnosticFeedsCountsAndTrace(t *testing.T) {
	runtime := &initializationRuntime{trace: traceRing{limit: 8}}
	runtime.countDiagnostic("wipic 0x1")
	runtime.countDiagnostic("wipic 0x1")
	runtime.countDiagnostic("kernel exit")

	if got := runtime.diagnosticCounts()["wipic 0x1"]; got != 2 {
		t.Errorf("counted %d wipic calls, want 2", got)
	}
	entries := runtime.trace.snapshot()
	if len(entries) != 3 {
		t.Fatalf("traced %d events, want 3", len(entries))
	}
	if entries[2].Event != "kernel exit" {
		t.Errorf("last traced event is %q, want kernel exit", entries[2].Event)
	}
}

func TestDiagnosticsSnapshotIsACopy(t *testing.T) {
	client := &Client{runtime: &initializationRuntime{trace: traceRing{limit: 4}}}
	client.runtime.client = client
	session := &Session{Client: client}
	client.runtime.countDiagnostic("load Main")

	diagnostics := session.Diagnostics()
	diagnostics.Counts["load Main"] = 99
	if got := client.runtime.diagnosticCounts()["load Main"]; got != 1 {
		t.Errorf("mutating the snapshot changed the runtime count to %d, want 1", got)
	}
	if diagnostics.Traced != 1 {
		t.Errorf("reported %d traced events, want 1", diagnostics.Traced)
	}
}

func TestFormatCountsOrdersByFrequency(t *testing.T) {
	diagnostics := Diagnostics{Counts: map[string]uint32{"rare": 1, "common": 30, "medium": 7}}

	text := diagnostics.FormatCounts(0)
	lines := strings.Split(strings.TrimSuffix(text, "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("formatted %d lines, want 3", len(lines))
	}
	for index, want := range []string{"common", "medium", "rare"} {
		if !strings.HasSuffix(lines[index], want) {
			t.Errorf("line %d is %q, want it to end with %q", index, lines[index], want)
		}
	}
	if limited := diagnostics.FormatCounts(1); strings.Count(limited, "\n") != 1 {
		t.Errorf("limited report is %q, want one line", limited)
	}
}

func TestSetDiagnosticsClampsTheTraceLimit(t *testing.T) {
	client := &Client{}
	client.SetDiagnostics(-5, nil)
	if client.traceLimit != 0 {
		t.Errorf("negative limit became %d, want 0", client.traceLimit)
	}
	client.SetDiagnostics(traceLimitCeiling*2, nil)
	if client.traceLimit != traceLimitCeiling {
		t.Errorf("oversized limit became %d, want %d", client.traceLimit, traceLimitCeiling)
	}
}

func TestNilSessionDiagnosticsAreEmpty(t *testing.T) {
	var session *Session
	if diagnostics := session.Diagnostics(); len(diagnostics.Counts) != 0 || diagnostics.Trace != nil {
		t.Errorf("nil session reported %+v, want an empty report", diagnostics)
	}
}

func TestCountDiagnosticCollapsesCallSitesAtTheNameLimit(t *testing.T) {
	runtime := &initializationRuntime{}
	// A method called from many sites must stay countable once the name budget
	// is gone, instead of vanishing into the overflow bucket.
	runtime.countDiagnostic("java a/B.c()V @0x1000")
	for index := 0; len(runtime.callCounts) < diagnosticNameLimit; index++ {
		runtime.countDiagnostic(fmt.Sprintf("filler %d", index))
	}
	runtime.countDiagnostic("java a/B.c()V @0x2000")
	runtime.countDiagnostic("java a/B.c()V @0x3000")

	if got := runtime.diagnosticCounts()["java a/B.c()V"]; got != 2 {
		t.Errorf("collapsed count is %d, want 2", got)
	}
	// The site counted before the limit keeps its own qualified entry.
	if got := runtime.diagnosticCounts()["java a/B.c()V @0x1000"]; got != 1 {
		t.Errorf("the site counted before the limit is %d, want 1", got)
	}
}

func TestCountDiagnosticOverflowsPastTheNameCeiling(t *testing.T) {
	runtime := &initializationRuntime{}
	// Collapsed names get headroom, but not an unbounded amount: past the
	// ceiling every unseen name lands in the overflow bucket.
	for index := 0; len(runtime.callCounts) < diagnosticNameCeiling; index++ {
		runtime.countDiagnostic(fmt.Sprintf("filler %d", index))
	}
	runtime.countDiagnostic("java a/B.c()V @0x1000")
	runtime.countDiagnostic("wholly new event")

	if got := runtime.diagnosticCounts()["diagnostic overflow"]; got != 2 {
		t.Errorf("overflow count is %d, want 2", got)
	}
	if len(runtime.callCounts) != diagnosticNameCeiling+1 {
		t.Errorf("counted %d names, want the ceiling plus the overflow bucket", len(runtime.callCounts))
	}
}

func TestCollapseDiagnosticNameKeepsNamesWithoutASite(t *testing.T) {
	for name, want := range map[string]string{
		"java a/B.c()V @0x1000": "java a/B.c()V",
		"kernel exit":           "kernel exit",
		"@0x1000":               "@0x1000",
	} {
		if got := collapseDiagnosticName(name); got != want {
			t.Errorf("collapsing %q gave %q, want %q", name, got, want)
		}
	}
}

func TestCountDiagnosticKeepsFailureDetailPastTheNameLimit(t *testing.T) {
	runtime := &initializationRuntime{}
	for index := 0; len(runtime.callCounts) < diagnosticNameLimit; index++ {
		runtime.countDiagnostic(fmt.Sprintf("filler %d", index))
	}
	// An ordinary event loses its call site once the budget is gone, but a
	// throw keeps it: the site and register dump are the reason it is recorded.
	runtime.countDiagnostic("java a/B.c()V @0x2000")
	runtime.countDiagnostic("throw java/lang/ArrayIndexOutOfBoundsException @0x166ebd regs=[1 2] stack=[3 4]")
	runtime.countDiagnostic("jdb load error Main: broken")

	if got := runtime.diagnosticCounts()["java a/B.c()V"]; got != 1 {
		t.Errorf("ordinary event was not collapsed, count %d", got)
	}
	for _, detailed := range []string{
		"throw java/lang/ArrayIndexOutOfBoundsException @0x166ebd regs=[1 2] stack=[3 4]",
		"jdb load error Main: broken",
	} {
		if got := runtime.diagnosticCounts()[detailed]; got != 1 {
			t.Errorf("detail-bearing event %q was not kept whole", detailed)
		}
	}
}

func TestDiagnosticKeepsDetailSelectsFailureEvents(t *testing.T) {
	for name, want := range map[string]bool{
		"throw java/lang/Exception @0x1": true,
		"raise java/lang/Error":          true,
		"kernel exit":                    true,
		"wipic stub table 12 function 3": true,
		"save store error db/x: denied":  true,
		"java image decode error EOF":    true,
		"java a/B.c()V @0x1000":          false,
		"newarray [B len=32":             false,
		"flush lcd":                      false,
	} {
		if got := diagnosticKeepsDetail(name); got != want {
			t.Errorf("diagnosticKeepsDetail(%q) = %t, want %t", name, got, want)
		}
	}
}

// The event boundary is crossed once per key, timer and repaint a game
// receives, so it counts by kind rather than by a name composed per crossing —
// which is what diagEvent is for. The name a report prints has to be the one
// it always printed, including for a kind that is not one of ours.
func TestGuestEventNamesMatchWhatWasComposedBefore(t *testing.T) {
	for _, kind := range []int32{0, 1, 42, -1} {
		event := diagEvent{kind: diagGuestEvent, nums: [5]uint32{uint32(kind)}}
		want := fmt.Sprintf("event %d", kind)
		if got := event.String(); got != want {
			t.Errorf("event kind %d is named %q, want %q", kind, got, want)
		}
	}
	// Two of the same kind are one counter, which is the whole reason the
	// event is its own key rather than a string.
	first := diagEvent{kind: diagGuestEvent, nums: [5]uint32{3}}
	if first != (diagEvent{kind: diagGuestEvent, nums: [5]uint32{3}}) {
		t.Error("two events of one kind are not the same key")
	}
	if first == (diagEvent{kind: diagGuestEvent, nums: [5]uint32{4}}) {
		t.Error("two events of different kinds share a key")
	}
}
