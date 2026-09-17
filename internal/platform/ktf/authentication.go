package ktf

import (
	"bytes"
	"fmt"
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
	readError       error
}

func newCertificateSaveStore(base SaveStore, certificate []byte) *certificateSaveStore {
	store := &certificateSaveStore{base: base, certificate: bytes.Clone(certificate)}
	if base != nil {
		data, _, err := backend.ReadSave(base, databaseRemovedKey)
		if err != nil {
			store.readError = err
			return store
		}
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
	data, present, _ := store.ReadSave(name)
	return data, present
}

func (store *certificateSaveStore) ReadSave(name string) ([]byte, bool, error) {
	key, err := NormalizeSaveKey(name)
	if err != nil {
		return nil, false, err
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	switch key {
	case certificateSaveKey:
		return bytes.Clone(store.certificate), true, nil
	case databaseRemovedKey:
		return bytes.Clone(store.removed), true, store.readError
	}
	return backend.ReadSave(store.base, key)
}

func (store *certificateSaveStore) StoreSave(name string, data []byte) error {
	return store.StoreSaves(map[string][]byte{name: data})
}

func (store *certificateSaveStore) StoreSaves(entries map[string][]byte) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	staged := make(map[string][]byte, len(entries))
	certificate, removed := store.certificate, store.removed
	seen := make(map[string]bool)
	for name, data := range entries {
		key, err := NormalizeSaveKey(name)
		if err != nil {
			return err
		}
		if seen[key] {
			return fmt.Errorf("duplicate canonical save key %q", key)
		}
		seen[key] = true
		switch key {
		case certificateSaveKey:
			certificate = bytes.Clone(data)
			continue
		case databaseRemovedKey:
			if store.readError != nil {
				return store.readError
			}
			removed = bytes.Clone(data)
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
		staged[key] = data
	}
	if err := backend.StoreSaves(store.base, staged); err != nil {
		return err
	}
	store.certificate, store.removed = certificate, removed
	return nil
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
