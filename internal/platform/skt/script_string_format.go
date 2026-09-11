package skt

import (
	"bytes"
	"fmt"

	"github.com/movingwoo/wfeature/internal/sgsvm"
)

func formatScriptStringResource(vm *sgsvm.VM) error {
	if err := vm.Require(3); err != nil {
		return err
	}
	a := vm.Args(3)
	destination := vm.Resource(int(a[0]))
	format := vm.Resource(int(a[1]))
	source := vm.Resource(int(a[2]))
	if err := vm.Error(); err != nil {
		return err
	}
	formatLength, err := scriptResourceStringLength(vm, format.Data)
	if err != nil {
		return err
	}
	sourceLength, err := scriptResourceStringLength(vm, source.Data)
	if err != nil {
		return err
	}
	if err := vm.ChargeWork(formatLength + 256); err != nil {
		return err
	}
	result, err := scriptStringFormat(format.Data[:formatLength], source.Data[:sourceLength])
	if err != nil {
		return err
	}
	// Formatting finishes before resizing, including when any IDs alias.
	ok, err := resizeScriptResource(vm, destination, len(result))
	if err != nil || !ok {
		return err
	}
	copy(destination.Data, result)
	return vm.Error()
}

// scriptStringFormat substitutes the same byte string for every %s. Unlike
// numeric formatting, '+' and '#' are not flags. Precision deliberately uses
// bounded conventional behavior instead of the original non-advancing parser.
func scriptStringFormat(format, source []byte) ([]byte, error) {
	if n := bytes.IndexByte(format, 0); n >= 0 {
		format = format[:n]
	}
	if n := bytes.IndexByte(source, 0); n >= 0 {
		source = source[:n]
	}
	if len(format) > 4096 {
		return nil, fmt.Errorf("SGS string format exceeds 4096 bytes")
	}
	out := make([]byte, 0, 256)
	appendByte := func(value byte) {
		if len(out) < 255 {
			out = append(out, value)
		}
	}
	pad := func(value byte, count int) {
		for n := min(count, 255-len(out)); n > 0; n-- {
			out = append(out, value)
		}
	}
	for i := 0; i < len(format) && len(out) < 255; i++ {
		if format[i] != '%' {
			out = append(out, format[i])
			continue
		}
		i++
		left, zero := false, false
		width, precision := 0, -1
		for i < len(format) {
			switch c := format[i]; {
			case c == '-':
				left = true
			case c == '0':
				zero = true
			case c == '.':
				precision = 0
				for i+1 < len(format) && format[i+1] >= '0' && format[i+1] <= '9' {
					i++
					precision = min(256, precision*10+int(format[i]-'0'))
				}
			case c >= '1' && c <= '9':
				width = int(c - '0')
				for i+1 < len(format) && format[i+1] >= '0' && format[i+1] <= '9' {
					i++
					width = min(256, width*10+int(format[i]-'0'))
				}
			default:
				goto conversion
			}
			i++
		}
	conversion:
		if i >= len(format) {
			break
		}
		if format[i] != 's' {
			appendByte(format[i])
			continue
		}
		length := len(source)
		if precision >= 0 {
			length = min(length, precision)
		}
		padding := max(0, width-length)
		if !left {
			value := byte(' ')
			if zero {
				value = '0'
			}
			pad(value, padding)
		}
		out = append(out, source[:min(length, 255-len(out))]...)
		if left {
			pad(' ', padding)
		}
	}
	return append(out, 0), nil
}
