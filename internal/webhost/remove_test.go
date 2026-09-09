package webhost

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/movingwoo/wfeature/internal/wsproto"
)

// remove asks for one game to go, the way the page does: the picker's own path
// in the query.
func remove(t *testing.T, server *Server, game string) *httptest.ResponseRecorder {
	t.Helper()
	target := "/api/games?" + url.Values{removeGameQuery: {game}}.Encode()
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, httptest.NewRequest(http.MethodDelete, target, nil))
	return recorder
}

// The whole reason this half of the route exists: on a phone the directory a
// game lands in cannot be opened by anything else, so a game added by mistake
// would stay there for as long as the app is installed.
func TestAGameAddedFromThePageCanBeRemovedFromIt(t *testing.T) {
	addedRoot := t.TempDir()
	server := newTestServer(t, Options{AddedRoot: addedRoot})

	if recorder := upload(t, server, "한글이름.zip", []byte("PK\x03\x04")); recorder.Code != http.StatusOK {
		t.Fatalf("upload = %d: %s", recorder.Code, recorder.Body)
	}
	games := ListGames("", addedRoot)
	if len(games) != 1 {
		t.Fatalf("the picker lists %+v", games)
	}

	// The page sends back exactly what the listing gave it, percent-encoding
	// and all, which for a Korean name is most of the string.
	if recorder := remove(t, server, games[0].Path); recorder.Code != http.StatusOK {
		t.Fatalf("remove = %d: %s", recorder.Code, recorder.Body)
	}
	if _, err := os.Stat(filepath.Join(addedRoot, "한글이름.zip")); !os.IsNotExist(err) {
		t.Fatalf("the archive is still there: %v", err)
	}
	if games := ListGames("", addedRoot); len(games) != 0 {
		t.Errorf("the picker still lists %+v", games)
	}
}

// The library beside the server is somebody's own, put there by hand and often
// by the hundred. A button on a page is not what stands between it and a
// deletion, so the server refuses even when the page asks.
func TestTheLibraryIsNotThePagesToRemove(t *testing.T) {
	gameRoot := t.TempDir()
	if err := os.MkdirAll(filepath.Join(gameRoot, "ktf"), 0o755); err != nil {
		t.Fatal(err)
	}
	kept := filepath.Join(gameRoot, "ktf", "game.zip")
	if err := os.WriteFile(kept, []byte("PK\x03\x04"), 0o644); err != nil {
		t.Fatal(err)
	}
	server := newTestServer(t, Options{GameRoot: gameRoot, AddedRoot: t.TempDir()})

	recorder := remove(t, server, "games/ktf/game.zip")
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", recorder.Code)
	}
	// The sentence is for the person who pressed the button, so it says where
	// the file is rather than which rule refused it.
	if !strings.Contains(recorder.Body.String(), "직접 넣은 것이라") {
		t.Errorf("body = %q", recorder.Body.String())
	}
	if _, err := os.Stat(kept); err != nil {
		t.Fatalf("the library archive is gone: %v", err)
	}
}

// A removal is a path from the network reaching os.Remove, so the paths worth
// trying are the ones that climb.
func TestARemovalCannotReachOutsideTheAddedRoot(t *testing.T) {
	parent := t.TempDir()
	addedRoot := filepath.Join(parent, "ext")
	if err := os.MkdirAll(addedRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	neighbour := filepath.Join(parent, "neighbour.zip")
	if err := os.WriteFile(neighbour, []byte("PK\x03\x04"), 0o644); err != nil {
		t.Fatal(err)
	}
	server := newTestServer(t, Options{GameRoot: t.TempDir(), AddedRoot: addedRoot})

	for _, game := range []string{
		"ext/../neighbour.zip",
		"ext/%2e%2e/neighbour.zip",
		"../neighbour.zip",
		"/etc/passwd",
		"ext",
		"",
	} {
		if recorder := remove(t, server, game); recorder.Code == http.StatusOK {
			t.Errorf("%q was removed", game)
		}
	}
	if _, err := os.Stat(neighbour); err != nil {
		t.Fatalf("a removal reached outside the added root: %v", err)
	}
}

// A stale list is the ordinary way to ask for a game that is not there — two
// tabs, one of them a minute behind. The page reloads its list either way.
func TestRemovingAGameThatIsGoneSaysSo(t *testing.T) {
	server := newTestServer(t, Options{AddedRoot: t.TempDir()})
	recorder := remove(t, server, "ext/missing.zip")
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", recorder.Code)
	}
	if !strings.Contains(recorder.Body.String(), "이미 없습니다") {
		t.Errorf("body = %q", recorder.Body.String())
	}
}

