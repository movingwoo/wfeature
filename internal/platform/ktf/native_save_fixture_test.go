package ktf

import (
	"bytes"
	"encoding/binary"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/movingwoo/wfeature/internal/armcore"
	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/testfixture"
)

// The authored save fixture is what a storage test drives with keys, so these
// pin its contract on the native file interface as this platform serves it
// today: what each key does to the save store, to the two tables a write waits
// in, and to the words the module reports through. See
// internal/testfixture/ktf_native_save.go.

// nativeSaveFixtureForms are the fixture's two modules.
var nativeSaveFixtureForms = []struct {
	name  string
	build func() ([]byte, error)
	frame bool
}{
	{"frame callback", testfixture.KTFNativeSaveArchive, true},
	{"no frame callback", testfixture.KTFNativeSaveArchiveWithoutFrame, false},
}

// nativeSaveFixtureStore is a memory store that counts the writes it is given.
// A test can make it refuse them, the way a full disk does, or fail the read
// of one key, the way a damaged one does.
type nativeSaveFixtureStore struct {
	*backend.MemorySaveStore
	// writes counts the writes the store took, and attempts every write it was
	// given, taken or not.
	writes, attempts int
	refuse           bool
	unreadable       string
}

func (store *nativeSaveFixtureStore) StoreSave(name string, data []byte) error {
	store.attempts++
	if store.refuse {
		return errors.New("the fixture store refuses the write")
	}
	store.writes++
	return store.MemorySaveStore.StoreSave(name, data)
}

func (store *nativeSaveFixtureStore) ReadSave(name string) ([]byte, bool, error) {
	if store.unreadable != "" && name == store.unreadable {
		return nil, false, errors.New("the fixture store cannot read the entry")
	}
	return store.MemorySaveStore.ReadSave(name)
}

// snapshot is everything the store holds, for a test that compares the store
// across a step that must not change it.
func (store *nativeSaveFixtureStore) snapshot(t *testing.T) []backend.SaveEntry {
	t.Helper()
	entries, err := store.MemorySaveStore.SnapshotSaves()
	if err != nil {
		t.Fatal(err)
	}
	return entries
}

func newNativeSaveFixtureStore(t *testing.T, entries ...backend.SaveEntry) *nativeSaveFixtureStore {
	t.Helper()
	memory, err := backend.NewMemorySaveStore(entries)
	if err != nil {
		t.Fatal(err)
	}
	return &nativeSaveFixtureStore{MemorySaveStore: memory}
}

// nativeSaveFixture is one session of the fixture and what a test drives it
// with.
type nativeSaveFixture struct {
	t       *testing.T
	archive []byte
	session *NativeSession
	store   *nativeSaveFixtureStore
	clock   *ManualClock
}

// nativeSaveFixtureReading is what READ left in the module's four words.
type nativeSaveFixtureReading struct{ a, b, status, length uint32 }

// startNativeSaveFixture starts one form through the ordinary loader, over a
// store a test may hand to more than one session.
func startNativeSaveFixture(t *testing.T, build func() ([]byte, error), store *nativeSaveFixtureStore) *nativeSaveFixture {
	t.Helper()
	archive, err := build()
	if err != nil {
		t.Fatal(err)
	}
	clock := NewManualClock(time.Unix(1, 0))
	session, err := StartNativeSession(t.Context(), archive, NativeSessionOptions{Clock: clock, SaveStore: store, Width: 32, Height: 48})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(session.Close)
	return &nativeSaveFixture{t: t, archive: archive, session: session, store: store, clock: clock}
}

// press sends one key down and up again, the way a Host does.
func (fixture *nativeSaveFixture) press(key int32) {
	fixture.t.Helper()
	for _, event := range []int32{KeyPressed, KeyReleased} {
		if err := fixture.session.SendKey(fixture.t.Context(), event, key); err != nil {
			fixture.t.Fatalf("key %q: %v", key, err)
		}
	}
}

func (fixture *nativeSaveFixture) word(address uint32) uint32 {
	fixture.t.Helper()
	value, err := fixture.session.Client.ReadWord(address)
	if err != nil {
		fixture.t.Fatal(err)
	}
	return value
}

// progress writes the word the module saves, through the core's memory.
func (fixture *nativeSaveFixture) progress(value uint32) {
	fixture.t.Helper()
	word := binary.LittleEndian.AppendUint32(nil, value)
	if err := fixture.session.Client.core.Memory().Write(testfixture.KTFNativeSaveProgress, word); err != nil {
		fixture.t.Fatal(err)
	}
}

