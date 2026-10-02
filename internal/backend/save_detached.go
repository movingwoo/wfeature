package backend

import (
	"fmt"
	"strconv"
	"sync"
)

// DetachedSaveStore stands in for the save store while a quick load validates
// its records. Validation must not touch a save: the live store reaches the
// restored runtime only when the load commits, and what that runtime needs
// from the saves is read then. Leaving the store unset would keep validation
// off the disk just as well, but it would hide a storage call made by mistake,
// because "no store" is an ordinary session without persistence. This store
// gives the same answers and counts the calls, so a stray one is a number
// somebody can check.
//
// A read answers "absent" and never an error. A platform that meets a read
// error keeps it for the life of the session and refuses or drops every later
// write, so an error answered here would outlive the validation that asked. A
// write is refused: nothing is behind this store, and accepting one would lose
// it without a trace.
//
// It is safe for concurrent use.
type DetachedSaveStore struct {
	mu    sync.Mutex
	calls int
	first string
}

// NewDetachedSaveStore returns a placeholder that no call has reached yet.
func NewDetachedSaveStore() *DetachedSaveStore {
	return &DetachedSaveStore{}
}

// detachedName quotes a key for a message. A key reaches this store from a
// slot's records, which are untrusted input, so only its front is kept.
func detachedName(name string) string {
	const limit = 64
	if len(name) > limit {
		return strconv.Quote(name[:limit]) + "..."
	}
	return strconv.Quote(name)
}

// note counts one call and keeps a description of the first one.
func (store *DetachedSaveStore) note(call string) {
	if store == nil {
		return
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.calls == 0 {
		store.first = call
	}
	store.calls++
}

// LoadSave answers "absent" and counts the call.
func (store *DetachedSaveStore) LoadSave(name string) ([]byte, bool) {
	store.note("read " + detachedName(name))
	return nil, false
}

// ReadSave is the error-aware read that backend.ReadSave prefers. It answers
// "absent" without an error for every name, including one a real store would
// refuse, and counts the call.
func (store *DetachedSaveStore) ReadSave(name string) ([]byte, bool, error) {
	store.note("read " + detachedName(name))
	return nil, false, nil
}

// StoreSave refuses the write and counts the call.
func (store *DetachedSaveStore) StoreSave(name string, data []byte) error {
	store.note("write " + detachedName(name))
	return fmt.Errorf("detached save store refuses the write of %s", detachedName(name))
}

// StoreSaves refuses the batch and counts it as one call. Without it a batch
// of several entries would be turned away by backend.StoreSaves before it
// reached this store, and so would not be counted.
func (store *DetachedSaveStore) StoreSaves(entries map[string][]byte) error {
	call := fmt.Sprintf("batch write of %d entries", len(entries))
	// The smallest key, so the description does not depend on map order.
	first, named := "", false
	for name := range entries {
		if !named || name < first {
			first, named = name, true
		}
	}
	if named {
		call += " starting at " + detachedName(first)
	}
	store.note(call)
	return fmt.Errorf("detached save store refuses a %s", call)
}

// Calls reports how many store calls have reached the placeholder: every read
// through either read method and every refused write, a batch counting once.
// Zero means the code it was handed to never asked for a save.
func (store *DetachedSaveStore) Calls() int {
	if store == nil {
		return 0
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	return store.calls
}

// FirstCall describes the first call that reached the placeholder, such as
// `read "db/index"`, for the log line or the test failure that reports a
// placeholder that was used. It is empty while Calls is zero.
func (store *DetachedSaveStore) FirstCall() string {
	if store == nil {
		return ""
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	return store.first
}
