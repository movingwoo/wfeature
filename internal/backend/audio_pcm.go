package backend

import (
	"cmp"
	"math"
	"slices"

	"github.com/movingwoo/wfeature/internal/audio/smaf"
)

type audioPCMKey struct {
	sound   AudioHandle
	channel uint16
}

func validPCMControl(control, value uint8) bool {
	return (control == 7 || control == 11 || control == 10) && value < 128
}

func (output *audioOutput) pcmChannel(sound AudioHandle, channel uint16) *AudioPCMChannelState {
	if channel == 0 {
		return nil
	}
	key := audioPCMKey{sound, channel}
	if state := output.pcmChannels[key]; state != nil {
		return state
	}
	if len(output.pcmChannels) >= maxPCMChannels {
		output.invalid = true
		return nil
	}
	// Initial volume is unspecified. Neutral gain preserves prior playback
	// until an explicit volume arrives; expression and pan are specified.
	state := &AudioPCMChannelState{Sound: sound, Channel: channel, Volume: 127, Expression: 127, Pan: 64}
	output.pcmChannels[key] = state
	return state
}

func (output *audioOutput) PCMControl(channel uint16, control, value uint8) {
	if channel == 0 || !validPCMControl(control, value) {
		output.invalid = true
		return
	}
	output.currentChannels()
	state := output.pcmChannel(output.sound, channel)
	if state == nil {
		return
	}
	switch control {
	case 7:
		state.Volume, state.VolumeSet = value, true
	case 11:
		state.Expression = value
	case 10:
		state.Pan = value
	}
	if output.livePCM() {
		emitAudioEvent(output.sink, output.sound, smaf.Event{Type: smaf.EventPCMControl, PCMChannel: channel, Control: control, Value: value})
	}
}

func (output *audioOutput) livePCM() bool {
	_, owned := output.sink.(OwnedAudioSink)
	capability, ok := output.sink.(AudioPCMSink)
	return owned && ok && capability.PCMChannels()
}

func (output *audioOutput) capturePCM() []AudioPCMChannelState {
	var states []AudioPCMChannelState
	for _, state := range output.pcmChannels {
		states = append(states, *state)
	}
	slices.SortFunc(states, func(a, b AudioPCMChannelState) int {
		if order := cmp.Compare(a.Sound, b.Sound); order != 0 {
			return order
		}
		return cmp.Compare(a.Channel, b.Channel)
	})
	return states
}

// emitWave keeps retained PCM raw. Older sinks receive an onset-only stereo
// rendering; they cannot change a sample after emission. Scaling this copy
// leaves replay, later gain changes and capable reconnects independent.
func (output *audioOutput) emitWave(sound AudioHandle, event smaf.Event, framePhase uint32) {
	if output.sink == nil {
		return
	}
	if event.PCMChannel != 0 && !output.livePCM() {
		state := output.pcmChannel(sound, event.PCMChannel)
		if state == nil {
			return
		}
		volume, expression := float64(state.Volume)/127, float64(state.Expression)/127
		gain := volume * volume * expression * expression
		angle := math.Pi * float64(state.Pan) / 254
		left, right := gain*math.Cos(angle), gain*math.Sin(angle)
		samples := make([]int16, len(event.Wave)*2)
		for i, sample := range event.Wave {
			samples[2*i], samples[2*i+1] = int16(float64(sample)*left), int16(float64(sample)*right)
		}
		event.PCMChannel, event.WaveChannels, event.Wave = 0, 2, samples
	}
	output.destination(sound).resumeWave(event, framePhase)
}

func (output *audioOutput) replayPCM(selected func(AudioHandle) bool) {
	if !output.livePCM() {
		return
	}
	for _, state := range output.capturePCM() {
		if !selected(state.Sound) {
			continue
		}
		if state.VolumeSet {
			emitAudioEvent(output.sink, state.Sound, smaf.Event{Type: smaf.EventPCMControl, PCMChannel: state.Channel, Control: 7, Value: state.Volume})
		}
		emitAudioEvent(output.sink, state.Sound, smaf.Event{Type: smaf.EventPCMControl, PCMChannel: state.Channel, Control: 11, Value: state.Expression})
		emitAudioEvent(output.sink, state.Sound, smaf.Event{Type: smaf.EventPCMControl, PCMChannel: state.Channel, Control: 10, Value: state.Pan})
	}
}
