package session

import (
	"bytes"
	"encoding/binary"
	"errors"
	"strings"
	"testing"

	"github.com/movingwoo/wfeature/internal/backend"
)

// What a quick save and a quick load do to the game's own saves, said once for
// every checkpoint variant on the titles of save_title_test.go.
//
// The rule is one sentence: the game's saves come first. A checkpoint is
// execution state; a load brings the game back and leaves every save where it
// is, the restored game reads the saves as they are then, and what it writes
// lands on top of them. The one thing either step writes is what the running
// game had already written and the host had not yet stored.

const (
	saveFirst  uint32 = 0x11110001
	saveSecond uint32 = 0x22220002
	saveThird  uint32 = 0x33330003
	saveFourth uint32 = 0x44440004
	saveFifth  uint32 = 0x55550005
)

// The scenario the change exists for: quick save, play on and save in the
// game, quick load. The save made after the quick save is still the save, byte
// for byte; the game is back where the quick save was taken; it reads the
// later save; and its next write lands on that save.
//
// It is run twice: over the running game, and into a session that never ran
// the title, which is what a Host that was started again does with a slot.
func TestQuickLoadKeepsTheGamesSaves(t *testing.T) {
	for _, title := range saveTitles() {
		for _, mode := range []string{"over the running game", "into a new session"} {
			t.Run(title.name+"/"+mode, func(t *testing.T) {
				game := startSaveTitle(t, title)
				game.act(saveFirst, title.save)
				game.wantStored(title.parts(saveFirst, saveFirst), "the save made before the quick save")
				slot := game.quickSave()

				game.act(saveSecond, title.save)
				game.wantStored(title.parts(saveSecond, saveSecond), "the save made after the quick save")
				before, writes := game.tree(), game.watch.writes
				// The same platform session the running game is in, kept to ask
				// afterwards what became of it.
				displaced := *game.s
				if mode == "into a new session" {
					game.restart(slot)
				} else {
					game.quickLoad(slot)
					// The runtime the load displaced is cut off from the saves
					// and from the Host: nothing it still does can reach either.
					if displaced.Cheat() != nil {
						t.Fatal("the displaced runtime is still attached after the load")
					}
				}
				// Nothing was pending, so the load wrote nothing at all.
				if game.watch.writes != writes {
					t.Fatalf("the load made %d store writes", game.watch.writes-writes)
				}
				game.wantTree(before, "the load")

				if progress := game.word(title.progress); progress != saveFirst {
					t.Fatalf("the game's progress word is %#x after the load, want the quick save's %#x", progress, saveFirst)
				}
				if seen := game.readSave(); seen != title.found(saveSecond, saveSecond) {
					t.Fatalf("after the load the game read %+v, want the save made after the quick save", seen)
				}
				// Part A is written in place. Part B is still the later save's:
				// the write went onto the save as it is, not onto a copy of the
				// save as it was.
				game.act(saveThird, title.patch)
				game.wantStored(title.parts(saveThird, saveSecond), "a patch after the load")
				if seen := game.readSave(); seen != title.found(saveThird, saveSecond) {
					t.Fatalf("after the patch the game read %+v", seen)
				}
			})
		}
	}
}

// A checkpoint belongs to one archive. Loading another archive's over a running
// game is refused, whatever archive bytes it is offered with, and the running
// game and its saves are as they were.
func TestQuickLoadRefusesAnotherArchivesCheckpoint(t *testing.T) {
	titles := saveTitles()
	// Two titles of one variant, so that nothing but the archive tells them
	// apart: the two forms of the native package.
	running, other := startSaveTitle(t, titles[2]), startSaveTitle(t, titles[3])
	if running.title.variant != other.title.variant || bytes.Equal(running.archive, other.archive) {
		t.Fatal("the two titles are not two archives of one variant")
	}
	running.act(saveFirst, running.title.save)
	other.act(saveSecond, other.title.save)
	foreign := other.quickSave()

	before, writes, session := running.tree(), running.watch.writes, running.s
	for name, archive := range map[string][]byte{"with its own archive": other.archive, "with the running game's archive": running.archive} {
		if err := running.s.LoadCheckpoint(t.Context(), archive, foreign); !errors.Is(err, backend.ErrCheckpointIdentity) {
			t.Fatalf("another archive's checkpoint %s answered %v, want the identity refusal", name, err)
		}
	}
	if running.watch.writes != writes {
		t.Fatalf("the refused loads made %d store writes", running.watch.writes-writes)
	}
	running.wantTree(before, "the refused loads")
	if running.s != session || !running.s.Running() || running.word(running.title.progress) != saveFirst {
		t.Fatal("the refused loads displaced or stopped the running game")
	}
	if seen := running.readSave(); seen != running.title.found(saveFirst, saveFirst) {
		t.Fatalf("after the refused loads the running game read %+v", seen)
	}
}

