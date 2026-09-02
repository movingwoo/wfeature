package wipi

import (
	"github.com/movingwoo/wfeature/internal/api/midp"
	"github.com/movingwoo/wfeature/internal/jvm"
)

// definitions is the WIPI Java surface the local corpus links against, and
// nothing beyond it. Each class names its MIDP counterpart as its superclass,
// so a member the two share is inherited rather than declared twice: the whole
// of `Graphics` below is the handful of calls MIDP does not have.
func definitions() []jvm.ClassDefinition {
	return []jvm.ClassDefinition{
		eventQueueDefinition(),
		jletDefinition(),
		displayDefinition(),
		cardDefinition(),
		graphicsDefinition(),
		imageDefinition(),
		fontDefinition(),
		backLightDefinition(),
		fileDefinition(),
		fileSystemDefinition(),
		clipDefinition(),
		playListenerDefinition(),
		playerDefinition(),
		vibratorDefinition(),
		networkDefinition(),
		socketDefinition(),
		urlDefinition(),
	}
}

// eventQueueDefinition publishes the constants a Card's keyNotify is read
// against. The game keys are the numbers MIDP uses for the same directions,
// which is why the platform's own key table serves both.
func eventQueueDefinition() jvm.ClassDefinition {
	return jvm.ClassDefinition{
		Name:      EventQueueClass,
		SuperName: "java/lang/Object",
		Access:    jvm.AccessPublic,
		Fields: []jvm.FieldDefinition{
			{Name: "KEY_EVENT", Descriptor: "I", Access: constantField, Constant: jvm.IntValue(1)},
			{Name: "KEY_PRESSED", Descriptor: "I", Access: constantField, Constant: jvm.IntValue(1)},
			{Name: "KEY_RELEASED", Descriptor: "I", Access: constantField, Constant: jvm.IntValue(2)},
			{Name: "KEY_REPEATED", Descriptor: "I", Access: constantField, Constant: jvm.IntValue(3)},
			{Name: "KEY_TYPED", Descriptor: "I", Access: constantField, Constant: jvm.IntValue(4)},
			{Name: "UP", Descriptor: "I", Access: constantField, Constant: jvm.IntValue(1)},
			{Name: "LEFT", Descriptor: "I", Access: constantField, Constant: jvm.IntValue(2)},
			{Name: "RIGHT", Descriptor: "I", Access: constantField, Constant: jvm.IntValue(5)},
			{Name: "DOWN", Descriptor: "I", Access: constantField, Constant: jvm.IntValue(6)},
			{Name: "FIRE", Descriptor: "I", Access: constantField, Constant: jvm.IntValue(8)},
			{Name: "GAME_A", Descriptor: "I", Access: constantField, Constant: jvm.IntValue(9)},
			{Name: "GAME_B", Descriptor: "I", Access: constantField, Constant: jvm.IntValue(10)},
			{Name: "GAME_C", Descriptor: "I", Access: constantField, Constant: jvm.IntValue(11)},
			{Name: "GAME_D", Descriptor: "I", Access: constantField, Constant: jvm.IntValue(12)},
			{Name: "CLEAR", Descriptor: "I", Access: constantField, Constant: jvm.IntValue(99)},
			{Name: "SOFT1", Descriptor: "I", Access: constantField, Constant: jvm.IntValue(21)},
			{Name: "SOFT2", Descriptor: "I", Access: constantField, Constant: jvm.IntValue(22)},
			{Name: "KEY_NUM0", Descriptor: "I", Access: constantField, Constant: jvm.IntValue(48)},
			{Name: "KEY_NUM1", Descriptor: "I", Access: constantField, Constant: jvm.IntValue(49)},
			{Name: "KEY_NUM2", Descriptor: "I", Access: constantField, Constant: jvm.IntValue(50)},
			{Name: "KEY_NUM3", Descriptor: "I", Access: constantField, Constant: jvm.IntValue(51)},
			{Name: "KEY_NUM4", Descriptor: "I", Access: constantField, Constant: jvm.IntValue(52)},
			{Name: "KEY_NUM5", Descriptor: "I", Access: constantField, Constant: jvm.IntValue(53)},
			{Name: "KEY_NUM6", Descriptor: "I", Access: constantField, Constant: jvm.IntValue(54)},
			{Name: "KEY_NUM7", Descriptor: "I", Access: constantField, Constant: jvm.IntValue(55)},
			{Name: "KEY_NUM8", Descriptor: "I", Access: constantField, Constant: jvm.IntValue(56)},
			{Name: "KEY_NUM9", Descriptor: "I", Access: constantField, Constant: jvm.IntValue(57)},
			{Name: "KEY_STAR", Descriptor: "I", Access: constantField, Constant: jvm.IntValue(42)},
			{Name: "KEY_POUND", Descriptor: "I", Access: constantField, Constant: jvm.IntValue(35)},
		},
		Methods: []jvm.MethodDefinition{
			{Name: "<init>", Descriptor: "()V", Access: publicMethod, Body: superInit("java/lang/Object", "()V")},
		},
	}
}

