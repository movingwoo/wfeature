// Package appserver runs the server inside another program.
//
// The desktop server is a process: it parses flags, listens, logs, and waits
// for a signal or a stop request. A phone has no room for that shape. On
// Android the app starts the same binary as a child, but **iOS does not let an
// app start a process at all** — the Go code has to be linked into the app and
// called, which means there has to be something to call.
//
// That is this package: the smallest thing that turns a directory into a
// running server on loopback, and hands back the port it took. It deliberately
// does not own a signal handler, a logger of its own, or an exit — those
// belong to whatever is embedding it, and an app that gets them from a library
// has two things fighting over its lifecycle.
package appserver

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"path/filepath"
	"time"

	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/webhost"
	"github.com/movingwoo/wfeature/web"
)

// Options is what an embedder has to decide. Only Root is required: everything
// else has an answer that is right for a phone, which is the only thing that
// embeds this today.
type Options struct {
	// Root is the directory the games, saves and logs live under. On a phone
	// this is the app's own container, which is the only place it may write.
	Root string

	// Port is the loopback port to take. Zero asks the operating system for
	// one, which is what an app should do: a fixed port is a port another app
	// can already be holding, and the app is the only thing that needs to know
	// which one was taken.
	Port int

	// Logger receives the server's log. A nil logger discards it.
	Logger *slog.Logger
}

// Server is a running server. The embedder holds it for as long as the app is
// alive and closes it when the app goes away.
type Server struct {
	port       int
	httpServer *http.Server
	host       *webhost.Server
	logger     *slog.Logger
}

// Port is the loopback port the server took, which is the number the app needs
// to point a web view at.
func (s *Server) Port() int { return s.port }

// URL is the address to load.
func (s *Server) URL() string {
	return fmt.Sprintf("http://127.0.0.1:%d", s.port)
}

// Start listens and serves, and returns once the port is open — so an app that
// gets a Server back can load the page without polling for it.
//
// **It binds loopback and nothing else.** A phone carries its games and its
// saves in an app container, and a server on the Wi-Fi would be that container
// offered to the room with no key in front of it. The desktop's reasons for
// binding every interface — a phone in the house drawing what it sends — do
// not apply to the phone that is running it.
func Start(options Options) (*Server, error) {
	if options.Root == "" {
		return nil, fmt.Errorf("appserver: no root directory")
	}
	logger := options.Logger
	if logger == nil {
		logger = backend.NewLogger(io.Discard)
	}

	gameRoot := filepath.Join(options.Root, "games")
	saveRoot := filepath.Join(options.Root, "savedata", "ktf")
	logRoot := filepath.Join(options.Root, "logs")

	host, err := webhost.New(webhost.Options{
		Client:   web.Client(),
		GameRoot: gameRoot,
		SaveRoot: saveRoot,
		LogRoot:  logRoot,
		Logger:   logger,
	})
	if err != nil {
		return nil, fmt.Errorf("appserver: %w", err)
	}

	address := net.JoinHostPort("127.0.0.1", fmt.Sprint(options.Port))
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return nil, fmt.Errorf("appserver: listen on %s: %w", address, err)
	}
	port := listener.Addr().(*net.TCPAddr).Port

	httpServer := &http.Server{
		Handler:           host,
		ReadHeaderTimeout: 10 * time.Second,
		// A game archive being added from the page is the longest transfer
		// there is, in either direction, and these are the same minutes the
		// desktop allows. The session socket keeps neither: a hijack clears
		// both connection deadlines. See cmd/server for the whole of it.
		ReadTimeout:  10 * time.Minute,
		WriteTimeout: 10 * time.Minute,
	}
	go func() { _ = httpServer.Serve(listener) }()

	logger.Info("serving wfeature in-process",
		"url", fmt.Sprintf("http://127.0.0.1:%d", port),
		"profile", host.Profile(),
		"games", gameRoot,
		"saves", saveRoot)

	return &Server{port: port, httpServer: httpServer, host: host, logger: logger}, nil
}

// Close stops the server and finishes what it was writing. A save being
// committed is the reason this drains rather than dropping the connections.
func (s *Server) Close() error {
	if s == nil || s.httpServer == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	err := s.httpServer.Shutdown(ctx)
	s.host.CloseParkedSessions()
	return err
}
