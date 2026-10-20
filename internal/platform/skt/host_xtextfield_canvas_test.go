package skt

import (
	"context"
	"errors"
	"testing"

	"github.com/movingwoo/wfeature/internal/api/midp"
	"github.com/movingwoo/wfeature/internal/api/skvm"
	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/jvm"
)

// A focused field the current Canvas draws is offered however it is drawn.
// Painted onto the screen, the strict route takes it; drawn by a title that
// reads its text and draws that itself, or with its screen paint evidence gone
// after a refocus or a screen round trip, the fallback does, and the commit
// lands either way. An old snapshot still fails once the field or the screen
// changed under it, and with the focus off and nothing painted there is no
// field at all.
func TestHostXTextFieldFocusedOnTheCurrentCanvas(t *testing.T) {
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
					_, err := runtime.VM.InvokeVirtual(field, "getText", "()Ljava/lang/String;")
					return jvm.VoidValue(), err
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
	repaint := func() {
		t.Helper()
		if err := runtime.repaintNewCurrentCanvas(canvas); err != nil {
			t.Fatal(err)
		}
		if err := runtime.RunPending(); err != nil {
			t.Fatal(err)
		}
	}
	text := func() string {
		data, _ := nativeXTextField(field)
		return string(data.text)
	}
	showCanvas()
	unpainted, err := runtime.TextInput(context.Background())
	if err != nil {
		t.Fatalf("focused field whose text the Canvas draws: %v", err)
	}
	before := paints
	if err := unpainted.Commit(context.Background(), "가"); err != nil {
		t.Fatalf("commit into the unpainted field: %v", err)
	}
	if text() != "가" || paints <= before {
		t.Fatalf("unpainted commit left %q after %d -> %d paints", text(), before, paints)
	}

	drawField = true
	repaint()
	edit, err := runtime.TextInput(context.Background())
	if err != nil || edit.Text != "가" {
		t.Fatalf("painted focused field = %+v, %v", edit, err)
	}
	before = paints
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
	if err := current.Commit(context.Background(), "stale"); !errors.Is(err, backend.ErrTextInputChanged) {
		t.Fatalf("restored focus accepted an old snapshot: %v", err)
	}
	if refocused, err := runtime.TextInput(context.Background()); err != nil || refocused.Text != "한글" {
		t.Fatalf("refocused field before its next paint = %+v, %v", refocused, err)
	}
	repaint()
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
	back, err := runtime.TextInput(context.Background())
	if err != nil || back.Text != "한글" {
		t.Fatalf("focused field after a screen round trip = %+v, %v", back, err)
	}
	if err := back.Commit(context.Background(), "왕복"); err != nil || text() != "왕복" {
		t.Fatalf("commit after a screen round trip: %v, text %q", err, text())
	}
	setXTextFieldFocusForTest(t, runtime, field, false)
	if _, err := runtime.TextInput(context.Background()); !errors.Is(err, backend.ErrNoTextInput) {
		t.Fatalf("an unfocused field nothing painted since the screen changed: %v", err)
	}
}

