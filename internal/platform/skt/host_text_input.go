package skt

import (
	"context"
	"fmt"
	"unicode/utf16"

	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/jvm"
)

const textFieldUneditable int32 = 0x20000

type hostTextTargetKind uint8

const (
	hostTextBox hostTextTargetKind = iota
	hostFormTextField
	hostXTextField
	hostTextComponent
)

type hostTextTarget struct {
	kind            hostTextTargetKind
	display         *jvm.Object
	field           *jvm.Object
	original        string
	maxSize         int32
	constraint      int32
	size            int32
	caret           int32
	revision        uint64
	displayRevision uint64
	menuRevision    uint64
	screenRevision  uint64
}

// TextInput exposes the active field to a Host keyboard or IME. The snapshot
// and its commit run under the same serialization lock as keypad events. A
// commit therefore cannot land on a field that replaced the original while
// the Host was composing text.
func (runtime *Runtime) TextInput(ctx context.Context) (*backend.TextInput, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	runtime.dispatchMu.Lock()
	defer runtime.dispatchMu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return runtime.textInputSnapshot()
}

func (runtime *Runtime) textInputSnapshot() (*backend.TextInput, error) {
	if runtime.State() != StateActive {
		return nil, backend.ErrNoTextInput
	}
	runtime.textMu.Lock()
	runtime.displayMu.RLock()
	current := runtime.currentDisplayable
	displayRevision := runtime.displayRevision
	runtime.displayMu.RUnlock()
	if current == nil {
		runtime.textMu.Unlock()
		return nil, backend.ErrNoTextInput
	}

	state := runtime.lcdui()
	state.mu.Lock()
	display := state.displayables[current]
	var menuRevision uint64
	if display != nil {
		menuRevision = display.menuRevision
	}
	if display != nil && display.menuOpen {
		state.mu.Unlock()
		runtime.textMu.Unlock()
		return nil, backend.ErrNoTextInput
	}
	if display != nil && display.screen != nil {
		screen := display.screen
		switch screen.kind {
		case screenTextBox:
			if screen.constraint&textFieldUneditable == 0 {
				target := hostTextTarget{
					kind: hostTextBox, display: current, original: string(screen.text),
					displayRevision: displayRevision, menuRevision: menuRevision, revision: screen.textRevision,
					maxSize: screen.maxSize, constraint: screen.constraint,
				}
				result := hostTextInput(runtime, target, screen.maxSize, screen.constraint, true)
				state.mu.Unlock()
				runtime.textMu.Unlock()
				return result, nil
			}
		case screenForm:
			if screen.selection >= 0 && screen.selection < len(screen.items) {
				field := screen.items[screen.selection]
				if data, ok := field.Native.(*itemData); ok && data != nil && data.kind == itemText &&
					data.constraint&textFieldUneditable == 0 {
					target := hostTextTarget{
						kind: hostFormTextField, display: current, field: field, original: string(data.text),
						displayRevision: displayRevision, menuRevision: menuRevision, revision: data.textRevision, screenRevision: screen.textRevision,
						maxSize: data.maxSize, constraint: data.constraint,
					}
					result := hostTextInput(runtime, target, data.maxSize, data.constraint, false)
					state.mu.Unlock()
					runtime.textMu.Unlock()
					return result, nil
				}
			}
		}
	}
	state.mu.Unlock()

	vendor := runtime.skvm()
	vendor.mu.Lock()
	field := vendor.focusedTextField
	if data, ok := nativeXTextField(field); ok && data.focus && data.visibleOn(current, displayRevision) &&
		data.constraints&textFieldUneditable == 0 {
		target := hostTextTarget{
			kind: hostXTextField, display: current, field: field, original: string(data.text),
			displayRevision: displayRevision, menuRevision: menuRevision, revision: data.textRevision,
			maxSize: data.maxSize, constraint: data.constraints,
		}
		result := hostTextInput(runtime, target, data.maxSize, data.constraints, false)
		vendor.mu.Unlock()
		runtime.textMu.Unlock()
		return result, nil
	}
	component := vendor.textInput.component
	revision := vendor.textInput.revision
	vendor.mu.Unlock()
	runtime.textMu.Unlock()
	if component == nil {
		return nil, backend.ErrNoTextInput
	}
	return runtime.hostTextComponentInput(current, component, revision, displayRevision, menuRevision)
}

func hostTextInput(runtime *Runtime, target hostTextTarget, maxSize, constraint int32, multiline bool) *backend.TextInput {
	return &backend.TextInput{
		Text:      target.original,
		MaxLength: int(maxSize),
		Multiline: multiline,
		Password:  constraint&midpTextFieldPassword != 0,
		InputMode: hostInputMode(constraint),
		Commit: func(ctx context.Context, text string) error {
			return runtime.commitTextInput(ctx, target, text)
		},
	}
}

