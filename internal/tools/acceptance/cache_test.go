package main

import (
	"crypto/sha256"
	"encoding/hex"
	"runtime"
	"strings"
	"testing"
	"time"
)

// The whole of what the cache is allowed to claim: the same bytes, read by the
// same build, were already answered for. Anything else is measured again.
func TestAnArchiveIsCarriedForwardOnlyWhenItsBytesAndTheBuildAreUnchanged(t *testing.T) {
	steady := []byte("PK\x03\x04 the same file as last time")
	moved := []byte("PK\x03\x04 re-downloaded since")
	root := corpusRoot(t, map[string]map[string][]byte{
		"lgt": {"steady.zip": steady, "moved.zip": moved, "new.zip": []byte("PK\x03\x04 never seen")},
	})
	survey := surveyCorpus(root, []string{"lgt"})
	planned := map[string][]stage{"lgt": stagesOf("lgt")}
	now := buildOf("a-commit", false)
	before := buildOf("a-commit", false)
	before.Run = "2026-09-01T00:00:00Z"

	rows := []archiveRecord{
		answered("lgt", "steady.zip", digestOf(steady), before.Run),
		// The same name over different bytes: what was measured then is not
		// this file.
		answered("lgt", "moved.zip", digestOf([]byte("something else")), before.Run),
	}
	plan := planCache(true, cacheSource{"2026-09-01.ndjson", before, rows}, survey, planned, now)

	if _, ok := plan.carried[cacheKey{"lgt", "steady.zip"}]; !ok {
		t.Error("an unchanged archive under an unchanged build was measured again")
	}
	if _, ok := plan.carried[cacheKey{"lgt", "moved.zip"}]; ok {
		t.Error("an archive whose bytes changed was carried forward")
	}
	if _, ok := plan.carried[cacheKey{"lgt", "new.zip"}]; ok {
		t.Error("an archive no earlier run saw was carried forward")
	}

	selection := plan.selection("lgt", survey["lgt"])
	if got := strings.Join(selection, ","); got != "moved.zip,new.zip" {
		t.Errorf("the stage was asked to run %q, want the two that changed", got)
	}
}

// A record carries the file name and a selection has to be written in the name
// `go test` rewrites it to. A key that mixed the two would quietly refuse to
// carry every archive with a space in its name — and a corpus of downloads is
// full of them, so the cache would look like it worked while doing the most
// expensive part of its job over again.
func TestAnArchiveWithASpaceInItsNameIsCarriedForward(t *testing.T) {
	data := []byte("PK\x03\x04 unchanged")
	root := corpusRoot(t, map[string]map[string][]byte{"lgt": {"a title with spaces.zip": data}})
	survey := surveyCorpus(root, []string{"lgt"})
	planned := map[string][]stage{"lgt": stagesOf("lgt")}
	then := buildOf("a-commit", false)
	then.Run = "2026-09-01T00:00:00Z"
	// The record names the file on disk, spaces and all.
	rows := []archiveRecord{answered("lgt", "a title with spaces.zip", digestOf(data), then.Run)}

	plan := planCache(true, cacheSource{"2026-09-01.ndjson", then, rows}, survey, planned, buildOf("a-commit", false))
	if _, ok := plan.carried[cacheKey{"lgt", "a_title_with_spaces.zip"}]; !ok {
		t.Fatalf("an archive with a space in its name was measured again: %+v", plan)
	}
	if selection := plan.selection("lgt", survey["lgt"]); len(selection) != 0 {
		t.Errorf("the stage was still asked to run %v", selection)
	}
}

