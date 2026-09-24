package webhost

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"image"
	"image/draw"
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
	messages := encodeFrameMessages(t, false, frames...)
	var pictures []image.Image
	for _, message := range messages {
		picture, err := png.Decode(bytes.NewReader(message.binary))
		if err != nil {
			t.Fatal(err)
		}
		pictures = append(pictures, picture)
	}
	return pictures
}

func encodeFrameMessages(t *testing.T, patches bool, frames ...pendingFrame) []outboundMessage {
	t.Helper()
	runner := &sessionRunner{server: newTestServer(t, Options{}),
		framePatches: patches,
		frames:       make(chan pendingFrame, len(frames)), outFrames: make(chan outboundMessage, len(frames))}
	for _, frame := range frames {
		runner.frames <- frame
	}
	close(runner.frames)
	runner.writeFrames(t.Context())
	close(runner.outFrames)
	var messages []outboundMessage
	for message := range runner.outFrames {
		messages = append(messages, message)
	}
	return messages
}

func TestFramePatchesReconstructLosslessly(t *testing.T) {
	for _, scale := range []int{1, 2, 3, 4} {
		t.Run(fmt.Sprint(scale), func(t *testing.T) {
			first := pendingFrame{RGBA: make([]byte, 64*80*4), Width: 64, Height: 80, Scale: scale}
			for i := 0; i < len(first.RGBA); i += 4 {
				first.RGBA[i], first.RGBA[i+1], first.RGBA[i+3] = byte(i/4), byte(i/256), 255
			}
			second := first
			second.RGBA = bytes.Clone(first.RGBA)
			copy(second.RGBA[(30*64+20)*4:], []byte{240, 20, 0, 255})
			third := second
			third.RGBA = bytes.Clone(second.RGBA)
			// Erasing to transparency must replace the old pixel, not blend.
			clear(third.RGBA[(30*64+20)*4 : (30*64+20)*4+4])
			copy(third.RGBA[:4], []byte{0, 240, 0, 255})
			forced := third
			forced.Force = true
			resized := first
			resized.Width, resized.Height = first.Height, first.Width
			frames := []pendingFrame{first, second, third, forced, resized}
			messages := encodeFrameMessages(t, true, frames...)
			if len(messages) != len(frames) {
				t.Fatalf("got %d pictures", len(messages))
			}
			var canvas *image.RGBA
			for i, message := range messages {
				payload := message.binary
				patch := bytes.HasPrefix(payload, []byte("WFP1"))
				if patch != (i == 1 || i == 2) {
					t.Fatalf("picture %d patch=%v", i, patch)
				}
				var origin image.Point
				if patch {
					origin = image.Pt(int(binary.BigEndian.Uint32(payload[4:8])), int(binary.BigEndian.Uint32(payload[8:12])))
					payload = payload[12:]
				}
				picture, err := png.Decode(bytes.NewReader(payload))
				if err != nil {
					t.Fatal(err)
				}
				if !patch {
					canvas = image.NewRGBA(picture.Bounds())
				}
				draw.Draw(canvas, picture.Bounds().Add(origin), picture, picture.Bounds().Min, draw.Src)
				want := encodeFrames(t, frames[i])[0]
				if canvas.Bounds() != want.Bounds() {
					t.Fatal("wrong reconstructed dimensions")
				}
				for y := 0; y < canvas.Bounds().Dy(); y++ {
					for x := 0; x < canvas.Bounds().Dx(); x++ {
						r, g, b, a := canvas.At(x, y).RGBA()
						wr, wg, wb, wa := want.At(x, y).RGBA()
						if [4]uint32{r, g, b, a} != [4]uint32{wr, wg, wb, wa} {
							t.Fatalf("picture %d differs at %d,%d", i, x, y)
						}
					}
				}
			}
			legacy := encodeFrameMessages(t, false, second)[0]
			if len(messages[1].binary)*2 >= len(legacy.binary) {
				t.Fatalf("patch %d bytes, full %d: want at least 50%% reduction", len(messages[1].binary), len(legacy.binary))
			}
		})
	}
}

