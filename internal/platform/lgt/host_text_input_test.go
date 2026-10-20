package lgt

import (
	"context"
	"errors"
	"testing"

	"github.com/movingwoo/wfeature/internal/backend"
)

func focusedJavaTextFixture(
	t *testing.T, kind javaWidgetKind, constraint int32, text string,
) (*Session, uint32, uint32) {
	t.Helper()
	client := fixtureClient(t)
	shell, err := newTestObject(t, client, "org/kwis/msp/lwc/ShellComponent")
	if err != nil {
		t.Fatal(err)
	}
	class := "org/kwis/msp/lwc/TextFieldComponent"
	if kind == javaWidgetTextBox {
		class = "org/kwis/msp/lwc/TextBoxComponent"
	}
	field, err := newTestObject(t, client, class)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := javaShellComponentConstructor(client, t.Context(), client.thread,
		[]uint32{shell, 0, 0, 16, 8}); err != nil {
		t.Fatal(err)
	}
	if _, err := javaTextComponentConstructor(kind)(client, t.Context(), client.thread,
		[]uint32{field, newTestString(t, client, text), uint32(constraint)}); err != nil {
		t.Fatal(err)
	}
	if _, err := javaComponentAddChild(client, t.Context(), client.thread, []uint32{shell, field}); err != nil {
		t.Fatal(err)
	}
	if _, err := javaComponentShow(true)(client, t.Context(), client.thread, []uint32{shell}); err != nil {
		t.Fatal(err)
	}
	if _, err := javaComponentSetFocus(client, t.Context(), client.thread, []uint32{field}); err != nil {
		t.Fatal(err)
	}
	return &Session{client: client}, shell, field
}

