package lgt

import (
	"slices"
	"testing"
)

const (
	cardPushKey     = javaDisplayClass + ".pushCard(Lorg/kwis/msp/lcdui/Card;)V"
	cardDisplayKey  = javaDisplayClass + ".getDefaultDisplay()Lorg/kwis/msp/lcdui/Display;"
	cardAtKey       = javaCardClass + ".<init>(IIII)V"
	cardOnAtKey     = javaCardClass + ".<init>(Lorg/kwis/msp/lcdui/Display;IIII)V"
	cardOnAtFlagKey = javaCardClass + ".<init>(Lorg/kwis/msp/lcdui/Display;IIIIZ)V"
	cardPlainKey    = javaCardClass + ".<init>()V"
	cardOnKey       = javaCardClass + ".<init>(Lorg/kwis/msp/lcdui/Display;)V"
	cardFlagKey     = javaCardClass + ".<init>(Z)V"
	cardMoveKey     = javaCardClass + ".move(II)V"
	cardResizeKey   = javaCardClass + ".resize(II)V"
	cardShownKey    = javaCardClass + ".isShown()Z"
	cardPaintSlot   = 20
)

// fillingPaint is a card's paint that fills one rectangle in the current
// colour through the Graphics it is handed, by dispatching the Graphics slot
// given: push {r4, lr}; sub sp, #4; movs r0, #height; str r0, [sp]; mov r0, r1;
// movs r1, #x; movs r2, #y; movs r3, #width; ldr r4, [pc, #8]; mov ip, r4;
// svc #5; add sp, #4; pop {r4, pc}; nop; and the slot word.
func fillingPaint(x, y, width, height uint8, slot uint32) []uint16 {
	return []uint16{
		0xb510, 0xb081, 0x2000 | uint16(height), 0x9000, 0x4608,
		0x2100 | uint16(x), 0x2200 | uint16(y), 0x2300 | uint16(width),
		0x4c02, 0x46a4, 0xdf05, 0xb001, 0xbd10, 0x46c0,
		uint16(slot), uint16(slot >> 16),
	}
}

// installPaintingCardClass builds a card class the title wrote, extending the
// platform's Card, whose paint fills from (x, y) to far past any card's size.
func installPaintingCardClass(t *testing.T, client *Client, name string, x, y uint8) *javaRuntimeClass {
	t.Helper()
	slot := installJavaWidgetVirtualCall(t, client, javaGraphicsClass, cardPaintSlot,
		javaMemberRef{Name: "fillRect", Descriptor: "(IIII)V"})
	paint := installThumbAt(t, client, 0, fillingPaint(x, y, 255, 255, slot)...)
	if (paint&^1)%4 != 0 {
		t.Fatalf("the paint routine at %#x leaves its literal unaligned", paint)
	}
	platform, err := client.preparePlatformJavaClass(javaCardClass)
	if err != nil {
		t.Fatal(err)
	}
	class := &javaRuntimeClass{
		Name: name, Handle: 1, Slots: platform.Slots, Instance: platform.Instance, Super: platform,
		Record: javaClass{Name: name, Methods: []javaMember{
			{Name: javaCardPaintMethod, Descriptor: "(Lorg/kwis/msp/lcdui/Graphics;)V", Body: paint},
		}},
	}
	if class.Object, err = client.allocateJavaClassObject(class); err != nil {
		t.Fatal(err)
	}
	class.dataBlock, _ = client.readWord(class.Object + 8)
	runtime := client.javaRuntimeState()
	runtime.byObject[class.Object] = class
	runtime.byName[name] = class
	if err := client.buildJavaVTable(class); err != nil {
		t.Fatal(err)
	}
	return class
}

func newTestCard(t *testing.T, client *Client, class *javaRuntimeClass) uint32 {
	t.Helper()
	card, err := client.allocateJavaObject(class)
	if err != nil {
		t.Fatal(err)
	}
	return card
}

// patternScreen gives every pixel of the screen a value of its own that no
// draw here makes, and answers a copy of them.
func patternScreen(t *testing.T, client *Client) []uint16 {
	t.Helper()
	screen, err := client.screenSurface()
	if err != nil {
		t.Fatal(err)
	}
	for index := range screen.pixels {
		screen.pixels[index] = uint16(0x8000 | index)
	}
	return slices.Clone(screen.pixels)
}

