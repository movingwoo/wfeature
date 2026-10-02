package testfixture_test

import (
	"bytes"
	"encoding/binary"
	"path/filepath"
	"strings"
	"testing"

	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/cheat"
	"github.com/movingwoo/wfeature/internal/session"
	"github.com/movingwoo/wfeature/internal/testfixture"
)

// The save fixtures are driven the way a Host drives a title: started through
// the shared session, sent keys, ticked, and asked for guest words through the
// cheat engine's memory access. What a key did is then read in two places, the
// guest's words and the save store.

func lgtSaveStart(t *testing.T, archive []byte, store backend.SaveStore) *session.Session {
	t.Helper()
	started, err := session.Start(t.Context(), archive, session.Options{SaveStore: store})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(started.Close)
	lgtSaveTicks(t, started, 2)
	return started
}

func lgtSaveTicks(t *testing.T, s *session.Session, count int) {
	t.Helper()
	for range count {
		if _, err := s.Tick(t.Context(), 0); err != nil {
			t.Fatal(err)
		}
	}
}

func lgtSaveWord(t *testing.T, s *session.Session, address uint32) uint32 {
	t.Helper()
	data, err := s.Cheat().ReadBytes(address, 4)
	if err != nil {
		t.Fatal(err)
	}
	return binary.LittleEndian.Uint32(data)
}

func lgtSaveSetWord(t *testing.T, s *session.Session, address, value uint32) {
	t.Helper()
	if err := s.Cheat().WriteValue(address, cheat.ValueType{Kind: cheat.KindU32}, int64(value)); err != nil {
		t.Fatal(err)
	}
}

// lgtSavePress is one key press: down, the tick that delivers it, up, and the
// tick that delivers that.
func lgtSavePress(t *testing.T, s *session.Session, key int32) {
	t.Helper()
	for _, action := range []string{session.KeyPress, session.KeyRelease} {
		if err := s.SendKey(t.Context(), action, key); err != nil {
			t.Fatal(err)
		}
		lgtSaveTicks(t, s, 1)
	}
}

// lgtSaveAct sets the progress word and presses a key.
func lgtSaveAct(t *testing.T, s *session.Session, progress uint32, key int32) {
	t.Helper()
	lgtSaveSetWord(t, s, testfixture.LGTSaveProgress, progress)
	lgtSavePress(t, s, key)
}

func lgtSavePart(value uint32) []byte {
	return binary.LittleEndian.AppendUint32(nil, value)
}

// lgtSaveParts is the save a title writes from two progress values: part A
// from the first, part B from the second.
func lgtSaveParts(a, b uint32) []byte {
	return append(lgtSavePart(a), lgtSavePart(b^testfixture.LGTSaveXOR)...)
}

func lgtSaveStored(t *testing.T, store backend.SaveStore, key string) ([]byte, bool) {
	t.Helper()
	data, found, err := backend.ReadSave(store, key)
	if err != nil {
		t.Fatal(err)
	}
	return data, found
}

func lgtSaveWantStored(t *testing.T, store backend.SaveStore, key string, want []byte, why string) {
	t.Helper()
	if data, found := lgtSaveStored(t, store, key); !found || !bytes.Equal(data, want) {
		t.Fatalf("%s: the store holds %x (found %t), want %x", why, data, found, want)
	}
}

// lgtSaveSeen is what a read left in the guest.
type lgtSaveSeen struct {
	A, B, Status, Length uint32
}

// lgtSaveRead presses the read key with a progress word that is none of the
// saved ones, so what comes back can only have come from the save.
func lgtSaveRead(t *testing.T, s *session.Session) lgtSaveSeen {
	t.Helper()
	lgtSaveAct(t, s, 0xdeadbeef, testfixture.LGTSaveKeyRead)
	return lgtSaveSeen{
		A: lgtSaveWord(t, s, testfixture.LGTSaveSeenA), B: lgtSaveWord(t, s, testfixture.LGTSaveSeenB),
		Status: lgtSaveWord(t, s, testfixture.LGTSaveStatus), Length: lgtSaveWord(t, s, testfixture.LGTSaveLength),
	}
}

