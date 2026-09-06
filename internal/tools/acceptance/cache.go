package main

import (
	"fmt"
	"sort"
	"strings"
)

// Not measuring again what has not changed.
//
// A sweep of three hundred archives is hours, and most of a run is spent
// re-answering questions whose inputs did not move: the same bytes, driven by
// the same build, produce the same answer. So a run may carry an earlier run's
// row forward instead of measuring it again, and the corpus can be swept often
// enough to be worth sweeping.
//
// Two rules keep that from becoming a lie.
//
// **The identity has to cover everything that decides the answer.** The
// archive's SHA-256 is the input; the build identity is the code that reads
// it. That identity is the commit when the checkout is clean and the commit
// plus a digest of everything differing from it when it is not — see `build`
// for why a commit alone will not do — and a run whose operating system,
// architecture or Go version differs is not the same measurement either. The
// rule is deliberately blunt in one direction: it would rather re-measure a
// corpus needlessly than report an answer the current code never gave.
//
// **A carried row has to say that it was carried.** A record that reads like a
// measurement but is a copy of one from a week ago turns a report into an
// assertion nobody made. Every carried line says `cached` and names the run it
// was actually measured in, which chains: a row carried twice still names the
// run that measured it rather than the one that copied it.
//
// An archive is carried as a whole rather than a stage at a time. All of a
// platform's stages are run from one `go test` invocation per stage, so a
// half-cached archive would cost the same run and leave two kinds of row in
// one line; requiring the earlier run to have answered every stage this run
// intends to run keeps a line one thing or the other.

// cacheKey is one archive in one corpus, under the name `go test` gives its
// subtest — which is what the selection passed back to the probe has to match.
// The corpus rather than the platform, because a sweep may ask one platform's
// ladder of several group directories and two of them can hold a file of the
// same name.
type cacheKey struct{ corpus, archive string }

// cachePlan is what this run may take from an earlier one.
type cachePlan struct {
	// from is the record file the carried rows were read out of, empty when
	// nothing is carried.
	from string
	// why says what the report should print: either what is being carried or
	// the reason nothing can be.
	why string
	// carried is the rows to reuse, already rewritten for this run.
	carried map[cacheKey]archiveRecord
}

// planCache decides what this run may carry forward from the run before it.
func planCache(enabled bool, source cacheSource,
	survey map[string][]corpusEntry, planned map[string][]stage, now runRecord) cachePlan {
	previousFile, previous, rows := source.file, source.run, source.rows
	switch {
	case !enabled:
		return cachePlan{why: "Nothing was carried forward: `-cache=false`."}
	case previousFile == "":
		return cachePlan{why: "Nothing was carried forward: there is no earlier run to read."}
	case now.Build == "":
		return cachePlan{why: "Nothing was carried forward: this run's code cannot be identified, so nothing can be said to have answered for it already."}
	case previous.Build == "":
		return cachePlan{why: fmt.Sprintf("Nothing was carried forward: %s was written by a build that cannot be identified.", previousFile)}
	case previous.Build != now.Build:
		return cachePlan{why: fmt.Sprintf("Nothing was carried forward: %s ran different code.", previousFile)}
	case previous.GOOS != now.GOOS || previous.GOARCH != now.GOARCH || previous.Go != now.Go:
		return cachePlan{why: fmt.Sprintf("Nothing was carried forward: %s ran on %s/%s with %s.",
			previousFile, previous.GOOS, previous.GOARCH, previous.Go)}
	}

	before := map[cacheKey]archiveRecord{}
	for _, record := range rows {
		// Under the name `go test` gives the subtest, not the name on disk: a
		// record carries the file name and a selection has to be written in
		// the rewritten one, so a key that mixed the two would quietly refuse
		// to carry every archive with a space in its name.
		before[cacheKey{record.Corpus, subtestName(record.Archive)}] = record
	}
	plan := cachePlan{from: previousFile, carried: map[cacheKey]archiveRecord{}}
	for corpusName, entries := range survey {
		want := planned[corpusName]
		if len(want) == 0 {
			continue
		}
		rungs := 0
		for _, current := range want {
			if current.rung > 0 {
				rungs++
			}
		}
		for _, entry := range entries {
			at := cacheKey{corpusName, entry.subtest}
			was, ok := before[at]
			if !ok || was.SHA256 == "" || was.SHA256 != entry.facts.sha256 {
				continue
			}
			// A ladder of a different length is a different measurement, even
			// where the rows line up: a grade is a position on the ladder that
			// produced it.
			if was.Ladder != rungs {
				continue
			}
			// Every stage this run intends to ask has to have been answered
			// then, or the carried line would be missing a rung this run would
			// otherwise have filled in.
			answered := true
			for _, current := range want {
				if _, told := was.Stages[current.name]; !told {
					answered = false
					break
				}
			}
			if !answered {
				continue
			}
			plan.carried[at] = was
		}
	}
	if len(plan.carried) == 0 {
		return cachePlan{why: fmt.Sprintf("Nothing was carried forward: nothing in %s matched this corpus.", previousFile)}
	}
	return plan
}

