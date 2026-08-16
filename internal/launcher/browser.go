package launcher

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
)

// errNoDisplay is why nothing was opened on a machine with no session to open
// it in. A caller that was only being helpful ignores it.
var errNoDisplay = errors.New("there is no desktop session to open a page in")

// OpenBrowser shows a page in whatever the user browses with.
//
// It exists because of how a release is used: the launcher is double-clicked,
// and a server with no page in front of it looks like nothing happened. The
// launchers used to do this in shell — `open` on macOS, `xdg-open` on Linux,
// `start` on Windows — after sleeping a second and hoping the listener was up
// by then. Here the caller opens the page once the port has answered, so the
// sleep is gone with the three spellings.
func OpenBrowser(url string) error {
	var command *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		command = exec.Command("open", url)
	case "linux":
		// A Linux box is as likely to be a headless server reached over SSH as
		// it is a desktop, and there is nothing there to open a page in.
		if os.Getenv("DISPLAY") == "" && os.Getenv("WAYLAND_DISPLAY") == "" {
			return errNoDisplay
		}
		command = exec.Command("xdg-open", url)
	case "windows":
		// rundll32 rather than `start`, which is a shell builtin and would
		// need a shell to interpret it.
		command = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		command = exec.Command("xdg-open", url)
	}
	if err := command.Start(); err != nil {
		return fmt.Errorf("open %s: %w", url, err)
	}
	// The browser outlives this process and is not waited for; releasing the
	// child is all that is left to do.
	go func() { _ = command.Wait() }()
	return nil
}
