package webhost

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/cheat"
	"github.com/movingwoo/wfeature/internal/session"
	"github.com/movingwoo/wfeature/internal/testfixture"
	"github.com/movingwoo/wfeature/internal/wsproto"
)

// Quick save, quick load and the game's saves, as the web host carries them.
//
// internal/session says what the two steps do to a save on every checkpoint
// variant. These tests are about what this host adds: the slot file beside the
// save folder, the sentences a person reads when a step is refused, the start
// that restores from a slot, and what an earlier build left on disk. They run
// on two of the authored storage titles, one whose platform stores a write as
// it is made and one whose writes wait in an open file's buffer; the other
// variants meet the same host code and are covered where they differ, in
// internal/session and in the platform packages.

// saveHostTitle is one storage fixture with what these tests need of it.
type saveHostTitle struct {
	name, platform string
	build          func() ([]byte, error)
	// tick is true where a key reaches the title at a tick.
	tick bool

	progress, seenA, seenB          uint32
	save, patch, read, hold, finish int32
	mask                            uint32
	key                             string
	// pending is true where the part a hold writes is still in the host when
	// the key returns.
	pending bool
}

var saveHostTitles = []saveHostTitle{
	{
		name: "ktf java", platform: "ktf", build: testfixture.KTFSaveArchive,
		progress: testfixture.KTFSaveProgress, seenA: testfixture.KTFSaveSeenA, seenB: testfixture.KTFSaveSeenB,
		save: testfixture.KTFSaveActionSave, patch: testfixture.KTFSaveActionPatch, read: testfixture.KTFSaveActionRead,
		hold: testfixture.KTFSaveActionHold, finish: testfixture.KTFSaveActionFinish,
		mask: testfixture.KTFSaveMask, key: testfixture.KTFSaveStoreKey,
	},
	{
		name: "lgt clet", platform: "lgt", build: testfixture.LGTSaveArchive, tick: true,
		progress: testfixture.LGTSaveProgress, seenA: testfixture.LGTSaveSeenA, seenB: testfixture.LGTSaveSeenB,
		save: testfixture.LGTSaveKeySave, patch: testfixture.LGTSaveKeyPatch, read: testfixture.LGTSaveKeyRead,
		hold: testfixture.LGTSaveKeyHold, finish: testfixture.LGTSaveKeyFinish,
		mask: testfixture.LGTSaveXOR, key: testfixture.LGTSaveFileKey, pending: true,
	},
}

func (title saveHostTitle) parts(a, b uint32) []byte {
	return binary.LittleEndian.AppendUint32(binary.LittleEndian.AppendUint32(nil, a), b^title.mask)
}

// saveHost is a server with one storage title in its library, and a runner
// that plays it the way a connection's goroutine does.
type saveHost struct {
	t       *testing.T
	title   saveHostTitle
	root    string
	archive []byte
	game    string
	log     *bytes.Buffer
	server  *Server
	r       *sessionRunner
	// directory is the title's save folder, and file its save in it.
	directory, file string
	next            uint64
}

func newSaveHost(t *testing.T, title saveHostTitle) *saveHost {
	t.Helper()
	archive, err := title.build()
	if err != nil {
		t.Fatal(err)
	}
	summary, err := session.Inspect(archive)
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	writeCheckpointGame(t, root, title.platform, archive)
	host := &saveHost{t: t, title: title, root: root, archive: archive, game: "games/" + title.platform + "/checkpoint.zip", log: &bytes.Buffer{}}
	host.server = newTestServer(t, Options{GameRoot: filepath.Join(root, "games"), SaveRoot: filepath.Join(root, "saves", "ktf"),
		LogRoot: filepath.Join(root, "logs"), Logger: backend.NewLogger(host.log)})
	host.directory = host.server.saveDirectory(title.platform, summary.SaveOwner)
	if host.directory == "" {
		t.Fatal("the title has no save folder")
	}
	host.file = filepath.Join(host.directory, filepath.FromSlash(title.key))
	host.r = &sessionRunner{server: host.server, frames: make(chan pendingFrame, 1), outText: make(chan outboundMessage, 256)}
	t.Cleanup(host.r.stopGame)
	return host
}

