package ladder

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAnEmptyDirectoryAndAMissingOneFingerprintTheSame(t *testing.T) {
	empty := t.TempDir()
	if Footprint(empty) != Footprint(filepath.Join(empty, "never-made")) {
		t.Fatal("an empty save directory and one that was never made fingerprinted differently, so a launch that wrote nothing would read as a launch that wrote")
	}
}

func TestAWrittenFileChangesTheFingerprint(t *testing.T) {
	root := t.TempDir()
	before := Footprint(root)
	if err := os.WriteFile(filepath.Join(root, "save"), []byte("first run"), 0o600); err != nil {
		t.Fatal(err)
	}
	if Footprint(root) == before {
		t.Fatal("a launch that wrote a save fingerprinted the same as one that wrote nothing, so the notice's second launch would never be given")
	}
}

func TestRewritingTheSameBytesIsNotAWrite(t *testing.T) {
	// A title that opens its save file and puts back what was already in it
	// has recorded nothing, and a second launch over it is the first launch
	// again.
	root := t.TempDir()
	name := filepath.Join(root, "save")
	if err := os.WriteFile(name, []byte("same"), 0o600); err != nil {
		t.Fatal(err)
	}
	before := Footprint(root)
	if err := os.WriteFile(name, []byte("same"), 0o600); err != nil {
		t.Fatal(err)
	}
	if Footprint(root) != before {
		t.Fatal("rewriting a file with the bytes it already held changed the fingerprint")
	}
}

func TestContentsCountAndNotJustNames(t *testing.T) {
	root := t.TempDir()
	name := filepath.Join(root, "save")
	if err := os.WriteFile(name, []byte("one"), 0o600); err != nil {
		t.Fatal(err)
	}
	before := Footprint(root)
	if err := os.WriteFile(name, []byte("two"), 0o600); err != nil {
		t.Fatal(err)
	}
	if Footprint(root) == before {
		t.Fatal("a save file rewritten with different bytes fingerprinted the same, so a fingerprint over names alone would have been enough")
	}
}

func TestNestedFilesCount(t *testing.T) {
	// Saves land under a directory named for the archive, so a fingerprint
	// that only read the top level would see nothing whatever a title wrote.
	root := t.TempDir()
	before := Footprint(root)
	nested := filepath.Join(root, "owner", "deeper")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nested, "save"), []byte("written"), 0o600); err != nil {
		t.Fatal(err)
	}
	if Footprint(root) == before {
		t.Fatal("a save written below the root did not change the fingerprint")
	}
}

func TestAnEmptyRootIsZero(t *testing.T) {
	if Footprint("") != 0 {
		t.Fatal("a rung with no save directory got a fingerprint out of one")
	}
}
