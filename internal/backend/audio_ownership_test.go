package backend

import (
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/movingwoo/wfeature/internal/audio/smaf"
)

type ownedAudioEvent struct {
	sound AudioHandle
	event smaf.Event
}

// Record the public sink boundary, including any unintended legacy calls.
type ownedAudioProbe struct {
	recordingSink
	events []ownedAudioEvent
	stops  []AudioHandle
}

func (sink *ownedAudioProbe) AudioEvent(sound AudioHandle, event smaf.Event) {
	event.Wave, event.SysEx = slices.Clone(event.Wave), slices.Clone(event.SysEx)
	sink.events = append(sink.events, ownedAudioEvent{sound, event})
}

func (sink *ownedAudioProbe) StopSound(sound AudioHandle) {
	sink.stops = append(sink.stops, sound)
}

func (sink *ownedAudioProbe) ofType(kind smaf.EventType) []ownedAudioEvent {
	var found []ownedAudioEvent
	for _, event := range sink.events {
		if event.event.Type == kind {
			found = append(found, event)
		}
	}
	return found
}

func loadOwnedAudio(t *testing.T, audio *Audio, events []smaf.Event) AudioHandle {
	t.Helper()
	handle, err := audio.LoadEvents(events)
	if err != nil {
		t.Fatal(err)
	}
	if err := audio.Play(handle, 0, false); err != nil {
		t.Fatal(err)
	}
	return handle
}

func captureOwnedAudio(t *testing.T, audio *Audio) AudioState {
	t.Helper()
	saved, err := audio.CaptureState()
	if err != nil {
		t.Fatal(err)
	}
	return saved
}

func TestAudioStopKeepsAnotherSoundOnTheSameChannel(t *testing.T) {
	for _, sameNote := range []bool{false, true} {
		audio := NewAudio(nil)
		load := func(note uint8) AudioHandle {
			handle, err := audio.LoadEvents([]smaf.Event{
				{Type: smaf.EventNoteOn, Note: note, Velocity: 100},
				{Type: smaf.EventWave, WaveChannels: 1, SamplingRate: 8000, Wave: make([]int16, 8000)},
				{Time: 1000, Type: smaf.EventEnd},
			})
			if err != nil {
				t.Fatal(err)
			}
			if err := audio.Play(handle, 0, false); err != nil {
				t.Fatal(err)
			}
			return handle
		}
		first := load(60)
		note := uint8(67)
		if sameNote {
			note = 60
		}
		second := load(note)
		audio.Advance(0)
		saved, err := audio.CaptureState()
		if err != nil || len(saved.Output.Notes) != 2 {
			t.Fatalf("concurrent voices = %d, %v", len(saved.Output.Notes), err)
		}
		audio.Stop(first)
		saved, err = audio.CaptureState()
		if err != nil || len(saved.Output.Notes) != 1 || saved.Output.Notes[0].Note != note || len(saved.Output.Waves) != 1 || !audio.Playing(second) {
			t.Fatalf("stopping one sound changed another: notes=%+v waves=%d err=%v", saved.Output.Notes, len(saved.Output.Waves), err)
		}
	}
}

