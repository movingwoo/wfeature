package backend

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/movingwoo/wfeature/internal/audio/smaf"
)

func TestAudioStateContinuesEventsAndRepeatPosition(t *testing.T) {
	sink := &recordingSink{}
	source := NewAudio(sink)
	handle, err := source.LoadEvents([]smaf.Event{
		{Type: smaf.EventProgramChange, Channel: 2, Program: 27},
		{Time: 10, Type: smaf.EventNoteOn, Channel: 2, Note: 64, Velocity: 80},
		{Time: 30, Type: smaf.EventNoteOff, Channel: 2, Note: 64},
		{Time: 40, Type: smaf.EventWave, WaveChannels: 1, SamplingRate: 8000, Wave: []int16{-7, 9}},
		{Time: 50, Type: smaf.EventSysEx, SysEx: []byte{0xf0, 0x7e, 0xf7}},
		{Time: 60, Type: smaf.EventEnd},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := source.Play(handle, 0, true); err != nil {
		t.Fatal(err)
	}
	source.Advance(20 * time.Millisecond)
	source.SetVolume(25)
	saved, err := source.CaptureState()
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(saved)
	if err != nil {
		t.Fatal(err)
	}
	var decoded AudioState
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	freshSink := &recordingSink{}
	fresh, err := NewAudioFromState(decoded, freshSink)
	if err != nil {
		t.Fatal(err)
	}
	if len(freshSink.calls) != 0 || !fresh.Playing(handle) || fresh.Volume() != 25 {
		t.Fatal("constructing an audio timeline emitted output or changed playback")
	}
	sink.calls = nil
	for _, now := range []time.Duration{29, 30, 60, 81, 130} {
		source.Advance(now * time.Millisecond)
		fresh.Advance(now * time.Millisecond)
	}
	if !reflect.DeepEqual(sink.calls, freshSink.calls) {
		t.Fatalf("continued output differs: got %v, want %v", freshSink.calls, sink.calls)
	}
	// Stopping must release the saved active notes and used channels too.
	stopSink := &recordingSink{}
	stopped, err := NewAudioFromState(decoded, stopSink)
	if err != nil {
		t.Fatal(err)
	}
	stopped.Stop(handle)
	if stopSink.count("off 2 64") != 1 || stopSink.count("control 2") != 3 || stopped.Playing(handle) {
		t.Fatalf("stop after restore = %v", stopSink.calls)
	}
	decoded.Sounds[0].Events[3].Wave[0] = 123
	decoded.Sounds[0].Events[4].SysEx[0] = 0
	again, err := fresh.CaptureState()
	if err != nil || again.Sounds[0].Events[3].Wave[0] != -7 || again.Sounds[0].Events[4].SysEx[0] != 0xf0 {
		t.Fatalf("restored audio shares snapshot buffers: %v", err)
	}
	next, err := fresh.LoadEvents([]smaf.Event{{Time: 1, Type: smaf.EventEnd}})
	if err != nil || next != handle+1 {
		t.Fatalf("next restored sound handle = %d, %v", next, err)
	}
}

func TestAudioStateRejectsMalformedDataBeforeOutput(t *testing.T) {
	source := NewAudio(nil)
	handle, err := source.LoadEvents([]smaf.Event{{Time: 1, Type: smaf.EventNoteOn, Note: 60}, {Time: 10, Type: smaf.EventEnd}})
	if err != nil {
		t.Fatal(err)
	}
	if err := source.Play(handle, time.Second, false); err != nil {
		t.Fatal(err)
	}
	saved, err := source.CaptureState()
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(saved)
	if err != nil {
		t.Fatal(err)
	}
	for _, damage := range []func(*AudioState){
		func(s *AudioState) { s.Version++ },
		func(s *AudioState) { s.MaxSounds = defaultMaxSounds + 1 },
		func(s *AudioState) { s.Volume = 101 },
		func(s *AudioState) { s.Next = 0 },
		func(s *AudioState) { s.Sounds = append(s.Sounds, s.Sounds[0]) },
		func(s *AudioState) { s.Sounds[0].Cursor = len(s.Sounds[0].Events) },
		func(s *AudioState) { s.Sounds[0].Length++ },
		func(s *AudioState) { s.Sounds[0].StartedAt = time.Duration(1<<63 - 1) },
		func(s *AudioState) { s.Sounds[0].Events[1].Time = 0 },
		func(s *AudioState) { s.Sounds[0].Events[0].Type = 255 },
		func(s *AudioState) {
			s.Sounds[0].Events[0].Type = smaf.EventWave
			s.Sounds[0].Events[0].Wave = []int16{1, 2}
		},
	} {
		var bad AudioState
		if err := json.Unmarshal(data, &bad); err != nil {
			t.Fatal(err)
		}
		damage(&bad)
		sink := &recordingSink{}
		if fresh, err := NewAudioFromState(bad, sink); err == nil || fresh != nil || len(sink.calls) != 0 {
			t.Fatal("malformed audio state constructed a timeline or emitted output")
		}
	}
	if !source.Playing(handle) {
		t.Fatal("refused audio state changed the source")
	}
}
