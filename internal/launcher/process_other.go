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
//
// A process that has exited and not yet been collected by its parent answers
// the signal too: the kernel keeps its entry, and nothing else of it, until
// the parent waits for it. It holds no port and no file any more, so for a
// stop it has ended. Taken for a running one, it held a stop through every one
// of its waits, and the stop then reported that the server would not end.
func processExited(pid int) bool {
	process, err := os.FindProcess(pid)
	if err != nil {
		return true
	}
	defer process.Release()
	err = process.Signal(syscall.Signal(0))
	if errors.Is(err, os.ErrProcessDone) || errors.Is(err, syscall.ESRCH) {
		return true
	}
	return processUncollected(pid)
}