// slot is where the host keeps the title's quick save, under the current
// format's name or under the name an earlier build used.
func (host *saveHost) slot(legacy bool) string {
	name := fmt.Sprintf("%x.v2.wfq", backend.SaveIdentity(host.archive))
	if legacy {
		name = fmt.Sprintf("%x.wfq", backend.SaveIdentity(host.archive))
	}
	return filepath.Join(filepath.Dir(host.directory), ".wfeature-quicksave", "owners", filepath.Base(host.directory), name)
}

// request sends one message to the runner and answers its reply: the message
// that carries the request's ID.
func (host *saveHost) request(message clientMessage) serverMessage {
	host.t.Helper()
	host.next++
	message.ID = host.next
	if timelineCommand(message.Kind) {
		message.Epoch = host.r.outputEpoch.Load()
	}
	host.r.handle(host.t.Context(), message)
	for _, reply := range readCheckpointReplies(host.t, host.r) {
		if reply.ID == message.ID {
			return reply
		}
	}
	host.t.Fatalf("no reply to %s", message.Kind)
	return serverMessage{}
}

// start starts the title, from its quick save when asked.
func (host *saveHost) start(quickLoad bool) serverMessage {
	host.t.Helper()
	reply := host.request(clientMessage{Kind: clientStart, Game: host.game, QuickLoad: quickLoad})
	if reply.Kind == serverStarted && host.title.tick {
		host.ticks(2)
	}
	return reply
}

func (host *saveHost) mustStart() {
	host.t.Helper()
	if reply := host.start(false); reply.Kind != serverStarted || host.r.game == nil || !host.r.started.CanCheckpoint {
		host.t.Fatalf("the title did not start: %+v", reply)
	}
}

func (host *saveHost) ticks(count int) {
	host.t.Helper()
	for range count {
		if _, err := host.r.game.Tick(host.t.Context(), 0); err != nil {
			host.t.Fatal(err)
		}
	}
}

func (host *saveHost) word(address uint32) uint32 {
	host.t.Helper()
	data, err := host.r.game.Cheat().ReadBytes(address, 4)
	if err != nil {
		host.t.Fatal(err)
	}
	return binary.LittleEndian.Uint32(data)
}

// act sets the progress word and presses a key through the runner, which is
// one action of the title.
func (host *saveHost) act(progress uint32, key int32) {
	host.t.Helper()
	word := cheat.ValueType{Kind: cheat.KindU32, Endian: cheat.Little}
	if err := host.r.game.Cheat().WriteValue(host.title.progress, word, int64(progress)); err != nil {
		host.t.Fatal(err)
	}
	for _, action := range []string{session.KeyPress, session.KeyRelease} {
		host.r.handle(host.t.Context(), clientMessage{Kind: clientKey, Action: action, Code: key, Epoch: host.r.outputEpoch.Load()})
		if host.title.tick {
			host.ticks(1)
		}
	}
	if replies := readCheckpointReplies(host.t, host.r); len(replies) != 0 {
		host.t.Fatalf("a key answered %+v", replies)
	}
}

func (host *saveHost) wantStored(want []byte, when string) {
	host.t.Helper()
	if data, err := os.ReadFile(host.file); err != nil || !bytes.Equal(data, want) {
		host.t.Fatalf("%s: the save on disk is %x (%v), want %x", when, data, err, want)
	}
}

func (host *saveHost) quickSave() serverMessage {
	host.t.Helper()
	return host.request(clientMessage{Kind: clientQuickSave})
}

func (host *saveHost) quickLoad() serverMessage {
	host.t.Helper()
	return host.request(clientMessage{Kind: clientQuickLoad})
}

func (host *saveHost) mustQuickSave() {
	host.t.Helper()
	if reply := host.quickSave(); reply.Kind != serverResult {
		host.t.Fatalf("quick save: %+v", reply)
	}
}

func (host *saveHost) mustQuickLoad() {
	host.t.Helper()
	if reply := host.quickLoad(); reply.Kind != serverRestored {
		host.t.Fatalf("quick load: %+v", reply)
	}
}

