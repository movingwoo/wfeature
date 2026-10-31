package ktf

import (
	"bytes"
	"context"
	"github.com/movingwoo/wfeature/internal/platform/wipicrt"
	"path/filepath"
	"testing"
)

func TestSaveRecordsRoundTrip(t *testing.T) {
	records := [][]byte{
		[]byte("first"),
		nil,
		{},
		[]byte{0x00, 0xff, 0x7f},
	}
	decoded, err := wipicrt.DecodeSaveRecords(wipicrt.EncodeSaveRecords(records))
	if err != nil {
		t.Fatal(err)
	}
	if len(decoded) != len(records) {
		t.Fatalf("decoded %d records, want %d", len(decoded), len(records))
	}
	for index, record := range records {
		if record == nil {
			if decoded[index] != nil {
				t.Fatalf("record %d should stay a tombstone", index)
			}
			continue
		}
		if !bytes.Equal(decoded[index], record) {
			t.Fatalf("record %d = %v, want %v", index, decoded[index], record)
		}
	}
}

func TestDecodeSaveRecordsRejectsTruncation(t *testing.T) {
	encoded := wipicrt.EncodeSaveRecords([][]byte{[]byte("payload")})
	for _, size := range []int{0, 3, 5, len(encoded) - 1} {
		if _, err := wipicrt.DecodeSaveRecords(encoded[:size]); err == nil {
			t.Fatalf("truncation to %d bytes was accepted", size)
		}
	}
}

func TestDirectorySaveStoreRoundTrip(t *testing.T) {
	store := wipicrt.NewDirectorySaveStore(filepath.Join(t.TempDir(), "aid"))
	if _, ok := store.LoadSave("db/game.dat"); ok {
		t.Fatal("missing entry reported present")
	}
	if err := store.StoreSave("db/game.dat", []byte("save")); err != nil {
		t.Fatal(err)
	}
	data, ok := store.LoadSave("db/game.dat")
	if !ok || string(data) != "save" {
		t.Fatalf("loaded %q ok=%t, want save", data, ok)
	}
}

func TestDirectorySaveStoreRejectsTraversal(t *testing.T) {
	store := wipicrt.NewDirectorySaveStore(t.TempDir())
	for _, key := range []string{"", "..", "db/../../etc", ".", "a\\b", "nul\x00"} {
		if err := store.StoreSave(key, []byte("x")); err == nil {
			t.Fatalf("key %q was accepted", key)
		}
		if _, ok := store.LoadSave(key); ok {
			t.Fatalf("key %q loaded", key)
		}
	}
}

// TestDirectorySaveStoreNormalizesCurrentDirectory covers the guest names that
// arrive with filesystem-style prefixes: a guest opens "./OptionSave", which
// has to land in the same file as the bare name.
func TestDirectorySaveStoreNormalizesCurrentDirectory(t *testing.T) {
	store := wipicrt.NewDirectorySaveStore(t.TempDir())
	if err := store.StoreSave("db/./OptionSave", []byte("options")); err != nil {
		t.Fatal(err)
	}
	data, ok := store.LoadSave("db/OptionSave")
	if !ok || string(data) != "options" {
		t.Fatalf("loaded %q ok=%t, want options", data, ok)
	}
	if err := store.StoreSave("db//slot.dat", []byte("slot")); err != nil {
		t.Fatal(err)
	}
	if data, ok := store.LoadSave("db/slot.dat"); !ok || string(data) != "slot" {
		t.Fatalf("loaded %q ok=%t, want slot", data, ok)
	}
}

