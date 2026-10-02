// Package storageinventory holds the arithmetic the opt-in storage inventory
// probes share: what to keep of a count taken at every tick boundary, and how
// a library of archives is summed up from it.
//
// # Why it is here rather than three times over
//
// Three probes count host-side storage state in three runtimes — the LGT
// client, the KTF descriptor runtime and the KTF native platform. What each of
// them counts is its own business, because it is what that runtime is made of:
// open handles, catalog entries, unsaved keys. What each of them then does
// with a count is the same, and a figure is only worth comparing across
// platforms if it was arrived at the same way on each: the largest value an
// archive showed, the number of boundaries that showed it at all, whether a
// set of counts was ever set or set at every boundary, and how many archives
// of a library that was true of. That is here once, the way internal/ladder
// holds the one judgment three acceptance ladders share.
//
// Nothing but a test imports this package, and nothing in it runs a guest.
//
// # What a boundary is
//
// A probe ticks a session and looks at its tables between two ticks, which is
// the only moment a quick save or a quick load can be taken at. One look is
// one boundary, and what it produces is one number per counter, in the order
// the probe's Layout names them.
//
// # Counters and groups
//
// A counter is something countable at a boundary: open handles, dirty ones.
// A group is a question asked of several counters at once — "is any write
// pending" is "is any handle dirty, or does any stream bound to a file hold
// bytes" — and it is answered per boundary, so an archive can be said to have
// had a write pending at twelve boundaries of a hundred and fifty, or at every
// one of them. The second is the answer that matters to a rule that would
// refuse a key press while a write is pending: a title for which that is true
// at every boundary could never be quick saved at all.
//
// A group also counts runs: how many separate stretches of consecutive
// boundaries it was set for, and how long the longest was. For the pending
// group that is how many ticks one save spans.
package storageinventory

import (
	"fmt"
	"sort"
	"strings"
)

// Schema is the first field of every line a probe writes. A reader that meets
// a number it does not know is reading a file written by a newer probe.
const Schema = 1

// The kinds of line a record file holds: what ran, one line per archive, and
// the library summed up.
const (
	RunKind     = "run"
	ArchiveKind = "archive"
	SummaryKind = "summary"
)

// A Group is a set of counters that answer one question about a boundary
// together: was any of them not zero.
type Group struct {
	Name     string   `json:"name"`
	Counters []string `json:"counters"`
}

// A Limit names a count that is worth a line of its own once it is exceeded,
// because a rule elsewhere refuses past it.
type Limit struct {
	Counter string `json:"counter"`
	Above   int    `json:"above"`
}

// Layout is what one probe counts, in the order it counts it.
type Layout struct {
	Platform string
	Counters []string
	Groups   []Group
	Limits   []Limit
}

// Validate refuses a layout a tally could not be kept for. A layout is program
// text, so this is a check on the probe rather than on anything it measured.
func (layout Layout) Validate() error {
	if layout.Platform == "" || len(layout.Counters) == 0 {
		return fmt.Errorf("storage inventory layout names no platform or no counter")
	}
	known := make(map[string]bool, len(layout.Counters))
	for _, name := range layout.Counters {
		if name == "" || known[name] {
			return fmt.Errorf("storage inventory counter %q is empty or repeated", name)
		}
		known[name] = true
	}
	groups := make(map[string]bool, len(layout.Groups))
	for _, group := range layout.Groups {
		if group.Name == "" || groups[group.Name] || len(group.Counters) == 0 {
			return fmt.Errorf("storage inventory group %q is empty or repeated", group.Name)
		}
		groups[group.Name] = true
		for _, name := range group.Counters {
			if !known[name] {
				return fmt.Errorf("storage inventory group %q names the unknown counter %q", group.Name, name)
			}
		}
	}
	for _, limit := range layout.Limits {
		if !known[limit.Counter] {
			return fmt.Errorf("storage inventory limit names the unknown counter %q", limit.Counter)
		}
	}
	return nil
}

func (layout Layout) index(name string) int {
	for index, counter := range layout.Counters {
		if counter == name {
			return index
		}
	}
	return -1
}

// exampleLimit is how many names a tally keeps per counter. The names are what
// turns "two handles were dirty" into which files they were, and a handful is
// enough to recognise a title's save by.
const exampleLimit = 4

// Tally is one archive's boundaries.
type Tally struct {
	layout     Layout
	boundaries int
	max        []int
	seen       []int
	groups     []groupTally
	examples   map[string][]string
}

type groupTally struct {
	members    []int
	boundaries int
	runs       int
	longest    int
	current    int
}