// selection is the subtest names a stage has to actually run, and whether that
// is all of them. A nil selection means "everything", which is what the probe
// gets when nothing on its platform was carried; an empty non-nil one means
// the stage has nothing left to measure and is not run at all.
func (plan cachePlan) selection(corpusName string, entries []corpusEntry) []string {
	carried := plan.carriedCount(corpusName, entries)
	if carried == 0 {
		return nil
	}
	names := make([]string, 0, len(entries)-carried)
	for _, entry := range entries {
		if _, ok := plan.carried[cacheKey{corpusName, entry.subtest}]; !ok {
			names = append(names, entry.subtest)
		}
	}
	sort.Strings(names)
	return names
}

// carriedCount is how many of a corpus's archives this run will not measure.
func (plan cachePlan) carriedCount(corpusName string, entries []corpusEntry) int {
	carried := 0
	for _, entry := range entries {
		if _, ok := plan.carried[cacheKey{corpusName, entry.subtest}]; ok {
			carried++
		}
	}
	return carried
}

// describe is the paragraph the report carries about the cache. A run that
// says only "nothing changed since last time" while half its rows were copied
// from last time is making a claim it did not measure, so the count and the
// file it came from are printed beside the answers.
func (plan cachePlan) describe(results []result) string {
	if plan.from == "" {
		if plan.why == "" {
			return ""
		}
		return plan.why + "\n\n"
	}
	// The archives, not the stage rows: one archive carried past a ladder of
	// three stages is one line in the file, and counting it three times would
	// overstate what was reused.
	carried := len(plan.carried)
	if carried == 0 {
		return "Nothing was carried forward.\n\n"
	}
	stages := 0
	for _, outcome := range results {
		if outcome.cached > 0 && outcome.ranNothing() {
			stages++
		}
	}
	note := fmt.Sprintf("**%d of the rows below were not measured in this run.** "+
		"Their archives are the same bytes this build already answered for in `%s`, so that run's answers were carried forward; "+
		"every one of them is marked `cached` in the records and names the run it was measured in.",
		carried, plan.from)
	if stages > 0 {
		note += fmt.Sprintf(" %d stage(s) had nothing left to measure and were not run at all.", stages)
	}
	return note + "\n\n"
}

// runPattern is what `-run` is given so a probe answers for some of its corpus
// rather than all of it. There is no flag on the probes for this and there
// should not be: a probe's job is to ask its question of an archive, and which
// archives are worth asking about this time is the sweep's business. `go test`
// already knows how to select subtests, and a subtest here is an archive.
func runPattern(test string, selection []string) string {
	pattern := "^" + test + "$"
	if selection == nil {
		return pattern
	}
	quoted := make([]string, 0, len(selection))
	for _, name := range selection {
		quoted = append(quoted, quoteRun(name))
	}
	return pattern + "/^(" + strings.Join(quoted, "|") + ")$"
}

// quoteRun escapes a subtest name for the pattern above. `regexp.QuoteMeta`
// would do it, except that `go test` splits the pattern on `/` before it
// compiles the parts, so a backslash-escaped separator would still split. A
// file name cannot carry one, and a name that somehow does is refused rather
// than silently selecting something else.
func quoteRun(name string) string {
	escaped := &strings.Builder{}
	for _, character := range name {
		if strings.ContainsRune(`\.+*?()|[]{}^$/`, character) {
			escaped.WriteByte('\\')
		}
		escaped.WriteRune(character)
	}
	return escaped.String()
}
