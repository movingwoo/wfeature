// Package webhost serves the browser client: the PWA shell, the game
// archives, the save API and the debug-log API, and the emulation sessions the
// page plays through. Hosting a session means running the emulator, so this is
// Go for the same reason the emulator is, and a release is then one binary per
// operating system rather than a binary plus a runtime.
//
// The client files are served from an fs.FS, which is an embedded copy in a
// released binary and a directory during development. Game archives, saves and
// logs always live on disk under the ignored var/ tree, because they are the
// user's data rather than the build's.
//
// Every path in a request is untrusted. Components are checked before they are
// joined, so neither a traversal nor a percent-encoded one can name a file
// outside the root it belongs to.
package webhost

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"os"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/licenses"
	"github.com/movingwoo/wfeature/internal/platform/ktf"
	"github.com/movingwoo/wfeature/internal/wsproto"
)

// maxRequestBody bounds a save or a debug report. Both are written to disk, so
// the limit is what stops one request from filling it.
const maxRequestBody = 8 << 20

// contentTypes names the types the client actually serves. Anything else is
// sent as bytes rather than guessed at, because guessing is what
// X-Content-Type-Options exists to prevent.
var contentTypes = map[string]string{
	".css":         "text/css; charset=utf-8",
	".html":        "text/html; charset=utf-8",
	".js":          "text/javascript; charset=utf-8",
	".json":        "application/json; charset=utf-8",
	".png":         "image/png",
	".webmanifest": "application/manifest+json; charset=utf-8",
	".zip":         "application/zip",
	".jar":         "application/java-archive",
}

func contentType(name string) string {
	if found, ok := contentTypes[strings.ToLower(path.Ext(name))]; ok {
		return found
	}
	return "application/octet-stream"
}

// Options configures a Server. Only Client is required; the rest default to
// the ignored var/ tree beside the working directory.
type Options struct {
	// Client holds the PWA shell: index.html and the scripts beside it. A
	// released binary passes its embedded copy and a development run passes
	// os.DirFS over web/.
	Client fs.FS

	// GameRoot holds the archives, grouped by platform directory.
	GameRoot string
	// AddedRoot holds the archives that arrived through the page's add button
	// — `var/ext` in a checkout, `ext/` beside the games directory on a phone.
	// It is a root of its own rather than a directory inside GameRoot because
	// what is in it is what the page may delete, and because the library's
	// group directories are named by whoever made them: reserving a name in
	// there would be reserving one out of somebody's own categories. A host
	// that leaves it empty serves no added games and no removals.
	AddedRoot string
	// SaveRoot is this profile's KTF save tree; a platform segment in a
	// request reroots to a sibling directory.
	SaveRoot string
	// LogRoot receives the session reports and the page logs saved beside them.
	LogRoot string
	// Version names the release this binary came from, for /api/status. An
	// unstamped build leaves it empty and the endpoint says so.
	Version string

	// RequestShutdown asks the process to stop the way a signal does: drain,
	// then exit. It is what POST /api/shutdown calls, and a host that does not
	// install it has no such route — a server embedded in something larger
	// does not get to end that program.
	RequestShutdown func()

	Logger *slog.Logger
}

