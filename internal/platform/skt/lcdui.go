package skt

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"unicode/utf16"

	"github.com/movingwoo/wfeature/internal/textinput"

	"github.com/movingwoo/wfeature/internal/api/midp"
	"github.com/movingwoo/wfeature/internal/jvm"
)

// Screen kinds. A Screen is drawn by the runtime rather than by the
// application, so the kind decides which renderer runs and how keys are read.
type screenKind uint8

const (
	screenNone screenKind = iota
	screenForm
	screenTextBox
	screenList
	screenAlert
)

// Item kinds inside a Form.
type itemKind uint8

const (
	itemString itemKind = iota
	itemImage
	itemChoice
	itemText
)

// Choice types from javax.microedition.lcdui.Choice.
const (
	choiceExclusive int32 = 1
	choiceMultiple  int32 = 2
	choiceImplicit  int32 = 3
	choicePopup     int32 = 4
)

// alertForever is Alert.FOREVER: the alert stays until dismissed.
const alertForever int32 = -2

// alertDefaultTimeout is what getDefaultTimeout answers and what an alert
// created without an explicit timeout uses. Real devices chose a couple of
// seconds; the runtime has no timer thread for screens, so this is the value
// reported rather than a deadline that fires.
const alertDefaultTimeout int32 = 2000

// commandData is one Command. Priority orders the soft keys: MIDP says lower
// is more important.
type commandData struct {
	label     string
	longLabel string
	kind      int32
	priority  int32
}

type choiceElement struct {
	text     string
	image    *jvm.Object
	selected bool
}

// choiceData is shared by ChoiceGroup (an Item) and List (a Screen), because
// the two differ in where they are shown, not in what they hold.
type choiceData struct {
	kind      int32
	elements  []choiceElement
	fitPolicy int32
}

// selectedIndex answers the single-selection index, or -1.
func (choice *choiceData) selectedIndex() int32 {
	for index, element := range choice.elements {
		if element.selected {
			return int32(index)
		}
	}
	return -1
}

// selectExclusive makes index the only selected element, which is what
// EXCLUSIVE, IMPLICIT, and POPUP all mean.
func (choice *choiceData) selectExclusive(index int) {
	for scan := range choice.elements {
		choice.elements[scan].selected = scan == index
	}
}

type itemData struct {
	kind       itemKind
	label      string
	layout     int32
	text       []rune
	maxSize    int32
	constraint int32
	appearance int32
	image      *jvm.Object
	altText    string
	font       *jvm.Object
	choice     *choiceData
	commands   []*jvm.Object
	listener   *jvm.Object
	owner      *jvm.Object
}

// screenData is the content of a runtime-drawn screen.
type screenData struct {
	kind          screenKind
	items         []*jvm.Object
	text          []rune
	maxSize       int32
	constraint    int32
	caret         int
	choice        *choiceData
	alertText     string
	alertImage    *jvm.Object
	alertType     *jvm.Object
	timeout       int32
	selectCommand *jvm.Object
	itemListener  *jvm.Object
	// selection is the focused row: an item index for a Form, an element
	// index for a List, and unused elsewhere.
	selection int
	// subSelection is the focused element inside the focused Form item when
	// that item is a ChoiceGroup.
	subSelection int
	scroll       int
	// input is the keypad editor a TextBox types through. It is created on
	// first use, because a screen the user never types into needs none.
	input *textinput.State
}

// displayableData is the part of a Displayable every kind has: its title, its
// ticker, its commands, and the listener they are reported to. It also holds
// the runtime-drawn screen content when the Displayable is a Screen, and the
// state of the command menu the runtime opens when there are more commands
// than soft keys.
type displayableData struct {
	title     string
	ticker    *jvm.Object
	commands  []*jvm.Object
	listener  *jvm.Object
	screen    *screenData
	menuOpen  bool
	menuIndex int
	// game is the GameCanvas buffer, present only for a GameCanvas.
	game *gameCanvasData
}

// gameCanvasData is a GameCanvas's off-screen buffer and its key state.
type gameCanvasData struct {
	buffer       *imageData
	graphics     *jvm.Object
	suppressKeys bool
	keyStates    int32
}

// lcduiState maps Displayables to the state above. Displayable's constructor
// is not native — an application subclasses Canvas and the runtime never sees
// the allocation — so the state is created on first use rather than at
// construction, and a map keyed by the object is the only place it can live.
type lcduiState struct {
	mu           sync.Mutex
	displayables map[*jvm.Object]*displayableData
}

func (runtime *Runtime) lcdui() *lcduiState {
	runtime.lcduiOnce.Do(func() {
		runtime.lcduiState = &lcduiState{displayables: make(map[*jvm.Object]*displayableData)}
	})
	return runtime.lcduiState
}

// displayableState returns the state of one Displayable, creating it on first
// use.
func (runtime *Runtime) displayableState(object *jvm.Object) *displayableData {
	state := runtime.lcdui()
	state.mu.Lock()
	defer state.mu.Unlock()
	data := state.displayables[object]
	if data == nil {
		data = &displayableData{}
		state.displayables[object] = data
	}
	return data
}

// screenState returns the screen content of a Displayable, creating it with
// the given kind if this is the first native that needs it. The kind comes
// from the calling method rather than from the class name because a game
// subclasses these freely.
func (runtime *Runtime) screenState(object *jvm.Object, kind screenKind) *screenData {
	data := runtime.displayableState(object)
	state := runtime.lcdui()
	state.mu.Lock()
	defer state.mu.Unlock()
	if data.screen == nil {
		data.screen = &screenData{kind: kind, timeout: alertDefaultTimeout, selection: 0}
	}
	if data.screen.kind == screenNone {
		data.screen.kind = kind
	}
	return data.screen
}

// isScreen reports whether the current Displayable is one the runtime draws.
func (runtime *Runtime) isScreen(object *jvm.Object) bool {
	if object == nil {
		return false
	}
	subclass, err := runtime.VM.IsSubclassOf(object.ClassName, midp.ScreenClass)
	return err == nil && subclass
}

// --- Command ---

func (runtime *Runtime) initCommand(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	receiver, err := referenceArgument(arguments, 0)
	if err != nil {
		return jvm.VoidValue(), err
	}
	label, err := optionalStringArgument(arguments, 1)
	if err != nil {
		return jvm.VoidValue(), err
	}
	longLabel, err := optionalStringArgument(arguments, 2)
	if err != nil {
		return jvm.VoidValue(), err
	}
	kind, err := intArgument(arguments, 3)
	if err != nil {
		return jvm.VoidValue(), err
	}
	priority, err := intArgument(arguments, 4)
	if err != nil {
		return jvm.VoidValue(), err
	}
	if kind < midp.CommandScreen || kind > midp.CommandItem {
		return jvm.VoidValue(), newGuestException("java/lang/IllegalArgumentException",
			fmt.Sprintf("command type %d", kind))
	}
	receiver.Native = &commandData{label: label, longLabel: longLabel, kind: kind, priority: priority}
	return jvm.VoidValue(), nil
}

func commandArgument(arguments []jvm.Value, index int) (*commandData, error) {
	object, err := referenceArgument(arguments, index)
	if err != nil {
		return nil, err
	}
	if object == nil {
		return nil, newGuestException("java/lang/NullPointerException", "Command is null")
	}
	data, ok := object.Native.(*commandData)
	if !ok || data == nil {
		return nil, fmt.Errorf("argument %d is not a Command", index)
	}
	return data, nil
}

func commandOf(object *jvm.Object) *commandData {
	if object == nil {
		return nil
	}
	data, _ := object.Native.(*commandData)
	return data
}

func (runtime *Runtime) commandLabel(vm *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	data, err := commandArgument(arguments, 0)
	if err != nil {
		return jvm.VoidValue(), err
	}
	return jvm.ReferenceValue(vm.NewString(data.label)), nil
}