// Loading twice is loading once: the second load finds the saves as the first
// left them, which is as they were.
func TestQuickLoadTwiceLeavesTheSameSaves(t *testing.T) {
	for _, title := range saveTitles() {
		t.Run(title.name, func(t *testing.T) {
			game := startSaveTitle(t, title)
			game.act(saveFirst, title.save)
			slot := game.quickSave()
			game.act(saveSecond, title.save)
			before, writes := game.tree(), game.watch.writes
			game.quickLoad(slot)
			game.quickLoad(slot)
			if game.watch.writes != writes {
				t.Fatalf("two loads made %d store writes", game.watch.writes-writes)
			}
			game.wantTree(before, "two loads")
			if seen := game.readSave(); seen != title.found(saveSecond, saveSecond) {
				t.Fatalf("after two loads the game read %+v", seen)
			}
		})
	}
}

// afterHold is the save on disk once a HOLD of part a has returned over a save
// of (b, b), and afterQuickStep once a quick save or a quick load has followed.
func (title saveTitle) afterHold(a, b uint32) []byte {
	if title.held == holdStored {
		return title.parts(a, b)
	}
	return title.parts(b, b)
}

func (title saveTitle) afterQuickStep(a, b uint32) []byte {
	if title.held == holdInStream {
		return title.parts(b, b)
	}
	return title.parts(a, b)
}

// A quick save writes one thing: what the game had written and the host had
// not stored. A checkpoint holds no save bytes, so a write that only the
// running session has would be nowhere once that session is gone. Where the
// platform stores every write as it is made there is nothing to store, and the
// quick save writes nothing at all. Either way it writes nothing the game did
// not issue: what a title has put in a stream and not flushed stays there.
func TestQuickSaveStoresOnlyWhatTheGameIssued(t *testing.T) {
	for _, title := range saveTitles() {
		t.Run(title.name, func(t *testing.T) {
			game := startSaveTitle(t, title)
			game.act(saveFirst, title.save)
			game.act(saveSecond, title.hold)
			game.wantStored(title.afterHold(saveSecond, saveFirst), "the hold")

			before, writes := game.tree(), game.watch.writes
			slot := game.quickSave()
			game.wantStored(title.afterQuickStep(saveSecond, saveFirst), "the quick save")
			if title.held == holdPending {
				if game.watch.writes != writes+1 {
					t.Fatalf("the quick save made %d store writes, want the one the game had issued", game.watch.writes-writes)
				}
				game.wantTreeBut(before, "the quick save")
			} else {
				if game.watch.writes != writes {
					t.Fatalf("the quick save made %d store writes with nothing pending", game.watch.writes-writes)
				}
				game.wantTree(before, "the quick save")
			}

			// Taken again, it has nothing left to store.
			before, writes = game.tree(), game.watch.writes
			game.quickSave()
			if game.watch.writes != writes {
				t.Fatalf("a second quick save made %d store writes", game.watch.writes-writes)
			}
			game.wantTree(before, "a second quick save")

			// The game goes on where it was, through what it kept open.
			game.act(saveThird, title.finish)
			game.wantStored(title.parts(saveSecond, saveThird), "finishing after the quick save")

			// And the checkpoint holds what was kept open: finishing through it
			// after a load completes the same save over what is there now.
			game.quickLoad(slot)
			game.act(saveFourth, title.finish)
			game.wantStored(title.parts(saveSecond, saveFourth), "finishing after the load")
		})
	}
}

