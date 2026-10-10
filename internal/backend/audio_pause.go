package backend

import (
	"fmt"
	"math"
	"slices"
	"time"
)

// AudioResumeSink restores a held note at its elapsed envelope position.
// Ordinary note-on remains a new attack. Hosts without this optional boundary
// receive note-on when resuming, retaining the older diagnostic behavior.
type AudioResumeSink interface {
	OwnedAudioSink
	ResumeNote(sound AudioHandle, channel, note, velocity uint8, age time.Duration)
}

// SetRepeat changes the next score-end action without discarding a retained
// cursor. A media API may change its loop setting while the clip is paused.
func (audio *Audio) SetRepeat(handle AudioHandle, repeat bool) error {
	count := int32(1)
	if repeat {
		count = -1
	}
	return audio.SetLoopCount(handle, count)
}

// SetLoopCount includes the current pass in a finite count; -1 loops forever.
// It changes neither the current position nor completed-pass accounting.
func (audio *Audio) SetLoopCount(handle AudioHandle, count int32) error {
	if audio == nil {
		return fmt.Errorf("audio is not configured")
	}
	audio.mutex.Lock()
	defer audio.mutex.Unlock()
	current := audio.sounds[handle]
	if current == nil {
		return fmt.Errorf("audio handle %d is not loaded", handle)
	}
	if count == 0 || count < -1 {
		return fmt.Errorf("audio loop count is invalid")
	}
	if current.transient && count != 1 {
		return fmt.Errorf("a transient sound cannot repeat")
	}
	current.repeat, current.remaining = count == -1, max(count-1, 0)
	return nil
}

// Pause freezes a clip's guest cursor and unscaled output position. Its notes
// and PCM remain within the same bounded output bookkeeping while inaudible.
// Repeated Pause calls do not move the saved position. A completed score can
// still have a PCM tail, which is paused along with the clip.
func (audio *Audio) Pause(handle AudioHandle, now time.Duration) error {
	if audio == nil {
		return fmt.Errorf("audio is not configured")
	}
	audio.mutex.Lock()
	defer audio.mutex.Unlock()
	current, ok := audio.sounds[handle]
	if !ok {
		return fmt.Errorf("audio handle %d is not loaded", handle)
	}
	if current.transient {
		return fmt.Errorf("transient audio cannot be paused")
	}
	if current.startedAt < 0 || now < 0 || now < current.startedAt || current.cursor > 0 && now-current.startedAt < time.Duration(current.events[current.cursor-1].Time)*time.Millisecond {
		return fmt.Errorf("audio pause clock is invalid")
	}
	if current.paused {
		return nil
	}
	audio.advanceSounds(now)
	hostNow := audio.sink.currentTime()
	current.paused, current.pausedAt = true, now
	if _, exists := audio.sink.soundChannels[handle]; !exists {
		audio.sink.setGain(handle, audio.soundGain(current))
	}
	audio.sink.pauseSound(handle, hostNow)
	return nil
}

// Resume continues a paused clip without changing its repeat mode or replaying
// past score events. Repeated Resume calls leave already running output alone.
func (audio *Audio) Resume(handle AudioHandle, now time.Duration) error {
	if audio == nil {
		return fmt.Errorf("audio is not configured")
	}
	audio.mutex.Lock()
	defer audio.mutex.Unlock()
	current, ok := audio.sounds[handle]
	if !ok {
		return fmt.Errorf("audio handle %d is not loaded", handle)
	}
	if !current.paused {
		return nil
	}
	delay := now - current.pausedAt
	if now < current.pausedAt || delay < 0 || current.startedAt > time.Duration(math.MaxInt64)-current.length-delay {
		return fmt.Errorf("audio resume clock is invalid")
	}
	hostNow := audio.sink.currentTime()
	if hostNow.Before(audio.sink.paused[handle]) {
		return fmt.Errorf("audio output clock moved backward")
	}
	// A resumed voice is newer than every score event already due. Merge
	// those events before applying the shared active-voice budget on resume.
	audio.advanceSounds(now)
	hostNow = audio.sink.currentTime()
	current.startedAt += delay
	current.paused, current.pausedAt = false, 0
	audio.sink.resumeSound(handle, hostNow)
	return nil
}

// Paused distinguishes a retained cursor from an explicit stop or natural end.
func (audio *Audio) Paused(handle AudioHandle) bool {
	if audio == nil {
		return false
	}
	audio.mutex.Lock()
	defer audio.mutex.Unlock()
	current := audio.sounds[handle]
	return current != nil && current.paused
}

func (output *audioOutput) ownerTime(sound AudioHandle, now time.Time) time.Time {
	if paused, ok := output.paused[sound]; ok {
		return paused
	}
	return now
}

func (output *audioOutput) pauseSound(sound AudioHandle, now time.Time) {
	output.paused[sound] = now
	if sink, ok := output.sink.(OwnedAudioSink); ok {
		sink.StopSound(sound)
	} else if output.sink != nil {
		for _, note := range output.notes {
			if note.Sound == sound {
				output.destination(sound).MIDINoteOff(note.Channel, note.Note, 0)
			}
		}
	}
}

func (output *audioOutput) resumeSound(sound AudioHandle, now time.Time) {
	paused, ok := output.paused[sound]
	if !ok {
		return
	}
	delay := now.Sub(paused)
	output.pruneNotes(now)
	// The page creates resumed sources after its other active voices. Keep
	// the same order so both ends steal the same oldest voice at capacity.
	var resumed []audioOutputNote
	output.notes = slices.DeleteFunc(output.notes, func(note audioOutputNote) bool {
		if note.Sound != sound {
			return false
		}
		note.started, note.ends = note.started.Add(delay), note.ends.Add(delay)
		resumed = append(resumed, note)
		return true
	})
	for i := range output.waves {
		if output.waves[i].Sound == sound {
			output.waves[i].started = output.waves[i].started.Add(delay)
		}
	}
	delete(output.paused, sound)
	for _, note := range resumed {
		output.appendNote(note)
	}
	output.replaySound(sound, now)
}