func TestTextInputCommitsAWholeHostCompositionToAVisibleFocusedField(t *testing.T) {
	session, _, field := focusedJavaTextFixture(t, javaWidgetTextField, javaTextConstraintAny, "old")
	state := session.client.javaWidgetState(field)
	if _, err := javaTextComponentSetMaxLength(session.client, t.Context(), session.client.thread,
		[]uint32{field, 4}); err != nil {
		t.Fatal(err)
	}

	input, err := session.TextInput(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if input.Text != "old" || input.MaxLength != 4 || input.Multiline || input.Password || input.InputMode != "text" {
		t.Fatalf("TextInput() = %+v", input)
	}
	// Text the handset's encoding has no code for is refused: a title's own
	// font indexes its glyphs by the bytes of the text.
	for _, refused := range []string{"\U0001F642", "\uB620"} {
		if err := input.Commit(context.Background(), refused); !errors.Is(err, backend.ErrInvalidTextInput) {
			t.Fatalf("commit %q error = %v, want a refusal", refused, err)
		}
	}
	if err := input.Commit(context.Background(), "한글AB"); err != nil {
		t.Fatal(err)
	}
	if state.text != "한글AB" {
		t.Fatalf("field text = %q, want committed composition", state.text)
	}

	boxSession, _, _ := focusedJavaTextFixture(t, javaWidgetTextBox, javaTextConstraintPassword, "12")
	box, err := boxSession.TextInput(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !box.Multiline || !box.Password || box.InputMode != "numeric" {
		t.Fatalf("TextBox input = %+v", box)
	}
}

func TestTextInputRejectsInvalidConstraintsWithoutTruncating(t *testing.T) {
	for _, probe := range []struct {
		name       string
		constraint int32
		valid      string
		invalid    string
	}{
		{name: "number", constraint: javaTextConstraintNumber, valid: "-12 3", invalid: "12a"},
		{name: "password", constraint: javaTextConstraintPassword, valid: "123", invalid: "12-"},
		{name: "email", constraint: javaTextConstraintEmailAddress, valid: "a+b@example.test", invalid: "a b"},
		{name: "url", constraint: javaTextConstraintURL, valid: "https://example.test/a?b=1", invalid: "주소"},
		{name: "phone", constraint: javaTextConstraintPhoneNumber, valid: "021234", invalid: "+82"},
	} {
		t.Run(probe.name, func(t *testing.T) {
			session, _, _ := focusedJavaTextFixture(t, javaWidgetTextField, probe.constraint, "")
			input, err := session.TextInput(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if err := input.Commit(context.Background(), probe.invalid); !errors.Is(err, backend.ErrInvalidTextInput) {
				t.Fatalf("invalid commit error = %v", err)
			}
			if err := input.Commit(context.Background(), probe.valid); err != nil {
				t.Fatalf("valid commit: %v", err)
			}
		})
	}

	session, _, field := focusedJavaTextFixture(t, javaWidgetTextField, javaTextConstraintAny, "")
	state := session.client.javaWidgetState(field)
	state.maxLength = 2
	input, err := session.TextInput(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := input.Commit(context.Background(), "한🙂"); !errors.Is(err, backend.ErrInvalidTextInput) {
		t.Fatalf("over-limit composition error = %v", err)
	}
	if state.text != "" {
		t.Fatalf("invalid composition was truncated to %q", state.text)
	}
	if err := input.Commit(context.Background(), "한글"); err != nil {
		t.Fatalf("two-unit composition: %v", err)
	}

	session, _, _ = focusedJavaTextFixture(t, javaWidgetTextField, javaTextConstraintAny, "")
	input, err = session.TextInput(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := input.Commit(context.Background(), "two\nlines"); !errors.Is(err, backend.ErrInvalidTextInput) {
		t.Fatalf("single-line newline error = %v", err)
	}
}

// The field the player sees takes the text whether or not the title gave it
// focus: a focused field comes first, and without one the only field under a
// shown shell is taken. A hidden shell, and a field whose handler has a
// listener, are still refused.
func TestTextInputTakesTheShownFieldAndRejectsAListener(t *testing.T) {
	session, shell, field := focusedJavaTextFixture(t, javaWidgetTextField, javaTextConstraintAny, "old")
	runtime := session.client.javaRun
	runtime.focusedWidget = 0
	edit, err := session.TextInput(context.Background())
	if err != nil || edit.Text != "old" {
		t.Fatalf("unfocused field under a shown shell: %+v %v", edit, err)
	}
	session.client.javaWidgetState(shell).shown = false
	if _, err := session.TextInput(context.Background()); !errors.Is(err, backend.ErrNoTextInput) {
		t.Fatalf("hidden shell error = %v", err)
	}
	runtime.focusedWidget = field
	if _, err := session.TextInput(context.Background()); !errors.Is(err, backend.ErrNoTextInput) {
		t.Fatalf("focused field in a hidden shell error = %v", err)
	}
	session.client.javaWidgetState(shell).shown = true
	handler, err := newTestObject(t, session.client, javaInputMethodHandlerClass)
	if err != nil {
		t.Fatal(err)
	}
	session.client.javaWidgetState(field).inputHandler = handler
	session.client.javaWidgetState(handler).listener = 0x1234
	if _, err := session.TextInput(context.Background()); !errors.Is(err, backend.ErrNoTextInput) {
		t.Fatalf("field with unsupported listener error = %v", err)
	}

	cleanSession, _, cleanField := focusedJavaTextFixture(t, javaWidgetTextField, javaTextConstraintAny, "old")
	edit, err = cleanSession.TextInput(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	cleanHandler, err := newTestObject(t, cleanSession.client, javaInputMethodHandlerClass)
	if err != nil {
		t.Fatal(err)
	}
	cleanSession.client.javaWidgetState(cleanField).inputHandler = cleanHandler
	if _, err := javaInputMethodSetListener(cleanSession.client, t.Context(), cleanSession.client.thread,
		[]uint32{cleanHandler, 0x5678}); err != nil {
		t.Fatal(err)
	}
	if err := edit.Commit(context.Background(), "new"); !errors.Is(err, backend.ErrTextInputChanged) {
		t.Fatalf("commit after listener installation error = %v", err)
	}
}

func TestTextInputCommitRejectsRestoredLifecycleAndGuestEdits(t *testing.T) {
	session, shell, field := focusedJavaTextFixture(t, javaWidgetTextField, javaTextConstraintAny, "same")
	input, err := session.TextInput(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := javaComponentShow(false)(session.client, t.Context(), session.client.thread,
		[]uint32{shell}); err != nil {
		t.Fatal(err)
	}
	if _, err := javaComponentShow(true)(session.client, t.Context(), session.client.thread,
		[]uint32{shell}); err != nil {
		t.Fatal(err)
	}
	if err := input.Commit(context.Background(), "host"); !errors.Is(err, backend.ErrTextInputChanged) {
		t.Fatalf("commit after hide and reshow error = %v", err)
	}

	input, err = session.TextInput(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	other, err := newTestObject(t, session.client, "org/kwis/msp/lwc/TextFieldComponent")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := javaComponentSetFocus(session.client, t.Context(), session.client.thread, []uint32{other}); err != nil {
		t.Fatal(err)
	}
	if _, err := javaComponentSetFocus(session.client, t.Context(), session.client.thread, []uint32{field}); err != nil {
		t.Fatal(err)
	}
	if err := input.Commit(context.Background(), "host"); !errors.Is(err, backend.ErrTextInputChanged) {
		t.Fatalf("commit after restored focus error = %v", err)
	}

	input, err = session.TextInput(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	changed := newTestString(t, session.client, "changed")
	original := newTestString(t, session.client, "same")
	if _, err := javaTextComponentSetString(session.client, t.Context(), session.client.thread,
		[]uint32{field, changed}); err != nil {
		t.Fatal(err)
	}
	if _, err := javaTextComponentSetString(session.client, t.Context(), session.client.thread,
		[]uint32{field, original}); err != nil {
		t.Fatal(err)
	}
	if err := input.Commit(context.Background(), "host"); !errors.Is(err, backend.ErrTextInputChanged) {
		t.Fatalf("commit after restored guest text error = %v", err)
	}

	input, err = session.TextInput(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	secondShell, err := newTestObject(t, session.client, "org/kwis/msp/lwc/ShellComponent")
	if err != nil {
		t.Fatal(err)
	}
	session.client.setJavaWidgetParent(field, secondShell)
	session.client.setJavaWidgetParent(field, shell)
	if err := input.Commit(context.Background(), "host"); !errors.Is(err, backend.ErrTextInputChanged) {
		t.Fatalf("commit after restored parent error = %v", err)
	}
}

func newJavaTextField(t *testing.T, client *Client, text string) uint32 {
	t.Helper()
	field, err := newTestObject(t, client, "org/kwis/msp/lwc/TextFieldComponent")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := javaTextComponentConstructor(javaWidgetTextField)(client, t.Context(), client.thread,
		[]uint32{field, newTestString(t, client, text), uint32(javaTextConstraintAny)}); err != nil {
		t.Fatal(err)
	}
	return field
}

// Without focus the Host takes a field only when the screen leaves one
// choice: the only field under a shown shell, or, when no shell shows one,
// the only field that never joined a shell, which the title draws itself.
func TestTextInputChoosesAnUnfocusedFieldOnlyWhenItIsTheOnlyOne(t *testing.T) {
	session, shell, field := focusedJavaTextFixture(t, javaWidgetTextField, javaTextConstraintAny, "first")
	client := session.client
	client.javaRun.focusedWidget = 0
	target := func() string {
		t.Helper()
		edit, err := session.TextInput(context.Background())
		if errors.Is(err, backend.ErrNoTextInput) {
			return ""
		}
		if err != nil {
			t.Fatal(err)
		}
		return edit.Text
	}
	loose := newJavaTextField(t, client, "loose")
	if got := target(); got != "first" {
		t.Fatalf("a field in no shell outranked the shown one: %q", got)
	}
	second := newJavaTextField(t, client, "second")
	if _, err := javaComponentAddChild(client, t.Context(), client.thread, []uint32{shell, second}); err != nil {
		t.Fatal(err)
	}
	if got := target(); got != "" {
		t.Fatalf("two shown fields without focus offered %q", got)
	}
	if _, err := javaComponentSetFocus(client, t.Context(), client.thread, []uint32{second}); err != nil {
		t.Fatal(err)
	}
	if got := target(); got != "second" {
		t.Fatalf("focus did not decide between shown fields: %q", got)
	}
	// A focused field the title draws itself is taken like a shown one.
	if _, err := javaComponentSetFocus(client, t.Context(), client.thread, []uint32{loose}); err != nil {
		t.Fatal(err)
	}
	if got := target(); got != "loose" {
		t.Fatalf("focused field in no shell: %q", got)
	}
	client.javaRun.focusedWidget = 0
	if _, err := javaComponentShow(false)(client, t.Context(), client.thread, []uint32{shell}); err != nil {
		t.Fatal(err)
	}
	paintJavaTextReads(t, client, loose)
	if got := target(); got != "loose" {
		t.Fatalf("the only field in no shell, with the shell hidden: %q", got)
	}
	other := newJavaTextField(t, client, "other")
	paintJavaTextReads(t, client, loose, other)
	if got := target(); got != "" {
		t.Fatalf("two fields in no shell offered %q", got)
	}
	_ = field
}

// paintJavaTextReads stands in for a paint of the pushed card that reads the
// text of objects, the way a title drawing its own field does.
func paintJavaTextReads(t *testing.T, client *Client, objects ...uint32) {
	t.Helper()
	runtime := client.javaRuntimeState()
	if runtime.card == 0 {
		runtime.card = 0x7000
	}
	runtime.beginTextReads()
	for _, object := range objects {
		if _, err := javaTextComponentGetString(client, t.Context(), client.thread, []uint32{object}); err != nil {
			t.Fatal(err)
		}
	}
	runtime.endTextReads(runtime.card, true)
}

// A field in no shell is offered on the evidence that the card on the screen
// read it while it last painted text: one the title built and never drew, or
// one it dropped for a new one that the collector has not reached yet, is not.
// A frame that reads no text keeps the evidence, another card's paint does not
// carry it, and a collected field leaves it.
func TestTextInputOffersAFieldInNoShellOnlyWhenThePaintReadsIt(t *testing.T) {
	session, shell, _ := focusedJavaTextFixture(t, javaWidgetTextField, javaTextConstraintAny, "shown")
	client := session.client
	runtime := client.javaRun
	runtime.focusedWidget = 0
	if _, err := javaComponentShow(false)(client, t.Context(), client.thread, []uint32{shell}); err != nil {
		t.Fatal(err)
	}
	target := func() string {
		t.Helper()
		edit, err := session.TextInput(context.Background())
		if errors.Is(err, backend.ErrNoTextInput) {
			return ""
		}
		if err != nil {
			t.Fatal(err)
		}
		return edit.Text
	}
	helper := newJavaTextField(t, client, "helper")
	if got := target(); got != "" {
		t.Fatalf("a field never drawn was offered: %q", got)
	}
	old := newJavaTextField(t, client, "old name")
	paintJavaTextReads(t, client, old)
	if got := target(); got != "old name" {
		t.Fatalf("the field the paint read: %q", got)
	}
	// The dialog opens again with a new field; the old one is still held.
	name := newJavaTextField(t, client, "new name")
	paintJavaTextReads(t, client, name)
	if got := target(); got != "new name" {
		t.Fatalf("the field the paint read now: %q", got)
	}
	paintJavaTextReads(t, client)
	if got := target(); got != "new name" {
		t.Fatalf("a paint that read no text dropped the field: %q", got)
	}
	card := runtime.card
	runtime.card = card + 0x100
	if got := target(); got != "" {
		t.Fatalf("a field read by a card no longer on the screen was offered: %q", got)
	}
	runtime.card = card
	runtime.forgetTextReads([]uint32{name})
	if got := target(); got != "" {
		t.Fatalf("a collected field was offered: %q", got)
	}
	_ = helper
}

// A focused field on the screen that cannot take a Host edit keeps the edit
// from any field beside it; a focus left behind a hidden shell does not.
func TestTextInputDoesNotPassAFocusedFieldOverForAnother(t *testing.T) {
	session, shell, field := focusedJavaTextFixture(t, javaWidgetTextField, javaTextConstraintAny, "focused")
	client := session.client
	beside := newJavaTextField(t, client, "beside")
	if _, err := javaComponentAddChild(client, t.Context(), client.thread, []uint32{shell, beside}); err != nil {
		t.Fatal(err)
	}
	handler, err := newTestObject(t, client, javaInputMethodHandlerClass)
	if err != nil {
		t.Fatal(err)
	}
	client.javaWidgetState(field).inputHandler = handler
	client.javaWidgetState(handler).listener = 0x1234
	if edit, err := session.TextInput(context.Background()); !errors.Is(err, backend.ErrNoTextInput) {
		t.Fatalf("a field beside a focused one that refuses was offered: %+v %v", edit, err)
	}
	loose := newJavaTextField(t, client, "loose")
	paintJavaTextReads(t, client, loose)
	if edit, err := session.TextInput(context.Background()); !errors.Is(err, backend.ErrNoTextInput) {
		t.Fatalf("a drawn field was offered in place of the focused one: %+v %v", edit, err)
	}
	// Behind a hidden shell the focus says nothing about the screen.
	if _, err := javaComponentShow(false)(client, t.Context(), client.thread, []uint32{shell}); err != nil {
		t.Fatal(err)
	}
	edit, err := session.TextInput(context.Background())
	if err != nil || edit.Text != "loose" {
		t.Fatalf("with the focused field hidden: %+v %v", edit, err)
	}
}

// A title can repeat its notifications and select its input mode on every
// frame. None of that changes the field, and a commit made meanwhile lands.
func TestTextInputCommitSurvivesRepeatedNotificationsAndModeSelection(t *testing.T) {
	session, shell, field := focusedJavaTextFixture(t, javaWidgetTextField, javaTextConstraintAny, "old")
	client := session.client
	edit, err := session.TextInput(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, object := range []uint32{shell, field} {
		if _, err := javaComponentShowNotify(client, t.Context(), client.thread, []uint32{object, 1}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := javaComponentFocusNotify(client, t.Context(), client.thread, []uint32{field, 1}); err != nil {
		t.Fatal(err)
	}
	if handler := client.javaWidgetState(field).inputHandler; handler != 0 {
		if _, err := javaInputMethodSetMode(client, t.Context(), client.thread, []uint32{handler, 2}); err != nil {
			t.Fatal(err)
		}
	}
	if err := edit.Commit(context.Background(), "new"); err != nil {
		t.Fatalf("commit after repeated notifications: %v", err)
	}
	if got := client.javaWidgetState(field).text; got != "new" {
		t.Fatalf("field holds %q", got)
	}
}

func cTextInputFixture(t *testing.T, mode uint32) *Session {
	t.Helper()
	session, err := StartSession(t.Context(), fixtureArchive(t), SessionOptions{
		Width: 16, Height: 8, MaxSteps: 1 << 20,
	})
	if err != nil {
		t.Fatal(err)
	}
	if applied := callSlot(t, session.client, slotIMSetCurrentMode, mode); applied != 1 {
		t.Fatalf("setting input mode %d answered %d", mode, applied)
	}
	// A key the player pressed has reached the widget, which passes every
	// key to the automaton, as a name widget does.
	session.client.SendEvent(EventKeyPressed, '#', 0)
	if err := session.client.deliverEvents(t.Context()); err != nil {
		t.Fatal(err)
	}
	return session
}

func TestCTextInputPassesACompleteHostCompositionThroughTheGuestWidget(t *testing.T) {
	for mode := uint32(0); mode < uint32(len(inputModes)); mode++ {
		t.Run(inputModes[mode], func(t *testing.T) {
			session := cTextInputFixture(t, mode)
			input, err := session.TextInput(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			if input.Text != "" || !input.Append || input.MaxLength != maxCTextInputLength ||
				input.Multiline || input.Password || input.InputMode != "text" {
				t.Fatalf("TextInput() = %+v", input)
			}

			want := "한글A1"
			if err := input.Commit(t.Context(), want); err != nil {
				t.Fatal(err)
			}
			length, err := session.client.readWord(fixtureInputSizes)
			if err != nil {
				t.Fatal(err)
			}
			encoded, err := validateCTextInput(want)
			if err != nil {
				t.Fatal(err)
			}
			got := make([]byte, length)
			if err := session.client.core.Memory().Read(fixtureInputCompleted, got); err != nil {
				t.Fatal(err)
			}
			if string(got) != string(encoded) || decodeEUCKR(got) != want {
				t.Fatalf("completed text = %x (%q), want %x (%q)", got, decodeEUCKR(got), encoded, want)
			}
			if composing, err := session.client.readWord(fixtureInputSizes + 4); err != nil || composing != 0 {
				t.Fatalf("composing length = %d, want 0: %v", composing, err)
			}

		})
	}
}

func TestCTextInputValidatesEncodingAndCapacityBeforeInsertion(t *testing.T) {
	session := cTextInputFixture(t, 3)
	input, err := session.TextInput(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if input.InputMode != "text" {
		t.Fatalf("input mode = %q, want text for host composition", input.InputMode)
	}
	for _, invalid := range []string{"12\n", "🙂"} {
		if err := input.Commit(t.Context(), invalid); !errors.Is(err, backend.ErrInvalidTextInput) {
			t.Fatalf("commit %q error = %v", invalid, err)
		}
	}
	if err := input.Commit(t.Context(), "1203"); err != nil {
		t.Fatalf("numeric commit: %v", err)
	}

	session = cTextInputFixture(t, 2)
	input, err = session.TextInput(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if err := input.Commit(t.Context(), "🙂"); !errors.Is(err, backend.ErrInvalidTextInput) {
		t.Fatalf("unencodable commit error = %v", err)
	}
	// The fixture widget supplies a 16-byte buffer. A value that needs more
	// arrives over successive carriers in whole characters, the buffer full
	// each time but the last: 15 bytes, then the remaining character.
	calls := session.client.cTextInput.calls
	if err := input.Commit(t.Context(), "123456789012345가"); err != nil {
		t.Fatalf("over-capacity commit error = %v", err)
	}
	if got := session.client.cTextInput.calls - calls; got != 2 {
		t.Fatalf("carriers taken = %d, want 2", got)
	}
	if got, err := session.client.readCString(fixtureInputCompleted); err != nil || decodeEUCKR([]byte(got)) != "가" {
		t.Fatalf("last completion = %q: %v", got, err)
	}
}

func TestCTextInputRejectsAChangedInputMethodSnapshot(t *testing.T) {
	session := cTextInputFixture(t, 2)
	input, err := session.TextInput(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	callSlot(t, session.client, slotIMSetCurrentMode, 2)
	if err := input.Commit(t.Context(), "한글"); !errors.Is(err, backend.ErrTextInputChanged) {
		t.Fatalf("commit after mode reset error = %v", err)
	}

	fresh := &Session{client: fixtureClient(t)}
	if _, err := fresh.TextInput(t.Context()); !errors.Is(err, backend.ErrNoTextInput) {
		t.Fatalf("inactive C input error = %v", err)
	}
}

func TestTextComponentEditingCountsUTF16Units(t *testing.T) {
	client := fixtureClient(t)
	const field = 0x1000
	state := client.javaWidgetState(field)
	state.text = "A🙂B"
	if _, err := javaTextComponentDelete(client, t.Context(), nil, []uint32{field, 1, 2}); err != nil {
		t.Fatal(err)
	}
	if state.text != "AB" {
		t.Fatalf("delete two UTF-16 units = %q, want %q", state.text, "AB")
	}
}

func TestShellAndFocusLifecycleInvokeApplicationNotifications(t *testing.T) {
	client := fixtureClient(t)
	showWord, err := client.allocate(4)
	if err != nil {
		t.Fatal(err)
	}
	focusWord, err := client.allocate(4)
	if err != nil {
		t.Fatal(err)
	}
	callback := func(offset, target uint32) uint32 {
		t.Helper()
		// ldr r2, [pc, #4]; str r1, [r2]; bx lr; nop; .word target
		return installThumbAt(t, client, offset,
			0x4a01, 0x6011, 0x4770, 0x46c0, uint16(target), uint16(target>>16))
	}
	platform, err := client.preparePlatformJavaClass("org/kwis/msp/lwc/TextFieldComponent")
	if err != nil {
		t.Fatal(err)
	}
	class := &javaRuntimeClass{
		Name: "test/HostTextField", Handle: 1, Slots: platform.Slots,
		Instance: platform.Instance, Super: platform,
		Record: javaClass{Name: "test/HostTextField", Methods: []javaMember{
			{Name: "showNotify", Descriptor: "(Z)V", Body: callback(0, showWord)},
			{Name: "focusNotify", Descriptor: "(Z)V", Body: callback(16, focusWord)},
		}},
	}
	classObject, err := client.allocateJavaClassObject(class)
	if err != nil {
		t.Fatal(err)
	}
	class.Object = classObject
	class.dataBlock, _ = client.readWord(classObject + 8)
	runtime := client.javaRuntimeState()
	runtime.byObject[classObject] = class
	runtime.byName[class.Name] = class
	if err := client.buildJavaVTable(class); err != nil {
		t.Fatal(err)
	}
	field, err := client.allocateJavaObject(class)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := javaTextComponentConstructor(javaWidgetTextField)(client, t.Context(), client.thread,
		[]uint32{field, newTestString(t, client, ""), uint32(javaTextConstraintAny)}); err != nil {
		t.Fatal(err)
	}
	shell, err := newTestObject(t, client, "org/kwis/msp/lwc/ShellComponent")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := javaShellComponentConstructor(client, t.Context(), client.thread,
		[]uint32{shell, 0, 0, 16, 8}); err != nil {
		t.Fatal(err)
	}
	if _, err := javaComponentAddChild(client, t.Context(), client.thread, []uint32{shell, field}); err != nil {
		t.Fatal(err)
	}
	if _, err := javaComponentShow(true)(client, t.Context(), client.thread, []uint32{shell}); err != nil {
		t.Fatal(err)
	}
	if got, err := client.readWord(showWord); err != nil || got != 1 {
		t.Fatalf("showNotify value = %d, %v", got, err)
	}
	if _, err := javaComponentSetFocus(client, t.Context(), client.thread, []uint32{field}); err != nil {
		t.Fatal(err)
	}
	if got, err := client.readWord(focusWord); err != nil || got != 1 {
		t.Fatalf("focusNotify(true) value = %d, %v", got, err)
	}
	other, err := newTestObject(t, client, "org/kwis/msp/lwc/TextFieldComponent")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := javaComponentSetFocus(client, t.Context(), client.thread, []uint32{other}); err != nil {
		t.Fatal(err)
	}
	if got, err := client.readWord(focusWord); err != nil || got != 0 {
		t.Fatalf("focusNotify(false) value = %d, %v", got, err)
	}
}

func installJavaWidgetCallbackClass(
	t *testing.T, client *Client, platformName, className, methodName string, body uint32,
) uint32 {
	t.Helper()
	platform, err := client.preparePlatformJavaClass(platformName)
	if err != nil {
		t.Fatal(err)
	}
	class := &javaRuntimeClass{
		Name: className, Handle: 1, Slots: platform.Slots,
		Instance: platform.Instance, Super: platform,
		Record: javaClass{Name: className, Methods: []javaMember{
			{Name: methodName, Descriptor: "(Z)V", Body: body},
		}},
	}
	classObject, err := client.allocateJavaClassObject(class)
	if err != nil {
		t.Fatal(err)
	}
	class.Object = classObject
	class.dataBlock, _ = client.readWord(classObject + 8)
	runtime := client.javaRuntimeState()
	runtime.byObject[classObject] = class
	runtime.byName[class.Name] = class
	if err := client.buildJavaVTable(class); err != nil {
		t.Fatal(err)
	}
	object, err := client.allocateJavaObject(class)
	if err != nil {
		t.Fatal(err)
	}
	return object
}

func installJavaWidgetVirtualCall(
	t *testing.T, client *Client, owner string, slot uint32, member javaMemberRef,
) uint32 {
	t.Helper()
	if _, err := client.preparePlatformJavaClass(owner); err != nil {
		t.Fatal(err)
	}
	layout := newJavaLayout()
	layout.class(owner).Virtual[javaMemberKey(member)] = slot
	client.javaLink = &javaLink{
		layout: layout,
		surface: &javaSurface{
			VirtualMethods: []javaMemberRef{member},
		},
	}
	return javaVirtualSlot(owner, slot)
}

func TestFocusCallbackCanReplaceTheOuterFocusRequest(t *testing.T) {
	client := fixtureClient(t)
	other, err := newTestObject(t, client, "org/kwis/msp/lwc/TextFieldComponent")
	if err != nil {
		t.Fatal(err)
	}
	slot := installJavaWidgetVirtualCall(t, client, "org/kwis/msp/lwc/Component", 20,
		javaMemberRef{Name: "setFocus", Descriptor: "()V"})
	// On focusNotify(false), dispatch Component.setFocus on other. The true
	// notification returns directly, so installing the initial focus is safe.
	callback := installThumbAt(t, client, 0,
		0xb500, 0x2900, 0xd103, 0x4802, 0x4b02, 0x469c, 0xdf05, 0xbd00,
		uint16(other), uint16(other>>16), uint16(slot), uint16(slot>>16),
	)
	old := installJavaWidgetCallbackClass(t, client,
		"org/kwis/msp/lwc/TextFieldComponent", "test/ReentrantFocusField", "focusNotify", callback)
	target, err := newTestObject(t, client, "org/kwis/msp/lwc/TextFieldComponent")
	if err != nil {
		t.Fatal(err)
	}

	if _, err := javaComponentSetFocus(client, t.Context(), client.thread, []uint32{old}); err != nil {
		t.Fatal(err)
	}
	if _, err := javaComponentSetFocus(client, t.Context(), client.thread, []uint32{target}); err != nil {
		t.Fatal(err)
	}
	if got := client.javaRuntimeState().focusedWidget; got != other {
		t.Fatalf("focused widget = %#x, want callback target %#x", got, other)
	}
	if client.javaWidgetState(target).focused {
		t.Fatal("outer target remained focused after the callback moved focus")
	}
}

func TestShowCallbackCanHideTheShellBeforeOuterTraversalContinues(t *testing.T) {
	client := fixtureClient(t)
	slot := installJavaWidgetVirtualCall(t, client, "org/kwis/msp/lwc/ShellComponent", 20,
		javaMemberRef{Name: "hide", Descriptor: "()V"})
	// On showNotify(true), dispatch hide on this shell. The false notification
	// returns directly, allowing the nested hide traversal to complete.
	callback := installThumbAt(t, client, 0,
		0xb500, 0x2900, 0xd002, 0x4b02, 0x469c, 0xdf05, 0xbd00, 0x46c0,
		uint16(slot), uint16(slot>>16),
	)
	shell := installJavaWidgetCallbackClass(t, client,
		"org/kwis/msp/lwc/ShellComponent", "test/ReentrantShell", "showNotify", callback)
	client.javaWidgetState(shell).kind = javaWidgetShell
	field, err := newTestObject(t, client, "org/kwis/msp/lwc/TextFieldComponent")
	if err != nil {
		t.Fatal(err)
	}
	client.javaWidgetState(field).kind = javaWidgetTextField
	if _, err := javaComponentAddChild(client, t.Context(), client.thread, []uint32{shell, field}); err != nil {
		t.Fatal(err)
	}

	if _, err := javaComponentShow(true)(client, t.Context(), client.thread, []uint32{shell}); err != nil {
		t.Fatal(err)
	}
	if client.javaWidgetState(shell).shown {
		t.Fatal("shell remained shown after its callback hid it")
	}
	if client.javaWidgetState(field).shown || client.javaWidgetShown(field) {
		t.Fatal("outer show traversal reshown a child after the shell callback hid it")
	}
}

func TestWidgetLifecycleCallbackUsesExactInheritedDescriptor(t *testing.T) {
	client := fixtureClient(t)
	called, err := client.allocate(4)
	if err != nil {
		t.Fatal(err)
	}
	// ldr r2, [pc, #4]; str r1, [r2]; bx lr; nop; .word called
	body := installThumbAt(t, client, 0,
		0x4a01, 0x6011, 0x4770, 0x46c0, uint16(called), uint16(called>>16))
	const superHandle = 0x4000
	client.javaRuntimeState().byHandle[superHandle] = &javaRuntimeClass{Record: javaClass{
		Name: "test/LifecycleBase", Methods: []javaMember{
			{Name: "showNotify", Descriptor: "(Z)V", Body: body},
		},
	}}
	platform, err := client.preparePlatformJavaClass("org/kwis/msp/lwc/TextFieldComponent")
	if err != nil {
		t.Fatal(err)
	}
	class := &javaRuntimeClass{
		Name: "test/LifecycleChild", Handle: 1, Slots: platform.Slots,
		Instance: platform.Instance, Super: platform,
		Record: javaClass{Name: "test/LifecycleChild", SuperHandle: superHandle, Methods: []javaMember{
			{Name: "showNotify", Descriptor: "()V", Body: 1},
		}},
	}
	classObject, err := client.allocateJavaClassObject(class)
	if err != nil {
		t.Fatal(err)
	}
	class.Object = classObject
	client.javaRuntimeState().byObject[classObject] = class
	client.javaRuntimeState().byName[class.Name] = class
	if err := client.buildJavaVTable(class); err != nil {
		t.Fatal(err)
	}
	field, err := client.allocateJavaObject(class)
	if err != nil {
		t.Fatal(err)
	}
	if err := client.callJavaWidgetOverride(t.Context(), client.thread,
		field, "showNotify", "(Z)V", true); err != nil {
		t.Fatal(err)
	}
	if got, err := client.readWord(called); err != nil || got != 1 {
		t.Fatalf("inherited showNotify(Z) value = %d, %v", got, err)
	}
}

func TestAddChildRejectsShellsAndCyclesBeforeMutation(t *testing.T) {
	client := fixtureClient(t)
	container, err := newTestObject(t, client, "org/kwis/msp/lwc/ContainerComponent")
	if err != nil {
		t.Fatal(err)
	}
	child, err := newTestObject(t, client, "org/kwis/msp/lwc/ContainerComponent")
	if err != nil {
		t.Fatal(err)
	}
	shell, err := newTestObject(t, client, "org/kwis/msp/lwc/ShellComponent")
	if err != nil {
		t.Fatal(err)
	}
	client.javaWidgetState(shell).kind = javaWidgetShell

	if _, err := javaComponentAddChild(client, t.Context(), client.thread,
		[]uint32{container, container}); err == nil {
		t.Fatal("adding a component to itself succeeded")
	}
	if _, err := javaComponentAddChild(client, t.Context(), client.thread,
		[]uint32{container, shell}); err == nil {
		t.Fatal("adding a shell as a child succeeded")
	}
	if _, err := javaComponentAddChild(client, t.Context(), client.thread,
		[]uint32{container, child}); err != nil {
		t.Fatal(err)
	}
	if _, err := javaComponentAddChild(client, t.Context(), client.thread,
		[]uint32{child, container}); err == nil {
		t.Fatal("adding an ancestor as a child succeeded")
	}
	if got := client.javaWidgetState(container).parent; got != 0 {
		t.Fatalf("rejected cycle changed container parent to %#x", got)
	}
	if got := len(client.javaWidgetState(child).children); got != 0 {
		t.Fatalf("rejected cycle added %d children", got)
	}
}

// A card only paints when it has asked to, and a Host commit reaches the field
// without a key that would have made the title ask. The committed text stayed
// off screen until the next press.
func TestTextInputCommitAsksThePushedCardToPaint(t *testing.T) {
	session, _, _ := focusedJavaTextFixture(t, javaWidgetTextField, javaTextConstraintAny, "old")
	runtime := session.client.javaRun
	runtime.card, runtime.cardDirty = 0x1000, false
	input, err := session.TextInput(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := input.Commit(context.Background(), "new"); err != nil {
		t.Fatal(err)
	}
	if !runtime.cardDirty {
		t.Fatal("commit left the pushed card without a paint request")
	}
}
