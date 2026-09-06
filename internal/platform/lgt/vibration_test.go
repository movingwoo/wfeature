package lgt

import (
	"context"
	"testing"
	"time"
)

// This platform answers the guest's vibration on two surfaces — the WIPI C
// media block and the Java class — and both of them used to return success
// without reading their arguments, on the reasoning that there is no motor
// here. Whether there is one is the Host's answer rather than this runtime's,
// so both now record the request where a Host reads it. These check that they
// reach the same place, because a title that uses one and not the other must
// not be the difference between a buzz and silence.

func TestWIPICVibratorRecordsTheRequest(t *testing.T) {
	client := fixtureClient(t)

	if state := client.vibrator.State(); state.Request != 0 || state.Active() {
		t.Fatalf("a title that has not asked reports %+v", state)
	}

	if result := callSlot(t, client, slotVibrator, 40, 120); result != uint32(wipiSuccess) {
		t.Errorf("MC_mdaVibrator answered %d, want success", result)
	}
	state := client.vibrator.State()
	if state.Request != 1 || state.Level != 40 || state.Duration != 120*time.Millisecond {
		t.Fatalf("state = %+v, want level 40 for 120ms", state)
	}
	if !state.Active() {
		t.Errorf("the request is not running: %+v", state)
	}

	// A level of zero is how the guest turns the motor off through this call,
	// and it is a request of its own so that a Host watching the counter
	// learns about it rather than waiting out a duration.
	if result := callSlot(t, client, slotVibrator, 0, 0); result != uint32(wipiSuccess) {
		t.Errorf("MC_mdaVibrator(0, 0) answered %d, want success", result)
	}
	if state := client.vibrator.State(); state.Request != 2 || state.Active() {
		t.Errorf("the motor was left running: %+v", state)
	}
}

func TestJavaVibratorReachesTheSameBoundary(t *testing.T) {
	client := fixtureClient(t)

	if _, err := javaVibratorOn(client, context.Background(), nil, []uint32{60, 250}); err != nil {
		t.Fatalf("Vibrator.on: %v", err)
	}
	state := client.vibrator.State()
	if state.Request != 1 || state.Level != 60 || state.Duration != 250*time.Millisecond {
		t.Fatalf("state = %+v", state)
	}

	if _, err := javaVibratorOff(client, context.Background(), nil, nil); err != nil {
		t.Fatalf("Vibrator.off: %v", err)
	}
	if state := client.vibrator.State(); state.Request != 2 || state.Level != 0 || state.Active() {
		t.Errorf("stop left %+v", state)
	}
}

// A duration of zero at a level above zero is the guest asking for a vibration
// that runs until it stops it, which is the rule in the specification a reader
// would not guess. Taking it for "no time at all" is how a continuous buzz
// becomes silence.
func TestVibratorZeroDurationIsUntilStopped(t *testing.T) {
	client := fixtureClient(t)
	callSlot(t, client, slotVibrator, 100, 0)
	state := client.vibrator.State()
	if !state.Indefinite() || !state.Active() {
		t.Errorf("a zero duration was not read as indefinite: %+v", state)
	}
}

// The session is where a Host reads the request, and a session that never ran
// a guest has nothing to report rather than a buzz nobody asked for.
func TestSessionReportsTheVibrationRequest(t *testing.T) {
	client := fixtureClient(t)
	session := &Session{client: client}

	if state := session.Vibration(); state.Request != 0 || state.Active() {
		t.Fatalf("an untouched session reports %+v", state)
	}

	callSlot(t, client, slotVibrator, 75, 300)
	state := session.Vibration()
	if state.Request != 1 || state.Level != 75 || state.Duration != 300*time.Millisecond {
		t.Errorf("session state = %+v", state)
	}

	var absent *Session
	if state := absent.Vibration(); state.Request != 0 {
		t.Errorf("a session that is not there reports %+v", state)
	}
}
