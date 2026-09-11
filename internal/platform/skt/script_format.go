package skt

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"

	"github.com/movingwoo/wfeature/internal/sgsvm"
)

// formatScriptResource implements the original bounded word formatter. Its result
// contains at most 255 bytes plus a terminator, including embedded EUC-KR bytes.
func formatScriptResource(vm *sgsvm.VM, count int) error {
	a := vm.Args(2 + count)
	dst, src := vm.Resource(int(a[0])), vm.Resource(int(a[1]))
	if vm.Error() != nil {
		return vm.Error()
	}
	data := src.Data
	if n := bytes.IndexByte(data, 0); n >= 0 {
		data = data[:n]
	}
	if len(data) > 4096 {
		return fmt.Errorf("SGS format exceeds 4096 bytes")
	}
	if err := vm.ChargeWork(len(data) + 256); err != nil {
		return err
	}
	result, err := scriptFormat(data, a[2:]...)
	if err != nil {
		return err
	}
	ok, err := resizeScriptResource(vm, dst, len(result))
	if err != nil || !ok {
		return err
	}
	copy(dst.Data, result)
	return nil
}

func scriptFormat(format []byte, values ...int16) ([]byte, error) {
	out := make([]byte, 0, 256)
	appendText := func(s string) { n := min(len(s), 255-len(out)); out = append(out, s[:n]...) }
	used := 0
	for i := 0; i < len(format) && len(out) < 255; i++ {
		if format[i] != '%' {
			out = append(out, format[i])
			continue
		}
		i++
		left, zero, alternate := false, false, false
		sign := ""
		width, precision := 0, -1
		for i < len(format) {
			c := format[i]
			switch c {
			case '-':
				left = true
			case '+':
				sign = "+"
			case ' ':
				if sign == "" {
					sign = " "
				}
			case '#':
				alternate = true
			case '0':
				zero = true
			case '.':
				precision = 0
				for i+1 < len(format) && format[i+1] >= '0' && format[i+1] <= '9' {
					i++
					precision = min(256, precision*10+int(format[i]-'0'))
				}
			default:
				if c >= '1' && c <= '9' {
					width = int(c - '0')
					for i+1 < len(format) && format[i+1] >= '0' && format[i+1] <= '9' {
						i++
						width = min(256, width*10+int(format[i]-'0'))
					}
				} else {
					goto conversion
				}
			}
			i++
		}
	conversion:
		if i >= len(format) {
			break
		}
		verb := format[i]
		if verb != 'd' && verb != 'x' && verb != 'X' && verb != 'c' {
			appendText(string([]byte{verb}))
			continue
		}
		if used >= len(values) {
			return nil, fmt.Errorf("SGS format has too few arguments")
		}
		value := values[used]
		used++
		digits, prefix := "", ""
		switch verb {
		case 'c':
			digits = string([]byte{byte(value)})
		case 'd':
			n := int(value)
			prefix = sign
			if n < 0 {
				prefix = "-"
				n = -n
			}
			digits = strconv.Itoa(n)
		case 'x', 'X':
			// The native formatter receives a sign-extended 32-bit word.
			digits = strconv.FormatUint(uint64(uint32(int32(value))), 16)
			if verb == 'X' {
				digits = strings.ToUpper(digits)
			}
			if alternate && value != 0 {
				prefix = "0" + string(verb)
			}
		}
		if verb != 'c' {
			if value == 0 && precision == 0 {
				digits = ""
			}
			digits = strings.Repeat("0", max(0, precision-len(digits))) + digits
			if precision >= 0 {
				zero = false
			}
		}
		padding := max(0, width-len(prefix)-len(digits))
		if !left && !zero {
			appendText(strings.Repeat(" ", padding))
		}
		appendText(prefix)
		if !left && zero {
			appendText(strings.Repeat("0", padding))
		}
		appendText(digits)
		if left {
			appendText(strings.Repeat(" ", padding))
		}
	}
	// A formatted character can itself be NUL; resource allocation follows strlen.
	if n := bytes.IndexByte(out, 0); n >= 0 {
		out = out[:n]
	}
	return append(out, 0), nil
}
