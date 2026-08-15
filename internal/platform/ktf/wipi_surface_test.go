package ktf

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
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
	wipiSurfaceReferencePath   = "testdata/wipi_java_surface.txt"
	wipiSurfaceGapsPath        = "testdata/wipi_java_gaps.txt"
	referenceSourceEnvironment = "WFEATURE_WIPI_REFERENCE_SOURCE"
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

// TestRegenerateWIPISurfaceReference rewrites the reference file from a local
// checkout of the original implementation. It is the repeatable half of the gap
// measurement: run it after pulling the reference sources to see what they
// gained. The environment variable points at the directory holding that
// implementation's WIPI Java class definitions.
//
//	WFEATURE_WIPI_REFERENCE_SOURCE=<class source dir> go test -run TestRegenerateWIPISurfaceReference ./internal/platform/ktf
func TestRegenerateWIPISurfaceReference(t *testing.T) {
	root := os.Getenv(referenceSourceEnvironment)
	if root == "" {
		t.Skipf("set %s to the reference WIPI Java class sources to regenerate %s", referenceSourceEnvironment, wipiSurfaceReferencePath)
	}
	entries, err := parseReferenceClassProtos(root)
	if err != nil {
		t.Fatalf("parse reference class protos: %v", err)
	}
	if len(entries) == 0 {
		t.Fatalf("no class protos found under %s", root)
	}
	content := strings.Join(entries, "\n") + "\n"
	if err := os.WriteFile(wipiSurfaceReferencePath, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", wipiSurfaceReferencePath, err)
	}
	t.Logf("wrote %d entries to %s", len(entries), wipiSurfaceReferencePath)
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

var (
	referenceClassNamePattern = regexp.MustCompile(`\w*JavaClassProto\s*\{\s*name:\s*"([^"]+)"`)
	referenceMethodPattern    = regexp.MustCompile(`JavaMethodProto::new(?:_abstract)?\s*\(\s*"([^"]+)"\s*,\s*"([^"]+)"`)
)

// parseReferenceClassProtos extracts every org.kwis class proto and its
// declared methods from the original Rust sources. Test modules are cut first
// so their fixture classes never reach the reference.
func parseReferenceClassProtos(root string) ([]string, error) {
	var entries []string
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".rs") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		source := string(data)
		if index := strings.Index(source, "#[cfg(test)]"); index >= 0 {
			source = source[:index]
		}
		name := referenceClassNamePattern.FindStringSubmatch(source)
		if name == nil || !strings.HasPrefix(name[1], "org/kwis/") {
			return nil
		}
		entries = append(entries, name[1])
		for _, method := range referenceMethodPattern.FindAllStringSubmatch(source, -1) {
			entries = append(entries, fmt.Sprintf("%s %s %s", name[1], method[1], method[2]))
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(entries)
	return entries, nil
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
