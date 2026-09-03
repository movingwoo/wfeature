package skt

import (
	"bytes"
	_ "embed"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/movingwoo/wfeature/internal/api/skvm"
	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/jvm"
)

//go:embed testdata/arithmetic.jar
var arithmeticJAR []byte

//go:embed testdata/lifecycle.jar
var lifecycleJAR []byte

//go:embed testdata/deferred-start.jar
var deferredStartJAR []byte

//go:embed testdata/runtime-failure.jar
var runtimeFailureJAR []byte

//go:embed testdata/display.jar
var displayJAR []byte

//go:embed testdata/canvas.jar
var canvasJAR []byte

func TestJARToBytecodeExecution(t *testing.T) {
	archive, err := Open(arithmeticJAR)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if archive.Descriptor.Name != "Arithmetic Fixture" || archive.Descriptor.MainClass != "Arithmetic" {
		t.Fatalf("descriptor = %+v", archive.Descriptor)
	}

	vm := jvm.New(archive, jvm.Options{})
	result, err := vm.InvokeStatic(archive.Descriptor.MainClass, "sumTwice", "(I)I", jvm.IntValue(10))
	if err != nil {
		t.Fatalf("InvokeStatic() error = %v", err)
	}
	value, err := result.Int32()
	if err != nil {
		t.Fatal(err)
	}
	if value != 90 {
		t.Fatalf("sumTwice(10) = %d, want 90", value)
	}

	result, err = vm.InvokeStatic(archive.Descriptor.MainClass, "objectMath", "(II)I", jvm.IntValue(40), jvm.IntValue(2))
	if err != nil {
		t.Fatalf("objectMath() error = %v", err)
	}
	value, err = result.Int32()
	if err != nil {
		t.Fatal(err)
	}
	if value != 42 {
		t.Fatalf("objectMath(40, 2) = %d, want 42", value)
	}

	result, err = vm.InvokeStatic(archive.Descriptor.MainClass, "plainObjectMath", "()I")
	if err != nil {
		t.Fatalf("plainObjectMath() error = %v", err)
	}
	value, err = result.Int32()
	if err != nil {
		t.Fatal(err)
	}
	if value != 1 {
		t.Fatalf("plainObjectMath() = %d, want 1", value)
	}
	result, err = vm.InvokeStatic(archive.Descriptor.MainClass, "commonLibraryMath", "()I")
	if err != nil {
		t.Fatalf("commonLibraryMath() error = %v", err)
	}
	value, _ = result.Int32()
	if value != 1 {
		t.Fatalf("commonLibraryMath() = %d, want 1", value)
	}
	if _, err := vm.InvokeStatic(archive.Descriptor.MainClass, "startThread", "()V"); err != nil {
		t.Fatalf("startThread() error = %v", err)
	}
	deadline := time.Now().Add(time.Second)
	for {
		result, err = vm.InvokeStatic(archive.Descriptor.MainClass, "threadResult", "()I")
		if err != nil {
			t.Fatal(err)
		}
		value, _ = result.Int32()
		if value == 42 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("JAR guest Thread did not run before deadline")
		}
		time.Sleep(time.Millisecond)
	}

	result, err = vm.InvokeStatic(archive.Descriptor.MainClass, "arraySum", "(I)I", jvm.IntValue(5))
	if err != nil {
		t.Fatalf("arraySum() error = %v", err)
	}
	value, err = result.Int32()
	if err != nil {
		t.Fatal(err)
	}
	if value != 15 {
		t.Fatalf("arraySum(5) = %d, want 15", value)
	}

	result, err = vm.InvokeStatic(archive.Descriptor.MainClass, "interfaceMath", "(I)I", jvm.IntValue(41))
	if err != nil {
		t.Fatalf("interfaceMath() error = %v", err)
	}
	value, err = result.Int32()
	if err != nil {
		t.Fatal(err)
	}
	if value != 42 {
		t.Fatalf("interfaceMath(41) = %d, want 42", value)
	}
}

func TestStartRunsMIDletConstructorAndStartApp(t *testing.T) {
	archive, err := Open(lifecycleJAR)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	runtime, err := Start(archive, testRuntimeOptions(t))
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if runtime.MIDlet.ClassName != "LifecycleMIDlet" {
		t.Fatalf("MIDlet class = %q", runtime.MIDlet.ClassName)
	}
	if got := runtime.Summary(); got.Name != "Lifecycle Fixture" || got.MainClass != "LifecycleMIDlet" || got.State != StateActive || got.Error != "" {
		t.Fatalf("Summary() = %+v", got)
	}

	result, err := runtime.VM.InvokeStatic("LifecycleMIDlet", "state", "()I")
	if err != nil {
		t.Fatalf("state() error = %v", err)
	}
	state, err := result.Int32()
	if err != nil {
		t.Fatal(err)
	}
	if state != 2 {
		t.Fatalf("state() = %d, want 2 after constructor and startApp", state)
	}

	result, err = runtime.VM.InvokeStatic("LifecycleMIDlet", "appProperty", "()Ljava/lang/String;")
	if err != nil {
		t.Fatalf("appProperty() error = %v", err)
	}
	property, err := result.Reference()
	if err != nil {
		t.Fatal(err)
	}
	if property == nil || property.Native != "from-manifest" {
		t.Fatalf("appProperty() = %#v, want from-manifest", property)
	}
	if value, ok := runtime.AppProperty("FIXTURE-PROPERTY"); !ok || value != "from-manifest" {
		t.Fatalf("AppProperty() = %q, %v", value, ok)
	}
}

