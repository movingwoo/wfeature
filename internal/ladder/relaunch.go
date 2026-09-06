package ladder

import (
	"fmt"
	"hash/fnv"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
)

// The other two judgments the three platforms' interactive rungs share: when a
// screen that answered no key is worth asking again, and when a launch that
// answered no key is worth launching again.
//
// Both exist because "no key changed the screen" was being reported about
// titles that were never asked a question they could answer.
//
// # A screen that a key did not move is not always a screen that answers no key
//
// The rung settles a screen and presses. A title whose opening runs longer
// than the press window answers nothing, because at that moment there is
// nothing to answer: the opening is still playing. Measured here, one title
// holds its logo for about twenty-four thousand ticks and then animates to a
// hundred and twenty thousand, against a press window of five keys times
// sixty-four ticks.
//
// The obvious repair is to widen the press window, and it is the wrong one.
// A window long enough to outlast an opening is a window the opening's own
// next screen arrives inside, and the rung credits it to the key: it would
// report that `fire` changed the screen after nineteen thousand ticks about a
// title that ignored `fire` and simply got to the end of its logo. That is the
// error the settle step exists to prevent — a change measured against a screen
// that was already moving — moved from before the press to after it. Widening
// buys reach by giving up the only thing the rung measures.
//
// So the screen is settled again instead. When no key moved it, the rung waits
// with **nothing held** for the screen to leave the set it settled on. A
// screen that moves with no key held is a title whose opening is still
// running, and what it moved to is a new screen: settle that one and ask it
// the same question. A screen that does not move at all while nothing is held
// is a screen that answers no key, and there is nothing hopeful left to say
// about it.
//
// The cost falls where the evidence is. A title whose first key answers pays
// nothing; only a title that answered no key waits, and only for as long as it
// keeps proving it is still going.
//
// # A second launch has to be a second run
//
// The rungs already give an archive two launches, because the handset's
// first-run notice ends the first one: the title writes its save, tells the
// player to start it again, and stops. That was recognised by the session
// ending, which is the only thing a rung could see.
//
// It is not the only shape the notice takes. One title measured here puts the
// notice up and **waits on it** — it does not end, it settles on a still
// screen, and no key moves it. Launched a second time against the same save
// directory it plays, and `fire` answers. Read only through "did the session
// end", that title is a failure; it is a title that was asked its question
// before it was ready to be asked.
//
// Widening "it ended itself" to "it did not get past its first screen" is
// where the risk is, and the risk is a free retry for a title that is
// legitimately stuck. What bounds it is that a rerun which reads the same
// bytes is not a second run at all: pointed at an unchanged save directory the
// guest takes the same branch on the same input and the rung learns nothing it
// did not already know. So a title that did not end itself gets its second
// launch only when the first one **wrote something for the second to read**,
// which is exactly what the notice does. A title that answered no key and
// recorded nothing is refused, because there is no version of the second
// launch that differs from the first.
//
// That is also what keeps `a third would be a probe hoping` true. A second
// launch over unchanged bytes is already hoping; this is the test that says
// so.

// SettleRounds is how many times a rung settles a screen and asks a key of it
// before it reports that no key answered.
//
// Two, for the same reason the launches are two: the first round asks the
// screen the title booted to, and the second asks the screen its opening moved
// to. A third would be a probe waiting out a title's whole attract loop and
// calling whatever it landed on an answer.
const SettleRounds = 2

// Footprint identifies what a launch left in a save directory, so that a rung
// can tell a second run from a second attempt at the same one. Nothing there,
// or a directory that cannot be read, is zero.
//
// Names and contents both: a title that opens its save file and writes back
// the bytes that were already in it has recorded nothing, and a fingerprint
// taken over names alone would call that a first-run notice.
func Footprint(root string) uint64 {
	if root == "" {
		return 0
	}
	// A root that is not there yet is nothing, not an error to fingerprint.
	// The first launch of an archive can be handed a directory the platform
	// has not created yet, and a fingerprint that read that as its own kind of
	// content would call the creation of an empty directory a write.
	if _, err := os.Stat(root); err != nil {
		return 0
	}
	var names []string
	if err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			// An unreadable corner of the tree is reported as itself rather
			// than skipped: "this could not be read" is a state that can
			// change between two launches, and a fingerprint that dropped it
			// would call that no change.
			names = append(names, fmt.Sprintf("%s\x00error=%v", path, err))
			return nil
		}
		if entry.IsDir() {
			return nil
		}
		names = append(names, path)
		return nil
	}); err != nil {
		return 0
	}
	if len(names) == 0 {
		// A directory with nothing in it is nothing, and has to fingerprint as
		// the same nothing a directory that does not exist does: the two are
		// the same amount of evidence that a launch wrote something, and a
		// rung that told them apart would give a second launch to a platform
		// that merely made the folder.
		return 0
	}
	sort.Strings(names)
	sum := fnv.New64a()
	for _, name := range names {
		relative, err := filepath.Rel(root, name)
		if err != nil {
			relative = name
		}
		fmt.Fprintf(sum, "%s\x00", relative)
		body, err := os.ReadFile(name)
		if err != nil {
			fmt.Fprintf(sum, "unreadable=%v\x00", err)
			continue
		}
		fmt.Fprintf(sum, "%d\x00", len(body))
		sum.Write(body)
	}
	return sum.Sum64()
}
