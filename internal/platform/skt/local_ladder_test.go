package skt_test

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/platform/skt"
)

// Two rungs above a first frame.
//
// The boot probe beside this one stops at the moment a title has painted
// something, and that is not far enough to tell a title that plays from a
// title that painted its opening screen and then stopped answering. A run that
// ends without an error is not evidence either: a loop that has stopped doing
// anything ticks quietly for as long as it is asked to.
//
// So two more rungs:
//
//   - sustained: it kept running past its first frame, without an error and
//     without destroying itself.
//   - interactive: a key changed what it draws. The screen is waited on until
//     it settles first, because a change measured against a screen that was
//     already animating says nothing about the key.
//
// A tick here is real time — a MIDlet's threads sleep against the wall clock
// and there is no guest clock to multiply — so the windows are counted in
// ticks of the same length the boot probe uses, and the archives run in
// parallel.

const (
	// How long a title has to paint something before the rungs above have
	// anything to measure. This is the boot probe's own window.
	localLadderBootTicks = 300
	// How long it then has to keep running.
	localLadderSustainTicks = 200
	// A screen counts as settled once its pixels are unchanged for this many
	// consecutive ticks.
	localLadderSettleRuns = 8
	// How long to wait for that before deciding the title is animating and
	// cannot answer this question from a frame.
	localLadderSettleLimit = 150
	// How long a key is held. **A press of a single tick is missed by some
	// titles**, which read the pad on their own schedule rather than on the
	// one the event arrived by.
	localLadderHoldTicks = 16
	// How long to keep ticking after the key is released, because what a key
	// starts is often a transition rather than an immediate redraw.
	localLadderReleaseTicks = 48
	// The pause between ticks, which is the boot probe's.
	localLadderTickPause = 16 * time.Millisecond
)

// The keys tried, in the order they are tried. The pad values are the
// handset's, which is what a title of this era compares against.
var localLadderKeys = []struct {
	name string
	code int32
}{
	{"fire", skt.KeyCodeFire},
	{"5", '5'},
	{"down", skt.KeyCodeDown},
	{"right", skt.KeyCodeRight},
	{"call", skt.KeyCodeCall},
}

// TestLocalSKTArchivesSustainAFrame asks whether a title that painted keeps
// running afterwards.
//
//	WFEATURE_SKT_SUSTAINED_ACCEPTANCE=1 go test -run TestLocalSKTArchivesSustainAFrame -v ./internal/platform/skt
func TestLocalSKTArchivesSustainAFrame(t *testing.T) {
	if os.Getenv("WFEATURE_SKT_SUSTAINED_ACCEPTANCE") != "1" {
		t.Skip("set WFEATURE_SKT_SUSTAINED_ACCEPTANCE=1 to run ignored local SKT archives past their first frame")
	}
	sustain := localLadderTicks(t, "WFEATURE_SKT_SUSTAIN_TICKS", localLadderSustainTicks)
	eachLocalSKTArchive(t, func(t *testing.T, session *skt.Runtime, framebuffer *backend.MemoryFramebuffer) {
		painted, ticks, why := tickToFirstFrame(t, session, framebuffer)
		if !painted {
			// The boot rung below already reports this archive, and reporting
			// it twice would count one defect as two.
			t.Skipf("%s in %d ticks, which the boot rung below this one reports", why, ticks)
		}
		_, before := framebuffer.Snapshot()
		for tick := 0; tick < sustain; tick++ {
			if state := session.State(); state == skt.StateDestroyed {
				t.Fatalf("the MIDlet destroyed itself %d ticks after its first frame", tick)
			} else if state == skt.StateError {
				t.Fatalf("the MIDlet ended in error %d ticks after its first frame: %v", tick, session.Summary().Error)
			}
			session.AdvanceAudio()
			if err := session.RunPending(); err != nil {
				t.Fatalf("tick %d after the first frame: %v", tick, err)
			}
			time.Sleep(localLadderTickPause)
		}
		_, after := framebuffer.Snapshot()
		// A title that is still presenting is drawing; one that is not may be
		// running a loop that draws only when something changes, so this is
		// logged rather than required.
		t.Logf("ran %d ticks past its first frame (which took %d), presented %d more", sustain, ticks, after-before)
	})
}

