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
// **A screen that a key did not move is not always a screen that answers no
// key.** A title whose opening runs longer than the press window answers
// nothing because its opening is still playing, so a screen that answered no
// key is watched with nothing held, and if it moves on its own, what it moved
// to is settled and asked in turn. Widening the press window instead would
// have let the opening's own next screen land inside it and be credited to the
// key; `internal/ladder` has the argument.
//
// **That watch happens on the last round too, and it used to not.** The rung
// asks a fixed number of screens, and it left the loop at the last one without
// watching it — so the single screen whose verdict actually gets reported was
// the single screen nobody had looked at with nothing held. Five archives were
// failed there for answering no key and not one of them was stopped: each
// leaves that screen by itself, and four go on to a menu, a title screen or a
// prologue that answers a key. A screen still moving when the rung runs out of
// screens is now reported the way a screen that never settles is — as
// unanswerable rather than as a failure — because "no key changed the screen"
// is a claim about the title and this is not evidence for it. See
// `docs/ktf.md`, "Five titles the interactive rung failed".
//
// **A title is allowed to end its first launch.** Several here put the
// handset's own restart notice up before anything else: they write their save,
// tell the player to start the title again, and end. That is correct
// behaviour, and at this rung it is indistinguishable from a title that died —
// both stop the session. So an archive that ends itself is given the second
// launch the notice asked for, against the same save directory, and only an
// ending on that launch is reported. A probe that judged every title on a
// launch the platform treats as an installation would be measuring its own
// fresh directory. The notice does not always end the launch — one title on
// the sibling platform waits on it — so an archive that answered no key gets
// that second launch too, but only when the first one wrote something for the
// second to read. This is the same rule the sibling platform's ladder applies,
// and for the same reason: two platforms must not answer one question
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
	// Sized from the corpus rather than chosen, and re-sized once. The first
	// number was 4096: the openings measured then moved again within a few
	// hundred ticks of the press window closing, and 4096 was an order above
	// the largest of them. Then five archives failed this rung and **not one
	// of them was stopped** — driven with nothing held they leave the screen
	// they were failed on after 323, 2123, 4606, 8898 and **23934** ticks, and
	// four of the five go on to a screen a key plainly answers. So the budget
	// is a little under three times the largest of those. Widening it costs
	// nothing measurable, because only a screen that has already answered no
	// key ever waits here and the wait ends the tick the screen moves.
	localLadderOpeningTicks = 65536
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
	eachLocalKTFArchive(t, func(t *testing.T, _ string, launch func(*testing.T) *Session) {
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
				// The reason goes last: the sweep records the final line a
				// subtest printed, so counts printed after it are what the
				// report files this archive under. See `docs/testing.md`.
				t.Fatal(withDiagnosticCounts(session.Client.runtime.diagnosticCounts(), 40,
					"tick %d after the first frame: %v", ran, err))
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
	eachLocalKTFArchive(t, func(t *testing.T, saves string, launch func(*testing.T) *Session) {
		for launched := 1; ; launched++ {
			// What the save directory held before this launch. A launch that
			// answers no key is only worth repeating if it wrote something for
			// the repeat to read; see the relaunch below.
			had := ladder.Footprint(saves)
			session := launch(t)
			painted, ticks, why := tickToFirstFrame(t, session)
			if !painted {
				t.Skipf("%s in %d ticks, which the frame rung below this one reports", why, ticks)
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
			// playing is how long the last screen the rung asked took to move
			// on its own, and negative when it did not move at all. It is the
			// difference between a screen that answers no key and a screen
			// that was asked before the title was ready to be asked.
			ended, tried, rounds, moved, playing := "", []string(nil), 0, 0, -1
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
					//
					// The launch is named here as well as on the outcomes
					// below, because this is where a title that ended its
					// first launch and then played on its second lands, and a
					// report that left the relaunch off this line would say a
					// title was never asked rather than that it had to be
					// restarted first.
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
				// **The last round is watched like every round before it.**
				// The loop used to leave here, before the watch, so the one
				// screen whose verdict gets reported was the one screen
				// nobody looked at with nothing held — and "no key changed the
				// screen" is a claim about a title, made from a screen that
				// might have been about to change by itself. Five archives
				// were reported that way and none of them was stopped.
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
				if round == ladder.SettleRounds {
					// The rung has run out of screens to ask and this one was
					// still moving, so what it moved to was never asked.
					playing = opening
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
			unanswerable, why := openingReport(playing)
			report := fmt.Sprintf("no key changed the screen%s: tried %s, held %d ticks each over %d settled %s, %d flushes since it settled%s, %s",
				openingMovedOn(moved), strings.Join(tried, ", "), hold, rounds, screens(rounds),
				session.Flushes()-presents, afterRestart(launched), why)
			if unanswerable {
				t.Skip(report)
			}
			t.Fatal(report)
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
		code, known := KeyCodeByName(name)
		if !known {
			t.Fatalf("no key called %q", name)
		}
		answer.tried = append(answer.tried, name)
		changed, after, err := pressAndWatch(session, code, screen, hold, localLadderReleaseTicks)
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
		if _, err := session.Tick(context.Background()); err != nil {
			return false, waited, err
		}
		session.SkipToNextDeadline()
		if screen.Changed(session.FrameDigest()) {
			return true, waited, nil
		}
	}
	return false, waited, nil
}

// openingMovedOn says, in a report, whether the screen that answered no key was
// the one the title booted to or one its opening moved on to. The two are
// different defects and a line that spelled them the same way would send
// whoever read it to the wrong place.
func openingMovedOn(moved int) string {
	if moved == 0 {
		return ""
	}
	return fmt.Sprintf(", over %d %s its opening moved on to", moved, screens(moved))
}

// openingReport says what a settled screen that no key moved actually is, and
// whether it is a failure at all. `playing` is how long that screen took to
// move on its own with nothing held, and negative when it did not.
//
// The two answers are not two ways of saying one thing:
//
//   - **It moved on its own once the keys were done with it.** Its opening was
//     still playing, so the title was asked its question before it was ready
//     to be asked, and what it moved to is a screen nobody has measured. That
//     is unanswerable — the same answer this rung already gives a screen that
//     never settles, and for the same reason: not knowing and not working are
//     different answers.
//   - **It did not move on its own either.** That is a stopped screen, and "no
//     key changed it" is a claim about the title.
//
// Both sentences end in their reason, because the sweep files a subtest under
// the last line it printed; see `docs/testing.md`.
func openingReport(playing int) (unanswerable bool, why string) {
	if playing >= 0 {
		return true, fmt.Sprintf("and its opening was still playing: with nothing held it left that screen %d ticks later, so what it answers has not been measured yet", playing)
	}
	return false, fmt.Sprintf("and it did not move on its own in the %d ticks after", localLadderOpeningTicks)
}

// TestAScreenThatLeavesOnItsOwnIsNotAScreenThatAnsweredNoKey pins the judgment
// the rung's last round used to skip.
//
// Unlike the rungs above it this needs no archive, because what it checks is
// the decision rather than the driving: a screen that leaves by itself has not
// refused the keys, and reporting it as one is a claim about a title made from
// a screen that was still playing its opening. Five archives were reported
// that way before the watch was moved onto the last round.
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

// eachLocalKTFArchive runs one subtest per archive in the local corpus, and
// hands the rung a way to launch it rather than a launched session, so that a
// rung meeting the restart notice can take the launch it asked for. Every
// launch of one archive shares one save directory, which is what makes the
// second one a second run rather than another first.
//
// One subtest per archive is what lets a report name the archive a refusal
// came from; a count at the end of a log says a number where a report needs a
// name.
func eachLocalKTFArchive(t *testing.T, ask func(t *testing.T, saves string, launch func(*testing.T) *Session)) {
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
			ask(t, saves, func(t *testing.T) *Session {
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