func (runtime *Runtime) commandLongLabel(vm *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	data, err := commandArgument(arguments, 0)
	if err != nil {
		return jvm.VoidValue(), err
	}
	if data.longLabel == "" {
		return jvm.ReferenceValue(nil), nil
	}
	return jvm.ReferenceValue(vm.NewString(data.longLabel)), nil
}

func (runtime *Runtime) commandType(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	data, err := commandArgument(arguments, 0)
	if err != nil {
		return jvm.VoidValue(), err
	}
	return jvm.IntValue(data.kind), nil
}

func (runtime *Runtime) commandPriority(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	data, err := commandArgument(arguments, 0)
	if err != nil {
		return jvm.VoidValue(), err
	}
	return jvm.IntValue(data.priority), nil
}

// --- Ticker ---

func (runtime *Runtime) initTicker(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	receiver, err := referenceArgument(arguments, 0)
	if err != nil {
		return jvm.VoidValue(), err
	}
	text, err := stringArgument(arguments, 1)
	if err != nil {
		return jvm.VoidValue(), err
	}
	receiver.Native = &text
	return jvm.VoidValue(), nil
}

func tickerText(object *jvm.Object) string {
	if object == nil {
		return ""
	}
	text, ok := object.Native.(*string)
	if !ok || text == nil {
		return ""
	}
	return *text
}

func (runtime *Runtime) tickerString(vm *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	receiver, err := referenceArgument(arguments, 0)
	if err != nil {
		return jvm.VoidValue(), err
	}
	return jvm.ReferenceValue(vm.NewString(tickerText(receiver))), nil
}

func (runtime *Runtime) setTickerString(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	receiver, err := referenceArgument(arguments, 0)
	if err != nil {
		return jvm.VoidValue(), err
	}
	text, err := stringArgument(arguments, 1)
	if err != nil {
		return jvm.VoidValue(), err
	}
	receiver.Native = &text
	return jvm.VoidValue(), runtime.refreshCurrentScreen(nil)
}

// --- Displayable commands, title, ticker ---

func (runtime *Runtime) addDisplayableCommand(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	receiver, err := referenceArgument(arguments, 0)
	if err != nil {
		return jvm.VoidValue(), err
	}
	command, err := referenceArgument(arguments, 1)
	if err != nil {
		return jvm.VoidValue(), err
	}
	if command == nil {
		return jvm.VoidValue(), newGuestException("java/lang/NullPointerException", "Command is null")
	}
	if commandOf(command) == nil {
		return jvm.VoidValue(), fmt.Errorf("argument 1 is not a Command")
	}
	data := runtime.displayableState(receiver)
	state := runtime.lcdui()
	state.mu.Lock()
	for _, existing := range data.commands {
		if existing == command {
			state.mu.Unlock()
			return jvm.VoidValue(), nil
		}
	}
	data.commands = append(data.commands, command)
	sortCommands(data.commands)
	state.mu.Unlock()
	return jvm.VoidValue(), runtime.refreshCurrentScreen(receiver)
}

func (runtime *Runtime) removeDisplayableCommand(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	receiver, err := referenceArgument(arguments, 0)
	if err != nil {
		return jvm.VoidValue(), err
	}
	command, err := referenceArgument(arguments, 1)
	if err != nil || command == nil {
		return jvm.VoidValue(), err
	}
	data := runtime.displayableState(receiver)
	state := runtime.lcdui()
	state.mu.Lock()
	remaining := data.commands[:0]
	for _, existing := range data.commands {
		if existing != command {
			remaining = append(remaining, existing)
		}
	}
	data.commands = remaining
	state.mu.Unlock()
	return jvm.VoidValue(), runtime.refreshCurrentScreen(receiver)
}

func (runtime *Runtime) setDisplayableCommandListener(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	receiver, err := referenceArgument(arguments, 0)
	if err != nil {
		return jvm.VoidValue(), err
	}
	listener, err := referenceArgument(arguments, 1)
	if err != nil {
		return jvm.VoidValue(), err
	}
	data := runtime.displayableState(receiver)
	state := runtime.lcdui()
	state.mu.Lock()
	data.listener = listener
	state.mu.Unlock()
	return jvm.VoidValue(), nil
}

func (runtime *Runtime) displayableTitle(vm *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	receiver, err := referenceArgument(arguments, 0)
	if err != nil {
		return jvm.VoidValue(), err
	}
	data := runtime.displayableState(receiver)
	state := runtime.lcdui()
	state.mu.Lock()
	title := data.title
	state.mu.Unlock()
	if title == "" {
		return jvm.ReferenceValue(nil), nil
	}
	return jvm.ReferenceValue(vm.NewString(title)), nil
}

func (runtime *Runtime) setDisplayableTitle(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	receiver, err := referenceArgument(arguments, 0)
	if err != nil {
		return jvm.VoidValue(), err
	}
	title, err := optionalStringArgument(arguments, 1)
	if err != nil {
		return jvm.VoidValue(), err
	}
	data := runtime.displayableState(receiver)
	state := runtime.lcdui()
	state.mu.Lock()
	data.title = title
	state.mu.Unlock()
	return jvm.VoidValue(), runtime.refreshCurrentScreen(receiver)
}

func (runtime *Runtime) displayableTicker(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	receiver, err := referenceArgument(arguments, 0)
	if err != nil {
		return jvm.VoidValue(), err
	}
	data := runtime.displayableState(receiver)
	state := runtime.lcdui()
	state.mu.Lock()
	ticker := data.ticker
	state.mu.Unlock()
	return jvm.ReferenceValue(ticker), nil
}

func (runtime *Runtime) setDisplayableTicker(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	receiver, err := referenceArgument(arguments, 0)
	if err != nil {
		return jvm.VoidValue(), err
	}
	ticker, err := referenceArgument(arguments, 1)
	if err != nil {
		return jvm.VoidValue(), err
	}
	data := runtime.displayableState(receiver)
	state := runtime.lcdui()
	state.mu.Lock()
	data.ticker = ticker
	state.mu.Unlock()
	return jvm.VoidValue(), runtime.refreshCurrentScreen(receiver)
}

// sortCommands orders commands the way the soft keys present them: MIDP says
// a lower priority number is more important, and equal priorities keep the
// order they were added in, so the sort is stable.
func sortCommands(commands []*jvm.Object) {
	sort.SliceStable(commands, func(left, right int) bool {
		leftData, rightData := commandOf(commands[left]), commandOf(commands[right])
		if leftData == nil || rightData == nil {
			return false
		}
		return leftData.priority < rightData.priority
	})
}

// --- Item ---

func (runtime *Runtime) initItem(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	receiver, err := referenceArgument(arguments, 0)
	if err != nil {
		return jvm.VoidValue(), err
	}
	if _, ok := receiver.Native.(*itemData); !ok {
		receiver.Native = &itemData{}
	}
	return jvm.VoidValue(), nil
}

// itemOf returns an Item's state, creating it when a subclass constructor
// reaches a setter before Item's own constructor has run.
func itemOf(object *jvm.Object) (*itemData, error) {
	if object == nil {
		return nil, newGuestException("java/lang/NullPointerException", "Item is null")
	}
	data, ok := object.Native.(*itemData)
	if !ok || data == nil {
		data = &itemData{}
		object.Native = data
	}
	return data, nil
}

func itemArgument(arguments []jvm.Value, index int) (*jvm.Object, *itemData, error) {
	object, err := referenceArgument(arguments, index)
	if err != nil {
		return nil, nil, err
	}
	data, err := itemOf(object)
	if err != nil {
		return nil, nil, err
	}
	return object, data, nil
}

