package webhost

import (
	"time"

	"github.com/movingwoo/wfeature/internal/backend"
)

// negotiatedStreamFeature reports whether the page asked for a sound feature
// only the binary stream carries: a sound's own FM voices (voice=1), whose
// definitions travel once each by id, and the velocity that sets a stream
// wave's level (wavevelocity=1). A page that did not ask hears the same notes
// on its ordinary instruments and every wave at full level.
func negotiatedStreamFeature(value string, ownership bool, protocol int) bool {
	return value == "1" && ownership && protocol == protocolStream
}

// ResumeVoiceNote is ResumeNote for a held note that plays its sound's own
// voice. sendAudio removes the voice for pages that did not request it.
func (a *audioCollector) ResumeVoiceNote(sound backend.AudioHandle, channel, note, velocity, voice, pitch uint8, age time.Duration) {
	a.append(audioEvent{Kind: audioNoteResume, Sound: uint32(sound), Channel: channel, Note: note, Velocity: velocity, Age: audioAgeMilliseconds(age), Voice: voice, Pitch: pitch})
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
