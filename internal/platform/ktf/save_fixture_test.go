package ktf

import (
	"bytes"
	"encoding/binary"
	"strings"
	"testing"

	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/testfixture"
)

// saveFixtureVariant is one of the two authored storage archives with the
// guest words a test reads and writes in it. Both run the same routines; they
// differ in how they are loaded, which is what makes them two checkpoint
// variants.
type saveFixtureVariant struct {
	name    string
	archive func() ([]byte, error)
	variant uint16

	progress, seenA, seenB, status, length, actions, kept uint32
}

func saveFixtureVariants() []saveFixtureVariant {
	return []saveFixtureVariant{
		{
			name: "java", archive: testfixture.KTFSaveArchive, variant: backend.CheckpointKTFJava,
			progress: testfixture.KTFSaveProgress, seenA: testfixture.KTFSaveSeenA, seenB: testfixture.KTFSaveSeenB,
			status: testfixture.KTFSaveStatus, length: testfixture.KTFSaveLength,
			actions: testfixture.KTFSaveActions, kept: testfixture.KTFSaveKept,
		},
		{
			name: "module", archive: testfixture.KTFModuleSaveArchive, variant: backend.CheckpointKTFModule,
			progress: testfixture.KTFModuleSaveProgress, seenA: testfixture.KTFModuleSaveSeenA, seenB: testfixture.KTFModuleSaveSeenB,
			status: testfixture.KTFModuleSaveStatus, length: testfixture.KTFModuleSaveLength,
			actions: testfixture.KTFModuleSaveActions, kept: testfixture.KTFModuleSaveKept,
		},
	}
}

// saveFixture is a started storage fixture beside the store it saves into.
type saveFixture struct {
	t       *testing.T
	variant saveFixtureVariant
	archive []byte
	options SessionOptions
	store   *backend.MemorySaveStore
	session *Session
}

func startSaveFixture(t *testing.T, variant saveFixtureVariant) *saveFixture {
	t.Helper()
	archive, err := variant.archive()
	if err != nil {
		t.Fatalf("author the archive: %v", err)
	}
	store, err := backend.NewMemorySaveStore(nil)
	if err != nil {
		t.Fatal(err)
	}
	fixture := &saveFixture{t: t, variant: variant, archive: archive, store: store, options: SessionOptions{SaveStore: store}}
	// The ordinary loader: the same StartSession a Host calls for any archive.
	fixture.session, err = StartSession(t.Context(), archive, fixture.options)
	if err != nil {
		t.Fatalf("start through the ordinary loader: %v", err)
	}
	t.Cleanup(func() { fixture.session.Close() })
	return fixture
}

func (fixture *saveFixture) word(address uint32) uint32 {
	fixture.t.Helper()
	return binary.LittleEndian.Uint32(readTestBytes(fixture.t, fixture.session.Client, address, 4))
}

// press sets the progress word and sends one key press, which is one action.
func (fixture *saveFixture) press(action int32, progress uint32) {
	fixture.t.Helper()
	writeTestWords(fixture.t, fixture.session.Client, fixture.variant.progress, []uint32{progress})
	before := fixture.word(fixture.variant.actions)
	if err := fixture.session.SendKey(fixture.t.Context(), KeyPressed, action); err != nil {
		fixture.t.Fatalf("press %q: %v", rune(action), err)
	}
	if after := fixture.word(fixture.variant.actions); after != before+1 {
		fixture.t.Fatalf("press %q finished %d actions, want one", rune(action), after-before)
	}
	if err := fixture.session.SendKey(fixture.t.Context(), KeyReleased, action); err != nil {
		fixture.t.Fatalf("release %q: %v", rune(action), err)
	}
	if after := fixture.word(fixture.variant.actions); after != before+1 {
		fixture.t.Fatalf("releasing %q acted again", rune(action))
	}
}

func (fixture *saveFixture) stored() ([]byte, bool) {
	return fixture.store.LoadSave(testfixture.KTFSaveStoreKey)
}

func (fixture *saveFixture) expectStored(when string, want []byte) {
	fixture.t.Helper()
	if data, found := fixture.stored(); !found || !bytes.Equal(data, want) {
		fixture.t.Fatalf("%s: the store holds %x (found=%t), want %x", when, data, found, want)
	}
}

