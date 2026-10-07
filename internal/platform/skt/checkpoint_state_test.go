package skt

import (
	"bytes"
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/movingwoo/wfeature/internal/api/wipi"
	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/jvm"
	"github.com/movingwoo/wfeature/internal/keypad"
	"github.com/movingwoo/wfeature/internal/textinput"
)

func newPlatformCheckpointRuntime(t *testing.T) *Runtime {
	t.Helper()
	archive, err := Open(canvasJAR)
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := Start(archive, Options{Framebuffer: newTestFramebuffer(t, 32, 24), DisableAuthentication: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtime.Destroy(true) })
	return runtime
}

func capturePlatformCheckpointTest(t *testing.T, source *Runtime, now time.Time) (javaPlatformState, jvm.HeapState) {
	t.Helper()
	heap := newCheckpointHeap(source, now)
	heap.guestNow = now
	saved, roots, err := source.capturePlatformState(heap)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := backend.EncodeCheckpointRecord(saved)
	if err != nil {
		t.Fatal(err)
	}
	var copied javaPlatformState
	if err := backend.DecodeCheckpointRecord(encoded, &copied); err != nil {
		t.Fatal(err)
	}
	state, err := source.VM.CaptureHeapState(roots, heap.codec())
	if err != nil {
		t.Fatal(err)
	}
	return copied, state
}

