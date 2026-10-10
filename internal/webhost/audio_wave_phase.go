package webhost

import (
	"github.com/movingwoo/wfeature/internal/audio/smaf"
	"github.com/movingwoo/wfeature/internal/backend"
)

func negotiatedWavePhase(value string, ownership bool) bool {
	return value == "1" && ownership
}

// Keep phase separate from presentation time: a replay's controls and waves
// share one ordered instant even when their positions within a frame differ.
// sendAudio removes the extension for pages that did not request it.
func (a *audioCollector) ResumeWave(sound backend.AudioHandle, event smaf.Event, framePhase uint32) {
	a.queueAudioEvent(sound, event, framePhase)
}
