package ktf

import (
	"context"
	"encoding/binary"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/testfixture"
)

// A quick load brings back the title and leaves its saves alone. These drive
// the two authored storage archives with keys — the Java/AOT title and the
// older descriptor module — through a quick save and a quick load, and look at
// the save store around each step. The save is a File; the other storage
// tables are covered where their host state can be planted, in
// storage_rebind_test.go.

// The scenario the rule exists for: quick save, the title saves, quick load.
// The save made after the quick save is still the save, the load and the
// rounds after it write nothing, the next read is the save as it is and the
// next write changes only what it writes.
func TestSessionQuickLoadLeavesTheSaveMadeAfterTheQuickSave(t *testing.T) {
	const first, second, third = 0x11111111, 0x22222222, 0x33333333
	for _, variant := range saveFixtureVariants() {
		t.Run(variant.name, func(t *testing.T) {
			fixture := startSaveFixture(t, variant)
			fixture.press(testfixture.KTFSaveActionSave, first)
			checkpoint := fixture.quickSave()

			fixture.press(testfixture.KTFSaveActionSave, second)
			fixture.expectStored("after the save made after the quick save", testfixture.KTFSaveContent(second))
			before, writes, source := fixture.snapshot(), fixture.probe.writes, fixture.session

			fixture.quickLoad(checkpoint)
			if fixture.session == source || source.Client != nil {
				t.Fatal("the load did not displace the running session")
			}
			if fixture.word(variant.progress) != first {
				t.Fatal("the load did not bring the title back to the quick save")
			}
			for range 5 {
				if _, err := fixture.session.Tick(t.Context()); err != nil {
					t.Fatal(err)
				}
			}
			if after := fixture.snapshot(); !reflect.DeepEqual(after, before) || fixture.probe.writes != writes {
				t.Fatalf("the load or the rounds after it changed the saves: %d writes, %+v", fixture.probe.writes-writes, after)
			}
			// The title reads the save as it is now, not as it was at the
			// quick save.
			fixture.press(testfixture.KTFSaveActionRead, 0)
			fixture.expectRead("a read after the load", testfixture.KTFSaveFound, 8, second, second^testfixture.KTFSaveMask)
			// And a write of four bytes changes four bytes of it.
			fixture.press(testfixture.KTFSaveActionPatch, third)
			fixture.expectStored("after the restored title patched its save", saveParts(third, second))

			// Loading again, and again, is loading once.
			before, writes = fixture.snapshot(), fixture.probe.writes
			fixture.quickLoad(checkpoint)
			fixture.quickLoad(checkpoint)
			if after := fixture.snapshot(); !reflect.DeepEqual(after, before) || fixture.probe.writes != writes {
				t.Fatalf("two more loads changed the saves: %+v", after)
			}
			fixture.press(testfixture.KTFSaveActionRead, 0)
			fixture.expectRead("a read after three loads", testfixture.KTFSaveFound, 8, third, second^testfixture.KTFSaveMask)
		})
	}
}

// A File the title kept open across the quick save is the file as the store
// has it after a load, at the cursor it had: what the title writes next lands
// on top of the save that is there now.
func TestSessionQuickLoadGivesAKeptFileTheSaveAsItIs(t *testing.T) {
	const first, second, third, fourth, fifth = 0x11111111, 0x22222222, 0x33333333, 0x44444444, 0x55555555
	for _, variant := range saveFixtureVariants() {
		t.Run(variant.name, func(t *testing.T) {
			fixture := startSaveFixture(t, variant)
			fixture.press(testfixture.KTFSaveActionSave, first)
			fixture.press(testfixture.KTFSaveActionHold, second)
			fixture.expectStored("after the hold", saveParts(second, first))
			kept := fixture.word(variant.kept)
			checkpoint := fixture.quickSave()

			// The running title finishes that save and makes another.
			fixture.press(testfixture.KTFSaveActionFinish, third)
			fixture.press(testfixture.KTFSaveActionSave, fourth)
			before := fixture.snapshot()

			fixture.quickLoad(checkpoint)
			if got := fixture.word(variant.kept); got != kept {
				t.Fatalf("the restored title keeps File %#x, want %#x", got, kept)
			}
			if after := fixture.snapshot(); !reflect.DeepEqual(after, before) {
				t.Fatalf("the load changed the saves: %+v", after)
			}
			// Part B goes where the kept cursor stands, in the save as it is.
			fixture.press(testfixture.KTFSaveActionFinish, fifth)
			fixture.expectStored("after the restored title finished", saveParts(fourth, fifth))
		})
	}
}

