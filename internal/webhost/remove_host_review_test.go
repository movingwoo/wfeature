package webhost

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

// What the page may delete is what it added: an archive, under a name the
// upload would have accepted. The added root also holds the server's own
// bookkeeping — the marker that keeps the one-time adoption from running
// again, an upload still being written — and none of that is a game.
func TestRemovalReachesOnlyTheArchivesThePageAdded(t *testing.T) {
	addedRoot := t.TempDir()
	server := newTestServer(t, Options{AddedRoot: addedRoot})
	for _, name := range []string{adoptedMarker, ".upload-123456", "notes.txt", ".hidden.zip"} {
		file := filepath.Join(addedRoot, name)
		if err := os.WriteFile(file, nil, 0o644); err != nil {
			t.Fatal(err)
		}
		if recorder := remove(t, server, "ext/"+name); recorder.Code != http.StatusBadRequest {
			t.Errorf("removing %q answered %d, want 400", name, recorder.Code)
		}
		if _, err := os.Stat(file); err != nil {
			t.Errorf("%q is gone: %v", name, err)
		}
	}

	// The archive beside them still goes.
	if err := os.WriteFile(filepath.Join(addedRoot, "game.JAR"), []byte("PK\x03\x04"), 0o644); err != nil {
		t.Fatal(err)
	}
	if recorder := remove(t, server, "ext/game.JAR"); recorder.Code != http.StatusOK {
		t.Fatalf("removing an added archive answered %d: %s", recorder.Code, recorder.Body)
	}
}
