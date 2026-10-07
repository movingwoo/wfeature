package main

import (
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/movingwoo/wfeature/internal/webhost"
	"github.com/movingwoo/wfeature/internal/wsproto"
	"github.com/movingwoo/wfeature/web"
)

// Stopping the server ends the session of a page that is still attached. The
// HTTP server's own shutdown does not reach a session socket, so before the
// handler closed the games itself a stop returned with that socket still open
// and the game behind it running until the process went — with whatever its
// title had written and not yet handed to the save store.
//
// This drives the same serve and drain the binary runs, and asks for the stop
// the way POST /api/shutdown does.
func TestStoppingTheServerEndsAnAttachedSession(t *testing.T) {
	root := t.TempDir()
	gameRoot := filepath.Join(root, "games")
	if err := os.MkdirAll(filepath.Join(gameRoot, "skt"), 0o755); err != nil {
		t.Fatal(err)
	}
	archive, err := os.ReadFile(filepath.Join("..", "..", "internal", "platform", "skt", "testdata", "canvas-skt.zip"))
	if err != nil {
		t.Fatalf("read the canvas fixture: %v", err)
	}
	if err := os.WriteFile(filepath.Join(gameRoot, "skt", "canvas.zip"), archive, 0o644); err != nil {
		t.Fatal(err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	handler, err := webhost.New(webhost.Options{
		Client:   web.Client(),
		GameRoot: gameRoot,
		SaveRoot: filepath.Join(root, "savedata", "ktf"),
		LogRoot:  filepath.Join(root, "logs"),
		Logger:   logger,
	})
	if err != nil {
		t.Fatalf("webhost.New: %v", err)
	}
	listener, err := listen("127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	requested := make(chan struct{})
	served := make(chan error, 1)
	go func() { served <- serve(listener, handler, logger, requested) }()

	connection, _, err := wsproto.Dial("ws://"+listener.Addr().String()+"/api/session", nil)
	if err != nil {
		t.Fatalf("dial the session: %v", err)
	}
	defer connection.Close()
	if err := connection.WriteText(`{"kind":"start","game":"games/skt/canvas.zip"}`); err != nil {
		t.Fatalf("start the game: %v", err)
	}
	// Reading happens on one goroutine for the whole test: first until the
	// game has started, then until the server ends the socket.
	started := make(chan struct{})
	ended := make(chan struct{})
	go func() {
		defer close(ended)
		running := false
		for {
			opcode, payload, err := connection.ReadMessage()
			if err != nil {
				return
			}
			if running || opcode != wsproto.OpText {
				continue
			}
			var message struct {
				Kind string `json:"kind"`
			}
			if json.Unmarshal(payload, &message) == nil && message.Kind == "started" {
				running = true
				close(started)
			}
		}
	}()
	select {
	case <-started:
	case <-ended:
		t.Fatal("the socket closed before the game started")
	case <-time.After(30 * time.Second):
		t.Fatal("the game did not start")
	}

	close(requested)
	select {
	case err := <-served:
		if err != nil {
			t.Fatalf("serve returned %v", err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("the server did not stop")
	}
	select {
	case <-ended:
	case <-time.After(10 * time.Second):
		t.Fatal("the server stopped and left the attached session's socket open")
	}
}
