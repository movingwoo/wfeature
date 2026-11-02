package ladder

import "fmt"

// The words the interactive rungs' reports share, so that a failure reads the
// same whichever platform's ladder wrote it.

// Screens is "screen" or "screens", so a count reads as a sentence.
func Screens(count int) string {
	if count == 1 {
		return "screen"
	}
	return "screens"
}

// OpeningMovedOn says, in a failure, whether the screen that answered no key
// was the one the title booted to or one its opening moved on to. The two are
// different defects and a line that spelled them the same way would send
// whoever read it to the wrong place. openingTicks is how long the rung
// watched the opening for.
func OpeningMovedOn(moved, openingTicks int) string {
	if moved == 0 {
		return fmt.Sprintf(", and it did not move on its own in the %d ticks after", openingTicks)
	}
	return fmt.Sprintf(", over %d %s its opening moved on to", moved, Screens(moved))
}
