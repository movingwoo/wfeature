// Command server hosts the browser client over HTTP: the PWA shell, the game
// archives, the save API and the debug-log API, and the emulation sessions the
// page plays through. The sessions run in this process, which is why the server
// is Go: it is the emulator with a socket in front of it.
//
// A release is one binary per operating system. It carries the client inside
// itself, reads games from a directory beside it and is left running; the
// phone is a browser.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/launcher"
	"github.com/movingwoo/wfeature/internal/webhost"
	"github.com/movingwoo/wfeature/internal/wipic"
	"github.com/movingwoo/wfeature/web"
)

// version names the release this binary came from. A release stamps it
// (`-ldflags "-X main.version=..."`, see the dist target); a build from a
// checkout stays "dev". A user who downloaded an archive has no other way to
// say which build they are running when something goes wrong.
var version = "dev"

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		// A command that already explained itself to the user in their own
		// words does not get a second, English line under it.
		if !errors.Is(err, errAlreadyReported) {
			fmt.Fprintln(os.Stderr, err)
		}
		os.Exit(1)
	}
}

// errAlreadyReported marks a failure the user has been told about. It still
// ends the process with a non-zero status, which is what a script around it
// reads.
var errAlreadyReported = errors.New("already reported")

// environmentOr lets the documented environment variables keep working while
// the flags take precedence, so a shell profile can set a game root once and a
// single run can still override it.
func environmentOr(name, fallback string) string {
	if value, ok := os.LookupEnv(name); ok && value != "" {
		return value
	}
	return fallback
}

// dataRoot decides where games, saves and logs live when nothing says
// otherwise. A checkout has var/ beside the working directory and keeps using
// it; a released binary is a single file the user dropped somewhere with a
// games/ folder next to it, and it would be surprising for that copy to read
// and write whichever directory it happened to be launched from.
//
// The rule is therefore: the working directory wins if it already looks like
// a checkout, and otherwise the executable's own directory does.
func dataRoot() (root string, layout string) {
	if info, err := os.Stat("var"); err == nil && info.IsDir() {
		return "var", "var/"
	}
	executable, err := os.Executable()
	if err != nil {
		return "var", "var/"
	}
	beside := filepath.Dir(executable)
	if info, err := os.Stat(filepath.Join(beside, "games")); err == nil && info.IsDir() {
		return beside, "beside the executable"
	}
	return "var", "var/"
}

