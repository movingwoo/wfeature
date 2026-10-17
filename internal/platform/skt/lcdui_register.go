package skt

import (
	"fmt"

	"github.com/movingwoo/wfeature/internal/api/midp"
	"github.com/movingwoo/wfeature/internal/jvm"
)

type nativeRegistration struct {
	class      string
	name       string
	descriptor string
	method     jvm.NativeMethod
}

// screenContent runs a native that reads or changes what a runtime-drawn
// screen shows — a List's or ChoiceGroup's elements, a Form's items, an
// Item's or an Alert's fields, a Ticker's text — under textMu. The Host paints
// that content and moves through it on a key under the same lock, and a title
// calls these from its own threads as freely as from a callback, so without it
// a List rebuilt during a key press is indexed through a stale length. None of
// these natives calls back into the guest, which is what makes holding the
// lock for the whole call safe; the text natives take it themselves.
func (runtime *Runtime) screenContent(method jvm.NativeMethod) jvm.NativeMethod {
	return func(vm *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
		runtime.textMu.Lock()
		defer runtime.textMu.Unlock()
		return method(vm, arguments)
	}
}

// registerHighLevelNatives connects the high-level lcdui surface, the
// GameCanvas buffer, and the media package to this runtime.
func (runtime *Runtime) registerHighLevelNatives() error {
	const (
		command  = "Ljavax/microedition/lcdui/Command;"
		item     = "Ljavax/microedition/lcdui/Item;"
		image    = "Ljavax/microedition/lcdui/Image;"
		font     = "Ljavax/microedition/lcdui/Font;"
		ticker   = "Ljavax/microedition/lcdui/Ticker;"
		graphics = "Ljavax/microedition/lcdui/Graphics;"
		text     = "Ljava/lang/String;"
	)

	registrations := []nativeRegistration{
		// Displayable — commands, title and ticker belong to every screen.
		{midp.DisplayableClass, "addCommand", "(" + command + ")V", runtime.addDisplayableCommand},
		{midp.DisplayableClass, "removeCommand", "(" + command + ")V", runtime.removeDisplayableCommand},
		{midp.DisplayableClass, "setCommandListener", "(Ljavax/microedition/lcdui/CommandListener;)V", runtime.setDisplayableCommandListener},
		{midp.DisplayableClass, "getTitle", "()" + text, runtime.displayableTitle},
		{midp.DisplayableClass, "setTitle", "(" + text + ")V", runtime.setDisplayableTitle},
		{midp.DisplayableClass, "getTicker", "()" + ticker, runtime.displayableTicker},
		{midp.DisplayableClass, "setTicker", "(" + ticker + ")V", runtime.setDisplayableTicker},

		{midp.CommandClass, "init", "(" + text + text + "II)V", runtime.initCommand},
		{midp.CommandClass, "getLabel", "()" + text, runtime.commandLabel},
		{midp.CommandClass, "getLongLabel", "()" + text, runtime.commandLongLabel},
		{midp.CommandClass, "getCommandType", "()I", runtime.commandType},
		{midp.CommandClass, "getPriority", "()I", runtime.commandPriority},

		{midp.TickerClass, "init", "(" + text + ")V", runtime.screenContent(runtime.initTicker)},
		{midp.TickerClass, "getString", "()" + text, runtime.screenContent(runtime.tickerString)},
		{midp.TickerClass, "setString", "(" + text + ")V", runtime.screenContent(runtime.setTickerString)},

		{midp.ItemClass, "initItem", "()V", runtime.screenContent(runtime.initItem)},
		{midp.ItemClass, "getLabel", "()" + text, runtime.screenContent(runtime.itemLabel)},
		{midp.ItemClass, "setLabel", "(" + text + ")V", runtime.screenContent(runtime.setItemLabel)},
		{midp.ItemClass, "getLayout", "()I", runtime.screenContent(runtime.itemLayout)},
		{midp.ItemClass, "setLayout", "(I)V", runtime.screenContent(runtime.setItemLayout)},
		{midp.ItemClass, "addCommand", "(" + command + ")V", runtime.screenContent(runtime.addItemCommand)},
		{midp.ItemClass, "removeCommand", "(" + command + ")V", runtime.screenContent(runtime.removeItemCommand)},
		{midp.ItemClass, "setDefaultCommand", "(" + command + ")V", runtime.screenContent(runtime.setItemDefaultCommand)},
		{midp.ItemClass, "setItemCommandListener", "(Ljavax/microedition/lcdui/ItemCommandListener;)V", runtime.screenContent(runtime.setItemCommandListener)},
		{midp.ItemClass, "getPreferredWidth", "()I", runtime.itemPreferredWidth},
		{midp.ItemClass, "getPreferredHeight", "()I", runtime.itemPreferredHeight},
		{midp.ItemClass, "getMinimumWidth", "()I", runtime.itemPreferredWidth},
		{midp.ItemClass, "getMinimumHeight", "()I", runtime.itemPreferredHeight},
		{midp.ItemClass, "notifyStateChanged", "()V", runtime.notifyItemStateChanged},

		{midp.StringItemClass, "initText", "(" + text + "I)V", runtime.screenContent(runtime.initStringItem)},
		{midp.StringItemClass, "getText", "()" + text, runtime.screenContent(runtime.stringItemText)},
		{midp.StringItemClass, "setText", "(" + text + ")V", runtime.screenContent(runtime.setStringItemText)},
		{midp.StringItemClass, "getAppearanceMode", "()I", runtime.screenContent(runtime.stringItemAppearance)},
		{midp.StringItemClass, "setFont", "(" + font + ")V", runtime.screenContent(runtime.setItemFont)},
		{midp.StringItemClass, "getFont", "()" + font, runtime.screenContent(runtime.itemFont)},

		{midp.ImageItemClass, "initImage", "(" + image + text + ")V", runtime.screenContent(runtime.initImageItem)},
		{midp.ImageItemClass, "getImage", "()" + image, runtime.screenContent(runtime.imageItemImage)},
		{midp.ImageItemClass, "setImage", "(" + image + ")V", runtime.screenContent(runtime.setImageItemImage)},
		{midp.ImageItemClass, "getAltText", "()" + text, runtime.screenContent(runtime.imageItemAltText)},
		{midp.ImageItemClass, "setAltText", "(" + text + ")V", runtime.screenContent(runtime.setImageItemAltText)},

		{midp.FormClass, "append", "(" + item + ")I", runtime.screenContent(runtime.formAppend)},
		{midp.FormClass, "insert", "(I" + item + ")V", runtime.screenContent(runtime.formInsert)},
		{midp.FormClass, "delete", "(I)V", runtime.screenContent(runtime.formDelete)},
		{midp.FormClass, "deleteAll", "()V", runtime.screenContent(runtime.formDeleteAll)},
		{midp.FormClass, "set", "(I" + item + ")V", runtime.screenContent(runtime.formSet)},
		{midp.FormClass, "get", "(I)" + item, runtime.screenContent(runtime.formGet)},
		{midp.FormClass, "size", "()I", runtime.screenContent(runtime.formSize)},
		{midp.FormClass, "setItemStateListener", "(Ljavax/microedition/lcdui/ItemStateListener;)V", runtime.setFormItemStateListener},

		{midp.ListClass, "setSelectCommand", "(" + command + ")V", runtime.screenContent(runtime.setListSelectCommand)},

		{midp.AlertClass, "initAlert", "(" + text + image + "Ljavax/microedition/lcdui/AlertType;)V", runtime.screenContent(runtime.initAlert)},
		{midp.AlertClass, "getString", "()" + text, runtime.screenContent(runtime.alertString)},
		{midp.AlertClass, "setString", "(" + text + ")V", runtime.screenContent(runtime.setAlertString)},
		{midp.AlertClass, "getImage", "()" + image, runtime.screenContent(runtime.alertImage)},
		{midp.AlertClass, "setImage", "(" + image + ")V", runtime.screenContent(runtime.setAlertImage)},
		{midp.AlertClass, "getType", "()Ljavax/microedition/lcdui/AlertType;", runtime.screenContent(runtime.alertType)},
		{midp.AlertClass, "setType", "(Ljavax/microedition/lcdui/AlertType;)V", runtime.screenContent(runtime.setAlertType)},
		{midp.AlertClass, "getTimeout", "()I", runtime.screenContent(runtime.alertTimeout)},
		{midp.AlertClass, "setTimeout", "(I)V", runtime.screenContent(runtime.setAlertTimeout)},
		{midp.AlertClass, "getDefaultTimeout", "()I", runtime.alertDefaultTimeout},
		{midp.AlertTypeClass, "playSound", "(Ljavax/microedition/lcdui/Display;)Z", runtime.playAlertSound},

		{midp.GameCanvasClass, "initBuffer", "(Z)V", runtime.initGameCanvasBuffer},
		{midp.GameCanvasClass, "getGraphics", "()" + graphics, runtime.gameCanvasGraphics},
		{midp.GameCanvasClass, "getKeyStates", "()I", runtime.gameCanvasKeyStates},
		{midp.GameCanvasClass, "flushGraphics", "()V", runtime.gameCanvasFlush},
		{midp.GameCanvasClass, "flushGraphics", "(IIII)V", runtime.gameCanvasFlush},
		{midp.GameCanvasClass, "drawBuffer", "(" + graphics + ")V", runtime.gameCanvasDrawBuffer},

		{midp.ManagerClass, "createPlayer", "(Ljava/io/InputStream;" + text + ")Ljavax/microedition/media/Player;", runtime.createPlayerFromStream},
		{midp.ManagerClass, "createPlayer", "(" + text + ")Ljavax/microedition/media/Player;", runtime.createPlayerFromLocator},
		{midp.ManagerClass, "playTone", "(III)V", runtime.playTone},
		{midp.ManagerClass, "getSupportedContentTypes", "(" + text + ")[" + text, runtime.supportedContentTypes},
		{midp.ManagerClass, "getSupportedProtocols", "(" + text + ")[" + text, runtime.supportedProtocols},

		{midp.PlayerClass, "realize", "()V", runtime.playerRealize},
		{midp.PlayerClass, "prefetch", "()V", runtime.playerPrefetch},
		{midp.PlayerClass, "start", "()V", runtime.playerStart},
		{midp.PlayerClass, "stop", "()V", runtime.playerStop},
		{midp.PlayerClass, "deallocate", "()V", runtime.playerDeallocate},
		{midp.PlayerClass, "close", "()V", runtime.playerClose},
		{midp.PlayerClass, "getState", "()I", runtime.playerState},
		{midp.PlayerClass, "getDuration", "()J", runtime.playerDuration},
		{midp.PlayerClass, "getMediaTime", "()J", runtime.playerMediaTime},
		{midp.PlayerClass, "setMediaTime", "(J)J", runtime.setPlayerMediaTime},
		{midp.PlayerClass, "setLoopCount", "(I)V", runtime.setPlayerLoopCount},
		{midp.PlayerClass, "getContentType", "()" + text, runtime.playerContentType},
		{midp.PlayerClass, "addPlayerListener", "(Ljavax/microedition/media/PlayerListener;)V", runtime.addPlayerListener},
		{midp.PlayerClass, "removePlayerListener", "(Ljavax/microedition/media/PlayerListener;)V", runtime.removePlayerListener},
		{midp.PlayerClass, "getControl", "(" + text + ")Ljavax/microedition/media/Control;", runtime.playerControl},
		{midp.PlayerClass, "getControls", "()[Ljavax/microedition/media/Control;", runtime.playerControls},
		{midp.RuntimeVolumeControlClass, "setLevel", "(I)I", runtime.setPlayerVolumeLevel},
		{midp.RuntimeVolumeControlClass, "getLevel", "()I", runtime.playerVolumeLevel},
		{midp.RuntimeVolumeControlClass, "setMute", "(Z)V", runtime.setPlayerVolumeMute},
		{midp.RuntimeVolumeControlClass, "isMuted", "()Z", runtime.playerVolumeMuted},
	}

	// TextField and TextBox have the same text methods on different receivers,
	// and ChoiceGroup and List the same choice methods, so both pairs are
	// registered from one table rather than written out twice.
	registrations = append(registrations, runtime.textRegistrations(midp.TextFieldClass, screenNone)...)
	registrations = append(registrations, runtime.textRegistrations(midp.TextBoxClass, screenTextBox)...)
	registrations = append(registrations, runtime.choiceRegistrations(midp.ChoiceGroupClass, screenNone)...)
	registrations = append(registrations, runtime.choiceRegistrations(midp.ListClass, screenList)...)

	for _, registration := range registrations {
		if err := runtime.registerNative(registration.class, registration.name, registration.descriptor, registration.method); err != nil {
			return fmt.Errorf("register %s.%s%s: %w", registration.class, registration.name, registration.descriptor, err)
		}
	}
	return nil
}

