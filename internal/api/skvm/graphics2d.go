package skvm

import "github.com/movingwoo/wfeature/internal/jvm"

// The two static methods on com.skt.m.Graphics2D that make one rather than
// draw with one. Both are built on the surface below them — the wrapper's own
// constructor and MIDP's Image — so they are Go bodies here rather than
// platform natives: nothing about either answer depends on the Host.

// graphics2DFor wraps a MIDP Graphics. Six local titles reach the extended
// drawing this way rather than through the constructor, and the object they
// get back is a new wrapper each time, which is what the constructor gives:
// the wrapper holds no state of its own beyond the Graphics it was handed.
func graphics2DFor(call *jvm.Invocation, arguments []jvm.Value) (jvm.Value, error) {
	if len(arguments) < 1 {
		return jvm.VoidValue(), jvm.Throw("java/lang/IllegalArgumentException", "getGraphics2D expected a Graphics")
	}
	graphics, err := arguments[0].Reference()
	if err != nil {
		return jvm.VoidValue(), err
	}
	if graphics == nil {
		return jvm.VoidValue(), jvm.Throw("java/lang/NullPointerException", "getGraphics2D graphics is null")
	}
	wrapper, err := call.NewObject(Graphics2DClass, "(Ljavax/microedition/lcdui/Graphics;)V", arguments[0])
	if err != nil {
		return jvm.VoidValue(), err
	}
	return jvm.ReferenceValue(wrapper), nil
}

// createMaskableImage answers a mutable image. The vendor's own name for it is
// about the per-pixel mask its Graphics2D can read and write; here that mask is
// the image's alpha, which every mutable image already has, so what this adds
// over Image.createImage is the name a title calls it by.
func createMaskableImage(call *jvm.Invocation, arguments []jvm.Value) (jvm.Value, error) {
	if len(arguments) < 2 {
		return jvm.VoidValue(), jvm.Throw("java/lang/IllegalArgumentException", "createMaskableImage expected a width and a height")
	}
	width, err := arguments[0].Int32()
	if err != nil {
		return jvm.VoidValue(), err
	}
	height, err := arguments[1].Int32()
	if err != nil {
		return jvm.VoidValue(), err
	}
	if width <= 0 || height <= 0 {
		return jvm.VoidValue(), jvm.Throw("java/lang/IllegalArgumentException", "createMaskableImage size must be positive")
	}
	return call.InvokeStatic("javax/microedition/lcdui/Image", "createImage", "(II)Ljavax/microedition/lcdui/Image;",
		jvm.IntValue(width), jvm.IntValue(height))
}
