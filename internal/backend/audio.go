package backend

import (
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/movingwoo/wfeature/internal/audio/smaf"
)

// AudioSink is what a Host has to provide to make sound audible. It is split
// into PCM and MIDI because SMAF is: a file's percussion and melody are note
// events for a synthesiser, while its sampled sounds are waveforms to mix, and
// no Host renders both the same way. The browser uses its oscillator
// synthesizer and Web Audio; the CLI records instead of playing. A Host that
// implements OwnedAudioSink additionally receives independent clip identities.
//
// Audio serializes sink calls under its mutex. Calls also occur during Play,
// Stop, Close and volume changes, not only Advance. A sink must not call back
// into the same Audio. Slice arguments are borrowed, read-only for the duration
// of the call; a sink that queues them for later use must copy them.
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
//
// Play and Advance must use one monotonic guest-time domain per Audio instance:
// the same origin and units. A playback-speed change affects how quickly the
// Host advances guest time. SetPlaybackRate maps output deadlines into real
// presentation seconds without rescaling score cursors or pitch. Restarting
// the guest clock requires a new timeline; backward timestamps are not a seek.
type Audio struct {
	mutex  sync.Mutex
	sink   *audioOutput
	sounds map[AudioHandle]*sound
	next   AudioHandle
	// maxSounds bounds what a game can retain by loading and never closing.
	maxSounds int
	// Admission sums cached per-sound costs instead of rescanning old payloads.
	resourceLimits audioResourceUsage
	// volume is the guest device level, independent of the user's Host mixer.
	// Gain-capable Hosts apply device and clip levels to active sources too.
	volume int
	timing audioTiming
}

type sound struct {
	handle    AudioHandle
	events    []smaf.Event
	resources audioResourceUsage
	volume    int
	muted     bool
	// transient sounds belong to a fire-and-forget call, not a reusable clip.
	transient bool
	// length is the timestamp of the last event, which is where a repeat
	// restarts — not the last note, so trailing silence is preserved.
	length time.Duration

	playing   bool
	repeat    bool
	paused    bool
	pausedAt  time.Duration
	remaining int32 // Additional finite passes; repeat is the unbounded mode.
	completed uint64
	position  time.Duration
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
	return NewAudioWithClock(sink, nil)
}

// NewAudioWithClock uses an unscaled Host clock between presentation service
// boundaries. MIDI scheduling uses the guest clock passed to Advance; sample
// and envelope ages also include the already elapsed part of a coarse batch.
func NewAudioWithClock(sink AudioSink, now func() time.Time) *Audio {
	return &Audio{sink: newAudioOutput(sink, now), sounds: map[AudioHandle]*sound{}, maxSounds: defaultMaxSounds, resourceLimits: defaultAudioResourceLimits(), volume: maxAudioVolume}
}

// SetSink swaps the Host output a timeline plays through, keeping everything
// already loaded and everything already sounding. A Host that attaches its
// speaker after the program has started — which is when a session learns it has
// one — would otherwise have to replace the timeline, and with it the clips a
// title loaded while starting up.
func (audio *Audio) SetSink(sink AudioSink) {
	if audio == nil {
		return
	}
	audio.mutex.Lock()
	defer audio.mutex.Unlock()
	audio.sink.sink = sink
	audio.outputTime(audio.timing.current)
	if live, ok := sink.(AudioGainSink); ok {
		for _, current := range audio.sounds {
			if _, active := audio.sink.soundChannels[current.handle]; active {
				live.SoundGain(current.handle, audio.soundGain(current))
			}
		}
	}
}

// maxAudioVolume is the loudest a WIPI or MIDP volume goes; zero is silent.
const maxAudioVolume = 100

// SetVolume sets the device level a guest asked for, clamped to 0..100.
// Hosts implementing AudioGainSink also update active MIDI and PCM without
// discarding their playback positions, so raising the level restores them.
func (audio *Audio) SetVolume(percent int) {
	if audio == nil {
		return
	}
	audio.mutex.Lock()
	defer audio.mutex.Unlock()
	audio.volume = min(max(percent, 0), maxAudioVolume)
	for _, current := range audio.sounds {
		audio.updateGain(current)
	}
}

// Volume reports the level SetVolume was last given.
func (audio *Audio) Volume() int {
	if audio == nil {
		return maxAudioVolume
	}
	audio.mutex.Lock()
	defer audio.mutex.Unlock()
	return audio.volume
}

