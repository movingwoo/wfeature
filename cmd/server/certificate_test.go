package main

import (
	"bufio"
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/movingwoo/wfeature/internal/webhost"
	"github.com/movingwoo/wfeature/web"
)

// **HTTPS goes where a phone reaches the server.** The default address binds
// every interface, so the HTTPS listener takes the port 100 above it; a
// loopback address is a secure context already and a socket is a proxy's to
// secure, so neither gets one unless it is asked for.
func TestTheHTTPSListenerFollowsAnAddressAPhoneCanReach(t *testing.T) {
	for _, probe := range []struct{ value, address, want string }{
		{"auto", ":11541", ":11641"},
		{"auto", "0.0.0.0:11541", "0.0.0.0:11641"},
		{"auto", "[::]:11599", "[::]:11699"},
		{"auto", "192.168.0.5:11541", "192.168.0.5:11641"},
		{"auto", ":65500", ""},
		{"auto", "127.0.0.1:11541", ""},
		{"auto", "[::1]:11541", ""},
		{"auto", "localhost:11541", ""},
		{"auto", "unix:/run/wfeature.sock", ""},
		{"auto", ":0", ""},
		{"off", ":11541", ""},
		{"", ":11541", ""},
		{":9443", "127.0.0.1:11541", ":9443"},
	} {
		if got := httpsAddress(probe.value, probe.address); got != probe.want {
			t.Errorf("httpsAddress(%q, %q) = %q, want %q", probe.value, probe.address, got, probe.want)
		}
	}
}

// **One certificate, kept.** Each new certificate is a new warning on every
// phone, so the one written first is served again while it names every
// address and has a month to run. A new address makes a new one that still
// names the old addresses, so a machine moving between two networks settles.
// Apple's platforms refuse any server certificate valid longer than 825 days.
func TestTheSelfSignedCertificateIsMadeOnceAndKept(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "tls")
	home, office := net.ParseIP("192.168.0.60").To4(), net.ParseIP("10.1.2.3").To4()
	now := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	first, created, err := secureCertificate(dir, []string{"localhost", "studio.local"}, []net.IP{home}, now)
	if err != nil || !created {
		t.Fatalf("first certificate: created %v, %v", created, err)
	}
	leaf := first.Leaf
	if !slices.Equal(leaf.DNSNames, []string{"localhost", "studio.local"}) || !leaf.IPAddresses[0].Equal(home) {
		t.Fatalf("names %v, addresses %v", leaf.DNSNames, leaf.IPAddresses)
	}
	if lifetime := leaf.NotAfter.Sub(leaf.NotBefore); lifetime > 825*24*time.Hour {
		t.Fatalf("valid for %v, longer than 825 days", lifetime)
	}
	if !slices.Contains(leaf.ExtKeyUsage, x509.ExtKeyUsageServerAuth) || leaf.IsCA {
		t.Fatalf("usage %v, CA %v", leaf.ExtKeyUsage, leaf.IsCA)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(filepath.Join(dir, keyFile))
		if err != nil || info.Mode().Perm() != 0o600 {
			t.Fatalf("key file mode %v, %v", info.Mode().Perm(), err)
		}
	}

	again, created, err := secureCertificate(dir, []string{"localhost", "studio.local"}, []net.IP{home}, now.Add(400*24*time.Hour))
	if err != nil || created || !bytes.Equal(again.Certificate[0], first.Certificate[0]) {
		t.Fatalf("the kept certificate was not served again: created %v, %v", created, err)
	}

	moved, created, err := secureCertificate(dir, []string{"localhost", "studio.local"}, []net.IP{office}, now.Add(time.Hour))
	if err != nil || !created {
		t.Fatalf("a new address did not make a new certificate: created %v, %v", created, err)
	}
	if !slices.ContainsFunc(moved.Leaf.IPAddresses, office.Equal) || !slices.ContainsFunc(moved.Leaf.IPAddresses, home.Equal) {
		t.Fatalf("addresses %v, want both networks", moved.Leaf.IPAddresses)
	}
	back, created, err := secureCertificate(dir, []string{"localhost", "studio.local"}, []net.IP{home}, now.Add(2*time.Hour))
	if err != nil || created || !bytes.Equal(back.Certificate[0], moved.Certificate[0]) {
		t.Fatalf("returning to the first network made yet another certificate: created %v, %v", created, err)
	}

	_, created, err = secureCertificate(dir, []string{"localhost", "studio.local"}, []net.IP{home}, moved.Leaf.NotAfter.Add(-29*24*time.Hour))
	if err != nil || !created {
		t.Fatalf("a certificate with a month to run was not renewed: created %v, %v", created, err)
	}
	if err := os.WriteFile(filepath.Join(dir, certificateFile), []byte("not a certificate"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, created, err = secureCertificate(dir, []string{"localhost"}, nil, now); err != nil || !created {
		t.Fatalf("an unreadable certificate was not replaced: created %v, %v", created, err)
	}
}

// A listener "auto" chose that cannot open leaves the server on plain HTTP; one
// asked for by address stops it.
func TestAnHTTPSListenerThatCannotOpenIsRequiredOnlyWhenAskedFor(t *testing.T) {
	blocked := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blocked, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	var logged bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logged, nil))
	secure, err := listenSecurely("auto", "0.0.0.0:59990", filepath.Join(blocked, "tls"), logger)
	if err != nil || secure != nil {
		t.Fatalf("auto: listener %v, error %v", secure, err)
	}
	if !strings.Contains(logged.String(), "serving without HTTPS") {
		t.Fatalf("no warning: %s", logged.String())
	}
	if _, err := listenSecurely("127.0.0.1:0", "127.0.0.1:11541", filepath.Join(blocked, "tls"), logger); err == nil {
		t.Fatal("an HTTPS address asked for failed silently")
	}
}

