package skvm

import "github.com/movingwoo/wfeature/internal/jvm"

// com.xce.lcdui.Toolkit's class initializer. The fields it fills are the
// handset's own metrics, so they are read from the font and the screen this
// runtime is actually drawing with rather than written down here: a title lays
// its menus out on FONT_HEIGHT, and a number that disagreed with what
// drawString paints would put every line in the wrong place.
const (
	toolkitFontDescriptor     = "Ljavax/microedition/lcdui/Font;"
	toolkitGraphicsDescriptor = "Ljavax/microedition/lcdui/Graphics;"
	// toolkitWidestChar is the character the vendor measured MAX_CHARWIDTH
	// with. It is the widest of the Latin capitals in a proportional face and
	// the whole cell in a fixed one, which is what a title reserving room for
	// one character is asking for.
	toolkitWidestChar = 'W'
)

func toolkitClassInit(call *jvm.Invocation, _ []jvm.Value) (jvm.Value, error) {
	font, err := call.InvokeStatic("javax/microedition/lcdui/Font", "getDefaultFont", "()Ljavax/microedition/lcdui/Font;")
	if err != nil {
		return jvm.VoidValue(), err
	}
	if err := call.SetStaticField(ToolkitClass, "DEFAULT_FONT", toolkitFontDescriptor, font); err != nil {
		return jvm.VoidValue(), err
	}
	fontObject, err := font.Reference()
	if err != nil {
		return jvm.VoidValue(), err
	}
	if fontObject == nil {
		return jvm.VoidValue(), jvm.Throw("java/lang/IllegalStateException", "no default font")
	}
	height, err := call.InvokeVirtual(fontObject, "getHeight", "()I")
	if err != nil {
		return jvm.VoidValue(), err
	}
	if err := call.SetStaticField(ToolkitClass, "FONT_HEIGHT", "I", height); err != nil {
		return jvm.VoidValue(), err
	}
	// FONT_GAP is the extra leading between lines. This runtime's font metrics
	// already include it in the height, so the gap on top of that is none.
	if err := call.SetStaticField(ToolkitClass, "FONT_GAP", "I", jvm.IntValue(0)); err != nil {
		return jvm.VoidValue(), err
	}
	widest, err := call.InvokeVirtual(fontObject, "charWidth", "(C)I", jvm.IntValue(toolkitWidestChar))
	if err != nil {
		return jvm.VoidValue(), err
	}
	if err := call.SetStaticField(ToolkitClass, "MAX_CHARWIDTH", "I", widest); err != nil {
		return jvm.VoidValue(), err
	}
	// The screen Graphics is the platform's, and it is the same object a
	// Canvas paint is handed: on this vendor that object outlives the paint,
	// which is the whole reason a title reaches it through a static field.
	graphics, err := call.InvokeStatic(ToolkitClass, "screenGraphics", "()Ljavax/microedition/lcdui/Graphics;")
	if err != nil {
		return jvm.VoidValue(), err
	}
	return jvm.VoidValue(), call.SetStaticField(ToolkitClass, "graphics", toolkitGraphicsDescriptor, graphics)
}
