package backend

import (
	"bytes"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestAudioPCMAdmissionStateRejectsInvalidChargesBeforeOutput(t *testing.T) {
	for _, test := range []struct {
		name   string
		change func(*AudioState)
	}{
		{"missing charge", func(s *AudioState) { s.Output.Waves[0].BudgetBytes = 0 }},
		{"charge smaller than tail", func(s *AudioState) { s.Output.Waves[0].BudgetBytes = 6 }},
		{"odd charge", func(s *AudioState) { s.Output.Waves[0].BudgetBytes = 9 }},
		{"partial stereo frame", func(s *AudioState) {
			s.Output.Waves[0].Channels, s.Output.Waves[0].BudgetBytes = 2, 10
		}},
		{"single charge exceeds limit", func(s *AudioState) { s.Output.Waves[0].BudgetBytes = maxOutputBytes + 2 }},
		{"combined charges exceed limit", func(s *AudioState) {
			s.Output.Waves = append(s.Output.Waves, s.Output.Waves[0])
			s.Output.Waves[0].BudgetBytes = maxOutputBytes
		}},
		{"v6 with explicit charge", func(s *AudioState) { s.Version = 6 }},
		{"v7 with explicit charge", func(s *AudioState) { s.Version = 7 }},
	} {
		t.Run(test.name, func(t *testing.T) {
			state := checkpointAudioFixture()
			test.change(&state)
			sink := &recordingSink{}
			if audio, err := NewAudioFromState(state, sink); err == nil || audio != nil || len(sink.calls) != 0 {
				t.Fatalf("invalid charge reached a runtime or output: %v", err)
			}
		})
	}
}

func TestAudioPCMAdmissionLegacyChargesUpgradeWithoutChangingCaller(t *testing.T) {
	for _, version := range []uint32{6, 7} {
		state := checkpointAudioFixture()
		state.Version = version
		state.Output.Waves[0].BudgetBytes = 0
		before := slices.Clone(state.Output.Waves)
		now := time.Unix(1000, 0)
		audio, err := NewAudioFromStateWithClock(state, nil, func() time.Time { return now })
		if err != nil {
			t.Fatalf("version %d: %v", version, err)
		}
		got, err := audio.CaptureState()
		if err != nil || got.Version != audioStateVersion || got.Output.Waves[0].BudgetBytes != 8 ||
			!reflect.DeepEqual(state.Output.Waves, before) {
			t.Fatalf("version %d changed caller data or failed charge migration: %v", version, err)
		}
		got.Output.Waves[0].Samples[0] = 0
		again, err := audio.CaptureState()
		if err != nil || again.Output.Waves[0].Samples[0] != 11 || again.Output.Waves[0].BudgetBytes != 8 {
			t.Fatal("upgraded output retained a caller-owned sample or lost its charge")
		}
	}
}

func TestAudioPCMAdmissionDiagnosticsRespectProfileAndSurviveSinkChanges(t *testing.T) {
	var log bytes.Buffer
	now := time.Unix(1000, 0)
	audio := NewAudioWithClock(nil, func() time.Time { return now })
	audio.SetLogger(NewLogger(&log))
	for range maxOutputWaves + 10 {
		audio.sink.PlayWave(1, 1, []int16{1})
	}
	if audio.sink.pcmRefusals != 10 {
		t.Fatalf("nil output sink lost PCM refusal diagnostics: %d", audio.sink.pcmRefusals)
	}
	wantLogs := 0
	if DebugBuild() {
		wantLogs = 4 // The first, second, fourth and eighth refused waves.
		for _, field := range []string{"reason=\"wave limit\"", "sound=0", "waves=256", "bytes=512", "incoming_bytes=2", "refused=8"} {
			if !strings.Contains(log.String(), field) {
				t.Fatalf("PCM admission diagnostic lacks %s", field)
			}
		}
	}
	if strings.Count(log.String(), "audio PCM admission refused") != wantLogs {
		t.Fatalf("profile %s emitted unbounded or missing PCM diagnostics: %s", BuildProfile(), log.String())
	}
	before, err := audio.CaptureState()
	if err != nil {
		t.Fatal(err)
	}
	audio.SetSink(&recordingSink{})
	for range 6 {
		audio.sink.PlayWave(1, 1, []int16{1})
	}
	if DebugBuild() {
		wantLogs++ // Changing output must not restart the diagnostic throttle.
	}
	if audio.sink.pcmRefusals != 16 || strings.Count(log.String(), "audio PCM admission refused") != wantLogs {
		t.Fatal("changing the output sink reset or detached PCM diagnostics")
	}
	after, err := audio.CaptureState()
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("diagnostic counters changed portable audio state")
	}
	audio.SetLogger(nil)
	for range 16 { // Reach the next diagnostic threshold after detachment.
		audio.sink.PlayWave(1, 1, []int16{1})
	}
	if strings.Count(log.String(), "audio PCM admission refused") != wantLogs {
		t.Fatal("detached PCM logger still received output")
	}
}
