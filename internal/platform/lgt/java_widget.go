package lgt

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/movingwoo/wfeature/internal/armcore"
)

// The lwc widgets, and the input-method automaton behind the text ones
//
// Two local titles build a text field on a shell and put it on their card: one
// asks the player for a name, the other for a message. **Nothing here draws any
// of it**, for the reason the other platform's toolkit is not drawn either —
// there is no widget surface, and inventing one would be a screen nobody has
// seen. What a widget does here is keep what it was given and answer it back,
// which is what the code around it reads: a title builds its dialog once and
// tests what it built before building it again.
//
// The state is per object rather than per class because a title holds several
// at once — a field, the shell it sits in, and the handler the field owns.
type javaWidget struct {
	// text is what a text component holds, and maxLength the limit it was
	// given. Zero is "no limit", which is what a component that was never told
	// one has.
	text      string
	maxLength int32
	// mode is the input mode an InputMethodHandler is in, and listener the
	// object it was told to hand characters to. Nothing hands any over: text
	// reaches a component through the Host keypad rather than through a key the
	// title forwards, so the listener is kept and not fired.
	mode     int32
	listener uint32
	// children are the components added to a container, in the order they were
	// added, which is what a title that walks its own screen reads back.
	children []uint32
	// shown is whether the title has put this component on the screen.
	shown bool
}

// maxWidgetChildren bounds a container. A title that adds more than this is
// leaking rather than laying out.
const maxWidgetChildren = 256

// javaWidgetState answers the state behind one widget object, building it the
// first time. A component this platform never saw constructed is still a
// component — the module allocates the object itself and a constructor this
// platform does not serve leaves nothing behind — so a missing entry is made
// rather than refused.
func (client *Client) javaWidgetState(object uint32) *javaWidget {
	runtime := client.javaRuntimeState()
	if runtime.widgets == nil {
		runtime.widgets = map[uint32]*javaWidget{}
	}
	state, ok := runtime.widgets[object]
	if !ok {
		state = &javaWidget{}
		runtime.widgets[object] = state
	}
	return state
}

