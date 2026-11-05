package lgt

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/movingwoo/wfeature/internal/backend"
)

// The words the lifecycle fixture's methods record what happened in.
const (
	lifecyclePauses   = 0 // pauseApp()V calls
	lifecycleResumes  = 1 // resumeApp()V calls
	lifecycleDestroys = 2 // destroyApp(Z)V calls
	lifecycleArgument = 3 // what destroyApp was handed
	lifecycleAlive    = 4 // what Thread.isAlive answered inside destroyApp
	lifecycleReceiver = 5 // the object the last of them was called on
	lifecycleOverload = 6 // calls of an overload declared under a lifecycle name
)

// The words the serial-call tests record in. They are lifecycleAlive's and
// lifecycleOverload's, which those tests do not read, and the fixture's last.
const (
	serialPaused = 4 // the title's own flag: pauseApp sets it, resumeApp clears it
	serialRuns   = 6 // runs of the Runnable the title handed callSerially
	serialSaw    = 7 // the flag that Runnable found on its last run
)

// newJletFixture is the Java checkpoint fixture as a Java title is: no Clet
// entry points, because a Java title registers none, and the stubs the
// lifecycle methods below reach for.
func newJletFixture(t *testing.T, store backend.SaveStore) *javaThreadFixture {
	t.Helper()
	fixture := newJavaThreadFixture(t, store)
	client := fixture.client
	client.clet = CletFunctions{}
	// Jlet.notifyDestroyed, reached through the static and special method
	// table the way a module reaches it, as the entry after Thread's two.
	surface := client.javaLink.surface
	surface.Classes = append(surface.Classes,
		javaAPIClass{Name: "org/kwis/msp/lcdui/Jlet", StaticMethods: javaRun{Start: 2, Count: 1}})
	surface.StaticMethods = append(surface.StaticMethods, javaMemberRef{Name: "notifyDestroyed", Descriptor: "()V"})
	for name, slot := range map[string][2]uint32{
		"notifyDestroyed": {svcCategoryJava, javaStaticMethodSlot(2)},
		"write":           {svcCategoryWIPIC, slotFsWrite},
		"exit":            {svcCategoryWIPIC, slotExit},
	} {
		address, err := client.stub(slot[0], slot[1])
		if err != nil {
			t.Fatal(err)
		}
		fixture.stubs[name] = address
	}
	return fixture
}

// jlet makes the application object: the one instance of a class declaring
// the methods given, recorded where the launcher records the one it builds.
func (fixture *javaThreadFixture) jlet(methods ...javaMember) uint32 {
	fixture.t.Helper()
	class, instance := fixture.newClass("Application", 0)
	class.Record.Methods = methods
	fixture.client.javaRuntimeState().jlet = instance
	return instance
}

// lifecycleMethod assembles a method that records the object it was called on,
// does what more adds, and counts the call.
func (fixture *javaThreadFixture) lifecycleMethod(counter uint32, more func(a *assembler)) uint32 {
	return fixture.install(func(a *assembler) {
		a.emit(armPushLR)
		a.literal(4, fixture.counter(lifecycleReceiver))
		a.emit(armStr(0, 4, 0))
		if more != nil {
			more(a)
		}
		fixture.increment(a, counter)
		a.emit(armPopPC)
	})
}

func (fixture *javaThreadFixture) lifecycleWord(index uint32) uint32 {
	fixture.t.Helper()
	return guestWord(fixture.t, fixture.session, fixture.counter(index))
}