// TestClientSaveStoreSeedsCDatabase drives the WIPI C database open path
// directly against a Host store, covering the persisted-save precedence.
func TestClientSaveStoreSeedsCDatabase(t *testing.T) {
	root := t.TempDir()
	store := wipicrt.NewDirectorySaveStore(root)
	if err := store.StoreSave("db/slot.dat", []byte{1, 2, 3}); err != nil {
		t.Fatal(err)
	}
	runtime := detachedRuntime(&wipicrt.Client{SaveStore: store})
	data, ok := runtime.LoadSave("db/slot.dat")
	if !ok || !bytes.Equal(data, []byte{1, 2, 3}) {
		t.Fatalf("loadSave = %v ok=%t", data, ok)
	}
	cdb := &wipicrt.RuntimeCFile{Name: "slot.dat", Data: []byte{9}}
	cdb.Persist(runtime.Runtime)
	persisted, ok := store.LoadSave("db/slot.dat")
	if !ok || !bytes.Equal(persisted, []byte{9}) {
		t.Fatalf("persisted = %v ok=%t, want [9]", persisted, ok)
	}
}

func TestStartSessionAsyncReportsFailureThroughPump(t *testing.T) {
	pending := StartSessionAsync(context.Background(), []byte("not a zip"), SessionOptions{})
	var finished bool
	var err error
	for round := 0; round < 1000 && !finished; round++ {
		finished, err = pending.Pump()
	}
	if !finished {
		t.Fatal("pending session never finished")
	}
	if err == nil {
		t.Fatal("expected an open error from the pending session")
	}
	if pending.Session() != nil {
		t.Fatal("failed start must not expose a session")
	}
}

// A deleted file is gone, including one whose bytes are in the save store
// under it or in the mounted archive under that. Dropping only the in-memory
// entry leaves it resolving from the layer below, and a title that deletes a
// save and then asks whether a save is there is told yes — which is how the
// sibling platform lost two titles' entire opening sequences.
func TestUnlinkedGuestFilesStopExisting(t *testing.T) {
	store := wipicrt.NewDirectorySaveStore(t.TempDir())
	if err := store.StoreSave("fs/save/slot.dat", []byte("persisted")); err != nil {
		t.Fatal(err)
	}
	runtime := detachedRuntime(&wipicrt.Client{
		SaveStore: store,
		Files:     map[string][]byte{"packaged.dat": []byte("packaged")},
	})
	runtime.GuestFiles = map[string][]byte{"save/slot.dat": []byte("in memory")}

	for _, name := range []string{"save/slot.dat", "packaged.dat"} {
		if _, exists := runtime.GuestFile(name); !exists {
			t.Fatalf("%s is missing before it is deleted", name)
		}
		runtime.markGuestFileRemoved(name, true)
		delete(runtime.GuestFiles, name)
		if _, exists := runtime.GuestFile(name); exists {
			t.Fatalf("%s still resolves after being deleted", name)
		}
		// A leading slash names the same file, which is what the resolution
		// under this already assumes.
		if _, exists := runtime.GuestFile("/" + name); exists {
			t.Fatalf("/%s still resolves after %s was deleted", name, name)
		}
	}

	// Writing the path brings it back, and reads answer what was written
	// rather than what was underneath.
	runtime.GuestFiles["save/slot.dat"] = []byte("fresh")
	runtime.storeGuestFile("save/slot.dat", []byte("fresh"))
	data, exists := runtime.GuestFile("save/slot.dat")
	if !exists || string(data) != "fresh" {
		t.Fatalf("the rewritten file = %q/%t, want fresh", data, exists)
	}
}

// The removal outlives the session, because a title that deletes its save,
// exits and comes back must not find the save again.
func TestUnlinkSurvivesAReload(t *testing.T) {
	store := wipicrt.NewDirectorySaveStore(t.TempDir())
	if err := store.StoreSave("fs/save/slot.dat", []byte("persisted")); err != nil {
		t.Fatal(err)
	}
	first := detachedRuntime(&wipicrt.Client{SaveStore: store})
	first.markGuestFileRemoved("save/slot.dat", true)

	second := detachedRuntime(&wipicrt.Client{SaveStore: store})
	if _, exists := second.GuestFile("save/slot.dat"); exists {
		t.Fatal("a file deleted in an earlier session is back")
	}
}
