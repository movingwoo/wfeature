package backend

import (
	"errors"
	"fmt"

	"github.com/movingwoo/wfeature/internal/audio/smaf"
)

// ErrAudioResourceLimit reports an admission that would exceed the retained
// sound budget. It does not consume a handle or alter already loaded sounds.
var ErrAudioResourceLimit = errors.New("loaded audio exceeds resource limit")

// These are logical payload costs, shared with checkpoint validation. Reserve
// the possible active keys at admission so starting playback cannot exceed it.
type audioResourceUsage struct {
	entries     uint64
	bytes       uint64
	pcmChannels uint64
}

// Reserve channel state at load time, including controls without a wave.
// Three replay controls per group fit within the Host's reconstruction budget.
const maxPCMChannels = 2048

func defaultAudioResourceLimits() audioResourceUsage {
	return audioResourceUsage{entries: maxAudioStateEntries, bytes: maxAudioStateBytes, pcmChannels: maxPCMChannels}
}

func (used *audioResourceUsage) add(next, limit audioResourceUsage) error {
	if used.entries > limit.entries || next.entries > limit.entries-used.entries {
		return fmt.Errorf("%w: more than %d event and active-key entries", ErrAudioResourceLimit, limit.entries)
	}
	if used.bytes > limit.bytes || next.bytes > limit.bytes-used.bytes {
		return fmt.Errorf("%w: more than %d payload bytes", ErrAudioResourceLimit, limit.bytes)
	}
	if used.pcmChannels > limit.pcmChannels || next.pcmChannels > limit.pcmChannels-used.pcmChannels {
		return fmt.Errorf("%w: more than %d PCM channels", ErrAudioResourceLimit, limit.pcmChannels)
	}
	used.entries += next.entries
	used.bytes += next.bytes
	used.pcmChannels += next.pcmChannels
	return nil
}

func (used *audioResourceUsage) payload(count int, width uint64) error {
	if used.bytes > maxAudioStateBytes || uint64(count) > (maxAudioStateBytes-used.bytes)/width {
		return fmt.Errorf("%w: more than %d payload bytes", ErrAudioResourceLimit, maxAudioStateBytes)
	}
	used.bytes += uint64(count) * width
	return nil
}

func soundResourceUsage(events []smaf.Event, active []AudioNoteState) (audioResourceUsage, error) {
	used := audioResourceUsage{entries: uint64(len(events)), bytes: 128}
	if err := used.add(audioResourceUsage{}, defaultAudioResourceLimits()); err != nil {
		return audioResourceUsage{}, err
	}
	if err := used.payload(len(events), 64); err != nil {
		return audioResourceUsage{}, err
	}
	var keys [16][128]bool
	keyCount := 0
	mark := func(channel, note uint8) {
		if !keys[channel][note] {
			keys[channel][note] = true
			keyCount++
		}
	}
	groups := make(map[uint16]bool)
	for index, event := range events {
		if event.Type > smaf.EventPCMControl || index > 0 && events[index-1].Time > event.Time {
			return audioResourceUsage{}, fmt.Errorf("audio events have an unsupported type or order")
		}
		if event.PCMChannel != 0 && event.Type != smaf.EventWave && event.Type != smaf.EventPCMControl {
			return audioResourceUsage{}, fmt.Errorf("audio event has an unrelated PCM channel")
		}
		if event.Type == smaf.EventPCMControl && (event.PCMChannel == 0 || !validPCMControl(event.Control, event.Value)) {
			return audioResourceUsage{}, fmt.Errorf("audio PCM control is invalid")
		}
		if event.Type == smaf.EventWave && event.PCMChannel != 0 && (event.WaveChannels != 1 || event.SamplingRate == 0) {
			return audioResourceUsage{}, fmt.Errorf("audio controlled PCM must be mono with a sample rate")
		}
		if event.PCMChannel != 0 && !groups[event.PCMChannel] {
			if err := used.add(audioResourceUsage{entries: 1, bytes: 16, pcmChannels: 1}, defaultAudioResourceLimits()); err != nil {
				return audioResourceUsage{}, err
			}
			groups[event.PCMChannel] = true
		}
		if event.Type == smaf.EventNoteOn || event.Type == smaf.EventNoteOff {
			if event.Channel >= 16 || event.Note >= 128 || event.Velocity >= 128 {
				return audioResourceUsage{}, fmt.Errorf("audio note has an invalid channel, key or velocity")
			}
			if event.Type == smaf.EventNoteOn && event.Velocity != 0 {
				mark(event.Channel, event.Note)
			}
		}
		if event.Type == smaf.EventWave && len(event.Wave) != 0 && (event.WaveChannels == 0 || event.SamplingRate == 0) {
			return audioResourceUsage{}, fmt.Errorf("audio wave has no channel or sample rate")
		}
		if err := used.payload(len(event.Wave), 2); err != nil {
			return audioResourceUsage{}, err
		}
		if err := used.payload(len(event.SysEx), 1); err != nil {
			return audioResourceUsage{}, err
		}
	}
	var held [16][128]bool
	for _, note := range active {
		if note.Channel >= 16 || note.Note >= 128 || held[note.Channel][note.Note] {
			return audioResourceUsage{}, fmt.Errorf("audio active key is invalid or duplicated")
		}
		held[note.Channel][note.Note] = true
		mark(note.Channel, note.Note)
	}
	if err := used.add(audioResourceUsage{entries: uint64(keyCount), bytes: uint64(keyCount) * 8}, defaultAudioResourceLimits()); err != nil {
		return audioResourceUsage{}, err
	}
	return used, nil
}

// Costs are immutable after admission. Closing a handle releases its budget by
// removing its cached record; stop and pause retain the loaded payload.
func (audio *Audio) admitSoundResources(next audioResourceUsage) error {
	used := next
	if err := used.add(audioResourceUsage{}, audio.resourceLimits); err != nil {
		return err
	}
	for _, current := range audio.sounds {
		if err := used.add(current.resources, audio.resourceLimits); err != nil {
			return err
		}
	}
	return nil
}
