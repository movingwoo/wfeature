package backend

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
)

var ErrSaveDirectoryBusy = errors.New("save directory is in use by another session or tool")

// ClaimSaveDirectory excludes competing game sessions and save imports for
// their entire lifetime. Reads take only the shorter transaction lock. Hosts
// retain this claim while a game is parked and release it after closing the
// runtime, including any final guest writes.
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

func acquireSaveLock(root, name string, wait bool) (func(), error) {
	paths, err := replacementPaths(root)
	if err != nil {
		return nil, err
	}
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
	if err := lockSaveFile(file, wait); err != nil {
		file.Close()
		return nil, fmt.Errorf("lock save directory: %w", err)
	}
	var released sync.Once
	return func() {
		released.Do(func() {
			unlockSaveFile(file)
			_ = file.Close()
		})
	}, nil
}

func prepareSaveDirectory(paths saveReplacementPaths) error {
	if _, err := existingSaveDirectory(paths.root); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(paths.root), 0755); err != nil {
		return err
	}
	for _, path := range []string{filepath.Dir(filepath.Dir(paths.directory)), filepath.Dir(paths.directory), paths.directory} {
		if err := os.Mkdir(path, 0700); err != nil && !errors.Is(err, fs.ErrExist) {
			return err
		}
		if _, err := existingSaveDirectory(path); err != nil {
			return err
		}
	}
	return nil
}
