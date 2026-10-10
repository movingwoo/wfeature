package backend

import (
	"fmt"
	"time"

	"github.com/movingwoo/wfeature/internal/audio/smaf"
)

// PlayTransient starts an authored one-shot sequence and releases its handle
// and output on completion or stop. It retains the ordinary loaded-sound bound
// and permits overlapping calls. Loading and publishing playback are atomic;
// callers never acquire a handle they would have to close on failure.
// Events have the same copying, resource and ordering contract as LoadEvents.
func (audio *Audio) PlayTransient(events []smaf.Event, now time.Duration) error {
	if audio == nil {
		return fmt.Errorf("audio is not configured")
	}
	if len(events) == 0 {
		return fmt.Errorf("sequence has no events")
	}
	audio.mutex.Lock()
	defer audio.mutex.Unlock()
	handle, err := audio.loadEvents(events, true)
	if err != nil {
		return err
	}
	audio.advanceSounds(now)
	current := audio.sounds[handle]
	current.playing, current.transient, current.startedAt = true, true, now
	audio.sink.setGain(handle, audio.soundGain(current))
	return nil
}
