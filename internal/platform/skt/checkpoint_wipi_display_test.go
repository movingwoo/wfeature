package skt

import (
	"testing"
	"time"

	"github.com/movingwoo/wfeature/internal/api/midp"
	"github.com/movingwoo/wfeature/internal/api/wipi"
	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/jvm"
)

func startWIPIDisplayCheckpoint(t *testing.T) (*Runtime, *backend.MemorySaveStore) {
	t.Helper()
	archive, err := Open(audioGainJAR)
	if err != nil {
		t.Fatal(err)
	}
	store, err := backend.NewMemorySaveStore(nil)
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := Start(archive, Options{Framebuffer: newTestFramebuffer(t, 32, 24), SaveStore: store})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtime.Destroy(true) })
	if runtime.display != nil || runtime.displayOwner != nil {
		t.Fatal("the authored fixture unexpectedly initialized a Display")
	}
	return runtime, store
}

func invokeWIPICheckpointDisplay(t *testing.T, runtime *Runtime, named bool) *jvm.Object {
	t.Helper()
	method, descriptor := "getDefaultDisplay", "()L"+wipi.DisplayClass+";"
	var arguments []jvm.Value
	if named {
		method, descriptor = "getDisplay", "(Ljava/lang/String;)L"+wipi.DisplayClass+";"
		arguments = []jvm.Value{jvm.ReferenceValue(runtime.VM.NewString("default"))}
	}
	value, err := runtime.VM.InvokeStatic(wipi.DisplayClass, method, descriptor, arguments...)
	if err != nil {
		t.Fatal(err)
	}
	object, err := value.Reference()
	if err != nil || object == nil {
		t.Fatalf("WIPI display factory returned %v, %v", object, err)
	}
	return object
}

func checkpointAudioGainClip(t *testing.T, runtime *Runtime, name string) (*jvm.Object, backend.AudioHandle) {
	t.Helper()
	value, err := runtime.VM.StaticField("AudioGainMIDlet", name, "L"+wipi.ClipClass+";")
	if err != nil {
		t.Fatal(err)
	}
	clip, err := value.Reference()
	if err != nil || clip == nil {
		t.Fatalf("missing static clip %s: %v", name, err)
	}
	player := clip.Native.(*wipiClipData).player
	handle := player.Native.(*playerData).handle
	if runtime.mediaPlayers[handle] != player {
		t.Fatalf("static clip %s lost its Player registry alias", name)
	}
	return clip, handle
}

