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
// **The report is not committed and cannot be.** Its rows are the archive file
// names, and those are the games' names; it lands under `var/`, which is
// ignored for exactly that reason.
//
// Usage:
//
//	go run ./internal/tools/acceptance [-out var/acceptance] [-platform ktf,lgt,skt]
//
// `make acceptance` is the same run.
package main

import (
	"bufio"
	"encoding/json"
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
}

var stages = []stage{
	{"ktf", "parse", "the archive opens and its module parses",
		"./internal/platform/ktf", "TestLocalKTFArchivesParse", "WFEATURE_KTF_ACCEPTANCE"},
	{"ktf", "initialize", "the module runs its own initialisation",
		"./internal/platform/ktf", "TestLocalKTFArchivesInitialize", "WFEATURE_KTF_EXECUTE_ACCEPTANCE"},
	{"ktf", "load", "the main class loads",
		"./internal/platform/ktf", "TestLocalKTFArchivesLoadMainClass", "WFEATURE_KTF_LIFECYCLE_ACCEPTANCE"},
	{"ktf", "construct", "the main class is constructed",
		"./internal/platform/ktf", "TestLocalKTFArchivesConstructMainClass", "WFEATURE_KTF_CONSTRUCT_ACCEPTANCE"},
	{"ktf", "start", "the title's start method returns",
		"./internal/platform/ktf", "TestLocalKTFArchivesStartMainClass", "WFEATURE_KTF_START_ACCEPTANCE"},
	{"ktf", "frame", "the title paints a first frame",
		"./internal/platform/ktf", "TestLocalKTFArchivesRenderFirstFrame", "WFEATURE_KTF_FRAME_ACCEPTANCE"},
	{"lgt", "boot", "the module boots and asks to present a frame",
		"./internal/platform/lgt", "TestLocalLGTArchivesBootAndPaint", "WFEATURE_LGT_ACCEPTANCE"},
	{"skt", "boot", "the title boots and paints",
		"./internal/platform/skt", "TestLocalSKTArchivesBootAndPaint", "WFEATURE_SKT_ACCEPTANCE"},
	{"skt", "sound", "every sound the archive carries decodes",
		"./internal/platform/skt", "TestLocalSKTArchiveSoundsDecode", "WFEATURE_SKT_ACCEPTANCE"},
}

// Where each platform's corpus lives, relative to the repository root. The
// probes read these directories themselves; they are counted here so the
// report says what was in front of the run as well as what came out of it.
var corpus = map[string]string{
	"ktf": filepath.Join("var", "games", "ktf"),
	"lgt": filepath.Join("var", "games", "lgt"),
	"skt": filepath.Join("var", "games", "skt"),
}

func main() {
	out := flag.String("out", filepath.Join("var", "acceptance"), "the directory the report is written to")
	only := flag.String("platform", "ktf,lgt,skt", "which platforms to run, comma separated")
	timeout := flag.String("timeout", "60m", "the `go test` timeout for one stage")
	flag.Parse()

	root, err := repositoryRoot()
	if err != nil {
		fmt.Fprintln(os.Stderr, "acceptance:", err)
		os.Exit(2)
	}
	wanted := map[string]bool{}
	for _, platform := range strings.Split(*only, ",") {
		wanted[strings.TrimSpace(strings.ToLower(platform))] = true
	}

	started := time.Now()
	var results []result
	for _, current := range stages {
		if !wanted[current.platform] {
			continue
		}
		fmt.Fprintf(os.Stderr, "%s %s: ", current.platform, current.name)
		outcome := run(root, current, *timeout)
		results = append(results, outcome)
		fmt.Fprintf(os.Stderr, "%d passed, %d skipped, %d failed (%s)\n",
			len(outcome.passed), len(outcome.skipped), len(outcome.failed), outcome.elapsed.Round(time.Second))
	}

	report := write(root, results, started)
	directory := *out
	if !filepath.IsAbs(directory) {
		directory = filepath.Join(root, directory)
	}
	if err := os.MkdirAll(directory, 0o755); err != nil {
		fmt.Fprintln(os.Stderr, "acceptance:", err)
		os.Exit(2)
	}
	file := filepath.Join(directory, started.Format("2006-01-02")+".md")
	if err := os.WriteFile(file, []byte(report), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "acceptance:", err)
		os.Exit(2)
	}
	fmt.Println(file)

	// A failing archive is a finding rather than a broken run, so the exit
	// code says whether a stage could not be run at all. A report that lists
	// eleven failures is a successful run of this command.
	for _, outcome := range results {
		if outcome.err != "" {
			os.Exit(1)
		}
	}
}

