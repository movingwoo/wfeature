package ktf

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/movingwoo/wfeature/internal/ladder"
)

// A stopped screen is read by trying every key, and by not pressing one.
//
// The interactive rung puts five keys to a settled screen and reports that the
// screen answers no key when none of them moves it. That sentence has three
// possible readings — the title wants something we never make true, it wants a
// key the rung never tries, or it is a screen where nothing should happen —
// and the rung cannot tell them apart, because it stops after five keys and
// because it never measures what the same span of ticks does with nothing
// held.
//
// This probe tells them apart. It takes one archive by absolute path and walks
// the rung's own shape — launch, first frame, settle, ask, wait for the
// opening to move on, settle again — with three differences that are the whole
// point:
//
//   - it holds **every** key this platform names, not five;
//   - it watches before it presses, not after. A key can only be credited for
//     a change on a screen that was provably not going to change anyway, and
//     the only proof of that is the screen sitting still, with nothing held,
//     for a window past the one the sweep spends. Pressing first is the rung's
//     order and it cannot separate the two: pointed at the first archive in
//     that order the probe answered that `hangup` moved the screen, which no
//     title of this era acts on — the logo was going to advance at that tick.
//     Interleaving an idle window beside every key does not fix it either;
//     another archive's drift landed in a press window instead of the idle
//     window next to it and the same screen was credited to `5`. A control
//     that shares a timeline with what it is controlling for is not a control;
//   - `WFEATURE_KTF_STOPPED_IDLE` drops the keys entirely and reports only how
//     long each screen takes to leave by itself, which is what says whether a
//     title's opening ever finishes.
//
// All-the-same-digest is itself a result: it says the screen is not waiting
// for a key. So is a screen that leaves on its own — it says the question was
// asked too early. See `docs/ktf.md`, "Five titles the interactive rung
// failed", for what this was written to read and what it read.
//
//	WFEATURE_KTF_STOPPED_PROBE=1 \
//	WFEATURE_KTF_STOPPED_ARCHIVE=/absolute/path.zip \
//	go test -run TestLocalKTFStoppedScreenAnswersEveryKey -v ./internal/platform/ktf
//
// The rest is optional and named for what it widens: STOPPED_ROUNDS how many
// screens are asked, STOPPED_QUIET how long each must sit still first,
// OPENING_TICKS how long it is given to leave, STOPPED_KEYS which keys are
// held, STOPPED_SWEEP_ROUND the first round to press anything on,
// STOPPED_FRAMEDIR where each screen is written as a PNG, STOPPED_SAVES a save
// directory that outlives the run, STOPPED_LOG the guest's own printk, and
// STOPPED_TRACE how many boundary crossings to print at the end — which is
// what says what a stopped title is looping on.
func TestLocalKTFStoppedScreenAnswersEveryKey(t *testing.T) {
	if os.Getenv("WFEATURE_KTF_STOPPED_PROBE") != "1" {
		t.Skip("set WFEATURE_KTF_STOPPED_PROBE=1 to sweep every key against one archive's settled screen")
	}
	archive := os.Getenv("WFEATURE_KTF_STOPPED_ARCHIVE")
	if archive == "" {
		t.Skip("set WFEATURE_KTF_STOPPED_ARCHIVE to an absolute archive path")
	}
	hold := localLadderTicks(t, "WFEATURE_KTF_HOLD_TICKS", localLadderHoldTicks)
	release := localLadderTicks(t, "WFEATURE_KTF_RELEASE_TICKS", localLadderReleaseTicks)
	launches := localLadderTicks(t, "WFEATURE_KTF_LAUNCHES", localLadderLaunches)
	rounds := localLadderTicks(t, "WFEATURE_KTF_STOPPED_ROUNDS", ladder.SettleRounds)
	sweepAt := 0
	if value := os.Getenv("WFEATURE_KTF_STOPPED_SWEEP_ROUND"); value != "" {
		sweepAt = localLadderTicks(t, "WFEATURE_KTF_STOPPED_SWEEP_ROUND", 0)
	}
	opening := localLadderTicks(t, "WFEATURE_KTF_OPENING_TICKS", localLadderOpeningTicks)
	quiet := localLadderTicks(t, "WFEATURE_KTF_STOPPED_QUIET", opening)
	idle := 0
	if value := os.Getenv("WFEATURE_KTF_STOPPED_IDLE"); value != "" {
		idle = localLadderTicks(t, "WFEATURE_KTF_STOPPED_IDLE", 0)
	}
	frames := os.Getenv("WFEATURE_KTF_STOPPED_FRAMEDIR")
	keys := allProbeKeys()
	if named := os.Getenv("WFEATURE_KTF_STOPPED_KEYS"); named != "" {
		keys = strings.Split(named, ",")
	}

	data, err := os.ReadFile(archive)
	if err != nil {
		t.Fatal(err)
	}
	saves := t.TempDir()
	if root := os.Getenv("WFEATURE_KTF_STOPPED_SAVES"); root != "" {
		saves = root
	}
	for launched := 1; launched <= launches; launched++ {
		had := ladder.Footprint(saves)
		session, err := StartSession(context.Background(), data, SessionOptions{
			MaxSteps:   localAcceptanceMaxSteps(t),
			Clock:      NewManualClock(time.Time{}),
			SaveRoot:   saves,
			Logger:     probeLogger(t),
			TraceLimit: localLadderTicks(t, "WFEATURE_KTF_STOPPED_TRACE", 1) - 1,
		})
		if err != nil {
			t.Fatalf("launch %d start: %v", launched, err)
		}
		painted, ticks, why := tickToFirstFrame(t, session)
		if !painted {
			t.Logf("launch %d: %s in %d ticks", launched, why, ticks)
			session.Close()
			if ladder.Footprint(saves) != had {
				continue
			}
			return
		}
		t.Logf("launch %d: painted after %d ticks, flushes %d", launched, ticks, session.Flushes())
		answered := probeLaunch(t, session, launched, rounds, hold, release, opening, quiet, idle, sweepAt, keys, frames)
		t.Logf("launch %d: diagnostics %s", launched, topCounts(session.Client.runtime.diagnosticCounts(), 60))
		if trace := session.Diagnostics().Trace; len(trace) > 0 {
			t.Logf("launch %d: the last %d boundary crossings, oldest first:", launched, len(trace))
			for _, entry := range trace {
				t.Logf("  %d %s", entry.Sequence, entry.Event)
			}
		}
		session.Close()
		if answered {
			return
		}
		if wrote := ladder.Footprint(saves); wrote == had {
			t.Logf("launch %d wrote nothing to its save directory, so a second launch would read the same bytes", launched)
			return
		}
		t.Logf("launch %d wrote to its save directory", launched)
	}
}

