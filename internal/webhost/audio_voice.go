package webhost

import (
	"time"

	"github.com/movingwoo/wfeature/internal/backend"
)

// negotiatedVoice reports whether the page synthesizes a sound's own FM
// voices. The definitions travel once each by id, so only the binary stream
// carries them; a page that did not ask hears the same notes on its ordinary
// instruments.
func negotiatedVoice(value string, ownership bool, protocol int) bool {
	return value == "1" && ownership && protocol == protocolStream
}

// ResumeVoiceNote is ResumeNote for a held note that plays its sound's own
// voice. sendAudio removes the voice for pages that did not request it.
func (a *audioCollector) ResumeVoiceNote(sound backend.AudioHandle, channel, note, velocity, voice, pitch uint8, age time.Duration) {
	milliseconds := uint32(min(max(age.Milliseconds(), 0), int64(1<<32-1)))
	a.append(audioEvent{Kind: audioNoteResume, Sound: uint32(sound), Channel: channel, Note: note, Velocity: velocity, Age: milliseconds, Voice: voice, Pitch: pitch})
}

// withoutVoices keeps every note of a batch for a page without FM voices and
// drops what only such a page reads.
func withoutVoices(events []audioEvent) []audioEvent {
	plain := events[:0]
	for _, event := range events {
		if event.Kind == audioVoice {
			continue
		}
		event.Voice, event.Pitch = 0, 0
		plain = append(plain, event)
	}
	return plain
}
