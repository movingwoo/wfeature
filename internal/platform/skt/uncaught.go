package skt

import (
	"errors"
	"fmt"
	"sync"

	"github.com/movingwoo/wfeature/internal/jvm"
)

// An exception nothing caught ends the callback, not the MIDlet
//
// Every guest callback this platform runs from a Host pass — a Canvas `paint`,
// a key delivery, a queued serial Runnable, a command action, a listener — is
// entered from a Host service call, and the guest frame it runs in is one this
// Host built. That is not where the game's own `try` is: a MIDlet whose game
// loop is a thread wraps its work in `catch (Exception)` on *that* thread, and
// the paint this Host starts to satisfy a `repaint` runs with an empty handler
// chain, so the same throw its own loop swallows arrives here with nowhere to
// go.
//
// Ending the session for it is wrong twice over, for the reasons the WIPI side
// of this repository already records (`ktf.md`, and `absorbUncaughtCallback`
// there): the language says an exception nothing catches ends the thread, and
// a callback that threw simply did not happen. The shape that found it here is
// the one MIDP makes unavoidable — `startApp` shows a Canvas and *then* starts
// the thread that fills in what the Canvas paints with, so the first repaint
// and the thread's first assignment race, and the handset's green-threaded VM
// happened to win that race every time while real threads do not. The paint
// that lost it used to be the end of the game; now it is one frame nobody drew.
//
// **What is absorbed is only an exception, never a fault.** A guest that named
// a method this platform does not publish, an archive that could not be read,
// a limit a Host imposes — none of those are `jvm.GuestException`, and all of
// them still stop the run. This is the case where the guest is behaving
// exactly as written and the only question is who catches it.
//
// The MIDlet's own lifecycle calls are deliberately not absorbed. MIDP says a
// `startApp` that throws is a MIDlet that failed to start, and a start failure
// is what a Host has to be told.
type uncaughtCallbacks struct {
	mu    sync.Mutex
	count uint64
	first string
}

func (runtime *Runtime) absorbUncaughtCallback(callback string, err error) error {
	if err == nil || runtime == nil {
		return err
	}
	var guest *jvm.GuestException
	if !errors.As(err, &guest) {
		return err
	}
	name := "java/lang/Throwable"
	if guest.Object != nil && guest.Object.ClassName != "" {
		name = guest.Object.ClassName
	}
	runtime.uncaught.mu.Lock()
	runtime.uncaught.count++
	if runtime.uncaught.first == "" {
		runtime.uncaught.first = callback + ": " + err.Error()
	}
	runtime.uncaught.mu.Unlock()
	if runtime.logger != nil {
		runtime.logger.Warn("MIDP guest exception ended a callback", "callback", callback, "class", name, "error", err)
	}
	return nil
}

// UncaughtCallbacks reports how many callbacks ended in an exception nothing
// caught, and what the first one said. A Host reads it the way it reads a tick
// error: the session survived, and this is what it survived. A sweep that
// reads only the exit code counts a title failing every paint as one that
// plays, which is why both numbers reach the summary.
func (runtime *Runtime) UncaughtCallbacks() (uint64, string) {
	if runtime == nil {
		return 0, ""
	}
	runtime.uncaught.mu.Lock()
	defer runtime.uncaught.mu.Unlock()
	return runtime.uncaught.count, runtime.uncaught.first
}

// guestCallback runs one guest callback entered from a Host pass and absorbs
// the exception nothing caught. The name is what the report and the log say,
// so it is the callback rather than the method.
func (runtime *Runtime) guestCallback(name string, receiver *jvm.Object, method, descriptor string, arguments ...jvm.Value) error {
	_, err := runtime.VM.InvokeVirtual(receiver, method, descriptor, arguments...)
	if err != nil {
		return runtime.absorbUncaughtCallback(fmt.Sprintf("%s %s.%s", name, receiver.ClassName, method), err)
	}
	return nil
}
