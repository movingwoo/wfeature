package backend

import "errors"

// What a quick save or a quick load refuses for a reason outside the slot's
// own bytes. checkpoint.go holds the refusals about the envelope; these are
// about the ordinary saves beside it and about a slot from an earlier build.
// Each is its own error because a Host has a different sentence for each, and
// each is returned wrapped: the platform that refuses knows the file and what
// the store answered, puts that in front, and a Host finds the sentinel with
// errors.Is.
var (
	// ErrCheckpointSaveWrite is returned when the writes the running game had
	// already issued, and the host still held, could not be stored. A quick
	// save and a quick load both store those writes before they do anything
	// else, so either one returns it. The key press is refused and the game
	// keeps running; no save is reverted or removed.
	ErrCheckpointSaveWrite = errors.New("backend: the running game's issued saves could not be stored")

	// ErrCheckpointSaveRead is returned when a quick load could not read the
	// saves on disk. A load restores execution state only and rebuilds the
	// restored game's view of its saves from the disk as it is, so a save it
	// cannot read refuses the load before the running game is displaced.
	ErrCheckpointSaveRead = errors.New("backend: the saves on disk could not be read")

	// ErrCheckpointLegacy is returned when the quick save an archive has was
	// written in an earlier slot format. The file is intact and stays where it
	// is: it is refused, and never converted, rewritten or removed. It is a
	// different error from ErrCheckpointVersion on purpose, because that one
	// is also the answer for an unsupported execution variant, and a Host
	// cannot tell a person "an earlier build wrote this" from it.
	ErrCheckpointLegacy = errors.New("backend: this checkpoint was written in an earlier slot format")
)
