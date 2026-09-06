// Command acceptance runs the local archive probes for all three platforms and
// writes down what they answered, on the day they answered it.
//
// The probes are opt-in `go test` runs behind environment variables, one per
// platform and six for KTF, because a real archive is ignored local data
// rather than a fixture. That made every count in the documentation a sentence
// somebody typed after a run — "currently 43 of 44" — with no date on it and
// no way to tell, a month later, whether it was still true or whether the
// corpus had simply changed underneath it. This runs the lot in one command
// and writes a dated report, so prose can point at a file instead of carrying
// a number it cannot keep.
//
// Beside the report it writes one JSON object per line per archive, which is
// what two runs are compared with. The report is prose and prose does not
// subtract: two of them differ everywhere the corpus is unchanged, and the one
// archive that lost a rung is a line in the middle of that. The records are
// read back by this same command to say what changed and what every failure in
// a run had in common.
//
// **Neither file is committed and neither can be.** Their rows are the archive
// file names, and those are the games' names; they land under `var/`, which is
// ignored for exactly that reason.
//
// Usage:
//
//	go run ./internal/tools/acceptance [-out var/acceptance] [-platform ktf,lgt,skt]
//	go run ./internal/tools/acceptance -cache=false        # measure everything again
//	go run ./internal/tools/acceptance -compare old.ndjson new.ndjson
//	go run ./internal/tools/acceptance -games dir[,dir] [-exclude name[,name]]
//
// `make acceptance` is the first of those. The last points the same ladders at
// a tree this repository does not keep, with the platform of every file
// decided by its bytes rather than by the folder it is filed in; see sweep.go
// for what that costs and why the exclusions are the caller's to name.
package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/movingwoo/wfeature/internal/platform/detect"
)

// A stage is one probe: a test, the variable that lets it run, and how far
// through a title's start it gets. KTF has six because its ladder is where an
// archive stops rather than whether it stops — parsing is not linking, linking
// is not a constructed main class, and a constructed main class is not a frame.
type stage struct {
	platform string
	name     string
	what     string
	pkg      string
	test     string
	env      string
	// rung is where this stage sits on its platform's ladder, counting from
	// one, and zero for a stage that is not a rung at all. A ladder is what
	// lets a run be compared with the one before it as "this archive got
	// further" rather than as a set of independent yes and no answers, and a
	// check that asks something else of the same archive — whether its sounds
	// decode, say — has no place on it.
	rung int
}