func (runtime *Runtime) itemLabel(vm *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	_, data, err := itemArgument(arguments, 0)
	if err != nil {
		return jvm.VoidValue(), err
	}
	if data.label == "" {
		return jvm.ReferenceValue(nil), nil
	}
	return jvm.ReferenceValue(vm.NewString(data.label)), nil
}

func (runtime *Runtime) setItemLabel(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	object, data, err := itemArgument(arguments, 0)
	if err != nil {
		return jvm.VoidValue(), err
	}
	label, err := optionalStringArgument(arguments, 1)
	if err != nil {
		return jvm.VoidValue(), err
	}
	data.label = label
	return jvm.VoidValue(), runtime.refreshItemOwner(object, data)
}

func (runtime *Runtime) itemLayout(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	_, data, err := itemArgument(arguments, 0)
	if err != nil {
		return jvm.VoidValue(), err
	}
	return jvm.IntValue(data.layout), nil
}

func (runtime *Runtime) setItemLayout(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	object, data, err := itemArgument(arguments, 0)
	if err != nil {
		return jvm.VoidValue(), err
	}
	layout, err := intArgument(arguments, 1)
	if err != nil {
		return jvm.VoidValue(), err
	}
	data.layout = layout
	return jvm.VoidValue(), runtime.refreshItemOwner(object, data)
}

func (runtime *Runtime) addItemCommand(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	object, data, err := itemArgument(arguments, 0)
	if err != nil {
		return jvm.VoidValue(), err
	}
	command, err := referenceArgument(arguments, 1)
	if err != nil {
		return jvm.VoidValue(), err
	}
	if command == nil || commandOf(command) == nil {
		return jvm.VoidValue(), newGuestException("java/lang/NullPointerException", "Command is null")
	}
	for _, existing := range data.commands {
		if existing == command {
			return jvm.VoidValue(), nil
		}
	}
	data.commands = append(data.commands, command)
	sortCommands(data.commands)
	return jvm.VoidValue(), runtime.refreshItemOwner(object, data)
}

func (runtime *Runtime) removeItemCommand(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	object, data, err := itemArgument(arguments, 0)
	if err != nil {
		return jvm.VoidValue(), err
	}
	command, err := referenceArgument(arguments, 1)
	if err != nil || command == nil {
		return jvm.VoidValue(), err
	}
	remaining := data.commands[:0]
	for _, existing := range data.commands {
		if existing != command {
			remaining = append(remaining, existing)
		}
	}
	data.commands = remaining
	return jvm.VoidValue(), runtime.refreshItemOwner(object, data)
}

func (runtime *Runtime) setItemDefaultCommand(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	object, data, err := itemArgument(arguments, 0)
	if err != nil {
		return jvm.VoidValue(), err
	}
	command, err := referenceArgument(arguments, 1)
	if err != nil {
		return jvm.VoidValue(), err
	}
	if command == nil {
		return jvm.VoidValue(), nil
	}
	// The default command is the one the selection key activates, which this
	// runtime represents by putting it first.
	remaining := []*jvm.Object{command}
	for _, existing := range data.commands {
		if existing != command {
			remaining = append(remaining, existing)
		}
	}
	data.commands = remaining
	return jvm.VoidValue(), runtime.refreshItemOwner(object, data)
}

func (runtime *Runtime) setItemCommandListener(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	_, data, err := itemArgument(arguments, 0)
	if err != nil {
		return jvm.VoidValue(), err
	}
	listener, err := referenceArgument(arguments, 1)
	if err != nil {
		return jvm.VoidValue(), err
	}
	data.listener = listener
	return jvm.VoidValue(), nil
}

func (runtime *Runtime) itemPreferredWidth(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	_, data, err := itemArgument(arguments, 0)
	if err != nil {
		return jvm.VoidValue(), err
	}
	width, _ := runtime.itemExtent(data)
	return jvm.IntValue(int32(width)), nil
}

func (runtime *Runtime) itemPreferredHeight(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	_, data, err := itemArgument(arguments, 0)
	if err != nil {
		return jvm.VoidValue(), err
	}
	_, height := runtime.itemExtent(data)
	return jvm.IntValue(int32(height)), nil
}

// notifyItemStateChanged reports an item edit to the Form's listener. MIDP
// says the application calls this after changing an interactive item itself.
func (runtime *Runtime) notifyItemStateChanged(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	object, data, err := itemArgument(arguments, 0)
	if err != nil {
		return jvm.VoidValue(), err
	}
	return jvm.VoidValue(), runtime.reportItemStateChange(object, data)
}

// reportItemStateChange calls the owning Form's ItemStateListener.
func (runtime *Runtime) reportItemStateChange(item *jvm.Object, data *itemData) error {
	if data.owner == nil {
		return nil
	}
	form := runtime.screenState(data.owner, screenForm)
	state := runtime.lcdui()
	state.mu.Lock()
	listener := form.itemListener
	state.mu.Unlock()
	if listener == nil {
		return nil
	}
	if _, err := runtime.VM.InvokeVirtual(listener, "itemStateChanged",
		"(Ljavax/microedition/lcdui/Item;)V", jvm.ReferenceValue(item)); err != nil {
		return fmt.Errorf("deliver itemStateChanged: %w", err)
	}
	return nil
}

// refreshItemOwner repaints the Form an item belongs to, if it is on screen.
func (runtime *Runtime) refreshItemOwner(_ *jvm.Object, data *itemData) error {
	if data.owner == nil {
		return nil
	}
	return runtime.refreshCurrentScreen(data.owner)
}

// --- StringItem / ImageItem ---

func (runtime *Runtime) initStringItem(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	_, data, err := itemArgument(arguments, 0)
	if err != nil {
		return jvm.VoidValue(), err
	}
	text, err := optionalStringArgument(arguments, 1)
	if err != nil {
		return jvm.VoidValue(), err
	}
	appearance, err := intArgument(arguments, 2)
	if err != nil {
		return jvm.VoidValue(), err
	}
	data.kind = itemString
	data.text = []rune(text)
	data.appearance = appearance
	return jvm.VoidValue(), nil
}

func (runtime *Runtime) stringItemText(vm *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	_, data, err := itemArgument(arguments, 0)
	if err != nil {
		return jvm.VoidValue(), err
	}
	if len(data.text) == 0 {
		return jvm.ReferenceValue(nil), nil
	}
	return jvm.ReferenceValue(vm.NewString(string(data.text))), nil
}

func (runtime *Runtime) setStringItemText(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	object, data, err := itemArgument(arguments, 0)
	if err != nil {
		return jvm.VoidValue(), err
	}
	text, err := optionalStringArgument(arguments, 1)
	if err != nil {
		return jvm.VoidValue(), err
	}
	data.text = []rune(text)
	return jvm.VoidValue(), runtime.refreshItemOwner(object, data)
}

func (runtime *Runtime) stringItemAppearance(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	_, data, err := itemArgument(arguments, 0)
	if err != nil {
		return jvm.VoidValue(), err
	}
	return jvm.IntValue(data.appearance), nil
}

func (runtime *Runtime) setItemFont(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	object, data, err := itemArgument(arguments, 0)
	if err != nil {
		return jvm.VoidValue(), err
	}
	font, err := referenceArgument(arguments, 1)
	if err != nil {
		return jvm.VoidValue(), err
	}
	data.font = font
	return jvm.VoidValue(), runtime.refreshItemOwner(object, data)
}

func (runtime *Runtime) itemFont(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	_, data, err := itemArgument(arguments, 0)
	if err != nil {
		return jvm.VoidValue(), err
	}
	if data.font == nil {
		return jvm.ReferenceValue(runtime.fontObject(fontSystem, fontPlain, fontMedium)), nil
	}
	return jvm.ReferenceValue(data.font), nil
}

