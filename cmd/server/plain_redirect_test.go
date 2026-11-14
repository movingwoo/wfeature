package main

import (
	"bufio"
	"crypto/tls"
	"crypto/x509"
	"io"
	"log"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// **Plain HTTP on the HTTPS port is sent to HTTPS.** A phone's browser puts
// `http://` in front of an address typed without a scheme, and the standard
// library answered that on the HTTPS port with a 400 and an English line. The
// port now answers with a redirect to the same host, port, path and query over
// HTTPS, keeps a method other than GET and HEAD through 307, and still serves
// TLS to a browser that asked for it. The redirect is temporary (302, 307): a
// browser keeps a permanent one for good, past the server turning HTTPS off.
func TestPlainHTTPOnTheHTTPSPortIsSentToHTTPS(t *testing.T) {
	certificate, _, err := secureCertificate(filepath.Join(t.TempDir(), "tls"), []string{"localhost"},
		[]net.IP{net.IPv4(127, 0, 0, 1).To4()}, nil, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	plain, err := listen("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	secure := secureListener(plain, certificate)
	server := &http.Server{
		Handler: http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
			_, _ = io.WriteString(writer, "served over TLS")
		}),
		ReadHeaderTimeout: 10 * time.Second,
		ErrorLog:          log.New(io.Discard, "", 0),
	}
	go func() { _ = server.Serve(secure) }()
	defer server.Close()
	address := secure.Addr().String()
	_, port, err := net.SplitHostPort(address)
	if err != nil {
		t.Fatal(err)
	}

	ask := func(request string) (status, location string) {
		t.Helper()
		connection, err := net.DialTimeout("tcp", address, 5*time.Second)
		if err != nil {
			t.Fatal(err)
		}
		defer connection.Close()
		_ = connection.SetDeadline(time.Now().Add(10 * time.Second))
		if _, err := io.WriteString(connection, request); err != nil {
			t.Fatal(err)
		}
		response, err := http.ReadResponse(bufio.NewReader(connection), nil)
		if err != nil {
			t.Fatalf("read the answer to %q: %v", strings.SplitN(request, "\r\n", 2)[0], err)
		}
		response.Body.Close()
		return response.Status, response.Header.Get("Location")
	}
	for _, probe := range []struct{ request, status, location string }{
		{"GET /index.html?game=a HTTP/1.1\r\nHost: studio.local:11641\r\n\r\n", "302 Found", "https://studio.local:11641/index.html?game=a"},
		{"GET / HTTP/1.1\r\nHost: 192.168.0.60:11641\r\nUser-Agent: phone\r\n\r\n", "302 Found", "https://192.168.0.60:11641/"},
		{"POST /api/saves HTTP/1.1\r\nHost: [fd00::5]:11641\r\nContent-Length: 0\r\n\r\n", "307 Temporary Redirect", "https://[fd00::5]:11641/api/saves"},
		// A host no URL can carry is replaced by the address the request
		// reached, rather than copied into the Location.
		{"GET /x HTTP/1.1\r\nHost: evil.example/path\r\n\r\n", "302 Found", "https://" + address + "/x"},
		// A browser leaves 80 out of an http:// address; the redirect names
		// the port the request reached rather than letting https:// mean 443.
		{"GET /a HTTP/1.1\r\nHost: studio.local\r\n\r\n", "302 Found", "https://studio.local:" + port + "/a"},
		{"GET / HTTP/1.1\r\nHost: [fd00::5]\r\n\r\n", "302 Found", "https://[fd00::5]:" + port + "/"},
	} {
		status, location := ask(probe.request)
		if status != probe.status || location != probe.location {
			t.Errorf("%q answered %q to %q, want %q to %q", strings.SplitN(probe.request, "\r\n", 2)[0], status, location, probe.status, probe.location)
		}
	}

	// What is not a request line is left to the TLS server, which refuses it.
	connection, err := net.DialTimeout("tcp", address, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	_ = connection.SetDeadline(time.Now().Add(10 * time.Second))
	_, _ = connection.Write([]byte{0, 1, 2, 3, 4, 5, 6, 7})
	if answer, _ := io.ReadAll(connection); strings.Contains(string(answer), "Location") {
		t.Fatalf("a connection that is neither TLS nor HTTP was answered with %q", answer)
	}
	connection.Close()

	trusted := x509.NewCertPool()
	trusted.AddCert(certificate.Leaf)
	client := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: trusted}}, Timeout: 10 * time.Second}
	response, err := client.Get("https://" + address + "/")
	if err != nil {
		t.Fatalf("GET over TLS: %v", err)
	}
	body, _ := io.ReadAll(response.Body)
	response.Body.Close()
	if response.StatusCode != http.StatusOK || string(body) != "served over TLS" {
		t.Fatalf("GET over TLS answered %d %q", response.StatusCode, body)
	}
}
