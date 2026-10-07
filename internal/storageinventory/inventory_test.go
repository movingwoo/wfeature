package storageinventory

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

// testLayout is a probe with three counters, one group over two of them and
// one limit, which is every part of a layout there is.
var testLayout = Layout{
	Platform: "test",
	Counters: []string{"handles", "dirty", "sinks"},
	Groups:   []Group{{Name: "pending", Counters: []string{"dirty", "sinks"}}},
	Limits:   []Limit{{Counter: "handles", Above: 3}},
}

func observe(tally *Tally, boundaries ...[3]int) {
	for _, counts := range boundaries {
		tally.Observe(counts[:])
	}
}

// What a tally keeps of a counter is its largest value and how many boundaries
// showed it at all. Those are two different facts: a title that once had four
// handles open and a title that has one open for ever answer the first the
// other way round from the second.
func TestTallyKeepsTheLargestValueAndHowOftenItWasSet(t *testing.T) {
	tally := testLayout.NewTally()
	observe(tally, [3]int{1, 0, 0}, [3]int{4, 1, 0}, [3]int{2, 0, 0}, [3]int{0, 0, 0})
	record := tally.Record("archive.zip", "abc", "clet", "ok")
	if record.Boundaries != 4 {
		t.Fatalf("boundaries = %d, want 4", record.Boundaries)
	}
	if want := map[string]int{"handles": 4, "dirty": 1, "sinks": 0}; !reflect.DeepEqual(record.Max, want) {
		t.Errorf("max = %v, want %v", record.Max, want)
	}
	if want := map[string]int{"handles": 3, "dirty": 1, "sinks": 0}; !reflect.DeepEqual(record.BoundariesWith, want) {
		t.Errorf("boundaries with = %v, want %v", record.BoundariesWith, want)
	}
	if record.Schema != Schema || record.Kind != ArchiveKind || record.Platform != "test" ||
		record.Archive != "archive.zip" || record.SHA256 != "abc" || record.Variant != "clet" || record.Result != "ok" {
		t.Errorf("the record lost what it was told about the archive: %+v", record)
	}
}

// A group is set at a boundary when any of its counters is, and a run is a
// stretch of consecutive boundaries it was set for. The pending group's runs
// are what say how many ticks one save spans.
func TestGroupCountsBoundariesAndRuns(t *testing.T) {
	for _, tc := range []struct {
		name       string
		boundaries [][3]int
		want       GroupRecord
	}{
		{"never set", [][3]int{{1, 0, 0}, {2, 0, 0}}, GroupRecord{}},
		{"either member sets it", [][3]int{{0, 1, 0}, {0, 0, 2}, {0, 3, 3}},
			GroupRecord{Boundaries: 3, Every: true, Runs: 1, LongestRun: 3}},
		{"two runs, the second longer", [][3]int{{0, 1, 0}, {0, 0, 0}, {0, 1, 0}, {0, 0, 1}, {0, 1, 0}, {0, 0, 0}},
			GroupRecord{Boundaries: 4, Runs: 2, LongestRun: 3}},
		{"a run still open at the last boundary counts", [][3]int{{0, 0, 0}, {0, 1, 0}, {0, 1, 0}},
			GroupRecord{Boundaries: 2, Runs: 1, LongestRun: 2}},
		{"a counter outside the group does not set it", [][3]int{{9, 0, 0}}, GroupRecord{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tally := testLayout.NewTally()
			observe(tally, tc.boundaries...)
			if got := tally.Record("a", "", "", "ok").Groups["pending"]; got != tc.want {
				t.Fatalf("pending = %+v, want %+v", got, tc.want)
			}
		})
	}
}

// An archive that reached no boundary has had nothing true of it at every
// boundary. Reading "all of none" as "every" would file every title that
// failed to start under the rule a refusal is written for.
func TestNoBoundaryIsNotEveryBoundary(t *testing.T) {
	record := testLayout.NewTally().Record("a", "", "", "skipped: start: no module")
	if record.Boundaries != 0 || record.Groups["pending"].Every || record.every("handles") {
		t.Fatalf("an archive with no boundary was reported as set at every one: %+v", record)
	}
}

func TestExamplesAreDistinctAndBounded(t *testing.T) {
	tally := testLayout.NewTally()
	for _, name := range []string{"save.dat", "save.dat", "b", "c", "d", "e"} {
		tally.Example("dirty", name)
	}
	tally.Example("sinks", strings.Repeat("x", 500))
	record := tally.Record("a", "", "", "ok")
	if want := []string{"save.dat", "b", "c", "d"}; !reflect.DeepEqual(record.Examples["dirty"], want) {
		t.Errorf("examples = %v, want the first %d distinct names %v", record.Examples["dirty"], exampleLimit, want)
	}
	if got := len(record.Examples["sinks"][0]); got != 120 {
		t.Errorf("a %d-byte example was kept, want it cut to 120", got)
	}
	if _, kept := record.Examples["handles"]; kept {
		t.Error("a counter nothing was noted for has examples")
	}
}