// A Host parks a Java title the way it parks a Clet, and the title can only
// hear of it through its Jlet: it registers no Clet, so the pauseClet and
// resumeClet a session drove ran nothing for it, and its own pauseApp and
// resumeApp were never entered. They are entered on the application object,
// found by name and descriptor — an overload under the same name is not the
// method meant — and a parked title that came back still runs.
func TestParkingAJavaTitleRunsItsJletPauseAndResume(t *testing.T) {
	ctx := context.Background()
	store, _ := backend.NewMemorySaveStore(nil)
	fixture := newJletFixture(t, store)
	jlet := fixture.jlet(
		javaMember{Name: javaJletPauseApp, Descriptor: "(I)V", Body: fixture.lifecycleMethod(lifecycleOverload, nil)},
		javaMember{Name: javaJletPauseApp, Descriptor: "()V", Body: fixture.lifecycleMethod(lifecyclePauses, nil)},
		javaMember{Name: javaJletResumeApp, Descriptor: "()V", Body: fixture.lifecycleMethod(lifecycleResumes, nil)},
	)
	fixture.settle()

	// A quick save tells the title nothing, Java or not.
	if _, err := fixture.session.CaptureCheckpoint(ctx); err != nil {
		t.Fatalf("capture: %v", err)
	}
	if got := fixture.lifecycleWord(lifecyclePauses); got != 0 {
		t.Fatalf("a quick save ran pauseApp %d times", got)
	}

	if err := fixture.session.Pause(ctx); err != nil {
		t.Fatalf("Pause() = %v", err)
	}
	if pauses, resumes := fixture.lifecycleWord(lifecyclePauses), fixture.lifecycleWord(lifecycleResumes); pauses != 1 || resumes != 0 {
		t.Fatalf("after a pause, pauseApp ran %d times and resumeApp %d, want 1 and 0", pauses, resumes)
	}
	if got := fixture.lifecycleWord(lifecycleReceiver); got != jlet {
		t.Fatalf("pauseApp was called on %#x, want the application object %#x", got, jlet)
	}
	if err := fixture.session.Resume(ctx); err != nil {
		t.Fatalf("Resume() = %v", err)
	}
	if pauses, resumes := fixture.lifecycleWord(lifecyclePauses), fixture.lifecycleWord(lifecycleResumes); pauses != 1 || resumes != 1 {
		t.Fatalf("after a resume, pauseApp ran %d times and resumeApp %d, want 1 and 1", pauses, resumes)
	}
	if got := fixture.lifecycleWord(lifecycleOverload); got != 0 {
		t.Fatalf("an overload of pauseApp ran %d times", got)
	}
	tickSession(t, fixture.session, 2)
}

// Closing a Java title runs its destroyApp(true), once, on the application
// object, and before anything is taken from it: its thread is still alive in
// there, and what it writes into a file it left open is what the close stores.
func TestClosingAJavaTitleRunsDestroyAppBeforeItsThreadsAndFiles(t *testing.T) {
	ctx := context.Background()
	store := newFaultStore(t, nil)
	fixture := newJletFixture(t, store)
	client := fixture.client
	files := newGuestFiles(t, client)
	handle := files.open("final.dat", fileOpenReadWrite)
	if err := client.core.Memory().Write(files.buffer, []byte("FINAL")); err != nil {
		t.Fatal(err)
	}
	worker := fixture.start("Worker", fixture.install(func(a *assembler) {
		top := a.here()
		fixture.call(a, "sleep", 30, 0)
		fixture.loop(a, top)
	}))
	alive, err := client.stub(svcCategoryJava, javaVirtualSlot(javaThreadClass, 13))
	if err != nil {
		t.Fatal(err)
	}
	if dispatch, ok := client.javaPlatformVirtualDispatch(javaVirtualSlot(javaThreadClass, 13)); !ok || dispatch.Called != "isAlive()Z" {
		t.Fatalf("the isAlive stub resolves to %+v", dispatch)
	}
	fixture.stubs["alive"] = alive
	jlet := fixture.jlet(javaMember{Name: javaJletDestroyApp, Descriptor: "(Z)V",
		Body: fixture.lifecycleMethod(lifecycleDestroys, func(a *assembler) {
			a.literal(4, fixture.counter(lifecycleArgument))
			a.emit(armStr(1, 4, 0))
			fixture.call(a, "alive", worker)
			a.literal(4, fixture.counter(lifecycleAlive))
			a.emit(armStr(0, 4, 0))
			fixture.call(a, "write", handle, files.buffer, 5)
		})})
	fixture.settle()

	if err := fixture.session.Close(ctx); err != nil {
		t.Fatalf("Close() = %v", err)
	}
	if got := fixture.lifecycleWord(lifecycleDestroys); got != 1 {
		t.Fatalf("destroyApp ran %d times, want once", got)
	}
	if receiver, argument := fixture.lifecycleWord(lifecycleReceiver), fixture.lifecycleWord(lifecycleArgument); receiver != jlet || argument != javaTrue {
		t.Fatalf("destroyApp was called on %#x with %d, want the application object %#x with true", receiver, argument, jlet)
	}
	if got := fixture.lifecycleWord(lifecycleAlive); got != javaTrue {
		t.Fatal("the title's thread was already stopped when its destroyApp ran")
	}
	if got := store.held("fs/final.dat"); got != "FINAL" {
		t.Fatalf("after the close the store holds %q, want what destroyApp wrote", got)
	}
	if !client.javaThreadsStopped || len(client.javaRun.workers) != 0 {
		t.Fatal("the close left the title's thread running")
	}
	// The ending is one-way: a second close does not run it again.
	if err := fixture.session.Close(ctx); err != nil {
		t.Fatalf("a second Close() = %v", err)
	}
	if got := fixture.lifecycleWord(lifecycleDestroys); got != 1 {
		t.Fatalf("a second close ran destroyApp again (%d calls)", got)
	}
}

