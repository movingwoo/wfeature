package lgt

import (
	"bytes"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/movingwoo/wfeature/internal/backend"
)

// What automatic authentication keeps for the length of one run
//
// A session that matched one of the recognised offline exchanges runs with an
// adapter between the title and its saves, or with a bounded local service in
// place of the network, and each of those holds state the title has moved: a
// certificate it rewrote, the stage an exchange has reached, a callback it is
// waiting on. None of it is in a save file — keeping it out of one is the
// point — so a checkpoint that left it behind would restore a title half way
// through an exchange that had been put back to the start.
//
// **The part that is derived is derived again, and only the part that moved is
// kept.** Which exchange a module is a client of is read from the module's own
// code, the same way it was when the session started, and a record that claims
// an adapter this module does not match is refused.
//
// What an adapter read from the store is derived again as well, from the store
// the restored session runs over: the word the options adapter hides, the two
// path lists the 58-byte adapter answers from its own copy, and the flag and
// certificate the 100-byte adapter writes back into every store of its file.
// Kept in a record, those would be older bytes that an adapter puts back over
// newer ones. What a record keeps is what was never in a file: the certificate
// an adapter issued for the run, and whether the title removed or made it.
//
// The 100-byte adapter's flag and certificate are also in the record, as the
// one fallback: they are used when the file they came from is gone or no
// longer has a header this adapter reads, because the adapter cannot store
// that file at all without them, and zeros in their place would be a
// certificate the title never had.

const (
	adapterStoreNone           = ""
	adapterStoreOptions        = "options"
	adapterStoreCertificate58  = "certificate-58"
	adapterStoreCertificate100 = "certificate-100"

	// maxAdapterBytes bounds what an adapter record may hold. The largest thing
	// one keeps is a save file's worth of bytes.
	maxAdapterBytes = 1 << 20
)

type callbackState struct {
	Address, Param uint32
	Serial         uint64
}

type socketState struct {
	Descriptor                         uint32
	Connected, Failed                  bool
	Stage                              uint8
	Request, Response, PendingResponse []byte
	Connect, Read, Write               callbackState
}

type networkState struct {
	Protocol uint8
	Active   bool
	Next     uint32
	Serial   uint64
	Sockets  []socketState
}

// adapterStateVersion is the layout of adapterState. Version 1 carried the
// adapters' copies of what they had read from the store.
const adapterStateVersion = 2

// adapterState is the save adapter a session runs under, if it has one, and
// the local service, if it has one. A session has at most one of each.
type adapterState struct {
	Version uint32
	Store   string

	// The private certificate of the 58-byte adapter, and of the 100-byte
	// adapter once it is active.
	Certificate []byte
	// The 58-byte adapter answers the two path lists from its own copies, in
	// which the certificate is listed or not as the title left it.
	CertificateRemoved, CertificateCreated bool

	// The embedded 100-byte certificate adapter. The flag and the certificate
	// it found in its file are the fallback described above.
	Active              bool
	OriginalFlag        uint8
	OriginalCertificate []byte

	Network *networkState
}

// captureAdapters records the adapter above the Host's store and answers the
// store underneath it. It reads the adapter's own memory and makes no store
// call.
func (client *Client) captureAdapters() (adapterState, backend.SaveStore, error) {
	saved := adapterState{Version: adapterStateVersion}
	base := client.saveStore
	switch store := client.saveStore.(type) {
	case *authenticationOptionStore:
		store.mu.Lock()
		if store.readError != nil {
			store.mu.Unlock()
			return adapterState{}, nil, fmt.Errorf("LGT checkpoint cannot capture after a failed save read: %w", store.readError)
		}
		saved.Store = adapterStoreOptions
		base = store.base
		store.mu.Unlock()
	case *authenticationCertificate58Store:
		store.mu.Lock()
		saved.Store, saved.Certificate = adapterStoreCertificate58, bytes.Clone(store.certificate)
		saved.CertificateRemoved = certificate58Listed(store.ledgers[fileRemovedKey])
		saved.CertificateCreated = certificate58Listed(store.ledgers[fileCreatedKey])
		base = store.base
		store.mu.Unlock()
	case *authenticationCertificate100Store:
		store.mu.Lock()
		saved.Store, saved.Active, saved.OriginalFlag = adapterStoreCertificate100, store.active, store.originalFlag
		saved.OriginalCertificate, saved.Certificate = bytes.Clone(store.originalCertificate), bytes.Clone(store.certificate)
		base = store.base
		store.mu.Unlock()
	}
	if network := client.notificationNetwork; network != nil {
		record := &networkState{Protocol: uint8(network.contract.protocol), Active: network.active, Next: network.next, Serial: network.serial}
		for _, descriptor := range slices.Sorted(maps.Keys(network.sockets)) {
			socket := network.sockets[descriptor]
			if socket == nil {
				return adapterState{}, nil, fmt.Errorf("LGT checkpoint has a missing local socket")
			}
			record.Sockets = append(record.Sockets, socketState{
				Descriptor: descriptor, Connected: socket.connected, Failed: socket.failed, Stage: socket.stage,
				Request: bytes.Clone(socket.request), Response: bytes.Clone(socket.response),
				PendingResponse: bytes.Clone(socket.pendingResponse),
				Connect:         callbackState{socket.connect.address, socket.connect.param, socket.connect.serial},
				Read:            callbackState{socket.read.address, socket.read.param, socket.read.serial},
				Write:           callbackState{socket.write.address, socket.write.param, socket.write.serial},
			})
		}
		saved.Network = record
	}
	return saved, base, saved.validate()
}

