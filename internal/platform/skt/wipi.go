package skt

import (
	"fmt"
	"strings"

	"github.com/movingwoo/wfeature/internal/api/midp"
	"github.com/movingwoo/wfeature/internal/api/wipi"
	"github.com/movingwoo/wfeature/internal/jvm"
)

// The WIPI Java half of this container
//
// Most archives here hold a MIDlet; three hold a Jlet, which is the same
// packaging with a WIPI application class inside. `internal/api/wipi` declares
// that surface as a layer over MIDP — the reasons are in that package's own
// comment — and what is left for the platform is the half a class definition
// cannot carry: the objects the WIPI classes hand a title, and the calls that
// need the runtime behind them.
//
// **A Jlet session stamps its objects with the WIPI class names**, because
// `invokevirtual` resolves on the class of the receiver: a Card's own code
// calls `Graphics.setAlpha`, and an object named `javax/microedition/lcdui/
// Graphics` would not resolve it. A MIDlet session stamps the MIDP names as it
// always did, so nothing about the other eighty-eight archives changes.

// wipiClassNames answers the pair of names one of the shared display classes
// goes by in this session.
func (runtime *Runtime) graphicsClassName() string {
	if runtime.jlet {
		return wipi.GraphicsClass
	}
	return midp.GraphicsClass
}

func (runtime *Runtime) imageClassName() string {
	if runtime.jlet {
		return wipi.ImageClass
	}
	return midp.ImageClass
}

func (runtime *Runtime) fontClassName() string {
	if runtime.jlet {
		return wipi.FontClass
	}
	return midp.FontClass
}

// isGraphicsClass, isImageClass and isFontClass are the receiver checks the
// natives make. They accept both names rather than the session's own, because
// a check is about what the object *is* and an object outlives the question of
// which vocabulary made it.
func isGraphicsClass(name string) bool {
	return name == midp.GraphicsClass || name == wipi.GraphicsClass
}

func isImageClass(name string) bool {
	return name == midp.ImageClass || name == wipi.ImageClass
}

func isFontClass(name string) bool {
	return name == midp.FontClass || name == wipi.FontClass
}

