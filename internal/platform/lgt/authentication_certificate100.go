package lgt

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"sync"

	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/wipic"
)

// Two 100-byte records share a file with ordinary progress. Only the
// certificate and its presence flag are private; all other bytes persist.
type authenticationCertificate100Store struct {
	mu                               sync.Mutex
	base                             backend.SaveStore
	volatile                         map[string][]byte
	contract                         certificate100Contract
	key                              string
	active                           bool
	publishHeader                    func([]byte) bool
	originalFlag                     byte
	originalCertificate, certificate []byte
}

func newAuthenticationCertificate100Store(base backend.SaveStore, contract *certificate100Contract) *authenticationCertificate100Store {
	key, _ := fileSaveKey(contract.name)
	return &authenticationCertificate100Store{base: base, contract: *contract, key: key, volatile: make(map[string][]byte)}
}

func cryptCertificate100(data []byte, seed, multiplier uint32, encode bool) {
	for i, value := range data {
		data[i] = value ^ byte(seed>>8)
		if encode {
			value = data[i]
		}
		seed = uint32(byte(seed+uint32(value))) * multiplier
	}
}

func (store *authenticationCertificate100Store) header(data []byte) ([]byte, bool) {
	if len(data) < 200 {
		return nil, false
	}
	header := bytes.Clone(data[:100])
	cryptCertificate100(header, store.contract.headerSeed, store.contract.multiplier, false)
	var sum uint32
	for _, b := range header[:48] {
		sum += uint32(b)
	}
	return header, sum == binary.LittleEndian.Uint32(header[48:52])
}

func (store *authenticationCertificate100Store) putHeader(data, header []byte, flag byte) {
	header[27] = flag
	var sum uint32
	for _, b := range header[:48] {
		sum += uint32(b)
	}
	binary.LittleEndian.PutUint32(header[48:52], sum)
	cryptCertificate100(header, store.contract.headerSeed, store.contract.multiplier, true)
	copy(data, header)
}

// Activate after initialization has created or read the ordinary save. This
// also handles a first run without inventing game progress or default settings.
func (store *authenticationCertificate100Store) activate(client *Client) bool {
	if client.archive.Descriptor.AID != store.contract.application || wipic.ValidateSubscriberNumber(client.subscriberNumber) != nil {
		return false
	}
	plain := []byte(client.subscriberNumber + store.contract.application + store.contract.token)
	if len(plain) >= 100 {
		return false
	}
	data, present := client.readFile(store.contract.name)
	header, valid := store.header(data)
	if !present || !valid {
		return false
	}
	live := make([]byte, 48)
	if err := client.core.Memory().Read(store.contract.state, live); err != nil || !bytes.Equal(live, header[:48]) {
		return false
	}
	// Do not replace a file while the guest still holds its previous contents.
	for _, file := range client.files {
		if canonicalFileName(file.name) == canonicalFileName(store.contract.name) {
			return false
		}
	}
	cryptCertificate100(plain, store.contract.seed, store.contract.multiplier, true)
	certificate := make([]byte, 100)
	copy(certificate, plain)
	if err := client.core.Memory().Write(store.contract.state+27, []byte{1}); err != nil {
		return false
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	store.originalFlag = header[27]
	store.originalCertificate = bytes.Clone(data[100:200])
	store.certificate = certificate
	store.publishHeader = func(header []byte) bool {
		current := make([]byte, 48)
		if err := client.core.Memory().Read(store.contract.state, current); err != nil {
			return false
		}
		// The private presence bit may already differ from backing storage.
		current[27] = header[27]
		if !bytes.Equal(current, header[:48]) {
			return false
		}
		return client.core.Memory().Write(store.contract.state+27, []byte{1}) == nil
	}
	store.active = true
	return true
}

func (store *authenticationCertificate100Store) LoadSave(name string) ([]byte, bool) {
	data, present, _ := store.ReadSave(name)
	return data, present
}

func (store *authenticationCertificate100Store) ReadSave(name string) ([]byte, bool, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	var data []byte
	var present bool
	var err error
	if store.base != nil {
		data, present, err = backend.ReadSave(store.base, name)
	} else {
		data, present = store.volatile[name]
	}
	data = bytes.Clone(data)
	if err == nil && present && store.active && name == store.key {
		if header, valid := store.header(data); valid {
			store.putHeader(data, header, 1)
			copy(data[100:200], store.certificate)
		}
	}
	return data, present, err
}

func (store *authenticationCertificate100Store) StoreSave(name string, data []byte) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	data = bytes.Clone(data)
	if store.active && name == store.key && len(data) > 0 {
		header, valid := store.header(data)
		if !valid {
			return fmt.Errorf("invalid embedded certificate options header")
		}
		// A guest can initialize settings again after its first notice.
		// Keep its matched cached presence bit coherent with the view.
		if store.publishHeader != nil {
			store.publishHeader(header)
		}
		store.putHeader(data, header, store.originalFlag)
		copy(data[100:200], store.originalCertificate)
	}
	if store.base != nil {
		return store.base.StoreSave(name, data)
	}
	store.volatile[name] = data
	return nil
}