func (runtime *Runtime) initImageItem(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	_, data, err := itemArgument(arguments, 0)
	if err != nil {
		return jvm.VoidValue(), err
	}
	image, err := referenceArgument(arguments, 1)
	if err != nil {
		return jvm.VoidValue(), err
	}
	altText, err := optionalStringArgument(arguments, 2)
	if err != nil {
		return jvm.VoidValue(), err
	}
	data.kind = itemImage
	data.image = image
	data.altText = altText
	return jvm.VoidValue(), nil
}

func (runtime *Runtime) imageItemImage(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	_, data, err := itemArgument(arguments, 0)
	if err != nil {
		return jvm.VoidValue(), err
	}
	return jvm.ReferenceValue(data.image), nil
}

func (runtime *Runtime) setImageItemImage(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	object, data, err := itemArgument(arguments, 0)
	if err != nil {
		return jvm.VoidValue(), err
	}
	image, err := referenceArgument(arguments, 1)
	if err != nil {
		return jvm.VoidValue(), err
	}
	data.image = image
	return jvm.VoidValue(), runtime.refreshItemOwner(object, data)
}

func (runtime *Runtime) imageItemAltText(vm *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	_, data, err := itemArgument(arguments, 0)
	if err != nil {
		return jvm.VoidValue(), err
	}
	if data.altText == "" {
		return jvm.ReferenceValue(nil), nil
	}
	return jvm.ReferenceValue(vm.NewString(data.altText)), nil
}

func (runtime *Runtime) setImageItemAltText(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	_, data, err := itemArgument(arguments, 0)
	if err != nil {
		return jvm.VoidValue(), err
	}
	altText, err := optionalStringArgument(arguments, 1)
	if err != nil {
		return jvm.VoidValue(), err
	}
	data.altText = altText
	return jvm.VoidValue(), nil
}

// --- text content shared by TextField and TextBox ---

// textTarget is the editable text of either a TextField item or a TextBox
// screen. The two have identical text methods, so they share one
// implementation and differ only in where the characters live.
type textTarget struct {
	text       *[]rune
	maxSize    *int32
	constraint *int32
	caret      *int
	refresh    func() error
	notify     func() error
}

func (runtime *Runtime) textTargetArgument(arguments []jvm.Value, kind screenKind) (*textTarget, error) {
	object, err := referenceArgument(arguments, 0)
	if err != nil {
		return nil, err
	}
	if object == nil {
		return nil, newGuestException("java/lang/NullPointerException", "receiver is null")
	}
	if kind == screenTextBox {
		screen := runtime.screenState(object, screenTextBox)
		return &textTarget{
			text:       &screen.text,
			maxSize:    &screen.maxSize,
			constraint: &screen.constraint,
			caret:      &screen.caret,
			refresh:    func() error { return runtime.refreshCurrentScreen(object) },
			notify:     func() error { return nil },
		}, nil
	}
	data, err := itemOf(object)
	if err != nil {
		return nil, err
	}
	return &textTarget{
		text:       &data.text,
		maxSize:    &data.maxSize,
		constraint: &data.constraint,
		caret:      new(int),
		refresh:    func() error { return runtime.refreshItemOwner(object, data) },
		notify:     func() error { return runtime.reportItemStateChange(object, data) },
	}, nil
}

func (runtime *Runtime) initText(kind screenKind) jvm.NativeMethod {
	return func(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
		target, err := runtime.textTargetArgument(arguments, kind)
		if err != nil {
			return jvm.VoidValue(), err
		}
		text, err := optionalStringArgument(arguments, 1)
		if err != nil {
			return jvm.VoidValue(), err
		}
		maxSize, err := intArgument(arguments, 2)
		if err != nil {
			return jvm.VoidValue(), err
		}
		constraint, err := intArgument(arguments, 3)
		if err != nil {
			return jvm.VoidValue(), err
		}
		if maxSize <= 0 {
			return jvm.VoidValue(), newGuestException("java/lang/IllegalArgumentException",
				fmt.Sprintf("maxSize %d", maxSize))
		}
		runes := []rune(text)
		if int32(len(runes)) > maxSize {
			return jvm.VoidValue(), newGuestException("java/lang/IllegalArgumentException",
				"initial text is longer than maxSize")
		}
		if object, _ := referenceArgument(arguments, 0); object != nil {
			if data, ok := object.Native.(*itemData); ok {
				data.kind = itemText
			}
		}
		*target.text = runes
		*target.maxSize = maxSize
		*target.constraint = constraint
		*target.caret = len(runes)
		return jvm.VoidValue(), nil
	}
}

func (runtime *Runtime) textString(kind screenKind) jvm.NativeMethod {
	return func(vm *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
		target, err := runtime.textTargetArgument(arguments, kind)
		if err != nil {
			return jvm.VoidValue(), err
		}
		return jvm.ReferenceValue(vm.NewString(string(*target.text))), nil
	}
}

func (runtime *Runtime) setTextString(kind screenKind) jvm.NativeMethod {
	return func(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
		target, err := runtime.textTargetArgument(arguments, kind)
		if err != nil {
			return jvm.VoidValue(), err
		}
		text, err := optionalStringArgument(arguments, 1)
		if err != nil {
			return jvm.VoidValue(), err
		}
		runes := []rune(text)
		if int32(len(runes)) > *target.maxSize {
			return jvm.VoidValue(), newGuestException("java/lang/IllegalArgumentException",
				"text is longer than maxSize")
		}
		*target.text = runes
		*target.caret = len(runes)
		return jvm.VoidValue(), target.refresh()
	}
}

func (runtime *Runtime) textChars(kind screenKind) jvm.NativeMethod {
	return func(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
		target, err := runtime.textTargetArgument(arguments, kind)
		if err != nil {
			return jvm.VoidValue(), err
		}
		array, values, err := primitiveArrayArgument(arguments, 1, jvm.TypeChar)
		if err != nil {
			return jvm.VoidValue(), err
		}
		units := utf16.Encode(*target.text)
		if len(units) > len(values) {
			return jvm.VoidValue(), newGuestException("java/lang/ArrayIndexOutOfBoundsException",
				"destination array is shorter than the text")
		}
		copied := make([]jvm.Value, len(units))
		for index, unit := range units {
			copied[index] = jvm.IntValue(int32(unit))
		}
		if err := jvm.SetArrayRange(array, 0, copied); err != nil {
			return jvm.VoidValue(), err
		}
		return jvm.IntValue(int32(len(units))), nil
	}
}

func (runtime *Runtime) setTextChars(kind screenKind) jvm.NativeMethod {
	return func(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
		target, err := runtime.textTargetArgument(arguments, kind)
		if err != nil {
			return jvm.VoidValue(), err
		}
		units, err := characterArraySlice(arguments, 1, 2, 3)
		if err != nil {
			return jvm.VoidValue(), err
		}
		runes := utf16.Decode(units)
		if int32(len(runes)) > *target.maxSize {
			return jvm.VoidValue(), newGuestException("java/lang/IllegalArgumentException",
				"text is longer than maxSize")
		}
		*target.text = runes
		*target.caret = len(runes)
		return jvm.VoidValue(), target.refresh()
	}
}

func (runtime *Runtime) insertText(kind screenKind) jvm.NativeMethod {
	return func(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
		target, err := runtime.textTargetArgument(arguments, kind)
		if err != nil {
			return jvm.VoidValue(), err
		}
		text, err := stringArgument(arguments, 1)
		if err != nil {
			return jvm.VoidValue(), err
		}
		position, err := intArgument(arguments, 2)
		if err != nil {
			return jvm.VoidValue(), err
		}
		current := *target.text
		if position < 0 {
			position = 0
		}
		if int(position) > len(current) {
			position = int32(len(current))
		}
		inserted := []rune(text)
		if int32(len(current)+len(inserted)) > *target.maxSize {
			return jvm.VoidValue(), newGuestException("java/lang/IllegalArgumentException",
				"insertion would exceed maxSize")
		}
		combined := make([]rune, 0, len(current)+len(inserted))
		combined = append(combined, current[:position]...)
		combined = append(combined, inserted...)
		combined = append(combined, current[position:]...)
		*target.text = combined
		*target.caret = int(position) + len(inserted)
		return jvm.VoidValue(), target.refresh()
	}
}