func TestWIPIDisplayCheckpointWithoutMIDPOwnerPreservesAudio(t *testing.T) {
	for _, named := range []bool{false, true} {
		name := "default factory"
		if named {
			name = "named factory"
		}
		t.Run(name, func(t *testing.T) {
			runtime, store := startWIPIDisplayCheckpoint(t)
			display := invokeWIPICheckpointDisplay(t, runtime, named)
			if display.ClassName != wipi.DisplayClass || display != invokeWIPICheckpointDisplay(t, runtime, !named) || runtime.displayOwner != nil {
				t.Fatal("WIPI factories changed display identity or invented a MIDP owner")
			}
			sink := &sktGainSink{}
			runtime.AttachAudioSink(sink)
			guestNow := time.Now()
			runtime.pace, runtime.paceStart = backend.NewSpeedClock(func() time.Time { return guestNow }), guestNow
			if _, err := runtime.VM.InvokeStatic("AudioGainMIDlet", "begin", "([B)V", jvm.ReferenceValue(jvm.NewByteArray(audioStopSound()))); err != nil {
				t.Fatal(err)
			}
			runtime.AdvanceAudio()
			guestNow = guestNow.Add(100 * time.Millisecond)
			runtime.AdvanceAudio()
			first, firstHandle := checkpointAudioGainClip(t, runtime, "first")
			second, secondHandle := checkpointAudioGainClip(t, runtime, "second")
			if sink.notes != 2 || sink.gains[firstHandle] != 2500 || sink.gains[secondHandle] != 10000 {
				t.Fatalf("authored clip setup failed: notes=%d, gains=%v", sink.notes, sink.gains)
			}
			slot, err := runtime.CaptureCheckpointWithSession(t.Context(), nil)
			if err != nil {
				t.Fatalf("valid ownerless WIPI display refused capture: %v", err)
			}
			var saved javaCheckpointState
			if err := backend.DecodeCheckpointRecord(slot.Runtime, &saved); err != nil {
				t.Fatal(err)
			}
			if saved.Platform.Display == 0 || saved.Platform.DisplayOwner != 0 || len(saved.Audio.Output.Notes) != 2 {
				t.Fatal("checkpoint lost the ownerless display or held audio")
			}
			prepared, err := PrepareJavaCheckpoint(audioGainJAR, slot, Options{})
			if err != nil {
				t.Fatal(err)
			}
			defer prepared.Discard()
			freshSink := &sktGainSink{}
			restored, err := prepared.Commit(t.Context(), runtime, store, newTestFramebuffer(t, 32, 24), freshSink)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = restored.Destroy(true) })
			restored.ResumeCheckpointOutput()
			freshDisplay := invokeWIPICheckpointDisplay(t, restored, named)
			if freshDisplay == display || freshDisplay != restored.display || freshDisplay != invokeWIPICheckpointDisplay(t, restored, !named) || restored.displayOwner != nil {
				t.Fatal("restore changed factory identity or ownerless display semantics")
			}
			freshFirst, restoredFirst := checkpointAudioGainClip(t, restored, "first")
			freshSecond, restoredSecond := checkpointAudioGainClip(t, restored, "second")
			if freshFirst == first || freshSecond == second || freshFirst == freshSecond || restoredFirst != firstHandle || restoredSecond != secondHandle ||
				freshSink.notes != 2 || freshSink.gains[firstHandle] != 2500 || freshSink.gains[secondHandle] != 10000 {
				t.Fatalf("restore changed clip roots or audio: notes=%d, gains=%v", freshSink.notes, freshSink.gains)
			}
			if _, err := restored.VM.InvokeStatic("AudioGainMIDlet", "setDeviceLevel", "(I)I", jvm.IntValue(50)); err != nil || freshSink.gains[firstHandle] != 1250 || freshSink.gains[secondHandle] != 5000 || freshSink.notes != 2 {
				t.Fatalf("restored live gain changed playback: gains=%v, notes=%d, err=%v", freshSink.gains, freshSink.notes, err)
			}
			if _, err := restored.CaptureCheckpointWithSession(t.Context(), nil); err != nil {
				t.Fatalf("restored ownerless display could not be captured again: %v", err)
			}
		})
	}
}