// jletDefinition is the WIPI application class. It extends MIDlet so that the
// lifecycle a Host already drives — construct, start, pause, destroy — reaches
// a Jlet unchanged; what it adds is the argument array WIPI hands startApp and
// the two statics a title uses to find itself.
func jletDefinition() jvm.ClassDefinition {
	return jvm.ClassDefinition{
		Name:      JletClass,
		SuperName: midp.MIDletClass,
		Access:    jvm.AccessPublic | jvm.AccessAbstract,
		Fields: []jvm.FieldDefinition{
			{Name: "ACTIVE", Descriptor: "I", Access: constantField, Constant: jvm.IntValue(1)},
			{Name: "PAUSED", Descriptor: "I", Access: constantField, Constant: jvm.IntValue(2)},
			{Name: "DESTROYED", Descriptor: "I", Access: constantField, Constant: jvm.IntValue(3)},
		},
		Methods: []jvm.MethodDefinition{
			{Name: "<init>", Descriptor: "()V", Access: protectedMethod, Body: jletInit},
			// The MIDlet lifecycle, translated. A Host calls the left-hand
			// side and the title implements the right-hand side; the two
			// pauseApp and destroyApp shapes are the same, so only the start
			// needs a body of its own.
			{Name: "startApp", Descriptor: "()V", Access: protectedMethod, Body: jletStartApp},
			{Name: "startApp", Descriptor: "([Ljava/lang/String;)V", Access: jvm.AccessProtected | jvm.AccessAbstract},
			{Name: "pauseApp", Descriptor: "()V", Access: protectedMethod, Body: doNothing},
			{Name: "resumeApp", Descriptor: "()V", Access: protectedMethod, Body: doNothing},
			{Name: "destroyApp", Descriptor: "(Z)V", Access: jvm.AccessProtected | jvm.AccessAbstract},
			{Name: "notifyDestroyed", Descriptor: "()V", Access: publicMethod, Body: forwardSpecial(midp.MIDletClass, "notifyDestroyed", "()V")},
			{Name: "getAppProperty", Descriptor: "(Ljava/lang/String;)Ljava/lang/String;", Access: publicMethod, Body: forwardSpecial(midp.MIDletClass, "getAppProperty", "(Ljava/lang/String;)Ljava/lang/String;")},
			{Name: "getActiveJlet", Descriptor: "()Lorg/kwis/msp/lcdui/Jlet;", Access: publicStaticNat},
			{Name: "getCurrentJlet", Descriptor: "()Lorg/kwis/msp/lcdui/Jlet;", Access: publicStaticNat},
			{Name: "getCurrentProgramID", Descriptor: "()I", Access: publicMethod, Body: answerInt(0)},
			{Name: "getEventQueue", Descriptor: "()Lorg/kwis/msp/lcdui/EventQueue;", Access: publicNative},
		},
	}
}

func jletInit(call *jvm.Invocation, arguments []jvm.Value) (jvm.Value, error) {
	object, err := receiver(arguments)
	if err != nil {
		return jvm.VoidValue(), err
	}
	if _, err := call.InvokeSpecial(object, midp.MIDletClass, "<init>", "()V"); err != nil {
		return jvm.VoidValue(), err
	}
	return jvm.VoidValue(), nil
}

// jletStartApp is the MIDlet entry point translated into the WIPI one. The
// argument array is what `System.execute` would have passed and nothing here
// executes a program with arguments, so it is empty rather than null: the
// specification hands one over and a title that reads its length would fail on
// the other answer.
func jletStartApp(call *jvm.Invocation, arguments []jvm.Value) (jvm.Value, error) {
	object, err := receiver(arguments)
	if err != nil {
		return jvm.VoidValue(), err
	}
	empty, err := call.VM().NewArray(jvm.Type{Kind: jvm.TypeReference, ClassName: "java/lang/String"}, 0)
	if err != nil {
		return jvm.VoidValue(), err
	}
	_, err = call.InvokeVirtual(object, "startApp", "([Ljava/lang/String;)V", jvm.ReferenceValue(empty))
	return jvm.VoidValue(), err
}

