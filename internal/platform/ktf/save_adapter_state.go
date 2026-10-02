package ktf

import (
	"bytes"
	"fmt"
)

// Save adapters own per-run authentication state above the Host's save store.
// They must travel with a checkpoint: rebuilding a certificate from the archive
// would erase changes and deletion state made by the running guest.
type saveAdapterState struct {
	Version uint32
	Layers  []saveAdapterLayer
}

type saveAdapterLayer struct {
	Kind            string
	Certificate     []byte
	Removed         []byte
	OriginalRemoved bool
	Number          string
	Table           []byte
	Files           [5][]byte
	Lengths         [5]uint32
}

func captureSaveAdapters(store SaveStore) (saveAdapterState, SaveStore, error) {
	saved := saveAdapterState{Version: 1}
	for {
		var layer saveAdapterLayer
		switch current := store.(type) {
		case *certificateSaveStore:
			if current == nil {
				return saveAdapterState{}, nil, fmt.Errorf("KTF certificate adapter is nil")
			}
			current.mu.Lock()
			if current.readError != nil || uint64(len(current.certificate))+uint64(len(current.removed)) > maxHeapStorageBytes {
				current.mu.Unlock()
				return saveAdapterState{}, nil, fmt.Errorf("KTF certificate adapter has a failed read or exceeds size limits")
			}
			layer = saveAdapterLayer{Kind: "certificate", Certificate: current.certificate, Removed: current.removed, OriginalRemoved: current.originalRemoved}
			if err := (saveAdapterState{Version: 1, Layers: append(saved.Layers, layer)}).validate(); err != nil {
				current.mu.Unlock()
				return saveAdapterState{}, nil, err
			}
			layer.Certificate, layer.Removed = bytes.Clone(layer.Certificate), bytes.Clone(layer.Removed)
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
			if err := (saveAdapterState{Version: 1, Layers: append(saved.Layers, layer)}).validate(); err != nil {
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
	if saved.Version != 1 || len(saved.Layers) > 3 {
		return fmt.Errorf("KTF save adapter version or nesting is invalid")
	}
	var size uint64
	for _, layer := range saved.Layers {
		size += uint64(len(layer.Certificate)) + uint64(len(layer.Removed)) + uint64(len(layer.Table))
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
			if len(layer.Number) != 11 || len(layer.Table) != 256 || len(layer.Certificate) != 0 || len(layer.Removed) != 0 || layer.OriginalRemoved {
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

// Restoring an adapter never consults or writes the Host store. The enclosing
// session supplies an isolated base containing the snapshot's saved files.
func restoreSaveAdapters(saved saveAdapterState, base SaveStore) (SaveStore, error) {
	if err := saved.validate(); err != nil {
		return nil, err
	}
	for index := len(saved.Layers) - 1; index >= 0; index-- {
		layer := saved.Layers[index]
		switch layer.Kind {
		case "certificate":
			base = &certificateSaveStore{base: base, certificate: bytes.Clone(layer.Certificate), removed: bytes.Clone(layer.Removed), originalRemoved: layer.OriginalRemoved}
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