// probeLaunch walks one launch through the rung's rounds and reports whether
// anything at all moved a settled screen.
func probeLaunch(t *testing.T, session *Session, launched, rounds, hold, release, opening, quiet, idle, sweepAt int, keys []string, frames string) bool {
	t.Helper()
	for round := 1; round <= rounds; round++ {
		screen, settled, waited, err := tickUntilSettled(session)
		if err != nil {
			t.Logf("launch %d round %d: settling refused after %d ticks: %v", launched, round, waited, err)
			return false
		}
		if !settled {
			t.Logf("launch %d round %d: %s", launched, round, screen.Unsettled(waited))
			return false
		}
		t.Logf("launch %d round %d: settled after %d ticks on %d frame(s) %v, flushes %d",
			launched, round, waited, len(screen.Frames()), screen.Frames(), session.Flushes())
		if frames != "" {
			saveProbeFrame(t, session, filepath.Join(frames, fmt.Sprintf("launch%d-round%d-settled.png", launched, round)))
		}
		if idle > 0 {
			// The control: the same span with nothing held.
			left, waited, err := tickUntilScreenMoves(session, screen, idle)
			t.Logf("launch %d round %d: idle control — moved=%v after %d of %d ticks with nothing held (err %v)",
				launched, round, left, waited, idle, err)
			if left && frames != "" {
				saveProbeFrame(t, session, filepath.Join(frames, fmt.Sprintf("launch%d-round%d-idle.png", launched, round)))
			}
			if left {
				continue
			}
		} else if sweepAt <= 0 || round >= sweepAt {
			// **The wait comes before the keys, not after them.** A key can
			// only be credited for a change on a screen that provably was not
			// going to change anyway, and the only proof of that is the
			// screen sitting still, with nothing held, for a window well past
			// the one the sweep spends. Pressing first and waiting afterwards
			// — which is the order the rung uses — cannot separate the two:
			// one archive here advances its logo on its own 2120 ticks after
			// it settles, and 2120 ticks after it settles is somewhere in the
			// middle of a sweep. Interleaving an idle window per key does not
			// fix it either; the drift simply lands in a press window instead
			// of the idle window beside it, and it did.
			left, waited, err := tickUntilScreenMoves(session, screen, quiet)
			if err != nil {
				t.Logf("launch %d round %d: the quiet window refused after %d ticks: %v", launched, round, waited, err)
				return false
			}
			if left {
				t.Logf("launch %d round %d: the screen moved on its own after %d of %d quiet ticks, so its opening is still playing and no key can be credited here",
					launched, round, waited, quiet)
				if frames != "" {
					saveProbeFrame(t, session, filepath.Join(frames, fmt.Sprintf("launch%d-round%d-drift.png", launched, round)))
				}
				continue
			}
			t.Logf("launch %d round %d: the screen sat still for all %d quiet ticks, so what a key does next is the key's",
				launched, round, quiet)
			moved, drifted, stopped := sweepEveryKey(t, session, screen, keys, hold, release, launched, round, frames)
			if stopped {
				return false
			}
			if moved != "" {
				t.Logf("launch %d round %d: %s moved the screen", launched, round, moved)
				return true
			}
			if drifted {
				continue
			}
		}
		left, spent, err := tickUntilScreenMoves(session, screen, opening)
		if err != nil {
			t.Logf("launch %d round %d: waiting for the opening to move on refused after %d ticks: %v",
				launched, round, spent, err)
			return false
		}
		t.Logf("launch %d round %d: opening moved on = %v after %d of %d ticks", launched, round, left, spent, opening)
		if !left {
			return false
		}
	}
	return false
}

