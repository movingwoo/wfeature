package lgt

import (
	"context"
	"testing"

	"github.com/movingwoo/wfeature/internal/armcore"
)

// A polygon arrives as two parallel coordinate arrays, a count, and the
// context the colour comes out of — the shape MC_grpFillPolygon has. The
// filled form is what one title's tutorial calls; the outline is its
// neighbour in the block and shares the reader.
func TestFillPolygonFillsTheTriangleItNames(t *testing.T) {
	client := fixtureClient(t)

	const colour = uint16(0x07e0)
	context, pointer := polygonFixtureContext(t, client, colour)

	// A right triangle with its corner at the origin: (0,0), (3,0), (0,3).
	xs, err := client.allocateWords([]uint32{0, 3, 0})
	if err != nil {
		t.Fatal(err)
	}
	ys, err := client.allocateWords([]uint32{0, 0, 3})
	if err != nil {
		t.Fatal(err)
	}

	callDrawSlot(t, client, slotFillPolygon, context.target.handle, xs, ys, 3, pointer)

	// Row y covers x from 0 to 3-y, which is the even-odd rule on this shape.
	// The bottom row is not one of them: a scanline edge is half-open at its
	// lower vertex, here as in the KTF runtime's polygon, so y = 3 has no
	// crossings at all.
	for y := 0; y < 3; y++ {
		for x := 0; x < 5; x++ {
			want := uint16(0)
			if x <= 3-y {
				want = colour
			}
			if got := context.target.pixels[y*context.target.width+x]; got != want {
				t.Fatalf("pixel (%d, %d) = %#x, want %#x", x, y, got, want)
			}
		}
	}
}

// The outline form draws the edges and leaves the middle alone, which is the
// difference worth having a test for: both slots take the same arguments and
// only the fill flag tells them apart.
func TestDrawPolygonLeavesTheInsideAlone(t *testing.T) {
	client := fixtureClient(t)

	const colour = uint16(0xf800)
	context, pointer := polygonFixtureContext(t, client, colour)

	xs, err := client.allocateWords([]uint32{0, 4, 4, 0})
	if err != nil {
		t.Fatal(err)
	}
	ys, err := client.allocateWords([]uint32{0, 0, 4, 4})
	if err != nil {
		t.Fatal(err)
	}

	callDrawSlot(t, client, slotDrawPolygon, context.target.handle, xs, ys, 4, pointer)

	if got := context.target.pixels[2*context.target.width+2]; got != 0 {
		t.Fatalf("the middle pixel is %#x, want it untouched", got)
	}
	if got := context.target.pixels[0]; got != colour {
		t.Fatalf("the corner pixel is %#x, want the outline colour %#x", got, colour)
	}
}

// A count the game computed wrongly has to cost a refused call rather than a
// read of the whole address space, because the arrays behind it are the
// game's own memory.
func TestPolygonRefusesAnAbsurdPointCount(t *testing.T) {
	client := fixtureClient(t)
	if _, _, err := client.readPolygonPoints(0, 0, maxPolygonPoints+1); err == nil {
		t.Fatal("readPolygonPoints accepted a count past the bound")
	}
}

// polygonFixtureContext answers a screen-targeting context written into guest
// memory, since a draw slot reads the context out of the game's own memory
// rather than taking one.
func polygonFixtureContext(t *testing.T, client *Client, colour uint16) (*graphicsContext, uint32) {
	t.Helper()
	pointer, err := client.allocateBytes(make([]byte, grpContextSize))
	if err != nil {
		t.Fatal(err)
	}
	if code := client.initContext(pointer); code != wipiSuccess {
		t.Fatalf("initContext answered %d", code)
	}
	if err := client.writeWord(pointer+grpContextForeground, uint32(colour)); err != nil {
		t.Fatal(err)
	}
	// The screen carries no handle until the guest asks for it, and a draw
	// slot names its destination by handle, so the target here is a surface of
	// its own — which is what an off-screen draw is anyway.
	target, err := client.newFramebuffer(8, 8, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := client.syncToGuest(target); err != nil {
		t.Fatal(err)
	}
	drawContext, err := client.contextFor(context.Background(), client.thread, target, pointer)
	if err != nil {
		t.Fatal(err)
	}
	return drawContext, pointer
}

// callDrawSlot calls a draw slot whose fifth argument is on the stack, which
// is where the ARM procedure call standard puts it.
func callDrawSlot(t *testing.T, client *Client, slot uint32, arguments ...uint32) {
	t.Helper()
	thread := armcore.NewThread(armcore.NewContext())
	stack, err := client.allocateBytes(make([]byte, 64))
	if err != nil {
		t.Fatal(err)
	}
	if err := thread.SetRegister(armcore.RegisterSP, stack); err != nil {
		t.Fatal(err)
	}
	for index, value := range arguments {
		if index < 4 {
			if err := thread.SetRegister(index, value); err != nil {
				t.Fatal(err)
			}
			continue
		}
		if err := client.writeWord(stack+uint32(index-4)*4, value); err != nil {
			t.Fatal(err)
		}
	}
	if err := client.handleWIPICSVC(context.Background(), thread, slot); err != nil {
		t.Fatalf("slot %#x: %v", slot, err)
	}
	code, err := thread.Register(0)
	if err != nil {
		t.Fatal(err)
	}
	if int32(code) != wipiSuccess {
		t.Fatalf("slot %#x answered %d", slot, int32(code))
	}
}
