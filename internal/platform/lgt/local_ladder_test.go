package lgt

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

	"github.com/movingwoo/wfeature/internal/ladder"
	"github.com/movingwoo/wfeature/internal/platform/ktf"
)

// Two rungs above a first frame.
//
// The boot probe beside this one stops at the moment a title asks to present
// something with a lit pixel in it, and that is not far enough to tell a title
// that plays from one that painted its opening screen and then stopped
// answering. A run that ends without an error is not evidence either: a loop
// that has stopped doing anything ticks quietly for as long as it is asked to.
//
// So two more rungs, the same two the other platforms already have:
//
//   - sustained: it kept running past its first frame, without an error and
//     without ending itself, for a window well past the one it took to paint.
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
// A screen that never settles is reported as unanswerable rather than as a
// failure. Not knowing and not working are different answers, and a ladder
// that spells them the same way turns an unread question into a defect.
//
// A screen that a key did not move is not always a screen that answers no key.
// A title whose opening runs longer than the press window answers nothing
// because its opening is still playing, so a screen that answered no key is
// watched with nothing held, and if it moves on its own, what it moved to is
// settled and asked in turn. Widening the press window instead would have let
// the opening's own next screen land inside it and be credited to the key;
// `internal/ladder` has the argument.
//
// **A title on this platform is allowed to end its first launch.** Several
// here put the handset's own restart notice up before anything else: they
// write their save, tell the player to start the title again, and end. That is
// correct behaviour, and at this rung it is indistinguishable from a title
// that died — both stop the session. So an archive that ends itself is given
// the second launch the notice asked for, against the same save directory, and
// only an ending on that launch is reported. A probe that judged every title
// on a launch the platform treats as an installation would be measuring its
// own fresh directory.
//
// **The notice does not always end the launch.** One title here puts it up and
// waits on it: it settles on a still screen, no key moves it, and launched a
// second time against the same save directory it plays and `fire` answers. So
// an archive that answered no key gets that second launch too — but only when
// the first launch wrote something for the second to read, because a rerun
// over unchanged bytes is the same run rather than a second one.
//
// The clock here is virtual — a tick advances the guest by the session's tick
// and nothing else does — so these windows cost guest work rather than
// seconds, and the archives run one at a time.
//
// Both are opt-in like the rest of the local probes: real archives are ignored
// local data rather than fixtures.

const (
	// How far a title is given to paint its first frame before the rungs above
	// it have anything to measure. The boot rung below asks for a frame within
	// 60 ticks; this is several times that, because a rung that reports "it
	// never painted" for a title the rung below passed would be reporting a
	// difference between two windows rather than anything about the title.
	localLadderBootTicks = 300
	// How long a title has to keep running for after that. It is several times
	// the window most titles take to paint, and a title still going at the end
	// of it is running rather than coasting to a stop.
	localLadderSustainTicks = 300
	// How long to wait for that. A title still animating at the end of this is
	// one whose answer to a key cannot be read from a frame, so it is reported
	// as unanswerable rather than as a failure.
	localLadderSettleLimit = 240
	// How long a key is held. **A press of a single tick is missed by some
	// titles**, which read the pad on their own schedule rather than on the
	// one the event arrived by, so the hold is generous.
	localLadderHoldTicks = 16
	// How long to keep ticking after the key is released. What a key starts is
	// often a transition rather than an immediate redraw.
	localLadderReleaseTicks = 48
	// How long a screen that answered no key is given to move on its own
	// before the rung reports that it answers no key.
	//
	// A title whose opening runs longer than the press window has not refused
	// the keys: it has not yet been asked a question it can answer, and the
	// evidence for that is a screen that keeps moving with nothing held. See
	// `internal/ladder` for why this is waited out rather than folded into the
	// press window — a press window long enough to outlast an opening credits
	// the opening's own next screen to the key.
	//
	// Sized from the corpus rather than chosen: the openings measured here
	// move again within a few hundred ticks of the press window closing, and
	// this is an order above the largest of them, which costs nothing because
	// only a title that has already answered no key ever spends it.
	localLadderOpeningTicks = 4096
	// How many launches an archive gets. Two, because the first one is what
	// the platform's restart notice ends; a third would be a probe hoping.
	localLadderLaunches = 2
)

