package backend

import (
	"bytes"
	"cmp"
	"fmt"
	"maps"
	"math"
	"slices"
	"time"

	"github.com/movingwoo/wfeature/internal/audio/smaf"
)

// AudioState is the logical playback timeline. Host synthesizer voices, output
// queues and physical sample clocks are separate from this component.
type AudioState struct {
	Version   uint32
	Next      AudioHandle
	MaxSounds int
	Volume    int
	Sounds    []AudioSoundState
	Output    AudioOutputState
}
type AudioSoundState struct {
	Handle       AudioHandle
	Volume       int
	Muted        bool
	Transient    bool
	Events       []smaf.Event
	Length       time.Duration
	Playing      bool
	Repeat       bool
	Paused       bool
	PausedAt     time.Duration
	Remaining    int32
	Completed    uint64
	Position     time.Duration
	StartedAt    time.Duration
	Cursor       int
	ActiveNotes  []AudioNoteState
	UsedChannels [16]bool
}
type AudioNoteState struct{ Channel, Note uint8 }

const (
	// Version 9 preserves PCM subframe position through tail trimming. Versions
	// 6 through 8 remain readable at their saved whole-frame positions.
	audioStateVersion    = 9
	maxAudioStateEntries = 1 << 20
	maxAudioStateBytes   = 128 << 20
)

func validateAudioState(saved AudioState) error {
	if saved.Version < 6 || saved.Version > audioStateVersion || saved.MaxSounds < 0 || saved.MaxSounds > defaultMaxSounds || len(saved.Sounds) > saved.MaxSounds || saved.Volume < 0 || saved.Volume > maxAudioVolume {
		return fmt.Errorf("audio state version, sound limit or volume is invalid")
	}
	if err := saved.Output.validate(saved.Version); err != nil {
		return err
	}
	var used audioResourceUsage
	reservedPCM := make(map[audioPCMKey]bool)
	if saved.Version == 6 && len(saved.Output.PCMChannels) != 0 {
		return fmt.Errorf("legacy audio state contains PCM channel controls")
	}
	for index, current := range saved.Sounds {
		resources, err := soundResourceUsage(current.Events, current.ActiveNotes)
		if err != nil {
			return err
		}
		if err := used.add(resources, defaultAudioResourceLimits()); err != nil {
			return err
		}
		for _, event := range current.Events {
			if event.PCMChannel != 0 {
				if saved.Version == 6 {
					return fmt.Errorf("legacy audio state contains PCM channel events")
				}
				reservedPCM[audioPCMKey{current.Handle, event.PCMChannel}] = true
			}
		}
		if current.Volume < 0 || current.Volume > maxAudioVolume || current.Handle == 0 || current.Handle > saved.Next || index > 0 && saved.Sounds[index-1].Handle >= current.Handle || len(current.Events) == 0 || current.Cursor < 0 || current.Cursor > len(current.Events) || current.Playing && current.Cursor == len(current.Events) || !current.Playing && len(current.ActiveNotes) != 0 {
			return fmt.Errorf("audio state handle or playback cursor is invalid")
		}
		if current.Transient && (!current.Playing || current.Repeat || current.Paused) {
			return fmt.Errorf("audio transient state must be a playing one-shot")
		}
		if current.Remaining < 0 || current.Remaining > math.MaxInt32-1 || current.Repeat && current.Remaining != 0 || current.Transient && current.Remaining != 0 || current.Position < 0 || current.Position > current.Length || current.Completed > uint64(math.MaxInt64/int64(time.Millisecond))+1 {
			return fmt.Errorf("audio loop accounting or media position is invalid")
		}
		if !current.Paused && current.PausedAt != 0 || current.Paused && (current.StartedAt < 0 || current.PausedAt < 0 || current.PausedAt < current.StartedAt || current.Cursor > 0 && current.PausedAt-current.StartedAt < time.Duration(current.Events[current.Cursor-1].Time)*time.Millisecond) {
			return fmt.Errorf("audio state has a pause clock without a paused cursor")
		}
		if current.Paused {
			position := min(current.PausedAt-current.StartedAt, current.Length)
			// Explicit stop resets position, including after natural completion.
			// Pausing that idle clip retains zero rather than advancing its clock.
			if !current.Playing && current.Position == 0 {
				position = 0
			}
			if current.Position != position {
				return fmt.Errorf("audio media position differs from its pause clock")
			}
		}
		length := time.Duration(current.Events[len(current.Events)-1].Time) * time.Millisecond
		if current.Length != length || current.StartedAt > time.Duration(math.MaxInt64)-length {
			return fmt.Errorf("audio state length or playback origin is invalid")
		}
	}
	for _, state := range saved.Output.PCMChannels {
		if !reservedPCM[audioPCMKey{state.Sound, state.Channel}] {
			return fmt.Errorf("audio output refers to an unreserved PCM channel")
		}
	}
	paused := make(map[AudioHandle]bool)
	for _, owner := range saved.Output.Sounds {
		index, ok := slices.BinarySearchFunc(saved.Sounds, owner.Sound, func(sound AudioSoundState, handle AudioHandle) int { return cmp.Compare(sound.Handle, handle) })
		if !ok || saved.Sounds[index].Handle != owner.Sound {
			return fmt.Errorf("audio output refers to an unloaded sound")
		}
		current := saved.Sounds[index]
		paused[owner.Sound] = owner.Paused
		if owner.Paused != current.Paused {
			return fmt.Errorf("audio output and timeline pause states differ")
		}
		gain := uint16(saved.Volume * current.Volume)
		if current.Muted {
			gain = 0
		}
		if owner.Gain != gain {
			return fmt.Errorf("audio output gain differs from its device or clip level")
		}
	}
	for _, current := range saved.Sounds {
		if current.Paused != paused[current.Handle] {
			return fmt.Errorf("audio paused cursor has no frozen output state")
		}
	}
	return nil
}

