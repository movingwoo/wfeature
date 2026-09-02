package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/movingwoo/wfeature/internal/launcher"
	"github.com/movingwoo/wfeature/internal/webhost"
)

// The header is one string with two spellings, because the launcher does not
// import the server to read it. This is what holds them together.
func TestTheAdminHeaderIsSpelledTheSameOnBothSides(t *testing.T) {
	if launcher.AdminHeader != webhost.AdminHeader {
		t.Fatalf("launcher sends %q and the server reads %q",
			launcher.AdminHeader, webhost.AdminHeader)
	}
}

// The access key outlives a restart and the admin key does not. That pair is
// the whole design: a link that changed every time the server was restarted is
// a link nobody keeps — the phone that installed the page as an app would be
// the first to break — while an admin key belongs to a process and there is no
// reason for it to survive one.
func TestARestartKeepsTheLinkAndReplacesTheAdminKey(t *testing.T) {
	root := t.TempDir()

	first, err := preparePublicState(root, 11541)
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if first.Key == "" || first.Admin == "" {
		t.Fatalf("a first run left a key empty: %+v", first)
	}

	second, err := preparePublicState(root, 11599)
	if err != nil {
		t.Fatalf("prepare again: %v", err)
	}
	if second.Key != first.Key {
		t.Errorf("the link changed across a restart: %q then %q", first.Key, second.Key)
	}
	if second.Admin == first.Admin {
		t.Error("the admin key survived the run it belonged to")
	}
	if second.Port != 11599 || second.PID != os.Getpid() {
		t.Errorf("the file does not say which run wrote it: %+v", second)
	}

	// And what the launcher reads back is what was written.
	if read := readPublicState(root); read != second {
		t.Errorf("read %+v, want %+v", read, second)
	}
}

// The file holds both keys, so it is the one thing here that must not be
// readable by other accounts on a shared machine.
func TestTheKeysAreWrittenPrivately(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("file modes are not how Windows answers this")
	}
	root := t.TempDir()
	if _, err := preparePublicState(root, 11541); err != nil {
		t.Fatalf("prepare: %v", err)
	}
	info, err := os.Stat(publicStatePath(root))
	if err != nil {
		t.Fatal(err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Errorf("mode = %04o, want 0600", mode)
	}
}

// A missing, empty or half-written file is an ordinary state — no public run
// has happened yet, or the file was deleted — and every caller reads it as
// "there is nothing to say" rather than failing.
func TestAnUnreadableStateFileIsNotAnError(t *testing.T) {
	root := t.TempDir()
	if state := readPublicState(root); state != (publicState{}) {
		t.Errorf("a missing file read as %+v", state)
	}
	path := publicStatePath(root)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if state := readPublicState(root); state != (publicState{}) {
		t.Errorf("a broken file read as %+v", state)
	}
}

// The link is what a person is given, so it has to be one that works when it
// is pasted: the key on the address, nothing else to assemble.
func TestTheLinkCarriesTheKey(t *testing.T) {
	got := keyedURL("http://192.168.0.5:11541", "abc-123")
	if got != "http://192.168.0.5:11541/?k=abc-123" {
		t.Errorf("keyedURL() = %q", got)
	}
	if got := keyedURL("http://192.168.0.5:11541", ""); strings.Contains(got, "?") {
		t.Errorf("a server with no key was given a link with one: %q", got)
	}
}