// A field left focused on a Canvas the title has moved on from is no field
// of the new one: neither built for it nor drawn while it is up, it is not
// offered there, and an edit opened on the old Canvas is not committed into
// it. Once the new Canvas draws it, it is.
func TestHostXTextFieldLeftFocusedOnAnotherCanvasIsNotOffered(t *testing.T) {
	runtime := startUIFixture(t)
	invokeFixtureVoid(t, runtime, "UIMIDlet", "showBuffer")
	nameCanvas := runtime.currentDisplayable
	field := newXTextField(t, runtime, nameCanvas, "name", 8, 0)
	setXTextFieldFocusForTest(t, runtime, field, true)
	edit, err := runtime.TextInput(context.Background())
	if err != nil || edit.Text != "name" {
		t.Fatalf("the field on its own Canvas = %+v, %v", edit, err)
	}
	if err := runtime.VM.DefineClass(jvm.ClassDefinition{
		Name: "GameCanvas", SuperName: midp.CanvasClass, Access: jvm.AccessPublic,
		Methods: []jvm.MethodDefinition{{Name: "paint", Descriptor: "(Ljavax/microedition/lcdui/Graphics;)V", Access: jvm.AccessPublic,
			Body: func(*jvm.Invocation, []jvm.Value) (jvm.Value, error) { return jvm.VoidValue(), nil }}},
	}); err != nil {
		t.Fatal(err)
	}
	game := &jvm.Object{ClassName: "GameCanvas"}
	if _, err := runtime.VM.InvokeVirtual(runtime.display, "setCurrent", "(Ljavax/microedition/lcdui/Displayable;)V", jvm.ReferenceValue(game)); err != nil {
		t.Fatal(err)
	}
	if err := runtime.RunPending(); err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.TextInput(context.Background()); !errors.Is(err, backend.ErrNoTextInput) {
		t.Fatalf("a field focused on the previous Canvas was offered: %v", err)
	}
	if err := edit.Commit(context.Background(), "changed"); !errors.Is(err, backend.ErrTextInputChanged) {
		t.Fatalf("an edit from the previous Canvas: %v", err)
	}
	if data, _ := nativeXTextField(field); string(data.text) != "name" {
		t.Fatalf("the field from the previous Canvas holds %q", data.text)
	}
	paintXTextFieldOffscreen(t, runtime, field)
	if again, err := runtime.TextInput(context.Background()); err != nil || again.Text != "name" {
		t.Fatalf("the field once the new Canvas draws it = %+v, %v", again, err)
	}
}

// A title that draws its field into its own back buffer never paints it onto
// the screen Graphics, so the strict route cannot place it. The field is
// focused and a Canvas is up, so the fallback offers it and the commit lands
// in it. A Screen is drawn by the runtime and cannot be showing the field, so
// while one is current the same field is not offered.
func TestHostXTextFieldPaintedOffscreenIsOffered(t *testing.T) {
	runtime := startUIFixture(t)
	placeholder, err := runtime.VM.NewObject(midp.CanvasClass, "()V")
	if err != nil {
		t.Fatal(err)
	}
	field := newXTextField(t, runtime, placeholder, "ab", 8, 0)
	setXTextFieldFocusForTest(t, runtime, field, true)
	paintXTextFieldOffscreen(t, runtime, field)
	if _, err := runtime.TextInput(context.Background()); !errors.Is(err, backend.ErrNoTextInput) {
		t.Fatalf("a List offered a vendor field: %v", err)
	}
	invokeFixtureVoid(t, runtime, "UIMIDlet", "showBuffer")
	paintXTextFieldOffscreen(t, runtime, field)
	edit, err := runtime.TextInput(context.Background())
	if err != nil || edit.Text != "ab" || edit.Append {
		t.Fatalf("focused field painted offscreen = %+v, %v", edit, err)
	}
	if err := edit.Commit(context.Background(), "버퍼"); err != nil {
		t.Fatal(err)
	}
	if data, _ := nativeXTextField(field); string(data.text) != "버퍼" {
		t.Fatalf("XTextField text = %q", data.text)
	}
}