func lgtSaveFound(a, b uint32) lgtSaveSeen {
	return lgtSaveSeen{A: a, B: b ^ testfixture.LGTSaveXOR, Status: testfixture.LGTSaveStatusFound, Length: 8}
}

var lgtSaveMissing = lgtSaveSeen{Status: testfixture.LGTSaveStatusMissing}

func lgtSaveArchive(t *testing.T) []byte {
	t.Helper()
	archive, err := testfixture.LGTSaveArchive()
	if err != nil {
		t.Fatal(err)
	}
	return archive
}

// The Clet keeps the checkpoint Clet's behaviour: it starts once, counts its
// frames and records the events it is handed, at the same addresses.
func TestLGTSaveClet(t *testing.T) {
	archive := lgtSaveArchive(t)
	summary, err := session.Inspect(archive)
	if err != nil || summary.Platform != "lgt" || summary.SaveOwner != testfixture.LGTSaveOwner {
		t.Fatalf("the archive is %+v, %v", summary, err)
	}
	store, _ := backend.NewMemorySaveStore(nil)
	s := lgtSaveStart(t, archive, store)
	frames := lgtSaveWord(t, s, testfixture.LGTCheckpointFrameCounter)
	lgtSavePress(t, s, '0')
	if lgtSaveWord(t, s, testfixture.LGTCheckpointStartupCounter) != 1 ||
		lgtSaveWord(t, s, testfixture.LGTCheckpointFrameCounter) != frames+2 ||
		lgtSaveWord(t, s, testfixture.LGTCheckpointLastKey) != '0' ||
		lgtSaveWord(t, s, testfixture.LGTCheckpointLifecycle) != 2 {
		t.Fatal("the Clet did not start once, count two frames and record the key")
	}
	// A key that is none of the fixture's does nothing to the save.
	if entries, _ := store.SnapshotSaves(); len(entries) != 0 {
		t.Fatalf("a key with no action wrote %d save entries", len(entries))
	}
}

// Save, read, patch and delete, each seen from both sides: the store holds
// what the key wrote, and the guest reads back what the store holds.
func TestLGTSaveCletKeysWriteAndReadTheSave(t *testing.T) {
	archive := lgtSaveArchive(t)
	store := backend.NewDirectorySaveStore(filepath.Join(t.TempDir(), testfixture.LGTSaveOwner))
	s := lgtSaveStart(t, archive, store)
	const key = testfixture.LGTSaveFileKey

	if seen := lgtSaveRead(t, s); seen != lgtSaveMissing {
		t.Fatalf("before any save a read answered %+v", seen)
	}
	if _, found := lgtSaveStored(t, store, key); found {
		t.Fatal("a read created the save")
	}

	lgtSaveAct(t, s, 0x11110001, testfixture.LGTSaveKeySave)
	lgtSaveWantStored(t, store, key, lgtSaveParts(0x11110001, 0x11110001), "save")
	if seen := lgtSaveRead(t, s); seen != lgtSaveFound(0x11110001, 0x11110001) {
		t.Fatalf("after a save a read answered %+v", seen)
	}

	// Part A only, in place: part B is still the first save's.
	lgtSaveAct(t, s, 0x22220002, testfixture.LGTSaveKeyPatch)
	lgtSaveWantStored(t, store, key, lgtSaveParts(0x22220002, 0x11110001), "patch")
	if seen := lgtSaveRead(t, s); seen != lgtSaveFound(0x22220002, 0x11110001) {
		t.Fatalf("after a patch a read answered %+v", seen)
	}

	// The store has no delete of its own: the platform lists the path as
	// removed and answers as if it were gone.
	lgtSavePress(t, s, testfixture.LGTSaveKeyDelete)
	if seen := lgtSaveRead(t, s); seen != lgtSaveMissing {
		t.Fatalf("after a delete a read answered %+v", seen)
	}
	if removed, _ := lgtSaveStored(t, store, "fs/.removed"); !strings.Contains(string(removed), testfixture.LGTSaveFileName) {
		t.Fatalf("the removal list holds %q", removed)
	}

	// A patch on a save that is not there creates one holding part A alone.
	lgtSaveAct(t, s, 0x33330003, testfixture.LGTSaveKeyPatch)
	lgtSaveWantStored(t, store, key, lgtSavePart(0x33330003), "patch of an absent save")
	if seen := lgtSaveRead(t, s); seen != (lgtSaveSeen{A: 0x33330003, Status: testfixture.LGTSaveStatusFound, Length: 4}) {
		t.Fatalf("after a patch of an absent save a read answered %+v", seen)
	}

	// A save is a whole rewrite, whatever was there.
	lgtSaveAct(t, s, 0x44440004, testfixture.LGTSaveKeySave)
	lgtSaveWantStored(t, store, key, lgtSaveParts(0x44440004, 0x44440004), "save over a patch")
}

