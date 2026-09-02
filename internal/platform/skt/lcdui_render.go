package skt

import (
	"fmt"

	"github.com/movingwoo/wfeature/internal/api/midp"
	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/jvm"
)

// The runtime draws its own screens, so it picks its own colors. They are
// chosen for contrast on a small display rather than to imitate any one
// handset's theme.
const (
	screenBackground    uint32 = 0xffffff
	screenForeground    uint32 = 0x000000
	screenTitleBar      uint32 = 0x2b4c7e
	screenTitleText     uint32 = 0xffffff
	screenSelectionFill uint32 = 0x2b4c7e
	screenSelectionText uint32 = 0xffffff
	screenDivider       uint32 = 0x9aa5b1
	screenMenuFill      uint32 = 0xf0f2f5
)

const (
	screenPadding    = 2
	screenIndicatorW = 10
)

// refreshCurrentScreen repaints the current Displayable if it is a
// runtime-drawn Screen and the argument is that screen (or nil, meaning "the
// current one, whatever it is"). A mutation to a screen nobody is looking at
// costs nothing.
func (runtime *Runtime) refreshCurrentScreen(screen *jvm.Object) error {
	runtime.displayMu.RLock()
	current := runtime.currentDisplayable
	runtime.displayMu.RUnlock()
	if current == nil || (screen != nil && screen != current) {
		return nil
	}
	if !runtime.isScreen(current) {
		return nil
	}
	return runtime.queueScreenPaint(current)
}

// queueScreenPaint posts the paint onto the same bounded FIFO the Canvas
// repaint uses, so a screen redraw cannot reenter a guest callback that is
// already running.
func (runtime *Runtime) queueScreenPaint(screen *jvm.Object) error {
	runtime.displayMu.Lock()
	if runtime.currentDisplayable != screen {
		runtime.displayMu.Unlock()
		return nil
	}
	if runtime.screenPaintQueued {
		runtime.displayMu.Unlock()
		return nil
	}
	runtime.screenPaintQueued = true
	runtime.displayMu.Unlock()
	if err := runtime.events.Post("Screen.paint", runtime.paintPendingScreen); err != nil {
		runtime.displayMu.Lock()
		runtime.screenPaintQueued = false
		runtime.displayMu.Unlock()
		return fmt.Errorf("queue Screen paint: %w", err)
	}
	return nil
}

func (runtime *Runtime) paintPendingScreen() error {
	runtime.displayMu.Lock()
	runtime.screenPaintQueued = false
	current := runtime.currentDisplayable
	runtime.displayMu.Unlock()
	if current == nil || !runtime.isScreen(current) || runtime.State() != StateActive {
		return nil
	}
	return runtime.paintScreen(current)
}

