package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"runtime/debug"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/movingwoo/wfeature/internal/platform/detect"
)

// One archive, one line.
//
// The dated report beside this is written for a person: it groups by stage and
// it carries the reason a title did not pass in the words the probe printed.
// What it cannot do is answer "what changed since last time" — two of them
// differ in their prose everywhere the corpus is unchanged, and the archive
// that dropped a rung is a line somewhere in the middle of that. So the same
// run also writes a record per archive, one JSON object per line, with the
// fields a machine needs to line two runs up against each other: what the file
// was, how far it got, and where it stopped.
//
// The schema version is the first field of every line. A reader that meets a
// number it does not know is reading a file written by a newer tool, and it
// should say so rather than quietly ignoring fields it cannot see.
// Version 2 added the fields that say a line was carried forward from an
// earlier run rather than measured in this one, and the flag that says whether
// the checkout behind a run was modified. Both change what a line means, so a
// tool that cannot see them must refuse the file rather than read a copy as a
// measurement.
const recordSchema = 2

// A record file's lines are one run record followed by one archive record per
// file in the corpus — including the files no probe ever picked up. A download
// that did not finish is invisible in a pass count, and a file none of the
// loaders claimed is invisible in a failure count; both are lines here, with
// the reason nothing claimed them.
const (
	runKind     = "run"
	archiveKind = "archive"
)

// runRecord is the first line of a record file: what ran, on what, and what
// was in front of it.
type runRecord struct {
	Schema   int    `json:"schema"`
	Kind     string `json:"kind"`
	Run      string `json:"run"`
	GOOS     string `json:"goos"`
	GOARCH   string `json:"goarch"`
	Go       string `json:"go"`
	Revision string `json:"revision,omitempty"`
	// Modified says the checkout had uncommitted changes, which is why the
	// revision on its own does not identify what ran.
	Modified bool `json:"modified,omitempty"`
	// Build is what two runs are compared by when one is deciding whether the
	// other's answers still apply: the revision when the checkout was clean,
	// and the revision with a digest of what differs from it when it was not.
	// Empty means this run cannot be identified at all, and nothing may be
	// carried into or out of it.
	Build string `json:"build,omitempty"`
	// CachedFrom names the record file whose answers this run carried
	// forward, when it carried any.
	CachedFrom string         `json:"cached_from,omitempty"`
	Stages     []stageRecord  `json:"stages"`
	Corpus     []corpusRecord `json:"corpus"`
}

// stageRecord is one probe's run, which is what says whether a missing archive
// record means "it passed nothing" or "the stage never ran at all".
type stageRecord struct {
	Platform string `json:"platform"`
	Stage    string `json:"stage"`
	Rung     int    `json:"rung,omitempty"`
	Ran      int    `json:"ran"`
	Passed   int    `json:"passed"`
	Skipped  int    `json:"skipped"`
	Failed   int    `json:"failed"`
	// Cached is how many of this stage's archives were not measured in this
	// run at all. Ran counts only what was.
	Cached  int     `json:"cached"`
	Seconds float64 `json:"seconds"`
	Error   string  `json:"error,omitempty"`
	// NotRun says the stage was never started, because every archive on its
	// platform was carried forward. It is not the same answer as Error.
	NotRun bool `json:"not_run,omitempty"`
}

type corpusRecord struct {
	Platform  string `json:"platform"`
	Directory string `json:"directory"`
	Files     int    `json:"files"`
}

