package main

import (
	"bufio"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
)

// A person who types the HTTPS address without `https://` gets plain HTTP on
// the HTTPS port: a phone's browser puts `http://` in front of a bare address
// and port. The standard library answers that with a 400 and one English line,
// `Client sent an HTTP request to an HTTPS server.`, and the page never opens.
// The port answers such a request instead with a redirect to the same address
// over HTTPS, so the same thing typed lands on the page.
//
// It has to happen before TLS: once the TLS server has read the first record,
// all that is left of a plain request is the error the standard library
// answers. A TLS connection opens with a handshake record, type 0x16; a request
// line opens with its method, in capitals. The first byte is read where the
// handshake would read it, on the connection's own goroutine and under the
// deadline the HTTP server sets for a handshake, so a client that sends
// nothing holds the accept loop for no time at all.

// maxRedirectRequest bounds what is read of a plain request: its request line
// and headers, which is all a redirect needs. Anything longer is not a browser
// that mistyped an address.
const maxRedirectRequest = 16 << 10

// errPlainHTTPRedirected ends the TLS handshake of a connection that turned out
// to carry plain HTTP and has had its redirect. The HTTP server logs it at the
// level the HTTPS listener's handshake errors go to.
var errPlainHTTPRedirected = errors.New("a plain HTTP request on the HTTPS port was redirected to HTTPS")

// secureListener is the HTTPS listener over plain, presenting certificate,
// with plain HTTP on it redirected to HTTPS. HTTP/1.1 only: the session is a
// WebSocket upgrade, which HTTP/2 here would not carry.
func secureListener(plain net.Listener, certificate tls.Certificate) net.Listener {
	return tls.NewListener(plainRedirectListener{plain}, &tls.Config{Certificates: []tls.Certificate{certificate}, MinVersion: tls.VersionTLS12})
}

type plainRedirectListener struct{ net.Listener }

func (listener plainRedirectListener) Accept() (net.Conn, error) {
	connection, err := listener.Listener.Accept()
	if err != nil {
		return nil, err
	}
	return &plainRedirectConn{Conn: connection, reader: bufio.NewReader(connection)}, nil
}

// plainRedirectConn reads through a buffer, so the byte it looks at first is
// still there for the TLS server to read.
type plainRedirectConn struct {
	net.Conn
	reader  *bufio.Reader
	checked bool
}

func (connection *plainRedirectConn) Read(buffer []byte) (int, error) {
	if !connection.checked {
		connection.checked = true
		first, err := connection.reader.Peek(1)
		if err != nil {
			return 0, err
		}
		if first[0] >= 'A' && first[0] <= 'Z' {
			connection.redirect()
			return 0, errPlainHTTPRedirected
		}
	}
	return connection.reader.Read(buffer)
}

// redirect answers the plain request with a redirect to the address it was
// sent to, over HTTPS, and closes the connection. A request that does not
// parse, or names no host a URL can carry, is closed without an answer.
func (connection *plainRedirectConn) redirect() {
	defer connection.Conn.Close()
	request, err := http.ReadRequest(bufio.NewReader(io.LimitReader(connection.reader, maxRedirectRequest)))
	if err != nil {
		return
	}
	host := redirectHost(request.Host, connection.LocalAddr())
	// 301 is the redirect every browser keeps to GET; another method keeps
	// its own through 308.
	status := http.StatusMovedPermanently
	if request.Method != http.MethodGet && request.Method != http.MethodHead {
		status = http.StatusPermanentRedirect
	}
	_, _ = fmt.Fprintf(connection.Conn, "HTTP/1.1 %d %s\r\nLocation: https://%s%s\r\nContent-Length: 0\r\nConnection: close\r\n\r\n",
		status, http.StatusText(status), host, request.URL.RequestURI())
}

// redirectHost is the host and port the redirect names: the ones the request
// was sent to, which is the address the person typed and the port this
// listener is on, whatever -addr and -https moved it to. A Host without a port
// had the scheme's default left out — a browser omits 80 from an `http://`
// address — and `https://` would put 443 in its place, so the port the
// request reached is written in. A Host no URL can carry is replaced by the
// address the request reached.
func redirectHost(header string, local net.Addr) string {
	if !redirectableHost(header) {
		return local.String()
	}
	if _, _, err := net.SplitHostPort(header); err == nil {
		return header
	}
	_, port, err := net.SplitHostPort(local.String())
	if err != nil {
		return header
	}
	name := header
	if strings.HasPrefix(name, "[") && strings.HasSuffix(name, "]") {
		name = name[1 : len(name)-1]
	}
	return net.JoinHostPort(name, port)
}

// redirectableHost reports whether a Host header can stand in a Location as it
// is: a name or address and an optional port, nothing that would end the
// header or change what the URL means.
func redirectableHost(host string) bool {
	if host == "" || len(host) > 255 || strings.ContainsAny(host, " \t\r\n/\\@?#%") {
		return false
	}
	name := host
	if parsed, _, err := net.SplitHostPort(host); err == nil {
		name = parsed
	}
	return name != "" && !strings.ContainsAny(name, "<>\"'`{}|^")
}
