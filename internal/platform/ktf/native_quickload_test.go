package ktf

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/cheat"
	"github.com/movingwoo/wfeature/internal/testfixture"
)

// A quick load brings back the title and leaves its saves alone. These drive
// the authored save fixture with keys, in both of its forms, through a quick
// save and a quick load, and look at the save store around each step.

// nativeSaveFound is what READ reports for a whole save whose parts were
// written at progress a and b.
func nativeSaveFound(a, b uint32) nativeSaveFixtureReading {
	return nativeSaveFixtureReading{a: a, b: b ^ testfixture.KTFNativeSaveMask, status: testfixture.KTFNativeSaveStatusFound, length: 8}
}

// The scenario the rule exists for: quick save, the title saves, quick load.
// The save made after the quick save is still the save, the load itself and
// the boundaries after it write nothing, the next read is the save as it is
// and the next write changes only what it writes.
func TestNativeQuickLoadLeavesTheSaveMadeAfterTheQuickSave(t *testing.T) {
	const first, second = 0x11111111, 0x22222222
	for _, form := range nativeSaveFixtureForms {
		t.Run(form.name, func(t *testing.T) {
			source := startNativeSaveFixture(t, form.build, newNativeSaveFixtureStore(t))
			store := source.store
			source.progress(first)
			source.press(testfixture.KTFNativeSaveKeySave)
			checkpoint := source.quickSave()

			source.progress(second)
			source.press(testfixture.KTFNativeSaveKeySave)
			source.wantStored("after the save made after the quick save", testfixture.KTFNativeSaveFile(second, second))
			before, attempts := store.snapshot(t), store.attempts

			restored := source.quickLoad(checkpoint)
			if restored.word(testfixture.KTFNativeSaveProgress) != first || restored.word(testfixture.KTFNativeSaveStartupCounter) != 1 {
				t.Fatal("the load did not bring the title back to the quick save")
			}
			if after := store.snapshot(t); !reflect.DeepEqual(after, before) || store.attempts != attempts {
				t.Fatalf("the load changed the saves or wrote to the store: %d writes, %+v", store.attempts-attempts, after)
			}
			for range 10 {
				restored.boundary(form.frame)
			}
			if after := store.snapshot(t); !reflect.DeepEqual(after, before) || store.attempts != attempts {
				t.Fatalf("the boundaries after the load wrote to the store: %d writes, %+v", store.attempts-attempts, after)
			}
			if len(restored.session.platform.written) != 0 {
				t.Fatal("the restored session started with files of its own")
			}

			// The title reads the save as it is now, not as it was at the
			// quick save.
			if got := restored.read(); got != nativeSaveFound(second, second) {
				t.Fatalf("READ after the load = %+v, want the save made after the quick save %+v", got, nativeSaveFound(second, second))
			}
			// And a write of four bytes changes four bytes of it.
			restored.press(testfixture.KTFNativeSaveKeyPatch)
			restored.wantStored("after the restored title patched its save", testfixture.KTFNativeSaveFile(first, second))

			// The displaced session is gone, and closing what is left of it
			// reaches no store.
			attempts = store.attempts
			source.session.Close()
			if store.attempts != attempts {
				t.Fatal("closing the displaced session wrote to the store")
			}
		})
	}
}

// Loading twice is loading once: nothing is set aside by the first load for
// the second to lose.
func TestNativeQuickLoadTwiceWritesNothing(t *testing.T) {
	const first, second = 0x11111111, 0x22222222
	for _, form := range nativeSaveFixtureForms {
		t.Run(form.name, func(t *testing.T) {
			source := startNativeSaveFixture(t, form.build, newNativeSaveFixtureStore(t))
			store := source.store
			source.progress(first)
			source.press(testfixture.KTFNativeSaveKeySave)
			checkpoint := source.quickSave()
			source.progress(second)
			source.press(testfixture.KTFNativeSaveKeySave)
			before, attempts := store.snapshot(t), store.attempts

			once := source.quickLoad(checkpoint)
			twice := once.quickLoad(checkpoint)
			thrice := twice.quickLoad(checkpoint)
			if after := store.snapshot(t); !reflect.DeepEqual(after, before) || store.attempts != attempts {
				t.Fatalf("three loads in a row changed the saves: %d writes, %+v", store.attempts-attempts, after)
			}
			if got := thrice.read(); got != nativeSaveFound(second, second) {
				t.Fatalf("READ after three loads = %+v", got)
			}
		})
	}
}

