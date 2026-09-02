package skt

import (
	"os"
	"path/filepath"
	"testing"
)

// container builds an SKT archive: a .msd naming the title beside the JAR it
// describes. The JAR's contents do not matter here — a save owner is read from
// the descriptor, and reading it must not cost the class file.
func container(t *testing.T, progName, mainClass string) []byte {
	t.Helper()
	descriptor := "MIDlet-Name: Title\r\nDD-ProgName: " + progName +
		"\r\nMIDlet-1: Title, , " + mainClass + "\r\n"
	return makeJAR(t, map[string][]byte{
		"game.msd": []byte(descriptor),
		"game.jar": makeJAR(t, map[string][]byte{"unused": []byte("x")}),
	})
}

func write(t *testing.T, root, name string, data []byte) {
	t.Helper()
	full := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

// A repack can carry another game's program number, and then two titles open
// one record store and overwrite each other. This is the third platform where
// that is possible and the check used to cover only the other two.
func TestTwoTitlesUnderOneProgramNumberCollide(t *testing.T) {
	root := t.TempDir()
	write(t, root, "skt/one.zip", container(t, "PG0001", "one.Main"))
	write(t, root, "skt/two.zip", container(t, "PG0001", "two.Main"))

	collisions, err := SaveOwnerCollisions(root)
	if err != nil {
		t.Fatalf("SaveOwnerCollisions() error = %v", err)
	}
	if len(collisions) != 1 {
		t.Fatalf("SaveOwnerCollisions() = %d collisions, want 1", len(collisions))
	}
	if collisions[0].Owner != "PG0001" || len(collisions[0].Claims) != 2 {
		t.Fatalf("collision = %+v", collisions[0])
	}
}

// One title shipped twice — a re-release, a modified copy — shares its program
// number on purpose and runs the same class. Reporting that would train the
// reader to ignore the tool.
func TestOneTitleShippedTwiceIsNotACollision(t *testing.T) {
	root := t.TempDir()
	write(t, root, "skt/original.zip", container(t, "PG0002", "same.Main"))
	write(t, root, "skt/modified.zip", container(t, "PG0002", "same.Main"))

	collisions, err := SaveOwnerCollisions(root)
	if err != nil {
		t.Fatalf("SaveOwnerCollisions() error = %v", err)
	}
	if len(collisions) != 0 {
		t.Fatalf("SaveOwnerCollisions() = %+v, want none", collisions)
	}
}

// The scan stops at the Host's boundary, so an archive filed in a diagnostic
// corpus below it is not reported against one a Host can actually start.
func TestTheScanStopsAtTheHostsBoundary(t *testing.T) {
	root := t.TempDir()
	write(t, root, "skt/one.zip", container(t, "PG0003", "one.Main"))
	write(t, root, "test/corpus/two.zip", container(t, "PG0003", "two.Main"))

	collisions, err := SaveOwnerCollisions(root)
	if err != nil {
		t.Fatalf("SaveOwnerCollisions() error = %v", err)
	}
	if len(collisions) != 0 {
		t.Fatalf("SaveOwnerCollisions() = %+v, want none: the second archive is below the boundary", collisions)
	}
}

// Archives of the other two platforms sit in neighbouring directories, and a
// scan that fails on one of them would report nothing at all.
func TestArchivesOfOtherPlatformsAreSkipped(t *testing.T) {
	root := t.TempDir()
	write(t, root, "ktf/not-skt.zip", []byte("not an archive of any kind"))
	write(t, root, "skt/one.zip", container(t, "PG0004", "one.Main"))

	if _, err := SaveOwnerCollisions(root); err != nil {
		t.Fatalf("SaveOwnerCollisions() error = %v", err)
	}
}
