package backend

import (
	"bytes"
	"errors"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// The bound is the whole entry or nothing. An entry of exactly the limit is
// read; one byte more is an error that says which key and what limit, and no
// bytes come back with it, because the first sixteen bytes of a save are not a
// smaller save.
func TestReadSaveLimitRefusesAnEntryOneByteOver(t *testing.T) {
	store := NewDirectorySaveStore(filepath.Join(t.TempDir(), "owner"))
	const key, limit = "fs/slot.dat", 16
	exact := bytes.Repeat([]byte("s"), limit)
	if err := store.StoreSave(key, exact); err != nil {
		t.Fatal(err)
	}
	if data, found, err := ReadSaveLimit(store, key, limit); err != nil || !found || !bytes.Equal(data, exact) {
		t.Fatalf("an entry of exactly the limit = %q, %t, %v", data, found, err)
	}

	over := append(bytes.Clone(exact), 'x')
	if err := store.StoreSave(key, over); err != nil {
		t.Fatal(err)
	}
	data, found, err := ReadSaveLimit(store, key, limit)
	if !errors.Is(err, errSaveLimit) {
		t.Fatalf("an entry one byte over the limit = %q, %t, %v", data, found, err)
	}
	if data != nil || found {
		t.Fatalf("the refusal came with %d bytes, found=%t: a truncated read", len(data), found)
	}
	for _, part := range []string{strconv.Quote(key), strconv.Itoa(limit), strconv.Itoa(limit + 1)} {
		if !strings.Contains(err.Error(), part) {
			t.Errorf("the refusal %q does not mention %s", err, part)
		}
	}
	// The limit is the caller's and not the store's: the entry is still there
	// for a read that can afford it.
	if data, found, err := ReadSave(store, key); err != nil || !found || !bytes.Equal(data, over) {
		t.Fatalf("an ordinary read after the refusal = %q, %t, %v", data, found, err)
	}
	if data, found, err := ReadSaveLimit(store, key, limit+1); err != nil || !found || !bytes.Equal(data, over) {
		t.Fatalf("a read with room for it = %q, %t, %v", data, found, err)
	}

	// A caller whose budget is spent passes zero, and only an empty entry or a
	// missing one fits that.
	if err := store.StoreSave("fs/empty", nil); err != nil {
		t.Fatal(err)
	}
	if data, found, err := ReadSaveLimit(store, "fs/empty", 0); err != nil || !found || len(data) != 0 {
		t.Fatalf("an empty entry on a spent budget = %q, %t, %v", data, found, err)
	}
	if _, _, err := ReadSaveLimit(store, key, 0); !errors.Is(err, errSaveLimit) {
		t.Fatalf("an entry on a spent budget = %v", err)
	}
	// The largest limit there is must still read: one past it does not exist.
	if data, found, err := ReadSaveLimit(store, key, math.MaxInt64); err != nil || !found || !bytes.Equal(data, over) {
		t.Fatalf("a read with the largest limit = %q, %t, %v", data, found, err)
	}
}

// Apart from the limit the bounded read is the ordinary read: the same keys
// name the same entries, the same keys are refused, and the same things count
// as "no entry". Each row asks both and requires one answer.
func TestReadSaveLimitAnswersLikeAnOrdinaryRead(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "owner")
	store := NewDirectorySaveStore(root)
	for key, data := range map[string]string{"db/slot": "progress", "fs/empty": "", "rms/.index": "slot0\n"} {
		if err := store.StoreSave(key, []byte(data)); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(root, "fs", "nested", "deeper"), 0o755); err != nil {
		t.Fatal(err)
	}
	// What a key that climbed out of the root would find.
	if err := os.WriteFile(filepath.Join(parent, "outside"), []byte("not a save"), 0o644); err != nil {
		t.Fatal(err)
	}

	const found, absent, refused = "found", "absent", "refused"
	for _, test := range []struct {
		name, key, want string
	}{
		{"an entry", "db/slot", found},
		{"an empty entry", "fs/empty", found},
		{"a dotted entry", "rms/.index", found},
		{"a key that needs normalising", "./db//slot", found},
		{"a missing key", "db/missing", absent},
		{"a key in a directory that is not there", "nowhere/slot", absent},
		{"a key below a saved file", "db/slot/child", absent},
		{"a directory at the key path", "fs/nested", refused},
		{"a directory holding entries at the key path", "db", refused},
		{"a key that climbs out of the root", "../outside", refused},
		{"an empty key", "", refused},
		{"a key of nothing but dots", "./.", refused},
		{"a key with a backslash", `db\slot`, refused},
		{"a key with a NUL", "db/sl\x00t", refused},
		{"a key over the length limit", strings.Repeat("k", 513), refused},
	} {
		t.Run(test.name, func(t *testing.T) {
			ordinary, ordinaryFound, ordinaryErr := ReadSave(store, test.key)
			bounded, boundedFound, boundedErr := ReadSaveLimit(store, test.key, 1<<20)
			got := absent
			switch {
			case boundedErr != nil:
				got = refused
			case boundedFound:
				got = found
			}
			if got != test.want {
				t.Fatalf("the bounded read = %q, %t, %v; want %s", bounded, boundedFound, boundedErr, test.want)
			}
			if boundedFound != ordinaryFound || (boundedErr == nil) != (ordinaryErr == nil) || !bytes.Equal(bounded, ordinary) {
				t.Fatalf("the two reads differ: bounded %q, %t, %v; ordinary %q, %t, %v",
					bounded, boundedFound, boundedErr, ordinary, ordinaryFound, ordinaryErr)
			}
			if test.want != refused {
				return
			}
			if bounded != nil {
				t.Fatalf("a refused read returned %q", bounded)
			}
			// None of these is a save that is too large, and a spent budget
			// must not turn one into that: a directory has a size of its own.
			for _, limit := range []int64{0, 1 << 20} {
				if _, _, err := ReadSaveLimit(store, test.key, limit); err == nil || errors.Is(err, errSaveLimit) {
					t.Fatalf("with limit %d the refusal is %v", limit, err)
				}
			}
		})
	}

	if _, _, err := ReadSaveLimit(store, "fs/nested", 1<<20); !errors.Is(err, syscall.EISDIR) {
		t.Fatalf("a directory at the key path = %v, want the error for reading a directory", err)
	}
	// A title that has never saved has no directory at all, which is absence
	// and not an error.
	never := NewDirectorySaveStore(filepath.Join(parent, "never-saved"))
	if data, found, err := ReadSaveLimit(never, "db/slot", 1<<20); data != nil || found || err != nil {
		t.Fatalf("a read before the first save = %q, %t, %v", data, found, err)
	}
	if _, err := os.Lstat(filepath.Join(parent, "never-saved")); !os.IsNotExist(err) {
		t.Fatalf("a read created the save directory: %v", err)
	}
}