// A write through a handle that is still open is in the platform's buffer for
// that handle and nowhere else: the store has the save as it was until the
// handle is closed, by the title or by the session ending.
func TestLGTSaveCletHoldLeavesTheWritePending(t *testing.T) {
	archive := lgtSaveArchive(t)
	const key = testfixture.LGTSaveFileKey
	held := func(t *testing.T) (*session.Session, *backend.MemorySaveStore) {
		store, _ := backend.NewMemorySaveStore(nil)
		s := lgtSaveStart(t, archive, store)
		lgtSaveAct(t, s, 0x11110001, testfixture.LGTSaveKeySave)
		lgtSaveAct(t, s, 0x22220002, testfixture.LGTSaveKeyHold)
		if lgtSaveWord(t, s, testfixture.LGTSaveHeld) == 0 {
			t.Fatal("hold kept no handle")
		}
		lgtSaveWantStored(t, store, key, lgtSaveParts(0x11110001, 0x11110001), "hold")
		return s, store
	}

	t.Run("finish", func(t *testing.T) {
		s, store := held(t)
		// A second hold is ignored while a handle is kept, and a read through
		// another handle sees what the store has.
		handle := lgtSaveWord(t, s, testfixture.LGTSaveHeld)
		lgtSaveAct(t, s, 0x77770007, testfixture.LGTSaveKeyHold)
		lgtSaveAct(t, s, 0x77770007, testfixture.LGTSaveKeyTruncate)
		if lgtSaveWord(t, s, testfixture.LGTSaveHeld) != handle {
			t.Fatal("a second hold replaced the handle that was kept")
		}
		if seen := lgtSaveRead(t, s); seen != lgtSaveFound(0x11110001, 0x11110001) {
			t.Fatalf("while a write is pending a read answered %+v", seen)
		}
		lgtSaveAct(t, s, 0x33330003, testfixture.LGTSaveKeyFinish)
		if lgtSaveWord(t, s, testfixture.LGTSaveHeld) != 0 {
			t.Fatal("finish kept the handle")
		}
		lgtSaveWantStored(t, store, key, lgtSaveParts(0x22220002, 0x33330003), "finish")
		// With nothing kept, finish does nothing.
		lgtSaveAct(t, s, 0x55550005, testfixture.LGTSaveKeyFinish)
		lgtSaveWantStored(t, store, key, lgtSaveParts(0x22220002, 0x33330003), "a finish with nothing kept")
	})
	t.Run("session close", func(t *testing.T) {
		s, store := held(t)
		s.Close()
		lgtSaveWantStored(t, store, key, lgtSaveParts(0x22220002, 0x11110001), "close")
	})
	t.Run("absent save", func(t *testing.T) {
		// An open with write intent creates the file, so a hold on a save that
		// is not there stores an empty one and keeps part A back.
		store, _ := backend.NewMemorySaveStore(nil)
		s := lgtSaveStart(t, archive, store)
		lgtSaveAct(t, s, 0x22220002, testfixture.LGTSaveKeyHold)
		lgtSaveWantStored(t, store, key, []byte{}, "hold on an absent save")
		lgtSaveAct(t, s, 0x33330003, testfixture.LGTSaveKeyFinish)
		lgtSaveWantStored(t, store, key, lgtSaveParts(0x22220002, 0x33330003), "finish")
	})
}