// saveTree is every file under a directory with its bytes.
func saveTree(t *testing.T, root string) map[string]string {
	t.Helper()
	files := make(map[string]string)
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if entry.IsDir() {
			files[filepath.ToSlash(relative)+"/"] = ""
			return nil
		}
		data, err := os.ReadFile(path)
		if errors.Is(err, fs.ErrPermission) {
			// A file a test made unreadable is compared by its presence.
			files[filepath.ToSlash(relative)] = "unreadable"
			return nil
		}
		files[filepath.ToSlash(relative)] = string(data)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}

// wantSaveTree fails when a directory is not, file for file and byte for
// byte, what it was. added names the entries that are allowed to be new.
func wantSaveTree(t *testing.T, root string, before map[string]string, when string, added func(path string) bool) {
	t.Helper()
	after := saveTree(t, root)
	for path, data := range before {
		now, kept := after[path]
		if !kept {
			t.Errorf("%s: %s is gone", when, path)
		} else if now != data {
			t.Errorf("%s: %s changed from %x to %x", when, path, data, now)
		}
	}
	for path := range after {
		if _, was := before[path]; !was && (added == nil || !added(path)) {
			t.Errorf("%s: %s appeared", when, path)
		}
	}
	if t.Failed() {
		t.FailNow()
	}
}

// saves is the root every platform's save folders are under.
func (host *saveHost) saves() string { return filepath.Join(host.root, "saves") }

// unreadableSave takes every permission off the title's save file, which makes
// the store fail to read it, the way a damaged disk does. The function it
// answers gives them back. A system that ignores the bits, or an administrator
// who is not held to them, cannot make the case, and the test is skipped.
func (host *saveHost) unreadableSave() func() {
	host.t.Helper()
	if runtime.GOOS == "windows" {
		host.t.Skip("this system does not refuse a read for the permission bits")
	}
	if err := os.Chmod(host.file, 0); err != nil {
		host.t.Fatal(err)
	}
	if file, err := os.Open(host.file); err == nil {
		file.Close()
		_ = os.Chmod(host.file, 0o644)
		host.t.Skip("this system reads a file without read permission")
	}
	return func() {
		host.t.Helper()
		if err := os.Chmod(host.file, 0o644); err != nil {
			host.t.Fatal(err)
		}
	}
}

// blockSave puts a directory where the title's save file is, which makes the
// store fail to write that save, the way a damaged disk does; a read finds no
// save there. The function it answers puts the file back.
func (host *saveHost) blockSave() func() {
	host.t.Helper()
	data, err := os.ReadFile(host.file)
	if err != nil {
		host.t.Fatal(err)
	}
	if err := os.Remove(host.file); err != nil {
		host.t.Fatal(err)
	}
	if err := os.Mkdir(host.file, 0o755); err != nil {
		host.t.Fatal(err)
	}
	return func() {
		host.t.Helper()
		if err := os.Remove(host.file); err != nil {
			host.t.Fatal(err)
		}
		if err := os.WriteFile(host.file, data, 0o644); err != nil {
			host.t.Fatal(err)
		}
	}
}