// archiveRecord is one file in the corpus and everything this run learned
// about it.
type archiveRecord struct {
	Schema   int    `json:"schema"`
	Kind     string `json:"kind"`
	Run      string `json:"run"`
	Platform string `json:"platform"`
	Archive  string `json:"archive"`
	// The digest is what makes two runs comparable across a corpus that
	// changes: a file re-downloaded under the same name is a different file,
	// and a rung it lost is not a regression.
	SHA256 string `json:"sha256,omitempty"`
	Size   int64  `json:"size,omitempty"`
	// What detection answered, and — when nothing claimed it — why. A corpus
	// sweep's most misleading number is the count of files that did not run,
	// because only one of the reasons behind it is work this project can do.
	Detected     string `json:"detected,omitempty"`
	DetectReason string `json:"detect_reason,omitempty"`
	DetectError  string `json:"detect_error,omitempty"`
	// Grade is the highest rung of its platform's ladder this file reached,
	// and the rung number beside it is what a comparison sorts on.
	Grade string `json:"grade"`
	Rung  int    `json:"rung"`
	// Ladder is how many rungs the platform had in this run, so a grade read
	// later is not mistaken for a distance from a ladder of another length.
	Ladder int `json:"ladder"`
	// Stopped is the lowest rung the file did not pass, with the outcome and
	// the reason the probe printed.
	Stopped        string `json:"stopped,omitempty"`
	StoppedOutcome string `json:"stopped_outcome,omitempty"`
	Why            string `json:"why,omitempty"`
	// WhyClass is the reason with the parts that differ between two archives
	// of the same failure taken out of it, which is what a grouping counts.
	WhyClass string                  `json:"why_class,omitempty"`
	Stages   map[string]stageOutcome `json:"stages,omitempty"`
	// Cached says this line was not measured in this run: the file's bytes and
	// the build were both what an earlier run already answered for, so that
	// run's answer was carried forward. MeasuredRun names the run that
	// actually asked, which survives being carried more than once — a line
	// copied twice still names the run that measured it.
	//
	// A record that reads like a measurement and is a copy of one turns a
	// report into a claim nobody made, so these two fields are the price of
	// the cache being allowed to exist at all.
	Cached      bool   `json:"cached,omitempty"`
	MeasuredRun string `json:"measured_run,omitempty"`
}

type stageOutcome struct {
	Outcome string `json:"outcome"`
	Why     string `json:"why,omitempty"`
}

const (
	outcomePassed  = "pass"
	outcomeSkipped = "skip"
	outcomeFailed  = "fail"
)

// The three answers that are not a rung.
//
// gradeNone is a file that reached no rung and was refused at one. gradeUnrun
// is one no ladder stage answered for at all — a file in the directory that no
// probe picked up, or a stage that could not run. gradeSkipped is one every
// rung knowingly declined: a package of a shape this ladder does not drive is
// not a failure, and a corpus sweep that counts it as one is reporting a
// number about itself.
//
// All three are different answers, and a comparison must not read any of them
// as a fall from another.
const (
	gradeNone    = "none"
	gradeUnrun   = "unrun"
	gradeSkipped = "skipped"
)

// corpusEntry is one file in a corpus directory, read once. The same bytes
// answer three questions — what was in front of the run, what the loaders make
// of the file, and whether an earlier run's answer still applies to it — and
// reading them once is what keeps those three from disagreeing.
type corpusEntry struct {
	name    string // the file name on disk
	subtest string // the name `go test` gives the subtest for it
	path    string
	facts   fileFacts
}

// fileFacts is what a corpus file says about itself before any probe runs.
type fileFacts struct {
	sha256       string
	size         int64
	detected     string
	detectReason string
	detectError  string
}

// surveyCorpus reads every file in every corpus directory this run covers.
func surveyCorpus(root string, platforms []string) map[string][]corpusEntry {
	survey := map[string][]corpusEntry{}
	for _, platform := range platforms {
		directory := filepath.Join(root, corpus[platform])
		var entries []corpusEntry
		for _, name := range corpusFiles(directory) {
			entry := corpusEntry{
				name: name,
				// A subtest is named after the archive, but `go test` rewrites
				// that name before it prints it or matches it: a space becomes
				// an underscore. Rows come back under the rewritten name and
				// the file is on disk under the original, so every join and
				// every selection is made on the rewritten one.
				subtest: subtestName(name),
				path:    filepath.Join(directory, name),
			}
			entry.facts = readFacts(entry.path)
			entries = append(entries, entry)
		}
		survey[platform] = entries
	}
	return survey
}

