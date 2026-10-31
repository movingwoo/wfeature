package ktf

import (
	"github.com/movingwoo/wfeature/internal/platform/wipicrt"
	"time"
)

// frameLoopPeriod raises to the platform's frame resolution a wait a guest
// thread declares immediately after publishing a frame of its own, and leaves
// every other wait exactly as the guest asked for it.
//
// **A frame loop does not have to live in a timer or in a card's paint.** The
// two entry points above cover those; a third shape puts the whole loop on a
// guest thread — `repaint`, `serviceRepaints`, `Thread.sleep`, round again —
// and reached `waitDeadline` instead, so it got neither the floor nor the
// meaning. With a period of zero that is not a slow game, it is no game: one
// local title asks for `Thread.sleep(0)`, which this platform answered by
// returning without parking anything, and its loop then ran for a whole slice
// of guest steps per round. Measured at 4,813 published frames for every one
// a Host collected, which is a world stepping four thousand times between two
// pictures of it — the game sprinting and the screen crawling, which is what a
// person reports as unbearably slow.
//
// **What makes it a frame period is that a frame came first.** The flag is set
// by `presentScreen` on the thread that published, and read and cleared here,
// so the only wait this touches is the one a thread takes straight after
// putting a picture on the panel. A loader thread that sleeps between chunks
// without drawing is untouched, which matters: flooring its sleeps would turn
// a load of a few hundred short waits into a load of a few hundred frames.
//
// The floor is the same one and for the same reason — zero and ten
// milliseconds are both this era's "as soon as you can", and the resolution a
// timer is accurate to is the system's underneath. A thread asking for more
// than a frame is asking to sleep and keeps what it asked for. **The frame's
// own work is deliberately not charged here**: `chargedFramePeriod` measures
// from the last time it was read, which for a thread that reaches this after a
// long start-up would be the whole start-up, and unlike the timer path there
// is no deadline to clamp the result against — `sleepCurrentWorker` parks for
// the length it is handed.
func (client *Client) frameLoopPeriod(wait time.Duration) time.Duration {
	if client == nil {
		return wait
	}
	worker := client.activeWorker
	if worker == nil || !worker.publishedFrame {
		return wait
	}
	worker.publishedFrame = false
	if wait < wipicrt.MinGuestFramePeriod {
		return wipicrt.MinGuestFramePeriod
	}
	return wait
}
