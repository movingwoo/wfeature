//go:build windows

package backend

import (
	"errors"
	"syscall"
)

// saveNameRefused reports whether an error is Windows refusing a name rather
// than finding nothing at it: ERROR_INVALID_NAME, 123, for a probe of an
// earlier build's spelling holding a character Windows does not take in a
// name, such as ':'. No file can be at such a name, so it is not there.
func saveNameRefused(err error) bool {
	return errors.Is(err, syscall.Errno(123))
}