// wipiRegistrations are the members `internal/api/wipi` declares native: the
// ones that need the runtime, and the factories that have to stamp a WIPI
// class name on what they build. Most of them are the MIDP handler of the same
// contract, registered again under the WIPI class — a handler reads its
// arguments and never its descriptor, so one implementation serves both.
func (runtime *Runtime) wipiRegistrations() []nativeRegistration {
	const text = "Ljava/lang/String;"
	return []nativeRegistration{
		{wipi.JletClass, "getActiveJlet", "()Lorg/kwis/msp/lcdui/Jlet;", runtime.wipiActiveJlet},
		{wipi.JletClass, "getCurrentJlet", "()Lorg/kwis/msp/lcdui/Jlet;", runtime.wipiActiveJlet},
		{wipi.JletClass, "getEventQueue", "()Lorg/kwis/msp/lcdui/EventQueue;", runtime.wipiEventQueue},

		{wipi.DisplayClass, "getDefaultDisplay", "()Lorg/kwis/msp/lcdui/Display;", runtime.wipiDefaultDisplay},
		{wipi.DisplayClass, "getDisplay", "(" + text + ")Lorg/kwis/msp/lcdui/Display;", runtime.wipiDefaultDisplay},
		{wipi.DisplayClass, "pushCard", "(Lorg/kwis/msp/lcdui/Card;)V", runtime.wipiPushCard},
		{wipi.DisplayClass, "popCard", "()Lorg/kwis/msp/lcdui/Card;", runtime.wipiPopCard},
		{wipi.DisplayClass, "removeCard", "(Lorg/kwis/msp/lcdui/Card;)Z", runtime.wipiRemoveCard},
		{wipi.DisplayClass, "removeAllCards", "()V", runtime.wipiRemoveAllCards},
		{wipi.DisplayClass, "countCard", "()I", runtime.wipiCountCard},
		{wipi.DisplayClass, "getWidth", "()I", runtime.wipiDisplayWidth},
		{wipi.DisplayClass, "getHeight", "()I", runtime.wipiDisplayHeight},
		{wipi.DisplayClass, "flush", "()V", runtime.ignoreVoid},
		{wipi.DisplayClass, "getGameAction", "(I)I", wipiGameAction},
		{wipi.DisplayClass, "getKeyCode", "(I)I", wipiKeyCode},
		{wipi.DisplayClass, "getKeyName", "(I)" + text, wipiKeyName},

		{wipi.CardClass, "getDisplay", "()Lorg/kwis/msp/lcdui/Display;", runtime.wipiDefaultDisplay},
		{wipi.CardClass, "getHeight", "()I", runtime.wipiDisplayHeight},
		{wipi.CardClass, "move", "(II)V", runtime.wipiCardMove},
		{wipi.CardClass, "resize", "(II)V", runtime.wipiCardResize},

		{wipi.GraphicsClass, "getFont", "()Lorg/kwis/msp/lcdui/Font;", runtime.getGraphicsFont},
		{wipi.GraphicsClass, "reset", "()V", runtime.resetGraphics},
		{wipi.GraphicsClass, "clipRect", "(IIII)V", runtime.clipGraphicsRect},
		{wipi.GraphicsClass, "drawChar", "(CIII)V", runtime.drawGraphicsChar},
		{wipi.GraphicsClass, "setAlpha", "(I)V", runtime.setGraphicsAlpha},
		{wipi.GraphicsClass, "getAlpha", "()I", runtime.getGraphicsAlpha},
		{wipi.GraphicsClass, "getRGBPixels", "(IIII[III)V", runtime.getGraphicsRGBPixels},
		{wipi.GraphicsClass, "setRGBPixels", "(IIII[III)V", runtime.setGraphicsRGBPixels},
		{wipi.GraphicsClass, "drawImage", "(Lorg/kwis/msp/lcdui/Image;IIIIIII)V", runtime.wipiDrawImageRegion},

		{wipi.ImageClass, "createImage", "(II)Lorg/kwis/msp/lcdui/Image;", runtime.createMutableImage},
		{wipi.ImageClass, "createImage", "(" + text + ")Lorg/kwis/msp/lcdui/Image;", runtime.createImageFromResource},
		{wipi.ImageClass, "createImage", "([BII)Lorg/kwis/msp/lcdui/Image;", runtime.createImageFromBytes},
		{wipi.ImageClass, "getGraphics", "()Lorg/kwis/msp/lcdui/Graphics;", runtime.getImageGraphics},

		{wipi.FontClass, "getDefaultFont", "()Lorg/kwis/msp/lcdui/Font;", runtime.getDefaultFont},
		{wipi.FontClass, "getFont", "(III)Lorg/kwis/msp/lcdui/Font;", runtime.getFontByAttributes},

		{wipi.FileClass, "<init>", "(" + text + "I)V", runtime.initXFileName},
		{wipi.FileClass, "<init>", "(" + text + "II)V", runtime.initXFileName},
		{wipi.FileClass, "read", "([B)I", runtime.wipiFileReadWhole},
		{wipi.FileClass, "read", "([BII)I", runtime.xFileRead},
		{wipi.FileClass, "write", "([B)I", runtime.wipiFileWriteWhole},
		{wipi.FileClass, "write", "([BII)I", runtime.xFileWrite},
		{wipi.FileClass, "seek", "(I)V", runtime.wipiFileSeek},
		{wipi.FileClass, "tell", "()I", runtime.wipiFileTell},
		{wipi.FileClass, "sizeOf", "()I", runtime.wipiFileSizeOf},
		{wipi.FileClass, "close", "()V", runtime.xFileClose},
		{wipi.FileClass, "openInputStream", "()Ljava/io/InputStream;", runtime.wipiFileInputStream},
		{wipi.FileClass, "openDataInputStream", "()Ljava/io/DataInputStream;", runtime.wipiFileDataInputStream},
		{wipi.FileClass, "openOutputStream", "()Ljava/io/OutputStream;", runtime.wipiFileOutputStream},
		{wipi.FileClass, "openDataOutputStream", "()Ljava/io/DataOutputStream;", runtime.wipiFileDataOutputStream},

		{wipi.FileSystemClass, "exists", "(" + text + ")Z", runtime.xFileExists},
		{wipi.FileSystemClass, "isFile", "(" + text + ")Z", runtime.xFileExists},
		{wipi.FileSystemClass, "isDirectory", "(" + text + ")Z", answerFalse},
		{wipi.FileSystemClass, "remove", "(" + text + ")V", runtime.wipiFileRemove},
		{wipi.FileSystemClass, "mkdir", "(" + text + ")V", runtime.ignoreVoid},
		{wipi.FileSystemClass, "sizeOf", "(" + text + ")I", runtime.xFileSize},

		{wipi.ClipClass, "<init>", "(" + text + "[B)V", runtime.wipiClipInit},
		{wipi.ClipClass, "<init>", "(" + text + text + ")V", runtime.wipiClipInitResource},
		{wipi.ClipClass, "setListener", "(Lorg/kwis/msp/media/PlayListener;)V", runtime.wipiClipSetListener},
		{wipi.ClipClass, "setVolume", "(I)V", runtime.ignoreVoid},
		{wipi.ClipClass, "getVolume", "()I", runtime.wipiClipVolume},
		{wipi.ClipClass, "getType", "()" + text, runtime.wipiClipType},

		{wipi.PlayerClass, "play", "(Lorg/kwis/msp/media/Clip;Z)Z", runtime.wipiPlayerPlay},
		{wipi.PlayerClass, "stop", "(Lorg/kwis/msp/media/Clip;)Z", runtime.wipiPlayerStop},
		{wipi.PlayerClass, "pause", "(Lorg/kwis/msp/media/Clip;)Z", runtime.wipiPlayerStop},
		{wipi.PlayerClass, "resume", "(Lorg/kwis/msp/media/Clip;)Z", runtime.wipiPlayerPlayAgain},
	}
}