// read presses READ and answers what the module reported.
func (fixture *nativeSaveFixture) read() nativeSaveFixtureReading {
	fixture.t.Helper()
	fixture.press(testfixture.KTFNativeSaveKeyRead)
	return nativeSaveFixtureReading{
		a:      fixture.word(testfixture.KTFNativeSaveSeenA),
		b:      fixture.word(testfixture.KTFNativeSaveSeenB),
		status: fixture.word(testfixture.KTFNativeSaveStatus),
		length: fixture.word(testfixture.KTFNativeSaveLength),
	}
}

// wantStored fails unless the store holds exactly want, and nil is no save at
// all: an emptied save is a save that is there and holds nothing.
func (fixture *nativeSaveFixture) wantStored(when string, want []byte) {
	fixture.t.Helper()
	got, found, err := fixture.store.ReadSave(testfixture.KTFNativeSaveStoreKey)
	if err != nil {
		fixture.t.Fatal(err)
	}
	if found != (want != nil) || !bytes.Equal(got, want) {
		fixture.t.Fatalf("%s: the store holds %x (found %t), want %x", when, got, found, want)
	}
}

// wantPending fails unless the platform is keeping exactly want for the save
// without having stored it, and nil is nothing kept back. A write waits either
// marked for the next boundary or refused by an earlier one; wantRefused tells
// the two apart.
func (fixture *nativeSaveFixture) wantPending(when string, want []byte) {
	fixture.t.Helper()
	platform := fixture.session.platform
	marked := platform.unsaved[testfixture.KTFNativeSaveName] || platform.refused[testfixture.KTFNativeSaveName]
	if marked != (want != nil) || marked && !bytes.Equal(platform.written[testfixture.KTFNativeSaveName], want) {
		fixture.t.Fatalf("%s: the platform holds %x back (marked %t), want %x", when, platform.written[testfixture.KTFNativeSaveName], marked, want)
	}
}

// wantRefused fails unless the save is waiting because a boundary gave it to
// the store and the store would not take it.
func (fixture *nativeSaveFixture) wantRefused(when string, want bool) {
	fixture.t.Helper()
	platform := fixture.session.platform
	if platform.refused[testfixture.KTFNativeSaveName] != want || want && platform.unsaved[testfixture.KTFNativeSaveName] {
		fixture.t.Fatalf("%s: refused=%t and unsaved=%t, want refused=%t", when,
			platform.refused[testfixture.KTFNativeSaveName], platform.unsaved[testfixture.KTFNativeSaveName], want)
	}
}

// quickSave takes a checkpoint of the fixture's session and carries it through
// its bytes, the way a slot does.
func (fixture *nativeSaveFixture) quickSave() backend.Checkpoint {
	fixture.t.Helper()
	checkpoint, err := fixture.session.CaptureCheckpoint(fixture.t.Context())
	if err != nil {
		fixture.t.Fatalf("quick save: %v", err)
	}
	encoded, err := backend.EncodeCheckpoint(checkpoint)
	if err != nil {
		fixture.t.Fatal(err)
	}
	decoded, err := backend.DecodeCheckpoint(encoded, backend.SaveIdentity(fixture.archive))
	if err != nil {
		fixture.t.Fatal(err)
	}
	return decoded
}

// tryQuickLoad loads a checkpoint over the fixture's running session, on the
// store that session runs over. It answers the fixture of the restored
// session, or the refusal, and after a refusal the running session is still
// the fixture's own.
func (fixture *nativeSaveFixture) tryQuickLoad(checkpoint backend.Checkpoint) (*nativeSaveFixture, error) {
	fixture.t.Helper()
	clock := NewManualClock(time.Unix(500, 0))
	prepared, err := PrepareNativeSessionCheckpoint(fixture.archive, checkpoint, NativeSessionOptions{Clock: clock, SaveStore: fixture.store})
	if err != nil {
		return nil, err
	}
	defer prepared.Discard()
	if calls, first := prepared.PreparationStoreCalls(); calls != 0 {
		fixture.t.Fatalf("checking the load made %d save store calls, the first being %s", calls, first)
	}
	session, err := prepared.Commit(fixture.t.Context(), fixture.session, fixture.store)
	if err != nil {
		return nil, err
	}
	fixture.t.Cleanup(session.Close)
	return &nativeSaveFixture{t: fixture.t, archive: fixture.archive, session: session, store: fixture.store, clock: clock}, nil
}

// quickLoad is tryQuickLoad for a load that has to be accepted.
func (fixture *nativeSaveFixture) quickLoad(checkpoint backend.Checkpoint) *nativeSaveFixture {
	fixture.t.Helper()
	restored, err := fixture.tryQuickLoad(checkpoint)
	if err != nil {
		fixture.t.Fatalf("quick load: %v", err)
	}
	return restored
}

