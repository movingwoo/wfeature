// Package wipi is the WIPI Java class library — `org.kwis.msp` and
// `org.kwis.msf` — as this runtime publishes it to a program written against
// the standard rather than against MIDP.
//
// **It is a layer over MIDP rather than a second runtime.** This vendor's
// container carries both kinds of program: most of the local archives hold a
// MIDlet, and three hold a Jlet, which is the same JAR shape with a different
// application class inside. The display, the pixels, the fonts, the images and
// the save boundary a Jlet needs are the ones already built for the MIDlet
// half, so each class here **extends its MIDP counterpart** — `Jlet` a
// `MIDlet`, `Card` a `Canvas`, and `Display`, `Graphics`, `Image` and `Font`
// the classes of those names — and adds only what the WIPI standard has that
// MIDP does not.
//
// The specification says each of them extends `Object`, and that difference is
// not observable here: nothing in the local corpus casts or tests one of these
// against a MIDP type, and the alternative is a second display path with its
// own paint scheduling, its own key delivery and its own frame. If a title ever
// does test one, this paragraph is the decision to revisit.
//
// What a Host has to know is that the *objects* a Jlet session is handed carry
// the WIPI class name — a `Card`'s paint is given an `org.kwis.msp.lcdui.
// Graphics`, not the MIDP one — because `invokevirtual` resolves on the class
// of the receiver. `internal/platform/skt` stamps them; see `wipi.go` there.
package wipi

import (
	"fmt"

	"github.com/movingwoo/wfeature/internal/jvm"
)

const (
	JletClass       = "org/kwis/msp/lcdui/Jlet"
	CardClass       = "org/kwis/msp/lcdui/Card"
	DisplayClass    = "org/kwis/msp/lcdui/Display"
	GraphicsClass   = "org/kwis/msp/lcdui/Graphics"
	ImageClass      = "org/kwis/msp/lcdui/Image"
	FontClass       = "org/kwis/msp/lcdui/Font"
	EventQueueClass = "org/kwis/msp/lcdui/EventQueue"

	BackLightClass  = "org/kwis/msp/handset/BackLight"
	FileClass       = "org/kwis/msp/io/File"
	FileSystemClass = "org/kwis/msp/io/FileSystem"

	ClipClass         = "org/kwis/msp/media/Clip"
	PlayerClass       = "org/kwis/msp/media/Player"
	PlayListenerClass = "org/kwis/msp/media/PlayListener"
	VibratorClass     = "org/kwis/msp/media/Vibrator"

	NetworkClass = "org/kwis/msf/io/Network"
	SocketClass  = "org/kwis/msf/io/Socket"
	URLClass     = "org/kwis/msf/io/URL"
)

// Define installs the library on a machine that already has MIDP, because
// every class here names a MIDP class as its superclass.
func Define(machine *jvm.VM) error {
	for _, definition := range definitions() {
		if err := machine.DefineClass(definition); err != nil {
			return fmt.Errorf("WIPI Java library: %w", err)
		}
	}
	return nil
}

// IsJlet answers whether a class is a WIPI application rather than a MIDlet.
// A Host asks it of an archive's main class, because the two are packaged
// identically and only the application class tells them apart.
func IsJlet(machine *jvm.VM, className string) (bool, error) {
	if machine == nil || className == "" {
		return false, nil
	}
	return machine.IsSubclassOf(className, JletClass)
}

const (
	publicMethod    = jvm.AccessPublic
	publicStatic    = jvm.AccessPublic | jvm.AccessStatic
	publicNative    = jvm.AccessPublic | jvm.AccessNative
	publicStaticNat = jvm.AccessPublic | jvm.AccessStatic | jvm.AccessNative
	protectedMethod = jvm.AccessProtected
	constantField   = jvm.AccessPublic | jvm.AccessStatic | jvm.AccessFinal
)

func receiver(arguments []jvm.Value) (*jvm.Object, error) {
	if len(arguments) == 0 {
		return nil, fmt.Errorf("method has no receiver")
	}
	object, err := arguments[0].Reference()
	if err != nil {
		return nil, err
	}
	if object == nil {
		return nil, jvm.Throw("java/lang/NullPointerException", "receiver is null")
	}
	return object, nil
}

// forwardSpecial calls a named MIDP class's method rather than dispatching, so
// a WIPI override can still reach the implementation it is layered over.
//
// **It is always the special form, never a virtual dispatch.** A WIPI method
// that shares its name and descriptor with the MIDP one it forwards to is an
// override of it: dispatching would resolve to the WIPI method again and the
// call would recurse until the host stack ran out, which is exactly what three
// of these did before they were written this way.
func forwardSpecial(className, name, descriptor string) jvm.ContextMethod {
	return func(call *jvm.Invocation, arguments []jvm.Value) (jvm.Value, error) {
		object, err := receiver(arguments)
		if err != nil {
			return jvm.VoidValue(), err
		}
		return call.InvokeSpecial(object, className, name, descriptor, arguments[1:]...)
	}
}

func doNothing(*jvm.Invocation, []jvm.Value) (jvm.Value, error) {
	return jvm.VoidValue(), nil
}

func answerInt(value int32) jvm.ContextMethod {
	return func(*jvm.Invocation, []jvm.Value) (jvm.Value, error) {
		return jvm.IntValue(value), nil
	}
}

func answerBool(value bool) jvm.ContextMethod {
	result := int32(0)
	if value {
		result = 1
	}
	return func(*jvm.Invocation, []jvm.Value) (jvm.Value, error) {
		return jvm.IntValue(result), nil
	}
}

func superInit(className, descriptor string) jvm.ContextMethod {
	return func(call *jvm.Invocation, arguments []jvm.Value) (jvm.Value, error) {
		object, err := receiver(arguments)
		if err != nil {
			return jvm.VoidValue(), err
		}
		_, err = call.InvokeSpecial(object, className, "<init>", descriptor)
		return jvm.VoidValue(), err
	}
}