// sweepEveryKey holds every named key against one settled screen and names the
// first that draws something the screen never held. The second value says the
// title stopped, which is not the same answer as "no key moved it".
func sweepEveryKey(t *testing.T, session *Session, screen *ladder.Watcher, keys []string, hold, release, launched, round int, frames string) (moved string, drifted, stopped bool) {
	t.Helper()
	for _, name := range keys {
		code, known := KeyCodeByName(name)
		if !known {
			t.Fatalf("no key called %q", name)
		}
		// The paired control. A key that "moved the screen" moved nothing if
		// the screen was going to move at that tick anyway, and the first
		// archive this was pointed at did exactly that — its logo advanced on
		// its own 2120 ticks after it settled, which landed under the eleventh
		// key the sweep held. So every key is preceded by an idle window of
		// the same length: only a screen that sat through the window before
		// the key can have the key credited for what happens in it.
		left, waited, err := tickUntilScreenMoves(session, screen, hold+release)
		if err != nil {
			t.Logf("  %-6s the control window before it refused after %d ticks: %v", name, waited, err)
			return "", false, true
		}
		if left {
			t.Logf("  %-6s the screen moved on its own %d ticks into the control window before this key, so it is not stopped and no key can be credited here",
				name, waited)
			if frames != "" {
				saveProbeFrame(t, session, filepath.Join(frames, fmt.Sprintf("launch%d-round%d-drift.png", launched, round)))
			}
			return "", true, false
		}
		before := session.Flushes()
		changed, after, err := pressAndWatch(session, code, screen, hold, release)
		flushed := session.Flushes() - before
		if errors.Is(err, ErrGuestExited) {
			t.Logf("  %-6s ended the title after %d ticks (%d flushes)", name, after, flushed)
			return "", false, true
		}
		if err != nil {
			t.Logf("  %-6s refused after %d ticks: %v", name, after, err)
			return "", false, true
		}
		if changed {
			t.Logf("  %-6s CHANGED the screen after %d ticks (%d flushes), with the %d idle ticks before it quiet",
				name, after, flushed, hold+release)
			if frames != "" {
				saveProbeFrame(t, session, filepath.Join(frames, fmt.Sprintf("launch%d-round%d-%s.png", launched, round, name)))
			}
			return name, false, false
		}
		t.Logf("  %-6s no change (%d flushes, digest %d)", name, flushed, session.FrameDigest())
	}
	return "", false, false
}

// allProbeKeys is every key name this platform resolves, in the order a person
// reading the log wants them: the pad and its buttons, then the keypad.
func allProbeKeys() []string {
	named := []string{"fire", "soft1", "soft2", "soft3", "up", "down", "left", "right", "clear", "call", "hangup"}
	for character := '0'; character <= '9'; character++ {
		named = append(named, string(character))
	}
	return append(named, "*", "#")
}

// topCounts renders the diagnostic counters largest first, so a probe's log
// says what the guest spent the run asking for.
func topCounts(counts map[string]uint32, limit int) string {
	type row struct {
		name  string
		count uint32
	}
	rows := make([]row, 0, len(counts))
	for name, count := range counts {
		rows = append(rows, row{name, count})
	}
	sort.Slice(rows, func(one, two int) bool {
		if rows[one].count != rows[two].count {
			return rows[one].count > rows[two].count
		}
		return rows[one].name < rows[two].name
	})
	if len(rows) > limit {
		rows = rows[:limit]
	}
	parts := make([]string, 0, len(rows))
	for _, entry := range rows {
		parts = append(parts, fmt.Sprintf("%s=%d", entry.name, entry.count))
	}
	return strings.Join(parts, " ")
}

// probeLogger is the guest's own voice — its printk lines and what the runtime
// says about the exceptions it throws — routed to the probe's log, and nil
// unless it was asked for. A stopped screen that says why it is stopped says
// it here.
func probeLogger(t *testing.T) *slog.Logger {
	if os.Getenv("WFEATURE_KTF_STOPPED_LOG") != "1" {
		return nil
	}
	return slog.New(slog.NewTextHandler(probeWriter{t}, &slog.HandlerOptions{Level: slog.LevelDebug}))
}

// probeWriter puts a log line in the test's own output, so one archive's run
// is one block a person can read top to bottom.
type probeWriter struct{ t *testing.T }

func (writer probeWriter) Write(line []byte) (int, error) {
	writer.t.Log(strings.TrimRight(string(line), "\n"))
	return len(line), nil
}

// saveProbeFrame writes what is on the screen now, for a person to look at.
func saveProbeFrame(t *testing.T, session *Session, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Logf("make frame directory: %v", err)
		return
	}
	frame, width, height, _ := session.Frame()
	writeFramePNG(t, path, frame, width, height)
}
