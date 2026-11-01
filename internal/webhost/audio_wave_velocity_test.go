package webhost

import (
	"bytes"
	"testing"

	"github.com/movingwoo/wfeature/internal/audio/smaf"
)

func TestWaveVelocityNegotiationNeedsOwnershipAndTheStream(t *testing.T) {
	for _, test := range []struct {
		value     string
		ownership bool
		protocol  int
		want      bool
	}{
		{"1", true, protocolStream, true},
		{"1", false, protocolStream, false},
		{"1", true, protocolPictures, false},
		{"", true, protocolStream, false},
		{"true", true, protocolStream, false},
	} {
		if got := negotiatedWaveVelocity(test.value, test.ownership, test.protocol); got != test.want {
			t.Errorf("negotiatedWaveVelocity(%q, %t, %d) = %t", test.value, test.ownership, test.protocol, got)
		}
	}
}

// A stream wave's velocity travels as its own operation right before the
// wave it belongs to; a wave without one is sent exactly as before.
func TestStreamAudioPrefixesAWaveWithItsVelocity(t *testing.T) {
	collector := &audioCollector{pcmChannels: true}
	samples := []int16{1, -1, 2, -2}
	collector.AudioEvent(3, smaf.Event{Type: smaf.EventWave, Velocity: 40, WaveChannels: 1, SamplingRate: 8000, Wave: samples})
	collector.AudioEvent(3, smaf.Event{Type: smaf.EventWave, WaveChannels: 1, SamplingRate: 8000, Wave: samples})
	collector.ResumeWave(3, smaf.Event{Type: smaf.EventWave, Velocity: 41, WaveChannels: 1, SamplingRate: 8000, Wave: samples}, 5)
	events, _ := collector.takeBatch()
	var definitions audioDefinitions
	var ops []byte
	operations := readAudio(t, definitions.encode(events))
	for _, operation := range operations {
		ops = append(ops, operation.op)
	}
	want := []byte{audioOpSelectSound, audioOpDefine, audioOpWaveVelocity, audioOpPlayWave, audioOpPlayWave, audioOpWaveVelocity, audioOpResumeWave}
	if !bytes.Equal(ops, want) {
		t.Fatalf("operations % x, want % x", ops, want)
	}
	if operations[2].operands[0] != 40 || operations[5].operands[0] != 41 {
		t.Fatalf("wave velocities %d and %d, want 40 and 41", operations[2].operands[0], operations[5].operands[0])
	}
}

func TestPagesWithoutWaveVelocitiesHearEveryWaveAtFullLevel(t *testing.T) {
	for _, negotiated := range []bool{false, true} {
		runner := stalledRunner(t, 1)
		runner.protocol, runner.soundOwnership, runner.soundResume = protocolStream, true, true
		runner.soundWaveVelocity = negotiated
		if !runner.sendAudio([]audioEvent{{Kind: audioPlayWave, Sound: 3, Channels: 1, Rate: 8000, Velocity: 40, pcm: []byte{1, 0}}}, true) {
			t.Fatal("audio was not queued")
		}
		message := <-runner.outText
		found := false
		for _, operation := range readAudio(t, message.binary) {
			found = found || operation.op == audioOpWaveVelocity
		}
		if found != negotiated {
			t.Fatalf("negotiated=%t sent a wave velocity: %t", negotiated, found)
		}
	}
}