// A title that never focuses its field still paints the one it means the
// player to type into, so with nothing focused the one field painted on the
// current Canvas is offered, into whatever Graphics it was painted. A field
// nobody painted is not, even on its own Canvas. The commit hands the title
// the redraw key a focused field's commit does, and an edit opened before
// another field took the focus is not committed. A focused field that cannot
// take text is still the focused one, and the painted field is not offered in
// its place.
func TestHostXTextFieldWithoutFocusOffersTheOnePaintedField(t *testing.T) {
	r := startUIFixture(t)
	var field *jvm.Object
	canvas, redraws, keys := redrawOnKeyCanvas(t, r, "test/UnfocusedFieldCanvas", func(key int32) error {
		_, err := r.VM.InvokeVirtual(field, "keyPressed", "(I)V", jvm.IntValue(key))
		return err
	})
	field = newXTextField(t, r, canvas, "ab", 8, 0)
	if _, err := r.TextInput(context.Background()); !errors.Is(err, backend.ErrNoTextInput) {
		t.Fatalf("an unfocused field nothing painted: %v", err)
	}
	paintXTextFieldOffscreen(t, r, field)
	edit, err := r.TextInput(context.Background())
	if err != nil || edit.Text != "ab" || edit.MaxLength != 8 || edit.Append {
		t.Fatalf("the one painted field = %+v, %v", edit, err)
	}
	if err := edit.Commit(context.Background(), "한글"); err != nil {
		t.Fatal(err)
	}
	if data, _ := nativeXTextField(field); string(data.text) != "한글" {
		t.Fatalf("XTextField text = %q", data.text)
	}
	if *redraws != 1 || len(*keys) != 1 || (*keys)[0] != hostTextRedrawKey {
		t.Fatalf("title redraws = %d after keys %v, want one after the redraw key", *redraws, *keys)
	}
	stale, err := r.TextInput(context.Background())
	if err != nil || stale.Text != "한글" {
		t.Fatalf("reopen = %+v, %v", stale, err)
	}
	other := newXTextField(t, r, canvas, "other", 8, 0)
	setXTextFieldFocusForTest(t, r, other, true)
	if err := stale.Commit(context.Background(), "wrong field"); !errors.Is(err, backend.ErrTextInputChanged) {
		t.Fatalf("an edit of the painted field survived another field taking the focus: %v", err)
	}
	if focused, err := r.TextInput(context.Background()); err != nil || focused.Text != "other" {
		t.Fatalf("the focused field beside a painted one = %+v, %v", focused, err)
	}
	uneditable := newXTextField(t, r, canvas, "fixed", 8, textFieldUneditable)
	setXTextFieldFocusForTest(t, r, uneditable, true)
	paintXTextFieldOffscreen(t, r, field)
	if _, err := r.TextInput(context.Background()); !errors.Is(err, backend.ErrNoTextInput) {
		t.Fatalf("a painted field was offered in place of the focused UNEDITABLE one: %v", err)
	}
}

// A title closes its editor by turning the field's focus off and no longer
// painting it. Focus calls clear the paint record as they clear the screen
// evidence, so the closed field is not offered on the screen that follows,
// and a field still painted after its focus went off is offered again by its
// next paint.
func TestHostXTextFieldClosedByItsFocusIsNotOffered(t *testing.T) {
	runtime := startUIFixture(t)
	invokeFixtureVoid(t, runtime, "UIMIDlet", "showBuffer")
	placeholder, err := runtime.VM.NewObject(midp.CanvasClass, "()V")
	if err != nil {
		t.Fatal(err)
	}
	field := newXTextField(t, runtime, placeholder, "name", 8, 0)
	setXTextFieldFocusForTest(t, runtime, field, true)
	paintXTextFieldOffscreen(t, runtime, field)
	if edit, err := runtime.TextInput(context.Background()); err != nil || edit.Text != "name" {
		t.Fatalf("the open editor = %+v, %v", edit, err)
	}
	setXTextFieldFocusForTest(t, runtime, field, false)
	if _, err := runtime.TextInput(context.Background()); !errors.Is(err, backend.ErrNoTextInput) {
		t.Fatalf("the closed field was offered: %v", err)
	}
	paintXTextFieldOffscreen(t, runtime, field)
	if edit, err := runtime.TextInput(context.Background()); err != nil || edit.Text != "name" {
		t.Fatalf("a field painted after its focus went off = %+v, %v", edit, err)
	}
}

// A focus call that changes nothing — a title turning off, after every paint,
// the focus its fields do not have — neither empties the record of paints nor
// spends an edit, so the one painted field stays offered and its commit lands.
func TestHostXTextFieldRepeatedFocusOffKeepsThePaintedField(t *testing.T) {
	runtime := startUIFixture(t)
	invokeFixtureVoid(t, runtime, "UIMIDlet", "showBuffer")
	field := newXTextField(t, runtime, runtime.currentDisplayable, "name", 8, 0)
	frame := func() {
		t.Helper()
		paintXTextFieldOffscreen(t, runtime, field)
		setXTextFieldFocusForTest(t, runtime, field, false)
	}
	frame()
	edit, err := runtime.TextInput(context.Background())
	if err != nil || edit.Text != "name" {
		t.Fatalf("the painted field after a focus-off that changed nothing = %+v, %v", edit, err)
	}
	frame()
	if err := edit.Commit(context.Background(), "이름"); err != nil {
		t.Fatalf("commit across a frame that set the same focus: %v", err)
	}
	if data, _ := nativeXTextField(field); string(data.text) != "이름" {
		t.Fatalf("the field holds %q", data.text)
	}
}