// An exception a lifecycle method throws and does not catch ends that call and
// not the game, and it is counted the way any callback's is: the park, the
// resume and the close all go ahead.
func TestAJletLifecycleExceptionIsCountedAndTheGameGoesOn(t *testing.T) {
	ctx := context.Background()
	fixture := newJletFixture(t, nil)
	throwing := func(counter uint32) uint32 {
		return fixture.lifecycleMethod(counter, func(a *assembler) {
			fixture.increment(a, counter)
			fixture.call(a, "throw", 0)
		})
	}
	fixture.jlet(
		javaMember{Name: javaJletPauseApp, Descriptor: "()V", Body: throwing(lifecyclePauses)},
		javaMember{Name: javaJletResumeApp, Descriptor: "()V", Body: throwing(lifecycleResumes)},
		javaMember{Name: javaJletDestroyApp, Descriptor: "(Z)V", Body: throwing(lifecycleDestroys)},
	)
	fixture.settle()

	if err := fixture.session.Pause(ctx); err != nil {
		t.Fatalf("Pause() over a throwing pauseApp = %v", err)
	}
	if err := fixture.session.Resume(ctx); err != nil {
		t.Fatalf("Resume() over a throwing resumeApp = %v", err)
	}
	tickSession(t, fixture.session, 1)
	if err := fixture.session.Close(ctx); err != nil {
		t.Fatalf("Close() over a throwing destroyApp = %v", err)
	}
	for name, index := range map[string]uint32{"pauseApp": lifecyclePauses, "resumeApp": lifecycleResumes, "destroyApp": lifecycleDestroys} {
		// Each counts once before it throws; the line after the throw is
		// never reached.
		if got := fixture.lifecycleWord(index); got != 1 {
			t.Errorf("%s reached its throw %d times, want once", name, got)
		}
	}
	count, first := fixture.session.UncaughtCallbacks()
	if count != 3 || !strings.HasPrefix(first, javaJletPauseApp+": ") || !strings.Contains(first, javaThrowNullClass) {
		t.Fatalf("UncaughtCallbacks() = %d, %q; want the three callbacks, the first pauseApp's NullPointerException", count, first)
	}
}

