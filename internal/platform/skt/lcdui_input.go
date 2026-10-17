package skt

import (
	"fmt"
	"time"

	"github.com/movingwoo/wfeature/internal/api/midp"
	"github.com/movingwoo/wfeature/internal/jvm"
	"github.com/movingwoo/wfeature/internal/textinput"
)

// Soft key codes. MIDP never standardized them — every handset picked its
// own negative values — so the runtime uses the two the Nokia devices used,
// which is what MIDlets of the era were written against when they read raw
// key codes at all.
const (
	KeyCodeSoft1 int32 = -6
	KeyCodeSoft2 int32 = -7
)

// The TextField constraints that admit only numbers.
const (
	textFieldNumeric     int32 = 2
	textFieldPhoneNumber int32 = 3
	textFieldDecimal     int32 = 5
)

// deliverScreenKey routes a key press to a runtime-drawn screen: navigation
// moves the selection, the soft keys reach the commands, and nothing is
// forwarded to the application except through a command or item callback.
// Only presses act; releases and repeats on a Screen do nothing, because a
// Screen has no callback that would observe them.
func (runtime *Runtime) deliverScreenKey(screen *jvm.Object, eventType KeyEventType, keyCode int32) error {
	if eventType != KeyPressed {
		return nil
	}
	data := runtime.displayableState(screen)
	content := runtime.screenState(screen, screenNone)
	state := runtime.lcdui()

	state.mu.Lock()
	commands := append([]*jvm.Object(nil), data.commands...)
	menuOpen := data.menuOpen
	state.mu.Unlock()

	if menuOpen {
		return runtime.handleMenuKey(screen, data, content, commands, keyCode)
	}

	switch keyCode {
	case KeyCodeSoft1:
		if len(commands) > 0 {
			return runtime.fireScreenCommand(screen, content, commands[0])
		}
		return nil
	case KeyCodeSoft2:
		switch {
		case len(commands) > 2:
			state.mu.Lock()
			data.menuRevision++
			data.menuOpen = true
			data.menuIndex = 0
			state.mu.Unlock()
			return runtime.queueScreenPaint(screen)
		case len(commands) == 2:
			return runtime.fireScreenCommand(screen, content, commands[1])
		}
		return nil
	}

	switch content.kind {
	case screenList:
		return runtime.handleListKey(screen, content, commands, keyCode)
	case screenForm:
		return runtime.handleFormKey(screen, content, keyCode)
	case screenTextBox:
		return runtime.handleTextKey(screen, content, keyCode)
	}
	return nil
}

// handleTextKey types into a TextBox. The editing is the shared keypad input
// method, so a game types the same way here as it does in a KTF lwc field.
func (runtime *Runtime) handleTextKey(screen *jvm.Object, content *screenData, keyCode int32) error {
	runtime.textMu.Lock()
	if !keypadEditable(content.constraint) {
		runtime.textMu.Unlock()
		return nil
	}
	editor := runtime.textEditor(content)
	runtime.holdDigitMode(editor, content.constraint)
	// The pad moves the caret, and only the pad: a digit is a letter here,
	// so the 4 and 6 a game reads as left and right have to reach the
	// keypad table instead. Without that neither "ghi" nor "mno" is typable.
	switch keyCode {
	case KeyCodeLeft:
		content.textRevision++
		editor.MoveCaret(-1)
		runtime.textMu.Unlock()
		return runtime.queueScreenPaint(screen)
	case KeyCodeRight:
		content.textRevision++
		editor.MoveCaret(1)
		runtime.textMu.Unlock()
		return runtime.queueScreenPaint(screen)
	}
	if keyCode < 0 || keyCode > 0x7f {
		runtime.textMu.Unlock()
		return nil
	}
	if !editor.Key(rune(keyCode), runtime.editorClock()) {
		runtime.textMu.Unlock()
		return nil
	}
	content.textRevision++
	content.text = []rune(editor.Text())
	content.caret = editor.Caret()
	runtime.textMu.Unlock()
	return runtime.queueScreenPaint(screen)
}

