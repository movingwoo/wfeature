package backend

import (
	"fmt"
	"slices"
	"time"
)

// AudioOutputState is the portable output represented by the page's MIDI
// synthesizer and PCM player. Oscillator phase, release tails and network/device
// latency are not execution state. Notes restart their envelopes on restoration;
// PCM resumes at a whole sample frame measured on the unscaled Host clock.
type AudioOutputState struct {
	Channels [16]AudioChannelState
	Notes    []AudioVoiceState
	Waves    []AudioWaveState
}

type AudioChannelState struct {
	Program, Volume, Expression, Pan, Sustain uint8
	Bend                                      uint16
}

type AudioVoiceState struct {
	Channel, Note, Velocity uint8
	// A note keeps the level, timbre and pan it began with. Later channel
	// changes affect future notes; pitch bend also affects sounding notes.
	StartedWith AudioChannelState
	DrumLeft    time.Duration
}

type AudioWaveState struct {
	Channels uint8
	Rate     uint32
	Samples  []int16
}

type audioOutputNote struct {
	AudioVoiceState
	ends time.Time
}

type audioOutputWave struct {
	AudioWaveState
	started time.Time
}

// These bounds govern bookkeeping, not ordinary playback. If an unusually
// large output cannot be represented, capture refuses while it is outstanding.
const (
	maxOutputNotes = 24 // Matches the page synthesizer's voice budget.
	maxOutputWaves = 256
	maxOutputBytes = 32 << 20
	drumDuration   = 200 * time.Millisecond
)

type audioOutput struct {
	sink       AudioSink
	now        func() time.Time
	channels   [16]AudioChannelState
	notes      []audioOutputNote
	waves      []audioOutputWave
	waveBytes  int
	missingPCM time.Time
	invalid    bool
	restoredAt time.Time
	detached   bool
}

func newAudioOutput(sink AudioSink, now func() time.Time) *audioOutput {
	if now == nil {
		now = time.Now
	}
	output := &audioOutput{sink: sink, now: now}
	for i := range output.channels {
		output.channels[i] = AudioChannelState{Volume: 100, Expression: 127, Pan: 64, Bend: 8192}
	}
	return output
}

func (output *audioOutput) removeNote(channel, note uint8) {
	output.notes = slices.DeleteFunc(output.notes, func(current audioOutputNote) bool {
		return current.Channel == channel && current.Note == note
	})
}

func (output *audioOutput) MIDINoteOn(channel, note, velocity uint8) {
	// Web Audio retires percussion independently of the guest timeline.
	if len(output.notes) != 0 {
		now := output.now()
		output.notes = slices.DeleteFunc(output.notes, func(note audioOutputNote) bool {
			return note.Channel == 9 && !note.ends.After(now)
		})
	}
	if channel >= 16 || note >= 128 || velocity >= 128 {
		output.invalid = true
	} else if velocity == 0 {
		output.removeNote(channel, note)
	} else {
		// The page steals before replacing a retriggered key too.
		if len(output.notes) == maxOutputNotes {
			output.notes = slices.Delete(output.notes, 0, 1)
		}
		output.removeNote(channel, note)
		current := audioOutputNote{AudioVoiceState: AudioVoiceState{Channel: channel, Note: note, Velocity: velocity, StartedWith: output.channels[channel]}}
		if channel == 9 {
			current.ends = output.now().Add(drumDuration)
		}
		output.notes = append(output.notes, current)
	}
	if output.sink != nil {
		output.sink.MIDINoteOn(channel, note, velocity)
	}
}

func (output *audioOutput) MIDINoteOff(channel, note, velocity uint8) {
	output.removeNote(channel, note)
	if output.sink != nil {
		output.sink.MIDINoteOff(channel, note, velocity)
	}
}

func (output *audioOutput) MIDIProgramChange(channel, program uint8) {
	if channel >= 16 || program >= 128 {
		output.invalid = true
	} else {
		output.channels[channel].Program = program
	}
	if output.sink != nil {
		output.sink.MIDIProgramChange(channel, program)
	}
}

