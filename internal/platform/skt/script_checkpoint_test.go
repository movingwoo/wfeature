package skt

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"image"
	"reflect"
	"testing"
	"time"

	"github.com/movingwoo/wfeature/internal/backend"
)

// The authored script writes four counters on startup, key and termination.
// Their observable saves expose any lifecycle event repeated by restoration.
func scriptCheckpointArchive(t *testing.T) []byte {
	t.Helper()
	data := make([]byte, 52)
	data[0] = 1
	copy(data[10:26], "Checkpoint SGS")
	entry := func(index int, code ...byte) {
		binary.LittleEndian.PutUint16(data[28+index*2:], uint16(len(data)))
		data = append(data, code...)
	}
	entry(0, 0x3a, 16, 1, 5, 16, 5, 4, 0x99, 0x55, 0x78, 0xff)
	entry(1, 0x3a, 17, 1, 5, 16, 5, 4, 0x99, 0xff)
	entry(2, 0x3a, 19, 1, 0xff)
	entry(3, 0x3a, 18, 1, 5, 16, 5, 4, 0x99, 0x56, 0x78, 0xff)
	variables := len(data)
	for range 20 {
		data = append(data, 1, 1, 0, 0)
	}
	initializers := len(data)
	resources := len(data)
	data = append(data, 0, 8, 6, 0, 1, 8, 6, 0)
	resourceData := len(data)
	data = append(data, 8, 1, 1, 0, 0, 3, 8, 1, 1, 0, 0, 0)
	for i, offset := range []int{variables, initializers, resources, resourceData} {
		binary.LittleEndian.PutUint16(data[44+i*2:], uint16(offset))
	}
	var descriptor []byte
	for _, value := range []string{"application/x-gnex-sgs", "SGS"} {
		descriptor = binary.LittleEndian.AppendUint32(descriptor, uint32(len(value)))
		descriptor = append(descriptor, value...)
	}
	var archive bytes.Buffer
	writer := zip.NewWriter(&archive)
	for _, file := range []struct {
		name string
		data []byte
	}{{"checkpoint.mod", descriptor}, {"checkpoint.sgs", data}} {
		entry, err := writer.Create(file.name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write(file.data); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return archive.Bytes()
}

type scriptCheckpointStore struct {
	*backend.MemorySaveStore
	reads, writes int
}

func (store *scriptCheckpointStore) LoadSave(name string) ([]byte, bool) {
	store.reads++
	return store.MemorySaveStore.LoadSave(name)
}
func (store *scriptCheckpointStore) StoreSave(name string, data []byte) error {
	store.writes++
	return store.MemorySaveStore.StoreSave(name, data)
}

type scriptCheckpointSink struct{ calls, on, off int }

func (sink *scriptCheckpointSink) PlayWave(uint8, uint32, []int16) { sink.calls++ }
func (sink *scriptCheckpointSink) MIDINoteOn(uint8, uint8, uint8)  { sink.calls++; sink.on++ }
func (sink *scriptCheckpointSink) MIDINoteOff(uint8, uint8, uint8) { sink.calls++; sink.off++ }
func (sink *scriptCheckpointSink) MIDIProgramChange(uint8, uint8)  { sink.calls++ }
func (sink *scriptCheckpointSink) MIDIControlChange(uint8, uint8, uint8) {
	sink.calls++
}
func (sink *scriptCheckpointSink) MIDIPitchBend(uint8, uint16) { sink.calls++ }
func (sink *scriptCheckpointSink) MIDISysEx([]byte)            { sink.calls++ }

func newScriptCheckpointTest(t *testing.T) ([]byte, *ScriptSession, *scriptCheckpointStore, *backend.MemoryFramebuffer, *scriptCheckpointSink) {
	t.Helper()
	data := scriptCheckpointArchive(t)
	archive, err := Open(data)
	if err != nil {
		t.Fatal(err)
	}
	framebuffer, err := backend.NewMemoryFramebuffer(8, 6)
	if err != nil {
		t.Fatal(err)
	}
	memory, err := backend.NewMemorySaveStore(nil)
	if err != nil {
		t.Fatal(err)
	}
	store := &scriptCheckpointStore{MemorySaveStore: memory}
	sink := &scriptCheckpointSink{}
	session, err := StartScript(t.Context(), archive, ScriptOptions{Framebuffer: framebuffer, SaveStore: store, AudioSink: sink, Speed: 1})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = session.Close() })
	return data, session, store, framebuffer, sink
}