// The scenario the change exists for, through the runner's own commands: quick
// save, save in the game, quick load. The folder is what it was, the page is
// told the game was restored, and the game reads and writes the later save.
func TestQuickLoadLeavesTheGamesSavesInTheirFolder(t *testing.T) {
	for _, title := range saveHostTitles {
		t.Run(title.name, func(t *testing.T) {
			host := newSaveHost(t, title)
			host.mustStart()
			host.act(0x11110001, title.save)
			host.mustQuickSave()
			if _, err := os.Stat(host.slot(false)); err != nil {
				t.Fatalf("the quick save is not beside the save folder: %v", err)
			}
			host.act(0x22220002, title.save)
			host.wantStored(title.parts(0x22220002, 0x22220002), "the save made after the quick save")

			before := saveTree(t, host.saves())
			reply := host.quickLoad()
			if reply.Kind != serverRestored || reply.Started == nil || !reply.Started.Restored || !reply.Started.HasCheckpoint {
				t.Fatalf("quick load: %+v", reply)
			}
			wantSaveTree(t, host.saves(), before, "the quick load", nil)
			if progress := host.word(title.progress); progress != 0x11110001 {
				t.Fatalf("the game's progress word is %#x after the load", progress)
			}
			host.act(0xdeadbeef, title.read)
			if a, b := host.word(title.seenA), host.word(title.seenB); a != 0x22220002 || b != 0x22220002^title.mask {
				t.Fatalf("after the load the game read %#x and %#x, want the save made after the quick save", a, b)
			}
			host.act(0x33330003, title.patch)
			host.wantStored(title.parts(0x33330003, 0x22220002), "a patch after the load")

			// Stopping and starting from the slot is the same load.
			host.r.stopGame()
			before = saveTree(t, host.saves())
			if reply := host.start(true); reply.Kind != serverRestored || !reply.Started.Restored {
				t.Fatalf("start from the quick save: %+v", reply)
			}
			wantSaveTree(t, host.saves(), before, "the start from the quick save", nil)
			host.act(0xdeadbeef, title.read)
			if a, b := host.word(title.seenA), host.word(title.seenB); a != 0x33330003 || b != 0x22220002^title.mask {
				t.Fatalf("after the start from the quick save the game read %#x and %#x", a, b)
			}
		})
	}
}

// The refusals a person can act on reach the page as a sentence in the page's
// language, and the cause — which names the file and what the store answered —
// goes to the log. In every case the game that was running is still running,
// nothing on disk has changed, and the same press works once the cause is gone.
func TestQuickStepRefusalsAreWordedForThePage(t *testing.T) {
	const (
		saveWrite = "게임이 쓰던 세이브를 디스크에 기록하지 못해 퀵세이브를 중단했습니다. 게임은 그대로입니다."
		loadWrite = "게임이 쓰던 세이브를 디스크에 기록하지 못해 퀵로드를 중단했습니다. 게임은 그대로입니다."
		loadRead  = "세이브를 읽지 못해 퀵로드를 중단했습니다. 게임은 그대로입니다."
		startRead = "세이브를 읽지 못해 퀵로드를 중단했습니다."
	)
	// refused checks one refusal: the reply, the log, the disk and the game.
	refused := func(t *testing.T, host *saveHost, reply serverMessage, sentence string, before map[string]string, running *session.Session) {
		t.Helper()
		if reply.Kind != serverError || reply.Message != sentence {
			t.Fatalf("the refusal reads %+v, want %q", reply, sentence)
		}
		if !strings.Contains(host.log.String(), "checkpoint refused") || !strings.Contains(host.log.String(), filepath.Base(host.file)) {
			t.Fatalf("the log does not name the save the store refused:\n%s", host.log.String())
		}
		wantSaveTree(t, host.saves(), before, "the refusal", nil)
		if running != nil && (host.r.game != running || !host.r.game.Running() || host.r.outputEpoch.Load() != 0) {
			t.Fatal("the refusal displaced or stopped the running game, or reset the page's output")
		}
	}

	for _, title := range saveHostTitles {
		if title.pending {
			// A write the title issued and the host still holds is stored by
			// either step first, and a disk that refuses it refuses the step.
			for _, step := range []string{"quick save", "quick load"} {
				t.Run(title.name+"/the disk refuses a pending write at a "+step, func(t *testing.T) {
					host := newSaveHost(t, title)
					host.mustStart()
					host.act(0x11110001, title.save)
					host.mustQuickSave()
					host.act(0x22220002, title.hold)
					host.wantStored(title.parts(0x11110001, 0x11110001), "the hold")
					restore := host.blockSave()
					before, running := saveTree(t, host.saves()), host.r.game
					if step == "quick save" {
						refused(t, host, host.quickSave(), saveWrite, before, running)
					} else {
						refused(t, host, host.quickLoad(), loadWrite, before, running)
					}
					restore()
					// The write is still the running game's, and the step works.
					if step == "quick save" {
						host.mustQuickSave()
					} else {
						host.mustQuickLoad()
					}
					host.wantStored(title.parts(0x22220002, 0x11110001), "the same "+step+" on a disk that takes the write")
				})
			}
		}

		t.Run(title.name+"/a save a quick load cannot read", func(t *testing.T) {
			host := newSaveHost(t, title)
			host.mustStart()
			host.act(0x11110001, title.save)
			host.act(0x22220002, title.hold)
			// The slot has the save open, so a load has to read it.
			host.mustQuickSave()
			host.act(0x33330003, title.finish)
			host.wantStored(title.parts(0x22220002, 0x33330003), "the finished save")
			restore := host.unreadableSave()
			before, running := saveTree(t, host.saves()), host.r.game
			refused(t, host, host.quickLoad(), loadRead, before, running)

			// A start from the slot meets the same save, with no game to keep.
			host.r.stopGame()
			before = saveTree(t, host.saves())
			refused(t, host, host.start(true), startRead, before, nil)
			if host.r.game != nil {
				t.Fatal("a refused start left a game")
			}
			restore()
			if reply := host.start(true); reply.Kind != serverRestored {
				t.Fatalf("the start from the quick save once the save can be read: %+v", reply)
			}
			host.act(0x44440004, title.finish)
			host.wantStored(title.parts(0x22220002, 0x44440004), "finishing after the start from the quick save")
		})
	}
}