// paintCard pushes a card and runs the paint the push asks for, or repaints
// the card already shown.
func paintCard(t *testing.T, client *Client, card uint32) {
	t.Helper()
	runtime := client.javaRuntimeState()
	if runtime.card != card {
		mustJavaCall(t, client, cardPushKey, mustJavaCall(t, client, cardDisplayKey), card)
	} else {
		runtime.cardDirty = true
	}
	if err := client.PaintJava(t.Context()); err != nil {
		t.Fatal(err)
	}
}

// checkScreen compares the screen and the frame presented from it with what
// they should hold, pixel by pixel, naming the first few that differ.
func checkScreen(t *testing.T, client *Client, want []uint16, what string) {
	t.Helper()
	wrong := 0
	for index, pixel := range client.screen.pixels {
		if pixel != want[index] || client.presented[index] != want[index] {
			if wrong++; wrong <= 4 {
				t.Errorf("%s: pixel (%d, %d) = %#x, presented %#x, want %#x", what,
					index%client.screen.width, index/client.screen.width, pixel, client.presented[index], want[index])
			}
		}
	}
}

// filled is what a screen holds after a fill of black over a rectangle,
// clipped to the screen.
func filled(screen []uint16, width, left, top, right, bottom int) []uint16 {
	result := slices.Clone(screen)
	for y := max(top, 0); y < bottom && y < len(screen)/width; y++ {
		for x := max(left, 0); x < right && x < width; x++ {
			result[y*width+x] = 0
		}
	}
	return result
}

// The positioned constructors give a card the box they name, which getX, getY,
// getWidth and getHeight answer; move and resize change two of its numbers.
// A card built without a position, or with a box that is the screen, answers
// the screen, and so getWidth and getHeight answer what they always have. A
// size of zero or less is kept as given, as KTF keeps it.
func TestJavaCardAnswersItsBox(t *testing.T) {
	client := fixtureClient(t)
	class, err := client.preparePlatformJavaClass(javaCardClass)
	if err != nil {
		t.Fatal(err)
	}
	box := func(card uint32) [4]int32 {
		t.Helper()
		var answer [4]int32
		for index, name := range []string{"getX()I", "getY()I", "getWidth()I", "getHeight()I"} {
			answer[index] = int32(mustJavaCall(t, client, javaCardClass+"."+name, card))
		}
		return answer
	}
	display := mustJavaCall(t, client, cardDisplayKey)
	screen := [4]int32{0, 0, 16, 8}
	for _, build := range []struct {
		name      string
		arguments func(card uint32) []uint32
		key       string
		want      [4]int32
	}{
		{"Card()", func(card uint32) []uint32 { return []uint32{card} }, cardPlainKey, screen},
		{"Card(Display)", func(card uint32) []uint32 { return []uint32{card, display} }, cardOnKey, screen},
		{"Card(boolean)", func(card uint32) []uint32 { return []uint32{card, 1} }, cardFlagKey, screen},
		{"Card(x, y, w, h)", func(card uint32) []uint32 { return []uint32{card, 3, 2, 5, 4} }, cardAtKey, [4]int32{3, 2, 5, 4}},
		{"Card(Display, x, y, w, h)", func(card uint32) []uint32 { return []uint32{card, display, 0xfffffffe, 1, 20, 9} },
			cardOnAtKey, [4]int32{-2, 1, 20, 9}},
		{"Card(Display, x, y, w, h, boolean)", func(card uint32) []uint32 { return []uint32{card, display, 7, 0, 0, 0xffffffff, 1} },
			cardOnAtFlagKey, [4]int32{7, 0, 0, -1}},
		{"Card(x, y, w, h) the size of the screen", func(card uint32) []uint32 { return []uint32{card, 0, 0, 16, 8} }, cardAtKey, screen},
	} {
		card, err := client.allocateJavaObject(class)
		if err != nil {
			t.Fatal(err)
		}
		mustJavaCall(t, client, build.key, build.arguments(card)...)
		if got := box(card); got != build.want {
			t.Errorf("%s answers the box %v, want %v", build.name, got, build.want)
		}
		_, positioned := client.javaRuntimeState().cardBounds[card]
		if positioned != (build.want != screen) {
			t.Errorf("%s: a box is kept for it = %v, want %v", build.name, positioned, build.want != screen)
		}
	}

	card, err := client.allocateJavaObject(class)
	if err != nil {
		t.Fatal(err)
	}
	mustJavaCall(t, client, cardPlainKey, card)
	mustJavaCall(t, client, cardMoveKey, card, 4, 0xfffffffd)
	if got, want := box(card), [4]int32{4, -3, 16, 8}; got != want {
		t.Errorf("a card that is the whole screen moved to (4, -3) answers %v, want %v", got, want)
	}
	mustJavaCall(t, client, cardResizeKey, card, 6, 0)
	if got, want := box(card), [4]int32{4, -3, 6, 0}; got != want {
		t.Errorf("the card resized to 6 by 0 answers %v, want %v", got, want)
	}
	mustJavaCall(t, client, cardMoveKey, card, 0, 0)
	mustJavaCall(t, client, cardResizeKey, card, 16, 8)
	if _, positioned := client.javaRuntimeState().cardBounds[card]; positioned {
		t.Error("a card moved and resized back to the screen keeps a box")
	}
	if got := box(card); got != screen {
		t.Errorf("the card back at the screen answers %v", got)
	}
}

