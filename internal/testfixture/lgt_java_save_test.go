package testfixture_test

import (
	"bytes"
	"encoding/binary"
	"strings"
	"testing"

	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/session"
	"github.com/movingwoo/wfeature/internal/testfixture"
)

func lgtJavaSaveArchive(t *testing.T) []byte {
	t.Helper()
	archive, err := testfixture.LGTJavaSaveArchive()
	if err != nil {
		t.Fatal(err)
	}
	return archive
}

// lgtJavaSaveSurface is one of the title's three ways to its save, and how a
// test reads what that way stored.
type lgtJavaSaveSurface struct {
	name    string
	surface uint32
	// key is the save store's entry, and removed what the removal list calls
	// the path once the title has deleted it.
	key, removed string
	// content is the save's parts as one run of bytes: the file as it is, or
	// the record store's records one after another.
	content func(t *testing.T, data []byte) []byte
}

var lgtJavaSaveSurfaces = []lgtJavaSaveSurface{
	{"file", testfixture.LGTJavaSaveSurfaceFile, testfixture.LGTSaveFileKey, testfixture.LGTSaveFileName,
		func(_ *testing.T, data []byte) []byte { return data }},
	{"stream", testfixture.LGTJavaSaveSurfaceStream, testfixture.LGTSaveFileKey, testfixture.LGTSaveFileName,
		func(_ *testing.T, data []byte) []byte { return data }},
	{"database", testfixture.LGTJavaSaveSurfaceDatabase, testfixture.LGTJavaSaveDatabaseKey,
		testfixture.LGTJavaSaveDatabaseName + ".db", lgtJavaSaveRecords},
}

// lgtJavaSaveRecords joins the records of the container the platform keeps a
// DataBase in. The container is the platform's own: the magic and a version, a
// record size, a count, then one length-prefixed record each, with a length of
// all ones for a slot whose record was deleted.
func lgtJavaSaveRecords(t *testing.T, container []byte) []byte {
	t.Helper()
	if len(container) < 16 || string(container[:4]) != "WFDB" {
		t.Fatalf("the stored database is not a container: %x", container)
	}
	joined := []byte{}
	rest := container[16:]
	for record := uint32(0); record < binary.LittleEndian.Uint32(container[12:]); record++ {
		if len(rest) < 4 {
			t.Fatalf("the stored database ends inside record %d: %x", record, container)
		}
		length := binary.LittleEndian.Uint32(rest)
		rest = rest[4:]
		if length == ^uint32(0) {
			continue
		}
		if uint64(length) > uint64(len(rest)) {
			t.Fatalf("record %d of the stored database claims %d bytes: %x", record, length, container)
		}
		joined = append(joined, rest[:length]...)
		rest = rest[length:]
	}
	return joined
}

// stored is the save's parts as the store holds them now.
func (surface lgtJavaSaveSurface) stored(t *testing.T, store backend.SaveStore) ([]byte, bool) {
	t.Helper()
	data, found := lgtSaveStored(t, store, surface.key)
	if !found {
		return nil, false
	}
	return surface.content(t, data), true
}

func (surface lgtJavaSaveSurface) wantStored(t *testing.T, store backend.SaveStore, want []byte, why string) {
	t.Helper()
	if data, found := surface.stored(t, store); !found || !bytes.Equal(data, want) {
		t.Fatalf("%s: the store holds %x (found %t), want %x", why, data, found, want)
	}
}

// start runs the title with its keys pointed at this surface.
func (surface lgtJavaSaveSurface) start(t *testing.T, archive []byte, store backend.SaveStore) *session.Session {
	t.Helper()
	s := lgtSaveStart(t, archive, store)
	lgtSaveSetWord(t, s, testfixture.LGTJavaSaveSurface, surface.surface)
	return s
}

// lgtJavaSaveCaughtEverything fails a test whose title let an exception out of
// a callback: the two the fixture expects are caught in its own try regions.
func lgtJavaSaveCaughtEverything(t *testing.T, s *session.Session) {
	t.Helper()
	if count, first := s.LGT().UncaughtCallbacks(); count != 0 {
		t.Fatalf("%d callbacks ended in an exception: %s", count, first)
	}
}