// boundary runs what the form has for an ordinary boundary that is not a file
// close: the next frame where one is registered, and a tick that finds nothing
// to run where none is.
func (fixture *nativeSaveFixture) boundary(frame bool) {
	fixture.t.Helper()
	if frame {
		fixture.frame()
		return
	}
	fixture.clock.Advance(time.Second)
	if ran, err := fixture.session.Tick(fixture.t.Context()); err != nil || ran {
		fixture.t.Fatalf("tick with no frame callback = %t, %v", ran, err)
	}
}

// frame runs the one frame that is due next.
func (fixture *nativeSaveFixture) frame() {
	fixture.t.Helper()
	if !fixture.session.SkipToNextDeadline() {
		fixture.t.Fatal("the fixture has no frame scheduled")
	}
	if ran, err := fixture.session.Tick(fixture.t.Context()); err != nil || !ran {
		fixture.t.Fatalf("frame = %t, %v", ran, err)
	}
}

// TestNativeSaveFixtureStartsThroughTheOrdinaryLoader covers what tells the two
// forms apart before any key: one has a frame the platform calls back, counts
// and presents, and the other has nothing for a tick to run.
func TestNativeSaveFixtureStartsThroughTheOrdinaryLoader(t *testing.T) {
	for _, form := range nativeSaveFixtureForms {
		t.Run(form.name, func(t *testing.T) {
			fixture := startNativeSaveFixture(t, form.build, newNativeSaveFixtureStore(t))
			session, platform := fixture.session, fixture.session.platform
			if !IsNativeArchive(fixture.archive) || session.Archive.Info.ApplicationID != testfixture.KTFNativeSaveApplicationID {
				t.Fatal("the fixture is not the native package it is authored as")
			}
			if owner := session.SaveOwner(); owner != testfixture.KTFNativeSaveOwner {
				t.Fatalf("save owner = %q, want the exported %q", owner, testfixture.KTFNativeSaveOwner)
			}
			if fixture.word(testfixture.KTFNativeSaveStartupCounter) != 1 {
				t.Fatal("fixture did not run its startup event exactly once")
			}
			// Starting touches no file: the save is the keys' business.
			if len(platform.FileOpens()) != 0 || len(platform.files) != 0 || fixture.store.writes != 0 {
				t.Fatalf("startup opened %d files and stored %d times", len(platform.FileOpens()), fixture.store.writes)
			}
			fixture.wantStored("after startup", nil)
			fixture.wantPending("after startup", nil)
			if !form.frame {
				// Nothing is scheduled, so a tick runs nothing however far the
				// clock has moved.
				if platform.frame != nil || session.SkipToNextDeadline() {
					t.Fatal("the form without a frame callback registered one")
				}
				fixture.clock.Advance(time.Second)
				if ran, err := session.Tick(t.Context()); err != nil || ran {
					t.Fatalf("tick with no frame callback = %t, %v", ran, err)
				}
				if fixture.word(testfixture.KTFNativeSaveFrameCounter) != 0 || session.Flushes() != 0 {
					t.Fatal("the form without a frame callback drew a frame")
				}
				return
			}
			if platform.FrameInterval() != 16*time.Millisecond {
				t.Fatalf("frame interval = %v, want the 16ms the start event asks for", platform.FrameInterval())
			}
			for round := 1; round <= 3; round++ {
				fixture.frame()
				pixels, width, height, flushes := session.Frame()
				if width != 32 || height != 48 || flushes != uint32(round) || pixels[0] != byte(round) || fixture.word(testfixture.KTFNativeSaveFrameCounter) != uint32(round) {
					t.Fatalf("frame %d did not count, fill and present: %dx%d, flushes=%d, pixel=%x", round, width, height, flushes, pixels[:4])
				}
			}
			if fixture.word(testfixture.KTFNativeSaveStartupCounter) != 1 {
				t.Fatal("frames replayed startup")
			}
		})
	}
}