var stages = []stage{
	{"ktf", "parse", "the archive opens and its module parses",
		"./internal/platform/ktf", "TestLocalKTFArchivesParse", "WFEATURE_KTF_ACCEPTANCE", 1},
	{"ktf", "initialize", "the module runs its own initialisation",
		"./internal/platform/ktf", "TestLocalKTFArchivesInitialize", "WFEATURE_KTF_EXECUTE_ACCEPTANCE", 2},
	{"ktf", "load", "the main class loads",
		"./internal/platform/ktf", "TestLocalKTFArchivesLoadMainClass", "WFEATURE_KTF_LIFECYCLE_ACCEPTANCE", 3},
	{"ktf", "construct", "the main class is constructed",
		"./internal/platform/ktf", "TestLocalKTFArchivesConstructMainClass", "WFEATURE_KTF_CONSTRUCT_ACCEPTANCE", 4},
	{"ktf", "start", "the title's start method returns",
		"./internal/platform/ktf", "TestLocalKTFArchivesStartMainClass", "WFEATURE_KTF_START_ACCEPTANCE", 5},
	{"ktf", "frame", "the title paints a first frame",
		"./internal/platform/ktf", "TestLocalKTFArchivesRenderFirstFrame", "WFEATURE_KTF_FRAME_ACCEPTANCE", 6},
	{"ktf", "sustained", "the title keeps running past its first frame",
		"./internal/platform/ktf", "TestLocalKTFArchivesSustainAFrame", "WFEATURE_KTF_SUSTAINED_ACCEPTANCE", 7},
	{"ktf", "interactive", "a key changes what the title draws",
		"./internal/platform/ktf", "TestLocalKTFArchivesAnswerAKey", "WFEATURE_KTF_INTERACTIVE_ACCEPTANCE", 8},
	{"lgt", "boot", "the module boots and asks to present a frame",
		"./internal/platform/lgt", "TestLocalLGTArchivesBootAndPaint", "WFEATURE_LGT_ACCEPTANCE", 1},
	{"lgt", "sustained", "the title keeps running past its first frame",
		"./internal/platform/lgt", "TestLocalLGTArchivesSustainAFrame", "WFEATURE_LGT_SUSTAINED_ACCEPTANCE", 2},
	{"lgt", "interactive", "a key changes what the title draws",
		"./internal/platform/lgt", "TestLocalLGTArchivesAnswerAKey", "WFEATURE_LGT_INTERACTIVE_ACCEPTANCE", 3},
	{"skt", "boot", "the title boots and paints",
		"./internal/platform/skt", "TestLocalSKTArchivesBootAndPaint", "WFEATURE_SKT_ACCEPTANCE", 1},
	{"skt", "sustained", "the title keeps running past its first frame",
		"./internal/platform/skt", "TestLocalSKTArchivesSustainAFrame", "WFEATURE_SKT_SUSTAINED_ACCEPTANCE", 2},
	{"skt", "interactive", "a key changes what the title draws",
		"./internal/platform/skt", "TestLocalSKTArchivesAnswerAKey", "WFEATURE_SKT_INTERACTIVE_ACCEPTANCE", 3},
	// Not a rung: it asks something else of the same archive, and a title
	// whose sounds do not decode has not fallen down the ladder.
	{"skt", "sound", "every sound the archive carries decodes",
		"./internal/platform/skt", "TestLocalSKTArchiveSoundsDecode", "WFEATURE_SKT_ACCEPTANCE", 0},
}

// recordExtension is what the machine-readable half of a run is written as:
// one JSON object per line, so a comparison can read it a record at a time and
// a shell can grep it.
const recordExtension = ".ndjson"

// wholeCorpusRow is the row a probe that checks a whole corpus in one test
// produces. It names no file, so it is a stage's count rather than an
// archive's record.
const wholeCorpusRow = "the whole corpus, in one test"

// Where each platform's corpus lives, relative to the repository root. The
// probes read these directories themselves; they are counted here so the
// report says what was in front of the run as well as what came out of it.
var corpus = map[string]string{
	"ktf": filepath.Join("var", "games", "ktf"),
	"lgt": filepath.Join("var", "games", "lgt"),
	"skt": filepath.Join("var", "games", "skt"),
}

func main() { os.Exit(command()) }