func readFacts(path string) fileFacts {
	data, err := os.ReadFile(path)
	if err != nil {
		return fileFacts{detectError: err.Error()}
	}
	digest := sha256.Sum256(data)
	facts := fileFacts{sha256: hex.EncodeToString(digest[:]), size: int64(len(data))}
	detected, reason, err := detect.Classify(data)
	facts.detected = string(detected)
	facts.detectReason = string(reason)
	if err != nil {
		facts.detectError = err.Error()
	}
	return facts
}

// runHeader is what a run says about itself before it has measured anything,
// which is also what the cache compares two runs by.
func runHeader(root string, started time.Time) runRecord {
	commit, modified, identity := build(root)
	return runRecord{
		Schema:   recordSchema,
		Kind:     runKind,
		Run:      started.UTC().Format(time.RFC3339),
		GOOS:     runtime.GOOS,
		GOARCH:   runtime.GOARCH,
		Go:       runtime.Version(),
		Revision: commit,
		Modified: modified,
		Build:    identity,
	}
}

// buildRecords turns a run into its lines: the run itself, then every file in
// every corpus directory that was in front of it.
func buildRecords(run runRecord, results []result, platforms []string,
	survey map[string][]corpusEntry, plan cachePlan) (runRecord, []archiveRecord) {
	for _, outcome := range results {
		run.Stages = append(run.Stages, stageRecord{
			Platform: outcome.stage.platform,
			Stage:    outcome.stage.name,
			Rung:     outcome.stage.rung,
			Ran:      len(outcome.passed) + len(outcome.skipped) + len(outcome.failed),
			Passed:   len(outcome.passed),
			Skipped:  len(outcome.skipped),
			Failed:   len(outcome.failed),
			Cached:   outcome.cached,
			Seconds:  outcome.elapsed.Seconds(),
			Error:    outcome.err,
			NotRun:   outcome.notRun,
		})
	}

	// Every stage's rows, keyed by the platform and archive they belong to.
	type key struct{ platform, archive string }
	rows := map[key]map[string]stageOutcome{}
	add := func(platform, archive, stage, outcome, why string) {
		// The row a probe writes for a whole corpus in one test is not an
		// archive and has no file behind it; the stage counts in the run
		// record are where it is visible.
		if archive == wholeCorpusRow {
			return
		}
		at := key{platform, archive}
		if rows[at] == nil {
			rows[at] = map[string]stageOutcome{}
		}
		rows[at][stage] = stageOutcome{Outcome: outcome, Why: why}
	}
	for _, outcome := range results {
		for _, archive := range outcome.passed {
			add(outcome.stage.platform, archive, outcome.stage.name, outcomePassed, "")
		}
		for _, entry := range outcome.skipped {
			add(outcome.stage.platform, entry.archive, outcome.stage.name, outcomeSkipped, entry.why)
		}
		for _, entry := range outcome.failed {
			add(outcome.stage.platform, entry.archive, outcome.stage.name, outcomeFailed, entry.why)
		}
	}

	// The union of what the probes answered for and what is in the directory.
	// The second is the larger set and the difference between them is a
	// finding of its own.
	seen := map[key]bool{}
	var archives []archiveRecord
	carried := 0
	for _, platform := range platforms {
		entries := survey[platform]
		run.Corpus = append(run.Corpus, corpusRecord{
			Platform:  platform,
			Directory: corpus[platform],
			Files:     len(entries),
		})
		for _, entry := range entries {
			at := key{platform, entry.subtest}
			seen[at] = true
			if was, ok := plan.carried[cacheKey(at)]; ok {
				archives = append(archives, carriedRecord(run.Run, was))
				carried++
				continue
			}
			archives = append(archives, archiveRecordFor(run.Run, platform, entry.name, entry.facts, rows[at], results))
		}
	}
	if carried > 0 {
		run.CachedFrom = plan.from
	}
	// A row for an archive that is no longer in the directory still belongs in
	// the file: a probe answered for it, and dropping it would make the run
	// look as though it never asked.
	for at, stages := range rows {
		if seen[at] {
			continue
		}
		// The name here is the rewritten one, which is the only name this run
		// ever saw for a file that is no longer in the directory.
		archives = append(archives, archiveRecordFor(run.Run, at.platform, at.archive, fileFacts{}, stages, results))
	}
	sort.Slice(archives, func(one, two int) bool {
		if archives[one].Platform != archives[two].Platform {
			return archives[one].Platform < archives[two].Platform
		}
		return archives[one].Archive < archives[two].Archive
	})
	return run, archives
}

