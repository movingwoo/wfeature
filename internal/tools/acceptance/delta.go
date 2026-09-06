package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// What changed since last time, and what all of this run's failures have in
// common. Both are readings of the record file rather than new measurements:
// once a run says how far every archive got and where it stopped, the two
// questions a person actually asks after a sweep — "did anything get worse"
// and "what is the largest single cause" — are answered without running
// anything again.

// A change is one archive read twice.
type change struct {
	kind     changeKind
	platform string
	archive  string
	before   archiveRecord
	after    archiveRecord
}

type changeKind int

const (
	// better and worse are the two that matter, and they are only decided
	// between two runs of the same file.
	better changeKind = iota
	worse
	// replaced is the same name over different bytes. A corpus is a directory
	// somebody adds to, and a file re-downloaded under the name it had is not
	// the file that was measured last time; calling its grade a regression
	// would be a claim about this project made from somebody else's repack.
	replaced
	// added and removed are the corpus changing shape.
	added
	removed
	// unrunEither is a pair where one side has no ladder answer at all. A run
	// of one platform, or a stage that could not start, leaves records that
	// are not a fall from anything.
	unrunEither
)

// compare lines two runs up by platform and file name.
func compare(previous, current []archiveRecord) []change {
	type key struct{ platform, archive string }
	before := map[key]archiveRecord{}
	for _, record := range previous {
		before[key{record.Platform, record.Archive}] = record
	}
	seen := map[key]bool{}
	var changes []change
	for _, record := range current {
		at := key{record.Platform, record.Archive}
		seen[at] = true
		was, existed := before[at]
		if !existed {
			changes = append(changes, change{kind: added, platform: at.platform, archive: at.archive, after: record})
			continue
		}
		switch {
		case was.Grade == gradeUnrun || record.Grade == gradeUnrun:
			changes = append(changes, change{kind: unrunEither, platform: at.platform, archive: at.archive, before: was, after: record})
		case was.SHA256 != "" && record.SHA256 != "" && was.SHA256 != record.SHA256:
			changes = append(changes, change{kind: replaced, platform: at.platform, archive: at.archive, before: was, after: record})
		case record.Rung > was.Rung:
			changes = append(changes, change{kind: better, platform: at.platform, archive: at.archive, before: was, after: record})
		case record.Rung < was.Rung:
			changes = append(changes, change{kind: worse, platform: at.platform, archive: at.archive, before: was, after: record})
		// Two files can sit on no rung at all for different reasons: one was
		// refused at the rung it was asked, the other was knowingly declined
		// by every rung. Moving between those two is a real change with no
		// change of rung behind it, so a comparison that only looks at the
		// number would report nothing.
		case was.Grade == gradeNone && record.Grade == gradeSkipped:
			changes = append(changes, change{kind: better, platform: at.platform, archive: at.archive, before: was, after: record})
		case was.Grade == gradeSkipped && record.Grade == gradeNone:
			changes = append(changes, change{kind: worse, platform: at.platform, archive: at.archive, before: was, after: record})
		}
	}
	for at, record := range before {
		if !seen[at] {
			changes = append(changes, change{kind: removed, platform: at.platform, archive: at.archive, before: record})
		}
	}
	sort.Slice(changes, func(one, two int) bool {
		if changes[one].kind != changes[two].kind {
			return changes[one].kind < changes[two].kind
		}
		if changes[one].platform != changes[two].platform {
			return changes[one].platform < changes[two].platform
		}
		return changes[one].archive < changes[two].archive
	})
	return changes
}

// writeDelta puts the comparison into the report. A run that is the same as
// the one before it says so in a line, which is the answer a release check
// wants most often and the one a report full of prose cannot give.
func writeDelta(report *strings.Builder, previousRun runRecord, from string, changes []change) {
	fmt.Fprintf(report, "## Since %s\n\n", describeRun(previousRun, from))
	counted := map[changeKind]int{}
	for _, entry := range changes {
		counted[entry.kind]++
	}
	if counted[better]+counted[worse]+counted[added]+counted[removed]+counted[replaced] == 0 {
		report.WriteString("Every archive reached the same rung it reached then.\n\n")
		return
	}
	for _, section := range []struct {
		kind    changeKind
		heading string
	}{
		{worse, "Got worse"},
		{better, "Got further"},
		{replaced, "The file changed, so the grades are not comparable"},
		{added, "New in the corpus"},
		{removed, "No longer in the corpus"},
	} {
		rows := 0
		for _, entry := range changes {
			if entry.kind != section.kind {
				continue
			}
			if rows == 0 {
				fmt.Fprintf(report, "### %s (%d)\n\n", section.heading, counted[section.kind])
			}
			rows++
			report.WriteString(describeChange(entry))
		}
		if rows > 0 {
			report.WriteString("\n")
		}
	}
}