// command is main with a return value, so that every failure leaves through
// one door. The tool builds a temporary directory it has to remove, and
// `os.Exit` runs no deferred call: an exit in the middle of a sweep would
// leave a shadow root behind in the temporary directory every time a stage
// could not run.
func command() int {
	out := flag.String("out", filepath.Join("var", "acceptance"), "the directory the report is written to")
	only := flag.String("platform", "ktf,lgt,skt", "which platforms to run, comma separated")
	timeout := flag.String("timeout", "60m", "the `go test` timeout for one stage")
	since := flag.String("since", "auto", "the record `file` this run is compared with: a path, `auto` for the newest already in the output directory, or empty for none")
	compareOnly := flag.Bool("compare", false, "compare two record files and write the difference to standard output, running no probes")
	cache := flag.Bool("cache", true, "carry an archive's answer forward from the run -since names, when the file's bytes and this build are both unchanged")
	games := flag.String("games", "", "sweep these `directories` instead of the three platform folders, recursively, with each file's platform decided by its bytes")
	exclude := flag.String("exclude", "", "directory `names` -games must not descend into, by base name or by path")
	flag.Parse()

	if *compareOnly {
		if err := compareFiles(flag.Args()); err != nil {
			fmt.Fprintln(os.Stderr, "acceptance:", err)
			return 2
		}
		return 0
	}

	root, err := repositoryRoot()
	if err != nil {
		fmt.Fprintln(os.Stderr, "acceptance:", err)
		return 2
	}
	wanted := map[string]bool{}
	for _, platform := range strings.Split(*only, ",") {
		wanted[strings.TrimSpace(strings.ToLower(platform))] = true
	}
	var platforms []string
	for _, platform := range []string{"ktf", "lgt", "skt"} {
		if wanted[platform] {
			platforms = append(platforms, platform)
		}
	}

	started := time.Now()
	directory := *out
	if !filepath.IsAbs(directory) {
		directory = filepath.Join(root, directory)
	}
	if err := os.MkdirAll(directory, 0o755); err != nil {
		fmt.Fprintln(os.Stderr, "acceptance:", err)
		return 2
	}
	file := filepath.Join(directory, started.Format("2006-01-02")+".md")
	records := filepath.Join(directory, started.Format("2006-01-02")+recordExtension)

	// The run this one is compared with is read before this one is measured,
	// because it is also what says which archives still have to be. A second
	// run on the same day reads the day before it rather than itself.
	previousFile, previousRun, previous := load(*since, directory, records)

	// The corpus is read once: the same bytes answer what is in front of the
	// run, what the loaders make of each file, and whether an earlier answer
	// still applies.
	var corpora []corpusDir
	var survey map[string][]corpusEntry
	if *games == "" {
		corpora = defaultCorpora(platforms)
		survey = surveyCorpus(root, corpora)
	} else {
		corpora, survey, err = sweepTrees(root, split(*games), split(*exclude))
		if err != nil {
			fmt.Fprintln(os.Stderr, "acceptance:", err)
			return 2
		}
		// -platform still selects, because a sweep of a mixed tree is often
		// worth running one ladder at a time. A corpus of files nothing
		// claimed is kept whatever is selected: it is the half of the answer
		// that no platform was ever going to give.
		corpora = onlyPlatforms(corpora, wanted)
	}
	planned := map[string][]stage{}
	for _, at := range corpora {
		for _, current := range stages {
			if current.platform == at.platform && wanted[current.platform] {
				planned[at.name] = append(planned[at.name], current)
			}
		}
	}
	header := runHeader(root, started)
	// The cache reads the most recent run there is, which on a second run in
	// one day is that day's own file — the one the comparison deliberately
	// steps over. Comparing a run with itself says nothing, which is why the
	// delta skips it; reusing its answers says everything, which is why this
	// does not. It is read before this run overwrites it.
	plan := planCache(*cache, sourceOf(records, previousFile, previousRun, previous), survey, planned, header)

	// A swept corpus is not where a probe looks, so the probes are compiled
	// against a module root whose `var` this tool owns. Nothing under the
	// checkout is written; see sweep.go.
	var shadow *shadowRoot
	for _, at := range corpora {
		if !at.staged {
			continue
		}
		shadow, err = newShadowRoot(root)
		if err != nil {
			fmt.Fprintln(os.Stderr, "acceptance:", err)
			return 2
		}
		defer shadow.close()
		break
	}

	var results []result
	for _, at := range corpora {
		want := planned[at.name]
		if len(want) == 0 {
			continue
		}
		entries := survey[at.name]
		if at.staged {
			if err := shadow.stage(at.platform, entries); err != nil {
				fmt.Fprintln(os.Stderr, "acceptance:", err)
				return 2
			}
		}
		for _, current := range want {
			selection := plan.selection(at.name, entries)
			fmt.Fprintf(os.Stderr, "%s %s: ", at.name, current.name)
			outcome := run(shadow.dirOf(root, at), current, *timeout, selection)
			outcome.corpus = at
			outcome.cached = plan.carriedCount(at.name, entries)
			results = append(results, outcome)
			fmt.Fprintf(os.Stderr, "%d passed, %d skipped, %d failed, %d carried forward (%s)\n",
				len(outcome.passed), len(outcome.skipped), len(outcome.failed), outcome.cached,
				outcome.elapsed.Round(time.Second))
		}
	}

	runOf, archives := buildRecords(header, results, corpora, survey, plan)
	report := write(root, corpora, survey, results, started, plan) +
		writeUnclaimed(archives) + analysis(archives, previousFile, previousRun, previous)
	if err := os.WriteFile(file, []byte(report), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "acceptance:", err)
		return 2
	}
	if err := writeRecords(records, runOf, archives); err != nil {
		fmt.Fprintln(os.Stderr, "acceptance:", err)
		return 2
	}
	fmt.Println(file)
	fmt.Println(records)

	// A failing archive is a finding rather than a broken run, so the exit
	// code says whether a stage could not be run at all. A report that lists
	// eleven failures is a successful run of this command.
	for _, outcome := range results {
		if outcome.err != "" {
			return 1
		}
	}
	return 0
}