// displayDefinition is the WIPI screen. It extends the MIDP Display so the one
// display object a session owns answers both vocabularies, and the card stack
// is the platform's: pushCard is what setCurrent is here.
func displayDefinition() jvm.ClassDefinition {
	return jvm.ClassDefinition{
		Name:      DisplayClass,
		SuperName: midp.DisplayClass,
		Access:    jvm.AccessPublic,
		Methods: []jvm.MethodDefinition{
			{Name: "getDefaultDisplay", Descriptor: "()Lorg/kwis/msp/lcdui/Display;", Access: publicStaticNat},
			{Name: "getDisplay", Descriptor: "(Ljava/lang/String;)Lorg/kwis/msp/lcdui/Display;", Access: publicStaticNat},
			{Name: "pushCard", Descriptor: "(Lorg/kwis/msp/lcdui/Card;)V", Access: publicNative},
			{Name: "popCard", Descriptor: "()Lorg/kwis/msp/lcdui/Card;", Access: publicNative},
			{Name: "removeCard", Descriptor: "(Lorg/kwis/msp/lcdui/Card;)Z", Access: publicNative},
			{Name: "removeAllCards", Descriptor: "()V", Access: publicNative},
			{Name: "countCard", Descriptor: "()I", Access: publicNative},
			{Name: "getWidth", Descriptor: "()I", Access: publicNative},
			{Name: "getHeight", Descriptor: "()I", Access: publicNative},
			{Name: "flush", Descriptor: "()V", Access: publicNative},
			{Name: "callSerially", Descriptor: "(Ljava/lang/Runnable;)V", Access: publicMethod, Body: forwardSpecial(midp.DisplayClass, "callSerially", "(Ljava/lang/Runnable;)V")},
			// The timeout form runs on the same queue: this platform takes one
			// serial Runnable per Host pass, so a delay it cannot honour is
			// better spent than refused.
			{Name: "callSerially", Descriptor: "(Ljava/lang/Runnable;I)V", Access: publicMethod, Body: callSeriallyTimeout},
			// getGameAction is a static here and an instance method on a MIDP
			// Canvas, and the mapping is the platform's one table.
			{Name: "getGameAction", Descriptor: "(I)I", Access: publicStaticNat},
			{Name: "getKeyCode", Descriptor: "(I)I", Access: publicStaticNat},
			{Name: "getKeyName", Descriptor: "(I)Ljava/lang/String;", Access: publicStaticNat},
			{Name: "isDoubleBuffered", Descriptor: "()Z", Access: publicMethod, Body: answerBool(true)},
			{Name: "isColor", Descriptor: "()Z", Access: publicMethod, Body: answerBool(true)},
			{Name: "numColors", Descriptor: "()I", Access: publicMethod, Body: answerInt(1 << 24)},
			{Name: "getBitsPerPixel", Descriptor: "()I", Access: publicMethod, Body: answerInt(24)},
			{Name: "hasPointerEvents", Descriptor: "()Z", Access: publicMethod, Body: answerBool(false)},
			{Name: "hasPointerMotionEvents", Descriptor: "()Z", Access: publicMethod, Body: answerBool(false)},
			{Name: "hasRepeatEvents", Descriptor: "()Z", Access: publicMethod, Body: answerBool(true)},
		},
	}
}

func callSeriallyTimeout(call *jvm.Invocation, arguments []jvm.Value) (jvm.Value, error) {
	object, err := receiver(arguments)
	if err != nil {
		return jvm.VoidValue(), err
	}
	if len(arguments) < 3 {
		return jvm.VoidValue(), jvm.Throw("java/lang/IllegalArgumentException", "callSerially takes a Runnable and a timeout")
	}
	return call.InvokeSpecial(object, midp.DisplayClass, "callSerially", "(Ljava/lang/Runnable;)V", arguments[1])
}

