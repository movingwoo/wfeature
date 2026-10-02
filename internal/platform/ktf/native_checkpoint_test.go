package ktf

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"image/color"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/movingwoo/wfeature/internal/armcore"
	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/testfixture"
)

func TestNativeCheckpointFixtureUsesOrdinaryStartupFramesAndInput(t *testing.T) {
	archive, err := testfixture.KTFNativeCheckpointArchive()
	if err != nil {
		t.Fatal(err)
	}
	clock := NewManualClock(time.Time{})
	session, err := StartNativeSession(t.Context(), archive, NativeSessionOptions{Clock: clock, Width: 32, Height: 48})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	read := func(address uint32) uint32 {
		t.Helper()
		var word [4]byte
		if err := session.Client.core.Memory().Read(address, word[:]); err != nil {
			t.Fatal(err)
		}
		return binary.LittleEndian.Uint32(word[:])
	}
	if read(testfixture.KTFNativeCheckpointStartupCounter) != 1 {
		t.Fatal("fixture did not run its startup event exactly once")
	}
	for round := 1; round <= 3; round++ {
		if !session.SkipToNextDeadline() {
			t.Fatal("fixture did not schedule its frame")
		}
		if progressed, err := session.Tick(t.Context()); err != nil || !progressed {
			t.Fatalf("fixture frame = %t, %v", progressed, err)
		}
		pixels, width, height, flushes := session.Frame()
		if width != 32 || height != 48 || flushes != uint32(round) || len(pixels) != width*height*4 || pixels[0] != byte(round) || pixels[3] != 255 || read(testfixture.KTFNativeCheckpointFrameCounter) != uint32(round) {
			t.Fatalf("frame %d did not fill and present through the platform: %dx%d, flushes=%d, pixel=%x", round, width, height, flushes, pixels[:min(4, len(pixels))])
		}
	}
	if err := session.SendKey(t.Context(), KeyPressed, KeyNum0+5); err != nil {
		t.Fatal(err)
	}
	if read(testfixture.KTFNativeCheckpointLastKey) != nativeKeyDigitBase+5 {
		t.Fatal("fixture did not receive a native key")
	}
	if read(testfixture.KTFNativeCheckpointStartupCounter) != 1 {
		t.Fatal("input replayed startup")
	}
}