// answerFalse is the body of a question this platform has one answer to.
func answerFalse(*jvm.VM, []jvm.Value) (jvm.Value, error) { return jvm.IntValue(0), nil }

func (runtime *Runtime) wipiActiveJlet(_ *jvm.VM, _ []jvm.Value) (jvm.Value, error) {
	return jvm.ReferenceValue(runtime.MIDlet), nil
}

// wipiEventQueue answers the one queue a Jlet has. Nothing in the local corpus
// reads an event out of it — the titles here take their input through
// keyNotify — so it is an object with the class's constants on it rather than
// a second event path.
func (runtime *Runtime) wipiEventQueue(vm *jvm.VM, _ []jvm.Value) (jvm.Value, error) {
	runtime.displayMu.Lock()
	defer runtime.displayMu.Unlock()
	if runtime.eventQueue == nil {
		queue, err := vm.NewObject(wipi.EventQueueClass, "()V")
		if err != nil {
			return jvm.VoidValue(), err
		}
		runtime.eventQueue = queue
	}
	return jvm.ReferenceValue(runtime.eventQueue), nil
}

// wipiDefaultDisplay answers the one display this runtime has, under the WIPI
// class name. It is the same object the MIDP half hands out, because a session
// has one screen whichever vocabulary asks for it.
func (runtime *Runtime) wipiDefaultDisplay(_ *jvm.VM, _ []jvm.Value) (jvm.Value, error) {
	runtime.displayMu.Lock()
	defer runtime.displayMu.Unlock()
	if runtime.display == nil {
		runtime.display = &jvm.Object{ClassName: wipi.DisplayClass, Fields: make(map[string]jvm.Value)}
	}
	return jvm.ReferenceValue(runtime.display), nil
}

// wipiPushCard shows a card. The stack is this platform's: what is on screen
// is the card on top, and the ones below it are what a pop goes back to.
func (runtime *Runtime) wipiPushCard(vm *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	card, err := referenceArgument(arguments, 1)
	if err != nil {
		return jvm.VoidValue(), err
	}
	if card == nil {
		return jvm.VoidValue(), newGuestException("java/lang/NullPointerException", "Display.pushCard card is null")
	}
	runtime.displayMu.Lock()
	runtime.cardStack = append(runtime.cardStack, card)
	runtime.displayMu.Unlock()
	return runtime.showCard(vm, card)
}

func (runtime *Runtime) wipiPopCard(vm *jvm.VM, _ []jvm.Value) (jvm.Value, error) {
	runtime.displayMu.Lock()
	if len(runtime.cardStack) == 0 {
		runtime.displayMu.Unlock()
		return jvm.ReferenceValue(nil), nil
	}
	popped := runtime.cardStack[len(runtime.cardStack)-1]
	runtime.cardStack = runtime.cardStack[:len(runtime.cardStack)-1]
	var next *jvm.Object
	if len(runtime.cardStack) > 0 {
		next = runtime.cardStack[len(runtime.cardStack)-1]
	}
	runtime.displayMu.Unlock()
	if next != nil {
		if _, err := runtime.showCard(vm, next); err != nil {
			return jvm.VoidValue(), err
		}
	}
	return jvm.ReferenceValue(popped), nil
}

func (runtime *Runtime) wipiRemoveCard(vm *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	card, err := referenceArgument(arguments, 1)
	if err != nil {
		return jvm.VoidValue(), err
	}
	runtime.displayMu.Lock()
	removed := false
	kept := runtime.cardStack[:0]
	for _, entry := range runtime.cardStack {
		if entry == card {
			removed = true
			continue
		}
		kept = append(kept, entry)
	}
	runtime.cardStack = kept
	var next *jvm.Object
	if len(runtime.cardStack) > 0 {
		next = runtime.cardStack[len(runtime.cardStack)-1]
	}
	runtime.displayMu.Unlock()
	if removed && next != nil {
		if _, err := runtime.showCard(vm, next); err != nil {
			return jvm.VoidValue(), err
		}
	}
	if removed {
		return jvm.IntValue(1), nil
	}
	return jvm.IntValue(0), nil
}

func (runtime *Runtime) wipiRemoveAllCards(_ *jvm.VM, _ []jvm.Value) (jvm.Value, error) {
	runtime.displayMu.Lock()
	runtime.cardStack = nil
	runtime.displayMu.Unlock()
	return jvm.VoidValue(), nil
}

func (runtime *Runtime) wipiCountCard(_ *jvm.VM, _ []jvm.Value) (jvm.Value, error) {
	runtime.displayMu.RLock()
	defer runtime.displayMu.RUnlock()
	return jvm.IntValue(int32(len(runtime.cardStack))), nil
}

func (runtime *Runtime) wipiDisplayWidth(_ *jvm.VM, _ []jvm.Value) (jvm.Value, error) {
	return jvm.IntValue(int32(runtime.frameWidth)), nil
}