// A quick save gives the store what the title has written and the store has
// not been given, so its record carries no write: an open file is a name, a
// cursor and two flags.
func TestNativeQuickSaveStoresPendingWritesAndRecordsNoFileBytes(t *testing.T) {
	const first, second = 0x11111111, 0x22222222
	for _, form := range nativeSaveFixtureForms {
		t.Run(form.name, func(t *testing.T) {
			fixture := startNativeSaveFixture(t, form.build, newNativeSaveFixtureStore(t))
			fixture.progress(first)
			fixture.press(testfixture.KTFNativeSaveKeySave)
			fixture.progress(second)
			fixture.press(testfixture.KTFNativeSaveKeyHold)
			fixture.wantStored("after HOLD", testfixture.KTFNativeSaveFile(first, first))
			fixture.wantPending("after HOLD", testfixture.KTFNativeSaveFile(second, first))

			checkpoint := fixture.quickSave()
			fixture.wantStored("after the quick save", testfixture.KTFNativeSaveFile(second, first))
			fixture.wantPending("after the quick save", nil)

			var record nativeState
			if err := backend.DecodeCheckpointRecord(checkpoint.Runtime, &record); err != nil {
				t.Fatal(err)
			}
			want := []nativeFileState{{Object: fixture.word(testfixture.KTFNativeSaveHandle), Name: []byte(testfixture.KTFNativeSaveName),
				Key: []byte(testfixture.KTFNativeSaveName), Position: 4, Writable: true}}
			if record.Version != nativeStateVersion || !reflect.DeepEqual(record.Files, want) {
				t.Fatalf("the record holds version %d and files %+v, want %+v", record.Version, record.Files, want)
			}
			var members map[string]json.RawMessage
			if err := json.Unmarshal(checkpoint.Runtime, &members); err != nil {
				t.Fatal(err)
			}
			for _, member := range []string{"Buffers", "Written", "Resources"} {
				if _, carried := members[member]; carried {
					t.Fatalf("the record carries %s", member)
				}
			}
		})
	}
}

// An emptying open is a write the title issued, so a quick save stores the
// emptied file. The object it kept is recorded as holding nothing.
func TestNativeQuickSaveStoresAnEmptyingOpen(t *testing.T) {
	const first = 0x11111111
	for _, form := range nativeSaveFixtureForms {
		t.Run(form.name, func(t *testing.T) {
			fixture := startNativeSaveFixture(t, form.build, newNativeSaveFixtureStore(t))
			fixture.progress(first)
			fixture.press(testfixture.KTFNativeSaveKeySave)
			fixture.press(testfixture.KTFNativeSaveKeyTruncate)
			fixture.wantStored("after TRUNCATE", testfixture.KTFNativeSaveFile(first, first))
			checkpoint := fixture.quickSave()
			fixture.wantStored("after the quick save", []byte{})
			var record nativeState
			if err := backend.DecodeCheckpointRecord(checkpoint.Runtime, &record); err != nil {
				t.Fatal(err)
			}
			if len(record.Files) != 1 || !record.Files[0].Truncated || record.Files[0].Length != 0 || record.Files[0].Position != 0 {
				t.Fatalf("the emptied object was recorded as %+v", record.Files)
			}
		})
	}
}

// A store that will not take the title's write refuses the quick save, and
// the write is still where it was: the session runs on, the next ordinary
// boundary makes the attempt it would have made, and a later quick save
// stores what an ordinary boundary was refused.
func TestNativeQuickSaveIsRefusedWhenAWriteCannotBeStored(t *testing.T) {
	const first, second = 0x11111111, 0x22222222
	for _, form := range nativeSaveFixtureForms {
		t.Run(form.name, func(t *testing.T) {
			fixture := startNativeSaveFixture(t, form.build, newNativeSaveFixtureStore(t))
			store := fixture.store
			fixture.progress(first)
			fixture.press(testfixture.KTFNativeSaveKeySave)
			fixture.progress(second)
			fixture.press(testfixture.KTFNativeSaveKeyHold)

			store.refuse = true
			attempts := store.attempts
			_, err := fixture.session.CaptureCheckpoint(t.Context())
			if !errors.Is(err, backend.ErrCheckpointSaveWrite) || !strings.Contains(err.Error(), testfixture.KTFNativeSaveName) {
				t.Fatalf("quick save over a store that refuses = %v", err)
			}
			if store.attempts != attempts+1 {
				t.Fatalf("the refused quick save asked the store %d times, want once", store.attempts-attempts)
			}
			fixture.wantStored("after the refused quick save", testfixture.KTFNativeSaveFile(first, first))
			fixture.wantPending("after the refused quick save", testfixture.KTFNativeSaveFile(second, first))
			fixture.wantRefused("after the refused quick save", false)

			// The ordinary boundary still asks once, and is refused in its turn.
			if form.frame {
				fixture.frame()
			} else {
				fixture.read()
			}
			if store.attempts != attempts+2 || fixture.session.platform.StoreFailures() != 2 {
				t.Fatalf("the ordinary boundary asked the store %d more times, %d failures counted", store.attempts-attempts-1, fixture.session.platform.StoreFailures())
			}
			fixture.wantRefused("after the ordinary boundary was refused", true)
			fixture.wantPending("after the ordinary boundary was refused", testfixture.KTFNativeSaveFile(second, first))

			store.refuse = false
			fixture.quickSave()
			fixture.wantStored("after the quick save the store accepted", testfixture.KTFNativeSaveFile(second, first))
			fixture.wantPending("after the quick save the store accepted", nil)
			if fixture.word(testfixture.KTFNativeSaveStartupCounter) != 1 || fixture.session.Client == nil {
				t.Fatal("the refusals disturbed the session")
			}
		})
	}
}

