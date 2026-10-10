package backend

import (
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"time"

	"github.com/movingwoo/wfeature/internal/audio/smaf"
)

// AudioOutputState is the portable output represented by the page's MIDI
// synthesizer and PCM player. Oscillator phase, release tails and network/device
// latency are not execution state. Notes retain their envelope age and PCM
// resumes at a whole sample frame on the presentation clock. Unscaled Host
// time continues aging output between service boundaries and supplies a
// monotonic floor when guest execution is slower than its selected rate.
type AudioOutputState struct {
	Channels    [16]AudioChannelState
	Sounds      []AudioSoundOutputState
	Notes       []AudioVoiceState
	Waves       []AudioWaveState
	PCMChannels []AudioPCMChannelState
}

type AudioSoundOutputState struct {
	Sound    AudioHandle
	Gain     uint16
	Paused   bool
	Channels [16]AudioChannelState
}

type AudioChannelState struct {
	Program, Volume, Expression, Pan, Sustain uint8
	Bend                                      uint16
	BendSemitones, BendCents                  uint8
	RPNMSB, RPNLSB, NRPNMSB, NRPNLSB          uint8
	NRPN                                      bool
}

type AudioVoiceState struct {
	Sound                   AudioHandle
	Channel, Note, Velocity uint8
	// Retain the original channel metadata for the note's program. Replay
	// combines that timbre with the owner's current live controllers; its
	// velocity and envelope age remain independent of channel gain and pan.
	StartedWith AudioChannelState
	DrumLeft    time.Duration
	Age         time.Duration
	// Released notes still sound because the owner's sustain pedal is down.
	Released bool
}

type AudioWaveState struct {
	Sound      AudioHandle
	PCMChannel uint16
	Channels   uint8
	Rate       uint32
	Samples    []int16
	// BudgetBytes retains the original admission charge when capture saves
	// only a sample suffix. Reconstructing output must not grant extra capacity.
	BudgetBytes uint32
	// FramePhase is the consumed fraction of the first remaining frame, in
	// billionths of a frame. It survives trimming without nanosecond rounding.
	FramePhase uint32
}

// AudioPCMChannelState retains the independent controls for one ATR channel.
// An unset volume uses neutral gain as compatibility policy, not a claimed
// handset default. Expression and pan have documented defaults of 127 and 64.
type AudioPCMChannelState struct {
	Sound                   AudioHandle
	Channel                 uint16
	Volume, Expression, Pan uint8
	VolumeSet               bool
}

type audioOutputNote struct {
	AudioVoiceState
	ends    time.Time
	started time.Time
}

type audioOutputWave struct {
	AudioWaveState
	started time.Time
}

// PCM admission and retained output share the same bounds. Refuse a newest
// wave that cannot be retained, so recovery cannot silently lose audible PCM.
const (
	maxOutputNotes = 24 // Matches the page synthesizer's voice budget.
	// Each paused owner retains at most one active voice budget. Paused
	// voices consume no page slots; resuming applies the active budget again.
	maxRetainedOutputNotes = maxOutputNotes * (defaultMaxSounds + 1)
	maxOutputWaves         = 256
	maxOutputBytes         = 32 << 20
	drumDuration           = 200 * time.Millisecond
)

type audioOutput struct {
	sink     AudioSink
	now      func() time.Time
	eventAt  *time.Time // Authored deadline within the current presentation batch.
	clockAt  time.Time
	hostAt   time.Time
	clockSet bool
	channels [16]AudioChannelState
	// sound selects the current callback owner under Audio.mutex.
	sound         AudioHandle
	soundChannels map[AudioHandle]*[16]AudioChannelState
	pcmChannels   map[audioPCMKey]*AudioPCMChannelState
	gains         map[AudioHandle]uint16
	paused        map[AudioHandle]time.Time
	notes         []audioOutputNote
	waves         []audioOutputWave
	waveBytes     int
	invalid       bool
	logger        *slog.Logger
	pcmRefusals   uint64
	restoredAt    time.Time
	detached      bool
}

func newAudioOutput(sink AudioSink, now func() time.Time) *audioOutput {
	if now == nil {
		now = time.Now
	}
	output := &audioOutput{sink: sink, now: now, soundChannels: make(map[AudioHandle]*[16]AudioChannelState), gains: make(map[AudioHandle]uint16), paused: make(map[AudioHandle]time.Time)}
	output.pcmChannels = make(map[audioPCMKey]*AudioPCMChannelState)
	output.channels = defaultAudioChannels()
	return output
}

