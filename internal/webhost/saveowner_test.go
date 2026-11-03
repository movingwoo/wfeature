package webhost

import (
	"archive/zip"
	"bytes"
	"io"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	goruntime "runtime"
	"strings"
	"testing"

	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/platform/ktf"
	"github.com/movingwoo/wfeature/internal/platform/lgt"
	"github.com/movingwoo/wfeature/internal/platform/skt"
	"github.com/movingwoo/wfeature/internal/session"
	"github.com/movingwoo/wfeature/internal/wsproto"
)

// Every platform's owner is one this Host accepts: an owner the platform
// answers that the Host refuses is a title started with nowhere to keep its
// saves, or, since that is refused, a title that does not start at all.
func TestSaveOwnersAreOwnersTheHostAccepts(t *testing.T) {
	names := []string{
		"Title: Part 2", `a*b?c"d<e>f|g`, "tab\there", "‮eman", "trailing.", " spaced ",
		strings.Repeat("가", 30), strings.Repeat("x", 65), "..", ".", "back\\slash", "DEL\x7f",
		".wfeature-quicksave",
	}
	for _, name := range names {
		for platform, owner := range map[string]string{
			"skt": skt.SaveOwner(skt.Descriptor{Name: name}),
			"ktf": ktf.SaveOwner(ktf.Descriptor{PID: name}),
			"lgt": lgt.SaveOwner(lgt.Descriptor{PID: name}),
		} {
			if owner == "" || ownerRejected(owner) {
				t.Errorf("name %q: %s owner %q is refused by the Host", name, platform, owner)
			}
		}
	}
}

// Saves the command line kept under a raw owner the Host refused move to the
// owner this build names before a start claims the directory, below the same
// platform root saveDirectory joins the owner to.
func TestAStartMovesSavesKeptUnderTheEarlierOwnerName(t *testing.T) {
	if goruntime.GOOS == "windows" {
		t.Skip("the earlier owner name cannot exist on this file system")
	}
	archive := sktArchiveNamed(t, "Puzzle: Deluxe")
	summary, err := session.Inspect(archive)
	if err != nil {
		t.Fatal(err)
	}
	if summary.SaveOwner == "Puzzle: Deluxe" || ownerRejected(summary.SaveOwner) {
		t.Fatalf("owner %q was not made into one the Host accepts", summary.SaveOwner)
	}
	data := t.TempDir()
	server := newTestServer(t, Options{SaveRoot: filepath.Join(data, "ktf")})
	legacy := backend.NewDirectorySaveStore(filepath.Join(data, "skt", "Puzzle: Deluxe"))
	if err := legacy.StoreSave("rms/save", []byte{1, 2, 3}); err != nil {
		t.Fatal(err)
	}

	server.migrateSaveOwner(summary)

	directory := server.saveDirectory(summary.Platform, summary.SaveOwner)
	if directory == "" {
		t.Fatal("no save directory for the migrated owner")
	}
	got, err := os.ReadFile(filepath.Join(directory, "rms", "save"))
	if err != nil || !bytes.Equal(got, []byte{1, 2, 3}) {
		t.Fatalf("migrated save = %v, %v; want [1 2 3]", got, err)
	}
	if _, err := os.Stat(filepath.Join(data, "skt", "Puzzle: Deluxe")); !os.IsNotExist(err) {
		t.Fatalf("the earlier owner directory is still there: %v", err)
	}
}

// sktArchiveNamed answers the RecordStore fixture MIDlet packaged the way an
// SKT title ships, a .msd beside its JAR, under another name.
func sktArchiveNamed(t *testing.T, name string) []byte {
	t.Helper()
	jar, err := os.ReadFile(filepath.Join("..", "platform", "skt", "testdata", "recordstore.jar"))
	if err != nil {
		t.Fatal(err)
	}
	descriptor := "MIDlet-Name: " + name + "\r\nMIDlet-1: " + name + ", , RecordStoreMIDlet\r\n"
	var out bytes.Buffer
	writer := zip.NewWriter(&out)
	for _, file := range []struct {
		name    string
		content []byte
	}{{"game.msd", []byte(descriptor)}, {"game.jar", jar}} {
		entry, err := writer.Create(file.name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write(file.content); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

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