// What the check is for: a file larger than the limit is refused from its
// size alone. The file here is sparse and sixty-four megabytes long, and the
// limit is half of that, as a rebuild budget would be. A read that found out
// by reading, even one that stopped at the limit, would allocate megabytes.
func TestReadSaveLimitRefusesALargeFileWithoutReadingIt(t *testing.T) {
	root := filepath.Join(t.TempDir(), "owner")
	store := NewDirectorySaveStore(root)
	if err := store.StoreSave("fs/slot.dat", nil); err != nil {
		t.Fatal(err)
	}
	const size, limit = 64 << 20, 32 << 20
	file, err := os.OpenFile(filepath.Join(root, "fs", "slot.dat"), os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	err = file.Truncate(size)
	_ = file.Close()
	if err != nil {
		t.Fatal(err)
	}

	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	data, found, err := ReadSaveLimit(store, "fs/slot.dat", limit)
	runtime.ReadMemStats(&after)
	if !errors.Is(err, errSaveLimit) || data != nil || found {
		t.Fatalf("a %d byte entry on a %d byte limit = %d bytes, %t, %v", size, limit, len(data), found, err)
	}
	if !strings.Contains(err.Error(), strconv.Itoa(size)) {
		t.Errorf("the refusal %q does not say how large the entry is", err)
	}
	if allocated := after.TotalAlloc - before.TotalAlloc; allocated > 1<<20 {
		t.Fatalf("refusing a %d byte entry allocated %d bytes: it was read before it was measured", size, allocated)
	}
}

// The size on the directory entry is a claim, and a file can grow after it was
// asked or have no size at all. The read itself stops one byte past the limit,
// which is what makes the limit hold either way.
func TestReadSaveLimitStopsAFileThatOutgrowsItsSize(t *testing.T) {
	const limit = 16
	exact := strings.Repeat("s", limit)
	if data, err := readSaveBounded(strings.NewReader(exact), "fs/slot.dat", limit); err != nil || string(data) != exact {
		t.Fatalf("a stream of exactly the limit = %q, %v", data, err)
	}
	data, err := readSaveBounded(strings.NewReader(exact+strings.Repeat("x", 4096)), "fs/slot.dat", limit)
	if !errors.Is(err, errSaveLimit) || data != nil {
		t.Fatalf("a stream past the limit = %d bytes, %v", len(data), err)
	}
	if !strings.Contains(err.Error(), strconv.Quote("fs/slot.dat")) || !strings.Contains(err.Error(), strconv.Itoa(limit)) {
		t.Errorf("the refusal %q does not name the key and the limit", err)
	}
	if data, err := readSaveBounded(strings.NewReader(exact), "fs/slot.dat", math.MaxInt64); err != nil || string(data) != exact {
		t.Fatalf("a stream under the largest limit = %q, %v", data, err)
	}
}

// A bounded read is a store operation like any other: it waits for whoever
// holds the save tree, including a holder in another store object, instead of
// reading a tree that is in the middle of being changed.
func TestReadSaveLimitWaitsForTheSaveTransaction(t *testing.T) {
	root := filepath.Join(t.TempDir(), "owner")
	if err := NewDirectorySaveStore(root).StoreSave("db/index", []byte("progress")); err != nil {
		t.Fatal(err)
	}
	unlock, err := lockSaveTree(root)
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	started, done := make(chan struct{}), make(chan error, 1)
	go func() {
		close(started)
		data, found, err := ReadSaveLimit(NewDirectorySaveStore(root), "db/index", 64)
		if err == nil && (!found || string(data) != "progress") {
			err = errors.New("the bounded read answered without the entry")
		}
		done <- err
	}()
	<-started
	select {
	case err := <-done:
		t.Fatalf("the bounded read did not wait for an active transaction: %v", err)
	case <-time.After(30 * time.Millisecond):
	}
	unlock()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the bounded read did not finish after the transaction ended")
	}
}

