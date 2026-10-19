//go:build !windows

package main

import (
	"bufio"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// serverHelperArguments carries the helper process's arguments, separated by
// a character no argument here contains.
const serverHelperArguments = "WFEATURE_SERVER_HELPER_ARGS"

// TestServerHelperProcess is the server itself, run as a child of the test
// below; it does nothing in an ordinary test run.
func TestServerHelperProcess(t *testing.T) {
	arguments, ok := os.LookupEnv(serverHelperArguments)
	if !ok {
		t.Skip("run only as a child of a test")
	}
	if err := run(strings.Split(arguments, "\x1f"), os.Stdout, os.Stderr); err != nil {
		os.Exit(1)
	}
	os.Exit(0)
}

// The Android app ends its server with Process.destroy(), which sends SIGTERM
// and then closes the pipe the server logs to. The server's next log line,
// "shutting down", then met a closed pipe, and a write to a broken standard
// error ends a Go program with SIGPIPE: the drain that closes every game, and
// with it the games' last saves, never ran. A server whose log is gone must
// still drain and exit as asked.
//
// Closing the window a server runs in — the way the release archives'
// READMEs tell a player to stop it — hangs up its terminal, and the server
// receives SIGHUP. Unhandled, that ended the process on the spot, without the
// drain: a game whose tab had been closed waits parked until the server stops,
// and what its title had not yet stored went with the process. The window is
// gone by then, so the log has nowhere to go either.
func TestASignalAfterTheLogPipeClosedDrainsTheServer(t *testing.T) {
	for _, signal := range []syscall.Signal{syscall.SIGTERM, syscall.SIGHUP} {
		t.Run(signal.String(), func(t *testing.T) {
			command, logs, _ := startServerHelper(t, false)
			// The pipe goes first, so the server meets it closed on its next
			// line.
			logs.Close()
			if err := command.Process.Signal(signal); err != nil {
				t.Fatal(err)
			}
			wantDrained(t, command, signal.String())
		})
	}
}

// A server started with its hangup ignored — `nohup ./wfeature-server &` over
// SSH on a headless machine, so that it outlives the login — keeps it ignored.
// Asking for the signal un-ignored it, and logging out stopped the server.
func TestAHangupIgnoredAtTheStartLeavesTheServerUp(t *testing.T) {
	command, _, url := startServerHelper(t, true)
	if err := command.Process.Signal(syscall.SIGHUP); err != nil {
		t.Fatal(err)
	}
	time.Sleep(time.Second)
	response, err := http.Get(url + "/api/status")
	if err != nil {
		command.Process.Kill()
		command.Wait()
		t.Fatalf("the server stopped answering after a hangup it was started ignoring: %v", err)
	}
	response.Body.Close()
	if err := command.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	wantDrained(t, command, "SIGTERM")
}

// startServerHelper runs the server as a child process on a free port and
// answers once it serves, with the pipe it logs to and its address. Asked to,
// it starts the server with its hangup ignored, as `nohup` does.
func startServerHelper(t *testing.T, hangupIgnored bool) (*exec.Cmd, io.ReadCloser, string) {
	t.Helper()
	root := t.TempDir()
	arguments := strings.Join([]string{
		"-addr", "127.0.0.1:0",
		"-games", filepath.Join(root, "games"),
		"-ext", filepath.Join(root, "ext"),
		"-saves", filepath.Join(root, "savedata", "ktf"),
		"-logs", filepath.Join(root, "logs"),
	}, "\x1f")
	command := exec.Command(os.Args[0], "-test.run=^TestServerHelperProcess$")
	if hangupIgnored {
		command = exec.Command("/bin/sh", "-c", `trap '' HUP; exec "$0" -test.run='^TestServerHelperProcess$'`, os.Args[0])
	}
	command.Env = append(os.Environ(), serverHelperArguments+"="+arguments)
	logs, err := command.StderrPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	serving := make(chan string, 1)
	go func() {
		scanner := bufio.NewScanner(logs)
		for scanner.Scan() {
			line := scanner.Text()
			if !strings.Contains(line, "serving wfeature") {
				continue
			}
			for _, field := range strings.Fields(line) {
				if address, ok := strings.CutPrefix(field, "url="); ok {
					serving <- address
					return
				}
			}
		}
		serving <- ""
	}()
	var url string
	select {
	case url = <-serving:
		if url == "" {
			command.Process.Kill()
			command.Wait()
			t.Fatal("the server ended before it was serving")
		}
	case <-time.After(30 * time.Second):
		command.Process.Kill()
		command.Wait()
		t.Fatal("the server was not serving after 30s")
	}
	// An answer means the serve loop runs, and it installs the signal handler
	// before it serves.
	for attempt := 0; ; attempt++ {
		response, err := http.Get(strings.TrimSuffix(url, "/") + "/api/status")
		if err == nil {
			response.Body.Close()
			break
		}
		if attempt == 300 {
			command.Process.Kill()
			command.Wait()
			t.Fatalf("the server did not answer: %v", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	return command, logs, strings.TrimSuffix(url, "/")
}

// wantDrained waits for a server that was sent a signal to end, and fails the
// test unless it ended by its drain: not by a signal, and with exit 0.
func wantDrained(t *testing.T, command *exec.Cmd, sent string) {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()
	select {
	case <-done:
	case <-time.After(30 * time.Second):
		command.Process.Kill()
		t.Fatalf("the server did not end within 30s of %s", sent)
	}
	status := command.ProcessState.Sys().(syscall.WaitStatus)
	if status.Signaled() {
		t.Fatalf("the server was ended by %v instead of draining", status.Signal())
	}
	if status.ExitStatus() != 0 {
		t.Fatalf("the server exited with %d, want 0 after its drain", status.ExitStatus())
	}
}