func scriptCheckpointCall(t *testing.T, session *ScriptSession, op byte, values ...int16) {
	t.Helper()
	for _, value := range values {
		session.vm.Push(value)
	}
	if err := session.Call(op, session.vm); err != nil {
		t.Fatal(err)
	}
}

func scriptCheckpointCapture(t *testing.T, session *ScriptSession) backend.Checkpoint {
	t.Helper()
	calls := 0
	checkpoint, err := session.CaptureCheckpointWithSession(t.Context(), func() ([]byte, error) {
		calls++
		return []byte("shared fixture state"), nil
	})
	if err != nil || calls != 1 {
		t.Fatalf("capture: %v, shared captures %d", err, calls)
	}
	data, err := backend.EncodeCheckpoint(checkpoint)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := backend.DecodeCheckpoint(data, checkpoint.Identity)
	if err != nil || !bytes.Equal(decoded.Session, []byte("shared fixture state")) || decoded.Variant != backend.CheckpointSKTScript {
		t.Fatalf("checkpoint envelope: %v", err)
	}
	return decoded
}

func scriptCheckpointPlay(t *testing.T, session *ScriptSession) {
	t.Helper()
	handle, err := session.audio.Load(scriptOneNoteSMAF())
	if err != nil {
		t.Fatal(err)
	}
	session.sound = handle
	if err := session.audio.Play(handle, session.clock, false); err != nil {
		t.Fatal(err)
	}
}

