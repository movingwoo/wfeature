package wsproto

import (
	"bufio"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// acceptGUID is the constant RFC 6455 mixes into the client's key. It is not a
// secret; it exists so a cached HTTP response can never be mistaken for a
// successful upgrade.
const acceptGUID = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"

// AcceptKey computes the Sec-WebSocket-Accept value for a client key.
func AcceptKey(key string) string {
	sum := sha1.Sum([]byte(key + acceptGUID))
	return base64.StdEncoding.EncodeToString(sum[:])
}

// IsUpgrade reports whether a request is asking for a WebSocket upgrade, so a
// single route can serve both a plain GET and a session.
func IsUpgrade(request *http.Request) bool {
	return headerHasToken(request.Header, "Connection", "upgrade") &&
		headerHasToken(request.Header, "Upgrade", "websocket")
}

// headerHasToken reports whether a comma-separated header lists a token,
// case-insensitively. `Connection: keep-alive, Upgrade` is legal, so a plain
// equality test would reject real browsers.
func headerHasToken(header http.Header, name, token string) bool {
	for _, value := range header.Values(name) {
		for _, candidate := range strings.Split(value, ",") {
			if strings.EqualFold(strings.TrimSpace(candidate), token) {
				return true
			}
		}
	}
	return false
}

// Upgrader turns an HTTP request into a Conn. The zero value is usable.
type Upgrader struct {
	// CheckOrigin decides whether a browser page may open this session. A
	// WebSocket handshake is not subject to CORS, so without this check any
	// page the user visits could drive a session on their own machine. The
	// default accepts a request with no Origin (a native client) and one
	// whose Origin host matches the host it connected to.
	CheckOrigin func(request *http.Request) bool

	// MaxMessageSize bounds an assembled message on the accepted connection.
	// Zero means DefaultMaxMessageSize.
	MaxMessageSize int
}

// Accept completes the server side of the handshake and returns the
// connection. On failure it has already answered the request with a status and
// a plain-text reason, so the caller only has to stop.
//
// After it returns successfully the ResponseWriter must not be used: the
// connection has been hijacked and the response is already on the wire.
func (u *Upgrader) Accept(writer http.ResponseWriter, request *http.Request) (*Conn, error) {
	if request.Method != http.MethodGet {
		return nil, refuse(writer, http.StatusMethodNotAllowed, "websocket upgrade requires GET")
	}
	if !IsUpgrade(request) {
		return nil, refuse(writer, http.StatusBadRequest, "not a websocket upgrade request")
	}
	if version := request.Header.Get("Sec-WebSocket-Version"); version != "13" {
		// The version is advertised back so a client speaking an older draft
		// learns what to retry with rather than guessing.
		writer.Header().Set("Sec-WebSocket-Version", "13")
		return nil, refuse(writer, http.StatusUpgradeRequired, "unsupported websocket version %q", version)
	}
	key := request.Header.Get("Sec-WebSocket-Key")
	if !validClientKey(key) {
		return nil, refuse(writer, http.StatusBadRequest, "missing or malformed Sec-WebSocket-Key")
	}
	if !u.checkOrigin(request) {
		return nil, refuse(writer, http.StatusForbidden, "origin not allowed")
	}

	hijacker, ok := writer.(http.Hijacker)
	if !ok {
		return nil, refuse(writer, http.StatusInternalServerError, "the http server does not support hijacking")
	}
	socket, buffered, err := hijacker.Hijack()
	if err != nil {
		return nil, refuse(writer, http.StatusInternalServerError, "hijack: %v", err)
	}

	response := "HTTP/1.1 101 Switching Protocols\r\n" +
		"Upgrade: websocket\r\n" +
		"Connection: Upgrade\r\n" +
		"Sec-WebSocket-Accept: " + AcceptKey(key) + "\r\n\r\n"
	if _, err := socket.Write([]byte(response)); err != nil {
		_ = socket.Close()
		return nil, fmt.Errorf("wsproto: write handshake response: %w", err)
	}

	// The http server's own read and write deadlines still apply to the
	// hijacked socket; a session lives far longer than one request, so they
	// are cleared and the session sets its own.
	_ = socket.SetDeadline(time.Time{})

	// Anything the client pipelined after the handshake is already sitting in
	// the buffered reader, so that reader — not the socket — is what the
	// connection reads from. Writes go straight to the socket.
	connection := Server(hijackedTransport{Conn: socket, reader: buffered.Reader})
	connection.MaxMessageSize = u.MaxMessageSize
	return connection, nil
}

// Accept upgrades with the default Upgrader.
func Accept(writer http.ResponseWriter, request *http.Request) (*Conn, error) {
	upgrader := Upgrader{}
	return upgrader.Accept(writer, request)
}

func (u *Upgrader) checkOrigin(request *http.Request) bool {
	if u.CheckOrigin != nil {
		return u.CheckOrigin(request)
	}
	return SameOrigin(request)
}

// SameOrigin is the default origin policy: no Origin header passes, and an
// Origin passes when its host matches the Host the request arrived on. The
// server is reached by IP or hostname on a home network, so comparing hosts is
// the only part that is meaningful; the scheme differs between ws and http.
func SameOrigin(request *http.Request) bool {
	origin := request.Header.Get("Origin")
	if origin == "" {
		return true
	}
	parsed, err := url.Parse(origin)
	if err != nil {
		return false
	}
	return strings.EqualFold(parsed.Host, request.Host)
}

// validClientKey checks the shape RFC 6455 requires: base64 of 16 random
// bytes. A server that accepts anything here will happily complete a
// handshake with a request that was never a WebSocket handshake.
func validClientKey(key string) bool {
	decoded, err := base64.StdEncoding.DecodeString(key)
	return err == nil && len(decoded) == 16
}

func refuse(writer http.ResponseWriter, status int, format string, arguments ...any) error {
	reason := fmt.Sprintf(format, arguments...)
	http.Error(writer, reason, status)
	return errors.New("wsproto: " + reason)
}

// hijackedTransport reads through the bufio.Reader the hijack handed over and
// writes straight to the socket. It embeds net.Conn so deadlines still reach
// the socket.
type hijackedTransport struct {
	net.Conn
	reader *bufio.Reader
}

func (h hijackedTransport) Read(into []byte) (int, error) { return h.reader.Read(into) }

// Dial opens a client connection to a ws:// URL. Nothing in the emulator
// speaks as a client — the browser does — but a Go client is what lets the
// session server be tested end to end without a browser. TLS is not supported
// because the server it talks to serves plain HTTP on a home network.
func Dial(rawURL string, header http.Header) (*Conn, *http.Response, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return nil, nil, fmt.Errorf("wsproto: parse %q: %w", rawURL, err)
	}
	if parsed.Scheme != "ws" {
		return nil, nil, fmt.Errorf("wsproto: unsupported scheme %q, expected ws", parsed.Scheme)
	}
	address := parsed.Host
	if parsed.Port() == "" {
		address = net.JoinHostPort(address, "80")
	}
	socket, err := net.Dial("tcp", address)
	if err != nil {
		return nil, nil, fmt.Errorf("wsproto: dial %s: %w", address, err)
	}

	var keyBytes [16]byte
	if _, err := rand.Read(keyBytes[:]); err != nil {
		_ = socket.Close()
		return nil, nil, fmt.Errorf("wsproto: read handshake key: %w", err)
	}
	key := base64.StdEncoding.EncodeToString(keyBytes[:])

	requestURI := parsed.RequestURI()
	var builder strings.Builder
	fmt.Fprintf(&builder, "GET %s HTTP/1.1\r\n", requestURI)
	fmt.Fprintf(&builder, "Host: %s\r\n", parsed.Host)
	builder.WriteString("Upgrade: websocket\r\nConnection: Upgrade\r\n")
	fmt.Fprintf(&builder, "Sec-WebSocket-Key: %s\r\n", key)
	builder.WriteString("Sec-WebSocket-Version: 13\r\n")
	for name, values := range header {
		for _, value := range values {
			fmt.Fprintf(&builder, "%s: %s\r\n", name, value)
		}
	}
	builder.WriteString("\r\n")
	if _, err := socket.Write([]byte(builder.String())); err != nil {
		_ = socket.Close()
		return nil, nil, fmt.Errorf("wsproto: write handshake: %w", err)
	}

	reader := bufio.NewReader(socket)
	request := &http.Request{Method: http.MethodGet, URL: parsed}
	response, err := http.ReadResponse(reader, request)
	if err != nil {
		_ = socket.Close()
		return nil, nil, fmt.Errorf("wsproto: read handshake response: %w", err)
	}
	if response.StatusCode != http.StatusSwitchingProtocols {
		_ = socket.Close()
		return nil, response, fmt.Errorf("wsproto: handshake answered %s", response.Status)
	}
	if !IsUpgrade(&http.Request{Header: response.Header}) {
		_ = socket.Close()
		return nil, response, errors.New("wsproto: handshake response is missing its upgrade headers")
	}
	if response.Header.Get("Sec-WebSocket-Accept") != AcceptKey(key) {
		_ = socket.Close()
		return nil, response, errors.New("wsproto: handshake response has the wrong accept key")
	}
	return Client(hijackedTransport{Conn: socket, reader: reader}), response, nil
}