// A quick load stores the running game's issued writes before anything else,
// for the same reason: the load displaces the only session that has them. The
// restored game then reads them, like any other save on disk.
func TestQuickLoadStoresTheRunningGamesIssuedWritesFirst(t *testing.T) {
	for _, title := range saveTitles() {
		t.Run(title.name, func(t *testing.T) {
			game := startSaveTitle(t, title)
			game.act(saveFirst, title.save)
			slot := game.quickSave()
			game.act(saveSecond, title.hold)
			game.wantStored(title.afterHold(saveSecond, saveFirst), "the hold")

			before, writes := game.tree(), game.watch.writes
			game.quickLoad(slot)
			game.wantStored(title.afterQuickStep(saveSecond, saveFirst), "the load")
			if title.held == holdPending {
				if game.watch.writes != writes+1 {
					t.Fatalf("the load made %d store writes, want the one the running game had issued", game.watch.writes-writes)
				}
				game.wantTreeBut(before, "the load")
			} else {
				if game.watch.writes != writes {
					t.Fatalf("the load made %d store writes with nothing pending", game.watch.writes-writes)
				}
				game.wantTree(before, "the load")
			}

			if progress := game.word(title.progress); progress != saveFirst {
				t.Fatalf("the game's progress word is %#x after the load, want %#x", progress, saveFirst)
			}
			want := title.found(saveSecond, saveFirst)
			if title.held == holdInStream {
				want = title.found(saveFirst, saveFirst)
			}
			if seen := game.readSave(); seen != want {
				t.Fatalf("after the load the game read %+v, want %+v", seen, want)
			}
			// The restored game kept nothing open, so it holds and finishes anew.
			game.act(saveThird, title.hold)
			game.act(saveFourth, title.finish)
			game.wantStored(title.parts(saveThird, saveFourth), "a hold and a finish after the load")
		})
	}
}

// A store that refuses the running game's writes refuses the key press: the
// quick save or the quick load does not happen, the game that was running is
// still the game, and its writes are still pending in it. Nothing on disk
// changed, and the same press succeeds once the store takes writes again.
func TestRefusedQuickStepsKeepTheRunningGamesWrites(t *testing.T) {
	for _, title := range saveTitles() {
		if title.held != holdPending {
			continue
		}
		for _, step := range []string{"quick save", "quick load"} {
			t.Run(title.name+"/"+step, func(t *testing.T) {
				game := startSaveTitle(t, title)
				game.act(saveFirst, title.save)
				slot := game.quickSave()
				game.act(saveSecond, title.hold)
				running := game.s

				before := game.tree()
				game.watch.refuse = true
				var err error
				if step == "quick save" {
					_, err = game.s.CaptureCheckpoint(t.Context())
				} else {
					err = game.s.LoadCheckpoint(t.Context(), game.archive, slot)
				}
				game.watch.refuse = false
				if !errors.Is(err, backend.ErrCheckpointSaveWrite) {
					t.Fatalf("the refused %s answered %v, want the save-write refusal", step, err)
				}
				game.wantTree(before, "the refused "+step)
				if game.s != running || !game.s.Running() || game.word(title.progress) != saveSecond {
					t.Fatalf("the refused %s displaced or stopped the running game", step)
				}

				// The write is still the running game's to finish.
				game.act(saveThird, title.finish)
				game.wantStored(title.parts(saveSecond, saveThird), "finishing after the refusal")
				// And the step itself is not spoiled by having been refused.
				game.act(saveFourth, title.hold)
				if step == "quick save" {
					game.quickSave()
				} else {
					game.quickLoad(slot)
				}
				game.wantStored(title.parts(saveFourth, saveThird), "the same "+step+" once the store takes writes")
			})
		}
	}
}

