package session

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"testing"
	"time"

	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/platform/ktf"
	"github.com/movingwoo/wfeature/internal/platform/skt/clet"
	"github.com/movingwoo/wfeature/internal/testfixture"
	"golang.org/x/text/encoding/korean"
)

func sktCletArchive(t *testing.T, options testfixture.SKTCletOptions) []byte {
	t.Helper()
	if testfixture.SKTCletImageBase != clet.ImageBase {
		t.Fatalf("fixture image base %#x differs from the engine's %#x", testfixture.SKTCletImageBase, clet.ImageBase)
	}
	archive, err := testfixture.SKTCletArchive(options, nil)
	if err != nil {
		t.Fatal(err)
	}
	return archive
}

func sktCletOptions(store backend.SaveStore) Options {
	return Options{SaveStore: store, Clock: ktf.NewManualClock(time.Time{})}
}

func sktCletStart(t *testing.T, archive []byte, store backend.SaveStore) *Session {
	t.Helper()
	s, err := Start(t.Context(), archive, sktCletOptions(store))
	if err != nil {
		t.Fatal(err)
	}
	if s.cletPlatform() == nil {
		t.Fatal("the fixture did not start as an SKT Clet")
	}
	return s
}

func sktCletMemory(t *testing.T, s *Session, address, size uint32) []byte {
	t.Helper()
	data := make([]byte, size)
	if err := s.cletPlatform().Client.Core().Memory().Read(address, data); err != nil {
		t.Fatal(err)
	}
	return data
}

func sktCletWord(t *testing.T, s *Session, address uint32) uint32 {
	return binary.LittleEndian.Uint32(sktCletMemory(t, s, address, 4))
}

func sktCletText(t *testing.T, s *Session) []byte {
	t.Helper()
	length := sktCletWord(t, s, testfixture.SKTCletTextLength)
	if length > testfixture.SKTCletTextLimit {
		t.Fatalf("field length %d", length)
	}
	return sktCletMemory(t, s, testfixture.SKTCletText, length)
}

type sktCletSample struct {
	flushes uint64
	frame   [32]byte
	elapsed time.Duration
}