// TestLocalSKTArchivesAnswerAKey asks whether a key changes what a title
// draws, which is the first rung a title that has stopped cannot reach.
//
//	WFEATURE_SKT_INTERACTIVE_ACCEPTANCE=1 go test -run TestLocalSKTArchivesAnswerAKey -v ./internal/platform/skt
func TestLocalSKTArchivesAnswerAKey(t *testing.T) {
	if os.Getenv("WFEATURE_SKT_INTERACTIVE_ACCEPTANCE") != "1" {
		t.Skip("set WFEATURE_SKT_INTERACTIVE_ACCEPTANCE=1 to send keys to ignored local SKT archives")
	}
	hold := localLadderTicks(t, "WFEATURE_SKT_HOLD_TICKS", localLadderHoldTicks)
	eachLocalSKTArchive(t, func(t *testing.T, session *skt.Runtime, framebuffer *backend.MemoryFramebuffer) {
		painted, ticks, why := tickToFirstFrame(t, session, framebuffer)
		if !painted {
			t.Skipf("%s in %d ticks, which the boot rung below this one reports", why, ticks)
		}
		settled, waited := tickUntilSettled(t, session, framebuffer)
		if !settled {
			t.Skipf("the screen was still changing after %d ticks, so a change after a key would prove nothing", waited)
		}
		baseline, presented := framebuffer.Snapshot()
		var tried []string
		for _, key := range localLadderKeys {
			tried = append(tried, key.name)
			changed, after, err := pressAndWatch(t, session, framebuffer, key.code, baseline, hold)
			if err != nil {
				t.Fatalf("holding %s: %v", key.name, err)
			}
			if changed {
				_, now := framebuffer.Snapshot()
				t.Logf("%s changed the screen after %d ticks (settled after %d, presented %d → %d)",
					key.name, after, waited, presented, now)
				return
			}
		}
		_, now := framebuffer.Snapshot()
		t.Fatalf("no key changed the screen: tried %s, held %d ticks each, %d presents since it settled",
			strings.Join(tried, ", "), hold, now-presented)
	})
}

// eachLocalSKTArchive runs one subtest per archive with a started session in
// front of it, so a rung is written as the question it asks rather than as
// another copy of the walk to it. One subtest per archive is what lets a
// report name the archive a refusal came from.
func eachLocalSKTArchive(t *testing.T, ask func(t *testing.T, session *skt.Runtime, framebuffer *backend.MemoryFramebuffer)) {
	t.Helper()
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate SKT acceptance test source")
	}
	directory := filepath.Join(filepath.Dir(source), "..", "..", "..", "var", "games", "skt")
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatalf("read local SKT game directory: %v", err)
	}
	ran := 0
	for _, entry := range entries {
		if entry.IsDir() || !strings.EqualFold(filepath.Ext(entry.Name()), ".zip") {
			continue
		}
		name := entry.Name()
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			data, err := os.ReadFile(filepath.Join(directory, name))
			if err != nil {
				t.Fatalf("read archive: %v", err)
			}
			archive, err := skt.Open(data)
			if err != nil {
				t.Fatalf("open archive: %v", err)
			}
			framebuffer, err := backend.NewMemoryFramebuffer(240, 320)
			if err != nil {
				t.Fatalf("framebuffer: %v", err)
			}
			// Saves go to the test's own directory: these rungs run long
			// enough for a title to reach a write, and a probe must not read
			// or write the progress a person made playing.
			session, err := skt.Start(archive, skt.Options{
				Framebuffer: framebuffer,
				SaveStore:   backend.NewDirectorySaveStore(t.TempDir()),
			})
			if err != nil {
				t.Fatalf("start: %v", err)
			}
			defer session.Destroy(true)
			ask(t, session, framebuffer)
		})
		ran++
	}
	if ran == 0 {
		t.Skip("no local SKT archives")
	}
}