// saveLimitPlainStore is a Host store from before the error-aware read: all it
// offers is LoadSave.
type saveLimitPlainStore map[string][]byte

func (store saveLimitPlainStore) LoadSave(name string) ([]byte, bool) {
	data, found := store[name]
	return data, found
}

func (store saveLimitPlainStore) StoreSave(name string, data []byte) error {
	store[name] = data
	return nil
}

// A store that cannot say how large an entry is gets read first and checked
// afterwards. That does not save the allocation, but the caller still never
// holds more than it asked for, and the answers keep the same shape.
func TestReadSaveLimitReadsOtherStoresAndThenChecks(t *testing.T) {
	const limit = 16
	exact := bytes.Repeat([]byte("s"), limit)
	over := append(bytes.Clone(exact), 'x')
	memory, err := NewMemorySaveStore([]SaveEntry{{Key: "fs/exact", Data: exact}, {Key: "fs/over", Data: over}})
	if err != nil {
		t.Fatal(err)
	}
	for name, store := range map[string]SaveStore{
		"an error-aware store": memory,
		"a plain store":        saveLimitPlainStore{"fs/exact": exact, "fs/over": over},
	} {
		t.Run(name, func(t *testing.T) {
			if data, found, err := ReadSaveLimit(store, "fs/exact", limit); err != nil || !found || !bytes.Equal(data, exact) {
				t.Fatalf("an entry of exactly the limit = %q, %t, %v", data, found, err)
			}
			data, found, err := ReadSaveLimit(store, "fs/over", limit)
			if !errors.Is(err, errSaveLimit) || data != nil || found {
				t.Fatalf("an entry one byte over the limit = %q, %t, %v", data, found, err)
			}
			if !strings.Contains(err.Error(), strconv.Quote("fs/over")) || !strings.Contains(err.Error(), strconv.Itoa(limit)) {
				t.Errorf("the refusal %q does not name the key and the limit", err)
			}
			if data, found, err := ReadSaveLimit(store, "fs/missing", limit); data != nil || found || err != nil {
				t.Fatalf("a missing key = %q, %t, %v", data, found, err)
			}
		})
	}
	// The store's own refusals pass through as they are.
	for _, key := range []string{"../outside", "fs"} {
		if data, found, err := ReadSaveLimit(memory, key, limit); err == nil || errors.Is(err, errSaveLimit) || data != nil || found {
			t.Fatalf("the memory store's refusal of %q = %q, %t, %v", key, data, found, err)
		}
	}
	if data, found, err := ReadSaveLimit(nil, "fs/exact", limit); data != nil || found || err != nil {
		t.Fatalf("a read with no store = %q, %t, %v", data, found, err)
	}
}

