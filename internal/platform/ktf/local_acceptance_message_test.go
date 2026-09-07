package ktf

import (
	"fmt"
	"strings"
	"testing"
)

// A probe failure is read by the sweep one line long.
//
// `internal/tools/acceptance` keeps the last line a subtest printed and
// nothing else, because that is where `t.Fatalf` leaves its message and
// because a report needs one sentence per archive rather than a transcript.
// That makes the order of a failure's own parts load-bearing: whatever the
// message ends with is the whole of what a person reading the sweep will see.
//
// These probes print the runtime's diagnostic counts with their failures, and
// the counts are a block of dozens of lines. Ending a message with that block
// spends the one line the report has on an ordinary successful call. It is not
// hypothetical: an archive that refused a rung was written down as
// `1 getmethod getClipX()I` — the fortieth count line of a run whose lookups
// had all succeeded — and reading the report cost a session spent looking for
// a member that was already declared, while the error that ended the run was
// never in the report at all.
//
// So the counts go first and the reason goes last.
func TestDiagnosticCountsDoNotDisplaceTheReason(t *testing.T) {
	// A run whose busiest counters are ordinary work and whose quietest is a
	// lookup that succeeded — the shape every one of these probes produces.
	counts := map[string]uint32{
		"wipic 0x3000b":                    369796,
		"hook strlen":                      136199,
		"java org/kwis/msp/lcdui/Graphics": 13,
		"getmethod getClipX()I":            1,
	}
	const reason = "tick 184 after the first frame: KTF pending timer count exceeds 256"

	// The shape this replaced: the reason first, the counts last. What the
	// sweep records from it names no failure.
	displaced := lastLine(fmt.Sprintf("%s\ncounts:\n%s", reason, formatDiagnosticCounts(counts, 40)))
	if strings.Contains(displaced, "pending timer") {
		t.Fatalf("the old shape is meant to lose the reason, and this one kept it: %q", displaced)
	}

	message := withDiagnosticCounts(counts, 40, "tick %d after the first frame: %v",
		184, fmt.Errorf("KTF pending timer count exceeds 256"))
	if got := lastLine(message); got != reason {
		t.Fatalf("the sweep would record %q, not the reason %q", got, reason)
	}
	// The counts still have to be there: they are how a person answers "why"
	// once the report has said "what".
	if !strings.Contains(message, "getmethod getClipX()I") || !strings.Contains(message, "counts:") {
		t.Fatalf("the counts were dropped rather than moved:\n%s", message)
	}
}

// lastLine is how the sweep reads a probe's failure: the last line it printed.
// See `collect` in internal/tools/acceptance.
func lastLine(message string) string {
	lines := strings.Split(strings.TrimRight(message, "\n"), "\n")
	return strings.TrimSpace(lines[len(lines)-1])
}
