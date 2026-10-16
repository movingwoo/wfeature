package main

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"image/png"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/cheat"
	"github.com/movingwoo/wfeature/internal/platform/skt"
	"github.com/movingwoo/wfeature/internal/sgsvm"
	"github.com/movingwoo/wfeature/internal/testfixture"
)

func writeReviewFixture(t *testing.T, name string, data []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// The console a -cheat run reads from the terminal is the only one whose
// `save` and `load` reach the file system. Every other console — the one the
// browser host drives over its session socket above all — keeps them off,
// which holds only while nothing outside this command turns them on.
func TestOnlyTheTerminalConsoleReachesFiles(t *testing.T) {
	memory := newPatchMemory()
	console := cheat.NewConsole(cheat.NewSession(memory))
	target := filepath.Join(t.TempDir(), "table.json")
	if output := console.Execute("save " + target); !strings.HasPrefix(output, "cheat: ") {
		t.Fatalf("a console nobody attached saved: %q", output)
	}
	attachCheatConsole(console, "no-such-archive.zip")
	if output := console.Execute("save " + target); !strings.Contains(output, "saved") {
		t.Fatalf("the terminal console did not save: %q", output)
	}

	root := filepath.Join("..", "..")
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			switch entry.Name() {
			case "var", "build", "node_modules", ".git", ".claude":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if !bytes.Contains(data, []byte(".AllowFiles(")) {
			return nil
		}
		if relative, _ := filepath.Rel(root, path); filepath.Dir(relative) != filepath.Join("cmd", "cli") {
			t.Errorf("%s gives a cheat console the file system; only the terminal one may", relative)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// `run` speaks the page's key codes, and the page sends -6 and -7 for the soft
// keys on every platform.
func TestSharedKeyNamesAreThePageCodes(t *testing.T) {
	for _, platform := range []string{"ktf", "lgt", "skt"} {
		lookup := sharedKeyLookup(platform)
		for name, want := range map[string]int32{"soft1": -6, "soft2": -7, "fire": 148, "clear": 8, "call": 10, "5": '5'} {
			if code, ok := lookup(name); !ok || code != want {
				t.Errorf("%s %s = %d/%t, want %d", platform, name, code, ok, want)
			}
		}
	}
	for _, name := range []string{"soft3", "ez", "hangup"} {
		if _, ok := sharedKeyLookup("ktf")(name); !ok {
			t.Errorf("ktf refused %s", name)
		}
		if code, ok := sharedKeyLookup("skt")(name); ok {
			t.Errorf("skt sent %s as %d, which a MIDlet or a script reads as another key", name, code)
		}
	}
}

// What a run mode cannot honour is refused by name and exit code 2, before the
// archive is read.
func TestRunKTFRefusesWhatARouteCannotHonour(t *testing.T) {
	routePath := writeReviewFixture(t, "route.txt", []byte("wait 4\n"))
	for _, test := range []struct {
		name  string
		extra []string
	}{
		{"-key", []string{"-key", "5:fire"}},
		{"-touch", []string{"-touch", "5:press:1,1"}},
		{"-park", []string{"-park", "5"}},
		{"-cheat", []string{"-cheat"}},
		{"-profile-from", []string{"-profile-from", "5"}},
	} {
		var stdout, stderr bytes.Buffer
		args := append([]string{"-route", routePath, "-save", t.TempDir()}, test.extra...)
		if code := runKTF("no-such-archive.zip", args, &stdout, &stderr); code != 2 || !strings.Contains(stderr.String(), test.name) {
			t.Errorf("-route with %s = %d: %s", test.name, code, stderr.String())
		}
	}
	var stdout, stderr bytes.Buffer
	if code := runKTF("no-such-archive.zip", []string{"-serve", "-profile-from", "5"}, &stdout, &stderr); code != 2 || !strings.Contains(stderr.String(), "-profile-from") {
		t.Errorf("-serve with -profile-from = %d: %s", code, stderr.String())
	}
}

func TestRunKTFRefusesWhatTheEarlierPackageCannotHonour(t *testing.T) {
	archive, err := testfixture.KTFNativeCheckpointArchive()
	if err != nil {
		t.Fatal(err)
	}
	path := writeReviewFixture(t, "native.zip", archive)
	for _, test := range []struct {
		name  string
		extra []string
	}{
		{"-touch", []string{"-touch", "5:press:1,1"}},
		{"-park", []string{"-park", "5"}},
		{"-profile-from", []string{"-profile-from", "5"}},
	} {
		var stdout, stderr bytes.Buffer
		args := append([]string{"-save", t.TempDir(), "-ticks", "2"}, test.extra...)
		if code := runKTF(path, args, &stdout, &stderr); code != 2 || !strings.Contains(stderr.String(), test.name+" is not available") {
			t.Errorf("%s on the earlier package = %d: %s", test.name, code, stderr.String())
		}
	}
	// Its session takes a speed like the other package's does, so -speed is
	// passed on rather than refused.
	for _, multiplier := range []string{"1", "2"} {
		var stdout, stderr bytes.Buffer
		if code := runKTF(path, []string{"-save", t.TempDir(), "-ticks", "2", "-speed", multiplier}, &stdout, &stderr); code != 0 {
			t.Errorf("-speed %s on the earlier package = %d: %s", multiplier, code, stderr.String())
		}
	}
}

// An explicit -ticks caps a route whatever count it names, the default's own
// number included.
func TestRunKTFRouteHonoursAnExplicitDefaultTickCount(t *testing.T) {
	// The storage fixture's card flushes a frame every tick, so a route over
	// it runs as long as its budget lets it.
	archive, err := testfixture.KTFSaveArchive()
	if err != nil {
		t.Fatal(err)
	}
	path := writeReviewFixture(t, "save.zip", archive)
	routePath := writeReviewFixture(t, "route.txt", []byte("wait 100\n"))
	var stdout, stderr bytes.Buffer
	code := runKTF(path, []string{"-save", t.TempDir(), "-route", routePath, "-ticks", "64"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("route run = %d: %s", code, stderr.String())
	}
	var summary struct {
		Ticks     int  `json:"ticks"`
		Completed bool `json:"route_completed"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &summary); err != nil {
		t.Fatalf("summary: %v\n%s", err, stdout.String())
	}
	if summary.Completed || summary.Ticks != 64 {
		t.Fatalf("a route capped at 64 ticks ran %d and completed=%t", summary.Ticks, summary.Completed)
	}
}

// litFrames is a session whose screen has been flushed once with one lit
// pixel on it.
type litFrames struct{}

func (litFrames) Flushes() uint32 { return 1 }
func (litFrames) Frame() ([]byte, int, int, uint32) {
	frame := make([]byte, 2*2*4)
	frame[0], frame[3] = 0xff, 0xff
	return frame, 2, 2, 1
}

// A probe stops at its first lit frame, and -framedir keeps that frame.
func TestProbeFrameDirKeepsTheFirstLitFrame(t *testing.T) {
	frameDir := t.TempDir()
	dumped := uint32(0)
	lit, err := probeFrame(litFrames{}, frameDir, false, 7, &dumped)
	if err != nil {
		t.Fatal(err)
	}
	if !lit {
		t.Fatal("a probe did not stop at a lit frame")
	}
	file, err := os.Open(filepath.Join(frameDir, "tick0007.png"))
	if err != nil {
		t.Fatalf("the lit frame the probe stopped at is not in -framedir: %v", err)
	}
	defer file.Close()
	picture, err := png.Decode(file)
	if err != nil {
		t.Fatal(err)
	}
	if r, _, _, _ := picture.At(0, 0).RGBA(); r == 0 {
		t.Fatal("the frame written is not the lit one")
	}
	// Under -play nothing stops, and a frame already written is not again.
	if lit, err := probeFrame(litFrames{}, frameDir, true, 8, &dumped); err != nil || lit {
		t.Fatalf("under -play: lit %t, err %v", lit, err)
	}
	if _, err := os.Stat(filepath.Join(frameDir, "tick0008.png")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("an unchanged frame was written again: %v", err)
	}
}

// A failed frame on the earlier package exits non-zero, as one on the other
// package does. The fault is a patch over the first instruction of the frame
// the fixture schedules.
func TestRunKTFNativeTickFailureExitsNonZero(t *testing.T) {
	archive, err := testfixture.KTFNativeCheckpointArchive()
	if err != nil {
		t.Fatal(err)
	}
	path := writeReviewFixture(t, "native.zip", archive)
	table := writeReviewFixture(t, "fault.json", []byte(`{"patches":[{"name":"fault","patches":[
		{"address":"0x00100100","expect":"10402de9","replace":"f000f0e7"}]}]}`))
	var stdout, stderr bytes.Buffer
	code := runKTF(path, []string{"-save", t.TempDir(), "-ticks", "8", "-patch", table}, &stdout, &stderr)
	if !strings.Contains(stdout.String(), "tick_error") {
		t.Fatalf("the patched frame did not fail (exit %d):\n%s\n%s", code, stdout.String(), stderr.String())
	}
	if code != 1 {
		t.Fatalf("a run whose frame failed exited %d", code)
	}
}

// containsBytes reports whether any file under root holds needle.
func containsBytes(t *testing.T, root string, needle []byte) bool {
	t.Helper()
	found := false
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() || found {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		found = bytes.Contains(data, needle)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return found
}

// A MIDlet that saves in destroyApp keeps that save after a run ends on its
// tick count: the run destroys it the way closing a server session does.
func TestRunSKTDestroysTheMIDletSoItsExitSaveLands(t *testing.T) {
	saves := t.TempDir()
	var stdout, stderr bytes.Buffer
	if code := run([]string{"runskt", filepath.Join("testdata", "destroy-save.jar"), "-ticks", "2", "-save", saves}, &stdout, &stderr); code != 0 {
		t.Fatalf("runskt = %d: %s", code, stderr.String())
	}
	if !containsBytes(t, saves, []byte("written-by-destroyApp")) {
		t.Fatal("the record store destroyApp writes is not in the save directory")
	}
}

// An LGT title's write to a file it left open lands when the run ends: the run
// closes its session, which flushes the file, as the server does.
func TestRunLGTClosesItsSessionSoAnOpenFileLands(t *testing.T) {
	archive, err := testfixture.LGTSaveArchive()
	if err != nil {
		t.Fatal(err)
	}
	path := writeReviewFixture(t, "save.zip", archive)
	saves := t.TempDir()
	var stdout, stderr bytes.Buffer
	if code := runLGT(path, []string{"-save", saves, "-ticks", "8", "-key", "2:9"}, &stdout, &stderr); code != 0 {
		t.Fatalf("runlgt = %d: %s", code, stderr.String())
	}
	data, found, err := backend.ReadSave(backend.NewDirectorySaveStore(saves), testfixture.LGTSaveFileKey)
	if err != nil {
		t.Fatal(err)
	}
	if !found || len(data) < 4 || binary.LittleEndian.Uint32(data) != 0 {
		t.Fatalf("the held save after the run = %x (found %t), want part A written", data, found)
	}
}

// runskt, runlgt and an SGS run take the claim the server takes, so one held
// elsewhere refuses them before they write, and theirs is let go when they end.
func TestRunsClaimTheirSaveDirectory(t *testing.T) {
	lgtArchive, err := testfixture.LGTSaveArchive()
	if err != nil {
		t.Fatal(err)
	}
	lgtPath := writeReviewFixture(t, "save.zip", lgtArchive)
	program := &sgsvm.Program{Data: []byte{0, 0xff}, Entries: [8]uint16{1}, Variables: make([]sgsvm.Variable, 17)}
	for index := range program.Variables {
		program.Variables[index] = sgsvm.Variable{Mutable: true, Offset: index, Values: []int16{0}}
	}
	program.Variables[16].Values = make([]int16, 33)
	starts := map[string]func(saves string, stdout, stderr *bytes.Buffer) int{
		"runskt": func(saves string, stdout, stderr *bytes.Buffer) int {
			return run([]string{"runskt", filepath.Join("testdata", "destroy-save.jar"), "-ticks", "1", "-save", saves}, stdout, stderr)
		},
		"runlgt": func(saves string, stdout, stderr *bytes.Buffer) int {
			return runLGT(lgtPath, []string{"-save", saves, "-ticks", "1"}, stdout, stderr)
		},
		"sgs": func(saves string, stdout, stderr *bytes.Buffer) int {
			archive := &skt.Archive{Script: program, ScriptSaveOwner: "sgs-review"}
			return runScript(archive, scriptCLIRun{ticks: 1, width: 16, height: 16, saveRoot: saves}, stdout, stderr)
		},
	}
	for name, start := range starts {
		t.Run(name, func(t *testing.T) {
			saves := filepath.Join(t.TempDir(), "owner")
			release, err := backend.ClaimSaveDirectory(saves)
			if err != nil {
				t.Fatal(err)
			}
			var stdout, stderr bytes.Buffer
			if code := start(saves, &stdout, &stderr); code != 1 || !strings.Contains(stderr.String(), backend.ErrSaveDirectoryBusy.Error()) {
				release()
				t.Fatalf("%s under a held claim = %d: %s", name, code, stderr.String())
			}
			release()
			stdout.Reset()
			stderr.Reset()
			if code := start(saves, &stdout, &stderr); strings.Contains(stderr.String(), backend.ErrSaveDirectoryBusy.Error()) {
				t.Fatalf("%s with the claim free = %d: %s", name, code, stderr.String())
			}
			release, err = backend.ClaimSaveDirectory(saves)
			if err != nil {
				t.Fatalf("%s kept its claim after it ended: %v", name, err)
			}
			release()
		})
	}
}
