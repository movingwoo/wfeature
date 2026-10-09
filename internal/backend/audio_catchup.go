package backend

import (
	"fmt"
	"time"

	"github.com/movingwoo/wfeature/internal/audio/smaf"
)

type audioCatchupLimits struct {
	events, bytes, notes uint64
}

// ValidateAudioCatchup bounds the work the first Advance after restoration can
// cause. It validates the saved shape, then visits only due events from each
// saved cursor. Paused sounds use their frozen clock, as their next Resume will.
// It neither advances the source nor emits or allocates output payloads.
func ValidateAudioCatchup(saved AudioState, elapsed time.Duration) error {
	return validateAudioCatchup(saved, elapsed, audioCatchupLimits{
		events: maxAudioStateEntries,
		bytes:  maxAudioStateBytes,
		notes:  maxAudioStateEntries,
	})
}

func validateAudioCatchup(saved AudioState, elapsed time.Duration, left audioCatchupLimits) error {
	if elapsed < 0 {
		return fmt.Errorf("audio catch-up clock is negative")
	}
	if err := validateAudioState(saved); err != nil {
		return err
	}
	for _, current := range saved.Sounds {
		position := elapsed
		if current.StartedAt < 0 {
			return fmt.Errorf("audio playback origin is negative")
		}
		if current.Paused {
			if current.PausedAt > elapsed {
				return fmt.Errorf("audio pause clock exceeds the guest clock")
			}
			position = current.PausedAt
		}
		if !current.Playing {
			continue
		}
		// The validated keys are unique and in range. Preserve their order:
		// note-on scans it, and note-off removes an entry with a stable copy.
		var held [16 * 128]AudioNoteState
		active := held[:copy(held[:], current.ActiveNotes)]
		offset := position - current.StartedAt
		cursor, remaining := current.Cursor, current.Remaining
		for {
			for cursor < len(current.Events) {
				event := current.Events[cursor]
				if time.Duration(event.Time)*time.Millisecond > offset {
					break
				}
				if left.events == 0 {
					return fmt.Errorf("audio catch-up exceeds its event limit")
				}
				left.events--
				// PCM may be copied and scaled on each emission. Count each
				// reference, including repeats, without copying the payload.
				size := 2*uint64(len(event.Wave)) + uint64(len(event.SysEx))
				if event.Type == smaf.EventWave && event.PCMChannel != 0 {
					// An incapable sink receives mono-to-stereo pan rendering.
					size += 2 * uint64(len(event.Wave))
				}
				if size > left.bytes {
					return fmt.Errorf("audio catch-up exceeds its data limit")
				}
				left.bytes -= size
				key := AudioNoteState{Channel: event.Channel, Note: event.Note}
				switch {
				case event.Type == smaf.EventNoteOn && event.Velocity != 0:
					found := false
					for _, note := range active {
						if left.notes == 0 {
							return fmt.Errorf("audio catch-up exceeds its note work limit")
						}
						left.notes--
						if note == key {
							found = true
							break
						}
					}
					if !found {
						active = append(active, key)
					}
				case event.Type == smaf.EventNoteOff || event.Type == smaf.EventNoteOn:
					// A miss scans every entry. A hit scans the prefix and
					// copies the suffix; their combined cost is len(active).
					if uint64(len(active)) > left.notes {
						return fmt.Errorf("audio catch-up exceeds its note work limit")
					}
					left.notes -= uint64(len(active))
					for index, note := range active {
						if note == key {
							active = append(active[:index], active[index+1:]...)
							break
						}
					}
				}
				cursor++
			}
			if cursor < len(current.Events) {
				break
			}
			if !current.Repeat && remaining == 0 || current.Length == 0 {
				if uint64(len(active)) > left.notes {
					return fmt.Errorf("audio catch-up exceeds its note work limit")
				}
				left.notes -= uint64(len(active))
				break
			}
			if remaining > 0 {
				remaining--
			}
			// The last event is at Length, so every completed nonzero pass
			// consumes that much offset. No multiplication or origin addition
			// can wrap, and even an End-only loop spends its event budget.
			offset -= current.Length
			cursor = 0
		}
	}
	return nil
}
