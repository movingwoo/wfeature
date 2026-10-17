package backend

import (
	"testing"
	"time"

	"github.com/movingwoo/wfeature/internal/audio/smaf"
)

// The recorder cannot represent a channel message whose fields do not fit
// MIDI, and the first one it is handed makes it refuse every quick save from
// then on. Such a message is refused where a note with the same fault already
// is — when the sound is loaded — so it never reaches the recorder, and the
// sounds that are fine go on playing and being saved.
func TestAnOutOfRangeChannelMessageIsRefusedAtLoad(t *testing.T) {
	now := time.Unix(1700000000, 0)
	audio := NewAudioWithClock(nil, func() time.Time { return now })
	elapsed := time.Duration(0)
	for _, bad := range []smaf.Event{
		{Type: smaf.EventControlChange, Channel: 0, Control: 7, Value: 144},
		{Type: smaf.EventControlChange, Channel: 0, Control: 135, Value: 1},
		{Type: smaf.EventControlChange, Channel: 16, Control: 7, Value: 1},
		{Type: smaf.EventProgramChange, Channel: 0, Program: 200},
		{Type: smaf.EventProgramChange, Channel: 16, Program: 1},
		{Type: smaf.EventPitchBend, Channel: 0, Bend: 16384},
		{Type: smaf.EventPitchBend, Channel: 16, Bend: 8192},
	} {
		events := []smaf.Event{bad, {Time: 10, Type: smaf.EventEnd}}
		handle, err := audio.LoadEvents(events)
		if err == nil {
			t.Errorf("a sound carrying %+v was loaded", bad)
			// Played, which is what used to poison the recorder.
			if err := audio.Play(handle, elapsed, false); err != nil {
				t.Fatal(err)
			}
			elapsed += 20 * time.Millisecond
			audio.Advance(elapsed)
		}
		if err := audio.PlayTransient(events, elapsed); err == nil {
			t.Errorf("a transient sound carrying %+v was played", bad)
			elapsed += 20 * time.Millisecond
			audio.Advance(elapsed)
		}
	}

	handle, err := audio.LoadEvents([]smaf.Event{
		{Type: smaf.EventProgramChange, Channel: 0, Program: 127},
		{Type: smaf.EventControlChange, Channel: 0, Control: 7, Value: 127},
		{Type: smaf.EventPitchBend, Channel: 0, Bend: 16383},
		{Type: smaf.EventNoteOn, Channel: 0, Note: 60, Velocity: 90},
		{Time: 500, Type: smaf.EventEnd},
	})
	if err != nil {
		t.Fatalf("a valid sound was refused: %v", err)
	}
	if err := audio.Play(handle, elapsed, false); err != nil {
		t.Fatal(err)
	}
	audio.Advance(elapsed + time.Millisecond)
	if _, err := audio.CaptureState(); err != nil {
		t.Fatalf("the quick save of a valid sound was refused: %v", err)
	}
}

// A quick save is not held to the load-time check. One written before it may
// carry a loaded sound with such a message in it, and it still restores.
func TestAQuickSaveWithAnOutOfRangeChannelMessageStillRestores(t *testing.T) {
	now := time.Unix(1700000000, 0)
	audio := NewAudioWithClock(nil, func() time.Time { return now })
	if _, err := audio.LoadEvents([]smaf.Event{
		{Type: smaf.EventControlChange, Channel: 0, Control: 7, Value: 100},
		{Time: 10, Type: smaf.EventEnd},
	}); err != nil {
		t.Fatal(err)
	}
	saved, err := audio.CaptureState()
	if err != nil {
		t.Fatal(err)
	}
	if len(saved.Sounds) != 1 || len(saved.Sounds[0].Events) != 2 {
		t.Fatalf("captured sounds = %+v", saved.Sounds)
	}
	// What an earlier build loaded and saved without looking.
	saved.Sounds[0].Events[0].Value = 144
	if _, err := NewAudioFromState(saved, nil); err != nil {
		t.Fatalf("an earlier quick save no longer restores: %v", err)
	}
}
