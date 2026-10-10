package backend

import (
	"testing"
	"time"
)

func TestAudioWavePhaseValidationPrecedesRuntimeConstruction(t *testing.T) {
	for _, phase := range []uint32{1000000000, ^uint32(0)} {
		state := checkpointAudioFixture()
		state.Output.Waves[0].FramePhase = phase
		sink := &recordingSink{}
		if audio, err := NewAudioFromState(state, sink); err == nil || audio != nil || len(sink.calls) != 0 {
			t.Fatalf("invalid phase %d reached a runtime or sink: %v", phase, err)
		}
	}
	for _, version := range []uint32{6, 7, 8} {
		state := checkpointAudioVersionFixture(version)
		audio, err := NewAudioFromStateWithClock(state, nil, func() time.Time { return time.Unix(1000, 0) })
		if err != nil {
			t.Fatalf("version %d baseline is invalid: %v", version, err)
		}
		upgraded, err := audio.CaptureState()
		if err != nil || upgraded.Version != audioStateVersion || len(upgraded.Output.Waves) != 1 || upgraded.Output.Waves[0].FramePhase != 0 {
			t.Fatalf("version %d invented legacy subframe position: %v", version, err)
		}
		state.Output.Waves[0].FramePhase = 1
		if _, err := NewAudioFromState(state, nil); err == nil {
			t.Fatalf("version %d accepted an unrepresentable phase", version)
		}
	}
	for _, phase := range []uint32{1, 999999999} {
		state := checkpointAudioFixture()
		state.Output.Waves[0].FramePhase = phase
		if _, err := NewAudioFromState(state, nil); err != nil {
			t.Fatalf("valid phase %d refused: %v", phase, err)
		}
	}
}