// archiveRecordFor fills in one file's line: what the file is, what detection
// said about it, and how far up the ladder it got.
func archiveRecordFor(run, platform, name string, facts fileFacts, stages map[string]stageOutcome, results []result) archiveRecord {
	record := archiveRecord{
		Schema:       recordSchema,
		Kind:         archiveKind,
		Run:          run,
		Platform:     platform,
		Archive:      name,
		SHA256:       facts.sha256,
		Size:         facts.size,
		Detected:     facts.detected,
		DetectReason: facts.detectReason,
		DetectError:  facts.detectError,
		Stages:       stages,
		// A line this run measured names itself as the run that measured it,
		// so a reader never has to know which fields were added when.
		MeasuredRun: run,
	}
	record.Grade, record.Rung, record.Ladder, record.Stopped, record.StoppedOutcome, record.Why = grade(platform, stages, results)
	record.WhyClass = classify(record.Why)
	return record
}

// carriedRecord is an earlier run's answer written into this run's file. Every
// measured field is the earlier one's, untouched: the point of carrying a line
// forward is that nothing about it was measured again, and re-deriving a grade
// here would be this run making a claim out of a copy.
//
// What changes is the provenance. Run is this run, because that is the file
// the line is in; MeasuredRun is the run that asked, which is carried rather
// than overwritten so a line copied a second time still names the run that
// measured it rather than the one that copied it.
func carriedRecord(run string, was archiveRecord) archiveRecord {
	measured := was.MeasuredRun
	if measured == "" {
		measured = was.Run
	}
	was.Schema = recordSchema
	was.Run = run
	was.Cached = true
	was.MeasuredRun = measured
	return was
}

// grade reads one archive's stage outcomes as a position on its platform's
// ladder: the highest rung it passed, and the lowest one it did not.
//
// The rungs are not read as a chain that stops at the first refusal, because
// they are not run as one — each is its own probe from a fresh start, and an
// archive that fails to construct its main class can still be answered for by
// the frame probe. The highest rung that answered "pass" is what it reached.
func grade(platform string, stages map[string]stageOutcome, results []result) (name string, rung, ladder int, stopped, stoppedOutcome, why string) {
	ordered := ladderStages(platform, results)
	ladder = len(ordered)
	name = gradeUnrun
	answered, refused := false, false
	for _, current := range ordered {
		outcome, ok := stages[current.name]
		if !ok {
			continue
		}
		answered = true
		if outcome.Outcome == outcomePassed {
			if current.rung > rung {
				name, rung = current.name, current.rung
			}
			continue
		}
		if outcome.Outcome == outcomeFailed {
			refused = true
		}
		if stopped == "" {
			stopped, stoppedOutcome, why = current.name, outcome.Outcome, outcome.Why
		}
	}
	switch {
	case !answered || rung > 0:
	case refused:
		name = gradeNone
	default:
		name = gradeSkipped
	}
	return name, rung, ladder, stopped, stoppedOutcome, why
}

// ladderStages are the rungs of one platform's ladder in this run, in order.
// They come from the run rather than from the table so a run of one platform
// does not report the ladder of another, and a stage that is not a rung — a
// check that asks something else of the same archive — is left out.
func ladderStages(platform string, results []result) []stage {
	var ordered []stage
	for _, outcome := range results {
		if outcome.stage.platform != platform || outcome.stage.rung == 0 {
			continue
		}
		ordered = append(ordered, outcome.stage)
	}
	sort.Slice(ordered, func(one, two int) bool { return ordered[one].rung < ordered[two].rung })
	return ordered
}

