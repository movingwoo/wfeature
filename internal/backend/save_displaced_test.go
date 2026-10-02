package backend

import (
	"os"
	"path/filepath"
	"testing"
)

// The states are the literal leftovers of save_recovery_leftovers_test.go.
func TestDisplacedGenerationReportsPreviousAndLeavesItAlone(t *testing.T) {
	t.Run("a title no earlier build loaded", func(t *testing.T) {
		parent := t.TempDir()
		root := filepath.Join(parent, "owner")
		store := NewDirectorySaveStore(root)
		if displaced, err := store.DisplacedGeneration(); err != nil || displaced {
			t.Fatalf("before the first save = %t, %v", displaced, err)
		}
		// Asking takes the tree lock like any store operation, which makes the
		// reserved directory and its lock file. It makes nothing else: no live
		// tree and no "previous" to find next time.
		leftoverExpect(t, parent, []string{leftoverReserved + "/lock="}, "after asking")
		if err := store.StoreSave("db/index", []byte("progress")); err != nil {
			t.Fatal(err)
		}
		if displaced, err := store.DisplacedGeneration(); err != nil || displaced {
			t.Fatalf("with ordinary saves = %t, %v", displaced, err)
		}
		leftoverExpect(t, parent, []string{"owner/db/index=progress", leftoverReserved + "/lock="}, "after asking again")
	})

	t.Run("a finished load", func(t *testing.T) {
		parent := t.TempDir()
		leftoverBuild(t, parent, leftoverFinished)
		for round := 0; round < 2; round++ {
			// A fresh store each time: the answer is on the disk, not in the
			// object that asked before.
			displaced, err := NewDirectorySaveStore(filepath.Join(parent, "owner")).DisplacedGeneration()
			if err != nil || !displaced {
				t.Fatalf("round %d = %t, %v", round, displaced, err)
			}
			leftoverExpect(t, parent, leftoverFinished, "after asking")
		}
	})

	// The directory is asked whether it exists and nothing more. Here it
	// cannot be listed at all, and the answer is the same.
	t.Run("a previous directory that cannot be opened", func(t *testing.T) {
		parent := t.TempDir()
		leftoverBuild(t, parent, leftoverFinished)
		previous := filepath.Join(parent, filepath.FromSlash(leftoverPrevious))
		t.Cleanup(func() { _ = os.Chmod(previous, 0o755) })
		if err := os.Chmod(previous, 0); err != nil {
			t.Fatal(err)
		}
		if _, err := os.ReadDir(previous); err == nil {
			t.Skip("this user can list a directory without permission, so the case shows nothing here")
		}
		displaced, err := NewDirectorySaveStore(filepath.Join(parent, "owner")).DisplacedGeneration()
		if err != nil || !displaced {
			t.Fatalf("an unreadable previous = %t, %v", displaced, err)
		}
		if err := os.Chmod(previous, 0o755); err != nil {
			t.Fatal(err)
		}
		leftoverExpect(t, parent, leftoverFinished, "after asking")
	})

	// An earlier build displaced whatever was live, and that can be a
	// directory with nothing in it. It is still there to be reported.
	t.Run("an empty previous directory", func(t *testing.T) {
		parent := t.TempDir()
		state := leftoverJoin(leftoverAt("owner", leftoverNew...), leftoverKept, []string{leftoverPrevious + "/"})
		leftoverBuild(t, parent, state)
		displaced, err := NewDirectorySaveStore(filepath.Join(parent, "owner")).DisplacedGeneration()
		if err != nil || !displaced {
			t.Fatalf("an empty previous = %t, %v", displaced, err)
		}
		leftoverExpect(t, parent, state, "after asking")
	})

	// Asked of a load that stopped between its renames, "previous" is the
	// title's live tree. Recovery runs first and puts it back, so the answer
	// is about what is left afterwards: nothing displaced.
	t.Run("a load that stopped between its renames", func(t *testing.T) {
		parent := t.TempDir()
		leftoverBuild(t, parent, leftoverHalfSwapped)
		displaced, err := NewDirectorySaveStore(filepath.Join(parent, "owner")).DisplacedGeneration()
		if err != nil || displaced {
			t.Fatalf("a half-finished load = %t, %v", displaced, err)
		}
		leftoverExpect(t, parent, leftoverJoin(leftoverAt("owner", leftoverOld...), leftoverKept), "after asking")
	})

	// A load that stopped after its second rename is complete, and the tree it
	// displaced stays displaced once its record is cleared.
	t.Run("a load that stopped before removing its record", func(t *testing.T) {
		parent := t.TempDir()
		leftoverBuild(t, parent, leftoverSwapped)
		displaced, err := NewDirectorySaveStore(filepath.Join(parent, "owner")).DisplacedGeneration()
		if err != nil || !displaced {
			t.Fatalf("a load with its record left over = %t, %v", displaced, err)
		}
		leftoverExpect(t, parent, leftoverFinished, "after asking")
	})

	// Something named "previous" that is not a directory is not a generation
	// anyone displaced, and saying "no" would hide it.
	t.Run("a file named previous", func(t *testing.T) {
		parent := t.TempDir()
		state := leftoverJoin(leftoverAt("owner", leftoverNew...), leftoverKept, leftoverAt(leftoverReserved, "previous=not a directory"))
		leftoverBuild(t, parent, state)
		if displaced, err := NewDirectorySaveStore(filepath.Join(parent, "owner")).DisplacedGeneration(); err == nil || displaced {
			t.Fatalf("a file named previous = %t, %v", displaced, err)
		}
		leftoverExpect(t, parent, state, "after asking")
	})
	t.Run("a link named previous", func(t *testing.T) {
		parent := t.TempDir()
		state := leftoverJoin(leftoverAt("owner", leftoverNew...), leftoverKept)
		leftoverBuild(t, parent, state)
		elsewhere := t.TempDir()
		if err := os.Symlink(elsewhere, filepath.Join(parent, filepath.FromSlash(leftoverPrevious))); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
		if displaced, err := NewDirectorySaveStore(filepath.Join(parent, "owner")).DisplacedGeneration(); err == nil || displaced {
			t.Fatalf("a link named previous = %t, %v", displaced, err)
		}
		if entries, err := os.ReadDir(elsewhere); err != nil || len(entries) != 0 {
			t.Fatalf("the link's target was written to: %v, %v", entries, err)
		}
	})

	t.Run("a store with no root", func(t *testing.T) {
		var missing *DirectorySaveStore
		if displaced, err := missing.DisplacedGeneration(); err == nil || displaced {
			t.Fatalf("a nil store = %t, %v", displaced, err)
		}
		if displaced, err := NewDirectorySaveStore("").DisplacedGeneration(); err == nil || displaced {
			t.Fatalf("a store with an empty root = %t, %v", displaced, err)
		}
	})
}
