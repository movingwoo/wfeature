package skt

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"

	"github.com/movingwoo/wfeature/internal/sgsvm"
)

func wideString(vm *sgsvm.WideVM, index int32) ([]byte, error) {
	r, err := wideResource(vm, index, false)
	if err != nil {
		return nil, err
	}
	if err := vm.Charge(len(r.Data)/16 + 1); err != nil {
		return nil, err
	}
	value, _, _ := bytes.Cut(r.Data, []byte{0})
	return value, nil
}

func putWideString(vm *sgsvm.WideVM, index int32, value []byte) error {
	r, err := wideResize(vm, index, len(value)+1)
	if err != nil {
		return err
	}
	copy(r.Data, value)
	r.Data[len(value)] = 0
	return nil
}

func wideStrings(id uint16, vm *sgsvm.WideVM) (bool, error) {
	var n int
	switch id {
	case 0x9f, 0xa1, 0xa3, 0xa4, 0xa5, 0xa8:
		n = 2
	case 0xa0, 0xa7:
		n = 1
	case 0xa2:
		n = 4
	case 0xa6, 0xad:
		n = 3
	case 0xae, 0xaf, 0xb0, 0xb1, 0xb2:
		n = int(id-0xae) + 3
	default:
		return false, nil
	}
	a, err := vm.Args(n)
	if err != nil {
		return true, err
	}
	switch id {
	case 0x9f:
		if a[1] < 0 || a[1] > 4<<20 {
			return true, fmt.Errorf("SGS: invalid string size %d", a[1])
		}
		size := max(2, (int(a[1])+1)&^1)
		if err := vm.Charge(1 + size/32); err != nil {
			return true, err
		}
		r, err := wideResize(vm, a[0], size)
		if err == nil {
			clear(r.Data)
		}
		return true, err
	case 0xa5, 0xa6:
		r, err := wideResource(vm, a[0], id == 0xa6)
		if err != nil {
			return true, err
		}
		if a[1] < 0 || int64(a[1]) >= int64(len(r.Data)) {
			return true, fmt.Errorf("SGS: character offset %d outside resource %d of %d bytes", a[1], a[0], len(r.Data))
		}
		if id == 0xa6 {
			r.Data[a[1]] = byte(a[2])
			return true, nil
		}
		return true, vm.Push(int32(r.Data[a[1]]))
	case 0xa8:
		return true, putWideString(vm, a[0], strconv.AppendInt(nil, int64(a[1]), 10))
	case 0xa0, 0xa7:
		value, err := wideString(vm, a[0])
		if err != nil {
			return true, err
		}
		if id == 0xa0 {
			return true, vm.Push(int32(len(value)))
		}
		s := strings.TrimSpace(string(value))
		i := 0
		if len(s) > 0 && (s[0] == '-' || s[0] == '+') {
			i++
		}
		for i < len(s) && s[i] >= '0' && s[i] <= '9' {
			i++
		}
		number, _ := strconv.ParseInt(s[:i], 10, 32)
		return true, vm.Push(int32(number))
	}
	src, err := wideString(vm, a[1])
	if err != nil {
		return true, err
	}
	var result []byte
	switch id {
	case 0xa1:
		result = src
	case 0xa2:
		if a[2] < 0 || a[3] < 0 {
			return true, fmt.Errorf("SGS: negative substring range")
		}
		start := min(int(a[2]), len(src))
		result = src[start : start+min(int(a[3]), len(src)-start)]
	case 0xa3, 0xa4:
		dst, err := wideString(vm, a[0])
		if err != nil {
			return true, err
		}
		if id == 0xa4 {
			return true, vm.Push(int32(bytes.Compare(dst, src)))
		}
		if len(src) > 4<<20-len(dst) {
			return true, fmt.Errorf("SGS: string exceeds memory limit")
		}
		result = append(bytes.Clone(dst), src...)
	case 0xad:
		arg, err := wideString(vm, a[2])
		if err != nil {
			return true, err
		}
		result, err = scriptStringFormat(src, arg)
		if err != nil {
			return true, err
		}
		result = result[:len(result)-1]
	default:
		result, err = formatScriptIntegers(src, a[2:]...)
		if err != nil {
			return true, err
		}
		result = result[:len(result)-1]
	}
	return true, putWideString(vm, a[0], result)
}