func (runtime *Runtime) deleteText(kind screenKind) jvm.NativeMethod {
	return func(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
		target, err := runtime.textTargetArgument(arguments, kind)
		if err != nil {
			return jvm.VoidValue(), err
		}
		offset, err := intArgument(arguments, 1)
		if err != nil {
			return jvm.VoidValue(), err
		}
		length, err := intArgument(arguments, 2)
		if err != nil {
			return jvm.VoidValue(), err
		}
		current := *target.text
		if offset < 0 || length < 0 || int64(offset)+int64(length) > int64(len(current)) {
			return jvm.VoidValue(), newGuestException("java/lang/StringIndexOutOfBoundsException",
				fmt.Sprintf("delete %d..%d of %d", offset, offset+length, len(current)))
		}
		remaining := make([]rune, 0, len(current)-int(length))
		remaining = append(remaining, current[:offset]...)
		remaining = append(remaining, current[offset+length:]...)
		*target.text = remaining
		*target.caret = int(offset)
		return jvm.VoidValue(), target.refresh()
	}
}

func (runtime *Runtime) textSize(kind screenKind) jvm.NativeMethod {
	return func(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
		target, err := runtime.textTargetArgument(arguments, kind)
		if err != nil {
			return jvm.VoidValue(), err
		}
		return jvm.IntValue(int32(len(*target.text))), nil
	}
}

func (runtime *Runtime) textMaxSize(kind screenKind) jvm.NativeMethod {
	return func(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
		target, err := runtime.textTargetArgument(arguments, kind)
		if err != nil {
			return jvm.VoidValue(), err
		}
		return jvm.IntValue(*target.maxSize), nil
	}
}

func (runtime *Runtime) setTextMaxSize(kind screenKind) jvm.NativeMethod {
	return func(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
		target, err := runtime.textTargetArgument(arguments, kind)
		if err != nil {
			return jvm.VoidValue(), err
		}
		maxSize, err := intArgument(arguments, 1)
		if err != nil {
			return jvm.VoidValue(), err
		}
		if maxSize <= 0 {
			return jvm.VoidValue(), newGuestException("java/lang/IllegalArgumentException",
				fmt.Sprintf("maxSize %d", maxSize))
		}
		*target.maxSize = maxSize
		if int32(len(*target.text)) > maxSize {
			// MIDP truncates rather than refusing, and answers with the size
			// actually granted.
			*target.text = (*target.text)[:maxSize]
			*target.caret = int(maxSize)
		}
		return jvm.IntValue(maxSize), target.refresh()
	}
}

func (runtime *Runtime) textCaretPosition(kind screenKind) jvm.NativeMethod {
	return func(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
		target, err := runtime.textTargetArgument(arguments, kind)
		if err != nil {
			return jvm.VoidValue(), err
		}
		return jvm.IntValue(int32(*target.caret)), nil
	}
}

func (runtime *Runtime) textConstraints(kind screenKind) jvm.NativeMethod {
	return func(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
		target, err := runtime.textTargetArgument(arguments, kind)
		if err != nil {
			return jvm.VoidValue(), err
		}
		return jvm.IntValue(*target.constraint), nil
	}
}

func (runtime *Runtime) setTextConstraints(kind screenKind) jvm.NativeMethod {
	return func(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
		target, err := runtime.textTargetArgument(arguments, kind)
		if err != nil {
			return jvm.VoidValue(), err
		}
		constraint, err := intArgument(arguments, 1)
		if err != nil {
			return jvm.VoidValue(), err
		}
		*target.constraint = constraint
		return jvm.VoidValue(), target.refresh()
	}
}

// --- choice, shared by ChoiceGroup and List ---

func (runtime *Runtime) choiceOf(arguments []jvm.Value, kind screenKind) (*jvm.Object, *choiceData, error) {
	object, err := referenceArgument(arguments, 0)
	if err != nil {
		return nil, nil, err
	}
	if object == nil {
		return nil, nil, newGuestException("java/lang/NullPointerException", "receiver is null")
	}
	if kind == screenList {
		screen := runtime.screenState(object, screenList)
		if screen.choice == nil {
			screen.choice = &choiceData{kind: choiceImplicit}
		}
		return object, screen.choice, nil
	}
	data, err := itemOf(object)
	if err != nil {
		return nil, nil, err
	}
	if data.choice == nil {
		data.choice = &choiceData{kind: choiceExclusive}
	}
	data.kind = itemChoice
	return object, data.choice, nil
}

func (runtime *Runtime) refreshChoiceOwner(object *jvm.Object, kind screenKind) error {
	if kind == screenList {
		return runtime.refreshCurrentScreen(object)
	}
	data, err := itemOf(object)
	if err != nil {
		return nil
	}
	return runtime.refreshItemOwner(object, data)
}

func (runtime *Runtime) initChoice(kind screenKind) jvm.NativeMethod {
	return func(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
		_, choice, err := runtime.choiceOf(arguments, kind)
		if err != nil {
			return jvm.VoidValue(), err
		}
		choiceType, err := intArgument(arguments, 1)
		if err != nil {
			return jvm.VoidValue(), err
		}
		if choiceType < choiceExclusive || choiceType > choicePopup {
			return jvm.VoidValue(), newGuestException("java/lang/IllegalArgumentException",
				fmt.Sprintf("choice type %d", choiceType))
		}
		if kind == screenList && choiceType == choicePopup {
			return jvm.VoidValue(), newGuestException("java/lang/IllegalArgumentException",
				"a List cannot be POPUP")
		}
		choice.kind = choiceType
		return jvm.VoidValue(), nil
	}
}

func (runtime *Runtime) choiceSize(kind screenKind) jvm.NativeMethod {
	return func(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
		_, choice, err := runtime.choiceOf(arguments, kind)
		if err != nil {
			return jvm.VoidValue(), err
		}
		return jvm.IntValue(int32(len(choice.elements))), nil
	}
}

func choiceElementIndex(choice *choiceData, index int32, allowEnd bool) (int, error) {
	limit := len(choice.elements)
	if allowEnd {
		limit++
	}
	if index < 0 || int(index) >= limit {
		return 0, newGuestException("java/lang/IndexOutOfBoundsException",
			fmt.Sprintf("element %d of %d", index, len(choice.elements)))
	}
	return int(index), nil
}

func (runtime *Runtime) choiceString(kind screenKind) jvm.NativeMethod {
	return func(vm *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
		_, choice, err := runtime.choiceOf(arguments, kind)
		if err != nil {
			return jvm.VoidValue(), err
		}
		index, err := intArgument(arguments, 1)
		if err != nil {
			return jvm.VoidValue(), err
		}
		position, err := choiceElementIndex(choice, index, false)
		if err != nil {
			return jvm.VoidValue(), err
		}
		return jvm.ReferenceValue(vm.NewString(choice.elements[position].text)), nil
	}
}

func (runtime *Runtime) choiceImage(kind screenKind) jvm.NativeMethod {
	return func(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
		_, choice, err := runtime.choiceOf(arguments, kind)
		if err != nil {
			return jvm.VoidValue(), err
		}
		index, err := intArgument(arguments, 1)
		if err != nil {
			return jvm.VoidValue(), err
		}
		position, err := choiceElementIndex(choice, index, false)
		if err != nil {
			return jvm.VoidValue(), err
		}
		return jvm.ReferenceValue(choice.elements[position].image), nil
	}
}

