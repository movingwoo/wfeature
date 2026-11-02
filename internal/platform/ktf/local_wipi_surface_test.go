package ktf

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/movingwoo/wfeature/internal/testlocal"
)

// TestLocalRegenerateWIPISurfaceReference rewrites the reference file from a local
// checkout of the original implementation. It is the repeatable half of the gap
// measurement: run it after pulling the reference sources to see what they
// gained. The environment variable points at the directory holding that
// implementation's WIPI Java class definitions.
//
//	WFEATURE_WIPI_REFERENCE_SOURCE=<class source dir> go test -run TestLocalRegenerateWIPISurfaceReference ./internal/platform/ktf
func TestLocalRegenerateWIPISurfaceReference(t *testing.T) {
	root := testlocal.Value(t, "WFEATURE_WIPI_REFERENCE_SOURCE", "the reference WIPI Java class sources to regenerate "+wipiSurfaceReferencePath)
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
