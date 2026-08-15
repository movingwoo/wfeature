// Package wipic holds the parts of the WIPI C library that are the same
// wherever they are called from. A platform decides how a call arrives and
// where its arguments live; what a format string means does not change with
// the platform, so it lives here rather than in either one.
package wipic

import (
	"fmt"
	"strconv"
	"strings"
)

const (
	// MaxWidth bounds a field width so a hostile format string cannot ask for
	// an unbounded run of padding.
	MaxWidth = 4096
	// MaxOutput bounds one rendered result.
	MaxOutput = 1 << 16
)

// ReadString reads a guest string for %s. A limit of zero or more is a
// precision: read at most that many bytes, stopping early at a terminator, and
// do not require one — `%.*s` is how a game prints a slice of a buffer that
// has no terminator at all. A negative limit asks for a whole C string.
type ReadString func(address uint32, limit int) ([]byte, error)

// specification is one parsed conversion: everything between the % and the
// conversion character, in the order C reads it.
type specification struct {
	leftAlign bool
	zero      bool
	plus      bool
	space     bool
	alternate bool
	width     int
	precision int // negative when the format gave none
	longs     int
}

// Format renders the printf subset the original MC_knlSprintk accepts: %%, %c,
// %d, %i, %u, %o, %x, %X, %p and %s, with the -, 0, +, space and # flags, a
// bounded width, a precision, and the h/l length modifiers. Width and
// precision may both be given as * and read an argument word of their own.
//
// next reads the next variadic argument, taking one or two words. read reads a
// guest string for %s. String arguments stay raw bytes, so guest text keeps its
// EUC-KR encoding for the caller to decode.
//
// A conversion the subset does not model is emitted as written rather than
// dropped. That keeps an unrecognised directive visible as itself instead of
// disappearing into the text around it, which is what a reader needs to see to
// name what is missing.
func Format(format []byte, next func(words int) (uint64, error), read ReadString) ([]byte, error) {
	result := make([]byte, 0, len(format)+32)
	for index := 0; index < len(format); index++ {
		if format[index] != '%' {
			result = append(result, format[index])
			continue
		}
		rendered, consumed, err := convert(format[index:], next, read)
		if err != nil {
			return nil, err
		}
		result = append(result, rendered...)
		if len(result) > MaxOutput {
			return nil, fmt.Errorf("sprintk output exceeds %d bytes", MaxOutput)
		}
		index += consumed - 1
	}
	return result, nil
}

// convert renders the one conversion starting at the % that begins format and
// answers how many bytes of it were consumed.
func convert(format []byte, next func(words int) (uint64, error), read ReadString) ([]byte, int, error) {
	spec := specification{precision: -1}
	index := 1
	for index < len(format) {
		flag := format[index]
		if flag != '-' && flag != '0' && flag != '+' && flag != ' ' && flag != '#' {
			break
		}
		switch flag {
		case '-':
			spec.leftAlign = true
		case '0':
			spec.zero = true
		case '+':
			spec.plus = true
		case ' ':
			spec.space = true
		case '#':
			spec.alternate = true
		}
		index++
	}
	if index < len(format) && format[index] == '*' {
		// A negative * width is a - flag with the width as its magnitude.
		argument, err := next(1)
		if err != nil {
			return nil, 0, err
		}
		if given := int32(uint32(argument)); given < 0 {
			spec.leftAlign = true
			spec.width = bound(-int(given))
		} else {
			spec.width = bound(int(given))
		}
		index++
	} else {
		width := 0
		for index < len(format) && format[index] >= '0' && format[index] <= '9' {
			width = bound(width*10 + int(format[index]-'0'))
			index++
		}
		spec.width = width
	}
	if index < len(format) && format[index] == '.' {
		index++
		if index < len(format) && format[index] == '*' {
			argument, err := next(1)
			if err != nil {
				return nil, 0, err
			}
			// A negative * precision is C's way of saying there is none.
			if given := int32(uint32(argument)); given < 0 {
				spec.precision = -1
			} else {
				spec.precision = bound(int(given))
			}
			index++
		} else {
			// A bare `.` is a precision of zero.
			precision := 0
			for index < len(format) && format[index] >= '0' && format[index] <= '9' {
				precision = bound(precision*10 + int(format[index]-'0'))
				index++
			}
			spec.precision = precision
		}
	}
	for index < len(format) && (format[index] == 'l' || format[index] == 'h' || format[index] == 'z') {
		if format[index] == 'l' {
			spec.longs++
		}
		index++
	}
	if index >= len(format) {
		// The format ran out before naming a conversion.
		return format, len(format), nil
	}
	directive := format[index]
	index++
	switch directive {
	case '%':
		return []byte{'%'}, index, nil
	case 'c':
		argument, err := next(1)
		if err != nil {
			return nil, 0, err
		}
		return pad([]byte{byte(argument)}, spec, false), index, nil
	case 's':
		argument, err := next(1)
		if err != nil {
			return nil, 0, err
		}
		text := []byte{}
		if argument != 0 {
			bytes, err := read(uint32(argument), spec.precision)
			if err != nil {
				return nil, 0, fmt.Errorf("read %%s argument at %#x: %w", uint32(argument), err)
			}
			text = bytes
			if spec.precision >= 0 && len(text) > spec.precision {
				text = text[:spec.precision]
			}
		}
		// A precision bounds a string rather than padding it, so the zero flag
		// has nothing to say here.
		return pad(text, spec, false), index, nil
	case 'd', 'i', 'u', 'o', 'x', 'X', 'p':
		rendered, err := integer(directive, spec, next)
		if err != nil {
			return nil, 0, err
		}
		return rendered, index, nil
	default:
		// A conversion the subset does not model is emitted as written.
		return format[:index], index, nil
	}
}