// TestNativeSaveFixtureSavesPatchesAndReads covers the three keys that close
// the file they open, which behave the same in both forms: the close is what
// hands the write to the store.
func TestNativeSaveFixtureSavesPatchesAndReads(t *testing.T) {
	const first, second, other = 0x11111111, 0x22222222, 0x7f7f7f7f
	for _, form := range nativeSaveFixtureForms {
		t.Run(form.name, func(t *testing.T) {
			fixture := startNativeSaveFixture(t, form.build, newNativeSaveFixtureStore(t))
			missing := nativeSaveFixtureReading{status: testfixture.KTFNativeSaveStatusMissing}
			if got := fixture.read(); got != missing {
				t.Fatalf("READ with no save = %+v, want %+v", got, missing)
			}
			if fixture.store.writes != 0 {
				t.Fatal("READ of a missing save wrote to the store")
			}
			fixture.wantStored("after READ of a missing save", nil)

			fixture.progress(first)
			fixture.press(testfixture.KTFNativeSaveKeySave)
			fixture.wantStored("after SAVE", testfixture.KTFNativeSaveFile(first, first))
			if fixture.store.writes != 1 {
				t.Fatalf("SAVE stored %d times, want the one write its close makes", fixture.store.writes)
			}

			// READ answers from the save and not from the word SAVE wrote it from.
			fixture.progress(other)
			found := nativeSaveFixtureReading{a: first, b: first ^ testfixture.KTFNativeSaveMask, status: testfixture.KTFNativeSaveStatusFound, length: 8}
			if got := fixture.read(); got != found {
				t.Fatalf("READ after SAVE = %+v, want %+v", got, found)
			}

			fixture.progress(second)
			fixture.press(testfixture.KTFNativeSaveKeyPatch)
			fixture.wantStored("after PATCH", testfixture.KTFNativeSaveFile(second, first))
			found.a = second
			if got := fixture.read(); got != found {
				t.Fatalf("READ after PATCH = %+v, want %+v", got, found)
			}

			// Each of the three closed what it opened and left nothing behind.
			if len(fixture.session.platform.files) != 0 || fixture.word(testfixture.KTFNativeSaveHandle) != 0 {
				t.Fatal("a key that closes its file left one open")
			}
			fixture.wantPending("after the closing keys", nil)
			if fixture.word(testfixture.KTFNativeSaveStartupCounter) != 1 {
				t.Fatal("keys replayed startup")
			}
		})
	}
}

// TestNativeSaveFixtureRewritesTheWholeSaveOnlyOnSave tells SAVE from PATCH on
// a save longer than the one the fixture writes: PATCH leaves the tail where it
// is, and SAVE leaves no byte of the file it replaced.
func TestNativeSaveFixtureRewritesTheWholeSaveOnlyOnSave(t *testing.T) {
	const first, second = 0x11111111, 0x22222222
	longer := []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12}
	for _, form := range nativeSaveFixtureForms {
		t.Run(form.name, func(t *testing.T) {
			store := newNativeSaveFixtureStore(t, backend.SaveEntry{Key: testfixture.KTFNativeSaveStoreKey, Data: longer})
			fixture := startNativeSaveFixture(t, form.build, store)
			want := nativeSaveFixtureReading{a: 0x04030201, b: 0x08070605, status: testfixture.KTFNativeSaveStatusFound, length: 12}
			if got := fixture.read(); got != want {
				t.Fatalf("READ of a longer save = %+v, want %+v", got, want)
			}
			fixture.progress(first)
			fixture.press(testfixture.KTFNativeSaveKeyPatch)
			fixture.wantStored("after PATCH", append(binary.LittleEndian.AppendUint32(nil, first), longer[4:]...))
			fixture.progress(second)
			fixture.press(testfixture.KTFNativeSaveKeySave)
			fixture.wantStored("after SAVE", testfixture.KTFNativeSaveFile(second, second))
		})
	}
}

// TestNativeSaveFixtureActsOncePerPress reads the platform's own list of opens,
// which is what says a press was one action and which one: a press arrives as
// two events and its release as a third, and a key with no action here is the
// digit the contract's DELETE would have had.
func TestNativeSaveFixtureActsOncePerPress(t *testing.T) {
	for _, form := range nativeSaveFixtureForms {
		t.Run(form.name, func(t *testing.T) {
			fixture := startNativeSaveFixture(t, form.build, newNativeSaveFixtureStore(t))
			// The action is done when the press returns, and the release adds
			// nothing to it.
			for _, event := range []int32{KeyPressed, KeyReleased} {
				if err := fixture.session.SendKey(t.Context(), event, testfixture.KTFNativeSaveKeySave); err != nil {
					t.Fatal(err)
				}
				if opens := len(fixture.session.platform.FileOpens()); opens != 1 || fixture.store.writes != 1 {
					t.Fatalf("after key event %d SAVE had opened %d files and stored %d times, want 1 and 1", event, opens, fixture.store.writes)
				}
			}
			for _, key := range []int32{
				testfixture.KTFNativeSaveKeyPatch,
				testfixture.KTFNativeSaveKeyRead,
				'4', '0', '8', '9', KeyFire, KeyUp, KeyClear,
				testfixture.KTFNativeSaveKeyFinish, // nothing is kept yet
				testfixture.KTFNativeSaveKeyHold,
				testfixture.KTFNativeSaveKeyHold,     // one object is kept at a time
				testfixture.KTFNativeSaveKeyTruncate, // and that goes for this key too
				testfixture.KTFNativeSaveKeyFinish,
				testfixture.KTFNativeSaveKeyTruncate,
				testfixture.KTFNativeSaveKeyFinish,
			} {
				fixture.press(key)
			}
			want := []NativeFileOpen{
				{Name: testfixture.KTFNativeSaveName, Mode: nativeModeWriteTruncate, Found: false},
				{Name: testfixture.KTFNativeSaveName, Mode: 2, Found: true},
				{Name: testfixture.KTFNativeSaveName, Mode: nativeModeRead, Found: true},
				{Name: testfixture.KTFNativeSaveName, Mode: 2, Found: true},
				{Name: testfixture.KTFNativeSaveName, Mode: nativeModeWriteTruncate, Found: true},
			}
			if got := fixture.session.platform.FileOpens(); !reflect.DeepEqual(got, want) {
				t.Fatalf("the keys opened %+v, want %+v", got, want)
			}
			if len(fixture.session.platform.files) != 0 || fixture.word(testfixture.KTFNativeSaveHandle) != 0 {
				t.Fatal("FINISH left the kept object open")
			}
		})
	}
}