// A lifecycle method that fails other than by throwing — here it reads memory
// that is not there — is reported and the game carries on, which is the rule
// the KTF runtime measured for the same calls. The platform's own thread is
// still usable after it: the resume that follows runs.
func TestAJletLifecycleFaultIsReportedAndTheGameGoesOn(t *testing.T) {
	ctx := context.Background()
	fixture := newJletFixture(t, nil)
	var logged bytes.Buffer
	fixture.client.logger = slog.New(slog.NewTextHandler(&logged, &slog.HandlerOptions{Level: slog.LevelWarn}))
	fixture.jlet(
		javaMember{Name: javaJletPauseApp, Descriptor: "()V", Body: fixture.lifecycleMethod(lifecyclePauses, func(a *assembler) {
			fixture.increment(a, lifecyclePauses)
			a.literal(4, 0x10000000) // nothing is mapped there
			a.emit(armLdr(0, 4, 0))
		})},
		javaMember{Name: javaJletResumeApp, Descriptor: "()V", Body: fixture.lifecycleMethod(lifecycleResumes, nil)},
	)
	fixture.settle()

	if err := fixture.session.Pause(ctx); err != nil {
		t.Fatalf("Pause() over a faulting pauseApp = %v", err)
	}
	if got := fixture.lifecycleWord(lifecyclePauses); got != 1 {
		t.Fatalf("pauseApp reached its fault %d times, want once", got)
	}
	if !strings.Contains(logged.String(), "LGT lifecycle callback failed") || !strings.Contains(logged.String(), javaJletPauseApp) {
		t.Fatalf("the fault was not reported: %q", logged.String())
	}
	if count, _ := fixture.session.UncaughtCallbacks(); count != 0 {
		t.Fatalf("a fault was counted as %d uncaught exceptions", count)
	}
	if err := fixture.session.Resume(ctx); err != nil {
		t.Fatalf("Resume() = %v", err)
	}
	if got := fixture.lifecycleWord(lifecycleResumes); got != 1 {
		t.Fatalf("resumeApp ran %d times after the faulted pause, want once", got)
	}
	tickSession(t, fixture.session, 2)
}

// A title that ends itself inside its pauseApp has ended: the Host is answered
// with the ending, as it is from a tick, and the close that follows does not
// run destroyApp, because a title that has ended itself is not destroyed again.
func TestAJletThatEndsItselfInPauseAppEnds(t *testing.T) {
	ctx := context.Background()
	fixture := newJletFixture(t, nil)
	fixture.jlet(
		javaMember{Name: javaJletPauseApp, Descriptor: "()V", Body: fixture.lifecycleMethod(lifecyclePauses, func(a *assembler) {
			fixture.call(a, "exit", 0)
		})},
		javaMember{Name: javaJletDestroyApp, Descriptor: "(Z)V", Body: fixture.lifecycleMethod(lifecycleDestroys, nil)},
	)
	fixture.settle()

	err := fixture.session.Pause(ctx)
	if !errors.Is(err, ErrGuestExited) {
		t.Fatalf("Pause() over a pauseApp that exits = %v, want the ending", err)
	}
	if !strings.Contains(err.Error(), javaJletPauseApp) {
		t.Errorf("the ending %q does not name the callback it came from", err)
	}
	if err := fixture.session.Close(ctx); err != nil {
		t.Fatalf("Close() after the ending = %v", err)
	}
	if got := fixture.lifecycleWord(lifecycleDestroys); got != 0 {
		t.Fatalf("destroyApp ran %d times after the title ended itself", got)
	}
}

// A destroyApp that calls notifyDestroyed, which is how most of them end, is
// an ending inside the close: the close carries on as one and stores what the
// title wrote.
func TestAJletThatCallsNotifyDestroyedInDestroyAppCloses(t *testing.T) {
	ctx := context.Background()
	store := newFaultStore(t, nil)
	fixture := newJletFixture(t, store)
	client := fixture.client
	files := newGuestFiles(t, client)
	handle := files.open("last.dat", fileOpenReadWrite)
	if err := client.core.Memory().Write(files.buffer, []byte("LAST")); err != nil {
		t.Fatal(err)
	}
	jlet := fixture.jlet(javaMember{Name: javaJletDestroyApp, Descriptor: "(Z)V",
		Body: fixture.lifecycleMethod(lifecycleDestroys, func(a *assembler) {
			a.emit(armMovReg(6, 0)) // r6 survives the write; the receiver is notifyDestroyed's
			fixture.call(a, "write", handle, files.buffer, 4)
			a.emit(armMovReg(0, 6))
			a.literal(5, fixture.stubs["notifyDestroyed"])
			a.call(5)
		})})
	fixture.settle()

	if err := fixture.session.Close(ctx); err != nil {
		t.Fatalf("Close() over a destroyApp that calls notifyDestroyed = %v", err)
	}
	if !client.exited {
		t.Fatal("notifyDestroyed inside destroyApp did not end the title")
	}
	if got := fixture.lifecycleWord(lifecycleReceiver); got != jlet {
		t.Fatalf("destroyApp was called on %#x, want %#x", got, jlet)
	}
	if got := store.held("fs/last.dat"); got != "LAST" {
		t.Fatalf("after the close the store holds %q, want what destroyApp wrote", got)
	}
	if !client.javaThreadsStopped {
		t.Fatal("the close stopped short of the threads")
	}
}