// cacheSource is the run whose answers may be carried forward.
type cacheSource struct {
	file string
	run  runRecord
	rows []archiveRecord
}

// sourceOf prefers a record file already written for today over the older one
// the comparison uses; see where it is called.
func sourceOf(today, previousFile string, previousRun runRecord, previous []archiveRecord) cacheSource {
	if run, rows, err := readRecords(today); err == nil {
		return cacheSource{file: today, run: run, rows: rows}
	}
	return cacheSource{file: previousFile, run: previousRun, rows: previous}
}

// load reads the run this one is compared with. A missing or unreadable file
// is reported and then dropped: a comparison is worth having and is not worth
// losing a run's own report over.
func load(since, directory, except string) (string, runRecord, []archiveRecord) {
	switch since {
	case "":
		return "", runRecord{}, nil
	case "auto":
		since = newestRecords(directory, except)
		if since == "" {
			return "", runRecord{}, nil
		}
	}
	previousRun, previous, err := readRecords(since)
	if err != nil {
		fmt.Fprintln(os.Stderr, "acceptance:", err)
		return "", runRecord{}, nil
	}
	return since, previousRun, previous
}

// analysis is the part of the report that is read out of the records rather
// than measured: what every failure in this run has in common, and what moved
// since the run before it.
func analysis(archives []archiveRecord, previousFile string, previousRun runRecord, previous []archiveRecord) string {
	report := &strings.Builder{}
	writeClusters(report, archives)
	if previousFile != "" {
		writeDelta(report, previousRun, previousFile, compare(previous, archives))
	}
	return report.String()
}

// compareFiles is the command without a run behind it: two record files in,
// the difference between them out. It is what a release check reaches for when
// the two runs it wants to compare have both already happened.
func compareFiles(paths []string) error {
	if len(paths) != 2 {
		return fmt.Errorf("-compare takes two record files, got %d", len(paths))
	}
	previousRun, previous, err := readRecords(paths[0])
	if err != nil {
		return err
	}
	_, current, err := readRecords(paths[1])
	if err != nil {
		return err
	}
	report := &strings.Builder{}
	writeClusters(report, current)
	writeDelta(report, previousRun, paths[0], compare(previous, current))
	fmt.Print(report.String())
	return nil
}

// result is one stage's run: which archives passed, which were skipped and
// why, and which failed with what.
type result struct {
	// corpus is the directory this stage was run over. One platform's ladder
	// may be run several times in a sweep, once per group directory, so a
	// stage alone no longer names a run.
	corpus  corpusDir
	stage   stage
	passed  []string
	skipped []note
	failed  []note
	elapsed time.Duration
	err     string // the stage could not be run at all
	// cached is how many of this stage's archives were not measured here at
	// all, because an earlier run already answered for the same bytes under
	// the same build. See cache.go.
	cached int
	// skipped entirely: every archive was carried forward, so `go test` was
	// never started.
	notRun bool
}

// ranNothing reports a stage that was not run at all because there was nothing
// left for it to measure.
func (outcome result) ranNothing() bool { return outcome.notRun }

type note struct {
	archive string
	why     string
}