func (runtime *Runtime) choiceAppend(kind screenKind) jvm.NativeMethod {
	return func(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
		object, choice, err := runtime.choiceOf(arguments, kind)
		if err != nil {
			return jvm.VoidValue(), err
		}
		text, err := stringArgument(arguments, 1)
		if err != nil {
			return jvm.VoidValue(), err
		}
		image, err := referenceArgument(arguments, 2)
		if err != nil {
			return jvm.VoidValue(), err
		}
		choice.elements = append(choice.elements, choiceElement{text: text, image: image})
		// The first element of a single-selection choice is selected, which is
		// what a device shows when a menu first appears.
		if choice.kind != choiceMultiple && choice.selectedIndex() < 0 {
			choice.selectExclusive(0)
		}
		return jvm.IntValue(int32(len(choice.elements) - 1)), runtime.refreshChoiceOwner(object, kind)
	}
}

func (runtime *Runtime) choiceInsert(kind screenKind) jvm.NativeMethod {
	return func(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
		object, choice, err := runtime.choiceOf(arguments, kind)
		if err != nil {
			return jvm.VoidValue(), err
		}
		index, err := intArgument(arguments, 1)
		if err != nil {
			return jvm.VoidValue(), err
		}
		position, err := choiceElementIndex(choice, index, true)
		if err != nil {
			return jvm.VoidValue(), err
		}
		text, err := stringArgument(arguments, 2)
		if err != nil {
			return jvm.VoidValue(), err
		}
		image, err := referenceArgument(arguments, 3)
		if err != nil {
			return jvm.VoidValue(), err
		}
		choice.elements = append(choice.elements, choiceElement{})
		copy(choice.elements[position+1:], choice.elements[position:])
		choice.elements[position] = choiceElement{text: text, image: image}
		if choice.kind != choiceMultiple && choice.selectedIndex() < 0 {
			choice.selectExclusive(0)
		}
		return jvm.VoidValue(), runtime.refreshChoiceOwner(object, kind)
	}
}

func (runtime *Runtime) choiceDelete(kind screenKind) jvm.NativeMethod {
	return func(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
		object, choice, err := runtime.choiceOf(arguments, kind)
		if err != nil {
			return jvm.VoidValue(), err
		}
		index, err := intArgument(arguments, 1)
		if err != nil {
			return jvm.VoidValue(), err
		}
		position, err := choiceElementIndex(choice, index, false)
		if err != nil {
			return jvm.VoidValue(), err
		}
		wasSelected := choice.elements[position].selected
		choice.elements = append(choice.elements[:position], choice.elements[position+1:]...)
		if wasSelected && choice.kind != choiceMultiple && len(choice.elements) > 0 {
			choice.selectExclusive(min(position, len(choice.elements)-1))
		}
		return jvm.VoidValue(), runtime.refreshChoiceOwner(object, kind)
	}
}

func (runtime *Runtime) choiceDeleteAll(kind screenKind) jvm.NativeMethod {
	return func(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
		object, choice, err := runtime.choiceOf(arguments, kind)
		if err != nil {
			return jvm.VoidValue(), err
		}
		choice.elements = nil
		return jvm.VoidValue(), runtime.refreshChoiceOwner(object, kind)
	}
}

func (runtime *Runtime) choiceSet(kind screenKind) jvm.NativeMethod {
	return func(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
		object, choice, err := runtime.choiceOf(arguments, kind)
		if err != nil {
			return jvm.VoidValue(), err
		}
		index, err := intArgument(arguments, 1)
		if err != nil {
			return jvm.VoidValue(), err
		}
		position, err := choiceElementIndex(choice, index, false)
		if err != nil {
			return jvm.VoidValue(), err
		}
		text, err := stringArgument(arguments, 2)
		if err != nil {
			return jvm.VoidValue(), err
		}
		image, err := referenceArgument(arguments, 3)
		if err != nil {
			return jvm.VoidValue(), err
		}
		choice.elements[position].text = text
		choice.elements[position].image = image
		return jvm.VoidValue(), runtime.refreshChoiceOwner(object, kind)
	}
}

func (runtime *Runtime) choiceIsSelected(kind screenKind) jvm.NativeMethod {
	return func(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
		_, choice, err := runtime.choiceOf(arguments, kind)
		if err != nil {
			return jvm.VoidValue(), err
		}
		index, err := intArgument(arguments, 1)
		if err != nil {
			return jvm.VoidValue(), err
		}
		position, err := choiceElementIndex(choice, index, false)
		if err != nil {
			return jvm.VoidValue(), err
		}
		if choice.elements[position].selected {
			return jvm.IntValue(1), nil
		}
		return jvm.IntValue(0), nil
	}
}

func (runtime *Runtime) choiceSelectedIndex(kind screenKind) jvm.NativeMethod {
	return func(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
		_, choice, err := runtime.choiceOf(arguments, kind)
		if err != nil {
			return jvm.VoidValue(), err
		}
		if choice.kind == choiceMultiple {
			// MULTIPLE has no single selection; MIDP answers -1.
			return jvm.IntValue(-1), nil
		}
		return jvm.IntValue(choice.selectedIndex()), nil
	}
}

func (runtime *Runtime) choiceSelectedFlags(kind screenKind) jvm.NativeMethod {
	return func(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
		_, choice, err := runtime.choiceOf(arguments, kind)
		if err != nil {
			return jvm.VoidValue(), err
		}
		array, values, err := primitiveArrayArgument(arguments, 1, jvm.TypeBoolean)
		if err != nil {
			return jvm.VoidValue(), err
		}
		if len(values) < len(choice.elements) {
			return jvm.VoidValue(), newGuestException("java/lang/IllegalArgumentException",
				"flag array is shorter than the choice")
		}
		flags := make([]jvm.Value, len(values))
		selected := int32(0)
		for index := range values {
			set := index < len(choice.elements) && choice.elements[index].selected
			if set {
				selected++
				flags[index] = jvm.IntValue(1)
				continue
			}
			flags[index] = jvm.IntValue(0)
		}
		if err := jvm.SetArrayRange(array, 0, flags); err != nil {
			return jvm.VoidValue(), err
		}
		return jvm.IntValue(selected), nil
	}
}

func (runtime *Runtime) choiceSetSelectedIndex(kind screenKind) jvm.NativeMethod {
	return func(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
		object, choice, err := runtime.choiceOf(arguments, kind)
		if err != nil {
			return jvm.VoidValue(), err
		}
		index, err := intArgument(arguments, 1)
		if err != nil {
			return jvm.VoidValue(), err
		}
		selected, err := booleanArgument(arguments, 2)
		if err != nil {
			return jvm.VoidValue(), err
		}
		position, err := choiceElementIndex(choice, index, false)
		if err != nil {
			return jvm.VoidValue(), err
		}
		if choice.kind == choiceMultiple {
			choice.elements[position].selected = selected
		} else if selected {
			choice.selectExclusive(position)
		}
		return jvm.VoidValue(), runtime.refreshChoiceOwner(object, kind)
	}
}