func (runtime *Runtime) wipiDisplayHeight(_ *jvm.VM, _ []jvm.Value) (jvm.Value, error) {
	return jvm.IntValue(int32(runtime.frameHeight)), nil
}

// wipiFlush is the WIPI name for putting the buffer on the panel. This
// platform presents at the end of a paint, so a flush between paints is the
// same present asked for early.
func (runtime *Runtime) wipiFlush(_ *jvm.VM, _ []jvm.Value) (jvm.Value, error) {
	return jvm.VoidValue(), nil
}

func (runtime *Runtime) wipiCardMove(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	return runtime.setCardFields(arguments, "x", "y")
}

func (runtime *Runtime) wipiCardResize(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	return runtime.setCardFields(arguments, "w", "h")
}

func (runtime *Runtime) setCardFields(arguments []jvm.Value, first, second string) (jvm.Value, error) {
	card, err := referenceArgument(arguments, 0)
	if err != nil {
		return jvm.VoidValue(), err
	}
	if card == nil {
		return jvm.VoidValue(), newGuestException("java/lang/NullPointerException", "Card is null")
	}
	one, err := intArgument(arguments, 1)
	if err != nil {
		return jvm.VoidValue(), err
	}
	two, err := intArgument(arguments, 2)
	if err != nil {
		return jvm.VoidValue(), err
	}
	if card.Fields == nil {
		card.Fields = make(map[string]jvm.Value)
	}
	card.Fields[first] = jvm.IntValue(one)
	card.Fields[second] = jvm.IntValue(two)
	return jvm.VoidValue(), nil
}

// The key codes a Jlet reads
//
// WIPI leaves the numbers to the handset — the specification says only that a
// control key arrives negative and that a title should ask `getGameAction`
// what it was — and this vendor answered with the game-key constants
// themselves: a Card is handed 1, 2, 5, 6 and 8 for the pad and fire, 90, 91
// and 92 for the soft keys, 99 for CLEAR, and the ASCII value for an ITU key.
//
// **Two local titles say so independently.** One keeps a per-handset key map
// as a resource, and its SKT table is keyed on exactly those numbers beside
// the ASCII digits; another switches on the key it is given over the same set
// and only calls `getGameAction` when the code it got was negative, which on
// this handset it never is. A MIDlet in the same container still reads this
// platform's own MIDP codes, so only a Jlet session translates.
const (
	wipiKeyUp    int32 = 1
	wipiKeyLeft  int32 = 2
	wipiKeyRight int32 = 5
	wipiKeyDown  int32 = 6
	wipiKeyFire  int32 = 8
	wipiKeyGameA int32 = 9
	wipiKeyGameB int32 = 10
	wipiKeyGameC int32 = 11
	wipiKeyGameD int32 = 12
	wipiKeySoft1 int32 = 90
	wipiKeySoft2 int32 = 91
	wipiKeyClear int32 = 99
	// The send key is one of the few the specification does fix, and it fixes
	// it negative.
	wipiKeySend int32 = -10
)

// wipiKeyOfDevice is that translation, from the code a Host sends to the code
// a Card is handed. An ITU key is its ASCII value on both sides and passes
// through.
func wipiKeyOfDevice(code int32) int32 {
	switch code {
	case KeyCodeUp:
		return wipiKeyUp
	case KeyCodeLeft:
		return wipiKeyLeft
	case KeyCodeRight:
		return wipiKeyRight
	case KeyCodeDown:
		return wipiKeyDown
	case KeyCodeFire:
		return wipiKeyFire
	case KeyCodeSoft1:
		return wipiKeySoft1
	case KeyCodeSoft2:
		return wipiKeySoft2
	case KeyCodeClear:
		return wipiKeyClear
	case KeyCodeCall:
		return wipiKeySend
	}
	return code
}

// wipiGameAction, wipiKeyCode and wipiKeyName are the statics WIPI puts on
// Display where MIDP puts them on Canvas. They speak the vocabulary above: the
// pad and fire codes *are* the game actions, so naming one of them answers
// itself, and the ITU keys a handset lets stand in for the pad answer the key
// they stand in for.
func wipiGameAction(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	code, err := intArgument(arguments, 0)
	if err != nil {
		return jvm.VoidValue(), err
	}
	switch code {
	case wipiKeyUp, '2':
		return jvm.IntValue(wipiKeyUp), nil
	case wipiKeyLeft, '4':
		return jvm.IntValue(wipiKeyLeft), nil
	case wipiKeyRight, '6':
		return jvm.IntValue(wipiKeyRight), nil
	case wipiKeyDown, '8':
		return jvm.IntValue(wipiKeyDown), nil
	case wipiKeyFire, '5':
		return jvm.IntValue(wipiKeyFire), nil
	case '1':
		return jvm.IntValue(wipiKeyGameA), nil
	case '3':
		return jvm.IntValue(wipiKeyGameB), nil
	case '7':
		return jvm.IntValue(wipiKeyGameC), nil
	case '9':
		return jvm.IntValue(wipiKeyGameD), nil
	}
	return jvm.IntValue(0), nil
}