// run executes one probe and reads its results out of `go test -json` rather
// than out of its printed output, which is what keeps a subtest's name and its
// reason together when several of them fail.
//
// A selection names the archives this run still has to measure; nil means all
// of them, and an empty one means the stage has nothing left to ask and is not
// started. The selection reaches the probe as `go test`'s own subtest filter,
// because a subtest here is an archive and `go test` already knows how to pick
// them — a flag on the probe would be a second way of saying the same thing.
func run(directory string, current stage, timeout string, selection []string) result {
	outcome := result{stage: current}
	if selection != nil && len(selection) == 0 {
		outcome.notRun = true
		return outcome
	}
	started := time.Now()

	command := exec.Command("go", "test", "-json", "-count=1",
		"-timeout", timeout, "-run", runPattern(current.test, selection), current.pkg)
	command.Dir = directory
	command.Env = append(os.Environ(), current.env+"=1")
	pipe, err := command.StdoutPipe()
	if err != nil {
		outcome.err = err.Error()
		return outcome
	}
	command.Stderr = os.Stderr
	if err := command.Start(); err != nil {
		outcome.err = err.Error()
		return outcome
	}

	outcome, readErr := collect(pipe, current, selection != nil)
	err = command.Wait()
	outcome.elapsed = time.Since(started)
	rows := len(outcome.passed) + len(outcome.skipped) + len(outcome.failed)
	switch {
	case readErr != nil:
		outcome.err = readErr.Error()
	case err != nil && rows == 0:
		outcome.err = fmt.Sprintf("%v (is %s set, and is there a corpus under %s?)", err, current.env,
			filepath.Join(directory, corpus[current.platform]))
	case err != nil && !ordinaryTestFailure(err, outcome):
		// A failing archive makes `go test` exit 1 and is the ordinary outcome
		// here. Any other way of exiting is not: a build that did not compile,
		// a process that was killed, a run that ended early. Reporting those
		// only when no rows arrived is how a stage that answered for a quarter
		// of its corpus is written down as a smaller but successful one.
		outcome.err = fmt.Sprintf("the probe ended with %v after answering for %d archive(s)", err, rows)
	case selection != nil && rows != len(selection):
		// A selection names exactly the archives this stage still has to ask
		// about, so the rows are countable in advance. Fewer means the stream
		// was cut, and there is no reading of the difference that is not a
		// gap in the record.
		outcome.err = fmt.Sprintf("asked about %d archive(s) and heard back about %d", len(selection), rows)
	}
	return outcome
}

// ordinaryTestFailure reports whether `go test`'s exit is the one a failing
// archive produces: status 1, with at least one failing row to account for it.
// A status of 2 is a build or setup problem, and a signal is a process that
// did not finish; neither leaves a trustworthy stage behind.
func ordinaryTestFailure(err error, outcome result) bool {
	var exit *exec.ExitError
	if !errors.As(err, &exit) {
		return false
	}
	return exit.ExitCode() == 1 && len(outcome.failed) > 0
}

