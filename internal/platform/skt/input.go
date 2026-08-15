package skt

import (
	"errors"
	"fmt"
	"strings"

	"github.com/movingwoo/wfeature/internal/api/midp"
	"github.com/movingwoo/wfeature/internal/jvm"
)

type KeyEventType string
type PointerEventType string

const (
	KeyPressed      KeyEventType     = "press"
	KeyReleased     KeyEventType     = "release"
	KeyRepeated     KeyEventType     = "repeat"
	PointerPressed  PointerEventType = "press"
	PointerReleased PointerEventType = "release"
	PointerDragged  PointerEventType = "drag"
)

// Directional and fire key values preserve the device-specific MIDP mapping
// used by the original emulator. Number, star, and pound keys use their ASCII
// values (48-57, 42, and 35).
const (
	KeyCodeUp    int32 = 141
	KeyCodeLeft  int32 = 142
	KeyCodeRight int32 = 145
	KeyCodeDown  int32 = 146
	KeyCodeFire  int32 = 148
	// The handset's send key, which a game reads like any other: it is the one
	// a title reaches for when it wants a key the keypad does not otherwise
	// have, typically a quick save.
	KeyCodeCall int32 = 10
	// The handset's CLR key. A title of this era draws "BACK:CLR" in the corner
	// of every screen it can be backed out of, and one local title's settings
	// screen is written that way: leaving it is what writes the settings, so
	// without this key a scripted run cannot reach the write at all. The value
	// is the handset's, the same one the original runtime carries.
	KeyCodeClear int32 = 8
)

var (
	ErrInvalidKeyEvent     = errors.New("invalid key event type")
	ErrInvalidPointerEvent = errors.New("invalid pointer event type")
)

// SendKey delivers a Host key event to the current active Canvas through the
// same bounded FIFO as lifecycle, display, and repaint events. Input is ignored
// while the MIDlet is not active or when the current Displayable is not a
// Canvas.
func (runtime *Runtime) SendKey(eventType KeyEventType, keyCode int32) error {
	callback, ok := keyCallback(eventType)
	if !ok {
		return fmt.Errorf("%w: %q", ErrInvalidKeyEvent, eventType)
	}
	return runtime.dispatch("Canvas."+callback, func() error {
		return runtime.deliverCurrentCanvasKey(eventType, callback, keyCode)
	})
}

func (runtime *Runtime) deliverCurrentCanvasKey(eventType KeyEventType, callback string, keyCode int32) error {
	if runtime.State() != StateActive {
		return nil
	}
	runtime.displayMu.RLock()
	current := runtime.currentDisplayable
	runtime.displayMu.RUnlock()
	if current == nil {
		return nil
	}
	isCanvas, err := runtime.VM.IsSubclassOf(current.ClassName, midp.CanvasClass)
	if err != nil {
		return fmt.Errorf("validate key event Canvas: %w", err)
	}
	if !isCanvas {
		// A runtime-drawn Screen reads keys itself: navigation moves its
		// selection and the soft keys reach its commands.
		if runtime.isScreen(current) {
			return runtime.deliverScreenKey(current, eventType, keyCode)
		}
		return nil
	}
	// A GameCanvas latches the key into getKeyStates whether or not it also
	// wants the callback, because polling is the whole point of the class.
	if !runtime.recordGameCanvasKey(current, eventType, keyCode) {
		return nil
	}
	// Soft keys are not Canvas keys: MIDP puts commands on every Displayable,
	// and a Canvas that added one has no other way to reach it.
	if keyCode == KeyCodeSoft1 || keyCode == KeyCodeSoft2 {
		if eventType != KeyPressed {
			return nil
		}
		commands := runtime.commandsOfDisplayable(current)
		index := 0
		if keyCode == KeyCodeSoft2 {
			index = 1
		}
		if index < len(commands) {
			return runtime.fireCommand(current, commands[index])
		}
		return nil
	}
	if runtime.logger != nil {
		runtime.logger.Debug("MIDP Canvas key event", "type", eventType, "code", keyCode, "class", current.ClassName)
	}
	if _, err := runtime.VM.InvokeVirtual(current, callback, "(I)V", jvm.IntValue(keyCode)); err != nil {
		return fmt.Errorf("deliver %s(%d) to Canvas %s: %w", callback, keyCode, current.ClassName, err)
	}
	return nil
}

// SendPointer delivers a Host pointer event in framebuffer coordinates to the
// current active Canvas. Hosts are responsible for scaling their view-space
// coordinates to the configured framebuffer dimensions.
func (runtime *Runtime) SendPointer(eventType PointerEventType, x, y int32) error {
	callback, ok := pointerCallback(eventType)
	if !ok {
		return fmt.Errorf("%w: %q", ErrInvalidPointerEvent, eventType)
	}
	return runtime.dispatch("Canvas."+callback, func() error {
		return runtime.deliverCurrentCanvasPointer(eventType, callback, x, y)
	})
}

