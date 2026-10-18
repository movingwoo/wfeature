package webhost

import (
	"context"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A parked game whose destroyApp never returns does not hold the server's stop
// past its deadline: CloseSessions returns once ctx ends, the game keeps its
// save directory claimed while its close runs on, and lets the claim go once
// the close has returned, which the platform bounds. Parked games used to be
// closed under the lock, one after another, before the wait began, so such a
// title kept the server from stopping and an app reopened behind it waiting
// for ever.
func TestAParkedGameThatWillNotCloseDoesNotHoldTheStop(t *testing.T) {
	archive, err := os.ReadFile(filepath.Join("..", "platform", "skt", "testdata", "hang-destroy-skt.zip"))
	if err != nil {
		t.Fatalf("read the fixture: %v", err)
	}
	root := t.TempDir()
	gameRoot := filepath.Join(root, "games")
	if err := os.MkdirAll(filepath.Join(gameRoot, "skt"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(gameRoot, "skt", "hang.zip"), archive, 0o644); err != nil {
		t.Fatal(err)
	}
	server := newTestServer(t, Options{
		GameRoot: gameRoot,
		SaveRoot: filepath.Join(root, "savedata", "skt"),
		LogRoot:  filepath.Join(root, "logs"),
	})
	httpServer := httptest.NewServer(server)
	t.Cleanup(httpServer.Close)
	url := "ws://" + strings.TrimPrefix(httpServer.URL, "http://") + "/api/session"

	connection := dialSession(t, url)
	expectMessage(t, connection, serverReady)
	send(t, connection, clientMessage{Kind: clientStart, Game: "games/skt/hang.zip"})
	expectMessage(t, connection, serverStarted)
	_ = connection.Close()
	waitForParked(t, server, 1)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	stopped := make(chan struct{})
	go func() {
		server.CloseSessions(ctx)
		close(stopped)
	}()
	select {
	case <-stopped:
	case <-time.After(5 * time.Second):
		t.Fatal("CloseSessions waited past its deadline for a parked game that will not close")
	}
	if server.parkedCount() != 0 {
		t.Fatalf("%d games still parked after the stop", server.parkedCount())
	}
	if claimed := server.claimCount(); claimed != 1 {
		t.Fatalf("%d save directories claimed while the parked game closes, want 1", claimed)
	}
	deadline := time.Now().Add(time.Minute)
	for server.claimCount() != 0 {
		if time.Now().After(deadline) {
			t.Fatal("the parked game's close never returned")
		}
		time.Sleep(50 * time.Millisecond)
	}
}