// What can be refused without asking the store is refused before the store is
// given anything: a quick save that is not going to happen stores nothing.
func TestNativeQuickSaveRefusalsStoreNothing(t *testing.T) {
	const first, second = 0x11111111, 0x22222222
	fixture := startNativeSaveFixture(t, testfixture.KTFNativeSaveArchiveWithoutFrame, newNativeSaveFixtureStore(t))
	store, session := fixture.store, fixture.session
	fixture.progress(first)
	fixture.press(testfixture.KTFNativeSaveKeySave)
	fixture.progress(second)
	fixture.press(testfixture.KTFNativeSaveKeyHold)
	attempts := store.attempts
	refused := func(why string, capture func() error) {
		t.Helper()
		if err := capture(); err == nil {
			t.Fatalf("%s: the quick save was accepted", why)
		}
		if store.attempts != attempts {
			t.Fatalf("%s: the refused quick save wrote to the store", why)
		}
		fixture.wantPending(why, testfixture.KTFNativeSaveFile(second, first))
	}
	capture := func() error {
		_, err := session.CaptureCheckpoint(t.Context())
		return err
	}
	cancelled, cancel := context.WithCancel(t.Context())
	cancel()
	refused("a cancelled request", func() error {
		_, err := session.CaptureCheckpoint(cancelled)
		return err
	})
	session.run.Lock()
	refused("a busy session", capture)
	session.run.Unlock()
	session.Cheat().Freezes().Insert(cheat.FreezeEntry{Address: testfixture.KTFNativeSaveProgress, Value: 1})
	refused("a frozen value", capture)
	session.Cheat().Freezes().Clear()
	session.Client.tracing = true
	refused("a traced client", capture)
	session.Client.tracing = false
	// The open object holds more than a load would read back.
	held := session.platform.files[fixture.word(testfixture.KTFNativeSaveHandle)]
	kept := held.data
	held.data = make([]byte, nativeStateStorageLimit+1)
	refused("open files past the limit", capture)
	held.data = kept

	// A capture that fails after the store was given the write leaves the
	// write stored: it was the title's, and the next boundary would have
	// stored it anyway. The session runs on and the next quick save works.
	failure := errors.New("the shared record could not be taken")
	if _, err := session.CaptureCheckpointWithSession(t.Context(), func() ([]byte, error) { return nil, failure }); !errors.Is(err, failure) {
		t.Fatalf("a capture whose shared record fails = %v", err)
	}
	fixture.wantStored("after the capture that failed late", testfixture.KTFNativeSaveFile(second, first))
	fixture.wantPending("after the capture that failed late", nil)
	fixture.quickSave()

	// A session with no store has nowhere a load could find its saves.
	bare, err := StartNativeSession(t.Context(), fixture.archive, NativeSessionOptions{Clock: NewManualClock(time.Unix(1, 0))})
	if err != nil {
		t.Fatal(err)
	}
	defer bare.Close()
	if _, err := bare.CaptureCheckpoint(t.Context()); err == nil || !strings.Contains(err.Error(), "save store") {
		t.Fatalf("a session with no save store = %v", err)
	}
}

// A quick load displaces the running session, so what that session's title
// has written and the store has not been given is stored first. It is what
// the restored title then reads.
func TestNativeQuickLoadStoresTheDisplacedSessionsPendingWrites(t *testing.T) {
	const first, second = 0x11111111, 0x22222222
	for _, form := range nativeSaveFixtureForms {
		t.Run(form.name, func(t *testing.T) {
			source := startNativeSaveFixture(t, form.build, newNativeSaveFixtureStore(t))
			store := source.store
			source.progress(first)
			source.press(testfixture.KTFNativeSaveKeySave)
			checkpoint := source.quickSave()
			source.progress(second)
			source.press(testfixture.KTFNativeSaveKeyHold)
			source.wantStored("before the load", testfixture.KTFNativeSaveFile(first, first))
			source.wantPending("before the load", testfixture.KTFNativeSaveFile(second, first))

			restored := source.quickLoad(checkpoint)
			restored.wantStored("after the load", testfixture.KTFNativeSaveFile(second, first))
			if got := restored.read(); got != nativeSaveFound(second, first) {
				t.Fatalf("READ after the load = %+v, want what the displaced session wrote %+v", got, nativeSaveFound(second, first))
			}
			attempts := store.attempts
			source.session.Close()
			if store.attempts != attempts {
				t.Fatal("closing the displaced session wrote to the store again")
			}
		})
	}
}

// A store that will not take the displaced session's write refuses the load.
// The running session is the one that still runs, with its write still
// waiting, and the next ordinary boundary asks the store as it would have.
func TestNativeQuickLoadIsRefusedWhenDisplacedWritesCannotBeStored(t *testing.T) {
	const first, second = 0x11111111, 0x22222222
	for _, form := range nativeSaveFixtureForms {
		t.Run(form.name, func(t *testing.T) {
			source := startNativeSaveFixture(t, form.build, newNativeSaveFixtureStore(t))
			store := source.store
			source.progress(first)
			source.press(testfixture.KTFNativeSaveKeySave)
			checkpoint := source.quickSave()
			source.progress(second)
			source.press(testfixture.KTFNativeSaveKeyHold)
			client, before := source.session.Client, store.snapshot(t)

			store.refuse = true
			if _, err := source.tryQuickLoad(checkpoint); !errors.Is(err, backend.ErrCheckpointSaveWrite) {
				t.Fatalf("quick load over a store that refuses = %v", err)
			}
			if source.session.Client != client || source.session.platform == nil || source.word(testfixture.KTFNativeSaveProgress) != second {
				t.Fatal("the refused load displaced the running session")
			}
			if after := store.snapshot(t); !reflect.DeepEqual(after, before) {
				t.Fatalf("the refused load changed the saves: %+v", after)
			}
			source.wantPending("after the refused load", testfixture.KTFNativeSaveFile(second, first))
			source.wantRefused("after the refused load", false)

			store.refuse = false
			restored := source.quickLoad(checkpoint)
			restored.wantStored("after the load the store accepted", testfixture.KTFNativeSaveFile(second, first))
			if restored.word(testfixture.KTFNativeSaveProgress) != first {
				t.Fatal("the accepted load did not bring the title back")
			}
		})
	}
}