// TestNativeSaveFixtureHeldWritesReachTheStoreAtTheFrameEnd covers the two
// keys that keep their file open, in the form that registers a frame: the
// write is in the platform's tables when the key returns and in the store when
// the next frame ends, which is where this platform carries a write that no
// close followed.
func TestNativeSaveFixtureHeldWritesReachTheStoreAtTheFrameEnd(t *testing.T) {
	const first, second, third, fourth = 0x11111111, 0x22222222, 0x33333333, 0x44444444
	fixture := startNativeSaveFixture(t, testfixture.KTFNativeSaveArchive, newNativeSaveFixtureStore(t))
	platform := fixture.session.platform
	fixture.progress(first)
	fixture.press(testfixture.KTFNativeSaveKeySave)
	fixture.frame()

	fixture.progress(second)
	fixture.press(testfixture.KTFNativeSaveKeyHold)
	handle := fixture.word(testfixture.KTFNativeSaveHandle)
	if file := platform.files[handle]; file == nil || !file.writable || file.position != 4 || len(platform.files) != 1 {
		t.Fatalf("HOLD did not keep one writable object behind part A: %+v", file)
	}
	fixture.wantStored("after HOLD", testfixture.KTFNativeSaveFile(first, first))
	fixture.wantPending("after HOLD", testfixture.KTFNativeSaveFile(second, first))
	// A tick that finds the frame not yet due runs nothing and stores nothing.
	if ran, err := fixture.session.Tick(t.Context()); err != nil || ran {
		t.Fatalf("tick before the frame is due = %t, %v", ran, err)
	}
	fixture.wantStored("after a tick with no frame due", testfixture.KTFNativeSaveFile(first, first))
	fixture.wantPending("after a tick with no frame due", testfixture.KTFNativeSaveFile(second, first))
	fixture.frame()
	fixture.wantStored("a frame after HOLD", testfixture.KTFNativeSaveFile(second, first))
	fixture.wantPending("a frame after HOLD", nil)

	fixture.progress(third)
	fixture.press(testfixture.KTFNativeSaveKeyFinish)
	fixture.wantStored("after FINISH", testfixture.KTFNativeSaveFile(second, third))
	if len(platform.files) != 0 || fixture.word(testfixture.KTFNativeSaveHandle) != 0 {
		t.Fatal("FINISH left the kept object open")
	}
	whole := nativeSaveFixtureReading{a: second, b: third ^ testfixture.KTFNativeSaveMask, status: testfixture.KTFNativeSaveStatusFound, length: 8}
	if got := fixture.read(); got != whole {
		t.Fatalf("READ after HOLD and FINISH = %+v, want %+v", got, whole)
	}

	// The emptying is itself a write the platform holds back: the store keeps
	// the save as it was until the frame ends, and has an empty one after it.
	fixture.press(testfixture.KTFNativeSaveKeyTruncate)
	handle = fixture.word(testfixture.KTFNativeSaveHandle)
	if file := platform.files[handle]; file == nil || !file.writable || file.position != 0 || len(file.data) != 0 {
		t.Fatalf("TRUNCATE did not keep one emptied object: %+v", file)
	}
	fixture.wantStored("after TRUNCATE", testfixture.KTFNativeSaveFile(second, third))
	fixture.wantPending("after TRUNCATE", []byte{})
	fixture.frame()
	fixture.wantStored("a frame after TRUNCATE", []byte{})
	// An emptied save is there and holds nothing, which is not a missing one,
	// and neither part of the earlier READ is left in the seen words.
	emptied := nativeSaveFixtureReading{status: testfixture.KTFNativeSaveStatusFound}
	if got := fixture.read(); got != emptied {
		t.Fatalf("READ of an emptied save = %+v, want %+v", got, emptied)
	}

	// Part B lands where the emptied file's cursor stands, which is its front.
	fixture.progress(fourth)
	fixture.press(testfixture.KTFNativeSaveKeyFinish)
	partB := binary.LittleEndian.AppendUint32(nil, fourth^testfixture.KTFNativeSaveMask)
	fixture.wantStored("after FINISH on an emptied save", partB)
	short := nativeSaveFixtureReading{a: fourth ^ testfixture.KTFNativeSaveMask, status: testfixture.KTFNativeSaveStatusFound, length: 4}
	if got := fixture.read(); got != short {
		t.Fatalf("READ of a four byte save = %+v, want %+v", got, short)
	}
}

