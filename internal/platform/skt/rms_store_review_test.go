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
	"time"

	"github.com/movingwoo/wfeature/internal/api/midp"
	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/jvm"
)

// A manifest's continuation lines are joined in linear time: a long logical
// line built one copy at a time cost seconds per megabyte and hours for an
// archive entry at the size limit.
func TestDescriptorContinuationLinesJoinInLinearTime(t *testing.T) {
	var manifest bytes.Buffer
	manifest.WriteString("MIDlet-1: a,b,c\nMIDlet-Description: d\n")
	const lines = 20000
	for range lines {
		manifest.WriteString(" 0123456789\n")
	}
	var before, after goruntime.MemStats
	goruntime.GC()
	goruntime.ReadMemStats(&before)
	descriptor, err := ParseDescriptor(manifest.Bytes())
	goruntime.ReadMemStats(&after)
	if err != nil {
		t.Fatal(err)
	}
	if value := descriptor.Properties["MIDlet-Description"]; len(value) != 1+lines*10 {
		t.Fatalf("joined description is %d bytes, want %d", len(value), 1+lines*10)
	}
	if allocated := after.TotalAlloc - before.TotalAlloc; allocated > 64<<20 {
		t.Fatalf("parsing a %d-byte manifest allocated %d bytes", manifest.Len(), allocated)
	}
}

// A descriptor carrying two spellings of one key answers the exact spelling,
// and the same one every time: the save owner is read from it.
func TestDescriptorPropertyPrefersTheExactKeyDeterministically(t *testing.T) {
	descriptor := Descriptor{Properties: map[string]string{"DD-PROGNAME": "5678", "DD-ProgName": "1234", "dd-progname": "9999"}}
	for range 64 {
		if value, ok := descriptor.Property("DD-ProgName"); !ok || value != "1234" {
			t.Fatalf("Property(DD-ProgName) = %q, %v, want the exact key", value, ok)
		}
		if owner := SaveOwner(descriptor); owner != "1234" {
			t.Fatalf("SaveOwner() = %q, want the exact key's program number", owner)
		}
		if value, _ := descriptor.Property("Dd-PrOgNaMe"); value != "5678" {
			t.Fatalf("Property(Dd-PrOgNaMe) = %q, want the first spelling in byte order", value)
		}
	}
}

