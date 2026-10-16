package session

import (
	"github.com/movingwoo/wfeature/internal/platform/detect"
	"github.com/movingwoo/wfeature/internal/platform/skt"
)

// MigrateSaveOwner moves the saves an earlier build kept under a save owner
// this build no longer answers to the owner Inspect answers now, below root:
// the platform's save root, the directory the owner is joined to. A Host calls
// it before it claims the title's save directory.
//
// Only SKT MIDlet owners changed: a name the browser Host refused — it held a
// character a folder name cannot, or it was too long — is now made into one it
// accepts, and the command line, which used the raw name, had saves under it.
// Every other archive answers nil without a move. See
// internal/platform/skt/skt.go, MigrateSaveOwner.
func MigrateSaveOwner(archive []byte, root string) error {
	if root == "" {
		return nil
	}
	if platform, _, err := detect.Classify(archive); err != nil || platform != detect.SKT {
		return nil
	}
	opened, err := skt.Open(archive)
	if err != nil || opened.Script != nil {
		return nil
	}
	return skt.MigrateSaveOwner(root, opened.Descriptor)
}
