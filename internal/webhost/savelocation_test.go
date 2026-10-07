package webhost

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/movingwoo/wfeature/internal/backend"
)

// The save locations a release before the file lock ran on, and what a start
// is told when the claim really is refused.

// closeAnotherGame is the advice only contention earns.
const closeAnotherGame = "다른 실행 중인 게임이나 도구를 종료"

// fallbackWarning is the line the server logs for a directory only this
// process is kept out of.
const fallbackWarning = "cannot hold a file lock"

// serverLog is a log a session's goroutines may write while a test reads it.
type serverLog struct {
	mutex sync.Mutex
	text  bytes.Buffer
}

func (log *serverLog) Write(data []byte) (int, error) {
	log.mutex.Lock()
	defer log.mutex.Unlock()
	return log.text.Write(data)
}

func (log *serverLog) count(text string) int {
	log.mutex.Lock()
	defer log.mutex.Unlock()
	return strings.Count(log.text.String(), text)
}

func startCheckpointGame(t *testing.T, server *Server) (*sessionRunner, []serverMessage) {
	t.Helper()
	r := &sessionRunner{server: server, frames: make(chan pendingFrame, 1), outText: make(chan outboundMessage, 64)}
	r.startGame(t.Context(), clientMessage{Kind: clientStart, Game: "games/ktf/checkpoint.zip", ID: 1})
	t.Cleanup(r.stopGame)
	return r, readCheckpointReplies(t, r)
}

// A per-game save directory that is a link is every road's directory: the save
// API, the backup routes and a session all go through it, and what they write
// is in the directory the link names.
func TestLinkedSaveDirectoryServesEveryRoadAndASession(t *testing.T) {
	root, archive := checkpointServerFiles(t)
	server := checkpointServer(t, root)
	directory := server.saveDirectory("ktf", "P0001")
	target := filepath.Join(t.TempDir(), "kept elsewhere")
	for _, path := range []string{target, filepath.Dir(directory)} {
		if err := os.MkdirAll(path, 0755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(target, directory); err != nil {
		t.Skipf("this platform cannot make the link the case needs: %v", err)
	}
	inTarget := func(key string) string {
		t.Helper()
		data, err := os.ReadFile(filepath.Join(target, filepath.FromSlash(key)))
		if err != nil {
			t.Fatalf("%s is not in the directory the link names: %v", key, err)
		}
		return string(data)
	}

	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, httptest.NewRequest(http.MethodPut, "/api/saves/P0001/db/slot", strings.NewReader("written")))
	if recorder.Code != http.StatusNoContent || inTarget("db/slot") != "written" {
		t.Fatalf("save API write through a link = %d: %s", recorder.Code, recorder.Body.String())
	}
	var listed saveResponse
	recorder = get(t, server, "/api/saves/P0001")
	if err := json.Unmarshal(recorder.Body.Bytes(), &listed); err != nil || recorder.Code != http.StatusOK || len(listed.Saves) != 1 || listed.Saves["db/slot"] == "" {
		t.Fatalf("save API listing through a link = %d: %s", recorder.Code, recorder.Body.String())
	}

	exported := savePackRequest(t, server, http.MethodGet, "games/ktf/checkpoint.zip", nil)
	if exported.Code != http.StatusOK {
		t.Fatalf("export through a link = %d: %s", exported.Code, exported.Body.String())
	}
	pack, err := backend.DecodeSavePack(exported.Body.Bytes())
	if err != nil || len(pack.Entries) != 1 || pack.Entries[0].Key != "db/slot" || string(pack.Entries[0].Data) != "written" {
		t.Fatalf("export through a link held %+v, %v", pack.Entries, err)
	}
	container, err := backend.EncodeSavePack(backend.SavePack{Identity: backend.SaveIdentity(archive), Entries: []backend.SaveEntry{
		{Key: "fs/restored", Data: []byte("restored")},
	}})
	if err != nil {
		t.Fatal(err)
	}
	imported := savePackRequest(t, server, http.MethodPost, "games/ktf/checkpoint.zip", container)
	var result savePackResult
	if err := json.Unmarshal(imported.Body.Bytes(), &result); err != nil || imported.Code != http.StatusOK || result.Written != 1 || result.Removed != 1 {
		t.Fatalf("import through a link = %d: %s", imported.Code, imported.Body.String())
	}
	if inTarget("fs/restored") != "restored" {
		t.Fatal("the import did not land in the link target")
	}
	if _, err := os.Lstat(filepath.Join(target, "db")); !os.IsNotExist(err) {
		t.Fatalf("the import left an entry the backup does not hold: %v", err)
	}

	r, replies := startCheckpointGame(t, server)
	if r.game == nil {
		t.Fatalf("a linked save directory refused the start: %+v", replies)
	}
	if held, _ := server.holdSaveDirectory(directory, "another writer", false); held {
		server.releaseSaveDirectory(directory)
		t.Fatal("the session did not hold its claim on a linked directory")
	}
	// A quick save holds no save and its slot is a file beside the link, so it
	// works here as anywhere, and the saves the link leads to stay as they were.
	r.quickSave(t.Context(), clientMessage{Kind: clientQuickSave, ID: 2})
	if replies := readCheckpointReplies(t, r); !r.started.CanCheckpoint || len(replies) != 1 || replies[0].Kind != serverResult || !r.started.HasCheckpoint {
		t.Fatalf("quick save on a linked directory = %+v", replies)
	}
	if inTarget("fs/restored") != "restored" {
		t.Fatal("the quick save changed the saves in the link target")
	}
	slots, err := filepath.Glob(filepath.Join(target, "*.wfq"))
	if err != nil || len(slots) != 0 {
		t.Fatalf("the quick save is in the link target: %q, %v", slots, err)
	}
	r.stopGame()
	if server.claimCount() != 0 {
		t.Fatal("stopping the game left the linked directory claimed")
	}
	if info, err := os.Lstat(directory); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("the save directory is no longer the link it was: %v", err)
	}
}