func (output *audioOutput) removeNote(channel, note uint8) {
	output.notes = slices.DeleteFunc(output.notes, func(current audioOutputNote) bool {
		return current.Sound == output.sound && current.Channel == channel && current.Note == note
	})
}

func (output *audioOutput) appendNote(note audioOutputNote) {
	active, oldest := 0, -1
	for i, current := range output.notes {
		if _, paused := output.paused[current.Sound]; !paused {
			active++
			if oldest == -1 {
				oldest = i
			}
		}
	}
	// Stealing precedes replacement of a retriggered key, as on the page.
	if active == maxOutputNotes {
		output.notes = slices.Delete(output.notes, oldest, oldest+1)
	}
	output.notes = slices.DeleteFunc(output.notes, func(previous audioOutputNote) bool {
		return previous.Sound == note.Sound && previous.Channel == note.Channel && previous.Note == note.Note
	})
	output.notes = append(output.notes, note)
}

func (output *audioOutput) pruneNotes(now time.Time) {
	// Web Audio retires percussion independently of the guest timeline.
	output.notes = slices.DeleteFunc(output.notes, func(note audioOutputNote) bool {
		return note.Channel == 9 && !note.ends.After(output.ownerTime(note.Sound, now))
	})
}

func (output *audioOutput) MIDINoteOn(channel, note, velocity uint8) {
	now := output.eventTime()
	output.pruneNotes(now)
	if channel >= 16 || note >= 128 || velocity >= 128 {
		output.invalid = true
	} else if velocity == 0 {
		output.releaseNote(channel, note)
	} else {
		current := audioOutputNote{AudioVoiceState: AudioVoiceState{Channel: channel, Note: note, Velocity: velocity, Sound: output.sound, StartedWith: output.currentChannels()[channel]}}
		current.started = now
		if channel == 9 {
			current.ends = current.started.Add(drumDuration)
		}
		output.appendNote(current)
	}
	if output.sink != nil {
		output.destination(output.sound).MIDINoteOn(channel, note, velocity)
	}
}

func (output *audioOutput) MIDINoteOff(channel, note, velocity uint8) {
	if channel >= 16 || note >= 128 || velocity >= 128 {
		output.invalid = true
	} else {
		output.releaseNote(channel, note)
	}
	if output.sink != nil {
		output.destination(output.sound).MIDINoteOff(channel, note, velocity)
	}
}

func (output *audioOutput) MIDIProgramChange(channel, program uint8) {
	if channel >= 16 || program >= 128 {
		output.invalid = true
	} else {
		output.currentChannels()[channel].Program = program
	}
	if output.sink != nil {
		output.destination(output.sound).MIDIProgramChange(channel, program)
	}
}

func (output *audioOutput) MIDIControlChange(channel, control, value uint8) {
	if channel >= 16 || control >= 128 || value >= 128 {
		output.invalid = true
	} else {
		state := &output.currentChannels()[channel]
		state.controlChange(control, value)
		switch control {
		case 64:
			if value < 64 {
				output.releaseSustained(channel)
			}
		case 121:
			output.releaseSustained(channel)
		case 120:
			output.stopChannel(channel, true)
		case 123:
			output.stopChannel(channel, false)
		}
	}
	if output.sink != nil {
		output.destination(output.sound).MIDIControlChange(channel, control, value)
	}
}

func (output *audioOutput) MIDIPitchBend(channel uint8, value uint16) {
	if channel >= 16 || value >= 16384 {
		output.invalid = true
	} else {
		output.currentChannels()[channel].Bend = value
	}
	if output.sink != nil {
		output.destination(output.sound).MIDIPitchBend(channel, value)
	}
}

func (output *audioOutput) MIDISysEx(data []byte) {
	// The page does not interpret device-specific SysEx. The timeline retains
	// these events for future delivery; replaying past messages is not a seek.
	if output.sink != nil {
		output.destination(output.sound).MIDISysEx(data)
	}
}

func waveDuration(wave AudioWaveState) time.Duration {
	if wave.Channels == 0 || wave.Rate == 0 {
		return 0
	}
	frames := int64(len(wave.Samples) / int(wave.Channels))
	// Retained waves have at most maxOutputBytes/2 frames. The numerator
	// therefore fits int64, even at a one-Hz or maximum uint32 sample rate.
	return time.Duration((frames*int64(time.Second) - int64(wave.FramePhase)) / int64(wave.Rate))
}