// The MIDlet's own properties come from its manifest; the system properties
// describe the host it is running on. A game reads the second to decide what
// it can do, and MIDP says an unknown name is null rather than an error — a
// title probing for an optional capability depends on that answer.
func TestSystemPropertiesDescribeTheHostAndAnswerNullForTheRest(t *testing.T) {
	archive, err := Open(lifecycleJAR)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	runtime, err := Start(archive, testRuntimeOptions(t))
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	for name, want := range map[string]string{
		"microedition.platform":      "wfeature",
		"microedition.profiles":      "MIDP-1.0",
		"microedition.configuration": "CLDC-1.0",
	} {
		result, err := runtime.VM.InvokeStatic("java/lang/System", "getProperty", "(Ljava/lang/String;)Ljava/lang/String;",
			jvm.ReferenceValue(runtime.VM.NewString(name)))
		if err != nil {
			t.Fatalf("getProperty(%q) error = %v", name, err)
		}
		value, err := result.Reference()
		if err != nil {
			t.Fatal(err)
		}
		if value == nil || value.Native != want {
			t.Fatalf("getProperty(%q) = %#v, want %q", name, value, want)
		}
	}

	result, err := runtime.VM.InvokeStatic("java/lang/System", "getProperty", "(Ljava/lang/String;)Ljava/lang/String;",
		jvm.ReferenceValue(runtime.VM.NewString("wireless.messaging.sms.smsc")))
	if err != nil {
		t.Fatalf("getProperty() on an unknown name error = %v", err)
	}
	if value, _ := result.Reference(); value != nil {
		t.Fatalf("getProperty() on an unknown name = %#v, want null", value)
	}
}

func TestDisplaySelectionIsStableDelayedAndCoalesced(t *testing.T) {
	archive, err := Open(displayJAR)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	runtime, err := Start(archive, testRuntimeOptions(t))
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	summary := runtime.Summary()
	if summary.State != StateActive || summary.Display == nil ||
		summary.Display.CurrentClass != "DisplayMIDlet$SecondScreen" || !summary.Display.Shown {
		t.Fatalf("Summary() = %+v, want active second screen", summary)
	}
	if state := invokeFixtureInt(t, runtime, "DisplayMIDlet", "displayState"); state != 31 {
		t.Fatalf("displayState() = %d, want all initial display checks set", state)
	}
	if !invokeFixtureBoolean(t, runtime, "DisplayMIDlet", "nullDisplayRejected") {
		t.Fatal("nullDisplayRejected() = false, want true")
	}

	if err := runtime.Pause(); err != nil {
		t.Fatalf("Pause() error = %v", err)
	}
	if visible := invokeFixtureInt(t, runtime, "DisplayMIDlet", "visibleScreen"); visible != 0 {
		t.Fatalf("visibleScreen() while paused = %d, want 0", visible)
	}
	if summary := runtime.Summary(); summary.Display == nil || summary.Display.Shown {
		t.Fatalf("Summary() while paused = %+v, want hidden display", summary)
	}

	if err := runtime.Resume(); err != nil {
		t.Fatalf("Resume() error = %v", err)
	}
	if visible := invokeFixtureInt(t, runtime, "DisplayMIDlet", "visibleScreen"); visible != 2 {
		t.Fatalf("visibleScreen() after resume = %d, want second screen", visible)
	}

	if _, err := runtime.VM.InvokeStatic("DisplayMIDlet", "requestFirstScreen", "()V"); err != nil {
		t.Fatalf("requestFirstScreen() error = %v", err)
	}
	if current := invokeFixtureInt(t, runtime, "DisplayMIDlet", "currentScreen"); current != 2 {
		t.Fatalf("currentScreen() before RunPending = %d, want delayed second screen", current)
	}
	if err := runtime.RunPending(); err != nil {
		t.Fatalf("RunPending() error = %v", err)
	}
	if current := invokeFixtureInt(t, runtime, "DisplayMIDlet", "currentScreen"); current != 1 {
		t.Fatalf("currentScreen() after RunPending = %d, want first screen", current)
	}

	if _, err := runtime.VM.InvokeStatic("DisplayMIDlet", "requestNoScreen", "()V"); err != nil {
		t.Fatalf("requestNoScreen() error = %v", err)
	}
	if err := runtime.RunPending(); err != nil {
		t.Fatalf("RunPending() after null request error = %v", err)
	}
	if current := invokeFixtureInt(t, runtime, "DisplayMIDlet", "currentScreen"); current != 1 {
		t.Fatalf("currentScreen() after null request = %d, want unchanged first screen", current)
	}
}

