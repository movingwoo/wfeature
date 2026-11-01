package backend

import (
	"testing"
	"time"

	"github.com/movingwoo/wfeature/internal/audio/smaf"
)

// A stream wave's velocity reaches the sink with it, survives a checkpoint
// while the wave sounds, and comes back with the reconstructed wave.
func TestAudioWaveVelocitySurvivesCaptureAndReconstruction(t *testing.T) {
	now := time.Unix(1000, 0)
	sink := &ownedAudioProbe{}
	audio := NewAudioWithClock(sink, func() time.Time { return now })
	owner := loadOwnedAudio(t, audio, []smaf.Event{
		{Type: smaf.EventWave, Velocity: 40, WaveChannels: 1, SamplingRate: 8, Wave: []int16{1, 2, 3, 4, 5, 6, 7, 8}},
		{Time: 2000, Type: smaf.EventEnd},
	})
	audio.Advance(0)
	if waves := sink.ofType(smaf.EventWave); len(waves) != 1 || waves[0].event.Velocity != 40 {
		t.Fatalf("emitted waves %+v, want one at velocity 40", waves)
	}
	now = now.Add(250 * time.Millisecond)
	audio.Advance(250 * time.Millisecond)
	saved := captureOwnedAudio(t, audio)
	if saved.Version != audioStateVersion || len(saved.Output.Waves) != 1 || saved.Output.Waves[0].Velocity != 40 {
		t.Fatalf("captured waves %+v", saved.Output.Waves)
	}
	encoded, err := EncodeCheckpointRecord(saved)
	if err != nil {
		t.Fatal(err)
	}
	var decoded AudioState
	if err := DecodeCheckpointRecord(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	restoredSink := &ownedAudioProbe{}
	restored, err := NewAudioFromStateWithClock(decoded, restoredSink, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	restored.ResumeOutput()
	waves := restoredSink.ofType(smaf.EventWave)
	if len(waves) != 1 || waves[0].sound != owner || waves[0].event.Velocity != 40 {
		t.Fatalf("reconstructed waves %+v, want the owner's wave at velocity 40", waves)
	}
}

// No writer before version 11 gave a wave a velocity; a record that has one
// is a hybrid, whether in a sound's events or in a sounding wave.
func TestAudioStateBeforeWaveVelocitiesRefusesThem(t *testing.T) {
	event := checkpointAudioVersionFixture(10)
	event.Sounds[0].Events[0].Velocity = 40
	if err := validateAudioState(event); err == nil {
		t.Fatal("version 10 accepted a wave event velocity")
	}
	sounding := checkpointAudioVersionFixture(10)
	sounding.Output.Waves[0].Velocity = 40
	if err := validateAudioState(sounding); err == nil {
		t.Fatal("version 10 accepted a sounding wave velocity")
	}
	if err := validateAudioState(checkpointAudioVersionFixture(10)); err != nil {
		t.Fatalf("version 10 without wave velocities was refused: %v", err)
	}
	invalid := checkpointAudioFixture()
	invalid.Output.Waves[0].Velocity = 128
	if err := validateAudioState(invalid); err == nil {
		t.Fatal("a sounding wave velocity past 127 was accepted")
	}
}