// javaWidgetMethods is the toolkit's own table, joined with the rest in
// java_api.go.
var javaWidgetMethods = map[string]javaPlatformMethod{
	// A text field and a text box are the same component to this platform:
	// both are the specification's TextComponent with a starting string and an
	// input constraint, and the difference between them is how the handset
	// drew them.
	"org/kwis/msp/lwc/TextFieldComponent.<init>(Ljava/lang/String;I)V": {
		Words: 3, Implementat: javaTextComponentConstructor},
	"org/kwis/msp/lwc/TextBoxComponent.<init>(Ljava/lang/String;I)V": {
		Words: 3, Implementat: javaTextComponentConstructor},
	"org/kwis/msp/lwc/TextComponent.getString()Ljava/lang/String;": {
		Words: 1, Implementat: javaTextComponentGetString},
	"org/kwis/msp/lwc/TextFieldComponent.setString(Ljava/lang/String;)V": {
		Words: 2, Implementat: javaTextComponentSetString},
	"org/kwis/msp/lwc/TextBoxComponent.setMaxLength(I)V": {
		Words: 2, Implementat: javaTextComponentSetMaxLength},
	"org/kwis/msp/lwc/TextFieldComponent.setMaxLength(I)V": {
		Words: 2, Implementat: javaTextComponentSetMaxLength},
	// Editing the text the component holds, in characters. A handset's own
	// input method calls these as the player types; here the title calls them
	// itself, which is the half that works without a widget being drawn.
	"org/kwis/msp/lwc/TextBoxComponent.insert([CIII)V": {
		Words: 5, Implementat: javaTextComponentInsert},
	"org/kwis/msp/lwc/TextBoxComponent.delete(II)V": {
		Words: 3, Implementat: javaTextComponentDelete},

	// A container keeps its children so a title can walk them; nothing lays
	// them out. The shell answers the index the specification gives it.
	"org/kwis/msp/lwc/ShellComponent.addComponent(Lorg/kwis/msp/lwc/Component;)I": {
		Words: 2, Implementat: javaComponentAddChild},
	// Showing and hiding a shell is the whole of what putting one on the
	// screen does here, and isShown is what a title asks before it builds its
	// screen again.
	"org/kwis/msp/lwc/ShellComponent.show()V": {Words: 1, Implementat: javaComponentShow(true)},
	"org/kwis/msp/lwc/ShellComponent.hide()V": {Words: 1, Implementat: javaComponentShow(false)},
	"org/kwis/msp/lwc/ShellComponent.isShown()Z": {
		Words: 1, Implementat: javaComponentIsShown},
	// The notifications the platform would send a drawn widget. It draws none,
	// so they are taken and recorded: a component that is told it is on the
	// screen answers isShown with it.
	"org/kwis/msp/lwc/TextComponent.showNotify(Z)V":       {Words: 2, Implementat: javaComponentShowNotify},
	"org/kwis/msp/lwc/TextFieldComponent.focusNotify(Z)V": {Words: 2, Implementat: javaNoResult},
	"org/kwis/msp/lwc/TextBoxComponent.focusNotify(Z)V":   {Words: 2, Implementat: javaNoResult},
	"org/kwis/msp/lwc/TextBoxComponent.configure(IIIII)V": {Words: 6, Implementat: javaNoResult},
	// A key offered to a widget. **Nothing here consumes one**: a widget that
	// answered true would take the key away from the card that is the only
	// thing drawing, and the player would be typing into something invisible.
	// False is what a component that did not handle the key answers, and it is
	// the honest answer for one that is not on the screen.
	"org/kwis/msp/lwc/TextFieldComponent.keyNotify(II)Z": {Words: 3, Implementat: javaZeroResult},
	"org/kwis/msp/lwc/TextBoxComponent.keyNotify(II)Z":   {Words: 3, Implementat: javaZeroResult},
	"org/kwis/msp/lwc/ShellComponent.keyNotify(II)Z":     {Words: 3, Implementat: javaZeroResult},

	// The automaton a text component owns. The specification builds it with an
	// input constraint and drives it with notifyKeyInput; there is no
	// composing surface behind it here, so what it does is keep the mode and
	// the listener it was given. A handler that could not be given a listener
	// has told the title its own input will never work, which is why the call
	// exists rather than being left to fail.
	"org/kwis/msp/lcdui/InputMethodHandler.<init>(I)V": {
		Words: 2, Implementat: javaInputMethodConstructor},
	"org/kwis/msp/lcdui/InputMethodHandler.setCurrentMode(I)Z": {
		Words: 2, Implementat: javaInputMethodSetMode},
	"org/kwis/msp/lcdui/InputMethodHandler.getCurrentMode()I": {
		Words: 1, Implementat: javaInputMethodGetMode},
	"org/kwis/msp/lcdui/InputMethodHandler.setInputMethodListener(Lorg/kwis/msp/lcdui/InputMethodListener;)V": {
		Words: 2, Implementat: javaInputMethodSetListener},

	// Numbers read out of and written into text. A title parses what it stored
	// as a string, and the language says what a string that is not a number
	// answers: NumberFormatException, which the exception hierarchy here
	// already places under IllegalArgumentException, so a title that guards its
	// own parse with a catch gets what it guarded against.
	"java/lang/Integer.parseInt(Ljava/lang/String;)I":  {Words: 1, Implementat: javaParseInt(32)},
	"java/lang/Integer.parseInt(Ljava/lang/String;I)I": {Words: 2, Implementat: javaParseIntRadix},
	"java/lang/Byte.parseByte(Ljava/lang/String;)B":    {Words: 1, Implementat: javaParseInt(8)},
	"java/lang/Short.parseShort(Ljava/lang/String;)S":  {Words: 1, Implementat: javaParseInt(16)},
	"java/lang/Integer.toString(I)Ljava/lang/String;":  {Words: 1, Implementat: javaIntegerToString},
	"java/lang/Long.toString(J)Ljava/lang/String;":     {Words: 2, Implementat: javaLongToString},
}

// javaParseInt is the shape of the three text-to-number statics: the same parse
// with a different width, because that is the only thing that separates them.
func javaParseInt(bits int) func(*Client, context.Context, *armcore.Thread, []uint32) (uint32, error) {
	return func(client *Client, _ context.Context, thread *armcore.Thread, arguments []uint32) (uint32, error) {
		return client.javaParseNumber(thread, arguments[0], 10, bits)
	}
}

func javaParseIntRadix(
	client *Client, _ context.Context, thread *armcore.Thread, arguments []uint32,
) (uint32, error) {
	return client.javaParseNumber(thread, arguments[0], int(int32(arguments[1])), 32)
}

