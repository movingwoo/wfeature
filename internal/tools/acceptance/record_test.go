package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The record file is what two runs are compared with, so every line has to
// carry what the comparison needs: the file it is about, the bytes it had, how
// far it got, and where it stopped.
func TestARecordCarriesTheFileTheGradeAndWhereItStopped(t *testing.T) {
	root := corpusRoot(t, map[string]map[string][]byte{
		"ktf": {
			"reaches-a-frame.zip": []byte("PK\x03\x04 not really an archive"),
			"stops-at-load.zip":   []byte("PK\x03\x04 nor is this one"),
		},
	})
	day := time.Date(2026, 9, 4, 10, 0, 0, 0, time.UTC)
	run, archives := buildRecords(root, []result{
		{
			stage:  stageNamed(t, "ktf", "parse"),
			passed: []string{"reaches-a-frame.zip", "stops-at-load.zip"},
		},
		{
			stage:  stageNamed(t, "ktf", "load"),
			passed: []string{"reaches-a-frame.zip"},
			failed: []note{{"stops-at-load.zip", "load main class: no class named in the descriptor"}},
		},
		{
			stage:  stageNamed(t, "ktf", "frame"),
			passed: []string{"reaches-a-frame.zip"},
		},
	}, day, []string{"ktf"})

	if run.Schema != recordSchema || run.Kind != runKind {
		t.Errorf("the run line is %+v", run)
	}
	if run.Run != "2026-09-04T10:00:00Z" {
		t.Errorf("run = %q", run.Run)
	}
	if len(archives) != 2 {
		t.Fatalf("%d archive records, want one per file in the directory", len(archives))
	}
	byName := map[string]archiveRecord{}
	for _, record := range archives {
		byName[record.Archive] = record
	}

	reached := byName["reaches-a-frame.zip"]
	if reached.Grade != "frame" || reached.Rung != 6 {
		t.Errorf("grade = %q at rung %d, want frame", reached.Grade, reached.Rung)
	}
	if reached.Stopped != "" {
		t.Errorf("an archive that passed every rung it was asked stopped at %q", reached.Stopped)
	}
	if reached.SHA256 == "" || reached.Size == 0 {
		t.Errorf("the record carries no digest for the bytes it graded: %+v", reached)
	}

	stopped := byName["stops-at-load.zip"]
	// Parse passed and load did not, so the grade is the rung below the one it
	// stopped at rather than the last stage that ran.
	if stopped.Grade != "parse" || stopped.Rung != 1 {
		t.Errorf("grade = %q at rung %d, want parse", stopped.Grade, stopped.Rung)
	}
	if stopped.Stopped != "load" || stopped.StoppedOutcome != outcomeFailed {
		t.Errorf("stopped = %q (%q), want load", stopped.Stopped, stopped.StoppedOutcome)
	}
	if stopped.Why == "" || stopped.WhyClass == "" {
		t.Errorf("a stopped archive carries no reason: %+v", stopped)
	}
}

// A file nothing picked up is the finding a pass count cannot show. It gets a
// record with what detection made of it, and a grade that says no rung
// answered rather than one that says it reached nothing.
func TestAFileNoProbeRanIsStillARecordWithWhyNothingClaimedIt(t *testing.T) {
	root := corpusRoot(t, map[string]map[string][]byte{
		"ktf": {"half-a-download.zip": []byte("PK\x03\x04 and then the connection dropped")},
	})
	_, archives := buildRecords(root, []result{{stage: stageNamed(t, "ktf", "parse")}}, time.Now(), []string{"ktf"})
	if len(archives) != 1 {
		t.Fatalf("%d records, want one", len(archives))
	}
	record := archives[0]
	if record.Grade != gradeUnrun {
		t.Errorf("grade = %q, want %q", record.Grade, gradeUnrun)
	}
	if record.DetectReason != "not-an-archive" {
		t.Errorf("detect reason = %q, want not-an-archive", record.DetectReason)
	}
	if record.DetectError == "" {
		t.Errorf("a file the reader refused carries no refusal")
	}
}

// A package of a shape this ladder does not drive is not a failure. Every rung
// declining it and every rung refusing it are different answers, and a sweep
// that reports both as "reached nothing" is reporting a number about itself.
func TestARungDecliningAnArchiveIsNotTheSameAsRefusingIt(t *testing.T) {
	root := corpusRoot(t, map[string]map[string][]byte{
		"ktf": {
			"declined.zip": []byte("PK\x03\x04"),
			"refused.zip":  []byte("PK\x03\x04"),
		},
	})
	_, archives := buildRecords(root, []result{{
		stage:   stageNamed(t, "ktf", "parse"),
		skipped: []note{{"declined.zip", "a package this probe does not drive"}},
		failed:  []note{{"refused.zip", "parse: not a valid zip file"}},
	}}, time.Now(), []string{"ktf"})
	grades := map[string]string{}
	for _, record := range archives {
		grades[record.Archive] = record.Grade
	}
	if grades["declined.zip"] != gradeSkipped {
		t.Errorf("a declined archive is graded %q, want %q", grades["declined.zip"], gradeSkipped)
	}
	if grades["refused.zip"] != gradeNone {
		t.Errorf("a refused archive is graded %q, want %q", grades["refused.zip"], gradeNone)
	}
}

