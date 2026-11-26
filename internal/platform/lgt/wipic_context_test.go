package lgt

import "testing"

func TestSettingAlphaInstallsThePixelBlend(t *testing.T) {
	const existing, incoming uint16 = 0x07ff, 0xf800
	for _, layout := range []string{"compact", "wide"} {
		t.Run(layout, func(t *testing.T) {
			client := fixtureClient(t)
			client.wideGraphicsContexts = layout == "wide"
			pointer := writeGuest(t, client, make([]byte, wideContextSize))
			answer := writeGuest(t, client, make([]byte, 4))
			if code := client.initContext(pointer); code != wipiSuccess {
				t.Fatal(code)
			}
			target, err := client.newFramebuffer(2, 2, false)
			if err != nil {
				t.Fatal(err)
			}
			set := func(field, value uint32) {
				t.Helper()
				if code := client.transferContextFieldFor(t, pointer, field, value); code != wipiSuccess {
					t.Fatalf("set field %d = %#x: %d", field, value, code)
				}
			}
			get := func(field uint32) uint32 {
				t.Helper()
				if code := int32(callSlot(t, client, slotGetContext, pointer, field, answer)); code != wipiSuccess {
					t.Fatalf("get field %d: %d", field, code)
				}
				value, err := client.readWord(answer)
				if err != nil {
					t.Fatal(err)
				}
				return value
			}
			fill := func(want uint16) {
				t.Helper()
				target.pixels[0] = existing
				if err := client.syncToGuest(target); err != nil {
					t.Fatal(err)
				}
				callDrawSlot(t, client, slotFillRect, target.handle, 0, 0, 1, 1, pointer)
				if got := target.pixels[0]; got != want {
					t.Fatalf("filled pixel = %#04x, want %#04x", got, want)
				}
			}
			set(grpFieldForeground, uint32(incoming))
			for _, sample := range []struct {
				alpha uint32
				pixel uint16
			}{{0, existing}, {100, 0x64d3}, {120, 0x7c30}, {255, incoming}} {
				set(grpFieldAlpha, sample.alpha)
				fill(sample.pixel)
				if get(grpFieldAlpha) != sample.alpha || get(grpFieldParam1) != sample.alpha {
					t.Fatalf("alpha and parameter must both read back as %d", sample.alpha)
				}
			}

			blend := get(grpFieldPixelOp)
			if blend != 0x330fffff {
				t.Fatalf("blend address = %#x, want the value stored in quick saves, 0x330fffff", blend)
			}
			set(grpFieldParam1, 100)
			fill(0x64d3)
			guest := installThumb(t, client, 0x1c10, 0x4770) // adds r0,r2,#0; bx lr
			set(grpFieldPixelOp, guest)
			fill(100)
			set(grpFieldAlpha, 120)
			fill(0x7c30)
			set(grpFieldPixelOp, 0)
			fill(incoming)
			set(grpFieldPixelOp, blend)
			fill(0x7c30)
			if client.installedPixelOps[blend] {
				t.Fatal("the blend occupied a guest callback slot")
			}
		})
	}
}

// The clip is handed over as an array of four M_Int32 — the corner inside the
// rectangle and the corner past it — not as two packed words. A title that
// sets one and a title that reads one back have to see the same four numbers,
// and the fill below is what says the rectangle the platform kept is the one
// that was asked for.
func TestClipIsFourNumbersAndTheFarCornerIsOutside(t *testing.T) {
	client := fixtureClient(t)
	pointer, _ := pixelOpFixture(t, client, 0, 0xf81f)
	target, err := client.newFramebuffer(8, 8, false)
	if err != nil {
		t.Fatal(err)
	}

	array, err := client.allocateBytes(make([]byte, 16))
	if err != nil {
		t.Fatal(err)
	}
	for index, value := range []uint32{2, 1, 5, 3} {
		if err := client.writeWord(array+uint32(index)*4, value); err != nil {
			t.Fatal(err)
		}
	}
	if code := int32(callSlot(t, client, slotSetContext, pointer, grpFieldClip, array)); code != wipiSuccess {
		t.Fatalf("setContext(clip) answered %d", code)
	}

	// Read it back into a different array, the way a title that decides what
	// to draw from its own clip does.
	answer, err := client.allocateBytes(make([]byte, 16))
	if err != nil {
		t.Fatal(err)
	}
	if code := int32(callSlot(t, client, slotGetContext, pointer, grpFieldClip, answer)); code != wipiSuccess {
		t.Fatalf("getContext(clip) answered %d", code)
	}
	for index, want := range []uint32{2, 1, 5, 3} {
		got, err := client.readWord(answer + uint32(index)*4)
		if err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Fatalf("clip[%d] = %d, want %d", index, got, want)
		}
	}

	// A fill of the whole surface leaves exactly the clip painted: x in 2..4
	// and y in 1..2, because the far corner is outside.
	callDrawSlot(t, client, slotFillRect, target.handle, 0, 0, 8, 8, pointer)
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			inside := x >= 2 && x < 5 && y >= 1 && y < 3
			pixel := target.pixels[y*8+x]
			if inside && pixel != 0xf81f {
				t.Fatalf("pixel (%d,%d) = %#x, want the fill inside the clip", x, y, pixel)
			}
			if !inside && pixel == 0xf81f {
				t.Fatalf("pixel (%d,%d) was painted outside the clip", x, y)
			}
		}
	}
}

// MC_grpGetRGBFromPixel answers the pixel it was handed and puts the three
// channels behind the pointers it was given.
func TestGetRGBFromPixelWritesItsChannels(t *testing.T) {
	client := fixtureClient(t)
	channels, err := client.allocateBytes(make([]byte, 12))
	if err != nil {
		t.Fatal(err)
	}
	pixel := uint32(rgb565(0xff, 0x80, 0x00))
	if answer := callSlot(t, client, slotGetRGBFromPixel,
		pixel, channels, channels+4, channels+8); answer != pixel {
		t.Fatalf("answer = %#x, want the pixel %#x", answer, pixel)
	}
	red, green, blue := unpack565(uint16(pixel))
	for index, want := range []uint32{red, green, blue} {
		got, err := client.readWord(channels + uint32(index)*4)
		if err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Fatalf("channel %d = %d, want %d", index, got, want)
		}
	}
}