// The Java title starts through the ordinary loader: the module's own startup
// hands over its class table, the launcher builds the Jlet, and the card it
// pushes is painted every tick and handed the keys.
func TestLGTJavaSave(t *testing.T) {
	archive := lgtJavaSaveArchive(t)
	summary, err := session.Inspect(archive)
	if err != nil || summary.Platform != "lgt" || summary.SaveOwner != testfixture.LGTJavaSaveOwner {
		t.Fatalf("the archive is %+v, %v", summary, err)
	}
	store, _ := backend.NewMemorySaveStore(nil)
	s := lgtSaveStart(t, archive, store)
	frames, flushes := lgtSaveWord(t, s, testfixture.LGTCheckpointFrameCounter), s.Flushes()
	before, _, _, _ := s.Frame()
	lgtSavePress(t, s, '2')
	if lgtSaveWord(t, s, testfixture.LGTCheckpointStartupCounter) != 1 ||
		lgtSaveWord(t, s, testfixture.LGTCheckpointFrameCounter) != frames+2 || s.Flushes() != flushes+2 ||
		lgtSaveWord(t, s, testfixture.LGTCheckpointLastKey) != '2' {
		t.Fatal("the title did not start once, paint two frames and record the key")
	}
	if after, _, _, _ := s.Frame(); bytes.Equal(before, after) {
		t.Fatal("two frames later the picture is the same")
	}
	if entries, _ := store.SnapshotSaves(); len(entries) != 0 {
		t.Fatalf("a key with no action wrote %d save entries", len(entries))
	}
	lgtJavaSaveCaughtEverything(t, s)
}

// Save, read, patch and delete on each surface, seen from both sides: the
// store holds what the key wrote, and the guest reads back what the store
// holds.
func TestLGTJavaSaveKeysWriteAndReadTheSave(t *testing.T) {
	archive := lgtJavaSaveArchive(t)
	for _, surface := range lgtJavaSaveSurfaces {
		t.Run(surface.name, func(t *testing.T) {
			store, _ := backend.NewMemorySaveStore(nil)
			s := surface.start(t, archive, store)

			if seen := lgtSaveRead(t, s); seen != lgtSaveMissing {
				t.Fatalf("before any save a read answered %+v", seen)
			}
			if _, found := surface.stored(t, store); found {
				t.Fatal("a read created the save")
			}
			// Deleting a save that is not there is not an error either.
			lgtSavePress(t, s, testfixture.LGTSaveKeyDelete)

			lgtSaveAct(t, s, 0x11110001, testfixture.LGTSaveKeySave)
			surface.wantStored(t, store, lgtSaveParts(0x11110001, 0x11110001), "save")
			if seen := lgtSaveRead(t, s); seen != lgtSaveFound(0x11110001, 0x11110001) {
				t.Fatalf("after a save a read answered %+v", seen)
			}

			// Part A only, in place: part B is still the first save's.
			lgtSaveAct(t, s, 0x22220002, testfixture.LGTSaveKeyPatch)
			surface.wantStored(t, store, lgtSaveParts(0x22220002, 0x11110001), "patch")
			if seen := lgtSaveRead(t, s); seen != lgtSaveFound(0x22220002, 0x11110001) {
				t.Fatalf("after a patch a read answered %+v", seen)
			}

			lgtSavePress(t, s, testfixture.LGTSaveKeyDelete)
			if seen := lgtSaveRead(t, s); seen != lgtSaveMissing {
				t.Fatalf("after a delete a read answered %+v", seen)
			}
			if removed, _ := lgtSaveStored(t, store, "fs/.removed"); !strings.Contains(string(removed), surface.removed) {
				t.Fatalf("the removal list holds %q", removed)
			}

			// A patch on a save that is not there creates one holding part A alone.
			lgtSaveAct(t, s, 0x33330003, testfixture.LGTSaveKeyPatch)
			surface.wantStored(t, store, lgtSavePart(0x33330003), "patch of an absent save")
			if seen := lgtSaveRead(t, s); seen != (lgtSaveSeen{A: 0x33330003, Status: testfixture.LGTSaveStatusFound, Length: 4}) {
				t.Fatalf("after a patch of an absent save a read answered %+v", seen)
			}

			// A save writes both parts, whatever was there.
			lgtSaveAct(t, s, 0x44440004, testfixture.LGTSaveKeySave)
			surface.wantStored(t, store, lgtSaveParts(0x44440004, 0x44440004), "save over a patch")
			lgtJavaSaveCaughtEverything(t, s)
		})
	}
}

