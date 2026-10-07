package backend

import (
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
)

var ErrSaveDirectoryBusy = errors.New("save directory is in use by another session or tool")

// ClaimSaveDirectory excludes competing game sessions and save imports for
// their entire lifetime. Reads take only the shorter transaction lock. Hosts
// retain this claim while a game is parked and release it after closing the
// runtime, including any final guest writes. Where the location cannot hold a
// file lock the claim excludes this process alone; see acquireSaveLock.
func ClaimSaveDirectory(root string) (release func(), err error) {
	return acquireSaveLock(root, "session", false)
}

// The lock file stays outside the directory that replacement moves. A kernel
// lock coordinates separate store objects and processes, and is released even
// when a process exits without running defers. Never unlink a lock file: a
// waiter may already have its inode open.
func lockSaveTree(root string) (func(), error) {
	return acquireSaveLock(root, "lock", true)
}

// Every lock is taken twice: in this process's own registry, keyed by the
// absolute root, and then as a kernel lock on a file in the reserved sibling
// directory. The kernel lock is what reaches another process, and it needs a
// location that can be written and a file system that has locks to give. A
// save tree does not always offer that — a read-only folder, a share without
// lock support — and the releases before this lock ran there on one mutex per
// store object. So a location that cannot hold the kernel lock keeps the
// registry alone: stronger than that mutex was, since it covers every store
// object on the root, and weaker than the lock, since another process is no
// longer kept out. The registry is taken first either way, so callers in one
// process exclude each other identically whichever answer the location gives.
//
// What falls back is a missing capability and nothing else. A lock somebody
// holds is contention and is reported as that; a reserved path that is a link
// or not a directory was not made by this program and stays refused.
func acquireSaveLock(root, name string, wait bool) (func(), error) {
	paths, err := replacementPaths(root)
	if err != nil {
		return nil, err
	}
	leave, err := enterSaveGate(paths.root, name, wait)
	if err != nil {
		return nil, err
	}
	unlock, err := lockSaveDirectory(paths, name, wait)
	if err != nil {
		if !saveLockUnavailable(err) {
			leave()
			return nil, err
		}
		recordSaveLockFallback(paths.root, err)
		unlock = func() {}
	}
	var released sync.Once
	return func() {
		released.Do(func() {
			unlock()
			leave()
		})
	}, nil
}

// lockSaveDirectory takes the kernel lock on one file of the reserved
// directory, creating both on first use.
func lockSaveDirectory(paths saveReplacementPaths, name string, wait bool) (func(), error) {
	if err := prepareSaveDirectory(paths); err != nil {
		return nil, err
	}
	directory, err := os.OpenRoot(paths.directory)
	if err != nil {
		return nil, err
	}
	defer directory.Close()
	info, err := directory.Lstat(name)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	if err == nil && !info.Mode().IsRegular() {
		return nil, fmt.Errorf("save lock is not a regular file")
	}
	file, err := directory.OpenFile(name, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	actual, err := file.Stat()
	if err != nil {
		file.Close()
		return nil, err
	}
	if !actual.Mode().IsRegular() || info != nil && !os.SameFile(info, actual) {
		file.Close()
		return nil, fmt.Errorf("save lock changed while opening")
	}
	if err := saveFileLock(file, wait); err != nil {
		file.Close()
		return nil, fmt.Errorf("lock save directory: %w", err)
	}
	return func() {
		unlockSaveFile(file)
		_ = file.Close()
	}, nil
}

// The two calls a location can refuse: making the reserved directory, and the
// platform's lock. They are variables so that a test can stand in for a file
// system that refuses one.
var (
	makeSaveDirectory = os.Mkdir
	saveFileLock      = lockSaveFile
)

func prepareSaveDirectory(paths saveReplacementPaths) error {
	if _, err := ownerSaveDirectory(paths.root); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(paths.root), 0755); err != nil {
		return err
	}
	for _, path := range []string{filepath.Dir(filepath.Dir(paths.directory)), filepath.Dir(paths.directory), paths.directory} {
		made := makeSaveDirectory(path, 0700)
		// What is there afterwards is judged, not what the attempt answered.
		// A reserved path that is a link or a file is refused however making
		// it failed, so a location that cannot be written does not turn
		// tampering into a missing capability; and a directory that is there
		// is used, so every level below it is judged the same way.
		exists, err := existingSaveDirectory(path)
		if err != nil {
			return err
		}
		if !exists {
			if made == nil {
				made = &fs.PathError{Op: "mkdir", Path: path, Err: fs.ErrNotExist}
			}
			return made
		}
	}
	return nil
}

