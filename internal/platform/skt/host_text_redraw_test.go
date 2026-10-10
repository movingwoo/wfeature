package skt

import (
	"context"
	"testing"

	"github.com/movingwoo/wfeature/internal/api/midp"
	"github.com/movingwoo/wfeature/internal/api/skvm"
	"github.com/movingwoo/wfeature/internal/jvm"
)

// redrawOnKeyCanvas defines a Canvas that hands every key to forward and then
// counts the redraw it performs, the way a title redraws its name field only
// after a key: one local title's TextComponent.repaint is empty, another's
// paint draws the field only on the pass its key handler asked for.
func redrawOnKeyCanvas(t *testing.T, r *Runtime, name string, forward func(key int32) error) (*jvm.Object, *int, *[]int32) {
	t.Helper()
	redraws, keys := 0, []int32{}
	if err := r.VM.DefineClass(jvm.ClassDefinition{
		Name: name, SuperName: midp.CanvasClass, Access: jvm.AccessPublic,
		Methods: []jvm.MethodDefinition{
			{Name: "paint", Descriptor: "(Ljavax/microedition/lcdui/Graphics;)V", Access: jvm.AccessPublic,
				Body: func(*jvm.Invocation, []jvm.Value) (jvm.Value, error) { return jvm.VoidValue(), nil }},
			{Name: "keyPressed", Descriptor: "(I)V", Access: jvm.AccessPublic,
				Body: func(_ *jvm.Invocation, args []jvm.Value) (jvm.Value, error) {
					key, _ := args[1].Int32()
					keys = append(keys, key)
					if err := forward(key); err != nil {
						return jvm.VoidValue(), err
					}
					redraws++
					return jvm.VoidValue(), nil
				}},
		},
	}); err != nil {
		t.Fatal(err)
	}
	canvas := &jvm.Object{ClassName: name}
	if _, err := r.VM.InvokeVirtual(r.display, "setCurrent", "(Ljavax/microedition/lcdui/Displayable;)V", jvm.ReferenceValue(canvas)); err != nil {
		t.Fatal(err)
	}
	if err := r.RunPending(); err != nil {
		t.Fatal(err)
	}
	return canvas, &redraws, &keys
}

func TestHostCommitRedrawsATitleTextComponent(t *testing.T) {
	r := startUIFixture(t)
	component := &jvm.Object{ClassName: "test/RedrawTextComponent", Fields: map[string]jvm.Value{}}
	handler := &jvm.Object{ClassName: skvm.TextComponentHandlerClass}
	text := []rune{}
	for _, name := range []string{"size", "getCaretPosition", "getMaxSize", "getConstraints"} {
		if err := r.VM.RegisterNative(component.ClassName, name, "()I", func(*jvm.VM, []jvm.Value) (jvm.Value, error) {
			switch name {
			case "size", "getCaretPosition":
				return jvm.IntValue(int32(len(text))), nil
			case "getMaxSize":
				return jvm.IntValue(6), nil
			}
			return jvm.IntValue(0), nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"repaint", "repaintIM"} {
		if err := r.VM.RegisterNative(component.ClassName, name, "()V", func(*jvm.VM, []jvm.Value) (jvm.Value, error) { return jvm.VoidValue(), nil }); err != nil {
			t.Fatal(err)
		}
	}
	if err := r.VM.RegisterNative(component.ClassName, "insert", "(C)V", func(_ *jvm.VM, args []jvm.Value) (jvm.Value, error) {
		character, _ := args[1].Int32()
		text = append(text, rune(character))
		return jvm.VoidValue(), nil
	}); err != nil {
		t.Fatal(err)
	}
	_, redraws, keys := redrawOnKeyCanvas(t, r, "test/TextComponentCanvas", func(key int32) error {
		_, err := r.VM.InvokeVirtual(handler, "keyPressed", "(I)Z", jvm.IntValue(key))
		return err
	})
	if _, err := r.setTextComponent(r.VM, []jvm.Value{jvm.ReferenceValue(handler), jvm.ReferenceValue(component)}); err != nil {
		t.Fatal(err)
	}
	edit, err := r.TextInput(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := edit.Commit(context.Background(), "가나다"); err != nil {
		t.Fatal(err)
	}
	if string(text) != "가나다" {
		t.Fatalf("component text = %q, want only the committed characters", string(text))
	}
	if *redraws != 1 || len(*keys) != 1 || (*keys)[0] != hostTextRedrawKey {
		t.Fatalf("title redraws = %d after keys %v, want one after the redraw key", *redraws, *keys)
	}
}

func TestHostCommitRedrawsATitleXTextField(t *testing.T) {
	r := startUIFixture(t)
	var field *jvm.Object
	canvas, redraws, keys := redrawOnKeyCanvas(t, r, "test/XTextFieldCanvas", func(key int32) error {
		_, err := r.VM.InvokeVirtual(field, "keyPressed", "(I)V", jvm.IntValue(key))
		return err
	})
	field = newXTextField(t, r, canvas, "", 8, 0)
	setXTextFieldFocusForTest(t, r, field, true)
	edit, err := r.TextInput(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := edit.Commit(context.Background(), "한글"); err != nil {
		t.Fatal(err)
	}
	if *redraws != 1 || len(*keys) != 1 || (*keys)[0] != hostTextRedrawKey {
		t.Fatalf("title redraws = %d after keys %v, want one after the redraw key", *redraws, *keys)
	}
	current, err := r.TextInput(context.Background())
	if err != nil || current.Text != "한글" {
		t.Fatalf("field after the redraw key = %+v, %v", current, err)
	}
}
