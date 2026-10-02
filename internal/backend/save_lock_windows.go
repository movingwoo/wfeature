package backend

import (
	"errors"
	"os"

	"golang.org/x/sys/windows"
)

func lockSaveFile(file *os.File, wait bool) error {
	mode := uint32(windows.LOCKFILE_EXCLUSIVE_LOCK)
	if !wait {
		mode |= windows.LOCKFILE_FAIL_IMMEDIATELY
	}
	err := windows.LockFileEx(windows.Handle(file.Fd()), mode, 0, 1, 0, &windows.Overlapped{})
	if errors.Is(err, windows.ERROR_LOCK_VIOLATION) {
		return ErrSaveDirectoryBusy
	}
	return err
}

func unlockSaveFile(file *os.File) {
	_ = windows.UnlockFileEx(windows.Handle(file.Fd()), 0, 1, 0, &windows.Overlapped{})
}

// What a location that cannot hold the lock answers here; see
// saveLockUnavailable. The first two are the reserved directory or the lock
// file not being creatable, by access rights or on write-protected media, and
// the rest are a volume whose driver has no byte-range locks to give. A lock
// or sharing violation is not among them: that is another holder.
var saveLockUnavailableErrors = []error{
	windows.ERROR_ACCESS_DENIED, windows.ERROR_WRITE_PROTECT,
	windows.ERROR_NOT_SUPPORTED, windows.ERROR_INVALID_FUNCTION, windows.ERROR_CALL_NOT_IMPLEMENTED,
}
