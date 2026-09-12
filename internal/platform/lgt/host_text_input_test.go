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
	if err := input.Commit(context.Background(), "한글🙂"); err != nil {
		t.Fatal(err)
	}
	if state.text != "한글🙂" {
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

func TestTextInputRequiresFocusAndAShownShellAndRejectsAListener(t *testing.T) {
	session, shell, field := focusedJavaTextFixture(t, javaWidgetTextField, javaTextConstraintAny, "old")
	runtime := session.client.javaRun
	runtime.focusedWidget = 0
	if _, err := session.TextInput(context.Background()); !errors.Is(err, backend.ErrNoTextInput) {
		t.Fatalf("unfocused field error = %v", err)
	}
	runtime.focusedWidget = field
	session.client.javaWidgetState(shell).shown = false
	if _, err := session.TextInput(context.Background()); !errors.Is(err, backend.ErrNoTextInput) {
		t.Fatalf("hidden shell error = %v", err)
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
	edit, err := cleanSession.TextInput(context.Background())
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
