package midp

import (
	"fmt"

	"github.com/movingwoo/wfeature/internal/jvm"
)

const (
	MIDletClass                     = "javax/microedition/midlet/MIDlet"
	MIDletStateChangeExceptionClass = "javax/microedition/midlet/MIDletStateChangeException"
	CanvasClass                     = "javax/microedition/lcdui/Canvas"
	DisplayClass                    = "javax/microedition/lcdui/Display"
	DisplayableClass                = "javax/microedition/lcdui/Displayable"
	FontClass                       = "javax/microedition/lcdui/Font"
	GraphicsClass                   = "javax/microedition/lcdui/Graphics"
	ImageClass                      = "javax/microedition/lcdui/Image"

	RecordStoreClass                  = "javax/microedition/rms/RecordStore"
	RecordSetClass                    = "javax/microedition/rms/RecordSet"
	RecordEnumerationClass            = "javax/microedition/rms/RecordEnumeration"
	RecordFilterClass                 = "javax/microedition/rms/RecordFilter"
	RecordComparatorClass             = "javax/microedition/rms/RecordComparator"
	RecordListenerClass               = "javax/microedition/rms/RecordListener"
	RecordStoreExceptionClass         = "javax/microedition/rms/RecordStoreException"
	RecordStoreNotOpenExceptionClass  = "javax/microedition/rms/RecordStoreNotOpenException"
	RecordStoreNotFoundExceptionClass = "javax/microedition/rms/RecordStoreNotFoundException"
	RecordStoreFullExceptionClass     = "javax/microedition/rms/RecordStoreFullException"
	InvalidRecordIDExceptionClass     = "javax/microedition/rms/InvalidRecordIDException"

	AlertClass               = "javax/microedition/lcdui/Alert"
	AlertTypeClass           = "javax/microedition/lcdui/AlertType"
	ChoiceClass              = "javax/microedition/lcdui/Choice"
	ChoiceGroupClass         = "javax/microedition/lcdui/ChoiceGroup"
	CommandClass             = "javax/microedition/lcdui/Command"
	CommandListenerClass     = "javax/microedition/lcdui/CommandListener"
	FormClass                = "javax/microedition/lcdui/Form"
	ImageItemClass           = "javax/microedition/lcdui/ImageItem"
	ItemClass                = "javax/microedition/lcdui/Item"
	ItemCommandListenerClass = "javax/microedition/lcdui/ItemCommandListener"
	ItemStateListenerClass   = "javax/microedition/lcdui/ItemStateListener"
	ListClass                = "javax/microedition/lcdui/List"
	ScreenClass              = "javax/microedition/lcdui/Screen"
	StringItemClass          = "javax/microedition/lcdui/StringItem"
	TextBoxClass             = "javax/microedition/lcdui/TextBox"
	TextFieldClass           = "javax/microedition/lcdui/TextField"
	TickerClass              = "javax/microedition/lcdui/Ticker"
	GameCanvasClass          = "javax/microedition/lcdui/game/GameCanvas"

	ManagerClass        = "javax/microedition/media/Manager"
	PlayerClass         = "javax/microedition/media/Player"
	PlayerListenerClass = "javax/microedition/media/PlayerListener"
	MediaExceptionClass = "javax/microedition/media/MediaException"

	// The Generic Connection Framework. Nothing behind it connects: the
	// factory refuses every name and the interfaces exist so a game that
	// names one resolves it. See docs/network.md.
	ConnectorClass                   = "javax/microedition/io/Connector"
	ConnectionClass                  = "javax/microedition/io/Connection"
	InputConnectionClass             = "javax/microedition/io/InputConnection"
	OutputConnectionClass            = "javax/microedition/io/OutputConnection"
	StreamConnectionClass            = "javax/microedition/io/StreamConnection"
	ContentConnectionClass           = "javax/microedition/io/ContentConnection"
	StreamConnectionNotifierClass    = "javax/microedition/io/StreamConnectionNotifier"
	HTTPConnectionClass              = "javax/microedition/io/HttpConnection"
	SocketConnectionClass            = "javax/microedition/io/SocketConnection"
	ConnectionNotFoundExceptionClass = "javax/microedition/io/ConnectionNotFoundException"
)

