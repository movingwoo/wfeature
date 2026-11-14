package skt

import (
	_ "embed"
	"strings"
	"testing"
	"time"
)

//go:embed testdata/restarting.jar
var restartingJAR []byte

// resumingJAR is a MIDlet that declares a resumeApp of its own and counts its
// lifecycle callbacks. See testdata/src/ResumingMIDlet.java.
//
//go:embed testdata/resuming.jar
var resumingJAR []byte

// resumeJletJAR is a Jlet that counts its lifecycle callbacks; its resumeApp
// clears the flag its pauseApp set. See testdata/src/ResumeJlet.java.
//
//go:embed testdata/resume-jlet.jar
var resumeJletJAR []byte

// restartLoopJAR is a MIDlet whose pauseApp ends its loop and whose startApp
// starts that same Thread again once the loop has ended. See
// testdata/src/RestartLoopMIDlet.java.
//
//go:embed testdata/restart-loop.jar
var restartLoopJAR []byte

func TestResumeCompatibilityDoesNotSkipDeferredInitialStart(t *testing.T) {
	archive, err := Open(deferredStartJAR)
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := Start(archive, testRuntimeOptions(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtime.Destroy(true) })
	runtime.resumeWithoutStart = true
	assertLifecycleState(t, runtime, StatePaused, 7)
	if err := runtime.Resume(); err != nil {
		t.Fatal(err)
	}
	assertLifecycleState(t, runtime, StateActive, 2)
}

func TestRecognizedResumePreservesCanvasProgressAndWorkers(t *testing.T) {
	archive, err := Open(restartingJAR)
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := Start(archive, testRuntimeOptions(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtime.Destroy(true) })
	// The authored class set is deliberately not in the release registry.
	// Enable the same selected behavior here; local fingerprint tests cover
	// which original archives are allowed to select it.
	runtime.resumeWithoutStart = true
	canvas := runtime.currentDisplayable
	workers := len(runtime.VM.ThreadObjects())
	for cycle := 1; cycle <= 3; cycle++ {
		if err := runtime.SendKey(KeyPressed, KeyCodeFire); err != nil {
			t.Fatal(err)
		}
		if err := runtime.Pause(); err != nil {
			t.Fatal(err)
		}
		if err := runtime.Resume(); err != nil {
			t.Fatal(err)
		}
		if runtime.currentDisplayable != canvas {
			t.Fatal("resume replaced the running Canvas")
		}
		if got := len(runtime.VM.ThreadObjects()); got != workers {
			t.Fatalf("resume changed worker count from %d to %d", workers, got)
		}
		if got := invokeFixtureInt(t, runtime, "RestartingMIDlet", "progress"); got != int32(cycle) {
			t.Fatalf("progress = %d, want %d", got, cycle)
		}
	}
	if got := invokeFixtureInt(t, runtime, "RestartingMIDlet", "starts"); got != 1 {
		t.Fatalf("start callbacks = %d, want 1", got)
	}
	if got := invokeFixtureInt(t, runtime, "RestartingMIDlet", "pauses"); got != 3 {
		t.Fatalf("pause callbacks = %d, want 3", got)
	}
}

// **An SKT MIDlet is resumed, not started again.** The fixture's startApp
// replaces its Canvas and starts another worker every time it runs, as SKT
// titles do; under MIDP's reactivation through startApp a park left it with a
// second game beside the first. It has no resumeApp, so a resume calls nothing.
func TestAnSKTMIDletIsResumedWithoutASecondStart(t *testing.T) {
	archive, err := Open(restartingJAR)
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := Start(archive, testRuntimeOptions(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtime.Destroy(true) })
	canvas := runtime.currentDisplayable
	workers := len(runtime.VM.ThreadObjects())
	for cycle := 1; cycle <= 2; cycle++ {
		if err := runtime.Pause(); err != nil {
			t.Fatal(err)
		}
		if err := runtime.Resume(); err != nil {
			t.Fatal(err)
		}
		if runtime.State() != StateActive {
			t.Fatalf("park %d: the MIDlet is %s after its resume", cycle, runtime.State())
		}
	}
	if got := invokeFixtureInt(t, runtime, "RestartingMIDlet", "starts"); got != 1 {
		t.Fatalf("startApp ran %d times, want only the first", got)
	}
	if runtime.currentDisplayable != canvas {
		t.Fatal("a resume replaced the running Canvas")
	}
	if got := len(runtime.VM.ThreadObjects()); got != workers {
		t.Fatalf("a resume changed the worker count from %d to %d", workers, got)
	}
}

