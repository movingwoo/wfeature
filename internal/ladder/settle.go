// Package ladder holds the one judgment the three platforms' local acceptance
// rungs share: has this screen settled, and did a key change it.
//
// # Why it is here rather than three times over
//
// The interactive rung asks whether a key changes what a title draws, and it
// waits for the screen to settle before pressing anything, because a change
// measured against a screen that was already moving says nothing about the
// key. That waiting is the same question on every platform, and a grade is
// only worth comparing across platforms if it means the same thing on each of
// them. Written once per platform it drifted into three answers to one
// question the first time it had to change; written here it cannot.
//
// What stays per platform is what a platform is made of — how a tick is spent,
// how a key is sent, what a frame is read from. None of that is this file's
// business. What arrives here is one number per tick: the identity of what is
// on the screen.
//
// # What a settled screen looks like
//
// The first version of this asked for one thing: the same frame N times in a
// row. That is what a *still* screen looks like, and it is not what a screen
// waiting for input looks like. A blinking prompt — a caret, an arrow, a line
// of text drawn in two phases — never presents the same frame twice in a row
// for long, and a run of identical frames never arrives however long it is
// waited for. Widening the window does not help: a screen that blinks every
// four ticks blinks every four ticks for as long as anyone watches.
//
// In a sweep of 457 archives, 46 were refused this rung on exactly that — 21
// KTF, 17 LGT, 8 SKT — which was the largest single cause left in the sweep.
// Before this judgment was written those 46 were measured: each was taken to
// its first frame and then watched for twice the window the rung gives it,
// with the identity of every presented frame recorded. Counting the distinct
// frames in the last 32 ticks of the rung's own window divides them cleanly:
//
//	frames cycled through | archives
//	                    1 |  1
//	                    2 | 27
//	                    3 |  2
//	                    4 |  1
//	                  6-10 |  8
//	                 16-32 |  7
//
// The first four rows are blinks over a still picture: they repeat with a
// period between 2 and 16 ticks and, in 31 of the 46, introduce no frame in
// the second half of a 64-tick span that the first half had not already shown.
// The last two rows keep producing frames never seen before, or cycle through
// six to thirty-two of them — those are animations and demo loops, and the
// rung is right to refuse them. Nothing measured falls between four and six.
//
// So the judgment gains a second way to settle, and keeps the first:
//
//	still:   the same frame for StillRuns ticks in a row.
//	cycling: over the last 2*CycleWindow ticks, at most CycleFrames distinct
//	         frames, with the same frame set in both halves.
//
// Both paths observe a full pair of cycle windows before settling. The old
// eight-tick shortcut could freeze on one phase of a 38-tick animation and
// mistake its next phase for a key response. A still screen now pays the same
// observation cost as a blinking screen.
//
// # Why these numbers
//
// CycleFrames is 4 because that is the largest blink measured and the smallest
// animation cycles six. Simulating the rule over the 46 recorded traces gives
// the same 31 archives for every CycleFrames from 4 to 8 and every
// CycleWindow from 16 to 40 — a plateau that wide means the answer is a
// property of the corpus rather than of a threshold somebody chose, and 4 is
// its low end, which is where a judgment that must not mistake a demo loop for
// a still screen belongs.
//
// CycleWindow is 32 because the slowest blink measured has a period of 16
// ticks, and a half-window has to hold at least two full cycles of that or the
// two halves are two different phases of one blink rather than two views of
// one cycle.
//
// # What a settled screen is compared against
//
// A screen that blinks has no single frame to compare a key against: half the
// ticks after the key differ from the frame that was up before it whether or
// not the key did anything. So what a settled screen leaves behind is the
// *set* of frames it cycles through, and a key changed the screen when it
// produces content that set never held. For a still screen that set has one
// member and this is the comparison the rung already made.
//
// # What is still unanswerable
//
// A screen that keeps producing new content is still reported as unanswerable
// rather than as a failure. Not knowing and not working are different answers,
// and the point of this file is to shrink "I do not know", not to spell it as
// something else.
package ladder

import (
	"fmt"
	"hash/fnv"
	"sort"
)

const (
	// StillRuns covers the same observation horizon as the cycling path.
	// The initial frame plus this many ticks provide two full cycle windows.
	StillRuns = 2*CycleWindow - 1
	// CycleWindow is half the span a repeating screen is judged over, in
	// ticks. See the package comment: the slowest blink measured repeats every
	// 16 ticks, and a half this size holds two of those.
	CycleWindow = 32
	// CycleFrames is the most distinct frames a settled screen may cycle
	// through. Four is the largest blink measured; the smallest animation
	// cycles six.
	CycleFrames = 4
)

