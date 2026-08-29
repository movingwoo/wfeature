package ktf

import (
	"errors"
	"fmt"

	"github.com/movingwoo/wfeature/internal/jvm"
)

// An exception nothing caught ends the callback, not the program
//
// Every guest callback this platform runs — a card's `paint`, a `keyNotify`, a
// queued Runnable, a timer task, a thread's `run` — is entered from a Host
// service call, and the guest frame it runs in is one this Host built. That is
// not where the game's own `try` is. A title whose frame loop is a thread
// wraps its work in `catch (Exception)` on *that* thread; the paint this Host
// starts to satisfy a `repaint` runs on the client thread, whose handler chain
// is empty, so the same throw that its own loop swallows arrives here with
// nowhere to go.
//
// Ending the session for it is the wrong answer twice over. The language says
// an exception nothing catches ends the *thread*, and every runtime on the
// other side of this bridge behaves that way: a callback that threw did not
// happen, and the next one still does. And the throw is frequently the title's
// own arithmetic on a screen this platform sized differently from the handset
// it was written for — one A-grade title indexes a fixed 32-entry row table
// with `height / 10 + 1`, which is 33 on a 320-pixel screen and fits on every
// handset whose card was shorter. On its own thread it catches that, draws the
// rest of the frame and plays; through the Host's paint it used to be the end
// of the game.
//
// **What is absorbed is only an exception, never a fault.** A guest that read
// unmapped memory, a method this platform does not publish, a limit a Host
// imposes — none of those are `jvm.GuestException`, and all of them still stop
// the run. This is the case where the guest is behaving exactly as written and
// the only question is who catches it.
//
// It is reported rather than swallowed: the class, the callback and the site
// go into the diagnostics and the log, so a title that fails every paint reads
// as a title failing every paint instead of a title that runs.
func (client *Client) absorbUncaughtCallback(callback string, err error) error {
	if err == nil {
		return nil
	}
	var guest *jvm.GuestException
	if !errors.As(err, &guest) {
		return err
	}
	name := "java/lang/Throwable"
	if guest.Object != nil && guest.Object.ClassName != "" {
		name = guest.Object.ClassName
	}
	if client == nil {
		return nil
	}
	if client.runtime != nil {
		client.runtime.countDiagnostic(fmt.Sprintf("uncaught %s in %s", name, callback))
	}
	client.uncaughtCallbacks++
	if client.uncaughtFirst == "" {
		client.uncaughtFirst = callback + ": " + err.Error()
	}
	if client.logger != nil {
		client.logger.Warn("KTF guest exception ended a callback", "callback", callback, "class", name, "error", err)
	}
	return nil
}

// UncaughtCallbacks reports how many callbacks ended in an exception nothing
// caught, and what the first one said. A Host reads it the way it reads a tick
// error: the session survived, and this is what it survived.
func (client *Client) UncaughtCallbacks() (uint64, string) {
	if client == nil {
		return 0, ""
	}
	return client.uncaughtCallbacks, client.uncaughtFirst
}