// The two streams are the CLI's: `answer` carries what a user asked the
// command to print and `output` carries the run itself — the log, and what the
// flag package says about a mistyped flag. `-version` is an answer, so it has
// to be readable through a pipe; the log is not, so it stays out of one.
func run(arguments []string, answer, output *os.File) error {
	// Two of the three things a launcher does are questions about a server
	// rather than a server to run, and they are answered here so that the
	// double-click scripts beside a release are a line each instead of a
	// per-operating-system reimplementation. See internal/launcher.
	if len(arguments) > 0 {
		switch arguments[0] {
		case "status":
			return reportStatus(arguments[1:], answer, output)
		case "stop":
			return stopServer(arguments[1:], answer, output)
		}
	}

	flags := flag.NewFlagSet("server", flag.ContinueOnError)
	flags.SetOutput(output)
	root, layout := dataRoot()
	address := flags.String("addr", environmentOr("WFEATURE_ADDR", defaultAddress()),
		"address to listen on")
	webRoot := flags.String("web", environmentOr("WFEATURE_WEB_ROOT", "web"),
		"directory holding the client files; when it is missing the embedded copy is served")
	gameRoot := flags.String("games", environmentOr("WFEATURE_GAME_ROOT", webhost.GameRootIn(root)),
		"directory holding the game archives, grouped by platform")
	saveRoot := flags.String("saves", environmentOr("WFEATURE_SAVE_ROOT", webhost.SaveRootIn(root)),
		"this profile's KTF save tree; other platforms are its siblings")
	logRoot := flags.String("logs", environmentOr("WFEATURE_LOG_ROOT", webhost.LogRootIn(root)),
		"directory receiving debug reports")
	number := flags.String("number", environmentOr("WFEATURE_PHONE_NUMBER", wipic.SubscriberNumber()),
		"the subscriber number this handset answers with; a shorter one opens a title that gates on billing")
	openPage := flags.Bool("open", environmentOr("WFEATURE_OPEN", "") != "",
		"open the page in a browser once the server is listening")
	showVersion := flags.Bool("version", false, "print the version and exit")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	if err := wipic.SetSubscriberNumber(*number); err != nil {
		return err
	}
	if *showVersion {
		fmt.Fprintf(answer, "wfeature-server %s (%s)\n", version, backend.BuildProfile())
		return nil
	}

	logger := backend.NewLogger(output)
	options := webhost.Options{
		Client:   web.Client(),
		GameRoot: *gameRoot,
		SaveRoot: *saveRoot,
		LogRoot:  *logRoot,
		Version:  version,
		Logger:   logger,
	}
	// A checkout serves its working copy so an edit to the page shows up on a
	// reload; a released binary has no such directory and serves what it
	// carries.
	source := "embedded"
	if info, err := os.Stat(*webRoot); err == nil && info.IsDir() {
		options.Client = os.DirFS(*webRoot)
		source = *webRoot
	}

	// The local stop request and a signal end at the same drain; this is the
	// channel the route closes. sync.Once keeps a second request from closing
	// a closed channel.
	shutdownRequested := make(chan struct{})
	var once sync.Once
	options.RequestShutdown = func() { once.Do(func() { close(shutdownRequested) }) }
	requestShutdown := shutdownRequested

	server, err := webhost.New(options)
	if err != nil {
		return err
	}

	listener, err := listen(*address)
	if err != nil {
		return err
	}
	// Which saves a session is playing should never be a guess, so the tree is
	// named on startup: the profiles keep separate ones.
	logger.Info("serving wfeature",
		"url", serverURL(listener),
		"version", version,
		"profile", server.Profile(),
		"client", source,
		"data", layout,
		"games", *gameRoot,
		"saves", *saveRoot)

	if *openPage {
		openWhenServing(listener)
	}
	return serve(listener, server, logger, requestShutdown)
}

// openWhenServing shows the page once the port answers. A launcher used to
// sleep a second and hope; this waits for the listener to be answering as this
// server, which is the condition that was being guessed at.
func openWhenServing(listener net.Listener) {
	address, ok := listener.Addr().(*net.TCPAddr)
	if !ok {
		return
	}
	go func() {
		report, serving := launcher.WaitUntilServing(context.Background(), address.Port, 10*time.Second)
		if !serving {
			return
		}
		_ = launcher.OpenBrowser(report.URL())
	}()
}

// portArgument reads the port a launcher passes. The flag is the documented
// spelling and the bare number is what the scripts these commands replaced
// took, so `stop 11599` keeps working.
func portArgument(name string, arguments []string, output *os.File) (int, error) {
	flags := flag.NewFlagSet(name, flag.ContinueOnError)
	flags.SetOutput(output)
	port := flags.Int("port", defaultPort(), "the port the server is on")
	if err := flags.Parse(arguments); err != nil {
		return 0, err
	}
	switch rest := flags.Args(); len(rest) {
	case 0:
	case 1:
		value, err := strconv.Atoi(rest[0])
		if err != nil || value <= 0 || value > 65535 {
			return 0, fmt.Errorf("%s: expected a port number, got %q", name, rest[0])
		}
		*port = value
	default:
		return 0, fmt.Errorf("%s: expected at most one port, got %d arguments", name, len(rest))
	}
	return *port, nil
}

// defaultPort is the port every launcher, the page and the documentation use,
// unless the environment moved it.
func defaultPort() int {
	if value, err := strconv.Atoi(environmentOr("WFEATURE_PORT", "")); err == nil && value > 0 {
		return value
	}
	return launcher.DefaultPort
}

// The text these two commands print is Korean, unlike everything else this
// binary writes. They are what a release user reads: the launcher beside the
// server is a double-clicked script that runs one of them and shows the
// answer, so this is the same audience the packaging README is written for.
// The log stays English, because it is read while debugging rather than while
// playing.

