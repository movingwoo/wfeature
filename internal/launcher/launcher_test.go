package launcher

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// listenLocal takes a port from the operating system, so a test never fights
// a real server for the one a user runs on.
func listenLocal(t *testing.T) (net.Listener, int) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	_, portText, err := net.SplitHostPort(listener.Addr().String())
	if err != nil {
		t.Fatalf("split %q: %v", listener.Addr(), err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatalf("port %q: %v", portText, err)
	}
	return listener, port
}

func TestQueryAnswersAnEmptyPort(t *testing.T) {
	listener, port := listenLocal(t)
	// Closing it hands back a port nothing is on, which is the case a
	// launcher has to tell apart from a stranger holding it.
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	report := Query(context.Background(), port)
	if report.State != Free {
		t.Fatalf("Query() = %v, want Free", report.State)
	}
}

func TestQueryRecognisesThisServer(t *testing.T) {
	listener, port := listenLocal(t)
	server := &http.Server{Handler: http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/api/status" {
			http.NotFound(writer, request)
			return
		}
		fmt.Fprint(writer, `{"server":"wfeature","profile":"release","version":"9.9.9","pid":4242}`)
	})}
	go func() { _ = server.Serve(listener) }()
	defer server.Close()

	report := Query(context.Background(), port)
	if report.State != Ours {
		t.Fatalf("Query() = %v, want Ours", report.State)
	}
	if report.PID != 4242 || report.Profile != "release" || report.Version != "9.9.9" {
		t.Fatalf("report = %+v", report)
	}
	if report.URL() != fmt.Sprintf("http://127.0.0.1:%d", port) {
		t.Errorf("URL() = %q", report.URL())
	}
}

// A port is not proof of identity. Something else answering there has to be
// reported and left alone, which is the rule the shell scripts had and the one
// worth keeping: the alternative is stopping a stranger's program.
func TestQueryRefusesToClaimAStranger(t *testing.T) {
	cases := map[string]http.HandlerFunc{
		"another server": func(writer http.ResponseWriter, _ *http.Request) {
			http.Error(writer, "not found", http.StatusNotFound)
		},
		"another JSON API": func(writer http.ResponseWriter, _ *http.Request) {
			fmt.Fprint(writer, `{"server":"something-else"}`)
		},
		"not HTTP at all": nil,
	}
	for name, handler := range cases {
		t.Run(name, func(t *testing.T) {
			listener, port := listenLocal(t)
			defer listener.Close()
			if handler == nil {
				// A listener that accepts and says nothing: the dial succeeds
				// and the request times out.
				go func() {
					for {
						connection, err := listener.Accept()
						if err != nil {
							return
						}
						_ = connection
					}
				}()
			} else {
				server := &http.Server{Handler: handler}
				go func() { _ = server.Serve(listener) }()
				defer server.Close()
			}
			report := Query(context.Background(), port)
			if report.State != Foreign {
				t.Fatalf("Query() = %v, want Foreign", report.State)
			}
			if report.Detail == "" {
				t.Error("a foreign port was reported without saying why")
			}
		})
	}
}

func TestStopAsksTheServerToStopItself(t *testing.T) {
	listener, port := listenLocal(t)
	stopped := make(chan struct{})
	var server *http.Server
	server = &http.Server{Handler: http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/api/status":
			fmt.Fprint(writer, `{"server":"wfeature","profile":"release","version":"dev","pid":1}`)
		case "/api/shutdown":
			if request.Method != http.MethodPost {
				http.Error(writer, "method", http.StatusMethodNotAllowed)
				return
			}
			writer.WriteHeader(http.StatusAccepted)
			// Shutdown rather than Close, because that is what the real server
			// does and the difference is the whole test: Close tears down the
			// connection this reply is still travelling on, so the stop request
			// fails, Stop skips the drain it would have won, and falls through
			// to signalling the pid the server named — which here is 1.
			go func() {
				_ = server.Shutdown(context.Background())
				close(stopped)
			}()
		default:
			http.NotFound(writer, request)
		}
	})}
	go func() { _ = server.Serve(listener) }()

	report, outcome, err := Stop(context.Background(), port)
	if err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	if outcome != Drained {
		t.Errorf("outcome = %v, want Drained: the server should stop itself", outcome)
	}
	if report.PID != 1 {
		t.Errorf("report.PID = %d, want the pid the server named", report.PID)
	}
	select {
	case <-stopped:
	case <-time.After(2 * time.Second):
		t.Fatal("the server was never asked to stop")
	}
}

// TestSignallableRefusesWhatIsNotAServer covers the number Stop is about to
// signal. It is checked here rather than through Stop for the case that
// matters most: a test that drove Stop with this process's own pid would, the
// day the check regressed, kill the run that was meant to report it.
func TestSignallableRefusesWhatIsNotAServer(t *testing.T) {
	for _, testCase := range []struct {
		name string
		pid  int
		want bool
	}{
		{name: "init", pid: 1, want: false},
		{name: "the process asking", pid: os.Getpid(), want: false},
		{name: "nothing", pid: 0, want: false},
		{name: "a negative pid", pid: -1, want: false},
		{name: "another process", pid: os.Getpid() + 1, want: true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			if got := signallable(testCase.pid); got != testCase.want {
				t.Errorf("signallable(%d) = %v, want %v", testCase.pid, got, testCase.want)
			}
		})
	}
}

