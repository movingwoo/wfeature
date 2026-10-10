package webhost

import (
	"encoding/binary"
	"slices"

	"github.com/movingwoo/wfeature/internal/backend"
)

// A cached page without sound ownership still receives scaled future events.
// Such a page cannot update active PCM gain. Keep at most its 24 held note keys
// so muting also preserves the old note-off behavior without unbounded state.
type legacyAudioState struct {
	gains map[uint32]uint16
	notes []audioEvent
}

func (state *legacyAudioState) flatten(events []audioEvent) []audioEvent {
	legacy := make([]audioEvent, 0, len(events))
	for _, event := range events {
		switch event.Kind {
		case audioAllOff:
			clear(state.gains)
			state.notes = nil
		case audioSoundGain:
			if state.gains == nil {
				state.gains = make(map[uint32]uint16)
			}
			state.gains[event.Sound] = event.Value
			if event.Value == 0 {
				for _, note := range state.notes {
					if note.Sound == event.Sound {
						legacy = append(legacy, audioEvent{Kind: audioNoteOff, Channel: note.Channel, Note: note.Note})
					}
				}
			}
			continue
		case audioStopSound:
			delete(state.gains, event.Sound)
			state.notes = slices.DeleteFunc(state.notes, func(note audioEvent) bool { return note.Sound == event.Sound })
			continue
		case audioNoteOn, audioNoteOff:
			state.notes = slices.DeleteFunc(state.notes, func(note audioEvent) bool {
				return note.Sound == event.Sound && note.Channel == event.Channel && note.Note == event.Note
			})
			if event.Kind == audioNoteOn && event.Velocity != 0 {
				if len(state.notes) == 24 {
					state.notes = state.notes[1:]
				}
				state.notes = append(state.notes, event)
			}
		case audioControlChange:
			if event.Control == 120 || event.Control == 123 {
				state.notes = slices.DeleteFunc(state.notes, func(note audioEvent) bool {
					return note.Sound == event.Sound && note.Channel == event.Channel
				})
			}
		}
		gain, ok := state.gains[event.Sound]
		if ok && gain != backend.AudioGainUnity {
			switch event.Kind {
			case audioNoteOn:
				event.Velocity = uint8(int(event.Velocity) * int(gain) / int(backend.AudioGainUnity))
			case audioPlayWave:
				pcm := slices.Clone(event.pcm)
				for i := 0; i+1 < len(pcm); i += 2 {
					sample := int16(binary.LittleEndian.Uint16(pcm[i:]))
					binary.LittleEndian.PutUint16(pcm[i:], uint16(int(sample)*int(gain)/int(backend.AudioGainUnity)))
				}
				event.pcm = pcm
			}
		}
		event.Sound = 0
		legacy = append(legacy, event)
	}
	return legacy
}