// A checkpoint brings back the devices and the objects the title holds, and
// none of what its files held: an open file is read again from the store the
// restored session runs over, and the tables that shadow the store start empty.
func TestNativeCheckpointPreservesDevicesAndReopensFiles(t *testing.T) {
	archive, err := testfixture.KTFNativeCheckpointArchive()
	if err != nil {
		t.Fatal(err)
	}
	store := newNativeSaveFixtureStore(t)
	clock := NewManualClock(time.Unix(1, 0))
	sink := &countingSink{}
	source, err := StartNativeSession(t.Context(), archive, NativeSessionOptions{Clock: clock, SaveStore: store, AudioSink: sink, Width: 32, Height: 48})
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	p := source.platform
	put := func(session *NativeSession, data []byte) uint32 {
		t.Helper()
		address, err := session.Client.Allocate(uint32(len(data)))
		if err != nil {
			t.Fatal(err)
		}
		if err := session.Client.core.Memory().Write(address, data); err != nil {
			t.Fatal(err)
		}
		return address
	}
	data := buildResourceFile([]nativeResourceGroup{{Kind: 6, First: 1}}, [][]byte{[]byte("abc")})
	p.keep("state.dat", data)
	file := nativeCall(t, p.openFile, 0, nativeString(t, p, "state.dat"), 2)
	nativeCall(t, p.writeFile, file, put(source, data[:1]), 1)
	resource, ok, err := p.resourceFile("state.dat")
	if err != nil || !ok {
		t.Fatalf("resource = %t, %v", ok, err)
	}
	p.resourceFile("missing.dat")
	// A running session keeps the tables it parsed even when the file they
	// came from changes under them.
	nativeCall(t, p.seekFile, file, nativeSeekStart, 0)
	nativeCall(t, p.writeFile, file, put(source, []byte{0xff}), 1)
	if _, err := parseNativeResourceFile(p.files[file].data); err == nil {
		t.Fatal("header was not changed")
	}
	bitmap := buildBitmap(1, 1, []color.RGBA{{A: 255}, {R: 11, A: 255}, {R: 22, A: 255}}, []byte{1, 0, 0, 0})
	object := nativeCall(t, p.createObject, nativeClassImage, put(source, bitmap))
	pixel := binary.LittleEndian.Uint32(bitmap[bitmapPixelOffsetField:])
	if err := source.Client.core.Memory().Write(p.images[object].data+pixel, []byte{2}); err != nil {
		t.Fatal(err)
	}
	nativeCall(t, p.setClip, 0, 0, put(source, oneNoteSMAF()))
	nativeCall(t, p.playClip)
	clock.Advance(30 * time.Millisecond)
	p.audio.Advance(p.guestElapsed())
	if sink.noteOns != 1 {
		t.Fatalf("fixture has no live note: %d", sink.noteOns)
	}
	callback, counter := uint32(testfixture.KTFNativeCheckpointCallback), uint32(testfixture.KTFNativeCheckpointCallbackCounter)
	nativeCall(t, p.addListener(nativeInterfaceTimed), 0, callback, counter)
	nativeCall(t, p.addListener(nativeInterfaceSound), 0, callback, counter)
	nativeCall(t, p.startTimed, 0, 20)
	nativeCall(t, p.resume, 0, callback, counter)
	p.posted = []nativePostedEvent{{Class: testfixture.KTFNativeCheckpointApplicationID, Event: 0x1234, First: 57, Secon: 91}}
	p.fileFailure = nativeFileFailed
	p.colours = map[uint32]uint32{17: 0x11223300}
	saved, err := source.CaptureCheckpoint(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	// The capture gave the store the write that was waiting for a boundary,
	// so the record has none to carry.
	stored, found := store.LoadSave(nativeSaveKey("state.dat"))
	if !found || !bytes.Equal(stored, p.files[file].data) || len(p.unsaved) != 0 || len(p.refused) != 0 {
		t.Fatalf("the capture left a write waiting: stored=%t unsaved=%d refused=%d", found, len(p.unsaved), len(p.refused))
	}
	// A load installs no save, so the second session's store is a copy of the
	// first's as the capture left it: the same disk, seen by another process.
	freshClock := NewManualClock(time.Unix(500, 0))
	freshStore := newNativeSaveFixtureStore(t, store.snapshot(t)...)
	freshSink := &countingSink{}
	prepared, err := PrepareNativeSessionCheckpoint(archive, saved, NativeSessionOptions{Clock: freshClock, SaveStore: freshStore, AudioSink: freshSink})
	if err != nil {
		t.Fatal(err)
	}
	defer prepared.Discard()
	if calls, first := prepared.PreparationStoreCalls(); calls != 0 {
		t.Fatalf("checking the load made %d save store calls, the first being %s", calls, first)
	}
	freshClock.Advance(time.Hour)
	restored, err := prepared.Commit(t.Context(), nil, freshStore)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	r := restored.platform
	if freshSink.noteOns != 0 {
		t.Fatal("detached restoration emitted audio")
	}
	restored.ResumeCheckpointOutput()
	if freshSink.noteOns != 1 {
		t.Fatal("restored output did not reconstruct the playing note")
	}
	if r.images[object].frame.RGBAAt(0, 0).R != 11 {
		t.Fatal("restoration replaced the old decoded image with changed guest pixels")
	}
	if err := r.refresh(r.images[object]); err != nil {
		t.Fatal(err)
	}
	if r.images[object].frame.RGBAAt(0, 0).R != 22 {
		t.Fatal("restored image did not refresh from guest memory")
	}
	if err := p.refresh(p.images[object]); err != nil {
		t.Fatal(err)
	}
	if r.fileFailure != p.fileFailure || r.colours[17] != 0x11223300 {
		t.Fatal("file or display service state changed")
	}
	// Nothing of the files came with the record. The object is the one the
	// title held, at its cursor, over a copy of its own of what the store has.
	if len(r.written) != 0 || len(r.unsaved) != 0 || len(r.refused) != 0 || len(r.resources) != 0 {
		t.Fatalf("the load brought back file tables: written=%d unsaved=%d refused=%d resources=%d", len(r.written), len(r.unsaved), len(r.refused), len(r.resources))
	}
	handle := r.files[file]
	if handle == nil || handle.position != 1 || !handle.writable || handle.truncated || !bytes.Equal(handle.data, stored) || &handle.data[0] == &p.files[file].data[0] {
		t.Fatalf("the open file was not read again from the store: %+v", handle)
	}
	if freshStore.attempts != 0 {
		t.Fatalf("the load wrote to the store %d times", freshStore.attempts)
	}
	for _, pair := range []struct {
		session *NativeSession
		clock   *ManualClock
	}{{source, clock}, {restored, freshClock}} {
		pair.clock.Advance(100 * time.Millisecond)
		if _, err := pair.session.Tick(t.Context()); err != nil {
			t.Fatal(err)
		}
		count, err := pair.session.Client.ReadWord(counter)
		if err != nil || count != 3 {
			t.Fatalf("pending resume, timer and sound callbacks = %d, %v", count, err)
		}
	}
	if freshStore.attempts != 0 {
		t.Fatalf("the first frame after the load wrote to the store %d times", freshStore.attempts)
	}
	a, err := source.CaptureCheckpoint(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	b, err := restored.CaptureCheckpoint(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(a.Runtime, b.Runtime) {
		t.Fatal("complete native state differs after identical continuation")
	}
	// A write through the restored object is the restored title's own: it
	// lands in the file as the store has it and waits for a boundary like any
	// other.
	nativeCall(t, r.writeFile, file, put(restored, []byte{'Z'}), 1)
	if !r.unsaved["state.dat"] || &r.written["state.dat"][0] != &handle.data[0] || handle.data[1] != 'Z' || !bytes.Equal(handle.data[2:], stored[2:]) {
		t.Fatal("a write after the load did not go into the file as the store has it")
	}
	// A resource request after the load parses the file as it is now, where
	// the session that was running still answers from what it parsed before
	// the header changed.
	if _, ok, err := r.resourceFile("state.dat"); err != nil || ok {
		t.Fatalf("the restored session answered a resource from a file that is not one any more: %t, %v", ok, err)
	}
	if cached, ok, err := p.resourceFile("state.dat"); err != nil || !ok || cached != resource {
		t.Fatalf("the running session lost the tables it had parsed: %t, %v", ok, err)
	}
	if item, ok := resource.item(6, 1); !ok || len(item) != 3 {
		t.Fatal("the running session's parsed tables no longer answer")
	}
}

func TestNativeCheckpointRefusesMalformedStateBeforeAdoption(t *testing.T) {
	archive, err := testfixture.KTFNativeCheckpointArchive()
	if err != nil {
		t.Fatal(err)
	}
	store, _ := backend.NewMemorySaveStore([]backend.SaveEntry{{Key: "progress", Data: []byte("original")}})
	source, err := StartNativeSession(t.Context(), archive, NativeSessionOptions{Clock: NewManualClock(time.Time{}), SaveStore: store})
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	// One open file, so the record has a file entry for the cases to damage.
	if nativeCall(t, source.platform.openFile, 0, nativeString(t, source.platform, "Slot.dat"), 2) == 0 {
		t.Fatal("the fixture could not open a file")
	}
	saved, err := source.CaptureCheckpoint(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name   string
		mutate func(*nativeState)
	}{
		{"version", func(s *nativeState) { s.Version++ }},
		{"earlier version", func(s *nativeState) { s.Version = 1 }},
		{"mapping", func(s *nativeState) { s.Core.Memory.Mappings[0].Permission = armcore.PermissionReadWriteExecute }},
		{"budget", func(s *nativeState) { s.Core.MaxSteps++ }},
		{"allocator overlap", func(s *nativeState) { s.Blocks = append(s.Blocks, s.Blocks[0]) }},
		{"allocator gap", func(s *nativeState) { s.Blocks = nil }},
		{"surface", func(s *nativeState) { s.Surfaces[0] = "unknown" }},
		{"interface", func(s *nativeState) { s.Interfaces[0].Address = 3 }},
		{"screen overflow", func(s *nativeState) { s.Screen.Width = math.MaxInt }},
		{"deadline overflow", func(s *nativeState) { s.Frame.Remaining = math.MinInt64 }},
		{"callback", func(s *nativeState) { s.Frame.Function = 0xdeadbeef }},
		{"file key of another name", func(s *nativeState) { s.Files[0].Key = []byte("other.dat") }},
		{"file key in the name's own case", func(s *nativeState) { s.Files[0].Key = []byte("Slot.dat") }},
		{"file key the store refuses", func(s *nativeState) { s.Files[0].Name, s.Files[0].Key = []byte(".."), []byte("..") }},
		{"file key the store reduces", func(s *nativeState) { s.Files[0].Name, s.Files[0].Key = []byte("."), []byte(".") }},
		{"file name past the name limit", func(s *nativeState) {
			name := bytes.Repeat([]byte{'a'}, nativeMaxFileName)
			s.Files[0].Name, s.Files[0].Key = name, name
		}},
		{"file cursor before the start", func(s *nativeState) { s.Files[0].Position = -1 }},
		{"file cursor past the bound", func(s *nativeState) { s.Files[0].Position = nativeStateStorageLimit + 1 }},
		{"file length without an emptying open", func(s *nativeState) { s.Files[0].Truncated, s.Files[0].Length = false, 1 }},
		{"file length below zero", func(s *nativeState) { s.Files[0].Truncated, s.Files[0].Length = true, -1 }},
		{"file length past the bound", func(s *nativeState) { s.Files[0].Truncated, s.Files[0].Length = true, nativeStateStorageLimit+1 }},
		{"file object twice", func(s *nativeState) { s.Files = append(s.Files, s.Files[0]) }},
		{"file object unaligned", func(s *nativeState) { s.Files[0].Object |= 1 }},
		{"file object outside memory", func(s *nativeState) { s.Files[0].Object = 0xdeadbee0 }},
		{"audio handle", func(s *nativeState) { s.Clip = 42 }},
		{"speed", func(s *nativeState) { s.Speed = 100 }},
	} {
		t.Run(test.name, func(t *testing.T) {
			var record nativeState
			if err := backend.DecodeCheckpointRecord(saved.Runtime, &record); err != nil {
				t.Fatal(err)
			}
			test.mutate(&record)
			bad := saved
			bad.Runtime, err = backend.EncodeCheckpointRecord(record)
			if err != nil {
				t.Fatal(err)
			}
			candidate, err := PrepareNativeSessionCheckpoint(archive, bad, NativeSessionOptions{SaveStore: store})
			if candidate != nil {
				candidate.Discard()
			}
			if err == nil {
				t.Fatal("malformed native state was accepted")
			}
			// A record of another version is told apart from a damaged one:
			// it is whole, and this build does not read it.
			if strings.Contains(test.name, "version") != errors.Is(err, backend.ErrCheckpointVersion) {
				t.Fatalf("refused with %v", err)
			}
			if data, _ := store.LoadSave("progress"); string(data) != "original" {
				t.Fatal("refused preparation changed saves")
			}
		})
	}
	source.run.Lock()
	_, err = source.CaptureCheckpoint(t.Context())
	source.run.Unlock()
	if !errors.Is(err, ErrCheckpointBusy) {
		t.Fatalf("busy session = %v", err)
	}
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := source.CaptureCheckpoint(canceled); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled capture = %v", err)
	}
	prepared, err := PrepareNativeSessionCheckpoint(archive, saved, NativeSessionOptions{SaveStore: store})
	if err != nil {
		t.Fatal(err)
	}
	defer prepared.Discard()
	if calls, first := prepared.PreparationStoreCalls(); calls != 0 {
		t.Fatalf("validation made %d save store calls, the first being %s", calls, first)
	}
	old := source.Client
	if _, err := prepared.Commit(t.Context(), source, nil); err == nil || source.Client != old {
		t.Fatal("a load without a save store changed the live runtime")
	}
	if _, err := prepared.Commit(canceled, source, store); !errors.Is(err, context.Canceled) || source.Client != old {
		t.Fatalf("a canceled load = %v, or it changed the live runtime", err)
	}
	if data, _ := store.LoadSave("progress"); string(data) != "original" {
		t.Fatal("refused adoption changed saves")
	}
	source.Client.Serve(NativePlatformTable, nativeSlotInitial, nativeAnswerOne)
	if _, err := source.CaptureCheckpoint(t.Context()); err == nil {
		t.Fatal("custom binding was silently lost")
	}
}

func TestNativeSessionCheckpointContinuesWithoutStartup(t *testing.T) {
	archive, err := testfixture.KTFNativeCheckpointArchive()
	if err != nil {
		t.Fatal(err)
	}
	store, _ := backend.NewMemorySaveStore([]backend.SaveEntry{{Key: "progress", Data: []byte("saved")}})
	clock := NewManualClock(time.Unix(10, 0))
	source, err := StartNativeSession(t.Context(), archive, NativeSessionOptions{Clock: clock, SaveStore: store, Speed: 2, Width: 32, Height: 48})
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	advance := func(s *NativeSession, rounds int) {
		t.Helper()
		for range rounds {
			if !s.SkipToNextDeadline() {
				t.Fatal("no scheduled frame")
			}
			if ran, err := s.Tick(t.Context()); err != nil || !ran {
				t.Fatalf("tick = %t, %v", ran, err)
			}
		}
	}
	advance(source, 3)
	checkpoint, err := source.CaptureCheckpoint(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := backend.EncodeCheckpoint(checkpoint)
	if err != nil {
		t.Fatal(err)
	}
	checkpoint, err = backend.DecodeCheckpoint(encoded, backend.SaveIdentity(archive))
	if err != nil {
		t.Fatal(err)
	}
	advance(source, 5)
	expected, width, height, flushes := source.Frame()
	steps, elapsed := source.Client.Steps(), source.GuestElapsed()
	_ = store.StoreSave("progress", []byte("later"))
	source.platform.keep("pending", []byte("discarded"))
	freshClock := NewManualClock(time.Unix(500, 0))
	prepared, err := PrepareNativeSessionCheckpoint(archive, checkpoint, NativeSessionOptions{Clock: freshClock, SaveStore: store})
	if err != nil {
		t.Fatal(err)
	}
	defer prepared.Discard()
	freshClock.Advance(time.Hour)
	restored, err := prepared.Commit(t.Context(), source, store)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	source.Close()
	// The load brought the module back and left the saves alone.
	if value, _ := store.LoadSave("progress"); string(value) != "later" {
		t.Fatalf("the save written after the checkpoint reads %q after the load", value)
	}
	// What the displaced runtime had written and not yet stored is a write its
	// title issued, so the load stored it before it took the runtime away.
	if value, found := store.LoadSave(nativeSaveKey("pending")); !found || string(value) != "discarded" {
		t.Fatalf("the displaced runtime's pending write was not stored: %q, %t", value, found)
	}
	advance(restored, 5)
	actual, actualWidth, actualHeight, actualFlushes := restored.Frame()
	if !bytes.Equal(expected, actual) || width != actualWidth || height != actualHeight || flushes != actualFlushes || steps != restored.Client.Steps() || elapsed != restored.GuestElapsed() {
		t.Fatalf("restored continuation differs: frame=%t flushes=%d/%d steps=%d/%d elapsed=%v/%v", bytes.Equal(expected, actual), flushes, actualFlushes, steps, restored.Client.Steps(), elapsed, restored.GuestElapsed())
	}
	startup, err := restored.Client.ReadWord(testfixture.KTFNativeCheckpointStartupCounter)
	if err != nil || startup != 1 {
		t.Fatalf("startup was replayed: %d, %v", startup, err)
	}
	if _, err := restored.CaptureCheckpoint(t.Context()); err != nil {
		t.Fatal(err)
	}
}
