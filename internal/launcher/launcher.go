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
	"slices"
	"strconv"
	"syscall"
	"time"

	"github.com/movingwoo/wfeature/internal/webhost"
)

// DefaultPort is the port the page, the documentation and every launcher use.
const DefaultPort = 11541

// askTimeout bounds one question to the server. It is short because the answer
// is local and a server too busy to reply within it is answered for by the
// fallbacks below rather than by waiting.
const askTimeout = 2 * time.Second

// drainWait is how long a stop waits for the server to finish what it was
// doing — a save being written is the case that matters — before it stops
// asking politely. The server bounds its own drain (webhost.StopDeadline), so
// this is a margin past that rather than the same number: a wait that ran out
// as the drain did would signal a server that was about to exit on its own.
const drainWait = webhost.StopDeadline + 5*time.Second

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
	Port int
	// SecurePort is where the server also answers HTTPS; zero for none.
	SecurePort int
	State      State
	PID        int
	Profile    string
	Version    string
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
	address := LANAddress()
	if address == "" {
		return ""
	}
	return fmt.Sprintf("http://%s", net.JoinHostPort(address, strconv.Itoa(r.Port)))
}

// SecureURL is the HTTPS address on this machine, or "" when there is none.
func (r Report) SecureURL() string {
	if r.SecurePort == 0 {
		return ""
	}
	return fmt.Sprintf("https://127.0.0.1:%d", r.SecurePort)
}

// SecureLANURL is the HTTPS address for another machine on the local network,
// the one a phone opens for the secure page, or "" when there is none.
func (r Report) SecureLANURL() string {
	address := LANAddress()
	if r.SecurePort == 0 || address == "" {
		return ""
	}
	return fmt.Sprintf("https://%s", net.JoinHostPort(address, strconv.Itoa(r.SecurePort)))
}

// LANAddress picks this machine's address on the local network: the one its
// traffic leaves by, preferring IPv4 because that is what a person can retype.
// It is the address LANURL and SecureLANURL name, or "" when there is none.
//
// **The routing table decides, not the order the interfaces are listed in.**
// The first routable address of the listing was the answer, and on Windows a
// virtual adapter's — WSL's, Hyper-V's — often comes first: an address no
// phone reaches, and a new one at every boot, which made the HTTPS certificate
// again at every start, since the address printed for another device is one
// it has to name. A tunnel's address (a VPN's point-to-point interface) is not
// on the local network either and is passed over. Without a route the
// listing decides, as it did.
func LANAddress() string {
	interfaces, err := net.Interfaces()
	if err != nil {
		return ""
	}
	var candidates []net.IP
	for _, candidate := range interfaces {
		addresses, err := candidate.Addrs()
		if err != nil {
			continue
		}
		candidates = append(candidates, lanAddresses(candidate.Flags, addresses)...)
	}
	return pickLANAddress(candidates, routedAddress("udp4", lanRouteProbe4), routedAddress("udp6", lanRouteProbe6))
}

// The destinations the routing table is asked about. They are documentation
// addresses (RFC 5737, RFC 3849), which no host has, and asking sends
// nothing: connecting a UDP socket only chooses its route and the address it
// would leave from.
const (
	lanRouteProbe4 = "192.0.2.1:9"
	lanRouteProbe6 = "[2001:db8::1]:9"
)

// routedAddress is the address this machine would send from to reach probe,
// or nil when it has no route there.
func routedAddress(network, probe string) net.IP {
	connection, err := net.Dial(network, probe)
	if err != nil {
		return nil
	}
	defer connection.Close()
	local, ok := connection.LocalAddr().(*net.UDPAddr)
	if !ok {
		return nil
	}
	return local.IP
}

// lanAddresses are the addresses of one interface another device on the
// local network could reach: none of a down, loopback or point-to-point
// interface, and of the rest the routable ones, loopback and link-local left
// out.
func lanAddresses(flags net.Flags, addresses []net.Addr) []net.IP {
	if flags&net.FlagUp == 0 || flags&(net.FlagLoopback|net.FlagPointToPoint) != 0 {
		return nil
	}
	var reachable []net.IP
	for _, entry := range addresses {
		network, ok := entry.(*net.IPNet)
		if !ok || network.IP.IsLoopback() || network.IP.IsLinkLocalUnicast() {
			continue
		}
		reachable = append(reachable, network.IP)
	}
	return reachable
}

// pickLANAddress is LANAddress's choice among the candidates, in the order
// they were listed: the IPv4 address the route leaves from if it is one of
// them, else the first IPv4 one, else the same for IPv6.
func pickLANAddress(candidates []net.IP, routed4, routed6 net.IP) string {
	among := func(address net.IP) bool {
		return address != nil && slices.ContainsFunc(candidates, address.Equal)
	}
	if among(routed4) && routed4.To4() != nil {
		return routed4.To4().String()
	}
	for _, address := range candidates {
		if four := address.To4(); four != nil {
			return four.String()
		}
	}
	if among(routed6) {
		return routed6.String()
	}
	if len(candidates) > 0 {
		return candidates[0].String()
	}
	return ""
}

// statusPayload is what /api/status answers with.
type statusPayload struct {
	Server  string `json:"server"`
	Profile string `json:"profile"`
	Version string `json:"version"`
	PID     int    `json:"pid"`
	HTTPS   int    `json:"https_port"`
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
	if payload.HTTPS > 0 && payload.HTTPS <= 65535 {
		report.SecurePort = payload.HTTPS
	}
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
	if !signallable(report.PID) {
		return report, Refused, fmt.Errorf(
			"the server on port %d named pid %d, which is not a process to stop", port, report.PID)
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

// signallable reports whether a pid a server named is one this may signal.
//
// The number came out of a network reply and nothing has checked it against
// anything. Init is not a server, and neither is the process doing the asking;
// the cost of believing either is not a failed stop but a killed init on a host
// where this runs with the permission to do it, or a launcher that kills itself
// halfway through stopping something else. Refusing by name says what happened,
// where signalling would say nothing until something else broke.
func signallable(pid int) bool {
	return pid > 0 && pid != 1 && pid != os.Getpid()
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
// port went quiet and its process has exited, or whatever holds the port now
// is not that process.
//
// **A quiet port is not a finished stop.** The server's drain closes its
// listener first and then closes its games, writing their saves, for as long
// as its own deadline allows; the process holds their save directories until
// it exits. A stop that answered at the port let a start that followed run
// beside it, refused every game the old one still held — and on Windows let
// an upgrade try to replace an executable that was still running. A pid this
// may not signal is not one it can watch either, so for those the port is
// the answer, as it was.
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
			if !signallable(pid) || processExited(pid) {
				return true
			}
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
