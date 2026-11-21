package ktf

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/movingwoo/wfeature/internal/platform/wipicrt"
	"github.com/movingwoo/wfeature/internal/testlocal"
)

// TestLocalDialFailureArchive checks the registered client itself, from the
// absolute path in WFEATURE_KTF_DIAL_FAILURE_ARCHIVE. Its capture carries the
// title's options record without the save it belongs to, so a first launch
// asks the title's server for one. The dial fails as every dial does here, the
// rewritten callback records the failure, and the saved-file check ends at the
// title's notice for a server that holds nothing: after OK the title sets the
// last byte of its ten-byte options record, which is what keeps a later launch
// from asking again. As packaged the byte stays clear and the title waits.
func TestLocalDialFailureArchive(t *testing.T) {
	path := testlocal.Value(t, "WFEATURE_KTF_DIAL_FAILURE_ARCHIVE", "a local archive")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	original := bytes.Clone(data)
	root := t.TempDir()
	session, err := StartSession(context.Background(), data, SessionOptions{SaveRoot: root, Clock: wipicrt.NewManualClock(time.Time{})})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	rewrite := dialFailureRewrites[dialFailureClient][0]
	code := make([]byte, len(rewrite.replace))
	if err := session.Client.Core().Memory().Read(rewrite.at+uint32(rewrite.offset), code); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(code, rewrite.replace) {
		t.Fatalf("dial callback = %x, want %x", code, rewrite.replace)
	}
	for round := 0; round < 2400; round++ {
		switch round % 150 {
		case 100:
			if err := session.SendKey(context.Background(), KeyPressed, wipicrt.KeyFire); err != nil {
				t.Fatal(err)
			}
		case 103:
			if err := session.SendKey(context.Background(), KeyReleased, wipicrt.KeyFire); err != nil {
				t.Fatal(err)
			}
		}
		session.SkipToNextDeadline()
		if _, err := session.Tick(context.Background()); err != nil {
			if errors.Is(err, ErrGuestExited) {
				break
			}
			t.Fatal(err)
		}
	}
	var options []byte
	if err := filepath.WalkDir(root, func(name string, entry fs.DirEntry, err error) error {
		if err == nil && entry.Type().IsRegular() && entry.Name() == "nvdata.dat" {
			options, err = os.ReadFile(name)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if len(options) != 10 || options[9] != 1 {
		t.Fatalf("options record = %x, want ten bytes ending in 01", options)
	}
	if !bytes.Equal(data, original) {
		t.Fatal("original archive changed")
	}
}
