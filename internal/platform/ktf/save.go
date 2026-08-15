package ktf

import (
	"fmt"

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
	return store.LoadSave(name)
}

// storeSave persists one save entry. Persistence failures never become guest
// errors — the in-memory copy stays authoritative for the session.
func (runtime *initializationRuntime) storeSave(name string, data []byte) {
	store := runtime.client.saveStore
	if store == nil {
		return
	}
	if err := store.StoreSave(name, data); err != nil {
		runtime.countDiagnostic(fmt.Sprintf("save store error %s: %v", name, err))
	}
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
