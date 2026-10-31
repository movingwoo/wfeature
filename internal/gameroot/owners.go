package gameroot

import (
	"os"
	"sort"
)

// Claim is one archive's claim on a save owner, with the descriptor the claim
// was read from.
type Claim[D any] struct {
	Path       string
	Descriptor D
}

// Collision is a save owner that titles which are not the same title all
// claim.
type Collision[D any] struct {
	Owner  string
	Claims []Claim[D]
}

// OwnerCollisions reports the save owners more than one title claims among the
// archives under root with one of the given extensions.
//
// A save owner is only an archive's claim about itself: a repack can carry one
// copied from another game, at which point two titles quietly open the same
// save directory and overwrite each other. Variants of one title share their
// owner on purpose, so a collision is an owner claimed under more than one
// identity — whatever tells two titles apart on the platform.
//
// read answers an archive's descriptor, and false for an archive that is not
// the platform's or names no owner; archives of other platforms sit in
// neighbouring directories and are not the check's business. Claims are in
// path order and collisions in owner order.
//
// The scan stops where the Host's picker stops. An archive deeper than that
// cannot be started from a Host, so a collision between two of them is not a
// collision a save will ever suffer — and an ignored diagnostic corpus under
// the game root is made of exactly those.
func OwnerCollisions[D any](root string, extensions []string, read func([]byte) (D, bool), owner, identity func(D) string) ([]Collision[D], error) {
	claims := make(map[string][]Claim[D])
	for _, name := range Paths(root, extensions...) {
		data, err := os.ReadFile(name)
		if err != nil {
			return nil, err
		}
		descriptor, ok := read(data)
		if !ok {
			continue
		}
		claimed := owner(descriptor)
		claims[claimed] = append(claims[claimed], Claim[D]{Path: name, Descriptor: descriptor})
	}

	var collisions []Collision[D]
	for claimed, owned := range claims {
		distinct := make(map[string]bool, len(owned))
		for _, claim := range owned {
			distinct[identity(claim.Descriptor)] = true
		}
		if len(distinct) < 2 {
			continue
		}
		sort.Slice(owned, func(left, right int) bool { return owned[left].Path < owned[right].Path })
		collisions = append(collisions, Collision[D]{Owner: claimed, Claims: owned})
	}
	sort.Slice(collisions, func(left, right int) bool { return collisions[left].Owner < collisions[right].Owner })
	return collisions, nil
}
