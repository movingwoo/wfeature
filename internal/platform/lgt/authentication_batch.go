package lgt

import (
	"fmt"
	"strings"

	"github.com/movingwoo/wfeature/internal/backend"
)

// An authentication adapter stands between a session and its store, so a batch
// the session writes reaches the store through it: the move of the databases
// into their own scope is one (moveDatabases), and backend.StoreSaves refuses
// a batch of several entries to a store without StoreSaves. Each adapter
// forwards a batch that names none of the entries it rewrites, whole, under
// its lock as StoreSave runs under it; one that names such an entry is
// refused, because the adapter rewrites those one at a time on their way down.

// adapterBatch gives base a batch the adapter does not rewrite any entry of.
func adapterBatch(base backend.SaveStore, entries map[string][]byte, rewrites func(key string) bool) error {
	if base == nil {
		return fmt.Errorf("an authentication adapter over no store takes no batch")
	}
	for name := range entries {
		key, err := backend.NormalizeSaveKey(name)
		if err != nil {
			return err
		}
		if rewrites(key) {
			return fmt.Errorf("a batch names %s, which the authentication adapter rewrites", key)
		}
	}
	return backend.StoreSaves(base, entries)
}

// StoreSaves forwards a batch that does not name the options file.
func (store *authenticationOptionStore) StoreSaves(entries map[string][]byte) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	return adapterBatch(store.base, entries, func(key string) bool { return key == authenticationOptionsKey })
}

// StoreSaves forwards a batch that does not name the certificate file while
// the adapter is rewriting it.
func (store *authenticationCertificate100Store) StoreSaves(entries map[string][]byte) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	return adapterBatch(store.base, entries, func(key string) bool { return store.active && key == store.key })
}

// StoreSaves forwards a batch that names neither the certificate nor a
// ledger the adapter keeps.
func (store *authenticationCertificate58Store) StoreSaves(entries map[string][]byte) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	return adapterBatch(store.base, entries, func(key string) bool {
		_, ledger := store.ledgers[key]
		return ledger || strings.EqualFold(key, authenticationCertificate58Key)
	})
}
