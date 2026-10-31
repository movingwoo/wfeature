package skt

import (
	"fmt"

	"github.com/movingwoo/wfeature/internal/gameroot"
)

// SaveOwnerClaim is one archive's claim on a save directory.
type SaveOwnerClaim = gameroot.Claim[Descriptor]

// SaveOwnerCollision is a save directory that titles which are not the same
// title all claim. Members are ordered by path so a report reads the same way
// twice.
type SaveOwnerCollision = gameroot.Collision[Descriptor]

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
	read := func(data []byte) (Descriptor, bool) {
		descriptor, err := ReadDescriptor(data)
		return descriptor, err == nil && SaveOwner(descriptor) != ""
	}
	return gameroot.OwnerCollisions(root, []string{".zip", ".jar"}, read, SaveOwner,
		func(descriptor Descriptor) string { return descriptor.MainClass })
}
