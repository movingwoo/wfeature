package lgt

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
// title all claim. Members are ordered by path so a report reads the same way
// twice.
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
	claims := make(map[string][]SaveOwnerClaim)
	// The scan stops where the Host's picker stops; see the note in
	// ktf.SaveOwnerCollisions for why a deeper archive is not this check's
	// business.
	for _, name := range gameroot.Paths(root, ".zip") {
		data, readErr := os.ReadFile(name)
		if readErr != nil {
			return nil, readErr
		}
		archive, openErr := Open(data)
		if openErr != nil {
			// Archives of other platforms sit in neighbouring directories and
			// are not this check's business.
			continue
		}
		owner := SaveOwner(archive.Descriptor)
		claims[owner] = append(claims[owner], SaveOwnerClaim{Path: name, Descriptor: archive.Descriptor})
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
