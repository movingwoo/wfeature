package backend

import "fmt"

// AudioGainUnity is unattenuated output. Device and clip percentages multiply
// on this scale without rounding either one to a MIDI velocity first.
const AudioGainUnity uint16 = 10000

// AudioGainSink applies a clip's gain to all its output, including held notes,
// PCM, percussion and release tails. Its input events contain unscaled samples
// and velocities. The user's Host volume controls remain independent.
type AudioGainSink interface {
	OwnedAudioSink
	SoundGain(sound AudioHandle, gain uint16)
}

func (audio *Audio) soundGain(current *sound) uint16 {
	if current.muted {
		return 0
	}
	return uint16(audio.volume * current.volume)
}

func (audio *Audio) updateGain(current *sound) {
	// A stopped or not-yet-played sound has no output. PCM tails still have an
	// owner after natural completion and receive changes through this path.
	if _, active := audio.sink.soundChannels[current.handle]; active {
		audio.sink.setGain(current.handle, audio.soundGain(current))
	}
}

// SetSoundVolume sets one clip's level, clamped to 0..100.
func (audio *Audio) SetSoundVolume(handle AudioHandle, percent int) error {
	if audio == nil {
		return fmt.Errorf("audio is not configured")
	}
	audio.mutex.Lock()
	defer audio.mutex.Unlock()
	current, ok := audio.sounds[handle]
	if !ok {
		return fmt.Errorf("audio handle %d is not loaded", handle)
	}
	current.volume = min(max(percent, 0), maxAudioVolume)
	audio.updateGain(current)
	return nil
}

// SoundVolume reports a clip's own level and mute flag, before device gain.
// Mute preserves the reported level so unmuting restores that level.
func (audio *Audio) SoundVolume(handle AudioHandle) (percent int, muted bool, err error) {
	if audio == nil {
		return 0, false, fmt.Errorf("audio is not configured")
	}
	audio.mutex.Lock()
	defer audio.mutex.Unlock()
	current, ok := audio.sounds[handle]
	if !ok {
		return 0, false, fmt.Errorf("audio handle %d is not loaded", handle)
	}
	return current.volume, current.muted, nil
}

// SetSoundMuted changes a clip's output without changing its saved level or
// advancing, stopping or restarting its timeline.
func (audio *Audio) SetSoundMuted(handle AudioHandle, muted bool) error {
	if audio == nil {
		return fmt.Errorf("audio is not configured")
	}
	audio.mutex.Lock()
	defer audio.mutex.Unlock()
	current, ok := audio.sounds[handle]
	if !ok {
		return fmt.Errorf("audio handle %d is not loaded", handle)
	}
	current.muted = muted
	audio.updateGain(current)
	return nil
}

func (output *audioOutput) gain(sound AudioHandle) uint16 {
	if gain, ok := output.gains[sound]; ok {
		return gain
	}
	return AudioGainUnity
}

func (output *audioOutput) setGain(sound AudioHandle, gain uint16) {
	output.gains[sound] = gain
	output.sound = sound
	output.currentChannels()
	if sink, ok := output.sink.(AudioGainSink); ok {
		sink.SoundGain(sound, gain)
	} else if gain == 0 && output.sink != nil {
		// Legacy diagnostic sinks can scale future events and release held
		// notes, but cannot adjust already emitted PCM or revive a held note.
		for _, note := range output.notes {
			if note.Sound == sound {
				output.destination(sound).MIDINoteOff(note.Channel, note.Note, 0)
			}
		}
	}
}

func scaleAudioSamples(samples []int16, gain uint16) []int16 {
	if gain == AudioGainUnity || len(samples) == 0 {
		return samples
	}
	scaled := make([]int16, len(samples))
	for i, sample := range samples {
		scaled[i] = int16(int(sample) * int(gain) / int(AudioGainUnity))
	}
	return scaled
}
