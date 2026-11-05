package lgt

import (
	"slices"
	"testing"
)

// A Graphics that has never been given an alpha draws. The transparency is the
// specification's three-way one and zero means "draw nothing", so a state built
// from the zero value would paint a black screen for every title that never
// calls setAlpha — which is what one local title did until this was fixed.
func TestJavaGraphicsDrawsWithoutBeingGivenAnAlpha(t *testing.T) {
	client := fixtureClient(t)
	screen, err := client.screenSurface()
	if err != nil {
		t.Fatal(err)
	}
	object, err := client.newJavaGraphics(screen.handle)
	if err != nil {
		t.Fatalf("newJavaGraphics() error = %v", err)
	}
	if _, err := javaSetColorPacked(client, nil, nil, []uint32{object, 0xffffff}); err != nil {
		t.Fatal(err)
	}
	if _, err := javaFillRect(client, nil, nil, []uint32{object, 0, 0, 4, 4}); err != nil {
		t.Fatalf("javaFillRect() error = %v", err)
	}
	if pixel := screen.pixels[0]; pixel == 0 {
		t.Fatal("the fill left the surface black")
	}

	// And zero really does draw nothing, which is the other half of the rule.
	if _, err := javaSetAlpha(client, nil, nil, []uint32{object, 0}); err != nil {
		t.Fatal(err)
	}
	if _, err := javaSetColorPacked(client, nil, nil, []uint32{object, 0xff0000}); err != nil {
		t.Fatal(err)
	}
	before := screen.pixels[0]
	if _, err := javaFillRect(client, nil, nil, []uint32{object, 0, 0, 4, 4}); err != nil {
		t.Fatal(err)
	}
	if screen.pixels[0] != before {
		t.Errorf("a fill at alpha zero changed the surface to %#x", screen.pixels[0])
	}
}

// The anchor moves the point a draw was given to the corner the drawing code
// takes. The constants are the specification's own.
func TestJavaAnchorPlacesTheBox(t *testing.T) {
	cases := []struct {
		name   string
		anchor int
		wantX  int
		wantY  int
	}{
		{"top left", javaAnchorLeft | javaAnchorTop, 100, 50},
		{"no bits at all is the top left", 0, 100, 50},
		{"centred horizontally", javaAnchorHCenter | javaAnchorTop, 90, 50},
		{"right and bottom", javaAnchorRight | javaAnchorBottom, 80, 30},
		{"centred both ways", javaAnchorHCenter | javaAnchorVCenter, 90, 40},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			x, y := javaAnchorOrigin(100, 50, 20, 20, test.anchor)
			if x != test.wantX || y != test.wantY {
				t.Errorf("javaAnchorOrigin() = %d,%d, want %d,%d", x, y, test.wantX, test.wantY)
			}
		})
	}
}

// A clip keeps a draw inside it, and it is read in the coordinates the title
// set it in.
func TestJavaGraphicsClipBoundsADraw(t *testing.T) {
	client := fixtureClient(t)
	screen, err := client.screenSurface()
	if err != nil {
		t.Fatal(err)
	}
	object, err := client.newJavaGraphics(screen.handle)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := javaSetClip(client, nil, nil, []uint32{object, 2, 2, 2, 2}); err != nil {
		t.Fatal(err)
	}
	if _, err := javaSetColorPacked(client, nil, nil, []uint32{object, 0xffffff}); err != nil {
		t.Fatal(err)
	}
	if _, err := javaFillRect(client, nil, nil, []uint32{object, 0, 0, 8, 8}); err != nil {
		t.Fatal(err)
	}
	if screen.pixels[0] != 0 {
		t.Error("a pixel outside the clip was drawn")
	}
	if screen.pixels[2*screen.width+2] == 0 {
		t.Error("a pixel inside the clip was not drawn")
	}
	width, err := javaClipValue(2)(client, nil, nil, []uint32{object})
	if err != nil {
		t.Fatal(err)
	}
	if width != 2 {
		t.Errorf("getClipWidth() = %d, want 2", width)
	}
}

