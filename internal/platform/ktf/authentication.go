package ktf

import (
	"bytes"
	"sync"

	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/wipic"
)

// Authentication reports the adaptation selected before the guest started.
func (session *Session) Authentication() backend.AuthenticationStatus {
	if session.Client.authentication == "" {
		return backend.AuthenticationOff
	}
	return session.Client.authentication
}

// A filename and an eight-bit checksum alone are too weak for automatic
// adaptation. Require the executable's cipher table and property/file names
// as well. No archive identity, title name or fixed code address is matched.
func authenticationCertificate(archive *Archive, number string) ([]byte, bool) {
	if archive == nil || archive.JAR == nil || wipic.ValidateSubscriberNumber(number) != nil {
		return nil, false
	}
	image := archive.JAR.Client.Data
	if !bytes.Contains(image, certificateTable[:]) ||
		!bytes.Contains(image, []byte("cert.c2s\x00")) ||
		!bytes.Contains(image, []byte("PHONENUMBER\x00")) {
		return nil, false
	}
	certificate, err := ProvisionCertificate(archive, number)
	if err != nil {
		return nil, false
	}
	return certificate.Data, true
}

// certificateSaveStore keeps the certificate and its deletion state local to
// this run. All other game progress still uses the original persistence path.
// In particular, writing the shared deletion ledger must preserve the original
// certificate's deletion bit while allowing other database removals to persist.
type certificateSaveStore struct {
	mu              sync.Mutex
	base            SaveStore
	certificate     []byte
	removed         []byte
	originalRemoved bool
}

func newCertificateSaveStore(base SaveStore, certificate []byte) *certificateSaveStore {
	store := &certificateSaveStore{base: base, certificate: bytes.Clone(certificate)}
	if base != nil {
		data, _ := base.LoadSave(databaseRemovedKey)
		var names []string
		for _, name := range splitRemovalList(data) {
			if name == certificateName {
				store.originalRemoved = true
			} else {
				names = append(names, name)
			}
		}
		store.removed = joinRemovalList(names)
	}
	return store
}

func (store *certificateSaveStore) LoadSave(name string) ([]byte, bool) {
	key, err := NormalizeSaveKey(name)
	if err != nil {
		return nil, false
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	switch key {
	case certificateSaveKey:
		return bytes.Clone(store.certificate), true
	case databaseRemovedKey:
		return bytes.Clone(store.removed), true
	}
	if store.base == nil {
		return nil, false
	}
	return store.base.LoadSave(key)
}

func (store *certificateSaveStore) StoreSave(name string, data []byte) error {
	key, err := NormalizeSaveKey(name)
	if err != nil {
		return err
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	switch key {
	case certificateSaveKey:
		store.certificate = bytes.Clone(data)
		return nil
	case databaseRemovedKey:
		store.removed = bytes.Clone(data)
		var names []string
		for _, entry := range splitRemovalList(data) {
			if entry != certificateName {
				names = append(names, entry)
			}
		}
		if store.originalRemoved {
			names = append(names, certificateName)
		}
		data = joinRemovalList(names)
	}
	if store.base == nil {
		return nil
	}
	return store.base.StoreSave(key, data)
}

// Every KTF identity API reads the same session snapshot used to issue the
// certificate. Host startup defaults are never changed by authentication.
func (client *Client) systemProperty(name string) (string, bool) {
	if name == "PHONENUMBER" || name == "MIN" {
		return client.subscriberNumber, true
	}
	value, ok := wipic.SystemProperties[name]
	return value, ok
}