func TestCanvasPaintsAndCoalescesRepaintsIntoHostFramebuffer(t *testing.T) {
	archive, err := Open(canvasJAR)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	framebuffer := newTestFramebuffer(t, 4, 3)
	runtime, err := Start(archive, Options{Framebuffer: framebuffer})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if dimensions := invokeFixtureInt(t, runtime, "CanvasMIDlet", "dimensions"); dimensions != 4003 {
		t.Fatalf("dimensions() = %d, want 4003", dimensions)
	}
	if paints := invokeFixtureInt(t, runtime, "CanvasMIDlet", "paintCount"); paints != 1 {
		t.Fatalf("paintCount() = %d after start, want 1", paints)
	}
	frame, presents := framebuffer.Snapshot()
	if presents != 1 {
		t.Fatalf("framebuffer presents = %d after start, want 1", presents)
	}
	assertRGBAPixel(t, frame, 0, 0, []byte{0x10, 0x20, 0x30, 0xff})
	assertRGBAPixel(t, frame, 1, 1, []byte{0xff, 0x00, 0x00, 0xff})

	if _, err := runtime.VM.InvokeStatic("CanvasMIDlet", "requestPartialRepaint", "()V"); err != nil {
		t.Fatalf("requestPartialRepaint() error = %v", err)
	}
	if _, presents := framebuffer.Snapshot(); presents != 1 {
		t.Fatalf("framebuffer presents = %d before RunPending, want 1", presents)
	}
	if err := runtime.RunPending(); err != nil {
		t.Fatalf("RunPending() error = %v", err)
	}
	frame, presents = framebuffer.Snapshot()
	if presents != 2 {
		t.Fatalf("framebuffer presents = %d after coalesced repaint, want 2", presents)
	}
	if paints := invokeFixtureInt(t, runtime, "CanvasMIDlet", "paintCount"); paints != 2 {
		t.Fatalf("paintCount() = %d after coalesced repaint, want 2", paints)
	}
	assertRGBAPixel(t, frame, 0, 0, []byte{0x00, 0xff, 0x00, 0xff})
	assertRGBAPixel(t, frame, 2, 0, []byte{0x00, 0xff, 0x00, 0xff})
	assertRGBAPixel(t, frame, 3, 0, []byte{0x10, 0x20, 0x30, 0xff})
	assertRGBAPixel(t, frame, 1, 1, []byte{0xff, 0x00, 0x00, 0xff})

	if _, err := runtime.VM.InvokeStatic("CanvasMIDlet", "requestSynchronousRepaint", "()V"); err != nil {
		t.Fatalf("requestSynchronousRepaint() error = %v", err)
	}
	frame, presents = framebuffer.Snapshot()
	if presents != 3 {
		t.Fatalf("framebuffer presents = %d after serviceRepaints, want 3", presents)
	}
	assertRGBAPixel(t, frame, 0, 0, []byte{0x00, 0x00, 0xff, 0xff})
	assertRGBAPixel(t, frame, 3, 0, []byte{0x00, 0x00, 0xff, 0xff})
	if err := runtime.RunPending(); err != nil {
		t.Fatalf("RunPending() after serviceRepaints error = %v", err)
	}
	if _, presents := framebuffer.Snapshot(); presents != 3 {
		t.Fatalf("framebuffer presents = %d after drained stale event, want 3", presents)
	}
	if color := invokeFixtureInt(t, runtime, "CanvasMIDlet", "lastColor"); color != 0x0000ff {
		t.Fatalf("lastColor() = %#x, want 0x0000ff", color)
	}
}

func TestGraphicsClipTranslationAndPrimitives(t *testing.T) {
	archive, err := Open(canvasJAR)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	framebuffer := newTestFramebuffer(t, 4, 3)
	runtime, err := Start(archive, Options{Framebuffer: framebuffer})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	if _, err := runtime.VM.InvokeStatic("CanvasMIDlet", "requestClipTranslationPaint", "()V"); err != nil {
		t.Fatalf("requestClipTranslationPaint() error = %v", err)
	}
	if state := invokeFixtureInt(t, runtime, "CanvasMIDlet", "graphicsState"); state != 110022 {
		t.Fatalf("graphicsState() = %d, want 110022", state)
	}
	frame, presents := framebuffer.Snapshot()
	if presents != 2 {
		t.Fatalf("framebuffer presents after clipped paint = %d, want 2", presents)
	}
	assertRGBAPixel(t, frame, 0, 0, []byte{0x10, 0x10, 0x10, 0xff})
	assertRGBAPixel(t, frame, 1, 1, []byte{0x00, 0xff, 0x00, 0xff})
	assertRGBAPixel(t, frame, 2, 2, []byte{0x00, 0xff, 0x00, 0xff})
	assertRGBAPixel(t, frame, 3, 2, []byte{0x10, 0x10, 0x10, 0xff})

	if _, err := runtime.VM.InvokeStatic("CanvasMIDlet", "requestExtremeLinePaint", "()V"); err != nil {
		t.Fatalf("requestExtremeLinePaint() error = %v", err)
	}
	frame, presents = framebuffer.Snapshot()
	if presents != 3 {
		t.Fatalf("framebuffer presents after line paint = %d, want 3", presents)
	}
	for x := 0; x < frame.Width; x++ {
		assertRGBAPixel(t, frame, x, 0, []byte{0x00, 0x00, 0x00, 0xff})
		assertRGBAPixel(t, frame, x, 1, []byte{0xff, 0xff, 0xff, 0xff})
		assertRGBAPixel(t, frame, x, 2, []byte{0x00, 0x00, 0x00, 0xff})
	}

	if _, err := runtime.VM.InvokeStatic("CanvasMIDlet", "requestRectanglePaint", "()V"); err != nil {
		t.Fatalf("requestRectanglePaint() error = %v", err)
	}
	frame, presents = framebuffer.Snapshot()
	if presents != 4 {
		t.Fatalf("framebuffer presents after rectangle paint = %d, want 4", presents)
	}
	for _, point := range [][2]int{{1, 0}, {2, 0}, {3, 0}, {1, 1}, {3, 1}, {1, 2}, {2, 2}, {3, 2}} {
		assertRGBAPixel(t, frame, point[0], point[1], []byte{0xff, 0x00, 0x00, 0xff})
	}
	for y := 0; y < frame.Height; y++ {
		assertRGBAPixel(t, frame, 0, y, []byte{0x00, 0x00, 0xff, 0xff})
	}
	assertRGBAPixel(t, frame, 2, 1, []byte{0x00, 0x00, 0x00, 0xff})
}