// cardDefinition is what a WIPI program draws on. It extends the MIDP Canvas,
// so the platform's paint scheduling, its key delivery and its notion of what
// is on screen all reach a Card unchanged; the two bridges below are where the
// vocabularies differ.
func cardDefinition() jvm.ClassDefinition {
	return jvm.ClassDefinition{
		Name:      CardClass,
		SuperName: midp.CanvasClass,
		Access:    jvm.AccessPublic | jvm.AccessAbstract,
		Fields: []jvm.FieldDefinition{
			{Name: "x", Descriptor: "I", Access: jvm.AccessProtected},
			{Name: "y", Descriptor: "I", Access: jvm.AccessProtected},
			{Name: "w", Descriptor: "I", Access: jvm.AccessProtected},
			{Name: "h", Descriptor: "I", Access: jvm.AccessProtected},
			{Name: "bTrans", Descriptor: "Z", Access: jvm.AccessProtected},
		},
		Methods: []jvm.MethodDefinition{
			{Name: "<init>", Descriptor: "()V", Access: publicMethod, Body: cardInit},
			{Name: "<init>", Descriptor: "(Z)V", Access: publicMethod, Body: cardInit},
			{Name: "<init>", Descriptor: "(Lorg/kwis/msp/lcdui/Display;)V", Access: publicMethod, Body: cardInit},
			{Name: "<init>", Descriptor: "(IIII)V", Access: publicMethod, Body: cardInitBounds},
			{Name: "<init>", Descriptor: "(Lorg/kwis/msp/lcdui/Display;IIII)V", Access: publicMethod, Body: cardInitDisplayBounds},
			{Name: "<init>", Descriptor: "(Lorg/kwis/msp/lcdui/Display;IIIIZ)V", Access: publicMethod, Body: cardInitDisplayBounds},
			// The bridge a Host's paint lands on. A Card overrides paint with
			// the WIPI Graphics in its signature, and the object it is handed
			// is one — see the package comment.
			{Name: "paint", Descriptor: "(Ljavax/microedition/lcdui/Graphics;)V", Access: protectedMethod, Body: cardPaintBridge},
			{Name: "paint", Descriptor: "(Lorg/kwis/msp/lcdui/Graphics;)V", Access: jvm.AccessProtected | jvm.AccessAbstract},
			// The other bridge. MIDP delivers three separate callbacks and
			// WIPI delivers one with the type as its first argument, so each
			// of the three names the type the specification gives it.
			{Name: "keyPressed", Descriptor: "(I)V", Access: protectedMethod, Body: cardKeyBridge(1)},
			{Name: "keyReleased", Descriptor: "(I)V", Access: protectedMethod, Body: cardKeyBridge(2)},
			{Name: "keyRepeated", Descriptor: "(I)V", Access: protectedMethod, Body: cardKeyBridge(3)},
			{Name: "keyNotify", Descriptor: "(II)Z", Access: protectedMethod, Body: answerBool(false)},
			{Name: "pointerNotify", Descriptor: "(III)Z", Access: protectedMethod, Body: answerBool(false)},
			{Name: "showNotify", Descriptor: "(Z)V", Access: protectedMethod, Body: doNothing},
			{Name: "getDisplay", Descriptor: "()Lorg/kwis/msp/lcdui/Display;", Access: publicNative},
			{Name: "getWidth", Descriptor: "()I", Access: publicMethod, Body: forwardSpecial(midp.CanvasClass, "getWidth", "()I")},
			{Name: "getHeight", Descriptor: "()I", Access: publicMethod, Body: forwardSpecial(midp.CanvasClass, "getHeight", "()I")},
			{Name: "getX", Descriptor: "()I", Access: publicMethod, Body: cardField("x")},
			{Name: "getY", Descriptor: "()I", Access: publicMethod, Body: cardField("y")},
			{Name: "isShown", Descriptor: "()Z", Access: publicMethod, Body: forwardSpecial(midp.DisplayableClass, "isShown", "()Z")},
			{Name: "repaint", Descriptor: "()V", Access: publicMethod, Body: forwardSpecial(midp.CanvasClass, "repaint", "()V")},
			{Name: "repaint", Descriptor: "(IIII)V", Access: publicMethod, Body: forwardSpecial(midp.CanvasClass, "repaint", "(IIII)V")},
			{Name: "serviceRepaints", Descriptor: "()V", Access: publicMethod, Body: forwardSpecial(midp.CanvasClass, "serviceRepaints", "()V")},
			{Name: "move", Descriptor: "(II)V", Access: publicNative},
			{Name: "resize", Descriptor: "(II)V", Access: publicNative},
		},
	}
}

func cardField(name string) jvm.ContextMethod {
	return func(_ *jvm.Invocation, arguments []jvm.Value) (jvm.Value, error) {
		object, err := receiver(arguments)
		if err != nil {
			return jvm.VoidValue(), err
		}
		value, ok := object.Fields[name]
		if !ok {
			return jvm.IntValue(0), nil
		}
		return value, nil
	}
}

func cardInit(call *jvm.Invocation, arguments []jvm.Value) (jvm.Value, error) {
	object, err := receiver(arguments)
	if err != nil {
		return jvm.VoidValue(), err
	}
	if _, err := call.InvokeSpecial(object, midp.CanvasClass, "<init>", "()V"); err != nil {
		return jvm.VoidValue(), err
	}
	return jvm.VoidValue(), setCardBounds(call, object, 0, 0, -1, -1)
}

