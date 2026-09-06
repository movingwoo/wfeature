package skt

import (
	"strings"
	"testing"

	"github.com/movingwoo/wfeature/internal/api/midp"
	"github.com/movingwoo/wfeature/internal/jvm"
)

// The order a Canvas is told about, and why it is the whole of two titles
//
// MIDP calls showNotify on the Canvas arriving on the screen before it paints
// it, and hideNotify on the one that left. Two archives in the reference
// corpus put load-bearing work in the first of those — one loads the picture
// its next paint draws, the other starts the thread that is its entire game —
// and neither says anything when it is not called: the first ends a paint on a
// null image and the second sits still for as long as anyone watches.
//
// So the order is what this pins, not merely the call: a showNotify that
// arrives after the first paint is the same defect wearing a different face.
func TestACanvasIsToldItIsShownBeforeItPaints(t *testing.T) {
	runtime := startProbeRuntime(t)
	var calls []string
	first := defineProbeCanvas(t, runtime, "ShowNotifyProbeOne", &calls)

	showProbeCanvas(t, runtime, first)

	if got := strings.Join(calls, ","); got != "one.showNotify,one.paint" {
		t.Fatalf("callbacks = %q, want the Canvas told it is shown before it is painted", got)
	}
}

// A Canvas that leaves the screen is told too, and the one arriving is told
// after it — a title that stops its thread in hideNotify and starts it again
// in showNotify has to see the pair in that order.
func TestTheCanvasThatLeavesIsToldBeforeTheOneThatArrives(t *testing.T) {
	runtime := startProbeRuntime(t)
	var calls []string
	first := defineProbeCanvas(t, runtime, "ShowNotifyProbeOne", &calls)
	second := defineProbeCanvas(t, runtime, "ShowNotifyProbeTwo", &calls)

	showProbeCanvas(t, runtime, first)
	calls = calls[:0]
	showProbeCanvas(t, runtime, second)

	if got := strings.Join(calls, ","); got != "one.hideNotify,two.showNotify,two.paint" {
		t.Fatalf("callbacks = %q, want the outgoing Canvas hidden before the incoming one is shown", got)
	}
}

// Becoming current again is not becoming current: a title told it was shown
// twice with nothing in between would run its once-per-appearance work twice.
func TestACanvasAlreadyOnTheScreenIsNotToldAgain(t *testing.T) {
	runtime := startProbeRuntime(t)
	var calls []string
	canvas := defineProbeCanvas(t, runtime, "ShowNotifyProbeOne", &calls)

	showProbeCanvas(t, runtime, canvas)
	calls = calls[:0]
	showProbeCanvas(t, runtime, canvas)

	for _, call := range calls {
		if strings.HasSuffix(call, ".showNotify") || strings.HasSuffix(call, ".hideNotify") {
			t.Fatalf("callbacks = %v, want no visibility callback for a Canvas that never left", calls)
		}
	}
}

// A throw out of showNotify is a callback that did not happen, not a session
// that ends — the rule uncaught.go states for every other guest callback.
func TestAThrowFromShowNotifyDoesNotEndTheSession(t *testing.T) {
	runtime := startProbeRuntime(t)
	throwing := jvm.ClassDefinition{
		Name:      "ShowNotifyProbeThrows",
		SuperName: midp.CanvasClass,
		Access:    jvm.AccessPublic,
		Methods: []jvm.MethodDefinition{
			{Name: "<init>", Descriptor: "()V", Access: jvm.AccessPublic, Body: probeReturn},
			{Name: "showNotify", Descriptor: "()V", Access: jvm.AccessProtected, Body: func(*jvm.Invocation, []jvm.Value) (jvm.Value, error) {
				return jvm.VoidValue(), newGuestException("java/lang/NullPointerException", "probe")
			}},
			{Name: "paint", Descriptor: "(Ljavax/microedition/lcdui/Graphics;)V", Access: jvm.AccessProtected, Body: probeReturn},
		},
	}
	if err := runtime.VM.DefineClass(throwing); err != nil {
		t.Fatalf("DefineClass() error = %v", err)
	}
	canvas, err := runtime.VM.NewObject("ShowNotifyProbeThrows", "()V")
	if err != nil {
		t.Fatalf("NewObject() error = %v", err)
	}

	showProbeCanvas(t, runtime, canvas)

	if state := runtime.State(); state != StateActive {
		t.Fatalf("State() = %v, want the session to survive a showNotify that threw", state)
	}
	count, first := runtime.UncaughtCallbacks()
	if count != 1 {
		t.Fatalf("UncaughtCallbacks() = %d, %q, want the one throw recorded", count, first)
	}
	if !strings.Contains(first, "showNotify") {
		t.Fatalf("UncaughtCallbacks() first = %q, want it to name showNotify", first)
	}
}

