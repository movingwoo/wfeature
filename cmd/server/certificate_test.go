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
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/movingwoo/wfeature/internal/launcher"
	"github.com/movingwoo/wfeature/internal/webhost"
	"github.com/movingwoo/wfeature/web"
)

// **HTTPS goes where a phone reaches the server, or where it is told.** The
// default address binds every interface, so the HTTPS listener takes the port
// 100 above it; a loopback address is a secure context already and a socket is
// a proxy's to secure, so neither gets one unless it is asked for. A bare port
// goes on -addr's host, as `stop 11542` takes one for the plain listener.
func TestTheHTTPSListenerGoesWhereAPhoneReachesItOrWhereItIsTold(t *testing.T) {
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
		{"11700", ":11541", ":11700"},
		{"11700", "192.168.0.5:11541", "192.168.0.5:11700"},
		{"11700", "127.0.0.1:11541", "127.0.0.1:11700"},
		{"11700", "unix:/run/wfeature.sock", ":11700"},
		{":9443", "127.0.0.1:11541", ":9443"},
		{"192.168.0.5:9443", ":11541", "192.168.0.5:9443"},
	} {
		got, err := httpsAddress(probe.value, probe.address)
		if err != nil || got != probe.want {
			t.Errorf("httpsAddress(%q, %q) = %q, %v; want %q", probe.value, probe.address, got, err, probe.want)
		}
	}
	for _, value := range []string{"0", "70000", "https", "192.168.0.5", "on"} {
		if got, err := httpsAddress(value, ":11541"); err == nil {
			t.Errorf("httpsAddress(%q) = %q, want an error", value, got)
		}
	}
}

func TestTheHTTPSListenerReadsItsAddressAndPortFromTheEnvironment(t *testing.T) {
	t.Setenv("WFEATURE_HTTPS_ADDR", "")
	t.Setenv("WFEATURE_HTTPS_PORT", "")
	if got := defaultHTTPS(); got != "auto" {
		t.Fatalf("defaultHTTPS() = %q, want auto", got)
	}
	t.Setenv("WFEATURE_HTTPS_PORT", "11700")
	if got := defaultHTTPS(); got != "11700" {
		t.Fatalf("defaultHTTPS() = %q, want the port", got)
	}
	t.Setenv("WFEATURE_HTTPS_ADDR", "off")
	if got := defaultHTTPS(); got != "off" {
		t.Fatalf("defaultHTTPS() = %q, want the address over the port", got)
	}
}

// status names the HTTPS address a phone opens, beside the plain ones.
func TestStatusNamesTheHTTPSAddress(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		_, _ = io.WriteString(writer, `{"server":"wfeature","profile":"release","version":"9.9.9","pid":4242,"https_port":11641}`)
	})}
	go func() { _ = server.Serve(listener) }()
	defer server.Close()
	read, write, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	if err := reportStatus([]string{"-port", strconv.Itoa(port)}, write, write); err != nil {
		t.Fatal(err)
	}
	write.Close()
	answer, _ := io.ReadAll(read)
	if !strings.Contains(string(answer), "  https      https://") || !strings.Contains(string(answer), ":11641\n") {
		t.Fatalf("status said:\n%s", answer)
	}
}

