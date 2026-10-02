package backend

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestCheckpointSlotSurvivesStoreRestartAndSaveReplacement(t *testing.T) {
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
	if err := store.ReplaceSaves([]SaveEntry{{Key: "replaced", Data: []byte{1}}}); err != nil {
		t.Fatal(err)
	}
	fresh := NewDirectorySaveStore(root)
	got, exists, err := fresh.LoadCheckpoint(identity)
	if err != nil || !exists || !bytes.Equal(got, data) {
		t.Fatalf("slot did not survive restart and replacement: %t, %v", exists, err)
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
	if err != nil || len(loose) != 1 || loose[0].Key != "replaced" {
		t.Fatalf("loose export included the reserved checkpoint: %+v, %v", loose, err)
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
			path := filepath.Join(paths.directory, fmt.Sprintf("%x.wfq", identity))
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