// A handle opened with truncation holds nothing and has written nothing, so
// the stored save is untouched until something is written through it.
func TestLGTSaveCletTruncateLeavesTheSaveUntilItIsFinished(t *testing.T) {
	archive := lgtSaveArchive(t)
	const key = testfixture.LGTSaveFileKey
	truncated := func(t *testing.T) (*session.Session, *backend.MemorySaveStore) {
		store, _ := backend.NewMemorySaveStore(nil)
		s := lgtSaveStart(t, archive, store)
		lgtSaveAct(t, s, 0x11110001, testfixture.LGTSaveKeySave)
		lgtSaveAct(t, s, 0x22220002, testfixture.LGTSaveKeyTruncate)
		if lgtSaveWord(t, s, testfixture.LGTSaveHeld) == 0 {
			t.Fatal("truncate kept no handle")
		}
		lgtSaveWantStored(t, store, key, lgtSaveParts(0x11110001, 0x11110001), "truncate")
		if seen := lgtSaveRead(t, s); seen != lgtSaveFound(0x11110001, 0x11110001) {
			t.Fatalf("while a truncation is pending a read answered %+v", seen)
		}
		return s, store
	}

	t.Run("finish", func(t *testing.T) {
		// Part B is the only thing written through the handle, so it is the
		// whole of the file: the first save's eight bytes are gone.
		s, store := truncated(t)
		lgtSaveAct(t, s, 0x33330003, testfixture.LGTSaveKeyFinish)
		lgtSaveWantStored(t, store, key, lgtSavePart(0x33330003^testfixture.LGTSaveXOR), "finish")
		want := lgtSaveSeen{A: 0x33330003 ^ testfixture.LGTSaveXOR, Status: testfixture.LGTSaveStatusFound, Length: 4}
		if seen := lgtSaveRead(t, s); seen != want {
			t.Fatalf("after the truncated save was finished a read answered %+v", seen)
		}
	})
	t.Run("session close", func(t *testing.T) {
		s, store := truncated(t)
		s.Close()
		lgtSaveWantStored(t, store, key, lgtSaveParts(0x11110001, 0x11110001), "close")
	})
}

