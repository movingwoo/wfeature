package ktf

import (
	"testing"
	"time"

	"github.com/movingwoo/wfeature/internal/jvm"
)

// The guest's vibration used to reach a diagnostic counter and stop there,
// which a run report could read and nothing else could. What a Host needs is
// the request itself, so these call the implementations the runtime registers
// for `Vibrator.on` and `Vibrator.off` and read them back where a Host reads
// them. That the two are registered under those names at all is what
// TestRuntimeJavaSurfaceCoversReference keeps.
func vibrationRuntime(t *testing.T) *initializationRuntime {
	t.Helper()
	_, runtime := newTestRuntime(t)
	return runtime
}

func TestVibratorRequestReachesTheBoundary(t *testing.T) {
	runtime := vibrationRuntime(t)

	if state := runtime.client.vibrator.State(); state.Request != 0 || state.Active() {
		t.Fatalf("a title that has not asked reports %+v", state)
	}

	if _, err := runtimeVibratorOn(runtime, nil, []jvm.Value{jvm.IntValue(60), jvm.IntValue(250)}); err != nil {
		t.Fatalf("Vibrator.on: %v", err)
	}
	state := runtime.client.vibrator.State()
	if state.Request != 1 || state.Level != 60 || state.Duration != 250*time.Millisecond {
		t.Fatalf("state = %+v", state)
	}
	if !state.Active() {
		t.Errorf("the request is not running: %+v", state)
	}

	if _, err := runtimeVibratorOff(runtime, nil, nil); err != nil {
		t.Fatalf("Vibrator.off: %v", err)
	}
	state = runtime.client.vibrator.State()
	// The stop is a request of its own, so a Host watching the counter learns
	// the motor was turned off rather than waiting out a duration.
	if state.Request != 2 || state.Level != 0 || state.Active() {
		t.Errorf("stop left %+v", state)
	}
}

// A duration of zero at a level above zero is the guest asking for a vibration
// that runs until it stops it, which is the rule in the specification a reader
// would not guess. Taking it for "no time at all" is how a continuous buzz
// becomes silence.
func TestVibratorZeroDurationIsUntilStopped(t *testing.T) {
	runtime := vibrationRuntime(t)
	if _, err := runtimeVibratorOn(runtime, nil, []jvm.Value{jvm.IntValue(100), jvm.IntValue(0)}); err != nil {
		t.Fatalf("Vibrator.on: %v", err)
	}
	state := runtime.client.vibrator.State()
	if !state.Indefinite() || !state.Active() {
		t.Errorf("a zero duration was not read as indefinite: %+v", state)
	}
}

// The WIPI C media block reaches the same vibrator as the Java class. Its two
// arguments used to go unread entirely, on the reasoning that there is no
// vibrator here — which is the Host's answer rather than this runtime's.
func TestWIPICVibratorReachesTheSameBoundary(t *testing.T) {
	runtime := vibrationRuntime(t)
	if result := mediaCall(t, runtime, wipicMediaVibrator, 40, 120); result != 0 {
		t.Errorf("MC_mdaVibrator answered %d, want success", result)
	}
	state := runtime.client.vibrator.State()
	if state.Level != 40 || state.Duration != 120*time.Millisecond {
		t.Errorf("state = %+v, want level 40 for 120ms", state)
	}

	// Level zero is how the guest turns it off through this call too; the
	// local titles pass (0, 0).
	if result := mediaCall(t, runtime, wipicMediaVibrator, 0, 0); result != 0 {
		t.Errorf("MC_mdaVibrator(0, 0) answered %d, want success", result)
	}
	if state := runtime.client.vibrator.State(); state.Active() || state.Request != 2 {
		t.Errorf("the motor was left running: %+v", state)
	}
}