// startProbeRuntime is a started session with a framebuffer big enough to
// paint into and nothing else of its own on the screen.
func startProbeRuntime(t *testing.T) *Runtime {
	t.Helper()
	archive, err := Open(canvasJAR)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	runtime, err := Start(archive, Options{Framebuffer: newTestFramebuffer(t, 40, 40)})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	t.Cleanup(func() { _ = runtime.Destroy(true) })
	return runtime
}

// defineProbeCanvas installs a Canvas subclass whose three callbacks append
// their own name to one list, which is what makes the order readable.
func defineProbeCanvas(t *testing.T, runtime *Runtime, name string, calls *[]string) *jvm.Object {
	t.Helper()
	label := "one"
	if strings.HasSuffix(name, "Two") {
		label = "two"
	}
	record := func(callback string) jvm.ContextMethod {
		return func(*jvm.Invocation, []jvm.Value) (jvm.Value, error) {
			*calls = append(*calls, label+"."+callback)
			return jvm.VoidValue(), nil
		}
	}
	definition := jvm.ClassDefinition{
		Name:      name,
		SuperName: midp.CanvasClass,
		Access:    jvm.AccessPublic,
		Methods: []jvm.MethodDefinition{
			{Name: "<init>", Descriptor: "()V", Access: jvm.AccessPublic, Body: probeReturn},
			{Name: "showNotify", Descriptor: "()V", Access: jvm.AccessProtected, Body: record("showNotify")},
			{Name: "hideNotify", Descriptor: "()V", Access: jvm.AccessProtected, Body: record("hideNotify")},
			{Name: "paint", Descriptor: "(Ljavax/microedition/lcdui/Graphics;)V", Access: jvm.AccessProtected, Body: record("paint")},
		},
	}
	if err := runtime.VM.DefineClass(definition); err != nil {
		t.Fatalf("DefineClass(%s) error = %v", name, err)
	}
	canvas, err := runtime.VM.NewObject(name, "()V")
	if err != nil {
		t.Fatalf("NewObject(%s) error = %v", name, err)
	}
	return canvas
}

// showProbeCanvas puts a Canvas on the screen the way a title does — through
// the Display native — and then runs the Host pass that applies it, so the
// test drives the same path a game does rather than the fields behind it.
func showProbeCanvas(t *testing.T, runtime *Runtime, canvas *jvm.Object) {
	t.Helper()
	display, err := runtime.VM.InvokeStatic(midp.DisplayClass, "getDisplay",
		"(Ljavax/microedition/midlet/MIDlet;)Ljavax/microedition/lcdui/Display;", jvm.ReferenceValue(runtime.MIDlet))
	if err != nil {
		t.Fatalf("getDisplay() error = %v", err)
	}
	object, err := display.Reference()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.VM.InvokeVirtual(object, "setCurrent",
		"(Ljavax/microedition/lcdui/Displayable;)V", jvm.ReferenceValue(canvas)); err != nil {
		t.Fatalf("setCurrent() error = %v", err)
	}
	if err := runtime.RunPending(); err != nil {
		t.Fatalf("RunPending() error = %v", err)
	}
}

func probeReturn(*jvm.Invocation, []jvm.Value) (jvm.Value, error) { return jvm.VoidValue(), nil }