// Load decodes a sound and answers a handle for it. Data that is not a format
// this package understands is an error, so a game asking to play something
// unsupported finds out rather than silently getting a handle to nothing.
func (audio *Audio) Load(data []byte) (AudioHandle, error) {
	if audio == nil {
		return 0, fmt.Errorf("audio is not configured")
	}
	events, err := smaf.Decode(data)
	if err != nil {
		return 0, fmt.Errorf("decode audio: %w", err)
	}
	if len(events) == 0 {
		return 0, fmt.Errorf("audio data is not a playable sound (%d bytes)", len(data))
	}

	audio.mutex.Lock()
	defer audio.mutex.Unlock()
	return audio.loadEvents(events, false)
}

// LoadEvents answers a handle for a sequence the caller built itself. MIDP's
// Manager.playTone is one note rather than a file, and giving it a handle here
// keeps every sound on the one timeline Advance drives instead of adding a
// second path to the sink. Events and nested PCM/SysEx data are copied, so the
// caller can reuse its buffers after return. Admission checks the combined
// loaded payload before copying or allocating a handle.
// Events must be ordered by their nonnegative millisecond offsets.
func (audio *Audio) LoadEvents(events []smaf.Event) (AudioHandle, error) {
	if audio == nil {
		return 0, fmt.Errorf("audio is not configured")
	}
	if len(events) == 0 {
		return 0, fmt.Errorf("sequence has no events")
	}
	audio.mutex.Lock()
	defer audio.mutex.Unlock()
	return audio.loadEvents(events, true)
}

// loadEvents runs under Audio.mutex. Its caller has checked the nonempty input.
// Decoded events already own their buffers; authored events need an owned copy.
func (audio *Audio) loadEvents(events []smaf.Event, copyEvents bool) (AudioHandle, error) {
	if len(audio.sounds) >= audio.maxSounds {
		return 0, fmt.Errorf("more than %d sounds loaded at once", audio.maxSounds)
	}
	if audio.next == ^AudioHandle(0) {
		return 0, fmt.Errorf("audio handle space exhausted")
	}
	resources, err := soundResourceUsage(events, nil)
	if err != nil {
		return 0, err
	}
	if err := audio.admitSoundResources(resources); err != nil {
		return 0, err
	}
	if copyEvents {
		events = cloneAudioEvents(events)
	}
	audio.next++
	handle := audio.next
	audio.sounds[handle] = &sound{
		handle:    handle,
		volume:    maxAudioVolume,
		events:    events,
		resources: resources,
		length:    time.Duration(events[len(events)-1].Time) * time.Millisecond,
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
	if current.transient && repeat {
		return fmt.Errorf("a transient sound cannot repeat")
	}
	audio.advanceSounds(now)
	audio.silence(current)
	audio.sink.stopSound(handle)
	audio.sink.setGain(handle, audio.soundGain(current))
	current.playing, current.repeat, current.startedAt, current.cursor = true, repeat, now, 0
	current.paused, current.pausedAt = false, 0
	current.remaining, current.completed, current.position = 0, 0, 0
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
		current.position = 0
		audio.sink.stopSound(handle)
		if current.transient {
			delete(audio.sounds, handle)
		}
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
	audio.sink.stopSound(handle)
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
		current.position = 0
		audio.sink.stopSound(current.handle)
		if current.transient {
			delete(audio.sounds, current.handle)
		}
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
	return ok && current.playing && !current.paused
}

// Advance emits all due scores in global deadline order. Equal deadlines use
// handle order, then the original event order within each score. The Host calls
// it once per tick; coarser ticks preserve the shared voice-admission order.
func (audio *Audio) Advance(now time.Duration) {
	if audio == nil {
		return
	}
	audio.mutex.Lock()
	defer audio.mutex.Unlock()
	audio.advanceSounds(now)
}

func (audio *Audio) emit(current *sound, event smaf.Event) {
	if audio.sink == nil {
		return
	}
	audio.sink.sound = current.handle
	switch event.Type {
	case smaf.EventWave:
		audio.sink.playWave(event)
	case smaf.EventPCMControl:
		audio.sink.PCMControl(event.PCMChannel, event.Control, event.Value)
	case smaf.EventNoteOn:
		audio.sink.MIDINoteOn(event.Channel, event.Note, event.Velocity)
		if event.Velocity == 0 {
			current.releaseNote(event.Channel, event.Note)
		} else if note := (activeNote{event.Channel, event.Note}); !slices.Contains(current.activeNotes, note) {
			// The output and page replace the same key on a retrigger. Keeping
			// duplicates here would grow forever for an unbalanced loop.
			current.activeNotes = append(current.activeNotes, note)
		}
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
	current.paused, current.pausedAt = false, 0
	if !current.playing {
		current.activeNotes = nil
		return
	}
	current.playing = false
	if audio.sink != nil {
		audio.sink.sound = current.handle
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
