package skt

import (
	_ "embed"
	"errors"
	"testing"
	"time"
)

//go:embed testdata/async-failure.jar
var asyncFailureJAR []byte

func TestJARThreadJoinAndRestartRefusal(t *testing.T) {
	archive, err := Open(asyncFailureJAR)
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := Start(archive, testRuntimeOptions(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(runtime.VM.Close)
	if got := invokeFixtureInt(t, runtime, "AsyncFailureMIDlet", "joinWorker"); got != 42 {
		t.Fatalf("joined result = %d", got)
	}
}

func TestBackgroundFailureReachesSession(t *testing.T) {
	archive, err := Open(asyncFailureJAR)
	if err != nil {
		t.Fatal(err)
	}
	observed := make(chan error, 2)
	options := testRuntimeOptions(t)
	options.JVM.AsyncError = func(err error) { observed <- err }
	runtime, err := Start(archive, options)
	if err != nil {
		t.Fatal(err)
	}
	var first error
	for i := 0; i < 2; i++ {
		if _, err := runtime.VM.InvokeStatic("AsyncFailureMIDlet", "startFailure", "()V"); err != nil {
			t.Fatal(err)
		}
		select {
		case err := <-observed:
			if i == 0 {
				first = err
			}
		case <-time.After(time.Second):
			t.Fatal("background failure was not reported")
		}
	}
	err = runtime.RunPending()
	if err == nil || !errors.Is(err, first) {
		t.Fatalf("RunPending = %v, want first error %v", err, first)
	}
	if runtime.State() != StateError {
		t.Fatalf("state = %s", runtime.State())
	}
	if next := runtime.RunPending(); next != err {
		t.Fatalf("failure changed: %v", next)
	}
}
