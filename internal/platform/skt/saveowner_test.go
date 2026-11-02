package skt

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"testing"

	"github.com/movingwoo/wfeature/internal/backend"
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

// storeOwnerRefused is the web host's owner rule, restated so the test
// does not take the platform's own copy of it on trust.
func storeOwnerRefused(owner string) bool {
	if owner == "" || len(owner) > 64 || owner == "." || owner == ".." {
		return true
	}
	for _, character := range owner {
		switch {
		case character < 0x20, character == 0x7f, strings.ContainsRune(`/\:*?"<>|`, character),
			character == 0x200e, character == 0x200f, character == 0x061c,
			character >= 0x202a && character <= 0x202e, character >= 0x2066 && character <= 0x2069,
			character == 0xfeff:
			return true
		}
	}
	return false
}

// Every owner satisfies the Host's rule, so the browser never runs a title
// with no save store; an owner that already did is left exactly as it was,
// because existing saves live under it.
func TestSaveOwnerSatisfiesTheHostOwnerRule(t *testing.T) {
	long := strings.Repeat("가", 30)
	for _, test := range []struct {
		descriptor Descriptor
		want       string
		mapped     bool
	}{
		{Descriptor{Name: "Puzzle Deluxe", MainClass: "p/Main"}, "Puzzle Deluxe", false},
		{Descriptor{Name: "퍼즐 디럭스", MainClass: "p/Main"}, "퍼즐 디럭스", false},
		{Descriptor{Name: "Puzzle_ Deluxe", MainClass: "p/Main"}, "Puzzle_ Deluxe", false},
		{Descriptor{Properties: map[string]string{"DD-ProgName": "40123"}}, "40123", false},
		{Descriptor{Name: "Puzzle: Deluxe?", MainClass: "p/Main"}, "Puzzle_ Deluxe_", true},
		{Descriptor{Name: "a‮b", MainClass: "p/Main"}, "a_b", true},
		{Descriptor{Properties: map[string]string{"DD-ProgName": "../../x"}}, ".._.._x", true},
		{Descriptor{Properties: map[string]string{"DD-ProgName": ".."}}, "..", true},
		{Descriptor{Name: long, MainClass: "p/Main"}, strings.Repeat("가", 18), true},
		{Descriptor{Name: long + "나", MainClass: "p/Main"}, strings.Repeat("가", 18), true},
	} {
		owner := SaveOwner(test.descriptor)
		if storeOwnerRefused(owner) {
			t.Errorf("SaveOwner(%q) = %q, which the Host refuses", test.descriptor.Name, owner)
		}
		// A mapped owner keeps the name's start and ends in a digest of the
		// whole name, so it cannot land on another title's directory.
		mapped := strings.HasPrefix(owner, test.want+"~") && len(owner) == len(test.want)+9
		if test.mapped && !mapped || !test.mapped && owner != test.want {
			t.Errorf("SaveOwner(%q) = %q, want %q (mapped %v)", test.descriptor.Name, owner, test.want, test.mapped)
		}
		if again := SaveOwner(test.descriptor); again != owner {
			t.Errorf("SaveOwner(%q) changed from %q to %q", test.descriptor.Name, owner, again)
		}
	}
	for _, pair := range [][2]Descriptor{
		{{Name: long}, {Name: long + "나"}},
		{{Name: "A: B"}, {Name: "A? B"}},
		{{Name: "A: B"}, {Name: "A_ B"}},
	} {
		if first, second := SaveOwner(pair[0]), SaveOwner(pair[1]); first == second {
			t.Errorf("%q and %q share the owner %q", pair[0].Name, pair[1].Name, first)
		}
	}
}

