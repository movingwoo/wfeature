package skt

import (
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/movingwoo/wfeature/internal/api/midp"
	"github.com/movingwoo/wfeature/internal/audio/smaf"
	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/jvm"
)

func TestCompletedTonesDoNotConsumeReusableSoundSlots(t *testing.T) {
	now := time.Unix(1000, 0)
	runtime := &Runtime{pace: backend.NewSpeedClock(func() time.Time { return now }), paceStart: now}
	audio := runtime.audioTimeline()
	reusable, err := audio.LoadEvents([]smaf.Event{{Time: 10000, Type: smaf.EventEnd}})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 512; i++ {
		if _, err := runtime.playTone(nil, []jvm.Value{jvm.IntValue(69), jvm.IntValue(1), jvm.IntValue(100)}); err != nil {
			t.Fatalf("tone %d: %v", i+1, err)
		}
		runtime.AdvanceAudio()
		now = now.Add(2 * time.Millisecond)
		runtime.AdvanceAudio()
	}
	saved, err := audio.CaptureState()
	if err != nil || len(saved.Sounds) != 1 || saved.Sounds[0].Handle != reusable {
		t.Fatalf("completed tones retained handles or removed reusable sound: %+v, %v", saved.Sounds, err)
	}
}

func TestTransientToneSurvivesJavaCheckpointAndReclaimsItsSlot(t *testing.T) {
	archive, err := Open(audioGainJAR)
	if err != nil {
		t.Fatal(err)
	}
	store, _ := backend.NewMemorySaveStore(nil)
	runtime, err := Start(archive, Options{Framebuffer: newTestFramebuffer(t, 32, 24), SaveStore: store})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = runtime.Destroy(true) }()
	if _, err := runtime.VM.InvokeStatic("AudioGainMIDlet", "tone", "(III)V", jvm.IntValue(69), jvm.IntValue(60000), jvm.IntValue(100)); err != nil {
		t.Fatal(err)
	}
	runtime.AdvanceAudio()
	saved, err := runtime.CaptureCheckpointWithSession(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := PrepareJavaCheckpoint(audioGainJAR, saved, Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer prepared.Discard()
	sink := &sktGainSink{}
	restored, err := prepared.Commit(t.Context(), runtime, store, newTestFramebuffer(t, 32, 24), sink)
	if err != nil {
		t.Fatal(err)
	}
	runtime = restored
	runtime.ResumeCheckpointOutput()
	state, err := runtime.audioTimeline().CaptureState()
	if err != nil || len(state.Sounds) != 1 || !state.Sounds[0].Transient || sink.notes != 1 {
		t.Fatalf("restored tone lost its lifetime/output: %+v, notes=%d, err=%v", state.Sounds, sink.notes, err)
	}
	runtime.pace.Rebase(runtime.pace.Now().Add(61 * time.Second))
	runtime.AdvanceAudio()
	state, err = runtime.audioTimeline().CaptureState()
	if err != nil || len(state.Sounds) != 0 || len(state.Output.Sounds) != 0 || len(state.Output.Notes) != 0 {
		t.Fatalf("restored tone retained its slot after completion: %+v, %v", state.Sounds, err)
	}
}

func TestToneGuestArgumentsAndUnsupportedCapabilities(t *testing.T) {
	archive, err := Open(audioGainJAR)
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := Start(archive, Options{Framebuffer: newTestFramebuffer(t, 32, 24)})
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Destroy(true)
	guestError := func(err error, class string) {
		t.Helper()
		var guest *jvm.GuestException
		if !errors.As(err, &guest) || guest.Object.ClassName != class {
			t.Fatalf("error = %v, want %s", err, class)
		}
	}
	for _, arguments := range [][3]int32{{-1, 1, 100}, {128, 1, 100}, {69, 0, 100}, {69, -1, 100}} {
		_, err := runtime.VM.InvokeStatic("AudioGainMIDlet", "tone", "(III)V",
			jvm.IntValue(arguments[0]), jvm.IntValue(arguments[1]), jvm.IntValue(arguments[2]))
		guestError(err, "java/lang/IllegalArgumentException")
	}
	// Different pitches make the raw velocity clamp visible without timing.
	for _, level := range []int32{-1, 101} {
		if _, err := runtime.VM.InvokeStatic("AudioGainMIDlet", "tone", "(III)V", jvm.IntValue(69), jvm.IntValue(1000), jvm.IntValue(level)); err != nil {
			t.Fatal(err)
		}
	}
	saved, err := runtime.audioTimeline().CaptureState()
	if err != nil || len(saved.Sounds) != 2 || saved.Sounds[0].Events[0].Velocity != 0 || saved.Sounds[1].Events[0].Velocity != 127 {
		t.Fatalf("tone volume clamp = %+v, %v", saved.Sounds, err)
	}
	for _, locator := range []jvm.Value{jvm.ReferenceValue(nil), jvm.ReferenceValue(runtime.VM.NewString(midp.ToneDeviceLocator))} {
		_, err := runtime.VM.InvokeStatic(midp.ManagerClass, "createPlayer", "(Ljava/lang/String;)Ljavax/microedition/media/Player;", locator)
		if object, _ := locator.Reference(); object == nil {
			guestError(err, "java/lang/IllegalArgumentException")
		} else {
			guestError(err, midp.MediaExceptionClass)
		}
	}
	_, err = runtime.VM.InvokeStatic(midp.ManagerClass, "createPlayer", "(Ljava/io/InputStream;Ljava/lang/String;)Ljavax/microedition/media/Player;", jvm.ReferenceValue(nil), jvm.ReferenceValue(nil))
	guestError(err, "java/lang/IllegalArgumentException")
	for _, data := range [][]byte{nil, audioStopSound()} {
		_, err := runtime.newPlayer(runtime.VM, data, "audio/x-tone-seq")
		guestError(err, midp.MediaExceptionClass)
	}
	_, err = runtime.newPlayer(runtime.VM, nil, "application/vnd.smaf")
	guestError(err, midp.MediaExceptionClass)
	for _, query := range []struct {
		method, argument string
		null             bool
		want             []string
	}{
		{"getSupportedContentTypes", "", true, []string{"application/vnd.smaf"}},
		{"getSupportedContentTypes", "resource", false, []string{"application/vnd.smaf"}},
		{"getSupportedContentTypes", "device", false, nil},
		{"getSupportedContentTypes", "http", false, nil},
		{"getSupportedContentTypes", "", false, nil},
		{"getSupportedProtocols", "", true, []string{"resource"}},
		{"getSupportedProtocols", "application/vnd.smaf", false, []string{"resource"}},
		{"getSupportedProtocols", "audio/x-tone-seq", false, nil},
		{"getSupportedProtocols", "audio/mp3", false, nil},
		{"getSupportedProtocols", "", false, nil},
	} {
		argument := jvm.ReferenceValue(nil)
		if !query.null {
			argument = jvm.ReferenceValue(runtime.VM.NewString(query.argument))
		}
		value, err := runtime.VM.InvokeStatic(midp.ManagerClass, query.method, "(Ljava/lang/String;)[Ljava/lang/String;", argument)
		if err != nil {
			t.Fatal(err)
		}
		array, _ := value.Reference()
		_, items, err := jvm.ArraySnapshot(array)
		if err != nil {
			t.Fatal(err)
		}
		var got []string
		for _, item := range items {
			text, _ := item.Reference()
			name, _ := jvm.StringText(text)
			got = append(got, name)
		}
		if !slices.Equal(got, query.want) {
			t.Fatalf("%s(%q, null=%v) = %v, want %v", query.method, query.argument, query.null, got, query.want)
		}
	}
}