// A Java drawing call does not touch guest memory, in either direction. That
// is the difference between this path and a Clet's, and it is worth pinning
// because putting the pair of syncs back would be invisible — every frame
// would still be right, and the emulator would spend two thirds of its time
// copying 150 KiB round trips that never find anything.
//
// The test poisons guest memory behind the surface. A draw that read it back
// would bring the poison into the runtime's pixels; a draw that wrote through
// would take the poison out of guest memory. Neither may happen, and the
// flush's publish afterwards is what puts the two back in step.
func TestAJavaDrawLeavesGuestMemoryToTheFlush(t *testing.T) {
	client := fixtureClient(t)
	screen, err := client.screenSurface()
	if err != nil {
		t.Fatal(err)
	}
	if screen.address == 0 {
		t.Fatal("the screen surface has no guest address, so this proves nothing")
	}
	const poison = 0xa5
	bytes := make([]byte, len(screen.pixels)*2)
	for index := range bytes {
		bytes[index] = poison
	}
	if err := client.core.Memory().Write(screen.address, bytes); err != nil {
		t.Fatal(err)
	}

	object, err := client.newJavaGraphics(screen.handle)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := javaSetColorPacked(client, nil, nil, []uint32{object, 0xffffff}); err != nil {
		t.Fatal(err)
	}
	if _, err := javaFillRect(client, nil, nil, []uint32{object, 0, 0, 2, 2}); err != nil {
		t.Fatal(err)
	}

	// A pixel the fill did not cover still holds what the runtime had, not
	// what guest memory holds.
	outside := screen.pixels[len(screen.pixels)-1]
	if outside == poison<<8|poison {
		t.Error("the draw read the surface back out of guest memory")
	}
	// And guest memory still holds the poison, because the draw did not
	// publish.
	after := make([]byte, 4)
	if err := client.core.Memory().Read(screen.address, after); err != nil {
		t.Fatal(err)
	}
	if after[0] != poison || after[1] != poison {
		t.Errorf("the draw wrote %#x %#x through to guest memory", after[0], after[1])
	}

	// The flush is what reconciles them, and after it the drawn pixel is the
	// one guest memory holds.
	if err := client.syncToGuest(screen); err != nil {
		t.Fatal(err)
	}
	if err := client.core.Memory().Read(screen.address, after); err != nil {
		t.Fatal(err)
	}
	if got := uint16(after[0]) | uint16(after[1])<<8; got != screen.pixels[0] {
		t.Errorf("after the publish guest memory holds %#x, want the drawn %#x", got, screen.pixels[0])
	}
}

// A Graphics reports back what its own setters left, as KTF's does: the alpha,
// opaque until set; the XOR mode; the stroke style, which a reset puts back to
// SOLID; and the colour's three channels.
func TestJavaGraphicsReportsWhatItsSettersLeft(t *testing.T) {
	client, _, graphics := screenGraphicsFixture(t)
	ask := func(name string) uint32 {
		t.Helper()
		return mustJavaCall(t, client, javaGraphicsClass+"."+name, graphics)
	}
	if got := ask("getAlpha()I"); got != 255 {
		t.Errorf("a fresh Graphics' alpha = %d, want 255", got)
	}
	if got := ask("isXORMode()Z"); got != javaFalse {
		t.Errorf("a fresh Graphics is in XOR mode")
	}
	if got := ask("getStrokeStyle()I"); got != 0 {
		t.Errorf("a fresh Graphics' stroke style = %d, want SOLID", got)
	}
	mustJavaCall(t, client, javaGraphicsClass+".setAlpha(I)V", graphics, 7)
	mustJavaCall(t, client, javaGraphicsClass+".setXORMode(Z)V", graphics, 1)
	mustJavaCall(t, client, javaGraphicsClass+".setStrokeStyle(I)V", graphics, 1)
	mustJavaCall(t, client, javaGraphicsClass+".setColor(I)V", graphics, 0x7f123456)
	for name, want := range map[string]uint32{
		"getAlpha()I": 7, "isXORMode()Z": javaTrue, "getStrokeStyle()I": 1,
		"getRedComponent()I": 0x12, "getGreenComponent()I": 0x34, "getBlueComponent()I": 0x56,
	} {
		if got := ask(name); got != want {
			t.Errorf("%s = %#x, want %#x", name, got, want)
		}
	}
	// KTF keeps whatever stroke style it is given, and so does this.
	mustJavaCall(t, client, javaGraphicsClass+".setStrokeStyle(I)V", graphics, 0xffffffff)
	if got := ask("getStrokeStyle()I"); got != 0xffffffff {
		t.Errorf("getStrokeStyle() = %#x after setStrokeStyle(-1), want -1", got)
	}
	mustJavaCall(t, client, javaGraphicsClass+".reset()V", graphics)
	if got := ask("getStrokeStyle()I"); got != 0 {
		t.Errorf("getStrokeStyle() after reset = %d, want SOLID", got)
	}
	if got := ask("isXORMode()Z"); got != javaFalse {
		t.Error("reset left the Graphics in XOR mode")
	}
}

