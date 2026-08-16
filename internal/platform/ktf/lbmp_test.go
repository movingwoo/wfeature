package ktf

import (
	"encoding/binary"
	"testing"
)

// A local archive ships five of these beside its PNGs, and until the decoder
// existed the image router handed them to the standard library, which reported
// an unknown format and left the title with an undecodable image. This proves
// the router reaches the decoder; the format itself is settled in
// `internal/wipic`.
func TestGuestImageDecoderReadsTheHandsetBitmap(t *testing.T) {
	// Two pixels of RGB332, red then green.
	body := []byte{0xe0, 0x1c}
	encoded := make([]byte, 24+len(body))
	copy(encoded, "LBMP")
	binary.LittleEndian.PutUint32(encoded[4:], 8)
	binary.LittleEndian.PutUint32(encoded[8:], 2)
	binary.LittleEndian.PutUint32(encoded[12:], 1)
	binary.LittleEndian.PutUint32(encoded[16:], uint32(len(body)))
	copy(encoded[24:], body)

	decoded, err := decodeGuestImage(encoded)
	if err != nil {
		t.Fatalf("decodeGuestImage() error = %v", err)
	}
	if bounds := decoded.Bounds(); bounds.Dx() != 2 || bounds.Dy() != 1 {
		t.Fatalf("bounds = %v, want 2x1", bounds)
	}
	red, green, blue, _ := decoded.At(0, 0).RGBA()
	if red>>8 != 0xff || green>>8 != 0 || blue>>8 != 0 {
		t.Fatalf("first pixel = %d,%d,%d, want a saturated red", red>>8, green>>8, blue>>8)
	}
}
