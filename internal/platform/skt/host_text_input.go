package skt

import (
	"context"
	"fmt"
	"unicode/utf16"

	"github.com/movingwoo/wfeature/internal/api/midp"
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
	// hostPaintedXTextField is a vendor field nobody focused, offered because
	// it is the one field the title painted on the current Displayable.
	hostPaintedXTextField
)

type hostTextTarget struct {
	kind hostTextTargetKind
	// fallback marks a target no strict route chose: it was found only after
	// every one of them came up empty, and its commit checks that the rule
	// that found it still finds it. See textInputSnapshot.
	fallback        bool
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

// textInputSnapshot finds the field a Host edit is for.
//
// The strict routes come first, in this order, and each names a field only
// when the platform's own state puts the player in it: a TextBox, the selected
// TextField of a Form, the focused XTextField when the screen is known to show
// it, and the TextComponent the vendor input method is attached to. **The
// fallbacks are asked only when every strict route came up empty**, so what
// the strict routes pick they still pick, in the same order. A field the
// player can see and cannot type into is the failure that matters; offering
// text input on a screen that shows no field is the lesser one. The fallbacks
// are a Form's only editable TextField, the focused XTextField on a Canvas
// whatever it was painted into, and, with no field focused, the one XTextField
// painted at this display revision.
//
// No fallback is asked while a TextComponent is attached, even one that
// cannot take text: that component is where the handset's keys type, so text
// written into any other field would land where the player is not typing.
func (runtime *Runtime) textInputSnapshot() (*backend.TextInput, error) {
	if runtime.State() != StateActive {
		return nil, backend.ErrNoTextInput
	}
	runtime.displayMu.RLock()
	current := runtime.currentDisplayable
	displayRevision := runtime.displayRevision
	runtime.displayMu.RUnlock()
	if current == nil {
		return nil, backend.ErrNoTextInput
	}
	// A vendor field is drawn by the title, so only a Canvas can be showing one
	// the strict route could not place. This is asked before any lock is taken
	// because a class hierarchy can need the loader; the Displayable cannot
	// change meanwhile, because it changes only on the event loop and the
	// caller holds dispatchMu.
	canvas, err := runtime.VM.IsSubclassOf(current.ClassName, midp.CanvasClass)
	canvas = canvas && err == nil

	runtime.textMu.Lock()
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
	var fallback *backend.TextInput
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
			if field, data, selected := formTextFieldForHost(screen); field != nil {
				target := hostTextTarget{
					kind: hostFormTextField, fallback: !selected, display: current, field: field, original: string(data.text),
					displayRevision: displayRevision, menuRevision: menuRevision, revision: data.textRevision, screenRevision: screen.textRevision,
					maxSize: data.maxSize, constraint: data.constraint,
				}
				result := hostTextInput(runtime, target, data.maxSize, data.constraint, false)
				if selected {
					state.mu.Unlock()
					runtime.textMu.Unlock()
					return result, nil
				}
				// The Form's only field waits for the routes below to come up
				// empty. textMu is held throughout, and every Form and
				// TextField change takes it, so what it was built from stays.
				fallback = result
			}
		}
	}
	state.mu.Unlock()

	vendor := runtime.skvm()
	vendor.mu.Lock()
	if field, data := vendor.focusedXTextField(); field != nil && data.visibleOn(current, displayRevision) &&
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
	if component == nil && fallback == nil && canvas {
		fallback = runtime.hostXTextFieldFallback(vendor, current, displayRevision, menuRevision)
	}
	vendor.mu.Unlock()
	runtime.textMu.Unlock()
	if component != nil {
		return runtime.hostTextComponentInput(current, component, revision, displayRevision, menuRevision)
	}
	if fallback == nil {
		return nil, backend.ErrNoTextInput
	}
	return fallback, nil
}