// wavePosition keeps the fractional frame in integer units across repeated
// captures. The expiry check bounds age*rate by the admitted sample count.
func wavePosition(wave AudioWaveState, age time.Duration) (int, uint32, bool) {
	if age < 0 || age >= waveDuration(wave) {
		return 0, 0, false
	}
	position := uint64(wave.FramePhase) + uint64(age)*uint64(wave.Rate)
	return int(position / uint64(time.Second)), uint32(position % uint64(time.Second)), true
}

func (output *audioOutput) pruneWaves(now time.Time) {
	output.waves = slices.DeleteFunc(output.waves, func(wave audioOutputWave) bool {
		if output.ownerTime(wave.Sound, now).Sub(wave.started) >= waveDuration(wave.AudioWaveState) {
			output.waveBytes -= int(wave.BudgetBytes)
			return true
		}
		return false
	})
}

func (output *audioOutput) PlayWave(channels uint8, rate uint32, samples []int16) {
	output.playWave(smaf.Event{Type: smaf.EventWave, WaveChannels: channels, SamplingRate: rate, Wave: samples})
}

func (output *audioOutput) playWave(event smaf.Event) {
	channels, rate, samples := event.WaveChannels, event.SamplingRate, event.Wave
	output.currentChannels()
	if event.PCMChannel != 0 && (channels != 1 || output.pcmChannel(output.sound, event.PCMChannel) == nil) {
		output.invalid = true
		return
	}
	if len(samples) != 0 {
		now := output.eventTime()
		output.pruneWaves(now)
		wave := AudioWaveState{Sound: output.sound, PCMChannel: event.PCMChannel, Channels: channels, Rate: rate, Samples: samples}
		if channels == 0 || channels > 32 || rate == 0 || len(samples)%int(channels) != 0 {
			output.invalid = true
			return
		}
		if len(output.waves) >= maxOutputWaves {
			output.refuseWave("wave limit", len(samples))
			return
		}
		if len(samples) > (maxOutputBytes-output.waveBytes)/2 {
			output.refuseWave("byte limit", len(samples))
			return
		}
		// Audio owns the immutable loaded samples. Retain the onset charge
		// until this source ends or stops, including while its owner is paused.
		wave.BudgetBytes = uint32(len(samples) * 2)
		output.waves = append(output.waves, audioOutputWave{AudioWaveState: wave, started: now})
		output.waveBytes += int(wave.BudgetBytes)
	}
	output.emitWave(output.sound, event, 0)
}

func (output *audioOutput) capture(now time.Time) (AudioOutputState, error) {
	if output.invalid {
		return AudioOutputState{}, fmt.Errorf("audio output cannot be represented within checkpoint limits")
	}
	saved := AudioOutputState{Channels: output.channels, PCMChannels: output.capturePCM()}
	for _, sound := range slices.Sorted(maps.Keys(output.soundChannels)) {
		_, paused := output.paused[sound]
		saved.Sounds = append(saved.Sounds, AudioSoundOutputState{Sound: sound, Gain: output.gain(sound), Paused: paused, Channels: *output.soundChannels[sound]})
	}
	for _, current := range output.notes {
		record := current.AudioVoiceState
		instant := output.ownerTime(record.Sound, now)
		record.Age = instant.Sub(current.started)
		if record.Channel == 9 {
			record.DrumLeft = current.ends.Sub(instant)
			if record.DrumLeft <= 0 {
				continue
			}
		}
		saved.Notes = append(saved.Notes, record)
	}
	for _, wave := range output.waves {
		age := output.ownerTime(wave.Sound, now).Sub(wave.started)
		if age < 0 {
			return AudioOutputState{}, fmt.Errorf("audio output clock moved backward")
		}
		frames, phase, alive := wavePosition(wave.AudioWaveState, age)
		if !alive {
			continue
		}
		saved.Waves = append(saved.Waves, AudioWaveState{Sound: wave.Sound, PCMChannel: wave.PCMChannel, Channels: wave.Channels, Rate: wave.Rate, BudgetBytes: wave.BudgetBytes, FramePhase: phase, Samples: slices.Clone(wave.Samples[frames*int(wave.Channels):])})
	}
	if err := saved.validate(audioStateVersion); err != nil {
		return AudioOutputState{}, err
	}
	return saved, nil
}

