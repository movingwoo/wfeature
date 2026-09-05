package ktf

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Two rungs above a first frame.
//
// Every probe in this package stops at the moment a title paints something,
// and this project's own record says that is not where the defects are: a
// title that paints its opening screen and then stops answering looks exactly
// like a title that plays, at that rung. The same is true of a run that ends
// well — "it ticked three thousand times without an error" is not "it works",
// because a loop that has stopped doing anything ticks quietly forever.
//
// So two more rungs, and both are questions the archive answers rather than
// counts a person reads afterwards:
//
//   - sustained: it kept running past its first frame, without an error and
//     without ending, for a window well past the one it took to paint.
//   - interactive: a key changed what it draws. The screen is waited on until
//     it settles first, because a change measured against a screen that was
//     already animating says nothing about the key.
//
// Both are opt-in like the rest of the local probes: real archives are ignored
// local data rather than fixtures.

const (
	// How far a title is given to paint its first frame before the rungs above
	// it have anything to measure. This matches the frame probe's ceiling: the
	// loop stops as soon as a title draws or has nothing left due, so a ceiling
	// well clear of the slowest costs the others nothing.
	localLadderBootTicks = 512
	// How long a title has to keep running for after that. It is several times
	// the window most titles take to paint, and a title still going at the end
	// of it is running rather than coasting to a stop.
	localLadderSustainTicks = 300
	// A screen counts as settled once its content is unchanged for this many
	// consecutive ticks, which is what separates a title waiting for input from
	// one animating an opening sequence.
	localLadderSettleRuns = 8
	// How long to wait for that. A title that is still animating at the end of
	// this is one whose answer to a key cannot be read from a frame, so it is
	// reported as unanswerable rather than as a failure.
	localLadderSettleLimit = 240
	// How long a key is held. **A press of a single tick is missed by some
	// titles**, which read the pad on their own schedule rather than on the
	// one the event arrived by, so the hold is generous.
	localLadderHoldTicks = 16
	// How long to keep ticking after the key is released. What a key starts is
	// often a transition rather than an immediate redraw.
	localLadderReleaseTicks = 48
)

// The keys tried, in the order they are tried. A title of this era answers a
// soft key or the fire button on its opening screen; a few answer only the
// keypad. The first one that moves the screen is the answer, and which one it
// was is worth logging, because it is the key a route for that archive will
// have to start with.
var localLadderKeys = []string{"fire", "soft1", "soft2", "5", "down"}

// TestLocalKTFArchivesSustainAFrame asks whether a title that painted keeps
// running afterwards.
//
//	WFEATURE_KTF_SUSTAINED_ACCEPTANCE=1 go test -run TestLocalKTFArchivesSustainAFrame -v ./internal/platform/ktf
func TestLocalKTFArchivesSustainAFrame(t *testing.T) {
	if os.Getenv("WFEATURE_KTF_SUSTAINED_ACCEPTANCE") != "1" {
		t.Skip("set WFEATURE_KTF_SUSTAINED_ACCEPTANCE=1 to run ignored local KTF archives past their first frame")
	}
	sustain := localLadderTicks(t, "WFEATURE_KTF_SUSTAIN_TICKS", localLadderSustainTicks)
	eachLocalKTFArchive(t, func(t *testing.T, session *Session) {
		painted, ticks, why := tickToFirstFrame(t, session)
		if !painted {
			// The frame rung already reports this archive, and reporting it
			// twice would count one defect as two.
			t.Skipf("%s in %d ticks, which the frame rung below this one reports", why, ticks)
		}
		before := session.Flushes()
		ran, err := tickFor(session, sustain)
		if err != nil {
			if errors.Is(err, ErrGuestExited) {
				t.Fatalf("the title ended itself %d ticks after its first frame", ran)
			}
			t.Fatalf("tick %d after the first frame: %v\ncounts:\n%s",
				ran, err, formatDiagnosticCounts(session.Client.runtime.diagnosticCounts(), 40))
		}
		if ran < sustain {
			t.Fatalf("nothing left to do %d ticks after the first frame, of %d asked for", ran, sustain)
		}
		// A title that is still flushing is drawing; one that is not may still
		// be running a loop that draws only when something changes, so this is
		// logged rather than required.
		t.Logf("ran %d ticks past its first frame (which took %d), flushes %d → %d",
			ran, ticks, before, session.Flushes())
	})
}