// javaParseNumber is the parse itself. The language trims nothing — `" 1"` is
// not a number — and a radix outside 2..36 is the caller's mistake; both answer
// the NumberFormatException the specification names, as a guest throw rather
// than a platform failure, because a title that guards its own parse cannot see
// an error the guest was never given.
func (client *Client) javaParseNumber(
	thread *armcore.Thread, object uint32, radix, bits int,
) (uint32, error) {
	text, ok := client.javaText(object)
	if !ok || object == 0 {
		return 0, client.throwJavaPlatform(thread, javaThrowNullClass, ": parsing a null string")
	}
	if radix < 2 || radix > 36 {
		return 0, client.throwJavaPlatform(thread, javaNumberFormatClass,
			": radix "+strconv.Itoa(radix))
	}
	value, err := strconv.ParseInt(strings.TrimPrefix(text, "+"), radix, bits)
	if err != nil {
		return 0, client.throwJavaPlatform(thread, javaNumberFormatClass, ": "+text)
	}
	return uint32(int32(value)), nil
}

const javaNumberFormatClass = "java/lang/NumberFormatException"

func javaIntegerToString(
	client *Client, _ context.Context, _ *armcore.Thread, arguments []uint32,
) (uint32, error) {
	return client.newJavaString(strconv.FormatInt(int64(int32(arguments[0])), 10))
}

// javaLongToString takes the two halves a 64-bit argument arrives in, low word
// first, which is the order every other long here is passed in.
func javaLongToString(
	client *Client, _ context.Context, _ *armcore.Thread, arguments []uint32,
) (uint32, error) {
	value := int64(uint64(arguments[0]) | uint64(arguments[1])<<32)
	return client.newJavaString(strconv.FormatInt(value, 10))
}

func javaTextComponentConstructor(
	client *Client, _ context.Context, _ *armcore.Thread, arguments []uint32,
) (uint32, error) {
	state := client.javaWidgetState(arguments[0])
	text, _ := client.javaText(arguments[1])
	state.text = text
	state.mode = int32(arguments[2])
	return 0, client.attachInputMethodHandler(arguments[0], int32(arguments[2]))
}

// attachInputMethodHandler gives a text component the automaton the
// specification says every one of them owns, and publishes it into the object
// word the module was told `imHandler` sits in.
//
// **The field is read rather than asked for.** The specification declares it
// protected on TextComponent, so a title takes the handler off the component
// instead of constructing one; a component whose word holds zero hands that
// title a null to register its listener on, which is a NullPointerException in
// its own code with nothing here to blame. See layoutPlatformFields.
func (client *Client) attachInputMethodHandler(component uint32, constraint int32) error {
	if client.javaLink == nil || client.javaLink.layout == nil {
		return nil
	}
	word, ok := client.javaLink.layout.platformFieldWord(
		javaTextComponentClass, "imHandler", "Lorg/kwis/msp/lcdui/InputMethodHandler;")
	if !ok {
		// The module never asked for the field, so nothing reads it.
		return nil
	}
	class, err := client.preparePlatformJavaClass(javaInputMethodHandlerClass)
	if err != nil {
		return err
	}
	handler, err := client.allocateJavaObject(class)
	if err != nil {
		return err
	}
	client.javaWidgetState(handler).mode = constraint
	block, err := client.readWord(component + 8)
	if err != nil {
		return err
	}
	return client.writeWord(block+word*4, handler)
}

const (
	javaTextComponentClass      = "org/kwis/msp/lwc/TextComponent"
	javaInputMethodHandlerClass = "org/kwis/msp/lcdui/InputMethodHandler"
)

func javaTextComponentGetString(
	client *Client, _ context.Context, _ *armcore.Thread, arguments []uint32,
) (uint32, error) {
	return client.newJavaString(client.javaWidgetState(arguments[0]).text)
}

func javaTextComponentSetString(
	client *Client, _ context.Context, _ *armcore.Thread, arguments []uint32,
) (uint32, error) {
	text, _ := client.javaText(arguments[1])
	client.javaWidgetState(arguments[0]).text = text
	return 0, nil
}

func javaTextComponentSetMaxLength(
	client *Client, _ context.Context, _ *armcore.Thread, arguments []uint32,
) (uint32, error) {
	client.javaWidgetState(arguments[0]).maxLength = int32(arguments[1])
	return 0, nil
}