// The parts of a reason that differ between two archives failing the same way:
// a count of ticks, an address, a length, a quantity of anything. Taking them
// out is what turns a list of individual failures into a count per cause, which
// is how the largest cause gets found without reading every line.
var (
	hexNumbers   = regexp.MustCompile(`0x[0-9a-fA-F]+`)
	plainNumbers = regexp.MustCompile(`\b\d+\b`)
	whitespace   = regexp.MustCompile(`\s+`)
)

// classify normalises a reason into the cause it is an instance of.
func classify(why string) string {
	if why == "" {
		return ""
	}
	normalized := hexNumbers.ReplaceAllString(why, "0xN")
	normalized = plainNumbers.ReplaceAllString(normalized, "N")
	normalized = whitespace.ReplaceAllString(normalized, " ")
	normalized = strings.TrimSpace(normalized)
	// A reason that carries a dump behind it — a trace, a table of counters —
	// is one cause with a tail, and the tail is what makes it unique.
	if cut := strings.Index(normalized, " counts:"); cut > 0 {
		normalized = normalized[:cut]
	}
	const longest = 160
	if len(normalized) > longest {
		normalized = strings.TrimSpace(normalized[:longest]) + "…"
	}
	return normalized
}

// writeRecords writes the run and its archives as one JSON object per line.
func writeRecords(path string, run runRecord, archives []archiveRecord) error {
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()
	encoder := json.NewEncoder(file)
	if err := encoder.Encode(run); err != nil {
		return err
	}
	for _, record := range archives {
		if err := encoder.Encode(record); err != nil {
			return err
		}
	}
	return file.Close()
}

// readRecords reads a record file back. A file written by a newer tool is
// refused rather than half-read: a comparison that silently drops the fields it
// does not know reports differences that are its own.
func readRecords(path string) (runRecord, []archiveRecord, error) {
	file, err := os.Open(path)
	if err != nil {
		return runRecord{}, nil, err
	}
	defer file.Close()
	decoder := json.NewDecoder(file)
	var run runRecord
	var archives []archiveRecord
	for {
		var line json.RawMessage
		if err := decoder.Decode(&line); err != nil {
			break
		}
		var head struct {
			Schema int    `json:"schema"`
			Kind   string `json:"kind"`
		}
		if err := json.Unmarshal(line, &head); err != nil {
			return runRecord{}, nil, fmt.Errorf("read %s: %w", filepath.Base(path), err)
		}
		if head.Schema != recordSchema {
			return runRecord{}, nil, fmt.Errorf("read %s: schema %d, this tool writes %d", filepath.Base(path), head.Schema, recordSchema)
		}
		switch head.Kind {
		case runKind:
			if err := json.Unmarshal(line, &run); err != nil {
				return runRecord{}, nil, fmt.Errorf("read %s: %w", filepath.Base(path), err)
			}
		case archiveKind:
			var record archiveRecord
			if err := json.Unmarshal(line, &record); err != nil {
				return runRecord{}, nil, fmt.Errorf("read %s: %w", filepath.Base(path), err)
			}
			archives = append(archives, record)
		}
	}
	return run, archives, nil
}

// corpusFiles lists what is in a corpus directory, archive or not. A file that
// is not an archive is a download that did not finish or a container this
// project does not read, and neither shows up as a failure because no probe
// ever picks it up; a record for it is where it becomes visible.
func corpusFiles(directory string) []string {
	entries, err := os.ReadDir(directory)
	if err != nil {
		return nil
	}
	var names []string
	for _, entry := range entries {
		// A dot file is the operating system's, not the corpus's.
		if entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		names = append(names, entry.Name())
	}
	sort.Strings(names)
	return names
}

// subtestName is what `go test` calls a subtest named after an archive. It
// rewrites the name it is given: a space becomes an underscore, and anything
// unprintable becomes its escape. Matching a row back to a file means applying
// the same rule rather than hoping the two agree.
func subtestName(name string) string {
	rewritten := &strings.Builder{}
	for _, character := range name {
		switch {
		case unicode.IsSpace(character):
			rewritten.WriteByte('_')
		case !strconv.IsPrint(character):
			quoted := strconv.QuoteRune(character)
			rewritten.WriteString(quoted[1 : len(quoted)-1])
		default:
			rewritten.WriteRune(character)
		}
	}
	return rewritten.String()
}

