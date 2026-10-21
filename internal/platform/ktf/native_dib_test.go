package ktf

import (
	"encoding/binary"
	"image/color"
	"testing"
)

// newLaterNativePlatform is a platform running a module of the later
// generation, the one that asks which AEE it runs on.
func newLaterNativePlatform(t *testing.T) *NativePlatform {
	t.Helper()
	platform := newTestNativePlatform(t, nil)
	platform.archive.Module = nativeGateModule()
	return platform
}

// nativeImageOf hands a bitmap to the factory and answers the object.
func nativeImageOf(t *testing.T, platform *NativePlatform, bitmap []byte) uint32 {
	t.Helper()
	address, err := platform.client.Allocate(uint32(len(bitmap)))
	if err != nil {
		t.Fatal(err)
	}
	if err := platform.client.core.Memory().Write(address, bitmap); err != nil {
		t.Fatal(err)
	}
	object := nativeCall(t, platform.createObject, nativeClassImage, address)
	if object == 0 {
		t.Fatal("the factory refused a bitmap")
	}
	return object
}

// nativeBlitCall blits a region of an object to the screen the way a title
// calls the display: the destination and width in registers, the rest stacked.
func nativeBlitCall(t *testing.T, platform *NativePlatform, object uint32, x, y, width, height, sourceX, sourceY, mode int32) uint32 {
	t.Helper()
	return nativeStackedCall(t, platform, platform.blit, []uint32{0, uint32(x), uint32(y), uint32(width)},
		uint32(height), object, uint32(sourceX), uint32(sourceY), uint32(mode))
}

// TestNativeLaterImageIsADIB covers the layout a later title reads its images
// through: BREW's IDIB, a header and the picture's pixels in one block, as
// 5-6-5 rows from the top. One title takes the bytes per pixel from the
// pointer at +8 when it starts and divides by them later, and read through the
// 2005 module's object it measured nothing.
func TestNativeLaterImageIsADIB(t *testing.T) {
	platform := newLaterNativePlatform(t)
	palette := []color.RGBA{
		{R: 0xff, G: 0x00, B: 0xff, A: 0xff},
		{R: 0x10, G: 0x20, B: 0x30, A: 0xff},
		{R: 0xf8, G: 0xfc, B: 0xf8, A: 0xff},
	}
	// Bottom-up rows of three pixels, each padded to four bytes.
	object := nativeImageOf(t, platform, buildBitmap(3, 2, palette, []byte{1, 2, 0, 0, 0, 1, 2, 0}))
	holder, err := platform.client.InterfaceObject(nativeClassDIB)
	if err != nil {
		t.Fatal(err)
	}
	table, err := platform.client.ReadWord(holder)
	if err != nil {
		t.Fatal(err)
	}
	header := nativeRead(t, platform, object, nativeDIBHeaderSize)
	for _, field := range []struct {
		name      string
		got, want uint32
	}{
		{name: "table", got: binary.LittleEndian.Uint32(header), want: table},
		{name: "pixels", got: binary.LittleEndian.Uint32(header[0x08:]), want: object + nativeDIBHeaderSize},
		{name: "transparent", got: binary.LittleEndian.Uint32(header[0x10:]), want: nativeDIBTransparent},
		{name: "width", got: uint32(binary.LittleEndian.Uint16(header[0x14:])), want: 3},
		{name: "height", got: uint32(binary.LittleEndian.Uint16(header[0x16:])), want: 2},
		{name: "pitch", got: uint32(binary.LittleEndian.Uint16(header[0x18:])), want: 6},
		{name: "palette size", got: uint32(binary.LittleEndian.Uint16(header[0x1a:])), want: 0},
		{name: "depth", got: uint32(header[0x1c]), want: nativeDIBDepth},
		{name: "scheme", got: uint32(header[0x1d]), want: nativeDIBScheme565},
	} {
		if field.got != field.want {
			t.Errorf("%s = %#x, want %#x", field.name, field.got, field.want)
		}
	}
	pixels := nativeRead(t, platform, object+nativeDIBHeaderSize, 12)
	for index, entry := range []int{0, 1, 2, 1, 2, 0} {
		if got, want := binary.LittleEndian.Uint16(pixels[2*index:]), encode565(palette[entry]); got != want {
			t.Errorf("pixel %d = %#04x, want %#04x", index, got, want)
		}
	}
	if _, ok := platform.dibAt(object); !ok {
		t.Error("the blit does not read the factory's own image as an IDIB")
	}

	// The 2005 module's object is the bitmap's address and one more word.
	earlier := newTestNativePlatform(t, nil)
	if _, ok := earlier.dibAt(nativeImageOf(t, earlier, buildBitmap(3, 2, palette, []byte{1, 2, 0, 0, 0, 1, 2, 0}))); ok {
		t.Error("the 2005 module's image reads as an IDIB")
	}
}

