package main

import (
	"fmt"
	"io"
	"os"

	"github.com/movingwoo/wfeature/internal/cheat"
)

// `-patch` is what takes a byte patch out of the interactive console.
//
// The mechanism itself has been here for a while: an entry declares the bytes
// it replaces, its spans apply as a unit, and a table file keyed by the hash
// of the loaded image says what the entry was found in. What was missing was
// any way to reach it that was not a person typing. Even loading a table lived
// inside the `-cheat` loop, so the patch mechanism was available on exactly one
// of the ways this binary is driven — and it is the runs nobody is sitting in
// front of that most want a gate opened: a route replaying past a check, a
// `-serve` session stepping and looking, a sweep over the whole library.
//
// `-cheat` and `-serve` both own stdin, so they refuse each other, and that
// refusal is right. This flag is the answer to it rather than an argument with
// it: it reads a file before the run and writes nothing to the terminal that a
// caller has to answer, so it combines with every way of driving a run.
//
// Nothing here writes guest memory. It reads the table format the console
// already saves and hands each entry to Session.ApplyTablePatches, which is the
// same path `load` takes: the declared bytes are verified before anything is
// written, an entry goes in whole or not at all, and a write that fails part of
// the way through puts back what it had already replaced. A second way into
// guest memory is precisely what this must not become.

// patchTable is one file named by `-patch`, kept beside the path it was read
// from so a refusal names which of several files described the span that did
// not match.
type patchTable struct {
	path  string
	table cheat.Table
}

// readPatchTables reads the tables named by `-patch`, in the order they were
// given.
//
// It runs while the command line is still being parsed, before the archive is
// opened, for the reason a route is parsed there: a mistyped span should be
// reported now rather than after the guest execution it takes to reach the
// first tick.
//
// The flag is repeatable because a table is a unit of provenance rather than a
// unit of use. A gate patch found last month and a scratch patch written this
// morning are two files with two notes and two key hashes, and merging them by
// hand to run them together would throw both away. Entries apply in the order
// the files were given, and the mechanism already refuses a name that is
// applied twice and spans that overlap something applied, so two tables that
// disagree are refused rather than layered.
func readPatchTables(paths []string) ([]patchTable, error) {
	tables := make([]patchTable, 0, len(paths))
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read patch table: %w", err)
		}
		table, err := cheat.UnmarshalTable(data)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		// A table with no patches is a table this flag would apply nothing
		// from. Running anyway would be a run that looks patched and is not,
		// which is the failure this whole mechanism is built to make loud.
		if len(table.Patches) == 0 {
			return nil, fmt.Errorf("%s carries no patches, so -patch has nothing to apply from it", path)
		}
		tables = append(tables, patchTable{path: path, table: table})
	}
	return tables, nil
}

// applyStartPatches applies every table's byte patches to a session that has
// started but has not been ticked, and reports on notices to out.
//
// **A refusal stops the run, and both of the ways it can refuse are refusals.**
//
// The declared bytes not being there is the first. Continuing with a warning
// would leave a run that observes exactly what the patch was written to change:
// the gate still closed, the scene still unreached, the same frame as before.
// A person watching a terminal would read the warning; a sweep over hundreds of
// archives writes the run's summary next to every other run's and nobody reads
// the line above it. So the run does not start, and the non-zero exit is the
// same signal every other unrunnable command already gives a batch driver.
//
// The table naming a different image is the second, and it is the one the
// console decides differently. The console warns and carries on because a
// person is about to see the result and can say "wrong file"; here there is
// nobody to say it. The declared bytes would usually catch the mismatch a
// moment later, but "usually" is the whole problem — a table applied to an
// image it was not found in can match at those addresses by coincidence, and
// that is the silent corruption the declared bytes exist to prevent, arriving
// through the one door they cannot close. The key is cheap and certain where it
// speaks, so it is believed. Where it says nothing — a hand-written table, or a
// platform whose title is a bag of classes rather than one image — nothing has
// been asserted, and a note on stderr is the honest report of that.
//
// Notices go to stderr, not stdout: a run's stdout is one JSON summary that a
// sweep parses, and a line about patches is diagnostics.
func applyStartPatches(console *cheat.Console, archivePath string, tables []patchTable, out io.Writer) error {
	if len(tables) == 0 {
		return nil
	}
	if console == nil {
		return fmt.Errorf("-patch: this run has no cheat session, so there is nothing to patch through")
	}
	keyCheatTable(console, archivePath)
	session := console.Session()
	key := session.TableKey()
	for _, named := range tables {
		switch named.table.Match(key) {
		case cheat.MatchNone:
			return fmt.Errorf("%s was made against a different image than this run is holding\n"+
				"(a patch is true of the image it was found in, not of a file name; drop the \"image\" and \"file\" keys from a copy of the table to apply it here anyway)",
				named.path)
		case cheat.MatchUnkeyed:
			fmt.Fprintf(out, "patch: nothing in %s says what it was made against, so its addresses are taken on trust\n", named.path)
		}
		entries, err := session.ApplyTablePatches(named.table)
		if err != nil {
			return fmt.Errorf("%s: %w\n"+
				"(the run is refused rather than continued: a run whose patch did not apply observes exactly what the patch was written to change)",
				named.path, err)
		}
		fmt.Fprintf(out, "patch: applied %d entr%s from %s\n", entries, plural(entries, "y", "ies"), named.path)
		// A table saved from a console session carries the frozen values and
		// the watches beside its patches. This flag applies neither, and
		// dropping them without saying so is the kind of quiet that makes the
		// next hour's investigation start from a wrong belief.
		if count := len(named.table.Entries); count > 0 {
			fmt.Fprintf(out, "patch: %s also carries %d frozen value%s, which -patch does not apply; -cheat and its `load` command do\n",
				named.path, count, plural(count, "", "s"))
		}
		if count := len(named.table.Watches); count > 0 {
			fmt.Fprintf(out, "patch: %s also carries %d watch%s, which -patch does not arm; -cheat and its `load` command do\n",
				named.path, count, plural(count, "", "es"))
		}
	}
	return nil
}

func plural(count int, one, many string) string {
	if count == 1 {
		return one
	}
	return many
}
