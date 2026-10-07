package skt

import (
	"slices"
	"sync"

	"github.com/movingwoo/wfeature/internal/backend"
)

// A frame being drawn can differ from the last picture the Host presented.
// Keeping that boundary here also covers guest-thread flushGraphics calls.
type javaCheckpointFramebuffer struct {
	mu            sync.Mutex
	output        backend.Framebuffer
	width, height int
	rgba          []byte
	presents      uint64
}

func (fb *javaCheckpointFramebuffer) Dimensions() (int, int) { return fb.width, fb.height }
func (fb *javaCheckpointFramebuffer) Present(frame backend.Frame) error {
	fb.mu.Lock()
	defer fb.mu.Unlock()
	if fb.output != nil {
		if err := fb.output.Present(frame); err != nil {
			return err
		}
	}
	fb.rgba = append(fb.rgba[:0], frame.RGBA...)
	fb.presents++
	return nil
}
func (fb *javaCheckpointFramebuffer) snapshot() (backend.Frame, uint64) {
	fb.mu.Lock()
	defer fb.mu.Unlock()
	return backend.Frame{Width: fb.width, Height: fb.height, RGBA: slices.Clone(fb.rgba)}, fb.presents
}