// A MIDlet that declares a resumeApp of its own — which MIDP does not have,
// and 56 of the 93 local SKT MIDlets do — is resumed through it, once a park,
// and its startApp runs once, at the start. An exception its resumeApp lets
// out ends that callback, not the title: one title's resumeApp does what it
// has to and then calls notify without holding the monitor.
func TestAMIDletThatDeclaresResumeAppIsResumedThroughIt(t *testing.T) {
	archive, err := Open(resumingJAR)
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := Start(archive, testRuntimeOptions(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtime.Destroy(true) })
	if runtime.jlet {
		t.Fatal("the fixture's main class was taken for a Jlet")
	}
	for cycle := int32(1); cycle <= 2; cycle++ {
		if err := runtime.Pause(); err != nil {
			t.Fatal(err)
		}
		if err := runtime.Resume(); err != nil {
			t.Fatal(err)
		}
		if got := invokeFixtureInt(t, runtime, "ResumingMIDlet", "resumes"); got != cycle {
			t.Fatalf("park %d: resumeApp ran %d times, want %d", cycle, got, cycle)
		}
		if got := invokeFixtureInt(t, runtime, "ResumingMIDlet", "starts"); got != 1 {
			t.Fatalf("park %d: startApp ran %d times, want only the first", cycle, got)
		}
		if invokeFixtureBoolean(t, runtime, "ResumingMIDlet", "paused") || runtime.State() != StateActive {
			t.Fatalf("park %d: the MIDlet is still paused after its resume", cycle)
		}
	}

	if _, err := runtime.VM.InvokeStatic("ResumingMIDlet", "failNextResume", "()V"); err != nil {
		t.Fatal(err)
	}
	if err := runtime.Pause(); err != nil {
		t.Fatal(err)
	}
	if err := runtime.Resume(); err != nil {
		t.Fatalf("Resume() over a throwing resumeApp = %v, want the title to go on", err)
	}
	if runtime.State() != StateActive {
		t.Fatalf("the MIDlet is %s after a resumeApp that threw", runtime.State())
	}
	count, first := runtime.UncaughtCallbacks()
	if count != 1 || !strings.Contains(first, "java/lang/ArithmeticException") {
		t.Fatalf("UncaughtCallbacks() = %d, %q; want the one resumeApp exception", count, first)
	}
}

// A parked Jlet comes back through its own resumeApp, as the WIPI lifecycle
// has it and as the KTF and LGT runtimes drive it: startApp runs once, at the
// start. A title whose pauseApp stops its frame timer restarts it in
// resumeApp, so a resume that ran startApp again instead left the game paused
// for good — two of the three local Jlet archives stood still after any park
// that way, their keys ignored.
func TestAParkedJletIsResumedThroughResumeApp(t *testing.T) {
	archive, err := Open(resumeJletJAR)
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := Start(archive, testRuntimeOptions(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtime.Destroy(true) })
	if !runtime.jlet {
		t.Fatal("the fixture's main class was not taken for a Jlet")
	}
	if got := invokeFixtureInt(t, runtime, "ResumeJlet", "starts"); got != 1 {
		t.Fatalf("startApp ran %d times at the start, want 1", got)
	}
	for cycle := int32(1); cycle <= 2; cycle++ {
		if err := runtime.Pause(); err != nil {
			t.Fatal(err)
		}
		if err := runtime.Resume(); err != nil {
			t.Fatal(err)
		}
		if got := invokeFixtureInt(t, runtime, "ResumeJlet", "pauses"); got != cycle {
			t.Fatalf("park %d: pauseApp ran %d times, want %d", cycle, got, cycle)
		}
		if got := invokeFixtureInt(t, runtime, "ResumeJlet", "resumes"); got != cycle {
			t.Fatalf("park %d: resumeApp ran %d times, want %d", cycle, got, cycle)
		}
		if got := invokeFixtureInt(t, runtime, "ResumeJlet", "starts"); got != 1 {
			t.Fatalf("park %d: startApp ran %d times, want only the first", cycle, got)
		}
		if invokeFixtureBoolean(t, runtime, "ResumeJlet", "paused") {
			t.Fatalf("park %d: the Jlet is still paused after its resume", cycle)
		}
	}
}

// A resumed title's screen is drawn again, as a handset redrew it after what
// had covered it. One title's resumeApp marks its screen for a redraw that
// only a paint carries out, and another's thread waits for the notify its
// paint ends with; the second startApp used to bring that paint by setting
// its Canvas current again.
func TestAResumedMIDletPaintsItsScreenAgain(t *testing.T) {
	archive, err := Open(canvasJAR)
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := Start(archive, Options{Framebuffer: newTestFramebuffer(t, 4, 3)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtime.Destroy(true) })
	before := invokeFixtureInt(t, runtime, "CanvasMIDlet", "paintCount")
	if err := runtime.Pause(); err != nil {
		t.Fatal(err)
	}
	if err := runtime.Resume(); err != nil {
		t.Fatal(err)
	}
	if err := runtime.RunPending(); err != nil {
		t.Fatal(err)
	}
	if got := invokeFixtureInt(t, runtime, "CanvasMIDlet", "paintCount"); got != before+1 {
		t.Fatalf("paintCount() = %d after a resume, want %d: the screen was not drawn again", got, before+1)
	}
}