// Server answers every route the client uses. Its zero value is not usable;
// build one with New.
type Server struct {
	client fs.FS
	// text is the table of everything the page says, read out of client; see
	// text.go.
	text      *pageText
	gameRoot  string
	addedRoot string
	saveRoot  string
	logRoot   string
	logger    *slog.Logger
	// profile is this binary's build profile. There is no flag for it: the
	// server is built per profile like every other binary here, so a flag
	// would only be a way to disagree with the binary that is running.
	profile string
	// traceLimit is how many recent boundary events a session keeps in order
	// for its report. The ordered trace is a debug-profile cost, so a release
	// server keeps only the counted totals.
	traceLimit int
	// diagnostics reports whether this build collects run reports at all. It
	// is the debug profile's answer: a release serves a page with no report
	// button and no console capture behind it, so the route that receives one
	// is not served either. See debuglog.go.
	diagnostics bool
	// textConsole lets a cheat message with no operation run a line of the
	// text console. Only tests set it, to read and write guest memory by
	// address; New never does, so a served page reaches the panel's
	// operations and nothing else.
	textConsole bool
	// debugLogMu guards debugLogPosts, the times of the reports accepted in
	// the last window; see allowDebugReport.
	debugLogMu    sync.Mutex
	debugLogPosts []time.Time
	version       string
	// requestShutdown is Options.RequestShutdown; nil means the route is not
	// served at all.
	requestShutdown func()
	// compressed holds the gzip form of the files the page loads; see
	// revalidate.go.
	compressed compressedBodies

	// parked holds games whose page went away, waiting under the token that
	// page was given; see resume.go. They live on the Server because they have
	// outlived the goroutine that was driving them.
	// parkedMu guards both parked and claims: a start that takes over a
	// parked game touches the two together, and one mutex is what keeps that
	// from needing an order.
	parkedMu       sync.Mutex
	parked         map[string]*parkedSession
	sessionsClosed bool
	attached       map[string]*sessionRunner // guarded by parkedMu, including starts in flight
	// claims holds the save directory of every game a session has in hand,
	// parked or playing; see saveclaim.go.
	claims map[string]*saveClaim
}

// New validates the options and returns the server.
func New(options Options) (*Server, error) {
	if options.Client == nil {
		return nil, errors.New("webhost: no client files")
	}
	logger := options.Logger
	if logger == nil {
		logger = backend.NewLogger(io.Discard)
	}
	server := &Server{
		client:          options.Client,
		text:            &pageText{files: options.Client},
		gameRoot:        options.GameRoot,
		addedRoot:       options.AddedRoot,
		saveRoot:        options.SaveRoot,
		logRoot:         options.LogRoot,
		logger:          logger,
		profile:         backend.BuildProfile(),
		traceLimit:      sessionTraceLimit(),
		diagnostics:     backend.DebugBuild(),
		version:         options.Version,
		requestShutdown: options.RequestShutdown,
	}
	// The page is filled from the text file on every load, so a file that does
	// not read, or a page naming a sentence the file does not have, would fail
	// every load from the first; a server says so when it starts instead.
	if _, err := server.renderClientPage(); err != nil {
		return nil, fmt.Errorf("webhost: %w", err)
	}
	return server, nil
}

// sessionTraceLimit is the debug profile's ordered-trace depth. It is the
// platform's own default so a server report and a CLI report describe the same
// depth of history.
func sessionTraceLimit() int {
	if backend.DebugBuild() {
		return ktf.DefaultTraceLimit
	}
	return 0
}

// Profile reports the build profile this server serves, which is its own.
func (s *Server) Profile() string { return s.profile }

func (s *Server) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	// The handler is registered without a mux, so nothing has cleaned this
	// path: what arrives is what the peer sent, minus percent-encoding.
	requestPath := request.URL.Path
	// **A browser sends a cross-site POST without asking first.** CORS keeps a
	// page elsewhere from reading the answer, not from sending the request, so
	// a page on any site could stop this server, add or remove a game, or
	// overwrite a save through loopback. Every such request from a page
	// carries that page's Origin, so a request that would change something is
	// held to the rule the session socket keeps: no Origin — the stop command,
	// curl — or one whose host is the host it arrived on. A GET changes
	// nothing and is not refused here; the socket's upgrade is a GET with its
	// own check.
	if request.Method != http.MethodGet && request.Method != http.MethodHead && !wsproto.SameOrigin(request) {
		s.logger.Warn("refused a request from another site", "method", request.Method,
			"path", requestPath, "origin", request.Header.Get("Origin"), "host", request.Host)
		writeError(writer, http.StatusForbidden, "Forbidden")
		return
	}
	switch {
	case requestPath == "/api/session":
		s.serveSession(writer, request)
	case requestPath == "/api/debug-log":
		s.serveDebugLog(writer, request)
	case strings.HasPrefix(requestPath, "/api/saves/"):
		s.serveSaves(writer, request)
	case requestPath == "/games.json":
		s.serveGameList(writer, request)
	case strings.HasPrefix(requestPath, "/games/"), strings.HasPrefix(requestPath, "/ext/"):
		s.serveGameArchive(writer, request)
	case requestPath == "/api/games":
		s.serveGames(writer, request)
	case requestPath == "/api/savepack":
		s.serveSavePack(writer, request)
	case requestPath == "/api/status":
		s.serveStatus(writer, request)
	case requestPath == "/api/shutdown":
		s.serveShutdown(writer, request)
	case requestPath == "/licenses":
		s.serveLicenses(writer, request)
	default:
		s.serveClient(writer, request)
	}
}

