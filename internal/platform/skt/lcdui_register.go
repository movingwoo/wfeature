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

		{midp.TickerClass, "init", "(" + text + ")V", runtime.initTicker},
		{midp.TickerClass, "getString", "()" + text, runtime.tickerString},
		{midp.TickerClass, "setString", "(" + text + ")V", runtime.setTickerString},

		{midp.ItemClass, "initItem", "()V", runtime.initItem},
		{midp.ItemClass, "getLabel", "()" + text, runtime.itemLabel},
		{midp.ItemClass, "setLabel", "(" + text + ")V", runtime.setItemLabel},
		{midp.ItemClass, "getLayout", "()I", runtime.itemLayout},
		{midp.ItemClass, "setLayout", "(I)V", runtime.setItemLayout},
		{midp.ItemClass, "addCommand", "(" + command + ")V", runtime.addItemCommand},
		{midp.ItemClass, "removeCommand", "(" + command + ")V", runtime.removeItemCommand},
		{midp.ItemClass, "setDefaultCommand", "(" + command + ")V", runtime.setItemDefaultCommand},
		{midp.ItemClass, "setItemCommandListener", "(Ljavax/microedition/lcdui/ItemCommandListener;)V", runtime.setItemCommandListener},
		{midp.ItemClass, "getPreferredWidth", "()I", runtime.itemPreferredWidth},
		{midp.ItemClass, "getPreferredHeight", "()I", runtime.itemPreferredHeight},
		{midp.ItemClass, "getMinimumWidth", "()I", runtime.itemPreferredWidth},
		{midp.ItemClass, "getMinimumHeight", "()I", runtime.itemPreferredHeight},
		{midp.ItemClass, "notifyStateChanged", "()V", runtime.notifyItemStateChanged},

		{midp.StringItemClass, "initText", "(" + text + "I)V", runtime.initStringItem},
		{midp.StringItemClass, "getText", "()" + text, runtime.stringItemText},
		{midp.StringItemClass, "setText", "(" + text + ")V", runtime.setStringItemText},
		{midp.StringItemClass, "getAppearanceMode", "()I", runtime.stringItemAppearance},
		{midp.StringItemClass, "setFont", "(" + font + ")V", runtime.setItemFont},
		{midp.StringItemClass, "getFont", "()" + font, runtime.itemFont},

		{midp.ImageItemClass, "initImage", "(" + image + text + ")V", runtime.initImageItem},
		{midp.ImageItemClass, "getImage", "()" + image, runtime.imageItemImage},
		{midp.ImageItemClass, "setImage", "(" + image + ")V", runtime.setImageItemImage},
		{midp.ImageItemClass, "getAltText", "()" + text, runtime.imageItemAltText},
		{midp.ImageItemClass, "setAltText", "(" + text + ")V", runtime.setImageItemAltText},

		{midp.FormClass, "append", "(" + item + ")I", runtime.formAppend},
		{midp.FormClass, "insert", "(I" + item + ")V", runtime.formInsert},
		{midp.FormClass, "delete", "(I)V", runtime.formDelete},
		{midp.FormClass, "deleteAll", "()V", runtime.formDeleteAll},
		{midp.FormClass, "set", "(I" + item + ")V", runtime.formSet},
		{midp.FormClass, "get", "(I)" + item, runtime.formGet},
		{midp.FormClass, "size", "()I", runtime.formSize},
		{midp.FormClass, "setItemStateListener", "(Ljavax/microedition/lcdui/ItemStateListener;)V", runtime.setFormItemStateListener},

		{midp.ListClass, "setSelectCommand", "(" + command + ")V", runtime.setListSelectCommand},

		{midp.AlertClass, "initAlert", "(" + text + image + "Ljavax/microedition/lcdui/AlertType;)V", runtime.initAlert},
		{midp.AlertClass, "getString", "()" + text, runtime.alertString},
		{midp.AlertClass, "setString", "(" + text + ")V", runtime.setAlertString},
		{midp.AlertClass, "getImage", "()" + image, runtime.alertImage},
		{midp.AlertClass, "setImage", "(" + image + ")V", runtime.setAlertImage},
		{midp.AlertClass, "getType", "()Ljavax/microedition/lcdui/AlertType;", runtime.alertType},
		{midp.AlertClass, "setType", "(Ljavax/microedition/lcdui/AlertType;)V", runtime.setAlertType},
		{midp.AlertClass, "getTimeout", "()I", runtime.alertTimeout},
		{midp.AlertClass, "setTimeout", "(I)V", runtime.setAlertTimeout},
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
		{class, "initChoice", "(I)V", runtime.initChoice(kind)},
		{class, "size", "()I", runtime.choiceSize(kind)},
		{class, "getString", "(I)" + text, runtime.choiceString(kind)},
		{class, "getImage", "(I)" + image, runtime.choiceImage(kind)},
		{class, "append", "(" + text + image + ")I", runtime.choiceAppend(kind)},
		{class, "insert", "(I" + text + image + ")V", runtime.choiceInsert(kind)},
		{class, "delete", "(I)V", runtime.choiceDelete(kind)},
		{class, "deleteAll", "()V", runtime.choiceDeleteAll(kind)},
		{class, "set", "(I" + text + image + ")V", runtime.choiceSet(kind)},
		{class, "isSelected", "(I)Z", runtime.choiceIsSelected(kind)},
		{class, "getSelectedIndex", "()I", runtime.choiceSelectedIndex(kind)},
		{class, "getSelectedFlags", "([Z)I", runtime.choiceSelectedFlags(kind)},
		{class, "setSelectedIndex", "(IZ)V", runtime.choiceSetSelectedIndex(kind)},
		{class, "setSelectedFlags", "([Z)V", runtime.choiceSetSelectedFlags(kind)},
		{class, "setFitPolicy", "(I)V", runtime.choiceSetFitPolicy(kind)},
		{class, "getFitPolicy", "()I", runtime.choiceFitPolicy(kind)},
		{class, "setFont", "(I" + font + ")V", runtime.ignoreChoiceElementFont},
		{class, "getFont", "(I)" + font, runtime.choiceElementFont},
	}
}
