package ktf

import (
	"cmp"
	"fmt"
	"slices"
	"weak"

	"github.com/movingwoo/wfeature/internal/api/wipi"
	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/jvm"
)

const (
	heapMediaRoot = "$wfeature.media.v1"
	heapMediaKind = "ktf-media-v1"
	maxMediaClips = 256
)

// A separate native kind extends the strict heap record without adding fields
// to the previous root or Clip shapes. References are Clip owners, followed by
// one Clip/listener pair per event. The temporary carrier is never adopted.
type heapMediaState struct {
	Clips  []heapMediaClip
	Events []int32
}
type heapMediaClip struct {
	Handle    backend.AudioHandle
	Completed uint64
	Active    bool
}
type heapMediaPayload struct {
	state      heapMediaState
	references []*jvm.Object
}

func parseHeapMedia(data []byte, referenceCount int) (heapMediaState, error) {
	var state heapMediaState
	if len(data) > 1<<20 {
		return state, fmt.Errorf("KTF media checkpoint data exceeds limit")
	}
	if err := backend.DecodeCheckpointRecord(data, &state); err != nil {
		return state, err
	}
	if len(state.Clips) > maxMediaClips || len(state.Events) > maxClipEvents || referenceCount != len(state.Clips)+2*len(state.Events) {
		return state, fmt.Errorf("KTF media checkpoint count is invalid")
	}
	for i, clip := range state.Clips {
		if clip.Handle == 0 || i > 0 && state.Clips[i-1].Handle >= clip.Handle {
			return state, fmt.Errorf("KTF media checkpoint handles are invalid")
		}
	}
	for _, code := range state.Events {
		if code < wipi.PlayEventEndOfData || code > wipi.PlayEventResume {
			return state, fmt.Errorf("KTF media checkpoint event code is invalid")
		}
	}
	return state, nil
}

func (runtime *initializationRuntime) captureMediaRoots() (*jvm.Object, error) {
	if len(runtime.mediaEvents) > maxClipEvents {
		return nil, fmt.Errorf("KTF media event queue exceeds limit")
	}
	type ownerState struct {
		object *jvm.Object
		state  *clipState
	}
	var loaded []ownerState
	for key, state := range runtime.clips {
		object := key.Value()
		if object == nil || state == nil || state.owner != nil && state.owner != object {
			return nil, fmt.Errorf("KTF media Clip owner is invalid or requires collection")
		}
		listener, err := clipListener(object)
		if err != nil {
			return nil, err
		}
		if err := runtime.validateClipListener(listener); err != nil {
			return nil, err
		}
		if err := runtime.validateClipObject(object, listener != nil); err != nil {
			return nil, err
		}
		if state.loaded {
			loaded = append(loaded, ownerState{object, state})
		} else if state.owner != nil || state.completed != 0 {
			return nil, fmt.Errorf("KTF unloaded Clip retains playback state")
		}
	}
	if len(loaded) > maxMediaClips {
		return nil, fmt.Errorf("KTF loaded media Clip count exceeds limit")
	}
	if len(loaded) == 0 && len(runtime.mediaEvents) == 0 {
		return nil, nil
	}
	slices.SortFunc(loaded, func(a, b ownerState) int { return cmp.Compare(a.state.handle, b.state.handle) })
	payload := &heapMediaPayload{}
	for _, clip := range loaded {
		active := clip.state.owner != nil
		if audio := runtime.client.audio; audio != nil {
			active = audio.Playing(clip.state.handle) || audio.Paused(clip.state.handle)
		}
		payload.state.Clips = append(payload.state.Clips, heapMediaClip{clip.state.handle, clip.state.completed, active})
		payload.references = append(payload.references, clip.object)
	}
	for _, event := range runtime.mediaEvents {
		if event.clip == nil || event.listener == nil || runtime.clips[weak.Make(event.clip)] == nil {
			return nil, fmt.Errorf("KTF queued media event has no Clip owner or recipient")
		}
		if err := runtime.validateClipObject(event.clip, true); err != nil {
			return nil, err
		}
		if err := runtime.validateClipListener(event.listener); err != nil {
			return nil, err
		}
		payload.state.Events = append(payload.state.Events, event.code)
		payload.references = append(payload.references, event.clip, event.listener)
	}
	return &jvm.Object{ClassName: jvm.ObjectClass, Native: payload}, nil
}

func captureHeapMedia(payload *heapMediaPayload) (jvm.HeapExternalPayload, error) {
	data, err := backend.EncodeCheckpointRecord(payload.state)
	if err != nil {
		return jvm.HeapExternalPayload{}, err
	}
	if _, err := parseHeapMedia(data, len(payload.references)); err != nil {
		return jvm.HeapExternalPayload{}, err
	}
	return jvm.HeapExternalPayload{Kind: heapMediaKind, Data: data, References: slices.Clone(payload.references)}, nil
}

func restoreHeapMedia(payload jvm.HeapExternalPayload) (*heapMediaPayload, error) {
	state, err := parseHeapMedia(payload.Data, len(payload.References))
	if err != nil {
		return nil, err
	}
	for _, object := range payload.References {
		if object == nil {
			return nil, fmt.Errorf("KTF media checkpoint has a null reference")
		}
	}
	return &heapMediaPayload{state, slices.Clone(payload.References)}, nil
}