// lgtSaveCheckpoint takes a checkpoint and checks which variant wrote it.
func lgtSaveCheckpoint(t *testing.T, s *session.Session, archive []byte, variant uint16) []byte {
	t.Helper()
	data, err := s.CaptureCheckpoint(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if checkpoint, err := backend.DecodeCheckpoint(data, backend.SaveIdentity(archive)); err != nil || checkpoint.Variant != variant {
		t.Fatalf("the checkpoint is variant %d, %v; want %d", checkpoint.Variant, err, variant)
	}
	return data
}

// The archive is a checkpoint subject as quick save and quick load stand
// today: a checkpoint taken after a save is loaded over the running session
// and into a new one, the guest is back where it was, and the keys still work.
//
// **Nothing here says what a load does to the saves.** The store holds the
// same bytes at every load as it did when the checkpoint was taken, so the
// test passes whether a load puts the checkpoint's saves back or leaves the
// store alone.
func TestLGTSaveCletIsACheckpointSubject(t *testing.T) {
	archive := lgtSaveArchive(t)
	const key = testfixture.LGTSaveFileKey

	t.Run("after a save", func(t *testing.T) {
		store, _ := backend.NewMemorySaveStore(nil)
		s := lgtSaveStart(t, archive, store)
		lgtSaveAct(t, s, 0x11110001, testfixture.LGTSaveKeySave)
		data := lgtSaveCheckpoint(t, s, archive, backend.CheckpointLGTClet)
		frames := lgtSaveWord(t, s, testfixture.LGTCheckpointFrameCounter)
		lgtSaveSetWord(t, s, testfixture.LGTSaveProgress, 0x99990009)
		lgtSaveTicks(t, s, 3)

		check := func(t *testing.T, restored *session.Session) {
			t.Helper()
			if lgtSaveWord(t, restored, testfixture.LGTCheckpointStartupCounter) != 1 ||
				lgtSaveWord(t, restored, testfixture.LGTCheckpointFrameCounter) != frames ||
				lgtSaveWord(t, restored, testfixture.LGTSaveProgress) != 0x11110001 {
				t.Fatal("the restored guest is not at the checkpoint")
			}
			lgtSaveTicks(t, restored, 2)
			if lgtSaveWord(t, restored, testfixture.LGTCheckpointFrameCounter) != frames+2 {
				t.Fatal("the restored guest does not run on from the checkpoint")
			}
			if seen := lgtSaveRead(t, restored); seen != lgtSaveFound(0x11110001, 0x11110001) {
				t.Fatalf("after a load a read answered %+v", seen)
			}
			lgtSaveAct(t, restored, 0x22220002, testfixture.LGTSaveKeyPatch)
			lgtSaveWantStored(t, store, key, lgtSaveParts(0x22220002, 0x11110001), "patch after a load")
			// The store goes back to what the checkpoint was taken beside.
			lgtSaveAct(t, restored, 0x11110001, testfixture.LGTSaveKeySave)
			lgtSaveWantStored(t, store, key, lgtSaveParts(0x11110001, 0x11110001), "save after a load")
		}
		if err := s.LoadCheckpoint(t.Context(), archive, data); err != nil {
			t.Fatal(err)
		}
		check(t, s)
		s.Close()
		restored, err := session.RestoreCheckpoint(t.Context(), archive, data, session.Options{SaveStore: store})
		if err != nil {
			t.Fatal(err)
		}
		defer restored.Close()
		check(t, restored)
	})

	// A handle that was open when the checkpoint was taken is open after the
	// load, under the same number, and finishing through it completes the
	// save the checkpoint was taken in the middle of.
	t.Run("with a handle held", func(t *testing.T) {
		store, _ := backend.NewMemorySaveStore(nil)
		s := lgtSaveStart(t, archive, store)
		lgtSaveAct(t, s, 0x11110001, testfixture.LGTSaveKeySave)
		lgtSaveAct(t, s, 0x22220002, testfixture.LGTSaveKeyHold)
		handle := lgtSaveWord(t, s, testfixture.LGTSaveHeld)
		data := lgtSaveCheckpoint(t, s, archive, backend.CheckpointLGTClet)

		check := func(t *testing.T, restored *session.Session, progress uint32) {
			t.Helper()
			if lgtSaveWord(t, restored, testfixture.LGTSaveHeld) != handle {
				t.Fatal("the restored guest does not hold the handle it held")
			}
			lgtSaveAct(t, restored, progress, testfixture.LGTSaveKeyFinish)
			lgtSaveWantStored(t, store, key, lgtSaveParts(0x22220002, progress), "finish after a load")
		}
		if err := s.LoadCheckpoint(t.Context(), archive, data); err != nil {
			t.Fatal(err)
		}
		check(t, s, 0x33330003)
		s.Close()
		restored, err := session.RestoreCheckpoint(t.Context(), archive, data, session.Options{SaveStore: store})
		if err != nil {
			t.Fatal(err)
		}
		defer restored.Close()
		check(t, restored, 0x44440004)
	})
}