// A card with a box paints into a surface of its size whose origin is its
// corner: the fill its paint makes from (1, 1) lands one pixel in from the
// box's corner and stops at the box, the pixel at the box's corner keeps what
// the screen held there, and nothing outside the box changes. A move and a
// resize take effect at the next paint, the card's own drawing going with it;
// a resize makes the surface again from what the screen holds there.
func TestJavaCardWithABoxPaintsAtItsCornerInsideItsBox(t *testing.T) {
	client := fixtureClient(t)
	card := newTestCard(t, client, installPaintingCardClass(t, client, "game/Board", 1, 1))
	before := patternScreen(t, client)
	const width = 16

	mustJavaCall(t, client, cardAtKey, card, 3, 2, 5, 4)
	paintCard(t, client, card)
	first := filled(before, width, 4, 3, 8, 6)
	checkScreen(t, client, first, "the first paint at (3, 2)")
	runtime := client.javaRuntimeState()
	surface := client.framebuffer(runtime.cardSurface)
	if surface == nil || surface.width != 5 || surface.height != 4 || runtime.cardSurfaceOwner != card {
		t.Fatalf("the card paints into %+v for %#x, want a 5 by 4 surface of its own", surface, runtime.cardSurfaceOwner)
	}
	graphics, err := client.javaGraphicsState(runtime.screenGraphics)
	if err != nil {
		t.Fatal(err)
	}
	if graphics.surface != surface.handle || graphics.translateX != 0 || graphics.clipSet {
		t.Errorf("the Graphics the card painted through is %+v, want the card's surface from its corner", *graphics)
	}
	for which, want := range []uint32{0, 0, 5, 4} {
		if got, _ := javaClipValue(which)(client, nil, nil, []uint32{runtime.screenGraphics}); got != want {
			t.Errorf("clip value %d after the paint = %d, want %d", which, got, want)
		}
	}

	mustJavaCall(t, client, cardMoveKey, card, 9, 4)
	if !slices.Equal(client.screen.pixels, first) {
		t.Error("a move changed the screen before the next paint")
	}
	paintCard(t, client, card)
	second := slices.Clone(first)
	for row := range 4 {
		for column := range 5 {
			pixel := before[(2+row)*width+3+column]
			if row > 0 && column > 0 {
				pixel = 0
			}
			second[(4+row)*width+9+column] = pixel
		}
	}
	checkScreen(t, client, second, "the paint after the move to (9, 4)")

	mustJavaCall(t, client, cardResizeKey, card, 6, 3)
	paintCard(t, client, card)
	third := filled(second, width, 10, 5, 15, 7)
	checkScreen(t, client, third, "the paint after the resize to 6 by 3")
	if surface := client.framebuffer(runtime.cardSurface); surface == nil || surface.width != 6 || surface.height != 3 {
		t.Errorf("after the resize the card paints into %+v, want a 6 by 3 surface", surface)
	}
}

