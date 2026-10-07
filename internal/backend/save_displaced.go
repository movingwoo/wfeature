package backend

import "fmt"

// DisplacedGeneration reports whether the saves an earlier build's quick load
// set aside are still beside the owner directory, as the "previous" directory
// in the reserved sibling. They can be the newest saves the player made before
// that load, and nothing knows whether they are newer than the live tree, so
// no code restores or removes them. A Host can only say that they are there.
//
// Like every store operation it takes the tree lock and settles an interrupted
// replacement first. A replacement that stopped between its two renames has a
// "previous" that is really the live tree, and recovery puts it back; asked
// before that, the answer would name saves that are about to stop being
// displaced. After it, a "previous" that exists stays until a person removes
// it.
//
// The directory is only asked whether it exists. Nothing inside it is listed
// or read, and nothing about it is changed.
func (store *DirectorySaveStore) DisplacedGeneration() (bool, error) {
	if store == nil {
		return false, fmt.Errorf("save store has no root")
	}
	unlock, err := lockSaveTree(store.root)
	if err != nil {
		return false, err
	}
	defer unlock()
	if err := store.recoverSaveReplacement(); err != nil {
		return false, err
	}
	paths, err := replacementPaths(store.root)
	if err != nil {
		return false, err
	}
	return existingSaveDirectory(paths.previous)
}