// paintScreen draws one screen into the Host framebuffer and presents it.
func (runtime *Runtime) paintScreen(screen *jvm.Object) error {
	data := runtime.displayableState(screen)
	content := runtime.screenState(screen, screenNone)
	font, _ := fontReceiver(runtime.fontObject(fontSystem, fontPlain, fontMedium))
	titleFont, _ := fontReceiver(runtime.fontObject(fontSystem, fontBold, fontMedium))

	runtime.renderMu.Lock()
	defer runtime.renderMu.Unlock()

	full := paintRect{minX: 0, minY: 0, maxX: runtime.frameWidth, maxY: runtime.frameHeight}
	context := &graphicsContext{
		pixels:     runtime.frameRGBA,
		width:      runtime.frameWidth,
		height:     runtime.frameHeight,
		deviceClip: full,
		clip:       full,
		active:     true,
	}
	context.color = screenBackground
	context.fillClipped(full)

	top := 0
	state := runtime.lcdui()
	state.mu.Lock()
	title := data.title
	ticker := tickerText(data.ticker)
	commands := append([]*jvm.Object(nil), data.commands...)
	menuOpen := data.menuOpen
	menuIndex := data.menuIndex
	state.mu.Unlock()

	if title != "" {
		height := titleFont.height + 2*screenPadding
		context.color = screenTitleBar
		context.fillClipped(paintRect{minX: 0, minY: 0, maxX: runtime.frameWidth, maxY: height})
		context.color = screenTitleText
		context.font = runtime.fontObject(fontSystem, fontBold, fontMedium)
		titleFont.render(context, []rune(trimToWidth(titleFont, title, runtime.frameWidth-2*screenPadding)),
			int64(screenPadding), int64(screenPadding))
		top = height
	}
	if ticker != "" {
		height := font.height + screenPadding
		context.color = screenForeground
		font.render(context, []rune(trimToWidth(font, ticker, runtime.frameWidth-2*screenPadding)),
			int64(screenPadding), int64(top+screenPadding/2))
		top += height
		context.color = screenDivider
		context.fillClipped(paintRect{minX: 0, minY: top, maxX: runtime.frameWidth, maxY: top + 1})
		top++
	}

	bottom := runtime.frameHeight
	if len(commands) > 0 {
		bottom -= font.height + 2*screenPadding
	}
	if bottom < top {
		bottom = top
	}

	body := paintRect{minX: 0, minY: top, maxX: runtime.frameWidth, maxY: bottom}
	context.clip = body
	switch content.kind {
	case screenForm:
		runtime.drawFormBody(context, font, content, body)
	case screenList:
		runtime.drawChoiceBody(context, font, content.choice, content.selection, body, true)
	case screenTextBox:
		runtime.drawTextBody(context, font, content, body)
	case screenAlert:
		runtime.drawAlertBody(context, font, content, body)
	}
	context.clip = full

	if len(commands) > 0 {
		runtime.drawSoftKeys(context, font, commands, bottom)
	}
	if menuOpen {
		runtime.drawCommandMenu(context, font, commands, menuIndex)
	}

	context.active = false
	frame := backend.Frame{
		Width:  runtime.frameWidth,
		Height: runtime.frameHeight,
		RGBA:   append([]byte(nil), runtime.frameRGBA...),
	}
	if err := runtime.framebuffer.Present(frame); err != nil {
		return fmt.Errorf("present Screen %s: %w", screen.ClassName, err)
	}
	return nil
}

// itemExtent is the space one Form item wants. It is also what
// getPreferredWidth/Height answer, so a game laying out against those numbers
// sees the same ones the renderer uses.
func (runtime *Runtime) itemExtent(data *itemData) (int, int) {
	font, _ := fontReceiver(runtime.fontObject(fontSystem, fontPlain, fontMedium))
	width := runtime.frameWidth - 2*screenPadding
	height := 0
	if data.label != "" {
		height += font.height
	}
	switch data.kind {
	case itemString, itemText:
		lines := wrapText(font, string(data.text), width)
		if len(lines) == 0 {
			lines = []string{""}
		}
		height += len(lines) * font.height
	case itemImage:
		if image, err := midpImageOf(data.image); err == nil && image != nil {
			height += image.height
			if image.width+2*screenPadding < width {
				width = image.width + 2*screenPadding
			}
		} else {
			height += font.height
		}
	case itemChoice:
		if data.choice != nil {
			height += max(len(data.choice.elements), 1) * font.height
		}
	}
	if height == 0 {
		height = font.height
	}
	return width, height
}

func (runtime *Runtime) drawFormBody(context *graphicsContext, font *fontData, content *screenData, body paintRect) {
	y := body.minY + screenPadding - content.scroll
	for index, item := range content.items {
		data, err := itemOf(item)
		if err != nil {
			continue
		}
		_, height := runtime.itemExtent(data)
		if index == content.selection && runtime.itemIsInteractive(data) {
			context.color = screenSelectionFill
			context.fillClipped(paintRect{minX: body.minX, minY: y - 1, maxX: body.maxX, maxY: y + height + 1})
		}
		runtime.drawItem(context, font, data, index == content.selection, body, y)
		y += height + screenPadding
	}
}

// itemIsInteractive reports whether the selection cursor stops on an item.
// A StringItem with no commands cannot be acted on, so skipping it is what a
// device does.
func (runtime *Runtime) itemIsInteractive(data *itemData) bool {
	if len(data.commands) > 0 {
		return true
	}
	return data.kind == itemChoice || data.kind == itemText
}

