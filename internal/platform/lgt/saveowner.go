package lgt

import (
	"github.com/movingwoo/wfeature/internal/gameroot"
)

// SaveOwnerClaim is one archive's claim on a save directory.
type SaveOwnerClaim = gameroot.Claim[Descriptor]

// SaveOwnerCollision is a save directory that titles which are not the same
// title all claim. Members are ordered by path so a report reads the same way
// twice.
type SaveOwnerCollision = gameroot.Collision[Descriptor]

// SaveOwnerCollisions reports the save directories more than one title claims.
//
// A title's PID is its own claim about itself, and a repacked archive can
// carry one copied from an unrelated game — the case this exists for arrived
// with a PID, a purchase Objectid and a download URL all copied, and only the
// AID, name and icon names edited. Nothing about that is visible at run time:
// the two games simply open the same save directory and overwrite each other,
// and the first sign of it is a save that will not load.
//
// Variants of one title — a game shipped again with modified drop rates — do
// share a PID, and that sharing is wanted. They also share an AID, which is
// what separates them from the case worth reporting: a collision is a save
// owner claimed under more than one AID.
func SaveOwnerCollisions(root string) ([]SaveOwnerCollision, error) {
	read := func(data []byte) (Descriptor, bool) {
		archive, err := Open(data)
		if err != nil {
			return Descriptor{}, false
		}
		return archive.Descriptor, true
	}
	return gameroot.OwnerCollisions(root, []string{".zip"}, read, SaveOwner,
		func(descriptor Descriptor) string { return descriptor.AID })
}