// A File opened to be rewritten holds only what the title wrote through it. A
// load gives it back at most that much, so what the restored title writes is
// never followed by the tail of a save it did not write; and the load itself
// does not carry out the emptying, which is the title's to do or not.
func TestSessionQuickLoadNeverHandsAnEmptiedFileANewerTail(t *testing.T) {
	const first, second, third, fourth = 0x11111111, 0x22222222, 0x33333333, 0x44444444
	partB := func(progress uint32) []byte {
		return binary.LittleEndian.AppendUint32(nil, progress^testfixture.KTFSaveMask)
	}
	for _, variant := range saveFixtureVariants() {
		t.Run(variant.name, func(t *testing.T) {
			fixture := startSaveFixture(t, variant)
			fixture.press(testfixture.KTFSaveActionSave, first)
			fixture.press(testfixture.KTFSaveActionTruncate, 0)
			// Opening to rewrite stores nothing, and neither does the quick save.
			checkpoint := fixture.quickSave()
			fixture.expectStored("after the quick save", testfixture.KTFSaveContent(first))

			fixture.press(testfixture.KTFSaveActionFinish, second)
			fixture.expectStored("after the running title finished", partB(second))
			fixture.press(testfixture.KTFSaveActionSave, third)
			before := fixture.snapshot()

			fixture.quickLoad(checkpoint)
			if after := fixture.snapshot(); !reflect.DeepEqual(after, before) {
				t.Fatalf("the load emptied the save or changed it: %+v", after)
			}
			fixture.press(testfixture.KTFSaveActionFinish, fourth)
			fixture.expectStored("after the restored title finished", partB(fourth))
		})
	}
}

// panickingSaves is a store whose reads panic, which a save store handed in by
// a Host is free to do.
type panickingSaves struct{ SaveStore }

func (panickingSaves) LoadSave(string) ([]byte, bool) { panic("the test store panics on a read") }

func (panickingSaves) ReadSave(string) ([]byte, bool, error) {
	panic("the test store panics on a read")
}

// A load that panics while it reads the saves still gives the running client
// its lock back. The shared session contains the panic and the Host then
// closes the session, and a close takes that lock: left held, the close would
// wait for ever.
func TestSessionQuickLoadReleasesTheRunningClientWhenTheStorePanics(t *testing.T) {
	for _, variant := range saveFixtureVariants() {
		t.Run(variant.name, func(t *testing.T) {
			fixture := startSaveFixture(t, variant)
			fixture.press(testfixture.KTFSaveActionSave, 0x11111111)
			checkpoint := fixture.quickSave()
			prepared, err := PrepareSessionCheckpoint(fixture.archive, checkpoint, fixture.options)
			if err != nil {
				t.Fatal(err)
			}
			defer prepared.Discard()
			var recovered any
			func() {
				defer func() { recovered = recover() }()
				_, _ = prepared.Commit(t.Context(), fixture.session, panickingSaves{fixture.probe})
			}()
			if recovered == nil {
				t.Fatal("the load did not read the store it was given")
			}
			if !fixture.session.Client.run.TryLock() {
				t.Fatal("the running client's lock was not given back")
			}
			fixture.session.Client.run.Unlock()
			// The running title is still the title, on its own store.
			fixture.press(testfixture.KTFSaveActionRead, 0)
			fixture.expectRead("a read after the panic", testfixture.KTFSaveFound, 8, 0x11111111, 0x11111111^testfixture.KTFSaveMask)
		})
	}
}