// A box partly off the screen shows the part that is on it; one wholly off it,
// at the far ends of the 32 bits a title can name, or with no width or height,
// shows nothing and changes nothing — its paint is still entered, into a
// surface nothing puts on the screen.
func TestJavaCardOffTheScreenShowsOnlyWhatIsOnIt(t *testing.T) {
	client := fixtureClient(t)
	class := installPaintingCardClass(t, client, "game/Board", 0, 0)
	before := patternScreen(t, client)
	partly := newTestCard(t, client, class)
	mustJavaCall(t, client, cardAtKey, partly, 0xfffffffe, 0xffffffff, 5, 4)
	paintCard(t, client, partly)
	shown := filled(before, 16, 0, 0, 3, 3)
	checkScreen(t, client, shown, "a card at (-2, -1)")
	for _, box := range [][4]uint32{
		{0x80000000, 0x80000000, 5, 4},
		{0x7fffffff, 0x7fffffff, 0x7fffffff, 0x7fffffff},
		{0x80000000, 0, 0x7fffffff, 8},
		{16, 8, 4, 4},
		// Over pixels the screen still holds, so the surface it is made from
		// is not black until the paint fills it.
		{10, 6, 0, 0xfffffffd},
	} {
		card := newTestCard(t, client, class)
		mustJavaCall(t, client, cardAtKey, card, box[0], box[1], box[2], box[3])
		paintCard(t, client, card)
		checkScreen(t, client, shown, "a card wholly off the screen")
		surface := client.framebuffer(client.javaRuntimeState().cardSurface)
		if surface == nil {
			t.Fatalf("the card at %#x was given no surface to paint into", box)
		}
		if box[0] == 10 && surface.pixels[0] != 0 {
			t.Errorf("the paint of the card with no height left its surface at %#x", surface.pixels[0])
		}
	}
}

// Drawing through the Graphics a title kept, between two paints, lands on the
// card's surface, from the card's corner, and is shown at the next paint.
func TestJavaCardShowsWhatWasDrawnBetweenPaints(t *testing.T) {
	client := fixtureClient(t)
	card := newTestCard(t, client, installPaintingCardClass(t, client, "game/Board", 1, 1))
	before := patternScreen(t, client)
	mustJavaCall(t, client, cardAtKey, card, 3, 2, 5, 4)
	paintCard(t, client, card)
	graphics := client.javaRuntimeState().screenGraphics
	mustJavaCall(t, client, javaGraphicsClass+".setColor(I)V", graphics, 0xffffff)
	mustJavaCall(t, client, javaGraphicsClass+".fillRect(IIII)V", graphics, 0, 0, 1, 1)
	if client.screen.pixels[2*16+3] != before[2*16+3] {
		t.Error("a draw between paints reached the screen before the next paint")
	}
	paintCard(t, client, card)
	want := filled(before, 16, 4, 3, 8, 6)
	want[2*16+3] = 0xffff
	checkScreen(t, client, want, "the paint after a draw between paints")
}

// A card without a box paints on the screen itself, as every card did before
// boxes, and so does a card whose box is the screen: the fill from (1, 1)
// covers the screen from there, no surface of its own is made, and the
// Graphics the card painted through draws on the screen.
func TestJavaCardWithoutABoxPaintsTheScreen(t *testing.T) {
	for _, build := range []struct {
		name      string
		key       string
		arguments []uint32
	}{
		{"Card()", cardPlainKey, nil},
		{"Card(0, 0, 16, 8)", cardAtKey, []uint32{0, 0, 16, 8}},
	} {
		t.Run(build.name, func(t *testing.T) {
			client := fixtureClient(t)
			card := newTestCard(t, client, installPaintingCardClass(t, client, "game/Board", 1, 1))
			before := patternScreen(t, client)
			mustJavaCall(t, client, build.key, append([]uint32{card}, build.arguments...)...)
			paintCard(t, client, card)
			checkScreen(t, client, filled(before, 16, 1, 1, 16, 8), build.name)
			runtime := client.javaRuntimeState()
			if runtime.cardSurface != 0 {
				t.Errorf("a card that is the whole screen was given the surface %#x", runtime.cardSurface)
			}
			graphics, err := client.javaGraphicsState(runtime.screenGraphics)
			if err != nil {
				t.Fatal(err)
			}
			if graphics.surface != client.screen.handle {
				t.Errorf("the card painted through a Graphics on %#x, want the screen", graphics.surface)
			}
		})
	}
}

