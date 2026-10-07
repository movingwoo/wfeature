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
// runtime, including any final guest writes. If every location refuses a file
// lock, the claim excludes this process alone; see acquireSaveLock.
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

// Every lock enters this process's registry under the resolved owner path,
// then takes a kernel lock beside that owner. Linked owners also retain the
// lock beside their original spelling for earlier processes and journals.
// Slots and recovery paths continue to use the original spelling. The kernel
// lock is what reaches another process, and it needs a location that can be
// written and a file system that has locks to give. A
// save tree does not always offer that — a read-only folder, a share without
// lock support — and the releases before this lock ran there on one mutex per
// store object. Every usable lock is retained when another location refuses
// one; for example, a linked owner with an unwritable parent still keeps a
// lock beside its writable target. If none work, the registry alone excludes
// every store object on the root in this process, but not another process.
// The registry is taken first either way, so callers in one process exclude
// each other identically whichever answer the locations give.
//
// What falls back is a missing capability and nothing else. A lock somebody
// holds is contention and is reported as that; a reserved path that is a link
// or not a directory was not made by this program and stays refused.
func acquireSaveLock(root, name string, wait bool) (func(), error) {
	paths, err := replacementPaths(root)
	if err != nil {
		return nil, err
	}
	identity, err := saveLockRoot(paths.root)
	if err != nil {
		return nil, err
	}
	resolved, err := replacementPaths(identity)
	if err != nil {
		return nil, err
	}
	leave, err := enterSaveGate(identity, name, wait)
	if err != nil {
		return nil, err
	}
	locations := []saveReplacementPaths{resolved}
	if paths.root != resolved.root {
		locations = append(locations, paths)
	}
	unlock, err := lockSaveDirectories(locations, name, wait)
	if err != nil {
		if !saveLockUnavailable(err) {
			leave()
			return nil, err
		}
		recordSaveLockFallback(identity, err)
		if unlock == nil {
			unlock = func() {}
		}
	}
	var released sync.Once
	return func() {
		released.Do(func() {
			unlock()
			leave()
		})
	}, nil
}

// saveLockRoot resolves links even when the owner or a parent has not been
// created yet. Resolving the nearest existing ancestor keeps a claim's identity
// unchanged when the first save creates the directory. A dangling link still
// names its target; neither the link nor the missing owner is created here.
func saveLockRoot(root string) (string, error) {
	trimSeparators := func(path string) string {
		minimum := len(filepath.VolumeName(path)) + 1
		for len(path) > minimum && os.IsPathSeparator(path[len(path)-1]) {
			path = path[:len(path)-1]
		}
		return path
	}
	path, suffix := root, ""
	for links := 0; ; {
		resolved, err := filepath.EvalSymlinks(path)
		if err == nil {
			return filepath.Join(resolved, suffix), nil
		}
		if !errors.Is(err, fs.ErrNotExist) {
			if saveLockUnavailable(err) {
				// An unreadable prefix cannot be resolved. Ordinary access
				// retains its existing error and process-only fallback.
				return root, nil
			}
			return "", err
		}
		if info, statErr := os.Lstat(path); statErr == nil && info.Mode()&os.ModeSymlink != 0 {
			if links++; links > 255 {
				return "", fmt.Errorf("save lock root has too many symbolic links")
			}
			target, err := os.Readlink(path)
			if err != nil {
				return "", err
			}
			if !filepath.IsAbs(target) {
				// Do not clean the target before following its links: in
				// "jump/../owner", jump may lead to another parent entirely.
				directory, _ := filepath.Split(path)
				target = directory + target
			}
			path = trimSeparators(target)
			continue
		}
		parent, base := filepath.Split(path)
		parent = trimSeparators(parent)
		if parent == path {
			return "", err
		}
		suffix = filepath.Join(base, suffix)
		path = parent
	}
}