// A refused quick step changes nothing about what the ordinary boundaries do:
// the next frame end, the next file close and the end of the session each
// still give the store the write, once.
func TestNativeOrdinaryBoundariesStillStoreAfterARefusedQuickStep(t *testing.T) {
	const first, second = 0x11111111, 0x22222222
	boundaries := []struct {
		name  string
		build func() ([]byte, error)
		reach func(*nativeSaveFixture)
	}{
		{"the end of a frame", testfixture.KTFNativeSaveArchive, func(fixture *nativeSaveFixture) { fixture.frame() }},
		{"a file close", testfixture.KTFNativeSaveArchiveWithoutFrame, func(fixture *nativeSaveFixture) { fixture.read() }},
		{"the end of the session", testfixture.KTFNativeSaveArchiveWithoutFrame, func(fixture *nativeSaveFixture) { fixture.session.Close() }},
	}
	for _, step := range []string{"quick save", "quick load"} {
		for _, boundary := range boundaries {
			t.Run(step+", then "+boundary.name, func(t *testing.T) {
				fixture := startNativeSaveFixture(t, boundary.build, newNativeSaveFixtureStore(t))
				store := fixture.store
				fixture.progress(first)
				fixture.press(testfixture.KTFNativeSaveKeySave)
				checkpoint := fixture.quickSave()
				fixture.progress(second)
				fixture.press(testfixture.KTFNativeSaveKeyHold)

				store.refuse = true
				var err error
				if step == "quick save" {
					_, err = fixture.session.CaptureCheckpoint(t.Context())
				} else {
					_, err = fixture.tryQuickLoad(checkpoint)
				}
				if !errors.Is(err, backend.ErrCheckpointSaveWrite) {
					t.Fatalf("the %s was not refused for the write: %v", step, err)
				}
				store.refuse = false

				attempts := store.attempts
				boundary.reach(fixture)
				if store.attempts != attempts+1 {
					t.Fatalf("%s asked the store %d times after the refused %s, want once", boundary.name, store.attempts-attempts, step)
				}
				fixture.wantStored("after "+boundary.name, testfixture.KTFNativeSaveFile(second, first))
			})
		}
	}
}

// The ordinary boundary asks the store once for a write and does not ask
// again at the next one, as it never did. What it was refused is not
// forgotten: the title still reads it back, a later write of the title's
// replaces it, and the end of the session asks once more.
func TestNativeOrdinaryFlushAsksOnceAndTheSessionEndAsksAgain(t *testing.T) {
	const first, second, third = 0x11111111, 0x22222222, 0x33333333
	fixture := startNativeSaveFixture(t, testfixture.KTFNativeSaveArchive, newNativeSaveFixtureStore(t))
	store, platform := fixture.store, fixture.session.platform
	fixture.progress(first)
	fixture.press(testfixture.KTFNativeSaveKeySave)
	fixture.progress(second)
	fixture.press(testfixture.KTFNativeSaveKeyHold)

	store.refuse = true
	attempts := store.attempts
	fixture.frame()
	if store.attempts != attempts+1 || platform.StoreFailures() != 1 {
		t.Fatalf("the frame end asked %d times and counted %d failures, want one of each", store.attempts-attempts, platform.StoreFailures())
	}
	fixture.wantRefused("after the frame end was refused", true)
	for range 3 {
		fixture.frame()
	}
	if got := fixture.read(); got != nativeSaveFound(second, first) {
		t.Fatalf("READ of a write the store refused = %+v, want the session's own copy", got)
	}
	if store.attempts != attempts+1 {
		t.Fatalf("later frames and a file close asked the store %d more times, want none", store.attempts-attempts-1)
	}
	fixture.wantStored("while the store refuses", testfixture.KTFNativeSaveFile(first, first))

	// The end of the session asks once more, for a store that still refuses
	// and for one that has recovered.
	t.Run("the session end asks again", func(t *testing.T) {
		other := startNativeSaveFixture(t, testfixture.KTFNativeSaveArchive, newNativeSaveFixtureStore(t))
		other.progress(first)
		other.press(testfixture.KTFNativeSaveKeyHold)
		other.store.refuse = true
		other.frame()
		other.store.refuse = false
		asked := other.store.attempts
		other.session.Close()
		if other.store.attempts != asked+1 {
			t.Fatalf("the session end asked %d times for the refused write, want once", other.store.attempts-asked)
		}
		other.wantStored("after the session ended", binary.LittleEndian.AppendUint32(nil, first))
	})

	// A later write of the title's is what the store is given next, whole.
	store.refuse = false
	fixture.progress(third)
	fixture.press(testfixture.KTFNativeSaveKeyFinish)
	if store.attempts != attempts+2 {
		t.Fatalf("FINISH asked the store %d times, want once", store.attempts-attempts-1)
	}
	fixture.wantStored("after FINISH", testfixture.KTFNativeSaveFile(second, third))
	fixture.wantPending("after FINISH", nil)
}

