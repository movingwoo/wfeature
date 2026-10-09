package backend

import (
	"fmt"
	"math/big"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/movingwoo/wfeature/internal/audio/smaf"
)

type wavePhaseRecord struct {
	sound AudioHandle
	event smaf.Event
	phase uint32
}

type wavePhaseProbe struct {
	ownedAudioProbe
	capable bool
	resumed []wavePhaseRecord
}

type gainWavePhaseProbe struct {
	wavePhaseProbe
	gains map[AudioHandle]uint16
}

func (sink *gainWavePhaseProbe) SoundGain(sound AudioHandle, gain uint16) {
	if sink.gains == nil {
		sink.gains = make(map[AudioHandle]uint16)
	}
	sink.gains[sound] = gain
}

func (sink *wavePhaseProbe) PCMChannels() bool { return sink.capable }
func (sink *wavePhaseProbe) ResumeWave(sound AudioHandle, event smaf.Event, phase uint32) {
	event.Wave = slices.Clone(event.Wave)
	sink.resumed = append(sink.resumed, wavePhaseRecord{sound, event, phase})
}

func (sink *wavePhaseProbe) waves() []wavePhaseRecord {
	waves := slices.Clone(sink.resumed)
	for _, event := range sink.ofType(smaf.EventWave) {
		waves = append(waves, wavePhaseRecord{sound: event.sound, event: event.event})
	}
	return waves
}