// Always take the resolved owner's lock before the caller's legacy lock.
// Parent links, case aliases and Unicode aliases may name the same lock file;
// compare opened identities before locking so those never lock themselves.
func lockSaveDirectories(paths []saveReplacementPaths, name string, wait bool) (release func(), err error) {
	var files []*os.File
	var identities []fs.FileInfo
	var locked []bool
	closeFiles := func() {
		for i := len(files) - 1; i >= 0; i-- {
			if locked[i] {
				unlockSaveFile(files[i])
			}
			_ = files[i].Close()
		}
	}
	defer func() {
		if err != nil && release == nil {
			closeFiles()
		}
	}()
	var unavailable error
	for _, location := range paths {
		if err := prepareSaveDirectory(location); err != nil {
			if !saveLockUnavailable(err) {
				return nil, err
			}
			if unavailable == nil {
				unavailable = err
			}
			continue
		}
		file, info, err := openSaveLockFile(location, name)
		if err != nil {
			if !saveLockUnavailable(err) {
				return nil, err
			}
			if unavailable == nil {
				unavailable = err
			}
			continue
		}
		duplicate := false
		for _, previous := range identities {
			duplicate = duplicate || os.SameFile(previous, info)
		}
		if duplicate {
			_ = file.Close()
		} else {
			files = append(files, file)
			identities = append(identities, info)
			locked = append(locked, false)
		}
	}
	// Inspect both reserved locations before falling back: a permission error
	// at one must never hide a tampered path at the other. Keep the original
	// error so callers can also use the legacy os.IsPermission predicate.
	// A linked owner's unavailable legacy location must not discard a usable
	// target lock, or another process could enter through the target path.
	for i, file := range files {
		if err := saveFileLock(file, wait); err != nil {
			if saveLockUnavailable(err) {
				if unavailable == nil {
					unavailable = err
				}
				continue
			}
			return nil, fmt.Errorf("lock save directory: %w", err)
		}
		locked[i] = true
	}
	return closeFiles, unavailable
}

// openSaveLockFile creates or opens one regular lock file without following
// links inside the reserved directory.
func openSaveLockFile(paths saveReplacementPaths, name string) (*os.File, fs.FileInfo, error) {
	directory, err := os.OpenRoot(paths.directory)
	if err != nil {
		return nil, nil, err
	}
	defer directory.Close()
	info, err := directory.Lstat(name)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, nil, err
	}
	if err == nil && !info.Mode().IsRegular() {
		return nil, nil, fmt.Errorf("save lock is not a regular file")
	}
	file, err := directory.OpenFile(name, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, nil, err
	}
	actual, err := file.Stat()
	if err != nil {
		file.Close()
		return nil, nil, err
	}
	if !actual.Mode().IsRegular() || info != nil && !os.SameFile(info, actual) {
		file.Close()
		return nil, nil, fmt.Errorf("save lock changed while opening")
	}
	return file, actual, nil
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

// saveLockFallback records why one of a root's locations could not hold a
// kernel lock, and whether a Host has reported it. Other locations may still
// hold locks.
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

// SaveLockFallback reports whether any lock location for a save root was
// unavailable, and its first failure. Usable locks are retained, but exclusion
// cannot be promised for every alias or earlier process; with no usable lock,
// only this process's goroutines are kept out.
func SaveLockFallback(root string) (cause error, fallback bool) {
	paths, err := replacementPaths(root)
	if err != nil {
		return nil, false
	}
	identity, err := saveLockRoot(paths.root)
	if err != nil {
		return nil, false
	}
	saveLockFallbacks.mutex.Lock()
	defer saveLockFallbacks.mutex.Unlock()
	if record := saveLockFallbacks.roots[identity]; record != nil {
		return record.cause, true
	}
	return nil, false
}

// WarnSaveLockFallback warns in a Host's log when a save root could not take
// every file lock, once per root for the life of the process. The warning is
// conservative when other aliases retain a usable lock. The store has no logger
// of its own and does not print, so each Host calls this where it takes the
// directory: the server when it claims, the CLI when it starts.
func WarnSaveLockFallback(logger *slog.Logger, root string) {
	paths, err := replacementPaths(root)
	if logger == nil || err != nil {
		return
	}
	identity, err := saveLockRoot(paths.root)
	if err != nil {
		return
	}
	saveLockFallbacks.mutex.Lock()
	record := saveLockFallbacks.roots[identity]
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
