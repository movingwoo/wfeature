package webhost

import (
	"bytes"
	"context"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/movingwoo/wfeature/internal/filter/hqx"
	"github.com/movingwoo/wfeature/internal/session"
	"github.com/movingwoo/wfeature/internal/wsproto"
)

// encodeFrames uses the real encoder queue and drains it through shutdown, so
// an unexpected extra frame fails by count rather than a timing assumption.
func encodeFrames(t *testing.T, frames ...pendingFrame) []image.Image {
	t.Helper()
	runner := &sessionRunner{server: newTestServer(t, Options{}),
		frames: make(chan pendingFrame, len(frames)), outFrames: make(chan outboundMessage, len(frames))}
	for _, frame := range frames {
		runner.frames <- frame
	}
	close(runner.frames)
	runner.writeFrames(t.Context())
	close(runner.outFrames)
	var pictures []image.Image
	for message := range runner.outFrames {
		picture, err := png.Decode(bytes.NewReader(message.binary))
		if err != nil {
			t.Fatal(err)
		}
		pictures = append(pictures, picture)
	}
	return pictures
}

func TestEncoderSkipsOnlyConsecutiveIdenticalPictures(t *testing.T) {
	red := pendingFrame{RGBA: []byte{240, 0, 0, 255}, Width: 1, Height: 1, Scale: 1}
	blue := pendingFrame{RGBA: []byte{0, 0, 240, 255}, Width: 1, Height: 1, Scale: 1}
	copyOfRed := red
	copyOfRed.RGBA = bytes.Clone(red.RGBA)
	pictures := encodeFrames(t, red, copyOfRed, blue, blue, red)
	if len(pictures) != 3 {
		t.Fatalf("encoded %d pictures, want 3 changes", len(pictures))
	}
	for i, want := range [][3]uint32{{240 * 257, 0, 0}, {0, 0, 240 * 257}, {240 * 257, 0, 0}} {
		r, g, b, a := pictures[i].At(0, 0).RGBA()
		if [3]uint32{r, g, b} != want || a != 65535 {
			t.Fatalf("picture %d has wrong pixels", i)
		}
	}
}

func TestEncoderPreservesScaleShapeAndForcedRedraws(t *testing.T) {
	frame := pendingFrame{RGBA: []byte{240, 0, 0, 255, 0, 0, 240, 255}, Width: 2, Height: 1, Scale: 1}
	for _, test := range []struct {
		name          string
		change        func(*pendingFrame)
		width, height int
	}{
		{"scale", func(f *pendingFrame) { f.Scale = 4 }, 8, 4},
		{"shape", func(f *pendingFrame) { f.Width, f.Height = 1, 2 }, 1, 2},
		{"redraw", func(f *pendingFrame) { f.Force = true }, 2, 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			next := frame
			test.change(&next)
			pictures := encodeFrames(t, frame, next)
			if len(pictures) != 2 {
				t.Fatalf("encoded %d pictures, want a new presentation", len(pictures))
			}
			if got := pictures[1].Bounds(); got.Dx() != test.width || got.Dy() != test.height {
				t.Fatalf("redrawn dimensions %v, want %dx%d", got, test.width, test.height)
			}
			if next.Scale > 1 {
				pixels, width, height, err := hqx.ScaleRGBA(next.RGBA, next.Width, next.Height, next.Scale)
				if err != nil {
					t.Fatal(err)
				}
				want := &image.RGBA{Pix: pixels, Stride: width * 4, Rect: image.Rect(0, 0, width, height)}
				for y := 0; y < height; y++ {
					for x := 0; x < width; x++ {
						r, g, b, a := pictures[1].At(x, y).RGBA()
						wr, wg, wb, wa := want.At(x, y).RGBA()
						if [4]uint32{r, g, b, a} != [4]uint32{wr, wg, wb, wa} {
							t.Fatalf("scaled pixel differs at %d,%d", x, y)
						}
					}
				}
			}
		})
	}
}

func TestForcedRedrawSurvivesAFullFrameQueue(t *testing.T) {
	archive, err := os.ReadFile(filepath.Join("..", "platform", "skt", "testdata", "canvas-skt.zip"))
	if err != nil {
		t.Fatal(err)
	}
	game, err := session.Start(t.Context(), archive, session.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(game.Close)
	runner := &sessionRunner{game: game, frames: make(chan pendingFrame, 1), forceFrame: true}
	runner.frames <- pendingFrame{}
	runner.pushFrame()
	if !runner.forceFrame {
		t.Fatal("a dropped redraw was forgotten")
	}
	<-runner.frames
	runner.pushFrame()
	frame := <-runner.frames
	if !frame.Force || runner.forceFrame {
		t.Fatal("redraw was not transferred to the accepted frame")
	}
	frame.Force = false
	pictures := encodeFrames(t, frame, frame, pendingFrame{RGBA: frame.RGBA, Width: frame.Width, Height: frame.Height, Scale: frame.Scale, Force: true})
	if len(pictures) != 2 {
		t.Fatalf("encoded %d pictures, want initial and explicit redraw", len(pictures))
	}
}

func TestForcedPictureFollowsQueuedLifecycleAnswers(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	server := newTestServer(t, Options{})
	host := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		upgrader := wsproto.Upgrader{}
		connection, err := upgrader.Accept(w, request)
		if err != nil {
			return
		}
		defer connection.Close()
		runner := &sessionRunner{server: server, connection: connection,
			outText: make(chan outboundMessage, 2), outFrames: make(chan outboundMessage, 1), writerDone: make(chan struct{})}
		runner.outText <- outboundMessage{text: `{"kind":"ready"}`}
		runner.outText <- outboundMessage{text: `{"kind":"started"}`}
		runner.outFrames <- outboundMessage{binary: []byte("picture"), redraw: true}
		runner.writeMessages(ctx, func() {})
	}))
	defer host.Close()
	defer cancel()
	connection, _, err := wsproto.Dial("ws"+host.URL[len("http"):], nil)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	_ = connection.SetReadDeadline(time.Now().Add(5 * time.Second))
	for i, want := range []wsproto.Opcode{wsproto.OpText, wsproto.OpText, wsproto.OpBinary} {
		opcode, _, err := connection.ReadMessage()
		if err != nil {
			t.Fatal(err)
		}
		if opcode != want {
			t.Fatalf("message %d opcode=%d, want %d before the forced picture", i, opcode, want)
		}
	}
}

func BenchmarkFrameEncoding(b *testing.B) {
	for _, scale := range []int{1, 4} {
		for _, changing := range []bool{false, true} {
			name := "original"
			if scale == 4 {
				name = "hq4x"
			}
			if changing {
				name += "/changed"
			} else {
				name += "/unchanged"
			}
			b.Run(name, func(b *testing.B) {
				pixels := make([]byte, 240*320*4)
				for i := 0; i < len(pixels); i += 4 {
					pixels[i], pixels[i+3] = 240, 255
				}
				other := bytes.Clone(pixels)
				if changing {
					other[0] = 0
				}
				frames := []pendingFrame{{RGBA: pixels, Width: 240, Height: 320, Scale: scale}, {RGBA: other, Width: 240, Height: 320, Scale: scale}}
				runner := &sessionRunner{frames: make(chan pendingFrame, 1), outFrames: make(chan outboundMessage, 1)}
				done := make(chan struct{})
				go func() { runner.writeFrames(context.Background()); close(runner.outFrames) }()
				go func() {
					for range runner.outFrames {
					}
					close(done)
				}()
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					runner.frames <- frames[i%2]
				}
				close(runner.frames)
				<-done
			})
		}
	}
}
