package midp

import "github.com/movingwoo/wfeature/internal/jvm"

// The LCDUI method bodies this layer owns. Everything that touches the Host —
// pixels, key state, the current screen — is a native the platform registers;
// what is left here is the part MIDP defines in terms of other MIDP calls, and
// it stays here so it runs the same way whichever platform is underneath.

// canvasRepaint asks for the whole surface. MIDP defines it as the ranged
// repaint over the canvas's own size, and both of those are virtual: a game
// that overrides getWidth to letterbox its screen expects the smaller area.
func canvasRepaint(call *jvm.Invocation, arguments []jvm.Value) (jvm.Value, error) {
	canvas, err := receiver(arguments)
	if err != nil {
		return jvm.VoidValue(), err
	}
	width, err := call.InvokeVirtual(canvas, "getWidth", "()I")
	if err != nil {
		return jvm.VoidValue(), err
	}
	height, err := call.InvokeVirtual(canvas, "getHeight", "()I")
	if err != nil {
		return jvm.VoidValue(), err
	}
	_, err = call.InvokeVirtual(canvas, "repaint", "(IIII)V", jvm.IntValue(0), jvm.IntValue(0), width, height)
	return jvm.VoidValue(), err
}

// Game actions and the key codes that produce them. The device codes are what
// this runtime's Host sends for the four-way pad and the select key; the digit
// keys are the MIDP mapping every handset also had.
const (
	gameActionUp    int32 = 1
	gameActionLeft  int32 = 2
	gameActionRight int32 = 5
	gameActionDown  int32 = 6
	gameActionFire  int32 = 8
	gameActionA     int32 = 9
	gameActionB     int32 = 10
	gameActionC     int32 = 11
	gameActionD     int32 = 12

	deviceKeyUp    int32 = 141
	deviceKeyLeft  int32 = 142
	deviceKeyRight int32 = 145
	deviceKeyDown  int32 = 146
	deviceKeyFire  int32 = 148
)

// canvasGameAction maps a key code to the action a game reads instead of the
// key. getKeyCode, the inverse, is the platform's: it answers with the code
// that Host actually sends.
func canvasGameAction(_ *jvm.Invocation, arguments []jvm.Value) (jvm.Value, error) {
	if len(arguments) < 2 {
		return jvm.VoidValue(), jvm.Throw("java/lang/IllegalArgumentException", "getGameAction takes a key code")
	}
	code, err := arguments[1].Int32()
	if err != nil {
		return jvm.VoidValue(), err
	}
	switch code {
	case deviceKeyUp, '2':
		return jvm.IntValue(gameActionUp), nil
	case deviceKeyLeft, '4':
		return jvm.IntValue(gameActionLeft), nil
	case deviceKeyRight, '6':
		return jvm.IntValue(gameActionRight), nil
	case deviceKeyDown, '8':
		return jvm.IntValue(gameActionDown), nil
	case deviceKeyFire, '5':
		return jvm.IntValue(gameActionFire), nil
	case '1':
		return jvm.IntValue(gameActionA), nil
	case '3':
		return jvm.IntValue(gameActionB), nil
	case '7':
		return jvm.IntValue(gameActionC), nil
	case '9':
		return jvm.IntValue(gameActionD), nil
	default:
		return jvm.IntValue(0), nil
	}
}

// gameCanvasInit hands the flag to the buffer the platform allocates.
func gameCanvasInit(call *jvm.Invocation, arguments []jvm.Value) (jvm.Value, error) {
	canvas, err := receiver(arguments)
	if err != nil {
		return jvm.VoidValue(), err
	}
	_, err = call.InvokeSpecial(canvas, GameCanvasClass, "initBuffer", "(Z)V", arguments[1])
	return jvm.VoidValue(), err
}