// collect reads a `go test -json` stream into one stage's rows. It reads the
// stream rather than the printed output because that is what keeps a subtest's
// name and its reason together when several of them fail at once — printed
// output interleaves, and the archive a line belongs to is not in the line.
func collect(stream io.Reader, current stage, filtered bool) (result, error) {
	outcome := result{stage: current}
	// The last line a subtest printed before it ended. A failure's is the
	// `t.Fatalf` that ended it and a skip's is the `t.Skip` reason, which is
	// the whole of what the report has to carry per archive.
	last := map[string]string{}
	// What the probe itself answered, for the one that does not split its
	// corpus into subtests.
	whole := map[string]bool{}
	scanner := bufio.NewScanner(stream)
	scanner.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	for scanner.Scan() {
		var event struct {
			Action string
			Test   string
			Output string
		}
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			continue
		}
		// Only the per-archive subtests are rows; the parent test's own pass
		// or fail is the sum of them — except when there are none, which is
		// kept below so a probe that reports one result for the whole corpus
		// is a row rather than an empty stage.
		archive, isSubtest := strings.CutPrefix(event.Test, current.test+"/")
		if !isSubtest {
			if event.Test == current.test {
				whole[event.Action] = true
				if line := reason(event.Output); line != "" {
					last[current.test] = line
				}
			}
			continue
		}
		switch event.Action {
		case "output":
			if line := reason(event.Output); line != "" {
				last[archive] = line
			}
		case "pass":
			outcome.passed = append(outcome.passed, archive)
		case "skip":
			outcome.skipped = append(outcome.skipped, note{archive, last[archive]})
		case "fail":
			outcome.failed = append(outcome.failed, note{archive, last[archive]})
		}
	}
	if err := scanner.Err(); err != nil {
		// The stream ended before `go test` did. Everything gathered so far
		// looks like a complete stage and is not one: this is the difference
		// between a corpus of twenty-five archives and a corpus of two hundred
		// and sixty that stopped being read.
		return outcome, fmt.Errorf("read the probe's output: %w", err)
	}
	if len(outcome.passed)+len(outcome.skipped)+len(outcome.failed) == 0 && !filtered {
		// A probe that checks the whole corpus in one test — its rows are the
		// lines it logged, and what the report can say is whether it passed.
		//
		// Not when a selection was in force: a filter that matched no subtest
		// leaves the parent test passing with nothing under it, and reading
		// that as "the whole corpus passed" would invent a row out of the
		// absence of one.
		row := note{wholeCorpusRow, last[current.test]}
		switch {
		case whole["fail"]:
			outcome.failed = append(outcome.failed, row)
		case whole["skip"]:
			outcome.skipped = append(outcome.skipped, row)
		case whole["pass"]:
			outcome.passed = append(outcome.passed, row.archive)
		}
	}
	sort.Strings(outcome.passed)
	sortNotes(outcome.skipped)
	sortNotes(outcome.failed)
	return outcome, nil
}

// reason reads one line of test output as what the report should carry, or
// returns empty for a line that says nothing about an archive.
//
// The lines `testing` frames a result with are the ones to drop: `--- FAIL:
// <test> (0.00s)` arrives after the message that explains the failure, so
// keeping it would overwrite every reason in the report with the word FAIL.
func reason(output string) string {
	line := strings.TrimSpace(output)
	switch {
	case line == "",
		strings.HasPrefix(line, "=== "),
		strings.HasPrefix(line, "--- "),
		strings.HasPrefix(line, "PASS"),
		strings.HasPrefix(line, "FAIL"),
		strings.HasPrefix(line, "ok "):
		return ""
	}
	return tidy(line)
}

// tidy strips the file and line a testing message carries in front of its
// reason. The line moves whenever the test is edited, and what the report is
// for is the reason.
func tidy(line string) string {
	if index := strings.Index(line, ".go:"); index >= 0 {
		if colon := strings.Index(line[index+4:], ": "); colon >= 0 {
			return strings.TrimSpace(line[index+4+colon+2:])
		}
	}
	return line
}

