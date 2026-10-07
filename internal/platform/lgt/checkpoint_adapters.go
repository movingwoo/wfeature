package lgt

import (
	"bytes"
	"fmt"
	"maps"
	"slices"

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
// an adapter this module does not match is refused. Restoring one reads no
// save and writes none: the store underneath it is the caller's.

const (
	adapterStoreNone           = ""
	adapterStoreOptions        = "options"
	adapterStoreCertificate58  = "certificate-58"
	adapterStoreCertificate100 = "certificate-100"

	// maxAdapterBytes bounds what an adapter record may hold. The largest thing
	// one keeps is a save file's worth of bytes.
	maxAdapterBytes = 1 << 20
)

type adapterFileState struct{ Key, Data []byte }

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

// adapterState is the save adapter a session runs under, if it has one, and
// the local service, if it has one. A session has at most one of each.
type adapterState struct {
	Version uint32
	Store   string

	// The cached-authentication adapter: the word it hides, and the options
	// file it holds when there is no store to hold it.
	Original []byte
	Volatile []byte

	// The 58-byte certificate adapter: the private certificate, and the two
	// path lists as the title sees them.
	Certificate                  []byte
	RemovedLedger, CreatedLedger []byte
	RemovedMember, CreatedMember bool

	// The embedded 100-byte certificate adapter.
	Active              bool
	OriginalFlag        uint8
	OriginalCertificate []byte
	Files               []adapterFileState

	Network *networkState
}

// captureAdapters records the adapter above the Host's store and answers the
// store underneath it, which is the one whose files a checkpoint snapshots.
func (client *Client) captureAdapters() (adapterState, backend.SaveStore, error) {
	saved := adapterState{Version: 1}
	base := client.saveStore
	switch store := client.saveStore.(type) {
	case *authenticationOptionStore:
		store.mu.Lock()
		if store.readError != nil {
			store.mu.Unlock()
			return adapterState{}, nil, fmt.Errorf("LGT checkpoint cannot capture after a failed save read: %w", store.readError)
		}
		saved.Store, saved.Original = adapterStoreOptions, bytes.Clone(store.original[:])
		if store.volatile != nil {
			saved.Volatile = append([]byte{}, store.volatile...)
		}
		base = store.base
		store.mu.Unlock()
	case *authenticationCertificate58Store:
		store.mu.Lock()
		saved.Store, saved.Certificate = adapterStoreCertificate58, bytes.Clone(store.certificate)
		saved.RemovedLedger, saved.CreatedLedger = append([]byte{}, store.ledgers[fileRemovedKey]...), append([]byte{}, store.ledgers[fileCreatedKey]...)
		saved.RemovedMember, saved.CreatedMember = store.originalMembership[fileRemovedKey], store.originalMembership[fileCreatedKey]
		base = store.base
		store.mu.Unlock()
	case *authenticationCertificate100Store:
		store.mu.Lock()
		saved.Store, saved.Active, saved.OriginalFlag = adapterStoreCertificate100, store.active, store.originalFlag
		saved.OriginalCertificate, saved.Certificate = bytes.Clone(store.originalCertificate), bytes.Clone(store.certificate)
		for _, key := range slices.Sorted(maps.Keys(store.volatile)) {
			saved.Files = append(saved.Files, adapterFileState{[]byte(key), append([]byte{}, store.volatile[key]...)})
		}
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
	if saved.Version != 1 {
		return invalid("an unsupported version")
	}
	size := len(saved.Original) + len(saved.Volatile) + len(saved.Certificate) + len(saved.RemovedLedger) +
		len(saved.CreatedLedger) + len(saved.OriginalCertificate)
	for _, file := range saved.Files {
		size += len(file.Key) + len(file.Data)
	}
	if size > maxAdapterBytes || len(saved.Files) > 64 {
		return invalid("more data than an adapter holds")
	}
	options := len(saved.Original) != 0 || saved.Volatile != nil
	certificate58 := saved.RemovedLedger != nil || saved.CreatedLedger != nil || saved.RemovedMember || saved.CreatedMember
	certificate100 := saved.Active || saved.OriginalFlag != 0 || saved.OriginalCertificate != nil || len(saved.Files) != 0
	switch saved.Store {
	case adapterStoreNone:
		if options || certificate58 || certificate100 || saved.Certificate != nil {
			return invalid("fields with no adapter to hold them")
		}
	case adapterStoreOptions:
		if len(saved.Original) != 4 || certificate58 || certificate100 || saved.Certificate != nil {
			return invalid("fields another adapter holds")
		}
	case adapterStoreCertificate58:
		if options || certificate100 || saved.RemovedLedger == nil || saved.CreatedLedger == nil {
			return invalid("fields another adapter holds")
		}
	case adapterStoreCertificate100:
		if options || certificate58 || saved.Active && (len(saved.Certificate) != 100 || len(saved.OriginalCertificate) != 100) {
			return invalid("fields another adapter holds")
		}
		for index, file := range saved.Files {
			if index > 0 && bytes.Compare(saved.Files[index-1].Key, file.Key) >= 0 {
				return invalid("files out of order")
			}
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

// restoreAdapters rebuilds the adapter over a store the caller supplies, and
// the local service beside it. It is given a client whose memory is already
// restored, because the one thing an adapter holds that is not data is where
// in that memory the guest keeps its own copy.
func (client *Client) restoreAdapters(archive *Archive, saved adapterState, base backend.SaveStore) error {
	if err := saved.validate(); err != nil {
		return err
	}
	mismatch := fmt.Errorf("LGT checkpoint authentication adapter does not match this module")
	client.saveStore, client.notificationNetwork = base, nil
	switch saved.Store {
	case adapterStoreOptions:
		if !authenticationOptions(client.module) {
			return mismatch
		}
		store := &authenticationOptionStore{base: base, archive: archive}
		copy(store.original[:], saved.Original)
		if saved.Volatile != nil {
			store.volatile = append([]byte{}, saved.Volatile...)
		}
		client.saveStore = store
	case adapterStoreCertificate58:
		if !authenticationCertificate58(client.module) {
			return mismatch
		}
		client.saveStore = &authenticationCertificate58Store{
			base: base, certificate: bytes.Clone(saved.Certificate),
			ledgers: map[string][]byte{
				fileRemovedKey: append([]byte{}, saved.RemovedLedger...),
				fileCreatedKey: append([]byte{}, saved.CreatedLedger...),
			},
			originalMembership: map[string]bool{fileRemovedKey: saved.RemovedMember, fileCreatedKey: saved.CreatedMember},
		}
	case adapterStoreCertificate100:
		contract := authenticationCertificate100(client.module)
		if contract == nil {
			return mismatch
		}
		if len(saved.Files) != 0 && base != nil {
			return fmt.Errorf("LGT checkpoint was taken without a save store and cannot be loaded into one")
		}
		store := newAuthenticationCertificate100Store(base, contract)
		for _, file := range saved.Files {
			store.volatile[string(file.Key)] = append([]byte{}, file.Data...)
		}
		store.originalFlag = saved.OriginalFlag
		store.originalCertificate, store.certificate = bytes.Clone(saved.OriginalCertificate), bytes.Clone(saved.Certificate)
		if saved.Active {
			store.bind(client)
			store.active = true
		}
		client.saveStore = store
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

// rebaseAdapters moves a restored adapter from the isolated store a load is
// prepared against onto the store it will run over. It is the one step of
// adoption that touches the adapter, and it only changes what is underneath.
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