func restorePlatformCheckpointTest(t *testing.T, source *Runtime, saved javaPlatformState, state jvm.HeapState, now time.Time) (*Runtime, []*jvm.Object, *checkpointHeap) {
	t.Helper()
	width, height := saved.dimensions()
	runtime, err := newRuntime(source.Archive, Options{Framebuffer: newTestFramebuffer(t, width, height), DisableAuthentication: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(runtime.VM.Close)
	if err := saved.restoreBuffers(runtime); err != nil {
		t.Fatal(err)
	}
	heap := newCheckpointHeap(runtime, now)
	heap.guestNow = now
	roots, err := runtime.VM.RestoreHeapState(state, heap.codec())
	if err != nil {
		t.Fatal(err)
	}
	if err := heap.finish(); err != nil {
		t.Fatal(err)
	}
	if err := runtime.restorePlatformState(saved, roots, heap); err != nil {
		t.Fatal(err)
	}
	return runtime, roots, heap
}

func TestJavaPlatformCheckpointRestoresGraphAliasesAndCaches(t *testing.T) {
	source := newPlatformCheckpointRuntime(t)
	now := time.Unix(1000, 0)
	source.lastRefresh = now.Add(-17 * time.Millisecond)
	source.frameRGBA[0] = 73
	source.refreshFrame = bytes.Clone(source.frameRGBA)
	source.refreshFrame[0] = 91
	source.refreshPending = true
	canvas := source.currentDisplayable
	source.displayableState(canvas).title = "checkpoint screen"
	source.fullScreen[canvas] = true
	source.pad = keypad.Pad{IsPad: isPadKey}
	source.pad.Key(true, KeyCodeLeft)
	source.pad.Key(true, KeyCodeRight)
	thread := &jvm.Object{ClassName: jvm.ThreadClass}
	source.pendingSerial = []*jvm.Object{thread, thread}
	if err := source.queueCanvasPaint(canvas, paintRect{maxX: 12, maxY: 10}); err != nil {
		t.Fatal(err)
	}
	vendor := source.skvm()
	vendor.backlightColor, vendor.audioVolume = -17, 37
	vendor.backlightOn, vendor.keyToneOn, vendor.zBufferEnabled = false, false, true
	vendor.textInput = textInputState{mode: textinput.ModeUppercase, revision: 4, lastKey: now.Add(-42 * time.Millisecond)}
	rms := source.rms()
	rms.stores["progress"] = &recordStore{name: "progress", records: [][]byte{[]byte("old save")}, open: 1, version: 3, modified: 77}
	rms.names, rms.loaded = []string{"progress"}, true
	stream, err := source.VM.NewObject(jvm.ByteArrayOutputStreamClass, "()V")
	if err != nil {
		t.Fatal(err)
	}
	file := &xFileData{name: "closed.bin", mode: xFileWrite, writable: true, data: []byte("old file")}
	source.wipiFileStreams = map[*jvm.Object]*xFileData{stream: file}
	saved, state := capturePlatformCheckpointTest(t, source, now)
	restored, _, heap := restorePlatformCheckpointTest(t, source, saved, state, now.Add(time.Hour))
	if restored.MIDlet == source.MIDlet || restored.displayOwner != restored.MIDlet || restored.currentDisplayable == canvas ||
		restored.screenGraphicsObject.Native != restored.screenGraphicsContext || &restored.screenGraphicsContext.pixels[0] != &restored.frameRGBA[0] {
		t.Fatal("restored platform lost object ownership or screen graphics aliases")
	}
	if restored.frameRGBA[0] != 73 || restored.refreshFrame[0] != 91 || !restored.refreshPending || &restored.frameRGBA[0] == &source.frameRGBA[0] {
		t.Fatal("render buffers were lost or borrowed from the source")
	}
	if got := restored.lcdui().displayables[restored.currentDisplayable]; got == nil || got.title != "checkpoint screen" {
		t.Fatal("hidden displayable state was lost")
	}
	if !restored.fullScreen[restored.currentDisplayable] || len(restored.pendingSerial) != 2 || restored.pendingSerial[0] != restored.pendingSerial[1] || restored.pendingSerial[0] == thread {
		t.Fatal("fullscreen or serial Runnable ownership changed")
	}
	if held, pressed := restored.pad.Held(); !pressed || held != KeyCodeRight {
		t.Fatal("restored keypad lost its current direction")
	}
	if !restored.lastRefresh.Equal(heap.guestNow.Add(-17*time.Millisecond)) || !restored.skvm().textInput.lastKey.Equal(heap.guestNow.Add(-42*time.Millisecond)) ||
		restored.skvm().audioVolume != 37 || restored.skvm().backlightColor != -17 || !restored.skvm().zBufferEnabled {
		t.Fatal("vendor settings or relative times changed")
	}
	if cached := restored.rms().stores["progress"]; cached == nil || cached == rms.stores["progress"] || cached.version != 3 || len(cached.records) != 0 ||
		len(restored.rms().names) != 0 || restored.rms().loaded {
		t.Fatal("RMS cache mapping lost identity metadata or restored ordinary save bytes")
	}
	if len(restored.wipiFileStreams) != 1 {
		t.Fatal("closed stream ownership map was lost")
	}
	for object, target := range restored.wipiFileStreams {
		if object == stream || target == file || target.name != "closed.bin" || target.open || len(target.data) != 0 {
			t.Fatal("closed stream lost its original file or restored save bytes")
		}
	}
	names, err := restored.events.CaptureNames()
	if err != nil || !reflect.DeepEqual(names, saved.Events) || !restored.paintQueued || !restored.paintPosted || restored.paintCanvas != restored.currentDisplayable {
		t.Fatalf("pending render events changed: %v, %v", names, err)
	}
	for key, font := range restored.fonts {
		if font.Native.(*fontData).fontKey != key || font == source.fonts[key] {
			t.Fatal("font map lost native key or ownership")
		}
	}
	before, _ := restored.VM.InvokeStatic("CanvasMIDlet", "paintCount", "()I")
	if err := restored.events.Run(); err != nil {
		t.Fatal(err)
	}
	after, err := restored.VM.InvokeStatic("CanvasMIDlet", "paintCount", "()I")
	b, _ := before.Int32()
	a, _ := after.Int32()
	if err != nil || a != b+1 || restored.paintPosted || restored.paintQueued {
		t.Fatalf("restored Canvas.repaint did not execute once: %d -> %d, %v", b, a, err)
	}
}

func TestJavaPlatformCheckpointReconstructsLifecycleNotifications(t *testing.T) {
	source := newPlatformCheckpointRuntime(t)
	for _, name := range []string{"MIDlet.notifyPaused", "MIDlet.resumeRequest", "MIDlet.notifyDestroyed", "System.exit"} {
		if err := source.events.Post(name, source.checkpointEvent(name)); err != nil {
			t.Fatal(err)
		}
	}
	saved, state := capturePlatformCheckpointTest(t, source, time.Unix(1000, 0))
	restored, _, _ := restorePlatformCheckpointTest(t, source, saved, state, time.Unix(2000, 0))
	if source.State() != StateActive || restored.State() != StateActive {
		t.Fatal("capture or restore executed a lifecycle notification")
	}
	if err := restored.events.Run(); err != nil || restored.State() != StateDestroyed || source.State() != StateActive {
		t.Fatalf("restored lifecycle FIFO: state %s, %v", restored.State(), err)
	}
}

func TestJavaPlatformCheckpointAcceptsWIPIDisplayRoots(t *testing.T) {
	source := newPlatformCheckpointRuntime(t)
	card := &jvm.Object{ClassName: wipi.CardClass}
	source.display = &jvm.Object{ClassName: wipi.DisplayClass}
	source.currentDisplayable = card
	source.cardStack = []*jvm.Object{card, card}
	source.fullScreen = map[*jvm.Object]bool{card: true}
	source.displayableState(card).title = "card"
	saved, state := capturePlatformCheckpointTest(t, source, time.Unix(1000, 0))
	restored, _, _ := restorePlatformCheckpointTest(t, source, saved, state, time.Unix(2000, 0))
	if restored.display.ClassName != wipi.DisplayClass || restored.currentDisplayable.ClassName != wipi.CardClass ||
		len(restored.cardStack) != 2 || restored.cardStack[0] != restored.currentDisplayable || restored.cardStack[1] != restored.currentDisplayable ||
		!restored.fullScreen[restored.currentDisplayable] || restored.displayableState(restored.currentDisplayable).title != "card" {
		t.Fatal("WIPI display roots or repeated card aliases were lost")
	}
}

func TestJavaPlatformCheckpointFullPreparationPreservesIdleGraph(t *testing.T) {
	source := newPlatformCheckpointRuntime(t)
	store, err := backend.NewMemorySaveStore(nil)
	if err != nil {
		t.Fatal(err)
	}
	source.AttachSaveStore(store)
	guestNow := time.Unix(1000, 0)
	source.pace.Rebase(guestNow)
	source.paceStart = guestNow
	source.lastRefresh = guestNow.Add(-17 * time.Millisecond)
	source.skvm().textInput.lastKey = guestNow.Add(-42 * time.Millisecond)
	frame, presents := source.checkpointSurface.snapshot()
	if _, err := source.VM.InvokeStatic("CanvasMIDlet", "drawAfterPaint", "()Z"); err != nil {
		t.Fatal(err)
	}
	paintCount := invokeFixtureInt(t, source, "CanvasMIDlet", "paintCount")
	if err := source.Pause(); err != nil {
		t.Fatal(err)
	}
	checkpoint, err := source.CaptureCheckpointWithSession(context.Background(), func() ([]byte, error) { return []byte("shared state"), nil })
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := backend.EncodeCheckpoint(checkpoint)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := backend.DecodeCheckpoint(encoded, source.Archive.identity)
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := PrepareJavaCheckpoint(canvasJAR, decoded, Options{DisableAuthentication: true})
	if err != nil {
		t.Fatal(err)
	}
	defer prepared.Discard()
	if calls, first := prepared.PreparationStoreCalls(); calls != 0 {
		t.Fatalf("preparation touched its placeholder store: %d, %s", calls, first)
	}
	if string(decoded.Session) != "shared state" || !prepared.Paused() || source.State() != StatePaused ||
		prepared.runtime.MIDlet == source.MIDlet || invokeFixtureInt(t, prepared.runtime, "CanvasMIDlet", "paintCount") != paintCount {
		t.Fatal("full preparation lost shared/lifecycle state, aliased the source or executed a callback")
	}
	got, count := prepared.Frame()
	if count != presents || !bytes.Equal(got.RGBA, frame.RGBA) || !bytes.Equal(prepared.runtime.frameRGBA, source.frameRGBA) || bytes.Equal(got.RGBA, prepared.runtime.frameRGBA) {
		t.Fatal("full preparation lost the presented/unpresented frame distinction")
	}
	if prepared.runtime.screenGraphicsObject.Native != prepared.runtime.screenGraphicsContext ||
		&prepared.runtime.screenGraphicsContext.pixels[0] != &prepared.runtime.frameRGBA[0] {
		t.Fatal("full preparation lost the screen graphics alias")
	}
	if !prepared.runtime.lastRefresh.Equal(source.lastRefresh) || !prepared.runtime.skvm().textInput.lastKey.Equal(source.skvm().textInput.lastKey) {
		t.Fatal("full preparation shifted guest refresh or input time into the wall clock")
	}
}

func TestJavaPlatformCheckpointRejectsCallbacksAndMalformedGraphs(t *testing.T) {
	source := newPlatformCheckpointRuntime(t)
	source.displayableState(source.currentDisplayable)
	source.fullScreen[source.currentDisplayable] = true
	saved, state := capturePlatformCheckpointTest(t, source, time.Unix(1000, 0))
	restored, roots, heap := restorePlatformCheckpointTest(t, source, saved, state, time.Unix(2000, 0))
	mutations := map[string]func(*javaPlatformState){
		"version":              func(s *javaPlatformState) { s.Version++ },
		"dimensions":           func(s *javaPlatformState) { s.Width = 0 },
		"frame length":         func(s *javaPlatformState) { s.FrameRGBA = s.FrameRGBA[:1] },
		"refresh missing":      func(s *javaPlatformState) { s.RefreshPending = true; s.RefreshFrame = nil },
		"time overflow":        func(s *javaPlatformState) { s.LastRefresh = checkpointTime{Set: true, Age: -1 << 63} },
		"lifecycle":            func(s *javaPlatformState) { s.State = StateError },
		"missing MIDlet":       func(s *javaPlatformState) { s.MIDlet = 0 },
		"root bounds":          func(s *javaPlatformState) { s.CurrentDisplayable = len(roots) + 1 },
		"root type":            func(s *javaPlatformState) { s.CurrentDisplayable = s.MIDlet },
		"font duplicate":       func(s *javaPlatformState) { s.Fonts = append(s.Fonts, s.Fonts[0]) },
		"fullscreen duplicate": func(s *javaPlatformState) { s.FullScreen = append(s.FullScreen, s.FullScreen[0]) },
		"display duplicate":    func(s *javaPlatformState) { s.Displayables = append(s.Displayables, s.Displayables[0]) },
		"display payload":      func(s *javaPlatformState) { s.Displayables[0].Native = s.MIDlet },
		"graphics alias":       func(s *javaPlatformState) { s.ScreenGraphicsContext = s.MIDlet },
		"missing event":        func(s *javaPlatformState) { s.PaintPosted = true },
		"unowned event":        func(s *javaPlatformState) { s.Events = []string{"Screen.paint"} },
		"serial closure":       func(s *javaPlatformState) { s.Events = []string{"Display.callSerially"} },
	}
	encoded, err := backend.EncodeCheckpointRecord(saved)
	if err != nil {
		t.Fatal(err)
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			var broken javaPlatformState
			if err := backend.DecodeCheckpointRecord(encoded, &broken); err != nil {
				t.Fatal(err)
			}
			mutate(&broken)
			if err := restored.restorePlatformState(broken, roots, heap); err == nil {
				t.Fatal("malformed platform state was accepted")
			}
		})
	}
	for _, field := range []*bool{&source.painting, &source.runningSerial} {
		*field = true
		if _, _, err := source.capturePlatformState(newCheckpointHeap(source, time.Now())); err == nil {
			t.Fatal("active guest callback was captured")
		}
		*field = false
	}
	if err := source.events.Post("Display.callSerially", func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	if _, _, err := source.capturePlatformState(newCheckpointHeap(source, time.Now())); err == nil {
		t.Fatal("queued serial closure was captured")
	}
}