// removed reports whether the save's name is on the removal list, which is how
// this runtime deletes: the bytes stay under their key.
func (fixture *saveFixture) removed() bool {
	list, _ := fixture.store.LoadSave(testfixture.KTFSaveRemovalKey)
	for _, name := range strings.Split(string(list), "\n") {
		if name == testfixture.KTFSaveName {
			return true
		}
	}
	return false
}

func (fixture *saveFixture) expectRead(when string, status, length, seenA, seenB uint32) {
	fixture.t.Helper()
	got := [4]uint32{fixture.word(fixture.variant.status), fixture.word(fixture.variant.length),
		fixture.word(fixture.variant.seenA), fixture.word(fixture.variant.seenB)}
	if want := [4]uint32{status, length, seenA, seenB}; got != want {
		fixture.t.Fatalf("%s: status, length, seen A, seen B = %#x, want %#x", when, got, want)
	}
}

func saveParts(a, b uint32) []byte {
	content := make([]byte, 8)
	binary.LittleEndian.PutUint32(content, a)
	binary.LittleEndian.PutUint32(content[4:], b^testfixture.KTFSaveMask)
	return content
}

// The fixtures are subjects for the tests that follow them, so they have to be
// what they say: started by the ordinary loader, on the variant they name.
func TestKTFSaveFixtureStartsThroughTheOrdinaryLoader(t *testing.T) {
	for _, variant := range saveFixtureVariants() {
		t.Run(variant.name, func(t *testing.T) {
			fixture := startSaveFixture(t, variant)
			if got := checkpointVariant(fixture.session.Client); got != variant.variant {
				t.Fatalf("checkpoint variant = %d, want %d", got, variant.variant)
			}
			if _, found := fixture.stored(); found {
				t.Fatal("starting the fixture stored a save")
			}
			if fixture.word(variant.actions) != 0 || fixture.word(variant.kept) != 0 {
				t.Fatal("the fixture acted before any key")
			}
		})
	}
}

func TestKTFSaveFixtureSavesPatchesReadsAndDeletes(t *testing.T) {
	for _, variant := range saveFixtureVariants() {
		t.Run(variant.name, func(t *testing.T) {
			fixture := startSaveFixture(t, variant)

			fixture.press(testfixture.KTFSaveActionRead, 0)
			fixture.expectRead("a read with no save", testfixture.KTFSaveMissing, 0, 0, 0)

			fixture.press(testfixture.KTFSaveActionSave, 0x11111111)
			fixture.expectStored("after a save", testfixture.KTFSaveContent(0x11111111))

			// A patch writes part A alone and keeps what follows it.
			fixture.press(testfixture.KTFSaveActionPatch, 0x22222222)
			fixture.expectStored("after a patch", saveParts(0x22222222, 0x11111111))

			fixture.press(testfixture.KTFSaveActionRead, 0x7777)
			fixture.expectRead("a read of the patched save", testfixture.KTFSaveFound, 8, 0x22222222, 0x11111111^testfixture.KTFSaveMask)

			fixture.press(testfixture.KTFSaveActionDelete, 0)
			if !fixture.removed() {
				t.Fatal("a delete did not put the save's name on the removal list")
			}
			fixture.press(testfixture.KTFSaveActionRead, 0)
			fixture.expectRead("a read after the delete", testfixture.KTFSaveMissing, 0, 0, 0)

			// A whole rewrite brings the save back and takes its name off the list.
			fixture.press(testfixture.KTFSaveActionSave, 0x33333333)
			fixture.expectStored("after a save over a deleted one", testfixture.KTFSaveContent(0x33333333))
			if fixture.removed() {
				t.Fatal("the saved name is still on the removal list")
			}
			fixture.press(testfixture.KTFSaveActionRead, 0)
			fixture.expectRead("a read of the new save", testfixture.KTFSaveFound, 8, 0x33333333, 0x33333333^testfixture.KTFSaveMask)
		})
	}
}