// lgtJavaSaveHeld is the three words that say what the title is keeping open.
type lgtJavaSaveHeld struct {
	File, Stream, Database uint32
}

func lgtJavaSaveHeldWords(t *testing.T, s *session.Session) lgtJavaSaveHeld {
	t.Helper()
	return lgtJavaSaveHeld{
		File:     lgtSaveWord(t, s, testfixture.LGTSaveHeld),
		Stream:   lgtSaveWord(t, s, testfixture.LGTJavaSaveHeldStream),
		Database: lgtSaveWord(t, s, testfixture.LGTJavaSaveHeldDatabase),
	}
}

// A File's write waits in the buffer of its open handle: the store has the
// save as it was until the File is closed, by the title or by the session
// ending.
func TestLGTJavaSaveFileHoldLeavesTheWritePending(t *testing.T) {
	archive := lgtJavaSaveArchive(t)
	surface := lgtJavaSaveSurfaces[0]
	held := func(t *testing.T) (*session.Session, *backend.MemorySaveStore) {
		store, _ := backend.NewMemorySaveStore(nil)
		s := surface.start(t, archive, store)
		lgtSaveAct(t, s, 0x11110001, testfixture.LGTSaveKeySave)
		lgtSaveAct(t, s, 0x22220002, testfixture.LGTSaveKeyHold)
		if kept := lgtJavaSaveHeldWords(t, s); kept.File == 0 || kept.Stream != 0 || kept.Database != 0 {
			t.Fatalf("hold kept %+v", kept)
		}
		surface.wantStored(t, store, lgtSaveParts(0x11110001, 0x11110001), "hold")
		return s, store
	}

	t.Run("finish", func(t *testing.T) {
		s, store := held(t)
		// A second hold is ignored while a File is kept, a flush has no stream
		// to flush, and a read through another File sees what the store has.
		kept := lgtJavaSaveHeldWords(t, s)
		lgtSaveAct(t, s, 0x77770007, testfixture.LGTSaveKeyHold)
		lgtSaveAct(t, s, 0x77770007, testfixture.LGTSaveKeyTruncate)
		lgtSavePress(t, s, testfixture.LGTJavaSaveKeyFlush)
		if lgtJavaSaveHeldWords(t, s) != kept {
			t.Fatal("a second hold replaced the File that was kept")
		}
		if seen := lgtSaveRead(t, s); seen != lgtSaveFound(0x11110001, 0x11110001) {
			t.Fatalf("while a write is pending a read answered %+v", seen)
		}
		lgtSaveAct(t, s, 0x33330003, testfixture.LGTSaveKeyFinish)
		if lgtJavaSaveHeldWords(t, s) != (lgtJavaSaveHeld{}) {
			t.Fatal("finish kept the File")
		}
		surface.wantStored(t, store, lgtSaveParts(0x22220002, 0x33330003), "finish")
		lgtSaveAct(t, s, 0x55550005, testfixture.LGTSaveKeyFinish)
		surface.wantStored(t, store, lgtSaveParts(0x22220002, 0x33330003), "a finish with nothing kept")
		lgtJavaSaveCaughtEverything(t, s)
	})
	t.Run("session close", func(t *testing.T) {
		s, store := held(t)
		s.Close()
		surface.wantStored(t, store, lgtSaveParts(0x22220002, 0x11110001), "close")
	})
}