// A commit only names a build when the checkout was that commit. A modified
// tree is exactly the state a person is in while changing the code being
// measured, which is when a carried answer would be believed and wrong.
func TestNothingIsCarriedForwardWhenTheBuildCannotBeIdentified(t *testing.T) {
	data := []byte("PK\x03\x04 unchanged")
	root := corpusRoot(t, map[string]map[string][]byte{"lgt": {"one.zip": data}})
	survey := surveyCorpus(root, []string{"lgt"})
	planned := map[string][]stage{"lgt": stagesOf("lgt")}
	clean := buildOf("a-commit", false)
	clean.Run = "2026-09-01T00:00:00Z"
	rows := []archiveRecord{answered("lgt", "one.zip", digestOf(data), clean.Run)}

	for _, refusal := range []struct {
		what string
		now  runRecord
		then runRecord
	}{
		{"this run cannot be identified", buildOf("a-commit", true), clean},
		{"the earlier run could not be identified", buildOf("a-commit", false), buildOf("a-commit", true)},
		{"the code changed", buildOf("another-commit", false), clean},
		{"there is no revision at all", buildOf("", true), clean},
	} {
		plan := planCache(true, cacheSource{"2026-09-01.ndjson", refusal.then, rows}, survey, planned, refusal.now)
		if len(plan.carried) != 0 {
			t.Errorf("%s and a row was still carried forward", refusal.what)
		}
		if plan.why == "" {
			t.Errorf("%s and the report is told nothing about why", refusal.what)
		}
	}

	off := planCache(false, cacheSource{"2026-09-01.ndjson", clean, rows}, survey, planned, buildOf("a-commit", false))
	if len(off.carried) != 0 {
		t.Error("-cache=false still carried a row forward")
	}
}

// A ladder this run would climb further than the earlier one did is not the
// same measurement, even where the rows line up: a grade is a position on the
// ladder that produced it.
func TestARowIsNotCarriedWhenItDoesNotAnswerEveryStageThisRunAsks(t *testing.T) {
	data := []byte("PK\x03\x04 unchanged")
	root := corpusRoot(t, map[string]map[string][]byte{"lgt": {"one.zip": data}})
	survey := surveyCorpus(root, []string{"lgt"})
	planned := map[string][]stage{"lgt": stagesOf("lgt")}
	then := buildOf("a-commit", false)
	then.Run = "2026-09-01T00:00:00Z"
	now := buildOf("a-commit", false)

	short := answered("lgt", "one.zip", digestOf(data), then.Run)
	// An earlier run that only knew the bottom rung.
	short.Stages = map[string]stageOutcome{"boot": {Outcome: outcomePassed}}
	short.Ladder = 1
	if plan := planCache(true, cacheSource{"2026-09-01.ndjson", then, []archiveRecord{short}}, survey, planned, now); len(plan.carried) != 0 {
		t.Error("a row from a shorter ladder was carried forward onto a longer one")
	}

	// The rungs are there but one of this run's stages was never asked.
	missing := answered("lgt", "one.zip", digestOf(data), then.Run)
	delete(missing.Stages, "interactive")
	if plan := planCache(true, cacheSource{"2026-09-01.ndjson", then, []archiveRecord{missing}}, survey, planned, now); len(plan.carried) != 0 {
		t.Error("a row missing one of this run's stages was carried forward")
	}
}

// The point of the cache is that nothing about a carried line was measured
// again, so the line has to say so — and it has to keep saying which run did
// measure it, however many runs copy it afterwards.
func TestACarriedLineSaysItWasCarriedAndNamesTheRunThatMeasuredIt(t *testing.T) {
	data := []byte("PK\x03\x04 unchanged")
	root := corpusRoot(t, map[string]map[string][]byte{"lgt": {"one.zip": data}})
	survey := surveyCorpus(root, []string{"lgt"})
	planned := map[string][]stage{"lgt": stagesOf("lgt")}
	then := buildOf("a-commit", false)
	then.Run = "2026-09-01T00:00:00Z"
	now := buildOf("a-commit", false)
	rows := []archiveRecord{answered("lgt", "one.zip", digestOf(data), then.Run)}

	plan := planCache(true, cacheSource{"2026-09-01.ndjson", then, rows}, survey, planned, now)
	header := runHeader(root, time.Date(2026, 9, 6, 0, 0, 0, 0, time.UTC))
	run, archives := buildRecords(header, []result{{stage: stageNamed(t, "lgt", "boot"), cached: 1, notRun: true}},
		[]string{"lgt"}, survey, plan)
	if len(archives) != 1 {
		t.Fatalf("%d records, want one", len(archives))
	}
	carried := archives[0]
	if !carried.Cached {
		t.Error("a carried line does not say it was carried")
	}
	if carried.Run != "2026-09-06T00:00:00Z" {
		t.Errorf("the line is filed under run %q, want this run's", carried.Run)
	}
	if carried.MeasuredRun != "2026-09-01T00:00:00Z" {
		t.Errorf("measured_run = %q, want the run that measured it", carried.MeasuredRun)
	}
	if carried.Grade != "interactive" {
		t.Errorf("grade = %q, want the earlier run's answer untouched", carried.Grade)
	}
	if run.CachedFrom != "2026-09-01.ndjson" {
		t.Errorf("the run line does not name where its carried rows came from: %q", run.CachedFrom)
	}

	// Copied a second time, it still names the run that asked rather than the
	// one that copied it.
	again := carriedRecord("2026-09-07T00:00:00Z", carried)
	if again.MeasuredRun != "2026-09-01T00:00:00Z" {
		t.Errorf("a line carried twice claims it was measured in %q", again.MeasuredRun)
	}
}