func TestScriptCheckpointRestoresContinuationAndRetainedAliases(t *testing.T) {
	archive, source, store, framebuffer, sourceSink := newScriptCheckpointTest(t)
	source.SetSpeed(2)
	scriptCheckpointCall(t, source, 0xa0, 1234)
	for range 11 {
		source.random.Uint64()
	}
	scriptCheckpointCall(t, source, 0x9a, 40, 1)
	scriptCheckpointCall(t, source, 0x9b, 15, 0)
	scriptCheckpointCall(t, source, 0xba, 7)
	scriptCheckpointPlay(t, source)
	source.vibrator.Vibrate(70, 100)
	if _, err := source.Advance(t.Context(), 5*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	g := source.graphics
	g.pixels[0], g.backup[0] = 0xe0, 0x1c
	g.clip, g.color, g.bank = image.Rect(1, 1, 8, 6), 42, 5
	g.textStyle, g.textFG, g.textBG, g.textAlign = 1, 2, 3, 1
	scriptCheckpointCall(t, source, 0x74, 0, 2, 2, 0, 0)
	scriptCheckpointCall(t, source, 0x74, 0, 4, 2, 1, 0)
	scriptCheckpointCall(t, source, 0x74, 0, 6, 2, 1, 1)
	if resized, err := resizeScriptResource(source.vm, &source.vm.Resources[1], 32); err != nil || !resized {
		t.Fatalf("resize queued resource: %t, %v", resized, err)
	}
	g.bitmapQueue[1].data[5] = 3
	source.Pause()
	frame, presents := framebuffer.Snapshot()
	vibration, timers, clock := source.Vibration(), source.timers, source.clock
	checkpoint := scriptCheckpointCapture(t, source)
	var expectedRandom [32]uint64
	for i := range expectedRandom {
		expectedRandom[i] = source.random.Uint64()
	}
	const laterSave = "ordinary save written after the checkpoint"
	if err := store.StoreSave("nv/data", []byte(laterSave)); err != nil {
		t.Fatal(err)
	}
	source.vm.Set(16, 0, 77)
	source.vm.Set(17, 0, 44)
	reads, writes, output := store.reads, store.writes, sourceSink.calls
	destination, _ := backend.NewMemoryFramebuffer(8, 6)
	sink := &scriptCheckpointSink{}
	prepared, err := PrepareScriptCheckpoint(archive, checkpoint, ScriptOptions{SaveStore: store, Framebuffer: destination, AudioSink: sink})
	if err != nil {
		t.Fatal(err)
	}
	defer prepared.Discard()
	if calls, first := prepared.PreparationStoreCalls(); calls != 0 || first != "" {
		t.Fatalf("prepare touched placeholder store: %d, %s", calls, first)
	}
	savedFrame, savedPresents := prepared.Frame()
	if prepared.Speed() != 2 || !prepared.Paused() || savedPresents != presents || !bytes.Equal(savedFrame.RGBA, frame.RGBA) || frame.RGBA[1] != 255 {
		t.Fatal("prepare lost shared settings or the last presented frame")
	}
	savedFrame.RGBA[0] = 0
	if own, _ := prepared.Frame(); own.RGBA[0] != 255 {
		t.Fatal("prepared frame borrows caller memory")
	}
	restored, err := prepared.Commit(t.Context(), source, store, destination)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = restored.Close() })
	if store.reads != reads || store.writes != writes || sink.calls != 0 || sourceSink.calls != output {
		t.Fatal("prepare or commit touched saves or emitted audio")
	}
	if _, count := destination.Snapshot(); count != 0 {
		t.Fatal("prepare or commit presented a frame")
	}
	if !source.closed || source.options.SaveStore != nil || source.vm.Value(16, 0) != 77 || source.vm.Value(17, 0) != 44 {
		t.Fatal("displaced source was not silently closed")
	}
	if err := source.Close(); err != nil || source.vm.Value(17, 0) != 44 {
		t.Fatal("closing displaced source executed termination")
	}
	if restored.vm.Value(16, 0) != 1 || restored.vm.Value(17, 0) != 0 || restored.vm.Value(18, 0) != 0 {
		t.Fatal("restore repeated a lifecycle event")
	}
	if data, _ := store.MemorySaveStore.LoadSave("nv/data"); string(data) != laterSave {
		t.Fatal("restore replaced the ordinary save")
	}
	if restored.clock != clock || restored.timers != timers || restored.Vibration() != vibration || restored.overlayPolicy != 7 || !restored.paused || !restored.audio.Playing(restored.sound) {
		t.Fatal("restore lost clock, timers, vibration, pause or logical sound")
	}
	for i, want := range expectedRandom {
		if got := restored.random.Uint64(); got != want {
			t.Fatalf("random continuation %d = %d, want %d", i, got, want)
		}
	}
	rg := restored.graphics
	if !bytes.Equal(rg.pixels, g.pixels) || !bytes.Equal(rg.backup, g.backup) || !bytes.Equal(rg.lastFrame, frame.RGBA) || rg.clip != g.clip ||
		rg.color != g.color || rg.bank != g.bank || rg.textStyle != g.textStyle || rg.textFG != g.textFG || rg.textBG != g.textBG || rg.textAlign != g.textAlign ||
		rg.bitmapQueueScratch != g.bitmapQueueScratch || rg.bitmapQueueOpaque != g.bitmapQueueOpaque {
		t.Fatal("restore lost indexed graphics, palette, text or queue scratch")
	}
	restored.vm.Resources[0].Data[5] = 1
	rg.bitmapQueue[1].data[5] = 2
	if rg.bitmapQueue[0].data[5] != 1 || rg.bitmapQueue[2].data[5] != 2 || restored.vm.Resources[1].Data[5] != 0 || source.vm.Resources[0].Data[5] != 3 || g.bitmapQueue[1].data[5] != 3 {
		t.Fatal("restored live or detached bitmap storage has incorrect aliases")
	}
	if err := rg.present(); err != nil {
		t.Fatal(err)
	}
	if pending, count := destination.Snapshot(); count != 1 || !bytes.Equal(pending.RGBA[:4], []byte{255, 0, 0, 255}) {
		t.Fatal("unpresented indexed pixels were replaced by the older visible frame")
	}
	if _, err := restored.Advance(t.Context(), 200*time.Millisecond); err != nil || restored.clock != clock || sink.on != 0 {
		t.Fatal("paused restored runtime advanced")
	}
	restored.Resume()
	if _, err := restored.Advance(t.Context(), 8*time.Millisecond); err != nil || sink.on != 1 || restored.timers[1].active || restored.vm.Value(19, 0) != 1 {
		t.Fatalf("restored one-shot timer or pending sound did not continue: %v", err)
	}
	if _, err := restored.Advance(t.Context(), 7*time.Millisecond); err != nil || restored.vm.Value(19, 0) != 2 || restored.timers[0].due != 80*time.Millisecond {
		t.Fatalf("restored repeat timer did not continue: %v", err)
	}
	if err := restored.SendKey(t.Context(), "press", '5'); err != nil || restored.vm.Value(18, 0) != 1 {
		t.Fatalf("restored key event did not continue: %v", err)
	}
	if data, _ := store.MemorySaveStore.LoadSave("nv/data"); len(data) != 64 || binary.LittleEndian.Uint16(data[4:]) != 1 || store.writes != writes+1 {
		t.Fatal("post-restore guest write missed the live store")
	}
	if _, err := restored.Advance(t.Context(), 50*time.Millisecond); err != nil || restored.Vibration().Active() || restored.audio.Playing(restored.sound) || sink.off == 0 {
		t.Fatalf("restored sound or vibration did not expire: %v", err)
	}
}

