package session

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/cheat"
	"github.com/movingwoo/wfeature/internal/platform/ktf"
	"github.com/movingwoo/wfeature/internal/testfixture"
)

// The storage fixtures through the shared session
//
// internal/testfixture authors one title for every checkpoint variant that
// keeps a save of two four-byte parts and acts on it once per key: write both
// parts, patch the first, read both back, delete, hold the save open after
// writing the first part, open it emptied without writing, and finish through
// what was kept. A test changes the progress word between keys, so it can tell
// which key wrote which part.
//
// saveTitle is one of those titles with what a Host test needs to drive it the
// way a Host drives any title: started by Start, sent keys by SendKey, ticked,
// and asked for guest words through the cheat engine. The tests built on it
// say what a quick save and a quick load do to the game's own saves, and they
// say it once for every variant, because the rule has no variants.

// saveTitleHold is where the part a HOLD writes is when the key returns.
type saveTitleHold int

const (
	// holdPending: the title issued the write and the host still holds it, in
	// an open file's buffer or a table of unstored names. A quick step stores
	// it first.
	holdPending saveTitleHold = iota
	// holdStored: the platform stores a write before the call returns.
	holdStored
	// holdInStream: the write is in a stream the title has not flushed, which
	// by the platform's own contract is not a write to the file yet. A quick
	// step leaves it where it is.
	holdInStream
)

type saveTitle struct {
	name    string
	variant uint16
	build   func() ([]byte, error)
	owner   string
	options func(Options) Options
	// ready runs once on a session that has just started.
	ready func(t *testing.T, s *Session)
	// tick is true where a key reaches the title at a tick.
	tick bool

	progress, seenA, seenB, status, length uint32
	// remove and truncate are zero where the surface has no such call.
	save, patch, read, remove, hold, truncate, finish int32
	mask                                              uint32

	// key is the save's entry in the store, and content what that entry holds
	// as the save's own bytes: the file as it is, or a record container's
	// records one after another.
	key     string
	content func(t *testing.T, stored []byte) []byte
	held    saveTitleHold
	// cursor is false for a surface whose kept object has no position: a
	// record store, where FINISH sets a record by number.
	cursor bool
}

func (title saveTitle) parts(a, b uint32) []byte {
	return binary.LittleEndian.AppendUint32(binary.LittleEndian.AppendUint32(nil, a), b^title.mask)
}

// saveTitleRecords joins the records of the container the LGT platform keeps a
// DataBase in: a sixteen-byte header whose last word is the record count, then
// one length-prefixed record each, a length of all ones standing for a record
// that was deleted.
func saveTitleRecords(t *testing.T, container []byte) []byte {
	t.Helper()
	if len(container) < 16 || string(container[:4]) != "WFDB" {
		t.Fatalf("the stored database is not a container: %x", container)
	}
	joined, rest := []byte{}, container[16:]
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
		joined, rest = append(joined, rest[:length]...), rest[length:]
	}
	return joined
}

