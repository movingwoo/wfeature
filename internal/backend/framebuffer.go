package backend

import (
	"fmt"
	"sync"
)

const MaxFramebufferPixels = 16 * 1024 * 1024

// Frame is a complete RGBA8888 image produced by the runtime. RGBA is only
// valid for the duration of Present; framebuffer implementations that retain
// it must copy the bytes.
type Frame struct {
	Width  int
	Height int
	RGBA   []byte
}

// Framebuffer is the Host-owned presentation boundary used by platform
// runtimes. Browser and native hosts provide the concrete implementation.
type Framebuffer interface {
	Dimensions() (width, height int)
	Present(Frame) error
}

// ValidateFramebuffer checks dimensions supplied by a Host before untrusted
// guest code can use them to allocate or index a frame.
func ValidateFramebuffer(framebuffer Framebuffer) (width, height int, err error) {
	if framebuffer == nil {
		return 0, 0, fmt.Errorf("framebuffer is nil")
	}
	width, height = framebuffer.Dimensions()
	if _, err := RGBAByteLength(width, height); err != nil {
		return 0, 0, err
	}
	return width, height, nil
}

// RGBAByteLength validates framebuffer dimensions and returns the exact byte
// count for a packed RGBA8888 frame without overflowing int arithmetic.
func RGBAByteLength(width, height int) (int, error) {
	if width <= 0 || height <= 0 {
		return 0, fmt.Errorf("framebuffer dimensions must be positive: %dx%d", width, height)
	}
	if width > MaxFramebufferPixels/height {
		return 0, fmt.Errorf("framebuffer %dx%d exceeds pixel limit %d", width, height, MaxFramebufferPixels)
	}
	return width * height * 4, nil
}

// MemoryFramebuffer is a thread-safe Host framebuffer used by the CLI and
// tests. Snapshot returns copies so callers cannot mutate a presented frame.
type MemoryFramebuffer struct {
	mu       sync.RWMutex
	width    int
	height   int
	rgba     []byte
	presents uint64
}

func NewMemoryFramebuffer(width, height int) (*MemoryFramebuffer, error) {
	length, err := RGBAByteLength(width, height)
	if err != nil {
		return nil, err
	}
	return &MemoryFramebuffer{
		width:  width,
		height: height,
		rgba:   make([]byte, length),
	}, nil
}

func (framebuffer *MemoryFramebuffer) Dimensions() (int, int) {
	return framebuffer.width, framebuffer.height
}

func (framebuffer *MemoryFramebuffer) Present(frame Frame) error {
	if frame.Width != framebuffer.width || frame.Height != framebuffer.height {
		return fmt.Errorf("frame dimensions %dx%d do not match framebuffer %dx%d", frame.Width, frame.Height, framebuffer.width, framebuffer.height)
	}
	length, err := RGBAByteLength(frame.Width, frame.Height)
	if err != nil {
		return err
	}
	if len(frame.RGBA) != length {
		return fmt.Errorf("RGBA frame length is %d, want %d", len(frame.RGBA), length)
	}

	framebuffer.mu.Lock()
	copy(framebuffer.rgba, frame.RGBA)
	framebuffer.presents++
	framebuffer.mu.Unlock()
	return nil
}

func (framebuffer *MemoryFramebuffer) Snapshot() (Frame, uint64) {
	framebuffer.mu.RLock()
	defer framebuffer.mu.RUnlock()
	rgba := append([]byte(nil), framebuffer.rgba...)
	return Frame{Width: framebuffer.width, Height: framebuffer.height, RGBA: rgba}, framebuffer.presents
}