func TestGraphicsImagesDecodeTransformBlendAndDrawRGB(t *testing.T) {
	archive, err := Open(canvasJAR)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	framebuffer := newTestFramebuffer(t, 4, 3)
	runtime, err := Start(archive, Options{Framebuffer: framebuffer})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	if _, err := runtime.VM.InvokeStatic("CanvasMIDlet", "requestImagePaint", "()V"); err != nil {
		t.Fatalf("requestImagePaint() error = %v", err)
	}
	if state := invokeFixtureInt(t, runtime, "CanvasMIDlet", "imageState"); state != 127 {
		t.Fatalf("imageState() = %d, want 127", state)
	}
	frame, presents := framebuffer.Snapshot()
	if presents != 2 {
		t.Fatalf("framebuffer presents after image paint = %d, want 2", presents)
	}
	want := [][][]byte{
		{{0x00, 0x00, 0xff, 0xff}, {0x00, 0x00, 0x00, 0xff}, {0xff, 0x00, 0x00, 0xff}, {0x00, 0x00, 0x00, 0xff}},
		{{0xff, 0xff, 0x00, 0xff}, {0xff, 0xff, 0xff, 0xff}, {0x00, 0x80, 0x00, 0xff}, {0x00, 0x00, 0x00, 0xff}},
		{{0x11, 0x22, 0x33, 0xff}, {0x44, 0x55, 0x66, 0xff}, {0xff, 0x00, 0xff, 0xff}, {0xff, 0xff, 0xff, 0xff}},
	}
	for y, row := range want {
		for x, pixel := range row {
			assertRGBAPixel(t, frame, x, y, pixel)
		}
	}
}

func TestGraphicsFontMetricsAnchorsAndTextEntryPoints(t *testing.T) {
	archive, err := Open(canvasJAR)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	framebuffer := newTestFramebuffer(t, 24, 12)
	runtime, err := Start(archive, Options{Framebuffer: framebuffer})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	if _, err := runtime.VM.InvokeStatic("CanvasMIDlet", "requestTextPaint", "()V"); err != nil {
		t.Fatalf("requestTextPaint() error = %v", err)
	}
	if state := invokeFixtureInt(t, runtime, "CanvasMIDlet", "fontState"); state != 63 {
		t.Fatalf("fontState() = %d, want 63", state)
	}
	frame, presents := framebuffer.Snapshot()
	if presents != 2 {
		t.Fatalf("framebuffer presents after text paint = %d, want 2", presents)
	}
	// The 5x7 body hangs from the reported baseline, so its top row is one
	// below the top of the line the three draws share.
	assertRGBAPixel(t, frame, 0, 1, []byte{0x00, 0x00, 0x00, 0xff})
	for x := 1; x <= 4; x++ {
		assertRGBAPixel(t, frame, x, 1, []byte{0xff, 0xff, 0xff, 0xff})
	}
	// The underline sits on the baseline the font reports, which is the
	// face's ascent — one row further down than the hand-fixed metrics put it.
	for _, span := range [][2]int{{0, 6}, {8, 14}, {16, 22}} {
		for x := span[0]; x <= span[1]; x++ {
			assertRGBAPixel(t, frame, x, 8, []byte{0xff, 0xff, 0xff, 0xff})
		}
	}
	for _, x := range []int{7, 15, 23} {
		assertRGBAPixel(t, frame, x, 8, []byte{0x00, 0x00, 0x00, 0xff})
	}
}

func TestCanvasReceivesActiveKeyEventsThroughEventLoop(t *testing.T) {
	archive, err := Open(canvasJAR)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	framebuffer := newTestFramebuffer(t, 4, 3)
	runtime, err := Start(archive, Options{Framebuffer: framebuffer})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	keyEvents := []struct {
		eventType  KeyEventType
		wantEvents int32
		wantColor  []byte
	}{
		{KeyPressed, 1, []byte{0xff, 0xff, 0x00, 0xff}},
		{KeyRepeated, 13, []byte{0xff, 0x00, 0xff, 0xff}},
		{KeyReleased, 132, []byte{0x00, 0xff, 0xff, 0xff}},
	}
	for index, keyEvent := range keyEvents {
		if err := runtime.SendKey(keyEvent.eventType, KeyCodeUp); err != nil {
			t.Fatalf("SendKey(%s) error = %v", keyEvent.eventType, err)
		}
		if events := invokeFixtureInt(t, runtime, "CanvasMIDlet", "keyEvents"); events != keyEvent.wantEvents {
			t.Fatalf("keyEvents() after %s = %d, want %d", keyEvent.eventType, events, keyEvent.wantEvents)
		}
		if code := invokeFixtureInt(t, runtime, "CanvasMIDlet", "lastKeyCode"); code != KeyCodeUp {
			t.Fatalf("lastKeyCode() after %s = %d, want %d", keyEvent.eventType, code, KeyCodeUp)
		}
		frame, presents := framebuffer.Snapshot()
		if wantPresents := uint64(index + 2); presents != wantPresents {
			t.Fatalf("presents after %s = %d, want %d", keyEvent.eventType, presents, wantPresents)
		}
		assertRGBAPixel(t, frame, 0, 0, keyEvent.wantColor)
	}

	if err := runtime.SendKey(KeyEventType("typed"), 1); !errors.Is(err, ErrInvalidKeyEvent) {
		t.Fatalf("SendKey(typed) error = %v, want ErrInvalidKeyEvent", err)
	}
	if runtime.State() != StateActive {
		t.Fatalf("state after invalid key event = %s, want active", runtime.State())
	}
	if err := runtime.Pause(); err != nil {
		t.Fatalf("Pause() error = %v", err)
	}
	if err := runtime.SendKey(KeyPressed, '1'); err != nil {
		t.Fatalf("SendKey() while paused error = %v", err)
	}
	if events := invokeFixtureInt(t, runtime, "CanvasMIDlet", "keyEvents"); events != 132 {
		t.Fatalf("keyEvents() while paused = %d, want unchanged 132", events)
	}
	if _, presents := framebuffer.Snapshot(); presents != 4 {
		t.Fatalf("presents while paused = %d, want unchanged 4", presents)
	}
}