func cloneAudioEvents(events []smaf.Event) []smaf.Event {
	result := slices.Clone(events)
	for index := range result {
		result[index].Wave = slices.Clone(result[index].Wave)
		result[index].SysEx = bytes.Clone(result[index].SysEx)
	}
	return result
}

// CaptureState does not advance playback or emit anything to the sink. The
// session owner must capture the corresponding guest clock at its barrier too.
func (audio *Audio) CaptureState() (AudioState, error) {
	if audio == nil {
		return AudioState{}, fmt.Errorf("audio is not configured")
	}
	audio.mutex.Lock()
	defer audio.mutex.Unlock()
	return audio.captureStateAt(audio.sink.currentTime())
}

// CaptureStateAt uses the owner's whole-session capture instant, from the
// unscaled Host clock given to NewAudioWithClock.
func (audio *Audio) CaptureStateAt(now time.Time) (AudioState, error) {
	if audio == nil {
		return AudioState{}, fmt.Errorf("audio is not configured")
	}
	audio.mutex.Lock()
	defer audio.mutex.Unlock()
	return audio.captureStateAt(audio.sink.timeAt(now))
}

func (audio *Audio) captureStateAt(now time.Time) (AudioState, error) {
	if len(audio.sounds) > defaultMaxSounds {
		return AudioState{}, fmt.Errorf("audio state exceeds its sound limit")
	}
	saved := AudioState{Version: audioStateVersion, Next: audio.next, MaxSounds: audio.maxSounds, Volume: audio.volume}
	var err error
	saved.Output, err = audio.sink.capture(now)
	if err != nil {
		return AudioState{}, err
	}
	var entries uint64
	for _, handle := range slices.Sorted(maps.Keys(audio.sounds)) {
		current := audio.sounds[handle]
		if current == nil {
			return AudioState{}, fmt.Errorf("audio state contains a null sound")
		}
		entries += uint64(len(current.events)) + uint64(len(current.activeNotes))
		if entries > maxAudioStateEntries {
			return AudioState{}, fmt.Errorf("audio state exceeds its event limit")
		}
		record := AudioSoundState{Handle: handle, Volume: current.volume, Muted: current.muted, Transient: current.transient, Events: current.events, Length: current.length, Playing: current.playing,
			Repeat: current.repeat, Paused: current.paused, PausedAt: current.pausedAt, Remaining: current.remaining, Completed: current.completed, Position: current.position, StartedAt: current.startedAt, Cursor: current.cursor, UsedChannels: current.usedChannels}
		for _, note := range current.activeNotes {
			record.ActiveNotes = append(record.ActiveNotes, AudioNoteState{Channel: note.channel, Note: note.note})
		}
		saved.Sounds = append(saved.Sounds, record)
	}
	if err := validateAudioState(saved); err != nil {
		return AudioState{}, err
	}
	for index := range saved.Sounds {
		saved.Sounds[index].Events = cloneAudioEvents(saved.Sounds[index].Events)
	}
	return saved, nil
}

// NewAudioFromState constructs a detached timeline without sending its past
// events to the sink. The owner must validate its clock relationship and restore
// Host output separately before advancing the timeline in a live session.
func NewAudioFromState(saved AudioState, sink AudioSink) (*Audio, error) {
	return NewAudioFromStateWithClock(saved, sink, nil)
}

// NewAudioFromStateWithClock uses the destination's unscaled output clock.
func NewAudioFromStateWithClock(saved AudioState, sink AudioSink, now func() time.Time) (*Audio, error) {
	if err := validateAudioState(saved); err != nil {
		return nil, err
	}
	audio := &Audio{sink: newAudioOutput(sink, now), sounds: make(map[AudioHandle]*sound, len(saved.Sounds)), next: saved.Next, maxSounds: saved.MaxSounds, resourceLimits: defaultAudioResourceLimits(), volume: saved.Volume}
	audio.sink.restore(saved.Output)
	for _, record := range saved.Sounds {
		resources, err := soundResourceUsage(record.Events, record.ActiveNotes)
		if err != nil {
			return nil, err
		}
		current := &sound{handle: record.Handle, volume: record.Volume, muted: record.Muted, transient: record.Transient, events: cloneAudioEvents(record.Events), resources: resources, length: record.Length, playing: record.Playing,
			repeat: record.Repeat, paused: record.Paused, pausedAt: record.PausedAt, remaining: record.Remaining, completed: record.Completed, position: record.Position, startedAt: record.StartedAt, cursor: record.Cursor, usedChannels: record.UsedChannels}
		for _, note := range record.ActiveNotes {
			current.activeNotes = append(current.activeNotes, activeNote{channel: note.Channel, note: note.Note})
		}
		audio.sounds[record.Handle] = current
	}
	return audio, nil
}
