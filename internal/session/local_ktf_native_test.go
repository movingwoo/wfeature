package session

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/movingwoo/wfeature/internal/platform/ktf"
	"github.com/movingwoo/wfeature/internal/testlocal"
)

// localKTFNativeArchive finds the earlier KTF package in the ignored local
// game directory. Real games are not in Git, so this is opt-in.
func localKTFNativeArchive(t *testing.T) []byte {
	t.Helper()
	testlocal.Enabled(t, "WFEATURE_KTF_NATIVE_ACCEPTANCE", "run the ignored local KTF native package")
	entries, err := os.ReadDir(filepath.Join("..", "..", "var", "games", "ktf"))
	if err != nil {
		t.Skipf("read the local KTF game directory: %v", err)
	}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".zip" {
			continue
		}
		data, err := os.ReadFile(filepath.Join("..", "..", "var", "games", "ktf", entry.Name()))
		if err != nil {
			t.Fatalf("read %s: %v", entry.Name(), err)
		}
		if ktf.IsNativeArchive(data) {
			return data
		}
	}
	t.Skip("no local KTF native package present")
	return nil
}

// The carrier's earlier download package reaches a Host through the same four
// moves as everything else, and this is where that is checked: a Host has no
// switch of its own, so a package the shared layer does not carry is a package
// no Host can run.
func TestLocalEarlierKTFPackageIsOnePlatformNeutralSession(t *testing.T) {
	archive := localKTFNativeArchive(t)
	summary, err := Inspect(archive)
	if err != nil {
		t.Fatalf("Inspect: %v", err)
	}
	if summary.Platform != "ktf" {
		t.Fatalf("platform = %q, want ktf — it is the same vendor in an earlier package", summary.Platform)
	}
	if summary.Name == "" || summary.SaveOwner == "" {
		t.Fatalf("summary = %+v, want a name and a save owner", summary)
	}

	started, err := Start(context.Background(), archive, Options{})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer started.Close()
	if !started.Running() {
		t.Fatal("the session is not running after a successful start")
	}
	if width, height := started.Screen(); width != DefaultWidth || height != DefaultHeight {
		t.Errorf("screen = %dx%d, want the handset's %dx%d", width, height, DefaultWidth, DefaultHeight)
	}

	// The title paces itself, so a tick answers with what is left of the
	// interval it asked for. Ticking for that long and no longer is what a
	// Host does with the answer.
	painted := false
	for round := 0; round < 400 && !painted; round++ {
		progress, err := started.Tick(context.Background(), 0)
		if err != nil {
			t.Fatalf("tick %d: %v", round, err)
		}
		if progress.Flushes > 0 {
			painted = true
		}
	}
	if !painted {
		t.Fatal("the title ended no frame")
	}
	frame, width, height, ok := started.Frame()
	if !ok || width != DefaultWidth || height != DefaultHeight || len(frame) != width*height*4 {
		t.Fatalf("frame = %d bytes at %dx%d (%v), want a full handset screen", len(frame), width, height, ok)
	}
	if elapsed, ok := started.GuestElapsed(); !ok || elapsed <= 0 {
		t.Errorf("guest elapsed = %v (%v), want time to have passed", elapsed, ok)
	}
	// A key runs guest code, and the shared vocabulary is what a Host sends.
	for _, action := range []string{KeyPress, KeyRelease} {
		if err := started.SendKey(context.Background(), action, 148); err != nil {
			t.Fatalf("send %s: %v", action, err)
		}
	}
}