func (runtime *Runtime) textRegistrations(class string, kind screenKind) []nativeRegistration {
	const text = "Ljava/lang/String;"
	return []nativeRegistration{
		{class, "initText", "(" + text + "II)V", runtime.initText(kind)},
		{class, "getString", "()" + text, runtime.textString(kind)},
		{class, "setString", "(" + text + ")V", runtime.setTextString(kind)},
		{class, "getChars", "([C)I", runtime.textChars(kind)},
		{class, "setChars", "([CII)V", runtime.setTextChars(kind)},
		{class, "insert", "(" + text + "I)V", runtime.insertText(kind)},
		{class, "delete", "(II)V", runtime.deleteText(kind)},
		{class, "size", "()I", runtime.textSize(kind)},
		{class, "getMaxSize", "()I", runtime.textMaxSize(kind)},
		{class, "setMaxSize", "(I)I", runtime.setTextMaxSize(kind)},
		{class, "getCaretPosition", "()I", runtime.textCaretPosition(kind)},
		{class, "getConstraints", "()I", runtime.textConstraints(kind)},
		{class, "setConstraints", "(I)V", runtime.setTextConstraints(kind)},
	}
}

func (runtime *Runtime) choiceRegistrations(class string, kind screenKind) []nativeRegistration {
	const (
		text  = "Ljava/lang/String;"
		image = "Ljavax/microedition/lcdui/Image;"
		font  = "Ljavax/microedition/lcdui/Font;"
	)
	return []nativeRegistration{
		{class, "initChoice", "(I)V", runtime.screenContent(runtime.initChoice(kind))},
		{class, "size", "()I", runtime.screenContent(runtime.choiceSize(kind))},
		{class, "getString", "(I)" + text, runtime.screenContent(runtime.choiceString(kind))},
		{class, "getImage", "(I)" + image, runtime.screenContent(runtime.choiceImage(kind))},
		{class, "append", "(" + text + image + ")I", runtime.screenContent(runtime.choiceAppend(kind))},
		{class, "insert", "(I" + text + image + ")V", runtime.screenContent(runtime.choiceInsert(kind))},
		{class, "delete", "(I)V", runtime.screenContent(runtime.choiceDelete(kind))},
		{class, "deleteAll", "()V", runtime.screenContent(runtime.choiceDeleteAll(kind))},
		{class, "set", "(I" + text + image + ")V", runtime.screenContent(runtime.choiceSet(kind))},
		{class, "isSelected", "(I)Z", runtime.screenContent(runtime.choiceIsSelected(kind))},
		{class, "getSelectedIndex", "()I", runtime.screenContent(runtime.choiceSelectedIndex(kind))},
		{class, "getSelectedFlags", "([Z)I", runtime.screenContent(runtime.choiceSelectedFlags(kind))},
		{class, "setSelectedIndex", "(IZ)V", runtime.screenContent(runtime.choiceSetSelectedIndex(kind))},
		{class, "setSelectedFlags", "([Z)V", runtime.screenContent(runtime.choiceSetSelectedFlags(kind))},
		{class, "setFitPolicy", "(I)V", runtime.screenContent(runtime.choiceSetFitPolicy(kind))},
		{class, "getFitPolicy", "()I", runtime.screenContent(runtime.choiceFitPolicy(kind))},
		{class, "setFont", "(I" + font + ")V", runtime.ignoreChoiceElementFont},
		{class, "getFont", "(I)" + font, runtime.choiceElementFont},
	}
}
