package lgt

import (
	"context"
	"testing"
	"unicode/utf16"

	"github.com/movingwoo/wfeature/internal/armcore"
)

// drawFixtureContext answers a context written into guest memory over a
// surface of its own, the shape polygonFixtureContext gives, at a size a line
// of text fits in.
func drawFixtureContext(t *testing.T, client *Client, width, height int, colour uint16) (*graphicsContext, uint32) {
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
	target, err := client.newFramebuffer(width, height, false)
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

func allocateUnicode(t *testing.T, client *Client, text string, terminated bool) uint32 {
	t.Helper()
	units := utf16.Encode([]rune(text))
	if terminated {
		units = append(units, 0)
	}
	data := make([]byte, 2*len(units))
	for index, unit := range units {
		data[2*index], data[2*index+1] = byte(unit), byte(unit>>8)
	}
	pointer, err := client.allocateBytes(data)
	if err != nil {
		t.Fatal(err)
	}
	return pointer
}

// drawUnicodeString draws what drawString draws for the same text, and
// nothing for a length of zero or less, as the specification has it.
func TestDrawUnicodeStringDrawsWhatDrawStringDraws(t *testing.T) {
	client := fixtureClient(t)
	const colour = uint16(0x001f)
	narrow, narrowContext := drawFixtureContext(t, client, 40, 20, colour)
	wide, wideContext := drawFixtureContext(t, client, 40, 20, colour)
	empty, emptyContext := drawFixtureContext(t, client, 40, 20, colour)

	text, err := client.allocateBytes(append(encodeEUCKR("A가"), 0))
	if err != nil {
		t.Fatal(err)
	}
	units := allocateUnicode(t, client, "A가", false)
	callDrawSlot(t, client, slotDrawString, narrow.target.handle, 1, 2, text, 0xffffffff, narrowContext)
	callDrawSlot(t, client, slotDrawUnicodeString, wide.target.handle, 1, 2, units, 2, wideContext)
	callDrawSlot(t, client, slotDrawUnicodeString, empty.target.handle, 1, 2, units, 0, emptyContext)

	drawn := 0
	for index, pixel := range narrow.target.pixels {
		if wide.target.pixels[index] != pixel {
			t.Fatalf("pixel %d is %#x from drawUnicodeString and %#x from drawString", index, wide.target.pixels[index], pixel)
		}
		if pixel == colour {
			drawn++
		}
		if empty.target.pixels[index] != 0 {
			t.Fatalf("a length of 0 drew pixel %d", index)
		}
	}
	if drawn == 0 {
		t.Fatal("the text drew nothing to compare")
	}
}

// getUnicodeStringWidth measures what drawUnicodeString draws, counted or up
// to the terminating zero.
func TestGetUnicodeStringWidthMeasuresTheText(t *testing.T) {
	client := fixtureClient(t)
	want := uint32(textWidth("A가나"))
	counted := allocateUnicode(t, client, "A가나다", false)
	if got := callSlot(t, client, slotGetUnicodeStringWidth, 0, counted, 3); got != want {
		t.Errorf("width of 3 units = %d, want %d", got, want)
	}
	terminated := allocateUnicode(t, client, "A가나", true)
	if got := callSlot(t, client, slotGetUnicodeStringWidth, 0, terminated, 0xffffffff); got != want {
		t.Errorf("width of a terminated string = %d, want %d", got, want)
	}
	if got := callSlot(t, client, slotGetUnicodeStringWidth, 0, counted, 0); got != 0 {
		t.Errorf("width of no units = %d, want 0", got)
	}
}

// drawImageRegion turns and mirrors the rectangle it copies, and draws nothing
// for a rectangle that reaches outside its source.
func TestDrawImageRegionTransformsTheRegion(t *testing.T) {
	// A 3 by 2 source, each pixel its own value:
	//
	//	1 2 3
	//	4 5 6
	source := [][]uint16{{1, 2, 3}, {4, 5, 6}}
	for _, probe := range []struct {
		name      string
		transform int32
		want      [][]uint16
	}{
		{"none", imageTransformNone, [][]uint16{{1, 2, 3}, {4, 5, 6}}},
		{"rot90", imageTransformRot90, [][]uint16{{4, 1}, {5, 2}, {6, 3}}},
		{"rot180", imageTransformRot180, [][]uint16{{6, 5, 4}, {3, 2, 1}}},
		{"rot270", imageTransformRot270, [][]uint16{{3, 6}, {2, 5}, {1, 4}}},
		{"mirror", imageTransformMirror, [][]uint16{{3, 2, 1}, {6, 5, 4}}},
		{"mirror rot90", imageTransformMirRot90, [][]uint16{{6, 3}, {5, 2}, {4, 1}}},
		{"mirror rot180", imageTransformMirRot180, [][]uint16{{4, 5, 6}, {1, 2, 3}}},
		{"mirror rot270", imageTransformMirRot270, [][]uint16{{1, 4}, {2, 5}, {3, 6}}},
	} {
		t.Run(probe.name, func(t *testing.T) {
			client := fixtureClient(t)
			target, pointer := drawFixtureContext(t, client, 6, 6, 0)
			image := regionFixtureSource(t, client, source)
			callDrawSlot(t, client, slotDrawImageRegion, target.target.handle, 1, 1, 3, 2, image.handle, 0, 0,
				uint32(probe.transform), pointer)
			for y := 0; y < 6; y++ {
				for x := 0; x < 6; x++ {
					want := uint16(0)
					if row, column := y-1, x-1; row >= 0 && row < len(probe.want) && column >= 0 && column < len(probe.want[row]) {
						want = probe.want[row][column]
					}
					if got := target.target.pixels[y*6+x]; got != want {
						t.Fatalf("pixel (%d, %d) = %d, want %d", x, y, got, want)
					}
				}
			}
		})
	}

	client := fixtureClient(t)
	target, pointer := drawFixtureContext(t, client, 6, 6, 0)
	image := regionFixtureSource(t, client, source)
	// One column past the source's right edge: nothing, where drawImage
	// would draw the part inside.
	callDrawSlot(t, client, slotDrawImageRegion, target.target.handle, 0, 0, 3, 2, image.handle, 1, 0, 0, pointer)
	for index, pixel := range target.target.pixels {
		if pixel != 0 {
			t.Fatalf("a region outside its source drew pixel %d", index)
		}
	}

	thread := armcore.NewThread(armcore.NewContext())
	stack, err := client.allocateBytes(make([]byte, 64))
	if err != nil {
		t.Fatal(err)
	}
	arguments := []uint32{target.target.handle, 0, 0, 3, 2, image.handle, 0, 0, 45, pointer}
	if err := thread.SetRegister(armcore.RegisterSP, stack); err != nil {
		t.Fatal(err)
	}
	for index, value := range arguments {
		if index < 4 {
			if err := thread.SetRegister(index, value); err != nil {
				t.Fatal(err)
			}
		} else if err := client.writeWord(stack+uint32(index-4)*4, value); err != nil {
			t.Fatal(err)
		}
	}
	if err := client.handleWIPICSVC(context.Background(), thread, slotDrawImageRegion); err == nil {
		t.Fatal("a transform the specification does not name was drawn")
	}
}

func regionFixtureSource(t *testing.T, client *Client, rows [][]uint16) *framebuffer {
	t.Helper()
	image, err := client.newFramebuffer(len(rows[0]), len(rows), false)
	if err != nil {
		t.Fatal(err)
	}
	for y, row := range rows {
		copy(image.pixels[y*image.width:], row)
	}
	if err := client.syncToGuest(image); err != nil {
		t.Fatal(err)
	}
	return image
}

// The network calls past the ones titles make answer M_E_ERROR, as KTF's
// table does, instead of stopping the session.
func TestNetworkCallsWithNoNetworkAnswerAnError(t *testing.T) {
	client := fixtureClient(t)
	for _, slot := range []uint32{
		slotNetSocketBind, slotNetGetMaxPacketLength, slotNetSocketSendTo, slotNetSocketRcvFrom,
		slotNetGetHostAddr, slotNetSocketAccept, slotNetHttpOpen, slotNetHttpConnect,
		slotNetHttpSetRequestMethod, slotNetHttpGetRequestMethod, slotNetHttpSetRequestProperty,
		slotNetHttpGetRequestProperty, slotNetHttpSetProxy, slotNetHttpGetProxy,
		slotNetHttpGetResponseCode, slotNetHttpGetResponseMessage, slotNetHttpGetHeaderField,
		slotNetHttpGetLength, slotNetHttpGetType, slotNetHttpGetEncoding, slotNetHttpClose,
	} {
		if !knownWIPICSlot(slot) || wipicSlotNames[slot] == "" {
			t.Errorf("slot %#x is not recorded as served and named", slot)
		}
		if got := int32(callSlot(t, client, slot, 0, 0, 0, 0)); got != wipiError {
			t.Errorf("slot %#x (%s) answered %d, want M_E_ERROR", slot, wipicSlotNames[slot], got)
		}
	}
}
