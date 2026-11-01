package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// A page opened by the machine's address over plain HTTP is not a secure
// context: the browser gives it no AudioWorklet, no game controller and no
// service worker. The same page over HTTPS is one, even under a certificate
// nobody vouches for once the person has accepted the warning, so the server
// presents a self-signed certificate of its own. It is kept, because every
// new certificate is a new warning on every phone.
const (
	certificateFile = "cert.pem"
	keyFile         = "key.pem"
	// Apple's platforms refuse a TLS server certificate valid for longer than
	// 825 days, accepted warning or not.
	certificateLifetime = 800 * 24 * time.Hour
	// A certificate this close to its end is replaced before it runs out.
	certificateRenewal = 30 * 24 * time.Hour
	// Addresses the certificate already names stay in a new one, so a laptop
	// that moves between two networks settles on one certificate for both;
	// this bounds how many it keeps.
	maxCertificateAddresses = 32
)

// secureCertificate answers the certificate the HTTPS listener presents: the
// one kept in dir while it names every given name and address and has more
// than a month to run, otherwise a new one written there, which keeps the
// addresses the old one named. created says which.
func secureCertificate(dir string, names []string, addresses []net.IP, now time.Time) (certificate tls.Certificate, created bool, err error) {
	certificatePath, keyPath := filepath.Join(dir, certificateFile), filepath.Join(dir, keyFile)
	// A missing or unreadable pair is replaced rather than served.
	var previous []net.IP
	if kept, err := tls.LoadX509KeyPair(certificatePath, keyPath); err == nil {
		if leaf, err := x509.ParseCertificate(kept.Certificate[0]); err == nil {
			if covers(leaf, names, addresses) && leaf.NotAfter.Sub(now) > certificateRenewal && !now.Before(leaf.NotBefore) {
				kept.Leaf = leaf
				return kept, false, nil
			}
			previous = leaf.IPAddresses
		}
	}

	wanted := mergeAddresses(addresses, previous)
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return tls.Certificate{}, false, fmt.Errorf("make the HTTPS key: %w", err)
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 127))
	if err != nil {
		return tls.Certificate{}, false, fmt.Errorf("make the HTTPS certificate serial: %w", err)
	}
	template := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: "wfeature", Organization: []string{"wfeature"}},
		NotBefore:             now.Add(-time.Hour),
		NotAfter:              now.Add(certificateLifetime),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		DNSNames:              names,
		IPAddresses:           wanted,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		return tls.Certificate{}, false, fmt.Errorf("make the HTTPS certificate: %w", err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return tls.Certificate{}, false, fmt.Errorf("encode the HTTPS key: %w", err)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return tls.Certificate{}, false, fmt.Errorf("make the certificate directory: %w", err)
	}
	if err := writeAtomically(keyPath, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}), 0o600); err != nil {
		return tls.Certificate{}, false, err
	}
	if err := writeAtomically(certificatePath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o644); err != nil {
		return tls.Certificate{}, false, err
	}
	leaf, err := x509.ParseCertificate(der)
	if err != nil {
		return tls.Certificate{}, false, fmt.Errorf("read back the HTTPS certificate: %w", err)
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key, Leaf: leaf}, true, nil
}

// covers reports whether a certificate names every name and address.
func covers(leaf *x509.Certificate, names []string, addresses []net.IP) bool {
	for _, name := range names {
		if !slices.Contains(leaf.DNSNames, name) {
			return false
		}
	}
	for _, address := range addresses {
		if !slices.ContainsFunc(leaf.IPAddresses, address.Equal) {
			return false
		}
	}
	return true
}

// mergeAddresses answers the current addresses followed by those an older
// certificate named, without repeats, at most maxCertificateAddresses.
func mergeAddresses(current, previous []net.IP) []net.IP {
	var merged []net.IP
	for _, address := range append(slices.Clone(current), previous...) {
		if len(merged) == maxCertificateAddresses {
			break
		}
		if !slices.ContainsFunc(merged, address.Equal) {
			merged = append(merged, address)
		}
	}
	return merged
}

func writeAtomically(path string, data []byte, mode os.FileMode) error {
	temporary, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*")
	if err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	defer os.Remove(temporary.Name())
	if err := temporary.Chmod(mode); err != nil {
		temporary.Close()
		return fmt.Errorf("write %s: %w", path, err)
	}
	if _, err := temporary.Write(data); err != nil {
		temporary.Close()
		return fmt.Errorf("write %s: %w", path, err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	if err := os.Rename(temporary.Name(), path); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

// reachableNames are the names a person may type for this machine: localhost,
// its host name, and that name on the local network's .local domain.
func reachableNames() []string {
	names := []string{"localhost"}
	host, err := os.Hostname()
	if err != nil {
		return names
	}
	host = strings.TrimSuffix(strings.ToLower(host), ".")
	if host == "" || strings.ContainsAny(host, " /\\:") {
		return names
	}
	names = append(names, host)
	if !strings.HasSuffix(host, ".local") && !strings.Contains(host, ".") {
		names = append(names, host+".local")
	}
	return names
}

// reachableAddresses are the machine's own addresses: loopback and every
// interface address a phone could use. Link-local IPv6 needs a zone no URL a
// person types carries, so it is left out.
func reachableAddresses() []net.IP {
	addresses := []net.IP{net.IPv4(127, 0, 0, 1).To4(), net.IPv6loopback}
	interfaces, err := net.InterfaceAddrs()
	if err != nil {
		return addresses
	}
	for _, entry := range interfaces {
		network, ok := entry.(*net.IPNet)
		if !ok || network.IP.IsLinkLocalUnicast() || network.IP.IsLinkLocalMulticast() {
			continue
		}
		address := network.IP
		if v4 := address.To4(); v4 != nil {
			address = v4
		}
		if !slices.ContainsFunc(addresses, address.Equal) {
			addresses = append(addresses, address)
		}
	}
	return addresses
}
