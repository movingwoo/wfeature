package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/movingwoo/wfeature/internal/testfixture"
)

func writeTempFixture(t *testing.T, name string, data []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// What a run mode cannot honour is refused by name and exit code 2, before the
// archive is read.
func TestRunKTFRefusesWhatARouteCannotHonour(t *testing.T) {
	routePath := writeTempFixture(t, "route.txt", []byte("wait 4\n"))
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
	path := writeTempFixture(t, "native.zip", archive)
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
	path := writeTempFixture(t, "save.zip", archive)
	routePath := writeTempFixture(t, "route.txt", []byte("wait 100\n"))
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