func cardInitBounds(call *jvm.Invocation, arguments []jvm.Value) (jvm.Value, error) {
	object, err := receiver(arguments)
	if err != nil {
		return jvm.VoidValue(), err
	}
	if _, err := call.InvokeSpecial(object, midp.CanvasClass, "<init>", "()V"); err != nil {
		return jvm.VoidValue(), err
	}
	return jvm.VoidValue(), setCardBoundsFrom(call, object, arguments, 1)
}

func cardInitDisplayBounds(call *jvm.Invocation, arguments []jvm.Value) (jvm.Value, error) {
	object, err := receiver(arguments)
	if err != nil {
		return jvm.VoidValue(), err
	}
	if _, err := call.InvokeSpecial(object, midp.CanvasClass, "<init>", "()V"); err != nil {
		return jvm.VoidValue(), err
	}
	if len(arguments) < 6 {
		return jvm.VoidValue(), setCardBounds(call, object, 0, 0, -1, -1)
	}
	return jvm.VoidValue(), setCardBoundsFrom(call, object, arguments, 2)
}

func setCardBoundsFrom(call *jvm.Invocation, object *jvm.Object, arguments []jvm.Value, start int) error {
	values := make([]int32, 4)
	for index := range values {
		if start+index >= len(arguments) {
			return jvm.Throw("java/lang/IllegalArgumentException", "Card bounds are incomplete")
		}
		value, err := arguments[start+index].Int32()
		if err != nil {
			return err
		}
		values[index] = value
	}
	return setCardBounds(call, object, values[0], values[1], values[2], values[3])
}

// setCardBounds fills the four protected fields a title reads directly. A card
// made without bounds is the whole screen, which is what the two no-size
// constructors mean, and the screen is what the Canvas underneath answers.
func setCardBounds(call *jvm.Invocation, object *jvm.Object, x, y, w, h int32) error {
	if w < 0 || h < 0 {
		width, err := call.InvokeVirtual(object, "getWidth", "()I")
		if err != nil {
			return err
		}
		height, err := call.InvokeVirtual(object, "getHeight", "()I")
		if err != nil {
			return err
		}
		w, _ = width.Int32()
		h, _ = height.Int32()
	}
	if object.Fields == nil {
		object.Fields = make(map[string]jvm.Value)
	}
	object.Fields["x"] = jvm.IntValue(x)
	object.Fields["y"] = jvm.IntValue(y)
	object.Fields["w"] = jvm.IntValue(w)
	object.Fields["h"] = jvm.IntValue(h)
	return nil
}

func cardPaintBridge(call *jvm.Invocation, arguments []jvm.Value) (jvm.Value, error) {
	object, err := receiver(arguments)
	if err != nil {
		return jvm.VoidValue(), err
	}
	if len(arguments) < 2 {
		return jvm.VoidValue(), jvm.Throw("java/lang/NullPointerException", "Card.paint has no Graphics")
	}
	_, err = call.InvokeVirtual(object, "paint", "(Lorg/kwis/msp/lcdui/Graphics;)V", arguments[1])
	return jvm.VoidValue(), err
}

func cardKeyBridge(eventType int32) jvm.ContextMethod {
	return func(call *jvm.Invocation, arguments []jvm.Value) (jvm.Value, error) {
		object, err := receiver(arguments)
		if err != nil {
			return jvm.VoidValue(), err
		}
		if len(arguments) < 2 {
			return jvm.VoidValue(), jvm.Throw("java/lang/IllegalArgumentException", "key event has no code")
		}
		_, err = call.InvokeVirtual(object, "keyNotify", "(II)Z", jvm.IntValue(eventType), arguments[1])
		return jvm.VoidValue(), err
	}
}

