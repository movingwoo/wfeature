package ktf

import (
	"errors"
	"testing"

	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/jvm"
)

func shellTextBox(t *testing.T) (*Session, *jvm.Object, *jvm.Object) {
	t.Helper()
	session, field := focusedLWCField(t, runtimeTextBoxComponentClass, 0, "old")
	delete(session.Client.runtime.runtimeObjects, "lwc:focus")
	shell := newWidget(runtimeShellComponentClass)
	callShellInput(t, session, runtimeComponentAddComponent, shell, jvm.ReferenceValue(field))
	return session, shell, field
}

func callShellInput(t *testing.T, session *Session, method runtimeJavaImplementation, receiver *jvm.Object, args ...jvm.Value) {
	t.Helper()
	if _, err := method(session.Client.runtime, session.Client.JVM(), append([]jvm.Value{jvm.ReferenceValue(receiver)}, args...)); err != nil {
		t.Fatal(err)
	}
}

func TestShellTextInputShowCommitHide(t *testing.T) {
	session, shell, field := shellTextBox(t)
	if _, err := session.TextInput(t.Context()); !errors.Is(err, backend.ErrNoTextInput) {
		t.Fatalf("hidden input: %v", err)
	}
	callShellInput(t, session, runtimeComponentShown(true), shell)
	edit, err := session.TextInput(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if !edit.Multiline || edit.Text != "old" {
		t.Fatalf("editor: %+v", edit)
	}
	if err := edit.Commit(t.Context(), "new name"); err != nil {
		t.Fatal(err)
	}
	if componentText(field) != "new name" || widgetInt(t, shell, componentShownField) != 1 {
		t.Fatal("commit must update the field and leave acceptance to the guest")
	}
	callShellInput(t, session, runtimeComponentShown(false), shell)
	if _, err := session.TextInput(t.Context()); !errors.Is(err, backend.ErrNoTextInput) {
		t.Fatalf("hidden input: %v", err)
	}
}

func TestShellTextInputRejectsChangedLifecycle(t *testing.T) {
	for _, change := range []string{"hide", "hide-show", "remove", "remove-add", "replace-shell", "work"} {
		t.Run(change, func(t *testing.T) {
			session, shell, field := shellTextBox(t)
			callShellInput(t, session, runtimeComponentShown(true), shell)
			edit, err := session.TextInput(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			switch change {
			case "hide", "hide-show":
				callShellInput(t, session, runtimeComponentShown(false), shell)
				if change == "hide-show" {
					callShellInput(t, session, runtimeComponentShown(true), shell)
				}
			case "remove", "remove-add":
				callShellInput(t, session, runtimeComponentRemoveAllComponents, shell)
				if change == "remove-add" {
					callShellInput(t, session, runtimeComponentAddComponent, shell, jvm.ReferenceValue(field))
				}
			case "replace-shell":
				callShellInput(t, session, runtimeComponentShown(true), newWidget(runtimeShellComponentClass))
			case "work":
				callShellInput(t, session, runtimeComponentSetWorkComponent, shell, jvm.ReferenceValue(newWidget(runtimeTextBoxComponentClass)))
			}
			if err := edit.Commit(t.Context(), "changed"); !errors.Is(err, backend.ErrTextInputChanged) {
				t.Fatalf("commit: %v", err)
			}
			if componentText(field) != "old" {
				t.Fatal("stale edit mutated field")
			}
		})
	}
}

// A shell's field is offered only when the Host can write into it: not a
// vendor field, whose form owns it, not a field a composition listener drives,
// not something that is no text field at all, and not a field its container no
// longer lists.
func TestShellTextInputRefusesFieldsItCannotWrite(t *testing.T) {
	for _, kind := range []string{"stale parent", "vendor", "listener", "non-text"} {
		t.Run(kind, func(t *testing.T) {
			session, shell, field := shellTextBox(t)
			switch kind {
			case "stale parent":
				// The field still names the shell as its parent, which no
				// longer lists it.
				container := newWidget(runtimeContainerComponentClass)
				container.Native = []*jvm.Object{field}
				shell.Native = []*jvm.Object{container}
			case "vendor":
				field.ClassName = runtimeGTextFieldClass
			case "listener":
				handler, _ := field.Fields[componentInputHandlerField].Reference()
				handler.Fields[inputMethodListenerField] = jvm.ReferenceValue(newWidget("test/Listener"))
			case "non-text":
				field.ClassName = runtimeButtonComponentClass
			}
			callShellInput(t, session, runtimeComponentShown(true), shell)
			if _, err := session.TextInput(t.Context()); !errors.Is(err, backend.ErrNoTextInput) {
				t.Fatalf("unwritable field offered: %v", err)
			}
		})
	}
}

// A shell holding several fields, or holding its field inside containers of
// its own, is typed into at the field the title marked with focusNotify, and
// without one at its first field. The fields are found where the title put
// them: the shell's children in order, then its work component.
func TestShellTextInputChoosesAmongItsFields(t *testing.T) {
	for _, layout := range []string{"first of two", "focused second", "nested", "work after children"} {
		t.Run(layout, func(t *testing.T) {
			session, shell, field := shellTextBox(t)
			second := unfocusedLWCField(t, session, "second")
			want := field
			switch layout {
			case "first of two":
				callShellInput(t, session, runtimeComponentAddComponent, shell, jvm.ReferenceValue(second))
			case "focused second":
				callShellInput(t, session, runtimeComponentAddComponent, shell, jvm.ReferenceValue(second))
				callShellInput(t, session, runtimeComponentBooleanField(componentFocusedField), second, jvm.IntValue(1))
				want = second
			case "nested":
				callShellInput(t, session, runtimeComponentRemoveAllComponents, shell)
				outer, inner := newWidget(runtimeContainerComponentClass), newWidget(runtimeContainerComponentClass)
				callShellInput(t, session, runtimeComponentAddComponent, shell, jvm.ReferenceValue(newWidget(runtimeButtonComponentClass)))
				callShellInput(t, session, runtimeComponentAddComponent, inner, jvm.ReferenceValue(field))
				callShellInput(t, session, runtimeComponentAddComponent, outer, jvm.ReferenceValue(inner))
				callShellInput(t, session, runtimeComponentAddComponent, shell, jvm.ReferenceValue(outer))
			case "work after children":
				callShellInput(t, session, runtimeComponentSetWorkComponent, shell, jvm.ReferenceValue(second))
			}
			callShellInput(t, session, runtimeComponentShown(true), shell)
			edit, err := session.TextInput(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			if err := edit.Commit(t.Context(), "typed"); err != nil || componentText(want) != "typed" {
				t.Fatalf("commit: field=%q second=%q err=%v", componentText(field), componentText(second), err)
			}
		})
	}
}

// A shell a title showed over its form and hid again leaves the form on the
// screen, although the record of the newest shell went with it: the form's
// field is offered whether or not the title gave it focus, and a commit lands
// in it. Hiding the form as well leaves nothing.
func TestShellTextInputFindsAShellStillShownBelowAHiddenOne(t *testing.T) {
	for _, focus := range []bool{false, true} {
		t.Run(map[bool]string{false: "no focus", true: "focus"}[focus], func(t *testing.T) {
			session, form, field := shellTextBox(t)
			callShellInput(t, session, runtimeComponentShown(true), form)
			if focus {
				callShellInput(t, session, runtimeComponentSetFocus, field)
			}
			message := newWidget(runtimeShellComponentClass)
			callShellInput(t, session, runtimeComponentShown(true), message)
			// Under the message, only a field the title focused is offered:
			// its form is still shown, but a shell is searched for fields only
			// when it is the newest one on the screen.
			switch _, err := session.TextInput(t.Context()); {
			case focus && err != nil:
				t.Fatalf("focused form field under a shown message: %v", err)
			case !focus && !errors.Is(err, backend.ErrNoTextInput):
				t.Fatalf("form field under a shown message: %v", err)
			}
			callShellInput(t, session, runtimeComponentShown(false), message)
			edit, err := session.TextInput(t.Context())
			if err != nil {
				t.Fatalf("form field after the message was hidden: %v", err)
			}
			if err := edit.Commit(t.Context(), "after"); err != nil || componentText(field) != "after" {
				t.Fatalf("commit: text=%q err=%v", componentText(field), err)
			}
			callShellInput(t, session, runtimeComponentShown(false), form)
			if _, err := session.TextInput(t.Context()); !errors.Is(err, backend.ErrNoTextInput) {
				t.Fatalf("hidden form offered its field: %v", err)
			}
		})
	}
}

// The history of shown shells keeps only shells still shown, newest first, and
// a shell shown again moves to the front without losing the ones below it.
func TestShellHistoryKeepsTheShellsStillShown(t *testing.T) {
	session, first, _ := shellTextBox(t)
	runtime := session.Client.runtime
	second, third := newWidget(runtimeShellComponentClass), newWidget(runtimeShellComponentClass)
	for _, shell := range []*jvm.Object{first, second, third, second} {
		callShellInput(t, session, runtimeComponentShown(true), shell)
	}
	history := func() []*jvm.Object {
		var shells []*jvm.Object
		for node := runtime.runtimeObjects[runtimeLWCShellHistoryObject]; node != nil && len(shells) < 8; node, _ = node.Fields[componentShellBelowField].Reference() {
			shells = append(shells, node)
		}
		return shells
	}
	if got := history(); len(got) != 3 || got[0] != second || got[1] != third || got[2] != first {
		t.Fatalf("history after showing second again = %v", got)
	}
	callShellInput(t, session, runtimeComponentShown(false), third)
	callShellInput(t, session, runtimeComponentShown(false), second)
	if got := history(); len(got) != 1 || got[0] != first || runtime.shownShell() != first {
		t.Fatalf("history after hiding two = %v", got)
	}
	if _, linked := third.Fields[componentShellBelowField]; linked {
		t.Fatal("a hidden shell kept its link into the history")
	}
}

func TestShellTextInputSupportsWorkComponent(t *testing.T) {
	session, shell, field := shellTextBox(t)
	callShellInput(t, session, runtimeComponentRemoveAllComponents, shell)
	callShellInput(t, session, runtimeComponentSetWorkComponent, shell, jvm.ReferenceValue(field))
	callShellInput(t, session, runtimeComponentShown(true), shell)
	edit, err := session.TextInput(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if err := edit.Commit(t.Context(), "text"); err != nil {
		t.Fatal(err)
	}
	if componentText(field) != "text" {
		t.Fatal("work component did not receive text")
	}
}

func TestShellTextInputPreservesExplicitGuestFocus(t *testing.T) {
	session, shell, _ := shellTextBox(t)
	field := newWidget(runtimeTextFieldComponentClass)
	callShellInput(t, session, runtimeTextComponentConstructorWithText, field, jvm.ReferenceValue(session.Client.vm.NewString("focused")), jvm.IntValue(0))
	callShellInput(t, session, runtimeComponentSetFocus, field)
	callShellInput(t, session, runtimeComponentShown(true), shell)
	edit, err := session.TextInput(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if edit.Multiline || edit.Text != "focused" {
		t.Fatalf("explicit focus was replaced: %+v", edit)
	}
}
