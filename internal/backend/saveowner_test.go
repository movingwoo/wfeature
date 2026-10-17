package backend

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// An earlier owner that is not one directory directly below the root — the
// root itself, its parent, the reserved directory — is never followed, so a
// title that names itself "." cannot walk every other title's quick saves.
func TestMigrateSaveOwnerFollowsOnlyAnEarlierOwnerDirectory(t *testing.T) {
	root := t.TempDir()
	reserved := filepath.Join(root, saveReservedName, "owners", "other", "previous")
	if err := os.MkdirAll(reserved, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, earlier := range []string{".", "..", saveReservedName, "a/b", ""} {
		if got := EarlierSaveOwner(earlier); got != "" {
			t.Errorf("EarlierSaveOwner(%q) = %q, want none", earlier, got)
		}
		if err := MigrateSaveOwner(root, earlier, AcceptedSaveOwner(earlier)); err != nil {
			t.Errorf("MigrateSaveOwner(%q) = %v", earlier, err)
		}
	}
	if _, err := os.Stat(reserved); err != nil {
		t.Fatalf("another owner's set-aside saves moved: %v", err)
	}
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 1 {
		t.Fatalf("the root holds %v (%v), want only the reserved directory", entries, err)
	}
}

// The move claims the owner's directory as well as the earlier one, so a
// session that holds the owner's directory — and may already have made it,
// empty — is never moved over; the saves stay where they are for the next
// start.
func TestMigrateSaveOwnerLeavesAnOwnerDirectoryASessionHolds(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the earlier owner name cannot exist on this file system")
	}
	root := t.TempDir()
	const earlier = "Title: Two"
	owner := AcceptedSaveOwner(earlier)
	if err := NewDirectorySaveStore(filepath.Join(root, earlier)).StoreSave("rms/save", []byte{1}); err != nil {
		t.Fatal(err)
	}
	release, err := ClaimSaveDirectory(filepath.Join(root, owner))
	if err != nil {
		t.Fatal(err)
	}
	if got := SaveOwnerDirectory(root, earlier, owner); got != filepath.Join(root, earlier) {
		t.Fatalf("saves are read from %q before the move, want the earlier directory", got)
	}
	if err := MigrateSaveOwner(root, earlier, owner); err == nil {
		t.Fatal("a move into a claimed owner directory reported no error")
	}
	if _, err := os.Stat(filepath.Join(root, earlier, "rms", "save")); err != nil {
		t.Fatalf("the earlier saves moved: %v", err)
	}
	release()
	if err := MigrateSaveOwner(root, earlier, owner); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(filepath.Join(root, owner, "rms", "save")); err != nil || len(data) != 1 {
		t.Fatalf("moved save = %v, %v", data, err)
	}
	if got := SaveOwnerDirectory(root, earlier, owner); got != filepath.Join(root, owner) {
		t.Fatalf("saves are read from %q after the move", got)
	}
}

// An earlier build's quick load that stopped between its two renames left the
// earlier owner's saves set aside in its reserved directory and nothing under
// its name. They are put back first and then moved like any other; the slot
// beside them moves with them, and what the stopped load had staged goes.
func TestMigrateSaveOwnerSettlesAQuickLoadStoppedUnderTheEarlierOwner(t *testing.T) {
	earlier := strings.Repeat("a", SaveOwnerMaxBytes+1)
	owner := AcceptedSaveOwner(earlier)
	root := t.TempDir()
	leftoverBuild(t, root, leftoverForOwner(leftoverHalfSwapped, earlier))
	if got := SaveOwnerDirectory(root, earlier, owner); got != filepath.Join(root, earlier) {
		t.Fatalf("saves are read from %q before the move, want the earlier owner's", got)
	}
	if err := MigrateSaveOwner(root, earlier, owner); err != nil {
		t.Fatal(err)
	}
	slot := strings.Repeat("5a", 32) + ".wfq=a slot from an earlier build"
	leftoverExpect(t, root, leftoverJoin(
		leftoverAt(owner, leftoverOld...),
		leftoverAt(saveReservedName+"/owners/"+earlier, "lock=", "session="),
		leftoverAt(saveReservedName+"/owners/"+owner, "session=", slot),
	), "after the move")
	wantSaves(t, NewDirectorySaveStore(filepath.Join(root, owner)), map[string]string{
		"db/index": "old", "fs/slot/old.dat": "old data", "rms/.index": "old stores",
	})
}

// Where the owner's directory is already there, nothing is merged into it or
// replaced, and the earlier owner's stopped load is left as it is for a person
// to look at.
func TestMigrateSaveOwnerLeavesAStoppedLoadBesideAnOwnerDirectory(t *testing.T) {
	earlier := strings.Repeat("a", SaveOwnerMaxBytes+1)
	owner := AcceptedSaveOwner(earlier)
	root := t.TempDir()
	state := leftoverJoin(leftoverForOwner(leftoverHalfSwapped, earlier), []string{owner + "/rms/save=the owner's own"})
	leftoverBuild(t, root, state)
	if got := SaveOwnerDirectory(root, earlier, owner); got != filepath.Join(root, owner) {
		t.Fatalf("saves are read from %q, want the owner's", got)
	}
	if err := MigrateSaveOwner(root, earlier, owner); err != nil {
		t.Fatal(err)
	}
	leftoverExpect(t, root, leftoverJoin(state, []string{saveReservedName + "/owners/" + owner + "/session="}), "after the migration")
}

// leftoverForOwner moves a state the leftover tests describe for the owner
// "owner" to another owner's directory and reserved directory.
func leftoverForOwner(state []string, owner string) []string {
	moved := make([]string, len(state))
	for index, line := range state {
		if rest, reserved := strings.CutPrefix(line, leftoverReserved+"/"); reserved {
			moved[index] = saveReservedName + "/owners/" + owner + "/" + rest
		} else {
			moved[index] = owner + strings.TrimPrefix(line, "owner")
		}
	}
	return moved
}
