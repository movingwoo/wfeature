package main

import (
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

func TestCheckpointCLIProcess(t *testing.T)       { testCheckpointCLIProcess(t, false) }
func TestCheckpointNativeCLIProcess(t *testing.T) { testCheckpointCLIProcess(t, true) }

func testCheckpointCLIProcess(t *testing.T, native bool) {
	counterAddress := uint32(testfixture.KTFCheckpointStartupCounter)
	testName := "TestCheckpointCLIProcess"
	if native {
		counterAddress = testfixture.KTFNativeCheckpointStartupCounter
		testName = "TestCheckpointNativeCLIProcess"
	}
	if root := os.Getenv("WFEATURE_CLI_CHECKPOINT_ROOT"); root != "" {
		args := []string{"run", filepath.Join(root, "fixture.zip"), "-save", filepath.Join(root, "saves"), "-quickload", "-ticks", "0", "-quicksave"}
		if code := run(args, os.Stdout, os.Stderr); code != 0 {
			t.Fatalf("restarted CLI = %d", code)
		}
		return
	}
	_, root, archive := checkpointCLIFixture(t)
	if native {
		var err error
		archive, err = testfixture.KTFNativeCheckpointArchive()
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, "fixture.zip"), archive, 0600); err != nil {
			t.Fatal(err)
		}
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
	if progress, _ := store.LoadSave("progress"); string(progress) != "checkpoint progress" {
		t.Fatal("CLI restart did not replace ordinary save progress")
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
