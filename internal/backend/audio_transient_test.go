package backend

import (
	"reflect"
	"testing"
	"time"

	"github.com/movingwoo/wfeature/internal/audio/smaf"
)

func transientNote(length uint32) []smaf.Event {
	return []smaf.Event{
		{Type: smaf.EventNoteOn, Note: 60, Velocity: 100},
		{Time: length, Type: smaf.EventNoteOff, Note: 60},
		{Time: length, Type: smaf.EventEnd},
	}
}

func TestTransientAudioOverlapsAndRetainsLifetimeAfterCheckpoint(t *testing.T) {
	source := NewAudio(nil)
	reusable := loadOwnedAudio(t, source, transientNote(1000))
	for _, length := range []uint32{200, 400} {
		if err := source.PlayTransient(transientNote(length), 0); err != nil {
			t.Fatal(err)
		}
	}
	source.Advance(0)
	saved := captureOwnedAudio(t, source)
	if len(saved.Sounds) != 3 || len(saved.Output.Notes) != 3 || !saved.Sounds[1].Transient || !saved.Sounds[2].Transient || saved.Sounds[0].Transient {
		t.Fatal("overlapping tones lost ownership or changed a reusable clip")
	}
	encoded, err := EncodeCheckpointRecord(saved)
	if err != nil {
		t.Fatal(err)
	}
	var decoded AudioState
	if err := DecodeCheckpointRecord(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	sink := &gainAudioProbe{}
	fresh, err := NewAudioFromState(decoded, sink)
	if err != nil {
		t.Fatal(err)
	}
	fresh.ResumeOutput()
	if len(sink.ofType(smaf.EventNoteOn)) != 3 {
		t.Fatal("active transient tones were not reconstructed")
	}
	for i, at := range []time.Duration{200 * time.Millisecond, 400 * time.Millisecond, time.Second} {
		fresh.Advance(at)
		after := captureOwnedAudio(t, fresh)
		wantSounds := max(1, 2-i)
		wantNotes := 2 - i
		if len(after.Sounds) != wantSounds || len(after.Output.Notes) != wantNotes || after.Sounds[0].Handle != reusable {
			t.Fatalf("at %v: sounds=%+v notes=%+v", at, after.Sounds, after.Output.Notes)
		}
	}
	if _, ok := fresh.Length(reusable); !ok {
		t.Fatal("a completed reusable clip was reclaimed")
	}
}

func TestTransientAudioStopFreesOnlyTransientHandles(t *testing.T) {
	for _, all := range []bool{false, true} {
		audio := NewAudio(nil)
		reusable := loadOwnedAudio(t, audio, transientNote(1000))
		if err := audio.PlayTransient(transientNote(1000), 0); err != nil {
			t.Fatal(err)
		}
		audio.Advance(0)
		handle := captureOwnedAudio(t, audio).Sounds[1].Handle
		if err := audio.Play(handle, 0, true); err == nil {
			t.Fatal("a transient sequence became an infinite reusable clip")
		}
		if all {
			audio.StopAll()
		} else {
			audio.Stop(handle)
		}
		saved := captureOwnedAudio(t, audio)
		if len(saved.Sounds) != 1 || saved.Sounds[0].Handle != reusable || audio.Playing(reusable) == all {
			t.Fatal("stop kept a transient handle or changed the wrong reusable state")
		}
	}
}

func TestTransientAudioFailureDoesNotConsumeAHandleOrEvictAClip(t *testing.T) {
	audio := NewAudio(nil)
	audio.maxSounds = 1
	loadOwnedAudio(t, audio, transientNote(1000))
	before := captureOwnedAudio(t, audio)
	for _, events := range [][]smaf.Event{nil, transientNote(1)} {
		if err := audio.PlayTransient(events, 0); err == nil {
			t.Fatal("empty or over-limit transient request succeeded")
		}
		if after := captureOwnedAudio(t, audio); !reflect.DeepEqual(before, after) {
			t.Fatal("a failed transient call changed loaded playback")
		}
	}
	if err := (*Audio)(nil).PlayTransient(transientNote(1), 0); err == nil {
		t.Fatal("missing audio accepted a transient request")
	}
}

func TestTransientAudioStateRejectsStoppedOrRepeatingOwnership(t *testing.T) {
	audio := NewAudio(nil)
	if err := audio.PlayTransient(transientNote(1), 0); err != nil {
		t.Fatal(err)
	}
	for _, damage := range []func(*AudioState){
		func(saved *AudioState) { saved.Sounds[0].Repeat = true },
		func(saved *AudioState) { saved.Sounds[0].Playing = false },
	} {
		saved := captureOwnedAudio(t, audio)
		damage(&saved)
		if _, err := NewAudioFromState(saved, nil); err == nil {
			t.Fatal("invalid transient ownership was accepted")
		}
	}
}