// TestNativeSaveFixtureWithoutAFrameKeepsWritesPending is the other form. No
// frame ends, so a write that no close followed is in NativePlatform.written
// and NativePlatform.unsaved and nowhere else until the session is closed.
func TestNativeSaveFixtureWithoutAFrameKeepsWritesPending(t *testing.T) {
	const first, second, third, fourth, fifth = 0x11111111, 0x22222222, 0x33333333, 0x44444444, 0x55555555
	store := newNativeSaveFixtureStore(t)
	fixture := startNativeSaveFixture(t, testfixture.KTFNativeSaveArchiveWithoutFrame, store)
	idle := func(fixture *nativeSaveFixture) {
		t.Helper()
		for range 5 {
			fixture.clock.Advance(time.Second)
			if ran, err := fixture.session.Tick(t.Context()); err != nil || ran {
				t.Fatalf("tick with no frame callback = %t, %v", ran, err)
			}
		}
	}
	fixture.progress(first)
	fixture.press(testfixture.KTFNativeSaveKeySave)
	fixture.progress(second)
	fixture.press(testfixture.KTFNativeSaveKeyHold)
	idle(fixture)
	fixture.wantStored("after HOLD and five ticks", testfixture.KTFNativeSaveFile(first, first))
	fixture.wantPending("after HOLD and five ticks", testfixture.KTFNativeSaveFile(second, first))
	if store.writes != 1 {
		t.Fatalf("the store was written %d times, want only SAVE's", store.writes)
	}
	fixture.session.Close()
	fixture.wantStored("after the session closed", testfixture.KTFNativeSaveFile(second, first))
	if store.writes != 2 {
		t.Fatalf("the store was written %d times, want SAVE's and the close's", store.writes)
	}

	// A second session over the same store reads what the first one's close
	// stored, and its emptying open waits the same way.
	fixture = startNativeSaveFixture(t, testfixture.KTFNativeSaveArchiveWithoutFrame, store)
	found := nativeSaveFixtureReading{a: second, b: first ^ testfixture.KTFNativeSaveMask, status: testfixture.KTFNativeSaveStatusFound, length: 8}
	if got := fixture.read(); got != found {
		t.Fatalf("READ in a second session = %+v, want %+v", got, found)
	}
	fixture.press(testfixture.KTFNativeSaveKeyTruncate)
	idle(fixture)
	fixture.wantStored("after TRUNCATE and five ticks", testfixture.KTFNativeSaveFile(second, first))
	fixture.wantPending("after TRUNCATE and five ticks", []byte{})
	fixture.progress(third)
	fixture.press(testfixture.KTFNativeSaveKeyFinish)
	fixture.wantStored("after FINISH on an emptied save", binary.LittleEndian.AppendUint32(nil, third^testfixture.KTFNativeSaveMask))
	fixture.wantPending("after FINISH on an emptied save", nil)

	// HOLD and FINISH together write both parts, each from the progress word
	// as it stood when its key arrived.
	fixture.progress(fourth)
	fixture.press(testfixture.KTFNativeSaveKeyHold)
	fixture.progress(fifth)
	fixture.press(testfixture.KTFNativeSaveKeyFinish)
	fixture.wantStored("after HOLD and FINISH", testfixture.KTFNativeSaveFile(fourth, fifth))
}

// TestNativeSaveFixtureAnyCloseStoresWhatIsPending pins what a test using this
// fixture has to know before it presses READ: this platform stores every
// pending file when any file is closed, and READ closes the one it opened.
func TestNativeSaveFixtureAnyCloseStoresWhatIsPending(t *testing.T) {
	const first, second = 0x11111111, 0x22222222
	fixture := startNativeSaveFixture(t, testfixture.KTFNativeSaveArchiveWithoutFrame, newNativeSaveFixtureStore(t))
	fixture.progress(first)
	fixture.press(testfixture.KTFNativeSaveKeySave)
	fixture.progress(second)
	fixture.press(testfixture.KTFNativeSaveKeyHold)
	fixture.wantStored("after HOLD", testfixture.KTFNativeSaveFile(first, first))
	// The module reads back what it wrote, from the session's own copy.
	held := nativeSaveFixtureReading{a: second, b: first ^ testfixture.KTFNativeSaveMask, status: testfixture.KTFNativeSaveStatusFound, length: 8}
	if got := fixture.read(); got != held {
		t.Fatalf("READ while a write is held = %+v, want %+v", got, held)
	}
	fixture.wantStored("after READ closed its own file", testfixture.KTFNativeSaveFile(second, first))
	fixture.wantPending("after READ closed its own file", nil)
	if fixture.word(testfixture.KTFNativeSaveHandle) == 0 || len(fixture.session.platform.files) != 1 {
		t.Fatal("READ closed the object HOLD kept")
	}
}

