package ktf

import (
	"context"
	"slices"
	"unicode/utf16"

	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/jvm"
	"github.com/movingwoo/wfeature/internal/textinput"
)

const (
	componentConstraintField = "constraint:I"

	textConstraintAny          int32 = 0
	textConstraintNumber       int32 = 1
	textConstraintPassword     int32 = 2
	textConstraintEmailAddress int32 = 3
	textConstraintURL          int32 = 4
	textConstraintPhoneNumber  int32 = 5
)

// TextInput snapshots the active Java or C editor for a Host that composes text with
// its native keyboard or IME; resolveTextInputLocked decides which editor that
// is. Guest execution and this snapshot share the client run lock.
func (session *Session) TextInput(ctx context.Context) (*backend.TextInput, error) {
	if session == nil || session.Client == nil {
		return nil, backend.ErrNoTextInput
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	client := session.Client
	client.Run.Lock()
	defer client.Run.Unlock()
	if client.runtime == nil || client.workersStopped {
		return nil, backend.ErrNoTextInput
	}
	target, ok := client.resolveTextInputLocked(textInputResolution{sync: true})
	if !ok {
		return nil, backend.ErrNoTextInput
	}
	switch target.position.route() {
	case routeTitleField:
		return client.titleFieldTextInput(target), nil
	case routeCInput:
		return client.cTextInput(target), nil
	}
	return client.componentTextInput(target), nil
}

// Where a Host composition goes.
//
// Six routes can name an editor, in this order of preference: a field the
// title runs through its own InputMethodHandler, which takes the keys the title
// is handing it right now and is stronger evidence of a field on screen than
// any focus record; the sole listened GTextField of the shown vendor form; the
// LWC component the guest gave explicit focus; a shown shell's text field; a
// field the title draws itself (see drawnTextComponent); and the C input
// method.
//
// Every route is asked twice. The first pass holds each route to the rule it
// has always had, so whatever those rules pick they still pick, ahead of
// anything else. The second pass asks the routes again, in the same order,
// under relaxed rules, and only when the first found nothing. In either pass a
// route whose candidate cannot take the edit — a stale focus on a button, a
// field behind a hidden shell — hands the question to the next route. The
// first rules used to end the question there instead, so handing on only adds
// answers where they had none. A field the player cannot type into is the
// failure that matters; a screen offered input it has no field for costs only
// a commit that fails.
//
// A commit asks again and must be answered with the same editor. A route ahead
// of the one that answered now naming another editor is a change — text never
// lands in one editor while a stricter route names another — but a route that
// still names nothing is not, so the stale state of another route cannot spend
// an edit it had no part in.
type textInputRoute int

const (
	routeTitleField textInputRoute = iota
	routeVendorForm
	routeFocus
	routeShell
	routeDrawn
	routeCInput
	textInputRoutes
)

// textInputPosition is one route in one pass: the first pass's routes in
// order, then the second pass's.
type textInputPosition int

const textInputPositions = 2 * textInputPosition(textInputRoutes)

func (position textInputPosition) route() textInputRoute {
	return textInputRoute(int(position) % int(textInputRoutes))
}

func (position textInputPosition) relaxed() bool {
	return int(position) >= int(textInputRoutes)
}

// textInputTarget is the editor one position names.
type textInputTarget struct {
	position textInputPosition
	// component is the LWC or vendor field a component route names, and
	// vendor the state of the form a vendor field was found in.
	component *jvm.Object
	vendor    vendorTextInputState
	// handler and card are a title's own field and the card it is on.
	handler, card *jvm.Object
}

// identifies reports whether two answers name the same editor, whichever
// position named each: a field is the same field through every route that
// can find it.
func (target textInputTarget) identifies(other textInputTarget) bool {
	route, otherRoute := target.position.route(), other.position.route()
	switch {
	case route == routeTitleField || otherRoute == routeTitleField:
		return route == otherRoute && target.handler == other.handler && target.card == other.card
	case route == routeCInput || otherRoute == routeCInput:
		return route == otherRoute
	}
	return target.component == other.component
}

// textInputResolution is how one question is asked.
type textInputResolution struct {
	// sync lets a snapshot bring a recognized C controller's recorded state
	// up to date, as it always has. A commit only reads.
	sync bool
	// drawn is the field a commit was offered through the drawn-field route,
	// and drawnFrom the position that offered it. From that position on the
	// route answers with it, whatever the paints since read: the evidence a
	// field is drawn is a read, and a later paint that did not read it is no
	// evidence that it has gone. The commit holds the field to its identity
	// and its values instead; a route ahead of drawnFrom still decides.
	drawn     *jvm.Object
	drawnFrom textInputPosition
}

// resolveTextInputLocked answers the first editor any position names. The
// caller holds client.run.
func (client *Client) resolveTextInputLocked(resolution textInputResolution) (textInputTarget, bool) {
	for position := textInputPosition(0); position < textInputPositions; position++ {
		if target, ok := client.textInputAt(position, resolution); ok {
			return target, true
		}
	}
	return textInputTarget{}, false
}

// textInputAt answers the editor one position names, when it can take a Host
// edit.
func (client *Client) textInputAt(position textInputPosition, resolution textInputResolution) (textInputTarget, bool) {
	runtime := client.runtime
	relaxed := position.relaxed()
	target := textInputTarget{position: position}
	switch position.route() {
	case routeTitleField:
		if relaxed {
			target.handler, target.card = runtime.offeredInputMethod()
		} else {
			target.handler, target.card = runtime.runtimeObjects[inputMethodOpenObject], runtime.runtimeObjects[inputMethodCardObject]
		}
		return target, client.titleFieldOffered(target.handler, target.card, relaxed)
	case routeVendorForm:
		var state vendorTextInputState
		var ok bool
		if relaxed {
			state, ok = runtime.relaxedVendorTextInput()
		} else {
			state, ok = runtime.activeVendorTextInput()
		}
		if !ok || !client.textInputComponentUsable(state.field, true, relaxed) {
			return target, false
		}
		target.component, target.vendor = state.field, state
		return target, true
	case routeFocus:
		target.component = runtime.runtimeObjects["lwc:focus"]
	case routeShell:
		if relaxed {
			target.component = client.relaxedShellTextComponent()
		} else {
			target.component = client.shellTextComponent()
		}
	case routeDrawn:
		switch {
		case resolution.drawn != nil && position >= resolution.drawnFrom:
			target.component = resolution.drawn
		case relaxed:
			target.component = client.relaxedDrawnTextComponent()
		default:
			target.component = runtime.drawnTextComponent()
		}
	case routeCInput:
		return target, client.cInputOffered(relaxed, resolution.sync)
	}
	return target, target.component != nil && client.textInputComponentUsable(target.component, false, relaxed)
}

// textInputComponentUsable is what every component route asks of the field it
// names: the kind of field that route edits, a constraint this Host can
// validate, no composition listener, and for an LWC field a place on the
// screen under the pass's ownership rule. A vendor field's place is its
// form's, which the vendor route has already checked.
func (client *Client) textInputComponentUsable(component *jvm.Object, vendorRoute, relaxed bool) bool {
	_, vendor, ok := client.lwcTextInputKind(component)
	if !ok || vendor != vendorRoute {
		return false
	}
	constraintValue, hasConstraint := component.Fields[componentConstraintField]
	if _, ok := lwcTextConstraint(constraintValue, hasConstraint); !ok {
		return false
	}
	// InputMethodListener consumes per-key composition deltas. The Host
	// supplies a completed field value, which cannot be represented by that
	// interface without a cursor or replacement range.
	if listenerState, ok := lwcTextInputListenerState(component); !ok || listenerState.listener != nil {
		return false
	}
	if !vendor {
		if _, visible := client.lwcTextOwnership(component, relaxed); !visible {
			return false
		}
	}
	return true
}

// componentTextInput offers an LWC or vendor field. The field's value is the
// one this platform holds, so the Host replaces it whole.
func (client *Client) componentTextInput(target textInputTarget) *backend.TextInput {
	runtime := client.runtime
	component := target.component
	multiline, vendor, _ := client.lwcTextInputKind(component)
	constraintValue, hasConstraint := component.Fields[componentConstraintField]
	constraint, _ := lwcTextConstraint(constraintValue, hasConstraint)
	listenerState, _ := lwcTextInputListenerState(component)
	textValue, hasText := component.Fields[componentTextField]
	focusRevision := component.Fields[componentFocusRevisionField]
	var ownership []lwcTextAncestor
	if !vendor {
		ownership, _ = client.lwcTextOwnership(component, true)
	}
	maxValue, hasMax := component.Fields[componentMaxLengthField]
	text := componentText(component)
	maxLength := int(componentMaxLength(component))
	inputMode, password := lwcTextInputHints(constraint)
	var resolution textInputResolution
	if target.position.route() == routeDrawn {
		resolution.drawn, resolution.drawnFrom = component, target.position
	}

	return &backend.TextInput{
		Text:      text,
		MaxLength: maxLength,
		Multiline: multiline,
		Password:  password,
		InputMode: inputMode,
		Commit: func(commitCtx context.Context, replacement string) error {
			if err := commitCtx.Err(); err != nil {
				return err
			}
			client.Run.Lock()
			defer client.Run.Unlock()
			if client.runtime != runtime || client.workersStopped {
				return backend.ErrTextInputChanged
			}
			current, ok := client.resolveTextInputLocked(resolution)
			if !ok || !current.identifies(target) || vendor && !sameVendorTextInputState(current.vendor, target.vendor) {
				return backend.ErrTextInputChanged
			}
			if !vendor {
				currentOwnership, visible := client.lwcTextOwnership(component, true)
				if !visible || !slices.Equal(ownership, currentOwnership) {
					return backend.ErrTextInputChanged
				}
			}
			currentListenerState, listenerStateOK := lwcTextInputListenerState(component)
			if component.Fields[componentFocusRevisionField] != focusRevision ||
				!listenerStateOK || !sameLWCTextInputListenerState(currentListenerState, listenerState) ||
				!sameTextInputField(component, componentTextField, textValue, hasText) ||
				!sameTextInputField(component, componentConstraintField, constraintValue, hasConstraint) ||
				!sameTextInputField(component, componentMaxLengthField, maxValue, hasMax) {
				return backend.ErrTextInputChanged
			}
			if err := validateLWCTextInput(replacement, constraint, maxLength, multiline); err != nil {
				return err
			}
			if replacement == text {
				return nil
			}
			component.Fields[componentTextField] = jvm.ReferenceValue(client.vm.NewString(replacement))
			setComponentCursor(component, len([]rune(replacement)))
			if err := runtime.publishTextComponent(component); err != nil {
				return err
			}
			// Keep the two keypad adapters in step if either is used after a
			// Host composition. Their next key starts a fresh composition.
			textEditorFor(component).SetText(replacement)
			client.textMu.Lock()
			if client.focusedText == component {
				client.textEditor = textinput.NewUTF16(replacement, maxLength)
			}
			client.textMu.Unlock()
			// No key reached the title, so nothing in it asks for the frame
			// a key would have: a card whose worker owns its cadence kept the
			// old text on screen until the next press. Request it the way a
			// component repaints after its contents change.
			runtime.RepaintPending = true
			runtime.PostRepaintEvent()
			return nil
		},
	}
}

// A field a title draws itself.
//
// One title's character-name screen keeps a `TextFieldComponent` subclass it
// built at start-up and told it had focus through `focusNotify(true)`, never
// `setFocus`, and never adds it to a container. Its card draws the box and the
// name, reading `getString` on every paint, and forwards keys to the field's
// own `keyNotify`. With no explicit focus and no shell there was nothing for
// the Host to offer, so the only way to give the character a name was the
// multi-tap keypad.
//
// The evidence that such a field is on screen is the read, not the focus:
// that title's field is focused from start-up, through every menu and the
// game itself, and its card reads the text only while the name screen is
// drawn. So the Host offers a field the top card read during its last paint
// and that was told it has focus, when that is exactly one field. Two focused
// fields drawn together are ambiguous and offer nothing.
//
// The relaxed rule, consulted after every route under its first rule found
// nothing, needs no focus from a field that is the only one read, and keeps
// what the last paint that read any text published: a frame that reads none,
// or a paint that fails, says nothing about a field the frame before it drew.
// It keeps it for the card that painted it, and only while that card is on
// top: a card pushed over it, or one that replaced it, draws its own screen.
// Several fields read and none of them focused still offer nothing.

// maxPaintTextReads bounds how many distinct text components one paint is
// tracked for. A paint that reads more is not a name screen anyone could
// type into, and the rule declines rather than tracking the rest.
const maxPaintTextReads = 16

// beginTextReads starts collecting the text components a card paint reads.
func (runtime *initializationRuntime) beginTextReads() {
	runtime.paintTextReads, runtime.paintTextOverflow = nil, false
}

// endTextReads publishes what a paint of card read once it returned. A paint
// that failed published nothing a Host could rely on. The relaxed rule's set
// changes only on a paint that read text, which also records the card, and is
// emptied by one that read more than a name screen does.
func (runtime *initializationRuntime) endTextReads(card *jvm.Object, painted bool) {
	runtime.paintedText = nil
	if painted && !runtime.paintTextOverflow {
		runtime.paintedText = runtime.paintTextReads
	}
	switch {
	case painted && runtime.paintTextOverflow:
		runtime.lastPaintedText, runtime.lastPaintedCard = nil, nil
	case len(runtime.paintedText) > 0:
		runtime.lastPaintedText, runtime.lastPaintedCard = runtime.paintedText, card
	}
	runtime.paintTextReads, runtime.paintTextOverflow = nil, false
}

// forgetRemovedCardText drops the relaxed rule's set once the card that
// painted it has left the display. It would never be offered again, since the
// rule asks for that card on top and a card pushed again paints again, and
// holding it would keep the card and its fields from the object collector.
func (runtime *initializationRuntime) forgetRemovedCardText() {
	if card := runtime.lastPaintedCard; card != nil && !slices.Contains(runtime.displayCards, card) {
		runtime.lastPaintedText, runtime.lastPaintedCard = nil, nil
	}
}

// noteTextRead records a text component read while a card paints.
func (runtime *initializationRuntime) noteTextRead(component *jvm.Object) {
	if runtime == nil || !runtime.RepaintServicing || runtime.paintTextOverflow || slices.Contains(runtime.paintTextReads, component) {
		return
	}
	if len(runtime.paintTextReads) == maxPaintTextReads {
		runtime.paintTextOverflow = true
		return
	}
	runtime.paintTextReads = append(runtime.paintTextReads, component)
}

// drawnTextComponent answers the one focused text component the top card read
// during its last paint, or nil.
func (runtime *initializationRuntime) drawnTextComponent() *jvm.Object {
	var found *jvm.Object
	for _, component := range runtime.paintedText {
		if focused, err := component.Fields[componentFocusedField].Int32(); err != nil || focused == 0 {
			continue
		}
		if found != nil {
			return nil
		}
		found = component
	}
	return found
}

// relaxedDrawnTextComponent answers the field the relaxed rule offers: of the
// fields in the last set a paint published, while the card that painted it is
// on top, the only focused one, or else the only one. Components that are not
// LWC fields — a button's label, a vendor field — are not fields a Host edit
// can go to here, and do not count.
func (client *Client) relaxedDrawnTextComponent() *jvm.Object {
	if card := client.runtime.lastPaintedCard; card == nil || card != client.runtime.topCard() {
		return nil
	}
	var fields, focused []*jvm.Object
	for _, component := range client.runtime.lastPaintedText {
		if _, vendor, ok := client.lwcTextInputKind(component); !ok || vendor {
			continue
		}
		fields = append(fields, component)
		if value, err := component.Fields[componentFocusedField].Int32(); err == nil && value != 0 {
			focused = append(focused, component)
		}
	}
	switch {
	case len(focused) == 1:
		return focused[0]
	case len(fields) == 1:
		return fields[0]
	}
	return nil
}

// lwcTextInputKind follows a guest subclass to the runtime LWC field or box it
// extends. A vendor field is marked separately because it is editable only
// through its shown form, which the vendor route validates
// (activeVendorTextInput, then relaxedVendorTextInput).
func (client *Client) lwcTextInputKind(component *jvm.Object) (multiline, vendor, ok bool) {
	if client == nil || client.vm == nil || component == nil {
		return false, false, false
	}
	name := component.ClassName
	seen := make(map[string]bool)
	for depth := 0; depth < 64 && name != "" && !seen[name]; depth++ {
		seen[name] = true
		switch name {
		case runtimeTextFieldComponentClass:
			return false, false, true
		case runtimeTextBoxComponentClass:
			return true, false, true
		case runtimeGTextFieldClass:
			return false, true, true
		case runtimeTextComponentClass:
			return false, false, false
		}
		class, found := client.vm.AOTClass(name)
		if !found {
			return false, false, false
		}
		name = class.SuperName
	}
	return false, false, false
}

type vendorTextInputState struct {
	form               *jvm.Object
	field              *jvm.Object
	visibilityRevision jvm.Value
	childrenRevision   jvm.Value
	textRevision       jvm.Value
	event              runtimeComponentEventState
}

func (runtime *initializationRuntime) activeVendorTextInput() (vendorTextInputState, bool) {
	if runtime == nil {
		return vendorTextInputState{}, false
	}
	form := runtime.runtimeObjects[runtimeKFCShownFormObject]
	field := runtime.runtimeObjects[runtimeKFCActiveFieldObject]
	if form == nil || field == nil || runtime.uniqueVendorTextField(form) != field || !componentShown(form) {
		return vendorTextInputState{}, false
	}
	event, ok := runtimeComponentEventListenerState(field)
	if !ok || event.listener == nil {
		return vendorTextInputState{}, false
	}
	return vendorTextInputState{
		form:               form,
		field:              field,
		visibilityRevision: form.Fields[componentKFCVisibilityRevisionField],
		childrenRevision:   form.Fields[componentChildrenRevisionField],
		textRevision:       field.Fields[componentKFCTextRevisionField],
		event:              event,
	}, true
}

func sameVendorTextInputState(left, right vendorTextInputState) bool {
	return left.form == right.form && left.field == right.field &&
		left.visibilityRevision == right.visibilityRevision &&
		left.childrenRevision == right.childrenRevision &&
		left.textRevision == right.textRevision &&
		sameRuntimeComponentEventState(left.event, right.event)
}

func lwcTextConstraint(value jvm.Value, present bool) (int32, bool) {
	if !present {
		return textConstraintAny, true
	}
	constraint, err := value.Int32()
	if err != nil || constraint < textConstraintAny || constraint > textConstraintPhoneNumber {
		return 0, false
	}
	return constraint, true
}

func lwcTextInputHints(constraint int32) (inputMode string, password bool) {
	switch constraint {
	case textConstraintNumber:
		return "numeric", false
	case textConstraintPassword:
		return "numeric", true
	case textConstraintEmailAddress:
		return "email", false
	case textConstraintURL:
		return "url", false
	case textConstraintPhoneNumber:
		return "tel", false
	default:
		return "text", false
	}
}

func validateLWCTextInput(text string, constraint int32, maxLength int, multiline bool) error {
	if err := backend.ValidateTextInput(text); err != nil {
		return err
	}
	if _, ok := backend.EncodeKSC5601(text); !ok {
		return backend.ErrInvalidTextInput
	}
	// WIPI counts Java char values. A supplementary Unicode code point is a
	// surrogate pair and consumes two positions in the guest field.
	if maxLength > 0 && len(utf16.Encode([]rune(text))) > maxLength {
		return backend.ErrInvalidTextInput
	}
	if !multiline {
		for _, symbol := range text {
			if symbol == '\r' || symbol == '\n' {
				return backend.ErrInvalidTextInput
			}
		}
	}
	valid := true
	switch constraint {
	case textConstraintAny:
	case textConstraintNumber:
		valid = everyTextRune(text, func(symbol rune) bool {
			return symbol >= '0' && symbol <= '9' || symbol == '-' || symbol == ' '
		})
	case textConstraintPassword, textConstraintPhoneNumber:
		valid = everyTextRune(text, func(symbol rune) bool { return symbol >= '0' && symbol <= '9' })
	case textConstraintEmailAddress, textConstraintURL:
		// The WIPI contract describes these as English letters, digits and
		// symbols. Printable ASCII is that character set; shape validation
		// belongs to the title because the contract specifies no grammar.
		valid = everyTextRune(text, func(symbol rune) bool { return symbol >= 0x21 && symbol <= 0x7e })
	default:
		valid = false
	}
	if !valid {
		return backend.ErrInvalidTextInput
	}
	return nil
}

func everyTextRune(text string, valid func(rune) bool) bool {
	for _, symbol := range text {
		if !valid(symbol) {
			return false
		}
	}
	return true
}

func sameTextInputField(component *jvm.Object, name string, value jvm.Value, present bool) bool {
	current, ok := component.Fields[name]
	return ok == present && (!ok || current == value)
}

type lwcInputListenerState struct {
	revision      jvm.Value
	handlerValue  jvm.Value
	hasHandler    bool
	handler       *jvm.Object
	listenerValue jvm.Value
	hasListener   bool
	listener      *jvm.Object
}

// lwcTextInputListenerState returns the exact handler and delta-listener field
// state so installing or removing one also invalidates an outstanding commit.
func lwcTextInputListenerState(component *jvm.Object) (lwcInputListenerState, bool) {
	state := lwcInputListenerState{}
	state.handlerValue, state.hasHandler = component.Fields[componentInputHandlerField]
	if !state.hasHandler {
		return state, true
	}
	handler, err := state.handlerValue.Reference()
	if err != nil {
		return lwcInputListenerState{}, false
	}
	if handler == nil {
		return state, true
	}
	state.handler = handler
	state.revision = handler.Fields[inputMethodListenerRevisionField]
	state.listenerValue, state.hasListener = handler.Fields[inputMethodListenerField]
	if !state.hasListener {
		return state, true
	}
	listener, err := state.listenerValue.Reference()
	if err != nil {
		return lwcInputListenerState{}, false
	}
	state.listener = listener
	return state, true
}

func sameLWCTextInputListenerState(left, right lwcInputListenerState) bool {
	return left.revision == right.revision &&
		left.handler == right.handler && left.hasHandler == right.hasHandler &&
		(!left.hasHandler || left.handlerValue == right.handlerValue) &&
		left.listener == right.listener && left.hasListener == right.hasListener &&
		(!left.hasListener || left.listenerValue == right.listenerValue)
}
