// Package launcher answers the two questions a user asks about a server they
// are not looking at: is it running, and stop it.
//
// Both used to be shell — three copies of it, one per operating system, each
// finding the process behind a port with whichever tool that system had
// (`lsof`, `ss`, `fuser`, `netstat`) and then guessing from a process name
// whether it was ours. None of that is needed once the server answers for
// itself: /api/status says what it is and which process it is, and
// /api/shutdown stops it the way closing its window does. What is left is one
// implementation that behaves the same everywhere.
//
// Nothing here acts on a port alone. A stranger holding the port is reported
// and left alone, which is the rule the scripts had and the reason they asked
// twice before killing anything.
package launcher

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"runtime"
	"strconv"
	"syscall"
	"time"
)

// DefaultPort is the port the page, the documentation and every launcher use.
const DefaultPort = 11541

// askTimeout bounds one question to the server. It is short because the answer
// is local and a server too busy to reply within it is answered for by the
// fallbacks below rather than by waiting.
const askTimeout = 2 * time.Second

// drainWait is how long a stop waits for the server to finish what it was
// doing — a save being written is the case that matters — before it stops
// asking politely.
const drainWait = 10 * time.Second

// State is what was found on a port.
type State int

const (
	// Free means nothing is listening.
	Free State = iota
	// Ours means a wfeature server answered.
	Ours
	// Foreign means something is listening and it is not this server.
	Foreign
)

// Reason says why a port was called Foreign, so that a caller can put it in
// its own words rather than repeat this package's.
type Reason int

const (
	// NoReason is a port that is free or ours.
	NoReason Reason = iota
	// Silent means the connection opened and nothing usable came back.
	Silent
	// OtherServer means something answered, and it was not this server.
	OtherServer
)

// Report is one port's answer.
type Report struct {
	Port    int
	State   State
	PID     int
	Profile string
	Version string
	// Reason explains a Foreign result.
	Reason Reason
	// Detail is the technical half of Reason — an HTTP status, a transport
	// error — for a log or an error message rather than for a user.
	Detail string
}

// URL is the address to open in a browser.
func (r Report) URL() string { return fmt.Sprintf("http://127.0.0.1:%d", r.Port) }

// LANURL is the address another device on the same network reaches this
// server at, or empty when there is no such address to give. It is the whole
// point of running a server rather than a desktop application, and a user who
// has to find their own address usually does not.
func (r Report) LANURL() string {
	address := lanAddress()
	if address == "" {
		return ""
	}
	return fmt.Sprintf("http://%s", net.JoinHostPort(address, strconv.Itoa(r.Port)))
}

// lanAddress picks this machine's address on the local network: the first
// routable one, preferring IPv4 because that is what a person can retype.
func lanAddress() string {
	interfaces, err := net.Interfaces()
	if err != nil {
		return ""
	}
	fallback := ""
	for _, candidate := range interfaces {
		if candidate.Flags&net.FlagUp == 0 || candidate.Flags&net.FlagLoopback != 0 {
			continue
		}
		addresses, err := candidate.Addrs()
		if err != nil {
			continue
		}
		for _, entry := range addresses {
			network, ok := entry.(*net.IPNet)
			if !ok || network.IP.IsLoopback() || network.IP.IsLinkLocalUnicast() {
				continue
			}
			if four := network.IP.To4(); four != nil {
				return four.String()
			}
			if fallback == "" {
				fallback = network.IP.String()
			}
		}
	}
	return fallback
}

// statusPayload is what /api/status answers with.
type statusPayload struct {
	Server  string `json:"server"`
	Profile string `json:"profile"`
	Version string `json:"version"`
	PID     int    `json:"pid"`
}

// Query asks one port what is behind it.
//
// A refused connection is a free port. A connection that opens but does not
// answer as this server is a stranger, and saying which of the two it is
// matters: one is a port to use and the other is a port to leave alone.
func Query(ctx context.Context, port int) Report {
	report := Report{Port: port}
	address := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))

	dialContext, cancel := context.WithTimeout(ctx, askTimeout)
	defer cancel()
	connection, err := (&net.Dialer{}).DialContext(dialContext, "tcp", address)
	if err != nil {
		return report
	}
	_ = connection.Close()

	requestContext, cancelRequest := context.WithTimeout(ctx, askTimeout)
	defer cancelRequest()
	request, err := http.NewRequestWithContext(requestContext, http.MethodGet,
		"http://"+address+"/api/status", nil)
	if err != nil {
		report.State = Foreign
		report.Reason = Silent
		report.Detail = err.Error()
		return report
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		report.State = Foreign
		report.Reason = Silent
		report.Detail = "it did not answer /api/status"
		return report
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		report.State = Foreign
		report.Reason = OtherServer
		report.Detail = fmt.Sprintf("it answered /api/status with %s", response.Status)
		return report
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, 4096))
	if err != nil {
		report.State = Foreign
		report.Reason = Silent
		report.Detail = "its answer could not be read"
		return report
	}
	var payload statusPayload
	if err := json.Unmarshal(body, &payload); err != nil || payload.Server != "wfeature" {
		report.State = Foreign
		report.Reason = OtherServer
		report.Detail = "its answer is not this server's"
		return report
	}
	report.State = Ours
	report.PID = payload.PID
	report.Profile = payload.Profile
	report.Version = payload.Version
	return report
}

