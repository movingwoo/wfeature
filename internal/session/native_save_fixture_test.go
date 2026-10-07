package session

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/cheat"
	"github.com/movingwoo/wfeature/internal/platform/ktf"
	"github.com/movingwoo/wfeature/internal/testfixture"
)

func nativeSaveFixtureWord(t *testing.T, s *Session, address uint32) uint32 {
	t.Helper()
	data, err := s.Cheat().ReadBytes(address, 4)
	if err != nil {
		t.Fatal(err)
	}
	return binary.LittleEndian.Uint32(data)
}

// nativeSaveFixtureProgress writes the word the module saves. The cheat engine
// is the one way a Host has to write guest memory, and a plain write through it
// leaves nothing behind that a checkpoint refuses.
func nativeSaveFixtureProgress(t *testing.T, s *Session, value uint32) {
	t.Helper()
	word := cheat.ValueType{Kind: cheat.KindU32, Endian: cheat.Little}
	if err := s.Cheat().WriteValue(testfixture.KTFNativeSaveProgress, word, int64(value)); err != nil {
		t.Fatal(err)
	}
}

func nativeSaveFixturePress(t *testing.T, s *Session, key int32) {
	t.Helper()
	for _, action := range []string{KeyPress, KeyRelease} {
		if err := s.SendKey(t.Context(), action, key); err != nil {
			t.Fatalf("key %q: %v", key, err)
		}
	}
}

// The native save fixture through the calls a Host makes. The shared session
// classifies and starts the archive, takes keys in the codes a page sends, and
// keeps the save in a directory store under the owner the fixture exports;
// that is the route a storage test drives, so it is the route the fixture is
// checked on. The load is the one this branch has: nothing is written between
// the capture and the load, so what the test reads afterwards does not depend
// on whether a load puts saves back.
func TestNativeSaveFixtureThroughTheSharedSession(t *testing.T) {
	const first, second, third = 0x11111111, 0x22222222, 0x33333333
	for _, form := range []struct {
		name  string
		build func() ([]byte, error)
		frame bool
	}{
		{"frame callback", testfixture.KTFNativeSaveArchive, true},
		{"no frame callback", testfixture.KTFNativeSaveArchiveWithoutFrame, false},
	} {
		t.Run(form.name, func(t *testing.T) {
			archive, err := form.build()
			if err != nil {
				t.Fatal(err)
			}
			root := filepath.Join(t.TempDir(), testfixture.KTFNativeSaveOwner)
			file := filepath.Join(root, filepath.FromSlash(testfixture.KTFNativeSaveStoreKey))
			onDisk := func(when string, want []byte) {
				t.Helper()
				got, err := os.ReadFile(file)
				if want == nil && os.IsNotExist(err) {
					return
				}
				if err != nil || want == nil || !bytes.Equal(got, want) {
					t.Fatalf("%s: the save file holds %x (%v), want %x", when, got, err, want)
				}
			}
			s, err := Start(t.Context(), archive, Options{
				SaveStore: backend.NewDirectorySaveStore(root), Clock: ktf.NewManualClock(time.Unix(1, 0)), Width: 32, Height: 48})
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			if s.Platform() != "ktf" || s.ktfNative == nil || s.Summary().SaveOwner != testfixture.KTFNativeSaveOwner || !s.CanCheckpoint() {
				t.Fatalf("the fixture did not start as a native package a Host can checkpoint: %+v", s.Summary())
			}
			frame := func() {
				t.Helper()
				if !form.frame {
					// Nothing is scheduled, and a tick is still a call a Host makes.
					if progress, err := s.Tick(t.Context(), 0); err != nil || progress.Progressed {
						t.Fatalf("tick with no frame callback = %+v, %v", progress, err)
					}
					return
				}
				if !s.SkipToNextDeadline() {
					t.Fatal("the fixture has no frame scheduled")
				}
				if progress, err := s.Tick(t.Context(), 0); err != nil || !progress.Progressed {
					t.Fatalf("frame = %+v, %v", progress, err)
				}
			}
			read := func() [4]uint32 {
				t.Helper()
				nativeSaveFixturePress(t, s, testfixture.KTFNativeSaveKeyRead)
				return [4]uint32{
					nativeSaveFixtureWord(t, s, testfixture.KTFNativeSaveSeenA), nativeSaveFixtureWord(t, s, testfixture.KTFNativeSaveSeenB),
					nativeSaveFixtureWord(t, s, testfixture.KTFNativeSaveStatus), nativeSaveFixtureWord(t, s, testfixture.KTFNativeSaveLength)}
			}

			if got := read(); got != [4]uint32{0, 0, testfixture.KTFNativeSaveStatusMissing, 0} {
				t.Fatalf("READ with no save = %x", got)
			}
			onDisk("after READ of a missing save", nil)
			nativeSaveFixtureProgress(t, s, first)
			nativeSaveFixturePress(t, s, testfixture.KTFNativeSaveKeySave)
			onDisk("after SAVE", testfixture.KTFNativeSaveFile(first, first))
			frame()

			data, err := s.CaptureCheckpoint(t.Context())
			if err != nil {
				t.Fatalf("the fixture cannot be captured: %v", err)
			}
			frames := nativeSaveFixtureWord(t, s, testfixture.KTFNativeSaveFrameCounter)
			nativeSaveFixtureProgress(t, s, 0x7f7f7f7f)
			frame()
			if err := s.LoadCheckpoint(t.Context(), archive, data); err != nil {
				t.Fatalf("the fixture's checkpoint cannot be loaded: %v", err)
			}
			if nativeSaveFixtureWord(t, s, testfixture.KTFNativeSaveProgress) != first ||
				nativeSaveFixtureWord(t, s, testfixture.KTFNativeSaveFrameCounter) != frames ||
				nativeSaveFixtureWord(t, s, testfixture.KTFNativeSaveStartupCounter) != 1 || len(s.HeldKeys()) != 0 {
				t.Fatal("the load did not bring back the module's own words, or replayed startup")
			}
			onDisk("after the load", testfixture.KTFNativeSaveFile(first, first))
			if got := read(); got != [4]uint32{first, first ^ testfixture.KTFNativeSaveMask, testfixture.KTFNativeSaveStatusFound, 8} {
				t.Fatalf("READ after the load = %x", got)
			}

			// A write no close followed is not on the disk when the key returns.
			// The frame's end carries it there in one form, and only the
			// session's close does in the other.
			nativeSaveFixtureProgress(t, s, second)
			nativeSaveFixturePress(t, s, testfixture.KTFNativeSaveKeyHold)
			onDisk("after HOLD", testfixture.KTFNativeSaveFile(first, first))
			frame()
			if !form.frame {
				onDisk("after HOLD and a tick", testfixture.KTFNativeSaveFile(first, first))
				s.Close()
				onDisk("after the session closed", testfixture.KTFNativeSaveFile(second, first))
				return
			}
			onDisk("a frame after HOLD", testfixture.KTFNativeSaveFile(second, first))
			nativeSaveFixtureProgress(t, s, third)
			nativeSaveFixturePress(t, s, testfixture.KTFNativeSaveKeyFinish)
			onDisk("after FINISH", testfixture.KTFNativeSaveFile(second, third))
		})
	}
}
