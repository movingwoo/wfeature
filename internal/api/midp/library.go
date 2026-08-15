package midp

import _ "embed"

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

//go:embed classdata/javax/microedition/lcdui/Canvas.class
var canvasClass []byte

//go:embed classdata/javax/microedition/lcdui/Display.class
var displayClass []byte

//go:embed classdata/javax/microedition/lcdui/Displayable.class
var displayableClass []byte

//go:embed classdata/javax/microedition/lcdui/Graphics.class
var graphicsClass []byte

//go:embed classdata/javax/microedition/lcdui/Font.class
var fontClass []byte

//go:embed classdata/javax/microedition/lcdui/Image.class
var imageClass []byte

//go:embed classdata/javax/microedition/midlet/MIDlet.class
var midletClass []byte

//go:embed classdata/javax/microedition/midlet/MIDletStateChangeException.class
var midletStateChangeExceptionClass []byte

//go:embed classdata/javax/microedition/rms/RecordStore.class
var recordStoreClass []byte

//go:embed classdata/javax/microedition/rms/RecordSet.class
var recordSetClass []byte

//go:embed classdata/javax/microedition/rms/RecordEnumeration.class
var recordEnumerationClass []byte

//go:embed classdata/javax/microedition/rms/RecordFilter.class
var recordFilterClass []byte

//go:embed classdata/javax/microedition/rms/RecordComparator.class
var recordComparatorClass []byte

//go:embed classdata/javax/microedition/rms/RecordListener.class
var recordListenerClass []byte

//go:embed classdata/javax/microedition/rms/RecordStoreException.class
var recordStoreExceptionClass []byte

//go:embed classdata/javax/microedition/rms/RecordStoreNotOpenException.class
var recordStoreNotOpenExceptionClass []byte

//go:embed classdata/javax/microedition/rms/RecordStoreNotFoundException.class
var recordStoreNotFoundExceptionClass []byte

//go:embed classdata/javax/microedition/rms/RecordStoreFullException.class
var recordStoreFullExceptionClass []byte

//go:embed classdata/javax/microedition/rms/InvalidRecordIDException.class
var invalidRecordIDExceptionClass []byte

//go:embed classdata/javax/microedition/lcdui/Alert.class
var alertClass []byte

//go:embed classdata/javax/microedition/lcdui/AlertType.class
var alertTypeClass []byte

//go:embed classdata/javax/microedition/lcdui/Choice.class
var choiceClass []byte

//go:embed classdata/javax/microedition/lcdui/ChoiceGroup.class
var choiceGroupClass []byte

//go:embed classdata/javax/microedition/lcdui/Command.class
var commandClass []byte

//go:embed classdata/javax/microedition/lcdui/CommandListener.class
var commandListenerClass []byte

//go:embed classdata/javax/microedition/lcdui/Form.class
var formClass []byte

//go:embed classdata/javax/microedition/lcdui/ImageItem.class
var imageItemClass []byte

//go:embed classdata/javax/microedition/lcdui/Item.class
var itemClass []byte

//go:embed classdata/javax/microedition/lcdui/ItemCommandListener.class
var itemCommandListenerClass []byte

//go:embed classdata/javax/microedition/lcdui/ItemStateListener.class
var itemStateListenerClass []byte

//go:embed classdata/javax/microedition/lcdui/List.class
var listClass []byte

//go:embed classdata/javax/microedition/lcdui/Screen.class
var screenClass []byte

//go:embed classdata/javax/microedition/lcdui/StringItem.class
var stringItemClass []byte

//go:embed classdata/javax/microedition/lcdui/TextBox.class
var textBoxClass []byte

//go:embed classdata/javax/microedition/lcdui/TextField.class
var textFieldClass []byte

//go:embed classdata/javax/microedition/lcdui/Ticker.class
var tickerClass []byte

//go:embed classdata/javax/microedition/lcdui/game/GameCanvas.class
var gameCanvasClass []byte

//go:embed classdata/javax/microedition/media/Manager.class
var managerClass []byte

//go:embed classdata/javax/microedition/media/Player.class
var playerClass []byte

//go:embed classdata/javax/microedition/media/PlayerListener.class
var playerListenerClass []byte

//go:embed classdata/javax/microedition/media/MediaException.class
var mediaExceptionClass []byte

//go:embed classdata/javax/microedition/io/Connector.class
var connectorClass []byte