// Two fields painted on the same Displayable with neither focused leave no
// way to tell which the player means, so neither is offered, and an edit
// opened while only one was painted fails once the second is. The record
// starts over when the Displayable changes.
func TestHostXTextFieldTwoPaintedFieldsAreAmbiguous(t *testing.T) {
	runtime := startUIFixture(t)
	invokeFixtureVoid(t, runtime, "UIMIDlet", "showBuffer")
	owner := runtime.currentDisplayable
	first := newXTextField(t, runtime, owner, "first", 8, 0)
	second := newXTextField(t, runtime, owner, "second", 8, 0)
	paintXTextFieldOffscreen(t, runtime, first)
	paintXTextFieldOffscreen(t, runtime, first)
	edit, err := runtime.TextInput(context.Background())
	if err != nil || edit.Text != "first" {
		t.Fatalf("one field painted twice = %+v, %v", edit, err)
	}
	paintXTextFieldOffscreen(t, runtime, second)
	if _, err := runtime.TextInput(context.Background()); !errors.Is(err, backend.ErrNoTextInput) {
		t.Fatalf("two painted fields offered one: %v", err)
	}
	if err := edit.Commit(context.Background(), "stale"); !errors.Is(err, backend.ErrTextInputChanged) {
		t.Fatalf("commit after a second field was painted: %v", err)
	}
	if data, _ := nativeXTextField(first); string(data.text) != "first" {
		t.Fatalf("an ambiguous commit changed the first field to %q", data.text)
	}
	invokeFixtureVoid(t, runtime, "UIMIDlet", "showMenu")
	invokeFixtureVoid(t, runtime, "UIMIDlet", "showBuffer")
	if _, err := runtime.TextInput(context.Background()); !errors.Is(err, backend.ErrNoTextInput) {
		t.Fatalf("paints from before the screen changed were reused: %v", err)
	}
	paintXTextFieldOffscreen(t, runtime, second)
	if again, err := runtime.TextInput(context.Background()); err != nil || again.Text != "second" {
		t.Fatalf("the one field painted after the screen changed = %+v, %v", again, err)
	}
}

