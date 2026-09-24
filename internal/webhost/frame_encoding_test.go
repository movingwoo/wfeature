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
	messages := encodeFrameMessages(t, protocolPictures, frames...)
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

func encodeFrameMessages(t *testing.T, protocol int, frames ...pendingFrame) []outboundMessage {
	t.Helper()
	runner := &sessionRunner{server: newTestServer(t, Options{}),
		protocol: protocol,
		frames:   make(chan pendingFrame, len(frames)), outFrames: make(chan outboundMessage, len(frames))}
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

// streamHeader is a protocol 2 picture header, read the way the page reads it.
type streamHeader struct {
	operation, scale int
	shift, origin    image.Point
}

// applyStreamMessage does to canvas what the page does with one protocol 2
// picture, and answers the resulting picture.
func applyStreamMessage(t *testing.T, canvas *image.RGBA, payload []byte) (*image.RGBA, streamHeader) {
	t.Helper()
	if len(payload) < pictureHeaderSize || !bytes.Equal(payload[:4], pictureMagic) || payload[14] != 0 || payload[15] != 0 {
		t.Fatalf("not a protocol 2 picture: % x", payload[:min(len(payload), 16)])
	}
	header := streamHeader{
		operation: int(payload[4]),
		scale:     int(payload[5]),
		shift:     image.Pt(int(int16(binary.BigEndian.Uint16(payload[6:8]))), int(int16(binary.BigEndian.Uint16(payload[8:10])))),
		origin:    image.Pt(int(binary.BigEndian.Uint16(payload[10:12])), int(binary.BigEndian.Uint16(payload[12:14]))),
	}
	var picture image.Image
	if len(payload) > pictureHeaderSize {
		decoded, err := png.Decode(bytes.NewReader(payload[pictureHeaderSize:]))
		if err != nil {
			t.Fatal(err)
		}
		picture = decoded
	}
	switch header.operation {
	case pictureComplete:
		canvas = image.NewRGBA(picture.Bounds())
		draw.Draw(canvas, canvas.Bounds(), picture, image.Point{}, draw.Src)
	case pictureReplace:
		draw.Draw(canvas, picture.Bounds().Add(header.origin), picture, image.Point{}, draw.Src)
	case pictureMasked:
		if header.shift != (image.Point{}) {
			// The page draws its held picture at the shift over itself.
			held := image.NewRGBA(canvas.Bounds())
			draw.Draw(held, held.Bounds(), canvas, image.Point{}, draw.Src)
			draw.Draw(canvas, held.Bounds().Add(header.shift), held, image.Point{}, draw.Over)
		}
		if picture != nil {
			draw.Draw(canvas, picture.Bounds().Add(header.origin), picture, image.Point{}, draw.Over)
		}
	default:
		t.Fatalf("unknown operation %d", header.operation)
	}
	return canvas, header
}

func samePixels(t *testing.T, got *image.RGBA, want pendingFrame) {
	t.Helper()
	if got.Bounds() != image.Rect(0, 0, want.Width, want.Height) {
		t.Fatalf("reconstructed %v, want %dx%d", got.Bounds(), want.Width, want.Height)
	}
	if !bytes.Equal(got.Pix, want.RGBA) {
		for index := range got.Pix {
			if got.Pix[index] != want.RGBA[index] {
				pixel := index / 4
				t.Fatalf("pixel %d,%d differs: % x, want % x", pixel%want.Width, pixel/want.Width,
					got.Pix[pixel*4:pixel*4+4], want.RGBA[pixel*4:pixel*4+4])
			}
		}
	}
}

func TestStreamUpdatesReconstructLosslessly(t *testing.T) {
	for _, scale := range []int{1, 2, 3, 4} {
		t.Run(fmt.Sprint(scale), func(t *testing.T) {
			first := pendingFrame{RGBA: make([]byte, 64*80*4), Width: 64, Height: 80, Scale: scale}
			for i := 0; i < len(first.RGBA); i += 4 {
				first.RGBA[i], first.RGBA[i+1], first.RGBA[i+3] = byte(i/4), byte(i/256), 255
			}
			second := first
			second.RGBA = bytes.Clone(first.RGBA)
			copy(second.RGBA[(30*64+20)*4:], []byte{240, 20, 0, 255})
			copy(second.RGBA[(70*64+60)*4:], []byte{1, 2, 3, 255})
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
			messages := encodeFrameMessages(t, protocolStream, frames...)
			if len(messages) != len(frames) {
				t.Fatalf("got %d pictures", len(messages))
			}
			wantOperations := []int{pictureComplete, pictureMasked, pictureReplace, pictureComplete, pictureComplete}
			var canvas *image.RGBA
			for i, message := range messages {
				var header streamHeader
				canvas, header = applyStreamMessage(t, canvas, message.binary)
				if header.operation != wantOperations[i] || header.scale != scale {
					t.Fatalf("picture %d: operation %d scale %d, want %d and %d", i, header.operation, header.scale, wantOperations[i], scale)
				}
				samePixels(t, canvas, frames[i])
			}
			if len(messages[1].binary) >= len(messages[0].binary) {
				t.Fatalf("update %d bytes, complete %d", len(messages[1].binary), len(messages[0].binary))
			}
			// The page magnifies, so the update stays small at every scale.
			if legacy := encodeFrameMessages(t, protocolPictures, second)[0]; scale > 1 && len(messages[1].binary)*4 >= len(legacy.binary) {
				t.Fatalf("update %d bytes, magnified complete %d: want at least 75%% less", len(messages[1].binary), len(legacy.binary))
			}
		})
	}
}

// scrollingFrames is an authored field that scrolls under a fixed status bar,
// which is the shape of the scenes that changed almost every pixel a frame.
func scrollingFrames(count, step int) []pendingFrame {
	offsets := make([]int, count)
	for i := range offsets {
		offsets[i] = i * step
	}
	return scrolledField(offsets...)
}

// scrolledField draws the field at each of the given horizontal offsets.
func scrolledField(offsets ...int) []pendingFrame {
	const width, height = 120, 96
	// Eight-pixel tiles from a sixteen-colour set, with a mark in some of
	// them, which is what a handset's map looks like.
	field := func(x, y int) [4]byte {
		tile := uint32(x/8*31+y/8*17) * 2654435761 >> 28
		if x%8 == 3 && y%8 == 4 && tile%3 == 0 {
			tile = 15 - tile
		}
		return [4]byte{byte(tile * 16), byte(tile * 7), byte(255 - tile*9), 255}
	}
	frames := make([]pendingFrame, len(offsets))
	for i, offset := range offsets {
		pixels := make([]byte, width*height*4)
		for y := 0; y < height; y++ {
			for x := 0; x < width; x++ {
				pixel := field(x+offset, y)
				if y < 12 {
					pixel = [4]byte{0, 0, 80, 255}
				}
				copy(pixels[(y*width+x)*4:], pixel[:])
			}
		}
		frames[i] = pendingFrame{RGBA: pixels, Width: width, Height: height, Scale: 1}
	}
	return frames
}

func TestStreamFollowsAScrollingPicture(t *testing.T) {
	frames := scrollingFrames(4, 2)
	messages := encodeFrameMessages(t, protocolStream, frames...)
	var canvas *image.RGBA
	for i, message := range messages {
		var header streamHeader
		canvas, header = applyStreamMessage(t, canvas, message.binary)
		samePixels(t, canvas, frames[i])
		if i == 0 {
			continue
		}
		// The field moved two pixels left, so the held picture is drawn two
		// pixels left and only the strip it uncovers is new.
		if header.operation != pictureMasked || header.shift != image.Pt(-2, 0) {
			t.Fatalf("picture %d: operation %d shift %v, want a masked update after (-2,0)", i, header.operation, header.shift)
		}
		if len(message.binary)*4 >= len(messages[0].binary) {
			t.Fatalf("picture %d is %d bytes against %d complete", i, len(message.binary), len(messages[0].binary))
		}
	}
}

func TestStreamSearchesAgainWhenTheScrollChangesDirection(t *testing.T) {
	// The second scroll repeats the first, which the encoder tries before
	// searching; the third turns back, which only a search finds.
	frames := scrolledField(0, 2, 4, 1)
	messages := encodeFrameMessages(t, protocolStream, frames...)
	var canvas *image.RGBA
	for i, message := range messages {
		var header streamHeader
		canvas, header = applyStreamMessage(t, canvas, message.binary)
		samePixels(t, canvas, frames[i])
		if want := []image.Point{{}, {-2, 0}, {-2, 0}, {3, 0}}[i]; header.shift != want {
			t.Fatalf("picture %d shifted %v, want %v", i, header.shift, want)
		}
	}
}

func TestStreamScrollWithNothingElseCarriesNoPicture(t *testing.T) {
	frames := scrollingFrames(1, 0)
	// A picture that is exactly the last one drawn two pixels up over itself:
	// the uncovered strip at the bottom keeps what it had.
	moved := frames[0]
	moved.RGBA = bytes.Clone(frames[0].RGBA)
	row := moved.Width * 4
	copy(moved.RGBA[:(moved.Height-2)*row], frames[0].RGBA[2*row:])
	messages := encodeFrameMessages(t, protocolStream, frames[0], moved)
	if len(messages) != 2 || len(messages[1].binary) != pictureHeaderSize {
		t.Fatalf("got %d messages, the update %d bytes; want a bare header", len(messages), len(messages[len(messages)-1].binary))
	}
	canvas, header := applyStreamMessage(t, nil, messages[0].binary)
	canvas, header = applyStreamMessage(t, canvas, messages[1].binary)
	if header.shift != image.Pt(0, -2) {
		t.Fatalf("shift %v, want (0,-2)", header.shift)
	}
	samePixels(t, canvas, moved)
}

func TestStreamDoesNotScrollOverATranslucentPicture(t *testing.T) {
	frames := scrollingFrames(2, 2)
	for _, frame := range frames {
		// One pixel that is not opaque in the held picture would blend when
		// the page draws the picture over itself.
		clear(frame.RGBA[len(frame.RGBA)-4:])
	}
	messages := encodeFrameMessages(t, protocolStream, frames...)
	canvas, _ := applyStreamMessage(t, nil, messages[0].binary)
	canvas, header := applyStreamMessage(t, canvas, messages[1].binary)
	if header.shift != (image.Point{}) {
		t.Fatalf("shifted %v over a translucent picture", header.shift)
	}
	samePixels(t, canvas, frames[1])
}

// pngColorType reads the colour type from a PNG's header chunk.
func pngColorType(t *testing.T, data []byte) byte {
	t.Helper()
	if len(data) < 26 || string(data[12:16]) != "IHDR" {
		t.Fatal("not a PNG")
	}
	return data[25]
}

func TestStreamUsesAPaletteWhereTheColoursFit(t *testing.T) {
	few := pendingFrame{RGBA: make([]byte, 32*32*4), Width: 32, Height: 32, Scale: 1}
	many := pendingFrame{RGBA: make([]byte, 32*32*4), Width: 32, Height: 32, Scale: 1}
	for i := 0; i < 32*32; i++ {
		copy(few.RGBA[i*4:], []byte{byte(i % 7 * 30), 0, 0, 255})
		copy(many.RGBA[i*4:], []byte{byte(i), byte(i >> 8), 3, 255})
	}
	if got := pngColorType(t, encodeFrameMessages(t, protocolStream, few)[0].binary[pictureHeaderSize:]); got != 3 {
		t.Fatalf("seven colours were written with colour type %d, want a palette", got)
	}
	if got := pngColorType(t, encodeFrameMessages(t, protocolStream, many)[0].binary[pictureHeaderSize:]); got == 3 {
		t.Fatal("a thousand colours were written with a palette")
	}
}

func TestSessionNegotiatesTheStreamProtocol(t *testing.T) {
	for _, query := range []string{"", "protocol=2", "protocol=3", "frames=patch-v1"} {
		t.Run(query, func(t *testing.T) {
			connection, _ := sessionFixture(t, query)
			_ = connection.SetReadDeadline(time.Now().Add(10 * time.Second))
			expectMessage(t, connection, serverReady)
			send(t, connection, clientMessage{Kind: clientStart, Game: "games/skt/canvas.zip"})
			expectMessage(t, connection, serverStarted)
			readPicture := func() []byte {
				for {
					opcode, payload, err := connection.ReadMessage()
					if err != nil {
						t.Fatal(err)
					}
					if opcode == wsproto.OpBinary && !bytes.HasPrefix(payload, audioMagic) {
						return payload
					}
				}
			}
			stream := query == "protocol=2"
			first := readPicture()
			if bytes.HasPrefix(first, pictureMagic) != stream {
				t.Fatalf("first picture % x for query %q", first[:8], query)
			}
			send(t, connection, clientMessage{Kind: clientKey, Action: "press", Code: '1'})
			readPicture()
			send(t, connection, clientMessage{Kind: clientKey, Action: "press", Code: '2'})
			payload := readPicture()
			if bytes.HasPrefix(payload, pictureMagic) != stream {
				t.Fatalf("update % x for query %q", payload[:8], query)
			}
			// An explicit redraw on the same socket resets the base.
			send(t, connection, clientMessage{Kind: clientScale, Value: 2})
			payload = readPicture()
			if stream && (payload[4] != pictureComplete || payload[5] != 1) {
				// A MIDlet's surface is never magnified, whatever the page asks.
				t.Fatalf("redraw operation %d scale %d", payload[4], payload[5])
			}
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
	sizes := func(protocol int, frames []pendingFrame) int {
		total := 0
		for _, message := range encodeFrameMessages(t, protocol, frames...) {
			total += len(message.binary)
		}
		return total
	}
	sprite := bandwidthFrames()
	full, updates := sizes(protocolPictures, sprite), sizes(protocolStream, sprite)
	t.Logf("60 authored sprite frames: complete=%d bytes, updates=%d bytes, reduction=%.2f%%", full, updates, 100*(1-float64(updates)/float64(full)))
	if updates*10 >= full {
		t.Fatal("moving sprite should reduce frame payload by at least 90 percent")
	}
	scrolling := scrollingFrames(60, 2)
	full, updates = sizes(protocolPictures, scrolling), sizes(protocolStream, scrolling)
	t.Logf("60 authored scrolling frames: complete=%d bytes, updates=%d bytes, reduction=%.2f%%", full, updates, 100*(1-float64(updates)/float64(full)))
	if updates*4 >= full {
		t.Fatal("a scrolling field should reduce frame payload by at least 75 percent")
	}
}

func BenchmarkFramePatchBandwidth(b *testing.B) {
	frames := bandwidthFrames()
	for _, protocol := range []int{protocolPictures, protocolStream} {
		b.Run(fmt.Sprintf("protocol=%d", protocol), func(b *testing.B) {
			runner := &sessionRunner{protocol: protocol, frames: make(chan pendingFrame, 1), outFrames: make(chan outboundMessage, 1)}
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

func TestEncoderRefusesAPictureShorterThanItsSize(t *testing.T) {
	for _, protocol := range []int{protocolPictures, protocolStream} {
		short := pendingFrame{RGBA: make([]byte, 4), Width: 2, Height: 2, Scale: 1}
		whole := pendingFrame{RGBA: bytes.Repeat([]byte{9, 9, 9, 255}, 4), Width: 2, Height: 2, Scale: 1}
		if messages := encodeFrameMessages(t, protocol, short, whole); len(messages) != 1 {
			t.Fatalf("protocol %d sent %d pictures, want only the whole one", protocol, len(messages))
		}
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
