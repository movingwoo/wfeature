package lgt

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/movingwoo/wfeature/internal/armcore"
	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/platform/compatibility"
	"github.com/movingwoo/wfeature/internal/testlocal"
	"github.com/movingwoo/wfeature/internal/wipic"
)

// The compatibility registry's fixes are tested on authored fixtures beside
// each fix. The tests in this file check an entry against the original archive
// it recognizes, which stays local: each reads an absolute archive path from
// its own environment variable.

func TestLocalCertificateMessageSelection(t *testing.T) {
	path := testlocal.Value(t, "WFEATURE_LGT_CERTIFICATE_MESSAGE_ARCHIVE", "a local archive")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	original := bytes.Clone(data)
	for _, disabled := range []bool{false, true} {
		s, err := StartSession(context.Background(), data, SessionOptions{DisableAuthentication: disabled, SaveRoot: t.TempDir(), Width: 240, Height: 320})
		if err != nil {
			t.Fatal(err)
		}
		status := backend.AuthenticationLGTCertificateMessage
		if disabled {
			status = backend.AuthenticationOff
		}
		if s.Authentication() != status || (s.client.notificationNetwork == nil) != disabled {
			t.Fatal("incorrect session selection")
		}
		if err := s.Close(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if !bytes.Equal(data, original) {
		t.Fatal("original archive changed")
	}
}

func TestLocalSaveSubscriberArchive(t *testing.T) {
	path := testlocal.Value(t, "WFEATURE_LGT_SAVE_SUBSCRIBER_ARCHIVE", "a local archive")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	original := bytes.Clone(data)
	originalNumber := wipic.ConfiguredSubscriberNumber()
	t.Cleanup(func() { setTestHandset(t, originalNumber) })
	for _, tc := range []struct {
		configured bool
		disabled   bool
		status     backend.AuthenticationStatus
		reader     [2]byte
		record     [2]byte
	}{
		{false, false, backend.AuthenticationLGTSaveSubscriber, [2]byte{0x02, 0xe0}, [2]byte{0x02, 0xd1}},
		{true, false, backend.AuthenticationGuestManaged, [2]byte{0x02, 0xd0}, [2]byte{0x06, 0xd1}},
		{true, true, backend.AuthenticationOff, [2]byte{0x02, 0xd0}, [2]byte{0x06, 0xd1}},
	} {
		wipic.ResetSubscriberNumber()
		if tc.configured {
			// Even an explicit selection of the default digits is a handset choice.
			setTestHandset(t, wipic.SubscriberNumber())
		}
		store := backend.NewDirectorySaveStore(t.TempDir())
		opts := SessionOptions{DisableAuthentication: tc.disabled, SaveStore: store, Width: 240, Height: 320}
		session, err := StartSession(context.Background(), data, opts)
		if err != nil {
			t.Fatal(err)
		}
		check := func(s *Session) {
			t.Helper()
			var reader, record [2]byte
			if err := s.client.core.Memory().Read(0x69d9a, reader[:]); err != nil {
				t.Fatal(err)
			}
			if err := s.client.core.Memory().Read(0x682da, record[:]); err != nil {
				t.Fatal(err)
			}
			if reader != tc.reader || record != tc.record || s.Authentication() != tc.status {
				t.Fatalf("configured=%v disabled=%v reader=%x record=%x status=%s", tc.configured, tc.disabled, reader, record, s.Authentication())
			}
		}
		check(session)
		if !tc.disabled {
			snapshot := session.client.subscriberNumber
			checkpoint, err := session.CaptureCheckpoint(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if tc.configured {
				wipic.ResetSubscriberNumber()
			} else {
				setTestHandset(t, "12")
			}
			prepared, err := PrepareSessionCheckpoint(data, checkpoint, opts)
			if err != nil {
				t.Fatal(err)
			}
			restored, err := prepared.Commit(context.Background(), session, store)
			prepared.Discard()
			if err != nil {
				t.Fatal(err)
			}
			session = restored
			check(session)
			if session.client.subscriberNumber != snapshot {
				t.Fatal("restored handset differs from the recorded handset")
			}
		}
		if err := session.Close(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if !bytes.Equal(data, original) {
		t.Fatal("original archive changed")
	}
}

// TestLocalOwnerRecordArchive checks the second recognized revision, from the
// absolute path in WFEATURE_LGT_OWNER_RECORD_ARCHIVE: the implicit handset
// accepts its owner record's subscriber, and a configured handset or disabled
// compatibility leaves the comparison as packaged.
func TestLocalOwnerRecordArchive(t *testing.T) {
	path := testlocal.Value(t, "WFEATURE_LGT_OWNER_RECORD_ARCHIVE", "a local archive")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	original := bytes.Clone(data)
	originalNumber := wipic.ConfiguredSubscriberNumber()
	t.Cleanup(func() { setTestHandset(t, originalNumber) })
	for _, tc := range []struct {
		configured bool
		disabled   bool
		status     backend.AuthenticationStatus
		branch     [2]byte
	}{
		{false, false, backend.AuthenticationLGTSaveSubscriber, [2]byte{0xc0, 0x46}},
		{true, false, backend.AuthenticationGuestManaged, [2]byte{0x0b, 0xd1}},
		{false, true, backend.AuthenticationOff, [2]byte{0x0b, 0xd1}},
	} {
		wipic.ResetSubscriberNumber()
		if tc.configured {
			setTestHandset(t, wipic.SubscriberNumber())
		}
		session, err := StartSession(context.Background(), data, SessionOptions{DisableAuthentication: tc.disabled, SaveRoot: t.TempDir(), Width: 240, Height: 320})
		if err != nil {
			t.Fatal(err)
		}
		var branch [2]byte
		if err := session.client.core.Memory().Read(0x70eb2, branch[:]); err != nil {
			t.Fatal(err)
		}
		if branch != tc.branch || session.Authentication() != tc.status {
			t.Errorf("configured=%v disabled=%v branch=%x status=%s", tc.configured, tc.disabled, branch, session.Authentication())
		}
		if err := session.Close(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if !bytes.Equal(data, original) {
		t.Fatal("original archive changed")
	}
}

// TestLocalArchivedSubscriberFirstLaunch starts both recognized archives on an
// empty save root, where every private file the title reads is the capture's.
// With the comparison as packaged one title ends itself at its failure notice
// and the other stays on it; accepted, both keep drawing new screens under
// the same presses.
func TestLocalArchivedSubscriberFirstLaunch(t *testing.T) {
	for _, variable := range []string{"WFEATURE_LGT_SAVE_SUBSCRIBER_ARCHIVE", "WFEATURE_LGT_OWNER_RECORD_ARCHIVE"} {
		t.Run(variable, func(t *testing.T) {
			path := testlocal.Value(t, variable, "a local archive")
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			originalNumber := wipic.ConfiguredSubscriberNumber()
			t.Cleanup(func() { setTestHandset(t, originalNumber) })
			wipic.ResetSubscriberNumber()
			launch := func(disabled bool) (exited bool, screens int) {
				t.Helper()
				session, err := StartSession(context.Background(), data, SessionOptions{DisableAuthentication: disabled, SaveRoot: t.TempDir(), Width: 240, Height: 320})
				if err != nil {
					t.Fatal(err)
				}
				defer session.Close(context.Background())
				const confirm = 0xFFFFFFFB
				late := make(map[uint64]bool)
				for tick := 0; tick < 1500; tick++ {
					switch tick % 150 {
					case 100:
						session.SendKey(true, confirm)
					case 103:
						session.SendKey(false, confirm)
					}
					if err := session.Tick(context.Background()); err != nil {
						if errors.Is(err, ErrGuestExited) {
							return true, len(late)
						}
						t.Fatal(err)
					}
					if tick >= 900 {
						late[session.FrameDigest()] = true
					}
				}
				return false, len(late)
			}
			if exited, screens := launch(true); !exited && screens != 1 {
				t.Fatalf("the packaged comparison neither ended the title nor held one screen (%d screens)", screens)
			}
			if exited, screens := launch(false); exited || screens < 2 {
				t.Fatalf("first launch: exited=%v with %d screens after tick 900", exited, screens)
			}
		})
	}
}

func TestLocalColorKeyCalibrationArchive(t *testing.T) {
	path := testlocal.Value(t, "WFEATURE_LGT_COLOR_KEY_ARCHIVE", "a local archive")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	archive, err := Open(data)
	if err != nil {
		t.Fatal(err)
	}
	original := bytes.Clone(archive.Module)
	check := func(want byte) {
		t.Helper()
		client, err := Load(archive, Options{})
		if err != nil {
			t.Fatal(err)
		}
		var code [2]byte
		if err := client.core.Memory().Read(0x1746, code[:]); err != nil {
			t.Fatal(err)
		}
		if code != [2]byte{want, 0x22} {
			t.Fatalf("sample coordinate instruction=%x", code)
		}
	}
	check(24)
	if !bytes.Equal(original, archive.Module) {
		t.Fatal("archive module changed")
	}
	archive.Descriptor.AID, archive.Descriptor.PID = "other", "other"
	check(24)
	archive.Module = bytes.Clone(original)
	archive.Module[15] ^= 1
	check(0)
}

func TestLocalDirectFramebufferOriginArchive(t *testing.T) {
	testLocalStripOrigin(t, "WFEATURE_LGT_DIRECT_ORIGIN_ARCHIVE", 0x12bc, 0x23)
}

func TestLocalImageFramebufferOriginArchive(t *testing.T) {
	testLocalStripOrigin(t, "WFEATURE_LGT_IMAGE_ORIGIN_ARCHIVE", 0x28a10, 0x22)
}

func TestLocalScreenStripConstantArchive(t *testing.T) {
	testLocalStripOrigin(t, "WFEATURE_LGT_SCREEN_STRIP_ARCHIVE", 0x2120, 0x23)
}

func testLocalStripOrigin(t *testing.T, variable string, address uint32, opcode byte) {
	t.Helper()
	path := testlocal.Value(t, variable, "a local archive")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	archive, err := Open(data)
	if err != nil {
		t.Fatal(err)
	}
	original := bytes.Clone(archive.Module)
	check := func(want byte) {
		t.Helper()
		client, err := Load(archive, Options{})
		if err != nil {
			t.Fatal(err)
		}
		var instruction [2]byte
		if err := client.core.Memory().Read(address, instruction[:]); err != nil {
			t.Fatal(err)
		}
		if instruction != [2]byte{want, opcode} {
			t.Fatalf("origin instruction = %x, want %02x%02x", instruction, want, opcode)
		}
	}
	check(0)
	if !bytes.Equal(original, archive.Module) {
		t.Fatal("archive module changed")
	}
	archive.Descriptor.AID, archive.Descriptor.PID = "other", "other"
	check(0)
	archive.Module = bytes.Clone(original)
	archive.Module[15] ^= 1 // Loader-ignored ELF padding still changes the identity.
	check(24)
}

// WFEATURE_LGT_ORIGIN_ARCHIVE names an ignored local archive. Verify matching
// through the real loader, without starting gameplay or touching saved data.
func TestLocalFramebufferOriginArchive(t *testing.T) {
	path := testlocal.Value(t, "WFEATURE_LGT_ORIGIN_ARCHIVE", "a local archive")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	archive, err := Open(data)
	if err != nil {
		t.Fatal(err)
	}
	original := bytes.Clone(archive.Module)
	if !compatibility.IdentifyNative("lgt", original).Has(compatibility.LGTVisibleFramebufferOrigin) {
		t.Fatal("original module did not select correction")
	}
	check := func(want byte) {
		t.Helper()
		client, err := Load(archive, Options{})
		if err != nil {
			t.Fatal(err)
		}
		var instruction [2]byte
		if err := client.core.Memory().Read(framebufferOriginAddress+4, instruction[:]); err != nil {
			t.Fatal(err)
		}
		if instruction != [2]byte{want, 0x33} {
			t.Fatalf("loaded origin instruction = %x, want %02x33", instruction, want)
		}
	}
	check(0)
	if !bytes.Equal(original, archive.Module) {
		t.Fatal("loader changed original module")
	}
	archive.Descriptor.AID = "other"
	archive.Descriptor.PID = "other"
	archive.Resources["unrelated"] = []byte("other")
	check(0)
	// Even a change outside loadable code must reject this exact-module fix.
	archive.Module = bytes.Clone(original)
	archive.Module[15] ^= 1 // ELF identification padding, ignored by the loader.
	if compatibility.IdentifyNative("lgt", archive.Module).Has(compatibility.LGTVisibleFramebufferOrigin) {
		t.Fatal("module with changed padding selected correction")
	}
	check(24)
	// One changed instruction is a different revision, even with identical
	// descriptor and resources. It must retain its own initialization.
	archive.Module = bytes.Clone(original)
	archive.Module[0x1dfc4] = 25
	if compatibility.IdentifyNative("lgt", archive.Module).Has(compatibility.LGTVisibleFramebufferOrigin) {
		t.Fatal("changed module selected correction")
	}
	check(25)
}

// TestLocalRecordTableArchive checks the recognized revision itself, from the
// absolute path in WFEATURE_LGT_RECORD_TABLE_ARCHIVE: its module is selected,
// the search for each of the two maps without a record runs past the table as
// packaged, and leaves by the exit for a missing record once corrected.
func TestLocalRecordTableArchive(t *testing.T) {
	path := testlocal.Value(t, "WFEATURE_LGT_RECORD_TABLE_ARCHIVE", "a local archive")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	archive, err := Open(data)
	if err != nil {
		t.Fatal(err)
	}
	if !compatibility.IdentifyNative("lgt", archive.Module).Has(compatibility.LGTRecordTableTerminator) {
		t.Fatal("the archive's module is not selected")
	}
	packaged, ok := archive.Resource(recordTableName)
	if !ok {
		t.Fatalf("the archive has no %s", recordTableName)
	}
	packaged = bytes.Clone(packaged)
	client, err := Load(archive, Options{Width: 240, Height: 320})
	if err != nil {
		t.Fatal(err)
	}
	corrected, ok := client.titleFile(recordTableName)
	if !ok || !bytes.Equal(corrected, append(bytes.Clone(packaged), recordTableEnd...)) {
		t.Fatalf("the title read %d bytes for a %d-byte table", len(corrected), len(packaged))
	}
	for _, missing := range []byte{124, 126} {
		if got := searchRecord(packaged, 0, missing); got != "past" {
			t.Fatalf("map %d over the packaged table answered %s", missing, got)
		}
		if got := searchRecord(corrected, 0, missing); got != "end" {
			t.Fatalf("map %d over the corrected table answered %s", missing, got)
		}
	}
	if current, _ := archive.Resource(recordTableName); !bytes.Equal(current, packaged) {
		t.Fatal("the archive's own copy changed")
	}
}

// The packaged-certificate fix aliases a recognized archive's shipped
// certificate to the bare name the title opens it by. The original archive
// recognizes it; a byte changed in the module does not, so no alias is added.
func TestLocalPackagedCertificateArchive(t *testing.T) {
	location := testlocal.Value(t, "WFEATURE_LGT_PACKAGED_CERTIFICATE_ARCHIVE", "a local archive")
	data, err := os.ReadFile(location)
	if err != nil {
		t.Fatal(err)
	}
	archive, err := Open(data)
	if err != nil {
		t.Fatal(err)
	}
	served, ok := archive.Resource(certificateBareName)
	if !ok {
		t.Fatalf("the recognized archive does not serve %q", certificateBareName)
	}
	var packaged []byte
	for name, bytes := range archive.Packaged {
		if path.Base(name) == certificateBareName {
			packaged = bytes
		}
	}
	if packaged == nil || string(served) != string(packaged) {
		t.Fatalf("%q is not served from the packaged certificate", certificateBareName)
	}
	if !compatibility.IdentifyNative("lgt", archive.Module).Has(compatibility.LGTPackagedCertificate) {
		t.Fatal("the original module is not recognized")
	}
	changed := append([]byte(nil), archive.Module...)
	changed[len(changed)/2] ^= 0xff
	if compatibility.IdentifyNative("lgt", changed).Has(compatibility.LGTPackagedCertificate) {
		t.Fatal("a changed module is still recognized")
	}
}

// TestLocalDataServiceLockArchive checks the registered module itself, from the
// absolute path in WFEATURE_LGT_DATA_SERVICE_LOCK_ARCHIVE. Agreeing to its
// first-run consent opens the network scene. Answered a locked data service,
// the scene takes state 9 and sets its lock word, the title's own notice, and
// OK clears the word and leaves; answered as every other title is, it dials
// and stays in state 7 with the same key ignored. Compatibility that is
// disabled changes neither.
func TestLocalDataServiceLockArchive(t *testing.T) {
	path := testlocal.Value(t, "WFEATURE_LGT_DATA_SERVICE_LOCK_ARCHIVE", "a local archive")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	const (
		confirm, agree = 0xFFFFFFFB, 0x31
		sceneState     = 0x0156dad0
		lockWord       = 0x015078a0
		locked, dialed = 9, 7
	)
	launch := func(disabled, answered bool) (state, notice, left uint32) {
		t.Helper()
		session, err := StartSession(context.Background(), data, SessionOptions{DisableAuthentication: disabled, SaveRoot: t.TempDir(), Width: 240, Height: 320})
		if err != nil {
			t.Fatal(err)
		}
		defer session.Close(context.Background())
		if !session.client.dataServiceLock {
			t.Fatal("the registered module was not selected")
		}
		session.client.dataServiceLock = answered
		read := func(address uint32) uint32 {
			t.Helper()
			var word [4]byte
			if err := session.client.core.Memory().Read(address, word[:]); err != nil {
				t.Fatal(err)
			}
			return binary.LittleEndian.Uint32(word[:])
		}
		press := func(key uint32, ticks int) {
			t.Helper()
			session.SendKey(true, key)
			for tick := 0; tick < ticks; tick++ {
				if tick == 3 {
					session.SendKey(false, key)
				}
				if err := session.Tick(context.Background()); err != nil {
					t.Fatal(err)
				}
			}
		}
		for tick := 0; tick < 250; tick++ {
			if err := session.Tick(context.Background()); err != nil {
				t.Fatal(err)
			}
		}
		press(confirm, 150)
		press(agree, 200)
		state, notice = read(sceneState), read(lockWord)
		press(confirm, 200)
		return state, notice, read(lockWord)
	}
	for _, disabled := range []bool{false, true} {
		if state, notice, left := launch(disabled, true); state != locked || notice != 1 || left != 0 {
			t.Errorf("disabled=%v, answered: scene state %d, lock word %d at the notice and %d after OK", disabled, state, notice, left)
		}
		if state, notice, left := launch(disabled, false); state != dialed || notice != 0 || left != 0 {
			t.Errorf("disabled=%v, not answered: scene state %d, lock word %d and %d", disabled, state, notice, left)
		}
	}
}

// TestLocalNullPageArchive checks the registered modules themselves, from the
// absolute paths in WFEATURE_LGT_NULL_PAGE_ARCHIVES (separated by the system's
// path-list separator). The weapon key pressed in the opening of a new game
// reads through a null pointer; the run goes on, and a record without the page
// in its memory map, which an earlier build took, restores with it.
func TestLocalNullPageArchive(t *testing.T) {
	paths := testlocal.Value(t, "WFEATURE_LGT_NULL_PAGE_ARCHIVES", "local archives")
	for _, path := range filepath.SplitList(paths) {
		t.Run(filepath.Base(path), func(t *testing.T) {
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			session, err := StartSession(context.Background(), data, SessionOptions{SaveRoot: t.TempDir(), Width: 240, Height: 320})
			if err != nil {
				t.Fatal(err)
			}
			defer session.Close(context.Background())
			const confirm, weapon = 0xFFFFFFFB, 0x31
			press := func(key uint32, ticks int) {
				t.Helper()
				session.SendKey(true, key)
				for tick := 0; tick < ticks; tick++ {
					if tick == 3 {
						session.SendKey(false, key)
					}
					if err := session.Tick(context.Background()); err != nil {
						t.Fatal(err)
					}
				}
			}
			for tick := 0; tick < 250; tick++ {
				if err := session.Tick(context.Background()); err != nil {
					t.Fatal(err)
				}
			}
			for range 9 {
				press(confirm, 110)
			}
			press(weapon, 200)
			saved, err := session.client.captureClientState()
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Contains(saved.Core.Memory.Mappings, nullPageMapping) {
				t.Fatal("the record has no null page")
			}
			saved.Core.Memory.Mappings = slices.DeleteFunc(slices.Clone(saved.Core.Memory.Mappings), func(mapping armcore.MappingState) bool {
				return mapping == nullPageMapping
			})
			restored, err := restoreClientState(&moduleLoad{archive: session.archive}, saved, Options{SaveStore: backend.NewDetachedSaveStore(), MaxSteps: session.client.core.MaxSteps()})
			if err != nil {
				t.Fatalf("a record without the page: %v", err)
			}
			if err := restored.core.Memory().Read(0, make([]byte, 4)); err != nil {
				t.Fatalf("the restored client has no null page: %v", err)
			}
		})
	}
}

// TestLocalAuthenticationAllowanceArchive checks the registered modules from
// the absolute paths in WFEATURE_LGT_AUTHENTICATION_ALLOWANCE_ARCHIVES. Yes on
// the one-time authentication prompt fails, and OK on the failure notice lets
// the title record the authentication in its setup file; with compatibility
// disabled the same keys bring the prompt back and nothing is recorded.
func TestLocalAuthenticationAllowanceArchive(t *testing.T) {
	paths := testlocal.Value(t, "WFEATURE_LGT_AUTHENTICATION_ALLOWANCE_ARCHIVES", "local archives")
	for _, path := range filepath.SplitList(paths) {
		t.Run(filepath.Base(path), func(t *testing.T) {
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			launch := func(disabled bool) (backend.AuthenticationStatus, bool) {
				t.Helper()
				root := t.TempDir()
				session, err := StartSession(context.Background(), data, SessionOptions{DisableAuthentication: disabled, SaveRoot: root, Width: 240, Height: 320})
				if err != nil {
					t.Fatal(err)
				}
				defer session.Close(context.Background())
				const confirm = 0xFFFFFFFB
				for tick := 0; tick < 250+2*110; tick++ {
					switch tick {
					case 250, 360:
						session.SendKey(true, confirm)
					case 253, 363:
						session.SendKey(false, confirm)
					}
					if err := session.Tick(context.Background()); err != nil {
						t.Fatal(err)
					}
				}
				recorded := false
				if err := filepath.WalkDir(root, func(name string, entry fs.DirEntry, err error) error {
					if err == nil && entry.Type().IsRegular() && strings.EqualFold(strings.ReplaceAll(entry.Name(), "#", ""), "setup.dat") {
						recorded = true
					}
					return err
				}); err != nil {
					t.Fatal(err)
				}
				return session.Authentication(), recorded
			}
			if status, recorded := launch(false); status != backend.AuthenticationLGTAuthenticationAllowance || !recorded {
				t.Errorf("compatibility: status %s, setup recorded %v", status, recorded)
			}
			if status, recorded := launch(true); status != backend.AuthenticationOff || recorded {
				t.Errorf("disabled: status %s, setup recorded %v", status, recorded)
			}
		})
	}
}
