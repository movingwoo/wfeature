package ktf

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/movingwoo/wfeature/internal/platform/wipicrt"
	"github.com/movingwoo/wfeature/internal/testlocal"
)

// TestLocalSaveFileTimeArchive checks the registered client itself, from the
// absolute path in WFEATURE_KTF_SAVE_FILE_TIME_ARCHIVE. Its capture carries a
// save that records the time the original handset gave its file. Started on
// an empty save root, the title keeps that save rather than deleting it and
// writing its defaults, with compatibility enabled or disabled: the file time
// is not an authentication.
func TestLocalSaveFileTimeArchive(t *testing.T) {
	path := testlocal.Value(t, "WFEATURE_KTF_SAVE_FILE_TIME_ARCHIVE", "a local archive")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	original := bytes.Clone(data)
	rewrite := saveFileTimeRewrites[saveFileTimeClient][0]
	for _, disabled := range []bool{false, true} {
		root := t.TempDir()
		session, err := StartSession(context.Background(), data, SessionOptions{DisableAuthentication: disabled, SaveRoot: root, Clock: wipicrt.NewManualClock(time.Time{})})
		if err != nil {
			t.Fatal(err)
		}
		code := make([]byte, len(rewrite.replace))
		if err := session.Client.Core().Memory().Read(rewrite.at+uint32(rewrite.offset), code); err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(code, rewrite.replace) {
			t.Errorf("disabled=%v: comparison = %x, want %x", disabled, code, rewrite.replace)
		}
		for round := 0; round < 400; round++ {
			session.SkipToNextDeadline()
			if _, err := session.Tick(context.Background()); err != nil {
				if errors.Is(err, ErrGuestExited) {
					break
				}
				t.Fatal(err)
			}
		}
		session.Close()
		replaced := ""
		if err := filepath.WalkDir(root, func(name string, entry fs.DirEntry, err error) error {
			if err == nil && entry.Type().IsRegular() && strings.EqualFold(strings.ReplaceAll(entry.Name(), "#", ""), "herofile1") {
				replaced = name
			}
			return err
		}); err != nil {
			t.Fatal(err)
		}
		if replaced != "" {
			t.Errorf("disabled=%v: the title replaced the save its capture carries", disabled)
		}
	}
	if !bytes.Equal(data, original) {
		t.Fatal("original archive changed")
	}
}