func write(root string, corpora []corpusDir, survey map[string][]corpusEntry,
	results []result, started time.Time, plan cachePlan) string {
	report := &strings.Builder{}
	fmt.Fprintf(report, "# Local acceptance, %s\n\n", started.Format("2006-01-02"))
	fmt.Fprintf(report, "Written by `make acceptance` on %s/%s with %s.\n\n",
		runtime.GOOS, runtime.GOARCH, runtime.Version())
	report.WriteString("Every row is one archive in the ignored local corpus. " +
		"A skip is an archive this platform knowingly does not claim; a failure is one it does.\n\n")

	fmt.Fprintf(report, "## What was in front of it\n\n| corpus | directory | platform | archives | other files |\n|---|---|---|---|---|\n")
	for _, at := range corpora {
		// The corpus's own files rather than the directory's. One directory
		// can be several corpora — a group holding two platforms' archives
		// and the files nothing claimed — and re-counting the directory for
		// each of them would report the same files three times.
		archives, others := countEntries(survey[at.name])
		platform := at.platform
		if platform == "" {
			platform = "—"
		}
		fmt.Fprintf(report, "| `%s` | `%s` | %s | %d | %d |\n", at.name, at.directory, platform, archives, others)
	}
	report.WriteString("\n")

	report.WriteString("## What they answered\n\n")
	report.WriteString(plan.describe(results))
	report.WriteString("| corpus | platform | stage | ran | passed | skipped | failed | carried forward |\n|---|---|---|---|---|---|---|---|\n")
	for _, outcome := range results {
		ran := len(outcome.passed) + len(outcome.skipped) + len(outcome.failed)
		fmt.Fprintf(report, "| %s | %s | %s | %d | %d | %d | %d | %d |\n",
			outcome.corpus.name, strings.ToUpper(outcome.stage.platform), outcome.stage.name,
			ran, len(outcome.passed), len(outcome.skipped), len(outcome.failed), outcome.cached)
	}
	report.WriteString("\n")

	for _, outcome := range results {
		fmt.Fprintf(report, "## %s — %s — %s\n\n%s. `%s=1 go test -run %s %s`\n\n",
			outcome.corpus.name, strings.ToUpper(outcome.stage.platform), outcome.stage.name,
			upperFirst(outcome.stage.what), outcome.stage.env, outcome.stage.test, outcome.stage.pkg)
		if outcome.err != "" {
			fmt.Fprintf(report, "**This stage did not run**: %s\n\n", outcome.err)
			continue
		}
		if outcome.ranNothing() {
			fmt.Fprintf(report, "Not run: all %d archives were carried forward from an earlier run.\n\n", outcome.cached)
			continue
		}
		fmt.Fprintf(report, "Took %s.\n\n", outcome.elapsed.Round(time.Second))
		writeNotes(report, "Failed", outcome.failed)
		writeNotes(report, "Skipped", outcome.skipped)
		if len(outcome.failed) == 0 && len(outcome.skipped) == 0 {
			fmt.Fprintf(report, "All %d archives passed.\n\n", len(outcome.passed))
		}
	}
	return report.String()
}

func writeNotes(report *strings.Builder, heading string, notes []note) {
	if len(notes) == 0 {
		return
	}
	fmt.Fprintf(report, "### %s (%d)\n\n", heading, len(notes))
	for _, entry := range notes {
		why := entry.why
		if why == "" {
			why = "no reason printed"
		}
		fmt.Fprintf(report, "- `%s` — %s\n", entry.archive, why)
	}
	report.WriteString("\n")
}

// countEntries says how many of a corpus's files are archives by their name
// and how many are not. The second number is the one worth looking at: a file
// that is not an archive is a download that did not finish or a container this
// project does not read, and neither shows up as a failure because no probe
// ever picks it up.
//
// It counts what the survey found rather than listing the directory again,
// because a directory can be several corpora — a group holding two platforms'
// archives, and the files nothing claimed — and the survey is what already
// divided them. Dot files are the operating system's and were never in it: a
// Finder window leaves one in every directory it is opened in, and counting it
// as something that did not run would be a finding about nothing.
func countEntries(entries []corpusEntry) (archives, others int) {
	for _, entry := range entries {
		if strings.EqualFold(filepath.Ext(entry.name), ".zip") ||
			strings.EqualFold(filepath.Ext(entry.name), ".jad") {
			archives++
			continue
		}
		others++
	}
	return archives, others
}

func sortNotes(notes []note) {
	sort.Slice(notes, func(one, two int) bool { return notes[one].archive < notes[two].archive })
}

func upperFirst(text string) string {
	if text == "" {
		return text
	}
	return strings.ToUpper(text[:1]) + text[1:]
}

// repositoryRoot walks up from this source file to the directory holding
// go.mod, so the command can be run from anywhere the way `go run` is.
func repositoryRoot() (string, error) {
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		return "", fmt.Errorf("locate this source file")
	}
	directory := filepath.Dir(source)
	for {
		if _, err := os.Stat(filepath.Join(directory, "go.mod")); err == nil {
			return directory, nil
		}
		parent := filepath.Dir(directory)
		if parent == directory {
			return "", fmt.Errorf("no go.mod above %s", filepath.Dir(source))
		}
		directory = parent
	}
}

