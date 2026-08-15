package backend

import (
	"fmt"
	"sync"
	"time"

	"github.com/movingwoo/wfeature/internal/audio/smaf"
)

// AudioSink is what a Host has to provide to make sound audible. It is split
// into PCM and MIDI because SMAF is: a file's percussion and melody are note
// events for a synthesiser, while its sampled sounds are waveforms to mix, and
// no Host renders both the same way. The browser drives a soundfont
// synthesiser and Web Audio; the CLI records instead of playing.
//
// Every method is called from the Host's own goroutine while it advances the
// audio clock, never concurrently.
type AudioSink interface {
	PlayWave(channels uint8, samplingRate uint32, samples []int16)
	MIDINoteOn(channel, note, velocity uint8)
	MIDINoteOff(channel, note, velocity uint8)
	MIDIProgramChange(channel, program uint8)
	MIDIControlChange(channel, control, value uint8)
	MIDIPitchBend(channel uint8, value uint16)
	MIDISysEx(data []byte)
}

// AudioHandle identifies a loaded sound.
type AudioHandle uint32

// Audio owns the sounds a game has loaded and where each one is in its
// playback. It does not own a clock: Advance is called with the same clock the
// guest runs on, so a Host batching ticks through a manual clock hears the
// same sequence a Host running in real time does, only faster.
type Audio struct {
	mutex  sync.Mutex
	sink   AudioSink
	sounds map[AudioHandle]*sound
	next   AudioHandle
	// maxSounds bounds what a game can retain by loading and never closing.
	maxSounds int
}

type sound struct {
	events []smaf.Event
	// length is the timestamp of the last event, which is where a repeat
	// restarts — not the last note, so trailing silence is preserved.
	length time.Duration

	playing bool
	repeat  bool
	// startedAt is the clock reading this pass through the events began at.
	startedAt time.Duration
	cursor    int
	// activeNotes are the notes started and not yet stopped, so that stopping
	// playback mid-phrase does not leave a note sounding forever.
	activeNotes  []activeNote
	usedChannels [16]bool
}

type activeNote struct{ channel, note uint8 }

const defaultMaxSounds = 256

// NewAudio returns an Audio writing to sink. A nil sink is allowed and makes
// every sound silent, which is what a Host without an audio device wants.
func NewAudio(sink AudioSink) *Audio {
	return &Audio{sink: sink, sounds: map[AudioHandle]*sound{}, maxSounds: defaultMaxSounds}
}

// Load decodes a sound and answers a handle for it. Data that is not a format
// this package understands is an error, so a game asking to play something
// unsupported finds out rather than silently getting a handle to nothing.
func (audio *Audio) Load(data []byte) (AudioHandle, error) {
	if audio == nil {
		return 0, fmt.Errorf("audio is not configured")
	}
	events := smaf.Play(data)
	if len(events) == 0 {
		return 0, fmt.Errorf("audio data is not a playable sound (%d bytes)", len(data))
	}

	audio.mutex.Lock()
	defer audio.mutex.Unlock()
	if len(audio.sounds) >= audio.maxSounds {
		return 0, fmt.Errorf("more than %d sounds loaded at once", audio.maxSounds)
	}
	audio.next++
	handle := audio.next
	audio.sounds[handle] = &sound{events: events, length: time.Duration(events[len(events)-1].Time) * time.Millisecond}
	return handle, nil
}

// LoadEvents answers a handle for a sequence the caller built itself. MIDP's
// Manager.playTone is one note rather than a file, and giving it a handle here
// keeps every sound on the one timeline Advance drives instead of adding a
// second path to the sink.
func (audio *Audio) LoadEvents(events []smaf.Event) (AudioHandle, error) {
	if audio == nil {
		return 0, fmt.Errorf("audio is not configured")
	}
	if len(events) == 0 {
		return 0, fmt.Errorf("sequence has no events")
	}
	audio.mutex.Lock()
	defer audio.mutex.Unlock()
	if len(audio.sounds) >= audio.maxSounds {
		return 0, fmt.Errorf("more than %d sounds loaded at once", audio.maxSounds)
	}
	audio.next++
	handle := audio.next
	audio.sounds[handle] = &sound{
		events: append([]smaf.Event(nil), events...),
		length: time.Duration(events[len(events)-1].Time) * time.Millisecond,
	}
	return handle, nil
}

// Length reports how long a loaded sound runs, which is what a media API has
// to answer for getDuration.
func (audio *Audio) Length(handle AudioHandle) (time.Duration, bool) {
	if audio == nil {
		return 0, false
	}
	audio.mutex.Lock()
	defer audio.mutex.Unlock()
	current, ok := audio.sounds[handle]
	if !ok {
		return 0, false
	}
	return current.length, true
}

// Play starts a sound from its beginning. Playing one already playing restarts
// it, which is what a game retriggering a sound effect means.
func (audio *Audio) Play(handle AudioHandle, now time.Duration, repeat bool) error {
	if audio == nil {
		return fmt.Errorf("audio is not configured")
	}
	audio.mutex.Lock()
	defer audio.mutex.Unlock()
	current, ok := audio.sounds[handle]
	if !ok {
		return fmt.Errorf("audio handle %d is not loaded", handle)
	}
	audio.silence(current)
	current.playing, current.repeat, current.startedAt, current.cursor = true, repeat, now, 0
	return nil
}