func wipiKeyCode(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	action, err := intArgument(arguments, 0)
	if err != nil {
		return jvm.VoidValue(), err
	}
	codes := map[int32]int32{
		wipiKeyUp: wipiKeyUp, wipiKeyLeft: wipiKeyLeft,
		wipiKeyRight: wipiKeyRight, wipiKeyDown: wipiKeyDown,
		wipiKeyFire: wipiKeyFire, wipiKeyGameA: '1', wipiKeyGameB: '3',
		wipiKeyGameC: '7', wipiKeyGameD: '9',
	}
	code, ok := codes[action]
	if !ok {
		return jvm.VoidValue(), newGuestException("java/lang/IllegalArgumentException", "invalid game action")
	}
	return jvm.IntValue(code), nil
}

func wipiKeyName(vm *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	code, err := intArgument(arguments, 0)
	if err != nil {
		return jvm.VoidValue(), err
	}
	names := map[int32]string{
		wipiKeyUp: "UP", wipiKeyLeft: "LEFT", wipiKeyRight: "RIGHT",
		wipiKeyDown: "DOWN", wipiKeyFire: "FIRE",
		wipiKeySoft1: "SOFT1", wipiKeySoft2: "SOFT2", wipiKeyClear: "CLEAR",
		'*': "*", '#': "#",
		'0': "0", '1': "1", '2': "2", '3': "3", '4': "4",
		'5': "5", '6': "6", '7': "7", '8': "8", '9': "9",
	}
	name, ok := names[code]
	if !ok {
		return jvm.ReferenceValue(nil), nil
	}
	return jvm.ReferenceValue(vm.NewString(name)), nil
}

// setGraphicsAlpha keeps the blend factor a WIPI title sets and reports it
// back. **It is kept and not yet applied**: honouring it means blending every
// primitive this platform draws rather than only the images that carry their
// own alpha, and no local title has been shown to need it — all three of the
// WIPI titles here set it once, to the opaque value, beside a colour. A title
// that sets it to something else and expects to see through what it draws is
// the evidence to act on.
func (runtime *Runtime) setGraphicsAlpha(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	context, err := graphicsReceiver(arguments)
	if err != nil {
		return jvm.VoidValue(), err
	}
	alpha, err := intArgument(arguments, 1)
	if err != nil {
		return jvm.VoidValue(), err
	}
	if alpha < 0 {
		alpha = 0
	}
	if alpha > 255 {
		alpha = 255
	}
	context.alpha = uint8(alpha)
	return jvm.VoidValue(), nil
}

func (runtime *Runtime) getGraphicsAlpha(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	context, err := graphicsReceiver(arguments)
	if err != nil {
		return jvm.VoidValue(), err
	}
	return jvm.IntValue(int32(context.alpha)), nil
}

// getGraphicsRGBPixels and setGraphicsRGBPixels are WIPI's read and write of a
// rectangle of the surface. They are the same operation MIDP's drawRGB and
// Image.getRGB do, against the surface the Graphics draws on rather than an
// image, so they walk the context's pixels directly.
func (runtime *Runtime) getGraphicsRGBPixels(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	context, x, y, width, height, values, offset, scanLength, err := graphicsRGBArguments(arguments)
	if err != nil {
		return jvm.VoidValue(), err
	}
	pixels := make([]jvm.Value, 0, int(width)*int(height))
	for row := int32(0); row < height; row++ {
		for column := int32(0); column < width; column++ {
			index, ok := contextPixelIndexUnclipped(context, x+column, y+row)
			if !ok {
				pixels = append(pixels, jvm.IntValue(0))
				continue
			}
			pixels = append(pixels, jvm.IntValue(int32(uint32(context.pixels[index+3])<<24|
				uint32(context.pixels[index])<<16|uint32(context.pixels[index+1])<<8|
				uint32(context.pixels[index+2]))))
		}
	}
	for row := int32(0); row < height; row++ {
		start := int(offset) + int(row)*int(scanLength)
		if start < 0 || start+int(width) > len(values) {
			return jvm.VoidValue(), newGuestException("java/lang/ArrayIndexOutOfBoundsException", "getRGBPixels row is outside the array")
		}
		if err := jvm.SetArrayRange(arrayObjectOf(arguments, 4), start, pixels[int(row)*int(width):int(row+1)*int(width)]); err != nil {
			return jvm.VoidValue(), err
		}
	}
	return jvm.VoidValue(), nil
}

func (runtime *Runtime) setGraphicsRGBPixels(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	context, x, y, width, height, values, offset, scanLength, err := graphicsRGBArguments(arguments)
	if err != nil {
		return jvm.VoidValue(), err
	}
	context.withDestinationWrite(func() {
		for row := int32(0); row < height; row++ {
			for column := int32(0); column < width; column++ {
				source := int(offset) + int(row)*int(scanLength) + int(column)
				if source < 0 || source >= len(values) {
					continue
				}
				index, ok := contextPixelIndex(context, x+column, y+row)
				if !ok {
					continue
				}
				argb, valueErr := values[source].Int32()
				if valueErr != nil {
					continue
				}
				context.pixels[index] = byte(uint32(argb) >> 16)
				context.pixels[index+1] = byte(uint32(argb) >> 8)
				context.pixels[index+2] = byte(uint32(argb))
				context.pixels[index+3] = 0xff
			}
		}
	})
	return jvm.VoidValue(), nil
}

