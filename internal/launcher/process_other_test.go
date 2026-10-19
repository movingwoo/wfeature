//go:build !windows

package launcher

import (
	"os"
	"os/exec"
	"testing"
	"time"
)

// A server that has exited before its parent waited for it is a zombie until
// the parent does: its pid still answers signal zero. It holds no port and no
// file any more, so a stop must count it as ended. Taken for a running one, a
// stop waited out every one of its three bounds (drainWait each), signalled
// and killed it, and then reported that the server would not stop.
func TestAnExitedProcessNotYetCollectedHasEnded(t *testing.T) {
	if !processStateKnown {
		t.Skip("this system is not asked about a process's state; a pid that answers is taken for a running process")
	}
	// The test binary with no test to run exits at once.
	command := exec.Command(os.Args[0], "-test.run=^$")
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	collected := false
	t.Cleanup(func() {
		if !collected {
			_ = command.Wait()
		}
	})
	pid := command.Process.Pid
	deadline := time.Now().Add(10 * time.Second)
	for !processUncollected(pid) {
		if time.Now().After(deadline) {
			t.Fatalf("pid %d did not become a process awaiting collection within 10s", pid)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !processExited(pid) {
		t.Fatalf("pid %d has exited, awaiting collection, and is reported running", pid)
	}
	collected = true
	if err := command.Wait(); err != nil {
		t.Fatalf("the child: %v", err)
	}
	if !processExited(pid) {
		t.Fatalf("pid %d is collected and is reported running", pid)
	}
}

// A process that is running is not one that has ended, whatever its state.
func TestARunningProcessHasNotEnded(t *testing.T) {
	if processExited(os.Getpid()) || processUncollected(os.Getpid()) {
		t.Fatal("this test's own process is reported ended")
	}
}
