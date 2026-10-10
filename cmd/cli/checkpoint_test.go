package main

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/serve"
	"github.com/movingwoo/wfeature/internal/session"
	"github.com/movingwoo/wfeature/internal/testfixture"
)

func checkpointSKTCLIArchive(t *testing.T, kind string) []byte {
	t.Helper()
	if kind == "java" {
		archive, err := os.ReadFile(filepath.Join("..", "..", "internal", "platform", "skt", "testdata", "checkpoint-skt.zip"))
		if err != nil {
			t.Fatal(err)
		}
		return archive
	}
	// Authored SGS: load a counter and show white; each key saves its next
	// value and shows black. Shutdown has no save side effect.
	data := make([]byte, 52)
	data[0] = 1
	copy(data[10:26], "Host checkpoint")
	for index, code := range [][]byte{
		{5, 16, 5, 1, 0x98, 0x55, 0x78, 0xff}, {0xff}, {0xff},
		{0x3a, 16, 1, 5, 16, 5, 1, 0x99, 0x56, 0x78, 0xff},
	} {
		binary.LittleEndian.PutUint16(data[28+index*2:], uint16(len(data)))
		data = append(data, code...)
	}
	variables := len(data)
	for range 17 {
		data = append(data, 1, 1, 0, 0)
	}
	for i, offset := range []int{variables, len(data), len(data), len(data)} {
		binary.LittleEndian.PutUint16(data[44+i*2:], uint16(offset))
	}
	var descriptor []byte
	for _, value := range []string{"application/x-gnex-sgs", "SGS"} {
		descriptor = binary.LittleEndian.AppendUint32(descriptor, uint32(len(value)))
		descriptor = append(descriptor, value...)
	}
	var archive bytes.Buffer
	writer := zip.NewWriter(&archive)
	for _, entry := range []struct {
		name string
		data []byte
	}{{"checkpoint.mod", descriptor}, {"checkpoint.sgs", data}} {
		file, err := writer.Create(entry.name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := file.Write(entry.data); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return archive.Bytes()
}

func checkpointSKTCLIFixture(t *testing.T, kind string) (saveCLIFixture, *backend.DirectorySaveStore) {
	t.Helper()
	archive := checkpointSKTCLIArchive(t, kind)
	summary, err := session.Inspect(archive)
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	fixture := saveCLIFixture{path: filepath.Join(root, "checkpoint.zip"), saveRoot: filepath.Join(root, "saves"), archive: archive}
	fixture.slots = filepath.Join(fixture.saveRoot, ".wfeature-quicksave", "owners", summary.SaveOwner)
	if err := os.WriteFile(fixture.path, archive, 0600); err != nil {
		t.Fatal(err)
	}
	return fixture, backend.NewDirectorySaveStore(filepath.Join(fixture.saveRoot, summary.SaveOwner))
}

func TestCheckpointSKTCLILiveCommandsSurviveInvalidLoad(t *testing.T) {
	for _, kind := range []string{"java", "sgs"} {
		t.Run(kind, func(t *testing.T) {
			fixture, store := checkpointSKTCLIFixture(t, kind)
			seed, err := session.Start(t.Context(), fixture.archive, session.Options{SaveStore: store})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(seed.Close)
			if !seed.CanCheckpoint() {
				t.Fatal("SKT fixture does not advertise checkpoints")
			}
			data, err := seed.CaptureCheckpoint(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			identity := backend.SaveIdentity(fixture.archive)
			if err := store.StoreCheckpoint(identity, data); err != nil {
				t.Fatal(err)
			}
			seed.Close()
			data[len(data)-1] ^= 1
			if err := os.WriteFile(filepath.Join(fixture.slots, fmt.Sprintf("%x.v3.wfq", identity)), data, 0600); err != nil {
				t.Fatal(err)
			}
			commands := `{"cmd":"quickload"}
{"cmd":"key","key":"1","action":"press"}
{"cmd":"quicksave"}
{"cmd":"screen"}
{"cmd":"key","key":"1","action":"release"}
{"cmd":"key","key":"2","action":"press"}
{"cmd":"key","key":"2","action":"release"}
{"cmd":"quickload"}
{"cmd":"quicksave"}
{"cmd":"quit"}
`
			replies := serveSaveCLI(t, fixture, commands)
			for i, reply := range replies {
				if reply.OK != (i != 0) || reply.Ended {
					t.Fatalf("SKT command %d = %+v", i, reply)
				}
			}
			if replies[0].Error == "" || replies[3].Screen == nil || replies[7].Digest != replies[2].Digest {
				t.Fatal("invalid load was not refused or the saved frame was not restored")
			}
			data, found, err := store.LoadCheckpoint(identity)
			if err != nil || !found {
				t.Fatalf("SKT CLI did not replace the damaged slot: %v, %t", err, found)
			}
			restored, err := session.RestoreCheckpoint(t.Context(), fixture.archive, data, session.Options{SaveStore: store})
			if err != nil {
				t.Fatal(err)
			}
			defer restored.Close()
			if !restored.CanCheckpoint() || !restored.Running() || restored.Paused() || !reflect.DeepEqual(restored.HeldKeys(), []int32{'1'}) {
				t.Fatalf("CLI restored incorrect execution/input state: %v", restored.HeldKeys())
			}
			if _, err := restored.Tick(t.Context(), 0); err != nil {
				t.Fatalf("restored CLI execution cannot continue: %v", err)
			}
		})
	}
}

func TestCheckpointSKTCLIPausedStartupResumesThroughPark(t *testing.T) {
	for _, kind := range []string{"java", "sgs"} {
		t.Run(kind, func(t *testing.T) {
			fixture, store := checkpointSKTCLIFixture(t, kind)
			game, err := session.Start(t.Context(), fixture.archive, session.Options{SaveStore: store})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(game.Close)
			if err := game.Pause(t.Context()); err != nil {
				t.Fatal(err)
			}
			data, err := game.CaptureCheckpoint(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			identity := backend.SaveIdentity(fixture.archive)
			if err := store.StoreCheckpoint(identity, data); err != nil {
				t.Fatal(err)
			}
			game.Close()
			replies := serveSaveCLI(t, fixture, "{\"cmd\":\"step\"}\n{\"cmd\":\"park\",\"ms\":0}\n{\"cmd\":\"step\"}\n{\"cmd\":\"quicksave\"}\n{\"cmd\":\"quit\"}\n", "-quickload")
			for i, reply := range replies {
				if !reply.OK || reply.Ended || i == 0 && !reply.Stalled {
					t.Fatalf("paused SKT command %d = %+v", i, reply)
				}
			}
			data, _, err = store.LoadCheckpoint(identity)
			if err != nil {
				t.Fatal(err)
			}
			restored, err := session.RestoreCheckpoint(t.Context(), fixture.archive, data, session.Options{SaveStore: store})
			if err != nil {
				t.Fatal(err)
			}
			defer restored.Close()
			if restored.Paused() || !restored.Running() {
				t.Fatal("park did not resume the paused SKT checkpoint")
			}
		})
	}
}

func checkpointCLIFixture(t *testing.T) (path, root string, archive []byte) {
	t.Helper()
	archive, err := testfixture.KTFCheckpointArchive()
	if err != nil {
		t.Fatal(err)
	}
	root = t.TempDir()
	path = filepath.Join(root, "fixture.zip")
	if err := os.WriteFile(path, archive, 0600); err != nil {
		t.Fatal(err)
	}
	return path, root, archive
}

func TestCheckpointCLILiveCommandsAndRefusal(t *testing.T) {
	path, root, archive := checkpointCLIFixture(t)
	saveRoot := filepath.Join(root, "saves")
	commands := `{"cmd":"quickload"}
{"cmd":"key","key":"1","action":"press"}
{"cmd":"quicksave"}
{"cmd":"key","key":"1","action":"release"}
{"cmd":"quickload"}
{"cmd":"quicksave"}
{"cmd":"quit"}
`
	var output, diagnostics bytes.Buffer
	if code := runShared(t.Context(), path, []string{"-save", saveRoot, "-serve"}, strings.NewReader(commands), &output, &diagnostics); code != 0 {
		t.Fatalf("CLI = %d: %s", code, diagnostics.String())
	}
	decoder := json.NewDecoder(&output)
	for i := 0; i < 7; i++ {
		var response serve.Response
		if err := decoder.Decode(&response); err != nil {
			t.Fatal(err)
		}
		if response.OK != (i != 0) {
			t.Fatalf("command %d = %+v", i, response)
		}
	}
	store := backend.NewDirectorySaveStore(filepath.Join(saveRoot, "P0001"))
	data, found, err := store.LoadCheckpoint(backend.SaveIdentity(archive))
	if err != nil || !found {
		t.Fatalf("CLI did not persist its slot: %v, %t", err, found)
	}
	game, err := session.RestoreCheckpoint(t.Context(), archive, data, session.Options{SaveStore: store})
	if err != nil {
		t.Fatal(err)
	}
	defer game.Close()
	if !reflect.DeepEqual(game.HeldKeys(), []int32{49}) {
		t.Fatal("CLI load failed to restore the saved input hold")
	}
	var counter [4]byte
	if err := game.KTF().Client.Core().Memory().Read(testfixture.KTFCheckpointStartupCounter, counter[:]); err != nil || binary.LittleEndian.Uint32(counter[:]) != 1 {
		t.Fatalf("live CLI load replayed startup: %x, %v", counter, err)
	}
	// The Host claim ends only after runtime close, and must not leak on quit.
	release, err := backend.ClaimSaveDirectory(filepath.Join(saveRoot, "P0001"))
	if err != nil {
		t.Fatal(err)
	}
	release()
}

func TestCheckpointCLIPausedRestoreCanResumeThroughPark(t *testing.T) {
	path, root, archive := checkpointCLIFixture(t)
	saveRoot := filepath.Join(root, "saves")
	store := backend.NewDirectorySaveStore(filepath.Join(saveRoot, "P0001"))
	game, err := session.Start(t.Context(), archive, session.Options{SaveStore: store})
	if err != nil {
		t.Fatal(err)
	}
	defer game.Close()
	if err := game.Pause(t.Context()); err != nil {
		t.Fatal(err)
	}
	data, err := game.CaptureCheckpoint(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.StoreCheckpoint(backend.SaveIdentity(archive), data); err != nil {
		t.Fatal(err)
	}
	game.Close()
	commands := `{"cmd":"step"}
{"cmd":"park","ms":0}
{"cmd":"step"}
{"cmd":"quicksave"}
{"cmd":"quit"}
`
	var output, diagnostics bytes.Buffer
	if code := runShared(t.Context(), path, []string{"-save", saveRoot, "-serve", "-quickload"}, strings.NewReader(commands), &output, &diagnostics); code != 0 {
		t.Fatalf("paused CLI = %d: %s", code, diagnostics.String())
	}
	decoder := json.NewDecoder(&output)
	for i := range 5 {
		var response serve.Response
		if err := decoder.Decode(&response); err != nil {
			t.Fatal(err)
		}
		if !response.OK || response.Ended {
			t.Fatalf("command %d = %+v", i, response)
		}
		if i == 0 && !response.Stalled {
			t.Fatal("paused step did not report its stopped clock")
		}
	}
	data, _, err = store.LoadCheckpoint(backend.SaveIdentity(archive))
	if err != nil {
		t.Fatal(err)
	}
	resumed, err := session.RestoreCheckpoint(t.Context(), archive, data, session.Options{SaveStore: store})
	if err != nil {
		t.Fatal(err)
	}
	defer resumed.Close()
	if resumed.Paused() {
		t.Fatal("CLI park command did not resume the restored session")
	}
}

func TestCheckpointCLIProcess(t *testing.T) {
	testCheckpointCLIProcess(t, "TestCheckpointCLIProcess", testfixture.KTFCheckpointStartupCounter, testfixture.KTFCheckpointArchive)
}

func TestCheckpointNativeCLIProcess(t *testing.T) {
	testCheckpointCLIProcess(t, "TestCheckpointNativeCLIProcess", testfixture.KTFNativeCheckpointStartupCounter, testfixture.KTFNativeCheckpointArchive)
}

func TestCheckpointLGTCLIProcess(t *testing.T) {
	testCheckpointCLIProcess(t, "TestCheckpointLGTCLIProcess", testfixture.LGTCheckpointStartupCounter, testfixture.LGTCheckpointArchive)
}

func testCheckpointCLIProcess(t *testing.T, testName string, counterAddress uint32, build func() ([]byte, error)) {
	if root := os.Getenv("WFEATURE_CLI_CHECKPOINT_ROOT"); root != "" {
		args := []string{"run", filepath.Join(root, "fixture.zip"), "-save", filepath.Join(root, "saves"), "-quickload", "-ticks", "0", "-quicksave"}
		if code := run(args, os.Stdout, os.Stderr); code != 0 {
			t.Fatalf("restarted CLI = %d", code)
		}
		return
	}
	archive, err := build()
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "fixture.zip"), archive, 0600); err != nil {
		t.Fatal(err)
	}
	summary, err := session.Inspect(archive)
	if err != nil {
		t.Fatal(err)
	}
	store := backend.NewDirectorySaveStore(filepath.Join(root, "saves", summary.SaveOwner))
	if err := store.StoreSave("progress", []byte("checkpoint progress")); err != nil {
		t.Fatal(err)
	}
	game, err := session.Start(t.Context(), archive, session.Options{SaveStore: store})
	if err != nil {
		t.Fatal(err)
	}
	game.CheatConsole().Execute(fmt.Sprintf("set 0x%x 7 u32", counterAddress))
	if counter, err := game.Cheat().ReadBytes(counterAddress, 4); err != nil || binary.LittleEndian.Uint32(counter) != 7 {
		t.Fatal("source did not change its guest counter")
	}
	data, err := game.CaptureCheckpoint(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	identity := backend.SaveIdentity(archive)
	if err := store.StoreCheckpoint(identity, data); err != nil {
		t.Fatal(err)
	}
	game.Close()
	if err := store.StoreSave("progress", []byte("later progress")); err != nil {
		t.Fatal(err)
	}
	program := os.Getenv("WFEATURE_CLI_CHECKPOINT_RESTORE_BINARY")
	if program == "" {
		program = os.Args[0]
	}
	process := exec.CommandContext(t.Context(), program, "-test.run=^"+testName+"$", "-test.timeout=20s")
	process.Env = append(os.Environ(), "WFEATURE_CLI_CHECKPOINT_ROOT="+root)
	if output, err := process.CombinedOutput(); err != nil {
		t.Fatalf("CLI process: %v: %s", err, output)
	}
	// The restart brought the guest back and left the save written after the
	// checkpoint as it was.
	if progress, _ := store.LoadSave("progress"); string(progress) != "later progress" {
		t.Fatalf("CLI restart with -quickload changed the ordinary save: %q", progress)
	}
	data, _, err = store.LoadCheckpoint(identity)
	if err != nil {
		t.Fatal(err)
	}
	restored, err := session.RestoreCheckpoint(t.Context(), archive, data, session.Options{SaveStore: store})
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	counter, err := restored.Cheat().ReadBytes(counterAddress, 4)
	if err != nil || binary.LittleEndian.Uint32(counter) != 7 {
		t.Fatalf("restarted CLI replayed guest startup: %x, %v", counter, err)
	}
}

func TestCheckpointCLIClaimsAndMissingSlotLeaveSavesUntouched(t *testing.T) {
	path, root, _ := checkpointCLIFixture(t)
	saveRoot := filepath.Join(root, "saves")
	directory := filepath.Join(saveRoot, "P0001")
	store := backend.NewDirectorySaveStore(directory)
	if err := store.StoreSave("progress", []byte("original")); err != nil {
		t.Fatal(err)
	}
	release, err := backend.ClaimSaveDirectory(directory)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	var output, diagnostics bytes.Buffer
	if code := runShared(t.Context(), path, []string{"-save", saveRoot, "-ticks", "0"}, nil, &output, &diagnostics); code != 1 || !strings.Contains(diagnostics.String(), "in use") {
		t.Fatalf("CLI started under another Host: %d: %s", code, diagnostics.String())
	}
	output.Reset()
	diagnostics.Reset()
	if code := runKTF(path, []string{"-save", saveRoot, "-ticks", "1"}, &output, &diagnostics); code != 1 || !strings.Contains(diagnostics.String(), "in use") {
		t.Fatalf("legacy CLI started under another Host: %d: %s", code, diagnostics.String())
	}
	release()
	output.Reset()
	diagnostics.Reset()
	if code := runShared(t.Context(), path, []string{"-save", saveRoot, "-quickload", "-ticks", "0"}, nil, &output, &diagnostics); code != 1 || !strings.Contains(diagnostics.String(), "no checkpoint") {
		t.Fatalf("missing slot was not refused: %d: %s", code, diagnostics.String())
	}
	if data, _ := store.LoadSave("progress"); string(data) != "original" {
		t.Fatal("failed startup restore changed ordinary saves")
	}
	release, err = backend.ClaimSaveDirectory(directory)
	if err != nil {
		t.Fatal("failed CLI startup leaked its claim")
	}
	release()
}

// The LGT Clet is driven through the same command: live quick save and load
// between steps, a refused load that leaves the session running, and a slot a
// later process can start from. Without -play its ticks run back to back, so
// the frame count is the step count and not a function of how long the test
// machine took.
func TestCheckpointLGTCLILiveCommands(t *testing.T) {
	archive, err := testfixture.LGTCheckpointArchive()
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	path := filepath.Join(root, "fixture.zip")
	if err := os.WriteFile(path, archive, 0600); err != nil {
		t.Fatal(err)
	}
	saveRoot := filepath.Join(root, "saves")
	commands := `{"cmd":"quickload"}
{"cmd":"step","ticks":4}
{"cmd":"key","key":"1","action":"press"}
{"cmd":"quicksave"}
{"cmd":"key","key":"1","action":"release"}
{"cmd":"step","ticks":6}
{"cmd":"quickload"}
{"cmd":"step","ticks":2}
{"cmd":"quicksave"}
{"cmd":"quit"}
`
	var output, diagnostics bytes.Buffer
	if code := runShared(t.Context(), path, []string{"-save", saveRoot, "-serve"}, strings.NewReader(commands), &output, &diagnostics); code != 0 {
		t.Fatalf("CLI = %d: %s", code, diagnostics.String())
	}
	decoder := json.NewDecoder(&output)
	for i := 0; i < 10; i++ {
		var response serve.Response
		if err := decoder.Decode(&response); err != nil {
			t.Fatal(err)
		}
		if response.OK != (i != 0) {
			t.Fatalf("command %d = %+v", i, response)
		}
	}
	store := backend.NewDirectorySaveStore(filepath.Join(saveRoot, testfixture.LGTCheckpointSaveOwner))
	data, found, err := store.LoadCheckpoint(backend.SaveIdentity(archive))
	if err != nil || !found {
		t.Fatalf("CLI did not persist its slot: %v, %t", err, found)
	}
	game, err := session.RestoreCheckpoint(t.Context(), archive, data, session.Options{SaveStore: store})
	if err != nil {
		t.Fatal(err)
	}
	defer game.Close()
	// The second slot was written two steps after the first was loaded, with
	// the hold the first one carried still owned.
	if !reflect.DeepEqual(game.HeldKeys(), []int32{49}) {
		t.Fatalf("the slot holds %v", game.HeldKeys())
	}
	word := func(address uint32) uint32 {
		bytes, err := game.Cheat().ReadBytes(address, 4)
		if err != nil {
			t.Fatal(err)
		}
		return binary.LittleEndian.Uint32(bytes)
	}
	if word(testfixture.LGTCheckpointStartupCounter) != 1 || word(testfixture.LGTCheckpointFrameCounter) != 6 {
		t.Fatalf("the slot is at start %d, frame %d: want one start and the six frames of four steps and two",
			word(testfixture.LGTCheckpointStartupCounter), word(testfixture.LGTCheckpointFrameCounter))
	}
	release, err := backend.ClaimSaveDirectory(filepath.Join(saveRoot, testfixture.LGTCheckpointSaveOwner))
	if err != nil {
		t.Fatal(err)
	}
	release()
}
