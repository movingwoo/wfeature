package lgt

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"time"

	"github.com/movingwoo/wfeature/internal/api/wipi"
)

const maxJavaMediaEvents = 4096

type javaMediaEvent struct {
	clip, listener uint32
	code           int32
}

// The recipient belongs to the transition, even if setListener changes before
// delivery. Guest execution and the between-frame services are serialized by
// the cooperative scheduler; no guest code is entered while client.mu is held.
func (client *Client) queueJavaMediaEvent(object uint32, clip *mediaClip, code int32) error {
	if !clip.java || clip.listener == 0 {
		return nil
	}
	if len(client.javaMediaEvents) >= maxJavaMediaEvents {
		return fmt.Errorf("LGT Java media queue exceeds %d events", maxJavaMediaEvents)
	}
	client.javaMediaEvents = append(client.javaMediaEvents, javaMediaEvent{object, clip.listener, code})
	return nil
}

func (client *Client) syncJavaMediaClip(object uint32, clip *mediaClip) error {
	if !clip.java || !clip.loaded || client.audio == nil {
		return nil
	}
	progress, err := client.audio.PlaybackState(clip.handle)
	if err != nil {
		return err
	}
	if progress.Completed < clip.completed {
		return fmt.Errorf("LGT Java media completion count moved backwards")
	}
	if clip.listener != 0 {
		count := progress.Completed - clip.completed
		if count > uint64(maxJavaMediaEvents-len(client.javaMediaEvents)) {
			return fmt.Errorf("LGT Java media completions exceed the event queue")
		}
		for ; count > 0; count-- {
			client.javaMediaEvents = append(client.javaMediaEvents, javaMediaEvent{object, clip.listener, wipi.PlayEventEndOfData})
		}
	}
	clip.completed = progress.Completed
	return nil
}

func (client *Client) syncJavaMedia(now time.Duration) error {
	if client.audio != nil {
		client.audio.Advance(now)
	}
	for _, object := range slices.Sorted(maps.Keys(client.clips)) {
		if err := client.syncJavaMediaClip(object, client.clips[object]); err != nil {
			return err
		}
	}
	return nil
}

// A detached batch has no guest-memory owner. Pin every pair before the first
// call, since that callback may allocate, collect, or replace later recipients.
func (client *Client) takeJavaMediaEvents() ([]javaMediaEvent, int) {
	mark := client.javaPinMark()
	due := client.javaMediaEvents
	if len(due) != 0 {
		for _, event := range due {
			client.javaRun.pins = append(client.javaRun.pins, event.clip, event.listener)
		}
		client.javaMediaEvents = nil
	}
	return due, mark
}

func (client *Client) deliverJavaMediaEvents(ctx context.Context, due []javaMediaEvent) error {
	for _, event := range due {
		client.mu.Lock()
		body, err := client.javaMediaListenerBody(event.listener)
		client.mu.Unlock()
		if err != nil {
			return err
		}
		_, err = client.call(ctx, body, []uint32{event.listener, event.clip, uint32(event.code), 0})
		if err = client.absorbUncaughtCallback("PlayListener.playUpdate", err); err != nil {
			return fmt.Errorf("run LGT Java media callback: %w", err)
		}
	}
	return nil
}

// nextJavaMediaDue prevents a long frame interval from delaying a listener
// that drives the application. The clock is already in guest time on LGT.
func (client *Client) nextJavaMediaDue() (time.Duration, bool) {
	client.mu.Lock()
	defer client.mu.Unlock()
	if len(client.javaMediaEvents) != 0 {
		return 0, true
	}
	var wait time.Duration
	pending := false
	now := client.clock.now()
	for _, clip := range client.clips {
		if !clip.java || clip.listener == 0 || !clip.loaded {
			continue
		}
		end, ok := client.audio.NextCompletion(clip.handle)
		if !ok {
			// A zero-duration one-shot still owes END_OF_DATA on its next
			// advance; paused and stopped clips have no running deadline.
			if !client.audio.Playing(clip.handle) {
				continue
			}
			end = now
		}
		if delta := max(end-now, 0); !pending || delta < wait {
			wait, pending = delta, true
		}
	}
	return wait, pending
}