func saveTitles() []saveTitle {
	whole := func(_ *testing.T, stored []byte) []byte { return stored }
	manual := func(options Options) Options {
		options.Clock = ktf.NewManualClock(time.Unix(1, 0))
		return options
	}
	native := func(name string, build func() ([]byte, error)) saveTitle {
		return saveTitle{
			name: name, variant: backend.CheckpointKTFNative, build: build, owner: testfixture.KTFNativeSaveOwner,
			options: func(options Options) Options {
				options = manual(options)
				options.Width, options.Height = 32, 48
				return options
			},
			progress: testfixture.KTFNativeSaveProgress, seenA: testfixture.KTFNativeSaveSeenA, seenB: testfixture.KTFNativeSaveSeenB,
			status: testfixture.KTFNativeSaveStatus, length: testfixture.KTFNativeSaveLength,
			save: testfixture.KTFNativeSaveKeySave, patch: testfixture.KTFNativeSaveKeyPatch, read: testfixture.KTFNativeSaveKeyRead,
			hold: testfixture.KTFNativeSaveKeyHold, truncate: testfixture.KTFNativeSaveKeyTruncate, finish: testfixture.KTFNativeSaveKeyFinish,
			mask: testfixture.KTFNativeSaveMask, key: testfixture.KTFNativeSaveStoreKey, content: whole, held: holdPending, cursor: true,
		}
	}
	descriptor := func(name string, variant uint16, build func() ([]byte, error), words [5]uint32) saveTitle {
		return saveTitle{
			name: name, variant: variant, build: build, options: manual,
			progress: words[0], seenA: words[1], seenB: words[2], status: words[3], length: words[4],
			save: testfixture.KTFSaveActionSave, patch: testfixture.KTFSaveActionPatch, read: testfixture.KTFSaveActionRead,
			remove: testfixture.KTFSaveActionDelete, hold: testfixture.KTFSaveActionHold,
			truncate: testfixture.KTFSaveActionTruncate, finish: testfixture.KTFSaveActionFinish,
			mask: testfixture.KTFSaveMask, key: testfixture.KTFSaveStoreKey, content: whole, held: holdStored, cursor: true,
		}
	}
	lgt := func(name string, variant uint16, build func() ([]byte, error), owner string) saveTitle {
		return saveTitle{
			name: name, variant: variant, build: build, owner: owner, tick: true,
			options:  func(options Options) Options { return options },
			progress: testfixture.LGTSaveProgress, seenA: testfixture.LGTSaveSeenA, seenB: testfixture.LGTSaveSeenB,
			status: testfixture.LGTSaveStatus, length: testfixture.LGTSaveLength,
			save: testfixture.LGTSaveKeySave, patch: testfixture.LGTSaveKeyPatch, read: testfixture.LGTSaveKeyRead,
			remove: testfixture.LGTSaveKeyDelete, hold: testfixture.LGTSaveKeyHold,
			truncate: testfixture.LGTSaveKeyTruncate, finish: testfixture.LGTSaveKeyFinish,
			mask: testfixture.LGTSaveXOR, key: testfixture.LGTSaveFileKey, content: whole, held: holdPending, cursor: true,
		}
	}
	java := func(name string, surface uint32) saveTitle {
		title := lgt(name, backend.CheckpointLGTJava, testfixture.LGTJavaSaveArchive, testfixture.LGTJavaSaveOwner)
		title.ready = func(t *testing.T, s *Session) {
			t.Helper()
			saveTitleSetWord(t, s, testfixture.LGTJavaSaveSurface, surface)
		}
		return title
	}
	stream := java("lgt java stream", testfixture.LGTJavaSaveSurfaceStream)
	stream.held = holdInStream
	database := java("lgt java database", testfixture.LGTJavaSaveSurfaceDatabase)
	database.key, database.content, database.held = testfixture.LGTJavaSaveDatabaseKey, saveTitleRecords, holdStored
	database.truncate, database.cursor = 0, false
	return []saveTitle{
		descriptor("ktf java", backend.CheckpointKTFJava, testfixture.KTFSaveArchive, [5]uint32{
			testfixture.KTFSaveProgress, testfixture.KTFSaveSeenA, testfixture.KTFSaveSeenB, testfixture.KTFSaveStatus, testfixture.KTFSaveLength}),
		descriptor("ktf module", backend.CheckpointKTFModule, testfixture.KTFModuleSaveArchive, [5]uint32{
			testfixture.KTFModuleSaveProgress, testfixture.KTFModuleSaveSeenA, testfixture.KTFModuleSaveSeenB,
			testfixture.KTFModuleSaveStatus, testfixture.KTFModuleSaveLength}),
		native("ktf native", testfixture.KTFNativeSaveArchive),
		native("ktf native without a frame callback", testfixture.KTFNativeSaveArchiveWithoutFrame),
		lgt("lgt clet", backend.CheckpointLGTClet, testfixture.LGTSaveArchive, testfixture.LGTSaveOwner),
		java("lgt java file", testfixture.LGTJavaSaveSurfaceFile),
		stream,
		database,
	}
}

func saveTitleWord(t *testing.T, s *Session, address uint32) uint32 {
	t.Helper()
	data, err := s.Cheat().ReadBytes(address, 4)
	if err != nil {
		t.Fatal(err)
	}
	return binary.LittleEndian.Uint32(data)
}

func saveTitleSetWord(t *testing.T, s *Session, address, value uint32) {
	t.Helper()
	if err := s.Cheat().WriteValue(address, cheat.ValueType{Kind: cheat.KindU32, Endian: cheat.Little}, int64(value)); err != nil {
		t.Fatal(err)
	}
}

// saveStoreWatch is the store a title's session runs over in these tests: the
// directory store a Host uses, counting the writes that reach it, and failing
// every write or the read of one key when a test asks, the way a disk that is
// full or damaged does.
type saveStoreWatch struct {
	base       *backend.DirectorySaveStore
	writes     int
	refuse     bool
	unreadable string
}