// NewTally starts an archive. It panics on a layout Validate refuses, because
// that is a mistake in the probe and every archive would repeat it.
func (layout Layout) NewTally() *Tally {
	if err := layout.Validate(); err != nil {
		panic(err)
	}
	tally := &Tally{
		layout: layout,
		max:    make([]int, len(layout.Counters)),
		seen:   make([]int, len(layout.Counters)),
		groups: make([]groupTally, len(layout.Groups)),
	}
	for index, group := range layout.Groups {
		for _, name := range group.Counters {
			tally.groups[index].members = append(tally.groups[index].members, layout.index(name))
		}
	}
	return tally
}

// Observe adds one boundary. counts holds one number per counter in the
// layout's order; a slice of another length is the probe's mistake.
func (tally *Tally) Observe(counts []int) {
	if len(counts) != len(tally.layout.Counters) {
		panic(fmt.Sprintf("storage inventory boundary has %d counts for %d counters", len(counts), len(tally.layout.Counters)))
	}
	tally.boundaries++
	for index, count := range counts {
		if count > tally.max[index] {
			tally.max[index] = count
		}
		if count != 0 {
			tally.seen[index]++
		}
	}
	for index := range tally.groups {
		group := &tally.groups[index]
		set := false
		for _, member := range group.members {
			if counts[member] != 0 {
				set = true
				break
			}
		}
		if !set {
			group.current = 0
			continue
		}
		group.boundaries++
		if group.current == 0 {
			group.runs++
		}
		group.current++
		if group.current > group.longest {
			group.longest = group.current
		}
	}
}

// Example keeps one name a counter was seen on: a file, a database, the reason
// a walk was refused. Only the first few distinct names per counter are kept.
func (tally *Tally) Example(counter, name string) {
	if tally.layout.index(counter) < 0 {
		panic(fmt.Sprintf("storage inventory example names the unknown counter %q", counter))
	}
	if len(name) > 120 {
		name = name[:120]
	}
	held := tally.examples[counter]
	if len(held) >= exampleLimit {
		return
	}
	for _, existing := range held {
		if existing == name {
			return
		}
	}
	if tally.examples == nil {
		tally.examples = make(map[string][]string)
	}
	tally.examples[counter] = append(held, name)
}

// GroupRecord is what one group answered over an archive's boundaries.
type GroupRecord struct {
	// Boundaries is how many boundaries had any of the group's counters set,
	// and Every whether that was all of them.
	Boundaries int  `json:"boundaries"`
	Every      bool `json:"every"`
	// Runs is how many separate stretches of consecutive boundaries that was,
	// and LongestRun how many boundaries the longest of them covered.
	Runs       int `json:"runs"`
	LongestRun int `json:"longest_run"`
}

// ArchiveRecord is one archive and everything its boundaries showed.
type ArchiveRecord struct {
	Schema   int    `json:"schema"`
	Kind     string `json:"kind"`
	Platform string `json:"platform"`
	Archive  string `json:"archive"`
	SHA256   string `json:"sha256,omitempty"`
	// Variant is which of a platform's runtimes ran the archive.
	Variant string `json:"variant,omitempty"`
	// Result is "ok" for a run that visited every boundary it was asked for.
	// Anything else starts with a word and a colon — "skipped: ", "ended: ",
	// "stopped: " — and the boundaries visited before that still count.
	Result     string `json:"result"`
	Boundaries int    `json:"boundaries"`
	// Max is the largest value each counter showed, and BoundariesWith how
	// many boundaries showed it at anything but zero.
	Max            map[string]int         `json:"max"`
	BoundariesWith map[string]int         `json:"boundaries_with"`
	Groups         map[string]GroupRecord `json:"groups,omitempty"`
	Examples       map[string][]string    `json:"examples,omitempty"`
}

// Record closes an archive's tally.
func (tally *Tally) Record(archive, digest, variant, result string) ArchiveRecord {
	record := ArchiveRecord{
		Schema: Schema, Kind: ArchiveKind, Platform: tally.layout.Platform,
		Archive: archive, SHA256: digest, Variant: variant, Result: result,
		Boundaries:     tally.boundaries,
		Max:            make(map[string]int, len(tally.max)),
		BoundariesWith: make(map[string]int, len(tally.seen)),
	}
	for index, name := range tally.layout.Counters {
		record.Max[name] = tally.max[index]
		record.BoundariesWith[name] = tally.seen[index]
	}
	if len(tally.groups) != 0 {
		record.Groups = make(map[string]GroupRecord, len(tally.groups))
	}
	for index, group := range tally.layout.Groups {
		held := tally.groups[index]
		record.Groups[group.Name] = GroupRecord{
			Boundaries: held.boundaries,
			Every:      tally.boundaries > 0 && held.boundaries == tally.boundaries,
			Runs:       held.runs,
			LongestRun: held.longest,
		}
	}
	if len(tally.examples) != 0 {
		record.Examples = make(map[string][]string, len(tally.examples))
		for counter, names := range tally.examples {
			record.Examples[counter] = append([]string(nil), names...)
		}
	}
	return record
}

