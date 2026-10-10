package backend

import "github.com/movingwoo/wfeature/internal/audio/smaf"

// OwnedAudioSink receives independent playback streams. Sound handles scope
// every MIDI channel and sampled source; stopping one stream must also cancel
// its percussion and release tails. Audio serializes these calls with AudioSink
// calls. Event buffers are borrowed and read-only for the duration of the call.
// Hosts without this interface receive the legacy flattened diagnostic stream.
type OwnedAudioSink interface {
	AudioEvent(sound AudioHandle, event smaf.Event)
	StopSound(sound AudioHandle)
}

// AudioReplaySink lets a forwarding Host report that its destination flattens
// sound ownership, despite retaining owner tags inside the Host. Such a sink
// needs each reconstructed note preceded by its owner's channel controls.
// Without this interface, OwnedAudioSink implies independent reconstruction.
type AudioReplaySink interface {
	IndependentAudioReplay() bool
}

// AudioPCMSink reports whether an owned destination preserves independent PCM
// channels and live controls. Incapable sinks receive gain/pan at wave onset.
// A forwarding Host must answer for its current consumer, including reconnects.
type AudioPCMSink interface {
	PCMChannels() bool
}

// AudioWaveResumeSink preserves a resumed wave's position within its first
// remaining sample frame. Frame phase is in [0, 1e9), in billionths of a frame;
// its buffer offset in seconds is framePhase/(1e9*event.SamplingRate).
// Buffers are borrowed and read-only, as for AudioEvent. Older sinks receive
// the same whole-frame suffix without the fractional offset.
type AudioWaveResumeSink interface {
	OwnedAudioSink
	ResumeWave(sound AudioHandle, event smaf.Event, framePhase uint32)
}

func emitAudioEvent(sink AudioSink, sound AudioHandle, event smaf.Event) {
	if sink == nil {
		return
	}
	if owned, ok := sink.(OwnedAudioSink); ok {
		owned.AudioEvent(sound, event)
		return
	}
	switch event.Type {
	case smaf.EventWave:
		sink.PlayWave(event.WaveChannels, event.SamplingRate, event.Wave)
	case smaf.EventNoteOn:
		sink.MIDINoteOn(event.Channel, event.Note, event.Velocity)
	case smaf.EventNoteOff:
		sink.MIDINoteOff(event.Channel, event.Note, event.Velocity)
	case smaf.EventProgramChange:
		sink.MIDIProgramChange(event.Channel, event.Program)
	case smaf.EventControlChange:
		sink.MIDIControlChange(event.Channel, event.Control, event.Value)
	case smaf.EventPitchBend:
		sink.MIDIPitchBend(event.Channel, event.Bend)
	case smaf.EventSysEx:
		sink.MIDISysEx(event.SysEx)
	}
}

// audioSoundSink preserves identity while reconstructing output through helpers
// that also serve older diagnostic sinks.
type audioSoundSink struct {
	sink  AudioSink
	sound AudioHandle
	gain  uint16
}

func (s audioSoundSink) PlayWave(channels uint8, rate uint32, samples []int16) {
	s.playWave(smaf.Event{Type: smaf.EventWave, WaveChannels: channels, SamplingRate: rate, Wave: samples})
}
func (s audioSoundSink) playWave(event smaf.Event) {
	s.resumeWave(event, 0)
}
func (s audioSoundSink) resumeWave(event smaf.Event, framePhase uint32) {
	event = smaf.Event{Type: smaf.EventWave, PCMChannel: event.PCMChannel, WaveChannels: event.WaveChannels, SamplingRate: event.SamplingRate, Wave: event.Wave}
	if _, live := s.sink.(AudioGainSink); !live {
		event.Wave = scaleAudioSamples(event.Wave, s.gain)
	}
	if sink, ok := s.sink.(AudioWaveResumeSink); ok && framePhase != 0 {
		sink.ResumeWave(s.sound, event, framePhase)
		return
	}
	emitAudioEvent(s.sink, s.sound, event)
}
func (s audioSoundSink) MIDINoteOn(channel, note, velocity uint8) {
	if _, live := s.sink.(AudioGainSink); !live {
		velocity = uint8(int(velocity) * int(s.gain) / int(AudioGainUnity))
	}
	emitAudioEvent(s.sink, s.sound, smaf.Event{Type: smaf.EventNoteOn, Channel: channel, Note: note, Velocity: velocity})
}
func (s audioSoundSink) MIDINoteOff(channel, note, velocity uint8) {
	emitAudioEvent(s.sink, s.sound, smaf.Event{Type: smaf.EventNoteOff, Channel: channel, Note: note, Velocity: velocity})
}
func (s audioSoundSink) MIDIProgramChange(channel, program uint8) {
	emitAudioEvent(s.sink, s.sound, smaf.Event{Type: smaf.EventProgramChange, Channel: channel, Program: program})
}
func (s audioSoundSink) MIDIControlChange(channel, control, value uint8) {
	emitAudioEvent(s.sink, s.sound, smaf.Event{Type: smaf.EventControlChange, Channel: channel, Control: control, Value: value})
}
func (s audioSoundSink) MIDIPitchBend(channel uint8, value uint16) {
	emitAudioEvent(s.sink, s.sound, smaf.Event{Type: smaf.EventPitchBend, Channel: channel, Bend: value})
}
func (s audioSoundSink) MIDISysEx(data []byte) {
	emitAudioEvent(s.sink, s.sound, smaf.Event{Type: smaf.EventSysEx, SysEx: data})
}
