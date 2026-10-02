//go:build darwin || linux || freebsd || openbsd || netbsd || dragonfly

package backend

import (
	"errors"
	"os"
	"syscall"
)

func lockSaveFile(file *os.File, wait bool) error {
	mode := syscall.LOCK_EX
	if !wait {
		mode |= syscall.LOCK_NB
	}
	for {
		err := syscall.Flock(int(file.Fd()), mode)
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return ErrSaveDirectoryBusy
		}
		if !errors.Is(err, syscall.EINTR) {
			return err
		}
	}
}

func unlockSaveFile(file *os.File) { _ = syscall.Flock(int(file.Fd()), syscall.LOCK_UN) }