func (saved adapterState) validate() error {
	invalid := func(what string) error { return fmt.Errorf("LGT checkpoint authentication adapter has %s", what) }
	if saved.Version != adapterStateVersion {
		return invalid("an unsupported version")
	}
	if len(saved.Certificate)+len(saved.OriginalCertificate) > maxAdapterBytes {
		return invalid("more data than an adapter holds")
	}
	certificate58 := saved.CertificateRemoved || saved.CertificateCreated
	certificate100 := saved.Active || saved.OriginalFlag != 0 || saved.OriginalCertificate != nil
	switch saved.Store {
	case adapterStoreNone, adapterStoreOptions:
		if certificate58 || certificate100 || saved.Certificate != nil {
			return invalid("fields with no adapter to hold them")
		}
	case adapterStoreCertificate58:
		// The certificate is whatever the title last stored under the name,
		// and a title can leave it at any length: empty when its open made
		// the file, short while a write is half done.
		if certificate100 {
			return invalid("fields another adapter holds")
		}
	case adapterStoreCertificate100:
		if certificate58 || saved.Active && (len(saved.Certificate) != 100 || len(saved.OriginalCertificate) != 100) ||
			!saved.Active && (saved.Certificate != nil || saved.OriginalCertificate != nil || saved.OriginalFlag != 0) {
			return invalid("fields another adapter holds")
		}
	default:
		return invalid("an unknown kind")
	}
	if network := saved.Network; network != nil {
		if saved.Store != adapterStoreNone || network.Protocol > uint8(localCertificateMessageProtocol) || len(network.Sockets) > 4 {
			return invalid("an invalid local service")
		}
		for index, socket := range network.Sockets {
			if index > 0 && network.Sockets[index-1].Descriptor >= socket.Descriptor ||
				socket.Descriptor <= 100 || socket.Descriptor-100 > network.Next ||
				len(socket.Request) > 100 || len(socket.Response) > 65536 || len(socket.PendingResponse) > 65536 ||
				socket.Connect.Serial > network.Serial || socket.Read.Serial > network.Serial || socket.Write.Serial > network.Serial {
				return invalid("an invalid local socket")
			}
		}
	}
	return nil
}