// Adding the same file again lands on the same progress, which is what makes a
// removal something a player can change their mind about.
func TestRemovingAGameLeavesItsSavesAlone(t *testing.T) {
	addedRoot, saveRoot := t.TempDir(), filepath.Join(t.TempDir(), "ktf")
	save := filepath.Join(saveRoot, "0102DD43", "db", "SaveData")
	if err := os.MkdirAll(filepath.Dir(save), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(save, []byte("progress"), 0o644); err != nil {
		t.Fatal(err)
	}
	server := newTestServer(t, Options{AddedRoot: addedRoot, SaveRoot: saveRoot})

	if recorder := upload(t, server, "game.zip", []byte("PK\x03\x04")); recorder.Code != http.StatusOK {
		t.Fatalf("upload = %d", recorder.Code)
	}
	if recorder := remove(t, server, "ext/game.zip"); recorder.Code != http.StatusOK {
		t.Fatalf("remove = %d: %s", recorder.Code, recorder.Body)
	}
	if _, err := os.Stat(save); err != nil {
		t.Fatalf("the save went with the game: %v", err)
	}
}

// The two roots are one library to the picker and two to everything that
// touches a file, so the listing has to say which root each game came out of.
func TestThePickerListsBothRootsAndSaysWhichIsWhich(t *testing.T) {
	gameRoot, addedRoot := t.TempDir(), t.TempDir()
	if err := os.MkdirAll(filepath.Join(gameRoot, "ktf"), 0o755); err != nil {
		t.Fatal(err)
	}
	for path, root := range map[string]string{"ktf/library.zip": gameRoot, "added.zip": addedRoot} {
		if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(path)), []byte("PK"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	games := ListGames(gameRoot, addedRoot)
	want := []Game{
		{Group: "ktf", Name: "library", Path: "games/ktf/library.zip"},
		{Group: "", Name: "added", Path: "ext/added.zip", Added: true},
	}
	if len(games) != len(want) {
		t.Fatalf("the picker lists %+v", games)
	}
	for index, game := range games {
		if game != want[index] {
			t.Errorf("games[%d] = %+v, want %+v", index, game, want[index])
		}
	}
	// One directory named as both roots lists once. A host that does that has
	// made a mistake, and the mistake worth refusing is the one that would
	// offer the library as the page's to delete.
	if games := ListGames(gameRoot, gameRoot); len(games) != 1 || games[0].Added {
		t.Errorf("one directory as both roots lists %+v", games)
	}
}

// A game in the added root is a game: it plays, on the same start message and
// through the same resolver as one from the library. The picker offers the two
// in one list and this is what makes that honest.
func TestAGameInTheAddedRootPlays(t *testing.T) {
	archive, err := os.ReadFile(filepath.Join("..", "platform", "skt", "testdata", "canvas-skt.zip"))
	if err != nil {
		t.Fatalf("read the canvas fixture: %v", err)
	}
	root := t.TempDir()
	addedRoot := filepath.Join(root, "ext")
	if err := os.MkdirAll(addedRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	// The name is Korean because these names are, and because a path that
	// survives escaping and unescaping on the way to a file is the half of
	// this that is easy to get wrong.
	if err := os.WriteFile(filepath.Join(addedRoot, "한글이름.zip"), archive, 0o644); err != nil {
		t.Fatal(err)
	}
	server := newTestServer(t, Options{
		GameRoot:  filepath.Join(root, "games"),
		AddedRoot: addedRoot,
		SaveRoot:  filepath.Join(root, "savedata", "ktf"),
		LogRoot:   filepath.Join(root, "logs"),
	})
	httpServer := httptest.NewServer(server)
	t.Cleanup(httpServer.Close)

	connection, _, err := wsproto.Dial("ws://"+strings.TrimPrefix(httpServer.URL, "http://")+"/api/session", nil)
	if err != nil {
		t.Fatalf("dial the session: %v", err)
	}
	t.Cleanup(func() { _ = connection.Close() })
	expectMessage(t, connection, serverReady)

	games := ListGames("", addedRoot)
	if len(games) != 1 {
		t.Fatalf("the picker lists %+v", games)
	}
	send(t, connection, clientMessage{Kind: clientStart, Game: games[0].Path})
	started := expectMessage(t, connection, serverStarted)
	if started.Started == nil || started.Started.Platform != "skt" {
		t.Fatalf("started = %+v", started.Started)
	}
	// Saves are keyed by what is in the archive rather than by where it sits,
	// which is what lets a removed game be added again onto its own progress.
	if started.Started.SaveOwner == "" {
		t.Error("no save owner")
	}
}