func TestSessionNegotiatesFramePatches(t *testing.T) {
	for _, query := range []string{"", "frames=patch-v1", "frames=unknown"} {
		t.Run(query, func(t *testing.T) {
			connection, _ := sessionFixture(t, query)
			_ = connection.SetReadDeadline(time.Now().Add(10 * time.Second))
			expectMessage(t, connection, serverReady)
			send(t, connection, clientMessage{Kind: clientStart, Game: "games/skt/canvas.zip"})
			expectMessage(t, connection, serverStarted)
			expectFrame(t, connection) // Every connection starts with a full PNG.
			readPicture := func() []byte {
				for {
					opcode, payload, err := connection.ReadMessage()
					if err != nil {
						t.Fatal(err)
					}
					if opcode == wsproto.OpBinary {
						return payload
					}
				}
			}
			send(t, connection, clientMessage{Kind: clientKey, Action: "press", Code: '1'})
			readPicture()
			send(t, connection, clientMessage{Kind: clientKey, Action: "press", Code: '2'})
			payload := readPicture()
			if patch := bytes.HasPrefix(payload, []byte("WFP1")); patch != (query == "frames=patch-v1") {
				t.Fatalf("patch=%v for query %q", patch, query)
			}
			// An explicit redraw on the same socket must reset the base too.
			send(t, connection, clientMessage{Kind: clientScale, Value: 1})
			expectFrame(t, connection)
		})
	}
}

// A fixed amount of scene data makes both byte and encoder CPU comparisons
// meaningful. This authored moving sprite is not a real-game acceptance route.
func bandwidthFrames() []pendingFrame {
	const width, height = 240, 320
	background := make([]byte, width*height*4)
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			i := (y*width + x) * 4
			value := uint32(x/8+y/8*31) * 2654435761
			background[i], background[i+1], background[i+2], background[i+3] = byte(value), byte(value>>8), byte(value>>16), 255
		}
	}
	frames := make([]pendingFrame, 60)
	for i := range frames {
		pixels := bytes.Clone(background)
		for y := 140; y < 156; y++ {
			for x := 20 + i*2; x < 36+i*2; x++ {
				copy(pixels[(y*width+x)*4:], []byte{255, 255, 255, 255})
			}
		}
		frames[i] = pendingFrame{RGBA: pixels, Width: width, Height: height, Scale: 1}
	}
	return frames
}

func TestFramePatchBandwidth(t *testing.T) {
	frames := bandwidthFrames()
	sizes := func(patches bool) int {
		total := 0
		for _, message := range encodeFrameMessages(t, patches, frames...) {
			total += len(message.binary)
		}
		return total
	}
	full, patched := sizes(false), sizes(true)
	t.Logf("60 authored frames: full=%d bytes, patches=%d bytes, reduction=%.2f%%", full, patched, 100*(1-float64(patched)/float64(full)))
	if patched*10 >= full {
		t.Fatal("moving sprite should reduce frame payload by at least 90 percent")
	}
}

func BenchmarkFramePatchBandwidth(b *testing.B) {
	frames := bandwidthFrames()
	for _, patches := range []bool{false, true} {
		b.Run(fmt.Sprintf("patches=%v", patches), func(b *testing.B) {
			runner := &sessionRunner{framePatches: patches, frames: make(chan pendingFrame, 1), outFrames: make(chan outboundMessage, 1)}
			done := make(chan int, 1)
			go func() { runner.writeFrames(context.Background()); close(runner.outFrames) }()
			go func() {
				total := 0
				for message := range runner.outFrames {
					total += len(message.binary)
				}
				done <- total
			}()
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				runner.frames <- frames[i%len(frames)]
			}
			close(runner.frames)
			total := <-done
			b.ReportMetric(float64(total)/float64(b.N), "wire-B/frame")
		})
	}
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
