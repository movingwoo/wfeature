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

	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/platform/wipicrt"
	"github.com/movingwoo/wfeature/internal/testlocal"
	"github.com/movingwoo/wfeature/internal/wipic"
)

func TestLocalSubscriberReceiptRecognition(t *testing.T) {
	path := testlocal.Value(t, "WFEATURE_KTF_RECEIPT_ARCHIVE", "a local archive")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	archive, err := Open(data)
	if err != nil {
		t.Fatal(err)
	}
	base := wipicrt.NewDirectorySaveStore(t.TempDir())
	if _, ok := authenticationSubscriberReceipt(base, archive, "01012349876").(*subscriberReceiptStore); !ok {
		t.Fatal("local receipt revision not recognized")
	}
}

// TestLocalOwnerRecordArchive checks the registered client itself, from the
// absolute path in WFEATURE_KTF_OWNER_RECORD_ARCHIVE. The implicit handset
// accepts the owner record its capture carries; a configured handset and
// disabled compatibility leave the comparison as packaged. Started on an empty
// save root, the packaged comparison holds the title on one screen and the
// accepted one lets the same presses move through its screens.
func TestLocalOwnerRecordArchive(t *testing.T) {
	path := testlocal.Value(t, "WFEATURE_KTF_OWNER_RECORD_ARCHIVE", "a local archive")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	original := bytes.Clone(data)
	configuredNumber := wipic.ConfiguredSubscriberNumber()
	t.Cleanup(func() {
		wipic.ResetSubscriberNumber()
		if configuredNumber != "" {
			if err := wipic.SetSubscriberNumber(configuredNumber); err != nil {
				t.Fatal(err)
			}
		}
	})
	launch := func(configured, disabled bool) (backend.AuthenticationStatus, [2]byte, int) {
		t.Helper()
		wipic.ResetSubscriberNumber()
		if configured {
			if err := wipic.SetSubscriberNumber(wipic.SubscriberNumber()); err != nil {
				t.Fatal(err)
			}
		}
		session, err := StartSession(context.Background(), data, SessionOptions{
			DisableAuthentication: disabled, SaveRoot: t.TempDir(), Clock: wipicrt.NewManualClock(time.Time{}),
		})
		if err != nil {
			t.Fatal(err)
		}
		defer session.Close()
		var branch [2]byte
		if err := session.Client.Core().Memory().Read(0x13add6, branch[:]); err != nil {
			t.Fatal(err)
		}
		status := session.Authentication()
		late := make(map[uint64]bool)
		for round := 0; round < 1500; round++ {
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
			if round >= 900 {
				late[session.FrameDigest()] = true
			}
		}
		return status, branch, len(late)
	}
	packaged, accepted := [2]byte{0x06, 0xd1}, [2]byte{0xc0, 0x46}
	if status, branch, screens := launch(false, true); status != backend.AuthenticationOff || branch != packaged || screens != 1 {
		t.Errorf("disabled: status=%s branch=%x screens=%d", status, branch, screens)
	}
	if status, branch, screens := launch(true, false); status != backend.AuthenticationGuestManaged || branch != packaged || screens != 1 {
		t.Errorf("configured: status=%s branch=%x screens=%d", status, branch, screens)
	}
	if status, branch, screens := launch(false, false); status != backend.AuthenticationKTFSaveSubscriber || branch != accepted || screens < 2 {
		t.Errorf("implicit: status=%s branch=%x screens=%d", status, branch, screens)
	}
	if !bytes.Equal(data, original) {
		t.Fatal("original archive changed")
	}
}