const midpTextFieldPassword int32 = 0x10000

func hostInputMode(constraint int32) string {
	switch constraint & 0xffff {
	case 1:
		return "email"
	case 2:
		return "numeric"
	case 3:
		return "tel"
	case 4:
		return "url"
	case 5:
		return "decimal"
	default:
		return "text"
	}
}

func (runtime *Runtime) commitTextInput(ctx context.Context, target hostTextTarget, text string) error {
	if err := backend.ValidateTextInput(text); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	runtime.dispatchMu.Lock()
	defer runtime.dispatchMu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if !runtime.hostTextDisplayUnchanged(target) {
		return backend.ErrTextInputChanged
	}

	var err error
	switch target.kind {
	case hostTextBox:
		err = runtime.commitTextBox(target, text)
	case hostFormTextField:
		err = runtime.commitFormTextField(target, text)
	case hostXTextField:
		err = runtime.commitXTextField(target, text)
	case hostTextComponent:
		err = runtime.commitTextComponent(ctx, target, text)
	default:
		err = backend.ErrTextInputChanged
	}
	if err != nil {
		return err
	}
	if err := runtime.runEvents(); err != nil {
		return err
	}
	return runtime.presentRefresh()
}

// hostTextComponentInput exposes a title-owned TextComponent as an append
// target. The interface deliberately has no text getter: the title owns the
// buffer and the handset input method can only send edits to its current
// cursor.
func (runtime *Runtime) hostTextComponentInput(display, component *jvm.Object, revision, displayRevision, menuRevision uint64) (*backend.TextInput, error) {
	target, err := readHostTextComponent(runtime.VM, component)
	if err != nil {
		return nil, err
	}
	target.kind = hostTextComponent
	target.display = display
	target.field = component
	target.revision = revision
	target.displayRevision, target.menuRevision = displayRevision, menuRevision
	if !validHostTextComponent(target) {
		return nil, backend.ErrNoTextInput
	}

	state := runtime.skvm()
	state.mu.Lock()
	current := state.textInput.component
	currentRevision := state.textInput.revision
	state.mu.Unlock()
	if current != component || currentRevision != revision {
		return nil, backend.ErrNoTextInput
	}
	return &backend.TextInput{
		MaxLength: int(target.maxSize - target.size),
		Password:  target.constraint&midpTextFieldPassword != 0,
		Append:    true,
		InputMode: hostInputMode(target.constraint),
		Commit: func(ctx context.Context, text string) error {
			return runtime.commitTextInput(ctx, target, text)
		},
	}, nil
}

func readHostTextComponent(vm *jvm.VM, component *jvm.Object) (hostTextTarget, error) {
	read := func(name string) (int32, error) {
		value, err := vm.InvokeVirtual(component, name, "()I")
		if err != nil {
			return 0, fmt.Errorf("TextComponent.%s: %w", name, err)
		}
		result, err := value.Int32()
		if err != nil {
			return 0, fmt.Errorf("TextComponent.%s result: %w", name, err)
		}
		return result, nil
	}
	var target hostTextTarget
	var err error
	if target.size, err = read("size"); err != nil {
		return hostTextTarget{}, err
	}
	if target.caret, err = read("getCaretPosition"); err != nil {
		return hostTextTarget{}, err
	}
	if target.maxSize, err = read("getMaxSize"); err != nil {
		return hostTextTarget{}, err
	}
	if target.constraint, err = read("getConstraints"); err != nil {
		return hostTextTarget{}, err
	}
	return target, nil
}

func validHostTextComponent(target hostTextTarget) bool {
	base := target.constraint & 0xffff
	return target.size >= 0 && target.caret >= 0 && target.caret <= target.size &&
		target.maxSize > target.size && base <= 5 &&
		target.constraint&textFieldUneditable == 0
}

func validHostTextComponentAppend(text string, remaining, constraint int32) bool {
	if !validHostText(text, remaining, constraint, false) {
		return false
	}
	// The guest contract inserts one Java char per call. This JVM cannot
	// preserve a surrogate pair across two arbitrary guest callbacks, so reject
	// supplementary characters instead of silently inserting replacements.
	for _, character := range text {
		if character > 0xffff {
			return false
		}
	}
	return true
}

func (runtime *Runtime) hostTextDisplayUnchanged(target hostTextTarget) bool {
	if runtime.State() != StateActive {
		return false
	}
	runtime.displayMu.RLock()
	current, revision := runtime.currentDisplayable, runtime.displayRevision
	pendingChange := runtime.displayUpdateQueued && runtime.pendingDisplayable != current
	runtime.displayMu.RUnlock()
	if current != target.display || revision != target.displayRevision || pendingChange {
		return false
	}
	state := runtime.lcdui()
	state.mu.Lock()
	defer state.mu.Unlock()
	display := state.displayables[current]
	return display == nil && target.menuRevision == 0 || display != nil && !display.menuOpen && display.menuRevision == target.menuRevision
}

