// Package cheat implements emulator-attached memory search and patching.
// The scanner works against any MemoryTarget, which ARM-backed platform
// clients (KTF, later LGT) implement over their flat guest address space.
package cheat

import "fmt"

// ValueKind is the width and signedness of a scanned value. Everything is
// normalized to int64 once decoded so the filters only ever compare one type.
type ValueKind uint8

const (
	KindU8 ValueKind = iota
	KindU16
	KindU32
	KindI8
	KindI16
	KindI32
)

// Size returns the value width in bytes.
func (kind ValueKind) Size() int {
	switch kind {
	case KindU8, KindI8:
		return 1
	case KindU16, KindI16:
		return 2
	default:
		return 4
	}
}

func (kind ValueKind) String() string {
	switch kind {
	case KindU8:
		return "u8"
	case KindU16:
		return "u16"
	case KindU32:
		return "u32"
	case KindI8:
		return "i8"
	case KindI16:
		return "i16"
	case KindI32:
		return "i32"
	default:
		return "invalid"
	}
}

func parseValueKind(name string) (ValueKind, bool) {
	switch name {
	case "u8":
		return KindU8, true
	case "u16":
		return KindU16, true
	case "u32":
		return KindU32, true
	case "i8":
		return KindI8, true
	case "i16":
		return KindI16, true
	case "i32":
		return KindI32, true
	default:
		return 0, false
	}
}

// Endian selects the byte order used to decode a value. The ARM core itself
// is little-endian, but WIPI save records and a lot of network-sourced game
// data are stored big-endian, so both are scannable.
type Endian uint8

const (
	Little Endian = iota
	Big
)

// ValueType is a ValueKind plus the byte order used to decode it.
type ValueType struct {
	Kind   ValueKind
	Endian Endian
}

// Size returns the value width in bytes.
func (valueType ValueType) Size() int {
	return valueType.Kind.Size()
}

func (valueType ValueType) String() string {
	if valueType.Endian == Big {
		return valueType.Kind.String() + "be"
	}
	return valueType.Kind.String()
}

// ParseValueType parses "u32" / "u32be" / "u16be" and friends. A bare type is
// little-endian.
func ParseValueType(name string) (ValueType, bool) {
	endian := Little
	if rest, found := cutSuffix(name, "be"); found {
		if kind, ok := parseValueKind(rest); ok {
			return ValueType{Kind: kind, Endian: Big}, true
		}
	}
	if rest, found := cutSuffix(name, "le"); found {
		if kind, ok := parseValueKind(rest); ok {
			return ValueType{Kind: kind, Endian: Little}, true
		}
	}
	kind, ok := parseValueKind(name)
	if !ok {
		return ValueType{}, false
	}
	return ValueType{Kind: kind, Endian: endian}, true
}

func cutSuffix(s, suffix string) (string, bool) {
	if len(s) <= len(suffix) || s[len(s)-len(suffix):] != suffix {
		return s, false
	}
	return s[:len(s)-len(suffix)], true
}

// Decode reads bytes[:Size()] in this type's byte order. It reports false
// when the slice is short.
func (valueType ValueType) Decode(bytes []byte) (int64, bool) {
	size := valueType.Size()
	if len(bytes) < size {
		return 0, false
	}
	var raw uint32
	if valueType.Endian == Little {
		for index := range size {
			raw |= uint32(bytes[index]) << (8 * index)
		}
	} else {
		for index := range size {
			raw = raw<<8 | uint32(bytes[index])
		}
	}
	switch valueType.Kind {
	case KindI8:
		return int64(int8(raw)), true
	case KindI16:
		return int64(int16(raw)), true
	case KindI32:
		return int64(int32(raw)), true
	default:
		return int64(raw), true
	}
}

// Encode writes value truncated to this type's width, in this type's byte
// order. Values that would silently wrap are rejected; a typo'd cheat value
// that becomes a different number is worse than an error.
func (valueType ValueType) Encode(value int64) ([]byte, error) {
	var fits bool
	switch valueType.Kind {
	case KindU8:
		fits = value >= 0 && value <= 0xff
	case KindU16:
		fits = value >= 0 && value <= 0xffff
	case KindU32:
		fits = value >= 0 && value <= 0xffffffff
	case KindI8:
		fits = value >= -128 && value <= 127
	case KindI16:
		fits = value >= -32768 && value <= 32767
	case KindI32:
		fits = value >= -2147483648 && value <= 2147483647
	}
	if !fits {
		return nil, fmt.Errorf("%d does not fit in %s", value, valueType.Kind)
	}
	size := valueType.Size()
	raw := uint32(value)
	out := make([]byte, size)
	for index := range out {
		if valueType.Endian == Little {
			out[index] = byte(raw >> (8 * index))
		} else {
			out[index] = byte(raw >> (8 * (size - 1 - index)))
		}
	}
	return out, nil
}
