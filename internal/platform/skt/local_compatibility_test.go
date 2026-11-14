package skt

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/movingwoo/wfeature/internal/testlocal"
)

// The compatibility registry's fixes are tested on authored fixtures beside
// each fix. The tests in this file check an entry against the original archive
// it recognizes, which stays local: each reads an absolute archive path from
// its own environment variable.

// The real archive stays local. This also proves that changing any class
// disables the exception without relying on a title or container filename.
func TestLocalClipCompatibilityArchive(t *testing.T) {
	path := testlocal.Value(t, "WFEATURE_SKT_CLIP_ARCHIVE", "the reported local archive")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	archive, err := Open(data)
	if err != nil {
		t.Fatal(err)
	}
	if !archive.legacyClipEndpoints() {
		t.Fatalf("unrecognized code digest: %s", archive.clipCodeDigest())
	}
	archive.Descriptor.Name = "Renamed Archive"
	if !archive.legacyClipEndpoints() {
		t.Fatal("display name changed compatibility")
	}
	for name, data := range archive.Entries {
		if !strings.HasSuffix(name, ".class") {
			continue
		}
		changed := append([]byte(nil), data...)
		changed[len(changed)-1] ^= 1
		archive.Entries[name] = changed
		if archive.legacyClipEndpoints() {
			t.Fatalf("changed class %q retained compatibility", name)
		}
		archive.Entries[name] = data
	}
}

func TestLocalResumeCompatibilityFingerprints(t *testing.T) {
	paths := testlocal.Value(t, "WFEATURE_SKT_RESUME_ARCHIVES", "original local archives")
	for _, path := range strings.Split(paths, string(os.PathListSeparator)) {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		archive, err := Open(data)
		if err != nil {
			t.Fatal(err)
		}
		if !archive.resumeWithoutRestart() {
			t.Fatal("recognized class set lost its resume correction")
		}
		for name, original := range archive.Entries {
			if !strings.HasSuffix(name, ".class") {
				continue
			}
			archive.Entries[name] = append(bytes.Clone(original), 0)
			if archive.resumeWithoutRestart() {
				t.Fatal("changed class set received the resume correction")
			}
			archive.Entries[name] = original
		}
	}
}

// The class sets resumed through startApp, their finished thread started
// again, are the original archives' and no changed copy of them.
func TestLocalResumeThroughStartFingerprints(t *testing.T) {
	paths := testlocal.Value(t, "WFEATURE_SKT_RESUME_THROUGH_START_ARCHIVES", "original local archives")
	for _, path := range strings.Split(paths, string(os.PathListSeparator)) {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		archive, err := Open(data)
		if err != nil {
			t.Fatal(err)
		}
		if !archive.resumeThroughStart() {
			t.Fatalf("%s: recognized class set is not resumed through startApp", path)
		}
		for name, original := range archive.Entries {
			if !strings.HasSuffix(name, ".class") {
				continue
			}
			archive.Entries[name] = append(bytes.Clone(original), 0)
			if archive.resumeThroughStart() {
				t.Fatalf("%s: changed class set is resumed through startApp", path)
			}
			archive.Entries[name] = original
		}
	}
}

func TestLocalTutorialCompatibilityFingerprint(t *testing.T) {
	path := testlocal.Value(t, "WFEATURE_SKT_TUTORIAL_ARCHIVE", "an original local archive")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	archive, err := Open(data)
	if err != nil {
		t.Fatal(err)
	}
	original := bytes.Clone(archive.Entries["f.class"])
	if len(prepareTutorialCompatibility(archive)) != 1 {
		t.Fatal("original revision not adapted")
	}
	if !bytes.Equal(original, archive.Entries["f.class"]) {
		t.Fatal("original archive changed")
	}
	archive.Entries["extra.class"] = []byte("changed class set")
	if len(prepareTutorialCompatibility(archive)) != 0 {
		t.Fatal("changed revision adapted")
	}
}