// A Watcher decides whether a screen has settled, from the identity of what it
// presents. One identity per tick goes in; the first `true` out is the tick the
// screen settled on.
//
// The identity is whatever the platform can produce cheaply that changes when
// the picture changes: a frame digest the session already keeps, or Digest over
// the pixels. What matters is that two ticks showing the same picture produce
// the same number, and that this Watcher and the key comparison after it use
// the same one.
type Watcher struct {
	history []uint64
	frames  []uint64
	settled bool
}

// Observe records what is on the screen now and reports whether the screen has
// settled. It is called once for the frame that was up before the first tick
// and once per tick after that.
func (w *Watcher) Observe(frame uint64) bool {
	if w.settled {
		return true
	}
	w.history = append(w.history, frame)
	if frames, ok := w.still(); ok {
		w.settled, w.frames = true, frames
		return true
	}
	if frames, ok := w.cycling(); ok {
		w.settled, w.frames = true, frames
		return true
	}
	return false
}

// Settled reports whether the screen has settled.
func (w *Watcher) Settled() bool { return w.settled }

// Frames is the set of frames the settled screen cycles through, in the order
// their identities sort, and nil before it has settled. A still screen leaves
// one.
func (w *Watcher) Frames() []uint64 {
	return append([]uint64(nil), w.frames...)
}

// Changed reports whether a frame is content the settled screen never showed,
// which is the only evidence a key did anything that a blinking screen can
// give. Before the screen has settled there is nothing to compare against and
// nothing has changed.
func (w *Watcher) Changed(frame uint64) bool {
	if !w.settled {
		return false
	}
	for _, settled := range w.frames {
		if frame == settled {
			return false
		}
	}
	return true
}

// still reports a screen that has shown one frame long enough that nothing on
// it is moving at all.
func (w *Watcher) still() ([]uint64, bool) {
	if len(w.history) <= StillRuns {
		return nil, false
	}
	tail := w.history[len(w.history)-StillRuns-1:]
	for _, frame := range tail {
		if frame != tail[0] {
			return nil, false
		}
	}
	return []uint64{tail[0]}, true
}

// cycling reports a screen that repeats a handful of frames and has shown
// nothing else for long enough that the repetition is the whole of what it is
// doing.
//
// The two halves are compared rather than counted together, because counting
// alone cannot tell a blink from a crawl: a picture redrawn one slow step at a
// time also shows few distinct frames in a short window, and it is only the
// second half showing nothing the first half did not that says the screen is
// going round rather than forward.
func (w *Watcher) cycling() ([]uint64, bool) {
	if len(w.history) < 2*CycleWindow {
		return nil, false
	}
	recent := w.history[len(w.history)-CycleWindow:]
	before := w.history[len(w.history)-2*CycleWindow : len(w.history)-CycleWindow]
	cycle := map[uint64]bool{}
	for _, frame := range before {
		cycle[frame] = true
		if len(cycle) > CycleFrames {
			return nil, false
		}
	}
	recentFrames := map[uint64]bool{}
	for _, frame := range recent {
		recentFrames[frame] = true
		if !cycle[frame] {
			return nil, false
		}
	}
	if len(recentFrames) != len(cycle) {
		return nil, false
	}
	frames := make([]uint64, 0, len(cycle))
	for frame := range cycle {
		frames = append(frames, frame)
	}
	sort.Slice(frames, func(one, two int) bool { return frames[one] < frames[two] })
	return frames, true
}

// Unsettled says what the screen was doing when the rung gave up on it, for
// the line a skip is reported as. The counts are the difference between a
// title animating an opening sequence and one cycling through more frames than
// a blink: a report that said only "it was still changing" sent whoever read
// it back to measure this by hand, which is how this file came to be written.
func (w *Watcher) Unsettled(waited int) string {
	recent := w.history
	if len(recent) > CycleWindow {
		recent = recent[len(recent)-CycleWindow:]
	}
	seen := map[uint64]bool{}
	for _, frame := range recent {
		seen[frame] = true
	}
	before := map[uint64]bool{}
	if len(w.history) > len(recent) {
		for _, frame := range w.history[:len(w.history)-len(recent)] {
			before[frame] = true
		}
	}
	fresh := 0
	for frame := range seen {
		if !before[frame] {
			fresh++
		}
	}
	return fmt.Sprintf("the screen was still changing after %d ticks (%d different frames in the last %d, %d of them new), so a change after a key would prove nothing",
		waited, len(seen), len(recent), fresh)
}

// Digest identifies a picture by its pixels, for a platform whose session does
// not already keep an identity for the frame it presents. It is not a
// checksum anybody stores: it exists so that "the same picture" means the same
// thing to the Watcher and to the comparison after it.
func Digest(rgba []byte) uint64 {
	sum := fnv.New64a()
	sum.Write(rgba)
	return sum.Sum64()
}