func arrayObjectOf(arguments []jvm.Value, index int) *jvm.Object {
	object, err := referenceArgument(arguments, index)
	if err != nil {
		return nil
	}
	return object
}

func graphicsRGBArguments(arguments []jvm.Value) (
	context *graphicsContext, x, y, width, height int32,
	values []jvm.Value, offset, scanLength int32, err error,
) {
	context, err = graphicsReceiver(arguments)
	if err != nil {
		return nil, 0, 0, 0, 0, nil, 0, 0, err
	}
	x, y, width, height, err = rectArguments(arguments, 1)
	if err != nil {
		return nil, 0, 0, 0, 0, nil, 0, 0, err
	}
	if width < 0 || height < 0 {
		return nil, 0, 0, 0, 0, nil, 0, 0, newGuestException("java/lang/IllegalArgumentException", "RGB pixel rectangle is negative")
	}
	_, values, err = primitiveArrayArgument(arguments, 5, jvm.TypeInt)
	if err != nil {
		return nil, 0, 0, 0, 0, nil, 0, 0, err
	}
	offset, err = intArgument(arguments, 6)
	if err != nil {
		return nil, 0, 0, 0, 0, nil, 0, 0, err
	}
	scanLength, err = intArgument(arguments, 7)
	if err != nil {
		return nil, 0, 0, 0, 0, nil, 0, 0, err
	}
	if scanLength == 0 {
		scanLength = width
	}
	return context, x, y, width, height, values, offset, scanLength, nil
}

// wipiDrawImageRegion is the WIPI blit with a source rectangle. Its argument
// order is the image, the destination point, and then the rectangle inside the
// image, which is MIDP's drawRegion with the two halves swapped and no
// transform.
func (runtime *Runtime) wipiDrawImageRegion(vm *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	if len(arguments) < 9 {
		return jvm.VoidValue(), fmt.Errorf("WIPI drawImage takes an image, a destination and a source rectangle")
	}
	destinationX, destinationY := arguments[2], arguments[3]
	sourceX, sourceY := arguments[4], arguments[5]
	width, height := arguments[6], arguments[7]
	region := []jvm.Value{
		arguments[0], arguments[1],
		sourceX, sourceY, width, height,
		jvm.IntValue(0), destinationX, destinationY, jvm.IntValue(0),
	}
	return runtime.drawGraphicsRegion(vm, region)
}

// The WIPI file surface over the same handle the SKVM one uses. What differs
// is the shape of three calls: a read and a write of a whole array, and a seek
// that takes an absolute position rather than an offset and a whence.
func (runtime *Runtime) wipiFileReadWhole(vm *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	length, err := wipiArrayLength(arguments, 1)
	if err != nil {
		return jvm.VoidValue(), err
	}
	return runtime.xFileRead(vm, append(arguments[:2:2], jvm.IntValue(0), jvm.IntValue(length)))
}

func (runtime *Runtime) wipiFileWriteWhole(vm *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	length, err := wipiArrayLength(arguments, 1)
	if err != nil {
		return jvm.VoidValue(), err
	}
	return runtime.xFileWrite(vm, append(arguments[:2:2], jvm.IntValue(0), jvm.IntValue(length)))
}

func wipiArrayLength(arguments []jvm.Value, index int) (int32, error) {
	_, values, err := primitiveArrayArgument(arguments, index, jvm.TypeByte)
	if err != nil {
		return 0, err
	}
	return int32(len(values)), nil
}

func (runtime *Runtime) wipiFileSeek(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	file, err := xFileArgument(arguments)
	if err != nil {
		return jvm.VoidValue(), err
	}
	position, err := intArgument(arguments, 1)
	if err != nil {
		return jvm.VoidValue(), err
	}
	file.mu.Lock()
	defer file.mu.Unlock()
	if position < 0 {
		position = 0
	}
	if int(position) > len(file.data) {
		position = int32(len(file.data))
	}
	file.cursor = int(position)
	return jvm.VoidValue(), nil
}

func (runtime *Runtime) wipiFileTell(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	file, err := xFileArgument(arguments)
	if err != nil {
		return jvm.VoidValue(), err
	}
	file.mu.Lock()
	defer file.mu.Unlock()
	return jvm.IntValue(int32(file.cursor)), nil
}

func (runtime *Runtime) wipiFileSizeOf(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	file, err := xFileArgument(arguments)
	if err != nil {
		return jvm.VoidValue(), err
	}
	file.mu.Lock()
	defer file.mu.Unlock()
	return jvm.IntValue(int32(len(file.data))), nil
}