func (runtime *Runtime) deliverCurrentCanvasPointer(eventType PointerEventType, callback string, x, y int32) error {
	if runtime.State() != StateActive {
		return nil
	}
	runtime.displayMu.RLock()
	current := runtime.currentDisplayable
	runtime.displayMu.RUnlock()
	if current == nil {
		return nil
	}
	isCanvas, err := runtime.VM.IsSubclassOf(current.ClassName, midp.CanvasClass)
	if err != nil {
		return fmt.Errorf("validate pointer event Canvas: %w", err)
	}
	if !isCanvas {
		return nil
	}
	if runtime.logger != nil {
		runtime.logger.Debug("MIDP Canvas pointer event", "type", eventType, "x", x, "y", y, "class", current.ClassName)
	}
	if _, err := runtime.VM.InvokeVirtual(current, callback, "(II)V", jvm.IntValue(x), jvm.IntValue(y)); err != nil {
		return fmt.Errorf("deliver %s(%d, %d) to Canvas %s: %w", callback, x, y, current.ClassName, err)
	}
	return nil
}

// KeyCodeByName translates the Host's key names into the MIDP codes this
// runtime delivers. The names are the ones the WIPI platforms answer to, so a
// scripted run reads the same whichever vendor it drives; the codes are not,
// because a MIDlet compares against MIDP values.
func KeyCodeByName(name string) (int32, bool) {
	switch strings.ToLower(name) {
	case "up":
		return KeyCodeUp, true
	case "down":
		return KeyCodeDown, true
	case "left":
		return KeyCodeLeft, true
	case "right":
		return KeyCodeRight, true
	case "fire", "ok":
		return KeyCodeFire, true
	case "soft1":
		return KeyCodeSoft1, true
	case "soft2":
		return KeyCodeSoft2, true
	case "call":
		return KeyCodeCall, true
	case "clear", "clr", "back":
		return KeyCodeClear, true
	}
	if len(name) == 1 && (name[0] >= '0' && name[0] <= '9' || name[0] == '*' || name[0] == '#') {
		return int32(name[0]), true
	}
	return 0, false
}

func keyCallback(eventType KeyEventType) (string, bool) {
	switch eventType {
	case KeyPressed:
		return "keyPressed", true
	case KeyReleased:
		return "keyReleased", true
	case KeyRepeated:
		return "keyRepeated", true
	default:
		return "", false
	}
}

func pointerCallback(eventType PointerEventType) (string, bool) {
	switch eventType {
	case PointerPressed:
		return "pointerPressed", true
	case PointerReleased:
		return "pointerReleased", true
	case PointerDragged:
		return "pointerDragged", true
	default:
		return "", false
	}
}

func (runtime *Runtime) getCanvasKeyCode(vm *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	if _, err := runtime.canvasReceiver(vm, arguments); err != nil {
		return jvm.VoidValue(), err
	}
	action, err := intArgument(arguments, 1)
	if err != nil {
		return jvm.VoidValue(), err
	}
	keyCodes := map[int32]int32{
		1:  KeyCodeUp,
		2:  KeyCodeLeft,
		5:  KeyCodeRight,
		6:  KeyCodeDown,
		8:  KeyCodeFire,
		9:  '1',
		10: '3',
		11: '7',
		12: '9',
	}
	keyCode, ok := keyCodes[action]
	if !ok {
		return jvm.VoidValue(), newGuestException("java/lang/IllegalArgumentException", "invalid game action")
	}
	return jvm.IntValue(keyCode), nil
}

func (runtime *Runtime) getCanvasKeyName(vm *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	if _, err := runtime.canvasReceiver(vm, arguments); err != nil {
		return jvm.VoidValue(), err
	}
	keyCode, err := intArgument(arguments, 1)
	if err != nil {
		return jvm.VoidValue(), err
	}
	names := map[int32]string{
		KeyCodeUp: "UP", KeyCodeLeft: "LEFT", KeyCodeRight: "RIGHT",
		KeyCodeDown: "DOWN", KeyCodeFire: "FIRE", '*': "*", '#': "#",
		'0': "0", '1': "1", '2': "2", '3': "3", '4': "4",
		'5': "5", '6': "6", '7': "7", '8': "8", '9': "9",
	}
	name, ok := names[keyCode]
	if !ok {
		return jvm.VoidValue(), newGuestException("java/lang/IllegalArgumentException", "unknown key code")
	}
	return jvm.ReferenceValue(&jvm.Object{ClassName: "java/lang/String", Native: name}), nil
}