func (output *audioOutput) MIDIControlChange(channel, control, value uint8) {
	if channel >= 16 || control >= 128 || value >= 128 {
		output.invalid = true
	} else {
		state := &output.channels[channel]
		switch control {
		case 7:
			state.Volume = value
		case 10:
			state.Pan = value
		case 11:
			state.Expression = value
		case 64:
			state.Sustain = value
		case 120, 123:
			output.notes = slices.DeleteFunc(output.notes, func(note audioOutputNote) bool { return note.Channel == channel })
		}
	}
	if output.sink != nil {
		output.sink.MIDIControlChange(channel, control, value)
	}
}

func (output *audioOutput) MIDIPitchBend(channel uint8, value uint16) {
	if channel >= 16 || value >= 16384 {
		output.invalid = true
	} else {
		output.channels[channel].Bend = value
	}
	if output.sink != nil {
		output.sink.MIDIPitchBend(channel, value)
	}
}

func (output *audioOutput) MIDISysEx(data []byte) {
	// The page does not interpret device-specific SysEx. The timeline retains
	// these events for future delivery; replaying past messages is not a seek.
	if output.sink != nil {
		output.sink.MIDISysEx(data)
	}
}

func waveDuration(wave AudioWaveState) time.Duration {
	if wave.Channels == 0 || wave.Rate == 0 {
		return 0
	}
	frames := int64(len(wave.Samples) / int(wave.Channels))
	seconds, remainder := frames/int64(wave.Rate), frames%int64(wave.Rate)
	if seconds > int64((1<<63-1)/time.Second)-1 {
		return time.Duration(1<<63 - 1)
	}
	return time.Duration(seconds)*time.Second + time.Duration(remainder)*time.Second/time.Duration(wave.Rate)
}

func (output *audioOutput) pruneWaves(now time.Time) {
	output.waves = slices.DeleteFunc(output.waves, func(wave audioOutputWave) bool {
		if now.Sub(wave.started) >= waveDuration(wave.AudioWaveState) {
			output.waveBytes -= len(wave.Samples) * 2
			return true
		}
		return false
	})
}

func (output *audioOutput) PlayWave(channels uint8, rate uint32, samples []int16) {
	if len(samples) != 0 {
		now := output.now()
		output.pruneWaves(now)
		wave := AudioWaveState{Channels: channels, Rate: rate, Samples: samples}
		if channels == 0 || channels > 32 || rate == 0 || len(samples)%int(channels) != 0 {
			output.invalid = true
		} else if len(output.waves) >= maxOutputWaves || len(samples) > (maxOutputBytes-output.waveBytes)/2 {
			if ends := now.Add(waveDuration(wave)); ends.After(output.missingPCM) {
				output.missingPCM = ends
			}
		} else {
			// Audio owns the immutable loaded samples and any volume-scaled copy.
			output.waves = append(output.waves, audioOutputWave{AudioWaveState: wave, started: now})
			output.waveBytes += len(samples) * 2
		}
	}
	if output.sink != nil {
		output.sink.PlayWave(channels, rate, samples)
	}
}

func (output *audioOutput) capture(now time.Time) (AudioOutputState, error) {
	if output.invalid || now.Before(output.missingPCM) {
		return AudioOutputState{}, fmt.Errorf("audio output cannot be represented within checkpoint limits")
	}
	saved := AudioOutputState{Channels: output.channels}
	for _, current := range output.notes {
		record := current.AudioVoiceState
		if record.Channel == 9 {
			record.DrumLeft = current.ends.Sub(now)
			if record.DrumLeft <= 0 {
				continue
			}
		}
		saved.Notes = append(saved.Notes, record)
	}
	for _, wave := range output.waves {
		age := now.Sub(wave.started)
		if age < 0 {
			return AudioOutputState{}, fmt.Errorf("audio output clock moved backward")
		}
		if age >= waveDuration(wave.AudioWaveState) {
			continue
		}
		// Split seconds and their fraction to avoid multiplying a full duration
		// by the sampling rate. Remaining duration bounds the result by len.
		frames := int64(age/time.Second)*int64(wave.Rate) + int64(age%time.Second)*int64(wave.Rate)/int64(time.Second)
		saved.Waves = append(saved.Waves, AudioWaveState{Channels: wave.Channels, Rate: wave.Rate, Samples: slices.Clone(wave.Samples[int(frames)*int(wave.Channels):])})
	}
	if err := saved.validate(); err != nil {
		return AudioOutputState{}, err
	}
	return saved, nil
}

func (state AudioChannelState) valid() bool {
	return state.Program < 128 && state.Volume < 128 && state.Expression < 128 && state.Pan < 128 && state.Sustain < 128 && state.Bend < 16384
}