// every reports whether a counter was set at every boundary of an archive
// that had any.
func (record ArchiveRecord) every(counter string) bool {
	return record.Boundaries > 0 && record.BoundariesWith[counter] == record.Boundaries
}

// Line is one archive in a test log: what ran, how far, each group's answer,
// and every counter that was ever not zero as "name=largest/boundaries".
func (layout Layout) Line(record ArchiveRecord) string {
	var line strings.Builder
	digest := record.SHA256
	if len(digest) > 12 {
		digest = digest[:12]
	}
	fmt.Fprintf(&line, "%-6s %-12s %s: %s, %d boundaries", record.Variant, digest, record.Archive, record.Result, record.Boundaries)
	for _, group := range layout.Groups {
		held := record.Groups[group.Name]
		fmt.Fprintf(&line, "; %s at %d", group.Name, held.Boundaries)
		if held.Every {
			line.WriteString(" (every one)")
		}
		if held.Runs != 0 {
			fmt.Fprintf(&line, ", runs %d, longest %d", held.Runs, held.LongestRun)
		}
	}
	separator := ";"
	for _, name := range layout.Counters {
		if record.Max[name] != 0 {
			fmt.Fprintf(&line, "%s %s=%d/%d", separator, name, record.Max[name], record.BoundariesWith[name])
			separator = ""
		}
	}
	return line.String()
}

// GroupSummary is how many archives a group was ever set for, and how many it
// was set for at every boundary.
type GroupSummary struct {
	Ever  int `json:"ever"`
	Every int `json:"every"`
}

// LimitSummary is how many archives ever showed more than a limit.
type LimitSummary struct {
	Counter  string `json:"counter"`
	Above    int    `json:"above"`
	Archives int    `json:"archives"`
}

// SummaryRecord is a library: how many of its archives ever had each
// condition.
type SummaryRecord struct {
	Schema   int    `json:"schema"`
	Kind     string `json:"kind"`
	Platform string `json:"platform"`
	// Archives is every file the probe was handed, and Measured the ones that
	// reached at least one boundary. Only those are counted below.
	Archives int            `json:"archives"`
	Measured int            `json:"measured"`
	Variants map[string]int `json:"variants"`
	// Ever is how many archives showed a counter at anything but zero at some
	// boundary, and Every how many showed it at all of theirs.
	Ever          map[string]int            `json:"ever"`
	Every         map[string]int            `json:"every"`
	EverByVariant map[string]map[string]int `json:"ever_by_variant"`
	Groups        map[string]GroupSummary   `json:"groups,omitempty"`
	Limits        []LimitSummary            `json:"limits,omitempty"`
	// Results counts the archives by the word their result starts with.
	Results map[string]int `json:"results"`
}

// resultClass is the word a result starts with, which is what a count of
// results is grouped by: the reason after the colon differs per archive.
func resultClass(result string) string {
	class, _, _ := strings.Cut(result, ":")
	return strings.TrimSpace(class)
}

// Summarize sums a library up.
func (layout Layout) Summarize(records []ArchiveRecord) SummaryRecord {
	summary := SummaryRecord{
		Schema: Schema, Kind: SummaryKind, Platform: layout.Platform, Archives: len(records),
		Variants: map[string]int{}, Ever: map[string]int{}, Every: map[string]int{},
		EverByVariant: map[string]map[string]int{}, Results: map[string]int{},
	}
	for _, name := range layout.Counters {
		summary.Ever[name], summary.Every[name] = 0, 0
	}
	if len(layout.Groups) != 0 {
		summary.Groups = make(map[string]GroupSummary, len(layout.Groups))
		for _, group := range layout.Groups {
			summary.Groups[group.Name] = GroupSummary{}
		}
	}
	exceeded := make([]int, len(layout.Limits))
	for _, record := range records {
		summary.Results[resultClass(record.Result)]++
		if record.Boundaries == 0 {
			continue
		}
		summary.Measured++
		summary.Variants[record.Variant]++
		if summary.EverByVariant[record.Variant] == nil {
			summary.EverByVariant[record.Variant] = map[string]int{}
		}
		for _, name := range layout.Counters {
			if record.Max[name] == 0 {
				continue
			}
			summary.Ever[name]++
			summary.EverByVariant[record.Variant][name]++
			if record.every(name) {
				summary.Every[name]++
			}
		}
		for _, group := range layout.Groups {
			held, total := record.Groups[group.Name], summary.Groups[group.Name]
			if held.Boundaries != 0 {
				total.Ever++
			}
			if held.Every {
				total.Every++
			}
			summary.Groups[group.Name] = total
		}
		for index, limit := range layout.Limits {
			if record.Max[limit.Counter] > limit.Above {
				exceeded[index]++
			}
		}
	}
	for index, limit := range layout.Limits {
		summary.Limits = append(summary.Limits, LimitSummary{Counter: limit.Counter, Above: limit.Above, Archives: exceeded[index]})
	}
	return summary
}