// checkAdapters is the part of restoring an adapter that needs no store: the
// record is checked, the module is matched against the exchange the record
// names, and the local service is rebuilt. It is given a client whose memory
// is already restored. The adapter itself is built when the load commits, by
// attachAdapters, over the store the session will run on.
func (client *Client) checkAdapters(archive *Archive, saved adapterState) error {
	if err := saved.validate(); err != nil {
		return err
	}
	mismatch := fmt.Errorf("LGT checkpoint authentication adapter does not match this module")
	client.notificationNetwork = nil
	switch saved.Store {
	case adapterStoreOptions:
		if !authenticationOptions(client.module) {
			return mismatch
		}
	case adapterStoreCertificate58:
		if !authenticationCertificate58(client.module) {
			return mismatch
		}
	case adapterStoreCertificate100:
		if authenticationCertificate100(client.module) == nil {
			return mismatch
		}
	}
	if saved.Network == nil {
		// A dial the local service accepted is answered by that service when it
		// comes due, so a record that has one has the service too.
		for _, dial := range client.netConnects {
			if dial.offline {
				return fmt.Errorf("LGT checkpoint has a dial accepted by a local service it does not have")
			}
		}
		return nil
	}
	network, _ := client.recognizedNetwork(archive, false)
	if network == nil && authenticationCertificate100(client.module) == nil {
		network, _ = client.recognizedNetwork(archive, true)
	}
	if network == nil || uint8(network.contract.protocol) != saved.Network.Protocol {
		return mismatch
	}
	network.active, network.next, network.serial = saved.Network.Active, saved.Network.Next, saved.Network.Serial
	if len(saved.Network.Sockets) != 0 {
		network.sockets = make(map[uint32]*notificationSocketState, len(saved.Network.Sockets))
	}
	for _, socket := range saved.Network.Sockets {
		network.sockets[socket.Descriptor] = &notificationSocketState{
			connected: socket.Connected, failed: socket.Failed, stage: socket.Stage,
			request: bytes.Clone(socket.Request), response: bytes.Clone(socket.Response),
			pendingResponse: bytes.Clone(socket.PendingResponse),
			connect:         notificationCallback{socket.Connect.Address, socket.Connect.Param, socket.Connect.Serial},
			read:            notificationCallback{socket.Read.Address, socket.Read.Param, socket.Read.Serial},
			write:           notificationCallback{socket.Write.Address, socket.Write.Param, socket.Write.Serial},
		}
	}
	client.notificationNetwork = network
	return nil
}

// attachAdapters builds the recorded adapter over a store and makes it the
// client's store. What the adapter read from a store at the start of a session
// it reads here, from this one, and nothing is written. An error is a read
// that failed.
func (client *Client) attachAdapters(archive *Archive, saved adapterState, base backend.SaveStore) error {
	switch saved.Store {
	case adapterStoreOptions:
		store := newAuthenticationOptionStore(base, archive)
		if store.readError != nil {
			return store.readError
		}
		client.saveStore = store
	case adapterStoreCertificate58:
		store := &authenticationCertificate58Store{
			base: base, certificate: bytes.Clone(saved.Certificate),
			ledgers: make(map[string][]byte), originalMembership: make(map[string]bool),
		}
		for key, listed := range map[string]bool{fileRemovedKey: saved.CertificateRemoved, fileCreatedKey: saved.CertificateCreated} {
			original, _, err := backend.ReadSave(base, key)
			if err != nil {
				return err
			}
			store.originalMembership[key] = certificate58Listed(original)
			store.ledgers[key] = certificate58Ledger(original, listed)
		}
		client.saveStore = store
	case adapterStoreCertificate100:
		store := newAuthenticationCertificate100Store(base, authenticationCertificate100(client.module))
		if saved.Active {
			store.originalFlag, store.originalCertificate = saved.OriginalFlag, bytes.Clone(saved.OriginalCertificate)
			data, present, err := backend.ReadSave(base, store.key)
			if err != nil {
				return err
			}
			// The file as it is now says what the adapter has to keep in it.
			// The record's copy stands in only where the file cannot say.
			if header, valid := store.header(data); present && valid {
				store.originalFlag, store.originalCertificate = header[27], bytes.Clone(data[100:200])
			}
			store.certificate = bytes.Clone(saved.Certificate)
			store.bind(client)
			store.active = true
		}
		client.saveStore = store
	default:
		client.saveStore = base
	}
	return nil
}

// certificate58Listed reports whether a path list names the 58-byte adapter's
// certificate.
func certificate58Listed(list []byte) bool {
	for _, name := range strings.Split(string(list), "\n") {
		if strings.TrimSpace(name) == authenticationCertificate58Name {
			return true
		}
	}
	return false
}

// rebaseAdapters moves an adapter onto another store. It reads and writes
// nothing and cannot fail, which is why a load uses it for its last step: the
// restored adapter moves from the reader it was built over onto the store.
func (client *Client) rebaseAdapters(base backend.SaveStore) {
	switch store := client.saveStore.(type) {
	case *authenticationOptionStore:
		store.mu.Lock()
		store.base = base
		store.mu.Unlock()
	case *authenticationCertificate58Store:
		store.mu.Lock()
		store.base = base
		store.mu.Unlock()
	case *authenticationCertificate100Store:
		store.mu.Lock()
		store.base = base
		store.mu.Unlock()
	default:
		client.saveStore = base
	}
}
