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

// What a location that cannot hold the lock answers here; see
// saveLockUnavailable. The first three are the reserved directory or the lock
// file not being creatable, by permission or on a read-only file system, and
// the rest are a file system with no locks to give. EWOULDBLOCK is not among
// them: that is another holder.
var saveLockUnavailableErrors = []error{
	syscall.EACCES, syscall.EPERM, syscall.EROFS,
	syscall.ENOTSUP, syscall.EOPNOTSUPP, syscall.ENOLCK, syscall.ENOSYS,
}