// A destroyApp that never returns is ended at the ending bound, whoever closes
// the session, and the close goes on: the threads are stopped and what the
// title wrote is stored.
func TestADestroyAppThatNeverReturnsIsBoundedByTheEnding(t *testing.T) {
	previous := jletEndingBound
	jletEndingBound = 50 * time.Millisecond
	t.Cleanup(func() { jletEndingBound = previous })
	store := newFaultStore(t, nil)
	fixture := newJletFixture(t, store)
	client := fixture.client
	files := newGuestFiles(t, client)
	handle := files.open("spin.dat", fileOpenReadWrite)
	if err := client.core.Memory().Write(files.buffer, []byte("SPUN")); err != nil {
		t.Fatal(err)
	}
	fixture.jlet(javaMember{Name: javaJletDestroyApp, Descriptor: "(Z)V", Body: fixture.install(func(a *assembler) {
		a.emit(armPushLR)
		fixture.call(a, "write", handle, files.buffer, 4)
		fixture.increment(a, lifecycleDestroys)
		top := a.here()
		fixture.loop(a, top)
	})})
	fixture.settle()

	started := time.Now()
	if err := fixture.session.Close(context.Background()); err != nil {
		t.Fatalf("Close() = %v", err)
	}
	if elapsed := time.Since(started); elapsed > 10*time.Second {
		t.Fatalf("the close took %v, past the ending bound", elapsed)
	}
	if got := fixture.lifecycleWord(lifecycleDestroys); got != 1 {
		t.Fatalf("destroyApp reached its loop %d times, want once", got)
	}
	if got := store.held("fs/spin.dat"); got != "SPUN" {
		t.Fatalf("after the close the store holds %q, want what destroyApp wrote", got)
	}
	if !client.javaThreadsStopped {
		t.Fatal("the close stopped short of the threads")
	}
}

// A quick save carries no lifecycle state of its own: one taken while the
// title is parked holds what its pauseApp did in its memory, and the load is
// resumed through resumeApp like the session it was taken from.
func TestAJavaTitleParkedAtAQuickSaveResumesAfterTheLoad(t *testing.T) {
	ctx := context.Background()
	store, _ := backend.NewMemorySaveStore(nil)
	fixture := newJletFixture(t, store)
	fixture.jlet(
		javaMember{Name: javaJletPauseApp, Descriptor: "()V", Body: fixture.lifecycleMethod(lifecyclePauses, nil)},
		javaMember{Name: javaJletResumeApp, Descriptor: "()V", Body: fixture.lifecycleMethod(lifecycleResumes, nil)},
	)
	fixture.settle()
	if err := fixture.session.Pause(ctx); err != nil {
		t.Fatal(err)
	}
	checkpoint := roundTripCheckpoint(t, fixture.archive, fixture.session)
	if got := fixture.lifecycleWord(lifecycleResumes); got != 0 {
		t.Fatalf("the quick save ran resumeApp %d times", got)
	}
	restored := fixture.restore(checkpoint)
	if pauses, resumes := guestWord(t, restored, fixture.counter(lifecyclePauses)), guestWord(t, restored, fixture.counter(lifecycleResumes)); pauses != 1 || resumes != 0 {
		t.Fatalf("the load holds %d pauses and %d resumes, want what the parked session had", pauses, resumes)
	}
	if err := restored.Resume(ctx); err != nil {
		t.Fatalf("Resume() after the load = %v", err)
	}
	if got := guestWord(t, restored, fixture.counter(lifecycleResumes)); got != 1 {
		t.Fatalf("the loaded session's resumeApp ran %d times, want once", got)
	}
	tickSession(t, restored, 2)
}

