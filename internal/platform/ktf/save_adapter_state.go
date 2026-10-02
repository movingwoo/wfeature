package ktf

import (
	"bytes"
	"fmt"
	"slices"
)

// Save adapters own per-run authentication state above the Host's save store.
// What they made for this run travels with a checkpoint: rebuilding a
// certificate from the archive would erase a change the running guest made to
// it. What they read from the store does not travel: a load reads it again.
type saveAdapterState struct {
	Version uint32
	Layers  []saveAdapterLayer
}

// saveAdapterVersion is the layout of saveAdapterState. Version 1 carried the
// certificate adapter's copy of the removal list.
const saveAdapterVersion = 2

type saveAdapterLayer struct {
	Kind        string
	Certificate []byte
	// CertificateRemoved says the title removed its certificate in the
	// captured run. The removal list itself is not here: the adapter's view of
	// it is built from the list the store has when the checkpoint is bound.
	CertificateRemoved bool
	Number             string
	Table              []byte
	Files              [5][]byte
	Lengths            [5]uint32
}

// captureSaveAdapters records the adapter chain and answers the store under
// it. It reads the adapters' own memory and makes no store call.
func captureSaveAdapters(store SaveStore) (saveAdapterState, SaveStore, error) {
	saved := saveAdapterState{Version: saveAdapterVersion}
	for {
		var layer saveAdapterLayer
		switch current := store.(type) {
		case *certificateSaveStore:
			if current == nil {
				return saveAdapterState{}, nil, fmt.Errorf("KTF certificate adapter is nil")
			}
			current.mu.Lock()
			if current.readError != nil || uint64(len(current.certificate)) > maxHeapStorageBytes {
				current.mu.Unlock()
				return saveAdapterState{}, nil, fmt.Errorf("KTF certificate adapter has a failed read or exceeds size limits")
			}
			layer = saveAdapterLayer{Kind: "certificate", Certificate: current.certificate,
				CertificateRemoved: slices.Contains(splitRemovalList(current.removed), certificateName)}
			if err := (saveAdapterState{Version: saveAdapterVersion, Layers: append(saved.Layers, layer)}).validate(); err != nil {
				current.mu.Unlock()
				return saveAdapterState{}, nil, err
			}
			layer.Certificate = bytes.Clone(layer.Certificate)
			store = current.base
			current.mu.Unlock()
		case *subscriberReceiptStore:
			if current == nil || len(current.files) != len(subscriberReceiptFiles) {
				return saveAdapterState{}, nil, fmt.Errorf("KTF subscriber adapter has invalid files")
			}
			layer = saveAdapterLayer{Kind: "subscriber-receipt", Number: current.number, Table: current.table, Lengths: current.lengths}
			for index, name := range subscriberReceiptFiles {
				layer.Files[index] = current.files[name]
			}
			if err := (saveAdapterState{Version: saveAdapterVersion, Layers: append(saved.Layers, layer)}).validate(); err != nil {
				return saveAdapterState{}, nil, err
			}
			layer.Table = bytes.Clone(layer.Table)
			for index := range layer.Files {
				layer.Files[index] = bytes.Clone(layer.Files[index])
			}
			store = current.base
		default:
			return saved, store, saved.validate()
		}
		saved.Layers = append(saved.Layers, layer)
	}
}

func (saved saveAdapterState) validate() error {
	if saved.Version != saveAdapterVersion || len(saved.Layers) > 3 {
		return fmt.Errorf("KTF save adapter version or nesting is invalid")
	}
	var size uint64
	for _, layer := range saved.Layers {
		size += uint64(len(layer.Certificate)) + uint64(len(layer.Table))
		for _, file := range layer.Files {
			size += uint64(len(file))
		}
		if size > maxHeapStorageBytes {
			return fmt.Errorf("KTF save adapter data exceeds limit")
		}
		switch layer.Kind {
		case "certificate":
			if layer.Number != "" || len(layer.Table) != 0 || layer.Lengths != ([5]uint32{}) {
				return fmt.Errorf("KTF certificate adapter has unrelated fields")
			}
			for _, file := range layer.Files {
				if len(file) != 0 {
					return fmt.Errorf("KTF certificate adapter contains subscriber files")
				}
			}
		case "subscriber-receipt":
			if len(layer.Number) != 11 || len(layer.Table) != 256 || len(layer.Certificate) != 0 || layer.CertificateRemoved {
				return fmt.Errorf("KTF subscriber adapter has invalid fields")
			}
			for _, digit := range layer.Number {
				if digit < '0' || digit > '9' {
					return fmt.Errorf("KTF subscriber adapter number is invalid")
				}
			}
			for index, length := range layer.Lengths {
				if length == 0 || length >= 1<<19 || uint64(len(layer.Files[index])) != uint64(length)+44 {
					return fmt.Errorf("KTF subscriber adapter file length is invalid")
				}
			}
		default:
			return fmt.Errorf("KTF save adapter kind is unsupported")
		}
	}
	return nil
}

// bindSaveAdapters builds a recorded adapter chain over the store a restored
// session will run on. The certificate layer reads that store's removal list
// once, as a starting session's does, and nothing is written.
//
// The certificate itself is the record's. It is the one answer to a storage
// read that a load does not take from the store, because it was never there:
// the adapter issues it for the run and keeps it in memory.
func bindSaveAdapters(saved saveAdapterState, base SaveStore) (SaveStore, error) {
	if err := saved.validate(); err != nil {
		return nil, err
	}
	for index := len(saved.Layers) - 1; index >= 0; index-- {
		layer := saved.Layers[index]
		switch layer.Kind {
		case "certificate":
			store := &certificateSaveStore{base: base, certificate: bytes.Clone(layer.Certificate)}
			if err := store.loadRemovalView(layer.CertificateRemoved); err != nil {
				return nil, err
			}
			base = store
		case "subscriber-receipt":
			store := &subscriberReceiptStore{base: base, number: layer.Number, table: bytes.Clone(layer.Table), lengths: layer.Lengths, files: make(map[string][]byte)}
			for i, name := range subscriberReceiptFiles {
				store.files[name] = bytes.Clone(layer.Files[i])
			}
			base = store
		}
	}
	return base, nil
}

// rebaseSaveAdapters stands an adapter chain on another store and answers the
// top of the chain, which is the store itself when there is no adapter. It
// reads and writes nothing and cannot fail, which is why a load uses it for
// both of its swaps: the restored chain moves from the reader it was bound
// through onto the store, and the displaced session's chain moves onto a sink.
func rebaseSaveAdapters(store, base SaveStore) SaveStore {
	var certificate *certificateSaveStore
	var receipt *subscriberReceiptStore
	current := store
	// A chain has at most three layers; the bound keeps a malformed one from
	// being walked for ever.
	for depth := 0; depth < 4; depth++ {
		switch adapter := current.(type) {
		case *certificateSaveStore:
			if adapter == nil {
				break
			}
			adapter.mu.Lock()
			current = adapter.base
			adapter.mu.Unlock()
			certificate, receipt = adapter, nil
			continue
		case *subscriberReceiptStore:
			if adapter == nil {
				break
			}
			current = adapter.base
			certificate, receipt = nil, adapter
			continue
		}
		break
	}
	switch {
	case certificate != nil:
		certificate.mu.Lock()
		certificate.base = base
		certificate.mu.Unlock()
	case receipt != nil:
		receipt.base = base
	default:
		return base
	}
	return store
}