func TestScriptCheckpointResumesSoundingOutputOnlyWhenAdopted(t *testing.T) {
	archive, source, store, _, sourceSink := newScriptCheckpointTest(t)
	scriptCheckpointPlay(t, source)
	if _, err := source.Advance(t.Context(), 25*time.Millisecond); err != nil || sourceSink.on != 1 {
		t.Fatalf("fixture did not start its note: %v", err)
	}
	sink := &scriptCheckpointSink{}
	prepared, err := PrepareScriptCheckpoint(archive, scriptCheckpointCapture(t, source), ScriptOptions{AudioSink: sink})
	if err != nil {
		t.Fatal(err)
	}
	defer prepared.Discard()
	framebuffer, _ := backend.NewMemoryFramebuffer(8, 6)
	restored, err := prepared.Commit(t.Context(), source, store, framebuffer)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = restored.Close() })
	if sink.calls != 0 {
		t.Fatal("detached sounding note emitted before adoption")
	}
	restored.ResumeCheckpointOutput()
	if sink.on != 1 || restored.clock != 25*time.Millisecond {
		t.Fatal("adoption did not restore the sounding note at its saved time")
	}
	if _, err := restored.Advance(t.Context(), 25*time.Millisecond); err != nil || sink.off != 1 || restored.audio.Playing(restored.sound) {
		t.Fatalf("restored sounding note missed its original end: %v", err)
	}
}

