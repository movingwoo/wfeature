package ktf

import (
	"bytes"
	"time"

	"github.com/movingwoo/wfeature/internal/backend"
)

// SetFrameSink changes the optional Host queue only at an execution boundary.
func (client *Client) SetFrameSink(sink backend.FrameSink) {
	client.run.Lock()
	defer client.run.Unlock()
	client.frameSink = sink
	client.lastSample = nil
	client.frameSampleAt = time.Time{}
}

// publishIntermediateFrame runs at an explicit LCD boundary while execution
// still owns the run lock. No Host call back into Client.Frame is necessary.
func (runtime *initializationRuntime) publishIntermediateFrame() error {
	client := runtime.client
	if client.frameSink.Output == nil || runtime.screenFramebuffer == 0 {
		return nil
	}
	now := time.Now()
	if client.frameSampleClock != nil {
		now = client.frameSampleClock()
	}
	if !client.frameSampleAt.IsZero() && now.Sub(client.frameSampleAt) < time.Second/60 {
		return nil
	}
	client.frameSampleAt = now
	if client.framePending {
		if err := runtime.convertScreen(); err != nil {
			return err
		}
	}
	if bytes.Equal(client.lastSample, client.frame) {
		return nil
	}
	owned := append([]byte(nil), client.frame...)
	if client.frameSink.Offer(backend.FrameUpdate{RGBA: owned, Width: client.frameWidth, Height: client.frameHeight}) {
		// The receiver owns its copy and may modify it. The comparison image
		// belongs to the runtime, independently of both receiver and converter.
		client.lastSample = append(client.lastSample[:0], client.frame...)
	}
	return nil
}