// TestNativeSaveFixtureReadFollowsTheStoreUntilTheSessionWrites covers where a
// READ's answer comes from. The fixture has no key that deletes, so the store
// is changed behind the session, the way an import between two reads would
// change it. Until the session writes the save, every READ opens what the
// store holds at that moment, and a save that has gone is reported missing with
// nothing of the earlier READ left in the words. Once the session has written
// the save, the platform answers from the session's own copy.
func TestNativeSaveFixtureReadFollowsTheStoreUntilTheSessionWrites(t *testing.T) {
	const first, second, third = 0x11111111, 0x22222222, 0x33333333
	for _, form := range nativeSaveFixtureForms {
		t.Run(form.name, func(t *testing.T) {
			store := newNativeSaveFixtureStore(t, backend.SaveEntry{Key: testfixture.KTFNativeSaveStoreKey, Data: testfixture.KTFNativeSaveFile(first, first)})
			fixture := startNativeSaveFixture(t, form.build, store)
			behind := func(entries ...backend.SaveEntry) {
				t.Helper()
				if err := store.ReplaceSaves(entries); err != nil {
					t.Fatal(err)
				}
			}
			found := func(progress uint32) nativeSaveFixtureReading {
				return nativeSaveFixtureReading{a: progress, b: progress ^ testfixture.KTFNativeSaveMask, status: testfixture.KTFNativeSaveStatusFound, length: 8}
			}
			if got := fixture.read(); got != found(first) {
				t.Fatalf("READ of the save the session started over = %+v, want %+v", got, found(first))
			}
			behind()
			missing := nativeSaveFixtureReading{status: testfixture.KTFNativeSaveStatusMissing}
			if got := fixture.read(); got != missing {
				t.Fatalf("READ after the save went away = %+v, want %+v", got, missing)
			}
			behind(backend.SaveEntry{Key: testfixture.KTFNativeSaveStoreKey, Data: testfixture.KTFNativeSaveFile(second, second)})
			if got := fixture.read(); got != found(second) {
				t.Fatalf("READ after the save was replaced = %+v, want %+v", got, found(second))
			}
			if store.writes != 0 {
				t.Fatalf("READ stored %d times", store.writes)
			}

			fixture.progress(third)
			fixture.press(testfixture.KTFNativeSaveKeySave)
			behind(backend.SaveEntry{Key: testfixture.KTFNativeSaveStoreKey, Data: testfixture.KTFNativeSaveFile(first, first)})
			if got := fixture.read(); got != found(third) {
				t.Fatalf("READ after the session saved = %+v, want its own copy %+v", got, found(third))
			}
		})
	}
}

// TestNativeSaveFixtureReportsAReadThatFallsShort reaches the third status.
// Nothing this platform serves makes a read fall short of the record the same
// file reported, so the test serves the read slot itself.
func TestNativeSaveFixtureReportsAReadThatFallsShort(t *testing.T) {
	const first = 0x11111111
	fixture := startNativeSaveFixture(t, testfixture.KTFNativeSaveArchive, newNativeSaveFixtureStore(t))
	fixture.progress(first)
	fixture.press(testfixture.KTFNativeSaveKeySave)
	fixture.session.Client.Serve(nativeFileSurface, nativeFileRead, func(*armcore.Thread) (uint32, error) { return 0, nil })
	failed := nativeSaveFixtureReading{status: testfixture.KTFNativeSaveStatusError, length: 8}
	if got := fixture.read(); got != failed {
		t.Fatalf("READ that moved nothing = %+v, want %+v", got, failed)
	}
	if len(fixture.session.platform.files) != 0 {
		t.Fatal("a failed READ left its file open")
	}
}

// TestNativeSaveFixtureStoreFailureIsTheKeysError covers why the third status
// is not what a failing store looks like here. The platform does not answer the
// module when the store cannot be read: the call the key was delivered through
// fails with the store's own error, and the status word still holds what READ
// cleared it to, so a test has to look at the error and not at the word.
func TestNativeSaveFixtureStoreFailureIsTheKeysError(t *testing.T) {
	// A key below the save's own name makes the save's name a directory, which
	// a store reports as a failed read rather than as an absent entry.
	store := newNativeSaveFixtureStore(t, backend.SaveEntry{Key: testfixture.KTFNativeSaveStoreKey + "/below", Data: []byte{1}})
	fixture := startNativeSaveFixture(t, testfixture.KTFNativeSaveArchiveWithoutFrame, store)
	err := fixture.session.SendKey(t.Context(), KeyPressed, testfixture.KTFNativeSaveKeyRead)
	if err == nil || !strings.Contains(err.Error(), "directory") {
		t.Fatalf("READ over an unreadable store = %v, want the store's error", err)
	}
	if fixture.word(testfixture.KTFNativeSaveStatus) != testfixture.KTFNativeSaveStatusMissing || len(fixture.session.platform.files) != 0 {
		t.Fatal("the failed open answered the module or left a file open")
	}
}