// The command line joined the unsanitized owner into its save root, so a title
// whose name the Host refuses has its saves under that raw directory. They are
// moved to the owner both Hosts now use the first time the title starts.
func TestMigrateSaveOwnerMovesTheRawOwnerDirectory(t *testing.T) {
	if goruntime.GOOS == "windows" {
		t.Skip("the old directory name cannot exist on this file system")
	}
	root := t.TempDir()
	descriptor := Descriptor{Name: "Puzzle: Deluxe", MainClass: "p/Main"}
	legacy := backend.NewDirectorySaveStore(filepath.Join(root, "Puzzle: Deluxe"))
	records := backend.EncodeSaveRecords([][]byte{{1, 2, 3}})
	for key, data := range map[string][]byte{"rms/.index": []byte("save"), "rms/save": records, "fs/slot": {7}} {
		if err := legacy.StoreSave(key, data); err != nil {
			t.Fatal(err)
		}
	}
	// A quick-save slot and a set-aside generation, where the save store keeps
	// them for that owner name.
	var identity [32]byte
	identity[0] = 0x5a
	reserved := filepath.Join(root, ".wfeature-quicksave", "owners", "Puzzle: Deluxe")
	if err := os.MkdirAll(filepath.Join(reserved, "previous", "rms"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(reserved, "previous", "rms", "save"), records, 0o644); err != nil {
		t.Fatal(err)
	}
	slot := fmt.Sprintf("%x.wfq", identity)
	if err := os.WriteFile(filepath.Join(reserved, slot), []byte("quick save"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := MigrateSaveOwner(root, descriptor); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "Puzzle: Deluxe")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("old owner directory still there: %v", err)
	}
	moved := backend.NewDirectorySaveStore(filepath.Join(root, SaveOwner(descriptor)))
	if data, ok := moved.LoadSave("rms/save"); !ok || !bytes.Equal(data, records) {
		t.Fatalf("migrated record store = %v, %v", data, ok)
	}
	if data, ok := moved.LoadSave("fs/slot"); !ok || !bytes.Equal(data, []byte{7}) {
		t.Fatalf("migrated file = %v, %v", data, ok)
	}
	if found, err := moved.LegacyCheckpoint(identity); err != nil || !found {
		t.Fatalf("quick-save slot under the new owner = %v, %v", found, err)
	}
	if displaced, err := moved.DisplacedGeneration(); err != nil || !displaced {
		t.Fatalf("set-aside generation under the new owner = %v, %v", displaced, err)
	}

	// A runtime over the migrated owner opens the old store as its own.
	runtime := startRecordStoreFixture(t, moved)
	object := storeOpen(t, runtime, "save", false)
	if count := storeInvokeInt(t, storeInvoke(t, runtime, object, "getNumRecords", "()I")); count != 1 {
		t.Fatalf("migrated store has %d records, want 1", count)
	}

	// Run again, nothing moves; and an owner directory that already exists is
	// never merged into or replaced.
	if err := MigrateSaveOwner(root, descriptor); err != nil {
		t.Fatal(err)
	}
	if err := (backend.NewDirectorySaveStore(filepath.Join(root, "Puzzle: Deluxe"))).StoreSave("rms/save", []byte("old")); err != nil {
		t.Fatal(err)
	}
	if err := MigrateSaveOwner(root, descriptor); err != nil {
		t.Fatal(err)
	}
	if data, _ := moved.LoadSave("rms/save"); !bytes.Equal(data, records) {
		t.Fatal("migration replaced an owner directory that already existed")
	}
	if data, _ := os.ReadFile(filepath.Join(root, "Puzzle: Deluxe", "rms", "save")); string(data) != "old" {
		t.Fatal("migration merged into an owner directory that already existed")
	}

	// An old directory a session holds is left for the next start.
	busy := Descriptor{Name: "Busy: Title", MainClass: "b/Main"}
	if err := backend.NewDirectorySaveStore(filepath.Join(root, "Busy: Title")).StoreSave("rms/save", records); err != nil {
		t.Fatal(err)
	}
	release, err := backend.ClaimSaveDirectory(filepath.Join(root, "Busy: Title"))
	if err != nil {
		t.Fatal(err)
	}
	if err := MigrateSaveOwner(root, busy); err == nil {
		t.Fatal("migration of a claimed owner directory reported no error")
	}
	release()
	if _, err := os.Stat(filepath.Join(root, "Busy: Title", "rms", "save")); err != nil {
		t.Fatalf("claimed owner directory moved: %v", err)
	}
	if err := MigrateSaveOwner(root, busy); err != nil {
		t.Fatal(err)
	}
	if data, ok := backend.NewDirectorySaveStore(filepath.Join(root, SaveOwner(busy))).LoadSave("rms/save"); !ok || !bytes.Equal(data, records) {
		t.Fatalf("released owner directory did not migrate: %v, %v", data, ok)
	}
	// An owner that never changed has nothing to migrate, and a raw owner that
	// left the save root is never followed out of it.
	if err := MigrateSaveOwner(root, Descriptor{Name: "Puzzle Deluxe"}); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(filepath.Dir(root), filepath.Base(root)+"-outside")
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(outside) })
	if err := MigrateSaveOwner(root, Descriptor{Properties: map[string]string{"DD-ProgName": "../" + filepath.Base(outside)}}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(outside); err != nil {
		t.Fatalf("migration followed a raw owner out of the save root: %v", err)
	}
}
