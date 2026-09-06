package ladder

import (
	"strings"
	"testing"
)

// watch feeds a sequence of frame identities to a Watcher and reports the
// index it settled on, or -1.
func watch(t *testing.T, frames []uint64) (*Watcher, int) {
	t.Helper()
	watcher := &Watcher{}
	for index, frame := range frames {
		if watcher.Observe(frame) {
			return watcher, index
		}
	}
	return watcher, -1
}

// repeat builds a sequence that cycles through a pattern.
func repeat(pattern []uint64, times int) []uint64 {
	var out []uint64
	for index := 0; index < times; index++ {
		out = append(out, pattern...)
	}
	return out
}

func TestStillScreenSettlesOnTheEighthUnchangedTick(t *testing.T) {
	watcher, at := watch(t, repeat([]uint64{7}, 64))
	// The first identity is the frame that was up before the first tick, so
	// the eighth unchanged tick is index eight.
	if at != StillRuns {
		t.Fatalf("a still screen settled at %d, want %d", at, StillRuns)
	}
	if frames := watcher.Frames(); len(frames) != 1 || frames[0] != 7 {
		t.Fatalf("the settled screen left %v, want the one frame it showed", frames)
	}
	if watcher.Changed(7) {
		t.Fatal("the frame it settled on read as a change")
	}
	if !watcher.Changed(8) {
		t.Fatal("a frame it never showed did not read as a change")
	}
}

func TestBlinkingScreenSettles(t *testing.T) {
	// Two frames alternating every four ticks, which is the shape 27 of the
	// 46 measured archives have.
	blink := []uint64{1, 1, 1, 1, 2, 2, 2, 2}
	watcher, at := watch(t, repeat(blink, 16))
	if at < 0 {
		t.Fatal("a blinking screen never settled, which is the defect this judgment was written for")
	}
	if at+1 < 2*CycleWindow {
		t.Fatalf("a blinking screen settled at %d, before two windows had been seen", at)
	}
	if frames := watcher.Frames(); len(frames) != 2 {
		t.Fatalf("the settled screen left %v, want both frames of the blink", frames)
	}
	if watcher.Changed(1) || watcher.Changed(2) {
		t.Fatal("a frame of the blink read as a change, which would pass the rung without a key")
	}
	if !watcher.Changed(3) {
		t.Fatal("a frame outside the blink did not read as a change")
	}
}

func TestSlowestMeasuredBlinkSettles(t *testing.T) {
	// A period of 16 ticks is the slowest blink measured, and the reason the
	// window is 32: a half smaller than two periods sees two phases rather
	// than one cycle.
	var blink []uint64
	for index := 0; index < 8; index++ {
		blink = append(blink, 1)
	}
	for index := 0; index < 8; index++ {
		blink = append(blink, 2)
	}
	if _, at := watch(t, repeat(blink, 16)); at < 0 {
		t.Fatal("the slowest blink measured never settled")
	}
}

func TestAnimationIsNotSettled(t *testing.T) {
	var frames []uint64
	for index := 0; index < 400; index++ {
		frames = append(frames, uint64(index))
	}
	if watcher, at := watch(t, frames); at >= 0 {
		t.Fatalf("an animation settled at %d with %v", at, watcher.Frames())
	}
}

func TestSlowCrawlIsNotSettled(t *testing.T) {
	// A picture redrawn one step at a time shows few distinct frames in any
	// short window, which is why the two halves are compared rather than only
	// counted: nothing here repeats. Each step is held for fewer ticks than a
	// still screen needs, so this is the cycling path being asked, not the
	// still one.
	var frames []uint64
	for index := 0; index < 80; index++ {
		for held := 0; held < StillRuns-2; held++ {
			frames = append(frames, uint64(index))
		}
	}
	if _, at := watch(t, frames); at >= 0 {
		t.Fatalf("a crawl settled at %d", at)
	}
}

func TestALoopOfMoreFramesThanABlinkIsNotSettled(t *testing.T) {
	// Six frames is the smallest cycling animation measured, and a demo loop
	// read as a settled screen is what makes the rung report a title that
	// ignores keys as one that is broken.
	loop := []uint64{1, 1, 2, 2, 3, 3, 4, 4, 5, 5, 6, 6}
	if _, at := watch(t, repeat(loop, 40)); at >= 0 {
		t.Fatalf("a six-frame loop settled at %d", at)
	}
}

func TestUnsettledSaysWhatTheScreenWasDoing(t *testing.T) {
	var frames []uint64
	for index := 0; index < 200; index++ {
		frames = append(frames, uint64(index))
	}
	watcher, at := watch(t, frames)
	if at >= 0 {
		t.Fatalf("an animation settled at %d", at)
	}
	why := watcher.Unsettled(len(frames))
	if !strings.HasPrefix(why, "the screen was still changing after") {
		t.Fatalf("the reason reads %q, which no longer groups with the runs before it", why)
	}
	if !strings.Contains(why, "32 of them new") {
		t.Fatalf("the reason reads %q, want it to count the frames it had never seen", why)
	}
}

func TestNothingChangedBeforeItSettled(t *testing.T) {
	watcher := &Watcher{}
	watcher.Observe(1)
	if watcher.Changed(2) {
		t.Fatal("a screen with nothing to compare against reported a change")
	}
}