// A resumed title that is drawing anyway — here a frame loop that hands itself
// back through callSerially — is not asked for another paint. A handset's
// repaint joined the one pending; a second paint here started a second loop
// beside the first, and one title ran at double rate after every park.
func TestAResumedFrameLoopIsNotPaintedTwice(t *testing.T) {
	archive, err := Open(serialInputJAR)
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := Start(archive, Options{Framebuffer: newTestFramebuffer(t, 4, 3)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtime.Destroy(true) })
	for pass := 0; pass < 3; pass++ {
		if err := runtime.RunPending(); err != nil {
			t.Fatal(err)
		}
	}
	if err := runtime.Pause(); err != nil {
		t.Fatal(err)
	}
	// The moment between a loop's paint and its next repaint: its serial call
	// waits and no paint is queued yet. The title that doubled was parked
	// there, its serial call asleep before the repaint it would make.
	runtime.displayMu.Lock()
	looping := len(runtime.pendingSerial) > 0
	runtime.paintQueued = false
	runtime.displayMu.Unlock()
	if !looping {
		t.Fatal("the fixture's frame loop has no serial call waiting at the park")
	}
	before := invokeFixtureInt(t, runtime, "SerialInputMIDlet", "paints")
	if err := runtime.Resume(); err != nil {
		t.Fatal(err)
	}
	if got := invokeFixtureInt(t, runtime, "SerialInputMIDlet", "paints"); got != before {
		t.Fatalf("a resume painted %d times a title whose frame loop was about to draw", got-before)
	}
}

// **A class set written for MIDP's reactivation is resumed through startApp,
// its ended loop started again.** Two titles end a thread in pauseApp and, in
// startApp, start that same Thread object again once it is no longer alive,
// which the language refuses and the handset VM they were written for allowed.
// Selected, as skt.resume_through_start selects it, the fixture's startApp
// runs on the resume and its loop runs again; unselected, the same MIDlet is
// resumed without a second startApp, as every other title is, and its loop
// stays ended.
func TestAResumeThroughStartRunsTheEndedLoopAgain(t *testing.T) {
	for _, selected := range []bool{false, true} {
		name := "unselected"
		if selected {
			name = "selected"
		}
		t.Run(name, func(t *testing.T) {
			archive, err := Open(restartLoopJAR)
			if err != nil {
				t.Fatal(err)
			}
			// The authored class set is not in the release registry; what an
			// entry selects in newRuntime is selected here instead.
			options := testRuntimeOptions(t)
			options.JVM.RestartableThreads = selected
			runtime, err := Start(archive, options)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = runtime.Destroy(true) })
			runtime.resumeThroughStart = selected
			fixture := func(method string) int32 {
				t.Helper()
				return invokeFixtureInt(t, runtime, "RestartLoopMIDlet", method)
			}
			waitFor := func(what string, done func() bool) {
				t.Helper()
				for deadline := time.Now().Add(5 * time.Second); !done(); time.Sleep(5 * time.Millisecond) {
					if time.Now().After(deadline) {
						t.Fatalf("%s: timed out", what)
					}
				}
			}
			waitFor("the loop runs", func() bool { return fixture("frames") > 0 })
			if err := runtime.Pause(); err != nil {
				t.Fatal(err)
			}
			waitFor("the loop ends on pause", func() bool { return fixture("alive") == 0 })
			if err := runtime.Resume(); err != nil {
				t.Fatal(err)
			}
			if state := runtime.State(); state != StateActive {
				t.Fatalf("state after resume = %v, want active", state)
			}
			if !selected {
				if starts, runs, alive := fixture("starts"), fixture("runs"), fixture("alive"); starts != 1 || runs != 1 || alive != 0 {
					t.Fatalf("unselected resume: starts %d, runs %d, alive %d; want 1, 1, 0", starts, runs, alive)
				}
				return
			}
			if starts, refused := fixture("starts"), fixture("refused"); starts != 2 || refused != 0 {
				t.Fatalf("selected resume: starts %d, refused restarts %d; want 2, 0", starts, refused)
			}
			frames := fixture("frames")
			waitFor("the restarted loop runs", func() bool { return fixture("runs") == 2 && fixture("frames") > frames })
			if fixture("alive") != 1 {
				t.Fatal("the restarted loop is not alive")
			}
		})
	}
}