// A negative limit is a budget that was overdrawn before the call. It is an
// error for every store, and the store is not asked.
func TestReadSaveLimitRefusesANegativeLimit(t *testing.T) {
	directory := NewDirectorySaveStore(filepath.Join(t.TempDir(), "owner"))
	if err := directory.StoreSave("fs/slot.dat", nil); err != nil {
		t.Fatal(err)
	}
	counted := NewDetachedSaveStore()
	for name, store := range map[string]SaveStore{"a directory store": directory, "another store": counted, "no store": nil} {
		data, found, err := ReadSaveLimit(store, "fs/slot.dat", -1)
		if err == nil || errors.Is(err, errSaveLimit) || data != nil || found {
			t.Errorf("%s with a negative limit = %q, %t, %v", name, data, found, err)
		}
	}
	if counted.Calls() != 0 {
		t.Fatalf("a read that could not be allowed still reached the store: %s", counted.FirstCall())
	}
	if _, _, err := directory.ReadSaveLimit("fs/slot.dat", -1); err == nil {
		t.Fatal("the directory store read on a negative limit")
	}
	var missing *DirectorySaveStore
	if _, _, err := missing.ReadSaveLimit("fs/slot.dat", 16); err == nil {
		t.Fatal("a nil directory store answered a bounded read")
	}
}

// saveLimitCarelessStore offers the bounded read and ignores the limit, the
// way a wrapper that forgot to pass it down would.
type saveLimitCarelessStore struct {
	saveLimitPlainStore
	asked []int64
}

func (store *saveLimitCarelessStore) ReadSaveLimit(name string, limit int64) ([]byte, bool, error) {
	store.asked = append(store.asked, limit)
	data, found := store.LoadSave(name)
	return data, found, nil
}

// A store that offers the bounded read is given the limit, so that it can
// refuse before reading. What comes back is checked all the same: the promise
// is this function's, and it must hold for a store that got the limit wrong.
func TestReadSaveLimitHandsTheLimitDownAndStillChecks(t *testing.T) {
	const limit = 16
	store := &saveLimitCarelessStore{saveLimitPlainStore: saveLimitPlainStore{
		"fs/exact": bytes.Repeat([]byte("s"), limit),
		"fs/over":  bytes.Repeat([]byte("s"), limit+1),
	}}
	if data, found, err := ReadSaveLimit(store, "fs/exact", limit); err != nil || !found || len(data) != limit {
		t.Fatalf("an entry of exactly the limit = %q, %t, %v", data, found, err)
	}
	if data, found, err := ReadSaveLimit(store, "fs/over", limit); !errors.Is(err, errSaveLimit) || data != nil || found {
		t.Fatalf("an entry the store should have refused = %q, %t, %v", data, found, err)
	}
	if len(store.asked) != 2 || store.asked[0] != limit || store.asked[1] != limit {
		t.Fatalf("the store was asked with limits %v", store.asked)
	}
}
