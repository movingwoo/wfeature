//go:build !darwin && !linux && !freebsd && !openbsd && !netbsd && !dragonfly && !windows

package backend

import (
	"errors"
	"fmt"
	"os"
)

// This build knows no file lock for its operating system, which is the same
// answer as a file system without one: every root keeps the in-process
// exclusion alone. See acquireSaveLock.
func lockSaveFile(_ *os.File, _ bool) error {
	return fmt.Errorf("directory save locking on this operating system: %w", errors.ErrUnsupported)
}

func unlockSaveFile(_ *os.File) {}

// The two classes saveLockUnavailable names on every platform are all this
// build can tell apart.
var saveLockUnavailableErrors []error