// A stream's write waits one step further back, in the stream. A flush moves
// it into the File's buffer, which is still not the store; only the File's
// close is. A session that ends stores what is in a handle's buffer and not
// what is still in a stream.
func TestLGTJavaSaveStreamHoldLeavesTheWriteInTheStream(t *testing.T) {
	archive := lgtJavaSaveArchive(t)
	surface := lgtJavaSaveSurfaces[1]
	held := func(t *testing.T) (*session.Session, *backend.MemorySaveStore) {
		store, _ := backend.NewMemorySaveStore(nil)
		s := surface.start(t, archive, store)
		lgtSaveAct(t, s, 0x11110001, testfixture.LGTSaveKeySave)
		lgtSaveAct(t, s, 0x22220002, testfixture.LGTSaveKeyHold)
		if kept := lgtJavaSaveHeldWords(t, s); kept.File == 0 || kept.Stream == 0 || kept.Database != 0 {
			t.Fatalf("hold kept %+v", kept)
		}
		surface.wantStored(t, store, lgtSaveParts(0x11110001, 0x11110001), "hold")
		return s, store
	}

	t.Run("finish", func(t *testing.T) {
		s, store := held(t)
		lgtSaveAct(t, s, 0x33330003, testfixture.LGTSaveKeyFinish)
		if lgtJavaSaveHeldWords(t, s) != (lgtJavaSaveHeld{}) {
			t.Fatal("finish kept the File or its stream")
		}
		surface.wantStored(t, store, lgtSaveParts(0x22220002, 0x33330003), "finish")
		lgtJavaSaveCaughtEverything(t, s)
	})
	t.Run("flush and finish", func(t *testing.T) {
		s, store := held(t)
		lgtSavePress(t, s, testfixture.LGTJavaSaveKeyFlush)
		surface.wantStored(t, store, lgtSaveParts(0x11110001, 0x11110001), "flush")
		lgtSaveAct(t, s, 0x33330003, testfixture.LGTSaveKeyFinish)
		surface.wantStored(t, store, lgtSaveParts(0x22220002, 0x33330003), "finish")
		lgtJavaSaveCaughtEverything(t, s)
	})
	t.Run("session close", func(t *testing.T) {
		s, store := held(t)
		s.Close()
		surface.wantStored(t, store, lgtSaveParts(0x11110001, 0x11110001), "close with the write still in the stream")
	})
	t.Run("flush and session close", func(t *testing.T) {
		s, store := held(t)
		lgtSavePress(t, s, testfixture.LGTJavaSaveKeyFlush)
		s.Close()
		surface.wantStored(t, store, lgtSaveParts(0x22220002, 0x11110001), "close after a flush")
	})
}

// A record store holds nothing back: the record a hold puts is stored as it is
// put, and what the title keeps is the open DataBase.
func TestLGTJavaSaveDatabaseHoldStoresAsItWrites(t *testing.T) {
	archive := lgtJavaSaveArchive(t)
	surface := lgtJavaSaveSurfaces[2]
	store, _ := backend.NewMemorySaveStore(nil)
	s := surface.start(t, archive, store)
	lgtSaveAct(t, s, 0x11110001, testfixture.LGTSaveKeySave)
	lgtSaveAct(t, s, 0x22220002, testfixture.LGTSaveKeyHold)
	kept := lgtJavaSaveHeldWords(t, s)
	if kept.File != 0 || kept.Stream != 0 || kept.Database == 0 {
		t.Fatalf("hold kept %+v", kept)
	}
	surface.wantStored(t, store, lgtSaveParts(0x22220002, 0x11110001), "hold")

	// A record store has no open that truncates, so that key does nothing
	// here, and a second hold is ignored while a DataBase is kept.
	lgtSaveAct(t, s, 0x77770007, testfixture.LGTSaveKeyTruncate)
	lgtSaveAct(t, s, 0x77770007, testfixture.LGTSaveKeyHold)
	if lgtJavaSaveHeldWords(t, s) != kept {
		t.Fatal("a truncate or a second hold changed what is kept")
	}
	surface.wantStored(t, store, lgtSaveParts(0x22220002, 0x11110001), "truncate")

	lgtSaveAct(t, s, 0x33330003, testfixture.LGTSaveKeyFinish)
	if lgtJavaSaveHeldWords(t, s) != (lgtJavaSaveHeld{}) {
		t.Fatal("finish kept the DataBase")
	}
	surface.wantStored(t, store, lgtSaveParts(0x22220002, 0x33330003), "finish")
	lgtSaveAct(t, s, 0x55550005, testfixture.LGTSaveKeyFinish)
	surface.wantStored(t, store, lgtSaveParts(0x22220002, 0x33330003), "a finish with nothing kept")
	lgtJavaSaveCaughtEverything(t, s)
}

