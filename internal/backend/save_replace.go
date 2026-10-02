package backend

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
)

const saveReplaceMagic = "WFRSTR01"

// A replacement keeps its staging directory and previous generation outside
// guest keys. The fixed names below never come from checkpoint bytes.
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

// RecoverSaveTree resolves an interrupted whole-generation replacement before
// a Host reads loose save files. A prepared or half-swapped replacement rolls
// back; a completed swap keeps the new generation and the previous backup.
func RecoverSaveTree(root string) error {
	unlock, err := lockSaveTree(root)
	if err != nil {
		return err
	}
	defer unlock()
	store := NewDirectorySaveStore(root)
	return store.recoverSaveReplacement()
}

// The caller holds the directory lock. Check on every operation: another
// process may have stopped during a replacement since this object last ran.
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
		// Leave it for the next replacement to remove, retaining the backup.
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

// ReplaceSaves stages and syncs a complete generation, then swaps directories.
// A failed rename rolls back before returning; an interrupted process is
// recovered by the next read or write. The displaced generation remains in
// the reserved sibling namespace as "previous" until the next replacement.
// Directory syncing follows the ordinary store's filesystem/OS guarantees.
func (store *DirectorySaveStore) ReplaceSaves(entries []SaveEntry) error {
	if store == nil {
		return fmt.Errorf("save store has no root")
	}
	return store.replaceSaves(entries, os.Rename)
}

func (store *DirectorySaveStore) replaceSaves(entries []SaveEntry, rename func(string, string) error) error {
	ordered, err := validateSnapshotSaves(entries)
	if err != nil {
		return err
	}
	unlock, err := lockSaveTree(store.root)
	if err != nil {
		return err
	}
	defer unlock()
	if err := store.recoverSaveReplacement(); err != nil {
		return err
	}
	paths, err := replacementPaths(store.root)
	if err != nil {
		return err
	}
	// A tree containing unsupported entries must not be silently replaced by
	// a snapshot that could not have represented them in the first place.
	if _, err := readSnapshotSaves(paths.root); err != nil {
		return err
	}
	hadOld, err := existingSaveDirectory(paths.root)
	if err != nil {
		return err
	}
	if _, err := existingSaveDirectory(paths.next); err != nil {
		return err
	}
	if err := os.RemoveAll(paths.next); err != nil {
		return err
	}
	if err := os.Mkdir(paths.next, 0700); err != nil {
		return err
	}
	staged := &DirectorySaveStore{root: paths.next}
	for _, entry := range ordered {
		if err := staged.storeSave(entry.Key, entry.Data); err != nil {
			return err
		}
	}
	// Distinct checkpoint keys can name the same file on this filesystem.
	// Verify the staged generation before touching either live saves or backup.
	actual, err := readSnapshotSaves(paths.next)
	if err != nil {
		return err
	}
	if !slices.EqualFunc(actual, ordered, func(a, b SaveEntry) bool {
		return a.Key == b.Key && bytes.Equal(a.Data, b.Data)
	}) {
		return fmt.Errorf("save snapshot names or contents changed on the destination filesystem")
	}
	if err := filepath.WalkDir(paths.next, func(path string, entry fs.DirEntry, err error) error {
		if err == nil && entry.IsDir() {
			syncDirectory(path)
		}
		return err
	}); err != nil {
		return err
	}
	// Only one previous generation is retained. The live tree is untouched
	// while the older backup is removed and the new recovery record is written.
	if _, err := existingSaveDirectory(paths.previous); err != nil {
		return err
	}
	if err := os.RemoveAll(paths.previous); err != nil {
		return err
	}
	record := append([]byte(saveReplaceMagic), 0)
	if hadOld {
		record[len(saveReplaceMagic)] = 1
	}
	journal := &DirectorySaveStore{root: paths.directory}
	if err := journal.storeSave("intent", record); err != nil {
		return err
	}
	syncDirectory(filepath.Dir(paths.directory))
	syncDirectory(filepath.Dir(paths.root))
	movedOld := false
	rollback := func(cause error) error {
		// We still own this transaction and know which move completed. Do not
		// rely on the journal's continued existence to preserve the live tree.
		if movedOld {
			if err := os.Rename(paths.previous, paths.root); err != nil {
				return errors.Join(cause, fmt.Errorf("restore previous save generation: %w", err))
			}
			syncDirectory(filepath.Dir(paths.root))
			syncDirectory(paths.directory)
		}
		if err := os.Remove(paths.intent); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return errors.Join(cause, err)
		}
		syncDirectory(paths.directory)
		_ = os.RemoveAll(paths.next)
		return cause
	}
	if hadOld {
		if err := rename(paths.root, paths.previous); err != nil {
			return rollback(err)
		}
		movedOld = true
		syncDirectory(filepath.Dir(paths.root))
		syncDirectory(paths.directory)
	}
	if err := rename(paths.next, paths.root); err != nil {
		return rollback(err)
	}
	// This is the commit point. Subsequent cleanup cannot make adoption fail:
	// the complete new tree is live, and the journal already describes it.
	syncDirectory(filepath.Dir(paths.root))
	syncDirectory(paths.directory)
	if err := os.Remove(paths.intent); err == nil {
		syncDirectory(paths.directory)
	}
	return nil
}