// **One certificate, kept.** Each new certificate is a new warning on every
// phone, so the one written first is served again while it names every
// address a person opens and has a month to run. A new one of those makes a
// new certificate that still names the old addresses, so a machine moving
// between two networks settles.
// Apple's platforms refuse any server certificate valid longer than 825 days.
func TestTheSelfSignedCertificateIsMadeOnceAndKept(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "tls")
	home, office := net.ParseIP("192.168.0.60").To4(), net.ParseIP("10.1.2.3").To4()
	now := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	first, created, err := secureCertificate(dir, []string{"localhost", "studio.local"}, []net.IP{home}, nil, now)
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

	again, created, err := secureCertificate(dir, []string{"localhost", "studio.local"}, []net.IP{home}, nil, now.Add(400*24*time.Hour))
	if err != nil || created || !bytes.Equal(again.Certificate[0], first.Certificate[0]) {
		t.Fatalf("the kept certificate was not served again: created %v, %v", created, err)
	}

	moved, created, err := secureCertificate(dir, []string{"localhost", "studio.local"}, []net.IP{office}, nil, now.Add(time.Hour))
	if err != nil || !created {
		t.Fatalf("a new address did not make a new certificate: created %v, %v", created, err)
	}
	if !slices.ContainsFunc(moved.Leaf.IPAddresses, office.Equal) || !slices.ContainsFunc(moved.Leaf.IPAddresses, home.Equal) {
		t.Fatalf("addresses %v, want both networks", moved.Leaf.IPAddresses)
	}
	back, created, err := secureCertificate(dir, []string{"localhost", "studio.local"}, []net.IP{home}, nil, now.Add(2*time.Hour))
	if err != nil || created || !bytes.Equal(back.Certificate[0], moved.Certificate[0]) {
		t.Fatalf("returning to the first network made yet another certificate: created %v, %v", created, err)
	}

	_, created, err = secureCertificate(dir, []string{"localhost", "studio.local"}, []net.IP{home}, nil, moved.Leaf.NotAfter.Add(-29*24*time.Hour))
	if err != nil || !created {
		t.Fatalf("a certificate with a month to run was not renewed: created %v, %v", created, err)
	}
	if err := os.WriteFile(filepath.Join(dir, certificateFile), []byte("not a certificate"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, created, err = secureCertificate(dir, []string{"localhost"}, nil, nil, now); err != nil || !created {
		t.Fatalf("an unreadable certificate was not replaced: created %v, %v", created, err)
	}
}

// **An address nobody opens keeps the certificate.** An operating system
// replaces a temporary IPv6 address about once a day and gives a virtual
// adapter a new one at every boot, and a machine can have more addresses than
// a certificate keeps. None is the address a phone is told to open, so none
// makes a new certificate, which would be a new warning on every phone. A
// certificate made anyway names them, the opened ones first and all of them.
func TestAnAddressNobodyOpensKeepsTheCertificate(t *testing.T) {
	names := []string{"localhost", "studio.local"}
	opened := []net.IP{net.IPv4(127, 0, 0, 1).To4(), net.IPv6loopback, net.ParseIP("192.168.0.60").To4()}
	now := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	temporary := func(day int) net.IP { return net.ParseIP("2001:db8:1:2:bbbb:" + strconv.Itoa(day) + ":2:3") }

	dir := filepath.Join(t.TempDir(), "tls")
	first, _, err := secureCertificate(dir, names, opened, []net.IP{temporary(0)}, now)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.ContainsFunc(first.Leaf.IPAddresses, temporary(0).Equal) {
		t.Fatalf("addresses %v leave out the machine's other address", first.Leaf.IPAddresses)
	}
	for day := 1; day <= 7; day++ {
		served, created, err := secureCertificate(dir, names, opened, []net.IP{temporary(day)}, now.Add(time.Duration(day)*24*time.Hour))
		if err != nil {
			t.Fatal(err)
		}
		if created || !bytes.Equal(served.Certificate[0], first.Certificate[0]) {
			t.Fatalf("day %d: a new temporary IPv6 address made a new certificate", day)
		}
	}

	crowded := filepath.Join(t.TempDir(), "tls")
	var adapters []net.IP
	for index := 1; index <= maxCertificateAddresses+2; index++ {
		adapters = append(adapters, net.IPv4(172, 17, byte(index), 1).To4())
	}
	made, _, err := secureCertificate(crowded, names, opened, adapters, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(made.Leaf.IPAddresses) != maxCertificateAddresses || !slices.EqualFunc(made.Leaf.IPAddresses[:len(opened)], opened, net.IP.Equal) {
		t.Fatalf("addresses %v, want the opened ones first and %d in all", made.Leaf.IPAddresses, maxCertificateAddresses)
	}
	again, created, err := secureCertificate(crowded, names, opened, adapters, now.Add(time.Hour))
	if err != nil || created || !bytes.Equal(again.Certificate[0], made.Certificate[0]) {
		t.Fatalf("nothing changed between two starts, yet the second made a new certificate: created %v, %v", created, err)
	}
}

// **A new host name keeps the certificate.** macOS takes its host name from
// the network's DHCP, so a laptop that moves between two networks has another
// at each move, and a kept certificate asked for it was made again at each
// one, a new warning on every phone. Only localhost and the opened addresses
// are asked of a kept certificate; a new one, made for a new opened address,
// keeps the names the old one had, up to maxCertificateNames.
func TestAHostNameChangeKeepsTheCertificate(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "tls")
	home := []net.IP{net.IPv4(127, 0, 0, 1).To4(), net.ParseIP("192.168.0.60").To4()}
	named := func(host string) []string { return []string{"localhost", host, host + ".local"} }
	now := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	first, _, err := secureCertificate(dir, named("home-mac"), home, nil, now)
	if err != nil {
		t.Fatal(err)
	}
	for hour, host := range []string{"office-mac", "home-mac", "cafe-mac"} {
		served, created, err := secureCertificate(dir, named(host), home, nil, now.Add(time.Duration(hour+1)*time.Hour))
		if err != nil || created || !bytes.Equal(served.Certificate[0], first.Certificate[0]) {
			t.Fatalf("the host name %s made a new certificate: created %v, %v", host, created, err)
		}
	}

	office := append(slices.Clone(home), net.ParseIP("10.1.2.3").To4())
	moved, created, err := secureCertificate(dir, named("office-mac"), office, nil, now.Add(5*time.Hour))
	if err != nil || !created {
		t.Fatalf("a new opened address did not make a new certificate: created %v, %v", created, err)
	}
	if want := []string{"localhost", "office-mac", "office-mac.local", "home-mac", "home-mac.local"}; !slices.Equal(moved.Leaf.DNSNames, want) {
		t.Fatalf("names %v, want %v: this network's, then the ones the old certificate had", moved.Leaf.DNSNames, want)
	}

	var earlier []string
	for index := range maxCertificateNames + 4 {
		earlier = append(earlier, "host"+strconv.Itoa(index))
	}
	if kept := mergeNames(named("studio"), earlier); len(kept) != maxCertificateNames || !slices.Equal(kept[:3], named("studio")) {
		t.Fatalf("mergeNames kept %v, want this machine's names first and %d in all", kept, maxCertificateNames)
	}
}

// The addresses a kept certificate has to name are loopback, the one the
// server prints for another device, and the one the HTTPS listener was given.
func TestTheOpenedAddressesAreTheOnesAPersonIsGiven(t *testing.T) {
	lan := net.ParseIP(launcher.LANAddress())
	for _, probe := range []struct {
		address string
		given   net.IP
	}{
		{"[::]:11641", nil},
		{"0.0.0.0:11641", nil},
		{":11641", nil},
		{"192.168.0.5:11700", net.ParseIP("192.168.0.5").To4()},
	} {
		got := openedAddresses(probe.address)
		want := []net.IP{net.IPv4(127, 0, 0, 1).To4(), net.IPv6loopback}
		for _, address := range []net.IP{lan, probe.given} {
			if address != nil && !slices.ContainsFunc(want, address.Equal) {
				want = append(want, address)
			}
		}
		if !slices.EqualFunc(got, want, net.IP.Equal) {
			t.Errorf("openedAddresses(%q) = %v, want %v", probe.address, got, want)
		}
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

// **An HTTPS address this machine does not have keeps the certificate.** The
// address given to -https is one a kept certificate has to name, and the
// certificate used to be looked at before the port was opened: a typing slip
// made a new certificate, a new warning on every phone, for a server that then
// did not start.
func TestAnHTTPSAddressThatCannotOpenKeepsTheCertificate(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "tls")
	if _, _, err := secureCertificate(dir, reachableNames(), openedAddresses("[::]:11641"), reachableAddresses(), time.Now()); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(filepath.Join(dir, certificateFile))
	if err != nil {
		t.Fatal(err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	// 192.0.2.1 is TEST-NET-1 (RFC 5737), an address no machine is given.
	if listener, err := listenSecurely("192.0.2.1:11641", "0.0.0.0:11541", dir, logger); err == nil {
		listener.Close()
		t.Fatal("an HTTPS address this machine does not have was opened")
	}
	after, err := os.ReadFile(filepath.Join(dir, certificateFile))
	if err != nil || !bytes.Equal(after, before) {
		t.Fatalf("a listener that could not open replaced the certificate (%v)", err)
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
	certificate, _, err := secureCertificate(filepath.Join(root, "tls"), []string{"localhost"}, []net.IP{net.IPv4(127, 0, 0, 1).To4()}, nil, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	plain, err := listen("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	secure := secureListener(plain, certificate)
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
