package skt

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/movingwoo/wfeature/internal/api/skvm"
	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/jvm"
)

func TestHostTextInputCommitsTheCurrentTextBox(t *testing.T) {
	runtime := startUIFixture(t)
	invokeFixtureVoid(t, runtime, "UIMIDlet", "showTextBox")

	edit, err := runtime.TextInput(context.Background())
	if err != nil {
		t.Fatalf("TextInput() error = %v", err)
	}
	if edit.Text != "abc" || edit.MaxLength != 16 || !edit.Multiline || edit.Password || edit.InputMode != "text" {
		t.Fatalf("TextInput() = %+v", edit)
	}
	if err := edit.Commit(context.Background(), "한글 입력"); err != nil {
		t.Fatalf("Commit() error = %v", err)
	}
	if text := fixtureString(t, runtime, "UIMIDlet", "typedText"); text != "한글 입력" {
		t.Fatalf("typedText() = %q", text)
	}
}

func TestHostTextInputRejectsStaleTextBoxSnapshots(t *testing.T) {
	runtime := startUIFixture(t)
	invokeFixtureVoid(t, runtime, "UIMIDlet", "showTextBox")

	changedValue, err := runtime.TextInput(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	currentTextBox := runtime.currentDisplayable
	if _, err := runtime.VM.InvokeVirtual(currentTextBox, "setString", "(Ljava/lang/String;)V",
		jvm.ReferenceValue(runtime.VM.NewString("guest edit"))); err != nil {
		t.Fatalf("setString() error = %v", err)
	}
	if err := runtime.RunPending(); err != nil {
		t.Fatalf("RunPending() error = %v", err)
	}
	if err := changedValue.Commit(context.Background(), "host edit"); !errors.Is(err, backend.ErrTextInputChanged) {
		t.Fatalf("Commit() after guest edit error = %v", err)
	}

	current, err := runtime.TextInput(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	invokeFixtureVoid(t, runtime, "UIMIDlet", "showForm")
	if err := current.Commit(context.Background(), "host edit"); !errors.Is(err, backend.ErrTextInputChanged) {
		t.Fatalf("Commit() after screen change error = %v", err)
	}
}

func TestHostTextInputRejectsCommandMenuOverlay(t *testing.T) {
	runtime := startUIFixture(t)
	invokeFixtureVoid(t, runtime, "UIMIDlet", "showTextBox")

	setCurrentMenuOpenForTest(t, runtime, true)
	if _, err := runtime.TextInput(context.Background()); !errors.Is(err, backend.ErrNoTextInput) {
		t.Fatalf("TextInput() below command menu error = %v", err)
	}

	setCurrentMenuOpenForTest(t, runtime, false)
	edit, err := runtime.TextInput(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	setCurrentMenuOpenForTest(t, runtime, true)
	if err := edit.Commit(context.Background(), "host edit"); !errors.Is(err, backend.ErrTextInputChanged) {
		t.Fatalf("Commit() after command menu opened error = %v", err)
	}
}

func TestHostTextInputRejectsStaleConstraintMetadata(t *testing.T) {
	runtime := startUIFixture(t)
	invokeFixtureVoid(t, runtime, "UIMIDlet", "showTextBox")
	box := runtime.currentDisplayable

	constraintEdit, err := runtime.TextInput(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.VM.InvokeVirtual(box, "setConstraints", "(I)V", jvm.IntValue(2)); err != nil {
		t.Fatalf("setConstraints(NUMERIC) error = %v", err)
	}
	if err := constraintEdit.Commit(context.Background(), "12"); !errors.Is(err, backend.ErrTextInputChanged) {
		t.Fatalf("Commit() after constraint change error = %v", err)
	}

	maxSizeEdit, err := runtime.TextInput(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.VM.InvokeVirtual(box, "setMaxSize", "(I)I", jvm.IntValue(8)); err != nil {
		t.Fatalf("setMaxSize(8) error = %v", err)
	}
	if err := maxSizeEdit.Commit(context.Background(), "12"); !errors.Is(err, backend.ErrTextInputChanged) {
		t.Fatalf("Commit() after maxSize change error = %v", err)
	}
}

func TestHostTextInputSerializesGuestSetterAndPaint(t *testing.T) {
	runtime := startUIFixture(t)
	invokeFixtureVoid(t, runtime, "UIMIDlet", "showTextBox")
	box := runtime.currentDisplayable
	setter := runtime.setTextString(screenTextBox)
	values := []jvm.Value{
		jvm.ReferenceValue(box),
		jvm.ReferenceValue(runtime.VM.NewString("guest value")),
	}

	var group sync.WaitGroup
	setterErrors := make(chan error, 1)
	group.Add(1)
	go func() {
		defer group.Done()
		for index := 0; index < 200; index++ {
			if _, err := setter(runtime.VM, values); err != nil {
				setterErrors <- err
				return
			}
		}
	}()
	for index := 0; index < 200; index++ {
		edit, err := runtime.TextInput(context.Background())
		if err != nil {
			t.Fatalf("TextInput() error = %v", err)
		}
		if err := edit.Commit(context.Background(), "host value"); err != nil &&
			!errors.Is(err, backend.ErrTextInputChanged) {
			t.Fatalf("Commit() error = %v", err)
		}
	}
	group.Wait()
	select {
	case err := <-setterErrors:
		t.Fatalf("guest setString() error = %v", err)
	default:
	}
	if err := runtime.RunPending(); err != nil {
		t.Fatalf("RunPending() error = %v", err)
	}
}

func TestHostTextInputEnforcesMIDPConstraints(t *testing.T) {
	runtime := startUIFixture(t)
	invokeFixtureVoid(t, runtime, "UIMIDlet", "showTextBox")
	box := runtime.currentDisplayable
	if _, err := runtime.VM.InvokeVirtual(box, "setConstraints", "(I)V", jvm.IntValue(2)); err != nil {
		t.Fatalf("setConstraints(NUMERIC) error = %v", err)
	}
	if err := runtime.RunPending(); err != nil {
		t.Fatalf("RunPending() error = %v", err)
	}

	edit, err := runtime.TextInput(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if edit.InputMode != "numeric" {
		t.Fatalf("InputMode = %q, want numeric", edit.InputMode)
	}
	if err := edit.Commit(context.Background(), "12가"); !errors.Is(err, backend.ErrInvalidTextInput) {
		t.Fatalf("Commit(invalid numeric) error = %v", err)
	}
	if err := edit.Commit(context.Background(), "-42"); err != nil {
		t.Fatalf("Commit(valid numeric) error = %v", err)
	}
	if _, err := runtime.VM.InvokeVirtual(box, "setMaxSize", "(I)I", jvm.IntValue(1)); err != nil {
		t.Fatalf("setMaxSize(1) error = %v", err)
	}
	if _, err := runtime.VM.InvokeVirtual(box, "setConstraints", "(I)V", jvm.IntValue(0)); err != nil {
		t.Fatalf("setConstraints(ANY) error = %v", err)
	}
	oneUnit, err := runtime.TextInput(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := oneUnit.Commit(context.Background(), "😀"); !errors.Is(err, backend.ErrInvalidTextInput) {
		t.Fatalf("Commit(supplementary character into maxSize 1) error = %v", err)
	}
	if err := oneUnit.Commit(context.Background(), "가"); err != nil {
		t.Fatalf("Commit(BMP character into maxSize 1) error = %v", err)
	}

	if _, err := runtime.VM.InvokeVirtual(box, "setConstraints", "(I)V", jvm.IntValue(textFieldUneditable)); err != nil {
		t.Fatalf("setConstraints(UNEDITABLE) error = %v", err)
	}
	if _, err := runtime.TextInput(context.Background()); !errors.Is(err, backend.ErrNoTextInput) {
		t.Fatalf("TextInput() for UNEDITABLE field error = %v", err)
	}
}

func TestHostTextInputCommitsFocusedFormFieldAndNotifies(t *testing.T) {
	runtime := startUIFixture(t)
	invokeFixtureVoid(t, runtime, "UIMIDlet", "showForm")
	pressKey(t, runtime, KeyCodeDown)
	pressKey(t, runtime, KeyCodeDown)

	edit, err := runtime.TextInput(context.Background())
	if err != nil {
		t.Fatalf("TextInput() error = %v", err)
	}
	if edit.Text != "홍길동" || edit.MaxLength != 10 || edit.Multiline {
		t.Fatalf("TextInput() = %+v", edit)
	}
	if err := edit.Commit(context.Background(), "새 이름"); err != nil {
		t.Fatalf("Commit() error = %v", err)
	}
	if changes := invokeFixtureInt(t, runtime, "UIMIDlet", "itemChanges"); changes != 1 {
		t.Fatalf("itemChanges() = %d, want one Host edit callback", changes)
	}

	stale, err := runtime.TextInput(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	pressKey(t, runtime, KeyCodeUp)
	if err := stale.Commit(context.Background(), "다른 이름"); !errors.Is(err, backend.ErrTextInputChanged) {
		t.Fatalf("Commit() after focus change error = %v", err)
	}
}

func TestHostTextInputCommitsOnlyTheFocusedVisibleXTextField(t *testing.T) {
	runtime := startUIFixture(t)
	invokeFixtureVoid(t, runtime, "UIMIDlet", "showBuffer")
	owner := runtime.currentDisplayable
	field := newXTextField(t, runtime, owner, "ab", 6, 0)
	setXTextFieldFocusForTest(t, runtime, field, true)

	edit, err := runtime.TextInput(context.Background())
	if err != nil {
		t.Fatalf("TextInput() error = %v", err)
	}
	if edit.Text != "ab" || edit.MaxLength != 6 || edit.Multiline {
		t.Fatalf("TextInput() = %+v", edit)
	}
	if err := edit.Commit(context.Background(), "한글"); err != nil {
		t.Fatalf("Commit() error = %v", err)
	}
	if data, _ := nativeXTextField(field); string(data.text) != "한글" {
		t.Fatalf("XTextField text = %q", data.text)
	}

	stale, err := runtime.TextInput(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	replacement := newXTextField(t, runtime, owner, "next", 6, 0)
	setXTextFieldFocusForTest(t, runtime, replacement, true)
	if err := stale.Commit(context.Background(), "wrong field"); !errors.Is(err, backend.ErrTextInputChanged) {
		t.Fatalf("Commit() after vendor focus change error = %v", err)
	}
	if data, _ := nativeXTextField(field); string(data.text) != "한글" {
		t.Fatalf("stale commit changed old XTextField to %q", data.text)
	}
}

func TestHostTextInputConstraintMetadataAndSyntax(t *testing.T) {
	for _, probe := range []struct {
		name       string
		constraint int32
		mode       string
		valid      []string
		invalid    []string
	}{
		{name: "any", constraint: 0, mode: "text", valid: []string{"", "한글"}},
		{name: "email", constraint: 1, mode: "email", valid: []string{"name@example.invalid"}},
		{name: "numeric", constraint: 2, mode: "numeric", valid: []string{"", "0", "-12"}, invalid: []string{"-", "1.2", "１２"}},
		{name: "phone", constraint: 3, mode: "tel", valid: []string{"", "+82101234", "12#*"}, invalid: []string{"12-34", "phone"}},
		{name: "url", constraint: 4, mode: "url", valid: []string{"https://example.invalid/경로"}},
		{name: "decimal", constraint: 5, mode: "decimal", valid: []string{"", "-12", ".5", "1."}, invalid: []string{"-", ".", "1.2.3", "1e2"}},
	} {
		t.Run(probe.name, func(t *testing.T) {
			if mode := hostInputMode(probe.constraint); mode != probe.mode {
				t.Fatalf("hostInputMode(%d) = %q, want %q", probe.constraint, mode, probe.mode)
			}
			for _, text := range probe.valid {
				if !validHostText(text, 64, probe.constraint, false) {
					t.Errorf("validHostText(%q) = false", text)
				}
			}
			for _, text := range probe.invalid {
				if validHostText(text, 64, probe.constraint, false) {
					t.Errorf("validHostText(%q) = true", text)
				}
			}
		})
	}
	if hostInputMode(5|midpTextFieldPassword) != "decimal" {
		t.Fatal("PASSWORD modifier changed the base input mode")
	}
	if !validHostText("가", 1, 0, false) || validHostText("😀", 1, 0, false) || !validHostText("😀", 2, 0, false) {
		t.Fatal("maxSize did not count Java UTF-16 char units")
	}
}

func newXTextField(t *testing.T, runtime *Runtime, owner *jvm.Object, text string, maxSize, constraints int32) *jvm.Object {
	t.Helper()
	field, err := runtime.VM.NewObject(skvm.XTextFieldClass,
		"(Ljava/lang/String;IILjavax/microedition/lcdui/Canvas;)V",
		jvm.ReferenceValue(runtime.VM.NewString(text)), jvm.IntValue(maxSize), jvm.IntValue(constraints), jvm.ReferenceValue(owner))
	if err != nil {
		t.Fatalf("new XTextField error = %v", err)
	}
	return field
}

func setXTextFieldFocusForTest(t *testing.T, runtime *Runtime, field *jvm.Object, focused bool) {
	t.Helper()
	value := int32(0)
	if focused {
		value = 1
	}
	if _, err := runtime.VM.InvokeVirtual(field, "setFocus", "(Z)V", jvm.IntValue(value)); err != nil {
		t.Fatalf("setFocus(%t) error = %v", focused, err)
	}
}

func setCurrentMenuOpenForTest(t *testing.T, runtime *Runtime, open bool) {
	t.Helper()
	runtime.displayMu.RLock()
	current := runtime.currentDisplayable
	runtime.displayMu.RUnlock()
	state := runtime.lcdui()
	state.mu.Lock()
	defer state.mu.Unlock()
	display := state.displayables[current]
	if display == nil {
		t.Fatal("current Displayable has no LCDUI state")
	}
	display.menuOpen = open
}
