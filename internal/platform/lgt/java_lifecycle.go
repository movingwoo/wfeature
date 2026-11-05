package lgt

import (
	"context"
	"errors"
	"fmt"

	"github.com/movingwoo/wfeature/internal/backend"
)

// A Jlet's lifecycle: pauseApp when the session is parked, resumeApp when it
// comes back, destroyApp(true) when it closes.
//
// **A Java title registers no Clet**, so the pauseClet, resumeClet and
// destroyClet a session drives were never anything for one: the module's
// entry table is all zeros, and on the local Java titles they were measured on
// the three calls retired no guest instruction. The title's own lifecycle is
// three methods of its Jlet subclass — ordinary method records with entry
// points of their own, on the class the launcher built, the way startApp is —
// and this is where the platform enters them, on the object the launcher
// built. They are found by name and descriptor, up the chain of the title's
// own classes, so an override in the launcher class wins over the one in the
// class it extends.
//
// **They are entered the way a frame's paint is**: on the platform's own
// thread, between two ticks, with every guest thread parked where it is. A
// pause leaves the threads where they are and runs one method of the title's;
// the game still runs afterwards, which is the point. The close is the other
// case, and its order is the reason this exists: destroyApp is where a title
// writes its last state, so it runs while its threads are still there to be
// waited for and before its open files are given to the store, and only then
// are the threads stopped and the files flushed.
//
// **A callback that fails does not end the game**, which is the rule the KTF
// runtime measured for its pauseApp and resumeApp: titles that ran well
// without the pair were ended by it, at every park — in their own resumeApp,
// in a slot that platform did not serve, in a sound their archive did not
// carry. The callback is a courtesy the Host pays rather than something the
// title asked for, so an exception nothing caught is absorbed and counted as
// any callback's is (see uncaught.go), any other failure is logged, and the
// session carries on as it did before the calls existed. A title that ends
// itself inside one — `MC_knlExit`, `notifyDestroyed`, `System.exit` — is
// different: that is the title's own ending, and it reaches the Host as one.
//
// A title that declares none of them is not an error either: the
// specification's Jlet declares all three, and a class that inherits them
// from the platform has nothing of its own to run.
//
// **What the title queued with `callSerially` before its pauseApp is run
// before the park**, right after pauseApp. A handset keeps serving the
// application's event queue while the application is paused, so the run a
// title's frame chain had queued finds the flag pauseApp set and does not ask
// to be called again; a Host's park runs no tick until the resume, and
// without this the run was still waiting when resumeApp queued a chain of its
// own, and the title ran its frame twice a tick from then on. One local title
// stood still that way after every park, its keys ignored. A title with no
// pauseApp was told nothing, so its queue is left for the ticks as before.

// The lifecycle methods the platform enters, by their specification names, and
// the descriptors the local titles declare them with.
const (
	javaJletPauseApp      = "pauseApp"
	javaJletResumeApp     = "resumeApp"
	javaJletDestroyApp    = "destroyApp"
	javaJletLifecycleVoid = "()V"
	// destroyApp takes `unconditional`, which a closing session passes as
	// true: the session ends whatever the application answers.
	javaJletDestroyDescriptor = "(Z)V"
)

// jletEndingBound is how long a closing session's destroyApp may run: the
// ending every platform gives a title (backend.EndingBound). A test holds a
// title to a shorter one.
var jletEndingBound = backend.EndingBound

// PauseJlet runs the application's pauseApp and then, when there was one, the
// serial calls the title had queued before it (servePausedSerialCalls). It
// answers an error only for the title's own ending.
func (client *Client) PauseJlet(ctx context.Context) error {
	entered, err := client.runJletLifecycle(ctx, javaJletPauseApp, javaJletLifecycleVoid)
	if err != nil || !entered {
		return err
	}
	return client.servePausedSerialCalls(ctx)
}

// ResumeJlet runs the application's resumeApp. It answers an error only for
// the title's own ending.
func (client *Client) ResumeJlet(ctx context.Context) error {
	_, err := client.runJletLifecycle(ctx, javaJletResumeApp, javaJletLifecycleVoid)
	return err
}