// A load that cannot read a save it needs is refused before the running game
// is displaced, and reverts nothing: the running game goes on, saves again,
// and the same load is accepted once the save can be read.
func TestRefusedQuickLoadRevertsNothing(t *testing.T) {
	for _, title := range saveTitles() {
		t.Run(title.name, func(t *testing.T) {
			game := startSaveTitle(t, title)
			game.act(saveFirst, title.save)
			game.act(saveSecond, title.hold)
			// The checkpoint has the save open, so a load has to read it.
			slot := game.quickSave()
			game.act(saveThird, title.finish)
			game.wantStored(title.parts(saveSecond, saveThird), "the finished save")
			running := game.s

			before, writes := game.tree(), game.watch.writes
			game.watch.unreadable = title.key
			err := game.s.LoadCheckpoint(t.Context(), game.archive, slot)
			game.watch.unreadable = ""
			if !errors.Is(err, backend.ErrCheckpointSaveRead) {
				t.Fatalf("the load answered %v, want the save-read refusal", err)
			}
			if game.watch.writes != writes {
				t.Fatalf("the refused load made %d store writes", game.watch.writes-writes)
			}
			game.wantTree(before, "the refused load")
			if game.s != running || !game.s.Running() {
				t.Fatal("the refused load displaced or stopped the running game")
			}

			game.act(saveFourth, title.save)
			game.wantStored(title.parts(saveFourth, saveFourth), "a save after the refused load")
			if seen := game.readSave(); seen != title.found(saveFourth, saveFourth) {
				t.Fatalf("after the refused load the running game read %+v", seen)
			}
			game.quickLoad(slot)
			if progress := game.word(title.progress); progress != saveSecond {
				t.Fatalf("the game's progress word is %#x after the load, want %#x", progress, saveSecond)
			}
		})
	}
}

// A delete is the game's own later act, like a save: a load leaves it. The
// restored game, which remembers a save, finds none, and saving makes one.
func TestQuickLoadAfterTheGameDeletedItsSave(t *testing.T) {
	for _, title := range saveTitles() {
		if title.remove == 0 {
			// The native file interface has no call that removes a file.
			continue
		}
		t.Run(title.name, func(t *testing.T) {
			game := startSaveTitle(t, title)
			game.act(saveFirst, title.save)
			slot := game.quickSave()
			game.press(title.remove)
			if seen := game.readSave(); seen != (saveSeen{Status: saveStatusMissing}) {
				t.Fatalf("after the delete the game read %+v", seen)
			}

			before, writes := game.tree(), game.watch.writes
			game.quickLoad(slot)
			if game.watch.writes != writes {
				t.Fatalf("the load made %d store writes", game.watch.writes-writes)
			}
			game.wantTree(before, "the load")
			if seen := game.readSave(); seen != (saveSeen{Status: saveStatusMissing}) {
				t.Fatalf("after the load the game read %+v, want no save", seen)
			}

			game.act(saveThird, title.save)
			game.wantStored(title.parts(saveThird, saveThird), "a save after the load")
			if seen := game.readSave(); seen != title.found(saveThird, saveThird) {
				t.Fatalf("after saving again the game read %+v", seen)
			}
		})
	}
}