// A File whose open made a save that was not there asked for no empty file,
// and a load gives it the save as it is now, like any other File. What was
// saved behind its position after the quick save is still there after the
// restored title writes through it.
func TestSessionQuickLoadGivesAFileThatMadeItsSaveTheSaveAsItIsNow(t *testing.T) {
	const first, second, third = 0x11111111, 0x22222222, 0x33333333
	part := func(value uint32) []byte { return binary.LittleEndian.AppendUint32(nil, value) }
	for _, variant := range saveFixtureVariants() {
		t.Run(variant.name, func(t *testing.T) {
			fixture := startSaveFixture(t, variant)
			// No save yet: the hold's open makes it and writes part A.
			fixture.press(testfixture.KTFSaveActionHold, first)
			fixture.expectStored("after the hold", part(first))
			checkpoint := fixture.quickSave()
			fixture.press(testfixture.KTFSaveActionFinish, second)
			// A later save, longer than anything the title has written so far.
			later := append(testfixture.KTFSaveContent(second), "LATER"...)
			if err := fixture.store.StoreSave(testfixture.KTFSaveStoreKey, later); err != nil {
				t.Fatal(err)
			}
			before := fixture.snapshot()

			fixture.quickLoad(checkpoint)
			if after := fixture.snapshot(); !reflect.DeepEqual(after, before) {
				t.Fatalf("the load changed the save: %+v", after)
			}
			// Part B lands at the position the File had, and the rest of the
			// later save stays behind it.
			fixture.press(testfixture.KTFSaveActionFinish, third)
			want := append(append(part(second), part(third^testfixture.KTFSaveMask)...), "LATER"...)
			fixture.expectStored("after the restored title finished", want)
		})
	}
}

// A load after the title deleted its save finds no save: the restored title is
// told it is missing, an object it kept is empty, and its next write makes the
// save again from what it writes and nothing else.
func TestSessionQuickLoadAfterTheSaveWasDeleted(t *testing.T) {
	const first, second, third = 0x11111111, 0x22222222, 0x33333333
	for _, variant := range saveFixtureVariants() {
		t.Run(variant.name+", read then save", func(t *testing.T) {
			fixture := startSaveFixture(t, variant)
			fixture.press(testfixture.KTFSaveActionSave, first)
			checkpoint := fixture.quickSave()
			fixture.press(testfixture.KTFSaveActionDelete, 0)
			if !fixture.removed() {
				t.Fatal("the delete did not write the removal down")
			}
			before := fixture.snapshot()

			fixture.quickLoad(checkpoint)
			if after := fixture.snapshot(); !reflect.DeepEqual(after, before) {
				t.Fatalf("the load brought the deleted save back: %+v", after)
			}
			fixture.press(testfixture.KTFSaveActionRead, 0)
			fixture.expectRead("a read after the load", testfixture.KTFSaveMissing, 0, 0, 0)
			fixture.press(testfixture.KTFSaveActionSave, second)
			fixture.expectStored("after the restored title saved", testfixture.KTFSaveContent(second))
			if fixture.removed() {
				t.Fatal("the save the restored title made is still on the removal list")
			}
		})
		t.Run(variant.name+", write through a kept file", func(t *testing.T) {
			fixture := startSaveFixture(t, variant)
			fixture.press(testfixture.KTFSaveActionSave, first)
			fixture.press(testfixture.KTFSaveActionHold, second)
			checkpoint := fixture.quickSave()
			fixture.press(testfixture.KTFSaveActionFinish, second)
			fixture.press(testfixture.KTFSaveActionDelete, 0)

			fixture.quickLoad(checkpoint)
			// The kept object is empty and its cursor is where it was, past
			// the end: the write fills the gap with zeros.
			fixture.press(testfixture.KTFSaveActionFinish, third)
			want := append(make([]byte, 4), binary.LittleEndian.AppendUint32(nil, third^testfixture.KTFSaveMask)...)
			fixture.expectStored("after the restored title wrote through the kept file", want)
			if fixture.removed() {
				t.Fatal("the save the restored title made is still on the removal list")
			}
			// A quick save taken in that state, and a load of it, both work:
			// a cursor past the end is a state a checkpoint holds.
			fixture.press(testfixture.KTFSaveActionHold, first)
			fixture.press(testfixture.KTFSaveActionDelete, 0)
			again := fixture.quickSave()
			fixture.quickLoad(again)
			fixture.quickLoad(again)
		})
	}
}