func TestCanvasReceivesActivePointerEventsThroughEventLoop(t *testing.T) {
	archive, err := Open(canvasJAR)
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := Start(archive, testRuntimeOptions(t))
	if err != nil {
		t.Fatal(err)
	}

	for index, event := range []struct {
		typeName PointerEventType
		x        int32
		y        int32
		want     int32
	}{{PointerPressed, 1, 2, 1}, {PointerDragged, 3, 4, 13}, {PointerReleased, 5, 6, 132}} {
		if err := runtime.SendPointer(event.typeName, event.x, event.y); err != nil {
			t.Fatalf("SendPointer(%s) error = %v", event.typeName, err)
		}
		if events := invokeFixtureInt(t, runtime, "CanvasMIDlet", "pointerEvents"); events != event.want {
			t.Fatalf("pointerEvents() after event %d = %d, want %d", index, events, event.want)
		}
		if point := invokeFixtureInt(t, runtime, "CanvasMIDlet", "lastPointer"); point != event.x*1000+event.y {
			t.Fatalf("lastPointer() after event %d = %d", index, point)
		}
	}
	if err := runtime.SendPointer(PointerEventType("hover"), 0, 0); !errors.Is(err, ErrInvalidPointerEvent) {
		t.Fatalf("SendPointer(hover) error = %v, want ErrInvalidPointerEvent", err)
	}
	if err := runtime.Pause(); err != nil {
		t.Fatal(err)
	}
	if err := runtime.SendPointer(PointerPressed, 7, 8); err != nil {
		t.Fatal(err)
	}
	if events := invokeFixtureInt(t, runtime, "CanvasMIDlet", "pointerEvents"); events != 132 {
		t.Fatalf("pointerEvents() while paused = %d, want 132", events)
	}
}

func TestCanvasMapsKeysAndTracksFullScreenMode(t *testing.T) {
	archive, err := Open(canvasJAR)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	framebuffer := newTestFramebuffer(t, 4, 3)
	runtime, err := Start(archive, Options{Framebuffer: framebuffer})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if state := invokeFixtureInt(t, runtime, "CanvasMIDlet", "canvasAPIState"); state != 63 {
		t.Fatalf("canvasAPIState() = %d, want 63", state)
	}

	if _, err := runtime.VM.InvokeStatic("CanvasMIDlet", "setFullScreen", "(Z)V", jvm.IntValue(1)); err != nil {
		t.Fatalf("setFullScreen(true) error = %v", err)
	}
	if summary := runtime.Summary(); summary.Display == nil || !summary.Display.FullScreen {
		t.Fatalf("Summary() after full screen = %+v", summary)
	}
	if err := runtime.RunPending(); err != nil {
		t.Fatalf("RunPending() error = %v", err)
	}
	if _, presents := framebuffer.Snapshot(); presents != 2 {
		t.Fatalf("framebuffer presents after full-screen change = %d, want 2", presents)
	}

	if _, err := runtime.VM.InvokeStatic("CanvasMIDlet", "setFullScreen", "(Z)V", jvm.IntValue(0)); err != nil {
		t.Fatalf("setFullScreen(false) error = %v", err)
	}
	if summary := runtime.Summary(); summary.Display == nil || summary.Display.FullScreen {
		t.Fatalf("Summary() after leaving full screen = %+v", summary)
	}
}

func TestClassResourceDataInputStreamReadsJARResource(t *testing.T) {
	archive, err := Open(canvasJAR)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	runtime, err := Start(archive, testRuntimeOptions(t))
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if state := invokeFixtureInt(t, runtime, "CanvasMIDlet", "resourceState"); state != 3 {
		t.Fatalf("resourceState() = %d, want 3", state)
	}
}

// A title that owns its frame loop keeps the Graphics its paint was handed and
// draws through it from its own thread, pushing the result with XDisplay. MIDP
// calls that Graphics dead once paint returns; this vendor's handsets did not,
// and a title written for them draws its whole game that way.
func TestScreenGraphicsStaysUsableAfterPaint(t *testing.T) {
	archive, err := Open(canvasJAR)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	framebuffer := newTestFramebuffer(t, 4, 3)
	runtime, err := Start(archive, Options{Framebuffer: framebuffer})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if _, presents := framebuffer.Snapshot(); presents != 1 {
		t.Fatalf("framebuffer presents = %d after start, want 1", presents)
	}
	if !invokeFixtureBoolean(t, runtime, "CanvasMIDlet", "drawAfterPaint") {
		t.Fatal("drawAfterPaint() = false, want the kept Graphics to draw")
	}
	// XDisplay.refresh is how such a title puts what it drew on the screen. It
	// marks the screen ready rather than presenting on the spot — one local
	// family pushes about a hundred times per Host pass — so the picture
	// arrives with the pass, which is where a Host reads it anyway.
	if _, err := runtime.VM.InvokeStatic(skvm.XDisplayClass, "refresh", "(IIII)V",
		jvm.IntValue(0), jvm.IntValue(0), jvm.IntValue(1), jvm.IntValue(1)); err != nil {
		t.Fatalf("XDisplay.refresh() error = %v", err)
	}
	if _, presents := framebuffer.Snapshot(); presents != 1 {
		t.Fatalf("framebuffer presents = %d before the Host pass, want the refresh to wait for it", presents)
	}
	if err := runtime.RunPending(); err != nil {
		t.Fatalf("RunPending() error = %v", err)
	}
	frame, presents := framebuffer.Snapshot()
	if presents != 2 {
		t.Fatalf("framebuffer presents = %d after a refresh and a pass, want 2", presents)
	}
	// A pass with nothing pushed since the last one presents nothing.
	if err := runtime.RunPending(); err != nil {
		t.Fatalf("RunPending() error = %v", err)
	}
	if _, again := framebuffer.Snapshot(); again != 2 {
		t.Errorf("framebuffer presents = %d after an idle pass, want 2", again)
	}
	assertRGBAPixel(t, frame, 0, 0, []byte{0x00, 0xff, 0x00, 0xff})
	// The pixel the paint drew is still there: drawing outside paint adds to
	// the screen rather than starting a new one.
	assertRGBAPixel(t, frame, 1, 1, []byte{0xff, 0x00, 0x00, 0xff})
}