// graphicsDefinition is the MIDP Graphics plus what WIPI has and MIDP does
// not. Everything a title draws with that both standards share — the fills,
// the lines, the clip, the colour — is inherited.
func graphicsDefinition() jvm.ClassDefinition {
	return jvm.ClassDefinition{
		Name:      GraphicsClass,
		SuperName: midp.GraphicsClass,
		Access:    jvm.AccessPublic,
		Methods: []jvm.MethodDefinition{
			{Name: "<init>", Descriptor: "()V", Access: publicMethod, Body: superInit(midp.GraphicsClass, "()V")},
			{Name: "setColor", Descriptor: "(III)V", Access: publicMethod, Body: forwardSpecial(midp.GraphicsClass, "setColor", "(III)V")},
			{Name: "drawImage", Descriptor: "(Lorg/kwis/msp/lcdui/Image;III)V", Access: publicMethod, Body: forwardSpecial(midp.GraphicsClass, "drawImage", "(Ljavax/microedition/lcdui/Image;III)V")},
			{Name: "drawImage", Descriptor: "(Lorg/kwis/msp/lcdui/Image;IIIIIII)V", Access: publicNative},
			{Name: "getFont", Descriptor: "()Lorg/kwis/msp/lcdui/Font;", Access: publicNative},
			{Name: "setFont", Descriptor: "(Lorg/kwis/msp/lcdui/Font;)V", Access: publicMethod, Body: forwardSpecial(midp.GraphicsClass, "setFont", "(Ljavax/microedition/lcdui/Font;)V")},
			// The two WIPI has of its own. setAlpha is a blend factor this
			// platform keeps and reports, and reset puts back the state a
			// fresh Graphics would have had — both are already the SKVM
			// contracts of the same name.
			{Name: "setAlpha", Descriptor: "(I)V", Access: publicNative},
			{Name: "getAlpha", Descriptor: "()I", Access: publicNative},
			{Name: "reset", Descriptor: "()V", Access: publicNative},
			{Name: "getRGBPixels", Descriptor: "(IIII[III)V", Access: publicNative},
			{Name: "setRGBPixels", Descriptor: "(IIII[III)V", Access: publicNative},
			{Name: "drawChar", Descriptor: "(CIII)V", Access: publicNative},
			{Name: "clipRect", Descriptor: "(IIII)V", Access: publicNative},
			{Name: "drawArc", Descriptor: "(IIIIII)V", Access: publicMethod, Body: forwardSpecial(midp.GraphicsClass, "drawArc", "(IIIIII)V")},
			{Name: "fillArc", Descriptor: "(IIIIII)V", Access: publicMethod, Body: forwardSpecial(midp.GraphicsClass, "fillArc", "(IIIIII)V")},
		},
	}
}

// imageDefinition is the MIDP Image under a WIPI name. The factories are
// natives because they have to build an object carrying this class name, which
// is what a Card's own code will dispatch on.
func imageDefinition() jvm.ClassDefinition {
	return jvm.ClassDefinition{
		Name:      ImageClass,
		SuperName: midp.ImageClass,
		Access:    jvm.AccessPublic,
		Methods: []jvm.MethodDefinition{
			{Name: "createImage", Descriptor: "(II)Lorg/kwis/msp/lcdui/Image;", Access: publicStaticNat},
			{Name: "createImage", Descriptor: "(Ljava/lang/String;)Lorg/kwis/msp/lcdui/Image;", Access: publicStaticNat},
			{Name: "createImage", Descriptor: "([BII)Lorg/kwis/msp/lcdui/Image;", Access: publicStaticNat},
			{Name: "createImage", Descriptor: "(Lorg/kwis/msp/lcdui/Image;IIII)Lorg/kwis/msp/lcdui/Image;", Access: publicStaticNat},
			{Name: "getGraphics", Descriptor: "()Lorg/kwis/msp/lcdui/Graphics;", Access: publicNative},
		},
	}
}

func fontDefinition() jvm.ClassDefinition {
	return jvm.ClassDefinition{
		Name:      FontClass,
		SuperName: midp.FontClass,
		Access:    jvm.AccessPublic,
		Fields: []jvm.FieldDefinition{
			{Name: "FACE_SYSTEM", Descriptor: "I", Access: constantField, Constant: jvm.IntValue(0)},
			{Name: "FACE_MONOSPACE", Descriptor: "I", Access: constantField, Constant: jvm.IntValue(32)},
			{Name: "FACE_PROPORTIONAL", Descriptor: "I", Access: constantField, Constant: jvm.IntValue(64)},
			{Name: "STYLE_PLAIN", Descriptor: "I", Access: constantField, Constant: jvm.IntValue(0)},
			{Name: "STYLE_BOLD", Descriptor: "I", Access: constantField, Constant: jvm.IntValue(1)},
			{Name: "STYLE_ITALIC", Descriptor: "I", Access: constantField, Constant: jvm.IntValue(2)},
			{Name: "STYLE_UNDERLINED", Descriptor: "I", Access: constantField, Constant: jvm.IntValue(4)},
			{Name: "SIZE_SMALL", Descriptor: "I", Access: constantField, Constant: jvm.IntValue(8)},
			{Name: "SIZE_MEDIUM", Descriptor: "I", Access: constantField, Constant: jvm.IntValue(0)},
			{Name: "SIZE_LARGE", Descriptor: "I", Access: constantField, Constant: jvm.IntValue(16)},
		},
		Methods: []jvm.MethodDefinition{
			{Name: "getDefaultFont", Descriptor: "()Lorg/kwis/msp/lcdui/Font;", Access: publicStaticNat},
			{Name: "getFont", Descriptor: "(III)Lorg/kwis/msp/lcdui/Font;", Access: publicStaticNat},
		},
	}
}