// Validate numeric ownership before using restored objects. The optional audio
// state is supplied by complete client restores; heap-only fixtures have none.
func validateMediaHeap(saved runtimeHeapState, audio *backend.AudioState) error {
	heap := &saved.JVM
	payloadID := uint32(0)
	for _, binding := range saved.Roots.Objects {
		if string(binding.Name) != heapMediaRoot {
			continue
		}
		if payloadID != 0 || binding.Root == 0 || int(binding.Root) > len(heap.Roots) {
			return fmt.Errorf("KTF media carrier root is invalid")
		}
		id := heap.Roots[binding.Root-1]
		if id == 0 || int(id) > len(heap.Objects) || heap.Objects[id-1].Class != jvm.ObjectClass {
			return fmt.Errorf("KTF media carrier object is invalid")
		}
		payloadID = heap.Objects[id-1].Native
		if payloadID == 0 || int(payloadID) > len(heap.Payloads) || heap.Payloads[payloadID-1].Kind != "external" || heap.Payloads[payloadID-1].ExternalKind != heapMediaKind {
			return fmt.Errorf("KTF media carrier payload is invalid")
		}
	}
	for i, payload := range heap.Payloads {
		if payload.ExternalKind == heapMediaKind && uint32(i+1) != payloadID {
			return fmt.Errorf("KTF media payload has no unique carrier root")
		}
	}
	if payloadID == 0 {
		return nil // Previous records did not have Java playback callbacks.
	}
	payload := heap.Payloads[payloadID-1]
	state, err := parseHeapMedia(payload.Data, len(payload.References))
	if err != nil {
		return err
	}
	for _, ref := range payload.References {
		if ref == 0 || int(ref) > len(heap.Objects) {
			return fmt.Errorf("KTF media reference is invalid")
		}
	}
	owners := make(map[uint32]heapClip)
	loaded := 0
	for _, clip := range saved.Roots.Clips {
		if clip.Owner == 0 || int(clip.Owner) > len(heap.Roots) {
			return fmt.Errorf("KTF media Clip root is invalid")
		}
		owners[heap.Roots[clip.Owner-1]] = clip
		if clip.Loaded {
			loaded++
		}
	}
	if loaded != len(state.Clips) {
		return fmt.Errorf("KTF media record does not cover loaded Clips")
	}
	sounds := make(map[backend.AudioHandle]backend.AudioSoundState)
	if audio != nil {
		for _, sound := range audio.Sounds {
			sounds[sound.Handle] = sound
		}
	}
	seen := make(map[uint32]bool)
	for i, record := range state.Clips {
		owner := payload.References[i]
		clip, ok := owners[owner]
		if !ok || !clip.Loaded || clip.Handle != record.Handle || seen[owner] {
			return fmt.Errorf("KTF media record has a different Clip owner")
		}
		seen[owner] = true
		if audio != nil {
			sound, ok := sounds[record.Handle]
			// Every Java playback advance reconciles the counter under the
			// guest lock. Outstanding notifications belong in the saved queue,
			// never in an implicit gap that could overflow on the first tick.
			if !ok || record.Completed != sound.Completed || record.Active != (sound.Playing || sound.Paused) {
				return fmt.Errorf("KTF media progress differs from saved audio")
			}
		}
	}
	for i := range state.Events {
		if _, ok := owners[payload.References[len(state.Clips)+2*i]]; !ok {
			return fmt.Errorf("KTF media event references an unknown Clip")
		}
	}
	return nil
}

func (runtime *initializationRuntime) prepareMediaRoots(saved runtimeHeapRoots, roots []*jvm.Object, audio *backend.AudioState) (*heapMediaPayload, error) {
	object := func(root uint32) *jvm.Object { return roots[root-1] }
	var payload *heapMediaPayload
	for _, binding := range saved.Objects {
		if string(binding.Name) == heapMediaRoot {
			payload = object(binding.Root).Native.(*heapMediaPayload)
		}
	}
	for _, clip := range saved.Clips {
		listener, err := clipListener(object(clip.Owner))
		if err != nil {
			return nil, err
		}
		if err := runtime.validateClipListener(listener); err != nil {
			return nil, err
		}
		if err := runtime.validateClipObject(object(clip.Owner), listener != nil); err != nil {
			return nil, err
		}
	}
	if payload == nil {
		payload = &heapMediaPayload{}
		if audio != nil {
			sounds := make(map[backend.AudioHandle]backend.AudioSoundState)
			for _, sound := range audio.Sounds {
				sounds[sound.Handle] = sound
			}
			for _, clip := range saved.Clips {
				if clip.Loaded {
					sound := sounds[clip.Handle]
					payload.state.Clips = append(payload.state.Clips, heapMediaClip{clip.Handle, sound.Completed, sound.Playing || sound.Paused})
					payload.references = append(payload.references, object(clip.Owner))
				}
			}
		}
	}
	for i := range payload.state.Events {
		clip := payload.references[len(payload.state.Clips)+2*i]
		if err := runtime.validateClipObject(clip, true); err != nil {
			return nil, err
		}
		listener := payload.references[len(payload.state.Clips)+2*i+1]
		if err := runtime.validateClipListener(listener); err != nil {
			return nil, err
		}
	}
	return payload, nil
}

func (runtime *initializationRuntime) adoptMediaRoots(payload *heapMediaPayload) {
	for i, record := range payload.state.Clips {
		object := payload.references[i]
		state := runtime.clips[weak.Make(object)]
		state.completed = record.Completed
		if record.Active {
			state.owner = object
		}
	}
	runtime.mediaEvents = nil
	for i, code := range payload.state.Events {
		refs := payload.references[len(payload.state.Clips)+2*i:]
		runtime.mediaEvents = append(runtime.mediaEvents, clipEvent{refs[0], refs[1], code})
	}
}
