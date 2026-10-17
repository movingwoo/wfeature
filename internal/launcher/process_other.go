//go:build !windows

package launcher

import (
	"errors"
	"os"
	"syscall"
)

// processExited reports whether the process a server named has ended. Signal
// zero delivers nothing; it only asks whether the pid is there. A process
// owned by another user answers that it may not be signalled, which is still
// a process.
func processExited(pid int) bool {
	process, err := os.FindProcess(pid)
	if err != nil {
		return true
	}
	defer process.Release()
	err = process.Signal(syscall.Signal(0))
	return errors.Is(err, os.ErrProcessDone) || errors.Is(err, syscall.ESRCH)
}