// serveStatus says what this server is. The profile is the binary that is
// running rather than a setting, and from outside the process there is no other
// way to tell: two servers on two ports look alike from the outside, and the
// executable's path says nothing when it was started with `go run` or renamed
// on the way into a release archive. It is what the stop/status scripts read.
//
// Nothing here is a secret this server keeps — it has no authentication at all,
// and the page it serves already carries the same build.
func (s *Server) serveStatus(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet && request.Method != http.MethodHead {
		writeError(writer, http.StatusMethodNotAllowed, "Method Not Allowed")
		return
	}
	version := s.version
	if version == "" {
		version = "dev"
	}
	body, err := json.Marshal(struct {
		Server  string `json:"server"`
		Profile string `json:"profile"`
		Version string `json:"version"`
		PID     int    `json:"pid"`
	}{Server: "wfeature", Profile: s.profile, Version: version, PID: os.Getpid()})
	if err != nil {
		writeError(writer, http.StatusInternalServerError, "Internal Server Error")
		return
	}
	writeJSON(writer, http.StatusOK, body)
}

// serveShutdown stops the server the way closing its window does. It exists
// because that window is often gone — a launcher double-clicked weeks ago, a
// login that has ended — and because the alternative is finding the process
// that holds the port from outside, which took a different tool on every
// operating system and could not tell this server from a stranger.
//
// **Only a caller on this machine may use it.** The server binds every
// interface so a phone on the same network can play, and a stop anyone on that
// network can send is a way to end somebody's game from the next room. Local
// callers gain nothing they did not already have: whoever can reach loopback
// here can signal the process anyway. A page from another site open in a
// browser on this machine reaches loopback too and is not such a caller; it
// sends its Origin, and ServeHTTP refuses it before it gets here.
//
// The answer is sent before the drain starts, because the drain waits for
// in-flight requests and this is one of them.
func (s *Server) serveShutdown(writer http.ResponseWriter, request *http.Request) {
	if s.requestShutdown == nil {
		writeError(writer, http.StatusNotFound, "Not Found")
		return
	}
	if request.Method != http.MethodPost {
		writeError(writer, http.StatusMethodNotAllowed, "Method Not Allowed")
		return
	}
	if !isLoopbackRequest(request) {
		s.logger.Warn("refused a shutdown from off this machine", "from", request.RemoteAddr,
			"forwarded_for", request.Header.Get("X-Forwarded-For"))
		writeError(writer, http.StatusForbidden, "Forbidden")
		return
	}
	writeJSON(writer, http.StatusAccepted, []byte(`{"stopping":true}`))
	if flusher, ok := writer.(http.Flusher); ok {
		flusher.Flush()
	}
	s.logger.Info("stopping at a local request")
	go s.requestShutdown()
}