func describeChange(entry change) string {
	switch entry.kind {
	case added:
		return fmt.Sprintf("- `%s` (%s) — %s\n", entry.archive, entry.platform, gradeAndReason(entry.after))
	case removed:
		return fmt.Sprintf("- `%s` (%s) — was %s\n", entry.archive, entry.platform, gradeAndReason(entry.before))
	default:
		return fmt.Sprintf("- `%s` (%s) — %s → %s\n", entry.archive, entry.platform,
			entry.before.Grade, gradeAndReason(entry.after))
	}
}

func gradeAndReason(record archiveRecord) string {
	if record.Why == "" {
		return record.Grade
	}
	return fmt.Sprintf("%s, stopped at %s: %s", record.Grade, record.Stopped, record.Why)
}

func describeRun(previous runRecord, from string) string {
	if previous.Run != "" {
		return previous.Run
	}
	return filepath.Base(from)
}

// A cluster is one cause and every archive that met it. The reason a probe
// prints is written for the archive in front of it, so two archives failing
// the same way print two different lines; the class is what they have left
// when the counts and addresses are taken out.
type cluster struct {
	class    string
	archives []archiveRecord
}

// clusters groups this run's stopped archives by cause, largest first.
func clusters(archives []archiveRecord) []cluster {
	grouped := map[string][]archiveRecord{}
	for _, record := range archives {
		if record.WhyClass == "" {
			continue
		}
		grouped[record.WhyClass] = append(grouped[record.WhyClass], record)
	}
	var ordered []cluster
	for class, members := range grouped {
		ordered = append(ordered, cluster{class: class, archives: members})
	}
	sort.Slice(ordered, func(one, two int) bool {
		if len(ordered[one].archives) != len(ordered[two].archives) {
			return len(ordered[one].archives) > len(ordered[two].archives)
		}
		return ordered[one].class < ordered[two].class
	})
	return ordered
}

// writeClusters puts the grouping into the report. Reading a list of failures
// one line at a time is how a single cause behind a dozen of them stays
// invisible, and the count in front of a cause is what says which one to look
// at first.
func writeClusters(report *strings.Builder, archives []archiveRecord) {
	grouped := clusters(archives)
	if len(grouped) == 0 {
		return
	}
	report.WriteString("## Where they stopped, grouped by cause\n\n")
	report.WriteString("One row per cause, with the counts and addresses taken out of the reason so " +
		"two archives that stopped the same way count as two. The largest is where the next fix is.\n\n")
	report.WriteString("| archives | rung | cause |\n|---|---|---|\n")
	for _, entry := range grouped {
		rungs := map[string]bool{}
		for _, record := range entry.archives {
			rungs[record.Platform+" "+record.Stopped] = true
		}
		where := make([]string, 0, len(rungs))
		for rung := range rungs {
			where = append(where, rung)
		}
		sort.Strings(where)
		fmt.Fprintf(report, "| %d | %s | %s |\n", len(entry.archives), strings.Join(where, ", "), escapeCell(entry.class))
	}
	report.WriteString("\n")
	// The names behind each count, so a cause can be walked back to the files
	// it came from without a second run.
	for _, entry := range grouped {
		if len(entry.archives) < 2 {
			continue
		}
		fmt.Fprintf(report, "- **%s** — ", entry.class)
		names := make([]string, 0, len(entry.archives))
		for _, record := range entry.archives {
			names = append(names, "`"+record.Archive+"`")
		}
		sort.Strings(names)
		fmt.Fprintf(report, "%s\n", strings.Join(names, ", "))
	}
	report.WriteString("\n")
}

// escapeCell keeps a reason that carries a pipe from breaking the table it is
// written into.
func escapeCell(text string) string {
	return strings.ReplaceAll(text, "|", "\\|")
}

// newestRecords is the most recent record file in a directory other than the
// one this run is about to write, which is what `-since auto` compares with.
func newestRecords(directory, except string) string {
	entries, err := os.ReadDir(directory)
	if err != nil {
		return ""
	}
	var names []string
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), recordExtension) {
			continue
		}
		if filepath.Join(directory, entry.Name()) == except {
			continue
		}
		names = append(names, entry.Name())
	}
	if len(names) == 0 {
		return ""
	}
	// The names are dates, so the newest is the last one in order.
	sort.Strings(names)
	return filepath.Join(directory, names[len(names)-1])
}