// A refused claim has two different meanings and the person can act on only
// one of them. A directory another session holds is closed in that session; a
// save folder that could not be prepared has nothing to close, so it must not
// be told to.
func TestStartRefusalSaysWhatActuallyFailed(t *testing.T) {
	t.Run("contention", func(t *testing.T) {
		root, _ := checkpointServerFiles(t)
		holder, server := checkpointServer(t, root), checkpointServer(t, root)
		directory := holder.saveDirectory("ktf", "P0001")
		if ok, reason := holder.claimSaveDirectory(directory, "fixture"); !ok {
			t.Fatal(reason)
		}
		defer holder.releaseSaveDirectory(directory)
		r, replies := startCheckpointGame(t, server)
		if r.game != nil || len(replies) != 1 || replies[0].Kind != serverError {
			t.Fatalf("a held directory did not refuse the start: %+v", replies)
		}
		if !strings.Contains(replies[0].Message, closeAnotherGame) {
			t.Fatalf("contention was not told what to close: %q", replies[0].Message)
		}
	})
	t.Run("unusable folder", func(t *testing.T) {
		root, _ := checkpointServerFiles(t)
		server := checkpointServer(t, root)
		// The directory the lock keeps beside the saves is a file: nothing is
		// running, and nothing this build made is there.
		reserved := filepath.Join(filepath.Dir(server.saveDirectory("ktf", "P0001")), ".wfeature-quicksave")
		if err := os.MkdirAll(filepath.Dir(reserved), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(reserved, []byte("not a directory"), 0600); err != nil {
			t.Fatal(err)
		}
		r, replies := startCheckpointGame(t, server)
		if r.game != nil || len(replies) != 1 || replies[0].Kind != serverError {
			t.Fatalf("an unusable save folder did not refuse the start: %+v", replies)
		}
		if strings.Contains(replies[0].Message, closeAnotherGame) {
			t.Fatalf("an unusable save folder was reported as a running game: %q", replies[0].Message)
		}
		if !strings.Contains(replies[0].Message, "not a directory") {
			t.Fatalf("the refusal does not name its cause: %q", replies[0].Message)
		}
		if server.claimCount() != 0 || r.admitted {
			t.Fatal("a refused start kept a claim")
		}
	})
}

// A save root that cannot be written cannot hold the file lock either. The
// game starts anyway, as it did before that lock existed, on a claim held in
// this process alone: the server says so once, and a second session on the
// same directory is still refused as the contention it is.
func TestReadOnlySaveRootStartsOnAnInProcessClaim(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows does not refuse a write by directory mode bits")
	}
	if os.Geteuid() == 0 {
		t.Skip("the superuser is not refused by directory mode bits")
	}
	root, _ := checkpointServerFiles(t)
	saveRoot := filepath.Join(root, "saves", "ktf")
	if err := os.MkdirAll(saveRoot, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(saveRoot, 0555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(saveRoot, 0755) })

	log := &serverLog{}
	options := Options{GameRoot: filepath.Join(root, "games"), SaveRoot: saveRoot, LogRoot: filepath.Join(root, "logs"), Logger: backend.NewLogger(log)}
	server, competitor := newTestServer(t, options), newTestServer(t, options)
	directory := server.saveDirectory("ktf", "P0001")

	r, replies := startCheckpointGame(t, server)
	if r.game == nil {
		t.Fatalf("a read-only save root refused the start: %+v", replies)
	}
	if cause, fallback := backend.SaveLockFallback(directory); !fallback || !os.IsPermission(cause) {
		t.Fatalf("fallback = %t, %v; want the permission failure recorded", fallback, cause)
	}
	// The claim is where the server says it, before anything else reads.
	if reported := log.count(fallbackWarning); reported != 1 {
		t.Fatalf("the claim reported the fallback %d times, want once", reported)
	}
	other, refused := startCheckpointGame(t, competitor)
	if other.game != nil || len(refused) != 1 || !strings.Contains(refused[0].Message, closeAnotherGame) {
		t.Fatalf("a second session on a read-only save root = %+v", refused)
	}
	// Reads are served while the game holds its claim, as on any other root.
	if recorder := get(t, server, "/api/saves/P0001"); recorder.Code != http.StatusOK {
		t.Fatalf("listing on a read-only save root = %d: %s", recorder.Code, recorder.Body.String())
	}
	r.stopGame()
	// A write fails for the reason it always did, which is the save itself.
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, httptest.NewRequest(http.MethodPut, "/api/saves/P0001/db/slot", strings.NewReader("x")))
	if recorder.Code != http.StatusInternalServerError || log.count("save could not be written") != 1 || log.count("permission denied") == 0 {
		t.Fatalf("write on a read-only save root = %d: %s", recorder.Code, recorder.Body.String())
	}
	if again, replies := startCheckpointGame(t, competitor); again.game == nil {
		t.Fatalf("a released in-process claim stayed held: %+v", replies)
	}
	if reported := log.count(fallbackWarning); reported != 1 {
		t.Fatalf("the fallback was reported %d times, want once for the directory", reported)
	}
}