func (runtime *Runtime) checkTextComponentDelivery(ctx context.Context, target hostTextTarget) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !runtime.hostTextDisplayUnchanged(target) {
		return backend.ErrTextInputChanged
	}
	state := runtime.skvm()
	state.mu.Lock()
	defer state.mu.Unlock()
	if state.textInput.component != target.field || state.textInput.revision != target.revision {
		return backend.ErrTextInputChanged
	}
	return nil
}

func (runtime *Runtime) commitTextComponent(ctx context.Context, target hostTextTarget, text string) error {
	stateUI := runtime.lcdui()
	stateUI.mu.Lock()
	display := stateUI.displayables[target.display]
	menuOpen := display != nil && display.menuOpen
	stateUI.mu.Unlock()
	if menuOpen {
		return backend.ErrTextInputChanged
	}

	state := runtime.skvm()
	state.mu.Lock()
	current := state.textInput.component
	revision := state.textInput.revision
	state.mu.Unlock()
	if current != target.field || revision != target.revision {
		return backend.ErrTextInputChanged
	}
	metadata, err := readHostTextComponent(runtime.VM, target.field)
	if err != nil {
		return fmt.Errorf("%w: %v", backend.ErrTextInputChanged, err)
	}
	if metadata.size != target.size || metadata.caret != target.caret ||
		metadata.maxSize != target.maxSize || metadata.constraint != target.constraint ||
		!validHostTextComponent(metadata) {
		return backend.ErrTextInputChanged
	}
	remaining := target.maxSize - target.size
	if !validHostTextComponentAppend(text, remaining, target.constraint) {
		return backend.ErrInvalidTextInput
	}
	if text == "" {
		return nil
	}

	// Reserve this component revision before entering guest code. Other Host
	// snapshots now become stale even if an insert callback changes focus.
	state.mu.Lock()
	if state.textInput.component != target.field || state.textInput.revision != target.revision {
		state.mu.Unlock()
		return backend.ErrTextInputChanged
	}
	state.textInput.revision++
	target.revision = state.textInput.revision
	state.textInput.endCycle()
	state.mu.Unlock()
	for _, character := range utf16.Encode([]rune(text)) {
		if err := runtime.checkTextComponentDelivery(ctx, target); err != nil {
			return err
		}
		if _, err := runtime.VM.InvokeVirtual(target.field, "insert", "(C)V", jvm.IntValue(int32(character))); err != nil {
			return err
		}
	}
	if err := runtime.checkTextComponentDelivery(ctx, target); err != nil {
		return err
	}
	_, err = runtime.VM.InvokeVirtual(target.field, "repaint", "()V")
	return err
}

func (runtime *Runtime) commitTextBox(target hostTextTarget, text string) error {
	runtime.textMu.Lock()
	state := runtime.lcdui()
	state.mu.Lock()
	display := state.displayables[target.display]
	if display == nil || display.menuOpen || display.screen == nil || display.screen.kind != screenTextBox {
		state.mu.Unlock()
		runtime.textMu.Unlock()
		return backend.ErrTextInputChanged
	}
	screen := display.screen
	if screen.textRevision != target.revision || string(screen.text) != target.original || screen.maxSize != target.maxSize ||
		screen.constraint != target.constraint || screen.constraint&textFieldUneditable != 0 {
		state.mu.Unlock()
		runtime.textMu.Unlock()
		return backend.ErrTextInputChanged
	}
	if !validHostText(text, screen.maxSize, screen.constraint, true) {
		state.mu.Unlock()
		runtime.textMu.Unlock()
		return backend.ErrInvalidTextInput
	}
	screen.text = []rune(text)
	screen.textRevision++
	screen.caret = len(screen.text)
	if screen.input != nil {
		screen.input.SetText(text)
	}
	state.mu.Unlock()
	runtime.textMu.Unlock()
	return runtime.queueScreenPaint(target.display)
}