func TestScriptCheckpointRejectsDamagedRuntimeBeforeStoreOrOutput(t *testing.T) {
	archive, source, store, framebuffer, sourceSink := newScriptCheckpointTest(t)
	scriptCheckpointCall(t, source, 0x74, 0, 2, 2, 0, 0)
	scriptCheckpointPlay(t, source)
	checkpoint := scriptCheckpointCapture(t, source)
	mutations := map[string]func(*scriptCheckpointState){
		"version":           func(s *scriptCheckpointState) { s.Version++ },
		"speed":             func(s *scriptCheckpointState) { s.Speed = 17 },
		"clock":             func(s *scriptCheckpointState) { s.Clock = -1 },
		"dimensions":        func(s *scriptCheckpointState) { s.Graphics.Width = 0 },
		"pixels":            func(s *scriptCheckpointState) { s.Graphics.Pixels = nil },
		"backup":            func(s *scriptCheckpointState) { s.Graphics.Backup = nil },
		"frame":             func(s *scriptCheckpointState) { s.Graphics.Frame = s.Graphics.Frame[:4] },
		"present count":     func(s *scriptCheckpointState) { s.Graphics.Presents = 0 },
		"clip":              func(s *scriptCheckpointState) { s.Graphics.Clip = image.Rect(-1, 0, 2, 2) },
		"color":             func(s *scriptCheckpointState) { s.Graphics.Color = 182 },
		"palette":           func(s *scriptCheckpointState) { s.Graphics.Bank = 7 },
		"text style":        func(s *scriptCheckpointState) { s.Graphics.TextStyle = 4 },
		"text foreground":   func(s *scriptCheckpointState) { s.Graphics.TextFG = -1 },
		"text background":   func(s *scriptCheckpointState) { s.Graphics.TextBG = 182 },
		"text alignment":    func(s *scriptCheckpointState) { s.Graphics.TextAlign = 3 },
		"queue count":       func(s *scriptCheckpointState) { s.Graphics.Queue = make([]scriptBitmapState, 21) },
		"queue view count":  func(s *scriptCheckpointState) { s.VM.External = nil },
		"queue view bounds": func(s *scriptCheckpointState) { s.VM.External[0].Length = 65536 },
		"random length":     func(s *scriptCheckpointState) { s.Random = s.Random[:1] },
		"random encoding":   func(s *scriptCheckpointState) { s.Random[0] ^= 255 },
		"timer period":      func(s *scriptCheckpointState) { s.Timers[0] = scriptTimerState{Period: time.Millisecond, Active: true} },
		"timer deadline":    func(s *scriptCheckpointState) { s.Timers[0].Due = time.Minute },
		"vibration":         func(s *scriptCheckpointState) { s.Vibration.Level = 101 },
		"audio version":     func(s *scriptCheckpointState) { s.Audio.Version++ },
		"audio cursor":      func(s *scriptCheckpointState) { s.Audio.Sounds[0].Cursor = len(s.Audio.Sounds[0].Events) + 1 },
		"audio clock":       func(s *scriptCheckpointState) { s.Audio.Sounds[0].StartedAt = time.Second },
		"sound handle":      func(s *scriptCheckpointState) { s.Sound++ },
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			var saved scriptCheckpointState
			if err := backend.DecodeCheckpointRecord(checkpoint.Runtime, &saved); err != nil {
				t.Fatal(err)
			}
			mutate(&saved)
			broken := checkpoint
			var err error
			if broken.Runtime, err = backend.EncodeCheckpointRecord(saved); err != nil {
				t.Fatal(err)
			}
			// A valid envelope checksum forces refusal at runtime validation.
			wire, err := backend.EncodeCheckpoint(broken)
			if err != nil {
				t.Fatal(err)
			}
			broken, err = backend.DecodeCheckpoint(wire, checkpoint.Identity)
			if err != nil {
				t.Fatal(err)
			}
			sink := &scriptCheckpointSink{}
			before, presents := framebuffer.Snapshot()
			reads, writes, calls := store.reads, store.writes, sourceSink.calls
			prepared, err := PrepareScriptCheckpoint(archive, broken, ScriptOptions{SaveStore: store, Framebuffer: framebuffer, AudioSink: sink})
			if prepared != nil {
				prepared.Discard()
			}
			if err == nil || prepared != nil {
				t.Fatal("damaged checkpoint was prepared")
			}
			after, count := framebuffer.Snapshot()
			if store.reads != reads || store.writes != writes || sink.calls != 0 || sourceSink.calls != calls || source.closed || count != presents || !reflect.DeepEqual(after, before) {
				t.Fatal("refused preparation changed live saves, runtime or output")
			}
		})
	}
}

func TestScriptCheckpointFailedCaptureAndCommitLeaveSourceUsable(t *testing.T) {
	archive, source, store, framebuffer, sink := newScriptCheckpointTest(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := source.CaptureCheckpointWithSession(ctx, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled capture: %v", err)
	}
	want := errors.New("shared capture failed")
	if _, err := source.CaptureCheckpointWithSession(t.Context(), func() ([]byte, error) { return nil, want }); !errors.Is(err, want) {
		t.Fatalf("failed shared capture: %v", err)
	}
	prepared, err := PrepareScriptCheckpoint(archive, scriptCheckpointCapture(t, source), ScriptOptions{AudioSink: sink})
	if err != nil {
		t.Fatal(err)
	}
	defer prepared.Discard()
	reads, writes, output := store.reads, store.writes, sink.calls
	wrong, _ := backend.NewMemoryFramebuffer(7, 6)
	if _, err := prepared.Commit(t.Context(), source, store, wrong); err == nil {
		t.Fatal("commit accepted wrong framebuffer dimensions")
	}
	if _, err := prepared.Commit(ctx, source, store, framebuffer); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled commit: %v", err)
	}
	if source.closed || store.reads != reads || store.writes != writes || sink.calls != output {
		t.Fatal("failed commit changed the source")
	}
	prepared.Discard()
	if err := source.SendKey(t.Context(), "press", '5'); err != nil || source.vm.Value(18, 0) != 1 || store.writes != writes+1 {
		t.Fatalf("source cannot continue after refused restore: %v", err)
	}
}
