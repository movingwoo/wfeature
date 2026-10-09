package backend

import (
	"fmt"
	"time"
)

// AudioPlayback describes one clip after advancing its logical score. Completed
// counts natural ends since the latest Play or PlayCount, including loop ends.
// Stop retains that count so callers can distinguish completion from cancellation.
type AudioPlayback struct {
	Playing, Paused  bool
	Position, Length time.Duration
	Completed        uint64
}

// NextCompletion reports the current score pass's end in the guest clock used
// by Play and Advance. It neither advances playback nor includes PCM tails;
// an overdue boundary stays unchanged until the caller advances the score.
func (audio *Audio) NextCompletion(handle AudioHandle) (time.Duration, bool) {
	if audio == nil {
		return 0, false
	}
	audio.mutex.Lock()
	defer audio.mutex.Unlock()
	current := audio.sounds[handle]
	if current == nil || !current.playing || current.paused || current.length <= 0 ||
		current.startedAt > time.Duration(1<<63-1)-current.length {
		return 0, false
	}
	return current.startedAt + current.length, true
}

// PlayCount starts at the beginning with a finite pass count, or -1 for an
// unbounded loop. A zero-duration score can play once, but repeated playback
// is refused rather than creating a zero-time event loop.
func (audio *Audio) PlayCount(handle AudioHandle, now time.Duration, count int32) error {
	if audio == nil {
		return fmt.Errorf("audio is not configured")
	}
	audio.mutex.Lock()
	defer audio.mutex.Unlock()
	current := audio.sounds[handle]
	if current == nil {
		return fmt.Errorf("audio handle %d is not loaded", handle)
	}
	if count == 0 || count < -1 || now < 0 || now > time.Duration(1<<63-1)-current.length {
		return fmt.Errorf("audio loop count or start clock is invalid")
	}
	if count != 1 && (current.transient || current.length <= 0) {
		return fmt.Errorf("audio cannot repeat a transient or zero-duration score")
	}
	audio.advanceSounds(now)
	audio.silence(current)
	audio.sink.stopSound(handle)
	audio.sink.setGain(handle, audio.soundGain(current))
	current.playing, current.repeat, current.remaining = true, count == -1, max(count-1, 0)
	current.startedAt, current.cursor, current.position, current.completed = now, 0, 0, 0
	return nil
}

// Rewind resets the current pass without consuming or replenishing its loop
// budget. A paused clip keeps a frozen cursor at zero; a stopped clip remains
// stopped. Previously completed passes remain observable by the platform.
func (audio *Audio) Rewind(handle AudioHandle, now time.Duration) error {
	if audio == nil {
		return fmt.Errorf("audio is not configured")
	}
	audio.mutex.Lock()
	defer audio.mutex.Unlock()
	current := audio.sounds[handle]
	if current == nil {
		return fmt.Errorf("audio handle %d is not loaded", handle)
	}
	if now < 0 || now > time.Duration(1<<63-1)-current.length {
		return fmt.Errorf("audio rewind clock is invalid")
	}
	audio.advanceSounds(now)
	playing, paused := current.playing, current.paused
	audio.silence(current)
	audio.sink.stopSound(handle)
	current.startedAt, current.cursor, current.position = now, 0, 0
	current.playing, current.paused = playing, paused
	if playing || paused {
		audio.sink.setGain(handle, audio.soundGain(current))
	}
	if paused {
		current.pausedAt = now
		// stopSound already cancelled the old output; only its frozen owner
		// bookkeeping is needed until the new first frame is resumed.
		audio.sink.paused[handle] = audio.sink.currentTime()
	}
	return nil
}

// Playback advances all scores before reporting this clip's progress. A guest
// query cannot emit a later note before another owner's earlier note. Platforms
// must serialize the clock read and any dependent transitions across owners;
// callbacks and completion reconciliation remain outside Audio's lock.
func (audio *Audio) Playback(handle AudioHandle, now time.Duration) (AudioPlayback, error) {
	if audio == nil {
		return AudioPlayback{}, fmt.Errorf("audio is not configured")
	}
	audio.mutex.Lock()
	defer audio.mutex.Unlock()
	current := audio.sounds[handle]
	if current == nil {
		return AudioPlayback{}, fmt.Errorf("audio handle %d is not loaded", handle)
	}
	if now < 0 || now < current.startedAt || !current.paused && current.playing && now-current.startedAt < current.position {
		return AudioPlayback{}, fmt.Errorf("audio playback clock is invalid")
	}
	audio.advanceSounds(now)
	return current.playbackState(), nil
}

// PlaybackState reads already serviced progress without advancing any score.
// A platform that reconciles every clip after one global Advance uses this to
// observe that common boundary, without merging the same scores for each clip.
func (audio *Audio) PlaybackState(handle AudioHandle) (AudioPlayback, error) {
	if audio == nil {
		return AudioPlayback{}, fmt.Errorf("audio is not configured")
	}
	audio.mutex.Lock()
	defer audio.mutex.Unlock()
	current := audio.sounds[handle]
	if current == nil {
		return AudioPlayback{}, fmt.Errorf("audio handle %d is not loaded", handle)
	}
	return current.playbackState(), nil
}

func (current *sound) playbackState() AudioPlayback {
	return AudioPlayback{Playing: current.playing && !current.paused, Paused: current.paused,
		Position: current.position, Length: current.length, Completed: current.completed}
}
