package skt

import (
	"context"
	"errors"
	"testing"

	"github.com/movingwoo/wfeature/internal/api/skvm"
	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/jvm"
)

func TestSKTRestoredSnapshots(t *testing.T) {
	for _, change := range []string{"text", "screen", "constraint", "menu", "limit"} {
		t.Run(change, func(t *testing.T) {
			r := startUIFixture(t)
			invokeFixtureVoid(t, r, "UIMIDlet", "showTextBox")
			box := r.currentDisplayable
			edit, err := r.TextInput(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			invoke := func(name, descriptor string, v jvm.Value) {
				t.Helper()
				if _, err := r.VM.InvokeVirtual(box, name, descriptor, v); err != nil {
					t.Fatal(err)
				}
			}
			switch change {
			case "text":
				for _, v := range []string{"guest edit", edit.Text} {
					invoke("setString", "(Ljava/lang/String;)V", jvm.ReferenceValue(r.VM.NewString(v)))
				}
			case "screen":
				invokeFixtureVoid(t, r, "UIMIDlet", "showForm")
				invokeFixtureVoid(t, r, "UIMIDlet", "showTextBox")
			case "constraint":
				invoke("setConstraints", "(I)V", jvm.IntValue(2))
				invoke("setConstraints", "(I)V", jvm.IntValue(0))
			case "limit":
				invoke("setMaxSize", "(I)I", jvm.IntValue(int32(edit.MaxLength+1)))
				invoke("setMaxSize", "(I)I", jvm.IntValue(int32(edit.MaxLength)))
			case "menu":
				data := r.displayableState(box)
				data.commands = make([]*jvm.Object, 3)
				if err := r.deliverScreenKey(box, KeyPressed, KeyCodeSoft2); err != nil {
					t.Fatal(err)
				}
				if !data.menuOpen {
					t.Fatal("command menu did not open")
				}
				if err := r.deliverScreenKey(box, KeyPressed, KeyCodeSoft2); err != nil {
					t.Fatal(err)
				}
			}
			if err := edit.Commit(t.Context(), "host edit"); !errors.Is(err, backend.ErrTextInputChanged) {
				t.Fatalf("restored %s accepted stale edit: %v", change, err)
			}
			fresh, err := r.TextInput(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			if err := fresh.Commit(t.Context(), "fresh"); err != nil {
				t.Fatalf("fresh edit failed: %v", err)
			}
		})
	}
}

func TestSKTFormRestoredTargetRejectsOldEdit(t *testing.T) {
	for _, change := range []string{"selection", "text", "membership"} {
		t.Run(change, func(t *testing.T) {
			r := startUIFixture(t)
			invokeFixtureVoid(t, r, "UIMIDlet", "showForm")
			pressKey(t, r, KeyCodeDown)
			pressKey(t, r, KeyCodeDown)
			form := r.currentDisplayable
			data := r.screenState(form, screenForm)
			index := data.selection
			field := data.items[index]
			edit, err := r.TextInput(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			switch change {
			case "selection":
				pressKey(t, r, KeyCodeUp)
				pressKey(t, r, KeyCodeDown)
			case "text":
				for _, value := range []string{"guest", edit.Text} {
					if _, err := r.VM.InvokeVirtual(field, "setString", "(Ljava/lang/String;)V", jvm.ReferenceValue(r.VM.NewString(value))); err != nil {
						t.Fatal(err)
					}
				}
			case "membership":
				if _, err := r.VM.InvokeVirtual(form, "delete", "(I)V", jvm.IntValue(int32(index))); err != nil {
					t.Fatal(err)
				}
				if _, err := r.VM.InvokeVirtual(form, "insert", "(ILjavax/microedition/lcdui/Item;)V", jvm.IntValue(int32(index)), jvm.ReferenceValue(field)); err != nil {
					t.Fatal(err)
				}
				pressKey(t, r, KeyCodeDown)
			}
			if err := edit.Commit(t.Context(), "stale"); !errors.Is(err, backend.ErrTextInputChanged) {
				t.Fatalf("restored %s accepted stale edit: %v", change, err)
			}
			fresh, err := r.TextInput(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			if err := fresh.Commit(t.Context(), "fresh"); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestSKTXTextFieldRestoredTargetRejectsOldEdit(t *testing.T) {
	for _, change := range []string{"focus", "text", "limit"} {
		t.Run(change, func(t *testing.T) {
			r := startUIFixture(t)
			invokeFixtureVoid(t, r, "UIMIDlet", "showBuffer")
			field := newXTextField(t, r, r.currentDisplayable, "ab", 6, 0)
			setXTextFieldFocusForTest(t, r, field, true)
			edit, err := r.TextInput(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			switch change {
			case "focus":
				other := newXTextField(t, r, r.currentDisplayable, "other", 6, 0)
				setXTextFieldFocusForTest(t, r, other, true)
				setXTextFieldFocusForTest(t, r, field, true)
			case "text":
				for _, text := range []string{"guest", "ab"} {
					if _, err := r.setXTextFieldText(r.VM, []jvm.Value{jvm.ReferenceValue(field), jvm.ReferenceValue(r.VM.NewString(text))}); err != nil {
						t.Fatal(err)
					}
				}
			case "limit":
				for _, limit := range []int32{7, 6} {
					if _, err := r.setXTextFieldMaxSize(r.VM, []jvm.Value{jvm.ReferenceValue(field), jvm.IntValue(limit)}); err != nil {
						t.Fatal(err)
					}
				}
			}
			if err := edit.Commit(t.Context(), "stale"); !errors.Is(err, backend.ErrTextInputChanged) {
				t.Fatalf("restored %s accepted stale edit: %v", change, err)
			}
			fresh, err := r.TextInput(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			if err := fresh.Commit(t.Context(), "fresh"); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestSKTAppendStopsOnCallbackChange(t *testing.T) {
	for _, change := range []string{"detach", "restore", "screen", "cancel", "last-character"} {
		t.Run(change, func(t *testing.T) {
			r := startUIFixture(t)
			component := &jvm.Object{ClassName: "test/ChangingTextComponent", Fields: map[string]jvm.Value{}}
			handler := &jvm.Object{ClassName: skvm.TextComponentHandlerClass}
			inserted := 0
			attach := func(target *jvm.Object) {
				t.Helper()
				if _, err := r.setTextComponent(r.VM, []jvm.Value{jvm.ReferenceValue(handler), jvm.ReferenceValue(target)}); err != nil {
					t.Fatal(err)
				}
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			for _, name := range []string{"size", "getCaretPosition", "getMaxSize", "getConstraints"} {
				if err := r.VM.RegisterNative(component.ClassName, name, "()I", func(*jvm.VM, []jvm.Value) (jvm.Value, error) {
					switch name {
					case "size", "getCaretPosition":
						return jvm.IntValue(int32(inserted)), nil
					case "getMaxSize":
						return jvm.IntValue(16), nil
					}
					return jvm.IntValue(0), nil
				}); err != nil {
					t.Fatal(err)
				}
			}
			if err := r.VM.RegisterNative(component.ClassName, "repaint", "()V", func(*jvm.VM, []jvm.Value) (jvm.Value, error) { return jvm.VoidValue(), nil }); err != nil {
				t.Fatal(err)
			}
			stop := 1
			if change == "last-character" {
				stop = 3
			}
			if err := r.VM.RegisterNative(component.ClassName, "insert", "(C)V", func(*jvm.VM, []jvm.Value) (jvm.Value, error) {
				inserted++
				if inserted == stop {
					switch change {
					case "detach", "last-character":
						attach(nil)
					case "restore":
						attach(nil)
						attach(component)
					case "screen":
						if _, err := r.VM.InvokeStatic("UIMIDlet", "showTextBox", "()V"); err != nil {
							return jvm.VoidValue(), err
						}
					case "cancel":
						cancel()
					}
				}
				return jvm.VoidValue(), nil
			}); err != nil {
				t.Fatal(err)
			}
			attach(component)
			edit, err := r.TextInput(ctx)
			if err != nil {
				t.Fatal(err)
			}
			err = edit.Commit(ctx, "abc")
			wantErr := backend.ErrTextInputChanged
			if change == "cancel" {
				wantErr = context.Canceled
			}
			if inserted != stop || !errors.Is(err, wantErr) {
				t.Fatalf("delivery continued after %s: inserted=%d err=%v", change, inserted, err)
			}
			if err := edit.Commit(t.Context(), "abc"); !errors.Is(err, backend.ErrTextInputChanged) || inserted != stop {
				t.Fatalf("consumed snapshot could be reused: inserted=%d err=%v", inserted, err)
			}
		})
	}
}

func TestSKTHostThenKeypadUTF16Limit(t *testing.T) {
	for _, route := range []string{"TextBox", "XTextField"} {
		t.Run(route, func(t *testing.T) {
			r := startUIFixture(t)
			var field *jvm.Object
			if route == "TextBox" {
				invokeFixtureVoid(t, r, "UIMIDlet", "showTextBox")
				field = r.currentDisplayable
				if _, err := r.VM.InvokeVirtual(field, "setMaxSize", "(I)I", jvm.IntValue(4)); err != nil {
					t.Fatal(err)
				}
			} else {
				invokeFixtureVoid(t, r, "UIMIDlet", "showBuffer")
				field = newXTextField(t, r, r.currentDisplayable, "", 4, 0)
				setXTextFieldFocusForTest(t, r, field, true)
			}
			key := func(ch rune) {
				t.Helper()
				if route == "TextBox" {
					if err := r.deliverScreenKey(field, KeyPressed, int32(ch)); err != nil {
						t.Fatal(err)
					}
				} else if _, err := r.xTextFieldKeyPressed(r.VM, []jvm.Value{jvm.ReferenceValue(field), jvm.IntValue(int32(ch))}); err != nil {
					t.Fatal(err)
				}
			}
			value := func() string {
				if route == "TextBox" {
					return fixtureString(t, r, "UIMIDlet", "typedText")
				}
				data, _ := nativeXTextField(field)
				return string(data.text)
			}
			edit, err := r.TextInput(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			if err := edit.Commit(t.Context(), "AB🙂"); err != nil {
				t.Fatal(err)
			}
			key('2')
			if text := value(); text != "AB🙂" {
				t.Fatalf("keypad changed a full UTF-16 field: %q", text)
			}
			key('#')
			key('2')
			if text := value(); text != "ABa" {
				t.Fatalf("delete then insert: %q", text)
			}
		})
	}
}
