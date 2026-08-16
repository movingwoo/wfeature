package lgt

import (
	"encoding/binary"
	"testing"
)

// The image router here answers the same question this platform's neighbour
// does, so it takes the same formats. See internal/wipic for the format.
func TestImageDecoderReadsTheHandsetBitmap(t *testing.T) {
	body := []byte{0xe0, 0x1c}
	encoded := make([]byte, 24+len(body))
	copy(encoded, "LBMP")
	binary.LittleEndian.PutUint32(encoded[4:], 8)
	binary.LittleEndian.PutUint32(encoded[8:], 2)
	binary.LittleEndian.PutUint32(encoded[12:], 1)
	binary.LittleEndian.PutUint32(encoded[16:], uint32(len(body)))
	copy(encoded[24:], body)

	decoded, err := decodeImage(encoded)
	if err != nil {
		t.Fatalf("decodeImage() error = %v", err)
	}
	if bounds := decoded.Bounds(); bounds.Dx() != 2 || bounds.Dy() != 1 {
		t.Fatalf("bounds = %v, want 2x1", bounds)
	}
	red, green, blue, _ := decoded.At(0, 0).RGBA()
	if red>>8 != 0xff || green>>8 != 0 || blue>>8 != 0 {
		t.Fatalf("first pixel = %d,%d,%d, want a saturated red", red>>8, green>>8, blue>>8)
	}
}
