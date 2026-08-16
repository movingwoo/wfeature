package wipic

import (
	"encoding/binary"
	"image/color"
	"testing"
)

// buildLBMP writes the header a handset bitmap carries in front of the pixels
// it was given. The fixture is authored here rather than taken from an archive
// so the test owns every byte it asserts on.
func buildLBMP(depth, width, height uint32, body []byte) []byte {
	encoded := make([]byte, lbmpHeaderSize+len(body))
	copy(encoded, lbmpMagic)
	binary.LittleEndian.PutUint32(encoded[4:], depth)
	binary.LittleEndian.PutUint32(encoded[8:], width)
	binary.LittleEndian.PutUint32(encoded[12:], height)
	binary.LittleEndian.PutUint32(encoded[16:], uint32(len(body)))
	binary.LittleEndian.PutUint32(encoded[20:], 0)
	copy(encoded[lbmpHeaderSize:], body)
	return encoded
}

// Eight bits a pixel is rrrgggbb, and the widest value of each channel has to
// reach 255. Shifting the bits into place and stopping there makes white come
// out as 0xe0e0c0, which tints every image a title draws.
func TestDecodeLBMPExpandsRGB332(t *testing.T) {
	body := []byte{0x00, 0xff, 0xe0, 0x1c, 0x03, 0x24}
	encoded := buildLBMP(8, 6, 1, body)

	decoded, err := DecodeLBMP(encoded)
	if err != nil {
		t.Fatalf("DecodeLBMP() error = %v", err)
	}
	if bounds := decoded.Bounds(); bounds.Dx() != 6 || bounds.Dy() != 1 {
		t.Fatalf("bounds = %v, want 6x1", bounds)
	}
	want := []color.NRGBA{
		{0, 0, 0, 0xff},          // 0x00 black
		{0xff, 0xff, 0xff, 0xff}, // 0xff white, every channel saturated
		{0xff, 0, 0, 0xff},       // 0xe0 red
		{0, 0xff, 0, 0xff},       // 0x1c green
		{0, 0, 0xff, 0xff},       // 0x03 blue
		{0x24, 0x24, 0, 0xff},    // 0x24 the ramp's dark gold
	}
	for index, expected := range want {
		red, green, blue, alpha := decoded.At(index, 0).RGBA()
		got := color.NRGBA{uint8(red >> 8), uint8(green >> 8), uint8(blue >> 8), uint8(alpha >> 8)}
		if got != expected {
			t.Errorf("pixel %d = %+v, want %+v", index, got, expected)
		}
	}
}

// Sixteen bits a pixel is the LCD's own rrrrrggggggbbbbb, little-endian.
func TestDecodeLBMPExpandsRGB565(t *testing.T) {
	body := make([]byte, 8)
	for index, value := range []uint16{0x0000, 0xffff, 0xf800, 0x001f} {
		binary.LittleEndian.PutUint16(body[index*2:], value)
	}
	encoded := buildLBMP(16, 4, 1, body)

	decoded, err := DecodeLBMP(encoded)
	if err != nil {
		t.Fatalf("DecodeLBMP() error = %v", err)
	}
	want := []color.NRGBA{
		{0, 0, 0, 0xff},
		{0xff, 0xff, 0xff, 0xff},
		{0xff, 0, 0, 0xff},
		{0, 0, 0xff, 0xff},
	}
	for index, expected := range want {
		red, green, blue, alpha := decoded.At(index, 0).RGBA()
		got := color.NRGBA{uint8(red >> 8), uint8(green >> 8), uint8(blue >> 8), uint8(alpha >> 8)}
		if got != expected {
			t.Errorf("pixel %d = %+v, want %+v", index, got, expected)
		}
	}
}

// Rows run top to bottom and pixels left to right, which is what makes a
// decoded image the right way up rather than mirrored or flipped.
func TestDecodeLBMPRowOrder(t *testing.T) {
	// A 2x2 whose four pixels are all different: red, green, blue, white.
	body := []byte{0xe0, 0x1c, 0x03, 0xff}
	decoded, err := DecodeLBMP(buildLBMP(8, 2, 2, body))
	if err != nil {
		t.Fatal(err)
	}
	corners := map[[2]int]color.NRGBA{
		{0, 0}: {0xff, 0, 0, 0xff},
		{1, 0}: {0, 0xff, 0, 0xff},
		{0, 1}: {0, 0, 0xff, 0xff},
		{1, 1}: {0xff, 0xff, 0xff, 0xff},
	}
	for at, expected := range corners {
		red, green, blue, alpha := decoded.At(at[0], at[1]).RGBA()
		got := color.NRGBA{uint8(red >> 8), uint8(green >> 8), uint8(blue >> 8), uint8(alpha >> 8)}
		if got != expected {
			t.Errorf("pixel %v = %+v, want %+v", at, got, expected)
		}
	}
}

// The header comes out of an archive this build treats as untrusted, so every
// way it can disagree with the bytes behind it has to be a refusal rather than
// a read past the end or an allocation the header asked for.
func TestDecodeLBMPRefusesAHeaderThatDoesNotAddUp(t *testing.T) {
	tests := []struct {
		name    string
		encoded []byte
	}{
		{"not an LBMP at all", []byte("PNG\x00 and then some padding bytes")},
		{"truncated below the header", []byte("LBMP\x08\x00\x00")},
		{"a depth this format does not carry", buildLBMP(4, 2, 2, make([]byte, 4))},
		{"a grayscale depth, which is unsupported", buildLBMP(2, 2, 2, make([]byte, 4))},
		{"zero width", buildLBMP(8, 0, 2, nil)},
		{"zero height", buildLBMP(8, 2, 0, nil)},
		{"dimensions past the bound", buildLBMP(8, 1<<16, 1<<16, nil)},
		{"a size that disagrees with the dimensions", buildLBMP(8, 4, 4, make([]byte, 4))},
		{"a body shorter than the size it declares", append(buildLBMP(8, 4, 4, make([]byte, 16))[:lbmpHeaderSize], 1, 2, 3)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := DecodeLBMP(test.encoded); err == nil {
				t.Fatal("DecodeLBMP accepted it")
			}
		})
	}
}

// IsLBMP answers on the magic alone, so a truncated one is reported as a bad
// image of this format rather than falling through to a decoder that would
// call it a bad PNG.
func TestIsLBMPRoutesOnTheMagicAlone(t *testing.T) {
	if !IsLBMP(buildLBMP(8, 2, 2, make([]byte, 3))) {
		t.Fatal("a header with a short body was not routed here")
	}
	if IsLBMP([]byte("LBM")) {
		t.Fatal("something too short to hold a header was routed here")
	}
	if IsLBMP(make([]byte, 64)) {
		t.Fatal("zeroed bytes were routed here")
	}
}
