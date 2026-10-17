package launcher

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"testing"
	"time"
)

// The process the test below names as the server. It is this test binary run
// again, so the run needs nothing installed; it only has to outlive its port.
func TestHelperServerStillDraining(t *testing.T) {
	if os.Getenv("WFEATURE_LAUNCHER_DRAINING_HELPER") != "1" {
		t.Skip("run only as the draining helper process")
	}
	time.Sleep(1500 * time.Millisecond)
}

// A server that was asked to stop closes its listener first and then closes
// its games, writing their saves, for up to its own drain deadline. A free
// port is therefore not a finished stop: the process is still holding the
// save directories, and a start that follows straight away runs beside it.
// Stop waits for the process the server named, not only for the port.
func TestStopWaitsForTheServerProcessNotOnlyItsPort(t *testing.T) {
	helper := exec.Command(os.Args[0], "-test.run=^TestHelperServerStillDraining$")
	helper.Env = append(os.Environ(), "WFEATURE_LAUNCHER_DRAINING_HELPER=1")
	if err := helper.Start(); err != nil {
		t.Fatalf("start the helper process: %v", err)
	}
	exited := make(chan struct{})
	go func() {
		_ = helper.Wait()
		close(exited)
	}()
	t.Cleanup(func() {
		_ = helper.Process.Kill()
		<-exited
	})

	listener, port := listenLocal(t)
	var server *http.Server
	server = &http.Server{Handler: http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/api/status":
			fmt.Fprintf(writer, `{"server":"wfeature","profile":"release","version":"dev","pid":%d}`, helper.Process.Pid)
		case "/api/shutdown":
			writer.WriteHeader(http.StatusAccepted)
			go func() { _ = server.Shutdown(context.Background()) }()
		default:
			http.NotFound(writer, request)
		}
	})}
	go func() { _ = server.Serve(listener) }()

	started := time.Now()
	_, outcome, err := Stop(context.Background(), port)
	took := time.Since(started)
	if err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	if outcome != Drained {
		t.Fatalf("outcome = %v, want Drained", outcome)
	}
	// The process is reaped by the goroutine above, which is what lets the
	// stop see it gone, so it is gone or a moment from it by now.
	select {
	case <-exited:
	case <-time.After(time.Second):
		t.Fatalf("Stop reported Drained after %v while the server process was still running",
			took.Round(time.Millisecond))
	}
}