// keypadEditable reports whether keypad typing may change a field with this
// constraint. An UNEDITABLE field is a read-only view, the same rule the Host
// keyboard path applies. A TextBox and the vendor XTextField both ask it.
func keypadEditable(constraint int32) bool {
	return constraint&textFieldUneditable == 0
}

// holdDigitMode keeps the keypad editor of a field that takes only numbers
// in digit mode, so a key types its digit, which is what the handset's digit
// mode did; the mode key cannot leave it. The caller holds textMu.
func (runtime *Runtime) holdDigitMode(editor *textinput.State, constraint int32) {
	switch constraint & midp.TextFieldConstraintMask {
	case textFieldNumeric, textFieldPhoneNumber, textFieldDecimal:
		for presses := 0; editor.Mode() != textinput.ModeNumeric && presses < 3; presses++ {
			editor.Key('*', runtime.editorClock())
		}
	}
}

// textEditor is the screen's keypad editor, created on first use and kept in
// step with a value the application set programmatically.
func (runtime *Runtime) textEditor(content *screenData) *textinput.State {
	if content.input == nil {
		content.input = textinput.NewUTF16(string(content.text), int(content.maxSize))
		return content.input
	}
	if content.input.Text() != string(content.text) {
		content.input.SetText(string(content.text))
	}
	content.input.SetMaxUTF16Units(int(content.maxSize))
	return content.input
}

// editorClock is the guest time the multi-tap timeout is measured against, so
// a Host batching ticks types the same text as one running live.
func (runtime *Runtime) editorClock() time.Time {
	return time.Unix(0, runtime.clockMillis()*int64(time.Millisecond))
}

func (runtime *Runtime) handleMenuKey(screen *jvm.Object, data *displayableData, content *screenData, commands []*jvm.Object, keyCode int32) error {
	state := runtime.lcdui()
	switch {
	case isUpKey(keyCode):
		state.mu.Lock()
		data.menuIndex = max(data.menuIndex-1, 0)
		state.mu.Unlock()
		return runtime.queueScreenPaint(screen)
	case isDownKey(keyCode):
		state.mu.Lock()
		data.menuIndex = min(data.menuIndex+1, len(commands)-1)
		state.mu.Unlock()
		return runtime.queueScreenPaint(screen)
	case isFireKey(keyCode) || keyCode == KeyCodeSoft1:
		state.mu.Lock()
		index := data.menuIndex
		data.menuRevision++
		data.menuOpen = false
		state.mu.Unlock()
		if err := runtime.queueScreenPaint(screen); err != nil {
			return err
		}
		if index >= 0 && index < len(commands) {
			return runtime.fireScreenCommand(screen, content, commands[index])
		}
		return nil
	case keyCode == KeyCodeSoft2:
		state.mu.Lock()
		data.menuRevision++
		data.menuOpen = false
		state.mu.Unlock()
		return runtime.queueScreenPaint(screen)
	}
	return nil
}

// fireScreenCommand reports a command a soft key or the command menu chose.
// An IMPLICIT List selects its focused row first: MIDP defines IMPLICIT as the
// choice whose focused element is selected when a Command is initiated, and a
// title reads that row with getSelectedIndex in its commandAction.
func (runtime *Runtime) fireScreenCommand(screen *jvm.Object, content *screenData, command *jvm.Object) error {
	runtime.textMu.Lock()
	if choice := content.choice; content.kind == screenList && choice != nil && choice.kind == choiceImplicit && len(choice.elements) > 0 {
		content.selection = listFocus(content)
		choice.selectExclusive(content.selection)
	}
	runtime.textMu.Unlock()
	return runtime.fireCommand(screen, command)
}

// listFocus is a non-empty List's focused row, kept inside its rows. A focus
// restored from a checkpoint taken while the List was empty is -1, and the
// title may have added rows since.
func listFocus(content *screenData) int {
	return clamp(content.selection, 0, len(content.choice.elements)-1)
}

