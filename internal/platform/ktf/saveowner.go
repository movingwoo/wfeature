package ktf

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
// KTF keys saves by PID because its AIDs collide — one local AID is shared by
// four unrelated titles. That makes the PID load-bearing, and a PID is only
// the archive's claim about itself: a repack can carry one copied from another
// game, at which point two titles quietly open the same save directory and
// overwrite each other. The same check runs over the LGT library, where
// exactly that was found; see lgt.SaveOwnerCollisions.
//
// Variants of one title share a PID on purpose, and share an AID with it, so
// a collision is a save owner claimed under more than one AID.
func SaveOwnerCollisions(root string) ([]SaveOwnerCollision, error) {
	read := func(data []byte) (Descriptor, bool) {
		descriptor, err := ReadDescriptor(data)
		return descriptor, err == nil
	}
	return gameroot.OwnerCollisions(root, []string{".zip"}, read, SaveOwner,
		func(descriptor Descriptor) string { return descriptor.AID })
}