// Table is the library summary as the lines of a test log: one row per
// counter, group and limit, with the number of archives it was ever true of,
// the number it was true of at every boundary, and the first again per
// variant.
func (layout Layout) Table(summary SummaryRecord) []string {
	variants := make([]string, 0, len(summary.Variants))
	for variant := range summary.Variants {
		variants = append(variants, variant)
	}
	sort.Strings(variants)
	measured := make([]string, 0, len(variants))
	for _, variant := range variants {
		measured = append(measured, fmt.Sprintf("%s %d", variantLabel(variant), summary.Variants[variant]))
	}
	heading := fmt.Sprintf("%s storage inventory: %d archives, %d measured", layout.Platform, summary.Archives, summary.Measured)
	if len(measured) != 0 {
		heading += " (" + strings.Join(measured, ", ") + ")"
	}
	width := len("archives in which it was not zero")
	for _, name := range layout.Counters {
		width = max(width, len(name))
	}
	for _, group := range layout.Groups {
		width = max(width, len(groupLabel(group)))
	}
	for _, limit := range layout.Limits {
		width = max(width, len(limitLabel(limit)))
	}
	row := func(label string, ever int, every string, perVariant func(string) string) string {
		var line strings.Builder
		fmt.Fprintf(&line, "%-*s %6d %6s", width, label, ever, every)
		for _, variant := range variants {
			fmt.Fprintf(&line, " %6s", perVariant(variant))
		}
		return line.String()
	}
	var header strings.Builder
	fmt.Fprintf(&header, "%-*s %6s %6s", width, "archives in which it was not zero", "ever", "every")
	for _, variant := range variants {
		fmt.Fprintf(&header, " %6s", variantLabel(variant))
	}
	lines := []string{heading, header.String()}
	for _, name := range layout.Counters {
		lines = append(lines, row(name, summary.Ever[name], fmt.Sprint(summary.Every[name]), func(variant string) string {
			return fmt.Sprint(summary.EverByVariant[variant][name])
		}))
	}
	// A group and a limit have no per-variant figure: the record a reader
	// wants that from is the archive's own line.
	blank := func(string) string { return "" }
	for _, group := range layout.Groups {
		held := summary.Groups[group.Name]
		lines = append(lines, row(groupLabel(group), held.Ever, fmt.Sprint(held.Every), blank))
	}
	for _, limit := range summary.Limits {
		lines = append(lines, row(limitLabel(Limit{Counter: limit.Counter, Above: limit.Above}), limit.Archives, "", blank))
	}
	classes := make([]string, 0, len(summary.Results))
	for class := range summary.Results {
		classes = append(classes, class)
	}
	sort.Strings(classes)
	for _, class := range classes {
		lines = append(lines, fmt.Sprintf("%-*s %6d", width, "result "+class, summary.Results[class]))
	}
	return lines
}

func variantLabel(variant string) string {
	if variant == "" {
		return "-"
	}
	return variant
}

func groupLabel(group Group) string {
	return fmt.Sprintf("%s (%s)", group.Name, strings.Join(group.Counters, ", "))
}

func limitLabel(limit Limit) string {
	return fmt.Sprintf("%s above %d", limit.Counter, limit.Above)
}

// Reason is the first line of an error, cut to a length a table can carry.
// What stopped an archive is one line of a report, and a guest fault prints a
// register dump after its first.
func Reason(err error) string {
	if err == nil {
		return ""
	}
	text := err.Error()
	if cut := strings.IndexByte(text, '\n'); cut >= 0 {
		text = text[:cut]
	}
	if len(text) > 160 {
		text = text[:160]
	}
	return text
}
