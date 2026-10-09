package backend

import (
	"math"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/movingwoo/wfeature/internal/audio/smaf"
)

type resumeAudioProbe struct {
	gainAudioProbe
	resumed []AudioVoiceState
}

func (sink *resumeAudioProbe) ResumeNote(sound AudioHandle, channel, note, velocity uint8, age time.Duration) {
	sink.resumed = append(sink.resumed, AudioVoiceState{Sound: sound, Channel: channel, Note: note, Velocity: velocity, Age: age})
}

func pausedOutputNotes(saved AudioState, sound AudioHandle) []AudioVoiceState {
	return slices.DeleteFunc(slices.Clone(saved.Output.Notes), func(note AudioVoiceState) bool { return note.Sound != sound })
}

func TestAudioPauseRetainsCursorRepeatAndPCMPosition(t *testing.T) {
	now := time.Unix(1000, 0)
	sink := &ownedAudioProbe{}
	audio := NewAudioWithClock(sink, func() time.Time { return now })
	paused, ok := any(audio).(interface {
		Pause(AudioHandle, time.Duration) error
		Resume(AudioHandle, time.Duration) error
		Paused(AudioHandle) bool
	})
	if !ok {
		t.Fatal("audio cannot preserve a paused cursor")
	}
	handle := loadOwnedAudio(t, audio, []smaf.Event{
		{Type: smaf.EventNoteOn, Note: 60, Velocity: 100},
		{Type: smaf.EventWave, WaveChannels: 1, SamplingRate: 4, Wave: []int16{10, 11, 12, 13, 14, 15, 16, 17}},
		{Time: 500, Type: smaf.EventNoteOff, Note: 60},
		{Time: 600, Type: smaf.EventNoteOn, Note: 62, Velocity: 90},
		{Time: 800, Type: smaf.EventNoteOff, Note: 62},
		{Time: 1000, Type: smaf.EventEnd},
	})
	if err := audio.Play(handle, 0, true); err != nil {
		t.Fatal(err)
	}
	audio.Advance(0)
	now = now.Add(250 * time.Millisecond)
	if err := paused.Pause(handle, 250*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if audio.Playing(handle) || !paused.Paused(handle) {
		t.Fatal("pause still reports active playback")
	}
	sink.events = nil
	now = now.Add(10 * time.Second)
	audio.Advance(10250 * time.Millisecond)
	if len(sink.events) != 0 {
		t.Fatal("paused timeline emitted events")
	}
	if err := paused.Resume(handle, 10250*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	waves := sink.ofType(smaf.EventWave)
	if len(waves) != 1 || !slices.Equal(waves[0].event.Wave, []int16{11, 12, 13, 14, 15, 16, 17}) {
		t.Fatalf("resumed PCM = %+v, want only its unheard suffix", waves)
	}
	sink.events = nil
	audio.Advance(10499 * time.Millisecond)
	if len(sink.ofType(smaf.EventNoteOff)) != 0 {
		t.Fatal("pause shortened the remaining note gate")
	}
	audio.Advance(10500 * time.Millisecond)
	if len(sink.ofType(smaf.EventNoteOff)) != 1 {
		t.Fatal("resume restarted the note gate")
	}
	audio.Advance(11000 * time.Millisecond)
	if !audio.Playing(handle) || len(sink.ofType(smaf.EventNoteOn)) != 2 {
		t.Fatal("resume lost the original repeat boundary")
	}
}

func TestAudioPauseCheckpointKeepsFrozenOutputAndSkipsReconnect(t *testing.T) {
	now := time.Unix(1000, 0)
	sink := &resumeAudioProbe{}
	audio := NewAudioWithClock(sink, func() time.Time { return now })
	first := loadOwnedAudio(t, audio, []smaf.Event{
		{Type: smaf.EventNoteOn, Note: 60, Velocity: 100},
		{Type: smaf.EventNoteOn, Channel: 9, Note: 38, Velocity: 80},
		{Type: smaf.EventWave, WaveChannels: 2, SamplingRate: 20, Wave: []int16{0, 1, 2, 3, 4, 5, 6, 7, 8, 9}},
		{Time: 500, Type: smaf.EventNoteOff, Note: 60},
		{Time: 1000, Type: smaf.EventEnd},
	})
	second := loadOwnedAudio(t, audio, []smaf.Event{
		{Type: smaf.EventNoteOn, Note: 67, Velocity: 90},
		{Time: 60000, Type: smaf.EventEnd},
	})
	audio.SetVolume(50)
	if err := audio.SetSoundVolume(first, 40); err != nil {
		t.Fatal(err)
	}
	audio.Advance(0)
	now = now.Add(50 * time.Millisecond)
	sink.stops = nil
	if err := audio.Pause(first, 50*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	frozen := captureOwnedAudio(t, audio)
	if !slices.Equal(sink.stops, []AudioHandle{first}) {
		t.Fatalf("pause stopped owners %v, want only %d", sink.stops, first)
	}
	for _, note := range pausedOutputNotes(frozen, first) {
		if note.Age != 50*time.Millisecond || note.Channel == 9 && note.DrumLeft != 150*time.Millisecond {
			t.Fatalf("pause changed envelope position: %+v", note)
		}
	}
	now = now.Add(30 * time.Second)
	if err := audio.Pause(first, 30050*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	audio.Advance(30050 * time.Millisecond)
	saved := captureOwnedAudio(t, audio)
	if !reflect.DeepEqual(pausedOutputNotes(saved, first), pausedOutputNotes(frozen, first)) ||
		!reflect.DeepEqual(saved.Output.Waves, frozen.Output.Waves) || !reflect.DeepEqual(saved.Sounds[0], frozen.Sounds[0]) || len(sink.stops) != 1 {
		t.Fatal("a repeated pause or elapsed host time moved the frozen cursor")
	}
	sink.events, sink.resumed = nil, nil
	audio.ResumeOutput()
	if len(sink.resumed) != 1 || sink.resumed[0].Sound != second || len(sink.ofType(smaf.EventWave)) != 0 {
		t.Fatal("reconnect replayed paused notes or PCM")
	}
	for _, event := range sink.events {
		if event.sound == first {
			t.Fatal("reconnect reconstructed paused channel state")
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
	now = time.Unix(2000, 0)
	restoredSink := &resumeAudioProbe{}
	fresh, err := NewAudioFromStateWithClock(decoded, restoredSink, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(time.Minute)
	fresh.ActivateOutputClock()
	fresh.ResumeOutput()
	if len(restoredSink.resumed) != 1 || restoredSink.resumed[0].Sound != second || len(restoredSink.ofType(smaf.EventWave)) != 0 {
		t.Fatal("checkpoint adoption made a paused owner audible")
	}
	now = now.Add(7 * time.Second)
	fresh.Advance(37050 * time.Millisecond)
	restoredSink.events, restoredSink.resumed = nil, nil
	if err := fresh.Resume(first, 37050*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if len(restoredSink.resumed) != 2 || restoredSink.gains[first] != 2000 {
		t.Fatalf("resume lost held notes or clip gain: %+v, %v", restoredSink.resumed, restoredSink.gains)
	}
	for _, note := range restoredSink.resumed {
		if note.Sound != first || note.Age != 50*time.Millisecond {
			t.Fatalf("resume changed owner or envelope age: %+v", note)
		}
	}
	waves := restoredSink.ofType(smaf.EventWave)
	if len(waves) != 1 || waves[0].sound != first || !slices.Equal(waves[0].event.Wave, []int16{2, 3, 4, 5, 6, 7, 8, 9}) {
		t.Fatalf("resume did not preserve the frozen stereo frame: %+v", waves)
	}
	for _, event := range restoredSink.events {
		if event.sound == second {
			t.Fatal("resuming one owner reconstructed its peer")
		}
	}
	emitted := len(restoredSink.events)
	if err := fresh.Resume(first, 37050*time.Millisecond); err != nil || len(restoredSink.events) != emitted || len(restoredSink.resumed) != 2 {
		t.Fatal("repeated resume emitted another reconstruction")
	}
	restoredSink.events = nil
	fresh.Advance(37499 * time.Millisecond)
	if len(restoredSink.ofType(smaf.EventNoteOff)) != 0 {
		t.Fatal("checkpoint pause shortened the remaining guest gate")
	}
	fresh.Advance(37500 * time.Millisecond)
	if notes := restoredSink.ofType(smaf.EventNoteOff); len(notes) != 1 || notes[0].sound != first || notes[0].event.Note != 60 {
		t.Fatalf("checkpoint resume lost the remaining note-off: %+v", notes)
	}
}

func TestAudioPauseSeparatesRetainedAndActiveVoiceBudgets(t *testing.T) {
	now := time.Unix(1000, 0)
	sink := &resumeAudioProbe{}
	audio := NewAudioWithClock(sink, func() time.Time { return now })
	var events []smaf.Event
	for note := uint8(40); note <= 64; note++ {
		events = append(events, smaf.Event{Type: smaf.EventNoteOn, Note: note, Velocity: 100})
	}
	events = append(events, smaf.Event{Time: 1000, Type: smaf.EventEnd})
	first := loadOwnedAudio(t, audio, events)
	audio.Advance(0)
	if err := audio.Pause(first, 0); err != nil {
		t.Fatal(err)
	}
	second := loadOwnedAudio(t, audio, events)
	audio.Advance(0)
	saved := captureOwnedAudio(t, audio)
	for _, owner := range []AudioHandle{first, second} {
		notes := pausedOutputNotes(saved, owner)
		if len(notes) != 24 || notes[0].Note != 41 || notes[23].Note != 64 {
			t.Fatalf("owner %d lost its independent voice budget: %+v", owner, notes)
		}
	}
	for _, owner := range []AudioHandle{first, second} {
		bad := saved
		bad.Output.Notes = append(slices.Clone(saved.Output.Notes), AudioVoiceState{
			Sound: owner, Channel: 1, Note: 60, Velocity: 100, StartedWith: defaultAudioChannels()[1],
		})
		if _, err := NewAudioFromState(bad, nil); err == nil {
			t.Fatalf("checkpoint accepted a twenty-fifth voice for owner %d", owner)
		}
	}
	if err := audio.Resume(first, 0); err != nil {
		t.Fatal(err)
	}
	after := captureOwnedAudio(t, audio)
	if len(after.Output.Notes) != 24 || len(pausedOutputNotes(after, first)) != 24 || len(sink.resumed) != 24 {
		t.Fatal("resumed notes did not take the active voice budget in delivery order")
	}
	for i, note := range sink.resumed {
		if note.Sound != first || note.Note != uint8(41+i) {
			t.Fatalf("resume delivered a stolen or reordered note: %+v", sink.resumed)
		}
	}
}

func TestAudioResumeVoiceStealingFollowsDeliveryOrder(t *testing.T) {
	for _, expiredDrum := range []bool{false, true} {
		name := "new note after resume"
		if expiredDrum {
			name = "expired drum frees a slot"
		}
		t.Run(name, func(t *testing.T) {
			now := time.Unix(1000, 0)
			audio := NewAudioWithClock(nil, func() time.Time { return now })
			first := loadOwnedAudio(t, audio, transientNote(1000))
			audio.Advance(0)
			if err := audio.Pause(first, 0); err != nil {
				t.Fatal(err)
			}
			var events []smaf.Event
			for note := uint8(40); note <= 62; note++ {
				events = append(events, smaf.Event{Type: smaf.EventNoteOn, Note: note, Velocity: 100})
			}
			if expiredDrum {
				events = append(events, smaf.Event{Type: smaf.EventNoteOn, Channel: 9, Note: 38, Velocity: 100})
			}
			events = append(events, smaf.Event{Time: 1000, Type: smaf.EventEnd})
			second := loadOwnedAudio(t, audio, events)
			audio.Advance(0)
			now = now.Add(300 * time.Millisecond)
			if err := audio.Resume(first, 300*time.Millisecond); err != nil {
				t.Fatal(err)
			}
			saved := captureOwnedAudio(t, audio)
			if len(saved.Output.Notes) != 24 || saved.Output.Notes[0].Sound != second || saved.Output.Notes[0].Note != 40 || saved.Output.Notes[23].Sound != first {
				t.Fatalf("resume changed the audible voice order: %+v", saved.Output.Notes)
			}
			third := loadOwnedAudio(t, audio, []smaf.Event{
				{Type: smaf.EventNoteOn, Note: 70, Velocity: 100},
				{Time: 1000, Type: smaf.EventEnd},
			})
			audio.Advance(300 * time.Millisecond)
			saved = captureOwnedAudio(t, audio)
			if len(saved.Output.Notes) != 24 || saved.Output.Notes[0].Sound != second || saved.Output.Notes[0].Note != 41 || saved.Output.Notes[22].Sound != first || saved.Output.Notes[23].Sound != third {
				t.Fatalf("the next note stole a different voice from the page: %+v", saved.Output.Notes)
			}
		})
	}
}

func TestAudioPauseAtPCMCapacityRetainsOnlyAdmittedWaves(t *testing.T) {
	for _, emitted := range []bool{false, true} {
		name := "newly due PCM"
		if emitted {
			name = "already emitted PCM"
		}
		t.Run(name, func(t *testing.T) {
			now := time.Unix(1000, 0)
			sink := &resumeAudioProbe{}
			audio := NewAudioWithClock(sink, func() time.Time { return now })
			var events []smaf.Event
			for i := 0; i < 257; i++ {
				events = append(events, smaf.Event{Type: smaf.EventWave, WaveChannels: 1, SamplingRate: 4, Wave: []int16{1, 2, 3, 4}})
			}
			events = append(events, smaf.Event{Time: 5000, Type: smaf.EventEnd})
			first := loadOwnedAudio(t, audio, events)
			second := loadOwnedAudio(t, audio, transientNote(5000))
			if emitted {
				audio.Advance(0)
			}
			sink.stops = nil
			if err := audio.Pause(first, 0); err != nil || !audio.Paused(first) || audio.Playing(first) || !slices.Equal(sink.stops, []AudioHandle{first}) {
				t.Fatalf("admitted output could not be paused: %v", err)
			}
			if len(sink.ofType(smaf.EventWave)) != maxOutputWaves {
				t.Fatal("PCM beyond the retained budget reached the sink")
			}
			if err := audio.Pause(second, 0); err != nil || !slices.Equal(sink.stops, []AudioHandle{first, second}) {
				t.Fatalf("PCM capacity prevented a peer pause: %v", err)
			}
			now = now.Add(time.Second)
			if err := audio.Pause(first, time.Second); err != nil {
				t.Fatalf("repeated pause failed: %v", err)
			}
			if saved := captureOwnedAudio(t, audio); len(saved.Output.Waves) != maxOutputWaves || !audio.Paused(first) || !audio.Paused(second) {
				t.Fatal("paused PCM aged or exceeded the frozen snapshot budget")
			}
			sink.events = nil
			if err := audio.Resume(first, time.Second); err != nil || len(sink.ofType(smaf.EventWave)) != maxOutputWaves {
				t.Fatalf("resume reconstructed a refused wave or lost admitted output: %v", err)
			}
		})
	}
}

func TestAudioPauseRejectsInvalidClocksWithoutChangingOutput(t *testing.T) {
	for _, test := range []struct {
		name               string
		start, advance, at time.Duration
	}{
		{"negative", 0, 0, -time.Nanosecond},
		{"negative origin", -time.Millisecond, 0, 0},
		{"before origin", time.Second, time.Second, time.Second - time.Nanosecond},
		{"before emitted event", 0, 100 * time.Millisecond, 99 * time.Millisecond},
	} {
		t.Run(test.name, func(t *testing.T) {
			now := time.Unix(1000, 0)
			sink := &resumeAudioProbe{}
			audio := NewAudioWithClock(sink, func() time.Time { return now })
			handle := loadOwnedAudio(t, audio, []smaf.Event{
				{Type: smaf.EventNoteOn, Note: 60, Velocity: 100},
				{Time: 100, Type: smaf.EventNoteOn, Note: 67, Velocity: 90},
				{Time: 1000, Type: smaf.EventEnd},
			})
			if err := audio.Play(handle, test.start, false); err != nil {
				t.Fatal(err)
			}
			audio.Advance(test.advance)
			before := captureOwnedAudio(t, audio)
			sink.events, sink.stops = nil, nil
			if err := audio.Pause(handle, test.at); err == nil || audio.Paused(handle) || !audio.Playing(handle) {
				t.Fatal("invalid pause clock changed playback")
			}
			if after := captureOwnedAudio(t, audio); !reflect.DeepEqual(before, after) || len(sink.events) != 0 || len(sink.stops) != 0 {
				t.Fatal("invalid pause emitted or changed state")
			}
		})
	}
	for _, name := range []string{"backward guest", "overflow guest", "backward host"} {
		t.Run(name, func(t *testing.T) {
			now := time.Unix(1000, 0)
			sink := &resumeAudioProbe{}
			audio := NewAudioWithClock(sink, func() time.Time { return now })
			handle := loadOwnedAudio(t, audio, transientNote(1000))
			audio.Advance(0)
			now = now.Add(100 * time.Millisecond)
			if err := audio.Pause(handle, 100*time.Millisecond); err != nil {
				t.Fatal(err)
			}
			before := captureOwnedAudio(t, audio)
			sink.events, sink.stops, sink.resumed = nil, nil, nil
			at := 200 * time.Millisecond
			switch name {
			case "backward guest":
				at = 99 * time.Millisecond
			case "overflow guest":
				at = time.Duration(math.MaxInt64)
			case "backward host":
				now = now.Add(-time.Nanosecond)
			}
			if err := audio.Resume(handle, at); err == nil || !audio.Paused(handle) || audio.Playing(handle) {
				t.Fatal("invalid resume clock changed playback")
			}
			if after := captureOwnedAudio(t, audio); !reflect.DeepEqual(before, after) || len(sink.events) != 0 || len(sink.stops) != 0 || len(sink.resumed) != 0 {
				t.Fatal("invalid resume emitted or changed frozen state")
			}
		})
	}
}

func TestAudioPauseRefusesTransientBeforeAdvancingIt(t *testing.T) {
	now := time.Unix(1000, 0)
	sink := &resumeAudioProbe{}
	audio := NewAudioWithClock(sink, func() time.Time { return now })
	if err := audio.PlayTransient(transientNote(1000), 0); err != nil {
		t.Fatal(err)
	}
	audio.Advance(0)
	before := captureOwnedAudio(t, audio)
	handle := before.Sounds[0].Handle
	sink.events, sink.stops = nil, nil
	if err := audio.Pause(handle, time.Second); err == nil || audio.Paused(handle) {
		t.Fatal("a transient acquired a reusable paused cursor")
	}
	if after := captureOwnedAudio(t, audio); !reflect.DeepEqual(before, after) || len(sink.events) != 0 || len(sink.stops) != 0 {
		t.Fatal("refused pause advanced a transient into an uncapturable state")
	}
	audio.Advance(time.Second)
	if after := captureOwnedAudio(t, audio); len(after.Sounds) != 0 {
		t.Fatal("refused pause prevented natural transient reclamation")
	}
}

func TestAudioPauseCheckpointRejectsMalformedState(t *testing.T) {
	now := time.Unix(1000, 0)
	audio := NewAudioWithClock(nil, func() time.Time { return now })
	handle := loadOwnedAudio(t, audio, []smaf.Event{
		{Time: 20, Type: smaf.EventNoteOn, Note: 60, Velocity: 100},
		{Time: 20, Type: smaf.EventNoteOn, Channel: 9, Note: 38, Velocity: 90},
		{Time: 1000, Type: smaf.EventEnd},
	})
	if err := audio.Play(handle, 100*time.Millisecond, false); err != nil {
		t.Fatal(err)
	}
	audio.Advance(120 * time.Millisecond)
	now = now.Add(30 * time.Millisecond)
	if err := audio.Pause(handle, 150*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	saved := captureOwnedAudio(t, audio)
	for _, test := range []struct {
		name   string
		damage func(*AudioState)
	}{
		{"clock without pause", func(s *AudioState) { s.Sounds[0].Paused, s.Output.Sounds[0].Paused = false, false }},
		{"negative pause", func(s *AudioState) { s.Sounds[0].PausedAt = -time.Nanosecond }},
		{"pause before origin", func(s *AudioState) { s.Sounds[0].PausedAt = 99 * time.Millisecond }},
		{"negative paused origin", func(s *AudioState) { s.Sounds[0].StartedAt = -time.Millisecond }},
		{"pause before emitted event", func(s *AudioState) { s.Sounds[0].PausedAt = 119 * time.Millisecond }},
		{"missing frozen output", func(s *AudioState) { s.Output.Sounds[0].Paused = false }},
		{"unpaused timeline", func(s *AudioState) { s.Sounds[0].Paused, s.Sounds[0].PausedAt = false, 0 }},
		{"transient pause", func(s *AudioState) { s.Sounds[0].Transient = true }},
		{"negative note age", func(s *AudioState) { s.Output.Notes[0].Age = -time.Nanosecond }},
		{"inconsistent drum age", func(s *AudioState) { s.Output.Notes[1].Age++ }},
	} {
		t.Run(test.name, func(t *testing.T) {
			bad := saved
			bad.Sounds = slices.Clone(saved.Sounds)
			bad.Output.Sounds = slices.Clone(saved.Output.Sounds)
			bad.Output.Notes = slices.Clone(saved.Output.Notes)
			test.damage(&bad)
			sink := &resumeAudioProbe{}
			if fresh, err := NewAudioFromStateWithClock(bad, sink, func() time.Time { return now }); err == nil || fresh != nil {
				t.Fatal("malformed paused checkpoint was accepted")
			}
			if len(sink.events) != 0 || len(sink.stops) != 0 || len(sink.resumed) != 0 {
				t.Fatal("malformed paused checkpoint reached the sink")
			}
		})
	}
}

func TestAudioPauseIsDiscardedByStopRestartAndClose(t *testing.T) {
	for _, action := range []string{"stop", "stop all", "restart", "close"} {
		t.Run(action, func(t *testing.T) {
			now := time.Unix(1000, 0)
			sink := &resumeAudioProbe{}
			audio := NewAudioWithClock(sink, func() time.Time { return now })
			handle := loadOwnedAudio(t, audio, []smaf.Event{
				{Type: smaf.EventNoteOn, Note: 60, Velocity: 100},
				{Type: smaf.EventWave, WaveChannels: 1, SamplingRate: 4, Wave: []int16{1, 2, 3, 4}},
				{Time: 1000, Type: smaf.EventEnd},
			})
			audio.Advance(0)
			now = now.Add(250 * time.Millisecond)
			if err := audio.Pause(handle, 250*time.Millisecond); err != nil {
				t.Fatal(err)
			}
			switch action {
			case "stop":
				audio.Stop(handle)
			case "stop all":
				audio.StopAll()
			case "restart":
				if err := audio.Play(handle, time.Second, false); err != nil {
					t.Fatal(err)
				}
			case "close":
				if err := audio.Close(handle); err != nil {
					t.Fatal(err)
				}
			}
			if audio.Paused(handle) {
				t.Fatal("explicit lifecycle action retained a paused cursor")
			}
			saved := captureOwnedAudio(t, audio)
			if len(saved.Output.Notes) != 0 || len(saved.Output.Waves) != 0 {
				t.Fatal("explicit lifecycle action retained frozen output")
			}
			sink.events, sink.resumed = nil, nil
			err := audio.Resume(handle, time.Second)
			if (err != nil) != (action == "close") || len(sink.events) != 0 || len(sink.resumed) != 0 {
				t.Fatal("discarded cursor could still be resumed")
			}
			if action == "restart" {
				audio.Advance(time.Second)
				waves := sink.ofType(smaf.EventWave)
				if len(sink.ofType(smaf.EventNoteOn)) != 1 || len(waves) != 1 || !slices.Equal(waves[0].event.Wave, []int16{1, 2, 3, 4}) {
					t.Fatal("explicit restart reused the paused suffix")
				}
			}
		})
	}
}

func TestAudioPauseKeepsCompletedPCMTail(t *testing.T) {
	now := time.Unix(1000, 0)
	sink := &resumeAudioProbe{}
	audio := NewAudioWithClock(sink, func() time.Time { return now })
	handle := loadOwnedAudio(t, audio, []smaf.Event{
		{Type: smaf.EventWave, WaveChannels: 1, SamplingRate: 4, Wave: []int16{1, 2, 3, 4}},
		{Time: 100, Type: smaf.EventEnd},
	})
	audio.Advance(100 * time.Millisecond)
	now = now.Add(250 * time.Millisecond)
	if err := audio.Pause(handle, 250*time.Millisecond); err != nil || !audio.Paused(handle) || audio.Playing(handle) {
		t.Fatalf("completed score could not pause its PCM tail: %v", err)
	}
	now = now.Add(10 * time.Second)
	sink.events = nil
	if err := audio.Resume(handle, 10250*time.Millisecond); err != nil || audio.Paused(handle) || audio.Playing(handle) {
		t.Fatalf("resuming a PCM tail restarted its completed score: %v", err)
	}
	waves := sink.ofType(smaf.EventWave)
	if len(waves) != 1 || !slices.Equal(waves[0].event.Wave, []int16{2, 3, 4}) || len(sink.resumed) != 0 {
		t.Fatalf("completed PCM tail did not resume its unheard suffix: %+v", waves)
	}
	captureOwnedAudio(t, audio)
}
