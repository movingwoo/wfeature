package webhost

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"testing"
	"time"

	"github.com/movingwoo/wfeature/internal/backend"
)

// A drain gives a game with its page attached the round it is in and then its
// title's ending, and both are bounds a title's code is held to. A deadline at
// the ending's bound plus five seconds left a title behind, still ending and
// with its last save unwritten, whenever the round had waited past those five.
func TestTheStopDeadlineOutlastsARoundAndAnEnding(t *testing.T) {
	if StopDeadline <= backend.WaitBound+backend.EndingBound {
		t.Fatalf("StopDeadline is %v, not past a round's %v and an ending's %v", StopDeadline, backend.WaitBound, backend.EndingBound)
	}
}

// The Android app waits for the server a finished activity ended before it
// starts the next one, and ends one still there after a bound of its own,
// which is a number in Java rather than this constant. Within the server's
// drain deadline, it ended a server still closing its games, with their last
// saves.
func TestTheAndroidAppWaitsPastTheStopDeadline(t *testing.T) {
	source, err := os.ReadFile(filepath.Join("..", "..", "mobile", "android", "src", "com", "movingwoo", "wfeature", "MainActivity.java"))
	if err != nil {
		t.Fatal(err)
	}
	match := regexp.MustCompile(`ENDING_WAIT_SECONDS = (\d+);`).FindSubmatch(source)
	if match == nil {
		t.Fatal("MainActivity.java declares no ENDING_WAIT_SECONDS")
	}
	seconds, err := strconv.Atoi(string(match[1]))
	if err != nil {
		t.Fatal(err)
	}
	if wait := time.Duration(seconds) * time.Second; wait <= StopDeadline {
		t.Fatalf("the app waits %v for an ending server, within its drain deadline %v", wait, StopDeadline)
	}
}
