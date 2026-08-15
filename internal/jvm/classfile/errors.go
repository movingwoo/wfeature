package classfile

import (
	"errors"
	"fmt"
)

var ErrInvalidFormat = errors.New("invalid Java class file")

type UnsupportedVersionError struct {
	Major uint16
}

func (e *UnsupportedVersionError) Error() string {
	return fmt.Sprintf("unsupported Java class version %d", e.Major)
}

func invalid(offset int, format string, args ...any) error {
	detail := fmt.Sprintf(format, args...)
	return fmt.Errorf("%w at byte %d: %s", ErrInvalidFormat, offset, detail)
}
