package backend

import (
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func handMadeEntry(key string, data string) []byte {
	out := binary.LittleEndian.AppendUint16(nil, uint16(len(key)))
	out = append(out, key...)
	out = binary.LittleEndian.AppendUint32(out, uint32(len(data)))
	return append(out, data...)
}

// One owner directory cannot hold a file and a directory under one name, so
// no export ever carries `a` beside `a/b`. A pack that does cannot be written
// whole: the second write fails after the first has already replaced a save.
func TestSavePackRefusesAFileThatIsAlsoADirectory(t *testing.T) {
	payload := append(handMadeEntry("fs/a", "file"), handMadeEntry("fs/a/b", "child")...)
	container := handMadeContainer(sha256.Sum256(nil), payload)
	if _, err := DecodeSavePack(container); !errors.Is(err, ErrSavePackDamaged) {
		t.Fatalf("got %v, want ErrSavePackDamaged", err)
	}
}

// The entry count is bounded the way a quick save's is. Every entry of a
// restore is a file written, flushed and renamed into place, so a pack of
// millions of tiny entries would hold the import for hours; no title's save
// tree comes near the bound.
func TestSavePackRefusesMoreEntriesThanASaveTreeHolds(t *testing.T) {
	entries := make([]SaveEntry, maxSnapshotSaveEntries+1)
	var payload []byte
	for index := range entries {
		entries[index] = SaveEntry{Key: fmt.Sprintf("k/%06d", index)}
		payload = append(payload, handMadeEntry(entries[index].Key, "")...)
	}
	if _, err := DecodeSavePack(handMadeContainer(sha256.Sum256(nil), payload)); !errors.Is(err, ErrSavePackDamaged) {
		t.Fatalf("decode got %v, want ErrSavePackDamaged", err)
	}
	if _, err := EncodeSavePack(SavePack{Entries: entries}); err == nil {
		t.Fatal("an export wrote a pack no import would accept")
	}
	if _, err := EncodeSavePack(SavePack{Entries: entries[:maxSnapshotSaveEntries]}); err != nil {
		t.Fatalf("a pack at the bound was refused: %v", err)
	}
}

// A restore that cannot be written whole is refused before it writes
// anything. The live tree can hold a file where the backup needs a directory,
// or a directory where it needs a file, and finding that out on the second
// entry used to leave the first one already replaced.
func TestWriteSaveTreeRefusesWhatItCannotApplyWhole(t *testing.T) {
	for _, testCase := range []struct {
		name    string
		live    []string
		entries []SaveEntry
	}{
		{
			name:    "a live file where a directory is needed",
			live:    []string{"fs/0", "fs/a"},
			entries: []SaveEntry{{Key: "fs/0", Data: []byte("new")}, {Key: "fs/a/b", Data: []byte("new")}},
		},
		{
			name:    "a live directory where a file is needed",
			live:    []string{"fs/0", "fs/b/c"},
			entries: []SaveEntry{{Key: "fs/0", Data: []byte("new")}, {Key: "fs/b", Data: []byte("new")}},
		},
		{
			name:    "a name macOS will not store",
			live:    []string{"fs/0"},
			entries: []SaveEntry{{Key: "fs/0", Data: []byte("new")}, {Key: "fs/\xff", Data: []byte("new")}},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			if testCase.name == "a name macOS will not store" && runtime.GOOS != "darwin" && runtime.GOOS != "ios" {
				t.Skip("only macOS refuses a name that is not UTF-8")
			}
			root := t.TempDir()
			store := NewDirectorySaveStore(root)
			for _, key := range testCase.live {
				if err := store.StoreSave(key, []byte("old")); err != nil {
					t.Fatal(err)
				}
			}
			written, removed, err := WriteSaveTree(root, testCase.entries)
			if err == nil {
				t.Fatal("a restore that cannot be applied whole succeeded")
			}
			if written != 0 || removed != 0 {
				t.Fatalf("wrote %d and removed %d before refusing", written, removed)
			}
			for _, key := range testCase.live {
				if data, ok := store.LoadSave(key); !ok || string(data) != "old" {
					t.Fatalf("%s is %q/%v after the refusal, want the save that was there", key, data, ok)
				}
			}
		})
	}
}

// On a file system that ignores Unicode normalization — macOS by default — a
// rename onto an existing entry keeps that entry's old spelling. The restored
// bytes then sit under a name the backup did not use, and the prune that
// follows must not take that name for a save the backup left out.
//
// Case is not such a spelling any more: "rms/Data" and "rms/data" are two keys
// in two files on every host (see save_names.go), so the restore of one leaves
// the other out like any entry the backup does not name, and the tree holds
// the restored save alone either way.
func TestWriteSaveTreeKeepsARestoredSaveUnderAnotherSpelling(t *testing.T) {
	for _, testCase := range []struct {
		name     string
		existing string
		key      string
	}{
		{name: "case, which is another key", existing: "rms/Data", key: "rms/data"},
		{name: "normalization", existing: "rms/한", key: "rms/한"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			root := t.TempDir()
			store := NewDirectorySaveStore(root)
			if err := store.StoreSave(testCase.existing, []byte("old")); err != nil {
				t.Fatal(err)
			}
			if _, _, err := WriteSaveTree(root, []SaveEntry{{Key: testCase.key, Data: []byte("restored")}}); err != nil {
				t.Fatalf("write: %v", err)
			}
			data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(testCase.key)))
			if err != nil || string(data) != "restored" {
				t.Fatalf("the restored save reads %q, %v", data, err)
			}
			entries, err := ReadSaveTree(root)
			if err != nil {
				t.Fatal(err)
			}
			if len(entries) != 1 || string(entries[0].Data) != "restored" {
				t.Fatalf("the tree holds %+v, want the one restored save", entries)
			}
		})
	}
}
