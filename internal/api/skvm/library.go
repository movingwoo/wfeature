package skvm

import _ "embed"

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

//go:embed classdata/com/skt/m/AudioClip.class
var audioClipBytes []byte

//go:embed classdata/com/skt/m/AudioSystem.class
var audioSystemBytes []byte

//go:embed classdata/com/skt/m/BackLight.class
var backLightBytes []byte

//go:embed classdata/com/skt/m/Call.class
var callBytes []byte

//go:embed classdata/com/skt/m/Device.class
var deviceBytes []byte

//go:embed classdata/com/skt/m/Graphics2D.class
var graphics2DBytes []byte

//go:embed classdata/com/skt/m/MathFP.class
var mathFPBytes []byte

//go:embed classdata/com/skt/m/PhoneBook.class
var phoneBookBytes []byte

//go:embed classdata/com/skt/m/ProgressBar.class
var progressBarBytes []byte

//go:embed classdata/com/skt/m/ResourceAllocException.class
var resourceAllocExceptionBytes []byte

//go:embed classdata/com/skt/m/SISImage.class
var sISImageBytes []byte

//go:embed classdata/com/skt/m/SMS.class
var sMSBytes []byte

//go:embed classdata/com/skt/m/SMSListener.class
var sMSListenerBytes []byte

//go:embed classdata/com/skt/m/SMSMessage.class
var sMSMessageBytes []byte

//go:embed classdata/com/skt/m/UnsupportedFormatException.class
var unsupportedFormatExceptionBytes []byte

//go:embed classdata/com/skt/m/UserStopException.class
var userStopExceptionBytes []byte

//go:embed classdata/com/skt/m/Vibration.class
var vibrationBytes []byte

//go:embed classdata/com/skt/m3d/Graphics3D.class
var graphics3DBytes []byte

//go:embed classdata/com/skt/m3d/Object3D.class
var object3DBytes []byte

//go:embed classdata/com/xce/io/FileInputStream.class
var fileInputStreamBytes []byte

//go:embed classdata/com/xce/io/FileOutputStream.class
var fileOutputStreamBytes []byte

//go:embed classdata/com/xce/io/XFile.class
var xFileBytes []byte

//go:embed classdata/com/xce/lcdui/Toolkit.class
var toolkitBytes []byte

//go:embed classdata/com/xce/lcdui/XDisplay.class
var xDisplayBytes []byte

//go:embed classdata/com/xce/lcdui/XTextField.class
var xTextFieldBytes []byte

//go:embed classdata/net/wfeature/RuntimeAudioClip.class
var runtimeAudioClipBytes []byte

// Library is the runtime-owned SKVM class source. It is placed before an
// application's JAR so a game cannot replace platform classes, and after the
// MIDP library because these classes are built on it.
type Library struct{}

func (Library) ClassBytes(name string) ([]byte, bool) {
	switch name {
	case AudioClipClass:
		return audioClipBytes, true
	case AudioSystemClass:
		return audioSystemBytes, true
	case BackLightClass:
		return backLightBytes, true
	case CallClass:
		return callBytes, true
	case DeviceClass:
		return deviceBytes, true
	case Graphics2DClass:
		return graphics2DBytes, true
	case MathFPClass:
		return mathFPBytes, true
	case PhoneBookClass:
		return phoneBookBytes, true
	case ProgressBarClass:
		return progressBarBytes, true
	case ResourceAllocExceptionClass:
		return resourceAllocExceptionBytes, true
	case SISImageClass:
		return sISImageBytes, true
	case SMSClass:
		return sMSBytes, true
	case SMSListenerClass:
		return sMSListenerBytes, true
	case SMSMessageClass:
		return sMSMessageBytes, true
	case UnsupportedFormatExceptionClass:
		return unsupportedFormatExceptionBytes, true
	case UserStopExceptionClass:
		return userStopExceptionBytes, true
	case VibrationClass:
		return vibrationBytes, true
	case Graphics3DClass:
		return graphics3DBytes, true
	case Object3DClass:
		return object3DBytes, true
	case FileInputStreamClass:
		return fileInputStreamBytes, true
	case FileOutputStreamClass:
		return fileOutputStreamBytes, true
	case XFileClass:
		return xFileBytes, true
	case ToolkitClass:
		return toolkitBytes, true
	case XDisplayClass:
		return xDisplayBytes, true
	case XTextFieldClass:
		return xTextFieldBytes, true
	case RuntimeAudioClipClass:
		return runtimeAudioClipBytes, true
	}
	return nil, false
}