func (state AudioChannelState) valid() bool {
	return state.Program < 128 && state.Volume < 128 && state.Expression < 128 && state.Pan < 128 && state.Sustain < 128 && state.Bend < 16384 &&
		state.BendSemitones < 128 && state.BendCents < 100 && state.RPNMSB < 128 && state.RPNLSB < 128 && state.NRPNMSB < 128 && state.NRPNLSB < 128
}

func (saved AudioOutputState) validate(version uint32) error {
	invalid := func() error { return fmt.Errorf("audio output checkpoint has invalid channels, voices or samples") }
	if len(saved.Notes) > maxRetainedOutputNotes || len(saved.Waves) > maxOutputWaves || len(saved.Sounds) > defaultMaxSounds || len(saved.PCMChannels) > maxPCMChannels {
		return invalid()
	}
	for _, state := range saved.Channels {
		if !state.valid() {
			return invalid()
		}
	}
	owners := map[AudioHandle]bool{0: true}
	channels := map[AudioHandle][16]AudioChannelState{0: saved.Channels}
	paused := make(map[AudioHandle]bool)
	for i, sound := range saved.Sounds {
		if sound.Sound == 0 || sound.Gain > AudioGainUnity || owners[sound.Sound] || i > 0 && saved.Sounds[i-1].Sound >= sound.Sound {
			return invalid()
		}
		owners[sound.Sound] = true
		channels[sound.Sound] = sound.Channels
		paused[sound.Sound] = sound.Paused
		for _, state := range sound.Channels {
			if !state.valid() {
				return invalid()
			}
		}
	}
	groups := make(map[audioPCMKey]bool)
	for i, state := range saved.PCMChannels {
		if !owners[state.Sound] || state.Channel == 0 || state.Volume > 127 || state.Expression > 127 || state.Pan > 127 || !state.VolumeSet && state.Volume != 127 ||
			i > 0 && (saved.PCMChannels[i-1].Sound > state.Sound || saved.PCMChannels[i-1].Sound == state.Sound && saved.PCMChannels[i-1].Channel >= state.Channel) {
			return invalid()
		}
		groups[audioPCMKey{state.Sound, state.Channel}] = true
	}
	seen := map[[3]uint32]bool{}
	counts := make(map[AudioHandle]int)
	for _, note := range saved.Notes {
		key := [3]uint32{uint32(note.Sound), uint32(note.Channel), uint32(note.Note)}
		if !owners[note.Sound] || note.Channel >= 16 || note.Note >= 128 || note.Velocity == 0 || note.Velocity >= 128 || !note.StartedWith.valid() ||
			note.Age < 0 || note.DrumLeft < 0 || note.DrumLeft > drumDuration || (note.Channel == 9) != (note.DrumLeft > 0) || note.Channel == 9 && note.Age != drumDuration-note.DrumLeft || seen[key] {
			return invalid()
		}
		if note.Released && (note.Channel == 9 || channels[note.Sound][note.Channel].Sustain < 64) {
			return invalid()
		}
		owner := note.Sound
		if !paused[owner] {
			owner = 0 // All audible owners share the page's one voice budget.
		}
		counts[owner]++
		if counts[owner] > maxOutputNotes {
			return invalid()
		}
		seen[key] = true
	}
	bytes := 0
	for _, wave := range saved.Waves {
		if wave.PCMChannel != 0 && (wave.Channels != 1 || !groups[audioPCMKey{wave.Sound, wave.PCMChannel}]) {
			return invalid()
		}
		if !owners[wave.Sound] || wave.Channels == 0 || wave.Channels > 32 || wave.Rate == 0 || len(wave.Samples) == 0 || len(wave.Samples)%int(wave.Channels) != 0 {
			return invalid()
		}
		if wave.FramePhase >= uint32(time.Second) || version < 9 && wave.FramePhase != 0 {
			return invalid()
		}
		charge := wave.BudgetBytes
		if version == 6 || version == 7 {
			if charge != 0 || len(wave.Samples) > maxOutputBytes/2 {
				return invalid()
			}
			charge = uint32(len(wave.Samples) * 2)
		}
		if charge%uint32(2*wave.Channels) != 0 || charge > uint32(maxOutputBytes-bytes) || len(wave.Samples) > int(charge/2) {
			return invalid()
		}
		bytes += int(charge)
	}
	return nil
}