// A grey is the colour with the level on each channel, the level being the low
// eight bits of the argument — KTF's answers. getGrayScale stays unserved: KTF
// and the specification disagree on what it answers.
func TestJavaGraphicsGrayScaleIsAGreyColour(t *testing.T) {
	client, screen, graphics := screenGraphicsFixture(t)
	mustJavaCall(t, client, javaGraphicsClass+".setGrayScale(I)V", graphics, 0x80)
	for _, table := range []map[string]javaPlatformMethod{javaPlatformMethods, javaGraphicsMethods} {
		if _, served := table[javaGraphicsClass+".getGrayScale()I"]; served {
			t.Error("getGrayScale() is served, but KTF and the specification disagree on its answer")
		}
	}
	if got := mustJavaCall(t, client, javaGraphicsClass+".getColor()I", graphics); got != 0x808080 {
		t.Errorf("getColor() after setGrayScale = %#x, want 0x808080", got)
	}
	mustJavaCall(t, client, javaGraphicsClass+".fillRect(IIII)V", graphics, 0, 0, 1, 1)
	if got := screen.pixels[0]; got != rgb565(0x80, 0x80, 0x80) {
		t.Errorf("a fill after setGrayScale drew %#x, want the grey", got)
	}
	mustJavaCall(t, client, javaGraphicsClass+".setGrayScale(I)V", graphics, 0x1c0)
	if got := mustJavaCall(t, client, javaGraphicsClass+".getRedComponent()I", graphics); got != 0xc0 {
		t.Errorf("setGrayScale(0x1c0) left red at %#x, want the low byte 0xc0", got)
	}
}

// drawPolygon outlines the closed polygon: every edge is the line drawLine
// draws, the last point joined back to the first, through the translation and
// in the drawing mode — so in XOR mode the corners each edge shares are drawn
// twice, as two drawLine calls draw them.
func TestJavaGraphicsDrawPolygonIsItsEdgesDrawnAsLines(t *testing.T) {
	const drawPolygon = javaGraphicsClass + ".drawPolygon([I[I)V"
	for _, xor := range []uint32{0, 1} {
		polygonClient, polygonScreen, polygonGraphics := screenGraphicsFixture(t)
		lineClient, lineScreen, lineGraphics := screenGraphicsFixture(t)
		for _, side := range []struct {
			client   *Client
			graphics uint32
		}{{polygonClient, polygonGraphics}, {lineClient, lineGraphics}} {
			mustJavaCall(t, side.client, javaGraphicsClass+".setColor(I)V", side.graphics, 0xffffff)
			mustJavaCall(t, side.client, javaGraphicsClass+".setXORMode(Z)V", side.graphics, xor)
			mustJavaCall(t, side.client, javaGraphicsClass+".translate(II)V", side.graphics, 2, 1)
		}
		before := append([]uint16(nil), polygonScreen.pixels...)
		xs := newTestIntArray(t, polygonClient, 0, 9, 0)
		ys := newTestIntArray(t, polygonClient, 0, 0, 5)
		mustJavaCall(t, polygonClient, drawPolygon, polygonGraphics, xs, ys)
		for _, edge := range [][4]uint32{{0, 0, 9, 0}, {9, 0, 0, 5}, {0, 5, 0, 0}} {
			mustJavaCall(t, lineClient, javaGraphicsClass+".drawLine(IIII)V",
				lineGraphics, edge[0], edge[1], edge[2], edge[3])
		}
		drawn := 0
		for index := range polygonScreen.pixels {
			if polygonScreen.pixels[index] != lineScreen.pixels[index] {
				t.Fatalf("xor %d: pixel %d is %#x, and the three lines drew %#x",
					xor, index, polygonScreen.pixels[index], lineScreen.pixels[index])
			}
			if polygonScreen.pixels[index] != before[index] {
				drawn++
			}
		}
		if drawn == 0 {
			t.Fatalf("xor %d: the polygon drew nothing", xor)
		}
	}
}