// reportStatus says whether a server is up on a port and which build it is.
func reportStatus(arguments []string, answer, output *os.File) error {
	port, err := portArgument("status", arguments, output)
	if err != nil {
		return err
	}
	report := launcher.Query(context.Background(), port)
	switch report.State {
	case launcher.Ours:
		fmt.Fprintln(answer, "wfeature 서버가 돌고 있습니다.")
		fmt.Fprintf(answer, "  주소       %s\n", report.URL())
		if lan := report.LANURL(); lan != "" {
			fmt.Fprintf(answer, "  다른 기기  %s\n", lan)
		}
		fmt.Fprintf(answer, "  버전       %s (%s)\n", versionOrUnknown(report.Version), report.Profile)
		fmt.Fprintf(answer, "  멈추기     %s stop %d\n", launcherName(), port)
	case launcher.Foreign:
		fmt.Fprintf(answer, "포트 %d 를 쓰는 것이 있지만 wfeature 서버가 아닙니다.\n", port)
		fmt.Fprintf(answer, "  %s\n", foreignReason(report))
		if port < 65535 {
			fmt.Fprintf(answer, "  다른 포트로 띄우려면: %s -addr :%d\n", launcherName(), port+1)
		}
	default:
		fmt.Fprintf(answer, "포트 %d 에는 wfeature 서버가 없습니다.\n", port)
	}
	return nil
}

// foreignReason says, in the user's language, what is known about whatever is
// holding the port. The technical half stays in the report for a log.
func foreignReason(report launcher.Report) string {
	switch report.Reason {
	case launcher.OtherServer:
		return "다른 프로그램이 응답합니다."
	default:
		return "연결은 되지만 응답이 없습니다."
	}
}

func versionOrUnknown(version string) string {
	if version == "" {
		return "알 수 없음"
	}
	return version
}

// stopServer stops the server on a port, and nothing else that happens to be
// there.
func stopServer(arguments []string, answer, output *os.File) error {
	port, err := portArgument("stop", arguments, output)
	if err != nil {
		return err
	}
	report, outcome, err := launcher.Stop(context.Background(), port)
	switch outcome {
	case launcher.AlreadyStopped:
		fmt.Fprintf(answer, "포트 %d 에는 아무것도 없습니다. 서버는 이미 멈춰 있습니다.\n", port)
		return nil
	case launcher.Refused:
		if errors.Is(err, launcher.ErrNotOurs) {
			fmt.Fprintf(answer, "포트 %d 를 쓰는 것은 wfeature 서버가 아닙니다. 건드리지 않습니다.\n", port)
			fmt.Fprintf(answer, "  %s\n", foreignReason(report))
			return errAlreadyReported
		}
		return err
	case launcher.Signalled, launcher.Killed:
		fmt.Fprintf(answer, "wfeature 서버를 멈췄습니다 (pid %d).\n", report.PID)
		fmt.Fprintln(answer, "  정상 종료에 응답하지 않아 강제로 멈췄습니다.")
		return nil
	default:
		fmt.Fprintf(answer, "wfeature 서버를 멈췄습니다 (pid %d).\n", report.PID)
		return nil
	}
}

// launcherName is how this binary was invoked, for the line that tells a user
// what to type next. A release user typed ./wfeature-server beside the games
// folder; a checkout typed a path into build/, and repeating the path they
// used is the half that can be pasted back.
func launcherName() string {
	if invoked := os.Args[0]; strings.ContainsRune(invoked, filepath.Separator) {
		return invoked
	}
	executable, err := os.Executable()
	if err != nil {
		return "wfeature-server"
	}
	return "." + string(filepath.Separator) + filepath.Base(executable)
}

// defaultAddress keeps the port the page and the documentation already use,
// and binds every interface so a phone on the same network can reach it —
// which is the entire point of running a server rather than a desktop app.
func defaultAddress() string {
	host := environmentOr("WFEATURE_HOST", "")
	port := environmentOr("WFEATURE_PORT", "11541")
	return net.JoinHostPort(host, port)
}

// unixPrefix marks an -addr that names a Unix domain socket rather than a
// host and port.
const unixPrefix = "unix:"

// socketMode is what a fresh socket file is left at. A reverse proxy runs as
// its own user, so it reaches the socket through the group: run the server
// with that group — systemd's `Group=` — and the pair works without the socket
// being writable by everyone on the machine. See running.md.
const socketMode = 0o660

