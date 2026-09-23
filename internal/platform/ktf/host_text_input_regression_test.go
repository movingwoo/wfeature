package ktf

import (
	"errors"
	"testing"
	"unicode/utf16"

	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/jvm"
)

func TestKTFRestoredFocus(t *testing.T) {
	s, field := focusedLWCField(t, runtimeTextFieldComponentClass, 0, "same")
	edit, err := s.TextInput(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	other := newWidget(runtimeTextFieldComponentClass)
	for _, target := range []*jvm.Object{other, field} {
		if _, err := runtimeComponentSetFocus(s.Client.runtime, s.Client.vm, []jvm.Value{jvm.ReferenceValue(target)}); err != nil {
			t.Fatal(err)
		}
	}
	if err := edit.Commit(t.Context(), "replacement"); !errors.Is(err, backend.ErrTextInputChanged) {
		t.Fatalf("restored focus accepted stale edit: err=%v text=%q", err, componentText(field))
	}
	fresh, err := s.TextInput(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if err := fresh.Commit(t.Context(), "fresh"); err != nil || componentText(field) != "fresh" {
		t.Fatalf("fresh focus edit: text=%q err=%v", componentText(field), err)
	}
}

func TestKTFFormRestoredFocusRejectsOldEdit(t *testing.T) {
	s, shell, field := shellTextBox(t)
	form := newWidget(runtimeFormComponentClass)
	callShellInput(t, s, runtimeComponentRemoveAllComponents, shell)
	callShellInput(t, s, runtimeComponentAddComponent, form, jvm.ReferenceValue(field))
	other := newWidget(runtimeTextFieldComponentClass)
	callShellInput(t, s, runtimeComponentAddComponent, form, jvm.ReferenceValue(other))
	callShellInput(t, s, runtimeComponentSetWorkComponent, shell, jvm.ReferenceValue(form))
	callShellInput(t, s, runtimeComponentShown(true), shell)
	callShellInput(t, s, runtimeFormSetFocus, form, jvm.ReferenceValue(field))
	edit, err := s.TextInput(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	callShellInput(t, s, runtimeFormSetFocus, form, jvm.ReferenceValue(other))
	callShellInput(t, s, runtimeFormSetFocus, form, jvm.ReferenceValue(field))
	if err := edit.Commit(t.Context(), "stale"); !errors.Is(err, backend.ErrTextInputChanged) {
		t.Fatalf("restored form focus accepted stale input: %v", err)
	}
	fresh, err := s.TextInput(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if err := fresh.Commit(t.Context(), "fresh"); err != nil {
		t.Fatal(err)
	}
}

func TestKTFExplicitFocusRequiresLiveOwnership(t *testing.T) {
	for _, layout := range []string{"direct", "nested", "work", "subclass"} {
		t.Run(layout, func(t *testing.T) {
			s, field := focusedLWCField(t, runtimeTextFieldComponentClass, 0, "old")
			shell := newWidget(runtimeShellComponentClass)
			if layout == "subclass" {
				shell.ClassName = "test/HiddenShell"
				if err := s.Client.vm.RegisterAOTClass(jvm.AOTClassMetadata{
					Address: 0x40000000, Name: shell.ClassName, SuperName: runtimeShellComponentClass, AccessFlags: 0x0021,
				}); err != nil {
					t.Fatal(err)
				}
			}
			child := field
			if layout == "nested" {
				child = newWidget(runtimeContainerComponentClass)
				callShellInput(t, s, runtimeComponentAddComponent, child, jvm.ReferenceValue(field))
				if _, err := s.TextInput(t.Context()); !errors.Is(err, backend.ErrNoTextInput) {
					t.Fatalf("container without a shown shell: %v", err)
				}
			}
			add := runtimeComponentAddComponent
			if layout == "work" {
				add = runtimeComponentSetWorkComponent
			}
			callShellInput(t, s, add, shell, jvm.ReferenceValue(child))
			if _, err := s.TextInput(t.Context()); !errors.Is(err, backend.ErrNoTextInput) {
				t.Fatalf("never-shown shell offered input: %v", err)
			}
			callShellInput(t, s, runtimeComponentShown(true), shell)
			edit, err := s.TextInput(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			callShellInput(t, s, runtimeComponentShown(false), shell)
			if _, err := s.TextInput(t.Context()); !errors.Is(err, backend.ErrNoTextInput) {
				t.Fatalf("hidden shell offered input: %v", err)
			}
			callShellInput(t, s, runtimeComponentShown(true), shell)
			if err := edit.Commit(t.Context(), "stale"); !errors.Is(err, backend.ErrTextInputChanged) {
				t.Fatalf("hidden then restored shell accepted input: %v", err)
			}
			fresh, err := s.TextInput(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			if err := fresh.Commit(t.Context(), "fresh"); err != nil || componentText(field) != "fresh" {
				t.Fatalf("shown field commit: text=%q err=%v", componentText(field), err)
			}
		})
	}
}

func TestKTFExplicitFocusRejectsDetachedAndRestoredChildren(t *testing.T) {
	for _, change := range []string{"index", "identity", "all", "replace", "work"} {
		t.Run(change, func(t *testing.T) {
			s, shell, field := shellTextBox(t)
			if change == "work" {
				callShellInput(t, s, runtimeComponentRemoveAllComponents, shell)
				callShellInput(t, s, runtimeComponentSetWorkComponent, shell, jvm.ReferenceValue(field))
			}
			callShellInput(t, s, runtimeComponentShown(true), shell)
			callShellInput(t, s, runtimeComponentSetFocus, field)
			edit, err := s.TextInput(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			switch change {
			case "index":
				callShellInput(t, s, runtimeComponentRemoveComponent, shell, jvm.IntValue(0))
			case "identity":
				callShellInput(t, s, runtimeComponentRemoveComponent, shell, jvm.ReferenceValue(field))
			case "all":
				callShellInput(t, s, runtimeComponentRemoveAllComponents, shell)
			case "replace":
				callShellInput(t, s, runtimeComponentSetComponentAt, shell, jvm.IntValue(0), jvm.ReferenceValue(newWidget(runtimeTextFieldComponentClass)))
			case "work":
				callShellInput(t, s, runtimeComponentSetWorkComponent, shell, jvm.ReferenceValue(nil))
			}
			if _, err := s.TextInput(t.Context()); !errors.Is(err, backend.ErrNoTextInput) {
				t.Fatalf("detached field offered input: %v", err)
			}
			switch change {
			case "work":
				callShellInput(t, s, runtimeComponentSetWorkComponent, shell, jvm.ReferenceValue(field))
			case "replace":
				callShellInput(t, s, runtimeComponentSetComponentAt, shell, jvm.IntValue(0), jvm.ReferenceValue(field))
			default:
				callShellInput(t, s, runtimeComponentAddComponentAt, shell, jvm.IntValue(0), jvm.ReferenceValue(field))
			}
			if err := edit.Commit(t.Context(), "stale"); !errors.Is(err, backend.ErrTextInputChanged) {
				t.Fatalf("restored child accepted stale input: %v", err)
			}
			fresh, err := s.TextInput(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			if err := fresh.Commit(t.Context(), "fresh"); err != nil {
				t.Fatal(err)
			}
		})
	}
}
func TestKTFHiddenExplicitFocus(t *testing.T) {
	s, shell, field := shellTextBox(t)
	callShellInput(t, s, runtimeComponentShown(true), shell)
	callShellInput(t, s, runtimeComponentSetFocus, field)
	callShellInput(t, s, runtimeComponentShown(false), shell)
	edit, err := s.TextInput(t.Context())
	if !errors.Is(err, backend.ErrNoTextInput) {
		t.Fatalf("hidden focused field offered: edit=%v err=%v", edit != nil, err)
	}
}
func TestKTFHostThenKeypadUTF16Limit(t *testing.T) {
	for _, route := range []string{"guest keyNotify", "host keypad"} {
		t.Run(route, func(t *testing.T) {
			s, field := focusedLWCField(t, runtimeTextFieldComponentClass, 0, "")
			field.Fields[componentMaxLengthField] = jvm.IntValue(4)
			s.Client.FocusTextComponent(field)
			key := func(ch rune) {
				t.Helper()
				if route == "host keypad" {
					if !s.Client.TypeKey(ch) {
						t.Fatal("key was not consumed")
					}
					return
				}
				callShellInput(t, s, runtimeTextComponentKeyNotify, field, jvm.IntValue(KeyPressed), jvm.IntValue(int32(ch)))
			}
			edit, err := s.TextInput(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			if err := edit.Commit(t.Context(), "AB🙂"); err != nil {
				t.Fatal(err)
			}
			key('2')
			if text := componentText(field); text != "AB🙂" || len(utf16.Encode([]rune(text))) > 4 {
				t.Fatalf("keypad changed a full UTF-16 field: %q", text)
			}
			key('#')
			key('2')
			if text := componentText(field); text != "ABa" {
				t.Fatalf("delete then insert: %q", text)
			}
		})
	}
}

func TestKnownCEncodingLimits(t *testing.T) {
	for _, tc := range []struct {
		name, text string
		valid      bool
	}{
		{"precomposed Korean", "한글", true},
		{"decomposed Korean", "한", false},
		{"supplementary Unicode", "🙂", false},
		{"newline", "first\nsecond", false},
		{"empty", "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := validateCInput(tc.text)
			if (err == nil) != tc.valid {
				t.Fatalf("encoding contract: valid=%v err=%v", tc.valid, err)
			}
		})
	}
}