// A hold keeps a File open across key presses. This runtime stores a write
// before the call returns, so nothing is pending in the store's sense; what
// the Host keeps is the File's own copy of the save and its cursor.
func TestKTFSaveFixtureHoldKeepsAFileAndFinishCompletesIt(t *testing.T) {
	for _, variant := range saveFixtureVariants() {
		t.Run(variant.name, func(t *testing.T) {
			fixture := startSaveFixture(t, variant)
			fixture.press(testfixture.KTFSaveActionSave, 0x33333333)

			fixture.press(testfixture.KTFSaveActionHold, 0x44444444)
			fixture.expectStored("after a hold", saveParts(0x44444444, 0x33333333))
			if fixture.word(variant.kept) == 0 {
				t.Fatal("a hold kept no File")
			}

			fixture.press(testfixture.KTFSaveActionFinish, 0x55555555)
			fixture.expectStored("after the finish", saveParts(0x44444444, 0x55555555))
			if fixture.word(variant.kept) != 0 {
				t.Fatal("the finish did not let go of the File")
			}
		})
	}
}

// A File opened for truncation stores nothing until its first write, so the
// save on disk is whole until then. The finish writes part B alone into a copy
// that began empty, which is the whole of the new save.
func TestKTFSaveFixtureTruncateLeavesTheSaveUntilItIsFinished(t *testing.T) {
	for _, variant := range saveFixtureVariants() {
		t.Run(variant.name, func(t *testing.T) {
			fixture := startSaveFixture(t, variant)
			fixture.press(testfixture.KTFSaveActionSave, 0x33333333)

			fixture.press(testfixture.KTFSaveActionTruncate, 0)
			fixture.expectStored("after a truncating open", testfixture.KTFSaveContent(0x33333333))
			if fixture.word(variant.kept) == 0 {
				t.Fatal("a truncating open kept no File")
			}

			fixture.press(testfixture.KTFSaveActionFinish, 0x66666666)
			partB := make([]byte, 4)
			binary.LittleEndian.PutUint32(partB, 0x66666666^testfixture.KTFSaveMask)
			fixture.expectStored("after finishing the truncated save", partB)

			fixture.press(testfixture.KTFSaveActionRead, 0)
			if status, length := fixture.word(variant.status), fixture.word(variant.length); status != testfixture.KTFSaveFound || length != 4 {
				t.Fatalf("a read of the four-byte save answered status %d and length %d", status, length)
			}
		})
	}
}

// The fixtures exist to be quick-saved and quick-loaded, so they have to be
// valid checkpoint subjects as the tree stands: captured with a File held
// open, restored over the running session, and still answering keys.
func TestKTFSaveFixtureIsACheckpointSubject(t *testing.T) {
	for _, variant := range saveFixtureVariants() {
		t.Run(variant.name, func(t *testing.T) {
			fixture := startSaveFixture(t, variant)
			fixture.press(testfixture.KTFSaveActionSave, 0x33333333)
			fixture.press(testfixture.KTFSaveActionHold, 0x44444444)
			kept := fixture.word(variant.kept)

			saved, err := fixture.session.CaptureCheckpoint(t.Context())
			if err != nil {
				t.Fatalf("capture: %v", err)
			}
			if saved.Variant != variant.variant {
				t.Fatalf("captured variant %d, want %d", saved.Variant, variant.variant)
			}
			prepared, err := PrepareSessionCheckpoint(fixture.archive, saved, fixture.options)
			if err != nil {
				t.Fatalf("prepare: %v", err)
			}
			restored, err := prepared.Commit(t.Context(), fixture.session)
			if err != nil {
				prepared.Discard()
				t.Fatalf("commit: %v", err)
			}
			fixture.session = restored

			if got := fixture.word(variant.kept); got != kept {
				t.Fatalf("the restored session keeps File %#x, want %#x", got, kept)
			}
			fixture.press(testfixture.KTFSaveActionFinish, 0x55555555)
			fixture.expectStored("after finishing in the restored session", saveParts(0x44444444, 0x55555555))
			fixture.press(testfixture.KTFSaveActionRead, 0)
			fixture.expectRead("a read in the restored session", testfixture.KTFSaveFound, 8, 0x44444444, 0x55555555^testfixture.KTFSaveMask)

			for range 3 {
				if _, err := fixture.session.Tick(t.Context()); err != nil {
					t.Fatalf("tick after the load: %v", err)
				}
			}
			if _, err := fixture.session.CaptureCheckpoint(t.Context()); err != nil {
				t.Fatalf("capture after the load: %v", err)
			}
		})
	}
}
