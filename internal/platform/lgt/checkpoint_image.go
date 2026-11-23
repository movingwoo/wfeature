package lgt

import (
	"fmt"

	"github.com/movingwoo/wfeature/internal/backend"
)

// Earlier checkpoints contain the heap but no evidence that an image owns a
// particular allocation. Keep those images unowned when loading an old slot.
const sessionImageCheckpointVersion = 8

type imageSourceState struct {
	Image, Source uint32
}

type sessionImageCheckpointState struct {
	Version    uint32
	Session    sessionCheckpointState
	Media      *javaMediaState
	Library    *javaLibraryState
	Screen     *javaScreenState
	Scheduling *javaSchedulingState
	Sources    []imageSourceState
}

func (client *Client) captureImageSources(surfaces []surfaceState) []imageSourceState {
	var sources []imageSourceState
	for _, surface := range surfaces { // already ordered by handle
		if source := client.framebuffers[surface.Handle].source; source != nil && source.address != 0 {
			sources = append(sources, imageSourceState{surface.Handle, source.address})
		}
	}
	return sources
}

func (saved sessionImageCheckpointState) encode() ([]byte, error) {
	if len(saved.Sources) == 0 {
		return encodeLGTSchedulingRecord(saved.Session, saved.Media, saved.Library, saved.Screen, saved.Scheduling)
	}
	saved.Version = sessionImageCheckpointVersion
	return backend.EncodeCheckpointRecord(saved)
}

func decodeLGTImageRecord(data []byte) (sessionImageCheckpointState, error) {
	var saved sessionImageCheckpointState
	if version, ok := leadingRecordVersion(data); ok && version == sessionImageCheckpointVersion {
		if err := backend.DecodeCheckpointRecord(data, &saved); err != nil {
			return saved, err
		}
		if len(saved.Sources) == 0 || len(saved.Sources) > len(saved.Session.Client.Framebuffers) {
			return saved, fmt.Errorf("LGT checkpoint has an invalid image source count")
		}
		return saved, nil
	}
	var err error
	saved.Session, saved.Media, saved.Library, saved.Screen, saved.Scheduling, err = decodeLGTSchedulingRecord(data)
	return saved, err
}

func (client *Client) restoreImageSources(sources []imageSourceState) error {
	var previous uint32
	for _, entry := range sources {
		image := client.framebuffer(entry.Image)
		if entry.Image <= previous || image == nil || image.screen || client.heap.sizes[entry.Source] == 0 {
			return fmt.Errorf("LGT checkpoint has an invalid image source for handle %#x", entry.Image)
		}
		client.retainImageSource(image, entry.Source)
		previous = entry.Image
	}
	return nil
}