// When a card that is the whole screen is shown after a card with a box, its
// paint is on the screen again and the surface the other card painted into is
// given back; a card with a box shown after another starts from the screen
// rather than from the other card's surface.
func TestJavaCardSurfaceFollowsTheShownCard(t *testing.T) {
	client := fixtureClient(t)
	class := installPaintingCardClass(t, client, "game/Board", 1, 1)
	boxed, other, plain := newTestCard(t, client, class), newTestCard(t, client, class), newTestCard(t, client, class)
	before := patternScreen(t, client)
	mustJavaCall(t, client, cardAtKey, boxed, 3, 2, 5, 4)
	mustJavaCall(t, client, cardAtKey, other, 3, 2, 5, 4)
	mustJavaCall(t, client, cardPlainKey, plain)
	paintCard(t, client, boxed)
	runtime := client.javaRuntimeState()
	first := runtime.cardSurface
	client.screen.pixels[2*16+3] = 0x1111
	paintCard(t, client, other)
	if runtime.cardSurface == first || client.framebuffer(first) != nil || runtime.cardSurfaceOwner != other {
		t.Errorf("the second card with a box painted into %#x (the first had %#x)", runtime.cardSurface, first)
	}
	if got := client.screen.pixels[2*16+3]; got != 0x1111 {
		t.Errorf("the second card's corner shows %#x, want what the screen held there", got)
	}
	paintCard(t, client, plain)
	if runtime.cardSurface != 0 || runtime.cardSurfaceOwner != 0 {
		t.Errorf("the surface %#x stayed after a card that is the whole screen painted", runtime.cardSurface)
	}
	checkScreen(t, client, filled(before, 16, 1, 1, 16, 8), "the whole-screen card after the others")
}

// The box goes into the protected fields the module was told x, y, w, h and
// bTrans sit in — numbered here in the order one local title asks for them,
// y before x — when a positioned constructor, move or resize sets it. A card
// built without a position publishes nothing, as before.
func TestJavaCardPublishesItsBoxWhereTheModuleReadsIt(t *testing.T) {
	client := fixtureClient(t)
	fields := []javaMemberRef{
		{Name: "y", Descriptor: "I"}, {Name: "x", Descriptor: "I"}, {Name: "w", Descriptor: "I"},
		{Name: "h", Descriptor: "I"}, {Name: "bTrans", Descriptor: "Z"},
	}
	surface := &javaSurface{
		Fields:  fields,
		Classes: []javaAPIClass{{Name: javaCardClass, Fields: javaRun{Start: 0, Count: uint32(len(fields))}}},
	}
	layout := newJavaLayout()
	if _, err := layout.layoutPlatformFields(surface); err != nil {
		t.Fatal(err)
	}
	client.javaLink = &javaLink{surface: surface, layout: layout}
	words := func(card uint32) map[string]int32 {
		t.Helper()
		block, err := client.readWord(card + 8)
		if err != nil {
			t.Fatal(err)
		}
		answer := map[string]int32{}
		for _, field := range fields {
			word, ok := layout.platformFieldWord(javaCardClass, field.Name, field.Descriptor)
			if !ok {
				t.Fatalf("the layout did not number Card.%s", field.Name)
			}
			value, err := client.readWord(block + word*4)
			if err != nil {
				t.Fatal(err)
			}
			answer[field.Name] = int32(value)
		}
		return answer
	}
	if word, _ := layout.platformFieldWord(javaCardClass, "y", "I"); word != 0 {
		t.Fatalf("Card.y is word %d, want the first", word)
	}
	display := mustJavaCall(t, client, cardDisplayKey)
	card := installGuestSubclass(t, client, "game/Board", javaCardClass, 8, 0)
	mustJavaCall(t, client, cardPlainKey, card)
	if got := words(card); got["x"] != 0 || got["y"] != 0 || got["w"] != 0 || got["h"] != 0 || got["bTrans"] != 0 {
		t.Errorf("Card() published %v, want the words left as they were", got)
	}
	mustJavaCall(t, client, cardOnAtFlagKey, card, display, 3, 2, 5, 4, 1)
	if got, want := words(card), map[string]int32{"x": 3, "y": 2, "w": 5, "h": 4, "bTrans": 1}; !mapsEqual(got, want) {
		t.Errorf("Card(Display, 3, 2, 5, 4, true) published %v, want %v", got, want)
	}
	mustJavaCall(t, client, cardMoveKey, card, 0xfffffff9, 9)
	mustJavaCall(t, client, cardResizeKey, card, 0, 0xffffffff)
	if got, want := words(card), map[string]int32{"x": -7, "y": 9, "w": 0, "h": -1, "bTrans": 1}; !mapsEqual(got, want) {
		t.Errorf("after a move and a resize the fields hold %v, want %v", got, want)
	}
	other := installGuestSubclass(t, client, "game/Popup", javaCardClass, 8, 64)
	mustJavaCall(t, client, cardFlagKey, other, 1)
	if got, want := words(other), map[string]int32{"x": 0, "y": 0, "w": 0, "h": 0, "bTrans": 1}; !mapsEqual(got, want) {
		t.Errorf("Card(true) published %v, want only the flag", got)
	}
}