var errSaveStoreWatch = errors.New("the test store refuses this")

func (watch *saveStoreWatch) blocked(name string) bool {
	key, err := backend.NormalizeSaveKey(name)
	return err == nil && watch.unreadable != "" && key == watch.unreadable
}

func (watch *saveStoreWatch) LoadSave(name string) ([]byte, bool) {
	data, found, _ := watch.ReadSave(name)
	return data, found
}

func (watch *saveStoreWatch) ReadSave(name string) ([]byte, bool, error) {
	if watch.blocked(name) {
		return nil, false, errSaveStoreWatch
	}
	return watch.base.ReadSave(name)
}

func (watch *saveStoreWatch) ReadSaveLimit(name string, limit int64) ([]byte, bool, error) {
	if watch.blocked(name) {
		return nil, false, errSaveStoreWatch
	}
	return watch.base.ReadSaveLimit(name, limit)
}

func (watch *saveStoreWatch) StoreSave(name string, data []byte) error {
	watch.writes++
	if watch.refuse {
		return errSaveStoreWatch
	}
	return watch.base.StoreSave(name, data)
}

func (watch *saveStoreWatch) StoreSaves(entries map[string][]byte) error {
	watch.writes++
	if watch.refuse {
		return errSaveStoreWatch
	}
	return watch.base.StoreSaves(entries)
}

// saveGame is a started title beside the save directory it runs over.
type saveGame struct {
	t       *testing.T
	title   saveTitle
	archive []byte
	// root is the save root, and directory the title's own folder in it.
	root, directory string
	watch           *saveStoreWatch
	options         Options
	s               *Session
}

func startSaveTitle(t *testing.T, title saveTitle) *saveGame {
	t.Helper()
	archive, err := title.build()
	if err != nil {
		t.Fatal(err)
	}
	summary, err := Inspect(archive)
	if err != nil {
		t.Fatal(err)
	}
	if summary.SaveOwner == "" || title.owner != "" && summary.SaveOwner != title.owner {
		t.Fatalf("the archive's save owner is %q, want %q", summary.SaveOwner, title.owner)
	}
	root := t.TempDir()
	directory := filepath.Join(root, summary.SaveOwner)
	game := &saveGame{t: t, title: title, archive: archive, root: root, directory: directory,
		watch: &saveStoreWatch{base: backend.NewDirectorySaveStore(directory)}}
	game.options = title.options(Options{SaveStore: game.watch})
	game.s, err = Start(t.Context(), archive, game.options)
	if err != nil {
		t.Fatalf("start through the ordinary loader: %v", err)
	}
	t.Cleanup(func() { game.s.Close() })
	if !game.s.CanCheckpoint() {
		t.Fatal("the title cannot be checkpointed")
	}
	if title.tick {
		game.ticks(2)
	}
	if title.ready != nil {
		title.ready(t, game.s)
	}
	return game
}

func (game *saveGame) ticks(count int) {
	game.t.Helper()
	for range count {
		if _, err := game.s.Tick(game.t.Context(), 0); err != nil {
			game.t.Fatal(err)
		}
	}
}

func (game *saveGame) word(address uint32) uint32 {
	game.t.Helper()
	return saveTitleWord(game.t, game.s, address)
}

// press is one key press as a Host sends one: down and up, and the tick that
// delivers each where the platform hands a key over at a tick.
func (game *saveGame) press(key int32) {
	game.t.Helper()
	if key == 0 {
		game.t.Fatal("the title has no key for this action")
	}
	for _, action := range []string{KeyPress, KeyRelease} {
		if err := game.s.SendKey(game.t.Context(), action, key); err != nil {
			game.t.Fatalf("key %q %s: %v", rune(key), action, err)
		}
		if game.title.tick {
			game.ticks(1)
		}
	}
}

// act sets the progress word and presses a key, which is one action.
func (game *saveGame) act(progress uint32, key int32) {
	game.t.Helper()
	saveTitleSetWord(game.t, game.s, game.title.progress, progress)
	game.press(key)
}

// saveSeen is what a READ left in the guest.
type saveSeen struct {
	A, B, Status, Length uint32
}

// These fixtures all answer one for a save they found and zero for none.
const (
	saveStatusMissing = 0
	saveStatusFound   = 1
)