// The keys tried, in the order they are tried, by the names a route script
// uses. A title of this era answers a soft key or the fire button on its
// opening screen; a few answer only the keypad. The first one that moves the
// screen is the answer, and which one it was is worth logging, because it is
// the key a route for that archive will have to start with.
//
// The names are resolved through the table the CLI's LGT route path already
// resolves them with, rather than through a second copy written here: `fire`
// must not mean one thing to a route and another to a rung.
var localLadderKeys = []string{"fire", "soft1", "soft2", "5", "down"}

// TestLocalLGTArchivesSustainAFrame asks whether a title that painted keeps
// running afterwards.
//
//	WFEATURE_LGT_SUSTAINED_ACCEPTANCE=1 go test -run TestLocalLGTArchivesSustainAFrame -v ./internal/platform/lgt
func TestLocalLGTArchivesSustainAFrame(t *testing.T) {
	if os.Getenv("WFEATURE_LGT_SUSTAINED_ACCEPTANCE") != "1" {
		t.Skip("set WFEATURE_LGT_SUSTAINED_ACCEPTANCE=1 to run ignored local LGT archives past their first frame")
	}
	sustain := localLadderTicks(t, "WFEATURE_LGT_SUSTAIN_TICKS", localLadderSustainTicks)
	eachLocalLGTArchive(t, func(t *testing.T, _ string, launch func(*testing.T) *Session) {
		for launched := 1; ; launched++ {
			session := launch(t)
			painted, ticks, why := tickToFirstFrame(session)
			if !painted {
				// The boot rung already reports this archive, and reporting it
				// twice would count one defect as two.
				t.Skipf("%s in %d ticks, which the boot rung below this one reports", why, ticks)
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
				t.Fatalf("tick %d after the first frame: %v\n\n%s", ran, err, FormatSVCTrace(session.SVCTrace()))
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

// TestLocalLGTArchivesAnswerAKey asks whether a key changes what a title
// draws, which is the first rung that cannot be reached by a title that has
// stopped.
//
//	WFEATURE_LGT_INTERACTIVE_ACCEPTANCE=1 go test -run TestLocalLGTArchivesAnswerAKey -v ./internal/platform/lgt
func TestLocalLGTArchivesAnswerAKey(t *testing.T) {
	if os.Getenv("WFEATURE_LGT_INTERACTIVE_ACCEPTANCE") != "1" {
		t.Skip("set WFEATURE_LGT_INTERACTIVE_ACCEPTANCE=1 to send keys to ignored local LGT archives")
	}
	hold := localLadderTicks(t, "WFEATURE_LGT_HOLD_TICKS", localLadderHoldTicks)
	eachLocalLGTArchive(t, func(t *testing.T, saves string, launch func(*testing.T) *Session) {
		for launched := 1; ; launched++ {
			// What the save directory held before this launch. A launch that
			// answers no key is only worth repeating if it wrote something for
			// the repeat to read; see the relaunch below.
			had := ladder.Footprint(saves)
			session := launch(t)
			painted, ticks, why := tickToFirstFrame(session)
			if !painted {
				t.Skipf("%s in %d ticks, which the boot rung below this one reports", why, ticks)
			}
			presents := session.Flushes()
			// The screen is settled and asked, and if it answers nothing and
			// then moves on its own, the screen it moved to is settled and
			// asked in turn: a title whose opening outlasts the press window
			// answered nothing because its opening was still playing. See
			// `internal/ladder`.
			//
			// A title that ends itself does so in three places now — waiting
			// to settle, under a held key, and waiting for its opening to move
			// on — and each is said differently, because "it ended itself" is
			// the same sentence about three different moments.
			ended, tried, rounds, moved := "", []string(nil), 0, 0
			for round := 1; round <= ladder.SettleRounds; round++ {
				rounds = round
				screen, settled, waited, err := tickUntilSettled(session)
				if errors.Is(err, ErrGuestExited) {
					ended = fmt.Sprintf("%d ticks after its first frame, before the screen settled", waited)
					break
				}
				if err != nil {
					t.Fatalf("tick %d while waiting for the screen to settle: %v", waited, err)
				}
				if !settled {
					// A screen that never stops changing on its own cannot
					// answer this question: a change after a key would have
					// happened anyway.
					t.Skipf("%s%s", screen.Unsettled(waited), afterRestart(launched))
				}
				answer, after, stopped, err := pressEachKey(t, session, screen, hold)
				if err != nil {
					t.Fatalf("holding a key: %v", err)
				}
				tried = answer.tried
				if answer.key != "" {
					t.Logf("%s changed the screen after %d ticks (settled after %d on round %d of %d, %d screens into its opening, flushes %d → %d)%s",
						answer.key, after, waited, round, ladder.SettleRounds, moved, presents, session.Flushes(), afterRestart(launched))
					return
				}
				if stopped != "" {
					ended = fmt.Sprintf("while %s was held", stopped)
					break
				}
				if round == ladder.SettleRounds {
					break
				}
				left, opening, err := tickUntilScreenMoves(session, screen, localLadderOpeningTicks)
				if errors.Is(err, ErrGuestExited) {
					ended = fmt.Sprintf("%d ticks into waiting for its opening to move on", opening)
					break
				}
				if err != nil {
					t.Fatalf("tick %d while waiting for the opening to move on: %v", opening, err)
				}
				if !left {
					break
				}
				moved++
			}
			if ended != "" {
				if launched < localLadderLaunches {
					continue
				}
				t.Fatalf("the title ended itself %s, on its second launch", ended)
			}
			// A title that put the handset's first-run notice up and **waited
			// on it** rather than ending is a title asked its question before
			// it was ready to be asked, and it looks like this: no key moves
			// it, and the launch wrote a save. It gets the second launch the
			// notice asked for. Over an unchanged save directory it does not,
			// because a rerun that reads the same bytes is the same run. See
			// `internal/ladder`.
			if launched < localLadderLaunches && ladder.Footprint(saves) != had {
				t.Logf("no key changed the screen and this launch wrote to its save directory, so it is being launched again")
				continue
			}
			t.Fatalf("no key changed the screen%s: tried %s, held %d ticks each over %d settled %s, %d flushes since it settled%s",
				openingMovedOn(moved), strings.Join(tried, ", "), hold, rounds, screens(rounds),
				session.Flushes()-presents, afterRestart(launched))
		}
	})
}

// A keyAnswer is which key moved a settled screen, and which were put to it.
type keyAnswer struct {
	key   string
	tried []string
}

// pressEachKey holds each key in turn against one settled screen and reports
// the first that drew something the screen never held. The third value names
// the key the title ended itself under, which is a different answer from "no
// key moved it" and is the caller's to relaunch on.
func pressEachKey(t *testing.T, session *Session, screen *ladder.Watcher, hold int) (keyAnswer, int, string, error) {
	t.Helper()
	answer := keyAnswer{}
	for _, name := range localLadderKeys {
		code, known := ktf.KeyCodeByName(name)
		if !known {
			t.Fatalf("no key called %q", name)
		}
		answer.tried = append(answer.tried, name)
		changed, after, err := pressAndWatch(session, uint32(code), screen, hold, localLadderReleaseTicks)
		if errors.Is(err, ErrGuestExited) {
			return answer, after, name, nil
		}
		if err != nil {
			return answer, after, "", err
		}
		if changed {
			answer.key = name
			return answer, after, "", nil
		}
	}
	return answer, 0, "", nil
}

// tickUntilScreenMoves ticks with nothing held and reports whether the screen
// leaves the set it settled on. That is a title whose opening is still
// running: what it moves to is a screen the rung has not asked yet.
func tickUntilScreenMoves(session *Session, screen *ladder.Watcher, budget int) (moved bool, waited int, err error) {
	for ; waited < budget; waited++ {
		if err := session.Tick(context.Background()); err != nil {
			return false, waited, err
		}
		if screen.Changed(session.FrameDigest()) {
			return true, waited, nil
		}
	}
	return false, waited, nil
}

// openingMovedOn says, in a failure, whether the screen that answered no key
// was the one the title booted to or one its opening moved on to. The two are
// different defects and a line that spelled them the same way would send
// whoever read it to the wrong place.
func openingMovedOn(moved int) string {
	if moved == 0 {
		return fmt.Sprintf(", and it did not move on its own in the %d ticks after", localLadderOpeningTicks)
	}
	return fmt.Sprintf(", over %d %s its opening moved on to", moved, screens(moved))
}

// screens is "screen" or "screens", so a count reads as a sentence.
func screens(count int) string {
	if count == 1 {
		return "screen"
	}
	return "screens"
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

// eachLocalLGTArchive runs one subtest per archive in the local corpus, and
// hands the rung a way to launch it rather than a launched session, so that a
// rung meeting the restart notice can take the launch it asked for. Every
// launch of one archive shares one save directory, which is what makes the
// second one a second run rather than another first.
//
// One subtest per archive is what lets a report name the archive a refusal
// came from; a count at the end of a log says a number where a report needs a
// name.
func eachLocalLGTArchive(t *testing.T, ask func(t *testing.T, saves string, launch func(*testing.T) *Session)) {
	t.Helper()
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate LGT acceptance test source")
	}
	directory := filepath.Join(filepath.Dir(source), "..", "..", "..", "var", "games", "lgt")
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatalf("read local LGT game directory: %v", err)
	}
	ran := 0
	for _, entry := range entries {
		if entry.IsDir() || !strings.EqualFold(filepath.Ext(entry.Name()), ".zip") {
			continue
		}
		name := entry.Name()
		t.Run(name, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join(directory, name))
			if err != nil {
				t.Fatalf("read archive: %v", err)
			}
			// Saves go to the test's own directory. These rungs run long
			// enough for a title to reach a write, and a probe must not read
			// or write the progress a person made playing. One directory for
			// every launch of this archive: a second launch has to see what
			// the first one wrote or it is not a second run.
			saves := t.TempDir()
			ask(t, saves, func(t *testing.T) *Session {
				t.Helper()
				ctx := context.Background()
				session, err := StartSession(ctx, data, SessionOptions{
					SaveRoot: saves,
					TraceSVC: 16,
				})
				if err != nil {
					if errors.Is(err, ErrJavaAppUnsupported) {
						t.Skip("LGT Java app, which this platform does not support")
					}
					var failure *StartFailure
					if errors.As(err, &failure) && len(failure.Trace) > 0 {
						t.Fatalf("start: %v\n\n%s", err, FormatSVCTrace(failure.Trace))
					}
					t.Fatalf("start: %v", err)
				}
				t.Cleanup(func() { session.Close(context.Background()) })
				return session
			})
		})
		ran++
	}
	if ran == 0 {
		t.Skip("no local LGT archives")
	}
}

// tickToFirstFrame ticks until the title flushes a screen with something lit
// in it, which is the condition the boot rung below uses.
//
// It answers why it stopped as well as whether it painted. "Nothing painted"
// covers a title that ran out of ticks, one that ended itself, and one whose
// first tick refused, and a rung that reported all three the same way would
// send whoever read it to the wrong place.
func tickToFirstFrame(session *Session) (painted bool, ticks int, why string) {
	for ; ticks < localLadderBootTicks; ticks++ {
		if session.Flushes() > 0 && framePainted(session) {
			return true, ticks, ""
		}
		if err := session.Tick(context.Background()); err != nil {
			if errors.Is(err, ErrGuestExited) {
				return false, ticks, "the title ended itself before it painted"
			}
			return false, ticks, fmt.Sprintf("the title refused a tick before it painted: %v", err)
		}
	}
	return false, ticks, "nothing was painted"
}

// framePainted reports whether the presented frame has anything lit in it. An
// all-black screen is what a title that flushed before it drew presents, and
// counting it as a first frame would put a title on a rung it never reached.
func framePainted(session *Session) bool {
	frame, width, height, _ := session.Frame()
	if width == 0 || height == 0 {
		return false
	}
	for offset := 0; offset+3 < len(frame); offset += 4 {
		if frame[offset] != 0 || frame[offset+1] != 0 || frame[offset+2] != 0 {
			return true
		}
	}
	return false
}

// tickFor advances a session and reports how far it got.
func tickFor(session *Session, ticks int) (int, error) {
	for ran := 0; ran < ticks; ran++ {
		if err := session.Tick(context.Background()); err != nil {
			return ran, err
		}
	}
	return ticks, nil
}

// tickUntilSettled waits for the screen to stop changing on its own. A title
// that ends itself while it is being waited on is a different answer from one
// that is still animating, so the error travels rather than being folded into
// "not settled".
func tickUntilSettled(session *Session) (screen *ladder.Watcher, settled bool, waited int, err error) {
	screen = &ladder.Watcher{}
	screen.Observe(session.FrameDigest())
	for ; waited < localLadderSettleLimit; waited++ {
		if err := session.Tick(context.Background()); err != nil {
			return screen, false, waited, err
		}
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
func pressAndWatch(session *Session, key uint32, screen *ladder.Watcher, hold, after int) (bool, int, error) {
	ctx := context.Background()
	session.SendKey(true, key)
	elapsed := 0
	released := false
	// The release is sent whatever happens, because the pad this session keeps
	// is a handset's thumb: a key left held changes what the next key means.
	defer func() {
		if !released {
			session.SendKey(false, key)
		}
	}()
	watch := func(ticks int) (bool, error) {
		for index := 0; index < ticks; index++ {
			if err := session.Tick(ctx); err != nil {
				return false, err
			}
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
	session.SendKey(false, key)
	released = true
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