// A selection reaches the probe as `go test`'s own subtest filter, so the
// names in it have to survive being read as a pattern. A corpus is a directory
// of downloads and their names carry brackets, dots and plus signs.
func TestTheSelectionReachesGoTestAsAnEscapedSubtestPattern(t *testing.T) {
	if got := runPattern("TestSomething", nil); got != "^TestSomething$" {
		t.Errorf("with nothing selected the pattern is %q, want the test on its own", got)
	}
	got := runPattern("TestSomething", []string{"a+title_(2).zip", "plain.zip"})
	want := `^TestSomething$/^(a\+title_\(2\)\.zip|plain\.zip)$`
	if got != want {
		t.Errorf("pattern = %q, want %q", got, want)
	}
}

// A filter that matched no subtest leaves the parent test passing with nothing
// under it. Reading that as "the whole corpus passed" would invent a row out of
// the absence of one.
func TestAFilteredStageWithNoRowsDoesNotInventOne(t *testing.T) {
	stream := `{"Action":"run","Test":"TestSomething"}` + "\n" +
		`{"Action":"pass","Test":"TestSomething"}` + "\n"
	probe := stage{platform: "lgt", name: "boot", test: "TestSomething"}
	if outcome := collect(strings.NewReader(stream), probe, true); len(outcome.passed) != 0 {
		t.Errorf("a filtered stage invented %v", outcome.passed)
	}
	if outcome := collect(strings.NewReader(stream), probe, false); len(outcome.passed) != 1 {
		t.Errorf("an unfiltered whole-corpus probe lost its only row: %+v", outcome)
	}
}

// Helpers.

func stagesOf(platform string) []stage {
	var wanted []stage
	for _, current := range stages {
		if current.platform == platform {
			wanted = append(wanted, current)
		}
	}
	return wanted
}

// buildOf is a run identified by one commit. An unidentifiable build — the
// state a modified checkout is in when git cannot describe what differs — is
// the empty identity.
func buildOf(commit string, unidentified bool) runRecord {
	record := runRecord{
		Schema: recordSchema, Kind: runKind,
		GOOS: runtime.GOOS, GOARCH: runtime.GOARCH, Go: runtime.Version(),
		Revision: commit, Modified: unidentified, Build: commit,
	}
	if unidentified {
		record.Build = ""
	}
	return record
}

func digestOf(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// answered is a row from an earlier run that climbed the whole ladder.
func answered(platform, archive, digest, run string) archiveRecord {
	record := archiveRecord{
		Schema: recordSchema, Kind: archiveKind, Run: run, MeasuredRun: run,
		Platform: platform, Archive: archive, SHA256: digest,
		Stages: map[string]stageOutcome{},
	}
	rungs := 0
	for _, current := range stagesOf(platform) {
		record.Stages[current.name] = stageOutcome{Outcome: outcomePassed}
		if current.rung > rungs {
			record.Grade, rungs = current.name, current.rung
		}
	}
	record.Rung, record.Ladder = rungs, rungs
	return record
}
