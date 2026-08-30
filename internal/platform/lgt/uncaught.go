package lgt

import (
	"errors"

	"github.com/movingwoo/wfeature/internal/armcore"
)

// An exception nothing caught ends the callback, not the program
//
// Every guest callback this platform runs — a card's `paint`, its `keyNotify`,
// a thread's `run` — is entered from a Host service call, and the guest frame
// it runs in is one this platform built. That is not where the title's own
// `try` is: a title wraps its own loop in `catch`, and the paint this platform
// starts to satisfy a repaint runs with no handler of the title's under it, so
// the same throw its loop swallows arrives here with nowhere to go.
//
// The language says an exception nothing catches ends the *thread*, and that is
// what the handset did: the callback that threw did not happen, and the next
// one still does. Two local titles show the cost of the other answer. Both
// release the pictures of the scene they are leaving, call `System.gc`, and are
// painted before the next scene's pictures are loaded, so one frame draws a
// picture that is null — an exception the specification declares for exactly
// that argument — and the run ended on a frame the title itself was going to
// throw away.
//
// **What is absorbed is only an exception the application threw**, never a
// fault: a slot this platform does not serve, memory that is not mapped, a
// limit a Host imposes and a guest exit are none of them a
// `javaUncaughtThrow`, and every one of them still stops the run. This is the
// case where the guest is behaving exactly as written and the only question is
// who catches it.
//
// It is reported rather than swallowed, the same way the other platform reports
// it: a title that fails every paint has to read as a title failing every
// paint.
func (client *Client) absorbUncaughtCallback(callback string, err error) error {
	if err == nil {
		return nil
	}
	var throw *javaUncaughtThrow
	if !errors.As(err, &throw) {
		return err
	}
	client.uncaughtCallbacks++
	if client.uncaughtFirst == "" {
		client.uncaughtFirst = callback + ": " + err.Error()
	}
	if client.logger != nil {
		client.logger.Warn("LGT guest exception ended a callback",
			"callback", callback, "class", throw.Class, "error", err)
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

// javaNullImage is the exception the specification gives an image argument that
// is null: "NullPointerException - img가 null인 경우". A title that releases its
// pictures and is painted before it has loaded the next scene's hands one of
// these in, and what the handset did with it is throw — which its own frame
// loop catches, or which ends the paint.
func (client *Client) javaNullImage(thread *armcore.Thread, where string) error {
	return client.throwJavaPlatform(thread, javaThrowNullClass, ": "+where)
}
