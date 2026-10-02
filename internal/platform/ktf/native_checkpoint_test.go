package ktf

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"image/color"
	"math"
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

func TestNativeCheckpointPreservesSharedBuffersCachedViewsAndDevices(t *testing.T) {
	archive, err := testfixture.KTFNativeCheckpointArchive()
	if err != nil {
		t.Fatal(err)
	}
	store, _ := backend.NewMemorySaveStore(nil)
	clock := NewManualClock(time.Unix(1, 0))
	sink := &countingSink{}
	source, err := StartNativeSession(t.Context(), archive, NativeSessionOptions{Clock: clock, SaveStore: store, AudioSink: sink, Width: 32, Height: 48})
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	p := source.platform
	put := func(data []byte) uint32 {
		t.Helper()
		address, err := source.Client.Allocate(uint32(len(data)))
		if err != nil {
			t.Fatal(err)
		}
		if err := source.Client.core.Memory().Write(address, data); err != nil {
			t.Fatal(err)
		}
		return address
	}
	data := buildResourceFile([]nativeResourceGroup{{Kind: 6, First: 1}}, [][]byte{[]byte("abc")})
	p.keep("state.dat", data)
	file := nativeCall(t, p.openFile, 0, nativeString(t, p, "state.dat"), 2)
	// The first write makes written and the open file share the same buffer.
	nativeCall(t, p.writeFile, file, put(data[:1]), 1)
	resource, ok, err := p.resourceFile("state.dat")
	if err != nil || !ok {
		t.Fatalf("resource = %t, %v", ok, err)
	}
	p.resourceFile("missing.dat")
	// Cached parsed tables must remain valid even if their source header changes.
	nativeCall(t, p.seekFile, file, nativeSeekStart, 0)
	nativeCall(t, p.writeFile, file, put([]byte{0xff}), 1)
	if _, err := parseNativeResourceFile(p.files[file].data); err == nil {
		t.Fatal("header was not changed")
	}
	bitmap := buildBitmap(1, 1, []color.RGBA{{A: 255}, {R: 11, A: 255}, {R: 22, A: 255}}, []byte{1, 0, 0, 0})
	object := nativeCall(t, p.createObject, nativeClassImage, put(bitmap))
	pixel := binary.LittleEndian.Uint32(bitmap[bitmapPixelOffsetField:])
	if err := source.Client.core.Memory().Write(p.images[object].data+pixel, []byte{2}); err != nil {
		t.Fatal(err)
	}
	nativeCall(t, p.setClip, 0, 0, put(oneNoteSMAF()))
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
	freshClock := NewManualClock(time.Unix(500, 0))
	freshStore, _ := backend.NewMemorySaveStore(nil)
	freshSink := &countingSink{}
	prepared, err := PrepareNativeSessionCheckpoint(archive, saved, NativeSessionOptions{Clock: freshClock, SaveStore: freshStore, AudioSink: freshSink})
	if err != nil {
		t.Fatal(err)
	}
	defer prepared.Discard()
	freshClock.Advance(time.Hour)
	restored, err := prepared.Commit(t.Context(), nil)
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
	if cached, exists := r.resources["missing.dat"]; !exists || cached != nil {
		t.Fatal("negative resource cache disappeared")
	}
	if r.fileFailure != p.fileFailure || r.colours[17] != 0x11223300 || !r.unsaved["state.dat"] || r.files[file].position != 1 {
		t.Fatal("file or display service state changed")
	}
	item, ok := r.resources["state.dat"].item(6, 1)
	if !ok || string(item) != "abc" {
		t.Fatal("cached resource tables were reparsed or lost")
	}
	// Mutating the open file must still reach both views, but not the source.
	index := resource.index[0]
	r.files[file].data[index] = 'z'
	if r.written["state.dat"][index] != 'z' || item[0] != 'z' || resource.data[index] != 'a' {
		t.Fatal("buffer sharing or detached ownership changed")
	}
	p.files[file].data[index] = 'z'
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
	if !bytes.Equal(a.Saves[0].Data, b.Saves[0].Data) {
		t.Fatal("buffered save flush differs after restoration")
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
	saved, err := source.CaptureCheckpoint(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name   string
		mutate func(*nativeState)
	}{
		{"version", func(s *nativeState) { s.Version++ }},
		{"mapping", func(s *nativeState) { s.Core.Memory.Mappings[0].Permission = armcore.PermissionReadWriteExecute }},
		{"budget", func(s *nativeState) { s.Core.MaxSteps++ }},
		{"allocator overlap", func(s *nativeState) { s.Blocks = append(s.Blocks, s.Blocks[0]) }},
		{"allocator gap", func(s *nativeState) { s.Blocks = nil }},
		{"surface", func(s *nativeState) { s.Surfaces[0] = "unknown" }},
		{"interface", func(s *nativeState) { s.Interfaces[0].Address = 3 }},
		{"screen overflow", func(s *nativeState) { s.Screen.Width = math.MaxInt }},
		{"deadline overflow", func(s *nativeState) { s.Frame.Remaining = math.MinInt64 }},
		{"callback", func(s *nativeState) { s.Frame.Function = 0xdeadbeef }},
		{"buffer reference", func(s *nativeState) { s.Written = []nativeWrittenState{{Key: []byte("save"), Buffer: 1}} }},
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
	refused := &refusingCheckpointStore{store}
	prepared, err := PrepareNativeSessionCheckpoint(archive, saved, NativeSessionOptions{SaveStore: refused})
	if err != nil {
		t.Fatal(err)
	}
	defer prepared.Discard()
	old := source.Client
	if _, err := prepared.Commit(t.Context(), source); err == nil || source.Client != old {
		t.Fatal("refused adoption changed the live runtime")
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
	restored, err := prepared.Commit(t.Context(), source)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	source.Close()
	if value, _ := store.LoadSave("progress"); string(value) != "saved" {
		t.Fatal("ordinary saves did not restore")
	}
	if _, exists := store.LoadSave(nativeSaveKey("pending")); exists {
		t.Fatal("discarded runtime flushed over restored saves")
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