func (runtime *Runtime) wipiFileRemove(vm *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	if _, err := runtime.xFileUnlink(vm, arguments); err != nil {
		return jvm.VoidValue(), err
	}
	return jvm.VoidValue(), nil
}

func (runtime *Runtime) wipiFileInputStream(vm *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	return runtime.wipiFileStream(vm, arguments, jvm.ByteArrayInputStreamClass, "([B)V")
}

func (runtime *Runtime) wipiFileDataInputStream(vm *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	inner, err := runtime.wipiFileStream(vm, arguments, jvm.ByteArrayInputStreamClass, "([B)V")
	if err != nil {
		return jvm.VoidValue(), err
	}
	stream, err := vm.NewObject("java/io/DataInputStream", "(Ljava/io/InputStream;)V", inner)
	if err != nil {
		return jvm.VoidValue(), err
	}
	return jvm.ReferenceValue(stream), nil
}

// wipiFileStream hands the file's bytes to a stream the guest reads. The file
// stays open behind it, which is what a title expects when it reads a save
// through a stream and then seeks the handle itself.
func (runtime *Runtime) wipiFileStream(vm *jvm.VM, arguments []jvm.Value, className, descriptor string) (jvm.Value, error) {
	file, err := xFileArgument(arguments)
	if err != nil {
		return jvm.VoidValue(), err
	}
	file.mu.Lock()
	data := append([]byte(nil), file.data...)
	file.mu.Unlock()
	stream, err := vm.NewObject(className, descriptor, jvm.ReferenceValue(jvm.NewByteArray(data)))
	if err != nil {
		return jvm.VoidValue(), err
	}
	return jvm.ReferenceValue(stream), nil
}

// The writing streams are the file itself: a WIPI title opens one, writes its
// save through it and closes it, and what has to reach the save store is the
// handle's bytes rather than a buffer nobody reads back.
func (runtime *Runtime) wipiFileOutputStream(vm *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	file, err := xFileArgument(arguments)
	if err != nil {
		return jvm.VoidValue(), err
	}
	stream, err := vm.NewObject(jvm.ByteArrayOutputStreamClass, "()V")
	if err != nil {
		return jvm.VoidValue(), err
	}
	runtime.wipiStreamMu.Lock()
	if runtime.wipiFileStreams == nil {
		runtime.wipiFileStreams = make(map[*jvm.Object]*xFileData)
	}
	runtime.wipiFileStreams[stream] = file
	runtime.wipiStreamMu.Unlock()
	return jvm.ReferenceValue(stream), nil
}

func (runtime *Runtime) wipiFileDataOutputStream(vm *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	inner, err := runtime.wipiFileOutputStream(vm, arguments)
	if err != nil {
		return jvm.VoidValue(), err
	}
	stream, err := vm.NewObject("java/io/DataOutputStream", "(Ljava/io/OutputStream;)V", inner)
	if err != nil {
		return jvm.VoidValue(), err
	}
	return jvm.ReferenceValue(stream), nil
}

// wipiClipData is a WIPI sound: the bytes, the format name the title gave and
// the MIDP Player this platform plays them with. A Clip is created once and
// started and stopped many times, which is why the Player is built here and
// kept rather than made for each play.
type wipiClipData struct {
	contentType string
	player      *jvm.Object
	listener    *jvm.Object
	object      *jvm.Object
}

func (runtime *Runtime) wipiClipInit(vm *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	object, err := referenceArgument(arguments, 0)
	if err != nil {
		return jvm.VoidValue(), err
	}
	contentType, err := stringArgument(arguments, 1)
	if err != nil {
		return jvm.VoidValue(), err
	}
	array, err := referenceArgument(arguments, 2)
	if err != nil {
		return jvm.VoidValue(), err
	}
	var data []byte
	if array != nil {
		if data, err = jvm.ByteArraySnapshot(array); err != nil {
			return jvm.VoidValue(), err
		}
	}
	return jvm.VoidValue(), runtime.buildWIPIClip(vm, object, contentType, data)
}

// The two-string form names a resource in the archive rather than carrying its
// bytes. A sound the archive does not carry is an empty clip rather than a
// failure, which is the decision `docs/audio.md` records for the other
// platforms.
func (runtime *Runtime) wipiClipInitResource(vm *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	object, err := referenceArgument(arguments, 0)
	if err != nil {
		return jvm.VoidValue(), err
	}
	contentType, err := stringArgument(arguments, 1)
	if err != nil {
		return jvm.VoidValue(), err
	}
	name, err := stringArgument(arguments, 2)
	if err != nil {
		return jvm.VoidValue(), err
	}
	data, _ := runtime.Archive.Resource(strings.TrimPrefix(name, "/"))
	return jvm.VoidValue(), runtime.buildWIPIClip(vm, object, contentType, data)
}

