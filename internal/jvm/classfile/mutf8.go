package classfile

import (
	"fmt"
	"unicode/utf16"
)

func decodeModifiedUTF8(data []byte) (string, error) {
	units := make([]uint16, 0, len(data))
	for offset := 0; offset < len(data); {
		first := data[offset]
		switch {
		case first >= 0x01 && first <= 0x7f:
			units = append(units, uint16(first))
			offset++
		case first&0xe0 == 0xc0:
			if offset+1 >= len(data) || data[offset+1]&0xc0 != 0x80 {
				return "", fmt.Errorf("invalid modified UTF-8 at byte %d", offset)
			}
			value := uint16(first&0x1f)<<6 | uint16(data[offset+1]&0x3f)
			if value != 0 && value < 0x80 {
				return "", fmt.Errorf("overlong modified UTF-8 at byte %d", offset)
			}
			units = append(units, value)
			offset += 2
		case first&0xf0 == 0xe0:
			if offset+2 >= len(data) || data[offset+1]&0xc0 != 0x80 || data[offset+2]&0xc0 != 0x80 {
				return "", fmt.Errorf("invalid modified UTF-8 at byte %d", offset)
			}
			value := uint16(first&0x0f)<<12 | uint16(data[offset+1]&0x3f)<<6 | uint16(data[offset+2]&0x3f)
			if value < 0x800 {
				return "", fmt.Errorf("overlong modified UTF-8 at byte %d", offset)
			}
			units = append(units, value)
			offset += 3
		default:
			return "", fmt.Errorf("invalid modified UTF-8 at byte %d", offset)
		}
	}

	// CONSTANT_Utf8 encodes each UTF-16 unit on its own, so half a surrogate
	// pair is legal here and javac writes one for a literal such as "\uD800".
	// Refusing it refused a valid class. The text is Go's, which cannot hold a
	// lone half, so utf16.Decode makes it one replacement character, as the
	// runtime does for one built at run time.
	return string(utf16.Decode(units)), nil
}
