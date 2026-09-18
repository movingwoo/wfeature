package ktf

import (
	"fmt"
	"maps"

	"github.com/movingwoo/wfeature/internal/backend"
)

// The save boundary itself lives in internal/backend because MIDP RMS
// persists through the same contract and the same on-disk layout; KTF keeps
// these names so Hosts written against the platform package still compile.
type SaveStore = backend.SaveStore

// DirectorySaveStore is the native CLI's file-backed store.
type DirectorySaveStore = backend.DirectorySaveStore

// NewDirectorySaveStore roots a directory-backed save store.
func NewDirectorySaveStore(root string) *DirectorySaveStore {
	return backend.NewDirectorySaveStore(root)
}

// saveChanges commits content and its deletion ledger together, publishing the
// shared in-memory ledger only after the Host accepts the entire batch.
func (runtime *initializationRuntime) saveChanges(entries map[string][]byte, ledger string, current map[string]bool, changes map[string]bool) error {
	if runtime.saveReadError != nil {
		return runtime.saveReadError
	}
	staged := maps.Clone(current)
	if staged == nil {
		staged = make(map[string]bool)
	}
	changed := false
	for name, removed := range changes {
		if staged[name] == removed {
			continue
		}
		changed = true
		if removed {
			staged[name] = true
		} else {
			delete(staged, name)
		}
	}
	if entries == nil {
		entries = make(map[string][]byte)
	}
	if changed {
		names := make([]string, 0, len(staged))
		for name := range staged {
			names = append(names, name)
		}
		entries[ledger] = joinRemovalList(names)
	}
	if err := backend.StoreSaves(runtime.client.saveStore, entries); err != nil {
		runtime.countDiagnostic(fmt.Sprintf("save batch error: %v", err))
		return err
	}
	if changed {
		clear(current)
		maps.Copy(current, staged)
	}
	return nil
}

// NormalizeSaveKey reduces a save key to the canonical form every Host stores
// under.
func NormalizeSaveKey(name string) (string, error) {
	return backend.NormalizeSaveKey(name)
}

// loadSave reads one persisted save entry through the attached Host store.
func (runtime *initializationRuntime) loadSave(name string) ([]byte, bool) {
	store := runtime.client.saveStore
	if store == nil {
		return nil, false
	}
	data, present, err := backend.ReadSave(store, name)
	if err != nil && runtime.saveReadError == nil {
		runtime.saveReadError = fmt.Errorf("read save %s: %w", name, err)
	}
	return data, present
}

const saveRecordTombstone = backend.SaveRecordTombstone

// encodeSaveRecords serializes Java DataBase records as a count followed by
// length-prefixed entries; nil records keep their slot with a tombstone.
func encodeSaveRecords(records [][]byte) []byte {
	return backend.EncodeSaveRecords(records)
}

// decodeSaveRecords reverses encodeSaveRecords, rejecting truncated input.
func decodeSaveRecords(encoded []byte) ([][]byte, error) {
	return backend.DecodeSaveRecords(encoded)
}