// A subtest is named after the archive, and `go test` rewrites that name
// before it prints it. A record that joined the rows to the files by the name
// on disk would report every archive with a space in its name as one no probe
// ever answered for — and there are plenty of those in a corpus of downloads.
func TestARowIsJoinedToItsFileThroughTheNameGoTestPrints(t *testing.T) {
	root := corpusRoot(t, map[string]map[string][]byte{
		"ktf": {"a title with spaces.zip": []byte("PK\x03\x04")},
	})
	_, archives := buildRecords(root, []result{{
		stage:  stageNamed(t, "ktf", "parse"),
		passed: []string{"a_title_with_spaces.zip"},
	}}, time.Now(), []string{"ktf"})
	if len(archives) != 1 {
		t.Fatalf("%d records, want one: %+v", len(archives), archives)
	}
	if archives[0].Archive != "a title with spaces.zip" {
		t.Errorf("the record is named %q, want the name on disk", archives[0].Archive)
	}
	if archives[0].Grade != "parse" {
		t.Errorf("grade = %q, want the rung the row said it passed", archives[0].Grade)
	}
}

// Two archives that stopped the same way print two different lines, because a
// probe writes its reason for the archive in front of it. Grouping is what
// turns a list of failures into a count per cause, and the count is what says
// which one to look at first.
func TestFailuresGroupByCauseWithTheCountsTakenOut(t *testing.T) {
	archives := []archiveRecord{
		{Platform: "ktf", Archive: "one.zip", Stopped: "frame", Why: "no frame (ticks=512 flushes=0 drawn=0 tickErr=<nil>)"},
		{Platform: "ktf", Archive: "two.zip", Stopped: "frame", Why: "no frame (ticks=97 flushes=3 drawn=0 tickErr=<nil>)"},
		{Platform: "ktf", Archive: "three.zip", Stopped: "load", Why: "load main class: no class named"},
	}
	for index := range archives {
		archives[index].WhyClass = classify(archives[index].Why)
	}
	grouped := clusters(archives)
	if len(grouped) != 2 {
		t.Fatalf("%d causes, want 2: %+v", len(grouped), grouped)
	}
	if len(grouped[0].archives) != 2 {
		t.Errorf("the largest cause has %d archives, want the two that stopped the same way", len(grouped[0].archives))
	}
	if strings.Contains(grouped[0].class, "512") || strings.Contains(grouped[0].class, "97") {
		t.Errorf("the cause still carries the counts that differ between them: %q", grouped[0].class)
	}
}

// The comparison is the reason the records exist. What it must not do is
// report a file somebody replaced as a regression, or read "no rung answered"
// as a fall from the rung one did.
func TestTheComparisonNamesWhatMovedAndRefusesToGuessAtWhatDidNot(t *testing.T) {
	previous := []archiveRecord{
		{Platform: "ktf", Archive: "refused-then-declined.zip", Grade: gradeNone, SHA256: "99"},
		{Platform: "ktf", Archive: "declined-then-refused.zip", Grade: gradeSkipped, SHA256: "98"},
		{Platform: "ktf", Archive: "improved.zip", Grade: "load", Rung: 3, SHA256: "aa"},
		{Platform: "ktf", Archive: "regressed.zip", Grade: "frame", Rung: 6, SHA256: "bb"},
		{Platform: "ktf", Archive: "steady.zip", Grade: "frame", Rung: 6, SHA256: "cc"},
		{Platform: "ktf", Archive: "repacked.zip", Grade: "frame", Rung: 6, SHA256: "dd"},
		{Platform: "ktf", Archive: "gone.zip", Grade: "frame", Rung: 6, SHA256: "ee"},
		{Platform: "skt", Archive: "not-run-this-time.zip", Grade: "boot", Rung: 1, SHA256: "ff"},
	}
	current := []archiveRecord{
		{Platform: "ktf", Archive: "refused-then-declined.zip", Grade: gradeSkipped, SHA256: "99"},
		{Platform: "ktf", Archive: "declined-then-refused.zip", Grade: gradeNone, SHA256: "98"},
		{Platform: "ktf", Archive: "improved.zip", Grade: "frame", Rung: 6, SHA256: "aa"},
		{Platform: "ktf", Archive: "regressed.zip", Grade: "load", Rung: 3, SHA256: "bb", Stopped: "construct", Why: "construct: out of steps"},
		{Platform: "ktf", Archive: "steady.zip", Grade: "frame", Rung: 6, SHA256: "cc"},
		{Platform: "ktf", Archive: "repacked.zip", Grade: "load", Rung: 3, SHA256: "d1"},
		{Platform: "ktf", Archive: "arrived.zip", Grade: "frame", Rung: 6, SHA256: "11"},
		{Platform: "skt", Archive: "not-run-this-time.zip", Grade: gradeUnrun, SHA256: "ff"},
	}
	kinds := map[string]changeKind{}
	for _, entry := range compare(previous, current) {
		kinds[entry.archive] = entry.kind
	}
	for archive, want := range map[string]changeKind{
		"improved.zip":  better,
		"regressed.zip": worse,
		// No rung moved for either of these: one stopped being refused and
		// started being declined, and the other did the reverse.
		"refused-then-declined.zip": better,
		"declined-then-refused.zip": worse,
		"repacked.zip":              replaced,
		"arrived.zip":               added,
		"gone.zip":                  removed,
		"not-run-this-time.zip":     unrunEither,
	} {
		if kinds[archive] != want {
			t.Errorf("%s was reported as kind %d, want %d", archive, kinds[archive], want)
		}
	}
	if _, reported := kinds["steady.zip"]; reported {
		t.Errorf("an archive that reached the same rung was reported as a change")
	}

	report := &strings.Builder{}
	writeDelta(report, runRecord{Run: "2026-09-01T00:00:00Z"}, "old.ndjson", compare(previous, current))
	for _, wanted := range []string{
		"## Since 2026-09-01T00:00:00Z",
		"### Got worse (2)",
		"`regressed.zip` (ktf) — frame → load, stopped at construct: construct: out of steps",
		"### Got further (2)",
	} {
		if !strings.Contains(report.String(), wanted) {
			t.Errorf("the delta does not carry %q:\n%s", wanted, report.String())
		}
	}
}