func backLightDefinition() jvm.ClassDefinition {
	return jvm.ClassDefinition{
		Name:      BackLightClass,
		SuperName: "java/lang/Object",
		Access:    jvm.AccessPublic | jvm.AccessFinal,
		Methods: []jvm.MethodDefinition{
			// There is no lamp to drive, which is the decision `skvm.md`
			// records for the SKVM class of the same purpose.
			{Name: "alwaysOn", Descriptor: "()V", Access: publicStatic, Body: doNothing},
			{Name: "on", Descriptor: "(III)V", Access: publicStatic, Body: doNothing},
			{Name: "off", Descriptor: "()V", Access: publicStatic, Body: doNothing},
			{Name: "before", Descriptor: "()V", Access: publicStatic, Body: doNothing},
		},
	}
}

// fileDefinition and fileSystemDefinition are WIPI's own filesystem. They go
// through the same Host save boundary the SKVM `XFile` and MIDP RMS use, so a
// title persists into the one owner directory whichever vocabulary it writes
// with.
func fileDefinition() jvm.ClassDefinition {
	return jvm.ClassDefinition{
		Name:      FileClass,
		SuperName: "java/lang/Object",
		Access:    jvm.AccessPublic,
		Fields: []jvm.FieldDefinition{
			{Name: "READ_ONLY", Descriptor: "I", Access: constantField, Constant: jvm.IntValue(1)},
			{Name: "WRITE_ONLY", Descriptor: "I", Access: constantField, Constant: jvm.IntValue(2)},
			{Name: "READ_WRITE", Descriptor: "I", Access: constantField, Constant: jvm.IntValue(3)},
		},
		Methods: []jvm.MethodDefinition{
			{Name: "<init>", Descriptor: "(Ljava/lang/String;I)V", Access: publicNative},
			{Name: "<init>", Descriptor: "(Ljava/lang/String;II)V", Access: publicNative},
			{Name: "read", Descriptor: "([B)I", Access: publicNative},
			{Name: "read", Descriptor: "([BII)I", Access: publicNative},
			{Name: "write", Descriptor: "([B)I", Access: publicNative},
			{Name: "write", Descriptor: "([BII)I", Access: publicNative},
			{Name: "seek", Descriptor: "(I)V", Access: publicNative},
			{Name: "tell", Descriptor: "()I", Access: publicNative},
			{Name: "sizeOf", Descriptor: "()I", Access: publicNative},
			{Name: "close", Descriptor: "()V", Access: publicNative},
			{Name: "openInputStream", Descriptor: "()Ljava/io/InputStream;", Access: publicNative},
			{Name: "openDataInputStream", Descriptor: "()Ljava/io/DataInputStream;", Access: publicNative},
			{Name: "openOutputStream", Descriptor: "()Ljava/io/OutputStream;", Access: publicNative},
			{Name: "openDataOutputStream", Descriptor: "()Ljava/io/DataOutputStream;", Access: publicNative},
		},
	}
}

func fileSystemDefinition() jvm.ClassDefinition {
	return jvm.ClassDefinition{
		Name:      FileSystemClass,
		SuperName: "java/lang/Object",
		Access:    jvm.AccessPublic | jvm.AccessFinal,
		Methods: []jvm.MethodDefinition{
			{Name: "exists", Descriptor: "(Ljava/lang/String;)Z", Access: publicStaticNat},
			{Name: "isFile", Descriptor: "(Ljava/lang/String;)Z", Access: publicStaticNat},
			{Name: "isDirectory", Descriptor: "(Ljava/lang/String;)Z", Access: publicStaticNat},
			{Name: "remove", Descriptor: "(Ljava/lang/String;)V", Access: publicStaticNat},
			{Name: "mkdir", Descriptor: "(Ljava/lang/String;)V", Access: publicStaticNat},
			{Name: "sizeOf", Descriptor: "(Ljava/lang/String;)I", Access: publicStaticNat},
		},
	}
}

// clipDefinition and its two companions are WIPI's sound. A Clip holds the
// bytes and a name for the format; the Player starts and stops one. Both go
// through the platform's own audio path, which is where the formats this
// project decodes are decided (`docs/audio.md`).
func clipDefinition() jvm.ClassDefinition {
	return jvm.ClassDefinition{
		Name:      ClipClass,
		SuperName: "java/lang/Object",
		Access:    jvm.AccessPublic,
		Methods: []jvm.MethodDefinition{
			{Name: "<init>", Descriptor: "(Ljava/lang/String;[B)V", Access: publicNative},
			{Name: "<init>", Descriptor: "(Ljava/lang/String;Ljava/lang/String;)V", Access: publicNative},
			{Name: "setListener", Descriptor: "(Lorg/kwis/msp/media/PlayListener;)V", Access: publicNative},
			{Name: "setVolume", Descriptor: "(I)V", Access: publicNative},
			{Name: "getVolume", Descriptor: "()I", Access: publicNative},
			{Name: "getType", Descriptor: "()Ljava/lang/String;", Access: publicNative},
		},
	}
}