func mapsEqual(got, want map[string]int32) bool {
	if len(got) != len(want) {
		return false
	}
	for key, value := range want {
		if got[key] != value {
			return false
		}
	}
	return true
}

// isShown answers whether a card is the one the display shows: not before it
// is pushed, not once another card replaces it or it is removed, and never for
// null.
func TestJavaCardIsShownWhileTheDisplayShowsIt(t *testing.T) {
	client := fixtureClient(t)
	class, err := client.preparePlatformJavaClass(javaCardClass)
	if err != nil {
		t.Fatal(err)
	}
	first, err := client.allocateJavaObject(class)
	if err != nil {
		t.Fatal(err)
	}
	second, err := client.allocateJavaObject(class)
	if err != nil {
		t.Fatal(err)
	}
	display := mustJavaCall(t, client, cardDisplayKey)
	shown := func(card uint32) bool {
		t.Helper()
		return mustJavaCall(t, client, cardShownKey, card) == javaTrue
	}
	if shown(first) || shown(0) {
		t.Error("a card is shown before anything was pushed")
	}
	mustJavaCall(t, client, cardPushKey, display, first)
	if !shown(first) || shown(second) {
		t.Error("isShown does not follow the pushed card")
	}
	mustJavaCall(t, client, cardPushKey, display, second)
	if shown(first) || !shown(second) {
		t.Error("isShown does not follow the card that replaced the first")
	}
	mustJavaCall(t, client, javaDisplayClass+".removeCard(Lorg/kwis/msp/lcdui/Card;)Z", display, second)
	if shown(second) || shown(0) {
		t.Error("a removed card is still shown")
	}
}

// A constructor is a new object, and what was kept for an earlier card at the
// same address is not its: Card() forgets an earlier box and the surface's
// owner, and a box is not kept for a card the collector has reclaimed.
func TestJavaCardConstructorForgetsAnEarlierCardAtItsAddress(t *testing.T) {
	client := fixtureClient(t)
	class, err := client.preparePlatformJavaClass(javaCardClass)
	if err != nil {
		t.Fatal(err)
	}
	card, err := client.allocateJavaObject(class)
	if err != nil {
		t.Fatal(err)
	}
	runtime := client.javaRuntimeState()
	mustJavaCall(t, client, cardAtKey, card, 3, 2, 5, 4)
	runtime.cardSurfaceOwner = card
	mustJavaCall(t, client, cardPlainKey, card)
	if _, positioned := runtime.cardBounds[card]; positioned || runtime.cardSurfaceOwner != 0 {
		t.Error("Card() kept the box or the surface of an earlier card at its address")
	}
	if got := int32(mustJavaCall(t, client, javaCardClass+".getWidth()I", card)); got != 16 {
		t.Errorf("Card() over an earlier card's address is %d wide, want the screen", got)
	}

	gone, err := client.allocateJavaObject(class)
	if err != nil {
		t.Fatal(err)
	}
	mustJavaCall(t, client, cardAtKey, gone, 1, 1, 2, 2)
	delete(runtime.objects, gone)
	mustJavaCall(t, client, cardAtKey, card, 3, 2, 5, 4)
	if _, kept := runtime.cardBounds[gone]; kept {
		t.Error("the box of a reclaimed card is still kept")
	}
}

// move and resize on null are the NullPointerException SKT's Card throws, not
// a box kept for no card.
func TestJavaCardMoveOfNullThrows(t *testing.T) {
	client := fixtureClient(t)
	for _, key := range []string{cardMoveKey, cardResizeKey} {
		if _, err := javaCall(t, client, key, 0, 1, 1); thrownClass(err) != javaThrowNullClass {
			t.Errorf("%s on null = %v, want NullPointerException", key, err)
		}
	}
	if len(client.javaRuntimeState().cardBounds) != 0 {
		t.Error("a box was kept for null")
	}
}