//go:embed classdata/javax/microedition/io/Connection.class
var connectionClass []byte

//go:embed classdata/javax/microedition/io/InputConnection.class
var inputConnectionClass []byte

//go:embed classdata/javax/microedition/io/OutputConnection.class
var outputConnectionClass []byte

//go:embed classdata/javax/microedition/io/StreamConnection.class
var streamConnectionClass []byte

//go:embed classdata/javax/microedition/io/ContentConnection.class
var contentConnectionClass []byte

//go:embed classdata/javax/microedition/io/StreamConnectionNotifier.class
var streamConnectionNotifierClass []byte

//go:embed classdata/javax/microedition/io/HttpConnection.class
var httpConnectionClass []byte

//go:embed classdata/javax/microedition/io/SocketConnection.class
var socketConnectionClass []byte

//go:embed classdata/javax/microedition/io/ConnectionNotFoundException.class
var connectionNotFoundExceptionClass []byte

// Library is the runtime-owned MIDP class source. It is placed before an
// application's JAR so a game cannot replace platform classes.
type Library struct{}

func (Library) ClassBytes(name string) ([]byte, bool) {
	switch name {
	case CanvasClass:
		return canvasClass, true
	case DisplayClass:
		return displayClass, true
	case DisplayableClass:
		return displayableClass, true
	case GraphicsClass:
		return graphicsClass, true
	case FontClass:
		return fontClass, true
	case ImageClass:
		return imageClass, true
	case MIDletClass:
		return midletClass, true
	case MIDletStateChangeExceptionClass:
		return midletStateChangeExceptionClass, true
	case RecordStoreClass:
		return recordStoreClass, true
	case RecordSetClass:
		return recordSetClass, true
	case RecordEnumerationClass:
		return recordEnumerationClass, true
	case RecordFilterClass:
		return recordFilterClass, true
	case RecordComparatorClass:
		return recordComparatorClass, true
	case RecordListenerClass:
		return recordListenerClass, true
	case RecordStoreExceptionClass:
		return recordStoreExceptionClass, true
	case RecordStoreNotOpenExceptionClass:
		return recordStoreNotOpenExceptionClass, true
	case RecordStoreNotFoundExceptionClass:
		return recordStoreNotFoundExceptionClass, true
	case RecordStoreFullExceptionClass:
		return recordStoreFullExceptionClass, true
	case InvalidRecordIDExceptionClass:
		return invalidRecordIDExceptionClass, true
	case AlertClass:
		return alertClass, true
	case AlertTypeClass:
		return alertTypeClass, true
	case ChoiceClass:
		return choiceClass, true
	case ChoiceGroupClass:
		return choiceGroupClass, true
	case CommandClass:
		return commandClass, true
	case CommandListenerClass:
		return commandListenerClass, true
	case FormClass:
		return formClass, true
	case ImageItemClass:
		return imageItemClass, true
	case ItemClass:
		return itemClass, true
	case ItemCommandListenerClass:
		return itemCommandListenerClass, true
	case ItemStateListenerClass:
		return itemStateListenerClass, true
	case ListClass:
		return listClass, true
	case ScreenClass:
		return screenClass, true
	case StringItemClass:
		return stringItemClass, true
	case TextBoxClass:
		return textBoxClass, true
	case TextFieldClass:
		return textFieldClass, true
	case TickerClass:
		return tickerClass, true
	case GameCanvasClass:
		return gameCanvasClass, true
	case ManagerClass:
		return managerClass, true
	case PlayerClass:
		return playerClass, true
	case PlayerListenerClass:
		return playerListenerClass, true
	case MediaExceptionClass:
		return mediaExceptionClass, true
	case ConnectorClass:
		return connectorClass, true
	case ConnectionClass:
		return connectionClass, true
	case InputConnectionClass:
		return inputConnectionClass, true
	case OutputConnectionClass:
		return outputConnectionClass, true
	case StreamConnectionClass:
		return streamConnectionClass, true
	case ContentConnectionClass:
		return contentConnectionClass, true
	case StreamConnectionNotifierClass:
		return streamConnectionNotifierClass, true
	case HTTPConnectionClass:
		return httpConnectionClass, true
	case SocketConnectionClass:
		return socketConnectionClass, true
	case ConnectionNotFoundExceptionClass:
		return connectionNotFoundExceptionClass, true
	}
	return nil, false
}
