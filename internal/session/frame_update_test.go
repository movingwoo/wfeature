package session

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestFrameUpdateDefersWIPIScalingAndTransfersPixels(t *testing.T) {
	for _, test := range []struct {
		platform, archive string
		scale             int
	}{
		{"lgt", "text-input.zip", 3},
		{"skt", "canvas-skt.zip", 1},
	} {
		t.Run(test.platform, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join("..", "platform", test.platform, "testdata", test.archive))
			if err != nil {
				t.Fatal(err)
			}
			running, err := Start(t.Context(), data, Options{Scale: 3, DisableAuthentication: true})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(running.Close)
			frame, ok := running.FrameUpdate()
			if !ok || frame.Width != 240 || frame.Height != 320 || frame.Scale != test.scale {
				t.Fatalf("raw frame: present=%v %dx%d scale=%d", ok, frame.Width, frame.Height, frame.Scale)
			}
			_, width, height, ok := running.Frame()
			if !ok || width != 240*test.scale || height != 320*test.scale {
				t.Fatalf("synchronous frame: present=%v %dx%d", ok, width, height)
			}
			original := bytes.Clone(frame.RGBA)
			frame.RGBA[0] ^= 0xff
			next, ok := running.FrameUpdate()
			if !ok || !bytes.Equal(next.RGBA, original) {
				t.Fatal("retaining or changing a transferred frame changed the session")
			}
			running.Close()
			if _, ok := running.FrameUpdate(); ok {
				t.Fatal("closed session returned a frame")
			}
		})
	}
}