// Command types from javax.microedition.lcdui.Command.
const (
	CommandScreen int32 = 1
	CommandItem   int32 = 8
)

// TextField constraint bits the runtime acts on. The mask selects the base
// constraint; the modifier bits sit above it.
const (
	TextFieldConstraintMask     int32 = 0xFFFF
	TextFieldPasswordConstraint int32 = 5
	TextFieldPassword           int32 = 0x10000
)

// ToneDeviceLocator is Manager.TONE_DEVICE_LOCATOR.
const ToneDeviceLocator = "device://tone"

// The PlayerListener event names this runtime reports.
const (
	PlayerEventStarted = "started"
	PlayerEventStopped = "stopped"
	PlayerEventClosed  = "closed"
)

// Define installs the runtime-owned MIDP surface on a VM. The classes are
// declared in Go rather than shipped as class files, so this has to run before
// a game's own classes are linked against them — and it always runs, because
// a title that never opens a record store simply never initializes those
// classes.
//
// A definition publishes the signature; the body behind a native one is
// registered by the platform, which is where the Host — a screen, a save
// directory, an audio sink — actually is.
func Define(machine *jvm.VM) error {
	for _, definition := range definitions() {
		if err := machine.DefineClass(definition); err != nil {
			return fmt.Errorf("MIDP library: %w", err)
		}
	}
	return nil
}

// exceptionInit is the body of an exception constructor that only hands its
// arguments to its superclass, which is where the message a catch block reads
// is kept.
func exceptionInit(superName, descriptor string) jvm.ContextMethod {
	return func(call *jvm.Invocation, arguments []jvm.Value) (jvm.Value, error) {
		receiver, err := arguments[0].Reference()
		if err != nil {
			return jvm.VoidValue(), err
		}
		_, err = call.InvokeSpecial(receiver, superName, "<init>", descriptor, arguments[1:]...)
		return jvm.VoidValue(), err
	}
}

// emptyInit is the body of a constructor whose class keeps its state on the
// runtime side, so there is nothing to set up when one is made.
func emptyInit(_ *jvm.Invocation, _ []jvm.Value) (jvm.Value, error) {
	return jvm.VoidValue(), nil
}

// ignore is the body of a method a game may call and this runtime has nothing
// to do for. It is not a stub for something missing: the MIDP contract allows
// the runtime to do nothing here.
func ignore(_ *jvm.Invocation, _ []jvm.Value) (jvm.Value, error) {
	return jvm.VoidValue(), nil
}

// answerBool is the body of a capability question with a fixed answer.
func answerBool(answer bool) jvm.ContextMethod {
	return func(_ *jvm.Invocation, _ []jvm.Value) (jvm.Value, error) {
		if answer {
			return jvm.IntValue(1), nil
		}
		return jvm.IntValue(0), nil
	}
}

// answerInt is the body of a method that always answers the same number.
func answerInt(answer int32) jvm.ContextMethod {
	return func(_ *jvm.Invocation, _ []jvm.Value) (jvm.Value, error) {
		return jvm.IntValue(answer), nil
	}
}

// receiver reads the object a method was called on.
func receiver(arguments []jvm.Value) (*jvm.Object, error) {
	if len(arguments) == 0 {
		return nil, fmt.Errorf("instance method called without a receiver")
	}
	object, err := arguments[0].Reference()
	if err != nil {
		return nil, err
	}
	if object == nil {
		return nil, jvm.Throw("java/lang/NullPointerException", "null receiver")
	}
	return object, nil
}

// Definitions is the MIDP surface as data, for the tools that have to see it
// rather than run it. See jvm.CoreLibraryDefinitions.
func Definitions() []jvm.ClassDefinition { return definitions() }
