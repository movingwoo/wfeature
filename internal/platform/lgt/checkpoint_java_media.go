package lgt

import (
	"fmt"
	"maps"
	"slices"

	"github.com/movingwoo/wfeature/internal/api/wipi"
	"github.com/movingwoo/wfeature/internal/backend"
)

// Keep the strict version-2 session/client records intact. Version 3 wraps that
// record with Java media ownership and pending recipient snapshots; an older
// reader refuses the new shape instead of silently dropping callbacks.
type sessionMediaCheckpointState struct {
	Version uint32
	Session sessionCheckpointState
	Media   javaMediaState
}

const sessionMediaCheckpointVersion = 3

type javaMediaState struct {
	Clips  []javaMediaClipState
	Events []javaMediaEventState
}

type javaMediaClipState struct {
	Object    uint32
	Completed uint64
}

type javaMediaEventState struct {
	Clip, Listener uint32
	Code           int32
}

func decodeLGTSessionRecord(data []byte) (sessionCheckpointState, *javaMediaState, error) {
	var extended sessionMediaCheckpointState
	if err := backend.DecodeCheckpointRecord(data, &extended); err == nil {
		if extended.Version != sessionMediaCheckpointVersion {
			return sessionCheckpointState{}, nil, fmt.Errorf("LGT Java media checkpoint version %d: %w", extended.Version, backend.ErrCheckpointVersion)
		}
		return extended.Session, &extended.Media, nil
	}
	// The codec validates the entire schema before allocating the decoded
	// destination. Trying the two disjoint strict shapes cannot partially
	// restore a record, accept unknown fields, or double its decoded payload.
	var legacy sessionCheckpointState
	if err := backend.DecodeCheckpointRecord(data, &legacy); err != nil {
		return sessionCheckpointState{}, nil, err
	}
	return legacy, nil, nil
}

func (client *Client) captureJavaMediaState(audio backend.AudioState) (*javaMediaState, error) {
	saved := &javaMediaState{}
	for _, object := range slices.Sorted(maps.Keys(client.clips)) {
		if clip := client.clips[object]; clip.java {
			saved.Clips = append(saved.Clips, javaMediaClipState{object, clip.completed})
		}
	}
	for _, event := range client.javaMediaEvents {
		saved.Events = append(saved.Events, javaMediaEventState{event.clip, event.listener, event.code})
	}
	if err := client.validateJavaMediaState(saved, audio); err != nil {
		return nil, err
	}
	if len(saved.Clips) == 0 && len(saved.Events) == 0 {
		return nil, nil
	}
	return saved, nil
}

func (client *Client) restoreJavaMediaState(saved *javaMediaState, audio backend.AudioState) error {
	if saved == nil {
		// Version 2 has neither watermarks nor event snapshots. Recover only
		// issued Clip objects, start at the recorded completion count, and do
		// not invent historical notifications for old playback.
		saved = &javaMediaState{}
		completed := make(map[backend.AudioHandle]uint64, len(audio.Sounds))
		for _, sound := range audio.Sounds {
			completed[sound.Handle] = sound.Completed
		}
		for _, object := range slices.Sorted(maps.Keys(client.clips)) {
			clip := client.clips[object]
			if client.javaRun == nil {
				continue
			}
			if _, issued := client.javaRun.objects[object]; !issued {
				continue
			}
			if err := client.validateJavaMediaClip(object); err != nil {
				return err
			}
			entry := javaMediaClipState{Object: object}
			if clip.loaded {
				entry.Completed = completed[clip.handle]
			}
			saved.Clips = append(saved.Clips, entry)
		}
	}
	if err := client.validateJavaMediaState(saved, audio); err != nil {
		return err
	}
	for _, entry := range saved.Clips {
		clip := client.clips[entry.Object]
		clip.java, clip.completed = true, entry.Completed
	}
	client.javaMediaEvents = make([]javaMediaEvent, len(saved.Events))
	for index, event := range saved.Events {
		client.javaMediaEvents[index] = javaMediaEvent{event.Clip, event.Listener, event.Code}
	}
	return nil
}

// All validation precedes worker startup, device attachment, or save reads.
// Audio is the captured record, so checking a watermark cannot advance a score.
func (client *Client) validateJavaMediaState(saved *javaMediaState, audio backend.AudioState) error {
	invalid := func(what string) error { return fmt.Errorf("LGT Java media checkpoint has %s", what) }
	if len(saved.Clips) > maxStateRecords || len(saved.Events) > maxJavaMediaEvents {
		return invalid("too many clips or events")
	}
	sounds := make(map[backend.AudioHandle]backend.AudioSoundState, len(audio.Sounds))
	for _, sound := range audio.Sounds {
		sounds[sound.Handle] = sound
	}
	owners := make(map[uint32]bool, len(saved.Clips))
	listeners := map[uint32]bool{}
	checkListener := func(object uint32) error {
		if object == 0 || listeners[object] {
			return nil
		}
		if _, err := client.javaMediaListenerBody(object); err != nil {
			return err
		}
		listeners[object] = true
		return nil
	}
	for index, entry := range saved.Clips {
		if entry.Object == 0 || index > 0 && saved.Clips[index-1].Object >= entry.Object {
			return invalid("unordered or duplicate Clip owners")
		}
		clip := client.clips[entry.Object]
		if clip == nil {
			return invalid("a missing Clip owner")
		}
		if err := client.validateJavaMediaClip(entry.Object); err != nil {
			return err
		}
		if clip.callback != 0 || clip.status != 0 || len(clip.pending) != 0 {
			return invalid("a Java Clip with C callback state")
		}
		if err := checkListener(clip.listener); err != nil {
			return err
		}
		if clip.loaded {
			sound, exists := sounds[clip.handle]
			if !exists || sound.Transient || sound.Remaining != 0 || sound.Completed != entry.Completed ||
				sound.Paused != clip.javaPaused || sound.Repeat != clip.javaRepeat || sound.Volume != int(clip.volume) {
				return invalid("a Clip that disagrees with its audio state")
			}
		} else if entry.Completed != 0 || clip.javaPaused || clip.javaRepeat {
			return invalid("playback state on an unloaded Clip")
		}
		owners[entry.Object] = true
	}
	// Omitting an owner must not turn an issued Clip into an uncollected C
	// allocation, and two clips must not both own the same mixer handle.
	soundOwners := map[backend.AudioHandle]uint32{}
	for object, clip := range client.clips {
		issued := false
		if client.javaRun != nil {
			_, issued = client.javaRun.objects[object]
		}
		if (clip.java || issued || clip.listener != 0) && !owners[object] {
			return invalid("an unrecorded Java Clip")
		}
		if clip.loaded {
			if previous, exists := soundOwners[clip.handle]; exists && (owners[previous] || owners[object]) {
				return invalid("a shared audio owner")
			}
			soundOwners[clip.handle] = object
		}
	}
	for _, event := range saved.Events {
		if !owners[event.Clip] || event.Listener == 0 || event.Code < wipi.PlayEventEndOfData || event.Code > wipi.PlayEventResume {
			return invalid("an invalid pending playback event")
		}
		if err := checkListener(event.Listener); err != nil {
			return err
		}
	}
	return nil
}