func (runtime *Runtime) choiceSetSelectedFlags(kind screenKind) jvm.NativeMethod {
	return func(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
		object, choice, err := runtime.choiceOf(arguments, kind)
		if err != nil {
			return jvm.VoidValue(), err
		}
		_, values, err := primitiveArrayArgument(arguments, 1, jvm.TypeBoolean)
		if err != nil {
			return jvm.VoidValue(), err
		}
		if len(values) < len(choice.elements) {
			return jvm.VoidValue(), newGuestException("java/lang/IllegalArgumentException",
				"flag array is shorter than the choice")
		}
		first := -1
		for index := range choice.elements {
			raw, valueErr := values[index].Int32()
			if valueErr != nil {
				return jvm.VoidValue(), valueErr
			}
			set := raw != 0
			choice.elements[index].selected = set
			if set && first < 0 {
				first = index
			}
		}
		if choice.kind != choiceMultiple {
			// A single-selection choice keeps exactly one; MIDP takes the
			// first set flag, or element 0 when none is set.
			if first < 0 && len(choice.elements) > 0 {
				first = 0
			}
			if first >= 0 {
				choice.selectExclusive(first)
			}
		}
		return jvm.VoidValue(), runtime.refreshChoiceOwner(object, kind)
	}
}

func (runtime *Runtime) choiceSetFitPolicy(kind screenKind) jvm.NativeMethod {
	return func(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
		_, choice, err := runtime.choiceOf(arguments, kind)
		if err != nil {
			return jvm.VoidValue(), err
		}
		policy, err := intArgument(arguments, 1)
		if err != nil {
			return jvm.VoidValue(), err
		}
		choice.fitPolicy = policy
		return jvm.VoidValue(), nil
	}
}

func (runtime *Runtime) choiceFitPolicy(kind screenKind) jvm.NativeMethod {
	return func(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
		_, choice, err := runtime.choiceOf(arguments, kind)
		if err != nil {
			return jvm.VoidValue(), err
		}
		return jvm.IntValue(choice.fitPolicy), nil
	}
}

// choiceElementFont answers the default font: this runtime draws every choice
// element in one face, and reporting a per-element font it does not honor
// would be a lie a game could lay out against.
func (runtime *Runtime) choiceElementFont(_ *jvm.VM, _ []jvm.Value) (jvm.Value, error) {
	return jvm.ReferenceValue(runtime.fontObject(fontSystem, fontPlain, fontMedium)), nil
}

func (runtime *Runtime) ignoreChoiceElementFont(_ *jvm.VM, _ []jvm.Value) (jvm.Value, error) {
	return jvm.VoidValue(), nil
}

// --- Form ---

func (runtime *Runtime) formAppend(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	receiver, err := referenceArgument(arguments, 0)
	if err != nil {
		return jvm.VoidValue(), err
	}
	item, data, err := itemArgument(arguments, 1)
	if err != nil {
		return jvm.VoidValue(), err
	}
	if err := runtime.claimItem(item, data, receiver); err != nil {
		return jvm.VoidValue(), err
	}
	form := runtime.screenState(receiver, screenForm)
	form.items = append(form.items, item)
	return jvm.IntValue(int32(len(form.items) - 1)), runtime.refreshCurrentScreen(receiver)
}

// claimItem enforces the MIDP rule that an Item belongs to at most one Form.
func (runtime *Runtime) claimItem(item *jvm.Object, data *itemData, form *jvm.Object) error {
	if data.owner != nil && data.owner != form {
		return newGuestException("java/lang/IllegalStateException", "Item is already owned by a Form")
	}
	data.owner = form
	_ = item
	return nil
}

func (runtime *Runtime) formInsert(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	receiver, err := referenceArgument(arguments, 0)
	if err != nil {
		return jvm.VoidValue(), err
	}
	index, err := intArgument(arguments, 1)
	if err != nil {
		return jvm.VoidValue(), err
	}
	item, data, err := itemArgument(arguments, 2)
	if err != nil {
		return jvm.VoidValue(), err
	}
	form := runtime.screenState(receiver, screenForm)
	if index < 0 || int(index) > len(form.items) {
		return jvm.VoidValue(), newGuestException("java/lang/IndexOutOfBoundsException",
			fmt.Sprintf("item %d of %d", index, len(form.items)))
	}
	if err := runtime.claimItem(item, data, receiver); err != nil {
		return jvm.VoidValue(), err
	}
	form.items = append(form.items, nil)
	copy(form.items[index+1:], form.items[index:])
	form.items[index] = item
	return jvm.VoidValue(), runtime.refreshCurrentScreen(receiver)
}

func (runtime *Runtime) formDelete(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	receiver, err := referenceArgument(arguments, 0)
	if err != nil {
		return jvm.VoidValue(), err
	}
	index, err := intArgument(arguments, 1)
	if err != nil {
		return jvm.VoidValue(), err
	}
	form := runtime.screenState(receiver, screenForm)
	if index < 0 || int(index) >= len(form.items) {
		return jvm.VoidValue(), newGuestException("java/lang/IndexOutOfBoundsException",
			fmt.Sprintf("item %d of %d", index, len(form.items)))
	}
	if data, itemErr := itemOf(form.items[index]); itemErr == nil {
		data.owner = nil
	}
	form.items = append(form.items[:index], form.items[index+1:]...)
	if form.selection >= len(form.items) {
		form.selection = max(len(form.items)-1, 0)
	}
	return jvm.VoidValue(), runtime.refreshCurrentScreen(receiver)
}

func (runtime *Runtime) formDeleteAll(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	receiver, err := referenceArgument(arguments, 0)
	if err != nil {
		return jvm.VoidValue(), err
	}
	form := runtime.screenState(receiver, screenForm)
	for _, item := range form.items {
		if data, itemErr := itemOf(item); itemErr == nil {
			data.owner = nil
		}
	}
	form.items = nil
	form.selection = 0
	return jvm.VoidValue(), runtime.refreshCurrentScreen(receiver)
}

func (runtime *Runtime) formSet(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	receiver, err := referenceArgument(arguments, 0)
	if err != nil {
		return jvm.VoidValue(), err
	}
	index, err := intArgument(arguments, 1)
	if err != nil {
		return jvm.VoidValue(), err
	}
	item, data, err := itemArgument(arguments, 2)
	if err != nil {
		return jvm.VoidValue(), err
	}
	form := runtime.screenState(receiver, screenForm)
	if index < 0 || int(index) >= len(form.items) {
		return jvm.VoidValue(), newGuestException("java/lang/IndexOutOfBoundsException",
			fmt.Sprintf("item %d of %d", index, len(form.items)))
	}
	if err := runtime.claimItem(item, data, receiver); err != nil {
		return jvm.VoidValue(), err
	}
	if previous, itemErr := itemOf(form.items[index]); itemErr == nil {
		previous.owner = nil
	}
	form.items[index] = item
	return jvm.VoidValue(), runtime.refreshCurrentScreen(receiver)
}

func (runtime *Runtime) formGet(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	receiver, err := referenceArgument(arguments, 0)
	if err != nil {
		return jvm.VoidValue(), err
	}
	index, err := intArgument(arguments, 1)
	if err != nil {
		return jvm.VoidValue(), err
	}
	form := runtime.screenState(receiver, screenForm)
	if index < 0 || int(index) >= len(form.items) {
		return jvm.VoidValue(), newGuestException("java/lang/IndexOutOfBoundsException",
			fmt.Sprintf("item %d of %d", index, len(form.items)))
	}
	return jvm.ReferenceValue(form.items[index]), nil
}

func (runtime *Runtime) formSize(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	receiver, err := referenceArgument(arguments, 0)
	if err != nil {
		return jvm.VoidValue(), err
	}
	form := runtime.screenState(receiver, screenForm)
	return jvm.IntValue(int32(len(form.items))), nil
}

func (runtime *Runtime) setFormItemStateListener(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	receiver, err := referenceArgument(arguments, 0)
	if err != nil {
		return jvm.VoidValue(), err
	}
	listener, err := referenceArgument(arguments, 1)
	if err != nil {
		return jvm.VoidValue(), err
	}
	form := runtime.screenState(receiver, screenForm)
	form.itemListener = listener
	return jvm.VoidValue(), nil
}

// --- List ---

