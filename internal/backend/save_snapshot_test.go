package backend

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestMemorySaveSnapshotOwnsBytesAndReplacesWholeGeneration(t *testing.T) {
	input := []SaveEntry{{Key: "db/old", Data: []byte("before")}, {Key: "fs/empty"}}
	store, err := NewMemorySaveStore(input)
	if err != nil {
		t.Fatal(err)
	}
	input[0].Data[0] = 'X'
	data, present, err := store.ReadSave("db/old")
	if err != nil || !present || string(data) != "before" {
		t.Fatalf("isolated read: %q, %t, %v", data, present, err)
	}
	data[0] = 'X'
	saved, err := store.SnapshotSaves()
	if err != nil || string(saved[0].Data) != "before" {
		t.Fatalf("snapshot shares read bytes: %v", err)
	}
	saved[0].Data[0] = 'X'
	if data, _, _ := store.ReadSave("db/old"); string(data) != "before" {
		t.Fatal("snapshot shares source bytes")
	}
	if err := store.ReplaceSaves([]SaveEntry{{Key: "fs/new", Data: []byte("after")}}); err != nil {
		t.Fatal(err)
	}
	if _, present, _ := store.ReadSave("db/old"); present {
		t.Fatal("replacement kept a later generation's extra key")
	}
	if err := store.StoreSaves(map[string][]byte{"./db/a": []byte("one"), "db/b": {}}); err != nil {
		t.Fatal(err)
	}
	if data, present, err := store.ReadSave("db/b"); err != nil || !present || len(data) != 0 {
		t.Fatalf("empty save differs from missing: %t, %v", present, err)
	}
	if _, present, err := store.ReadSave("db"); err != nil || present {
		t.Fatal("a directory read differs from a directory store")
	}
	if _, present, err := store.ReadSave("db/a/child"); err != nil || present {
		t.Fatal("read below a file differs from a directory store")
	}
}

func TestSaveSnapshotRejectsConflictsAndOversizeBeforeMutation(t *testing.T) {
	store, err := NewMemorySaveStore([]SaveEntry{{Key: "kept", Data: []byte("original")}})
	if err != nil {
		t.Fatal(err)
	}
	for _, entries := range [][]SaveEntry{
		{{Key: "../escape"}}, {{Key: "./noncanonical"}}, {{Key: "duplicate"}, {Key: "duplicate"}},
		{{Key: "a"}, {Key: "a-b"}, {Key: "a/child"}},
		make([]SaveEntry, maxSnapshotSaveEntries+1),
		{{Key: "oversize", Data: make([]byte, savePackLimit)}},
	} {
		if err := store.ReplaceSaves(entries); err == nil {
			t.Fatal("accepted invalid snapshot keys or size")
		}
		got, _ := store.SnapshotSaves()
		if len(got) != 1 || got[0].Key != "kept" || string(got[0].Data) != "original" {
			t.Fatal("failed replacement mutated the source")
		}
	}
	if err := store.StoreSaves(map[string][]byte{"x": {1}, "./x": {2}}); err == nil {
		t.Fatal("accepted duplicate canonical batch keys")
	}
	if err := store.StoreSaves(map[string][]byte{"kept/child": {1}, "unrelated": {2}}); err == nil {
		t.Fatal("accepted batch write below a file")
	}
	if _, present, _ := store.ReadSave("unrelated"); present {
		t.Fatal("failed batch partly applied")
	}
}

func TestDirectorySaveSnapshotBoundsAndIncludesEmptyAndDottedKeys(t *testing.T) {
	root := filepath.Join(t.TempDir(), "owner")
	store := NewDirectorySaveStore(root)
	if entries, err := store.SnapshotSaves(); err != nil || len(entries) != 0 {
		t.Fatalf("first-run snapshot = %v, %v", entries, err)
	}
	want := []SaveEntry{{Key: "db/name", Data: []byte("saved")}, {Key: "fs/empty"}, {Key: "rms/.index", Data: []byte("index")}}
	for _, entry := range want {
		if err := store.StoreSave(entry.Key, entry.Data); err != nil {
			t.Fatal(err)
		}
	}
	got, err := store.SnapshotSaves()
	if err != nil || len(got) != len(want) {
		t.Fatalf("directory snapshot = %v, %v", got, err)
	}
	for index := range got {
		if got[index].Key != want[index].Key || !bytes.Equal(got[index].Data, want[index].Data) {
			t.Fatal("directory snapshot lost a file")
		}
	}
	file, err := os.Create(filepath.Join(root, "oversize"))
	if err != nil {
		t.Fatal(err)
	}
	err = file.Truncate(savePackLimit + 1)
	_ = file.Close()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.SnapshotSaves(); err == nil {
		t.Fatal("snapshot allocated an oversized sparse file")
	}
}

func TestDirectorySaveSnapshotRejectsSymlinks(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside")
	if err := os.WriteFile(outside, []byte("outside bytes"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "link")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if _, err := NewDirectorySaveStore(root).SnapshotSaves(); err == nil {
		t.Fatal("snapshot followed a linked save")
	}
}