// A write an ordinary boundary was refused is stored by the next quick load
// as it is by the next quick save.
func TestNativeQuickLoadStoresAWriteAnOrdinaryBoundaryWasRefused(t *testing.T) {
	const first, second = 0x11111111, 0x22222222
	source := startNativeSaveFixture(t, testfixture.KTFNativeSaveArchive, newNativeSaveFixtureStore(t))
	source.progress(first)
	source.press(testfixture.KTFNativeSaveKeySave)
	checkpoint := source.quickSave()
	source.progress(second)
	source.press(testfixture.KTFNativeSaveKeyHold)
	source.store.refuse = true
	source.frame()
	source.wantRefused("after the frame end was refused", true)
	source.store.refuse = false

	restored := source.quickLoad(checkpoint)
	restored.wantStored("after the load", testfixture.KTFNativeSaveFile(second, first))
}

// A file the restored title has open that cannot be read refuses the load
// before anything is stored: the files are read first, and the displaced
// session's writes are given to the store only when the load can go on.
func TestNativeQuickLoadReadFailureStoresNothing(t *testing.T) {
	const first, second, third, fourth = 0x11111111, 0x22222222, 0x33333333, 0x44444444
	for _, form := range nativeSaveFixtureForms {
		t.Run(form.name, func(t *testing.T) {
			source := startNativeSaveFixture(t, form.build, newNativeSaveFixtureStore(t))
			store := source.store
			source.progress(first)
			source.press(testfixture.KTFNativeSaveKeySave)
			source.progress(second)
			source.press(testfixture.KTFNativeSaveKeyHold)
			// The checkpoint holds the kept object, so a load reads its file.
			checkpoint := source.quickSave()
			source.progress(third)
			source.press(testfixture.KTFNativeSaveKeyFinish)
			source.progress(fourth)
			source.press(testfixture.KTFNativeSaveKeyHold)
			source.wantPending("before the load", testfixture.KTFNativeSaveFile(fourth, third))
			client, before, attempts := source.session.Client, store.snapshot(t), store.attempts

			store.unreadable = testfixture.KTFNativeSaveStoreKey
			_, err := source.tryQuickLoad(checkpoint)
			store.unreadable = ""
			if !errors.Is(err, backend.ErrCheckpointSaveRead) || !strings.Contains(err.Error(), testfixture.KTFNativeSaveName) {
				t.Fatalf("quick load over a save that cannot be read = %v", err)
			}
			if after := store.snapshot(t); !reflect.DeepEqual(after, before) || store.attempts != attempts {
				t.Fatalf("the refused load wrote to the store: %d writes, %+v", store.attempts-attempts, after)
			}
			if source.session.Client != client {
				t.Fatal("the refused load displaced the running session")
			}
			source.wantPending("after the refused load", testfixture.KTFNativeSaveFile(fourth, third))

			// The same load is accepted once the save can be read, and then
			// the displaced session's write is stored and is what the
			// restored object holds.
			restored := source.quickLoad(checkpoint)
			restored.wantStored("after the accepted load", testfixture.KTFNativeSaveFile(fourth, third))
			handle := restored.session.platform.files[restored.word(testfixture.KTFNativeSaveHandle)]
			if handle == nil || handle.position != 4 || !bytes.Equal(handle.data, testfixture.KTFNativeSaveFile(fourth, third)) {
				t.Fatalf("the restored object holds %+v, want the file as the store has it at the recorded cursor", handle)
			}
		})
	}
}

// On a directory store a file can have become a directory behind a slot. The
// load is refused for the read, and the tree is what it was.
func TestNativeQuickLoadRefusesADirectoryWhereAFileWas(t *testing.T) {
	const first, second = 0x11111111, 0x22222222
	archive, err := testfixture.KTFNativeSaveArchiveWithoutFrame()
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(t.TempDir(), testfixture.KTFNativeSaveOwner)
	store := backend.NewDirectorySaveStore(root)
	session, err := StartNativeSession(t.Context(), archive, NativeSessionOptions{Clock: NewManualClock(time.Unix(1, 0)), SaveStore: store})
	if err != nil {
		t.Fatal(err)
	}
	fixture := &nativeSaveFixture{t: t, archive: archive, session: session}
	fixture.progress(first)
	fixture.press(testfixture.KTFNativeSaveKeySave)
	fixture.progress(second)
	fixture.press(testfixture.KTFNativeSaveKeyHold)
	checkpoint := fixture.quickSave()
	session.Close()

	file := filepath.Join(root, "fs", testfixture.KTFNativeSaveName)
	if data, err := os.ReadFile(file); err != nil || !bytes.Equal(data, testfixture.KTFNativeSaveFile(second, first)) {
		t.Fatalf("the quick save left %x in the save file, %v", data, err)
	}
	if err := os.Remove(file); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(file, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(file, "below"), []byte("kept"), 0o644); err != nil {
		t.Fatal(err)
	}
	prepared, err := PrepareNativeSessionCheckpoint(archive, checkpoint, NativeSessionOptions{Clock: NewManualClock(time.Unix(500, 0))})
	if err != nil {
		t.Fatal(err)
	}
	defer prepared.Discard()
	if _, err := prepared.Commit(t.Context(), nil, store); !errors.Is(err, backend.ErrCheckpointSaveRead) {
		t.Fatalf("a load over a directory where the save was = %v", err)
	}
	if data, err := os.ReadFile(filepath.Join(file, "below")); err != nil || string(data) != "kept" {
		t.Fatalf("the refused load changed the tree: %q, %v", data, err)
	}
}