// A game sizes itself against the screen XDisplay publishes rather than asking
// its Canvas, so a zero there is a title that paints nothing and says nothing.
func TestXDisplayPublishesFramebufferSize(t *testing.T) {
	archive, err := Open(canvasJAR)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	runtime, err := Start(archive, Options{Framebuffer: newTestFramebuffer(t, 4, 3)})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	for _, field := range []struct {
		name string
		want int32
	}{{"width", 4}, {"height", 3}, {"height2", 3}} {
		value, err := runtime.VM.StaticField(skvm.XDisplayClass, field.name, "I")
		if err != nil {
			t.Fatalf("StaticField(%s) error = %v", field.name, err)
		}
		got, err := value.Int32()
		if err != nil {
			t.Fatal(err)
		}
		if got != field.want {
			t.Fatalf("XDisplay.%s = %d, want %d", field.name, got, field.want)
		}
	}
}

func TestCallSeriallyRunsOneRunnablePerPass(t *testing.T) {
	archive, err := Open(displayJAR)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	runtime, err := Start(archive, testRuntimeOptions(t))
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if _, err := runtime.VM.InvokeStatic("DisplayMIDlet", "requestSerial", "()V"); err != nil {
		t.Fatalf("requestSerial() error = %v", err)
	}
	// Queueing is not running: MIDP promises the Runnable follows the events
	// already on the loop, not the call that handed it over.
	if runs := invokeFixtureInt(t, runtime, "DisplayMIDlet", "serialRuns"); runs != 0 {
		t.Fatalf("serialRuns() before a pass = %d, want 0", runs)
	}
	for pass := 1; pass <= 2; pass++ {
		if err := runtime.RunPending(); err != nil {
			t.Fatalf("RunPending() pass %d error = %v", pass, err)
		}
		if runs := invokeFixtureInt(t, runtime, "DisplayMIDlet", "serialRuns"); runs != int32(pass) {
			t.Fatalf("serialRuns() after %d passes = %d, want %d", pass, runs, pass)
		}
	}

	if !invokeFixtureBoolean(t, runtime, "DisplayMIDlet", "nullSerialRejected") {
		t.Fatal("nullSerialRejected() = false, want true")
	}
}

// A frame loop written as a Runnable that hands itself back must advance one
// step per pass. Dispatched as an ordinary event it would re-queue itself
// inside the same drain and the pass would end on the event loop's own limit.
func TestCallSeriallyLoopAdvancesOneStepPerPass(t *testing.T) {
	archive, err := Open(displayJAR)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	runtime, err := Start(archive, testRuntimeOptions(t))
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if _, err := runtime.VM.InvokeStatic("DisplayMIDlet", "requestSerialLoop", "()V"); err != nil {
		t.Fatalf("requestSerialLoop() error = %v", err)
	}
	for pass := 1; pass <= 3; pass++ {
		if err := runtime.RunPending(); err != nil {
			t.Fatalf("RunPending() pass %d error = %v", pass, err)
		}
		if runs := invokeFixtureInt(t, runtime, "DisplayMIDlet", "loopRuns"); runs != int32(pass) {
			t.Fatalf("loopRuns() after %d passes = %d, want %d", pass, runs, pass)
		}
	}
}

func TestKeyEventsIgnoreCurrentNonCanvasDisplayable(t *testing.T) {
	archive, err := Open(displayJAR)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	runtime, err := Start(archive, testRuntimeOptions(t))
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if err := runtime.SendKey(KeyPressed, KeyCodeFire); err != nil {
		t.Fatalf("SendKey() to non-Canvas error = %v", err)
	}
	if runtime.State() != StateActive {
		t.Fatalf("state after ignored key event = %s, want active", runtime.State())
	}
}

func TestRuntimeProcessesPauseResumeAndDestroyEvents(t *testing.T) {
	runtime := startLifecycleFixture(t)

	if err := runtime.Pause(); err != nil {
		t.Fatalf("Pause() error = %v", err)
	}
	assertLifecycleState(t, runtime, StatePaused, 3)

	if err := runtime.Resume(); err != nil {
		t.Fatalf("Resume() error = %v", err)
	}
	assertLifecycleState(t, runtime, StateActive, 2)

	if err := runtime.Destroy(true); err != nil {
		t.Fatalf("Destroy() error = %v", err)
	}
	assertLifecycleState(t, runtime, StateDestroyed, 4)
	if err := runtime.Resume(); !errors.Is(err, ErrInvalidLifecycleState) {
		t.Fatalf("Resume() after destroy error = %v, want ErrInvalidLifecycleState", err)
	}
}

func TestInitialStartStateChangeExceptionPausesUntilRetry(t *testing.T) {
	archive, err := Open(deferredStartJAR)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	runtime, err := Start(archive, testRuntimeOptions(t))
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	assertLifecycleState(t, runtime, StatePaused, 7)
	if summary := runtime.Summary(); summary.Error != "" {
		t.Fatalf("Summary().Error = %q after deferred start", summary.Error)
	}

	if err := runtime.Resume(); err != nil {
		t.Fatalf("Resume() error = %v", err)
	}
	assertLifecycleState(t, runtime, StateActive, 2)
}