// storeReviewOwnerRefused is the web host's owner rule, restated so the test
// does not take the platform's own copy of it on trust.
func storeReviewOwnerRefused(owner string) bool {
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
		if storeReviewOwnerRefused(owner) {
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
	object := storeReviewOpen(t, runtime, "save", false)
	if count := storeReviewInt(t, storeReviewCall(t, runtime, object, "getNumRecords", "()I")); count != 1 {
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

func storeReviewOpen(t *testing.T, runtime *Runtime, name string, create bool) *jvm.Object {
	t.Helper()
	flag := int32(0)
	if create {
		flag = 1
	}
	value, err := runtime.VM.InvokeStatic(midp.RecordStoreClass, "openRecordStore", "(Ljava/lang/String;Z)Ljavax/microedition/rms/RecordStore;",
		jvm.ReferenceValue(runtime.VM.NewString(name)), jvm.IntValue(flag))
	if err != nil {
		t.Fatalf("openRecordStore(%q, %v): %v", name, create, err)
	}
	object, err := value.Reference()
	if err != nil || object == nil {
		t.Fatalf("openRecordStore(%q) returned %v", name, err)
	}
	return object
}

func storeReviewCall(t *testing.T, runtime *Runtime, object *jvm.Object, method, descriptor string, arguments ...jvm.Value) jvm.Value {
	t.Helper()
	value, err := runtime.VM.InvokeVirtual(object, method, descriptor, arguments...)
	if err != nil {
		t.Fatalf("%s%s: %v", method, descriptor, err)
	}
	return value
}

// The index can miss a store whose records were written, when the index write
// failed and nothing rewrote it. Creating the store then takes the records
// that are there instead of writing an empty list over the player's save; a
// deleted store, which leaves an empty list, still comes back empty.
func TestCreatingAnUnlistedStoreKeepsItsRecords(t *testing.T) {
	store := backend.NewDirectorySaveStore(t.TempDir())
	records := backend.EncodeSaveRecords([][]byte{{1, 2, 3}, nil, {4}})
	if err := store.StoreSave("rms/save", records); err != nil {
		t.Fatal(err)
	}
	if err := store.StoreSave("rms/cleared", backend.EncodeSaveRecords(nil)); err != nil {
		t.Fatal(err)
	}
	runtime := startRecordStoreFixture(t, store)
	object := storeReviewOpen(t, runtime, "save", true)
	if count := storeReviewInt(t, storeReviewCall(t, runtime, object, "getNumRecords", "()I")); count != 2 {
		t.Fatalf("created store has %d records, want the 2 already saved", count)
	}
	if next := storeReviewInt(t, storeReviewCall(t, runtime, object, "getNextRecordID", "()I")); next != 4 {
		t.Fatalf("next record id = %d, want 4", next)
	}
	if data, _ := store.LoadSave("rms/save"); !bytes.Equal(data, records) {
		t.Fatal("creating the store overwrote its saved records")
	}
	if index, _ := store.LoadSave("rms/.index"); !strings.Contains(string(index), "save") {
		t.Fatalf("index = %q, want the recovered store listed", index)
	}
	cleared := storeReviewOpen(t, runtime, "cleared", true)
	if count := storeReviewInt(t, storeReviewCall(t, runtime, cleared, "getNumRecords", "()I")); count != 0 {
		t.Fatalf("a deleted store came back with %d records", count)
	}
}

// A store write takes its encoding inside the write's own turn, so an
// encoding made before a newer write cannot land after it.
func TestRecordStoreWriteCannotLandBehindANewerOne(t *testing.T) {
	store := backend.NewDirectorySaveStore(t.TempDir())
	runtime := startRecordStoreFixture(t, store)
	object := storeReviewOpen(t, runtime, "race", true)
	storeReviewCall(t, runtime, object, "addRecord", "([BII)I", jvm.ReferenceValue(jvm.NewByteArray([]byte{0})), jvm.IntValue(0), jvm.IntValue(1))
	records := object.Native.(*recordStore)

	runtime.savePendingMu.Lock()
	done := make(chan error, 1)
	go func() {
		_, err := runtime.rmsSetRecord(runtime.VM, []jvm.Value{jvm.ReferenceValue(object), jvm.IntValue(1),
			jvm.ReferenceValue(jvm.NewByteArray([]byte{1})), jvm.IntValue(0), jvm.IntValue(1)})
		done <- err
	}()
	deadline := time.Now().Add(5 * time.Second)
	for {
		records.mu.Lock()
		changed := records.records[0][0] == 1
		records.mu.Unlock()
		if changed {
			break
		}
		if time.Now().After(deadline) {
			runtime.savePendingMu.Unlock()
			t.Fatal("setRecord never changed the record")
		}
		time.Sleep(time.Millisecond)
	}
	// The first writer is now waiting for its turn. A second thread's change
	// is written in that gap.
	time.Sleep(50 * time.Millisecond)
	records.mu.Lock()
	records.records[0] = []byte{2}
	records.mu.Unlock()
	newer := backend.EncodeSaveRecords([][]byte{{2}})
	if err := store.StoreSave("rms/race", newer); err != nil {
		runtime.savePendingMu.Unlock()
		t.Fatal(err)
	}
	runtime.savePendingMu.Unlock()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if data, _ := store.LoadSave("rms/race"); !bytes.Equal(data, newer) {
		t.Fatalf("store holds %v, want the newer records %v", data, newer)
	}
}

// Ids are never reused, so a store's slot count grows with every add even
// when records are deleted again. Past the count a saved store can hold it
// would no longer load, so the add is refused as a full store instead.
func TestRecordStoreRefusesMoreSlotsThanASaveCanHold(t *testing.T) {
	runtime := startRecordStoreFixture(t, nil)
	object := storeReviewOpen(t, runtime, "log", true)
	records := object.Native.(*recordStore)
	records.mu.Lock()
	records.records = make([][]byte, backend.MaxSaveRecords)
	records.mu.Unlock()
	_, err := runtime.VM.InvokeVirtual(object, "addRecord", "([BII)I", jvm.ReferenceValue(nil), jvm.IntValue(0), jvm.IntValue(0))
	if !runtime.VM.IsGuestException(err, midp.RecordStoreFullExceptionClass) {
		t.Fatalf("addRecord past the slot limit error = %v, want RecordStoreFullException", err)
	}
	records.mu.Lock()
	defer records.mu.Unlock()
	if len(records.records) != backend.MaxSaveRecords {
		t.Fatalf("store grew to %d slots", len(records.records))
	}
}

type unreadableScriptStore struct{ writes int }

func (*unreadableScriptStore) LoadSave(string) ([]byte, bool) { return nil, false }
func (*unreadableScriptStore) ReadSave(string) ([]byte, bool, error) {
	return nil, false, errors.New("save read failed")
}
func (store *unreadableScriptStore) StoreSave(string, []byte) error {
	store.writes++
	return nil
}

// An SGS load that cannot read the save stops rather than handing the title
// zeros it would save over the real progress.
func TestScriptSaveLoadReportsAReadError(t *testing.T) {
	store := &unreadableScriptStore{}
	s := newScriptTest(t, []byte{0xff}, store)
	s.vm.Push(16)
	s.vm.Push(33)
	if err := s.Call(0x98, s.vm); err == nil || !strings.Contains(err.Error(), "save read failed") {
		t.Fatalf("load with an unreadable save error = %v, want the read error", err)
	}
	if store.writes != 0 {
		t.Fatal("a failed load wrote the save")
	}
}