// The fallbacks come after every strict route, so a strict pick is never
// displaced. The TextComponent the vendor input method is attached to beats a
// focused field nothing shows on screen, a focused field painted onto the
// screen beats the component, and while a component is attached, even one too
// full to take text, no fallback is asked. A fallback edit opened before a
// component was attached is not committed. A Form's only TextField is a
// fallback too and waits the same way: this pins the order the strict routes
// had, not a finding that the component is the field on screen.
func TestHostTextInputFallbacksKeepTheStrictOrder(t *testing.T) {
	r := startUIFixture(t)
	invokeFixtureVoid(t, r, "UIMIDlet", "showBuffer")
	placeholder, err := r.VM.NewObject(midp.CanvasClass, "()V")
	if err != nil {
		t.Fatal(err)
	}
	field := newXTextField(t, r, placeholder, "field", 8, 0)
	setXTextFieldFocusForTest(t, r, field, true)
	size := int32(0)
	component := newTextComponentForTest(t, r, "test/PriorityTextComponent", &size, 8)
	handler := &jvm.Object{ClassName: skvm.TextComponentHandlerClass}
	attach := func(target *jvm.Object) {
		t.Helper()
		if _, err := r.setTextComponent(r.VM, []jvm.Value{jvm.ReferenceValue(handler), jvm.ReferenceValue(target)}); err != nil {
			t.Fatal(err)
		}
	}
	attach(component)
	if edit, err := r.TextInput(context.Background()); err != nil || !edit.Append {
		t.Fatalf("a focused field nothing shows displaced the attached component: %+v, %v", edit, err)
	}
	paintXTextFieldOnScreen(t, r, field)
	if edit, err := r.TextInput(context.Background()); err != nil || edit.Append || edit.Text != "field" {
		t.Fatalf("the attached component displaced a field painted on screen: %+v, %v", edit, err)
	}
	// A refocus takes the screen evidence away again, and a full component
	// cannot take text and still keeps the fallback from being asked.
	setXTextFieldFocusForTest(t, r, field, false)
	setXTextFieldFocusForTest(t, r, field, true)
	size = 8
	if _, err := r.TextInput(context.Background()); !errors.Is(err, backend.ErrNoTextInput) {
		t.Fatalf("a fallback was asked while a component was attached: %v", err)
	}
	attach(nil)
	fallback, err := r.TextInput(context.Background())
	if err != nil || fallback.Append || fallback.Text != "field" {
		t.Fatalf("focused field once nothing is attached = %+v, %v", fallback, err)
	}
	size = 0
	attach(component)
	if err := fallback.Commit(context.Background(), "late"); !errors.Is(err, backend.ErrTextInputChanged) {
		t.Fatalf("a fallback edit was committed after a component was attached: %v", err)
	}
	if data, _ := nativeXTextField(field); string(data.text) != "field" {
		t.Fatalf("the refused commit changed the field to %q", data.text)
	}

	formField := newTextFieldForTest(t, r, "이름", "form", 10, 0)
	showFormForTest(t, r, newStringItemForTest(t, r, "안내", "이름"), formField)
	if edit, err := r.TextInput(context.Background()); err != nil || !edit.Append {
		t.Fatalf("a Form's only field displaced the attached component: %+v, %v", edit, err)
	}
	attach(nil)
	formEdit, err := r.TextInput(context.Background())
	if err != nil || formEdit.Append || formEdit.Text != "form" {
		t.Fatalf("a Form's only field once nothing is attached = %+v, %v", formEdit, err)
	}
	attach(component)
	if err := formEdit.Commit(context.Background(), "late"); !errors.Is(err, backend.ErrTextInputChanged) {
		t.Fatalf("a Form fallback edit was committed after a component was attached: %v", err)
	}
	if text := textFieldStringForTest(t, r, formField); text != "form" {
		t.Fatalf("the refused commit changed the Form field to %q", text)
	}
}

// paintXTextFieldOffscreen paints a field into an Image the way a title draws
// its back buffer, which never touches the screen Graphics.
func paintXTextFieldOffscreen(t *testing.T, runtime *Runtime, field *jvm.Object) {
	t.Helper()
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
}

// paintXTextFieldOnScreen paints a field through the one screen Graphics, the
// paint the strict route takes as proof the field is shown.
func paintXTextFieldOnScreen(t *testing.T, runtime *Runtime, field *jvm.Object) {
	t.Helper()
	graphics, err := runtime.toolkitScreenGraphics(runtime.VM, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.VM.InvokeVirtual(field, "paint", "(Ljavax/microedition/lcdui/Graphics;)V", graphics); err != nil {
		t.Fatal(err)
	}
}

// newTextComponentForTest makes a title-owned TextComponent of the given
// class whose size the test sets. It takes no text: the tests that use it ask
// only which field a Host edit is for.
func newTextComponentForTest(t *testing.T, runtime *Runtime, class string, size *int32, maxSize int32) *jvm.Object {
	t.Helper()
	for _, name := range []string{"size", "getCaretPosition", "getMaxSize", "getConstraints"} {
		if err := runtime.VM.RegisterNative(class, name, "()I", func(*jvm.VM, []jvm.Value) (jvm.Value, error) {
			switch name {
			case "size", "getCaretPosition":
				return jvm.IntValue(*size), nil
			case "getMaxSize":
				return jvm.IntValue(maxSize), nil
			}
			return jvm.IntValue(0), nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	return &jvm.Object{ClassName: class, Fields: map[string]jvm.Value{}}
}