// A quick save an earlier build wrote cannot be loaded, and it is never
// converted, rewritten or removed. The page still offers the load, so that the
// person who asks is told why; a new quick save is written beside the old file
// and loads.
func TestQuickLoadRefusesAnEarlierBuildsSlotWithAReason(t *testing.T) {
	const sentence = "이 퀵세이브는 이전 빌드 형식이라 불러올 수 없습니다. 파일은 그대로 두었으니 새로 퀵세이브해 주세요."
	for _, title := range saveHostTitles {
		t.Run(title.name, func(t *testing.T) {
			host := newSaveHost(t, title)
			// What an earlier build left: its envelope under its own file name.
			// Nothing here reads it, so the bytes only have to be recognisable.
			earlier := append([]byte("WFSTATE\x00\x01\x00"), bytes.Repeat([]byte{0xa5}, 4096)...)
			if err := os.MkdirAll(filepath.Dir(host.slot(true)), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(host.slot(true), earlier, 0o600); err != nil {
				t.Fatal(err)
			}
			unchanged := func(when string) {
				t.Helper()
				if data, err := os.ReadFile(host.slot(true)); err != nil || !bytes.Equal(data, earlier) {
					t.Fatalf("%s: the earlier build's quick save was touched (%v)", when, err)
				}
			}

			// A start from it is refused before anything is started.
			if reply := host.start(true); reply.Kind != serverError || reply.Message != sentence || host.r.game != nil {
				t.Fatalf("a start from the earlier build's quick save: %+v", reply)
			}
			unchanged("a refused start")

			host.mustStart()
			if !host.r.started.HasCheckpoint {
				t.Fatal("the page is not offered the load, so nobody would be told why it cannot be done")
			}
			if !strings.Contains(host.log.String(), "an earlier build left files") || !strings.Contains(host.log.String(), "earlier_quick_save=true") {
				t.Fatalf("the start did not say that an earlier build's quick save is there:\n%s", host.log.String())
			}
			host.act(0x11110001, title.save)
			before, running := saveTree(t, host.saves()), host.r.game
			if reply := host.quickLoad(); reply.Kind != serverError || reply.Message != sentence {
				t.Fatalf("a load of the earlier build's quick save: %+v", reply)
			}
			if host.r.game != running || !running.Running() {
				t.Fatal("the refused load displaced or stopped the running game")
			}
			wantSaveTree(t, host.saves(), before, "the refused load", nil)
			if !strings.Contains(host.log.String(), filepath.Base(host.slot(true))) {
				t.Fatalf("the log does not name the file that was left in place:\n%s", host.log.String())
			}

			// A new quick save goes beside it under the current name.
			host.mustQuickSave()
			unchanged("a new quick save")
			if _, err := os.Stat(host.slot(false)); err != nil {
				t.Fatalf("the new quick save: %v", err)
			}
			host.act(0x22220002, title.save)
			host.mustQuickLoad()
			unchanged("a load of the new quick save")
			host.wantStored(title.parts(0x22220002, 0x22220002), "the save after the load")
		})
	}
}

// An earlier build's quick load set the saves it replaced aside, in a
// directory called "previous" beside the save folder. They may be the newest
// saves a person made before that load, and nothing knows whether they are
// newer than the saves in use, so this build never restores or removes them:
// it says in the log that they are there.
func TestSessionLeavesAnEarlierBuildsPreviousAlone(t *testing.T) {
	for _, title := range saveHostTitles {
		t.Run(title.name, func(t *testing.T) {
			host := newSaveHost(t, title)
			previous := filepath.Join(filepath.Dir(host.slot(false)), "previous")
			for name, data := range map[string]string{
				title.key: "the save an earlier quick load set aside", "fs/.removed": "gone.dat\n", "fs/other/deep.bin": "\x00\x01\x02",
			} {
				path := filepath.Join(previous, filepath.FromSlash(name))
				if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			before := saveTree(t, previous)

			host.mustStart()
			if !strings.Contains(host.log.String(), "an earlier build left files") || !strings.Contains(host.log.String(), "saves_set_aside_by_a_quick_load=true") {
				t.Fatalf("the start did not say that saves were set aside:\n%s", host.log.String())
			}
			if _, err := os.Stat(host.file); !errors.Is(err, fs.ErrNotExist) {
				t.Fatalf("starting put the set-aside save back into the save folder: %v", err)
			}
			host.act(0x11110001, title.save)
			host.mustQuickSave()
			host.act(0x22220002, title.save)
			host.mustQuickLoad()
			host.act(0x33330003, title.save)
			host.r.stopGame()
			wantSaveTree(t, previous, before, "a session with a quick save and a quick load", nil)
			host.wantStored(title.parts(0x33330003, 0x33330003), "the save in use")
		})
	}
}

// A start that restores from the quick save is refused while the game the page
// is running is still running, when the refusal is already certain: no slot, a
// slot an earlier build wrote, a damaged one, one taken from another archive.
// The running game keeps its saves claimed and goes on.
func TestStartFromAQuickSaveIsRefusedBeforeTheRunningGameIsStopped(t *testing.T) {
	title := saveHostTitles[0]
	other, err := testfixture.KTFCheckpointArchive()
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name string
		// slot leaves the slot in the state the start will find it in, given a
		// valid quick save of the title.
		slot func(t *testing.T, host *saveHost, valid []byte)
		want string
	}{
		{"no quick save", func(t *testing.T, host *saveHost, _ []byte) {
			if err := os.Remove(host.slot(false)); err != nil {
				t.Fatal(err)
			}
		}, "no quick save exists for this archive"},
		{"an earlier build's quick save", func(t *testing.T, host *saveHost, valid []byte) {
			if err := os.Rename(host.slot(false), host.slot(true)); err != nil {
				t.Fatal(err)
			}
		}, "이 퀵세이브는 이전 빌드 형식이라 불러올 수 없습니다. 파일은 그대로 두었으니 새로 퀵세이브해 주세요."},
		{"a damaged quick save", func(t *testing.T, host *saveHost, valid []byte) {
			damaged := bytes.Clone(valid)
			damaged[len(damaged)-1] ^= 1
			if err := os.WriteFile(host.slot(false), damaged, 0o600); err != nil {
				t.Fatal(err)
			}
		}, backend.ErrCheckpointDamaged.Error()},
		{"another archive's quick save", func(t *testing.T, host *saveHost, valid []byte) {
			checkpoint, err := backend.DecodeCheckpoint(valid, backend.SaveIdentity(host.archive))
			if err != nil {
				t.Fatal(err)
			}
			checkpoint.Identity = backend.SaveIdentity(other)
			foreign, err := backend.EncodeCheckpoint(checkpoint)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(host.slot(false), foreign, 0o600); err != nil {
				t.Fatal(err)
			}
		}, backend.ErrCheckpointIdentity.Error()},
	} {
		t.Run(test.name, func(t *testing.T) {
			host := newSaveHost(t, title)
			host.mustStart()
			host.act(0x11110001, title.save)
			host.mustQuickSave()
			valid, err := os.ReadFile(host.slot(false))
			if err != nil {
				t.Fatal(err)
			}
			test.slot(t, host, valid)
			host.act(0x22220002, title.hold)
			running, token := host.r.game, host.r.token

			reply := host.start(true)
			if reply.Kind != serverError || reply.Message != test.want {
				t.Fatalf("the start answered %+v, want %q", reply, test.want)
			}
			if host.r.game != running || !running.Running() || host.r.token != token {
				t.Fatal("the refused start stopped the game the page was running")
			}
			if ok, _ := host.server.holdSaveDirectory(host.directory, "another tool", false); ok {
				t.Fatal("the refused start let go of the running game's saves")
			}
			// The running game still has what it kept open.
			host.act(0x33330003, title.finish)
			host.wantStored(title.parts(0x22220002, 0x33330003), "finishing after the refused start")
		})
	}
}

// Only the slot is checked ahead of the start. An archive that cannot be read
// is the start's own refusal, worded and handled as for any start: the page
// asked for another game, so the one it was running has ended.
func TestStartFromAQuickSaveLeavesOtherRefusalsToTheStart(t *testing.T) {
	host := newSaveHost(t, saveHostTitles[0])
	answers := make(map[bool]serverMessage)
	for _, quickLoad := range []bool{false, true} {
		host.mustStart()
		host.mustQuickSave()
		reply := host.request(clientMessage{Kind: clientStart, Game: "games/ktf/missing.zip", QuickLoad: quickLoad})
		if reply.Kind != serverError || host.r.game != nil {
			t.Fatalf("a start of a missing archive (from the quick save: %t) answered %+v and left a game: %t", quickLoad, reply, host.r.game != nil)
		}
		answers[quickLoad] = reply
	}
	if answers[true].Message != answers[false].Message || answers[true].Exited != answers[false].Exited {
		t.Fatalf("a start from the quick save answered %+v for a missing archive, and a plain start %+v", answers[true], answers[false])
	}
}

// Saves as the released 0.5.1 leaves them — plain files under the save folder,
// dotted list keys, nested directories, an empty file, a temporary file a
// killed process left — are read where they are and stay byte for byte through
// a session that quick saves and quick loads. The one thing added is the
// reserved directory beside the save folder, where the slot and the locks are.
func TestOrdinarySavesFromTheReleasedLayoutAreUntouched(t *testing.T) {
	for _, title := range saveHostTitles {
		t.Run(title.name, func(t *testing.T) {
			host := newSaveHost(t, title)
			released := map[string]string{
				title.key:                  string(title.parts(0x0a0b0c0d, 0x0a0b0c0d)),
				"fs/.removed":              "old.dat\n",
				"fs/dir/nested/record.bin": "\x01\x02\x03\x04\x05",
				"fs/empty.dat":             "",
				"fs/." + filepath.Base(title.key) + ".1234567890": "half a write",
				"db/scores": "\x00\x00\x00\x01",
			}
			for name, data := range released {
				path := filepath.Join(host.directory, filepath.FromSlash(name))
				if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			before := saveTree(t, host.saves())
			reserved := func(path string) bool {
				return strings.HasPrefix(path, title.platform+"/.wfeature-quicksave/")
			}

			host.mustStart()
			// The game reads the save the release left.
			host.act(0xdeadbeef, title.read)
			if a, b := host.word(title.seenA), host.word(title.seenB); a != 0x0a0b0c0d || b != 0x0a0b0c0d^title.mask {
				t.Fatalf("the game read %#x and %#x from the released save", a, b)
			}
			host.mustQuickSave()
			host.mustQuickLoad()
			host.r.stopGame()
			if reply := host.start(true); reply.Kind != serverRestored {
				t.Fatalf("start from the quick save: %+v", reply)
			}
			host.r.stopGame()
			wantSaveTree(t, host.saves(), before, "a session over the released layout", reserved)
			if strings.Contains(host.log.String(), "an earlier build left files") {
				t.Fatalf("ordinary saves were reported as an earlier build's leftovers:\n%s", host.log.String())
			}

			// And the game's own save still lands in the same file.
			host.mustStart()
			host.act(0x11110001, title.save)
			host.wantStored(title.parts(0x11110001, 0x11110001), "a save over the released layout")
		})
	}
}

var consoleValue = regexp.MustCompile(`= (\d+)`)

// socketWord reads one guest word through the page's console.
func socketWord(t *testing.T, connection *wsproto.Conn, epoch uint64, address uint32) uint32 {
	t.Helper()
	answer := checkpointConsole(t, connection, epoch, fmt.Sprintf("read 0x%x u32", address))
	match := consoleValue.FindStringSubmatch(answer)
	if match == nil {
		t.Fatalf("the console answered %q", answer)
	}
	value, err := strconv.ParseUint(match[1], 10, 32)
	if err != nil {
		t.Fatalf("the console answered %q", answer)
	}
	return uint32(value)
}

// The same scenario over a real connection, where the runner ticks the game on
// its own clock and every command is a message: the two ends of the protocol
// agree on what a quick load is.
func TestQuickLoadLeavesSavesOverTheSocket(t *testing.T) {
	for _, title := range saveHostTitles {
		t.Run(title.name, func(t *testing.T) {
			host := newSaveHost(t, title)
			connection, _ := dialCheckpointServer(t, host.server)
			epoch := uint64(0)
			// act is one action of the title, and waits for the save it leaves:
			// a key reaches this title at a tick the runner makes when it is due.
			act := func(progress uint32, key int32, want []byte) {
				t.Helper()
				checkpointConsole(t, connection, epoch, fmt.Sprintf("set 0x%x %d u32", title.progress, progress))
				for _, action := range []string{session.KeyPress, session.KeyRelease} {
					send(t, connection, clientMessage{Kind: clientKey, Action: action, Code: key, Epoch: epoch})
				}
				deadline := time.Now().Add(20 * time.Second)
				for {
					data, err := os.ReadFile(host.file)
					if err == nil && bytes.Equal(data, want) {
						return
					}
					if time.Now().After(deadline) {
						t.Fatalf("the save on disk is %x (%v), want %x", data, err, want)
					}
					time.Sleep(5 * time.Millisecond)
				}
			}

			send(t, connection, clientMessage{Kind: clientStart, Game: host.game, ID: 1})
			if started := expectMessage(t, connection, serverStarted); started.Started == nil || !started.Started.CanCheckpoint || started.Started.HasCheckpoint {
				t.Fatalf("the title did not start as one that can be quick saved: %+v", started)
			}
			act(0x11110001, title.save, title.parts(0x11110001, 0x11110001))
			send(t, connection, clientMessage{Kind: clientQuickSave, ID: 2})
			if reply := expectMessage(t, connection, serverResult); reply.ID != 2 {
				t.Fatalf("quick save: %+v", reply)
			}
			act(0x22220002, title.save, title.parts(0x22220002, 0x22220002))

			before := saveTree(t, host.saves())
			send(t, connection, clientMessage{Kind: clientQuickLoad, ID: 3})
			restored := expectMessage(t, connection, serverRestored)
			if restored.ID != 3 || restored.Epoch != 1 || restored.Started == nil || !restored.Started.Restored {
				t.Fatalf("quick load: %+v", restored)
			}
			epoch = 1
			wantSaveTree(t, host.saves(), before, "the quick load", nil)
			if progress := socketWord(t, connection, epoch, title.progress); progress != 0x11110001 {
				t.Fatalf("the game's progress word is %#x after the load", progress)
			}
			// The restored game reads the save made after the quick save: it
			// patches part A and leaves that save's part B where it is.
			act(0x33330003, title.patch, title.parts(0x33330003, 0x22220002))
			send(t, connection, clientMessage{Kind: clientStop, ID: 4, Epoch: epoch})
			expectMessage(t, connection, serverResult)
		})
	}
}