// gameCanvasPaint satisfies the abstract paint a Canvas requires. A GameCanvas
// paints from its buffer, so a subclass only overrides paint when it wants the
// ordinary Canvas contract as well.
func gameCanvasPaint(call *jvm.Invocation, arguments []jvm.Value) (jvm.Value, error) {
	canvas, err := receiver(arguments)
	if err != nil {
		return jvm.VoidValue(), err
	}
	_, err = call.InvokeSpecial(canvas, GameCanvasClass, "drawBuffer", "(Ljavax/microedition/lcdui/Graphics;)V", arguments[1])
	return jvm.VoidValue(), err
}

const (
	imageDescriptor     = "Ljavax/microedition/lcdui/Image;"
	alertTypeDescriptor = "Ljavax/microedition/lcdui/AlertType;"
	stringDescriptor    = "Ljava/lang/String;"
	itemDescriptor      = "Ljavax/microedition/lcdui/Item;"
	commandDescriptor   = "Ljavax/microedition/lcdui/Command;"
)

// alertInitTitle is the one-argument Alert: a title and nothing else.
func alertInitTitle(call *jvm.Invocation, arguments []jvm.Value) (jvm.Value, error) {
	alert, err := receiver(arguments)
	if err != nil {
		return jvm.VoidValue(), err
	}
	_, err = call.InvokeSpecial(alert, AlertClass, "<init>",
		"("+stringDescriptor+stringDescriptor+imageDescriptor+alertTypeDescriptor+")V",
		arguments[1], jvm.ReferenceValue(nil), jvm.ReferenceValue(nil), jvm.ReferenceValue(nil))
	return jvm.VoidValue(), err
}

func alertInit(call *jvm.Invocation, arguments []jvm.Value) (jvm.Value, error) {
	alert, err := receiver(arguments)
	if err != nil {
		return jvm.VoidValue(), err
	}
	if _, err := call.InvokeVirtual(alert, "setTitle", "("+stringDescriptor+")V", arguments[1]); err != nil {
		return jvm.VoidValue(), err
	}
	_, err = call.InvokeSpecial(alert, AlertClass, "initAlert",
		"("+stringDescriptor+imageDescriptor+alertTypeDescriptor+")V", arguments[2], arguments[3], arguments[4])
	return jvm.VoidValue(), err
}

// alertTypeClassInit creates the five types MIDP publishes. They are compared
// by identity — a game switches on `type == AlertType.ERROR` — so each one is
// an object made once when the class initializes.
func alertTypeClassInit(call *jvm.Invocation, _ []jvm.Value) (jvm.Value, error) {
	for _, name := range []string{"INFO", "WARNING", "ERROR", "ALARM", "CONFIRMATION"} {
		object, err := call.NewObject(AlertTypeClass, "()V")
		if err != nil {
			return jvm.VoidValue(), err
		}
		if err := call.SetStaticField(AlertTypeClass, name, alertTypeDescriptor, jvm.ReferenceValue(object)); err != nil {
			return jvm.VoidValue(), err
		}
	}
	return jvm.VoidValue(), nil
}

// commandInit fills in the long label the three-argument constructor does not
// take, then hands both to the platform.
func commandInit(short bool) jvm.ContextMethod {
	return func(call *jvm.Invocation, arguments []jvm.Value) (jvm.Value, error) {
		command, err := receiver(arguments)
		if err != nil {
			return jvm.VoidValue(), err
		}
		longLabel := jvm.ReferenceValue(nil)
		rest := arguments[2:]
		if !short {
			longLabel = arguments[2]
			rest = arguments[3:]
		}
		_, err = call.InvokeSpecial(command, CommandClass, "init",
			"("+stringDescriptor+stringDescriptor+"II)V", arguments[1], longLabel, rest[0], rest[1])
		return jvm.VoidValue(), err
	}
}

// listClassInit creates the command a list reports when an element is chosen.
// A game compares the command it is handed against this one by identity.
func listClassInit(call *jvm.Invocation, _ []jvm.Value) (jvm.Value, error) {
	command, err := call.NewObject(CommandClass, "("+stringDescriptor+"II)V",
		jvm.ReferenceValue(call.VM().NewString("Select")), jvm.IntValue(CommandScreen), jvm.IntValue(0))
	if err != nil {
		return jvm.VoidValue(), err
	}
	return jvm.VoidValue(), call.SetStaticField(ListClass, "SELECT_COMMAND", commandDescriptor, jvm.ReferenceValue(command))
}