// sktCletStep presses the keys given for this tick, releases them, and runs a
// round, the way the probes drive a title.
func sktCletStep(t *testing.T, s *Session, keys ...int32) sktCletSample {
	t.Helper()
	for _, key := range keys {
		if err := s.SendKey(t.Context(), KeyPress, key); err != nil {
			t.Fatal(err)
		}
		if err := s.SendKey(t.Context(), KeyRelease, key); err != nil {
			t.Fatal(err)
		}
	}
	progress, err := s.Tick(t.Context(), 50*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	s.SkipToNextDeadline()
	rgba, _, _, _ := s.Frame()
	elapsed, _ := s.GuestElapsed()
	return sktCletSample{flushes: progress.Flushes, frame: sha256.Sum256(rgba), elapsed: elapsed}
}

func sktCletSnapshot(t *testing.T, store *backend.MemorySaveStore) *backend.MemorySaveStore {
	t.Helper()
	entries, err := store.SnapshotSaves()
	if err != nil {
		t.Fatal(err)
	}
	copied, err := backend.NewMemorySaveStore(entries)
	if err != nil {
		t.Fatal(err)
	}
	return copied
}

func sktCletSaves(t *testing.T, store *backend.MemorySaveStore) map[string]string {
	t.Helper()
	entries, err := store.SnapshotSaves()
	if err != nil {
		t.Fatal(err)
	}
	saves := make(map[string]string, len(entries))
	for _, entry := range entries {
		saves[entry.Key] = string(entry.Data)
	}
	return saves
}

// A restored Clet does what the uninterrupted one did, tick for tick: the same
// frames, the same flushes, the same guest clock, the same writes through a
// file it had open at the capture, and the same saves at the end.
func TestCheckpointSKTCletContinuesIdentically(t *testing.T) {
	archive := sktCletArchive(t, testfixture.SKTCletOptions{FieldAtStart: true})
	store, _ := backend.NewMemorySaveStore(nil)
	original := sktCletStart(t, archive, store)
	defer original.Close()
	if !original.CanCheckpoint() {
		t.Fatal("an SKT Clet session cannot be asked for a checkpoint")
	}
	script := map[int][]int32{3: {'6'}, 6: {'7'}, 14: {'7'}, 17: {'5'}, 21: {'7', '5'}}
	for tick := range 10 {
		sktCletStep(t, original, script[tick]...)
	}
	data, err := original.CaptureCheckpoint(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	atCapture := sktCletSnapshot(t, store)
	var want []sktCletSample
	for tick := 10; tick < 30; tick++ {
		want = append(want, sktCletStep(t, original, script[tick]...))
	}
	counted := sktCletWord(t, original, testfixture.SKTCletCounter)

	restoredStore := atCapture
	restored, err := RestoreCheckpoint(t.Context(), archive, data, sktCletOptions(restoredStore))
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	restored.ResumeCheckpointOutput()
	for index, tick := 0, 10; tick < 30; index, tick = index+1, tick+1 {
		if got := sktCletStep(t, restored, script[tick]...); got != want[index] {
			t.Fatalf("tick %d differs after the restore: %+v, want %+v", tick, got, want[index])
		}
	}
	if got := sktCletWord(t, restored, testfixture.SKTCletCounter); got != counted || counted < 20 {
		t.Fatalf("restored count %d, uninterrupted %d", got, counted)
	}
	got, wanted := sktCletSaves(t, restoredStore), sktCletSaves(t, store)
	if len(got[testfixture.SKTCletLogKey]) != 12 {
		t.Fatalf("the log is not the three counts written through its open handle: %x", got[testfixture.SKTCletLogKey])
	}
	for key, value := range wanted {
		if got[key] != value {
			t.Fatalf("save %s = %x after the restore, want %x", key, got[key], value)
		}
	}
}

// A load restores execution and nothing else: a save written after the
// capture is still the save after the load, and the restored program's open
// file reads the store as it is now.
func TestCheckpointSKTCletLoadKeepsLaterSaves(t *testing.T) {
	archive := sktCletArchive(t, testfixture.SKTCletOptions{})
	store, _ := backend.NewMemorySaveStore(nil)
	s := sktCletStart(t, archive, store)
	defer s.Close()
	for tick := range 6 {
		keys := map[int][]int32{1: {'6'}, 2: {'7'}, 3: {'5'}}[tick]
		sktCletStep(t, s, keys...)
	}
	data, err := s.CaptureCheckpoint(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	saved := sktCletSaves(t, store)
	// After the capture the program writes both files again: the save is
	// rewritten with a later count and the log grows.
	for range 4 {
		sktCletStep(t, s)
	}
	sktCletStep(t, s, '5', '7')
	later := sktCletSaves(t, store)
	if later[testfixture.SKTCletSaveKey] == saved[testfixture.SKTCletSaveKey] || len(later[testfixture.SKTCletLogKey]) != 8 {
		t.Fatalf("the fixture did not write after the capture: %x -> %x", saved, later)
	}
	if err := s.LoadCheckpoint(t.Context(), archive, data); err != nil {
		t.Fatal(err)
	}
	if got := sktCletSaves(t, store); got[testfixture.SKTCletSaveKey] != later[testfixture.SKTCletSaveKey] || got[testfixture.SKTCletLogKey] != later[testfixture.SKTCletLogKey] {
		t.Fatalf("the load changed the saves: %x, want %x", got, later)
	}
	// The log handle's cursor is where the capture left it, four bytes in,
	// over the file as it is now: the next append overwrites the second count.
	sktCletStep(t, s, '7')
	log := sktCletSaves(t, store)[testfixture.SKTCletLogKey]
	if len(log) != 8 || log[:4] != later[testfixture.SKTCletLogKey][:4] || log[4:] == later[testfixture.SKTCletLogKey][4:] {
		t.Fatalf("the restored handle did not write at its cursor over the current file: %x after %x", log, later[testfixture.SKTCletLogKey])
	}
}

func TestCheckpointSKTCletRefusals(t *testing.T) {
	archive := sktCletArchive(t, testfixture.SKTCletOptions{})
	store, _ := backend.NewMemorySaveStore(nil)
	s := sktCletStart(t, archive, store)
	defer s.Close()
	sktCletStep(t, s)
	data, err := s.CaptureCheckpoint(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	other := sktCletArchive(t, testfixture.SKTCletOptions{TimerKeys: true})
	if _, err := RestoreCheckpoint(t.Context(), other, data, sktCletOptions(store)); !errors.Is(err, backend.ErrCheckpointIdentity) {
		t.Fatalf("another archive's checkpoint loaded: %v", err)
	}
	options := sktCletOptions(store)
	options.DisableAuthentication = true
	if _, err := RestoreCheckpoint(t.Context(), archive, data, options); !errors.Is(err, backend.ErrCheckpointVersion) {
		t.Fatalf("a checkpoint loaded under another authentication setting: %v", err)
	}
	if _, err := RestoreCheckpoint(t.Context(), archive, data, Options{Clock: ktf.NewManualClock(time.Time{})}); err == nil {
		t.Fatal("a checkpoint loaded with no save store to read the saves from")
	}
	storeless, err := Start(t.Context(), archive, Options{Clock: ktf.NewManualClock(time.Time{})})
	if err != nil {
		t.Fatal(err)
	}
	defer storeless.Close()
	if _, err := storeless.CaptureCheckpoint(t.Context()); err == nil {
		t.Fatal("a session with no save store was captured")
	}
	// The record is untrusted: every damaged byte is refused or loads, and
	// none of them takes the process down or hangs it.
	checkpoint, err := backend.DecodeCheckpoint(data, backend.SaveIdentity(archive))
	if err != nil {
		t.Fatal(err)
	}
	for index := 0; index < len(checkpoint.Runtime); index += max(1, len(checkpoint.Runtime)/97) {
		damaged := checkpoint
		damaged.Runtime = bytes.Clone(checkpoint.Runtime)
		damaged.Runtime[index] ^= 0x5a
		encoded, err := backend.EncodeCheckpoint(damaged)
		if err != nil {
			t.Fatal(err)
		}
		restored, err := RestoreCheckpoint(t.Context(), archive, encoded, sktCletOptions(sktCletSnapshot(t, store)))
		if err == nil {
			sktCletStep(t, restored)
			restored.Close()
		}
	}
}

// The page's text box reaches the program's own field through its input
// method: each character is a carrier key the field hands to the input method,
// which answers it with the character as the completed text.
func TestTextInputSKTCletCommitsThroughTheField(t *testing.T) {
	for _, timerKeys := range []bool{false, true} {
		archive := sktCletArchive(t, testfixture.SKTCletOptions{FieldAtStart: true, TimerKeys: timerKeys})
		store, _ := backend.NewMemorySaveStore(nil)
		s := sktCletStart(t, archive, store)
		sktCletStep(t, s)
		if timerKeys {
			// A field that takes its keys on its frame is seen doing so before
			// its timer is known: the player's key goes through it first.
			sktCletStep(t, s, '1')
			sktCletStep(t, s)
		}
		edit, err := s.TextInput(t.Context())
		if err != nil {
			t.Fatalf("timer keys %t: no text input offered for an open field: %v", timerKeys, err)
		}
		if !edit.Append || edit.MaxLength != 64 {
			t.Fatalf("text input %+v", edit)
		}
		if err := edit.Commit(t.Context(), "가나A1"); err != nil {
			t.Fatalf("timer keys %t: commit: %v", timerKeys, err)
		}
		want, _ := korean.EUCKR.NewEncoder().Bytes([]byte("가나A1"))
		if got := sktCletText(t, s); !bytes.Equal(got, want) {
			t.Fatalf("timer keys %t: field holds %x, want %x", timerKeys, got, want)
		}
		// A spent snapshot is refused rather than typed twice.
		if err := edit.Commit(t.Context(), "B"); !errors.Is(err, backend.ErrTextInputChanged) {
			t.Fatalf("timer keys %t: a spent snapshot was committed again: %v", timerKeys, err)
		}
		s.Close()
	}
}

func TestTextInputSKTCletOffersOnlyAfterTheInputMethodIsUsed(t *testing.T) {
	archive := sktCletArchive(t, testfixture.SKTCletOptions{})
	store, _ := backend.NewMemorySaveStore(nil)
	s := sktCletStart(t, archive, store)
	defer s.Close()
	sktCletStep(t, s)
	if _, err := s.TextInput(t.Context()); !errors.Is(err, backend.ErrNoTextInput) {
		t.Fatalf("text input offered by a program that never used its input method: %v", err)
	}
	// The field hands a key to the input method, which is the evidence.
	sktCletStep(t, s, '1')
	edit, err := s.TextInput(t.Context())
	if err != nil {
		t.Fatalf("no text input after the field used its input method: %v", err)
	}
	if err := edit.Commit(t.Context(), "Z"); err != nil {
		t.Fatal(err)
	}
	if got := sktCletText(t, s); string(got) != "Z" {
		t.Fatalf("field holds %q", got)
	}
}

// A carrier the program takes as the key it is spends the offer: the screen
// has no field that takes keys through the input method. The offer comes back
// when the player presses a key or the program calls the input method again.
func TestTextInputSKTCletUnreachedCarrierWithdrawsTheOffer(t *testing.T) {
	archive := sktCletArchive(t, testfixture.SKTCletOptions{FieldAtStart: true, IgnoresCarrier: true})
	store, _ := backend.NewMemorySaveStore(nil)
	s := sktCletStart(t, archive, store)
	defer s.Close()
	sktCletStep(t, s)
	edit, err := s.TextInput(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if err := edit.Commit(t.Context(), "A"); !errors.Is(err, backend.ErrTextInputChanged) {
		t.Fatalf("a carrier nothing took was reported as %v", err)
	}
	if got := sktCletWord(t, s, testfixture.SKTCletLastKey); got != '0' {
		t.Fatalf("the program was not handed the carrier: last key %d", got)
	}
	if len(sktCletText(t, s)) != 0 {
		t.Fatal("text reached a field that never took the carrier")
	}
	if _, err := s.TextInput(t.Context()); !errors.Is(err, backend.ErrNoTextInput) {
		t.Fatalf("text input offered again after an unreached carrier: %v", err)
	}
	sktCletStep(t, s, '2')
	if _, err := s.TextInput(t.Context()); err != nil {
		t.Fatalf("a player's key did not bring the offer back: %v", err)
	}
}

func TestTextInputSKTCletRefusesWhatTheHandsetCannotEncode(t *testing.T) {
	archive := sktCletArchive(t, testfixture.SKTCletOptions{FieldAtStart: true})
	store, _ := backend.NewMemorySaveStore(nil)
	s := sktCletStart(t, archive, store)
	defer s.Close()
	sktCletStep(t, s)
	for _, text := range []string{"😀", "똠", "a\tb", string(make([]rune, 65))} {
		edit, err := s.TextInput(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		if err := edit.Commit(t.Context(), text); !errors.Is(err, backend.ErrInvalidTextInput) {
			t.Fatalf("%q was not refused: %v", text, err)
		}
	}
	if len(sktCletText(t, s)) != 0 {
		t.Fatal("refused text reached the field")
	}
}

// A digit field completes the player's own digits through the input method, and
// a quick load keeps a field open: the restored program is offered text at
// once, and its text is the field's value at the capture.
func TestTextInputSKTCletAcrossACheckpoint(t *testing.T) {
	archive := sktCletArchive(t, testfixture.SKTCletOptions{FieldAtStart: true, NumericField: true})
	store, _ := backend.NewMemorySaveStore(nil)
	s := sktCletStart(t, archive, store)
	defer s.Close()
	sktCletStep(t, s, '1', '2')
	if got := sktCletText(t, s); string(got) != "12" {
		t.Fatalf("the digit field holds %q", got)
	}
	data, err := s.CaptureCheckpoint(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	sktCletStep(t, s, '3')
	if err := s.LoadCheckpoint(t.Context(), archive, data); err != nil {
		t.Fatal(err)
	}
	if got := sktCletText(t, s); string(got) != "12" {
		t.Fatalf("the restored field holds %q", got)
	}
	edit, err := s.TextInput(t.Context())
	if err != nil {
		t.Fatalf("no text input right after a load: %v", err)
	}
	if err := edit.Commit(t.Context(), "34"); err != nil {
		t.Fatal(err)
	}
	if got := sktCletText(t, s); string(got) != "1234" {
		t.Fatalf("the field holds %q after the commit", got)
	}
}

// A checkpoint taken while the game is paused restores paused, and resuming
// both runs the program's own resume entry and then the same frames.
func TestCheckpointSKTCletWhilePaused(t *testing.T) {
	archive := sktCletArchive(t, testfixture.SKTCletOptions{})
	store, _ := backend.NewMemorySaveStore(nil)
	original := sktCletStart(t, archive, store)
	defer original.Close()
	for range 4 {
		sktCletStep(t, original)
	}
	if err := original.Pause(t.Context()); err != nil {
		t.Fatal(err)
	}
	data, err := original.CaptureCheckpoint(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	atCapture := sktCletSnapshot(t, store)
	if err := original.Resume(t.Context()); err != nil {
		t.Fatal(err)
	}
	var want []sktCletSample
	for range 8 {
		want = append(want, sktCletStep(t, original))
	}
	restored, err := RestoreCheckpoint(t.Context(), archive, data, sktCletOptions(atCapture))
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	if !restored.Paused() {
		t.Fatal("a checkpoint taken while paused restored running")
	}
	if err := restored.Resume(t.Context()); err != nil {
		t.Fatal(err)
	}
	for index := range 8 {
		if got := sktCletStep(t, restored); got != want[index] {
			t.Fatalf("tick %d after resuming differs: %+v, want %+v", index, got, want[index])
		}
	}
}

// What a Host shows as a Clet's authentication is what its loader applied
// before the program started. Session.Authentication asked every engine but
// this one, so a Clet whose certificate the registry adapted was reported as
// having no authentication support at all.
func TestTheAuthenticationACletStartedUnderIsReported(t *testing.T) {
	archive := sktCletArchive(t, testfixture.SKTCletOptions{})
	for _, test := range []struct {
		applied, want backend.AuthenticationStatus
	}{
		{backend.AuthenticationSKTNativeCertificate, backend.AuthenticationSKTNativeCertificate},
		{backend.AuthenticationSKTNativeSaveSubscriber, backend.AuthenticationSKTNativeSaveSubscriber},
		{"", backend.AuthenticationOff},
	} {
		program, _, err := sktNativeProgram(archive, false)
		if err != nil {
			t.Fatal(err)
		}
		program.Authentication = test.applied
		store, _ := backend.NewMemorySaveStore(nil)
		started, err := clet.StartCletSession(t.Context(), program, cletOptions(sktCletOptions(store)))
		if err != nil {
			t.Fatal(err)
		}
		running := &Session{engine: cletEngine{started}}
		got := running.Authentication()
		running.Close()
		if got != test.want {
			t.Errorf("a Clet started under %q reported %q, want %q", test.applied, got, test.want)
		}
	}
}