func (saved AudioOutputState) validate() error {
	invalid := func() error { return fmt.Errorf("audio output checkpoint has invalid channels, voices or samples") }
	if len(saved.Notes) > maxOutputNotes || len(saved.Waves) > maxOutputWaves {
		return invalid()
	}
	for _, state := range saved.Channels {
		if !state.valid() {
			return invalid()
		}
	}
	seen := [16][128]bool{}
	for _, note := range saved.Notes {
		if note.Channel >= 16 || note.Note >= 128 || note.Velocity == 0 || note.Velocity >= 128 || !note.StartedWith.valid() ||
			note.DrumLeft < 0 || note.DrumLeft > drumDuration || (note.Channel == 9) != (note.DrumLeft > 0) || seen[note.Channel][note.Note] {
			return invalid()
		}
		seen[note.Channel][note.Note] = true
	}
	bytes := 0
	for _, wave := range saved.Waves {
		if wave.Channels == 0 || wave.Channels > 32 || wave.Rate == 0 || len(wave.Samples) == 0 || len(wave.Samples)%int(wave.Channels) != 0 || len(wave.Samples) > (maxOutputBytes-bytes)/2 {
			return invalid()
		}
		bytes += len(wave.Samples) * 2
	}
	return nil
}

func (output *audioOutput) restore(saved AudioOutputState) {
	now := output.now()
	output.restoredAt, output.detached = now, true
	output.channels = saved.Channels
	for _, note := range saved.Notes {
		current := audioOutputNote{AudioVoiceState: note}
		if note.Channel == 9 {
			current.ends = now.Add(note.DrumLeft)
		}
		output.notes = append(output.notes, current)
	}
	for _, wave := range saved.Waves {
		wave.Samples = slices.Clone(wave.Samples)
		output.waves = append(output.waves, audioOutputWave{AudioWaveState: wave, started: now})
		output.waveBytes += len(wave.Samples) * 2
	}
}

// ActivateOutputClock excludes detached validation/commit time from saved PCM
// and percussion positions. It emits nothing and is idempotent.
func (audio *Audio) ActivateOutputClock() {
	if audio == nil {
		return
	}
	audio.mutex.Lock()
	defer audio.mutex.Unlock()
	output := audio.sink
	if !output.detached {
		return
	}
	from, to := output.restoredAt, output.now()
	for i := range output.waves {
		output.waves[i].started = to.Add(output.waves[i].started.Sub(from))
	}
	for i := range output.notes {
		if output.notes[i].Channel == 9 {
			output.notes[i].ends = to.Add(output.notes[i].ends.Sub(from))
		}
	}
	if !output.missingPCM.IsZero() {
		output.missingPCM = to.Add(output.missingPCM.Sub(from))
	}
	output.detached = false
}

func replayAudioChannel(sink AudioSink, channel uint8, state AudioChannelState) {
	sink.MIDIProgramChange(channel, state.Program)
	sink.MIDIControlChange(channel, 7, state.Volume)
	sink.MIDIControlChange(channel, 10, state.Pan)
	sink.MIDIControlChange(channel, 11, state.Expression)
	sink.MIDIControlChange(channel, 64, state.Sustain)
	sink.MIDIPitchBend(channel, state.Bend)
}

// ResumeOutput reconstructs sounding notes and sample tails after the Host
// clears its old output queues/device. It neither advances guest playback nor
// records the reconstruction as new events. The Host calls it once per adoption.
func (audio *Audio) ResumeOutput() {
	if audio == nil {
		return
	}
	audio.mutex.Lock()
	defer audio.mutex.Unlock()
	output := audio.sink
	if output.sink == nil {
		return
	}
	now := output.now()
	for _, note := range output.notes {
		if note.Channel == 9 && !note.ends.After(now) {
			continue
		}
		replayAudioChannel(output.sink, note.Channel, note.StartedWith)
		output.sink.MIDINoteOn(note.Channel, note.Note, note.Velocity)
	}
	for channel, state := range output.channels {
		replayAudioChannel(output.sink, uint8(channel), state)
	}
	// Restored buffers already start at the captured whole sample frame.
	for _, wave := range output.waves {
		output.sink.PlayWave(wave.Channels, wave.Rate, wave.Samples)
	}
}
