package lgt

import "testing"

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