// servePausedSerialCalls runs, once, what `callSerially` had been handed when
// the application was paused, in the order it was handed over, as the tick
// does (ServiceJavaSerialCalls): a run that queues another leaves it for the
// first tick after the resume. **A run that fails is a callback that failed**
// (jletLifecycleFailed): the park and the runs after it go on, and only the
// title ending itself is answered.
func (client *Client) servePausedSerialCalls(ctx context.Context) error {
	runtime := client.javaRun
	if runtime == nil || len(runtime.serial) == 0 || !client.jletEnterable() {
		return nil
	}
	due := runtime.serial
	runtime.serial = nil
	for _, runnable := range due {
		if err := client.callJavaRunnable(ctx, client.thread, runnable); err != nil {
			if err := client.jletLifecycleFailed("callSerially", err); err != nil {
				return err
			}
		}
	}
	return nil
}

// DestroyJlet runs the application's destroyApp(true), once. It answers an
// error only for the title's own ending, which the close it belongs to already
// is.
//
// **It is bounded by the ending every platform gives a title**
// (jletEndingBound), whoever asks: a closing session's caller usually bounds
// it already, and the earlier deadline of the two is the one kept. Past it the
// call ends, and the close goes on to store what the title wrote.
//
// **A panic in it does not take the rest of the close with it.** The threads
// still have to be stopped and the files still flushed after it, so the panic
// is reported here, with its stack, and the close carries on.
func (client *Client) DestroyJlet(ctx context.Context) (err error) {
	runtime := client.javaRun
	if runtime == nil || runtime.jlet == 0 || runtime.jletDestroyed || !client.jletEnterable() {
		return nil
	}
	// The ending is one-way: marked before the call, so an exit, an exception
	// or a spent budget inside it is not a reason to run it a second time.
	runtime.jletDestroyed = true
	ctx, cancel := context.WithTimeout(ctx, jletEndingBound)
	defer cancel()
	defer func() {
		if recovered := recover(); recovered != nil {
			_ = backend.GuestPanic(client.logger, "LGT "+javaJletDestroyApp, recovered)
			err = nil
		}
	}()
	_, err = client.runJletLifecycle(ctx, javaJletDestroyApp, javaJletDestroyDescriptor, javaTrue)
	return err
}

// jletEnterable reports whether guest code can still be entered for the
// application: not once it has ended itself, and not once its threads are
// stopped, after which the session is being taken apart.
func (client *Client) jletEnterable() bool {
	return !client.exited && !client.javaThreadsStopped
}

// runJletLifecycle enters one lifecycle method of the application object, if
// its class declares one, and answers whether it was entered and what the
// session has to know about how it ended.
func (client *Client) runJletLifecycle(ctx context.Context, name, descriptor string, arguments ...uint32) (bool, error) {
	runtime := client.javaRun
	if runtime == nil || runtime.jlet == 0 || !client.jletEnterable() {
		return false, nil
	}
	class, known := client.javaClassOfObject(runtime.jlet)
	if !known {
		return false, client.jletLifecycleFailed(name,
			fmt.Errorf("the application object at %#x is not one this platform issued", runtime.jlet))
	}
	method, owner, found := client.findJavaMethodDescribed(class.Record, name, descriptor)
	if !found || method.Body == 0 {
		return false, nil
	}
	if client.logger != nil {
		client.logger.Debug("LGT jlet lifecycle", "callback", name, "class", owner, "body", method.Body)
	}
	call := append([]uint32{runtime.jlet}, arguments...)
	if _, err := client.callOn(ctx, client.thread, method.Body, call); err != nil {
		return true, client.jletLifecycleFailed(name,
			fmt.Errorf("run %s.%s%s at %#x: %w", owner, name, descriptor, method.Body, err))
	}
	return true, nil
}

// jletLifecycleFailed sorts how a lifecycle callback failed: the title ending
// itself is answered as the ending it is, an exception nothing caught is
// absorbed and counted the way any callback's is, and anything else is
// reported in the log and dropped, so the session carries on.
func (client *Client) jletLifecycleFailed(callback string, err error) error {
	if errors.Is(err, ErrGuestExited) {
		return err
	}
	if client.absorbUncaughtCallback(callback, err) == nil {
		return nil
	}
	if client.logger != nil {
		client.logger.Warn("LGT lifecycle callback failed", "callback", callback, "error", err)
	}
	return nil
}