func TestInitialStartRuntimeExceptionForcesCleanupAndDestroysRuntime(t *testing.T) {
	archive, err := Open(runtimeFailureJAR)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	runtime, err := Start(archive, testRuntimeOptions(t))
	if err == nil {
		t.Fatal("Start() succeeded despite fixture RuntimeException")
	}
	if runtime == nil {
		t.Fatal("Start() returned a nil runtime after lifecycle failure")
	}
	if !strings.Contains(err.Error(), "start MIDlet LifecycleMIDlet") ||
		!strings.Contains(err.Error(), "java/lang/ArithmeticException") {
		t.Fatalf("Start() error = %v, want start RuntimeException diagnostic", err)
	}
	assertLifecycleState(t, runtime, StateDestroyed, 4)
	if summary := runtime.Summary(); summary.State != StateDestroyed || !strings.Contains(err.Error(), summary.Error) {
		t.Fatalf("Summary() = %+v, want destroyed state and original diagnostic", summary)
	}
}

func TestResumeStateChangeExceptionPausesUntilRetry(t *testing.T) {
	runtime := startLifecycleFixture(t)
	if _, err := runtime.VM.InvokeStatic("LifecycleMIDlet", "deferNextStart", "()V"); err != nil {
		t.Fatal(err)
	}
	if err := runtime.Pause(); err != nil {
		t.Fatalf("Pause() error = %v", err)
	}

	if err := runtime.Resume(); err != nil {
		t.Fatalf("Resume() error = %v", err)
	}
	assertLifecycleState(t, runtime, StatePaused, 7)
	if summary := runtime.Summary(); summary.Error != "" {
		t.Fatalf("Summary().Error = %q after deferred resume", summary.Error)
	}

	if err := runtime.Resume(); err != nil {
		t.Fatalf("retry Resume() error = %v", err)
	}
	assertLifecycleState(t, runtime, StateActive, 2)
}

func TestConditionalDestroyRefusalPreservesStateAndCanRetry(t *testing.T) {
	runtime := startLifecycleFixture(t)

	err := runtime.Destroy(false)
	if !errors.Is(err, ErrMIDletDestroyRefused) {
		t.Fatalf("Destroy(false) error = %v, want ErrMIDletDestroyRefused", err)
	}
	var guest *jvm.GuestException
	if !errors.As(err, &guest) || guest.Object == nil ||
		guest.Object.ClassName != "javax/microedition/midlet/MIDletStateChangeException" || guest.Message != "retry destroy" {
		t.Fatalf("Destroy(false) guest exception = %#v", guest)
	}
	assertLifecycleState(t, runtime, StateActive, 6)
	if summary := runtime.Summary(); summary.Error != "" {
		t.Fatalf("Summary().Error = %q after conditional refusal", summary.Error)
	}

	if err := runtime.Destroy(false); err != nil {
		t.Fatalf("retry Destroy(false) error = %v", err)
	}
	assertLifecycleState(t, runtime, StateDestroyed, 5)
}

func TestUnconditionalDestroyIgnoresStateChangeException(t *testing.T) {
	runtime := startLifecycleFixture(t)
	if _, err := runtime.VM.InvokeStatic("LifecycleMIDlet", "refuseForcedDestroy", "()V"); err != nil {
		t.Fatal(err)
	}

	if err := runtime.Destroy(true); err != nil {
		t.Fatalf("Destroy(true) error = %v", err)
	}
	if runtime.State() != StateDestroyed {
		t.Fatalf("runtime state = %s, want destroyed", runtime.State())
	}
	if summary := runtime.Summary(); summary.Error != "" {
		t.Fatalf("Summary().Error = %q after ignored exception", summary.Error)
	}
}

func TestRuntimeProcessesMIDletNotificationsThroughEventLoop(t *testing.T) {
	runtime := startLifecycleFixture(t)

	if _, err := runtime.VM.InvokeVirtual(runtime.MIDlet, "notifyPaused", "()V"); err != nil {
		t.Fatalf("notifyPaused() error = %v", err)
	}
	if runtime.State() != StateActive {
		t.Fatalf("state before RunPending() = %s, want active", runtime.State())
	}
	if err := runtime.RunPending(); err != nil {
		t.Fatalf("RunPending() error = %v", err)
	}
	if runtime.State() != StatePaused {
		t.Fatalf("state after notifyPaused = %s, want paused", runtime.State())
	}

	if _, err := runtime.VM.InvokeVirtual(runtime.MIDlet, "notifyDestroyed", "()V"); err != nil {
		t.Fatalf("notifyDestroyed() error = %v", err)
	}
	if err := runtime.RunPending(); err != nil {
		t.Fatalf("RunPending() error = %v", err)
	}
	if runtime.State() != StateDestroyed {
		t.Fatalf("state after notifyDestroyed = %s, want destroyed", runtime.State())
	}
}

func TestResumeRequestQueuedDuringPause(t *testing.T) {
	runtime := startLifecycleFixture(t)
	if _, err := runtime.VM.InvokeStatic("LifecycleMIDlet", "requestResumeOnPause", "()V"); err != nil {
		t.Fatal(err)
	}
	if err := runtime.Pause(); err != nil {
		t.Fatalf("Pause() error = %v", err)
	}
	assertLifecycleState(t, runtime, StateActive, 2)
}

func TestPauseRuntimeExceptionForcesCleanupAndDestroysRuntime(t *testing.T) {
	runtime := startLifecycleFixture(t)
	if _, err := runtime.VM.InvokeStatic("LifecycleMIDlet", "failNextPause", "()V"); err != nil {
		t.Fatal(err)
	}
	if err := runtime.Pause(); err == nil {
		t.Fatal("Pause() succeeded despite fixture failure")
	}
	summary := runtime.Summary()
	if summary.State != StateDestroyed || !strings.Contains(summary.Error, "pause MIDlet LifecycleMIDlet") ||
		!strings.Contains(summary.Error, "java/lang/ArithmeticException") {
		t.Fatalf("Summary() = %+v, want destroyed state with pause diagnostic", summary)
	}
	assertLifecycleState(t, runtime, StateDestroyed, 4)
	if err := runtime.Resume(); !errors.Is(err, ErrInvalidLifecycleState) {
		t.Fatalf("Resume() after failure error = %v, want ErrInvalidLifecycleState", err)
	}
}