// TestNativeBlitDrawsWhatATitleDrewIntoADIB covers the blit reading an IDIB's
// pixels out of guest memory. An image nobody drew into draws as its decoded
// picture, its transparent colour left out. A pixel the title wrote draws as
// the 5-6-5 it wrote. And an IDIB the title built itself from an image's
// header draws too, its transparent value left out only by a transparent copy.
func TestNativeBlitDrawsWhatATitleDrewIntoADIB(t *testing.T) {
	platform := newLaterNativePlatform(t)
	solid := color.RGBA{R: 0x13, G: 0x57, B: 0x9b, A: 0xff}
	other := color.RGBA{R: 0x40, G: 0x50, B: 0x60, A: 0xff}
	palette := []color.RGBA{nativeTransparent, solid, other}
	// The top row is transparent then solid, the bottom row other then solid.
	object := nativeImageOf(t, platform, buildBitmap(2, 2, palette, []byte{2, 1, 0, 0, 0, 1, 0, 0}))
	frame := platform.screen.frame
	under := color.RGBA{R: 1, G: 2, B: 3, A: 0xff}
	for y := 0; y < 60; y++ {
		for x := 0; x < 60; x++ {
			frame.SetRGBA(x, y, under)
		}
	}

	if got := nativeBlitCall(t, platform, object, 10, 10, 2, 2, 0, 0, nativeRasterTransparent); got != 1 {
		t.Fatalf("the blit answered %d, want 1", got)
	}
	for _, pixel := range []struct {
		x, y int
		want color.RGBA
	}{
		{10, 10, under}, {11, 10, solid}, {10, 11, other}, {11, 11, solid},
	} {
		// The picture's own colours, not their 5-6-5 rounding: an image
		// nobody drew into looks the way it always did.
		if got := frame.RGBAAt(pixel.x, pixel.y); got != pixel.want {
			t.Errorf("an untouched image drew %v at %d,%d, want %v", got, pixel.x, pixel.y, pixel.want)
		}
	}

	// The title draws into the image's pixels; the blit draws what it wrote.
	memory := platform.client.core.Memory()
	if err := memory.Write(object+nativeDIBHeaderSize, []byte{0xe0, 0x07}); err != nil {
		t.Fatal(err)
	}
	nativeBlitCall(t, platform, object, 20, 20, 2, 2, 0, 0, nativeRasterTransparent)
	if got, want := frame.RGBAAt(20, 20), decode565(0x07e0); got != want {
		t.Errorf("a pixel the title wrote drew %v, want %v", got, want)
	}
	if got := frame.RGBAAt(21, 20); got != solid {
		t.Errorf("the pixel beside it drew %v, want %v", got, solid)
	}

	// A picture of the title's own: the image's header copied in front of
	// pixels it filled itself.
	header := nativeRead(t, platform, object, nativeDIBHeaderSize)
	block, err := platform.client.Allocate(nativeDIBHeaderSize + 8)
	if err != nil {
		t.Fatal(err)
	}
	binary.LittleEndian.PutUint32(header[0x08:], block+nativeDIBHeaderSize)
	pixels := []byte{0x1f, 0xf8, 0x1f, 0x00, 0xff, 0xff, 0x00, 0x00}
	if err := memory.Write(block, append(header, pixels...)); err != nil {
		t.Fatal(err)
	}
	nativeBlitCall(t, platform, block, 30, 30, 2, 2, 0, 0, nativeRasterTransparent)
	if got := frame.RGBAAt(30, 30); got != under {
		t.Errorf("a transparent copy drew the transparent value as %v", got)
	}
	if got, want := frame.RGBAAt(31, 30), decode565(0x001f); got != want {
		t.Errorf("the title's own pixel drew %v, want %v", got, want)
	}
	nativeBlitCall(t, platform, block, 40, 40, 2, 2, 0, 0, 2)
	if got, want := frame.RGBAAt(40, 40), decode565(nativeDIBTransparent); got != want {
		t.Errorf("a plain copy drew the transparent value as %v, want it drawn: %v", got, want)
	}

	// Something that is not an IDIB, and not an image either, is a blit the
	// platform missed rather than one it drew.
	missed := platform.screen.missed
	stranger, err := platform.client.Allocate(nativeDIBHeaderSize)
	if err != nil {
		t.Fatal(err)
	}
	if got := nativeBlitCall(t, platform, stranger, 50, 50, 2, 2, 0, 0, nativeRasterTransparent); got != 0 || platform.screen.missed != missed+1 {
		t.Errorf("a blit of an unknown object answered %d with %d missed, want 0 and one more", got, platform.screen.missed-missed)
	}
}

// TestNativeDIBRefusesAHeaderItCannotDraw keeps the blit to the IDIBs it can
// read: 5-6-5 pixels, a palette of none, and sides and a pitch that bound the
// rows it reads out of guest memory.
func TestNativeDIBRefusesAHeaderItCannotDraw(t *testing.T) {
	platform := newLaterNativePlatform(t)
	object := nativeImageOf(t, platform, buildBitmap(2, 2, []color.RGBA{nativeTransparent, {A: 0xff}}, []byte{1, 1, 0, 0, 1, 1, 0, 0}))
	header := nativeRead(t, platform, object, nativeDIBHeaderSize)
	for _, change := range []struct {
		name   string
		mutate func([]byte)
	}{
		{name: "eight bits a pixel", mutate: func(h []byte) { h[0x1c] = 8 }},
		{name: "a palette", mutate: func(h []byte) { binary.LittleEndian.PutUint16(h[0x1a:], 16) }},
		{name: "another scheme", mutate: func(h []byte) { h[0x1d] = 12 }},
		{name: "no width", mutate: func(h []byte) { binary.LittleEndian.PutUint16(h[0x14:], 0) }},
		{name: "a side past the bound", mutate: func(h []byte) { binary.LittleEndian.PutUint16(h[0x16:], nativeDIBMaxSide+1) }},
		{name: "a pitch shorter than a row", mutate: func(h []byte) { binary.LittleEndian.PutUint16(h[0x18:], 3) }},
		{name: "another table", mutate: func(h []byte) { binary.LittleEndian.PutUint32(h, 0x1234) }},
	} {
		changed := append([]byte(nil), header...)
		change.mutate(changed)
		block, err := platform.client.Allocate(nativeDIBHeaderSize)
		if err != nil {
			t.Fatal(err)
		}
		if err := platform.client.core.Memory().Write(block, changed); err != nil {
			t.Fatal(err)
		}
		if _, ok := platform.dibAt(block); ok {
			t.Errorf("a header with %s reads as an IDIB", change.name)
		}
	}
}