// ErrNotOurs is returned when a port is held by something else. What the
// caller reports is up to it; what it must not do is stop it.
var ErrNotOurs = errors.New("the port is held by something that is not a wfeature server")

// Outcome says how a stop ended, so that the caller can tell a user what
// happened in the caller's own words.
type Outcome int

const (
	// AlreadyStopped means nothing was on the port.
	AlreadyStopped Outcome = iota
	// Drained means the server was asked and stopped itself, finishing what
	// it was writing.
	Drained
	// Signalled means the request did not work and the process was asked
	// through a signal instead.
	Signalled
	// Killed means neither worked and the process was ended outright.
	Killed
	// Refused means the port is somebody else's and nothing was done.
	Refused
)

// Stop ends the server on a port and says how it went.
//
// The polite path is the whole of it in the ordinary case: the server is asked
// to stop and drains itself, finishing a save that was being written. The
// fallbacks exist for a server that cannot answer — an older build without the
// route, or one wedged badly enough not to serve — and they are the same two
// steps the shell scripts took, in the same order.
func Stop(ctx context.Context, port int) (Report, Outcome, error) {
	report := Query(ctx, port)
	switch report.State {
	case Free:
		return report, AlreadyStopped, nil
	case Foreign:
		return report, Refused, ErrNotOurs
	}

	if err := requestShutdown(ctx, port); err == nil {
		if waitUntilStopped(ctx, port, report.PID, drainWait) {
			return report, Drained, nil
		}
	}

	// The server did not take the request, or took it and did not go. Signal
	// the process it told us it is.
	if report.PID <= 0 {
		return report, Refused, fmt.Errorf(
			"the server on port %d did not stop and did not say which process it is", port)
	}
	process, err := os.FindProcess(report.PID)
	if err != nil {
		return report, Refused, fmt.Errorf("find pid %d: %w", report.PID, err)
	}
	if err := terminate(process); err == nil && waitUntilStopped(ctx, port, report.PID, drainWait) {
		return report, Signalled, nil
	}

	if err := process.Kill(); err != nil {
		return report, Refused, fmt.Errorf("stop pid %d: %w", report.PID, err)
	}
	if !waitUntilStopped(ctx, port, report.PID, drainWait) {
		return report, Refused, fmt.Errorf(
			"the process on port %d is still there after being killed", port)
	}
	return report, Killed, nil
}

// requestShutdown asks the server to stop itself.
func requestShutdown(ctx context.Context, port int) error {
	requestContext, cancel := context.WithTimeout(ctx, askTimeout)
	defer cancel()
	address := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	request, err := http.NewRequestWithContext(requestContext, http.MethodPost,
		"http://"+address+"/api/shutdown", nil)
	if err != nil {
		return err
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
	if response.StatusCode != http.StatusAccepted {
		return fmt.Errorf("the server answered the stop with %s", response.Status)
	}
	return nil
}

// terminate asks a process to end the way Ctrl-C does. Windows has no such
// signal for a process that is not in this console's group, so there the only
// polite step is the one the server already took through its own route, and
// this reports that there is nothing left to try.
func terminate(process *os.Process) error {
	if runtime.GOOS == "windows" {
		return errors.New("windows has no terminate signal for another console")
	}
	return process.Signal(syscall.SIGTERM)
}

// waitUntilStopped polls until the server that was on the port is gone: the
// port went quiet, or whatever holds it now is not that process.
//
// The second half matters more than it looks. A machine that keeps a server
// running usually has something ready to take the port back — the other
// profile, a supervisor, the developer's next run — and a port that is busy
// again is not the same thing as a server that refused to stop. Waiting on the
// port alone reported a successful stop as a failure and then went on to
// signal a process that had already exited.
func waitUntilStopped(ctx context.Context, port, pid int, within time.Duration) bool {
	deadline := time.Now().Add(within)
	for {
		switch report := Query(ctx, port); {
		case report.State == Free:
			return true
		case report.State == Ours && pid > 0 && report.PID != pid:
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		select {
		case <-ctx.Done():
			return false
		case <-time.After(100 * time.Millisecond):
		}
	}
}

// WaitUntilServing polls a port until this server answers on it, which is how
// a launcher knows the page is worth opening. It answers false if the wait ran
// out or something else took the port.
func WaitUntilServing(ctx context.Context, port int, within time.Duration) (Report, bool) {
	deadline := time.Now().Add(within)
	for {
		report := Query(ctx, port)
		if report.State == Ours {
			return report, true
		}
		if time.Now().After(deadline) {
			return report, false
		}
		select {
		case <-ctx.Done():
			return report, false
		case <-time.After(100 * time.Millisecond):
		}
	}
}