// isLoopbackRequest reports whether the peer is on this machine. A Unix socket
// has no address to read, and being on one is itself the answer: nothing off
// the machine can open it.
//
// Except through a reverse proxy, which is what the socket exists for: the
// proxy is on this machine, so every visitor it forwards arrives from here,
// over the socket or over loopback. A request that carries a forwarding header
// came from somewhere else, whatever peer handed it over. The header is only
// as good as the proxy that sets it, which is why docs/running.md has the
// nginx block set one; a visitor can add one but gains nothing by it.
func isLoopbackRequest(request *http.Request) bool {
	for _, header := range []string{"Forwarded", "X-Forwarded-For", "X-Real-IP"} {
		if len(request.Header.Values(header)) > 0 {
			return false
		}
	}
	host, _, err := net.SplitHostPort(request.RemoteAddr)
	if err != nil {
		host = request.RemoteAddr
	}
	if host == "" || host == "@" {
		return true
	}
	address, err := netip.ParseAddr(host)
	if err != nil {
		return false
	}
	return address.IsLoopback()
}

// serveLicenses answers with this project's licence and the notices for every
// component it bundles. A release is one executable, so this is how a user who
// downloaded only that executable reads terms two of the bundled components
// ask to be passed along.
func (s *Server) serveLicenses(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet && request.Method != http.MethodHead {
		writeError(writer, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	s.serveRevalidated(writer, request, "text/plain; charset=utf-8", []byte(licenses.Project+"\n\n"+licenses.ThirdParty))
}

// securityHeaders are set on every response. The client is same-origin by
// construction, and nosniff is what makes the narrow content-type table above
// meaningful.
func securityHeaders(header http.Header) {
	header.Set("Cross-Origin-Resource-Policy", "same-origin")
	header.Set("X-Content-Type-Options", "nosniff")
}

func writeError(writer http.ResponseWriter, status int, reason string) {
	securityHeaders(writer.Header())
	writer.Header().Set("Cache-Control", "no-store")
	writer.Header().Set("Content-Type", "text/plain; charset=utf-8")
	writer.WriteHeader(status)
	_, _ = io.WriteString(writer, reason)
}

func writeJSON(writer http.ResponseWriter, status int, body []byte) {
	securityHeaders(writer.Header())
	writer.Header().Set("Cache-Control", "no-store")
	writer.Header().Set("Content-Type", contentTypes[".json"])
	writer.Header().Set("Content-Length", strconv.Itoa(len(body)))
	writer.WriteHeader(status)
	_, _ = writer.Write(body)
}

// pathComponents splits a request path and rejects anything that could steer
// the result out of the root it will be joined to. A NUL, a backslash and a
// traversal are all refused here rather than deeper, where the check would
// have to be repeated per root.
func pathComponents(requestPath string) ([]string, error) {
	components := make([]string, 0, 4)
	for _, component := range strings.Split(requestPath, "/") {
		// Empty and "." components name the same place, so they are dropped
		// rather than refused: a guest opens a database as "./OptionSave" and
		// the save key that reaches this API keeps the dot.
		if component == "" || component == "." {
			continue
		}
		if component == ".." || strings.ContainsAny(component, "\\\x00") {
			return nil, fmt.Errorf("invalid path component %q", component)
		}
		components = append(components, component)
	}
	return components, nil
}

// serveClient serves the PWA shell out of the client FS.
func (s *Server) serveClient(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet && request.Method != http.MethodHead {
		writeError(writer, http.StatusMethodNotAllowed, "Method Not Allowed")
		return
	}
	requestPath := request.URL.Path
	if requestPath == "/" {
		requestPath = "/index.html"
	}
	components, err := pathComponents(requestPath)
	if err != nil || len(components) == 0 {
		writeError(writer, http.StatusForbidden, "Forbidden")
		return
	}
	name := path.Join(components...)
	if name == pageFile {
		// The page is sent with its words filled in; see text.go.
		content, err := s.renderClientPage()
		if err != nil {
			s.logger.Error("could not fill the page from its text", "error", err)
			writeError(writer, http.StatusInternalServerError, "Internal Server Error")
			return
		}
		s.serveRevalidated(writer, request, contentType(name), content)
		return
	}
	content, err := fs.ReadFile(s.client, name)
	if err != nil {
		writeError(writer, http.StatusNotFound, "Not Found")
		return
	}
	// The shell changes with every build, so every load asks whether it did;
	// the service worker is what makes the page load offline.
	s.serveRevalidated(writer, request, contentType(name), content)
}