// A listing and an export take no claim, so on a server where nothing has been
// started they are the first to meet a save root that cannot hold the lock.
// Each says so for its own directory, once.
func TestUnclaimedReadsReportASaveRootWithoutAFileLock(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows does not refuse a write by directory mode bits")
	}
	if os.Geteuid() == 0 {
		t.Skip("the superuser is not refused by directory mode bits")
	}
	root, _ := checkpointServerFiles(t)
	saveRoot := filepath.Join(root, "saves", "ktf")
	if err := os.MkdirAll(saveRoot, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(saveRoot, 0555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(saveRoot, 0755) })
	log := &serverLog{}
	server := newTestServer(t, Options{GameRoot: filepath.Join(root, "games"), SaveRoot: saveRoot, LogRoot: filepath.Join(root, "logs"), Logger: backend.NewLogger(log)})

	for range 2 {
		if recorder := get(t, server, "/api/saves/LISTED"); recorder.Code != http.StatusOK {
			t.Fatalf("listing on a read-only save root = %d: %s", recorder.Code, recorder.Body.String())
		}
	}
	if reported := log.count(fallbackWarning); reported != 1 {
		t.Fatalf("a listing reported the fallback %d times, want once", reported)
	}
	// The game has never saved, which is the export's own answer; the read
	// that found that out is what met the directory.
	for range 2 {
		if exported := savePackRequest(t, server, http.MethodGet, "games/ktf/checkpoint.zip", nil); exported.Code != http.StatusNotFound {
			t.Fatalf("export on a read-only save root = %d: %s", exported.Code, exported.Body.String())
		}
	}
	if reported := log.count(fallbackWarning); reported != 2 {
		t.Fatalf("an export of another directory left the report at %d, want 2", reported)
	}
}