// A run that changed nothing has to say so in one line. A release check asks
// this question far more often than it asks any other, and a report that
// answers it with the whole corpus again is a report nobody reads.
func TestARunThatMovedNothingSaysSoInOneLine(t *testing.T) {
	same := []archiveRecord{{Platform: "ktf", Archive: "one.zip", Grade: "frame", Rung: 6, SHA256: "aa"}}
	report := &strings.Builder{}
	writeDelta(report, runRecord{Run: "2026-09-01T00:00:00Z"}, "old.ndjson", compare(same, same))
	if !strings.Contains(report.String(), "Every archive reached the same rung") {
		t.Errorf("an unchanged run was not reported as one:\n%s", report.String())
	}
}

// A record file written by a newer tool is refused rather than half-read. A
// comparison that silently drops the fields it cannot see reports differences
// that are its own.
func TestARecordFileFromANewerToolIsRefused(t *testing.T) {
	path := filepath.Join(t.TempDir(), "later"+recordExtension)
	line, err := json.Marshal(map[string]any{"schema": recordSchema + 1, "kind": runKind})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(line, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := readRecords(path); err == nil {
		t.Error("a file this tool cannot read came back as one it could")
	}
}

// What is written has to come back the same, because the two halves of a
// comparison are usually written by two different runs.
func TestRecordsSurviveTheirOwnFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "today"+recordExtension)
	run := runRecord{Schema: recordSchema, Kind: runKind, Run: "2026-09-04T10:00:00Z", GOOS: "darwin"}
	archives := []archiveRecord{{
		Schema: recordSchema, Kind: archiveKind, Run: run.Run,
		Platform: "ktf", Archive: "one.zip", SHA256: "aa", Size: 12,
		Grade: "frame", Rung: 6, Ladder: 8,
		Stages: map[string]stageOutcome{"frame": {Outcome: outcomePassed}},
	}}
	if err := writeRecords(path, run, archives); err != nil {
		t.Fatal(err)
	}
	readRun, readArchives, err := readRecords(path)
	if err != nil {
		t.Fatal(err)
	}
	if readRun.Run != run.Run || readRun.GOOS != run.GOOS {
		t.Errorf("the run line came back as %+v", readRun)
	}
	if len(readArchives) != 1 || readArchives[0].Grade != "frame" || readArchives[0].Ladder != 8 {
		t.Errorf("the archive line came back as %+v", readArchives)
	}
	if readArchives[0].Stages["frame"].Outcome != outcomePassed {
		t.Errorf("the stage outcomes came back as %+v", readArchives[0].Stages)
	}
}

// corpusRoot lays out a repository root with corpus directories in it, which
// is what the record builder reads to find the files no probe answered for.
func corpusRoot(t *testing.T, files map[string]map[string][]byte) string {
	t.Helper()
	root := t.TempDir()
	for platform, contents := range files {
		directory := filepath.Join(root, "var", "games", platform)
		if err := os.MkdirAll(directory, 0o755); err != nil {
			t.Fatal(err)
		}
		for name, data := range contents {
			if err := os.WriteFile(filepath.Join(directory, name), data, 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	return root
}
