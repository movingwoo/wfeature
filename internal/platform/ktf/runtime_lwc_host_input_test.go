package ktf

import (
	"context"
	"errors"
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

	var notifiedText string
	var notifiedLength, notifiedMode int32
	listener := newWidget("test/InputListener")
	if err := session.Client.JVM().RegisterNative("test/InputListener", "notifyTextChanged", "([CII)V", func(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
		array, err := arguments[1].Reference()
		if err != nil {
			return jvm.VoidValue(), err
		}
		_, values, err := jvm.ArraySnapshot(array)
		if err != nil {
			return jvm.VoidValue(), err
		}
		units := make([]uint16, len(values))
		for index, value := range values {
			unit, intErr := value.Int32()
			if intErr != nil {
				return jvm.VoidValue(), intErr
			}
			units[index] = uint16(unit)
		}
		notifiedText = string(utf16.Decode(units))
		notifiedLength, _ = arguments[2].Int32()
		notifiedMode, _ = arguments[3].Int32()
		if got := runtimeComponentText(field); got != "한글🙂" {
			t.Errorf("listener observed component text %q, want committed text", got)
		}
		return jvm.VoidValue(), nil
	}); err != nil {
		t.Fatal(err)
	}
	handler, err := field.Fields[componentInputHandlerField].Reference()
	if err != nil {
		t.Fatal(err)
	}
	handler.Fields[inputMethodListenerField] = jvm.ReferenceValue(listener)

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
	if notifiedText != "한글🙂" || notifiedLength != 4 || notifiedMode != 0 {
		t.Fatalf("notification = %q, length %d, mode %d", notifiedText, notifiedLength, notifiedMode)
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