// choiceInitEmpty is the two-argument List and ChoiceGroup constructor: a
// title or label, a choice type, and no elements yet.
func choiceInitEmpty(className string) jvm.ContextMethod {
	return func(call *jvm.Invocation, arguments []jvm.Value) (jvm.Value, error) {
		choice, err := receiver(arguments)
		if err != nil {
			return jvm.VoidValue(), err
		}
		empty, err := call.VM().NewArray(jvm.Type{Kind: jvm.TypeReference, ClassName: "java/lang/String"}, 0)
		if err != nil {
			return jvm.VoidValue(), err
		}
		_, err = call.InvokeSpecial(choice, className, "<init>",
			"("+stringDescriptor+"I[Ljava/lang/String;[Ljavax/microedition/lcdui/Image;)V",
			arguments[1], arguments[2], jvm.ReferenceValue(empty), jvm.ReferenceValue(nil))
		return jvm.VoidValue(), err
	}
}

// choiceInit fills a List or a ChoiceGroup from the parallel element arrays
// MIDP takes. A shorter image array than string array is the application's
// mistake and it gets the exception the array access would have raised.
func choiceInit(className string, titled bool) jvm.ContextMethod {
	return func(call *jvm.Invocation, arguments []jvm.Value) (jvm.Value, error) {
		choice, err := receiver(arguments)
		if err != nil {
			return jvm.VoidValue(), err
		}
		setter := "setLabel"
		if titled {
			setter = "setTitle"
		}
		if _, err := call.InvokeVirtual(choice, setter, "("+stringDescriptor+")V", arguments[1]); err != nil {
			return jvm.VoidValue(), err
		}
		if _, err := call.InvokeVirtual(choice, "initChoice", "(I)V", arguments[2]); err != nil {
			return jvm.VoidValue(), err
		}
		strings, err := arguments[3].Reference()
		if err != nil {
			return jvm.VoidValue(), err
		}
		if strings == nil {
			return jvm.VoidValue(), nil
		}
		images, err := arguments[4].Reference()
		if err != nil {
			return jvm.VoidValue(), err
		}
		_, length, ok := jvm.ArrayComponent(strings)
		if !ok {
			return jvm.VoidValue(), jvm.Throw("java/lang/IllegalArgumentException", "elements are not an array")
		}
		for index := 0; index < length; index++ {
			text, err := elementAt(strings, index)
			if err != nil {
				return jvm.VoidValue(), err
			}
			image := jvm.ReferenceValue(nil)
			if images != nil {
				if image, err = elementAt(images, index); err != nil {
					return jvm.VoidValue(), err
				}
			}
			if _, err := call.InvokeVirtual(choice, "append", "("+stringDescriptor+imageDescriptor+")I", text, image); err != nil {
				return jvm.VoidValue(), err
			}
		}
		return jvm.VoidValue(), nil
	}
}

// elementAt reads one element of a reference array, with the bounds check the
// array access it stands in for would have made.
func elementAt(array *jvm.Object, index int) (jvm.Value, error) {
	_, length, ok := jvm.ArrayComponent(array)
	if !ok {
		return jvm.VoidValue(), jvm.Throw("java/lang/IllegalArgumentException", "not an array")
	}
	if index < 0 || index >= length {
		return jvm.VoidValue(), jvm.Throw("java/lang/ArrayIndexOutOfBoundsException", "element out of range")
	}
	_, values, err := jvm.ArraySnapshot(array)
	if err != nil {
		return jvm.VoidValue(), err
	}
	return values[index], nil
}

