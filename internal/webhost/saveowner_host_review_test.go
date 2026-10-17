package webhost

import (
	"archive/zip"
	"bytes"
	"io"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
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
// every bit of progress. Every platform now maps such an owner to one that
// can name a directory, as SKT does, so the game starts and keeps its saves
// there; what the command line kept under the raw name moves with the start,
// and a quick-load check before it only reads.
func TestAGameWhoseSaveOwnerIsRefusedStartsUnderAMappedOwner(t *testing.T) {
	root := t.TempDir()
	gameRoot := filepath.Join(root, "games")
	if err := os.MkdirAll(filepath.Join(gameRoot, "lgt"), 0o755); err != nil {
		t.Fatal(err)
	}
	const raw = "PF:0001"
	mapped := backend.AcceptedSaveOwner(raw)
	if mapped == raw || ownerRejected(mapped) {
		t.Fatalf("owner %q was mapped to %q", raw, mapped)
	}
	if err := os.WriteFile(filepath.Join(gameRoot, "lgt", "colon.zip"), repackedLGTFixture(t, raw), 0o644); err != nil {
		t.Fatal(err)
	}
	lgtRoot := filepath.Join(root, "savedata", "lgt")
	earlier := filepath.Join(lgtRoot, raw)
	if runtime.GOOS != "windows" {
		if err := backend.NewDirectorySaveStore(earlier).StoreSave("fs/save.dat", []byte("kept")); err != nil {
			t.Fatal(err)
		}
	}
	server := newTestServer(t, Options{
		GameRoot: gameRoot,
		SaveRoot: filepath.Join(root, "savedata", "ktf"),
		LogRoot:  filepath.Join(root, "logs"),
	})
	if err := server.quickLoadRefusal("games/lgt/colon.zip"); err == nil {
		t.Fatal("a quick load with no slot was not refused")
	}
	if runtime.GOOS != "windows" {
		if _, err := os.Stat(earlier); err != nil {
			t.Fatalf("a quick-load check moved the saves: %v", err)
		}
	}
	httpServer := httptest.NewServer(server)
	t.Cleanup(httpServer.Close)
	connection, _, err := wsproto.Dial("ws://"+strings.TrimPrefix(httpServer.URL, "http://")+"/api/session", nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = connection.Close() })

	expectMessage(t, connection, serverReady)
	send(t, connection, clientMessage{Kind: clientStart, Game: "games/lgt/colon.zip", ID: 3})
	if started := expectMessage(t, connection, serverStarted); started.Started == nil || started.Started.SaveOwner != mapped {
		t.Fatalf("started = %+v, want the owner %q", started.Started, mapped)
	}
	if runtime.GOOS != "windows" {
		if data, err := os.ReadFile(filepath.Join(lgtRoot, mapped, "fs", "save.dat")); err != nil || string(data) != "kept" {
			t.Fatalf("saves under the mapped owner = %q, %v", data, err)
		}
		if _, err := os.Stat(earlier); !os.IsNotExist(err) {
			t.Fatalf("the earlier owner directory is still there: %v", err)
		}
	}
}
