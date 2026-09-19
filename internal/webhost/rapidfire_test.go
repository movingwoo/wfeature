package webhost

import (
	"context"
	"image"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/movingwoo/wfeature/internal/session"
)

// Exercise the existing socket protocol at the maximum manual cadence: both
// eligible keys held, with a release halfway through every 100 ms cycle.
func TestSessionRapidFire(t *testing.T) {
	connection, _ := sessionFixture(t)
	expectMessage(t, connection, serverReady)
	send(t, connection, clientMessage{Kind: clientStart, Game: "games/skt/canvas.zip"})
	expectMessage(t, connection, serverStarted)
	expectFrame(t, connection)
	for i := 0; i < 20; i++ {
		action := session.KeyPress
		if i%2 != 0 {
			action = session.KeyRelease
		}
		for _, code := range []int32{53, 148} {
			send(t, connection, clientMessage{Kind: clientKey, Action: action, Code: code})
		}
		time.Sleep(50 * time.Millisecond)
	}
	// Park is ordered after all keys, unlike ping, which bypasses the queue.
	send(t, connection, clientMessage{Kind: clientPark, ID: 901})
	if result := expectMessage(t, connection, serverResult); result.ID != 901 {
		t.Fatalf("park acknowledgement = %d", result.ID)
	}
}

// Fixed work: one press/release pair, including strict JSON decoding, handler
// dispatch, guest callbacks, repaint and PNG encoding for an authored MIDlet.
// Socket scheduling and real-game callback costs are deliberately separate.
func BenchmarkRapidFirePair(b *testing.B) {
	archive, err := os.ReadFile(filepath.Join("..", "platform", "skt", "testdata", "canvas-skt.zip"))
	if err != nil {
		b.Fatal(err)
	}
	ctx := context.Background()
	for _, keys := range []string{"OK", "Both"} {
		b.Run(keys, func(b *testing.B) {
			game, err := session.Start(ctx, archive, session.Options{})
			if err != nil {
				b.Fatal(err)
			}
			defer game.Close()
			if _, err := game.Tick(ctx, 0); err != nil {
				b.Fatal(err)
			}
			runner := &sessionRunner{game: game, gameCtx: ctx, outText: make(chan outboundMessage, 8)}
			codes := []string{"148"}
			if keys == "Both" {
				codes = append(codes, "53")
			}
			var messages []string
			for _, action := range []string{"press", "release"} {
				for _, code := range codes {
					messages = append(messages, `{"kind":"key","action":"`+action+`","code":`+code+`}`)
				}
			}
			encoder := png.Encoder{CompressionLevel: png.BestSpeed, BufferPool: &pngBufferPool{}}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				for _, text := range messages {
					var command clientMessage
					if err := decodeClientMessage(text, &command); err != nil {
						b.Fatal(err)
					}
					runner.handle(ctx, command)
					if len(runner.outText) != 0 {
						b.Fatal("key handler reported an error")
					}
					if _, err := game.Tick(ctx, 0); err != nil {
						b.Fatal(err)
					}
					frame, width, height, ok := game.Frame()
					if !ok {
						b.Fatal("no frame")
					}
					if err := encoder.Encode(io.Discard, &image.RGBA{Pix: frame, Stride: width * 4, Rect: image.Rect(0, 0, width, height)}); err != nil {
						b.Fatal(err)
					}
				}
				if len(runner.heldKeys) != 0 {
					b.Fatal("key remained held")
				}
			}
		})
	}
}

// This separates message parsing from the fixture's repaint cost.
func BenchmarkRapidFireDecodePair(b *testing.B) {
	messages := []string{`{"kind":"key","action":"press","code":148}`, `{"kind":"key","action":"release","code":148}`}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		for _, text := range messages {
			var command clientMessage
			if err := decodeClientMessage(text, &command); err != nil {
				b.Fatal(err)
			}
		}
	}
}
