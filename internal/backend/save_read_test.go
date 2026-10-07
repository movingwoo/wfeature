package backend

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// unreadableSave takes every permission off a saved file so that reading it
// fails, and reports whether it does. A system that ignores the bits, or an
// administrator who is not held to them, cannot make the case.
func unreadableSave(t *testing.T, path string) bool {
	t.Helper()
	if runtime.GOOS == "windows" {
		return false
	}
	if err := os.Chmod(path, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(path, 0o600) })
	if file, err := os.Open(path); err == nil {
		file.Close()
		return false
	}
	return true
}

func TestSaveReadDistinguishesMissingUnreadableAndEmpty(t *testing.T) {
	root := t.TempDir()
	store := NewDirectorySaveStore(root)
	if _, exists, err := ReadSave(store, "missing"); exists || err != nil {
		t.Fatalf("missing = %t, %v", exists, err)
	}
	if err := store.StoreSave("unreadable", []byte("save")); err != nil {
		t.Fatal(err)
	}
	if unreadableSave(t, filepath.Join(root, "unreadable")) {
		if _, _, err := ReadSave(store, "unreadable"); err == nil {
			t.Fatal("read error reported as absence")
		}
	}
	if err := store.StoreSave("empty", nil); err != nil {
		t.Fatal(err)
	}
	if data, exists, err := ReadSave(store, "empty"); len(data) != 0 || !exists || err != nil {
		t.Fatalf("empty = %v, %t, %v", data, exists, err)
	}
	if _, _, err := ReadSave(store, "../outside"); err == nil {
		t.Fatal("invalid path reported as absence")
	}
}

func TestSaveRecordsRejectTrailingData(t *testing.T) {
	if _, err := DecodeSaveRecords(append(EncodeSaveRecords(nil), 1)); err == nil {
		t.Fatal("trailing corruption accepted")
	}
}

func TestSaveReadBelowFileReportsMissing(t *testing.T) {
	store := NewDirectorySaveStore(t.TempDir())
	if err := store.StoreSave("fs/state", []byte("progress")); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"fs/state/asset", "fs/state/scene/asset"} {
		if _, exists, err := ReadSave(store, name); exists || err != nil {
			t.Fatalf("read %s = %t, %v; want missing", name, exists, err)
		}
	}
	if err := store.StoreSave("fs/state/asset", []byte("invalid")); err == nil {
		t.Fatal("write below a file succeeded")
	}
	if data, exists, err := ReadSave(store, "fs/state"); string(data) != "progress" || !exists || err != nil {
		t.Fatalf("parent file = %q, %t, %v", data, exists, err)
	}
}

// A key whose path is a directory has no entry of its own: keys below it made
// the directory and nothing could be stored under this one. Every read
// answers that, on both stores and both read paths, before the keys below it
// were written and after, and a write to it still fails without touching what
// is below it.
func TestSaveReadOfADirectoryReportsMissing(t *testing.T) {
	directory := NewDirectorySaveStore(filepath.Join(t.TempDir(), "owner"))
	memory, err := NewMemorySaveStore(nil)
	if err != nil {
		t.Fatal(err)
	}
	for name, store := range map[string]interface {
		SaveStore
		SaveReader
	}{"directory store": directory, "memory store": memory} {
		t.Run(name, func(t *testing.T) {
			absent := func(when string) {
				t.Helper()
				for _, key := range []string{"fs/saves", "fs"} {
					if data, found, err := ReadSave(store, key); data != nil || found || err != nil {
						t.Fatalf("%s: read %s = %q, %t, %v; want missing", when, key, data, found, err)
					}
					if data, found := store.LoadSave(key); data != nil || found {
						t.Fatalf("%s: load %s = %q, %t; want missing", when, key, data, found)
					}
					// A directory has a size of its own, which a spent budget
					// must not read as a save that is too large.
					for _, limit := range []int64{0, 1 << 20} {
						if data, found, err := ReadSaveLimit(store, key, limit); data != nil || found || err != nil {
							t.Fatalf("%s: bounded read %s with limit %d = %q, %t, %v; want missing", when, key, limit, data, found, err)
						}
					}
				}
			}
			absent("before a save")
			if err := store.StoreSave("fs/saves/slot", []byte("progress")); err != nil {
				t.Fatal(err)
			}
			absent("after a save below it")
			if err := store.StoreSave("fs/saves", []byte("over")); err == nil {
				t.Fatal("a write to a key that is a directory succeeded")
			}
			if data, found, err := ReadSave(store, "fs/saves/slot"); string(data) != "progress" || !found || err != nil {
				t.Fatalf("the save below = %q, %t, %v", data, found, err)
			}
			absent("after a refused write")
		})
	}
}

// An empty record and a deleted one are different records, and the encoding
// has always kept them apart: a length of zero and the tombstone. Decoding
// used to turn the first into nil, which every reader takes for the second.
// The bytes are pinned as well, because saves written by earlier releases are
// read with this decoder and nothing converts them.
func TestSaveRecordsKeepAnEmptyRecordApartFromADeletedOne(t *testing.T) {
	records := [][]byte{{}, nil, []byte("x"), {}}
	encoded := EncodeSaveRecords(records)
	want := []byte{4, 0, 0, 0, 0, 0, 0, 0, 0xff, 0xff, 0xff, 0xff, 1, 0, 0, 0, 'x', 0, 0, 0, 0}
	if !bytes.Equal(encoded, want) {
		t.Fatalf("encoded = %v, want %v", encoded, want)
	}
	decoded, err := DecodeSaveRecords(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if len(decoded) != len(records) {
		t.Fatalf("decoded %d records, want %d", len(decoded), len(records))
	}
	for index, record := range records {
		if (decoded[index] == nil) != (record == nil) || !bytes.Equal(decoded[index], record) {
			t.Fatalf("record %d = %v (nil %t), want %v (nil %t)", index, decoded[index], decoded[index] == nil, record, record == nil)
		}
	}
}
