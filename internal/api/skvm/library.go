package skvm

import (
	"fmt"

	"github.com/movingwoo/wfeature/internal/jvm"
)

// The SKVM class surface. Names are the internal form the loader uses.
const (
	AudioClipClass                  = "com/skt/m/AudioClip"
	AudioSystemClass                = "com/skt/m/AudioSystem"
	BackLightClass                  = "com/skt/m/BackLight"
	CallClass                       = "com/skt/m/Call"
	DeviceClass                     = "com/skt/m/Device"
	Graphics2DClass                 = "com/skt/m/Graphics2D"
	MathFPClass                     = "com/skt/m/MathFP"
	PhoneBookClass                  = "com/skt/m/PhoneBook"
	ProgressBarClass                = "com/skt/m/ProgressBar"
	ResourceAllocExceptionClass     = "com/skt/m/ResourceAllocException"
	SISImageClass                   = "com/skt/m/SISImage"
	SMSClass                        = "com/skt/m/SMS"
	SMSListenerClass                = "com/skt/m/SMSListener"
	SMSMessageClass                 = "com/skt/m/SMSMessage"
	UnsupportedFormatExceptionClass = "com/skt/m/UnsupportedFormatException"
	UserStopExceptionClass          = "com/skt/m/UserStopException"
	VibrationClass                  = "com/skt/m/Vibration"
	Graphics3DClass                 = "com/skt/m3d/Graphics3D"
	Object3DClass                   = "com/skt/m3d/Object3D"
	FileInputStreamClass            = "com/xce/io/FileInputStream"
	FileOutputStreamClass           = "com/xce/io/FileOutputStream"
	XFileClass                      = "com/xce/io/XFile"
	ToolkitClass                    = "com/xce/lcdui/Toolkit"
	XDisplayClass                   = "com/xce/lcdui/XDisplay"
	XTextFieldClass                 = "com/xce/lcdui/XTextField"
	RuntimeAudioClipClass           = "net/wfeature/RuntimeAudioClip"
)

// Define installs the runtime-owned SKVM surface on a VM. It follows MIDP,
// which it is built on: com.skt.m.Graphics2D wraps a MIDP Graphics, and
// com.xce.io writes through the same save boundary MIDP RMS uses.
func Define(machine *jvm.VM) error {
	for _, definition := range definitions() {
		if err := machine.DefineClass(definition); err != nil {
			return fmt.Errorf("SKVM library: %w", err)
		}
	}
	return nil
}

// forwardInit is the body of a constructor that only hands its arguments to
// the private native behind it, which is where the platform builds the state
// the object stands for.
func forwardInit(className, name, descriptor string) jvm.ContextMethod {
	return func(call *jvm.Invocation, arguments []jvm.Value) (jvm.Value, error) {
		object, err := receiver(arguments)
		if err != nil {
			return jvm.VoidValue(), err
		}
		_, err = call.InvokeSpecial(object, className, name, descriptor, arguments[1:]...)
		return jvm.VoidValue(), err
	}
}

// emptyInit is the body of a constructor for a class that keeps no state of
// its own: its methods are static, or the platform holds what they need.
func emptyInit(_ *jvm.Invocation, _ []jvm.Value) (jvm.Value, error) {
	return jvm.VoidValue(), nil
}

// exceptionInit hands an exception constructor's arguments to its superclass.
func exceptionInit(descriptor string) jvm.ContextMethod {
	return func(call *jvm.Invocation, arguments []jvm.Value) (jvm.Value, error) {
		object, err := receiver(arguments)
		if err != nil {
			return jvm.VoidValue(), err
		}
		_, err = call.InvokeSpecial(object, "java/lang/Exception", "<init>", descriptor, arguments[1:]...)
		return jvm.VoidValue(), err
	}
}

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

// Definitions is the SKVM surface as data, for the tools that have to see it
// rather than run it. See jvm.CoreLibraryDefinitions.
func Definitions() []jvm.ClassDefinition { return definitions() }
