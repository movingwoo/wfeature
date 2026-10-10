package backend

import (
	"errors"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/movingwoo/wfeature/internal/audio/smaf"
)

type pcmAudioProbe struct {
	gainAudioProbe
	capable bool
}

func (sink *pcmAudioProbe) PCMChannels() bool { return sink.capable }

func pcmTestEvents() []smaf.Event {
	return []smaf.Event{
		{Type: smaf.EventWave, PCMChannel: 1, WaveChannels: 1, SamplingRate: 4, Wave: []int16{10000, -10000, 8000, -8000, 6000, -6000, 4000, -4000}},
		{Time: 250, Type: smaf.EventPCMControl, PCMChannel: 1, Control: 7, Value: 63},
		{Time: 250, Type: smaf.EventPCMControl, PCMChannel: 1, Control: 11, Value: 64},
		{Time: 250, Type: smaf.EventPCMControl, PCMChannel: 1, Control: 10, Value: 0},
		{Time: 3000, Type: smaf.EventEnd},
	}
}

func TestPCMChannelsLiveControlsAndCheckpointTail(t *testing.T) {
	now := time.Unix(1000, 0)
	sink := &pcmAudioProbe{capable: true}
	audio := NewAudioWithClock(sink, func() time.Time { return now })
	first := loadOwnedAudio(t, audio, pcmTestEvents())
	second := loadOwnedAudio(t, audio, []smaf.Event{pcmTestEvents()[0], {Time: 3000, Type: smaf.EventEnd}})
	audio.Advance(0)
	audio.Advance(250 * time.Millisecond)
	controls, waves := sink.ofType(smaf.EventPCMControl), sink.ofType(smaf.EventWave)
	if len(controls) != 3 || len(waves) != 2 || len(sink.stops) != 2 {
		t.Fatalf("live control restarted output: controls=%d waves=%d stops=%d", len(controls), len(waves), len(sink.stops))
	}
	for _, event := range controls {
		if event.sound != first || event.event.PCMChannel != 1 {
			t.Fatal("PCM control crossed its owner or channel")
		}
	}
	if waves[0].event.PCMChannel != 1 || !slices.Equal(waves[0].event.Wave, pcmTestEvents()[0].Wave) {
		t.Fatal("live PCM lost its group or changed its raw payload")
	}
	now = now.Add(250 * time.Millisecond)
	saved := captureOwnedAudio(t, audio)
	want := []AudioPCMChannelState{
		{Sound: first, Channel: 1, Volume: 63, Expression: 64, Pan: 0, VolumeSet: true},
		{Sound: second, Channel: 1, Volume: 127, Expression: 127, Pan: 64},
	}
	if saved.Version != audioStateVersion || !reflect.DeepEqual(saved.Output.PCMChannels, want) || saved.Output.Waves[0].PCMChannel != 1 {
		t.Fatalf("checkpoint lost independent PCM state: version=%d channels=%+v", saved.Version, saved.Output.PCMChannels)
	}
	encoded, err := EncodeCheckpointRecord(saved)
	if err != nil {
		t.Fatal(err)
	}
	var decoded AudioState
	if err := DecodeCheckpointRecord(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	freshSink := &pcmAudioProbe{capable: true}
	fresh, err := NewAudioFromStateWithClock(decoded, freshSink, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	decoded.Output.PCMChannels[0].Volume = 0
	fresh.ActivateOutputClock()
	fresh.ResumeOutput()
	if got := captureOwnedAudio(t, fresh); !reflect.DeepEqual(saved, got) {
		t.Fatal("restore retained caller state or changed the saved position")
	}
	controls = freshSink.ofType(smaf.EventPCMControl)
	waves = freshSink.ofType(smaf.EventWave)
	if len(controls) != 5 || len(waves) != 2 || !slices.Equal(waves[0].event.Wave, pcmTestEvents()[0].Wave[2:]) || waves[0].event.PCMChannel != 1 {
		t.Fatalf("replay lost current controls or trimmed raw tail: controls=%d waves=%+v", len(controls), waves)
	}
	seenWave := false
	for _, event := range freshSink.events {
		if event.event.Type == smaf.EventWave {
			seenWave = true
		}
		if event.event.Type == smaf.EventPCMControl && seenWave {
			t.Fatal("replay configured PCM after its wave")
		}
	}
}

func TestPCMChannelsFallbackTracksCapabilitiesAndGain(t *testing.T) {
	sink := &pcmAudioProbe{capable: false}
	now := time.Unix(1000, 0)
	audio := NewAudioWithClock(sink, func() time.Time { return now })
	events := []smaf.Event{
		{Type: smaf.EventPCMControl, PCMChannel: 8, Control: 7, Value: 64},
		{Type: smaf.EventPCMControl, PCMChannel: 8, Control: 10, Value: 0},
		{Type: smaf.EventWave, PCMChannel: 8, WaveChannels: 1, SamplingRate: 4, Wave: []int16{10000, -10000, 8000, -8000}},
		{Time: 2000, Type: smaf.EventEnd},
	}
	handle := loadOwnedAudio(t, audio, events)
	audio.Advance(0)
	waves := sink.ofType(smaf.EventWave)
	if len(sink.ofType(smaf.EventPCMControl)) != 0 || len(waves) != 1 || waves[0].event.PCMChannel != 0 || waves[0].event.WaveChannels != 2 || !slices.Equal(waves[0].event.Wave[:4], []int16{2539, 0, -2539, 0}) {
		t.Fatalf("legacy onset gain/pan is wrong: %+v", waves)
	}
	// A reconnect can gain channel support without replacing its collector.
	sink.capable, sink.events = true, nil
	audio.ResumeOutput()
	if waves = sink.ofType(smaf.EventWave); len(sink.ofType(smaf.EventPCMControl)) != 3 || len(waves) != 1 || waves[0].event.PCMChannel != 8 || !slices.Equal(waves[0].event.Wave, events[2].Wave) {
		t.Fatal("capability change reused baked PCM or discarded channel state")
	}
	legacy := &ownedAudioProbe{}
	audio.SetSink(legacy)
	audio.SetSoundVolume(handle, 50)
	audio.ResumeOutput()
	waves = legacy.ofType(smaf.EventWave)
	if len(waves) != 1 || !slices.Equal(waves[0].event.Wave[:4], []int16{1269, 0, -1269, 0}) {
		t.Fatalf("clip gain was not applied once after PCM gain: %+v", waves)
	}
}

func TestPCMChannelsPauseResumeAndExplicitReset(t *testing.T) {
	now := time.Unix(1000, 0)
	sink := &pcmAudioProbe{capable: true}
	audio := NewAudioWithClock(sink, func() time.Time { return now })
	handle := loadOwnedAudio(t, audio, pcmTestEvents())
	audio.Advance(250 * time.Millisecond)
	if err := audio.Pause(handle, 250*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	now = now.Add(time.Second)
	if got := captureOwnedAudio(t, audio).Output.PCMChannels; len(got) != 1 || got[0].Volume != 63 {
		t.Fatal("pause discarded PCM state")
	}
	if err := audio.Resume(handle, 1250*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if len(sink.ofType(smaf.EventWave)) != 2 {
		t.Fatal("resume did not reconstruct the frozen wave")
	}
	audio.Stop(handle)
	if got := captureOwnedAudio(t, audio).Output.PCMChannels; len(got) != 0 {
		t.Fatal("explicit stop retained channel controls")
	}
	if err := audio.Play(handle, 1250*time.Millisecond, false); err != nil {
		t.Fatal(err)
	}
	audio.Advance(1250 * time.Millisecond)
	if got := captureOwnedAudio(t, audio).Output.PCMChannels; len(got) != 1 || got[0].VolumeSet || got[0].Volume != 127 {
		t.Fatal("restart inherited the previous run's controls")
	}
}

func TestPCMChannelsAdmissionAndMalformedRestore(t *testing.T) {
	audio := resourceAudio(nil)
	audio.resourceLimits.pcmChannels = 1
	handle := loadOwnedAudio(t, audio, pcmTestEvents())
	audio.Advance(250 * time.Millisecond)
	before := captureOwnedAudio(t, audio)
	if next, err := audio.LoadEvents(pcmTestEvents()); next != 0 || !errors.Is(err, ErrAudioResourceLimit) {
		t.Fatalf("second owner exceeded group budget: %d %v", next, err)
	}
	if !reflect.DeepEqual(before, captureOwnedAudio(t, audio)) {
		t.Fatal("PCM admission refusal changed state")
	}
	for _, mutate := range []func(*AudioState){
		func(s *AudioState) { s.Version = 6 },
		func(s *AudioState) { s.Output.PCMChannels[0].Channel = 0 },
		func(s *AudioState) { s.Output.PCMChannels[0].Volume = 128 },
		func(s *AudioState) { s.Output.PCMChannels[0].VolumeSet = false },
		func(s *AudioState) { s.Output.PCMChannels[0].Channel = 99; s.Output.Waves[0].PCMChannel = 99 },
		func(s *AudioState) { s.Output.Waves[0].PCMChannel = 2 },
		func(s *AudioState) { s.Output.PCMChannels = append(s.Output.PCMChannels, s.Output.PCMChannels[0]) },
	} {
		bad := captureOwnedAudio(t, audio)
		mutate(&bad)
		if restored, err := NewAudioFromState(bad, nil); restored != nil || err == nil {
			t.Fatal("malformed PCM checkpoint was adopted")
		}
	}
	if err := audio.Close(handle); err != nil {
		t.Fatal(err)
	}
	if _, err := audio.LoadEvents(pcmTestEvents()); err != nil {
		t.Fatalf("close retained PCM reservation: %v", err)
	}
}

func TestPCMChannelsRejectInvalidAuthoredEvents(t *testing.T) {
	for _, bad := range []smaf.Event{
		{Type: smaf.EventPCMControl, Control: 7, Value: 127},
		{Type: smaf.EventPCMControl, PCMChannel: 1, Control: 64, Value: 1},
		{Type: smaf.EventPCMControl, PCMChannel: 1, Control: 11, Value: 128},
		{Type: smaf.EventNoteOn, PCMChannel: 1, Velocity: 64},
		{Type: smaf.EventWave, PCMChannel: 1, WaveChannels: 2, SamplingRate: 4, Wave: []int16{1, 2}},
	} {
		if handle, err := NewAudio(nil).LoadEvents([]smaf.Event{bad}); handle != 0 || err == nil {
			t.Fatalf("invalid PCM event admitted: %+v", bad)
		}
	}
}

func TestPCMChannelsLegacyCheckpointAdoptsWithoutInventingControls(t *testing.T) {
	data := checkpointAudioJSON(t, checkpointAudioDocument(t, true))
	var legacy AudioState
	if err := DecodeCheckpointRecord(data, &legacy); err != nil {
		t.Fatal(err)
	}
	sink := &pcmAudioProbe{capable: true}
	audio, err := NewAudioFromStateWithClock(legacy, sink, func() time.Time { return time.Unix(1000, 0) })
	if err != nil {
		t.Fatal(err)
	}
	audio.ActivateOutputClock()
	audio.ResumeOutput()
	waves := sink.ofType(smaf.EventWave)
	if len(waves) != 1 || waves[0].event.PCMChannel != 0 || !slices.Equal(waves[0].event.Wave, []int16{11, 22, 33, 44}) || len(sink.ofType(smaf.EventPCMControl)) != 0 {
		t.Fatal("legacy restore invented routing or changed PCM")
	}
	upgraded := captureOwnedAudio(t, audio)
	if upgraded.Version != audioStateVersion || len(upgraded.Output.PCMChannels) != 0 {
		t.Fatal("legacy restore did not write current state")
	}
	encoded, err := EncodeCheckpointRecord(upgraded)
	if err != nil {
		t.Fatal(err)
	}
	var got AudioState
	if err := DecodeCheckpointRecord(encoded, &got); err != nil {
		t.Fatal(err)
	}
}

func TestPCMChannelsCatchupChargesStereoFallback(t *testing.T) {
	events := []smaf.Event{
		{Type: smaf.EventPCMControl, PCMChannel: 1, Control: 10, Value: 64},
		{Type: smaf.EventWave, PCMChannel: 1, WaveChannels: 1, SamplingRate: 8000, Wave: []int16{1, 2, 3}},
		{Time: 2, Type: smaf.EventEnd},
	}
	saved := catchupAudioState(t, events)
	saved.Sounds[0].Repeat = true
	checkCatchupBudget(t, saved, 3*time.Millisecond, audioCatchupLimits{events: 5, bytes: 24})
}

func TestPCMChannelsRepeatKeepsControlsAndEarlierTails(t *testing.T) {
	sink := &pcmAudioProbe{capable: true}
	audio := resourceAudio(sink)
	events := []smaf.Event{pcmTestEvents()[0], {Time: 250, Type: smaf.EventPCMControl, PCMChannel: 1, Control: 7, Value: 0}, {Time: 500, Type: smaf.EventEnd}}
	handle := loadOwnedAudio(t, audio, events)
	if err := audio.SetRepeat(handle, true); err != nil {
		t.Fatal(err)
	}
	audio.Advance(500 * time.Millisecond)
	saved := captureOwnedAudio(t, audio)
	if len(saved.Output.Waves) != 2 || len(saved.Output.PCMChannels) != 1 || saved.Output.PCMChannels[0].Volume != 0 || !saved.Output.PCMChannels[0].VolumeSet {
		t.Fatal("repeat discarded a tail or reset live controls")
	}
	waves := sink.ofType(smaf.EventWave)
	if len(waves) != 2 || !slices.Equal(waves[0].event.Wave, waves[1].event.Wave) {
		t.Fatal("repeat baked controls into raw samples")
	}
	if err := audio.SetRepeat(handle, false); err != nil {
		t.Fatal(err)
	}
	audio.Advance(time.Second)
	saved = captureOwnedAudio(t, audio)
	if audio.Playing(handle) || len(saved.Output.Waves) != 2 || len(saved.Output.PCMChannels) != 1 {
		t.Fatal("natural completion cancelled PCM channel state or tails")
	}
}

func TestPCMChannelsControlOnlyReservesStateAtAdmission(t *testing.T) {
	audio := resourceAudio(nil)
	audio.resourceLimits.pcmChannels = 2
	events := []smaf.Event{
		{Type: smaf.EventPCMControl, PCMChannel: 65535, Control: 7, Value: 100},
		{Type: smaf.EventPCMControl, PCMChannel: 1, Control: 11, Value: 70},
		{Type: smaf.EventPCMControl, PCMChannel: 65535, Control: 10, Value: 127},
		{Time: 1, Type: smaf.EventEnd},
	}
	used, err := soundResourceUsage(events, nil)
	if err != nil || used != (audioResourceUsage{entries: 6, bytes: 416, pcmChannels: 2}) {
		t.Fatalf("control-only resource charge: %+v %v", used, err)
	}
	loadOwnedAudio(t, audio, events)
	audio.Advance(time.Millisecond)
	saved := captureOwnedAudio(t, audio)
	if len(saved.Output.PCMChannels) != 2 || saved.Output.PCMChannels[0].Channel != 1 || saved.Output.PCMChannels[1].Channel != 65535 {
		t.Fatal("control-only state was lost or not sorted")
	}
	if handle, err := audio.LoadEvents(events[:1]); handle != 0 || !errors.Is(err, ErrAudioResourceLimit) {
		t.Fatal("control-only owner bypassed the group budget")
	}
}
