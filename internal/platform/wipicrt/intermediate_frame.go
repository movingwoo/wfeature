package wipicrt

import (
	"bytes"
	"time"

	"github.com/movingwoo/wfeature/internal/backend"
)

// SetFrameSink changes the optional Host queue only at an execution boundary.
func (client *Client) SetFrameSink(sink backend.FrameSink) {
	client.Run.Lock()
	defer client.Run.Unlock()
	client.FrameSink = sink
	client.LastSample = nil
	client.FrameSampleAt = time.Time{}
}

// publishIntermediateFrame runs at an explicit LCD boundary while execution
// still owns the run lock. No Host call back into Client.Frame is necessary.
func (runtime *Runtime) publishIntermediateFrame() error {
	client := runtime.Client
	if client.FrameSink.Output == nil || runtime.ScreenFramebuffer == 0 {
		return nil
	}
	now := time.Now()
	if client.FrameSampleClock != nil {
		now = client.FrameSampleClock()
	}
	if !client.FrameSampleAt.IsZero() && now.Sub(client.FrameSampleAt) < time.Second/60 {
		return nil
	}
	client.FrameSampleAt = now
	if client.FramePending {
		if err := runtime.ConvertScreen(); err != nil {
			return err
		}
	}
	if bytes.Equal(client.LastSample, client.FrameRGBA) {
		return nil
	}
	owned := append([]byte(nil), client.FrameRGBA...)
	if client.FrameSink.Offer(backend.FrameUpdate{RGBA: owned, Width: client.FrameWidth, Height: client.FrameHeight}) {
		// The receiver owns its copy and may modify it. The comparison image
		// belongs to the runtime, independently of both receiver and converter.
		client.LastSample = append(client.LastSample[:0], client.FrameRGBA...)
	}
	return nil
}