func (runtime *Runtime) drawItem(context *graphicsContext, font *fontData, data *itemData, selected bool, body paintRect, y int) {
	textColor := screenForeground
	if selected && runtime.itemIsInteractive(data) {
		textColor = screenSelectionText
	}
	x := body.minX + screenPadding
	width := body.maxX - body.minX - 2*screenPadding
	if data.label != "" {
		context.color = textColor
		font.render(context, []rune(trimToWidth(font, data.label, width)), int64(x), int64(y))
		y += font.height
	}
	switch data.kind {
	case itemString, itemText:
		context.color = textColor
		for _, line := range wrapText(font, string(data.text), width) {
			font.render(context, []rune(line), int64(x), int64(y))
			y += font.height
		}
	case itemImage:
		if image, err := midpImageOf(data.image); err == nil && image != nil {
			context.drawRGBA(image.snapshot(), image.width, image.height, int64(x), int64(y))
			break
		}
		context.color = textColor
		font.render(context, []rune(trimToWidth(font, data.altText, width)), int64(x), int64(y))
	case itemChoice:
		if data.choice == nil {
			break
		}
		for index, element := range data.choice.elements {
			context.color = textColor
			marker := "( )"
			if data.choice.kind == choiceMultiple {
				marker = "[ ]"
				if element.selected {
					marker = "[x]"
				}
			} else if element.selected {
				marker = "(*)"
			}
			label := marker + " " + element.text
			font.render(context, []rune(trimToWidth(font, label, width)), int64(x), int64(y))
			y += font.height
			_ = index
		}
	}
}

// drawChoiceBody renders a List: one element per row with the focused row
// highlighted.
func (runtime *Runtime) drawChoiceBody(context *graphicsContext, font *fontData, choice *choiceData, selection int, body paintRect, showMarker bool) {
	if choice == nil {
		return
	}
	rowHeight := font.height + screenPadding
	visible := max((body.maxY-body.minY)/max(rowHeight, 1), 1)
	first := 0
	if selection >= visible {
		first = selection - visible + 1
	}
	y := body.minY
	for index := first; index < len(choice.elements) && y+rowHeight <= body.maxY; index++ {
		element := choice.elements[index]
		textColor := screenForeground
		if index == selection {
			context.color = screenSelectionFill
			context.fillClipped(paintRect{minX: body.minX, minY: y, maxX: body.maxX, maxY: y + rowHeight})
			textColor = screenSelectionText
		}
		x := body.minX + screenPadding
		if showMarker && choice.kind != choiceImplicit {
			marker := "( )"
			if choice.kind == choiceMultiple {
				marker = "[ ]"
				if element.selected {
					marker = "[x]"
				}
			} else if element.selected {
				marker = "(*)"
			}
			context.color = textColor
			font.render(context, []rune(marker), int64(x), int64(y+screenPadding/2))
			x += screenIndicatorW + font.textWidth([]rune(marker)) - screenIndicatorW
			x += screenPadding
		}
		if element.image != nil {
			if image, err := midpImageOf(element.image); err == nil && image != nil {
				context.drawRGBA(image.snapshot(), image.width, image.height, int64(x), int64(y))
				x += image.width + screenPadding
			}
		}
		context.color = textColor
		font.render(context, []rune(trimToWidth(font, element.text, body.maxX-x-screenPadding)),
			int64(x), int64(y+screenPadding/2))
		y += rowHeight
	}
}

