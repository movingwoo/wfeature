package skt

import (
	"bytes"
	"fmt"
	"strconv"

	"github.com/movingwoo/wfeature/internal/sgsvm"
)

func (s *ScriptSession) traceCall(op byte, vm *sgsvm.VM) error {
	count := 4
	if op == 0xf1 {
		count = 2
	} else if op != 0xf0 {
		return fmt.Errorf("unsupported SGS trace service 0x%02x", op)
	}
	if err := vm.Require(count); err != nil {
		return err
	}
	a := vm.Args(count)
	format := vm.Resource(int(a[0])).Data
	if err := vm.Error(); err != nil {
		return err
	}
	if i := bytes.IndexByte(format, 0); i >= 0 {
		format = format[:i]
	} else if op == 0xf1 {
		return fmt.Errorf("SGS trace format is unterminated")
	}
	if len(format) > 4096 {
		return fmt.Errorf("SGS trace format exceeds 4096 bytes")
	}
	if err := vm.ChargeWork(len(format) + 256); err != nil {
		return err
	}
	var result []byte
	var err error
	if op == 0xf0 {
		result, err = scriptFormat(format, a[1:]...)
	} else {
		source := vm.Resource(int(a[1])).Data
		if err := vm.Error(); err != nil {
			return err
		}
		length, readErr := scriptResourceStringLength(vm, source)
		if readErr != nil {
			return readErr
		}
		source = source[:length]
		result, err = scriptStringFormat(format, source)
	}
	if err != nil {
		return err
	}
	if s.options.Logger != nil {
		// Preserve guest bytes in an ASCII diagnostic representation; shell output
		// stays English and no guest-controlled text becomes a log message template.
		s.options.Logger.Debug("SGS trace", "data", strconv.QuoteToASCII(string(bytes.TrimSuffix(result, []byte{0}))))
	}
	return nil
}
