package clet

import "github.com/movingwoo/wfeature/internal/backend"

// clipState is one WIPI-C clip's Host-side sound data, embedded in a
// wipicMediaClip (see wipic_media.go). A Clet has no Java Clip object, so the
// owner a KTF clip carried to keep the object alive is gone.
type clipState struct {
	data []byte
	// handle is the loaded sound, claimed on first play and dropped whenever
	// the data changes so a refilled clip reloads rather than replaying.
	handle backend.AudioHandle
	loaded bool
	// played records that the player has taken what the clip held, so bytes
	// already played are no longer in the buffer for the next fill.
	played    bool
	completed uint64
}