// integer renders one numeric conversion into a padded field.
func integer(directive byte, spec specification, next func(words int) (uint64, error)) ([]byte, error) {
	words := 1
	if spec.longs >= 2 {
		words = 2
	}
	raw, err := next(words)
	if err != nil {
		return nil, err
	}
	var text string
	switch directive {
	case 'd', 'i':
		value := int64(raw)
		if words == 1 {
			value = int64(int32(uint32(raw)))
		}
		sign := ""
		magnitude := uint64(value)
		if value < 0 {
			sign, magnitude = "-", uint64(-value)
		} else if spec.plus {
			sign = "+"
		} else if spec.space {
			sign = " "
		}
		text = sign + digits(magnitude, 10, false, spec.precision)
	case 'u':
		text = digits(raw, 10, false, spec.precision)
	case 'o':
		text = digits(raw, 8, false, spec.precision)
		if spec.alternate && !strings.HasPrefix(text, "0") {
			text = "0" + text
		}
	case 'x', 'X':
		text = digits(raw, 16, directive == 'X', spec.precision)
		if spec.alternate && raw != 0 {
			if directive == 'X' {
				text = "0X" + text
			} else {
				text = "0x" + text
			}
		}
	case 'p':
		text = "0x" + digits(raw, 16, false, spec.precision)
	}
	// A precision does its own zero filling, which leaves the zero flag with
	// nothing to add.
	if spec.precision >= 0 {
		spec.zero = false
	}
	return pad([]byte(text), spec, true), nil
}

func bound(value int) int {
	if value > MaxWidth {
		return MaxWidth
	}
	return value
}

// pad lays a rendered value into its field. A left-aligned field always fills
// with spaces: zeroes on the right would change the value.
func pad(text []byte, spec specification, numeric bool) []byte {
	missing := spec.width - len(text)
	if missing <= 0 {
		return text
	}
	result := make([]byte, 0, spec.width)
	if spec.leftAlign {
		result = append(result, text...)
		for ; missing > 0; missing-- {
			result = append(result, ' ')
		}
		return result
	}
	if spec.zero && numeric {
		// A zero-padded signed number keeps its sign in front of the fill.
		if len(text) > 0 && (text[0] == '-' || text[0] == '+' || text[0] == ' ') {
			result = append(result, text[0])
			text = text[1:]
		}
		for ; missing > 0; missing-- {
			result = append(result, '0')
		}
		return append(result, text...)
	}
	for ; missing > 0; missing-- {
		result = append(result, ' ')
	}
	return append(result, text...)
}

// digits renders the magnitude in the conversion's base, at least precision
// digits wide.
func digits(value uint64, base int, upper bool, precision int) string {
	text := strconv.FormatUint(value, base)
	if upper {
		text = strings.ToUpper(text)
	}
	if precision >= 0 && len(text) < precision {
		text = strings.Repeat("0", precision-len(text)) + text
	}
	// A precision of zero renders zero as nothing at all.
	if precision == 0 && value == 0 {
		text = ""
	}
	return text
}
