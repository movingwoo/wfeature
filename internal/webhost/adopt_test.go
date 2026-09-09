package webhost

import (
	"os"
	"path/filepath"
	"testing"
)

func writeArchive(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("PK\x03\x04"+filepath.Base(path)), 0o644); err != nil {
		t.Fatal(err)
	}
}

// The upgrade this exists for: a game added from the page before there was an
// added root is in the game root, where the page cannot reach it to remove it.
func TestTheUpgradeMovesWhatIsLooseInTheGameRoot(t *testing.T) {
	root := t.TempDir()
	gameRoot, addedRoot := filepath.Join(root, "games"), filepath.Join(root, "ext")
	writeArchive(t, filepath.Join(gameRoot, "한글이름.zip"))
	writeArchive(t, filepath.Join(gameRoot, "midlet.jar"))
	writeArchive(t, filepath.Join(gameRoot, "ktf", "library.zip"))
	writeArchive(t, filepath.Join(gameRoot, "notes.txt"))

	if moved := AdoptLooseGames(gameRoot, addedRoot, nil); moved != 2 {
		t.Fatalf("moved %d, want 2", moved)
	}
	for _, name := range []string{"한글이름.zip", "midlet.jar"} {
		if _, err := os.Stat(filepath.Join(addedRoot, name)); err != nil {
			t.Errorf("%s did not arrive: %v", name, err)
		}
		if _, err := os.Stat(filepath.Join(gameRoot, name)); !os.IsNotExist(err) {
			t.Errorf("%s is still in the game root", name)
		}
	}
	// A group directory is a library somebody assembled, and anything that is
	// not an archive is not this move's business.
	if _, err := os.Stat(filepath.Join(gameRoot, "ktf", "library.zip")); err != nil {
		t.Errorf("a grouped archive moved: %v", err)
	}
	if _, err := os.Stat(filepath.Join(gameRoot, "notes.txt")); err != nil {
		t.Errorf("a file that is not an archive moved: %v", err)
	}

	// And the picker offers what moved as the page's to delete.
	games := ListGames(gameRoot, addedRoot)
	for _, game := range games {
		if (game.Group == "") != game.Added {
			t.Errorf("after the move the picker offers %+v", game)
		}
	}
}

// It is an upgrade step, not a standing rule. A person may drop an archive
// straight into the game root by hand — the README has always said so — and a
// sweep that ran every start would keep filing those somewhere they did not
// choose.
func TestTheMoveRunsOnce(t *testing.T) {
	root := t.TempDir()
	gameRoot, addedRoot := filepath.Join(root, "games"), filepath.Join(root, "ext")
	writeArchive(t, filepath.Join(gameRoot, "first.zip"))

	if moved := AdoptLooseGames(gameRoot, addedRoot, nil); moved != 1 {
		t.Fatalf("moved %d, want 1", moved)
	}
	writeArchive(t, filepath.Join(gameRoot, "dropped-in-by-hand.zip"))
	if moved := AdoptLooseGames(gameRoot, addedRoot, nil); moved != 0 {
		t.Fatalf("a second run moved %d", moved)
	}
	if _, err := os.Stat(filepath.Join(gameRoot, "dropped-in-by-hand.zip")); err != nil {
		t.Errorf("a later drop was taken anyway: %v", err)
	}
}

// A fresh install has nothing to move and must not be marked as though it
// failed to: the marker is written either way, and the next start is quiet.
func TestAFreshInstallIsNothingToDo(t *testing.T) {
	root := t.TempDir()
	gameRoot, addedRoot := filepath.Join(root, "games"), filepath.Join(root, "ext")
	if moved := AdoptLooseGames(gameRoot, addedRoot, nil); moved != 0 {
		t.Fatalf("moved %d from a game root that is not there", moved)
	}
	if _, err := os.Stat(filepath.Join(addedRoot, adoptedMarker)); err != nil {
		t.Errorf("the move was not recorded as done: %v", err)
	}
	// The marker is not a game.
	if games := ListGames(gameRoot, addedRoot); len(games) != 0 {
		t.Errorf("the picker lists %+v", games)
	}
}

// Two files, one name, and nothing here can tell whether they are the same
// game. Neither is worth losing, and the move stays unfinished so a later
// start can complete it.
func TestANameAlreadyTakenIsLeftAlone(t *testing.T) {
	root := t.TempDir()
	gameRoot, addedRoot := filepath.Join(root, "games"), filepath.Join(root, "ext")
	writeArchive(t, filepath.Join(gameRoot, "same.zip"))
	writeArchive(t, filepath.Join(addedRoot, "same.zip"))
	writeArchive(t, filepath.Join(gameRoot, "other.zip"))

	if moved := AdoptLooseGames(gameRoot, addedRoot, nil); moved != 1 {
		t.Fatalf("moved %d, want the one that could move", moved)
	}
	kept, err := os.ReadFile(filepath.Join(addedRoot, "same.zip"))
	if err != nil || string(kept) != "PK\x03\x04same.zip" {
		t.Fatalf("the file in the added root was overwritten: %q %v", kept, err)
	}
	if _, err := os.Stat(filepath.Join(gameRoot, "same.zip")); err != nil {
		t.Errorf("the game root's copy is gone: %v", err)
	}
	if _, err := os.Stat(filepath.Join(addedRoot, adoptedMarker)); !os.IsNotExist(err) {
		t.Error("an unfinished move was recorded as done")
	}
}

// A host with one root, or with none, has nothing to move between.
func TestNoSecondRootIsNoMove(t *testing.T) {
	root := t.TempDir()
	gameRoot := filepath.Join(root, "games")
	writeArchive(t, filepath.Join(gameRoot, "game.zip"))
	for _, addedRoot := range []string{"", gameRoot} {
		if moved := AdoptLooseGames(gameRoot, addedRoot, nil); moved != 0 {
			t.Errorf("added root %q moved %d", addedRoot, moved)
		}
	}
	if _, err := os.Stat(filepath.Join(gameRoot, "game.zip")); err != nil {
		t.Errorf("the archive moved anyway: %v", err)
	}
}
