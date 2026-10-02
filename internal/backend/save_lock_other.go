//go:build !darwin && !linux && !freebsd && !openbsd && !netbsd && !dragonfly && !windows

package backend

import (
	"fmt"
	"os"
)

func lockSaveFile(_ *os.File, _ bool) error {
	return fmt.Errorf("directory save locking is unsupported on this operating system")
}

func unlockSaveFile(_ *os.File) {}
