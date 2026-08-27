package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/movingwoo/wfeature/internal/platform/ktf"
	"github.com/movingwoo/wfeature/internal/platform/lgt"
)

// A run that ended on a tick failure has to say so in its exit code. Without
// it a batch driving hundreds of archives reads every one of them as a pass:
// the failure is in the summary, and a sweep that had to parse the summary to
// find it is the sweep this rule exists to replace.
func TestARunThatFailedExitsNonZero(t *testing.T) {
	for _, test := range []struct {
		name       string
		err        error
		normalExit error
		want       int
	}{
		{"a run that finished", nil, ktf.ErrGuestExited, 0},
		{"a KTF guest that exited itself", ktf.ErrGuestExited, ktf.ErrGuestExited, 0},
		{"an LGT guest that exited itself", lgt.ErrGuestExited, lgt.ErrGuestExited, 0},
		// The platforms wrap the failure they report, so the answer has to
		// come from errors.Is rather than from equality.
		{"a wrapped guest exit", fmt.Errorf("tick 12: %w", ktf.ErrGuestExited), ktf.ErrGuestExited, 0},
		{"a failed tick", errors.New("unmapped read at 0x0"), ktf.ErrGuestExited, 1},
		// Ctrl-C cancels the context rather than killing the process, so an
		// interrupted route arrives here as a stopped tick. The person who
		// sent it does not need it reported back as a failure.
		{"a run somebody interrupted", context.Canceled, ktf.ErrGuestExited, 0},
		{"a wrapped interruption", fmt.Errorf("route: %w", context.Canceled), ktf.ErrGuestExited, 0},
		// One platform has no exit of its own to tell apart, and a nil
		// sentinel must not swallow the failure: errors.Is(err, nil) is false
		// for a real error, but a rule written the other way round would make
		// every failure here a pass.
		{"a failure on a platform with no guest exit", errors.New("start MIDlet: / by zero"), nil, 1},
		{"no failure on a platform with no guest exit", nil, nil, 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := exitForRun(test.err, test.normalExit); got != test.want {
				t.Fatalf("exitForRun(%v, %v) = %d, want %d", test.err, test.normalExit, got, test.want)
			}
		})
	}
}

// The rule above is only worth anything if a real command reaches it. This
// drives the whole entry point over a fixture whose MIDlet divides by zero on
// its first callback, which is the shape a run failure has from outside.
func TestACommandOverAFailingArchiveExitsNonZero(t *testing.T) {
	archive := filepath.Join("..", "..", "internal", "platform", "skt", "testdata", "runtime-failure.jar")
	if _, err := os.Stat(archive); err != nil {
		t.Skipf("fixture missing: %v", err)
	}
	var stdout, stderr bytes.Buffer
	if code := run([]string{"runskt", archive, "-ticks", "4", "-save", t.TempDir()}, &stdout, &stderr); code == 0 {
		t.Fatalf("run() = 0 over a failing archive; stderr:\n%s", stderr.String())
	}
	if !strings.Contains(stderr.String(), "ArithmeticException") {
		t.Fatalf("the failure is not on stderr:\n%s", stderr.String())
	}
}

// A fixture that runs cleanly still has to exit zero, or the rule above turns
// every run into a failure and a sweep learns nothing from either answer.
func TestACommandOverAWorkingArchiveExitsZero(t *testing.T) {
	archive := filepath.Join("..", "..", "internal", "platform", "skt", "testdata", "lifecycle.jar")
	if _, err := os.Stat(archive); err != nil {
		t.Skipf("fixture missing: %v", err)
	}
	var stdout, stderr bytes.Buffer
	if code := run([]string{"runskt", archive, "-ticks", "4", "-save", t.TempDir()}, &stdout, &stderr); code != 0 {
		t.Fatalf("run() = %d over a working archive; stderr:\n%s", code, stderr.String())
	}
}