// What drawPolygon is handed: a null array is the NullPointerException, arrays
// of two lengths give as many points as the shorter holds (KTF's reading), one
// point draws nothing, and a polygon of more points than the C slot takes is
// refused as that slot refuses one.
func TestJavaGraphicsDrawPolygonBoundsItsPoints(t *testing.T) {
	const drawPolygon = javaGraphicsClass + ".drawPolygon([I[I)V"
	client, screen, graphics := screenGraphicsFixture(t)
	mustJavaCall(t, client, javaGraphicsClass+".setColor(I)V", graphics, 0xffffff)
	points := newTestIntArray(t, client, 1, 2)
	if _, err := javaCall(t, client, drawPolygon, graphics, 0, points); thrownClass(err) != javaThrowNullClass {
		t.Errorf("drawPolygon(null, ys) = %v, want NullPointerException", err)
	}
	if _, err := javaCall(t, client, drawPolygon, graphics, points, 0); thrownClass(err) != javaThrowNullClass {
		t.Errorf("drawPolygon(xs, null) = %v, want NullPointerException", err)
	}
	before := append([]uint16(nil), screen.pixels...)
	mustJavaCall(t, client, drawPolygon, graphics, newTestIntArray(t, client, 3), newTestIntArray(t, client, 3, 4))
	for index := range before {
		if screen.pixels[index] != before[index] {
			t.Fatal("a polygon of one point drew")
		}
	}
	// Three xs and two ys are two points: the line between them.
	mustJavaCall(t, client, drawPolygon, graphics,
		newTestIntArray(t, client, 1, 6, 12), newTestIntArray(t, client, 2, 2))
	for x := 0; x < screen.width; x++ {
		lit := screen.pixels[2*screen.width+x] != before[2*screen.width+x]
		if want := x >= 1 && x <= 6; lit != want {
			t.Errorf("pixel (%d, 2) lit %v, want %v", x, lit, want)
		}
	}
	tooMany := make([]uint32, maxPolygonPoints+1)
	large := newTestIntArray(t, client, tooMany...)
	if _, err := javaCall(t, client, drawPolygon, graphics, large, large); err == nil || thrownClass(err) != "" {
		t.Errorf("a polygon of %d points = %v, want a refusal that is no guest exception", len(tooMany), err)
	}
}