// The mistakes a probe can make with a layout are refused where the probe is
// written rather than discovered in a library's worth of wrong numbers.
func TestLayoutMistakesAreRefused(t *testing.T) {
	for name, layout := range map[string]Layout{
		"no platform":            {Counters: []string{"a"}},
		"no counter":             {Platform: "p"},
		"an unnamed counter":     {Platform: "p", Counters: []string{"a", ""}},
		"a repeated counter":     {Platform: "p", Counters: []string{"a", "a"}},
		"an empty group":         {Platform: "p", Counters: []string{"a"}, Groups: []Group{{Name: "g"}}},
		"a group of a stranger":  {Platform: "p", Counters: []string{"a"}, Groups: []Group{{Name: "g", Counters: []string{"b"}}}},
		"a repeated group":       {Platform: "p", Counters: []string{"a"}, Groups: []Group{{Name: "g", Counters: []string{"a"}}, {Name: "g", Counters: []string{"a"}}}},
		"a limit on a stranger":  {Platform: "p", Counters: []string{"a"}, Limits: []Limit{{Counter: "b", Above: 1}}},
		"an unnamed group":       {Platform: "p", Counters: []string{"a"}, Groups: []Group{{Counters: []string{"a"}}}},
		"a group with a typo in": {Platform: "p", Counters: []string{"dirty"}, Groups: []Group{{Name: "pending", Counters: []string{"dirty", "drity"}}}},
	} {
		if err := layout.Validate(); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
	if err := testLayout.Validate(); err != nil {
		t.Fatalf("the test layout was refused: %v", err)
	}
	for name, mistake := range map[string]func(){
		"a tally of a refused layout":    func() { Layout{Platform: "p"}.NewTally() },
		"a boundary of the wrong width":  func() { testLayout.NewTally().Observe([]int{1, 2}) },
		"an example for an unknown name": func() { testLayout.NewTally().Example("drity", "save.dat") },
	} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("%s did not panic", name)
				}
			}()
			mistake()
		}()
	}
}

// libraryRecords is four archives: two that keep a write pending, one of them
// at every boundary, one that never does, and one that never started.
func libraryRecords() []ArchiveRecord {
	always := testLayout.NewTally()
	observe(always, [3]int{5, 1, 0}, [3]int{5, 1, 0})
	sometimes := testLayout.NewTally()
	observe(sometimes, [3]int{1, 0, 0}, [3]int{1, 0, 2}, [3]int{1, 0, 0})
	never := testLayout.NewTally()
	observe(never, [3]int{0, 0, 0})
	return []ArchiveRecord{
		always.Record("always.zip", "0123456789abcdef", "java", "ok"),
		sometimes.Record("sometimes.zip", "", "clet", "ended: the title exited at round 3"),
		never.Record("never.zip", "", "clet", "ok"),
		testLayout.NewTally().Record("broken.zip", "", "", "skipped: start: no module"),
	}
}

func TestSummaryCountsArchivesNotBoundaries(t *testing.T) {
	summary := testLayout.Summarize(libraryRecords())
	if summary.Schema != Schema || summary.Kind != SummaryKind || summary.Platform != "test" {
		t.Errorf("summary header = %+v", summary)
	}
	if summary.Archives != 4 || summary.Measured != 3 {
		t.Errorf("archives = %d, measured = %d, want 4 and 3: an archive with no boundary is listed and not counted", summary.Archives, summary.Measured)
	}
	if want := map[string]int{"java": 1, "clet": 2}; !reflect.DeepEqual(summary.Variants, want) {
		t.Errorf("variants = %v, want %v", summary.Variants, want)
	}
	if want := map[string]int{"handles": 2, "dirty": 1, "sinks": 1}; !reflect.DeepEqual(summary.Ever, want) {
		t.Errorf("ever = %v, want %v", summary.Ever, want)
	}
	// Handles were open at every boundary of two archives; only one of them
	// was dirty at every boundary.
	if want := map[string]int{"handles": 2, "dirty": 1, "sinks": 0}; !reflect.DeepEqual(summary.Every, want) {
		t.Errorf("every = %v, want %v", summary.Every, want)
	}
	if want := map[string]map[string]int{"java": {"handles": 1, "dirty": 1}, "clet": {"handles": 1, "sinks": 1}}; !reflect.DeepEqual(summary.EverByVariant, want) {
		t.Errorf("ever by variant = %v, want %v", summary.EverByVariant, want)
	}
	if want := (GroupSummary{Ever: 2, Every: 1}); summary.Groups["pending"] != want {
		t.Errorf("pending = %+v, want %+v", summary.Groups["pending"], want)
	}
	if want := []LimitSummary{{Counter: "handles", Above: 3, Archives: 1}}; !reflect.DeepEqual(summary.Limits, want) {
		t.Errorf("limits = %+v, want %+v", summary.Limits, want)
	}
	if want := map[string]int{"ok": 2, "ended": 1, "skipped": 1}; !reflect.DeepEqual(summary.Results, want) {
		t.Errorf("results = %v, want %v", summary.Results, want)
	}
}