// TestLocalSaveOwnerArchive checks the registered client itself, from the
// absolute path in WFEATURE_KTF_SAVE_OWNER_ARCHIVE. Its capture carries saves
// another handset wrote. The implicit handset and a configured one read them
// and start without writing a file: the title deletes what it refuses, so a
// configured handset is accepted too. Disabled compatibility leaves the
// comparison as packaged, and the title answers inside startApp by replacing
// the saves with its defaults: a routine that goes through a null pointer and
// completes on the page the registry maps for this client. All three then move
// through the title's screens.
func TestLocalSaveOwnerArchive(t *testing.T) {
	path := testlocal.Value(t, "WFEATURE_KTF_SAVE_OWNER_ARCHIVE", "a local archive")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	original := bytes.Clone(data)
	configuredNumber := wipic.ConfiguredSubscriberNumber()
	t.Cleanup(func() {
		wipic.ResetSubscriberNumber()
		if configuredNumber != "" {
			if err := wipic.SetSubscriberNumber(configuredNumber); err != nil {
				t.Fatal(err)
			}
		}
	})
	launch := func(configured, disabled bool) (backend.AuthenticationStatus, [2]byte, int, int) {
		t.Helper()
		wipic.ResetSubscriberNumber()
		if configured {
			if err := wipic.SetSubscriberNumber(wipic.SubscriberNumber()); err != nil {
				t.Fatal(err)
			}
		}
		root := t.TempDir()
		session, err := StartSession(context.Background(), data, SessionOptions{
			DisableAuthentication: disabled, SaveRoot: root, Clock: wipicrt.NewManualClock(time.Time{}),
		})
		if err != nil {
			t.Fatal(err)
		}
		defer session.Close()
		var branch [2]byte
		if err := session.Client.Core().Memory().Read(0x10fda2, branch[:]); err != nil {
			t.Fatal(err)
		}
		if err := session.Client.Core().Memory().Read(0, make([]byte, nullPageSize)); err != nil {
			t.Fatalf("null page: %v", err)
		}
		// What startApp left in the save root: the saves the title replaced.
		replaced := 0
		if err := filepath.WalkDir(root, func(_ string, entry fs.DirEntry, err error) error {
			if err == nil && entry.Type().IsRegular() && strings.HasSuffix(entry.Name(), ".dat") {
				replaced++
			}
			return err
		}); err != nil {
			t.Fatal(err)
		}
		status := session.Authentication()
		late := make(map[uint64]bool)
		for round := 0; round < 1200; round++ {
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
			if round >= 600 {
				late[session.FrameDigest()] = true
			}
		}
		return status, branch, replaced, len(late)
	}
	packaged, accepted := [2]byte{0x9f, 0xd1}, [2]byte{0xc0, 0x46}
	if status, branch, replaced, screens := launch(false, true); status != backend.AuthenticationOff || branch != packaged || replaced == 0 || screens < 2 {
		t.Errorf("disabled: status=%s branch=%x replaced=%d screens=%d", status, branch, replaced, screens)
	}
	if status, branch, replaced, screens := launch(true, false); status != backend.AuthenticationKTFSaveSubscriber || branch != accepted || replaced != 0 || screens < 2 {
		t.Errorf("configured: status=%s branch=%x replaced=%d screens=%d", status, branch, replaced, screens)
	}
	if status, branch, replaced, screens := launch(false, false); status != backend.AuthenticationKTFSaveSubscriber || branch != accepted || replaced != 0 || screens < 2 {
		t.Errorf("implicit: status=%s branch=%x replaced=%d screens=%d", status, branch, replaced, screens)
	}
	if !bytes.Equal(data, original) {
		t.Fatal("original archive changed")
	}
}

// TestLocalSaveSubscriberKeyArchive checks the registered client itself, from
// the absolute path in WFEATURE_KTF_SAVE_SUBSCRIBER_KEY_ARCHIVE. Its capture's
// saves are enciphered under the key of the handset the capture came from.
// Presented under the implicit handset's key, they are read as the title's
// own and nothing is rewritten; a configured handset and disabled
// compatibility read them as packaged, and the title replaces the options
// record with its defaults.
func TestLocalSaveSubscriberKeyArchive(t *testing.T) {
	path := testlocal.Value(t, "WFEATURE_KTF_SAVE_SUBSCRIBER_KEY_ARCHIVE", "a local archive")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	original := bytes.Clone(data)
	configuredNumber := wipic.ConfiguredSubscriberNumber()
	t.Cleanup(func() {
		wipic.ResetSubscriberNumber()
		if configuredNumber != "" {
			if err := wipic.SetSubscriberNumber(configuredNumber); err != nil {
				t.Fatal(err)
			}
		}
	})
	launch := func(configured, disabled bool) (backend.AuthenticationStatus, bool) {
		t.Helper()
		wipic.ResetSubscriberNumber()
		if configured {
			if err := wipic.SetSubscriberNumber(wipic.SubscriberNumber()); err != nil {
				t.Fatal(err)
			}
		}
		root := t.TempDir()
		session, err := StartSession(context.Background(), data, SessionOptions{
			DisableAuthentication: disabled, SaveRoot: root, Clock: wipicrt.NewManualClock(time.Time{}),
		})
		if err != nil {
			t.Fatal(err)
		}
		status := session.Authentication()
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
		rewritten := false
		if err := filepath.WalkDir(root, func(_ string, entry fs.DirEntry, err error) error {
			if err == nil && entry.Type().IsRegular() && strings.EqualFold(entry.Name(), "save.dat") {
				rewritten = true
			}
			return err
		}); err != nil {
			t.Fatal(err)
		}
		return status, rewritten
	}
	if status, rewritten := launch(false, false); status != backend.AuthenticationKTFSaveSubscriber || rewritten {
		t.Errorf("implicit: status=%s rewritten=%v", status, rewritten)
	}
	if status, rewritten := launch(true, false); status != backend.AuthenticationGuestManaged || !rewritten {
		t.Errorf("configured: status=%s rewritten=%v", status, rewritten)
	}
	if status, rewritten := launch(false, true); status != backend.AuthenticationOff || !rewritten {
		t.Errorf("disabled: status=%s rewritten=%v", status, rewritten)
	}
	if !bytes.Equal(data, original) {
		t.Fatal("original archive changed")
	}
}