// formInitTitle is the one-argument Form.
func formInitTitle(call *jvm.Invocation, arguments []jvm.Value) (jvm.Value, error) {
	form, err := receiver(arguments)
	if err != nil {
		return jvm.VoidValue(), err
	}
	_, err = call.InvokeVirtual(form, "setTitle", "("+stringDescriptor+")V", arguments[1])
	return jvm.VoidValue(), err
}

func formInitItems(call *jvm.Invocation, arguments []jvm.Value) (jvm.Value, error) {
	form, err := receiver(arguments)
	if err != nil {
		return jvm.VoidValue(), err
	}
	if _, err := call.InvokeVirtual(form, "setTitle", "("+stringDescriptor+")V", arguments[1]); err != nil {
		return jvm.VoidValue(), err
	}
	items, err := arguments[2].Reference()
	if err != nil || items == nil {
		return jvm.VoidValue(), err
	}
	_, length, ok := jvm.ArrayComponent(items)
	if !ok {
		return jvm.VoidValue(), jvm.Throw("java/lang/IllegalArgumentException", "items are not an array")
	}
	for index := 0; index < length; index++ {
		item, err := elementAt(items, index)
		if err != nil {
			return jvm.VoidValue(), err
		}
		if _, err := call.InvokeVirtual(form, "append", "("+itemDescriptor+")I", item); err != nil {
			return jvm.VoidValue(), err
		}
	}
	return jvm.VoidValue(), nil
}

// formAppendString and formAppendImage are the convenience overloads: MIDP
// defines both as appending an item that wraps the argument.
func formAppendString(call *jvm.Invocation, arguments []jvm.Value) (jvm.Value, error) {
	form, err := receiver(arguments)
	if err != nil {
		return jvm.VoidValue(), err
	}
	item, err := call.NewObject(StringItemClass, "("+stringDescriptor+stringDescriptor+")V",
		jvm.ReferenceValue(nil), arguments[1])
	if err != nil {
		return jvm.VoidValue(), err
	}
	return call.InvokeVirtual(form, "append", "("+itemDescriptor+")I", jvm.ReferenceValue(item))
}

func formAppendImage(call *jvm.Invocation, arguments []jvm.Value) (jvm.Value, error) {
	form, err := receiver(arguments)
	if err != nil {
		return jvm.VoidValue(), err
	}
	item, err := call.NewObject(ImageItemClass, "("+stringDescriptor+imageDescriptor+"I"+stringDescriptor+")V",
		jvm.ReferenceValue(nil), arguments[1], jvm.IntValue(itemLayoutDefault), jvm.ReferenceValue(nil))
	if err != nil {
		return jvm.VoidValue(), err
	}
	return call.InvokeVirtual(form, "append", "("+itemDescriptor+")I", jvm.ReferenceValue(item))
}

const (
	// itemLayoutDefault is Item.LAYOUT_DEFAULT and itemPlain is Item.PLAIN.
	itemLayoutDefault int32 = 0
	itemPlain         int32 = 0
)

// itemInit lets the platform create the state a Form draws the item from.
func itemInit(call *jvm.Invocation, arguments []jvm.Value) (jvm.Value, error) {
	item, err := receiver(arguments)
	if err != nil {
		return jvm.VoidValue(), err
	}
	_, err = call.InvokeVirtual(item, "initItem", "()V")
	return jvm.VoidValue(), err
}

func imageItemInit(call *jvm.Invocation, arguments []jvm.Value) (jvm.Value, error) {
	item, err := receiver(arguments)
	if err != nil {
		return jvm.VoidValue(), err
	}
	if _, err := call.InvokeVirtual(item, "setLabel", "("+stringDescriptor+")V", arguments[1]); err != nil {
		return jvm.VoidValue(), err
	}
	if _, err := call.InvokeVirtual(item, "setLayout", "(I)V", arguments[3]); err != nil {
		return jvm.VoidValue(), err
	}
	_, err = call.InvokeSpecial(item, ImageItemClass, "initImage", "("+imageDescriptor+stringDescriptor+")V",
		arguments[2], arguments[4])
	return jvm.VoidValue(), err
}