// handleListKey moves and acts on a List's focus. The List is read and
// changed under textMu, the lock a title's own threads edit it under, and the
// lock is released before anything calls back into the title.
func (runtime *Runtime) handleListKey(screen *jvm.Object, content *screenData, commands []*jvm.Object, keyCode int32) error {
	runtime.textMu.Lock()
	choice := content.choice
	if choice == nil || len(choice.elements) == 0 {
		runtime.textMu.Unlock()
		return nil
	}
	focus := listFocus(content)
	selected := false
	switch {
	case isUpKey(keyCode):
		content.selection = max(focus-1, 0)
	case isDownKey(keyCode):
		content.selection = min(focus+1, len(choice.elements)-1)
	case isFireKey(keyCode):
		content.selection = focus
		if choice.kind == choiceMultiple {
			choice.elements[focus].selected = !choice.elements[focus].selected
			break
		}
		choice.selectExclusive(focus)
		selected = choice.kind == choiceImplicit
	default:
		runtime.textMu.Unlock()
		return nil
	}
	runtime.textMu.Unlock()
	if err := runtime.queueScreenPaint(screen); err != nil {
		return err
	}
	if selected {
		// An IMPLICIT List reports the selection as a command, which is the
		// only way a game learns the user chose a row.
		return runtime.fireCommand(screen, runtime.listSelectCommand(content, commands))
	}
	return nil
}

// listSelectCommand is the command an IMPLICIT List fires on selection: the
// one the application set, or List.SELECT_COMMAND.
func (runtime *Runtime) listSelectCommand(content *screenData, commands []*jvm.Object) *jvm.Object {
	runtime.textMu.Lock()
	selectCommand := content.selectCommand
	runtime.textMu.Unlock()
	if selectCommand != nil {
		return selectCommand
	}
	selectCommand, err := runtime.staticSelectCommand()
	if err == nil && selectCommand != nil {
		return selectCommand
	}
	if len(commands) > 0 {
		return commands[0]
	}
	return nil
}

// staticSelectCommand reads List.SELECT_COMMAND, which the class initializer
// creates.
func (runtime *Runtime) staticSelectCommand() (*jvm.Object, error) {
	value, err := runtime.VM.StaticField(midp.ListClass, "SELECT_COMMAND", "Ljavax/microedition/lcdui/Command;")
	if err != nil {
		return nil, err
	}
	return value.Reference()
}

// handleFormKey moves through a Form and acts on its focused item, under
// textMu for the same reason handleListKey is.
func (runtime *Runtime) handleFormKey(screen *jvm.Object, content *screenData, keyCode int32) error {
	runtime.textMu.Lock()
	if len(content.items) == 0 {
		runtime.textMu.Unlock()
		return nil
	}
	switch {
	case isUpKey(keyCode):
		content.textRevision++
		content.selection = runtime.previousFocusableItem(content)
		content.subSelection = 0
		runtime.textMu.Unlock()
		return runtime.queueScreenPaint(screen)
	case isDownKey(keyCode):
		content.textRevision++
		content.selection = runtime.nextFocusableItem(content)
		content.subSelection = 0
		runtime.textMu.Unlock()
		return runtime.queueScreenPaint(screen)
	}
	item := content.items[min(content.selection, len(content.items)-1)]
	data, err := itemOf(item)
	if err != nil {
		runtime.textMu.Unlock()
		return nil
	}
	switch {
	case isLeftKey(keyCode) && data.choice != nil:
		content.subSelection = max(content.subSelection-1, 0)
		runtime.textMu.Unlock()
		return runtime.queueScreenPaint(screen)
	case isRightKey(keyCode) && data.choice != nil:
		// An empty ChoiceGroup keeps its element focus at 0 rather than -1,
		// which the FIRE after the title adds options would index with.
		content.subSelection = max(min(content.subSelection+1, len(data.choice.elements)-1), 0)
		runtime.textMu.Unlock()
		return runtime.queueScreenPaint(screen)
	case isFireKey(keyCode):
		if data.choice != nil && len(data.choice.elements) > 0 {
			// A focus of -1 can come back from a checkpoint taken while the
			// group was empty.
			index := clamp(content.subSelection, 0, len(data.choice.elements)-1)
			if data.choice.kind == choiceMultiple {
				data.choice.elements[index].selected = !data.choice.elements[index].selected
			} else {
				data.choice.selectExclusive(index)
			}
			runtime.textMu.Unlock()
			if err := runtime.queueScreenPaint(screen); err != nil {
				return err
			}
			return runtime.reportItemStateChange(item, data)
		}
		if len(data.commands) > 0 {
			command, listener := data.commands[0], data.listener
			runtime.textMu.Unlock()
			return runtime.fireItemCommand(item, listener, command)
		}
	}
	runtime.textMu.Unlock()
	return nil
}