// listen opens what -addr names: a TCP port, or a Unix domain socket when the
// address begins with `unix:`.
//
// The socket exists for one arrangement — a reverse proxy in front, terminating
// TLS and forwarding to a path instead of a port. Nothing above this line knows
// which it got: the rest of the server is written against net.Listener, and the
// session, the save API and the WebSocket upgrade are the same either way.
func listen(address string) (net.Listener, error) {
	path, ok := strings.CutPrefix(address, unixPrefix)
	if !ok {
		listener, err := net.Listen("tcp", address)
		if err != nil {
			return nil, fmt.Errorf("listen on %s: %w", address, err)
		}
		return listener, nil
	}
	if path == "" {
		return nil, fmt.Errorf("listen on %s: no socket path after %q", address, unixPrefix)
	}
	if err := clearStaleSocket(path); err != nil {
		return nil, err
	}
	listener, err := net.Listen("unix", path)
	if err != nil {
		return nil, fmt.Errorf("listen on %s: %w", path, err)
	}
	// The mode is set after the bind rather than through the umask, because the
	// umask belongs to whoever started the process and this has to be the same
	// either way. Go removes the file again when the listener closes.
	if err := os.Chmod(path, socketMode); err != nil {
		_ = listener.Close()
		return nil, fmt.Errorf("set the mode of %s: %w", path, err)
	}
	return listener, nil
}

// clearStaleSocket removes a socket file left behind by a server that did not
// get to close it — a kill, a power cut — because binding onto one fails with
// "address already in use" and reads exactly like a port conflict.
//
// **What it must not do is remove a socket a live server is serving.** So the
// file is not deleted for existing: it is connected to first, and only a
// refused connection says nobody is behind it. A path that is not a socket at
// all is left alone too, since that is a mistake in the argument rather than
// this server's leftover.
func clearStaleSocket(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return nil
	}
	if info.Mode()&os.ModeSocket == 0 {
		return fmt.Errorf("listen on %s: the path exists and is not a socket", path)
	}
	connection, err := net.DialTimeout("unix", path, time.Second)
	if err == nil {
		_ = connection.Close()
		return fmt.Errorf("listen on %s: a server is already serving this socket", path)
	}
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("remove the stale socket %s: %w", path, err)
	}
	return nil
}

// serverURL is what the startup line prints. A socket has no host to put in a
// URL, so it prints the path it is answering on instead — the address a proxy
// is pointed at.
func serverURL(listener net.Listener) string {
	if listener.Addr().Network() == "unix" {
		return unixPrefix + listener.Addr().String()
	}
	return "http://" + listener.Addr().String()
}

func serve(listener net.Listener, handler *webhost.Server, logger *slog.Logger, requested <-chan struct{}) error {
	httpServer := &http.Server{
		Handler: handler,
		// A game archive is tens of megabytes over a home network, so the
		// write timeout has to leave a slow phone time to finish downloading
		// one.
		ReadHeaderTimeout: 10 * time.Second,
		WriteTimeout:      10 * time.Minute,
	}

	// Ctrl-C during a save write should still finish the write, so the server
	// stops accepting and drains rather than dropping its connections.
	signalled, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	failed := make(chan error, 1)
	go func() { failed <- httpServer.Serve(listener) }()

	select {
	case err := <-failed:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-requested:
		logger.Info("shutting down")
		return drain(httpServer, handler, logger)
	case <-signalled.Done():
		logger.Info("shutting down")
		return drain(httpServer, handler, logger)
	}
}

// drain stops accepting and finishes what is in flight, which is what makes a
// stop safe during a save write. Both ways of asking end here: the signal a
// window's Ctrl-C sends, and the local request POST /api/shutdown makes.
func drain(httpServer *http.Server, handler *webhost.Server, logger *slog.Logger) error {
	shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := httpServer.Shutdown(shutdown); err != nil {
		logger.Error("shutdown was not clean", "error", err)
	}
	// A game waiting for a page to come back has no window left to wait in;
	// its guest goroutines would otherwise outlive this process's reason to
	// exist.
	handler.CloseParkedSessions()
	return nil
}
