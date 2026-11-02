package main

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/platform/skt"
	"github.com/movingwoo/wfeature/internal/sgsvm"
	"github.com/movingwoo/wfeature/internal/testfixture"
)

// The save locations a release before the file lock ran on still start a game
// from the command line, and the one that cannot hold the lock says so once.

const fallbackWarning = "cannot hold a file lock"

func TestCLIStartsThroughALinkedSaveDirectory(t *testing.T) {
	path, root, _ := checkpointCLIFixture(t)
	saveRoot := filepath.Join(root, "saves")
	target := filepath.Join(t.TempDir(), "kept elsewhere")
	for _, directory := range []string{saveRoot, target} {
		if err := os.Mkdir(directory, 0755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(target, filepath.Join(saveRoot, "P0001")); err != nil {
		t.Skipf("this platform cannot make the link the case needs: %v", err)
	}
	for name, start := range map[string]func(out, diagnostics *bytes.Buffer) int{
		"run": func(out, diagnostics *bytes.Buffer) int {
			return runShared(t.Context(), path, []string{"-save", saveRoot, "-ticks", "1"}, nil, out, diagnostics)
		},
		"runktf": func(out, diagnostics *bytes.Buffer) int {
			return runKTF(path, []string{"-save", saveRoot, "-ticks", "1"}, out, diagnostics)
		},
	} {
		var output, diagnostics bytes.Buffer
		if code := start(&output, &diagnostics); code != 0 {
			t.Fatalf("%s through a linked save directory = %d: %s", name, code, diagnostics.String())
		}
		if strings.Contains(diagnostics.String(), fallbackWarning) {
			t.Fatalf("%s reported a link as a location without a file lock: %s", name, diagnostics.String())
		}
	}
}

func TestCLIStartsOnAReadOnlySaveRootAndSaysSoOnce(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows does not refuse a write by directory mode bits")
	}
	if os.Geteuid() == 0 {
		t.Skip("the superuser is not refused by directory mode bits")
	}
	path, root, _ := checkpointCLIFixture(t)
	saveRoot := filepath.Join(root, "saves")
	if err := os.Mkdir(saveRoot, 0555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(saveRoot, 0755) })

	var output, diagnostics bytes.Buffer
	if code := runShared(t.Context(), path, []string{"-save", saveRoot, "-ticks", "1"}, nil, &output, &diagnostics); code != 0 {
		t.Fatalf("run on a read-only save root = %d: %s", code, diagnostics.String())
	}
	if reported := strings.Count(diagnostics.String(), fallbackWarning); reported != 1 {
		t.Fatalf("the fallback was reported %d times: %s", reported, diagnostics.String())
	}
	// The same directory in the same process has been reported; the earlier
	// command starts on it the same way and does not say it again.
	output.Reset()
	diagnostics.Reset()
	if code := runKTF(path, []string{"-save", saveRoot, "-ticks", "1"}, &output, &diagnostics); code != 0 {
		t.Fatalf("runktf on a read-only save root = %d: %s", code, diagnostics.String())
	}
	if strings.Contains(diagnostics.String(), fallbackWarning) {
		t.Fatalf("the fallback was reported twice for one directory: %s", diagnostics.String())
	}
}

// runskt, runlgt and an SGS run take the claim the server takes, so one held
// elsewhere refuses them before they write, and theirs is let go when they end.
func TestRunsClaimTheirSaveDirectory(t *testing.T) {
	lgtArchive, err := testfixture.LGTSaveArchive()
	if err != nil {
		t.Fatal(err)
	}
	lgtPath := writeTempFixture(t, "save.zip", lgtArchive)
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