// javaTextComponentInsert is `insert(char[] data, int offset, int length, int
// position)`: a run of characters put into the text at a position. A position
// past the end appends, which is what a caret at the end of the text is.
func javaTextComponentInsert(
	client *Client, _ context.Context, _ *armcore.Thread, arguments []uint32,
) (uint32, error) {
	units, err := client.javaCharsText(arguments[1], arguments[2], arguments[3])
	if err != nil {
		return 0, err
	}
	state := client.javaWidgetState(arguments[0])
	symbols := []rune(state.text)
	at := int(int32(arguments[4]))
	if at < 0 {
		at = 0
	}
	if at > len(symbols) {
		at = len(symbols)
	}
	grown := append([]rune{}, symbols[:at]...)
	grown = append(grown, []rune(units)...)
	grown = append(grown, symbols[at:]...)
	if state.maxLength > 0 && int32(len(grown)) > state.maxLength {
		grown = grown[:state.maxLength]
	}
	state.text = string(grown)
	return 0, nil
}

// javaTextComponentDelete is `delete(int offset, int length)`, in characters.
// A range outside the text is clipped rather than refused: the caller is the
// handset's own input method on a component nothing here is driving, so a
// position it computed against a caret this platform does not move is not the
// title's mistake.
func javaTextComponentDelete(
	client *Client, _ context.Context, _ *armcore.Thread, arguments []uint32,
) (uint32, error) {
	state := client.javaWidgetState(arguments[0])
	symbols := []rune(state.text)
	offset, length := int(int32(arguments[1])), int(int32(arguments[2]))
	if offset < 0 {
		offset = 0
	}
	if offset > len(symbols) {
		offset = len(symbols)
	}
	if length < 0 {
		length = 0
	}
	if offset+length > len(symbols) {
		length = len(symbols) - offset
	}
	state.text = string(append(append([]rune{}, symbols[:offset]...), symbols[offset+length:]...))
	return 0, nil
}

func javaComponentAddChild(
	client *Client, _ context.Context, _ *armcore.Thread, arguments []uint32,
) (uint32, error) {
	state := client.javaWidgetState(arguments[0])
	if len(state.children) >= maxWidgetChildren {
		return 0, fmt.Errorf("an lwc container holds more than %d children", maxWidgetChildren)
	}
	state.children = append(state.children, arguments[1])
	return uint32(len(state.children) - 1), nil
}

func javaComponentShow(shown bool) func(*Client, context.Context, *armcore.Thread, []uint32) (uint32, error) {
	return func(client *Client, _ context.Context, _ *armcore.Thread, arguments []uint32) (uint32, error) {
		client.javaWidgetState(arguments[0]).shown = shown
		return 0, nil
	}
}

func javaComponentShowNotify(
	client *Client, _ context.Context, _ *armcore.Thread, arguments []uint32,
) (uint32, error) {
	client.javaWidgetState(arguments[0]).shown = arguments[1] != 0
	return 0, nil
}

func javaComponentIsShown(
	client *Client, _ context.Context, _ *armcore.Thread, arguments []uint32,
) (uint32, error) {
	if client.javaWidgetState(arguments[0]).shown {
		return 1, nil
	}
	return 0, nil
}

func javaInputMethodConstructor(
	client *Client, _ context.Context, _ *armcore.Thread, arguments []uint32,
) (uint32, error) {
	client.javaWidgetState(arguments[0]).mode = int32(arguments[1])
	return 0, nil
}

// javaInputMethodSetMode accepts every mode. There is no on-device input method
// to switch: text arrives through the Host keypad, so no mode is less available
// than another.
func javaInputMethodSetMode(
	client *Client, _ context.Context, _ *armcore.Thread, arguments []uint32,
) (uint32, error) {
	client.javaWidgetState(arguments[0]).mode = int32(arguments[1])
	return 1, nil
}

func javaInputMethodGetMode(
	client *Client, _ context.Context, _ *armcore.Thread, arguments []uint32,
) (uint32, error) {
	return uint32(client.javaWidgetState(arguments[0]).mode), nil
}

func javaInputMethodSetListener(
	client *Client, _ context.Context, _ *armcore.Thread, arguments []uint32,
) (uint32, error) {
	client.javaWidgetState(arguments[0]).listener = arguments[1]
	return 0, nil
}

// javaNotifyDestroyed is `Jlet.notifyDestroyed()`: the application saying it is
// finished. It is the Java side of the ending `MC_knlExit` gives a Clet, and it
// is reported the same way — the Host observes ErrGuestExited and tears the
// session down instead of treating it as a failure. The call site is named for
// the reason the C exit names one: an ending nothing can locate reads as a
// broken title rather than a finished one.
func javaNotifyDestroyed(
	client *Client, _ context.Context, thread *armcore.Thread, _ []uint32,
) (uint32, error) {
	client.exited = true
	client.exitedFrom, _ = thread.Register(14)
	client.flushOpenFiles()
	return 0, fmt.Errorf("Jlet.notifyDestroyed: %w", client.exitError())
}
