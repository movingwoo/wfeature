package backend

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestCheckpointSlotSurvivesStoreRestartAndOrdinaryWrites(t *testing.T) {
	root := filepath.Join(t.TempDir(), "owner")
	store := NewDirectorySaveStore(root)
	identity := SaveIdentity([]byte("authored slot archive"))
	if exists, err := store.HasCheckpoint(identity); err != nil || exists {
		t.Fatalf("new slot exists: %t, %v", exists, err)
	}
	if data, exists, err := store.LoadCheckpoint(identity); err != nil || exists || data != nil {
		t.Fatalf("missing slot = %d, %t, %v", len(data), exists, err)
	}
	data, err := EncodeCheckpoint(Checkpoint{Identity: identity, Variant: CheckpointKTFJava, Runtime: []byte("{}")})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.StoreCheckpoint(identity, data); err != nil {
		t.Fatal(err)
	}
	if err := store.StoreSave("progress", []byte("ordinary save")); err != nil {
		t.Fatal(err)
	}
	entries, err := store.SnapshotSaves()
	if err != nil || len(entries) != 1 || entries[0].Key != "progress" {
		t.Fatalf("checkpoint became a guest save: %+v, %v", entries, err)
	}
	if err := store.StoreSaves(map[string][]byte{"progress": []byte("a later save"), "second": {1}}); err != nil {
		t.Fatal(err)
	}
	fresh := NewDirectorySaveStore(root)
	got, exists, err := fresh.LoadCheckpoint(identity)
	if err != nil || !exists || !bytes.Equal(got, data) {
		t.Fatalf("slot did not survive restart and later saves: %t, %v", exists, err)
	}
	bad := bytes.Clone(data)
	bad[len(bad)-1]++
	if err := fresh.StoreCheckpoint(identity, bad); err == nil {
		t.Fatal("damaged checkpoint overwrote slot")
	}
	got, _, _ = fresh.LoadCheckpoint(identity)
	if !bytes.Equal(got, data) {
		t.Fatal("failed write changed the previous checkpoint")
	}
	if exists, err := fresh.HasCheckpoint(SaveIdentity([]byte("different archive"))); err != nil || exists {
		t.Fatalf("another archive inherited this slot: %t, %v", exists, err)
	}
	loose, err := ReadSaveTree(root)
	if err != nil || len(loose) != 2 || loose[0].Key != "progress" || loose[1].Key != "second" {
		t.Fatalf("loose export included the reserved checkpoint: %+v, %v", loose, err)
	}
	paths, err := replacementPaths(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(paths.directory, fmt.Sprintf("%x.v2.wfq", identity))); err != nil {
		t.Fatalf("the slot is not under its version 2 name: %v", err)
	}
}

// A slot an earlier build wrote is its owner's: this build reports it, refuses
// to load it and never writes under its name. A new quick save lands beside it.
func TestCheckpointSlotNeverTouchesAnEarlierBuildsSlot(t *testing.T) {
	root := filepath.Join(t.TempDir(), "owner")
	store := NewDirectorySaveStore(root)
	identity := SaveIdentity([]byte("authored slot archive"))
	paths, err := replacementPaths(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(paths.directory, 0700); err != nil {
		t.Fatal(err)
	}
	earlierPath := filepath.Join(paths.directory, fmt.Sprintf("%x.wfq", identity))
	earlier := []byte("an earlier build's quick save, which nothing here decodes")
	if err := os.WriteFile(earlierPath, earlier, 0600); err != nil {
		t.Fatal(err)
	}
	untouched := func(after string) {
		t.Helper()
		if data, err := os.ReadFile(earlierPath); err != nil || !bytes.Equal(data, earlier) {
			t.Fatalf("%s changed the earlier slot: %q, %v", after, data, err)
		}
	}

	if exists, err := store.HasCheckpoint(identity); err != nil || !exists {
		t.Fatalf("an earlier slot is not offered, so its refusal could never be shown: %t, %v", exists, err)
	}
	if legacy, err := store.LegacyCheckpoint(identity); err != nil || !legacy {
		t.Fatalf("the earlier slot is not reported: %t, %v", legacy, err)
	}
	data, found, err := store.LoadCheckpoint(identity)
	if !found || data != nil || !errors.Is(err, ErrCheckpointLegacy) || errors.Is(err, ErrCheckpointVersion) {
		t.Fatalf("loading an earlier slot = %d bytes, found=%t, %v; want found with the earlier-format refusal", len(data), found, err)
	}
	untouched("a refused load")

	current, err := EncodeCheckpoint(Checkpoint{Identity: identity, Variant: CheckpointKTFJava, Runtime: []byte("{}")})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.StoreCheckpoint(identity, current); err != nil {
		t.Fatalf("a new quick save beside an earlier slot: %v", err)
	}
	untouched("a new quick save")
	got, found, err := store.LoadCheckpoint(identity)
	if err != nil || !found || !bytes.Equal(got, current) {
		t.Fatalf("the new slot did not load beside the earlier one: found=%t, %v", found, err)
	}
	if legacy, err := store.LegacyCheckpoint(identity); err != nil || !legacy {
		t.Fatalf("the earlier slot is no longer reported after a new quick save: %t, %v", legacy, err)
	}
	if err := store.StoreSave("progress", []byte("ordinary save")); err != nil {
		t.Fatal(err)
	}
	untouched("an ordinary save")

	// Another archive has no slot of either kind, and something that is not a
	// regular file under the earlier name is not a slot.
	other := SaveIdentity([]byte("another archive"))
	if exists, err := store.HasCheckpoint(other); err != nil || exists {
		t.Fatalf("another archive inherited a slot: %t, %v", exists, err)
	}
	if err := os.Mkdir(filepath.Join(paths.directory, fmt.Sprintf("%x.wfq", other)), 0700); err != nil {
		t.Fatal(err)
	}
	if exists, err := store.HasCheckpoint(other); err != nil || exists {
		t.Fatalf("a directory under the earlier name was offered as a slot: %t, %v", exists, err)
	}
	if _, found, err := store.LoadCheckpoint(other); err != nil || found {
		t.Fatalf("a directory under the earlier name was loaded: found=%t, %v", found, err)
	}
}

func TestCheckpointSlotRefusesLinksDirectoriesAndOversize(t *testing.T) {
	for _, kind := range []string{"symlink", "directory", "large"} {
		t.Run(kind, func(t *testing.T) {
			root := filepath.Join(t.TempDir(), "owner")
			store := NewDirectorySaveStore(root)
			identity := SaveIdentity([]byte("authored slot archive"))
			paths, err := replacementPaths(root)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.MkdirAll(paths.directory, 0700); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(paths.directory, checkpointSlotName(identity))
			switch kind {
			case "symlink":
				if err := os.Symlink(filepath.Join(t.TempDir(), "outside"), path); err != nil {
					t.Skipf("symlink unavailable: %v", err)
				}
			case "directory":
				if err := os.Mkdir(path, 0700); err != nil {
					t.Fatal(err)
				}
			case "large":
				file, err := os.Create(path)
				if err != nil {
					t.Fatal(err)
				}
				err = file.Truncate(CheckpointLimit + 1)
				file.Close()
				if err != nil {
					t.Fatal(err)
				}
			}
			if _, _, err := store.LoadCheckpoint(identity); err == nil {
				t.Fatal("unsafe slot was read")
			}
			if _, err := store.HasCheckpoint(identity); err == nil {
				t.Fatal("unsafe slot was offered as available")
			}
		})
	}
}