func playListenerDefinition() jvm.ClassDefinition {
	return jvm.ClassDefinition{
		Name:      PlayListenerClass,
		SuperName: "java/lang/Object",
		Access:    jvm.AccessPublic | jvm.AccessInterface | jvm.AccessAbstract,
		Methods: []jvm.MethodDefinition{
			{Name: "playDone", Descriptor: "(Lorg/kwis/msp/media/Clip;)V", Access: jvm.AccessPublic | jvm.AccessAbstract},
		},
	}
}

func playerDefinition() jvm.ClassDefinition {
	return jvm.ClassDefinition{
		Name:      PlayerClass,
		SuperName: "java/lang/Object",
		Access:    jvm.AccessPublic | jvm.AccessFinal,
		Methods: []jvm.MethodDefinition{
			{Name: "play", Descriptor: "(Lorg/kwis/msp/media/Clip;Z)Z", Access: publicStaticNat},
			{Name: "stop", Descriptor: "(Lorg/kwis/msp/media/Clip;)Z", Access: publicStaticNat},
			{Name: "pause", Descriptor: "(Lorg/kwis/msp/media/Clip;)Z", Access: publicStaticNat},
			{Name: "resume", Descriptor: "(Lorg/kwis/msp/media/Clip;)Z", Access: publicStaticNat},
		},
	}
}

func vibratorDefinition() jvm.ClassDefinition {
	return jvm.ClassDefinition{
		Name:      VibratorClass,
		SuperName: "java/lang/Object",
		Access:    jvm.AccessPublic | jvm.AccessFinal,
		Methods: []jvm.MethodDefinition{
			// No hardware to drive, the decision the SKVM Vibration takes.
			{Name: "on", Descriptor: "(II)V", Access: publicStatic, Body: doNothing},
			{Name: "off", Descriptor: "()V", Access: publicStatic, Body: doNothing},
		},
	}
}

// networkDefinition, socketDefinition and urlDefinition are WIPI's radio, and
// nothing behind them connects. `docs/network.md` carries the reason this
// project answers no network on any platform: reporting a connection a title
// can never read from is worse than reporting none. The classes exist so a
// title that names one resolves it and reaches its own error path.
func networkDefinition() jvm.ClassDefinition {
	return jvm.ClassDefinition{
		Name:      NetworkClass,
		SuperName: "java/lang/Object",
		Access:    jvm.AccessPublic | jvm.AccessFinal,
		Methods: []jvm.MethodDefinition{
			{Name: "connect", Descriptor: "()I", Access: publicStatic, Body: answerInt(-1)},
			{Name: "disconnect", Descriptor: "()V", Access: publicStatic, Body: doNothing},
			{Name: "isConnected", Descriptor: "()Z", Access: publicStatic, Body: answerBool(false)},
		},
	}
}

func socketDefinition() jvm.ClassDefinition {
	return jvm.ClassDefinition{
		Name:      SocketClass,
		SuperName: "java/lang/Object",
		Access:    jvm.AccessPublic,
		Methods: []jvm.MethodDefinition{
			{Name: "close", Descriptor: "()V", Access: publicMethod, Body: doNothing},
			{Name: "getInputStream", Descriptor: "()Ljava/io/InputStream;", Access: publicMethod, Body: refuseConnection},
			{Name: "getOutputStream", Descriptor: "()Ljava/io/OutputStream;", Access: publicMethod, Body: refuseConnection},
		},
	}
}

func urlDefinition() jvm.ClassDefinition {
	return jvm.ClassDefinition{
		Name:      URLClass,
		SuperName: "java/lang/Object",
		Access:    jvm.AccessPublic | jvm.AccessFinal,
		Methods: []jvm.MethodDefinition{
			{Name: "find", Descriptor: "(Ljava/lang/String;)Lorg/kwis/msf/io/Socket;", Access: publicStatic, Body: refuseConnection},
		},
	}
}

func refuseConnection(*jvm.Invocation, []jvm.Value) (jvm.Value, error) {
	return jvm.VoidValue(), jvm.Throw("java/io/IOException", "this runtime has no network")
}
