package skt

import (
	"testing"

	"github.com/movingwoo/wfeature/internal/backend"

	"github.com/movingwoo/wfeature/internal/api/midp"
	"github.com/movingwoo/wfeature/internal/api/skvm"
	"github.com/movingwoo/wfeature/internal/jvm"
)

// testScreenGraphics is the one screen Graphics this vendor hands out, taken
// the way a title takes it: through the static field the toolkit publishes.
func testScreenGraphics(t *testing.T, runtime *Runtime) jvm.Value {
	t.Helper()
	value, err := runtime.toolkitScreenGraphics(nil, nil)
	if err != nil {
		t.Fatalf("Toolkit.graphics error = %v", err)
	}
	return value
}

// setGrayScale is a grey by one number where setColor wants three. A title
// reaching it used to fail to resolve the method in the middle of a paint,
// which ends the run rather than drawing the wrong shade.
func TestGraphicsSetGrayScaleIsAGreyColour(t *testing.T) {
	archive, err := Open(canvasJAR)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	runtime, err := Start(archive, Options{Framebuffer: newTestFramebuffer(t, 4, 4)})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	graphics := testScreenGraphics(t, runtime)
	if _, err := runtime.setGraphicsGrayScale(nil, []jvm.Value{graphics, jvm.IntValue(0x42)}); err != nil {
		t.Fatalf("setGrayScale(0x42) error = %v", err)
	}
	value, err := runtime.getGraphicsColor(nil, []jvm.Value{graphics})
	if err != nil {
		t.Fatalf("getColor() error = %v", err)
	}
	got, err := value.Int32()
	if err != nil {
		t.Fatal(err)
	}
	if got != 0x424242 {
		t.Errorf("getColor() after setGrayScale(0x42) = %#06x, want 0x424242", got)
	}
	if _, err := runtime.setGraphicsGrayScale(nil, []jvm.Value{graphics, jvm.IntValue(256)}); err == nil {
		t.Error("setGrayScale(256) succeeded, want the argument refused")
	}
	// The declaration has to carry it too, or a title's own resolution walks
	// past Graphics to Object and stops there.
	if !declares(midp.Definitions(), midp.GraphicsClass, "setGrayScale", "(I)V") {
		t.Error("Graphics.setGrayScale is not declared")
	}
}

// XDisplay.clear blacks out what a Graphics draws on. The one local caller
// passes a null image and a zero point and then writes white text on what it
// cleared, so what it leaves has to be dark.
func TestXDisplayClearBlacksOutTheDestination(t *testing.T) {
	archive, err := Open(canvasJAR)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	runtime, err := Start(archive, Options{Framebuffer: newTestFramebuffer(t, 4, 4)})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	graphics := testScreenGraphics(t, runtime)
	// Something on the screen first, so a clear that did nothing would show.
	if _, err := runtime.setGraphicsColor(nil, []jvm.Value{graphics, jvm.IntValue(0xff00ff)}); err != nil {
		t.Fatalf("setColor() error = %v", err)
	}
	if _, err := runtime.fillGraphicsRect(nil, []jvm.Value{
		graphics, jvm.IntValue(0), jvm.IntValue(0), jvm.IntValue(4), jvm.IntValue(4),
	}); err != nil {
		t.Fatalf("fillRect() error = %v", err)
	}
	if _, err := runtime.xDisplayClear(nil, []jvm.Value{
		graphics, jvm.ReferenceValue(nil), jvm.IntValue(0), jvm.IntValue(0),
	}); err != nil {
		t.Fatalf("XDisplay.clear() error = %v", err)
	}
	// Read the destination rather than a presented frame: a Host pass would
	// repaint the fixture's Canvas over the top of what the clear left.
	runtime.renderMu.Lock()
	surface := append([]byte(nil), runtime.frameRGBA...)
	runtime.renderMu.Unlock()
	frame := backend.Frame{Width: 4, Height: 4, RGBA: surface}
	for _, point := range [][2]int{{0, 0}, {3, 0}, {0, 3}, {3, 3}} {
		assertRGBAPixel(t, frame, point[0], point[1], []byte{0x00, 0x00, 0x00, 0xff})
	}
	if !declares(skvm.Definitions(), skvm.XDisplayClass, "clear",
		"(Ljavax/microedition/lcdui/Graphics;Ljavax/microedition/lcdui/Image;II)V") {
		t.Error("XDisplay.clear is not declared")
	}
}

// declares answers whether a class library carries a member. A native this
// runtime registers but does not declare resolves nowhere: the walk goes up to
// Object and the title ends there, which is how both members above were found.
func declares(definitions []jvm.ClassDefinition, class, name, descriptor string) bool {
	for _, definition := range definitions {
		if definition.Name != class {
			continue
		}
		for _, method := range definition.Methods {
			if method.Name == name && method.Descriptor == descriptor {
				return true
			}
		}
	}
	return false
}
