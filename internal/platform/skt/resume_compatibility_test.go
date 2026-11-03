package skt

import (
	_ "embed"
	"testing"
)

//go:embed testdata/restarting.jar
var restartingJAR []byte

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

func TestUnrecognizedResumeStillInvokesStart(t *testing.T) {
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
	if err := runtime.Pause(); err != nil {
		t.Fatal(err)
	}
	if err := runtime.Resume(); err != nil {
		t.Fatal(err)
	}
	if got := invokeFixtureInt(t, runtime, "RestartingMIDlet", "starts"); got != 2 {
		t.Fatalf("standard start callbacks = %d, want 2", got)
	}
	if runtime.currentDisplayable == canvas {
		t.Fatal("standard MIDP resume did not run the fixture's start callback")
	}
}
