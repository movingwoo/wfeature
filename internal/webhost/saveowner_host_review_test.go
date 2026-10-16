package webhost

import (
	"archive/zip"
	"bytes"
	"io"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/wsproto"
)

// repackedLGTFixture is the LGT text-input fixture with its descriptor's PID
// replaced, which is the name its saves are kept under.
func repackedLGTFixture(t *testing.T, pid string) []byte {
	t.Helper()
	source, err := zip.OpenReader(filepath.Join("..", "platform", "lgt", "testdata", "text-input.zip"))
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	var packed bytes.Buffer
	writer := zip.NewWriter(&packed)
	for _, file := range source.File {
		reader, err := file.Open()
		if err != nil {
			t.Fatal(err)
		}
		contents, err := io.ReadAll(reader)
		reader.Close()
		if err != nil {
			t.Fatal(err)
		}
		if file.Name == "app_info" {
			contents = []byte(strings.Replace(string(contents), "PID=PF000001", "PID="+pid, 1))
		}
		entry, err := writer.Create(file.Name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write(contents); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return packed.Bytes()
}

// A game whose save owner cannot name a directory used to start with no save
// store at all: it saved, the player saw nothing wrong, and closing it lost
// every bit of progress. The start is refused instead, in words the page
// shows, and the server log says which owner it was.
func TestAGameWhoseSaveOwnerIsRefusedDoesNotStartWithoutSaves(t *testing.T) {
	root := t.TempDir()
	gameRoot := filepath.Join(root, "games")
	if err := os.MkdirAll(filepath.Join(gameRoot, "lgt"), 0o755); err != nil {
		t.Fatal(err)
	}
	const owner = "PF:0001"
	if err := os.WriteFile(filepath.Join(gameRoot, "lgt", "colon.zip"), repackedLGTFixture(t, owner), 0o644); err != nil {
		t.Fatal(err)
	}
	log := &lockedBuffer{}
	server := newTestServer(t, Options{
		GameRoot: gameRoot,
		SaveRoot: filepath.Join(root, "savedata", "ktf"),
		LogRoot:  filepath.Join(root, "logs"),
		Logger:   backend.NewLogger(log),
	})
	httpServer := httptest.NewServer(server)
	t.Cleanup(httpServer.Close)
	connection, _, err := wsproto.Dial("ws://"+strings.TrimPrefix(httpServer.URL, "http://")+"/api/session", nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = connection.Close() })

	expectMessage(t, connection, serverReady)
	send(t, connection, clientMessage{Kind: clientStart, Game: "games/lgt/colon.zip", ID: 3})
	reply := expectMessage(t, connection, serverError)
	if reply.ID != 3 || !strings.Contains(reply.Message, "세이브") {
		t.Fatalf("reply = %+v, want the start refused for its saves", reply)
	}
	if text := log.String(); !strings.Contains(text, owner) {
		t.Fatalf("the server log does not name the refused owner:\n%s", text)
	}

	// The same owner with nothing wrong in it starts, so what was refused
	// above is the name and not the fixture.
	if err := os.WriteFile(filepath.Join(gameRoot, "lgt", "plain.zip"), repackedLGTFixture(t, "PF0001"), 0o644); err != nil {
		t.Fatal(err)
	}
	send(t, connection, clientMessage{Kind: clientStart, Game: "games/lgt/plain.zip", ID: 4})
	if started := expectMessage(t, connection, serverStarted); started.Started == nil || started.Started.SaveOwner != "PF0001" {
		t.Fatalf("started = %+v", started.Started)
	}
}