// A quick save taken right after a load, before the title has run, is a
// checkpoint like any other: it holds what the first one held.
func TestSessionQuickSaveRightAfterAQuickLoad(t *testing.T) {
	const first, second = 0x11111111, 0x22222222
	for _, variant := range saveFixtureVariants() {
		t.Run(variant.name, func(t *testing.T) {
			fixture := startSaveFixture(t, variant)
			fixture.press(testfixture.KTFSaveActionSave, first)
			fixture.press(testfixture.KTFSaveActionHold, second)
			checkpoint := fixture.quickSave()
			fixture.quickLoad(checkpoint)
			again := fixture.quickSave()
			var before, after sessionCheckpointState
			if err := backend.DecodeCheckpointRecord(checkpoint.Runtime, &before); err != nil {
				t.Fatal(err)
			}
			if err := backend.DecodeCheckpointRecord(again.Runtime, &after); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(before.Client.Heap.Storage, after.Client.Heap.Storage) || !reflect.DeepEqual(before.Client.Heap.Databases, after.Client.Heap.Databases) || !reflect.DeepEqual(before.Adapters, after.Adapters) {
				t.Fatal("a quick save after a load records other storage than the one it was loaded from")
			}
			fixture.quickLoad(again)
			fixture.press(testfixture.KTFSaveActionFinish, second)
			fixture.expectStored("after the title finished in the twice-restored session", saveParts(second, second))
		})
	}
}

// A save that cannot be read refuses the load. The running session is the one
// that still runs, nothing was written, and the same load is accepted once the
// save can be read.
func TestSessionQuickLoadIsRefusedWhenASaveCannotBeRead(t *testing.T) {
	const first, second = 0x11111111, 0x22222222
	for _, variant := range saveFixtureVariants() {
		for _, key := range []string{testfixture.KTFSaveStoreKey, guestFileRemovedKey, databaseRemovedKey, directoryListKey, recordDatabaseRemovedKey, javaDatabaseRemovedKey} {
			t.Run(variant.name+", "+key, func(t *testing.T) {
				fixture := startSaveFixture(t, variant)
				fixture.press(testfixture.KTFSaveActionSave, first)
				fixture.press(testfixture.KTFSaveActionHold, second)
				checkpoint := fixture.quickSave()
				before, source, client := fixture.snapshot(), fixture.session, fixture.session.Client

				fixture.probe.unreadable = key
				err := fixture.tryQuickLoad(checkpoint)
				fixture.probe.unreadable = ""
				if !errors.Is(err, backend.ErrCheckpointSaveRead) {
					t.Fatalf("a load over an unreadable %s = %v", key, err)
				}
				if fixture.session != source || source.Client != client || client.workersStopped {
					t.Fatal("the refused load displaced the running session")
				}
				if after := fixture.snapshot(); !reflect.DeepEqual(after, before) {
					t.Fatalf("the refused load changed the saves: %+v", after)
				}
				// The running title carries on, and reads and writes its save.
				fixture.press(testfixture.KTFSaveActionFinish, first)
				fixture.expectStored("after the running title finished", saveParts(second, first))
				fixture.quickLoad(checkpoint)
				fixture.press(testfixture.KTFSaveActionFinish, second)
				fixture.expectStored("after the restored title finished", saveParts(second, second))
			})
		}
	}
}

// lateCancel is a context that is cancelled from its second question on, which
// is where Commit asks again: after the saves were read and before anything is
// adopted.
type lateCancel struct {
	context.Context
	asked int
}

func (ctx *lateCancel) Err() error {
	ctx.asked++
	if ctx.asked > 1 {
		return context.Canceled
	}
	return nil
}

// A load cancelled after its saves were read adopts nothing and leaves the
// restored runtime off the store, so what is discarded cannot reach a save.
// The same prepared load can still be committed.
func TestSessionQuickLoadCancelledAfterTheSavesWereRead(t *testing.T) {
	const first, second = 0x11111111, 0x22222222
	fixture := startSaveFixture(t, saveFixtureVariants()[0])
	fixture.press(testfixture.KTFSaveActionSave, first)
	fixture.press(testfixture.KTFSaveActionHold, second)
	checkpoint := fixture.quickSave()
	prepared, err := PrepareSessionCheckpoint(fixture.archive, checkpoint, fixture.options)
	if err != nil {
		t.Fatal(err)
	}
	defer prepared.Discard()
	restoredClient, source := prepared.session.Client, fixture.session.Client
	if _, err := prepared.Commit(&lateCancel{Context: t.Context()}, fixture.session, fixture.probe); !errors.Is(err, context.Canceled) {
		t.Fatalf("a load cancelled late = %v", err)
	}
	if restoredClient.saveStore != SaveStore(prepared.placeholder) || restoredClient.runtime.restoredStorage == nil {
		t.Fatal("the cancelled load left the restored runtime on the store")
	}
	if fixture.session.Client != source || source.workersStopped || source.saveStore != SaveStore(fixture.probe) {
		t.Fatal("the cancelled load changed the running session")
	}
	restored, err := prepared.Commit(t.Context(), fixture.session, fixture.probe)
	if err != nil {
		t.Fatalf("the load could not be committed after it was cancelled once: %v", err)
	}
	fixture.session = restored
	fixture.press(testfixture.KTFSaveActionFinish, first)
	fixture.expectStored("after the restored title finished", saveParts(second, first))
}

