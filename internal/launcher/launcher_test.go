package launcher

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
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
		fmt.Fprint(writer, `{"server":"wfeature","profile":"release","version":"9.9.9","pid":4242,"https_port":11641}`)
	})}
	go func() { _ = server.Serve(listener) }()
	defer server.Close()

	report := Query(context.Background(), port)
	if report.State != Ours {
		t.Fatalf("Query() = %v, want Ours", report.State)
	}
	if report.PID != 4242 || report.Profile != "release" || report.Version != "9.9.9" || report.SecurePort != 11641 {
		t.Fatalf("report = %+v", report)
	}
	if report.SecureURL() != "https://127.0.0.1:11641" {
		t.Errorf("SecureURL() = %q", report.SecureURL())
	}
	if (Report{Port: port}).SecureURL() != "" || (Report{Port: port}).SecureLANURL() != "" {
		t.Error("a server without HTTPS has an HTTPS address")
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

// **The address printed for another device is the one the route leaves by.**
// The first routable address of the interface listing was printed, and on
// Windows a virtual adapter's (WSL's, Hyper-V's) is often listed first: no
// phone reaches it, it changes at every boot, and the HTTPS certificate, which
// has to name it, was made again at every start. A tunnel's address is not on
// the local network either. Without a route, or with one through a tunnel,
// the listing decides, IPv4 first.
func TestTheLANAddressIsTheOneTheRouteLeavesBy(t *testing.T) {
	address := func(text string) net.IP { return net.ParseIP(text) }
	virtual, wifi := address("172.26.16.1").To4(), address("192.168.0.60").To4()
	stable, temporary := address("2001:db8:1::5"), address("2001:db8:1::a1b2")
	for _, probe := range []struct {
		name             string
		candidates       []net.IP
		routed4, routed6 net.IP
		want             string
	}{
		{"a virtual adapter listed first", []net.IP{virtual, wifi}, wifi, nil, "192.168.0.60"},
		{"no route", []net.IP{virtual, wifi}, nil, nil, "172.26.16.1"},
		{"a route through a tunnel", []net.IP{wifi}, address("100.90.13.106"), nil, "192.168.0.60"},
		{"IPv4 before an IPv6 route", []net.IP{stable, wifi}, nil, stable, "192.168.0.60"},
		{"IPv6 only", []net.IP{temporary, stable}, nil, stable, "2001:db8:1::5"},
		{"IPv6 only, no route", []net.IP{temporary, stable}, nil, nil, "2001:db8:1::a1b2"},
		{"nothing reachable", nil, wifi, nil, ""},
	} {
		if got := pickLANAddress(probe.candidates, probe.routed4, probe.routed6); got != probe.want {
			t.Errorf("%s: pickLANAddress = %q, want %q", probe.name, got, probe.want)
		}
	}

	network := func(text string) net.Addr {
		ip, block, err := net.ParseCIDR(text)
		if err != nil {
			t.Fatal(err)
		}
		return &net.IPNet{IP: ip, Mask: block.Mask}
	}
	listed := []net.Addr{network("fe80::1/64"), network("192.168.0.60/24"), network("2001:db8:1::5/64")}
	if got := lanAddresses(net.FlagUp|net.FlagBroadcast, listed); len(got) != 2 || !got[0].Equal(wifi) || !got[1].Equal(stable) {
		t.Errorf("an interface's reachable addresses = %v, want the IPv4 and global IPv6 ones", got)
	}
	for _, flags := range []net.Flags{0, net.FlagUp | net.FlagLoopback, net.FlagUp | net.FlagPointToPoint} {
		if got := lanAddresses(flags, listed); got != nil {
			t.Errorf("an interface flagged %v offered %v", flags, got)
		}
	}
}
