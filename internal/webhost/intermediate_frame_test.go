package webhost

import (
	"bytes"
	"image/png"
	"testing"
	"time"

	"github.com/movingwoo/wfeature/internal/backend"
)

func TestIntermediateQueueUsesTheExistingPNGTransport(t *testing.T) {
	runner := &sessionRunner{server: newTestServer(t, Options{}), frames: make(chan pendingFrame, 1), outFrames: make(chan outboundMessage, 1)}
	done := make(chan struct{})
	go func() { defer close(done); runner.writeFrames(t.Context()) }()
	frame := backend.FrameUpdate{RGBA: []byte{248, 0, 0, 255}, Width: 1, Height: 1}
	if !(backend.FrameSink{Output: runner.frames, Scale: 2}).Offer(frame) {
		t.Fatal("frame not queued")
	}
	select {
	case outgoing := <-runner.outFrames:
		picture, err := png.Decode(bytes.NewReader(outgoing.binary))
		if err != nil {
			t.Fatal(err)
		}
		if picture.Bounds().Dx() != 2 || picture.Bounds().Dy() != 2 {
			t.Fatal("Host scaling was lost")
		}
		r, g, b, a := picture.At(0, 0).RGBA()
		if r != 248*257 || g != 0 || b != 0 || a != 65535 {
			t.Fatal("PNG pixels changed")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("encoder waited for a session tick")
	}
	close(runner.frames)
	<-done
}
