package wipicrt

import "github.com/movingwoo/wfeature/internal/backend"

// ClipState is one WIPI-C clip's Host-side sound data, embedded in a
// WIPICMediaClip (see wipic_media.go). A platform's Java Clip carries the same
// data beside the object that owns it.
type ClipState struct {
	Data []byte
	// Handle is the loaded sound, claimed on first play and dropped whenever
	// the data changes so a refilled clip reloads rather than replaying.
	Handle backend.AudioHandle
	Loaded bool
	// Played records that the player has taken what the clip held, so bytes
	// already played are no longer in the buffer for the next fill.
	Played    bool
	Completed uint64
}