func TestStartRuntimeExceptionForcesCleanupAndDestroysRuntime(t *testing.T) {
	runtime := startLifecycleFixture(t)
	if _, err := runtime.VM.InvokeStatic("LifecycleMIDlet", "failNextStart", "()V"); err != nil {
		t.Fatal(err)
	}
	if err := runtime.Pause(); err != nil {
		t.Fatalf("Pause() error = %v", err)
	}

	if err := runtime.Resume(); err == nil {
		t.Fatal("Resume() succeeded despite fixture failure")
	}
	summary := runtime.Summary()
	if summary.State != StateDestroyed || !strings.Contains(summary.Error, "resume MIDlet LifecycleMIDlet") {
		t.Fatalf("Summary() = %+v, want destroyed state with resume diagnostic", summary)
	}
	assertLifecycleState(t, runtime, StateDestroyed, 4)
}

func TestForcedCleanupIgnoresDestroyRuntimeException(t *testing.T) {
	runtime := startLifecycleFixture(t)
	for _, method := range []string{"failNextPause", "failNextDestroy"} {
		if _, err := runtime.VM.InvokeStatic("LifecycleMIDlet", method, "()V"); err != nil {
			t.Fatal(err)
		}
	}

	err := runtime.Pause()
	if err == nil || !strings.Contains(err.Error(), "pause MIDlet LifecycleMIDlet") {
		t.Fatalf("Pause() error = %v, want original pause failure", err)
	}
	assertLifecycleState(t, runtime, StateDestroyed, 8)
	if summary := runtime.Summary(); strings.Contains(summary.Error, "forced destroy cleanup") {
		t.Fatalf("Summary().Error = %q, want original callback diagnostic", summary.Error)
	}
}

func TestDestroyRuntimeExceptionIsIgnoredAndDestroysRuntime(t *testing.T) {
	runtime := startLifecycleFixture(t)
	if _, err := runtime.VM.InvokeStatic("LifecycleMIDlet", "failNextDestroy", "()V"); err != nil {
		t.Fatal(err)
	}

	if err := runtime.Destroy(false); err != nil {
		t.Fatalf("Destroy(false) error = %v", err)
	}
	assertLifecycleState(t, runtime, StateDestroyed, 8)
	if summary := runtime.Summary(); summary.Error != "" {
		t.Fatalf("Summary().Error = %q after ignored destroy exception", summary.Error)
	}
}

func TestStartRejectsNonMIDletMainClass(t *testing.T) {
	archive, err := Open(arithmeticJAR)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if _, err := Start(archive, testRuntimeOptions(t)); err == nil {
		t.Fatal("Start() accepted a main class that does not extend MIDlet")
	}
}

func startLifecycleFixture(t *testing.T) *Runtime {
	t.Helper()
	archive, err := Open(lifecycleJAR)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	runtime, err := Start(archive, testRuntimeOptions(t))
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	return runtime
}

func testRuntimeOptions(t *testing.T) Options {
	t.Helper()
	return Options{Framebuffer: newTestFramebuffer(t, 4, 3)}
}

func newTestFramebuffer(t *testing.T, width, height int) *backend.MemoryFramebuffer {
	t.Helper()
	framebuffer, err := backend.NewMemoryFramebuffer(width, height)
	if err != nil {
		t.Fatalf("NewMemoryFramebuffer() error = %v", err)
	}
	return framebuffer
}

func assertRGBAPixel(t *testing.T, frame backend.Frame, x, y int, want []byte) {
	t.Helper()
	index := (y*frame.Width + x) * 4
	if index < 0 || index+4 > len(frame.RGBA) {
		t.Fatalf("pixel (%d, %d) outside %dx%d frame", x, y, frame.Width, frame.Height)
	}
	if got := frame.RGBA[index : index+4]; !bytes.Equal(got, want) {
		t.Fatalf("pixel (%d, %d) = %v, want %v", x, y, got, want)
	}
}

func assertLifecycleState(t *testing.T, runtime *Runtime, wantState LifecycleState, wantGuestState int32) {
	t.Helper()
	if got := runtime.State(); got != wantState {
		t.Fatalf("runtime state = %s, want %s", got, wantState)
	}
	result, err := runtime.VM.InvokeStatic("LifecycleMIDlet", "state", "()I")
	if err != nil {
		t.Fatalf("state() error = %v", err)
	}
	guestState, err := result.Int32()
	if err != nil {
		t.Fatal(err)
	}
	if guestState != wantGuestState {
		t.Fatalf("guest state = %d, want %d", guestState, wantGuestState)
	}
}

func invokeFixtureInt(t *testing.T, runtime *Runtime, className, method string) int32 {
	t.Helper()
	result, err := runtime.VM.InvokeStatic(className, method, "()I")
	if err != nil {
		t.Fatalf("%s.%s() error = %v", className, method, err)
	}
	value, err := result.Int32()
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func invokeFixtureBoolean(t *testing.T, runtime *Runtime, className, method string) bool {
	t.Helper()
	result, err := runtime.VM.InvokeStatic(className, method, "()Z")
	if err != nil {
		t.Fatalf("%s.%s() error = %v", className, method, err)
	}
	value, err := result.Int32()
	if err != nil {
		t.Fatal(err)
	}
	return value != 0
}