// TestLocalKTFArchivesAnswerAKey asks whether a key changes what a title
// draws, which is the first rung that cannot be reached by a title that has
// stopped.
//
//	WFEATURE_KTF_INTERACTIVE_ACCEPTANCE=1 go test -run TestLocalKTFArchivesAnswerAKey -v ./internal/platform/ktf
func TestLocalKTFArchivesAnswerAKey(t *testing.T) {
	if os.Getenv("WFEATURE_KTF_INTERACTIVE_ACCEPTANCE") != "1" {
		t.Skip("set WFEATURE_KTF_INTERACTIVE_ACCEPTANCE=1 to send keys to ignored local KTF archives")
	}
	hold := localLadderTicks(t, "WFEATURE_KTF_HOLD_TICKS", localLadderHoldTicks)
	eachLocalKTFArchive(t, func(t *testing.T, session *Session) {
		painted, ticks, why := tickToFirstFrame(t, session)
		if !painted {
			t.Skipf("%s in %d ticks, which the frame rung below this one reports", why, ticks)
		}
		settled, waited := tickUntilSettled(session)
		if !settled {
			// A screen that never stops changing on its own cannot answer this
			// question: a change after a key would have happened anyway.
			t.Skipf("the screen was still changing after %d ticks, so a change after a key would prove nothing", waited)
		}
		baseline := session.FrameDigest()
		presents := session.Flushes()
		var tried []string
		for _, name := range localLadderKeys {
			code, known := KeyCodeByName(name)
			if !known {
				t.Fatalf("no key called %q", name)
			}
			tried = append(tried, name)
			changed, after, err := pressAndWatch(session, code, baseline, hold, localLadderReleaseTicks)
			if err != nil {
				if errors.Is(err, ErrGuestExited) {
					t.Fatalf("the title ended itself while %s was held", name)
				}
				t.Fatalf("holding %s: %v", name, err)
			}
			if changed {
				t.Logf("%s changed the screen after %d ticks (settled after %d, flushes %d → %d)",
					name, after, waited, presents, session.Flushes())
				return
			}
		}
		t.Fatalf("no key changed the screen: tried %s, held %d ticks each, %d flushes since it settled",
			strings.Join(tried, ", "), hold, session.Flushes()-presents)
	})
}

// eachLocalKTFArchive runs one subtest per archive in the local corpus with a
// started session in front of it, so a rung is written as the question it
// asks rather than as another copy of the walk to it.
//
// One subtest per archive is what lets a report name the archive a refusal
// came from; a count at the end of a log says a number where a report needs a
// name.
func eachLocalKTFArchive(t *testing.T, ask func(t *testing.T, session *Session)) {
	t.Helper()
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate KTF acceptance test source")
	}
	directory := filepath.Join(filepath.Dir(source), "..", "..", "..", "var", "games", "ktf")
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatalf("read local KTF game directory: %v", err)
	}
	only := os.Getenv("WFEATURE_KTF_ONLY")
	ran := 0
	for _, entry := range entries {
		if entry.IsDir() || !strings.EqualFold(filepath.Ext(entry.Name()), ".zip") {
			continue
		}
		if only != "" && !strings.Contains(entry.Name(), only) {
			continue
		}
		name := entry.Name()
		t.Run(name, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join(directory, name))
			if err != nil {
				t.Fatal(err)
			}
			if isNativePackageArchive(data) {
				t.Skip("the earlier KTF package, which these rungs do not drive")
			}
			// A probe measures what the guest computes, not how long it takes,
			// so it runs a manual clock jumped to each next deadline: the same
			// sequence of guest work at no real cost.
			session, err := StartSession(context.Background(), data, SessionOptions{
				MaxSteps: localAcceptanceMaxSteps(t),
				Clock:    NewManualClock(time.Time{}),
				// Saves go to the test's own directory. These rungs run long
				// enough for a title to reach a write, and a probe must not
				// read or write the progress a person made playing.
				SaveRoot: t.TempDir(),
			})
			if err != nil {
				t.Fatalf("start: %v", err)
			}
			defer session.Close()
			ask(t, session)
		})
		ran++
	}
	if ran == 0 {
		t.Skip("no local KTF archives")
	}
}