func TestWIPIFirstDisplayBindsFirstMIDPOwner(t *testing.T) {
	runtime, store := startWIPIDisplayCheckpoint(t)
	display := invokeWIPICheckpointDisplay(t, runtime, false)
	lookup := func(owner *jvm.Object) (jvm.Value, error) {
		return runtime.VM.InvokeStatic(midp.DisplayClass, "getDisplay", "(L"+midp.MIDletClass+";)L"+midp.DisplayClass+";", jvm.ReferenceValue(owner))
	}
	value, err := lookup(runtime.MIDlet)
	if err != nil {
		t.Fatal(err)
	}
	actual, _ := value.Reference()
	if actual != display || runtime.displayOwner != runtime.MIDlet {
		t.Errorf("MIDP lookup did not bind the existing WIPI display to its first owner")
	}
	other, err := runtime.VM.NewObject("AudioGainMIDlet", "()V")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := lookup(other); err == nil {
		t.Error("a second MIDlet instance acquired the first owner's display")
	}
	if _, err := lookup(runtime.MIDlet); err != nil || runtime.display != display || runtime.displayOwner != runtime.MIDlet {
		t.Fatalf("rejected lookup changed the original owner's display: %v", err)
	}
	slot, err := runtime.CaptureCheckpointWithSession(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := PrepareJavaCheckpoint(audioGainJAR, slot, Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer prepared.Discard()
	restored, err := prepared.Commit(t.Context(), runtime, store, newTestFramebuffer(t, 32, 24), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = restored.Destroy(true) })
	if restored.display == display || restored.displayOwner != restored.MIDlet || restored.display.ClassName != wipi.DisplayClass {
		t.Fatal("checkpoint lost the bound WIPI display and MIDlet root alias")
	}
	runtime = restored
	value, err = lookup(runtime.MIDlet)
	actual, _ = value.Reference()
	if err != nil || actual != runtime.display || actual != invokeWIPICheckpointDisplay(t, runtime, true) {
		t.Fatalf("restored MIDP and WIPI factories no longer share the display: %v", err)
	}
}

func TestWIPIDisplayCheckpointRejectsInvalidOwnershipAtCapture(t *testing.T) {
	for _, ownerless := range []bool{true, false} {
		name := "MIDP display without owner"
		if !ownerless {
			name = "owner without display"
		}
		t.Run(name, func(t *testing.T) {
			runtime, _ := startWIPIDisplayCheckpoint(t)
			if _, err := runtime.VM.InvokeStatic(midp.DisplayClass, "getDisplay", "(L"+midp.MIDletClass+";)L"+midp.DisplayClass+";", jvm.ReferenceValue(runtime.MIDlet)); err != nil {
				t.Fatal(err)
			}
			if ownerless {
				runtime.displayOwner = nil
			} else {
				runtime.display = nil
			}
			if _, err := runtime.CaptureCheckpointWithSession(t.Context(), nil); err == nil {
				t.Fatal("capture accepted malformed display ownership")
			}
			if runtime.State() != StateActive {
				t.Fatal("refused capture changed the MIDlet lifecycle")
			}
		})
	}
}

func TestWIPIDisplayCheckpointRejectsInvalidTypedOwnership(t *testing.T) {
	runtime, _ := startWIPIDisplayCheckpoint(t)
	value, err := runtime.VM.InvokeStatic(midp.DisplayClass, "getDisplay", "(L"+midp.MIDletClass+";)L"+midp.DisplayClass+";", jvm.ReferenceValue(runtime.MIDlet))
	if err != nil {
		t.Fatal(err)
	}
	display, _ := value.Reference()
	slot, err := runtime.CaptureCheckpointWithSession(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	valid, err := PrepareJavaCheckpoint(audioGainJAR, slot, Options{})
	if err != nil {
		t.Fatalf("valid MIDP display checkpoint was rejected: %v", err)
	}
	valid.Discard()
	for _, test := range []struct {
		name   string
		mutate func(*javaPlatformState)
	}{
		{"MIDP display without owner", func(state *javaPlatformState) { state.DisplayOwner = 0 }},
		{"owner without display", func(state *javaPlatformState) { state.Display = 0 }},
		{"owner is display", func(state *javaPlatformState) { state.DisplayOwner = state.Display }},
		{"owner is outside roots", func(state *javaPlatformState) { state.DisplayOwner = state.RootCount + 1 }},
		{"display is MIDlet", func(state *javaPlatformState) { state.Display = state.MIDlet }},
	} {
		t.Run(test.name, func(t *testing.T) {
			var saved javaCheckpointState
			if err := backend.DecodeCheckpointRecord(slot.Runtime, &saved); err != nil {
				t.Fatal(err)
			}
			test.mutate(&saved.Platform)
			altered := slot
			altered.Runtime, err = backend.EncodeCheckpointRecord(saved)
			if err != nil {
				t.Fatal(err)
			}
			prepared, err := PrepareJavaCheckpoint(audioGainJAR, altered, Options{})
			if prepared != nil {
				prepared.Discard()
			}
			if err == nil {
				t.Fatal("typed restore accepted malformed display ownership")
			}
			if runtime.State() != StateActive || runtime.display != display || runtime.displayOwner != runtime.MIDlet {
				t.Fatal("refused preparation changed the live display or lifecycle")
			}
		})
	}
	if _, err := runtime.CaptureCheckpointWithSession(t.Context(), nil); err != nil {
		t.Fatalf("live source became unusable after refused preparations: %v", err)
	}
}
