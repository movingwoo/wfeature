package backend

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

type countingReadStore struct {
	*MemorySaveStore
	reads int
}

func (store *countingReadStore) ReadSave(name string) ([]byte, bool, error) {
	store.reads++
	return store.MemorySaveStore.ReadSave(name)
}

// A rebuild reads each key once and on one budget, and writes nothing.
func TestRebuildReaderReadsEachKeyOnceOnABudget(t *testing.T) {
	memory, err := NewMemorySaveStore([]SaveEntry{
		{Key: "fs/small", Data: []byte("0123456789")},
		{Key: "fs/large", Data: []byte(strings.Repeat("x", 40))},
	})
	if err != nil {
		t.Fatal(err)
	}
	base := &countingReadStore{MemorySaveStore: memory}
	reader := NewRebuildReader(base, 30)
	for range 5 {
		// Two spellings of one key are one key.
		for _, name := range []string{"fs/small", "fs/./small"} {
			if data, present, err := reader.ReadSave(name); err != nil || !present || string(data) != "0123456789" {
				t.Fatalf("read of %q = %q, %t, %v", name, data, present, err)
			}
		}
		if data, present := reader.LoadSave("fs/absent"); present || data != nil {
			t.Fatalf("an absent key reads %q", data)
		}
	}
	if base.reads != 2 || reader.Err() != nil {
		t.Fatalf("ten reads of one key and five of another reached the store %d times: %v", base.reads, reader.Err())
	}
	// Twenty bytes are left, and the large entry is forty.
	if _, _, err := reader.ReadSave("fs/large"); err == nil || reader.Err() == nil {
		t.Fatal("an entry over what was left of the budget was read")
	}
	if data, _ := memory.LoadSave("fs/small"); string(data) != "0123456789" {
		t.Fatal("reading changed the store")
	}
}

func TestRebuildReaderRefusesAndRemembersAWrite(t *testing.T) {
	memory, err := NewMemorySaveStore(nil)
	if err != nil {
		t.Fatal(err)
	}
	for name, write := range map[string]func(*RebuildReader) error{
		"a write": func(reader *RebuildReader) error { return reader.StoreSave("fs/save", []byte("x")) },
		"a batch": func(reader *RebuildReader) error {
			return StoreSaves(reader, map[string][]byte{"fs/b": []byte("x"), "fs/a": []byte("y")})
		},
	} {
		reader := NewRebuildReader(memory, 1<<20)
		if err := write(reader); err == nil || reader.Err() == nil {
			t.Fatalf("%s through a rebuild reader = %v, remembered as %v", name, err, reader.Err())
		}
		if entries, _ := memory.SnapshotSaves(); len(entries) != 0 {
			t.Fatalf("%s reached the store: %+v", name, entries)
		}
	}
}

// What the base store refuses is the reader's answer, and stays its answer.
func TestRebuildReaderKeepsTheFirstFailure(t *testing.T) {
	root := filepath.Join(t.TempDir(), "owner")
	store := NewDirectorySaveStore(root)
	if err := store.StoreSave("fs/save/below", []byte("x")); err != nil {
		t.Fatal(err)
	}
	reader := NewRebuildReader(store, 1<<20)
	// A directory at a key is a failed read, not an absent entry.
	_, _, first := reader.ReadSave("fs/save")
	if first == nil {
		t.Fatal("a directory at a key read as an entry or as absent")
	}
	if _, _, err := reader.ReadSave("../outside"); err == nil {
		t.Fatal("a key outside the store was read")
	}
	if !errors.Is(reader.Err(), first) && reader.Err().Error() != first.Error() {
		t.Fatalf("the reader remembers %v, want its first failure %v", reader.Err(), first)
	}
	// With no store behind it every key is absent.
	if data, present, err := NewRebuildReader(nil, 8).ReadSave("fs/save"); err != nil || present || data != nil {
		t.Fatalf("a read with no store = %q, %t, %v", data, present, err)
	}
}
