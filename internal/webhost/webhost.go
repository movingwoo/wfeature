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
	"net/http"
	"path"
	"strconv"
	"strings"
	"sync"

	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/licenses"
	"github.com/movingwoo/wfeature/internal/platform/ktf"
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
	// SaveRoot is this profile's KTF save tree; a platform segment in a
	// request reroots to a sibling directory.
	SaveRoot string
	// LogRoot receives the session reports and the page logs saved beside them.
	LogRoot string
	// Version names the release this binary came from, for /api/status. An
	// unstamped build leaves it empty and the endpoint says so.
	Version string

	Logger *slog.Logger
}

// Server answers every route the client uses. Its zero value is not usable;
// build one with New.
type Server struct {
	client   fs.FS
	gameRoot string
	saveRoot string
	logRoot  string
	logger   *slog.Logger
	// profile is this binary's build profile. There is no flag for it: the
	// server is built per profile like every other binary here, so a flag
	// would only be a way to disagree with the binary that is running.
	profile string
	// traceLimit is how many recent boundary events a session keeps in order
	// for its report. The ordered trace is a debug-profile cost, so a release
	// server keeps only the counted totals.
	traceLimit int
	version    string

	// parked holds games whose page went away, waiting under the token that
	// page was given; see resume.go. They live on the Server because they have
	// outlived the goroutine that was driving them.
	parkedMu sync.Mutex
	parked   map[string]*parkedSession
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
	return &Server{
		client:     options.Client,
		gameRoot:   options.GameRoot,
		saveRoot:   options.SaveRoot,
		logRoot:    options.LogRoot,
		logger:     logger,
		profile:    backend.BuildProfile(),
		traceLimit: sessionTraceLimit(),
		version:    options.Version,
	}, nil
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
	switch {
	case requestPath == "/api/session":
		s.serveSession(writer, request)
	case requestPath == "/api/debug-log":
		s.serveDebugLog(writer, request)
	case strings.HasPrefix(requestPath, "/api/saves/"):
		s.serveSaves(writer, request)
	case requestPath == "/games.json":
		s.serveGameList(writer, request)
	case strings.HasPrefix(requestPath, "/games/"):
		s.serveGameArchive(writer, request)
	case requestPath == "/api/status":
		s.serveStatus(writer, request)
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
	}{Server: "wfeature", Profile: s.profile, Version: version})
	if err != nil {
		writeError(writer, http.StatusInternalServerError, "Internal Server Error")
		return
	}
	writeJSON(writer, http.StatusOK, body)
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
	securityHeaders(writer.Header())
	writer.Header().Set("Content-Type", "text/plain; charset=utf-8")
	writer.WriteHeader(http.StatusOK)
	if request.Method == http.MethodHead {
		return
	}
	fmt.Fprint(writer, licenses.Project, "\n\n", licenses.ThirdParty)
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
	content, err := fs.ReadFile(s.client, name)
	if err != nil {
		writeError(writer, http.StatusNotFound, "Not Found")
		return
	}
	securityHeaders(writer.Header())
	// The shell is small and changes with every build, so it is never cached;
	// the service worker is what makes the page load offline.
	writer.Header().Set("Cache-Control", "no-store")
	writer.Header().Set("Content-Type", contentType(name))
	writer.Header().Set("Content-Length", strconv.Itoa(len(content)))
	writer.WriteHeader(http.StatusOK)
	if request.Method == http.MethodHead {
		return
	}
	_, _ = writer.Write(content)
}