// ownerSaveDirectory is existingSaveDirectory for the owner root on the lock
// and ordinary paths, where a link is followed rather than refused. A person
// may keep one game's saves elsewhere and leave a link in its place; every
// ordinary file operation goes through that link, as it did before the lock
// existed, and so do checkpoint slots, which live beside the link. Settling a
// replacement an earlier build left renames the directory itself, which a link
// does not survive, so that path keeps the stricter check. A link that leads
// nowhere reads as a directory that is not there yet, and the file operation
// that follows says what is wrong with it.
func ownerSaveDirectory(path string) (bool, error) {
	info, err := os.Stat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !info.IsDir() {
		return false, fmt.Errorf("save root is not a directory")
	}
	return true, nil
}

// saveGate is this process's own exclusion for one lock of one root.
type saveGate struct {
	held sync.Mutex
	// users counts the holder and everyone waiting, under saveGates.mutex, so
	// a gate nobody is at can be forgotten instead of kept per root forever.
	users int
}

type saveGateKey struct{ root, name string }

var saveGates = struct {
	mutex sync.Mutex
	open  map[saveGateKey]*saveGate
}{open: make(map[saveGateKey]*saveGate)}

// enterSaveGate takes the in-process half of a lock. A caller that does not
// wait is a claim, and a claim somebody in this process holds is busy exactly
// as one another process holds is.
func enterSaveGate(root, name string, wait bool) (func(), error) {
	key := saveGateKey{root: root, name: name}
	saveGates.mutex.Lock()
	gate := saveGates.open[key]
	if gate == nil {
		gate = &saveGate{}
		saveGates.open[key] = gate
	}
	gate.users++
	saveGates.mutex.Unlock()
	forget := func() {
		saveGates.mutex.Lock()
		if gate.users--; gate.users == 0 {
			delete(saveGates.open, key)
		}
		saveGates.mutex.Unlock()
	}
	if wait {
		gate.held.Lock()
	} else if !gate.held.TryLock() {
		forget()
		return nil, ErrSaveDirectoryBusy
	}
	return func() {
		gate.held.Unlock()
		forget()
	}, nil
}

// saveLockUnavailable reports a failure that says the location cannot hold the
// kernel lock at all: the reserved directory or the lock file may not be
// created there, or the file system has no locks. The platform files list
// their own codes beside the two classes every platform names the same way.
func saveLockUnavailable(err error) bool {
	if errors.Is(err, fs.ErrPermission) || errors.Is(err, errors.ErrUnsupported) {
		return true
	}
	for _, code := range saveLockUnavailableErrors {
		if errors.Is(err, code) {
			return true
		}
	}
	return false
}

// saveLockFallback is why one root has only the registry, and whether a Host
// has said so yet.
type saveLockFallback struct {
	cause    error
	reported bool
}

var saveLockFallbacks = struct {
	mutex sync.Mutex
	roots map[string]*saveLockFallback
}{roots: make(map[string]*saveLockFallback)}

// The first failure is the one kept: it is what a person has to act on, and
// later attempts at the same location answer the same thing.
func recordSaveLockFallback(root string, cause error) {
	saveLockFallbacks.mutex.Lock()
	defer saveLockFallbacks.mutex.Unlock()
	if saveLockFallbacks.roots[root] == nil {
		saveLockFallbacks.roots[root] = &saveLockFallback{cause: cause}
	}
}

// SaveLockFallback reports whether this process has excluded writers of a save
// root only among its own goroutines, and the failure that showed the location
// cannot hold the file lock. Other processes are not kept out of such a root.
func SaveLockFallback(root string) (cause error, fallback bool) {
	paths, err := replacementPaths(root)
	if err != nil {
		return nil, false
	}
	saveLockFallbacks.mutex.Lock()
	defer saveLockFallbacks.mutex.Unlock()
	if record := saveLockFallbacks.roots[paths.root]; record != nil {
		return record.cause, true
	}
	return nil, false
}

// WarnSaveLockFallback says in a Host's log that a save root has no file lock,
// once per root for the life of the process. The directory store has no logger
// of its own and does not print, so each Host calls this where it takes the
// directory: the server when it claims, the CLI when it starts.
func WarnSaveLockFallback(logger *slog.Logger, root string) {
	paths, err := replacementPaths(root)
	if logger == nil || err != nil {
		return
	}
	saveLockFallbacks.mutex.Lock()
	record := saveLockFallbacks.roots[paths.root]
	first := record != nil && !record.reported
	if first {
		record.reported = true
	}
	saveLockFallbacks.mutex.Unlock()
	if first {
		logger.Warn("save directory cannot hold a file lock; other processes are not kept out of it",
			"directory", paths.root, "error", record.cause)
	}
}