// formTextFieldForHost answers which TextField of a Form a Host edit is for,
// and whether it is the selected item. The selected item comes first when it
// is an editable TextField: that is the field the player is on. When it is
// not, a Form with exactly one editable TextField has that one. A Form's
// selection starts on its first item and appending items does not move it,
// only the pad does, so a Form that opens on a label above its one field would
// otherwise refuse the page until the player pressed Down. Two or more
// editable fields with the selection on neither leave no way to tell which the
// player means, and the answer is none. The caller holds textMu.
func formTextFieldForHost(form *screenData) (*jvm.Object, *itemData, bool) {
	if form.selection >= 0 && form.selection < len(form.items) {
		if data, ok := editableFormTextField(form.items[form.selection]); ok {
			return form.items[form.selection], data, true
		}
	}
	var only *jvm.Object
	var onlyData *itemData
	for _, item := range form.items {
		data, ok := editableFormTextField(item)
		if !ok || item == only {
			continue
		}
		if only != nil {
			return nil, nil, false
		}
		only, onlyData = item, data
	}
	return only, onlyData, false
}

// editableFormTextField reports a Form item a Host edit may write: a
// TextField that is not UNEDITABLE.
func editableFormTextField(item *jvm.Object) (*itemData, bool) {
	if item == nil {
		return nil, false
	}
	data, ok := item.Native.(*itemData)
	return data, ok && data != nil && data.kind == itemText && data.constraint&textFieldUneditable == 0
}

// hostXTextFieldFallback answers the vendor field a Host edit is for when the
// strict route could not place one on screen. A focused field is offered on
// any Canvas, whatever it was painted into: a title that draws it into its own
// back buffer, or draws its text itself, never paints it onto the screen
// Graphics, and the field is still the one the player is looking at. With no
// field focused, the one field painted, into any Graphics, at this display
// revision is offered; two make the paint ambiguous. A focused field that
// cannot take text is still the focused one, so a field merely painted beside
// it is not offered in its place.
//
// The caller holds textMu and the vendor lock, and has checked that the
// current Displayable is a Canvas and that no TextComponent is attached.
func (runtime *Runtime) hostXTextFieldFallback(vendor *skvmState, current *jvm.Object, displayRevision, menuRevision uint64) *backend.TextInput {
	kind := hostXTextField
	field, data := vendor.focusedXTextField()
	if field == nil {
		var ok bool
		kind, field = hostPaintedXTextField, runtime.textFieldPaints.only(current, displayRevision)
		if data, ok = nativeXTextField(field); !ok {
			return nil
		}
	}
	if data.constraints&textFieldUneditable != 0 {
		return nil
	}
	target := hostTextTarget{
		kind: kind, fallback: true, display: current, field: field, original: string(data.text),
		displayRevision: displayRevision, menuRevision: menuRevision, revision: data.textRevision,
		maxSize: data.maxSize, constraint: data.constraints,
	}
	return hostTextInput(runtime, target, data.maxSize, data.constraints, false)
}

// focusedXTextField answers the vendor field whose focus is on, or nil. The
// caller holds textMu and the vendor lock.
func (vendor *skvmState) focusedXTextField() (*jvm.Object, *xTextFieldData) {
	data, ok := nativeXTextField(vendor.focusedTextField)
	if !ok || !data.focus {
		return nil, nil
	}
	return vendor.focusedTextField, data
}

// xTextFieldPaints is the evidence for a vendor field nobody focused: which
// XTextField the title painted while the current Displayable was up. Any
// Graphics counts, the screen's or one a title draws its back buffer through,
// because either way the title drew the field to show it. It holds one field,
// and whether a second was painted too, rather than a list: the Host offers a
// field only when it is the one, and an archive that paints any number of
// fields cannot grow it. The record belongs to one display revision and
// starts over with the first paint after the Displayable changes; a focus
// call empties it, as it clears a focused field's screen evidence. Until then
// a second field keeps it ambiguous, even after the first is no longer drawn,
// because a paint does not say what it replaced. Guarded by textMu.
type xTextFieldPaints struct {
	display  *jvm.Object
	revision uint64
	field    *jvm.Object
	several  bool
}

// note records one paint of field while display was current at revision.
func (paints *xTextFieldPaints) note(field, display *jvm.Object, revision uint64) {
	switch {
	case paints.field == nil || paints.display != display || paints.revision != revision:
		*paints = xTextFieldPaints{display: display, revision: revision, field: field}
	case paints.field != field:
		paints.several = true
	}
}

