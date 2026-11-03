package ktf

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"
)

// The WIPI Java surface the original runtime exposes is large, and a class or
// method that is simply absent from our registration table fails inside a game
// with no hint that the boundary was never written. These tests turn that into
// a measured, reviewable list.
//
// testdata/wipi_java_surface.txt is the reference surface extracted from the
// original implementation, and testdata/wipi_java_gaps.txt records the
// entries we deliberately do not implement. Everything in the reference must be
// either registered or listed as a gap: a new gap fails the test, and so does a
// gap entry that is now implemented, which keeps the list from going stale.

const (
	wipiSurfaceReferencePath = "testdata/wipi_java_surface.txt"
	wipiSurfaceGapsPath      = "testdata/wipi_java_gaps.txt"
)

func TestRuntimeJavaSurfaceCoversReference(t *testing.T) {
	reference := readSurfaceFile(t, wipiSurfaceReferencePath)
	declaredGaps := readSurfaceFile(t, wipiSurfaceGapsPath)
	registered := registeredWIPISurface()

	var missing []string
	for _, entry := range reference {
		if !registered[entry] {
			missing = append(missing, entry)
		}
	}
	sort.Strings(missing)

	gapSet := make(map[string]bool, len(declaredGaps))
	for _, entry := range declaredGaps {
		gapSet[entry] = true
	}
	missingSet := make(map[string]bool, len(missing))
	for _, entry := range missing {
		missingSet[entry] = true
	}

	for _, entry := range missing {
		if !gapSet[entry] {
			t.Errorf("WIPI surface entry is neither implemented nor declared a gap: %s", entry)
		}
	}
	for _, entry := range declaredGaps {
		if !missingSet[entry] {
			t.Errorf("%s lists %s, which is now implemented; remove the line", wipiSurfaceGapsPath, entry)
		}
	}
	t.Logf("WIPI Java surface: %d reference entries, %d registered, %d declared gaps",
		len(reference), len(reference)-len(missing), len(declaredGaps))
}

// Every gap is written down with the reason it is one, and the reason is a
// comment above the entries it covers. When the last entry under a reason is
// implemented, the line comes out and the reason is left standing over
// nothing — a paragraph in a tracked file explaining a decision that is no
// longer taken, which reads as a limitation this runtime still has. The test
// above cannot see it: it compares entries, and there are none left to
// compare.
func TestEveryDeclaredGapReasonStillCoversAnEntry(t *testing.T) {
	content, err := os.ReadFile(wipiSurfaceGapsPath)
	if err != nil {
		t.Fatal(err)
	}
	reason, line := "", 0
	entries := 0
	report := func() {
		if reason != "" && entries == 0 {
			t.Errorf("%s:%d explains a gap that no longer lists an entry; remove the reason with it: %q",
				wipiSurfaceGapsPath, line, reason)
		}
	}
	for index, text := range strings.Split(string(content), "\n") {
		switch {
		case strings.HasPrefix(text, "#"):
			// A comment right under an entry list opens the next reason.
			if reason == "" || entries > 0 {
				report()
				reason, line, entries = strings.TrimSpace(strings.TrimPrefix(text, "#")), index+1, 0
			}
		case strings.TrimSpace(text) == "":
		default:
			entries++
		}
	}
	report()
}

// registeredWIPISurface reports the org.kwis entries of our registration table
// in the reference file's "class" and "class name descriptor" forms.
func registeredWIPISurface() map[string]bool {
	surface := make(map[string]bool)
	for name, definition := range runtimeJavaClasses {
		if !strings.HasPrefix(name, "org/kwis/") {
			continue
		}
		surface[name] = true
		for _, method := range definition.methods {
			surface[fmt.Sprintf("%s %s %s", name, method.name, method.descriptor)] = true
		}
	}
	return surface
}

func readSurfaceFile(t *testing.T, path string) []string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var entries []string
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		entries = append(entries, line)
	}
	return entries
}