func (title saveTitle) found(a, b uint32) saveSeen {
	return saveSeen{A: a, B: b ^ title.mask, Status: saveStatusFound, Length: 8}
}

// readSave presses the read key with a progress word that is none of the saved
// ones, so what comes back can only have come from the save.
func (game *saveGame) readSave() saveSeen {
	game.t.Helper()
	game.act(0xdeadbeef, game.title.read)
	return saveSeen{A: game.word(game.title.seenA), B: game.word(game.title.seenB),
		Status: game.word(game.title.status), Length: game.word(game.title.length)}
}

// stored is the save as the directory holds it now, read from the file itself
// rather than through the store a session uses.
func (game *saveGame) stored() ([]byte, bool) {
	game.t.Helper()
	data, err := os.ReadFile(filepath.Join(game.directory, filepath.FromSlash(game.title.key)))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, false
	}
	if err != nil {
		game.t.Fatal(err)
	}
	return game.title.content(game.t, data), true
}

func (game *saveGame) wantStored(want []byte, when string) {
	game.t.Helper()
	if data, found := game.stored(); !found || !bytes.Equal(data, want) {
		game.t.Fatalf("%s: the save on disk is %x (found %t), want %x", when, data, found, want)
	}
}

// tree is every file under the save root with its bytes: the title's folder
// and the reserved directory beside it, where the slots and the locks are.
func (game *saveGame) tree() map[string]string {
	game.t.Helper()
	files := make(map[string]string)
	err := filepath.WalkDir(game.root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(game.root, path)
		if err != nil {
			return err
		}
		if entry.IsDir() {
			files[filepath.ToSlash(relative)+"/"] = ""
			return nil
		}
		data, err := os.ReadFile(path)
		files[filepath.ToSlash(relative)] = string(data)
		return err
	})
	if err != nil {
		game.t.Fatal(err)
	}
	return files
}

// wantTree fails when the save root is not, file for file and byte for byte,
// what it was.
func (game *saveGame) wantTree(before map[string]string, when string) {
	game.t.Helper()
	after := game.tree()
	for path, data := range before {
		now, kept := after[path]
		if !kept {
			game.t.Errorf("%s: %s is gone", when, path)
		} else if now != data {
			game.t.Errorf("%s: %s changed from %x to %x", when, path, data, now)
		}
	}
	for path := range after {
		if _, was := before[path]; !was {
			game.t.Errorf("%s: %s appeared", when, path)
		}
	}
	if game.t.Failed() {
		game.t.FailNow()
	}
}

// wantTreeBut is wantTree with one file allowed to differ: the title's save.
func (game *saveGame) wantTreeBut(before map[string]string, when string) {
	game.t.Helper()
	save := filepath.ToSlash(filepath.Join(filepath.Base(game.directory), filepath.FromSlash(game.title.key)))
	now, found := game.tree()[save]
	if !found {
		game.t.Fatalf("%s: the save is not on disk", when)
	}
	settled := make(map[string]string, len(before))
	for path, data := range before {
		settled[path] = data
	}
	settled[save] = now
	game.wantTree(settled, when)
}

// quickSave takes a checkpoint the way a Host does and answers its bytes.
func (game *saveGame) quickSave() []byte {
	game.t.Helper()
	data, err := game.s.CaptureCheckpoint(game.t.Context())
	if err != nil {
		game.t.Fatalf("quick save: %v", err)
	}
	checkpoint, err := backend.DecodeCheckpoint(data, backend.SaveIdentity(game.archive))
	if err != nil || checkpoint.Variant != game.title.variant {
		game.t.Fatalf("the quick save is variant %d (%v), want %d", checkpoint.Variant, err, game.title.variant)
	}
	return data
}

// quickLoad loads a checkpoint over the running game.
func (game *saveGame) quickLoad(data []byte) {
	game.t.Helper()
	if err := game.s.LoadCheckpoint(game.t.Context(), game.archive, data); err != nil {
		game.t.Fatalf("quick load: %v", err)
	}
}

// restart closes the running game and restores the checkpoint into a session
// that never ran the title, the way a Host that was started again does.
func (game *saveGame) restart(data []byte) {
	game.t.Helper()
	game.s.Close()
	restored, err := RestoreCheckpoint(game.t.Context(), game.archive, data, game.options)
	if err != nil {
		game.t.Fatalf("restore into a new session: %v", err)
	}
	game.s = restored
}