// **The page and its session work over HTTPS.** The same handler answers on
// both listeners; the session socket upgrades over TLS with the page's own
// origin; stopping the server stops both.
func TestThePageAndItsSessionAreServedOverHTTPS(t *testing.T) {
	root := t.TempDir()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	handler, err := webhost.New(webhost.Options{
		Client: web.Client(), GameRoot: filepath.Join(root, "games"), SaveRoot: filepath.Join(root, "savedata", "ktf"),
		LogRoot: filepath.Join(root, "logs"), Logger: logger,
	})
	if err != nil {
		t.Fatalf("webhost.New: %v", err)
	}
	listener, err := listen("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	certificate, _, err := secureCertificate(filepath.Join(root, "tls"), []string{"localhost"}, []net.IP{net.IPv4(127, 0, 0, 1).To4()}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	plain, err := listen("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	secure := tls.NewListener(plain, &tls.Config{Certificates: []tls.Certificate{certificate}, MinVersion: tls.VersionTLS12})
	requested := make(chan struct{})
	served := make(chan error, 1)
	go func() { served <- serve(listener, secure, handler, logger, requested) }()

	trusted := x509.NewCertPool()
	trusted.AddCert(certificate.Leaf)
	client := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: trusted}}, Timeout: 10 * time.Second}
	origin := "https://" + secure.Addr().String()
	response, err := client.Get(origin + "/")
	if err != nil {
		t.Fatalf("GET over HTTPS: %v", err)
	}
	page, _ := io.ReadAll(response.Body)
	response.Body.Close()
	if response.StatusCode != http.StatusOK || !bytes.Contains(page, []byte("<html")) {
		t.Fatalf("GET / over HTTPS: %d %q", response.StatusCode, page[:min(len(page), 80)])
	}

	connection, err := tls.Dial("tcp", secure.Addr().String(), &tls.Config{RootCAs: trusted})
	if err != nil {
		t.Fatalf("dial the session over TLS: %v", err)
	}
	defer connection.Close()
	handshake := "GET /api/session HTTP/1.1\r\nHost: " + secure.Addr().String() + "\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n" +
		"Sec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==\r\nSec-WebSocket-Version: 13\r\nOrigin: " + origin + "\r\n\r\n"
	if _, err := connection.Write([]byte(handshake)); err != nil {
		t.Fatal(err)
	}
	_ = connection.SetReadDeadline(time.Now().Add(10 * time.Second))
	status, err := bufio.NewReader(connection).ReadString('\n')
	if err != nil || !strings.HasPrefix(status, "HTTP/1.1 101") {
		t.Fatalf("session upgrade over TLS: %q, %v", status, err)
	}

	close(requested)
	select {
	case err := <-served:
		if err != nil {
			t.Fatalf("serve: %v", err)
		}
	case <-time.After(webhost.StopDeadline + 5*time.Second):
		t.Fatal("the server did not stop")
	}
	if _, err := client.Get(origin + "/"); err == nil {
		t.Fatal("the HTTPS listener still answers after the stop")
	}
}