// tickToFirstFrame runs until the title has presented a frame with something
// lit in it, which is the condition the boot rung below uses.
//
// It answers why it stopped as well as whether it painted. "Nothing painted"
// covers a title that ran out of ticks, a title that ended itself, and a title
// whose first tick refused, and a rung that reported all three the same way
// would send whoever read it to the wrong place.
func tickToFirstFrame(t *testing.T, session *skt.Runtime, framebuffer *backend.MemoryFramebuffer) (painted bool, ticks int, why string) {
	t.Helper()
	for ; ticks < localLadderBootTicks; ticks++ {
		// The frame is looked at before the state, because a title can paint
		// and then end: starting a MIDlet runs its startApp, and one that puts
		// a screen up and destroys itself has painted. The boot rung below
		// counts that as a frame, so this must agree with it or the two rungs
		// would disagree about the same archive.
		frame, presents := framebuffer.Snapshot()
		if presents > 0 && litPixels(frame) > 0 {
			return true, ticks, ""
		}
		switch state := session.State(); state {
		case skt.StateDestroyed:
			return false, ticks, "the MIDlet destroyed itself before it painted"
		case skt.StateError:
			return false, ticks, fmt.Sprintf("the MIDlet ended in error before it painted: %v", session.Summary().Error)
		}
		session.AdvanceAudio()
		if err := session.RunPending(); err != nil {
			return false, ticks, fmt.Sprintf("the MIDlet refused a tick before it painted: %v", err)
		}
		time.Sleep(localLadderTickPause)
	}
	return false, ticks, "nothing was painted"
}

// tickUntilSettled waits for the screen to stop changing on its own.
func tickUntilSettled(t *testing.T, session *skt.Runtime, framebuffer *backend.MemoryFramebuffer) (settled bool, waited int) {
	t.Helper()
	previous, _ := framebuffer.Snapshot()
	steady := 0
	for ; waited < localLadderSettleLimit; waited++ {
		session.AdvanceAudio()
		if err := session.RunPending(); err != nil {
			return false, waited
		}
		time.Sleep(localLadderTickPause)
		current, _ := framebuffer.Snapshot()
		// The pixels rather than a digest of them: this is one comparison of
		// one frame per tick, and a comparison that stops at the first
		// differing byte costs less than a hash of the whole screen.
		if !bytes.Equal(current.RGBA, previous.RGBA) {
			previous = current
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
func pressAndWatch(t *testing.T, session *skt.Runtime, framebuffer *backend.MemoryFramebuffer, key int32, baseline backend.Frame, hold int) (bool, int, error) {
	t.Helper()
	if err := session.SendKey(skt.KeyPressed, key); err != nil {
		return false, 0, err
	}
	elapsed := 0
	watch := func(ticks int) (bool, error) {
		for index := 0; index < ticks; index++ {
			session.AdvanceAudio()
			if err := session.RunPending(); err != nil {
				return false, err
			}
			time.Sleep(localLadderTickPause)
			elapsed++
			current, _ := framebuffer.Snapshot()
			if !bytes.Equal(current.RGBA, baseline.RGBA) {
				return true, nil
			}
		}
		return false, nil
	}
	changed, err := watch(hold)
	if err != nil {
		return false, elapsed, err
	}
	if releaseErr := session.SendKey(skt.KeyReleased, key); releaseErr != nil && !changed {
		return false, elapsed, releaseErr
	}
	if changed {
		return true, elapsed, nil
	}
	changed, err = watch(localLadderReleaseTicks)
	return changed, elapsed, err
}

func litPixels(frame backend.Frame) int {
	lit := 0
	for offset := 0; offset+3 < len(frame.RGBA); offset += 4 {
		if frame.RGBA[offset] != 0 || frame.RGBA[offset+1] != 0 || frame.RGBA[offset+2] != 0 {
			lit++
		}
	}
	return lit
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
