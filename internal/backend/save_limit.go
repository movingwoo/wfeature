package backend

import (
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"syscall"
)

// errSaveLimit marks the refusal of an entry larger than the caller's limit,
// which is not the same thing as a read that failed.
var errSaveLimit = errors.New("save is over its read limit")

func saveLimitError(name string, size, limit int64) error {
	return fmt.Errorf("%w: %q is %d bytes, the limit is %d", errSaveLimit, name, size, limit)
}

// SaveLimitReader is the bounded form of SaveReader: a store that knows how
// large an entry is before it reads it, and so can refuse one that is too
// large without allocating for it. A store that wraps another can pass the
// call down to keep that; ReadSaveLimit works without it, by reading first.
type SaveLimitReader interface {
	ReadSaveLimit(name string, limit int64) ([]byte, bool, error)
}

// ReadSaveLimit reads one entry on a budget: it answers the entry's bytes only
// when there are at most limit of them. A larger entry is an error that names
// the key and the limit, never the first limit bytes: a save cut short is a
// different save, and a caller that built on one would write it back.
//
// It is for a read whose key did not come from the running guest. A quick load
// rebuilds storage for the names a slot lists, and those names are untrusted
// input: with no bound, a crafted slot makes one load read the largest file in
// the save directory once per name. The caller charges its budget before the
// read and passes what is left as the limit.
//
// A directory store checks the size before it reads. Any other store is read
// and then checked, which bounds what the caller keeps but not what that store
// allocated. Absence and read errors are reported as ReadSave reports them.
func ReadSaveLimit(store SaveStore, key string, limit int64) (data []byte, found bool, err error) {
	if limit < 0 {
		return nil, false, fmt.Errorf("save %q has a negative read limit %d", key, limit)
	}
	if store == nil {
		return nil, false, nil
	}
	if bounded, ok := store.(SaveLimitReader); ok {
		data, found, err = bounded.ReadSaveLimit(key, limit)
	} else {
		data, found, err = ReadSave(store, key)
	}
	if err != nil || !found {
		return nil, false, err
	}
	// Checked here for every store, the bounded ones included: what this
	// function promises its caller must not rest on each wrapper passing the
	// limit down correctly.
	if int64(len(data)) > limit {
		return nil, false, saveLimitError(key, int64(len(data)), limit)
	}
	return data, true, nil
}

// ReadSaveLimit is ReadSave with the entry's size checked before its bytes are
// read. It takes the same lock, settles an interrupted replacement first and
// resolves the key the same way, so the two reads answer alike for everything
// but an entry over the limit.
func (store *DirectorySaveStore) ReadSaveLimit(name string, limit int64) ([]byte, bool, error) {
	if store == nil {
		return nil, false, fmt.Errorf("save store has no root")
	}
	if limit < 0 {
		return nil, false, fmt.Errorf("save %q has a negative read limit %d", name, limit)
	}
	unlock, err := lockSaveTree(store.root)
	if err != nil {
		return nil, false, err
	}
	defer unlock()
	if err := store.recoverSaveReplacement(); err != nil {
		return nil, false, err
	}
	return store.readSaveLimit(name, limit)
}

func (store *DirectorySaveStore) readSaveLimit(name string, limit int64) ([]byte, bool, error) {
	path, err := store.savePath(name)
	if err != nil {
		return nil, false, err
	}
	file, err := os.Open(path)
	// Two of the answers readSave gives for a key with no entry: nothing at
	// the path, and a path that runs below a saved file.
	if errors.Is(err, os.ErrNotExist) || errors.Is(err, syscall.ENOTDIR) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	defer file.Close()
	// The size is asked of the open file, not of the path, so it is the size
	// of the bytes about to be read even if the name is replaced meanwhile.
	info, err := file.Stat()
	if err != nil {
		return nil, false, err
	}
	// The third: a directory, which keys below it made (see keyIsDirectory).
	// It opens without complaint and has a size of its own, so it has to be
	// decided before the size is compared, or a directory would be refused as
	// a save that is too large.
	if info.IsDir() {
		return nil, false, nil
	}
	if info.Size() > limit {
		return nil, false, saveLimitError(name, info.Size(), limit)
	}
	data, err := readSaveBounded(file, name, limit)
	if err != nil {
		return nil, false, err
	}
	return data, true, nil
}

// readSaveBounded reads at most one byte past the limit. The size check ahead
// of it is what refuses a large file without reading it; this is what still
// holds when the file grows after that check, or is not a regular file and had
// no size to check.
func readSaveBounded(reader io.Reader, name string, limit int64) ([]byte, error) {
	bound := limit
	if bound < math.MaxInt64 {
		bound++
	}
	data, err := io.ReadAll(io.LimitReader(reader, bound))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("%w: %q grew past %d bytes while it was read", errSaveLimit, name, limit)
	}
	return data, nil
}
