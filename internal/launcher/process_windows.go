//go:build windows

package launcher

import (
	"errors"

	"golang.org/x/sys/windows"
)

// processExited reports whether the process a server named has ended. A
// process that has gone cannot be opened, but one that has ended while
// something still holds a handle to it can, so the handle is asked whether
// it is signalled — which a process is from the moment it ends — rather than
// trusted for having opened.
func processExited(pid int) bool {
	handle, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(pid))
	if err != nil {
		// A process this may not open is still a process.
		return !errors.Is(err, windows.ERROR_ACCESS_DENIED)
	}
	defer windows.CloseHandle(handle)
	event, err := windows.WaitForSingleObject(handle, 0)
	return err == nil && event == windows.WAIT_OBJECT_0
}
