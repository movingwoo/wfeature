package skt

import (
	"fmt"
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

// ReadDescriptor reads a title's identity without loading the title. A save
// owner is the only thing some callers want, and parsing the main class to
// find out costs the whole class file for nothing.
func ReadDescriptor(data []byte) (Descriptor, error) {
	descriptor, jar, _, err := unpackArchive(data)
	if err != nil {
		return Descriptor{}, err
	}
	if jar != nil {
		return descriptor, nil
	}
	// A bare MIDlet JAR carries its identity in its own manifest.
	entries, err := readJAR(data)
	if err != nil {
		return Descriptor{}, err
	}
	manifest, ok := findEntry(entries, manifestPath)
	if !ok {
		return Descriptor{}, fmt.Errorf("SKT JAR has no %s", manifestPath)
	}
	return ParseDescriptor(manifest)
}

// SaveOwnerCollisions reports the save directories more than one title claims.
//
// An SKT title's saves are keyed by the program number the handset addressed it
// by, which is the archive's own claim about itself in exactly the way a KTF
// PID is: a repack can carry one copied from an unrelated game, and then two
// titles quietly open the same record store and overwrite each other. See
// ktf.SaveOwnerCollisions for the case that made this worth checking.
//
// What separates a collision from a variant is the main class rather than an
// AID, because an SKT descriptor declares no AID: two archives that run the
// same class are the same title shipped twice, and two that do not are two
// titles sharing a save directory.
//
// The scan stops where the Host's picker stops, and reads both shapes an SKT
// title arrives in — the container and the bare MIDlet JAR.
func SaveOwnerCollisions(root string) ([]SaveOwnerCollision, error) {
	claims := make(map[string][]SaveOwnerClaim)
	for _, name := range gameroot.Paths(root, ".zip", ".jar") {
		data, readErr := os.ReadFile(name)
		if readErr != nil {
			return nil, readErr
		}
		descriptor, err := ReadDescriptor(data)
		if err != nil {
			// Archives of other platforms sit in neighbouring directories and
			// are not this check's business.
			continue
		}
		owner := SaveOwner(descriptor)
		if owner == "" {
			continue
		}
		claims[owner] = append(claims[owner], SaveOwnerClaim{Path: name, Descriptor: descriptor})
	}

	var collisions []SaveOwnerCollision
	for owner, owned := range claims {
		distinct := make(map[string]bool, len(owned))
		for _, claim := range owned {
			distinct[claim.Descriptor.MainClass] = true
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
