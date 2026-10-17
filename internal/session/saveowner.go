package session

import (
	"path/filepath"

	"github.com/movingwoo/wfeature/internal/backend"
)

// MigrateSaveOwner moves the saves an earlier build kept under a save owner
// this build no longer answers to the owner Inspect answers now, below root:
// the platform's save root, the directory the owner is joined to. A Host calls
// it before it claims the title's save directory.
//
// The owner changed for a name the browser Host refused — it held a character
// a folder name cannot, or it was too long — which is now made into one it
// accepts on every platform; the command line, which used the raw name, had
// saves under it. A summary with no earlier owner answers nil without a move,
// and nothing here reads the archive again. See backend.MigrateSaveOwner.
func MigrateSaveOwner(summary Summary, root string) error {
	if root == "" || summary.EarlierSaveOwner == "" {
		return nil
	}
	return backend.MigrateSaveOwner(root, summary.EarlierSaveOwner, summary.SaveOwner)
}

// SaveDirectory answers the directory below root a summary's saves are read
// from without moving anything: the owner's, or, while MigrateSaveOwner has
// yet to move them, the earlier owner's. See backend.SaveOwnerDirectory.
func SaveDirectory(summary Summary, root string) string {
	if root == "" || summary.SaveOwner == "" {
		return ""
	}
	if summary.EarlierSaveOwner == "" {
		return filepath.Join(root, summary.SaveOwner)
	}
	return backend.SaveOwnerDirectory(root, summary.EarlierSaveOwner, summary.SaveOwner)
}