// What the game had open when the quick save was taken is open after a load,
// on the save as it is now: at the same position, over the bytes on disk, and
// empty where the save is gone. The host makes nothing and trims nothing to
// get there; the game's next write through the object does.
//
// Each case changes the save after the quick save, loads, and finishes through
// what was kept. A surface with a position writes part B at byte four, where
// the hold left it. A stream writes what it still held of the hold and then
// part B, from the start of the file. A record store sets its second record.
func TestQuickLoadReopensWhatTheGameHadOpenOnTheSaveAsItIsNow(t *testing.T) {
	part := func(value uint32) []byte { return binary.LittleEndian.AppendUint32(nil, value) }
	for _, title := range saveTitles() {
		// held is a game with a save, part A rewritten through an object it
		// still has open, and the quick save taken there; the running game has
		// then finished through that object.
		held := func(t *testing.T) (*saveGame, []byte) {
			game := startSaveTitle(t, title)
			game.act(saveFirst, title.save)
			game.act(saveSecond, title.hold)
			slot := game.quickSave()
			game.act(saveThird, title.finish)
			game.wantStored(title.parts(saveSecond, saveThird), "the finished save")
			return game, slot
		}
		// load is the quick load, which must leave the disk as it finds it.
		load := func(t *testing.T, game *saveGame, slot []byte) {
			t.Helper()
			before, writes := game.tree(), game.watch.writes
			game.quickLoad(slot)
			if game.watch.writes != writes {
				t.Fatalf("the load made %d store writes", game.watch.writes-writes)
			}
			game.wantTree(before, "the load")
		}

		t.Run(title.name+"/a later save", func(t *testing.T) {
			game, slot := held(t)
			game.act(saveFourth, title.save)
			load(t, game, slot)
			game.act(saveFifth, title.finish)
			want := title.parts(saveFourth, saveFifth)
			if title.held == holdInStream {
				want = title.parts(saveSecond, saveFifth)
			}
			game.wantStored(want, "finishing after the load")
		})

		if title.truncate != 0 {
			t.Run(title.name+"/a shorter save", func(t *testing.T) {
				game, slot := held(t)
				// An emptied file finished with part B alone is four bytes long.
				game.act(saveFourth, title.truncate)
				game.act(saveFourth, title.finish)
				game.wantStored(part(saveFourth^title.mask), "the shorter save")
				load(t, game, slot)
				game.act(saveFifth, title.finish)
				// The position is past nothing now: part B lands right behind
				// the four bytes that are there.
				want := append(part(saveFourth^title.mask), part(saveFifth^title.mask)...)
				seen := saveSeen{A: saveFourth ^ title.mask, B: saveFifth ^ title.mask, Status: saveStatusFound, Length: 8}
				if title.held == holdInStream {
					want, seen = title.parts(saveSecond, saveFifth), title.found(saveSecond, saveFifth)
				}
				game.wantStored(want, "finishing after the load")
				if got := game.readSave(); got != seen {
					t.Fatalf("after finishing the game read %+v, want %+v", got, seen)
				}
			})
		}

		if title.remove != 0 && title.cursor {
			t.Run(title.name+"/a deleted save", func(t *testing.T) {
				game, slot := held(t)
				game.press(title.remove)
				load(t, game, slot)
				if seen := game.readSave(); seen != (saveSeen{Status: saveStatusMissing}) {
					t.Fatalf("after the load the game read %+v, want no save", seen)
				}
				game.act(saveFifth, title.finish)
				// The object is empty and its position is still four, so the
				// write makes the save again with nothing before it.
				want := append(part(0), part(saveFifth^title.mask)...)
				seen := saveSeen{B: saveFifth ^ title.mask, Status: saveStatusFound, Length: 8}
				if title.held == holdInStream {
					want, seen = title.parts(saveSecond, saveFifth), title.found(saveSecond, saveFifth)
				}
				game.wantStored(want, "finishing after the load")
				if got := game.readSave(); got != seen {
					t.Fatalf("after finishing the game read %+v, want %+v", got, seen)
				}
			})
		}
	}
}

// Validating a checkpoint reaches for no save: it runs against a placeholder
// store, and the live store enters at the commit alone. A session says so in
// its log when that is not true, and no variant does, with a save kept open or
// without.
func TestQuickLoadValidatesWithoutTheSaves(t *testing.T) {
	for _, title := range saveTitles() {
		t.Run(title.name, func(t *testing.T) {
			var log bytes.Buffer
			logged := title
			logged.options = func(options Options) Options {
				options = title.options(options)
				options.Logger = backend.NewLogger(&log)
				return options
			}
			game := startSaveTitle(t, logged)
			game.act(saveFirst, title.save)
			plain := game.quickSave()
			game.act(saveSecond, title.hold)
			holding := game.quickSave()
			for _, slot := range [][]byte{plain, holding} {
				game.quickLoad(slot)
				game.restart(slot)
			}
			if strings.Contains(log.String(), "checkpoint validation asked the save store") {
				t.Fatalf("validating a checkpoint reached for a save:\n%s", log.String())
			}
		})
	}
}