// A checkpoint record of another version is told apart from a damaged one and
// from one taken under another setting, and none of the three reads a save.
func TestSessionCheckpointRefusesOtherRecordVersionsAndPolicies(t *testing.T) {
	fixture := startSaveFixture(t, saveFixtureVariants()[0])
	fixture.press(testfixture.KTFSaveActionSave, 0x11111111)
	checkpoint := fixture.quickSave()
	var record sessionCheckpointState
	if err := backend.DecodeCheckpointRecord(checkpoint.Runtime, &record); err != nil {
		t.Fatal(err)
	}
	reads := fixture.probe.reads
	for _, test := range []struct {
		name, reason string
		mutate       func(*sessionCheckpointState)
		options      func(*SessionOptions)
	}{
		{"an earlier session record", "version 1", func(s *sessionCheckpointState) { s.Version = 1 }, nil},
		{"a later session record", "version 3", func(s *sessionCheckpointState) { s.Version = 3 }, nil},
		{"another timer limit", "timer limit", nil, func(o *SessionOptions) { o.TimerLimit = 7 }},
		{"another authentication setting", "authentication", nil, func(o *SessionOptions) { o.DisableAuthentication = true }},
	} {
		t.Run(test.name, func(t *testing.T) {
			changed, options := record, fixture.options
			if test.mutate != nil {
				test.mutate(&changed)
			}
			if test.options != nil {
				test.options(&options)
			}
			bad := checkpoint
			var err error
			if bad.Runtime, err = backend.EncodeCheckpointRecord(changed); err != nil {
				t.Fatal(err)
			}
			prepared, err := PrepareSessionCheckpoint(fixture.archive, bad, options)
			if prepared != nil {
				prepared.Discard()
			}
			if !errors.Is(err, backend.ErrCheckpointVersion) || !strings.Contains(err.Error(), test.reason) {
				t.Fatalf("refused with %v, want the version error naming %q", err, test.reason)
			}
		})
	}
	// The records inside the session record have versions of their own, and a
	// record whose inner version is another one is refused as well.
	for name, mutate := range map[string]func(*sessionCheckpointState){
		"an earlier client record":  func(s *sessionCheckpointState) { s.Client.Version = 1 },
		"an earlier adapter record": func(s *sessionCheckpointState) { s.Adapters.Version = 1 },
	} {
		changed := record
		mutate(&changed)
		bad := checkpoint
		var err error
		if bad.Runtime, err = backend.EncodeCheckpointRecord(changed); err != nil {
			t.Fatal(err)
		}
		prepared, err := PrepareSessionCheckpoint(fixture.archive, bad, fixture.options)
		if prepared != nil {
			prepared.Discard()
		}
		if err == nil {
			t.Fatalf("%s was accepted", name)
		}
	}
	if fixture.probe.reads != reads {
		t.Fatalf("refusing the records read the store %d times", fixture.probe.reads-reads)
	}
}

// Taking a checkpoint reaches the store with nothing, so a store that is only
// asked for one key at a time is enough to take one over.
func TestSessionCaptureNeedsOnlyAPlainSaveStore(t *testing.T) {
	archive, err := testfixture.KTFSaveArchive()
	if err != nil {
		t.Fatal(err)
	}
	store := &checkpointStoreProbe{memorySaveStore: memorySaveStore{}}
	session, err := StartSession(t.Context(), archive, SessionOptions{SaveStore: store})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	writeTestWords(t, session.Client, testfixture.KTFSaveProgress, []uint32{0x11111111})
	if err := session.SendKey(t.Context(), KeyPressed, testfixture.KTFSaveActionSave); err != nil {
		t.Fatal(err)
	}
	reads, writes := store.reads, store.writes
	if _, err := session.CaptureCheckpoint(t.Context()); err != nil {
		t.Fatalf("a capture over a plain save store: %v", err)
	}
	if store.reads != reads || store.writes != writes {
		t.Fatalf("the capture made %d reads and %d writes", store.reads-reads, store.writes-writes)
	}
}