// An object whose open asked for an empty file holds only what the title
// wrote through it. A load gives it back at most that much, from the front of
// the file as it is now, so the file the restored title is rewriting is never
// followed by the tail of a newer one.
//
// An object whose open made a file that was not there asked for no such thing,
// and is given the file as it is, like any other: what the title saved through
// it after the quick save is still there after the restored title's next write.
func TestNativeQuickLoadNeverHandsAnEmptiedObjectANewerTail(t *testing.T) {
	const first, second, third = 0x11111111, 0x22222222, 0x33333333
	longer := []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12}
	behind := func(t *testing.T, store *nativeSaveFixtureStore, data []byte) {
		t.Helper()
		if err := store.MemorySaveStore.StoreSave(testfixture.KTFNativeSaveStoreKey, data); err != nil {
			t.Fatal(err)
		}
	}
	for _, form := range nativeSaveFixtureForms {
		t.Run(form.name+", emptied and not yet written", func(t *testing.T) {
			source := startNativeSaveFixture(t, form.build, newNativeSaveFixtureStore(t))
			store := source.store
			source.progress(first)
			source.press(testfixture.KTFNativeSaveKeySave)
			source.press(testfixture.KTFNativeSaveKeyTruncate)
			checkpoint := source.quickSave()
			source.wantStored("after the quick save", []byte{})
			// The running title finishes its save, and a longer one is saved
			// after it.
			source.progress(second)
			source.press(testfixture.KTFNativeSaveKeyFinish)
			behind(t, store, longer)
			before, attempts := store.snapshot(t), store.attempts

			restored := source.quickLoad(checkpoint)
			if after := store.snapshot(t); !reflect.DeepEqual(after, before) || store.attempts != attempts {
				t.Fatal("the load emptied the save again or wrote to the store")
			}
			handle := restored.session.platform.files[restored.word(testfixture.KTFNativeSaveHandle)]
			if handle == nil || !handle.truncated || len(handle.data) != 0 || handle.position != 0 {
				t.Fatalf("the emptied object came back as %+v, want it empty", handle)
			}
			restored.progress(third)
			restored.press(testfixture.KTFNativeSaveKeyFinish)
			restored.wantStored("after the restored title finished", binary.LittleEndian.AppendUint32(nil, third^testfixture.KTFNativeSaveMask))
		})
		t.Run(form.name+", made by its open and part written", func(t *testing.T) {
			source := startNativeSaveFixture(t, form.build, newNativeSaveFixtureStore(t))
			store := source.store
			// No save yet: HOLD's open makes the file and writes part A.
			source.progress(first)
			source.press(testfixture.KTFNativeSaveKeyHold)
			checkpoint := source.quickSave()
			source.wantStored("after the quick save", binary.LittleEndian.AppendUint32(nil, first))
			source.progress(second)
			source.press(testfixture.KTFNativeSaveKeyFinish)
			behind(t, store, longer)

			restored := source.quickLoad(checkpoint)
			handle := restored.session.platform.files[restored.word(testfixture.KTFNativeSaveHandle)]
			if handle == nil || handle.truncated || !bytes.Equal(handle.data, longer) || handle.position != 4 {
				t.Fatalf("the object came back as %+v, want the whole file as it is now and the position it had", handle)
			}
			// Part B lands where the position is, and what the later save holds
			// behind it is still there.
			restored.progress(third)
			restored.press(testfixture.KTFNativeSaveKeyFinish)
			want := append(binary.LittleEndian.AppendUint32(bytes.Clone(longer[:4]), third^testfixture.KTFNativeSaveMask), longer[8:]...)
			restored.wantStored("after the restored title finished", want)
		})
	}
}