// serialFrame makes the Runnable a title's frame chain hands callSerially: it
// counts its run and records the flag it found, which is what a title's run
// reads to decide whether to ask to be called again.
func (fixture *javaThreadFixture) serialFrame() uint32 {
	fixture.t.Helper()
	run := fixture.install(func(a *assembler) {
		a.emit(armPushLR)
		a.literal(4, fixture.counter(serialPaused))
		a.emit(armLdr(1, 4, 0))
		a.literal(4, fixture.counter(serialSaw))
		a.emit(armStr(1, 4, 0))
		fixture.increment(a, serialRuns)
		a.emit(armPopPC)
	})
	_, runnable := fixture.newClass("Frame", run)
	return runnable
}

// setSerialFlag is a lifecycle body that writes the title's flag.
func (fixture *javaThreadFixture) setSerialFlag(value uint32) func(a *assembler) {
	return func(a *assembler) {
		a.literal(4, fixture.counter(serialPaused))
		a.literal(0, value)
		a.emit(armStr(0, 4, 0))
	}
}

// A Runnable a title handed callSerially before it was parked runs during the
// park, after pauseApp: a handset serves the queue while the application is
// paused, and that run is the one that finds the flag pauseApp set and stops
// asking to be called. A park runs no tick until the resume, so without this
// the run was still queued when resumeApp queued one of its own, and the title
// ran its frame twice a tick from then on.
func TestParkingAJavaTitleServesTheSerialCallsItQueuedBeforePauseApp(t *testing.T) {
	ctx := context.Background()
	store, _ := backend.NewMemorySaveStore(nil)
	fixture := newJletFixture(t, store)
	fixture.jlet(
		javaMember{Name: javaJletPauseApp, Descriptor: "()V", Body: fixture.lifecycleMethod(lifecyclePauses, fixture.setSerialFlag(1))},
		javaMember{Name: javaJletResumeApp, Descriptor: "()V", Body: fixture.lifecycleMethod(lifecycleResumes, fixture.setSerialFlag(0))},
	)
	frame := fixture.serialFrame()
	fixture.settle()
	runtime := fixture.client.javaRuntimeState()
	runtime.serial = append(runtime.serial, frame)

	if err := fixture.session.Pause(ctx); err != nil {
		t.Fatalf("Pause() = %v", err)
	}
	if runs, saw := fixture.lifecycleWord(serialRuns), fixture.lifecycleWord(serialSaw); runs != 1 || saw != 1 {
		t.Fatalf("during the park the queued run ran %d times and found the flag %d, want once and set", runs, saw)
	}
	if len(runtime.serial) != 0 {
		t.Fatalf("after the park %d serial calls are still queued", len(runtime.serial))
	}
	if err := fixture.session.Resume(ctx); err != nil {
		t.Fatalf("Resume() = %v", err)
	}
	tickSession(t, fixture.session, 2)
	if runs := fixture.lifecycleWord(serialRuns); runs != 1 {
		t.Fatalf("the run ran %d times, want only the once during the park", runs)
	}
}

// A title that declares no pauseApp was told nothing, so the run it queued has
// no pause to find: the park leaves it for the first tick after the resume, as
// a park always did.
func TestParkingATitleWithoutPauseAppLeavesItsSerialCallsForTheTicks(t *testing.T) {
	ctx := context.Background()
	store, _ := backend.NewMemorySaveStore(nil)
	fixture := newJletFixture(t, store)
	fixture.jlet(
		javaMember{Name: javaJletResumeApp, Descriptor: "()V", Body: fixture.lifecycleMethod(lifecycleResumes, nil)},
	)
	frame := fixture.serialFrame()
	fixture.settle()
	runtime := fixture.client.javaRuntimeState()
	runtime.serial = append(runtime.serial, frame)

	if err := fixture.session.Pause(ctx); err != nil {
		t.Fatalf("Pause() = %v", err)
	}
	if runs := fixture.lifecycleWord(serialRuns); runs != 0 || len(runtime.serial) != 1 {
		t.Fatalf("the park ran the queued call %d times and left %d queued, want none run and one queued", runs, len(runtime.serial))
	}
	if err := fixture.session.Resume(ctx); err != nil {
		t.Fatalf("Resume() = %v", err)
	}
	tickSession(t, fixture.session, 1)
	if runs := fixture.lifecycleWord(serialRuns); runs != 1 {
		t.Fatalf("the first tick after the resume ran the queued call %d times, want once", runs)
	}
}