// Stop ends playback and releases whatever it left sounding.
func (audio *Audio) Stop(handle AudioHandle) {
	if audio == nil {
		return
	}
	audio.mutex.Lock()
	defer audio.mutex.Unlock()
	if current, ok := audio.sounds[handle]; ok {
		audio.silence(current)
	}
}

// Close stops a sound and forgets it.
func (audio *Audio) Close(handle AudioHandle) error {
	if audio == nil {
		return fmt.Errorf("audio is not configured")
	}
	audio.mutex.Lock()
	defer audio.mutex.Unlock()
	current, ok := audio.sounds[handle]
	if !ok {
		return fmt.Errorf("audio handle %d is not loaded", handle)
	}
	audio.silence(current)
	delete(audio.sounds, handle)
	return nil
}

// StopAll silences everything, which a Host does when a game exits.
func (audio *Audio) StopAll() {
	if audio == nil {
		return
	}
	audio.mutex.Lock()
	defer audio.mutex.Unlock()
	for _, current := range audio.sounds {
		audio.silence(current)
	}
}

// Playing reports whether a handle is sounding, which Java's Player.getState
// answers from.
func (audio *Audio) Playing(handle AudioHandle) bool {
	if audio == nil {
		return false
	}
	audio.mutex.Lock()
	defer audio.mutex.Unlock()
	current, ok := audio.sounds[handle]
	return ok && current.playing
}

// Advance emits everything due at now. The Host calls it once per tick; a
// batched Host whose clock jumps forward emits the skipped events in order
// rather than dropping them, because a note-off that is skipped never stops.
func (audio *Audio) Advance(now time.Duration) {
	if audio == nil || audio.sink == nil {
		return
	}
	audio.mutex.Lock()
	defer audio.mutex.Unlock()
	for _, current := range audio.sounds {
		audio.advanceSound(current, now)
	}
}

func (audio *Audio) advanceSound(current *sound, now time.Duration) {
	for current.playing {
		for current.cursor < len(current.events) {
			event := current.events[current.cursor]
			due := current.startedAt + time.Duration(event.Time)*time.Millisecond
			if due > now {
				return
			}
			current.cursor++
			audio.emit(current, event)
		}

		if !current.repeat {
			audio.silence(current)
			return
		}
		// Restarting from the track's length rather than from now keeps a
		// looping sound on its own grid however coarsely the Host ticks.
		current.startedAt += current.length
		current.cursor = 0
		if current.length <= 0 {
			// A zero-length track would spin forever; treat it as one-shot.
			audio.silence(current)
			return
		}
	}
}

func (audio *Audio) emit(current *sound, event smaf.Event) {
	switch event.Type {
	case smaf.EventWave:
		audio.sink.PlayWave(event.WaveChannels, event.SamplingRate, event.Wave)
	case smaf.EventNoteOn:
		audio.sink.MIDINoteOn(event.Channel, event.Note, event.Velocity)
		current.activeNotes = append(current.activeNotes, activeNote{event.Channel, event.Note})
		current.markChannel(event.Channel)
	case smaf.EventNoteOff:
		audio.sink.MIDINoteOff(event.Channel, event.Note, event.Velocity)
		current.releaseNote(event.Channel, event.Note)
	case smaf.EventProgramChange:
		audio.sink.MIDIProgramChange(event.Channel, event.Program)
		current.markChannel(event.Channel)
	case smaf.EventControlChange:
		audio.sink.MIDIControlChange(event.Channel, event.Control, event.Value)
		current.markChannel(event.Channel)
	case smaf.EventPitchBend:
		audio.sink.MIDIPitchBend(event.Channel, event.Bend)
		current.markChannel(event.Channel)
	case smaf.EventSysEx:
		audio.sink.MIDISysEx(event.SysEx)
	}
}

// silence ends playback and leaves the synthesiser as it found it: every note
// this sound started is released, and every channel it touched has its sustain
// pedal lifted and its sound and notes cut. Without the last part a sound
// stopped during a sustained chord rings on under the next one.
func (audio *Audio) silence(current *sound) {
	if !current.playing {
		current.activeNotes = nil
		return
	}
	current.playing = false
	if audio.sink != nil {
		for _, note := range current.activeNotes {
			audio.sink.MIDINoteOff(note.channel, note.note, 0)
		}
		for channel, used := range current.usedChannels {
			if !used {
				continue
			}
			audio.sink.MIDIControlChange(uint8(channel), 64, 0)
			audio.sink.MIDIControlChange(uint8(channel), 120, 0)
			audio.sink.MIDIControlChange(uint8(channel), 123, 0)
		}
	}
	current.activeNotes = nil
	current.usedChannels = [16]bool{}
}

func (current *sound) markChannel(channel uint8) {
	if channel < uint8(len(current.usedChannels)) {
		current.usedChannels[channel] = true
	}
}

func (current *sound) releaseNote(channel, note uint8) {
	for index, active := range current.activeNotes {
		if active.channel == channel && active.note == note {
			current.activeNotes = append(current.activeNotes[:index], current.activeNotes[index+1:]...)
			return
		}
	}
}
