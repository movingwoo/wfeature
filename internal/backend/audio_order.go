package backend

import (
	"container/heap"
	"time"
)

// Only due events enter this heap: their absolute deadlines cannot overflow
// because they have already been compared against now using elapsed offsets.
// Equal deadlines use handle order, then the original order within each score.
type dueAudio []*sound

func (due dueAudio) Len() int { return len(due) }
func (due dueAudio) Less(i, j int) bool {
	left, right := due[i], due[j]
	a := left.startedAt + time.Duration(left.events[left.cursor].Time)*time.Millisecond
	b := right.startedAt + time.Duration(right.events[right.cursor].Time)*time.Millisecond
	return a < b || a == b && left.handle < right.handle
}
func (due dueAudio) Swap(i, j int)   { due[i], due[j] = due[j], due[i] }
func (due *dueAudio) Push(value any) { *due = append(*due, value.(*sound)) }
func (due *dueAudio) Pop() any {
	last := len(*due) - 1
	current := (*due)[last]
	(*due)[last] = nil
	*due = (*due)[:last]
	return current
}

func soundDue(current *sound, now time.Duration) bool {
	return current.playing && !current.paused && current.cursor < len(current.events) &&
		now >= current.startedAt &&
		time.Duration(current.events[current.cursor].Time)*time.Millisecond <= now-current.startedAt
}

// advanceSounds merges score events before they reach the shared output voice
// budget. Draining one owner's whole backlog first changes which voices survive
// a coarse Host tick. This heap holds at most one entry per loaded sound, even
// when a tick spans many repeats. Audio.mutex serializes all callers.
func (audio *Audio) advanceSounds(now time.Duration) {
	host := audio.advanceOutputClock(now)
	due := make(dueAudio, 0, len(audio.sounds))
	for _, current := range audio.sounds {
		if soundDue(current, now) {
			due = append(due, current)
		}
	}
	heap.Init(&due)
	for len(due) != 0 {
		current := due[0]
		event := current.events[current.cursor]
		audio.outputDeadline(current.startedAt+time.Duration(event.Time)*time.Millisecond, now, host)
		current.cursor++
		audio.emit(current, event)
		if current.cursor == len(current.events) {
			current.completed++
			current.position = current.length
			if !current.repeat && current.remaining == 0 || current.length <= 0 {
				audio.silence(current)
				if current.transient {
					audio.sink.stopSound(current.handle)
					delete(audio.sounds, current.handle)
				}
			} else {
				if current.remaining > 0 {
					current.remaining--
				}
				// This pass ended no later than now, so the addition is safe.
				current.startedAt += current.length
				current.cursor, current.position = 0, 0
			}
		}
		if soundDue(current, now) {
			heap.Fix(&due, 0)
		} else {
			heap.Pop(&due)
		}
	}
	for _, current := range audio.sounds {
		if current.playing && !current.paused {
			current.position = min(max(now-current.startedAt, 0), current.length)
		}
	}
	audio.sink.eventAt = nil
	audio.sink.pruneNotes(host)
	audio.sink.pruneWaves(host)
	audio.currentOutputTime(now)
}