// result is one stage's run: which archives passed, which were skipped and
// why, and which failed with what.
type result struct {
	stage   stage
	passed  []string
	skipped []note
	failed  []note
	elapsed time.Duration
	err     string // the stage could not be run at all
}

type note struct {
	archive string
	why     string
}

// run executes one probe and reads its results out of `go test -json` rather
// than out of its printed output, which is what keeps a subtest's name and its
// reason together when several of them fail.
func run(root string, current stage, timeout string) result {
	outcome := result{stage: current}
	started := time.Now()

	command := exec.Command("go", "test", "-json", "-count=1",
		"-timeout", timeout, "-run", "^"+current.test+"$", current.pkg)
	command.Dir = root
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

	outcome = collect(pipe, current)
	err = command.Wait()
	outcome.elapsed = time.Since(started)
	// A failing archive makes `go test` exit non-zero, which is the ordinary
	// outcome here; a stage that produced no rows at all is the one worth
	// reporting as broken, because that is a probe that never ran.
	if err != nil && len(outcome.passed)+len(outcome.skipped)+len(outcome.failed) == 0 {
		outcome.err = fmt.Sprintf("%v (is %s set, and is there a corpus in %s?)", err, current.env, corpus[current.platform])
	}
	return outcome
}

// collect reads a `go test -json` stream into one stage's rows. It reads the
// stream rather than the printed output because that is what keeps a subtest's
// name and its reason together when several of them fail at once — printed
// output interleaves, and the archive a line belongs to is not in the line.
func collect(stream io.Reader, current stage) result {
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
	if len(outcome.passed)+len(outcome.skipped)+len(outcome.failed) == 0 {
		// A probe that checks the whole corpus in one test — its rows are the
		// lines it logged, and what the report can say is whether it passed.
		row := note{"the whole corpus, in one test", last[current.test]}
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
	return outcome
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

func write(root string, results []result, started time.Time) string {
	report := &strings.Builder{}
	fmt.Fprintf(report, "# Local acceptance, %s\n\n", started.Format("2006-01-02"))
	fmt.Fprintf(report, "Written by `make acceptance` on %s/%s with %s.\n\n",
		runtime.GOOS, runtime.GOARCH, runtime.Version())
	report.WriteString("Every row is one archive in the ignored local corpus. " +
		"A skip is an archive this platform knowingly does not claim; a failure is one it does.\n\n")

	fmt.Fprintf(report, "## What was in front of it\n\n| corpus | archives | other files |\n|---|---|---|\n")
	for _, platform := range []string{"ktf", "lgt", "skt"} {
		archives, others := countCorpus(filepath.Join(root, corpus[platform]))
		fmt.Fprintf(report, "| `%s` | %d | %d |\n", corpus[platform], archives, others)
	}
	report.WriteString("\n")

	report.WriteString("## What they answered\n\n| platform | stage | ran | passed | skipped | failed |\n|---|---|---|---|---|---|\n")
	for _, outcome := range results {
		ran := len(outcome.passed) + len(outcome.skipped) + len(outcome.failed)
		fmt.Fprintf(report, "| %s | %s | %d | %d | %d | %d |\n",
			strings.ToUpper(outcome.stage.platform), outcome.stage.name,
			ran, len(outcome.passed), len(outcome.skipped), len(outcome.failed))
	}
	report.WriteString("\n")

	for _, outcome := range results {
		fmt.Fprintf(report, "## %s — %s\n\n%s. `%s=1 go test -run %s %s`\n\n",
			strings.ToUpper(outcome.stage.platform), outcome.stage.name,
			upperFirst(outcome.stage.what), outcome.stage.env, outcome.stage.test, outcome.stage.pkg)
		if outcome.err != "" {
			fmt.Fprintf(report, "**This stage did not run**: %s\n\n", outcome.err)
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

// countCorpus says how many archives a directory holds and how many other
// files are sitting in it. The second number is the one worth looking at: a
// file that is not an archive is a download that did not finish or a container
// this project does not read, and neither shows up as a failure because no
// probe ever picks it up.
func countCorpus(directory string) (archives, others int) {
	entries, err := os.ReadDir(directory)
	if err != nil {
		return 0, 0
	}
	for _, entry := range entries {
		// A dot file is the operating system's, not the corpus's: a Finder
		// window leaves .DS_Store in every directory it is opened in, and
		// counting it as something that did not run would be a finding about
		// nothing.
		if entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		if strings.EqualFold(filepath.Ext(entry.Name()), ".zip") ||
			strings.EqualFold(filepath.Ext(entry.Name()), ".jad") {
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
