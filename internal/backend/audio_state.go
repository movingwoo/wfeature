package backend

import (
	"bytes"
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
	Events       []smaf.Event
	Length       time.Duration
	Playing      bool
	Repeat       bool
	StartedAt    time.Duration
	Cursor       int
	ActiveNotes  []AudioNoteState
	UsedChannels [16]bool
}
type AudioNoteState struct{ Channel, Note uint8 }

const (
	maxAudioStateEntries = 1 << 20
	maxAudioStateBytes   = 128 << 20
)

func validateAudioState(saved AudioState) error {
	if saved.Version != 1 || saved.MaxSounds < 0 || saved.MaxSounds > defaultMaxSounds || len(saved.Sounds) > saved.MaxSounds || saved.Volume < 0 || saved.Volume > maxAudioVolume {
		return fmt.Errorf("audio state version, sound limit or volume is invalid")
	}
	if err := saved.Output.validate(); err != nil {
		return err
	}
	used := uint64(len(saved.Sounds)) * 128
	var entries uint64
	charge := func(count int, width uint64) bool {
		if uint64(count) > (maxAudioStateBytes-used)/width {
			return false
		}
		used += uint64(count) * width
		return true
	}
	for index, current := range saved.Sounds {
		entries += uint64(len(current.Events)) + uint64(len(current.ActiveNotes))
		if entries > maxAudioStateEntries || !charge(len(current.Events), 64) || !charge(len(current.ActiveNotes), 8) {
			return fmt.Errorf("audio state exceeds its event or data limit")
		}
		if current.Handle == 0 || current.Handle > saved.Next || index > 0 && saved.Sounds[index-1].Handle >= current.Handle || len(current.Events) == 0 || current.Cursor < 0 || current.Cursor > len(current.Events) || current.Playing && current.Cursor == len(current.Events) || !current.Playing && len(current.ActiveNotes) != 0 {
			return fmt.Errorf("audio state handle or playback cursor is invalid")
		}
		length := time.Duration(current.Events[len(current.Events)-1].Time) * time.Millisecond
		if current.Length != length || current.StartedAt > time.Duration(math.MaxInt64)-length {
			return fmt.Errorf("audio state length or playback origin is invalid")
		}
		for i, event := range current.Events {
			if event.Type > smaf.EventEnd || i > 0 && current.Events[i-1].Time > event.Time {
				return fmt.Errorf("audio state events have an unsupported type or order")
			}
			if !charge(len(event.Wave), 2) || !charge(len(event.SysEx), 1) {
				return fmt.Errorf("audio state sample or SysEx data exceeds limit")
			}
			if event.Type == smaf.EventWave && len(event.Wave) != 0 && (event.WaveChannels == 0 || event.SamplingRate == 0) {
				return fmt.Errorf("audio state wave has no channel or sample rate")
			}
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
	return audio.captureStateAt(audio.sink.now())
}

// CaptureStateAt uses the owner's whole-session capture instant, from the
// unscaled Host clock given to NewAudioWithClock.
func (audio *Audio) CaptureStateAt(now time.Time) (AudioState, error) {
	if audio == nil {
		return AudioState{}, fmt.Errorf("audio is not configured")
	}
	audio.mutex.Lock()
	defer audio.mutex.Unlock()
	return audio.captureStateAt(now)
}

func (audio *Audio) captureStateAt(now time.Time) (AudioState, error) {
	if len(audio.sounds) > defaultMaxSounds {
		return AudioState{}, fmt.Errorf("audio state exceeds its sound limit")
	}
	saved := AudioState{Version: 1, Next: audio.next, MaxSounds: audio.maxSounds, Volume: audio.volume}
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
		record := AudioSoundState{Handle: handle, Events: current.events, Length: current.length, Playing: current.playing,
			Repeat: current.repeat, StartedAt: current.startedAt, Cursor: current.cursor, UsedChannels: current.usedChannels}
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
	audio := &Audio{sink: newAudioOutput(sink, now), sounds: make(map[AudioHandle]*sound, len(saved.Sounds)), next: saved.Next, maxSounds: saved.MaxSounds, volume: saved.Volume}
	audio.sink.restore(saved.Output)
	for _, record := range saved.Sounds {
		current := &sound{events: cloneAudioEvents(record.Events), length: record.Length, playing: record.Playing,
			repeat: record.Repeat, startedAt: record.StartedAt, cursor: record.Cursor, usedChannels: record.UsedChannels}
		for _, note := range record.ActiveNotes {
			current.activeNotes = append(current.activeNotes, activeNote{channel: note.Channel, note: note.Note})
		}
		audio.sounds[record.Handle] = current
	}
	return audio, nil
}