func TestAudioStopCancelsWaveAfterNaturalScoreEnd(t *testing.T) {
	now := time.Unix(1700000000, 0)
	audio := NewAudioWithClock(nil, func() time.Time { return now })
	handle, err := audio.LoadEvents([]smaf.Event{
		{Type: smaf.EventWave, WaveChannels: 1, SamplingRate: 8, Wave: make([]int16, 16)},
		{Time: 100, Type: smaf.EventEnd},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := audio.Play(handle, 0, false); err != nil {
		t.Fatal(err)
	}
	audio.Advance(100 * time.Millisecond)
	saved, err := audio.CaptureState()
	if err != nil || len(saved.Output.Waves) != 1 {
		t.Fatalf("natural end truncated PCM: %v", err)
	}
	audio.Stop(handle)
	saved, err = audio.CaptureState()
	if err != nil || len(saved.Output.Waves) != 0 {
		t.Fatalf("explicit stop retained PCM: %v", err)
	}
}

func TestOwnedAudioCheckpointPreservesIndependentChannelsAndPCM(t *testing.T) {
	now := time.Unix(1700000000, 0)
	clock := func() time.Time { return now }
	sink := &ownedAudioProbe{}
	audio := NewAudioWithClock(sink, clock)
	initial := []AudioChannelState{
		defaultPitchState(AudioChannelState{Program: 27, Volume: 90, Expression: 110, Pan: 20, Bend: 9000}),
		defaultPitchState(AudioChannelState{Program: 56, Volume: 40, Expression: 100, Pan: 100, Bend: 7000}),
	}
	var handles []AudioHandle
	for _, state := range initial {
		handles = append(handles, loadOwnedAudio(t, audio, []smaf.Event{
			{Type: smaf.EventProgramChange, Channel: 2, Program: state.Program},
			{Type: smaf.EventControlChange, Channel: 2, Control: 7, Value: state.Volume},
			{Type: smaf.EventControlChange, Channel: 2, Control: 11, Value: state.Expression},
			{Type: smaf.EventControlChange, Channel: 2, Control: 10, Value: state.Pan},
			{Type: smaf.EventPitchBend, Channel: 2, Bend: state.Bend},
			{Type: smaf.EventNoteOn, Channel: 2, Note: 64, Velocity: 80},
			{Type: smaf.EventWave, WaveChannels: 2, SamplingRate: 4, Wave: []int16{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15}},
			{Time: 20, Type: smaf.EventProgramChange, Channel: 2, Program: state.Program + 1},
			{Time: 20, Type: smaf.EventControlChange, Channel: 2, Control: 7, Value: state.Volume - 10},
			{Time: 20, Type: smaf.EventPitchBend, Channel: 2, Bend: state.Bend + 100},
			{Time: 1000, Type: smaf.EventNoteOff, Channel: 2, Note: 64},
			{Time: 2000, Type: smaf.EventEnd},
		}))
	}
	audio.Advance(20 * time.Millisecond)
	if len(sink.calls) != 0 || len(sink.ofType(smaf.EventNoteOn)) != 2 || len(sink.ofType(smaf.EventWave)) != 2 {
		t.Fatalf("owned sink lost events or used legacy delivery: %+v", sink)
	}
	for _, event := range sink.events {
		if !slices.Contains(handles, event.sound) {
			t.Fatalf("event has no loaded owner: %+v", event)
		}
	}
	for i, handle := range handles {
		var programs, volumes []uint8
		var bends []uint16
		for _, output := range sink.events {
			if output.sound != handle {
				continue
			}
			switch event := output.event; event.Type {
			case smaf.EventProgramChange:
				programs = append(programs, event.Program)
			case smaf.EventControlChange:
				if event.Control == 7 {
					volumes = append(volumes, event.Value)
				}
			case smaf.EventPitchBend:
				bends = append(bends, event.Bend)
			}
		}
		if !slices.Equal(programs, []uint8{initial[i].Program, initial[i].Program + 1}) ||
			!slices.Equal(volumes, []uint8{initial[i].Volume, initial[i].Volume - 10}) ||
			!slices.Equal(bends, []uint16{initial[i].Bend, initial[i].Bend + 100}) {
			t.Fatalf("owner %d controls crossed at the sink: programs=%v volumes=%v bends=%v", handle, programs, volumes, bends)
		}
	}
	now = now.Add(500 * time.Millisecond)
	saved := captureOwnedAudio(t, audio)
	if len(saved.Output.Notes) != 2 || len(saved.Output.Sounds) != 2 || len(saved.Output.Waves) != 2 {
		t.Fatalf("snapshot lost an owner: %+v", saved.Output)
	}
	for i, handle := range handles {
		found := false
		for _, note := range saved.Output.Notes {
			if note.Sound == handle {
				found = note.Channel == 2 && note.Note == 64 && note.StartedWith == initial[i]
			}
		}
		if !found {
			t.Fatalf("owner %d lost its initial channel state", handle)
		}
	}
	encoded, err := EncodeCheckpointRecord(saved)
	if err != nil {
		t.Fatal(err)
	}
	var decoded AudioState
	if err := DecodeCheckpointRecord(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	now = time.Unix(1900000000, 0)
	restoredSink := &ownedAudioProbe{}
	fresh, err := NewAudioFromStateWithClock(decoded, restoredSink, clock)
	if err != nil {
		t.Fatal(err)
	}
	if len(restoredSink.events)+len(restoredSink.stops)+len(restoredSink.calls) != 0 {
		t.Fatal("detached restore emitted sound")
	}
	decoded.Output.Sounds[0].Channels[2].Program = 120
	decoded.Output.Waves[0].Samples[0] = -3000
	now = now.Add(15 * time.Second)
	fresh.ActivateOutputClock()
	fresh.ActivateOutputClock()
	fresh.ResumeOutput()
	if again := captureOwnedAudio(t, fresh); !reflect.DeepEqual(again, saved) {
		t.Fatal("restoration changed owned playback state or retained caller storage")
	}
	notes, waves := restoredSink.ofType(smaf.EventNoteOn), restoredSink.ofType(smaf.EventWave)
	if len(notes) != 2 || len(waves) != 2 || len(restoredSink.calls) != 0 {
		t.Fatalf("restored owned notes/waves=%d/%d, legacy=%v", len(notes), len(waves), restoredSink.calls)
	}
	for i, handle := range handles {
		var program, volume uint8
		var bend uint16
		var noteSeen bool
		for _, output := range restoredSink.events {
			if output.sound != handle || output.event.Channel != 2 {
				continue
			}
			event := output.event
			switch event.Type {
			case smaf.EventProgramChange:
				program = event.Program
			case smaf.EventControlChange:
				if event.Control == 7 {
					volume = event.Value
				}
			case smaf.EventPitchBend:
				bend = event.Bend
			case smaf.EventNoteOn:
				noteSeen = true
				if event.Note != 64 || program != initial[i].Program || volume != initial[i].Volume-10 || bend != initial[i].Bend+100 {
					t.Fatalf("owner %d restarted under another voice's settings", handle)
				}
			}
		}
		if !noteSeen || program != initial[i].Program+1 || volume != initial[i].Volume-10 || bend != initial[i].Bend+100 {
			t.Fatalf("owner %d current channel state was not restored", handle)
		}
	}
	for _, wave := range waves {
		if !slices.Contains(handles, wave.sound) || wave.event.WaveChannels != 2 || wave.event.SamplingRate != 4 ||
			!slices.Equal(wave.event.Wave, []int16{4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15}) {
			t.Fatalf("restored PCM tail lost position or ownership: %+v", wave)
		}
	}
	restoredSink.events = nil
	fresh.Advance(time.Second)
	if offs := restoredSink.ofType(smaf.EventNoteOff); len(offs) != 2 || offs[0].sound == offs[1].sound {
		t.Fatalf("restored notes did not receive independent future note-offs: %+v", offs)
	}
}

func TestOwnedAudioNaturalCompletionKeepsOtherSound(t *testing.T) {
	sink := &ownedAudioProbe{}
	audio := NewAudio(sink)
	first := loadOwnedAudio(t, audio, []smaf.Event{{Type: smaf.EventNoteOn, Note: 60, Velocity: 100}, {Time: 100, Type: smaf.EventEnd}})
	second := loadOwnedAudio(t, audio, []smaf.Event{{Type: smaf.EventNoteOn, Note: 60, Velocity: 100}, {Time: 1000, Type: smaf.EventEnd}})
	audio.Advance(0)
	sink.events, sink.stops = nil, nil
	audio.Advance(100 * time.Millisecond)
	for _, event := range sink.events {
		if event.sound != first {
			t.Fatalf("completion touched the other sound: %+v", event)
		}
	}
	saved := captureOwnedAudio(t, audio)
	if audio.Playing(first) || !audio.Playing(second) || len(saved.Output.Notes) != 1 || saved.Output.Notes[0].Sound != second || slices.Contains(sink.stops, second) {
		t.Fatal("natural completion stopped an equal-pitch note owned by another sound")
	}
}

func TestOwnedAudioCancellationAndRestartIsolatePCM(t *testing.T) {
	for _, action := range []string{"stop", "close", "restart"} {
		for _, completed := range []bool{false, true} {
			name := action + "/active"
			if completed {
				name = action + "/completed"
			}
			t.Run(name, func(t *testing.T) {
				now := time.Unix(1700000000, 0)
				sink := &ownedAudioProbe{}
				audio := NewAudioWithClock(sink, func() time.Time { return now })
				load := func() AudioHandle {
					return loadOwnedAudio(t, audio, []smaf.Event{
						{Type: smaf.EventWave, WaveChannels: 1, SamplingRate: 4, Wave: []int16{1, 2, 3, 4, 5, 6, 7, 8}},
						{Time: 100, Type: smaf.EventEnd},
					})
				}
				first, second := load(), load()
				at := time.Duration(0)
				if completed {
					at = 100 * time.Millisecond
				}
				audio.Advance(at)
				sink.events, sink.stops = nil, nil
				switch action {
				case "stop":
					audio.Stop(first)
				case "close":
					if err := audio.Close(first); err != nil {
						t.Fatal(err)
					}
				case "restart":
					if err := audio.Play(first, 200*time.Millisecond, false); err != nil {
						t.Fatal(err)
					}
				}
				saved := captureOwnedAudio(t, audio)
				if !slices.Equal(sink.stops, []AudioHandle{first}) || len(saved.Output.Waves) != 1 || saved.Output.Waves[0].Sound != second {
					t.Fatalf("%s cancelled the wrong PCM: stops=%v waves=%+v", action, sink.stops, saved.Output.Waves)
				}
				if action == "restart" {
					audio.Advance(200 * time.Millisecond)
					waves := sink.ofType(smaf.EventWave)
					if len(waves) != 1 || waves[0].sound != first || len(captureOwnedAudio(t, audio).Output.Waves) != 2 {
						t.Fatal("restart replayed another owner or retained its old PCM")
					}
				}
			})
		}
	}
}

func TestOwnedAudioVoiceBudgetIsSharedAcrossOwners(t *testing.T) {
	audio := NewAudio(nil)
	var handles []AudioHandle
	for i := 0; i < 25; i++ {
		handles = append(handles, loadOwnedAudio(t, audio, []smaf.Event{
			{Type: smaf.EventNoteOn, Note: 60, Velocity: 100}, {Time: 1000, Type: smaf.EventEnd},
		}))
		audio.Advance(0)
	}
	saved := captureOwnedAudio(t, audio)
	if len(saved.Output.Notes) != 24 {
		t.Fatalf("voice budget grew per owner: %d voices", len(saved.Output.Notes))
	}
	for i, note := range saved.Output.Notes {
		if note.Sound != handles[i+1] {
			t.Fatalf("voice stealing lost owner order at %d: %+v", i, note)
		}
	}
	sink := &ownedAudioProbe{}
	audio.SetSink(sink)
	audio.ResumeOutput()
	notes := sink.ofType(smaf.EventNoteOn)
	if len(notes) != 24 {
		t.Fatalf("reconstruction exceeded the shared budget: %d", len(notes))
	}
	for _, note := range notes {
		if note.sound == handles[0] {
			t.Fatal("reconstruction resurrected the stolen voice")
		}
	}
}

func TestOwnedAudioRejectsMalformedOwnershipBeforeEmission(t *testing.T) {
	now := time.Unix(1700000000, 0)
	audio := NewAudioWithClock(nil, func() time.Time { return now })
	for i := 0; i < 2; i++ {
		loadOwnedAudio(t, audio, []smaf.Event{
			{Type: smaf.EventNoteOn, Note: 60, Velocity: 100},
			{Type: smaf.EventWave, WaveChannels: 1, SamplingRate: 4, Wave: []int16{1, 2, 3, 4}},
			{Time: 1000, Type: smaf.EventEnd},
		})
	}
	audio.Advance(0)
	encoded, err := EncodeCheckpointRecord(captureOwnedAudio(t, audio))
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name   string
		damage func(*AudioState)
	}{
		{"old-version", func(s *AudioState) { s.Version = 1 }},
		{"zero-owner", func(s *AudioState) { s.Output.Sounds[0].Sound = 0 }},
		{"duplicate-owner", func(s *AudioState) { s.Output.Sounds[1].Sound = s.Output.Sounds[0].Sound }},
		{"unsorted-owners", func(s *AudioState) { s.Output.Sounds[0], s.Output.Sounds[1] = s.Output.Sounds[1], s.Output.Sounds[0] }},
		{"unloaded-owner", func(s *AudioState) {
			extra := s.Output.Sounds[1]
			extra.Sound = 99
			s.Output.Sounds = append(s.Output.Sounds, extra)
		}},
		{"missing-owner", func(s *AudioState) { s.Output.Sounds = s.Output.Sounds[1:] }},
		{"invalid-owned-channel", func(s *AudioState) { s.Output.Sounds[0].Channels[0].Bend = 16384 }},
		{"unowned-note", func(s *AudioState) { s.Output.Notes[0].Sound = 99 }},
		{"duplicate-owned-note", func(s *AudioState) { s.Output.Notes = append(s.Output.Notes, s.Output.Notes[0]) }},
		{"unowned-wave", func(s *AudioState) { s.Output.Waves[0].Sound = 99 }},
	} {
		t.Run(test.name, func(t *testing.T) {
			var bad AudioState
			if err := DecodeCheckpointRecord(encoded, &bad); err != nil {
				t.Fatal(err)
			}
			test.damage(&bad)
			sink := &ownedAudioProbe{}
			if fresh, err := NewAudioFromState(bad, sink); fresh != nil || err == nil || len(sink.events)+len(sink.stops)+len(sink.calls) != 0 {
				t.Fatalf("malformed ownership reached construction/output: %v", err)
			}
		})
	}
}

func TestOwnedAudioHandleExhaustionNeverReusesAnOwner(t *testing.T) {
	for _, encoded := range []bool{false, true} {
		audio := NewAudio(nil)
		saved := captureOwnedAudio(t, audio)
		saved.Next = ^AudioHandle(0) - 1
		audio, err := NewAudioFromState(saved, nil)
		if err != nil {
			t.Fatal(err)
		}
		load := func() (AudioHandle, error) {
			if encoded {
				return audio.Load(oneNoteSMAF())
			}
			return audio.LoadEvents([]smaf.Event{{Type: smaf.EventNoteOn, Note: 60, Velocity: 100}, {Time: 10, Type: smaf.EventEnd}})
		}
		handle, err := load()
		if err != nil || handle != ^AudioHandle(0) {
			t.Fatalf("last unsigned handle=%d, %v", handle, err)
		}
		if err := audio.Close(handle); err != nil {
			t.Fatal(err)
		}
		for i := 0; i < 2; i++ {
			if handle, err := load(); handle != 0 || err == nil {
				t.Fatalf("exhausted owner space wrapped: handle=%d err=%v", handle, err)
			}
		}
		if after := captureOwnedAudio(t, audio); after.Next != ^AudioHandle(0) || len(after.Sounds) != 0 {
			t.Fatal("refused allocation changed the owner space")
		}
	}
}

func TestOwnedAudioCancellationReleasesOnlyItsAdmittedPCM(t *testing.T) {
	for _, action := range []string{"stop", "close", "stop-all"} {
		t.Run(action, func(t *testing.T) {
			now := time.Unix(1700000000, 0)
			sink := &ownedAudioProbe{}
			audio := NewAudioWithClock(sink, func() time.Time { return now })
			load := func() AudioHandle {
				var events []smaf.Event
				for i := 0; i < 130; i++ {
					events = append(events, smaf.Event{Type: smaf.EventWave, WaveChannels: 1, SamplingRate: 1, Wave: []int16{1, 2, 3, 4}})
				}
				events = append(events, smaf.Event{Time: 100, Type: smaf.EventEnd})
				handle := loadOwnedAudio(t, audio, events)
				audio.Advance(0)
				return handle
			}
			first, second := load(), load()
			if saved := captureOwnedAudio(t, audio); len(saved.Output.Waves) != maxOutputWaves || len(sink.ofType(smaf.EventWave)) != maxOutputWaves {
				t.Fatal("admitted PCM differs from the reconstructible output")
			}
			cancel := func(handle AudioHandle) {
				switch action {
				case "stop":
					audio.Stop(handle)
				case "close":
					if err := audio.Close(handle); err != nil {
						t.Fatal(err)
					}
				case "stop-all":
					audio.StopAll()
				}
			}
			sink.stops = nil
			cancel(first)
			if action != "stop-all" {
				remaining := captureOwnedAudio(t, audio)
				if len(remaining.Output.Waves) != maxOutputWaves-130 || !slices.Equal(sink.stops, []AudioHandle{first}) {
					t.Fatal("cancelling one owner changed its peer's admitted PCM")
				}
				for _, wave := range remaining.Output.Waves {
					if wave.Sound != second {
						t.Fatal("a stopped owner's PCM survived cancellation")
					}
				}
				cancel(second)
			}
			if saved := captureOwnedAudio(t, audio); len(saved.Output.Waves) != 0 {
				t.Fatal("cancelled PCM remained in the checkpoint")
			}
			if !slices.Contains(sink.stops, first) || !slices.Contains(sink.stops, second) {
				t.Fatal("cancelled PCM was not stopped at the sink")
			}
		})
	}
}

func TestOwnedAudioReconnectResumesOnlyUnheardPCMFrames(t *testing.T) {
	now := time.Unix(1700000000, 0)
	audio := NewAudioWithClock(nil, func() time.Time { return now })
	loadOwnedAudio(t, audio, []smaf.Event{
		{Type: smaf.EventWave, WaveChannels: 1, SamplingRate: 4, Wave: []int16{10, 11, 12, 13}},
		{Time: 5000, Type: smaf.EventEnd},
	})
	long := loadOwnedAudio(t, audio, []smaf.Event{
		{Type: smaf.EventWave, WaveChannels: 2, SamplingRate: 4, Wave: []int16{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15}},
		{Time: 5000, Type: smaf.EventEnd},
	})
	audio.Advance(0)
	now = now.Add(1250 * time.Millisecond)
	before := captureOwnedAudio(t, audio)
	sink := &ownedAudioProbe{}
	audio.SetSink(sink)
	audio.ResumeOutput()
	waves := sink.ofType(smaf.EventWave)
	if len(waves) != 1 || waves[0].sound != long || waves[0].event.WaveChannels != 2 || waves[0].event.SamplingRate != 4 ||
		!slices.Equal(waves[0].event.Wave, []int16{10, 11, 12, 13, 14, 15}) {
		t.Fatalf("reconnect replayed expired/heard PCM or lost ownership: %+v", waves)
	}
	if after := captureOwnedAudio(t, audio); !reflect.DeepEqual(before, after) {
		t.Fatal("reconnection changed playback state")
	}
	now = now.Add(250 * time.Millisecond)
	sink.events = nil
	audio.ResumeOutput()
	if waves = sink.ofType(smaf.EventWave); len(waves) != 1 || !slices.Equal(waves[0].event.Wave, []int16{12, 13, 14, 15}) {
		t.Fatalf("repeated reconstruction replayed the prior tail: %+v", waves)
	}
	now = now.Add(500 * time.Millisecond)
	sink.events = nil
	audio.ResumeOutput()
	if len(sink.ofType(smaf.EventWave)) != 0 {
		t.Fatal("fully heard PCM restarted")
	}
}