func (runtime *Runtime) commitFormTextField(target hostTextTarget, text string) error {
	runtime.textMu.Lock()
	state := runtime.lcdui()
	state.mu.Lock()
	display := state.displayables[target.display]
	if display == nil || display.menuOpen || display.screen == nil || display.screen.kind != screenForm {
		state.mu.Unlock()
		runtime.textMu.Unlock()
		return backend.ErrTextInputChanged
	}
	form := display.screen
	if form.textRevision != target.screenRevision || form.selection < 0 || form.selection >= len(form.items) || form.items[form.selection] != target.field {
		state.mu.Unlock()
		runtime.textMu.Unlock()
		return backend.ErrTextInputChanged
	}
	data, ok := target.field.Native.(*itemData)
	if !ok || data == nil || data.textRevision != target.revision || data.kind != itemText || data.owner != target.display ||
		string(data.text) != target.original || data.maxSize != target.maxSize || data.constraint != target.constraint ||
		data.constraint&textFieldUneditable != 0 {
		state.mu.Unlock()
		runtime.textMu.Unlock()
		return backend.ErrTextInputChanged
	}
	if !validHostText(text, data.maxSize, data.constraint, false) {
		state.mu.Unlock()
		runtime.textMu.Unlock()
		return backend.ErrInvalidTextInput
	}
	data.text = []rune(text)
	data.textRevision++
	state.mu.Unlock()
	runtime.textMu.Unlock()
	if err := runtime.refreshItemOwner(target.field, data); err != nil {
		return err
	}
	return runtime.reportItemStateChange(target.field, data)
}

func (runtime *Runtime) commitXTextField(target hostTextTarget, text string) error {
	runtime.textMu.Lock()
	stateUI := runtime.lcdui()
	stateUI.mu.Lock()
	display := stateUI.displayables[target.display]
	menuOpen := display != nil && display.menuOpen
	stateUI.mu.Unlock()
	if menuOpen {
		runtime.textMu.Unlock()
		return backend.ErrTextInputChanged
	}
	state := runtime.skvm()
	state.mu.Lock()
	data, ok := nativeXTextField(target.field)
	if !ok || data.textRevision != target.revision || state.focusedTextField != target.field || !data.focus || !data.visibleOn(target.display, target.displayRevision) ||
		string(data.text) != target.original || data.maxSize != target.maxSize || data.constraints != target.constraint ||
		data.constraints&textFieldUneditable != 0 {
		state.mu.Unlock()
		runtime.textMu.Unlock()
		return backend.ErrTextInputChanged
	}
	if !validHostText(text, data.maxSize, data.constraints, false) {
		state.mu.Unlock()
		runtime.textMu.Unlock()
		return backend.ErrInvalidTextInput
	}
	data.text = []rune(text)
	data.textRevision++
	if data.input != nil {
		data.input.SetText(text)
	}
	state.mu.Unlock()
	runtime.textMu.Unlock()
	if _, err := runtime.VM.InvokeVirtual(target.field, "repaint", "()V"); err != nil {
		return err
	}
	return runtime.repaintNewCurrentCanvas(target.display)
}

func (data *xTextFieldData) visibleOn(display *jvm.Object, revision uint64) bool {
	return data.owner == display || data.paintedDisplay == display && data.paintedDisplayRevision == revision
}

func nativeXTextField(field *jvm.Object) (*xTextFieldData, bool) {
	if field == nil {
		return nil, false
	}
	data, ok := field.Native.(*xTextFieldData)
	return data, ok && data != nil
}

func validHostText(text string, maxSize, constraint int32, multiline bool) bool {
	// TextField maxSize counts Java char values. Supplementary Unicode
	// characters therefore occupy two units even though Go represents one as
	// one rune.
	if maxSize <= 0 || len(utf16.Encode([]rune(text))) > int(maxSize) {
		return false
	}
	if !multiline {
		for _, character := range text {
			if character == '\r' || character == '\n' {
				return false
			}
		}
	}
	switch constraint & 0xffff {
	case 0, 1, 4:
		return true
	case 2:
		return validSignedDigits(text)
	case 3:
		return validPhoneNumber(text)
	case 5:
		return validDecimal(text)
	default:
		return false
	}
}

func validSignedDigits(text string) bool {
	if text == "" {
		return true
	}
	digits := text
	if digits[0] == '-' {
		digits = digits[1:]
	}
	if digits == "" {
		return false
	}
	for _, character := range digits {
		if character < '0' || character > '9' {
			return false
		}
	}
	return true
}

func validPhoneNumber(text string) bool {
	for index, character := range text {
		switch {
		case character >= '0' && character <= '9':
		case character == '*' || character == '#':
		case character == '+' && index == 0:
		default:
			return false
		}
	}
	return true
}

func validDecimal(text string) bool {
	if text == "" {
		return true
	}
	number := text
	if number[0] == '-' {
		number = number[1:]
	}
	if number == "" {
		return false
	}
	digits, points := 0, 0
	for _, character := range number {
		switch {
		case character >= '0' && character <= '9':
			digits++
		case character == '.':
			points++
		default:
			return false
		}
	}
	return digits > 0 && points <= 1
}
