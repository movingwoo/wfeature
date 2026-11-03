package ktf

import (
	"strconv"
	"strings"
	"testing"
)

// TestAScreenThatLeavesOnItsOwnIsNotAScreenThatAnsweredNoKey pins the judgment
// the rung's last round used to skip.
//
// Unlike the ladder's rungs in local_ladder_test.go this needs no archive,
// because what it checks is the decision rather than the driving: a screen
// that leaves by itself has not refused the keys, and reporting it as one is a
// claim about a title made from a screen that was still playing its opening.
// Five archives were reported that way before the watch was moved onto the
// last round.
func TestAScreenThatLeavesOnItsOwnIsNotAScreenThatAnsweredNoKey(t *testing.T) {
	unanswerable, why := openingReport(4606)
	if !unanswerable {
		t.Fatal("a screen that left on its own 4606 ticks after the keys was reported as a title that answers no key")
	}
	if !strings.Contains(why, "4606") {
		t.Errorf("the reason does not say how long the screen took to leave, so whoever reads it has to measure it again: %q", why)
	}
	unanswerable, why = openingReport(-1)
	if unanswerable {
		t.Fatal("a screen that did not move on its own was reported as unanswerable, so a stopped screen would be filed as a question nobody asked")
	}
	if !strings.Contains(why, strconv.Itoa(localLadderOpeningTicks)) {
		t.Errorf("the reason does not say how long the screen was watched for: %q", why)
	}
}