// split reads a comma-separated flag as the list it names, dropping empties so
// a trailing comma is not a directory called "".
func split(value string) []string {
	var parts []string
	for _, part := range strings.Split(value, ",") {
		if part = strings.TrimSpace(part); part != "" {
			parts = append(parts, part)
		}
	}
	return parts
}

// onlyPlatforms drops the corpora whose platform `-platform` did not name. The
// unclaimed corpora are kept whatever was named: what nothing claimed is not
// any platform's answer, and it is half of what a sweep of an unsorted tree is
// for.
func onlyPlatforms(corpora []corpusDir, wanted map[string]bool) []corpusDir {
	kept := make([]corpusDir, 0, len(corpora))
	for _, at := range corpora {
		if at.platform == "" || wanted[at.platform] {
			kept = append(kept, at)
		}
	}
	return kept
}

// writeUnclaimed is the other half of a sweep: the files no platform claimed,
// grouped by the reason nothing did.
//
// A count of files that did not run says nothing on its own, because only one
// of the reasons behind it is work this project can do. A package that was
// locked before it was distributed is not reachable by any amount of work on
// the loaders; a container of another format is a file somebody has to unpack;
// a zip of whole packages is a choice rather than a game. What is left — a
// readable archive carrying no marker any platform recognises — is the number
// worth acting on, and it is the one this table exists to separate out.
func writeUnclaimed(archives []archiveRecord) string {
	counted := map[string]int{}
	total := 0
	for _, record := range archives {
		if record.Platform != "" {
			continue
		}
		reason := record.DetectReason
		if reason == "" {
			reason = "no reason recorded"
		}
		counted[reason]++
		total++
	}
	if total == 0 {
		return ""
	}
	report := &strings.Builder{}
	report.WriteString("## What nothing claimed\n\n")
	fmt.Fprintf(report, "%d file(s) reached no ladder, because no platform claimed them. "+
		"Only `no-marker` is this project's work to do; the rest are what the file is.\n\n", total)
	report.WriteString("| files | reason | what it means |\n|---|---|---|\n")
	reasons := make([]string, 0, len(counted))
	for reason := range counted {
		reasons = append(reasons, reason)
	}
	sort.Slice(reasons, func(one, two int) bool {
		if counted[reasons[one]] != counted[reasons[two]] {
			return counted[reasons[one]] > counted[reasons[two]]
		}
		return reasons[one] < reasons[two]
	})
	for _, reason := range reasons {
		fmt.Fprintf(report, "| %d | `%s` | %s |\n", counted[reason], reason, meaningOf(reason))
	}
	report.WriteString("\n")
	// The names behind the one count that is ours, so the next investigation
	// starts from a list rather than from another sweep.
	var ours []string
	for _, record := range archives {
		if record.Platform == "" && record.DetectReason == string(detect.ReasonNoMarker) {
			ours = append(ours, "`"+record.Archive+"`")
		}
	}
	if len(ours) > 0 {
		sort.Strings(ours)
		fmt.Fprintf(report, "- **`no-marker`** — %s\n\n", strings.Join(ours, ", "))
	}
	return report.String()
}

// meaningOf is the one-line gloss the report carries beside a detection
// reason, so a table of counts can be read without the package beside it.
func meaningOf(reason string) string {
	switch detect.Reason(reason) {
	case detect.ReasonNoMarker:
		return "a readable archive carrying no marker any platform recognises — either not one of these packages, or one whose shape is not known here yet"
	case detect.ReasonDRMWrapped:
		return "locked before it was distributed; the key is not this project's to have"
	case detect.ReasonKnownFormatUnsupported:
		return "an archive of a format this does not read, so any package is one unpacking away"
	case detect.ReasonArchiveOfArchives:
		return "a bag of whole packages; the choice of which to run belongs to the person holding it"
	case detect.ReasonNotAnArchive:
		return "not an archive at all: a truncated download, a document, a program"
	default:
		return ""
	}
}