// A File opened with truncation holds nothing and has written nothing, so the
// stored save is untouched until something is written through it and it is
// closed — and then part B, the only thing written, is the whole of the file.
func TestLGTJavaSaveTruncateLeavesTheSaveUntilItIsFinished(t *testing.T) {
	archive := lgtJavaSaveArchive(t)
	for _, surface := range lgtJavaSaveSurfaces[:2] {
		t.Run(surface.name, func(t *testing.T) {
			truncated := func(t *testing.T) (*session.Session, *backend.MemorySaveStore) {
				store, _ := backend.NewMemorySaveStore(nil)
				s := surface.start(t, archive, store)
				lgtSaveAct(t, s, 0x11110001, testfixture.LGTSaveKeySave)
				lgtSaveAct(t, s, 0x22220002, testfixture.LGTSaveKeyTruncate)
				kept := lgtJavaSaveHeldWords(t, s)
				if kept.File == 0 || (kept.Stream != 0) != (surface.surface == testfixture.LGTJavaSaveSurfaceStream) {
					t.Fatalf("truncate kept %+v", kept)
				}
				surface.wantStored(t, store, lgtSaveParts(0x11110001, 0x11110001), "truncate")
				if seen := lgtSaveRead(t, s); seen != lgtSaveFound(0x11110001, 0x11110001) {
					t.Fatalf("while a truncation is pending a read answered %+v", seen)
				}
				return s, store
			}
			t.Run("finish", func(t *testing.T) {
				s, store := truncated(t)
				lgtSaveAct(t, s, 0x33330003, testfixture.LGTSaveKeyFinish)
				surface.wantStored(t, store, lgtSavePart(0x33330003^testfixture.LGTSaveXOR), "finish")
				want := lgtSaveSeen{A: 0x33330003 ^ testfixture.LGTSaveXOR, Status: testfixture.LGTSaveStatusFound, Length: 4}
				if seen := lgtSaveRead(t, s); seen != want {
					t.Fatalf("after the truncated save was finished a read answered %+v", seen)
				}
				lgtJavaSaveCaughtEverything(t, s)
			})
			t.Run("session close", func(t *testing.T) {
				s, store := truncated(t)
				s.Close()
				surface.wantStored(t, store, lgtSaveParts(0x11110001, 0x11110001), "close")
			})
		})
	}
}

