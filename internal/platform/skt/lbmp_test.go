package skt

import (
	"encoding/binary"
	"testing"
)

// No local archive for this platform ships a handset bitmap, but the decode is
// shared with the platforms whose archives do, and an image the others can
// read should not end a title here. See internal/wipic for the format.
func TestMIDPImageDecoderReadsTheHandsetBitmap(t *testing.T) {
	body := []byte{0xe0, 0x1c}
	encoded := make([]byte, 24+len(body))
	copy(encoded, "LBMP")
	binary.LittleEndian.PutUint32(encoded[4:], 8)
	binary.LittleEndian.PutUint32(encoded[8:], 2)
	binary.LittleEndian.PutUint32(encoded[12:], 1)
	binary.LittleEndian.PutUint32(encoded[16:], uint32(len(body)))
	copy(encoded[24:], body)

	object, err := decodeMIDPImage(encoded)
	if err != nil {
		t.Fatalf("decodeMIDPImage() error = %v", err)
	}
	image := object.Native.(*imageData)
	if image.width != 2 || image.height != 1 {
		t.Fatalf("size = %dx%d, want 2x1", image.width, image.height)
	}
	pixels := image.snapshot()
	if pixels[0] != 0xff || pixels[1] != 0 || pixels[2] != 0 || pixels[3] != 0xff {
		t.Fatalf("first pixel = %v, want an opaque saturated red", pixels[:4])
	}
}
