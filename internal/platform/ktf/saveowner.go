package ktf

import (
	"os"
	"sort"

	"github.com/movingwoo/wfeature/internal/gameroot"
)

// SaveOwnerClaim is one archive's claim on a save directory.
type SaveOwnerClaim struct {
	Path       string
	Descriptor Descriptor
}

// SaveOwnerCollision is a save directory that titles which are not the same
// title all claim.
type SaveOwnerCollision struct {
	Owner  string
	Claims []SaveOwnerClaim
}

// AIDs names the distinct titles caught in the collision, in the order their
// claims appear.
func (collision SaveOwnerCollision) AIDs() []string {
	seen := make(map[string]bool, len(collision.Claims))
	aids := make([]string, 0, len(collision.Claims))
	for _, claim := range collision.Claims {
		if seen[claim.Descriptor.AID] {
			continue
		}
		seen[claim.Descriptor.AID] = true
		aids = append(aids, claim.Descriptor.AID)
	}
	return aids
}

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
	claims := make(map[string][]SaveOwnerClaim)
	// The scan stops where the Host's picker stops. An archive deeper than
	// that cannot be started from a Host, so a collision between two of them
	// is not a collision a save will ever suffer — and an ignored diagnostic
	// corpus under the game root is made of exactly those.
	for _, name := range gameroot.Paths(root, ".zip") {
		data, readErr := os.ReadFile(name)
		if readErr != nil {
			return nil, readErr
		}
		descriptor, parseErr := ReadDescriptor(data)
		if parseErr != nil {
			// Archives of other platforms sit in neighbouring directories and
			// are not this check's business.
			continue
		}
		owner := SaveOwner(descriptor)
		claims[owner] = append(claims[owner], SaveOwnerClaim{Path: name, Descriptor: descriptor})
	}

	var collisions []SaveOwnerCollision
	for owner, owned := range claims {
		distinct := make(map[string]bool, len(owned))
		for _, claim := range owned {
			distinct[claim.Descriptor.AID] = true
		}
		if len(distinct) < 2 {
			continue
		}
		sort.Slice(owned, func(left, right int) bool { return owned[left].Path < owned[right].Path })
		collisions = append(collisions, SaveOwnerCollision{Owner: owner, Claims: owned})
	}
	sort.Slice(collisions, func(left, right int) bool { return collisions[left].Owner < collisions[right].Owner })
	return collisions, nil
}