// build says what code this run is. Two runs over the same corpus are the same
// measurement only when this is the same, and the record has to be able to say
// so.
//
// The checkout is asked rather than the binary. `debug.ReadBuildInfo` carries
// a revision and a modified flag, but only for a binary `go build` stamped,
// and this command is run with `go run` — which stamps nothing, and which
// compiles the probes out of whatever is in the tree at the moment they run.
// So the tree is the honest source, and the build stamp is the fallback for a
// copy of this tool that was built elsewhere.
//
// **A commit is not enough on its own.** It names a build only while the
// working tree is that commit, and a modified tree is precisely the state a
// person is in while changing the thing being measured — which is also exactly
// when a sweep is worth re-running often. So a modified tree is identified by
// the commit plus a digest of everything that differs from it: the diff
// against HEAD, and the content of every untracked file git would not ignore.
//
// That digest is a superset of what decides an answer — editing a document
// invalidates a corpus sweep — and the over-counting is the direction to err
// in. The alternative, ignoring a change because it looked unrelated, is a
// report claiming an answer this code never gave.
func build(root string) (commit string, modified bool, identity string) {
	commit, ok := gitOutput(root, "rev-parse", "HEAD")
	if !ok {
		return stampedBuild()
	}
	difference := sha256.New()
	fmt.Fprintf(difference, "commit %s\n", commit)
	diff, ok := gitBytes(root, "diff", "HEAD")
	if !ok {
		// git answered the first question and not the second, so nothing here
		// can be trusted to describe the tree.
		return commit, true, ""
	}
	fmt.Fprintf(difference, "diff %d\n", len(diff))
	difference.Write(diff)
	untracked, ok := gitOutput(root, "ls-files", "--others", "--exclude-standard")
	if !ok {
		return commit, true, ""
	}
	changed := len(diff) > 0
	for _, name := range strings.Split(untracked, "\n") {
		if name == "" {
			continue
		}
		changed = true
		fmt.Fprintf(difference, "untracked %s ", name)
		// An untracked path that cannot be read — a symbolic link to a
		// directory, a file removed since git listed it — still has to change
		// the identity, because what it would have contributed is unknown.
		data, err := os.ReadFile(filepath.Join(root, name))
		if err != nil {
			fmt.Fprintf(difference, "unreadable %v\n", err)
			continue
		}
		content := sha256.Sum256(data)
		fmt.Fprintf(difference, "%s\n", hex.EncodeToString(content[:]))
	}
	if !changed {
		return commit, false, commit
	}
	return commit, true, commit + "+" + hex.EncodeToString(difference.Sum(nil))[:16]
}

// stampedBuild is what a binary built by `go build` knows about itself, for a
// copy of this tool run outside the checkout it came from. A modified tree
// leaves nothing to identify the build with, because the stamp records that
// the tree differed and not how.
func stampedBuild() (commit string, modified bool, identity string) {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "", false, ""
	}
	for _, setting := range info.Settings {
		switch setting.Key {
		case "vcs.revision":
			commit = setting.Value
		case "vcs.modified":
			modified = setting.Value == "true"
		}
	}
	if modified {
		return commit, true, ""
	}
	return commit, false, commit
}

// gitOutput asks git one question about the checkout, and answers whether it
// could. A tree that is not a checkout at all is not an error here: it means
// this run cannot be identified, which the cache reads as "measure everything".
func gitOutput(root string, arguments ...string) (string, bool) {
	output, ok := gitBytes(root, arguments...)
	return strings.TrimSpace(string(output)), ok
}

func gitBytes(root string, arguments ...string) ([]byte, bool) {
	command := exec.Command("git", append([]string{"-C", root}, arguments...)...)
	output, err := command.Output()
	if err != nil {
		return nil, false
	}
	return output, true
}