func wavePhaseRoundTrip(t *testing.T, state AudioState) AudioState {
	t.Helper()
	data, err := EncodeCheckpointRecord(state)
	if err != nil {
		t.Fatal(err)
	}
	var decoded AudioState
	if err := DecodeCheckpointRecord(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(state, decoded) {
		t.Fatal("checkpoint codec changed PCM position or payload")
	}
	return decoded
}

func TestAudioWavePhaseRestoreMatchesFutureSuffixAndExpiry(t *testing.T) {
	origin := time.Unix(1000, 0)
	liveNow := origin
	live := NewAudioWithClock(nil, func() time.Time { return liveNow })
	loadOwnedAudio(t, live, []smaf.Event{
		{Type: smaf.EventWave, WaveChannels: 1, SamplingRate: 4, Wave: []int16{10, 11, 12, 13}},
		{Time: 2000, Type: smaf.EventEnd},
	})
	live.Advance(0)
	liveNow = origin.Add(125 * time.Millisecond)
	saved := captureOwnedAudio(t, live)
	if saved.Output.Waves[0].FramePhase != 500000000 || saved.Output.Waves[0].BudgetBytes != 8 {
		t.Fatal("half-frame capture lost phase or its original admission charge")
	}
	freshOrigin := time.Unix(2000, 0)
	freshNow := freshOrigin
	fresh, err := NewAudioFromStateWithClock(saved, nil, func() time.Time { return freshNow })
	if err != nil {
		t.Fatal(err)
	}
	fresh.ActivateOutputClock()
	for _, elapsed := range []time.Duration{125 * time.Millisecond, 500 * time.Millisecond, 875 * time.Millisecond} {
		liveNow = origin.Add(125*time.Millisecond + elapsed)
		freshNow = freshOrigin.Add(elapsed)
		want, got := captureOwnedAudio(t, live), captureOwnedAudio(t, fresh)
		if !reflect.DeepEqual(got.Output.Waves, want.Output.Waves) {
			t.Errorf("after %v: restored suffix/expiry differs: got %+v; want %+v", elapsed, got.Output.Waves, want.Output.Waves)
		}
	}
}

func TestAudioWavePhaseExpiryReleasesAdmissionAtTheOriginalDeadline(t *testing.T) {
	origin := time.Unix(1000, 0)
	liveNow := origin
	liveSink := &wavePhaseProbe{}
	live := NewAudioWithClock(liveSink, func() time.Time { return liveNow })
	loadOwnedAudio(t, live, append(admissionWaves(256, smaf.Event{
		Type: smaf.EventWave, WaveChannels: 1, SamplingRate: 4, Wave: []int16{10},
	}), smaf.Event{Time: 1000, Type: smaf.EventEnd}))
	candidate := loadOwnedAudio(t, live, []smaf.Event{
		{Time: 249, Type: smaf.EventWave, WaveChannels: 1, SamplingRate: 4, Wave: []int16{20}},
		{Time: 250, Type: smaf.EventWave, WaveChannels: 1, SamplingRate: 4, Wave: []int16{30}},
		{Time: 1000, Type: smaf.EventEnd},
	})
	live.Advance(125 * time.Millisecond)
	saved := wavePhaseRoundTrip(t, captureOwnedAudio(t, live))
	freshNow := time.Unix(2000, 0)
	freshSink := &wavePhaseProbe{}
	fresh, err := NewAudioFromStateWithClock(saved, freshSink, func() time.Time { return freshNow })
	if err != nil {
		t.Fatal(err)
	}
	fresh.ActivateOutputClock()
	if err := fresh.RebasePlaybackClock(125*time.Millisecond, 1); err != nil {
		t.Fatal(err)
	}
	for _, run := range []struct {
		name  string
		audio *Audio
		sink  *wavePhaseProbe
	}{{"live", live, liveSink}, {"restored", fresh, freshSink}} {
		run.sink.events, run.sink.resumed = nil, nil
		run.audio.Advance(249 * time.Millisecond)
		if len(run.sink.waves()) != 0 {
			t.Fatalf("%s granted capacity before the original end", run.name)
		}
		run.audio.Advance(250 * time.Millisecond)
		waves := run.sink.waves()
		if len(waves) != 1 || waves[0].sound != candidate || !slices.Equal(waves[0].event.Wave, []int16{30}) {
			t.Fatalf("%s lost exact-deadline admission or revived refused PCM: %+v", run.name, waves)
		}
		state := captureOwnedAudio(t, run.audio)
		if len(state.Output.Waves) != 1 || state.Output.Waves[0].BudgetBytes != 2 || state.Output.Waves[0].FramePhase != 0 {
			t.Fatalf("%s retained expired charges or phase", run.name)
		}
	}
}

func TestAudioWavePhaseRepeatedCheckpointsMatchOriginalIntegerClock(t *testing.T) {
	for _, test := range []struct {
		rate     uint32
		channels uint8
	}{
		{1, 1}, {3, 1}, {44100, 1}, {999999999, 1}, {1000000000, 1}, {1000000001, 1},
		{1500000000, 1}, {^uint32(0), 1}, {44100, 2}, {^uint32(0), 32},
	} {
		t.Run(fmt.Sprintf("%d/%d", test.rate, test.channels), func(t *testing.T) {
			rate, channels := test.rate, test.channels
			samples := make([]int16, 32*int(channels))
			for i := range samples {
				samples[i] = int16(i + 1)
			}
			group := uint16(0)
			if channels == 1 {
				group = 7
			}
			now := time.Unix(1000, 0)
			sink := &wavePhaseProbe{capable: true}
			audio := NewAudioWithClock(sink, func() time.Time { return now })
			owner := loadOwnedAudio(t, audio, []smaf.Event{
				{Type: smaf.EventWave, PCMChannel: group, WaveChannels: channels, SamplingRate: rate, Wave: samples},
				{Time: 60000, Type: smaf.EventEnd},
			})
			audio.Advance(0)
			// This oracle always uses the original untrimmed source and arbitrary
			// precision; it never consumes phase or duration produced by the runtime.
			billion := big.NewInt(1000000000)
			duration := new(big.Int).Mul(big.NewInt(32), billion)
			duration.Quo(duration, new(big.Int).SetUint64(uint64(rate)))
			end := time.Duration(duration.Int64())
			ages := []time.Duration{1, end / 7, end / 3, end / 2, end - 1, end}
			slices.Sort(ages)
			ages = slices.Compact(ages)
			previous := time.Duration(0)
			for _, age := range ages {
				now = now.Add(age - previous)
				previous = age
				sink.events, sink.resumed = nil, nil
				audio.ResumeOutput()
				state := captureOwnedAudio(t, audio)
				if age >= end {
					if len(state.Output.Waves) != 0 || len(sink.waves()) != 0 {
						t.Fatalf("age %v: a restored wave outlived the original floored expiry", age)
					}
					continue
				}
				position := new(big.Int).Mul(big.NewInt(int64(age)), new(big.Int).SetUint64(uint64(rate)))
				frames, remainder := new(big.Int), new(big.Int)
				frames.QuoRem(position, billion, remainder)
				wantSamples, wantPhase := samples[frames.Int64()*int64(channels):], uint32(remainder.Uint64())
				if len(state.Output.Waves) != 1 {
					t.Fatalf("age %v: lost a surviving wave", age)
				}
				wave := state.Output.Waves[0]
				if !slices.Equal(wave.Samples, wantSamples) || wave.FramePhase != wantPhase || wave.BudgetBytes != uint32(len(samples)*2) {
					t.Fatalf("age %v: original-clock suffix/phase differs: %+v; want phase %d samples %v", age, wave, wantPhase, wantSamples)
				}
				if waves := sink.waves(); len(waves) != 1 || waves[0].sound != owner || waves[0].phase != wantPhase ||
					waves[0].event.PCMChannel != group || waves[0].event.WaveChannels != channels || waves[0].event.SamplingRate != rate || !slices.Equal(waves[0].event.Wave, wantSamples) {
					t.Fatalf("age %v: replay lost its fractional offset, raw suffix or route: %+v", age, waves)
				}
				state = wavePhaseRoundTrip(t, state)
				now = now.Add(17 * time.Second)
				var err error
				audio, err = NewAudioFromStateWithClock(state, sink, func() time.Time { return now })
				if err != nil {
					t.Fatal(err)
				}
				now = now.Add(11 * time.Second)
				audio.ActivateOutputClock()
				if got := captureOwnedAudio(t, audio); !reflect.DeepEqual(got.Output, state.Output) {
					t.Fatal("detached activation changed retained PCM position")
				}
			}
		})
	}
}

func TestAudioWavePhasePauseAndDetachedRestoreKeepFraction(t *testing.T) {
	now := time.Unix(1000, 0)
	sink := &wavePhaseProbe{capable: true}
	audio := NewAudioWithClock(sink, func() time.Time { return now })
	owner := loadOwnedAudio(t, audio, []smaf.Event{
		{Type: smaf.EventWave, PCMChannel: 7, WaveChannels: 1, SamplingRate: 4, Wave: []int16{1, 2, 3, 4, 5, 6, 7, 8}},
		{Time: 3000, Type: smaf.EventEnd},
	})
	audio.Advance(0)
	now = now.Add(375 * time.Millisecond)
	if err := audio.Pause(owner, 375*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	frozen := captureOwnedAudio(t, audio)
	now = now.Add(10 * time.Second)
	audio.Advance(10375 * time.Millisecond)
	saved := wavePhaseRoundTrip(t, captureOwnedAudio(t, audio))
	if !reflect.DeepEqual(saved.Output.Waves, frozen.Output.Waves) {
		t.Fatal("paused host time consumed PCM phase")
	}
	now = time.Unix(2000, 0)
	fresh, err := NewAudioFromStateWithClock(saved, sink, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	if err := fresh.RebasePlaybackClock(10375*time.Millisecond, 1); err != nil {
		t.Fatal(err)
	}
	now = now.Add(7 * time.Second)
	fresh.ActivateOutputClock()
	fresh.ActivateOutputClock()
	sink.events, sink.resumed = nil, nil
	fresh.ResumeOutput()
	if len(sink.waves()) != 0 {
		t.Fatal("reconnect emitted a paused wave")
	}
	now = now.Add(2 * time.Second)
	fresh.Advance(12375 * time.Millisecond)
	if err := fresh.Resume(owner, 12375*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if waves := sink.waves(); len(waves) != 1 || waves[0].phase != 500000000 || !slices.Equal(waves[0].event.Wave, []int16{2, 3, 4, 5, 6, 7, 8}) {
		t.Fatalf("resume lost the frozen fractional frame: %+v", waves)
	}
	resumedAt := now
	now = resumedAt.Add(125 * time.Millisecond)
	state := captureOwnedAudio(t, fresh)
	if len(state.Output.Waves) != 1 || state.Output.Waves[0].FramePhase != 0 || state.Output.Waves[0].BudgetBytes != 16 ||
		!slices.Equal(state.Output.Waves[0].Samples, []int16{3, 4, 5, 6, 7, 8}) {
		t.Fatal("resumed fraction did not cross the original next frame")
	}
	now = resumedAt.Add(1625 * time.Millisecond)
	if state := captureOwnedAudio(t, fresh); len(state.Output.Waves) != 0 {
		t.Fatal("paused/restored wave outlived its original remaining duration")
	}
}

func TestAudioWavePhaseReplayKeepsRawPCMAndCapabilityFallback(t *testing.T) {
	now := time.Unix(1000, 0)
	audio := NewAudioWithClock(nil, func() time.Time { return now })
	owner := loadOwnedAudio(t, audio, []smaf.Event{
		{Type: smaf.EventPCMControl, PCMChannel: 7, Control: 7, Value: 64},
		{Type: smaf.EventPCMControl, PCMChannel: 7, Control: 10, Value: 0},
		{Type: smaf.EventWave, PCMChannel: 7, WaveChannels: 1, SamplingRate: 4, Wave: []int16{10000, -10000, 8000, -8000}},
		{Time: 2000, Type: smaf.EventEnd},
	})
	audio.SetVolume(50)
	if err := audio.SetSoundVolume(owner, 50); err != nil {
		t.Fatal(err)
	}
	audio.Advance(0)
	now = now.Add(375 * time.Millisecond)
	saved := wavePhaseRoundTrip(t, captureOwnedAudio(t, audio))
	for _, test := range []struct {
		name      string
		pcm, gain bool
		want      []int16
	}{
		{"live PCM and gain", true, true, []int16{-10000, 8000, -8000}},
		{"live PCM baked gain", true, false, []int16{-2500, 2000, -2000}},
		{"stereo fallback live gain", false, true, []int16{-2539, 0, 2031, 0, -2031, 0}},
		{"stereo fallback baked gain", false, false, []int16{-634, 0, 507, 0, -507, 0}},
	} {
		t.Run(test.name, func(t *testing.T) {
			probe := &wavePhaseProbe{capable: test.pcm}
			var sink AudioSink = probe
			var liveGain *gainWavePhaseProbe
			if test.gain {
				liveGain = &gainWavePhaseProbe{wavePhaseProbe: *probe}
				probe, sink = &liveGain.wavePhaseProbe, liveGain
			}
			fresh, err := NewAudioFromStateWithClock(saved, sink, func() time.Time { return now })
			if err != nil {
				t.Fatal(err)
			}
			fresh.ActivateOutputClock()
			fresh.ResumeOutput()
			group, channels := uint16(0), uint8(2)
			if test.pcm {
				group, channels = 7, 1
			}
			waves := probe.waves()
			if len(waves) != 1 || waves[0].sound != owner || waves[0].phase != 500000000 || waves[0].event.PCMChannel != group ||
				waves[0].event.WaveChannels != channels || waves[0].event.SamplingRate != 4 || !slices.Equal(waves[0].event.Wave, test.want) {
				t.Fatalf("fractional replay changed PCM, routing or gain: %+v", waves)
			}
			if liveGain != nil && liveGain.gains[owner] != 2500 {
				t.Fatal("fractional replay lost independent owner gain")
			}
			if got := captureOwnedAudio(t, fresh); !reflect.DeepEqual(got.Output, saved.Output) ||
				got.Output.Waves[0].BudgetBytes != 8 || !slices.Equal(got.Output.Waves[0].Samples, []int16{-10000, 8000, -8000}) {
				t.Fatal("rendering fallback modified retained raw PCM or its admission charge")
			}
		})
	}
	legacy := &ownedAudioProbe{}
	audio.SetSink(legacy)
	audio.ResumeOutput()
	if waves := legacy.ofType(smaf.EventWave); len(waves) != 1 || !slices.Equal(waves[0].event.Wave, []int16{-634, 0, 507, 0, -507, 0}) {
		t.Fatalf("sink without fractional replay lost its whole-frame fallback: %+v", waves)
	}
}

func TestAudioWavePhaseExpiryBeforeWideElapsedMultiplication(t *testing.T) {
	now := time.Unix(1000, 0)
	sink := &wavePhaseProbe{}
	audio := NewAudioWithClock(sink, func() time.Time { return now })
	loadOwnedAudio(t, audio, []smaf.Event{
		{Type: smaf.EventWave, WaveChannels: 1, SamplingRate: ^uint32(0), Wave: make([]int16, 32)},
		{Time: 1, Type: smaf.EventEnd},
	})
	audio.Advance(0)
	now = now.Add(time.Duration(1<<63 - 1))
	sink.events, sink.resumed = nil, nil
	audio.ResumeOutput()
	if saved := captureOwnedAudio(t, audio); len(saved.Output.Waves) != 0 || len(sink.waves()) != 0 {
		t.Fatal("huge elapsed time wrapped an expired wave into a surviving suffix")
	}
}