// The archive is a checkpoint subject as quick save and quick load stand
// today, on every surface: a checkpoint taken after a save, or with something
// kept open, is loaded over the running session and into a new one, the guest
// is back where it was, and the keys still work.
//
// As in the Clet's test, the store holds the same bytes at every load as it
// did when the checkpoint was taken, and what is kept open is only finished
// through, so nothing here says what a load does to the saves.
func TestLGTJavaSaveIsACheckpointSubject(t *testing.T) {
	archive := lgtJavaSaveArchive(t)
	// both loads the checkpoint over the session it was taken from and then
	// into a session that never ran the title, and checks each.
	both := func(t *testing.T, s *session.Session, store backend.SaveStore, data []byte, check func(*testing.T, *session.Session, uint32)) {
		t.Helper()
		if err := s.LoadCheckpoint(t.Context(), archive, data); err != nil {
			t.Fatal(err)
		}
		check(t, s, 0x33330003)
		lgtJavaSaveCaughtEverything(t, s)
		s.Close()
		restored, err := session.RestoreCheckpoint(t.Context(), archive, data, session.Options{SaveStore: store})
		if err != nil {
			t.Fatal(err)
		}
		defer restored.Close()
		check(t, restored, 0x44440004)
		lgtJavaSaveCaughtEverything(t, restored)
	}

	for _, surface := range lgtJavaSaveSurfaces {
		t.Run(surface.name, func(t *testing.T) {
			t.Run("after a save", func(t *testing.T) {
				store, _ := backend.NewMemorySaveStore(nil)
				s := surface.start(t, archive, store)
				lgtSaveAct(t, s, 0x11110001, testfixture.LGTSaveKeySave)
				data := lgtSaveCheckpoint(t, s, archive, backend.CheckpointLGTJava)
				frames := lgtSaveWord(t, s, testfixture.LGTCheckpointFrameCounter)
				lgtSaveSetWord(t, s, testfixture.LGTSaveProgress, 0x99990009)
				lgtSaveSetWord(t, s, testfixture.LGTJavaSaveSurface, 0x7f)
				lgtSaveTicks(t, s, 3)
				both(t, s, store, data, func(t *testing.T, restored *session.Session, _ uint32) {
					t.Helper()
					if lgtSaveWord(t, restored, testfixture.LGTCheckpointStartupCounter) != 1 ||
						lgtSaveWord(t, restored, testfixture.LGTCheckpointFrameCounter) != frames ||
						lgtSaveWord(t, restored, testfixture.LGTSaveProgress) != 0x11110001 ||
						lgtSaveWord(t, restored, testfixture.LGTJavaSaveSurface) != surface.surface {
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
					surface.wantStored(t, store, lgtSaveParts(0x22220002, 0x11110001), "patch after a load")
					// The store goes back to what the checkpoint was taken beside.
					lgtSaveAct(t, restored, 0x11110001, testfixture.LGTSaveKeySave)
					surface.wantStored(t, store, lgtSaveParts(0x11110001, 0x11110001), "save after a load")
				})
			})

			// What was open when the checkpoint was taken is open after the
			// load, as the same objects, and finishing through it completes
			// the save the checkpoint was taken in the middle of.
			t.Run("with a write held", func(t *testing.T) {
				store, _ := backend.NewMemorySaveStore(nil)
				s := surface.start(t, archive, store)
				lgtSaveAct(t, s, 0x11110001, testfixture.LGTSaveKeySave)
				lgtSaveAct(t, s, 0x22220002, testfixture.LGTSaveKeyHold)
				kept := lgtJavaSaveHeldWords(t, s)
				if kept == (lgtJavaSaveHeld{}) {
					t.Fatal("hold kept nothing")
				}
				data := lgtSaveCheckpoint(t, s, archive, backend.CheckpointLGTJava)
				both(t, s, store, data, func(t *testing.T, restored *session.Session, progress uint32) {
					t.Helper()
					if lgtJavaSaveHeldWords(t, restored) != kept {
						t.Fatal("the restored guest does not hold what it held")
					}
					lgtSaveAct(t, restored, progress, testfixture.LGTSaveKeyFinish)
					surface.wantStored(t, store, lgtSaveParts(0x22220002, progress), "finish after a load")
				})
			})

			if surface.surface == testfixture.LGTJavaSaveSurfaceDatabase {
				return
			}
			t.Run("with a truncation held", func(t *testing.T) {
				store, _ := backend.NewMemorySaveStore(nil)
				s := surface.start(t, archive, store)
				lgtSaveAct(t, s, 0x11110001, testfixture.LGTSaveKeySave)
				lgtSaveAct(t, s, 0x22220002, testfixture.LGTSaveKeyTruncate)
				kept := lgtJavaSaveHeldWords(t, s)
				data := lgtSaveCheckpoint(t, s, archive, backend.CheckpointLGTJava)
				both(t, s, store, data, func(t *testing.T, restored *session.Session, progress uint32) {
					t.Helper()
					if lgtJavaSaveHeldWords(t, restored) != kept {
						t.Fatal("the restored guest does not hold what it held")
					}
					lgtSaveAct(t, restored, progress, testfixture.LGTSaveKeyFinish)
					surface.wantStored(t, store, lgtSavePart(progress^testfixture.LGTSaveXOR), "finish after a load")
					// The next load is over a store that holds what this one
					// started over, and the title's own save is what puts it
					// there.
					lgtSaveAct(t, restored, 0x11110001, testfixture.LGTSaveKeySave)
				})
			})
		})
	}
}
