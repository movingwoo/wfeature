package main

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A socket path has to be short: the sockaddr_un a bind writes into is 104
// bytes on macOS and 108 on Linux, and a test's own temporary directory is
// most of that on its own. So these bind under the shortest directory the
// platform offers rather than under t.TempDir().
func socketPath(t *testing.T) string {
	t.Helper()
	directory, err := os.MkdirTemp("", "wf")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(directory) })
	return filepath.Join(directory, "s.sock")
}

// `-version` is what a user is asked for when a report says "which build?", so
// it has to survive a pipe: the CI smoke job greps it, and so does anyone with
// a shell. The log keeps its own stream, or the answer arrives mixed into the
// startup lines of a server that never started.
func TestTheVersionIsPrintedWhereAPipeCanReadIt(t *testing.T) {
	answerRead, answerWrite, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	outputRead, outputWrite, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}

	if err := run([]string{"-version"}, answerWrite, outputWrite); err != nil {
		t.Fatalf("-version: %v", err)
	}
	_ = answerWrite.Close()
	_ = outputWrite.Close()

	answer, err := io.ReadAll(answerRead)
	if err != nil {
		t.Fatal(err)
	}
	noise, err := io.ReadAll(outputRead)
	if err != nil {
		t.Fatal(err)
	}

	// The shape the smoke job greps for, whatever this build is stamped with.
	if !strings.HasPrefix(string(answer), "wfeature-server ") || !strings.Contains(string(answer), "(") {
		t.Errorf("the answer was %q, want a `wfeature-server <version> (<profile>)` line", answer)
	}
	if len(noise) != 0 {
		t.Errorf("the log stream carried %q, want the version on its own stream", noise)
	}
}

func TestListenOnAPortIsUnchanged(t *testing.T) {
	listener, err := listen("127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen on a port: %v", err)
	}
	defer listener.Close()

	if network := listener.Addr().Network(); network != "tcp" {
		t.Errorf("network = %q, want tcp", network)
	}
	if url := serverURL(listener); !strings.HasPrefix(url, "http://") {
		t.Errorf("startup url = %q, want an http:// address", url)
	}
}

// The whole point of the socket is that a reverse proxy can serve the page
// through it, so this drives the real server over one rather than testing the
// listener alone.
func TestListenOnASocketServesTheSameServer(t *testing.T) {
	path := socketPath(t)
	listener, err := listen(unixPrefix + path)
	if err != nil {
		t.Fatalf("listen on a socket: %v", err)
	}
	if network := listener.Addr().Network(); network != "unix" {
		t.Fatalf("network = %q, want unix", network)
	}
	if url := serverURL(listener); url != unixPrefix+path {
		t.Errorf("startup url = %q, want the socket path", url)
	}

	// What a proxy does: a file mode it can reach through its group, and no
	// port to point at.
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if mode := info.Mode().Perm(); mode != socketMode {
		t.Errorf("socket mode = %04o, want %04o", mode, socketMode)
	}

	server := &http.Server{Handler: http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(writer, "wfeature")
	})}
	go func() { _ = server.Serve(listener) }()
	defer func() { _ = server.Shutdown(context.Background()) }()

	client := &http.Client{Transport: &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", path)
		},
	}}
	// The host in the URL is ignored by the dialer above; it is the Host header
	// a proxy would set.
	response, err := client.Get("http://wfeature.local/")
	if err != nil {
		t.Fatalf("get over the socket: %v", err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK || string(body) != "wfeature" {
		t.Fatalf("answered %d %q over the socket", response.StatusCode, body)
	}
}

func TestASocketIsRemovedWhenTheListenerCloses(t *testing.T) {
	// A server that shuts down cleanly must not leave the next start to trip
	// over its socket.
	path := socketPath(t)
	listener, err := listen(unixPrefix + path)
	if err != nil {
		t.Fatal(err)
	}
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("the socket file outlived its listener: %v", err)
	}
}

func TestAStaleSocketIsReplaced(t *testing.T) {
	// A kill or a power cut leaves the file behind, and binding onto it fails
	// with "address already in use" — which reads exactly like a port conflict
	// and is not one. Starting again has to work.
	path := socketPath(t)
	first, err := listen(unixPrefix + path)
	if err != nil {
		t.Fatal(err)
	}
	// Close the listener without letting Go unlink the file, which is the state
	// a killed process leaves behind.
	unix, ok := first.(*net.UnixListener)
	if !ok {
		t.Fatalf("listener is %T, want *net.UnixListener", first)
	}
	unix.SetUnlinkOnClose(false)
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("the stale socket was not left behind: %v", err)
	}

	second, err := listen(unixPrefix + path)
	if err != nil {
		t.Fatalf("listen over a stale socket: %v", err)
	}
	_ = second.Close()
}

func TestALiveSocketIsNotTakenOver(t *testing.T) {
	// The other half of clearing a stale socket: a socket someone is serving is
	// not this server's to delete. Two servers on one path would otherwise
	// leave the second one holding a socket the first still thinks it owns.
	path := socketPath(t)
	first, err := listen(unixPrefix + path)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	// Something has to be accepting, or the connection that proves it is alive
	// is refused.
	go func() {
		for {
			connection, err := first.Accept()
			if err != nil {
				return
			}
			_ = connection.Close()
		}
	}()
	// Give the accept loop a moment to be waiting.
	time.Sleep(50 * time.Millisecond)

	second, err := listen(unixPrefix + path)
	if err == nil {
		_ = second.Close()
		t.Fatal("a second server took over a socket that was being served")
	}
	if !strings.Contains(err.Error(), "already serving") {
		t.Errorf("error = %q, want it to say the socket is already served", err)
	}
	// The first server is still there.
	if _, err := os.Stat(path); err != nil {
		t.Errorf("the live socket was removed: %v", err)
	}
}

func TestASocketPathIsRequiredAndMustBeASocket(t *testing.T) {
	if _, err := listen(unixPrefix); err == nil {
		t.Error("an empty socket path was accepted")
	}

	// A path that is an ordinary file is a mistake in the argument rather than
	// a leftover, so it is reported instead of deleted.
	path := socketPath(t)
	if err := os.WriteFile(path, []byte("not a socket"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := listen(unixPrefix + path)
	if err == nil {
		t.Fatal("an ordinary file was accepted as a socket path")
	}
	if !strings.Contains(err.Error(), "not a socket") {
		t.Errorf("error = %q, want it to say the path is not a socket", err)
	}
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		t.Error("the file was deleted rather than reported")
	}
}