func (runtime *Runtime) setListSelectCommand(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	receiver, err := referenceArgument(arguments, 0)
	if err != nil {
		return jvm.VoidValue(), err
	}
	command, err := referenceArgument(arguments, 1)
	if err != nil {
		return jvm.VoidValue(), err
	}
	screen := runtime.screenState(receiver, screenList)
	screen.selectCommand = command
	return jvm.VoidValue(), nil
}

// --- Alert ---

func (runtime *Runtime) initAlert(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	receiver, err := referenceArgument(arguments, 0)
	if err != nil {
		return jvm.VoidValue(), err
	}
	text, err := optionalStringArgument(arguments, 1)
	if err != nil {
		return jvm.VoidValue(), err
	}
	image, err := referenceArgument(arguments, 2)
	if err != nil {
		return jvm.VoidValue(), err
	}
	alertType, err := referenceArgument(arguments, 3)
	if err != nil {
		return jvm.VoidValue(), err
	}
	screen := runtime.screenState(receiver, screenAlert)
	screen.alertText = text
	screen.alertImage = image
	screen.alertType = alertType
	screen.timeout = alertDefaultTimeout
	return jvm.VoidValue(), nil
}

func (runtime *Runtime) alertString(vm *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	receiver, err := referenceArgument(arguments, 0)
	if err != nil {
		return jvm.VoidValue(), err
	}
	screen := runtime.screenState(receiver, screenAlert)
	if screen.alertText == "" {
		return jvm.ReferenceValue(nil), nil
	}
	return jvm.ReferenceValue(vm.NewString(screen.alertText)), nil
}

func (runtime *Runtime) setAlertString(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	receiver, err := referenceArgument(arguments, 0)
	if err != nil {
		return jvm.VoidValue(), err
	}
	text, err := optionalStringArgument(arguments, 1)
	if err != nil {
		return jvm.VoidValue(), err
	}
	screen := runtime.screenState(receiver, screenAlert)
	screen.alertText = text
	return jvm.VoidValue(), runtime.refreshCurrentScreen(receiver)
}

func (runtime *Runtime) alertImage(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	receiver, err := referenceArgument(arguments, 0)
	if err != nil {
		return jvm.VoidValue(), err
	}
	return jvm.ReferenceValue(runtime.screenState(receiver, screenAlert).alertImage), nil
}

func (runtime *Runtime) setAlertImage(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	receiver, err := referenceArgument(arguments, 0)
	if err != nil {
		return jvm.VoidValue(), err
	}
	image, err := referenceArgument(arguments, 1)
	if err != nil {
		return jvm.VoidValue(), err
	}
	runtime.screenState(receiver, screenAlert).alertImage = image
	return jvm.VoidValue(), runtime.refreshCurrentScreen(receiver)
}

func (runtime *Runtime) alertType(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	receiver, err := referenceArgument(arguments, 0)
	if err != nil {
		return jvm.VoidValue(), err
	}
	return jvm.ReferenceValue(runtime.screenState(receiver, screenAlert).alertType), nil
}

func (runtime *Runtime) setAlertType(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	receiver, err := referenceArgument(arguments, 0)
	if err != nil {
		return jvm.VoidValue(), err
	}
	alertType, err := referenceArgument(arguments, 1)
	if err != nil {
		return jvm.VoidValue(), err
	}
	runtime.screenState(receiver, screenAlert).alertType = alertType
	return jvm.VoidValue(), runtime.refreshCurrentScreen(receiver)
}

func (runtime *Runtime) alertTimeout(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	receiver, err := referenceArgument(arguments, 0)
	if err != nil {
		return jvm.VoidValue(), err
	}
	return jvm.IntValue(runtime.screenState(receiver, screenAlert).timeout), nil
}

func (runtime *Runtime) setAlertTimeout(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	receiver, err := referenceArgument(arguments, 0)
	if err != nil {
		return jvm.VoidValue(), err
	}
	timeout, err := intArgument(arguments, 1)
	if err != nil {
		return jvm.VoidValue(), err
	}
	if timeout <= 0 && timeout != alertForever {
		return jvm.VoidValue(), newGuestException("java/lang/IllegalArgumentException",
			fmt.Sprintf("alert timeout %d", timeout))
	}
	runtime.screenState(receiver, screenAlert).timeout = timeout
	return jvm.VoidValue(), nil
}

func (runtime *Runtime) alertDefaultTimeout(_ *jvm.VM, _ []jvm.Value) (jvm.Value, error) {
	return jvm.IntValue(alertDefaultTimeout), nil
}

// playAlertSound reports that nothing was played. The Host audio sink plays
// decoded media, not device tones, and answering true would tell a game a
// sound it can hear happened.
func (runtime *Runtime) playAlertSound(_ *jvm.VM, _ []jvm.Value) (jvm.Value, error) {
	return jvm.IntValue(0), nil
}

// optionalStringArgument reads a String argument that may legitimately be
// null, which most of the lcdui setters accept.
func optionalStringArgument(arguments []jvm.Value, index int) (string, error) {
	object, err := referenceArgument(arguments, index)
	if err != nil {
		return "", err
	}
	if object == nil {
		return "", nil
	}
	value, ok := object.Native.(string)
	if object.ClassName != jvm.StringClass || !ok {
		return "", fmt.Errorf("argument %d is not a String", index)
	}
	return value, nil
}

// commandsOfDisplayable copies the command list under the state lock.
func (runtime *Runtime) commandsOfDisplayable(object *jvm.Object) []*jvm.Object {
	data := runtime.displayableState(object)
	state := runtime.lcdui()
	state.mu.Lock()
	defer state.mu.Unlock()
	return append([]*jvm.Object(nil), data.commands...)
}

// commandLabelText is what the soft key shows: the short label, falling back
// to the long one.
func commandLabelText(object *jvm.Object) string {
	data := commandOf(object)
	if data == nil {
		return ""
	}
	if data.label != "" {
		return data.label
	}
	return data.longLabel
}

// trimToWidth shortens text with an ellipsis so it fits a pixel width.
func trimToWidth(font *fontData, text string, width int) string {
	if font == nil || width <= 0 {
		return text
	}
	runes := []rune(text)
	if font.textWidth(runes) <= width {
		return text
	}
	ellipsis := []rune("…")
	for length := len(runes) - 1; length > 0; length-- {
		if font.textWidth(append(append([]rune(nil), runes[:length]...), ellipsis...)) <= width {
			return string(runes[:length]) + "…"
		}
	}
	return ""
}

// wrapText breaks a paragraph into lines that fit a pixel width, splitting on
// spaces where it can and mid-word where a single word is too long.
func wrapText(font *fontData, text string, width int) []string {
	if text == "" {
		return nil
	}
	var lines []string
	for _, paragraph := range strings.Split(text, "\n") {
		if paragraph == "" {
			lines = append(lines, "")
			continue
		}
		current := ""
		flush := func() {
			lines = append(lines, current)
			current = ""
		}
		for _, word := range strings.Split(paragraph, " ") {
			candidate := word
			if current != "" {
				candidate = current + " " + word
			}
			if font == nil || font.textWidth([]rune(candidate)) <= width {
				current = candidate
				continue
			}
			if current != "" {
				flush()
			}
			// A word wider than the line is split at the last character that
			// still fits rather than dropped.
			remaining := []rune(word)
			for len(remaining) > 0 {
				cut := len(remaining)
				for cut > 1 && font.textWidth(remaining[:cut]) > width {
					cut--
				}
				lines = append(lines, string(remaining[:cut]))
				remaining = remaining[cut:]
			}
			if len(lines) > 0 {
				current = lines[len(lines)-1]
				lines = lines[:len(lines)-1]
			}
		}
		if current != "" {
			flush()
		}
	}
	return lines
}
