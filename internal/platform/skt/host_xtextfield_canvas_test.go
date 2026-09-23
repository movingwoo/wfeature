package skt

import (
	"context"
	"errors"
	"testing"

	"github.com/movingwoo/wfeature/internal/api/midp"
	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/jvm"
)

func TestHostXTextFieldPaintedOnCurrentCanvas(t *testing.T) {
	runtime := startUIFixture(t)
	placeholder, err := runtime.VM.NewObject(midp.CanvasClass, "()V")
	if err != nil {
		t.Fatal(err)
	}
	field := newXTextField(t, runtime, placeholder, "", 8, 0)
	setXTextFieldFocusForTest(t, runtime, field, true)
	paints := 0
	drawField := false
	if err := runtime.VM.DefineClass(jvm.ClassDefinition{
		Name: "HostEditorCanvas", SuperName: midp.CanvasClass, Access: jvm.AccessPublic,
		Methods: []jvm.MethodDefinition{{Name: "paint", Descriptor: "(Ljavax/microedition/lcdui/Graphics;)V", Access: jvm.AccessPublic,
			Body: func(_ *jvm.Invocation, args []jvm.Value) (jvm.Value, error) {
				paints++
				if !drawField {
					return jvm.VoidValue(), nil
				}
				return runtime.VM.InvokeVirtual(field, "paint", "(Ljavax/microedition/lcdui/Graphics;)V", args[1])
			}}},
	}); err != nil {
		t.Fatal(err)
	}
	canvas := &jvm.Object{ClassName: "HostEditorCanvas"}
	showCanvas := func() {
		t.Helper()
		if _, err := runtime.VM.InvokeVirtual(runtime.display, "setCurrent", "(Ljavax/microedition/lcdui/Displayable;)V", jvm.ReferenceValue(canvas)); err != nil {
			t.Fatal(err)
		}
		if err := runtime.RunPending(); err != nil {
			t.Fatal(err)
		}
	}
	showCanvas()
	if _, err := runtime.TextInput(context.Background()); !errors.Is(err, backend.ErrNoTextInput) {
		t.Fatalf("unpainted placeholder field: %v", err)
	}
	drawField = true
	if err := runtime.repaintNewCurrentCanvas(canvas); err != nil {
		t.Fatal(err)
	}
	if err := runtime.RunPending(); err != nil {
		t.Fatal(err)
	}
	edit, err := runtime.TextInput(context.Background())
	if err != nil {
		t.Fatalf("painted focused field: %v", err)
	}
	before := paints
	if err := edit.Commit(context.Background(), "한글"); err != nil {
		t.Fatal(err)
	}
	if paints <= before {
		t.Fatal("Host commit did not repaint the visible Canvas")
	}
	current, err := runtime.TextInput(context.Background())
	if err != nil || current.Text != "한글" {
		t.Fatalf("reopen = %+v, %v", current, err)
	}
	setXTextFieldFocusForTest(t, runtime, field, false)
	setXTextFieldFocusForTest(t, runtime, field, true)
	if _, err := runtime.TextInput(context.Background()); !errors.Is(err, backend.ErrNoTextInput) {
		t.Fatalf("restored focus reused an old paint: %v", err)
	}
	if err := current.Commit(context.Background(), "stale"); !errors.Is(err, backend.ErrTextInputChanged) {
		t.Fatalf("restored focus accepted an old snapshot: %v", err)
	}
	if err := runtime.repaintNewCurrentCanvas(canvas); err != nil {
		t.Fatal(err)
	}
	if err := runtime.RunPending(); err != nil {
		t.Fatal(err)
	}
	current, err = runtime.TextInput(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	invokeFixtureVoid(t, runtime, "UIMIDlet", "showTextBox")
	if err := current.Commit(context.Background(), "stale"); !errors.Is(err, backend.ErrTextInputChanged) {
		t.Fatalf("changed screen accepted an old snapshot: %v", err)
	}
	drawField = false
	showCanvas()
	if _, err := runtime.TextInput(context.Background()); !errors.Is(err, backend.ErrNoTextInput) {
		t.Fatalf("screen round trip reused an old paint: %v", err)
	}
}

func TestHostXTextFieldRejectsOffscreenPaint(t *testing.T) {
	runtime := startUIFixture(t)
	invokeFixtureVoid(t, runtime, "UIMIDlet", "showBuffer")
	placeholder, err := runtime.VM.NewObject(midp.CanvasClass, "()V")
	if err != nil {
		t.Fatal(err)
	}
	field := newXTextField(t, runtime, placeholder, "", 8, 0)
	setXTextFieldFocusForTest(t, runtime, field, true)
	value, err := runtime.VM.InvokeStatic(midp.ImageClass, "createImage", "(II)Ljavax/microedition/lcdui/Image;", jvm.IntValue(16), jvm.IntValue(16))
	if err != nil {
		t.Fatal(err)
	}
	bitmap, err := value.Reference()
	if err != nil {
		t.Fatal(err)
	}
	graphics, err := runtime.VM.InvokeVirtual(bitmap, "getGraphics", "()Ljavax/microedition/lcdui/Graphics;")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.VM.InvokeVirtual(field, "paint", "(Ljavax/microedition/lcdui/Graphics;)V", graphics); err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.TextInput(context.Background()); !errors.Is(err, backend.ErrNoTextInput) {
		t.Fatalf("offscreen paint exposed a hidden field: %v", err)
	}
}
