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

	for i := 0; i < len(units); i++ {
		unit := units[i]
		if 0xd800 <= unit && unit <= 0xdbff {
			if i+1 >= len(units) || units[i+1] < 0xdc00 || units[i+1] > 0xdfff {
				return "", fmt.Errorf("unpaired high surrogate at code unit %d", i)
			}
			i++
			continue
		}
		if 0xdc00 <= unit && unit <= 0xdfff {
			return "", fmt.Errorf("unpaired low surrogate at code unit %d", i)
		}
	}

	return string(utf16.Decode(units)), nil
}