// tickToFirstFrame ticks until the title flushes a screen with something lit
// in it, the same condition the frame rung below uses.
//
// It answers why it stopped as well as whether it painted. "Nothing painted"
// covers a title that ran out of ticks, one that ended itself, and one whose
// first tick refused, and a rung that reported all three the same way would
// send whoever read it to the wrong place.
func tickToFirstFrame(t *testing.T, session *Session) (painted bool, ticks int, why string) {
	t.Helper()
	for ; ticks < localLadderBootTicks; ticks++ {
		frame, _, _, flushes := session.Frame()
		if flushes > 0 && frameHasContent(frame) {
			return true, ticks, ""
		}
		progressed, err := session.Tick(context.Background())
		if err != nil {
			if errors.Is(err, ErrGuestExited) {
				return false, ticks, "the title ended itself before it painted"
			}
			return false, ticks, fmt.Sprintf("the title refused a tick before it painted: %v", err)
		}
		session.SkipToNextDeadline()
		if !progressed {
			if _, pending := session.NextDeadline(); !pending {
				return false, ticks, "the title had nothing left to do before it painted"
			}
		}
	}
	return false, ticks, "nothing was painted"
}

// tickFor advances a session and reports how far it got. A round that did
// nothing with nothing due is a title that has stopped, and stopping there is
// what makes "it ran the whole window" mean something.
func tickFor(session *Session, ticks int) (int, error) {
	for ran := 0; ran < ticks; ran++ {
		progressed, err := session.Tick(context.Background())
		if err != nil {
			return ran, err
		}
		session.SkipToNextDeadline()
		if !progressed {
			if _, pending := session.NextDeadline(); !pending {
				return ran, nil
			}
		}
	}
	return ticks, nil
}

// tickUntilSettled waits for the screen to stop changing on its own.
func tickUntilSettled(session *Session) (settled bool, waited int) {
	digest := session.FrameDigest()
	steady := 0
	for ; waited < localLadderSettleLimit; waited++ {
		if _, err := session.Tick(context.Background()); err != nil {
			return false, waited
		}
		session.SkipToNextDeadline()
		current := session.FrameDigest()
		if current != digest {
			digest = current
			steady = 0
			continue
		}
		steady++
		if steady >= localLadderSettleRuns {
			return true, waited
		}
	}
	return false, waited
}

// pressAndWatch holds one key down, releases it, and reports whether what the
// title draws differs from what it was drawing before the key.
func pressAndWatch(session *Session, key int32, baseline uint64, hold, after int) (bool, int, error) {
	ctx := context.Background()
	if err := session.SendKey(ctx, KeyPressed, key); err != nil {
		return false, 0, err
	}
	elapsed := 0
	watch := func(ticks int) (bool, error) {
		for index := 0; index < ticks; index++ {
			if _, err := session.Tick(ctx); err != nil {
				return false, err
			}
			session.SkipToNextDeadline()
			elapsed++
			if session.FrameDigest() != baseline {
				return true, nil
			}
		}
		return false, nil
	}
	changed, err := watch(hold)
	if err != nil {
		return false, elapsed, err
	}
	if releaseErr := session.SendKey(ctx, KeyReleased, key); releaseErr != nil && !changed {
		return false, elapsed, releaseErr
	}
	if changed {
		return true, elapsed, nil
	}
	changed, err = watch(after)
	return changed, elapsed, err
}

// localLadderTicks lets a window be widened from the environment, for the
// investigation that follows a rung nobody expected an archive to miss.
func localLadderTicks(t *testing.T, name string, fallback int) int {
	t.Helper()
	value := os.Getenv(name)
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		t.Fatalf("invalid %s %q", name, value)
	}
	return parsed
}