// An empty library is a summary too: every counter is there with a zero, so a
// reader sees which conditions were looked for and found in nothing.
func TestAnEmptyLibrarySummarizesToZeros(t *testing.T) {
	summary := testLayout.Summarize(nil)
	if summary.Archives != 0 || summary.Measured != 0 || len(summary.Ever) != 3 || len(summary.Every) != 3 {
		t.Fatalf("empty summary = %+v", summary)
	}
	for _, name := range testLayout.Counters {
		if summary.Ever[name] != 0 || summary.Every[name] != 0 {
			t.Errorf("%s was counted in an empty library", name)
		}
	}
	if len(summary.Limits) != 1 || summary.Limits[0].Archives != 0 {
		t.Errorf("limits = %+v", summary.Limits)
	}
	lines := testLayout.Table(summary)
	if len(lines) != 2+len(testLayout.Counters)+len(testLayout.Groups)+len(testLayout.Limits) {
		t.Fatalf("the empty table has %d lines:\n%s", len(lines), strings.Join(lines, "\n"))
	}
	if !strings.Contains(lines[0], "0 archives, 0 measured") {
		t.Errorf("heading = %q", lines[0])
	}
}

func TestTableAndLineSayWhatWasCounted(t *testing.T) {
	records := libraryRecords()
	lines := testLayout.Table(testLayout.Summarize(records))
	table := strings.Join(lines, "\n")
	fields := func(prefix string) []string {
		t.Helper()
		for _, line := range lines {
			if rest, found := strings.CutPrefix(line, prefix); found {
				return strings.Fields(rest)
			}
		}
		t.Fatalf("no row starts with %q in:\n%s", prefix, table)
		return nil
	}
	if !strings.Contains(lines[0], "4 archives, 3 measured (clet 2, java 1)") {
		t.Errorf("heading = %q", lines[0])
	}
	// ever, every, then one column per variant in name order: clet, java.
	if got, want := fields("handles "), []string{"2", "2", "1", "1"}; !reflect.DeepEqual(got, want) {
		t.Errorf("handles row = %v, want %v\n%s", got, want, table)
	}
	if got, want := fields("sinks "), []string{"1", "0", "1", "0"}; !reflect.DeepEqual(got, want) {
		t.Errorf("sinks row = %v, want %v\n%s", got, want, table)
	}
	if got, want := fields("pending (dirty, sinks)"), []string{"2", "1"}; !reflect.DeepEqual(got, want) {
		t.Errorf("pending row = %v, want %v\n%s", got, want, table)
	}
	if got, want := fields("handles above 3"), []string{"1"}; !reflect.DeepEqual(got, want) {
		t.Errorf("limit row = %v, want %v\n%s", got, want, table)
	}
	if got, want := fields("result skipped"), []string{"1"}; !reflect.DeepEqual(got, want) {
		t.Errorf("result row = %v, want %v\n%s", got, want, table)
	}

	line := testLayout.Line(records[0])
	for _, want := range []string{"java", "0123456789ab ", "always.zip: ok, 2 boundaries", "pending at 2 (every one), runs 1, longest 2; handles=5/2 dirty=1/2"} {
		if !strings.Contains(line, want) {
			t.Errorf("the archive line lacks %q: %s", want, line)
		}
	}
	if strings.Contains(line, "0123456789abc") || strings.Contains(line, "sinks=") {
		t.Errorf("the archive line carries a whole digest or a counter that was never set: %s", line)
	}
	if line := testLayout.Line(records[3]); !strings.Contains(line, "broken.zip: skipped: start: no module, 0 boundaries; pending at 0") {
		t.Errorf("an archive that never started reads %q", line)
	}
}

func TestReasonIsOneBoundedLine(t *testing.T) {
	if got := Reason(nil); got != "" {
		t.Errorf("no error has the reason %q", got)
	}
	if got := Reason(errors.New("data abort at 0x1000\nr0=1 r1=2")); got != "data abort at 0x1000" {
		t.Errorf("reason = %q, want the first line", got)
	}
	if got := Reason(errors.New(strings.Repeat("y", 400))); len(got) != 160 {
		t.Errorf("a %d-byte reason was kept, want 160", len(got))
	}
}