// imageItemInitAppearance is the five-argument constructor. The appearance
// mode is accepted and ignored: this runtime draws every image item the same
// way, and getAppearanceMode says so.
func imageItemInitAppearance(call *jvm.Invocation, arguments []jvm.Value) (jvm.Value, error) {
	item, err := receiver(arguments)
	if err != nil {
		return jvm.VoidValue(), err
	}
	_, err = call.InvokeSpecial(item, ImageItemClass, "<init>",
		"("+stringDescriptor+imageDescriptor+"I"+stringDescriptor+")V",
		arguments[1], arguments[2], arguments[3], arguments[4])
	return jvm.VoidValue(), err
}

func stringItemInit(call *jvm.Invocation, arguments []jvm.Value) (jvm.Value, error) {
	item, err := receiver(arguments)
	if err != nil {
		return jvm.VoidValue(), err
	}
	_, err = call.InvokeSpecial(item, StringItemClass, "<init>",
		"("+stringDescriptor+stringDescriptor+"I)V", arguments[1], arguments[2], jvm.IntValue(itemPlain))
	return jvm.VoidValue(), err
}

func stringItemInitAppearance(call *jvm.Invocation, arguments []jvm.Value) (jvm.Value, error) {
	item, err := receiver(arguments)
	if err != nil {
		return jvm.VoidValue(), err
	}
	if _, err := call.InvokeVirtual(item, "setLabel", "("+stringDescriptor+")V", arguments[1]); err != nil {
		return jvm.VoidValue(), err
	}
	_, err = call.InvokeSpecial(item, StringItemClass, "initText", "("+stringDescriptor+"I)V", arguments[2], arguments[3])
	return jvm.VoidValue(), err
}

// textInit is the TextBox and TextField constructor. They differ only in what
// their first argument names — a screen has a title, an item has a label.
func textInit(className string, titled bool) jvm.ContextMethod {
	return func(call *jvm.Invocation, arguments []jvm.Value) (jvm.Value, error) {
		text, err := receiver(arguments)
		if err != nil {
			return jvm.VoidValue(), err
		}
		setter := "setLabel"
		if titled {
			setter = "setTitle"
		}
		if _, err := call.InvokeVirtual(text, setter, "("+stringDescriptor+")V", arguments[1]); err != nil {
			return jvm.VoidValue(), err
		}
		_, err = call.InvokeSpecial(text, className, "initText", "("+stringDescriptor+"II)V",
			arguments[2], arguments[3], arguments[4])
		return jvm.VoidValue(), err
	}
}

func tickerInit(call *jvm.Invocation, arguments []jvm.Value) (jvm.Value, error) {
	ticker, err := receiver(arguments)
	if err != nil {
		return jvm.VoidValue(), err
	}
	_, err = call.InvokeSpecial(ticker, TickerClass, "init", "("+stringDescriptor+")V", arguments[1])
	return jvm.VoidValue(), err
}

// displayableRepaintIM is one vendor's addition to Displayable: a repaint that
// includes the input method's own area, which a handset drew over the screen
// while a text field had focus. One local title calls it from the text
// component it hands the platform, after every edit that changed what is on
// screen.
//
// There is no input method here to have an area — see the text component
// handler in the SKVM surface — so what is left of the call is the repaint the
// title expects to go with it. It is asked for virtually, so a Canvas gets its
// own; a Displayable that is not one has nothing to repaint and the call is
// where the title left it.
func displayableRepaintIM(call *jvm.Invocation, arguments []jvm.Value) (jvm.Value, error) {
	displayable, err := receiver(arguments)
	if err != nil {
		return jvm.VoidValue(), err
	}
	canvas, err := call.VM().IsSubclassOf(displayable.ClassName, CanvasClass)
	if err != nil {
		return jvm.VoidValue(), err
	}
	if !canvas {
		return jvm.VoidValue(), nil
	}
	_, err = call.InvokeVirtual(displayable, "repaint", "()V")
	return jvm.VoidValue(), err
}
