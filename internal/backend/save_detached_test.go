package backend

import (
	"strings"
	"sync"
	"testing"
)

// The placeholder is there to turn "validation asks for no save" into a number
// a test can check, so what has to hold is that no route to a store goes
// uncounted. Each route is taken on a placeholder of its own and through the
// helper a platform calls, since that helper is what decides which method the
// call arrives at.
func TestDetachedSaveStoreReportsAnyUse(t *testing.T) {
	untouched := NewDetachedSaveStore()
	if untouched.Calls() != 0 || untouched.FirstCall() != "" {
		t.Fatalf("an untouched placeholder reports %d calls, the first %q", untouched.Calls(), untouched.FirstCall())
	}
	// ReadSave and StoreSaves only reach a store's own methods through these
	// two interfaces. Without the first a read would still be counted, by way
	// of LoadSave; without the second a batch would be refused by the helper
	// and never counted at all.
	if _, ok := SaveStore(untouched).(SaveReader); !ok {
		t.Fatal("the placeholder is not the error-aware reader ReadSave looks for")
	}
	if _, ok := SaveStore(untouched).(SaveBatchStore); !ok {
		t.Fatal("the placeholder is not the batch store StoreSaves looks for")
	}

	for _, route := range []struct {
		name  string
		write bool
		use   func(store SaveStore) (found bool, err error)
		first string
	}{
		{"a plain read", false, func(store SaveStore) (bool, error) {
			data, found := store.LoadSave("db/index")
			return found || data != nil, nil
		}, `read "db/index"`},
		{"an error-aware read", false, func(store SaveStore) (bool, error) {
			data, found, err := ReadSave(store, "db/index")
			return found || data != nil, err
		}, `read "db/index"`},
		// A directory store answers this name with an error. Here it is
		// absence like any other, because the caller would keep the error.
		{"a read of a name a directory store refuses", false, func(store SaveStore) (bool, error) {
			data, found, err := ReadSave(store, "../outside")
			return found || data != nil, err
		}, `read "../outside"`},
		{"a bounded read", false, func(store SaveStore) (bool, error) {
			data, found, err := ReadSaveLimit(store, "fs/slot.dat", 16)
			return found || data != nil, err
		}, `read "fs/slot.dat"`},
		{"a write", true, func(store SaveStore) (bool, error) {
			return false, store.StoreSave("db/index", []byte("progress"))
		}, `write "db/index"`},
		{"a batch of one", true, func(store SaveStore) (bool, error) {
			return false, StoreSaves(store, map[string][]byte{"db/index": []byte("progress")})
		}, `write "db/index"`},
		{"a batch of several", true, func(store SaveStore) (bool, error) {
			return false, StoreSaves(store, map[string][]byte{"fs/b": []byte("second"), "fs/a": []byte("first"), "fs/c": nil})
		}, `batch write of 3 entries starting at "fs/a"`},
	} {
		t.Run(route.name, func(t *testing.T) {
			store := NewDetachedSaveStore()
			found, err := route.use(store)
			if found {
				t.Fatal("the placeholder answered with a save")
			}
			if route.write && err == nil {
				t.Fatal("the placeholder accepted a write it cannot keep")
			}
			if !route.write && err != nil {
				t.Fatalf("a read answered an error, which a platform keeps for the whole session: %v", err)
			}
			if store.Calls() != 1 {
				t.Fatalf("the call was counted %d times", store.Calls())
			}
			if store.FirstCall() != route.first {
				t.Fatalf("the call is described as %q, want %q", store.FirstCall(), route.first)
			}
			// A refused write must not become readable either.
			if data, found, err := ReadSave(store, "db/index"); data != nil || found || err != nil {
				t.Fatalf("a later read = %q, %t, %v", data, found, err)
			}
			if store.Calls() != 2 || store.FirstCall() != route.first {
				t.Fatalf("after a second call: %d calls, the first %q", store.Calls(), store.FirstCall())
			}
		})
	}
}

// A key reaches the placeholder from a slot's records, which nothing has
// vouched for, and what it reports goes into a log line. Neither the
// description nor the refusal may carry the whole key or break the line.
func TestDetachedSaveStoreBoundsWhatItRepeats(t *testing.T) {
	hostile := "fs/AAAA\nlevel=ERROR forged " + strings.Repeat("A", 4096)
	store := NewDetachedSaveStore()
	err := store.StoreSave(hostile, nil)
	if err == nil {
		t.Fatal("the placeholder accepted a write")
	}
	for name, text := range map[string]string{"description": store.FirstCall(), "refusal": err.Error()} {
		if len(text) > 160 || strings.ContainsAny(text, "\n\r") {
			t.Errorf("the %s repeats an untrusted key unbounded or across lines: %d bytes", name, len(text))
		}
		if !strings.Contains(text, `"fs/AAAA`) {
			t.Errorf("the %s does not say which key: %q", name, text)
		}
	}
}

// A restored runtime can have guest threads, and a store is called from
// whichever one is running. The count has to be exact under that, and the race
// detector has to stay quiet.
func TestDetachedSaveStoreCountsConcurrentCalls(t *testing.T) {
	store := NewDetachedSaveStore()
	const workers, rounds = 8, 100
	var group sync.WaitGroup
	for worker := 0; worker < workers; worker++ {
		group.Add(1)
		go func() {
			defer group.Done()
			for round := 0; round < rounds; round++ {
				switch (worker + round) % 4 {
				case 0:
					store.LoadSave("db/index")
				case 1:
					_, _, _ = store.ReadSave("db/index")
				case 2:
					_ = store.StoreSave("db/index", nil)
				default:
					_ = store.StoreSaves(map[string][]byte{"fs/a": nil, "fs/b": nil})
				}
				_ = store.Calls()
				_ = store.FirstCall()
			}
		}()
	}
	group.Wait()
	if store.Calls() != workers*rounds {
		t.Fatalf("%d calls were counted as %d", workers*rounds, store.Calls())
	}
}

// A nil placeholder inside a SaveStore is a mistake, but the answers stay the
// placeholder's: a read that reports an error would be kept by its caller.
func TestDetachedSaveStoreNilAnswersLikeAnEmptyOne(t *testing.T) {
	var store *DetachedSaveStore
	if data, found := store.LoadSave("db/index"); data != nil || found {
		t.Fatal("a nil placeholder answered a plain read with a save")
	}
	if data, found, err := ReadSave(store, "db/index"); data != nil || found || err != nil {
		t.Fatalf("a nil placeholder read = %q, %t, %v", data, found, err)
	}
	if err := store.StoreSave("db/index", nil); err == nil {
		t.Fatal("a nil placeholder accepted a write")
	}
	if err := store.StoreSaves(map[string][]byte{"fs/a": nil, "fs/b": nil}); err == nil {
		t.Fatal("a nil placeholder accepted a batch")
	}
	if store.Calls() != 0 || store.FirstCall() != "" {
		t.Fatalf("a nil placeholder reports %d calls, the first %q", store.Calls(), store.FirstCall())
	}
}