// TestStopRefusesTheServerThatNamesInit is the same refusal through Stop, on
// the one pid a run can drive without risking itself.
func TestStopRefusesTheServerThatNamesInit(t *testing.T) {
	listener, port := listenLocal(t)
	// A server that answers as ours and then refuses to stop is what puts Stop
	// on the signalling path at all.
	server := &http.Server{Handler: http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/api/status" {
			fmt.Fprint(writer, `{"server":"wfeature","profile":"release","version":"dev","pid":1}`)
			return
		}
		http.Error(writer, "no", http.StatusInternalServerError)
	})}
	go func() { _ = server.Serve(listener) }()
	defer func() { _ = server.Close() }()

	report, outcome, err := Stop(context.Background(), port)
	if err == nil {
		t.Fatal("a server naming init was believed")
	}
	if outcome != Refused {
		t.Errorf("outcome = %v, want Refused", outcome)
	}
	if report.PID != 1 {
		t.Errorf("report.PID = %d, want the 1 the server named", report.PID)
	}
	// The refusal has to be the check rather than a kill that happened to
	// fail, because a kill that happened to succeed is the thing being avoided.
	if !strings.Contains(err.Error(), "not a process to stop") {
		t.Errorf("error = %v, want the refusal rather than a failed signal", err)
	}
}

// Stopping is the one thing that must never act on a port alone.
func TestStopLeavesAStrangerAlone(t *testing.T) {
	stranger := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		http.Error(writer, "not found", http.StatusNotFound)
	}))
	defer stranger.Close()
	_, portText, err := net.SplitHostPort(strings.TrimPrefix(stranger.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatal(err)
	}

	report, outcome, err := Stop(context.Background(), port)
	if !errors.Is(err, ErrNotOurs) {
		t.Fatalf("Stop() error = %v, want ErrNotOurs", err)
	}
	if outcome != Refused {
		t.Errorf("outcome = %v, want Refused", outcome)
	}
	if report.Detail == "" {
		t.Error("a refusal was reported without saying what is on the port")
	}
	// And it is still there.
	if response, err := http.Get(stranger.URL); err == nil {
		response.Body.Close()
	} else {
		t.Fatalf("the stranger was stopped: %v", err)
	}
}

func TestStopOnAnEmptyPortSaysSo(t *testing.T) {
	listener, port := listenLocal(t)
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	_, outcome, err := Stop(context.Background(), port)
	if err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	if outcome != AlreadyStopped {
		t.Errorf("outcome = %v, want AlreadyStopped", outcome)
	}
}

func TestWaitUntilServingReturnsWhenTheServerAnswers(t *testing.T) {
	listener, port := listenLocal(t)
	server := &http.Server{Handler: http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(writer, `{"server":"wfeature","profile":"debug","version":"dev","pid":7}`)
	})}
	// Nothing answers for a moment, which is the window a launcher used to
	// sleep through before opening the page.
	go func() {
		time.Sleep(150 * time.Millisecond)
		_ = server.Serve(listener)
	}()
	defer server.Close()

	report, serving := WaitUntilServing(context.Background(), port, 3*time.Second)
	if !serving {
		t.Fatalf("WaitUntilServing() = %v, %v", report, serving)
	}
	if report.URL() != fmt.Sprintf("http://127.0.0.1:%d", port) {
		t.Errorf("URL() = %q", report.URL())
	}
}

func TestWaitUntilServingGivesUp(t *testing.T) {
	listener, port := listenLocal(t)
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	if _, serving := WaitUntilServing(context.Background(), port, 300*time.Millisecond); serving {
		t.Fatal("WaitUntilServing() answered true for a port with nothing on it")
	}
}

// A stop is finished when the server that was there is gone, which is not the
// same as the port being quiet: on a machine that keeps a server running,
// something is usually ready to take the port back. Waiting on the port alone
// reported a stop that worked as a failure, and then signalled a process that
// had already exited.
func TestStopAcceptsAnotherServerTakingThePort(t *testing.T) {
	listener, port := listenLocal(t)
	var pid atomic.Int64
	pid.Store(4242)
	server := &http.Server{Handler: http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/api/status":
			fmt.Fprintf(writer, `{"server":"wfeature","profile":"release","version":"dev","pid":%d}`, pid.Load())
		case "/api/shutdown":
			writer.WriteHeader(http.StatusAccepted)
			// The successor is this same listener answering as a different
			// process, which is what a restart looks like from outside.
			pid.Store(5353)
		default:
			http.NotFound(writer, request)
		}
	})}
	go func() { _ = server.Serve(listener) }()
	defer server.Close()

	report, outcome, err := Stop(context.Background(), port)
	if err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	if outcome != Drained {
		t.Errorf("outcome = %v, want Drained", outcome)
	}
	if report.PID != 4242 {
		t.Errorf("report.PID = %d, want the server that was asked to stop", report.PID)
	}
}
