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

	"github.com/movingwoo/wfeature/internal/ladder"
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
// What counts as settled, and what a key is then compared against, is one
// judgment shared by all three platforms rather than three copies of one
// design: `internal/ladder`. A screen is settled when it is still, or when it
// cycles through a handful of frames and shows nothing it had not already
// shown — a blinking prompt is a screen waiting for input, and asking for the
// same frame several ticks in a row refused 46 archives that were doing
// exactly that. What the settled screen leaves behind is the set of frames it
// cycles through, and a key changed the screen when it draws something that
// set never held.
//
// **A title is allowed to end its first launch.** Several here put the
// handset's own restart notice up before anything else: they write their save,
// tell the player to start the title again, and end. That is correct
// behaviour, and at this rung it is indistinguishable from a title that died —
// both stop the session. So an archive that ends itself is given the second
// launch the notice asked for, against the same save directory, and only an
// ending on that launch is reported. A probe that judged every title on a
// launch the platform treats as an installation would be measuring its own
// fresh directory. This is the same rule the sibling platform's ladder
// applies, and for the same reason: two platforms must not answer one question
// differently.
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
	// How many launches an archive gets. Two, because the first one is what
	// the platform's restart notice ends; a third would be a probe hoping.
	localLadderLaunches = 2
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
	eachLocalKTFArchive(t, func(t *testing.T, launch func(*testing.T) *Session) {
		for launched := 1; ; launched++ {
			session := launch(t)
			painted, ticks, why := tickToFirstFrame(t, session)
			if !painted {
				// The frame rung already reports this archive, and reporting
				// it twice would count one defect as two.
				t.Skipf("%s in %d ticks, which the frame rung below this one reports", why, ticks)
			}
			before := session.Flushes()
			ran, err := tickFor(session, sustain)
			if errors.Is(err, ErrGuestExited) {
				if launched < localLadderLaunches {
					continue
				}
				t.Fatalf("the title ended itself %d ticks after its first frame, on its second launch", ran)
			}
			if err != nil {
				t.Fatalf("tick %d after the first frame: %v\ncounts:\n%s",
					ran, err, formatDiagnosticCounts(session.Client.runtime.diagnosticCounts(), 40))
			}
			if ran < sustain {
				t.Fatalf("nothing left to do %d ticks after the first frame, of %d asked for%s",
					ran, sustain, afterRestart(launched))
			}
			// A title that is still flushing is drawing; one that is not may
			// still be running a loop that draws only when something changes,
			// so this is logged rather than required.
			t.Logf("ran %d ticks past its first frame (which took %d), flushes %d → %d%s",
				ran, ticks, before, session.Flushes(), afterRestart(launched))
			return
		}
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
	eachLocalKTFArchive(t, func(t *testing.T, launch func(*testing.T) *Session) {
		for launched := 1; ; launched++ {
			session := launch(t)
			painted, ticks, why := tickToFirstFrame(t, session)
			if !painted {
				t.Skipf("%s in %d ticks, which the frame rung below this one reports", why, ticks)
			}
			screen, settled, waited, err := tickUntilSettled(session)
			if errors.Is(err, ErrGuestExited) {
				if launched < localLadderLaunches {
					continue
				}
				t.Fatalf("the title ended itself %d ticks after its first frame on its second launch, before the screen settled", waited)
			}
			if err != nil {
				t.Fatalf("tick %d while waiting for the screen to settle: %v", waited, err)
			}
			if !settled {
				// A screen that never stops changing on its own cannot answer
				// this question: a change after a key would have happened
				// anyway.
				//
				// The launch is named here as well as on the outcomes below,
				// because this is where a title that ended its first launch
				// and then played on its second lands, and a report that left
				// the relaunch off this line would say a title was never asked
				// rather than that it had to be restarted first.
				t.Skipf("%s%s", screen.Unsettled(waited), afterRestart(launched))
			}
			presents := session.Flushes()
			var tried []string
			ended := ""
			for _, name := range localLadderKeys {
				code, known := KeyCodeByName(name)
				if !known {
					t.Fatalf("no key called %q", name)
				}
				tried = append(tried, name)
				changed, after, err := pressAndWatch(session, code, screen, hold, localLadderReleaseTicks)
				if errors.Is(err, ErrGuestExited) {
					ended = name
					break
				}
				if err != nil {
					t.Fatalf("holding %s: %v", name, err)
				}
				if changed {
					t.Logf("%s changed the screen after %d ticks (settled after %d, flushes %d → %d)%s",
						name, after, waited, presents, session.Flushes(), afterRestart(launched))
					return
				}
			}
			if ended != "" {
				if launched < localLadderLaunches {
					continue
				}
				t.Fatalf("the title ended itself while %s was held, on its second launch", ended)
			}
			t.Fatalf("no key changed the screen: tried %s, held %d ticks each, %d flushes since it settled%s",
				strings.Join(tried, ", "), hold, session.Flushes()-presents, afterRestart(launched))
		}
	})
}

// afterRestart says, in a log line, that what is being reported is the second
// launch rather than the first. A rung that quietly relaunched would hide a
// title's first-launch behaviour from whoever reads the report.
func afterRestart(launched int) string {
	if launched < 2 {
		return ""
	}
	return ", on its second launch"
}

// eachLocalKTFArchive runs one subtest per archive in the local corpus, and
// hands the rung a way to launch it rather than a launched session, so that a
// rung meeting the restart notice can take the launch it asked for. Every
// launch of one archive shares one save directory, which is what makes the
// second one a second run rather than another first.
//
// One subtest per archive is what lets a report name the archive a refusal
// came from; a count at the end of a log says a number where a report needs a
// name.
func eachLocalKTFArchive(t *testing.T, ask func(t *testing.T, launch func(*testing.T) *Session)) {
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
			// Saves go to the test's own directory. These rungs run long
			// enough for a title to reach a write, and a probe must not read
			// or write the progress a person made playing. One directory for
			// every launch of this archive: a second launch has to see what
			// the first one wrote or it is not a second run.
			saves := t.TempDir()
			ask(t, func(t *testing.T) *Session {
				t.Helper()
				// A probe measures what the guest computes, not how long it
				// takes, so it runs a manual clock jumped to each next
				// deadline: the same sequence of guest work at no real cost.
				session, err := StartSession(context.Background(), data, SessionOptions{
					MaxSteps: localAcceptanceMaxSteps(t),
					Clock:    NewManualClock(time.Time{}),
					SaveRoot: saves,
				})
				if err != nil {
					t.Fatalf("start: %v", err)
				}
				t.Cleanup(func() { session.Close() })
				return session
			})
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
//
// A refusal is answered rather than folded into "it never settled": a title
// that ends here is the restart notice, and the caller relaunches it. Reported
// as an unsettled screen it would have been skipped instead — which is the
// same title recorded as a question nobody asked rather than as a title that
// needed its second launch.
func tickUntilSettled(session *Session) (screen *ladder.Watcher, settled bool, waited int, err error) {
	screen = &ladder.Watcher{}
	screen.Observe(session.FrameDigest())
	for ; waited < localLadderSettleLimit; waited++ {
		if _, err := session.Tick(context.Background()); err != nil {
			return screen, false, waited, err
		}
		session.SkipToNextDeadline()
		if screen.Observe(session.FrameDigest()) {
			return screen, true, waited, nil
		}
	}
	return screen, false, waited, nil
}

// pressAndWatch holds one key down, releases it, and reports whether the title
// draws content the settled screen never held. The comparison is against the
// set the screen was cycling through rather than against one frame of it: a
// blinking prompt differs from any single frame of itself.
func pressAndWatch(session *Session, key int32, screen *ladder.Watcher, hold, after int) (bool, int, error) {
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
			if screen.Changed(session.FrameDigest()) {
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
