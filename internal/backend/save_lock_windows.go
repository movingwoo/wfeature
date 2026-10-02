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