// nextFocusableItem is the next item the selection cursor may land on,
// staying put when there is none.
func (runtime *Runtime) nextFocusableItem(content *screenData) int {
	for index := content.selection + 1; index < len(content.items); index++ {
		if data, err := itemOf(content.items[index]); err == nil && runtime.itemIsInteractive(data) {
			return index
		}
	}
	return content.selection
}

func (runtime *Runtime) previousFocusableItem(content *screenData) int {
	for index := content.selection - 1; index >= 0; index-- {
		if data, err := itemOf(content.items[index]); err == nil && runtime.itemIsInteractive(data) {
			return index
		}
	}
	return content.selection
}

// fireCommand reports a command to the Displayable's command listener.
func (runtime *Runtime) fireCommand(displayable *jvm.Object, command *jvm.Object) error {
	if command == nil {
		return nil
	}
	data := runtime.displayableState(displayable)
	state := runtime.lcdui()
	state.mu.Lock()
	listener := data.listener
	state.mu.Unlock()
	if listener == nil {
		return nil
	}
	if runtime.logger != nil {
		runtime.logger.Debug("MIDP command", "label", commandLabelText(command), "class", displayable.ClassName)
	}
	if _, err := runtime.VM.InvokeVirtual(listener, "commandAction",
		"(Ljavax/microedition/lcdui/Command;Ljavax/microedition/lcdui/Displayable;)V",
		jvm.ReferenceValue(command), jvm.ReferenceValue(displayable)); err != nil {
		if absorbed := runtime.absorbUncaughtCallback("commandAction "+listener.ClassName, err); absorbed != nil {
			return fmt.Errorf("deliver commandAction: %w", absorbed)
		}
	}
	return nil
}

// fireItemCommand reports a command attached to a Form item.
func (runtime *Runtime) fireItemCommand(item *jvm.Object, listener *jvm.Object, command *jvm.Object) error {
	if command == nil || listener == nil {
		return nil
	}
	if _, err := runtime.VM.InvokeVirtual(listener, "commandAction",
		"(Ljavax/microedition/lcdui/Command;Ljavax/microedition/lcdui/Item;)V",
		jvm.ReferenceValue(command), jvm.ReferenceValue(item)); err != nil {
		if absorbed := runtime.absorbUncaughtCallback("item commandAction "+listener.ClassName, err); absorbed != nil {
			return fmt.Errorf("deliver item commandAction: %w", absorbed)
		}
	}
	return nil
}

// The navigation keys a screen understands: the device codes and the numeric
// keypad equivalents Canvas.getGameAction already maps.
func isUpKey(keyCode int32) bool    { return keyCode == KeyCodeUp || keyCode == '2' }
func isDownKey(keyCode int32) bool  { return keyCode == KeyCodeDown || keyCode == '8' }
func isLeftKey(keyCode int32) bool  { return keyCode == KeyCodeLeft || keyCode == '4' }
func isRightKey(keyCode int32) bool { return keyCode == KeyCodeRight || keyCode == '6' }
func isFireKey(keyCode int32) bool  { return keyCode == KeyCodeFire || keyCode == '5' }

// canvasKeyState maps a key code to the GameCanvas.getKeyStates bit it sets.
func canvasKeyState(keyCode int32) int32 {
	switch {
	case isUpKey(keyCode):
		return 1 << 1
	case isDownKey(keyCode):
		return 1 << 6
	case isLeftKey(keyCode):
		return 1 << 2
	case isRightKey(keyCode):
		return 1 << 5
	case isFireKey(keyCode):
		return 1 << 8
	case keyCode == '1':
		return 1 << 9
	case keyCode == '3':
		return 1 << 10
	case keyCode == '7':
		return 1 << 11
	case keyCode == '9':
		return 1 << 12
	}
	return 0
}