// createImage(Image) is a copy a title can draw into — the specification's
// editable image, and KTF's mutable copy — of the source's size and pixels,
// with every masked pixel turned white and no mask of its own, as the
// specification's page says; drawing into it leaves the source alone.
// isMutable tells a decoded picture from one with pixels of its own.
func TestJavaImageCopyIsAMutablePictureOfItsOwn(t *testing.T) {
	const (
		createFromBytes = javaImageClass + ".createImage([BII)Lorg/kwis/msp/lcdui/Image;"
		createCopy      = javaImageClass + ".createImage(Lorg/kwis/msp/lcdui/Image;)Lorg/kwis/msp/lcdui/Image;"
		createBlank     = javaImageClass + ".createImage(II)Lorg/kwis/msp/lcdui/Image;"
		isMutable       = javaImageClass + ".isMutable()Z"
	)
	client := fixtureClient(t)
	encoded := encodedTestImage(t, 4, 2)
	bytes, err := client.newJavaByteArray(encoded)
	if err != nil {
		t.Fatal(err)
	}
	decoded := mustJavaCall(t, client, createFromBytes, bytes, 0, uint32(len(encoded)))
	if got := mustJavaCall(t, client, isMutable, decoded); got != javaFalse {
		t.Error("a decoded picture answers that it is mutable")
	}
	duplicate := mustJavaCall(t, client, createCopy, decoded)
	if duplicate == 0 || duplicate == decoded {
		t.Fatalf("the copy is %#x, want a new Image", duplicate)
	}
	if got := mustJavaCall(t, client, isMutable, duplicate); got != javaTrue {
		t.Error("the copy answers that it is immutable")
	}
	source, err := client.javaImageSurface(decoded)
	if err != nil {
		t.Fatal(err)
	}
	copied, err := client.javaImageSurface(duplicate)
	if err != nil {
		t.Fatal(err)
	}
	if copied == source || copied.width != 4 || copied.height != 2 {
		t.Fatalf("the copy is a %dx%d surface (shared: %v), want its own 4x2", copied.width, copied.height, copied == source)
	}
	if source.opaque == nil {
		t.Fatal("the source declares no transparency, so this proves nothing about the mask")
	}
	masked := 0
	for index := range source.pixels {
		pixel, drawn := source.blitPixel(index)
		want := pixel
		if !drawn {
			want, masked = copiedMaskPixel, masked+1
		}
		if copied.pixels[index] != want {
			t.Fatalf("the copy holds %#x at pixel %d, want %#x (masked in the source: %v)", copied.pixels[index], index, want, !drawn)
		}
	}
	if masked == 0 {
		t.Fatal("no source pixel is masked, so this proves nothing about the mask")
	}
	if copied.opaque != nil || copied.colorKeyed {
		t.Fatalf("the copy keeps a mask (%v, keyed %v), want none", copied.opaque, copied.colorKeyed)
	}
	// The copy is published, so a blit that reads it back out of guest memory
	// reads what it holds.
	published := make([]byte, len(copied.pixels)*2)
	if err := client.core.Memory().Read(copied.address, published); err != nil {
		t.Fatal(err)
	}
	for index, pixel := range copied.pixels {
		if got := uint16(published[index*2]) | uint16(published[index*2+1])<<8; got != pixel {
			t.Fatalf("guest memory holds %#x at pixel %d, want %#x", got, index, pixel)
		}
	}
	sourcePixels := append([]uint16(nil), source.pixels...)
	graphics := mustJavaCall(t, client, javaImageClass+".getGraphics()Lorg/kwis/msp/lcdui/Graphics;", duplicate)
	mustJavaCall(t, client, javaGraphicsClass+".setColor(I)V", graphics, 0x00ff00)
	mustJavaCall(t, client, javaGraphicsClass+".fillRect(IIII)V", graphics, 0, 0, 4, 2)
	if !slices.Equal(source.pixels, sourcePixels) {
		t.Error("drawing into the copy changed the source")
	}

	blank := mustJavaCall(t, client, createBlank, 3, 3)
	if got := mustJavaCall(t, client, isMutable, blank); got != javaTrue {
		t.Error("a picture made by createImage(width, height) answers that it is immutable")
	}
	// This platform lets a title draw into a decoded picture, giving it pixels
	// of its own first, and from then on it is the mutable kind. KTF's
	// getGraphics on a decoded picture draws nothing, and it stays immutable.
	mustJavaCall(t, client, javaImageClass+".getGraphics()Lorg/kwis/msp/lcdui/Graphics;", decoded)
	if got := mustJavaCall(t, client, isMutable, decoded); got != javaTrue {
		t.Error("a decoded picture given pixels of its own answers that it is immutable")
	}

	for _, key := range []string{createCopy, isMutable} {
		if _, err := javaCall(t, client, key, 0); thrownClass(err) != javaThrowNullClass {
			t.Errorf("%s of null = %v, want NullPointerException", key, err)
		}
	}
}