func (runtime *Runtime) drawTextBody(context *graphicsContext, font *fontData, content *screenData, body paintRect) {
	context.color = screenDivider
	context.fillClipped(paintRect{minX: body.minX + screenPadding, minY: body.minY + screenPadding,
		maxX: body.maxX - screenPadding, maxY: body.maxY - screenPadding})
	context.color = screenBackground
	context.fillClipped(paintRect{minX: body.minX + screenPadding + 1, minY: body.minY + screenPadding + 1,
		maxX: body.maxX - screenPadding - 1, maxY: body.maxY - screenPadding - 1})
	context.color = screenForeground
	display := string(content.text)
	if content.constraint&midp.TextFieldConstraintMask == midp.TextFieldPasswordConstraint ||
		content.constraint&midp.TextFieldPassword != 0 {
		display = ""
		for range content.text {
			display += "*"
		}
	}
	y := body.minY + 2*screenPadding
	for _, line := range wrapText(font, display, body.maxX-body.minX-4*screenPadding) {
		if y+font.height > body.maxY-screenPadding {
			break
		}
		font.render(context, []rune(line), int64(body.minX+2*screenPadding), int64(y))
		y += font.height
	}
}

func (runtime *Runtime) drawAlertBody(context *graphicsContext, font *fontData, content *screenData, body paintRect) {
	y := body.minY + screenPadding
	if image, err := midpImageOf(content.alertImage); err == nil && image != nil {
		left := body.minX + max((body.maxX-body.minX-image.width)/2, 0)
		context.drawRGBA(image.snapshot(), image.width, image.height, int64(left), int64(y))
		y += image.height + screenPadding
	}
	context.color = screenForeground
	for _, line := range wrapText(font, content.alertText, body.maxX-body.minX-2*screenPadding) {
		if y+font.height > body.maxY {
			break
		}
		width := font.textWidth([]rune(line))
		left := body.minX + max((body.maxX-body.minX-width)/2, 0)
		font.render(context, []rune(line), int64(left), int64(y))
		y += font.height
	}
}

// drawSoftKeys draws the command bar. Two commands get the two soft keys; a
// third and beyond live behind the right key, which opens the menu instead.
func (runtime *Runtime) drawSoftKeys(context *graphicsContext, font *fontData, commands []*jvm.Object, top int) {
	context.color = screenDivider
	context.fillClipped(paintRect{minX: 0, minY: top, maxX: runtime.frameWidth, maxY: top + 1})
	context.color = screenForeground
	left := commandLabelText(commands[0])
	font.render(context, []rune(trimToWidth(font, left, runtime.frameWidth/2-screenPadding)),
		int64(screenPadding), int64(top+screenPadding))
	if len(commands) == 1 {
		return
	}
	right := commandLabelText(commands[1])
	if len(commands) > 2 {
		right = "메뉴"
	}
	width := font.textWidth([]rune(right))
	font.render(context, []rune(right), int64(runtime.frameWidth-width-screenPadding), int64(top+screenPadding))
}

// drawCommandMenu overlays the full command list. Without it a screen with
// three or more commands would leave the third unreachable.
func (runtime *Runtime) drawCommandMenu(context *graphicsContext, font *fontData, commands []*jvm.Object, selection int) {
	rowHeight := font.height + screenPadding
	height := min(len(commands)*rowHeight+2*screenPadding, runtime.frameHeight)
	top := runtime.frameHeight - height
	area := paintRect{minX: 0, minY: top, maxX: runtime.frameWidth, maxY: runtime.frameHeight}
	context.color = screenMenuFill
	context.fillClipped(area)
	context.color = screenDivider
	context.fillClipped(paintRect{minX: 0, minY: top, maxX: runtime.frameWidth, maxY: top + 1})
	y := top + screenPadding
	for index, command := range commands {
		textColor := screenForeground
		if index == selection {
			context.color = screenSelectionFill
			context.fillClipped(paintRect{minX: 0, minY: y, maxX: runtime.frameWidth, maxY: y + rowHeight})
			textColor = screenSelectionText
		}
		context.color = textColor
		font.render(context, []rune(trimToWidth(font, commandLabelText(command), runtime.frameWidth-2*screenPadding)),
			int64(screenPadding), int64(y+screenPadding/2))
		y += rowHeight
	}
}

// midpImageOf resolves an Image object to its pixels, tolerating null.
func midpImageOf(object *jvm.Object) (*imageData, error) {
	if object == nil {
		return nil, nil
	}
	image, ok := object.Native.(*imageData)
	if !isImageClass(object.ClassName) || !ok {
		return nil, fmt.Errorf("not a MIDP Image")
	}
	return image, nil
}
