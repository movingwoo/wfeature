package backend

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

const saveReplaceMagic = "WFRSTR01"

// The reserved directory beside an owner's save folder.
//
// An earlier build's quick load replaced the whole save folder with the saves
// its slot carried: it staged them in "next", recorded an "intent", moved the
// live folder to "previous" and moved the staged one into place. This build
// replaces no save folder and has no code that does. What is here is what
// recognises and settles what such a replacement left behind, because a
// process that stopped half way left the live folder under another name.
//
// The directory also holds the lock files and the checkpoint slots, which is
// why its paths are named here. The names never come from checkpoint bytes.
type saveReplacementPaths struct {
	root, directory, next, previous, intent string
}

func replacementPaths(root string) (saveReplacementPaths, error) {
	if root == "" {
		return saveReplacementPaths{}, fmt.Errorf("save replacement has no root")
	}
	absolute, err := filepath.Abs(root)
	if err != nil {
		return saveReplacementPaths{}, err
	}
	base := filepath.Base(absolute)
	if base == "." || base == string(filepath.Separator) || base == ".wfeature-quicksave" {
		return saveReplacementPaths{}, fmt.Errorf("save replacement root cannot be a filesystem or reserved root")
	}
	// Keep the owner's filename here too, so case and Unicode aliases share
	// the same lock and journal on filesystems that alias the live directory.
	directory := filepath.Join(filepath.Dir(absolute), ".wfeature-quicksave", "owners", base)
	return saveReplacementPaths{root: absolute, directory: directory, next: filepath.Join(directory, "next"),
		previous: filepath.Join(directory, "previous"), intent: filepath.Join(directory, "intent")}, nil
}

func existingSaveDirectory(path string) (bool, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return false, fmt.Errorf("save replacement path is not a directory")
	}
	return true, nil
}

// RecoverSaveTree settles a whole-folder replacement an earlier build left
// interrupted, before a Host reads loose save files. A prepared or half-swapped
// replacement rolls back; a completed swap keeps the folder that is live and
// the "previous" beside it.
func RecoverSaveTree(root string) error {
	unlock, err := lockSaveTree(root)
	if err != nil {
		return err
	}
	defer unlock()
	store := NewDirectorySaveStore(root)
	return store.recoverSaveReplacement()
}

// The caller holds the directory lock. Every operation checks: what an earlier
// build left may be found by any of them first, and none may read or write a
// save folder whose live copy is still under another name.
func (store *DirectorySaveStore) recoverSaveReplacement() error {
	paths, err := replacementPaths(store.root)
	if err != nil {
		return err
	}
	if _, err := existingSaveDirectory(filepath.Dir(paths.directory)); err != nil {
		return err
	}
	exists, err := existingSaveDirectory(paths.directory)
	if err != nil {
		return err
	}
	if !exists {
		return nil
	}
	info, err := os.Lstat(paths.intent)
	if errors.Is(err, fs.ErrNotExist) {
		// Staging without a durable intent cannot have touched the live tree.
		// It and any "previous" stay where they are: nothing here removes what
		// an earlier build left.
		return nil
	}
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Size() != int64(len(saveReplaceMagic)+1) {
		return fmt.Errorf("save replacement recovery record is invalid")
	}
	file, err := os.Open(paths.intent)
	if err != nil {
		return err
	}
	data, readErr := io.ReadAll(io.LimitReader(file, int64(len(saveReplaceMagic)+2)))
	closeErr := file.Close()
	if readErr != nil || closeErr != nil {
		return errors.Join(readErr, closeErr)
	}
	if len(data) != len(saveReplaceMagic)+1 || string(data[:len(saveReplaceMagic)]) != saveReplaceMagic || data[len(saveReplaceMagic)] > 1 {
		return fmt.Errorf("save replacement recovery record is invalid")
	}
	hadOld := data[len(saveReplaceMagic)] != 0
	live, err := existingSaveDirectory(paths.root)
	if err != nil {
		return err
	}
	next, err := existingSaveDirectory(paths.next)
	if err != nil {
		return err
	}
	previous, err := existingSaveDirectory(paths.previous)
	if err != nil {
		return err
	}
	switch {
	case next && hadOld && !live && previous:
		if err := os.Rename(paths.previous, paths.root); err != nil {
			return fmt.Errorf("restore previous save generation: %w", err)
		}
	case next && live == hadOld && !previous:
		// Prepared but not swapped: the original tree is still authoritative.
	case !next && live && previous == hadOld:
		// Both renames completed. Keep the new generation and previous backup.
	default:
		return fmt.Errorf("save replacement directories disagree with the recovery record")
	}
	syncDirectory(filepath.Dir(paths.root))
	syncDirectory(paths.directory)
	if err := os.Remove(paths.intent); err != nil {
		return err
	}
	syncDirectory(paths.directory)
	// On rollback, clear the intent before deleting staging. A crash during
	// cleanup then leaves an ordinary old tree plus harmless staging, rather
	// than an ambiguous record with neither staging nor a previous generation.
	if next {
		_ = os.RemoveAll(paths.next)
	}
	return nil
}

func (store *DirectorySaveStore) SnapshotSaves() ([]SaveEntry, error) {
	if store == nil {
		return nil, fmt.Errorf("save store has no root")
	}
	unlock, err := lockSaveTree(store.root)
	if err != nil {
		return nil, err
	}
	defer unlock()
	if err := store.recoverSaveReplacement(); err != nil {
		return nil, err
	}
	return readSnapshotSaves(store.root)
}