func (output *audioOutput) restore(saved AudioOutputState) {
	now := output.currentTime()
	output.restoredAt, output.detached = now, true
	output.channels = saved.Channels
	for _, state := range saved.PCMChannels {
		state := state
		output.pcmChannels[audioPCMKey{state.Sound, state.Channel}] = &state
	}
	for _, sound := range saved.Sounds {
		channels := sound.Channels
		output.soundChannels[sound.Sound] = &channels
		output.gains[sound.Sound] = sound.Gain
		if sound.Paused {
			output.paused[sound.Sound] = now
		}
	}
	for _, note := range saved.Notes {
		current := audioOutputNote{AudioVoiceState: note, started: now.Add(-note.Age)}
		if note.Channel == 9 {
			current.ends = now.Add(note.DrumLeft)
		}
		output.notes = append(output.notes, current)
	}
	for _, wave := range saved.Waves {
		wave.Samples = slices.Clone(wave.Samples)
		if wave.BudgetBytes == 0 {
			// A validated v6/v7 record has only its saved suffix available.
			wave.BudgetBytes = uint32(len(wave.Samples) * 2)
		}
		output.waves = append(output.waves, audioOutputWave{AudioWaveState: wave, started: now})
		output.waveBytes += int(wave.BudgetBytes)
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
	host := output.now()
	from, to := output.restoredAt, output.timeAt(host)
	output.clockAt, output.hostAt, output.clockSet = to, host, true
	for i := range output.waves {
		output.waves[i].started = to.Add(output.waves[i].started.Sub(from))
	}
	for i := range output.notes {
		output.notes[i].started = to.Add(output.notes[i].started.Sub(from))
		if output.notes[i].Channel == 9 {
			output.notes[i].ends = to.Add(output.notes[i].ends.Sub(from))
		}
	}
	for sound, instant := range output.paused {
		output.paused[sound] = to.Add(instant.Sub(from))
	}
	output.detached = false
}

func replayAudioChannel(sink AudioSink, channel uint8, state AudioChannelState) {
	sink.MIDIProgramChange(channel, state.Program)
	sink.MIDIControlChange(channel, 7, state.Volume)
	sink.MIDIControlChange(channel, 10, state.Pan)
	sink.MIDIControlChange(channel, 11, state.Expression)
	sink.MIDIControlChange(channel, 64, state.Sustain)
	replayAudioParameters(sink, channel, state)
	sink.MIDIPitchBend(channel, state.Bend)
}

// ResumeOutput reconstructs sounding notes and sample tails after the Host
// clears its old output queues/device. It neither advances guest playback nor
// records the reconstruction as new events. The Host calls it after adoption,
// reconnect or dropped output, with the previous device state cleared.
func (audio *Audio) ResumeOutput() {
	if audio == nil {
		return
	}
	audio.mutex.Lock()
	defer audio.mutex.Unlock()
	audio.outputTime(audio.timing.current)
	output := audio.sink
	output.replay(output.currentTime(), 0, false)
}

func (output *audioOutput) replaySound(sound AudioHandle, now time.Time) {
	output.replay(now, sound, true)
}

func (output *audioOutput) replay(now time.Time, sound AudioHandle, only bool) {
	if output.sink == nil {
		return
	}
	selected := func(owner AudioHandle) bool {
		_, paused := output.paused[owner]
		return !paused && (!only || sound == owner)
	}
	if sink, ok := output.sink.(AudioGainSink); ok {
		for _, sound := range slices.Sorted(maps.Keys(output.soundChannels)) {
			if selected(sound) {
				sink.SoundGain(sound, output.gain(sound))
			}
		}
	}
	_, independent := output.sink.(OwnedAudioSink)
	if capability, ok := output.sink.(AudioReplaySink); ok {
		independent = independent && capability.IndependentAudioReplay()
	}
	replayChannels := func() {
		if selected(0) {
			for channel, state := range output.channels {
				replayAudioChannel(output.destination(0), uint8(channel), state)
			}
		}
		for _, sound := range slices.Sorted(maps.Keys(output.soundChannels)) {
			if !selected(sound) {
				continue
			}
			for channel, state := range output.soundChannels[sound] {
				replayAudioChannel(output.destination(sound), uint8(channel), state)
			}
		}
	}
	// Restore independent channels once before sounding any note. Reapplying
	// RPN 0 between voices would briefly reset fine sensitivity when CC6 arrives.
	if independent {
		replayChannels()
	}
	noteChannels := make(map[AudioHandle][16]bool)
	for _, note := range output.notes {
		if !selected(note.Sound) || note.Channel == 9 && !note.ends.After(now) {
			continue
		}
		channels := noteChannels[note.Sound]
		channels[note.Channel] = true
		noteChannels[note.Sound] = channels
		if independent {
			output.destination(note.Sound).MIDIProgramChange(note.Channel, note.StartedWith.Program)
		} else {
			state := output.channels[note.Channel]
			if owned := output.soundChannels[note.Sound]; owned != nil {
				state = owned[note.Channel]
			}
			state.Program = note.StartedWith.Program
			replayAudioChannel(output.destination(note.Sound), note.Channel, state)
		}
		if sink, ok := output.sink.(AudioResumeSink); ok {
			velocity := note.Velocity
			if _, raw := output.sink.(AudioGainSink); !raw {
				velocity = uint8(int(velocity) * int(output.gain(note.Sound)) / int(AudioGainUnity))
			}
			sink.ResumeNote(note.Sound, note.Channel, note.Note, velocity, max(0, now.Sub(note.started)))
		} else {
			output.destination(note.Sound).MIDINoteOn(note.Channel, note.Note, note.Velocity)
		}
		if note.Released {
			output.destination(note.Sound).MIDINoteOff(note.Channel, note.Note, 0)
		}
	}
	for _, sound := range slices.Sorted(maps.Keys(noteChannels)) {
		channels := output.channels
		if owned := output.soundChannels[sound]; owned != nil {
			channels = *owned
		}
		for channel, used := range noteChannels[sound] {
			if used {
				output.destination(sound).MIDIProgramChange(uint8(channel), channels[channel].Program)
			}
		}
	}
	if !independent {
		replayChannels()
	}
	output.replayPCM(selected)
	// Reconnect and queue recovery may happen after adoption. Preserve the
	// position within the first remaining frame as well as the whole prefix.
	for _, wave := range output.waves {
		if !selected(wave.Sound) {
			continue
		}
		age := now.Sub(wave.started)
		frames, phase, alive := wavePosition(wave.AudioWaveState, age)
		if !alive {
			continue
		}
		output.emitWave(wave.Sound, smaf.Event{Type: smaf.EventWave, PCMChannel: wave.PCMChannel, WaveChannels: wave.Channels, SamplingRate: wave.Rate, Wave: wave.Samples[frames*int(wave.Channels):]}, phase)
	}
}

func defaultAudioChannels() (channels [16]AudioChannelState) {
	for i := range channels {
		channels[i] = AudioChannelState{Volume: 100, Expression: 127, Pan: 64, Bend: 8192,
			BendSemitones: 2, RPNMSB: 127, RPNLSB: 127, NRPNMSB: 127, NRPNLSB: 127}
	}
	return channels
}

func (output *audioOutput) currentChannels() *[16]AudioChannelState {
	if output.sound == 0 {
		return &output.channels
	}
	channels := output.soundChannels[output.sound]
	if channels == nil {
		defaults := defaultAudioChannels()
		channels = &defaults
		output.soundChannels[output.sound] = channels
	}
	return channels
}

func (output *audioOutput) destination(sound AudioHandle) audioSoundSink {
	return audioSoundSink{sink: output.sink, sound: sound, gain: output.gain(sound)}
}

func (output *audioOutput) stopSound(sound AudioHandle) {
	output.notes = slices.DeleteFunc(output.notes, func(note audioOutputNote) bool { return note.Sound == sound })
	output.waves = slices.DeleteFunc(output.waves, func(wave audioOutputWave) bool {
		if wave.Sound != sound {
			return false
		}
		output.waveBytes -= int(wave.BudgetBytes)
		return true
	})
	delete(output.soundChannels, sound)
	for key := range output.pcmChannels {
		if key.sound == sound {
			delete(output.pcmChannels, key)
		}
	}
	delete(output.gains, sound)
	delete(output.paused, sound)
	if sink, ok := output.sink.(OwnedAudioSink); ok {
		sink.StopSound(sound)
	}
}
