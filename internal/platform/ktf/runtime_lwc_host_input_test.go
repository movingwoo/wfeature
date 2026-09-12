package ktf

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"unicode/utf16"

	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/jvm"
)

func focusedLWCField(t *testing.T, class string, constraint int32, text string) (*Session, *jvm.Object) {
	t.Helper()
	client, runtime := newTestRuntime(t)
	field := newWidget(class)
	if _, err := runtimeTextComponentConstructorWithText(runtime, client.JVM(), []jvm.Value{
		jvm.ReferenceValue(field), jvm.ReferenceValue(client.JVM().NewString(text)), jvm.IntValue(constraint),
	}); err != nil {
		t.Fatal(err)
	}
	runtime.runtimeObjects["lwc:focus"] = field
	return &Session{Client: client}, field
}

func TestTextInputCommitsAWholeHostCompositionToTheFocusedLWCField(t *testing.T) {
	session, field := focusedLWCField(t, runtimeTextFieldComponentClass, 0, "old")
	field.Fields[componentMaxLengthField] = jvm.IntValue(4)

	input, err := session.TextInput(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if input.Text != "old" || input.MaxLength != 4 || input.Multiline || input.Password || input.InputMode != "text" {
		t.Fatalf("TextInput() = %+v", input)
	}
	if err := input.Commit(context.Background(), "한글🙂"); err != nil {
		t.Fatal(err)
	}
	if got := runtimeComponentText(field); got != "한글🙂" {
		t.Fatalf("component text = %q, want committed composition", got)
	}
}

func TestTextInputRefusesAnInstalledCompositionDeltaListener(t *testing.T) {
	session, field := focusedLWCField(t, runtimeTextFieldComponentClass, 0, "old")
	handler, err := field.Fields[componentInputHandlerField].Reference()
	if err != nil {
		t.Fatal(err)
	}
	listener := newWidget("test/InputListener")
	listenerText := "old"
	callbackCount := 0
	if err := session.Client.JVM().RegisterNative("test/InputListener", "notifyTextChanged", "([CII)V", func(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
		callbackCount++
		// A replacement delta applies to the current composition fragment,
		// represented here by the last character. Treating a completed field
		// value as that delta would turn "old" plus "new" into "olnew".
		characters, err := arguments[1].Reference()
		if err != nil {
			return jvm.VoidValue(), err
		}
		_, values, err := jvm.ArraySnapshot(characters)
		if err != nil {
			return jvm.VoidValue(), err
		}
		length, err := arguments[2].Int32()
		if err != nil || length < 0 || int(length) > len(values) {
			return jvm.VoidValue(), fmt.Errorf("invalid input delta length %d", length)
		}
		mode, err := arguments[3].Int32()
		if err != nil || mode != 0 {
			return jvm.VoidValue(), fmt.Errorf("input delta mode = %d, want replacement", mode)
		}
		units := make([]uint16, length)
		for index := range units {
			unit, intErr := values[index].Int32()
			if intErr != nil {
				return jvm.VoidValue(), intErr
			}
			units[index] = uint16(unit)
		}
		listenerText = listenerText[:len(listenerText)-1] + string(utf16.Decode(units))
		return jvm.VoidValue(), nil
	}); err != nil {
		t.Fatal(err)
	}
	handler.Fields[inputMethodListenerField] = jvm.ReferenceValue(listener)

	if input, err := session.TextInput(context.Background()); !errors.Is(err, backend.ErrNoTextInput) {
		if err == nil {
			err = input.Commit(context.Background(), "new")
		}
		t.Fatalf("TextInput with composition listener error = %v, listener text = %q", err, listenerText)
	}
	if callbackCount != 0 || listenerText != "old" || runtimeComponentText(field) != "old" {
		t.Fatalf("refused input changed listener %q or component %q (%d callbacks)", listenerText, runtimeComponentText(field), callbackCount)
	}
}

func TestTextInputCommitRejectsAListenerInstalledAfterSnapshot(t *testing.T) {
	session, field := focusedLWCField(t, runtimeTextFieldComponentClass, 0, "old")
	input, err := session.TextInput(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	handler, err := field.Fields[componentInputHandlerField].Reference()
	if err != nil {
		t.Fatal(err)
	}
	handler.Fields[inputMethodListenerField] = jvm.ReferenceValue(newWidget("test/InputListener"))

	if err := input.Commit(context.Background(), "new"); !errors.Is(err, backend.ErrTextInputChanged) {
		t.Fatalf("commit after listener installation error = %v", err)
	}
	if got := runtimeComponentText(field); got != "old" {
		t.Fatalf("stale commit changed field to %q", got)
	}
}

func TestTextInputCommitRejectsAListenerInstalledThenRemovedAfterSnapshot(t *testing.T) {
	session, field := focusedLWCField(t, runtimeTextFieldComponentClass, 0, "old")
	input, err := session.TextInput(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	handler, err := field.Fields[componentInputHandlerField].Reference()
	if err != nil {
		t.Fatal(err)
	}
	setListener := runtimeComponentSetField("InputMethodHandler.setInputMethodListener", inputMethodListenerField)
	if _, err := setListener(session.Client.runtime, session.Client.JVM(), []jvm.Value{
		jvm.ReferenceValue(handler), jvm.ReferenceValue(newWidget("test/InputListener")),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := setListener(session.Client.runtime, session.Client.JVM(), []jvm.Value{
		jvm.ReferenceValue(handler), jvm.ReferenceValue(nil),
	}); err != nil {
		t.Fatal(err)
	}

	if err := input.Commit(context.Background(), "new"); !errors.Is(err, backend.ErrTextInputChanged) {
		t.Fatalf("commit after listener install and removal error = %v", err)
	}
	if got := runtimeComponentText(field); got != "old" {
		t.Fatalf("stale commit changed field to %q", got)
	}
}

func TestTextInputRefusesMalformedInputMethodState(t *testing.T) {
	for _, probe := range []struct {
		name   string
		mutate func(*jvm.Object)
	}{
		{
			name: "handler",
			mutate: func(field *jvm.Object) {
				field.Fields[componentInputHandlerField] = jvm.IntValue(1)
			},
		},
		{
			name: "listener",
			mutate: func(field *jvm.Object) {
				handler, _ := field.Fields[componentInputHandlerField].Reference()
				handler.Fields[inputMethodListenerField] = jvm.IntValue(1)
			},
		},
	} {
		t.Run(probe.name, func(t *testing.T) {
			session, field := focusedLWCField(t, runtimeTextFieldComponentClass, 0, "old")
			probe.mutate(field)
			if _, err := session.TextInput(context.Background()); !errors.Is(err, backend.ErrNoTextInput) {
				t.Fatalf("TextInput with malformed %s error = %v", probe.name, err)
			}
		})
	}
}

func TestTextInputCommitRejectsAStaleFocusOrFieldValue(t *testing.T) {
	session, field := focusedLWCField(t, runtimeTextFieldComponentClass, 0, "same")
	input, err := session.TextInput(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	// The visible text is unchanged, but the guest replaced the field value.
	// A snapshot must not overwrite a guest edit it did not observe.
	field.Fields[componentTextField] = jvm.ReferenceValue(session.Client.JVM().NewString("same"))
	if err := input.Commit(context.Background(), "host"); !errors.Is(err, backend.ErrTextInputChanged) {
		t.Fatalf("commit after field replacement error = %v", err)
	}

	input, err = session.TextInput(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	field.Fields[componentMaxLengthField] = jvm.IntValue(8)
	if err := input.Commit(context.Background(), "host"); !errors.Is(err, backend.ErrTextInputChanged) {
		t.Fatalf("commit after maximum-length change error = %v", err)
	}

	input, err = session.TextInput(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	other := newWidget(runtimeTextFieldComponentClass)
	session.Client.runtime.runtimeObjects["lwc:focus"] = other
	if err := input.Commit(context.Background(), "host"); !errors.Is(err, backend.ErrTextInputChanged) {
		t.Fatalf("commit after focus change error = %v", err)
	}
	if got := runtimeComponentText(field); got != "same" {
		t.Fatalf("stale commit changed old field to %q", got)
	}
}

func TestTextInputAppliesLWCConstraintsWithoutTruncation(t *testing.T) {
	for _, probe := range []struct {
		name       string
		constraint int32
		inputMode  string
		password   bool
		valid      string
		invalid    string
	}{
		{name: "any", constraint: 0, inputMode: "text", valid: "한글"},
		{name: "number", constraint: 1, inputMode: "numeric", valid: "-12 3", invalid: "12a"},
		{name: "password", constraint: 2, inputMode: "numeric", password: true, valid: "123", invalid: "12-"},
		{name: "email", constraint: 3, inputMode: "email", valid: "a+b@example.test", invalid: "a b"},
		{name: "url", constraint: 4, inputMode: "url", valid: "https://example.test/a?b=1", invalid: "주소"},
		{name: "phone", constraint: 5, inputMode: "tel", valid: "021234", invalid: "+82"},
	} {
		t.Run(probe.name, func(t *testing.T) {
			session, _ := focusedLWCField(t, runtimeTextFieldComponentClass, probe.constraint, "")
			input, err := session.TextInput(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if input.InputMode != probe.inputMode || input.Password != probe.password {
				t.Fatalf("hints = mode %q, password %t", input.InputMode, input.Password)
			}
			if probe.invalid != "" {
				if err := input.Commit(context.Background(), probe.invalid); !errors.Is(err, backend.ErrInvalidTextInput) {
					t.Fatalf("commit invalid %q error = %v", probe.invalid, err)
				}
			}
			if err := input.Commit(context.Background(), probe.valid); err != nil {
				t.Fatalf("commit valid %q: %v", probe.valid, err)
			}
		})
	}

	session, field := focusedLWCField(t, runtimeTextFieldComponentClass, 0, "")
	field.Fields[componentMaxLengthField] = jvm.IntValue(2)
	input, err := session.TextInput(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := input.Commit(context.Background(), "한🙂"); !errors.Is(err, backend.ErrInvalidTextInput) {
		t.Fatalf("over-limit commit error = %v", err)
	}
	if err := input.Commit(context.Background(), "한글"); err != nil {
		t.Fatalf("two Java-character commit: %v", err)
	}

	session, _ = focusedLWCField(t, runtimeTextFieldComponentClass, 0, "")
	input, err = session.TextInput(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := input.Commit(context.Background(), "two\nlines"); !errors.Is(err, backend.ErrInvalidTextInput) {
		t.Fatalf("single-line newline commit error = %v", err)
	}
}

func TestTextInputReportsOnlyAnActiveLWCEditor(t *testing.T) {
	session, _ := focusedLWCField(t, runtimeTextFieldComponentClass, 0, "")
	session.Client.runtime.runtimeObjects["lwc:focus"] = nil
	if _, err := session.TextInput(context.Background()); !errors.Is(err, backend.ErrNoTextInput) {
		t.Fatalf("no focus error = %v", err)
	}

	vendor := newWidget(runtimeGTextFieldClass)
	session.Client.runtime.runtimeObjects["lwc:focus"] = vendor
	if _, err := session.TextInput(context.Background()); !errors.Is(err, backend.ErrNoTextInput) {
		t.Fatalf("vendor modal field error = %v", err)
	}

	box := newWidget(runtimeTextBoxComponentClass)
	box.Fields[componentTextField] = jvm.ReferenceValue(session.Client.JVM().NewString("lines"))
	box.Fields["constraint:I"] = jvm.IntValue(0)
	session.Client.runtime.runtimeObjects["lwc:focus"] = box
	input, err := session.TextInput(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !input.Multiline {
		t.Fatal("TextBoxComponent was reported as single-line")
	}
	if err := input.Commit(context.Background(), "two\nlines"); err != nil {
		t.Fatalf("multiline commit: %v", err)
	}
}

func TestTextInputRecognizesAnAOTTextFieldSubclass(t *testing.T) {
	session, _ := focusedLWCField(t, runtimeTextFieldComponentClass, 0, "")
	const class = "test/NamedTextField"
	if err := session.Client.JVM().RegisterAOTClass(jvm.AOTClassMetadata{
		Address:     0x40000000,
		Name:        class,
		SuperName:   runtimeTextFieldComponentClass,
		AccessFlags: 0x0021,
	}); err != nil {
		t.Fatal(err)
	}
	field := newWidget(class)
	if _, err := runtimeTextComponentConstructorWithText(session.Client.runtime, session.Client.JVM(), []jvm.Value{
		jvm.ReferenceValue(field), jvm.ReferenceValue(session.Client.JVM().NewString("old")), jvm.IntValue(0),
	}); err != nil {
		t.Fatal(err)
	}
	session.Client.runtime.runtimeObjects["lwc:focus"] = field

	input, err := session.TextInput(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if input.Multiline {
		t.Fatal("TextFieldComponent subclass was reported as multiline")
	}
	if err := input.Commit(context.Background(), "composed"); err != nil {
		t.Fatal(err)
	}
	if got := runtimeComponentText(field); got != "composed" {
		t.Fatalf("subclass text = %q", got)
	}
}
