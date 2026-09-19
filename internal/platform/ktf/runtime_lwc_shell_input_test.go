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

func TestShellTextInputDoesNotGuessAnEditor(t *testing.T) {
	for _, kind := range []string{"multiple", "nested", "vendor", "listener", "non-text"} {
		t.Run(kind, func(t *testing.T) {
			session, shell, field := shellTextBox(t)
			switch kind {
			case "multiple":
				callShellInput(t, session, runtimeComponentAddComponent, shell, jvm.ReferenceValue(newWidget(runtimeTextBoxComponentClass)))
			case "nested":
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
				t.Fatalf("ambiguous input: %v", err)
			}
		})
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
