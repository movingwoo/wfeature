package gameroot

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// A platform group can be a link to a library kept elsewhere — another disk,
// a shared folder. The Host opens archives through such a link already, so the
// listing follows it too rather than taking the link for a stray file in the
// root. A link to a directory is still not a file, wherever it sits.
func TestEntriesFollowAGroupThatIsALink(t *testing.T) {
	library := build(t, "ktf/one.zip", "ktf/deeper/two.zip")
	root := build(t, "lgt/three.zip", "four.zip")
	if err := os.Symlink(filepath.Join(library, "ktf"), filepath.Join(root, "ktf")); err != nil {
		t.Skipf("this file system makes no links: %v", err)
	}
	if err := os.Symlink(filepath.Join(library, "ktf", "deeper"), filepath.Join(root, "lgt", "linked")); err != nil {
		t.Fatal(err)
	}

	want := []string{"ktf/one.zip", "lgt/three.zip", "/four.zip"}
	if got := names(Entries(root)); !reflect.DeepEqual(got, want) {
		t.Fatalf("Entries = %v, want %v", got, want)
	}
	if got := Paths(root, ".zip"); len(got) != 3 || got[1] != filepath.Join(root, "ktf", "one.zip") {
		t.Fatalf("Paths = %v, want the linked group's archive among them", got)
	}
}
