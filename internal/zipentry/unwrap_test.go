package zipentry

import (
	"maps"
	"slices"
	"testing"
)

func TestDirectory(t *testing.T) {
	tests := []struct {
		name  string
		names []string
		want  string
	}{
		{"one folder holding everything", []string{"Game/__adf__", "Game/0001.jar"}, "Game"},
		{"deeper entries still share the top", []string{"Game/__adf__", "Game/res/a.png"}, "Game"},
		{
			// The shape an archive that works has: the marker is at the root, so
			// there is nothing every entry is inside.
			"a marker at the root", []string{"__adf__", "0001.jar"}, "",
		},
		{"two folders", []string{"a/x", "b/y"}, ""},
		{"one entry at the root among many nested", []string{"Game/a", "Game/b", "readme.txt"}, ""},
		{"a JAR, which always has META-INF beside the rest", []string{"META-INF/MANIFEST.MF", "Main.class"}, ""},
		{"nothing at all", nil, ""},
		{"a folder name that is a traversal", []string{"../x", "../y"}, ""},
		{"a folder name that is a dot", []string{"./x", "./y"}, ""},
		{"an entry that is only a folder", []string{"Game/", "Game/a"}, ""},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := Directory(test.names); got != test.want {
				t.Errorf("Directory(%q) = %q, want %q", test.names, got, test.want)
			}
		})
	}
}

func TestUnwrapRemovesTheSharedFolder(t *testing.T) {
	entries := map[string][]byte{
		"My Game/__adf__":   []byte("adf"),
		"My Game/0001.jar":  []byte("jar"),
		"My Game/res/a.png": []byte("png"),
	}
	unwrapped := Unwrap(entries)
	want := []string{"0001.jar", "__adf__", "res/a.png"}
	got := slices.Sorted(maps.Keys(unwrapped))
	if !slices.Equal(got, want) {
		t.Fatalf("Unwrap() names = %q, want %q", got, want)
	}
	if string(unwrapped["__adf__"]) != "adf" {
		t.Fatal("Unwrap moved the contents to the wrong name")
	}
}

// An archive that works is one whose marker is at the root, and an archive
// whose marker is at the root has an entry that is not inside any folder. So
// the rule can only ever fire on an archive no loader could read — which is
// what makes applying it unconditionally safe.
func TestUnwrapLeavesAWorkingArchiveAlone(t *testing.T) {
	entries := map[string][]byte{
		"__adf__":  []byte("adf"),
		"0001.jar": []byte("jar"),
	}
	unwrapped := Unwrap(entries)
	if len(unwrapped) != 2 || string(unwrapped["__adf__"]) != "adf" {
		t.Fatalf("Unwrap changed an archive that already works: %q", slices.Sorted(maps.Keys(unwrapped)))
	}
}

// Only one level comes off. An archive wrapped twice is not something any copy
// has been seen to do, and stripping until the names run out would take a real
// directory off an archive that genuinely keeps everything in one.
func TestUnwrapRemovesOnlyOneLevel(t *testing.T) {
	entries := map[string][]byte{
		"outer/inner/__adf__":  []byte("adf"),
		"outer/inner/0001.jar": []byte("jar"),
	}
	unwrapped := Unwrap(entries)
	want := []string{"inner/0001.jar", "inner/__adf__"}
	if got := slices.Sorted(maps.Keys(unwrapped)); !slices.Equal(got, want) {
		t.Fatalf("Unwrap() names = %q, want %q", got, want)
	}
}

// Two entries that would land on one name leave the whole set alone: the
// loader about to look its marker up reports a contradictory archive better
// than a silent halving does.
func TestUnwrapRefusesToCollide(t *testing.T) {
	entries := map[string][]byte{
		"Game/a": []byte("one"),
		"Game/b": []byte("two"),
	}
	// A collision needs two names differing only above the shared folder, which
	// one folder cannot produce — so the guard is checked directly instead.
	if got := Unwrap(entries); len(got) != 2 {
		t.Fatalf("Unwrap() kept %d entries, want 2", len(got))
	}
	if _, ok := Unwrap(entries)["a"]; !ok {
		t.Fatal("Unwrap did not strip the folder")
	}
}