func (runtime *Runtime) buildWIPIClip(vm *jvm.VM, object *jvm.Object, contentType string, data []byte) error {
	clip := &wipiClipData{contentType: contentType, object: object}
	object.Native = clip
	if len(data) == 0 {
		return nil
	}
	player, err := runtime.newPlayer(vm, data, wipiPlayerContentType(contentType))
	if err != nil {
		// A format this project does not decode is a silent clip, not a
		// refused one: the title still plays.
		if runtime.logger != nil {
			runtime.logger.Debug("WIPI clip has no player", "type", contentType, "error", err)
		}
		return nil
	}
	reference, err := player.Reference()
	if err != nil {
		return err
	}
	clip.player = reference
	return nil
}

// wipiPlayerContentType translates the short names WIPI titles use into the
// MIME types the media path keys on.
func wipiPlayerContentType(name string) string {
	switch strings.ToLower(strings.TrimPrefix(name, ".")) {
	case "mid", "midi", "audio/midi":
		return "audio/midi"
	case "mmf", "smaf", "application/vnd.smaf":
		return "application/vnd.smaf"
	case "wav", "wave", "audio/x-wav":
		return "audio/x-wav"
	}
	return name
}

func wipiClipArgument(arguments []jvm.Value, index int) (*wipiClipData, error) {
	object, err := referenceArgument(arguments, index)
	if err != nil {
		return nil, err
	}
	if object == nil {
		return nil, newGuestException("java/lang/NullPointerException", "Clip is null")
	}
	clip, ok := object.Native.(*wipiClipData)
	if !ok || clip == nil {
		return nil, fmt.Errorf("argument %d is not a WIPI Clip", index)
	}
	return clip, nil
}

func (runtime *Runtime) wipiClipSetListener(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	clip, err := wipiClipArgument(arguments, 0)
	if err != nil {
		return jvm.VoidValue(), err
	}
	listener, err := referenceArgument(arguments, 1)
	if err != nil {
		return jvm.VoidValue(), err
	}
	clip.listener = listener
	return jvm.VoidValue(), nil
}

func (runtime *Runtime) wipiClipVolume(_ *jvm.VM, _ []jvm.Value) (jvm.Value, error) {
	return jvm.IntValue(100), nil
}

func (runtime *Runtime) wipiClipType(vm *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	clip, err := wipiClipArgument(arguments, 0)
	if err != nil {
		return jvm.VoidValue(), err
	}
	return jvm.ReferenceValue(vm.NewString(clip.contentType)), nil
}

func (runtime *Runtime) wipiPlayerPlay(vm *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	clip, err := wipiClipArgument(arguments, 0)
	if err != nil {
		return jvm.VoidValue(), err
	}
	if clip.player == nil {
		return jvm.IntValue(0), nil
	}
	loop, err := intArgument(arguments, 1)
	if err != nil {
		return jvm.VoidValue(), err
	}
	count := int32(1)
	if loop != 0 {
		count = -1
	}
	if _, err := runtime.setPlayerLoopCount(vm, []jvm.Value{jvm.ReferenceValue(clip.player), jvm.IntValue(count)}); err != nil {
		return jvm.VoidValue(), err
	}
	if _, err := runtime.playerStart(vm, []jvm.Value{jvm.ReferenceValue(clip.player)}); err != nil {
		return jvm.VoidValue(), err
	}
	return jvm.IntValue(1), nil
}

func (runtime *Runtime) wipiPlayerPlayAgain(vm *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	return runtime.wipiPlayerPlay(vm, append(arguments[:1:1], jvm.IntValue(0)))
}

func (runtime *Runtime) wipiPlayerStop(vm *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	clip, err := wipiClipArgument(arguments, 0)
	if err != nil {
		return jvm.VoidValue(), err
	}
	if clip.player == nil {
		return jvm.IntValue(0), nil
	}
	if _, err := runtime.playerStop(vm, []jvm.Value{jvm.ReferenceValue(clip.player)}); err != nil {
		return jvm.VoidValue(), err
	}
	return jvm.IntValue(1), nil
}

// showCard puts a card on the screen, and it does so **before the call
// returns**. MIDP's setCurrent is a request the next Host pass applies, and
// WIPI's pushCard is not: one title pushes its card and then spins on
// `isShown` until the card is up, from the same thread that would have to
// reach the Host pass for it to become true. Deferring there is a hang rather
// than a frame later.
//
// What is still deferred is the paint, which applyCurrentDisplayable queues
// the same way it does for a MIDlet.
func (runtime *Runtime) showCard(_ *jvm.VM, card *jvm.Object) (jvm.Value, error) {
	runtime.displayMu.Lock()
	runtime.pendingDisplayable = card
	runtime.displayMu.Unlock()
	return jvm.VoidValue(), runtime.applyCurrentDisplayable()
}

func (runtime *Runtime) registerWIPINatives() error {
	for _, registration := range runtime.wipiRegistrations() {
		if err := runtime.registerNative(registration.class, registration.name, registration.descriptor, registration.method); err != nil {
			return fmt.Errorf("register %s.%s%s: %w", registration.class, registration.name, registration.descriptor, err)
		}
	}
	return nil
}
