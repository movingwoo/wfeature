package gameroot

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// build lays out a game root: each path is created relative to the root, with
// a directory for every component before the last.
func build(t *testing.T, paths ...string) string {
	t.Helper()
	root := t.TempDir()
	for _, name := range paths {
		full := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte("archive"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func names(entries []Entry) []string {
	found := make([]string, 0, len(entries))
	for _, entry := range entries {
		found = append(found, entry.Group+"/"+entry.Name)
	}
	return found
}

// The boundary is the picker's: the root and one level of group, and an
// archive deeper than that is not offered. A diagnostic corpus filed under the
// root is what makes this load-bearing rather than tidy.
func TestEntriesStopOneLevelDown(t *testing.T) {
	root := build(t,
		"ktf/one.zip",
		"lgt/two.zip",
		"loose.zip",
		"test/KTF 1.2/deep.zip",
		"test/shallow.zip",
		"ktf/nested/deeper.zip",
	)
	want := []string{"ktf/one.zip", "lgt/two.zip", "test/shallow.zip", "/loose.zip"}
	if got := names(Entries(root)); !reflect.DeepEqual(got, want) {
		t.Errorf("Entries = %q, want %q", got, want)
	}
	wantPaths := []string{
		filepath.Join(root, "ktf", "one.zip"),
		filepath.Join(root, "lgt", "two.zip"),
		filepath.Join(root, "loose.zip"),
		filepath.Join(root, "test", "shallow.zip"),
	}
	if got := Paths(root, ".zip"); !reflect.DeepEqual(got, wantPaths) {
		t.Errorf("Paths = %q, want %q", got, wantPaths)
	}
}

// Ungrouped archives list after the groups, because an archive dropped into
// the root is the exception rather than the rule.
func TestEntriesPutTheRootLast(t *testing.T) {
	root := build(t, "zzz/last-group.zip", "aaa-loose.zip")
	want := []string{"zzz/last-group.zip", "/aaa-loose.zip"}
	if got := names(Entries(root)); !reflect.DeepEqual(got, want) {
		t.Errorf("Entries = %q, want %q", got, want)
	}
}

func TestPathsMatchExtensionWithoutCase(t *testing.T) {
	root := build(t, "ktf/upper.ZIP", "ktf/midlet.jar", "ktf/notes.txt")
	want := []string{filepath.Join(root, "ktf", "midlet.jar"), filepath.Join(root, "ktf", "upper.ZIP")}
	if got := Paths(root, "zip", ".JAR"); !reflect.DeepEqual(got, want) {
		t.Errorf("Paths = %q, want %q", got, want)
	}
}

// A root that is not there yet is a fresh install, not a failure: a Host has
// to keep working so the user can see where to put a game.
func TestEntriesOfMissingRootAreEmptyNotNil(t *testing.T) {
	got := Entries(filepath.Join(t.TempDir(), "absent"))
	if got == nil || len(got) != 0 {
		t.Errorf("Entries of a missing root = %v, want an empty slice", got)
	}
}