// only answers the one field painted while display was current at revision,
// or nil when none or several were.
func (paints *xTextFieldPaints) only(display *jvm.Object, revision uint64) *jvm.Object {
	if paints.several || paints.display != display || paints.revision != revision {
		return nil
	}
	return paints.field
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
	if _, ok := backend.EncodeKSC5601(text); !ok {
		return backend.ErrInvalidTextInput
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
	case hostXTextField, hostPaintedXTextField:
		err = runtime.commitXTextField(target, text)
	case hostTextComponent:
		err = runtime.commitTextComponent(ctx, target, text)
	default:
		err = backend.ErrTextInputChanged
	}
	if err != nil {
		return err
	}
	if target.kind == hostXTextField || target.kind == hostPaintedXTextField || target.kind == hostTextComponent {
		if err := runtime.redrawTitleField(); err != nil {
			return err
		}
	}
	if err := runtime.runEvents(); err != nil {
		return err
	}
	return runtime.presentRefresh()
}

// hostTextRedrawKey is the key a title is handed after a Host commit to a
// field it draws itself. No handset key has the code, so neither vendor input
// method edits with it, and a title's own handling finds nothing to act on.
const hostTextRedrawKey int32 = 0

// redrawTitleField hands the title one key after its field changed without
// one. A title draws such a field in its own key handling, after passing the
// key to the field: one local title's TextComponent.repaint is empty, and
// another's paint draws the field only on the pass its key handler asked
// for. Repainting the Canvas left the committed text off screen until the
// player pressed something. The key goes through the title's own path, so a
// title that queues keys for a game thread redraws when that thread gets it.
func (runtime *Runtime) redrawTitleField() error {
	for _, kind := range []KeyEventType{KeyPressed, KeyReleased} {
		if err := runtime.deliverKeyLocked(kind, hostTextRedrawKey); err != nil {
			return err
		}
	}
	return nil
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
	if target.fallback && runtime.textComponentAttached() {
		runtime.textMu.Unlock()
		return backend.ErrTextInputChanged
	}
	state := runtime.lcdui()
	state.mu.Lock()
	display := state.displayables[target.display]
	if display == nil || display.menuOpen || display.screen == nil || display.screen.kind != screenForm {
		state.mu.Unlock()
		runtime.textMu.Unlock()
		return backend.ErrTextInputChanged
	}
	form := display.screen
	// The field has to be the one the same rule picks now: the selected item,
	// or with the selection elsewhere the Form's only editable field. Another
	// field made editable since then leaves the fallback with two.
	chosen, _, selected := formTextFieldForHost(form)
	byFallback := !selected
	if form.textRevision != target.screenRevision || chosen != target.field || byFallback != target.fallback {
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
	if !ok || data.textRevision != target.revision || !runtime.hostXTextFieldStillChosen(state, target, data) ||
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

// hostXTextFieldStillChosen reports whether the rule that chose a vendor field
// for a Host edit would choose it again. A focused field the strict route
// placed must still be shown; one the fallback offered needs only its focus,
// because the Displayable it was offered on is still current and is a Canvas.
// The field painted with nothing focused must still be the only one painted.
// Either fallback also needs the vendor input method to be without a
// component, as it was when the fallback was asked. The caller holds textMu
// and the vendor lock.
func (runtime *Runtime) hostXTextFieldStillChosen(vendor *skvmState, target hostTextTarget, data *xTextFieldData) bool {
	if target.fallback && vendor.textInput.component != nil {
		return false
	}
	focused, _ := vendor.focusedXTextField()
	if target.kind == hostPaintedXTextField {
		return focused == nil && runtime.textFieldPaints.only(target.display, target.displayRevision) == target.field
	}
	return focused == target.field && (target.fallback || data.visibleOn(target.display, target.displayRevision))
}

// textComponentAttached reports whether the vendor input method has a
// component to type into. The caller holds textMu.
func (runtime *Runtime) textComponentAttached() bool {
	vendor := runtime.skvm()
	vendor.mu.Lock()
	defer vendor.mu.Unlock()
	return vendor.textInput.component != nil
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