// TestNativeSaveFixtureIsACheckpointSubject takes the fixture through the
// quick save and quick load this platform has today. It asserts only what a
// load that leaves ordinary saves alone would also show: between the capture
// and the load the running module changes its own memory and nothing else, so
// the save a restored module reads, and the one it completes through a file it
// kept, are the same either way.
func TestNativeSaveFixtureIsACheckpointSubject(t *testing.T) {
	const first, second, third = 0x11111111, 0x22222222, 0x33333333
	for _, form := range nativeSaveFixtureForms {
		for _, held := range []bool{false, true} {
			name := form.name + ", after SAVE"
			if held {
				name = form.name + ", holding a file"
			}
			t.Run(name, func(t *testing.T) {
				source := startNativeSaveFixture(t, form.build, newNativeSaveFixtureStore(t))
				source.progress(first)
				source.press(testfixture.KTFNativeSaveKeySave)
				saved := testfixture.KTFNativeSaveFile(first, first)
				if form.frame {
					source.frame()
				}
				if held {
					source.progress(second)
					source.press(testfixture.KTFNativeSaveKeyHold)
				}
				progress, handle := source.word(testfixture.KTFNativeSaveProgress), source.word(testfixture.KTFNativeSaveHandle)
				frames := source.word(testfixture.KTFNativeSaveFrameCounter)
				checkpoint, err := source.session.CaptureCheckpoint(t.Context())
				if err != nil {
					t.Fatalf("the fixture cannot be captured: %v", err)
				}
				encoded, err := backend.EncodeCheckpoint(checkpoint)
				if err != nil {
					t.Fatal(err)
				}
				if checkpoint, err = backend.DecodeCheckpoint(encoded, backend.SaveIdentity(source.archive)); err != nil {
					t.Fatal(err)
				}

				// The running module moves on in memory only.
				source.progress(0x7f7f7f7f)
				clock := NewManualClock(time.Unix(500, 0))
				prepared, err := PrepareNativeSessionCheckpoint(source.archive, checkpoint, NativeSessionOptions{Clock: clock, SaveStore: source.store})
				if err != nil {
					t.Fatalf("the fixture's checkpoint cannot be prepared: %v", err)
				}
				defer prepared.Discard()
				session, err := prepared.Commit(t.Context(), source.session, source.store)
				if err != nil {
					t.Fatalf("the fixture's checkpoint cannot be adopted: %v", err)
				}
				defer session.Close()
				restored := &nativeSaveFixture{t: t, archive: source.archive, session: session, store: source.store, clock: clock}
				if restored.word(testfixture.KTFNativeSaveStartupCounter) != 1 {
					t.Fatal("the load replayed startup")
				}
				if restored.word(testfixture.KTFNativeSaveProgress) != progress || restored.word(testfixture.KTFNativeSaveHandle) != handle || restored.word(testfixture.KTFNativeSaveFrameCounter) != frames {
					t.Fatal("the load did not bring back the module's own words")
				}
				if form.frame {
					restored.frame()
					if restored.word(testfixture.KTFNativeSaveFrameCounter) != frames+1 {
						t.Fatal("the restored module's frame did not carry on from the capture")
					}
				}
				if !held {
					restored.wantStored("after the load", saved)
					found := nativeSaveFixtureReading{a: first, b: first ^ testfixture.KTFNativeSaveMask, status: testfixture.KTFNativeSaveStatusFound, length: 8}
					if got := restored.read(); got != found {
						t.Fatalf("READ after the load = %+v, want %+v", got, found)
					}
					restored.progress(third)
					restored.press(testfixture.KTFNativeSaveKeySave)
					restored.wantStored("after the restored module saved", testfixture.KTFNativeSaveFile(third, third))
				} else {
					// The object kept across the capture still takes the rest
					// of the save.
					if file := session.platform.files[handle]; file == nil || !file.writable || file.position != 4 {
						t.Fatalf("the load lost the object HOLD kept: %+v", file)
					}
					restored.progress(third)
					restored.press(testfixture.KTFNativeSaveKeyFinish)
					restored.wantStored("after the restored module finished", testfixture.KTFNativeSaveFile(second, third))
				}
				if _, err := session.CaptureCheckpoint(t.Context()); err != nil {
					t.Fatalf("the restored fixture cannot be captured again: %v", err)
				}
			})
		}
	}
}