// An open file is the file as the store has it when the load commits, found
// the way a first open finds it, with the cursor where the record has it.
func TestNativeQuickLoadReopensFilesFromTheLiveStore(t *testing.T) {
	archive, err := testfixture.KTFNativeCheckpointArchive()
	if err != nil {
		t.Fatal(err)
	}
	original := []byte("0123456789")
	store := newNativeSaveFixtureStore(t,
		backend.SaveEntry{Key: "fs/grown.dat", Data: original},
		backend.SaveEntry{Key: "fs/shrunk.dat", Data: original},
		backend.SaveEntry{Key: "fs/gone.dat", Data: original},
		backend.SaveEntry{Key: "fs/twice.dat", Data: original},
	)
	source, err := StartNativeSession(t.Context(), archive, NativeSessionOptions{Clock: NewManualClock(time.Unix(1, 0)), SaveStore: store, Width: 32, Height: 48})
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	p := source.platform
	open := func(name string, mode uint32) uint32 {
		t.Helper()
		object := nativeCall(t, p.openFile, 0, nativeString(t, p, name), mode)
		if object == 0 {
			t.Fatalf("open %q was refused", name)
		}
		return object
	}
	grown, shrunk, gone := open("grown.dat", 2), open("Dir\\Shrunk.dat", 2), open("gone.dat", 2)
	for _, object := range []uint32{grown, shrunk, gone} {
		nativeCall(t, p.seekFile, object, nativeSeekStart, 6)
	}
	writer, reader := open("twice.dat", 2), open("twice.dat", nativeModeRead)

	checkpoint, err := source.CaptureCheckpoint(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	// Behind the session the saves change, the way a save made after the
	// quick save, or an import, changes them.
	if err := store.ReplaceSaves([]backend.SaveEntry{
		{Key: "fs/grown.dat", Data: []byte("0123456789ABCDEF")},
		{Key: "fs/shrunk.dat", Data: []byte("0123")},
		{Key: "fs/twice.dat", Data: []byte("abcdefgh")},
	}); err != nil {
		t.Fatal(err)
	}
	before, attempts := store.snapshot(t), store.attempts

	prepared, err := PrepareNativeSessionCheckpoint(archive, checkpoint, NativeSessionOptions{Clock: NewManualClock(time.Unix(500, 0))})
	if err != nil {
		t.Fatal(err)
	}
	defer prepared.Discard()
	restored, err := prepared.Commit(t.Context(), source, store)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	if after := store.snapshot(t); !reflect.DeepEqual(after, before) || store.attempts != attempts {
		t.Fatalf("the load wrote to the store or made a file: %+v", after)
	}
	r := restored.platform
	buffer, err := restored.Client.Allocate(32)
	if err != nil {
		t.Fatal(err)
	}
	read := func(object, length uint32) string {
		t.Helper()
		moved := nativeCall(t, r.readFile, object, buffer, length)
		return string(nativeRead(t, r, buffer, int(moved)))
	}
	length := func(object uint32) uint32 {
		t.Helper()
		nativeCall(t, r.fileStatus, object, buffer)
		return binary.LittleEndian.Uint32(nativeRead(t, r, buffer, nativeFileRecordSize)[nativeFileLengthOffset:])
	}
	write := func(object uint32, text string) {
		t.Helper()
		if err := restored.Client.core.Memory().Write(buffer, []byte(text)); err != nil {
			t.Fatal(err)
		}
		if moved := nativeCall(t, r.writeFile, object, buffer, uint32(len(text))); moved != uint32(len(text)) {
			t.Fatalf("wrote %d of %d bytes", moved, len(text))
		}
	}

	// A file that grew: the cursor is where it was, and reading goes on into
	// what the file holds now.
	if length(grown) != 16 || read(grown, 8) != "6789ABCD" {
		t.Fatal("the grown file was not read as it is now from the recorded cursor")
	}
	// A file that shrank below the cursor: the cursor stays, a read there
	// answers nothing, and a write fills the gap with zeros.
	if length(shrunk) != 4 || r.files[shrunk].position != 6 || read(shrunk, 4) != "" {
		t.Fatalf("the shrunk file came back as %+v", r.files[shrunk])
	}
	write(shrunk, "XY")
	if got := string(r.written["shrunk.dat"]); got != "0123\x00\x00XY" {
		t.Fatalf("a write past the end of the shrunk file left %q", got)
	}
	// A file that is gone: an empty object at its cursor, and nothing made.
	if length(gone) != 0 || r.files[gone].position != 6 || read(gone, 4) != "" {
		t.Fatalf("the vanished file came back as %+v", r.files[gone])
	}
	if _, found := store.LoadSave("fs/gone.dat"); found {
		t.Fatal("the load made the file that was gone")
	}
	// Two objects on one file: each has the file as it is now, in a copy of
	// its own, and a write through one leaves the other alone.
	if length(writer) != 8 || length(reader) != 8 || &r.files[writer].data[0] == &r.files[reader].data[0] {
		t.Fatal("two objects on one file do not each hold their own copy of it")
	}
	write(writer, "ZZ")
	if read(reader, 8) != "abcdefgh" || string(r.written["twice.dat"]) != "ZZcdefgh" {
		t.Fatal("a write through one object reached the other")
	}
}

// What a load reads is bounded: the names come from a slot, and every object
// counts towards one limit, in a copy of its own.
func TestNativeReopenFilesChargesEveryObjectAgainstOneLimit(t *testing.T) {
	store, err := backend.NewMemorySaveStore([]backend.SaveEntry{
		{Key: "fs/a.dat", Data: []byte("aaaaaa")},
		{Key: "fs/b.dat", Data: []byte("bbbbbb")},
	})
	if err != nil {
		t.Fatal(err)
	}
	platform := newTestNativePlatform(t, map[string][]byte{"title/shipped.dat": []byte("shipped!")})
	file := func(object uint32, name string) nativeFileState {
		return nativeFileState{Object: object, Name: []byte(name), Key: []byte(nativeFileKey(name))}
	}
	records := []nativeFileState{file(0x10, "a.dat"), file(0x14, "a.dat"), file(0x18, "b.dat")}
	buffers, err := platform.reopenFiles(store, records, 18)
	if err != nil {
		t.Fatal(err)
	}
	if string(buffers[0x10]) != "aaaaaa" || string(buffers[0x14]) != "aaaaaa" || string(buffers[0x18]) != "bbbbbb" || &buffers[0x10][0] == &buffers[0x14][0] {
		t.Fatalf("the objects were read as %q", buffers)
	}
	for _, refusal := range []struct {
		name    string
		records []nativeFileState
		limit   int64
	}{
		{"the objects together", records, 17},
		{"one entry", records[:1], 5},
		{"a shipped file", []nativeFileState{file(0x10, "shipped.dat")}, 7},
		// An emptied object keeps a part of its entry, and the whole entry is
		// still read on the limit.
		{"an emptied object's entry", []nativeFileState{{Object: 0x10, Name: []byte("a.dat"), Key: []byte("a.dat"), Truncated: true, Length: 2}}, 5},
		// What is read counts once per key, beside what is kept.
		{"two entries read for one kept", []nativeFileState{
			{Object: 0x10, Name: []byte("a.dat"), Key: []byte("a.dat"), Truncated: true, Length: 2}, file(0x14, "b.dat")}, 11},
	} {
		if _, err := platform.reopenFiles(store, refusal.records, refusal.limit); !errors.Is(err, backend.ErrCheckpointSaveRead) {
			t.Errorf("%s over the limit = %v", refusal.name, err)
		}
	}
	kept, err := platform.reopenFiles(store, []nativeFileState{
		{Object: 0x10, Name: []byte("a.dat"), Key: []byte("a.dat"), Truncated: true, Length: 2},
		file(0x14, "b.dat"),
	}, 12)
	if err != nil || string(kept[0x10]) != "aa" || string(kept[0x14]) != "bbbbbb" {
		t.Fatalf("an emptied object beside another = %q, %v", kept, err)
	}
	// A slot can name one file many times over, under names that differ and
	// under an emptied object that keeps none of it. The file is read once,
	// and an object that had written nothing does not read it at all.
	counted := &countedSaveReads{SaveStore: store}
	many := []nativeFileState{{Object: 0x0c, Name: []byte("z/a.dat"), Key: []byte("a.dat"), Truncated: true}}
	for index := range 64 {
		name := fmt.Sprintf("%d/A.dat", index)
		many = append(many, nativeFileState{Object: uint32(0x10 + 4*index), Name: []byte(name), Key: []byte(nativeFileKey(name)), Truncated: true, Length: 1})
	}
	if found, err := platform.reopenFiles(counted, many, 64); err != nil || counted.reads != 1 || len(found[0x0c]) != 0 || string(found[0x10]) != "a" {
		t.Fatalf("sixty-five objects on one file made %d store reads: %v", counted.reads, err)
	}
	if found, err := platform.reopenFiles(counted, many[:1], 0); err != nil || counted.reads != 1 || len(found[0x0c]) != 0 {
		t.Fatalf("an object that had written nothing read its file: %d reads, %v", counted.reads, err)
	}
	// A name the store has no entry for is the file the package ships, found
	// the way an open finds it, and a name the store has an entry for is that
	// entry.
	shipped := []nativeFileState{file(0x10, "Other\\Shipped.DAT")}
	if found, err := platform.reopenFiles(store, shipped, 64); err != nil || string(found[0x10]) != "shipped!" {
		t.Fatalf("a shipped file with no save = %q, %v", found, err)
	}
	if err := store.StoreSave("fs/shipped.dat", []byte("saved")); err != nil {
		t.Fatal(err)
	}
	if found, err := platform.reopenFiles(store, shipped, 64); err != nil || string(found[0x10]) != "saved" {
		t.Fatalf("a shipped file with a save over it = %q, %v", found, err)
	}
	if len(platform.written) != 0 || len(platform.unsaved) != 0 {
		t.Fatal("reading the files changed the platform")
	}
}

// countedSaveReads counts the reads that reach a store.
type countedSaveReads struct {
	SaveStore
	reads int
}

func (counted *countedSaveReads) LoadSave(name string) ([]byte, bool) {
	counted.reads++
	return counted.SaveStore.LoadSave(name)
}

func (counted *countedSaveReads) ReadSave(name string) ([]byte, bool, error) {
	counted.reads++
	return backend.ReadSave(counted.SaveStore, name)
}

// One name is one file, whichever separator and case the module wrote it
// with: the session's own copy, the store's entry and a later open agree.
func TestNativeFileNameIsOneFileWhicheverWayItIsWritten(t *testing.T) {
	store := memorySaveStore{}
	platform := newTestNativePlatform(t, nil)
	platform.AttachSaves(store)
	file := nativeCall(t, platform.openFile, 0, nativeString(t, platform, "Saves\\Slot.DAT"), 2)
	if file == 0 {
		t.Fatal("the open was refused")
	}
	if moved := nativeCall(t, platform.writeFile, file, nativeString(t, platform, "written"), 7); moved != 7 {
		t.Fatalf("wrote %d bytes", moved)
	}
	nativeCall(t, platform.closeFile, file)
	if got, found := store["fs/slot.dat"]; !found || string(got) != "written" || len(store) != 1 || platform.StoreFailures() != 0 {
		t.Fatalf("the store holds %v after %d refusals", store, platform.StoreFailures())
	}
	for _, name := range []string{"saves/slot.dat", "SLOT.DAT", "Saves\\Slot.DAT"} {
		if data, found, err := platform.contents(name); err != nil || !found || string(data) != "written" {
			t.Errorf("%q reads %q, %t, %v", name, data, found, err)
		}
	}
}

// Two entries of a package can share a base name. Which one a name finds does
// not change from one open to the next, or from one session to the next.
func TestNativePackagedFileWithASharedBaseNameIsAlwaysTheSameEntry(t *testing.T) {
	files := map[string][]byte{}
	for _, directory := range []string{"m", "c", "x", "a", "k", "t", "e", "q"} {
		files[directory+"/Data.BIN"] = []byte(directory)
	}
	for attempt := range 20 {
		platform := newTestNativePlatform(t, files)
		data, found, err := platform.contents("data.bin")
		if err != nil || !found || string(data) != "a" {
			t.Fatalf("attempt %d found %q, %t, %v; want the entry that sorts first", attempt, data, found, err)
		}
		// The name as an entry spells it is that entry.
		if exact, found, _ := platform.contents("k/Data.BIN"); !found || string(exact) != "k" {
			t.Fatalf("an entry's own name found %q", exact)
		}
	}
}
