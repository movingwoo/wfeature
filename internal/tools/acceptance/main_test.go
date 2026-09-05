package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// What a stage answered has to survive the trip through `go test -json`: the
// archive that failed, and the line that says why it failed. The reason is the
// last thing a subtest printed, and it is not in the same event as its name —
// which is the whole reason the report is built from the stream rather than
// from the printed output.
func TestAStageIsReadOutOfTheTestStream(t *testing.T) {
	probe := stage{platform: "lgt", name: "boot", test: "TestLocalLGTArchivesBootAndPaint"}
	stream := testStream(t, probe.test, []event{
		{Action: "run", Test: "a.zip"},
		{Action: "output", Test: "a.zip", Output: "=== RUN   TestLocalLGTArchivesBootAndPaint/a.zip\n"},
		{Action: "pass", Test: "a.zip"},

		{Action: "run", Test: "b.zip"},
		{Action: "output", Test: "b.zip", Output: "    local_acceptance_test.go:63: LGT Java app, which this platform does not support\n"},
		{Action: "skip", Test: "b.zip"},

		{Action: "run", Test: "c.zip"},
		{Action: "output", Test: "c.zip", Output: "    local_acceptance_test.go:71: start: parse module: bad section header\n"},
		{Action: "fail", Test: "c.zip"},

		// The parent test's own result is the sum of its subtests and is not
		// a row of its own.
		{Action: "fail", Test: ""},
	})

	outcome := collect(strings.NewReader(stream), probe)
	if got := strings.Join(outcome.passed, ","); got != "a.zip" {
		t.Errorf("passed = %q, want a.zip", got)
	}
	if len(outcome.skipped) != 1 || outcome.skipped[0].archive != "b.zip" {
		t.Fatalf("skipped = %v, want one row for b.zip", outcome.skipped)
	}
	if want := "LGT Java app, which this platform does not support"; outcome.skipped[0].why != want {
		t.Errorf("the skip reason is %q, want %q", outcome.skipped[0].why, want)
	}
	if len(outcome.failed) != 1 || outcome.failed[0].archive != "c.zip" {
		t.Fatalf("failed = %v, want one row for c.zip", outcome.failed)
	}
	if want := "start: parse module: bad section header"; outcome.failed[0].why != want {
		t.Errorf("the failure reason is %q, want %q", outcome.failed[0].why, want)
	}
}

// The file and line in front of a testing message is where the probe is
// written, not what it found, and it moves every time that file is edited. A
// report that carried it would differ from yesterday's for no reason.
func TestTheReasonLosesTheLineItWasPrintedFrom(t *testing.T) {
	for _, probe := range []struct{ line, want string }{
		{"    local_acceptance_test.go:63: the title never asked to present a frame",
			"the title never asked to present a frame"},
		{"    ktf_test.go:112: start: no main class named in the descriptor",
			"start: no main class named in the descriptor"},
		// Nothing to strip: a line that is not a testing message is its own
		// reason.
		{"panic: runtime error: index out of range", "panic: runtime error: index out of range"},
	} {
		if got := tidy(probe.line); got != probe.want {
			t.Errorf("tidy(%q) = %q, want %q", probe.line, got, probe.want)
		}
	}
}

// The report is what the documentation points at instead of carrying a number,
// so what it has to hold is the date, the corpus it ran over, and every
// archive that did not pass with the reason it did not.
func TestTheReportCarriesTheDateTheCorpusAndEveryArchiveThatDidNotPass(t *testing.T) {
	root := t.TempDir()
	for _, platform := range []string{"ktf", "lgt", "skt"} {
		if err := os.MkdirAll(filepath.Join(root, "var", "games", platform), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	place := func(platform, name string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(root, "var", "games", platform, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	place("lgt", "one.zip")
	place("lgt", "two.zip")
	// Not an archive, and no probe ever picks it up: a download that did not
	// finish is invisible in a pass count and is the reason the report says
	// what was in the directory as well.
	place("lgt", "half-a-download.zip.part")

	day := time.Date(2026, 9, 4, 10, 0, 0, 0, time.UTC)
	report := write(root, []result{{
		stage:   stageNamed(t, "lgt", "boot"),
		passed:  []string{"one.zip"},
		failed:  []note{{"two.zip", "the title never asked to present a frame"}},
		elapsed: 42 * time.Second,
	}}, day)

	for _, wanted := range []string{
		"# Local acceptance, 2026-09-04",
		"| `var/games/lgt` | 2 | 1 |",
		"| LGT | boot | 2 | 1 | 0 | 1 |",
		"`two.zip` — the title never asked to present a frame",
		"WFEATURE_LGT_ACCEPTANCE=1",
	} {
		if !strings.Contains(report, wanted) {
			t.Errorf("the report does not carry %q:\n%s", wanted, report)
		}
	}
}

// A stage that could not run at all is not the same as a stage every archive
// failed, and the report has to say which it was: the first is a probe that
// never started, the second is a platform with work to do.
func TestAStageThatCouldNotRunSaysSo(t *testing.T) {
	report := write(t.TempDir(), []result{{
		stage: stageNamed(t, "ktf", "parse"),
		err:   "exit status 2 (is WFEATURE_KTF_ACCEPTANCE set, and is there a corpus in var/games/ktf?)",
	}}, time.Now())
	if !strings.Contains(report, "**This stage did not run**") {
		t.Errorf("a stage that did not run was reported as one that did:\n%s", report)
	}
}

// Helpers.

func stageNamed(t *testing.T, platform, name string) stage {
	t.Helper()
	for _, current := range stages {
		if current.platform == platform && current.name == name {
			return current
		}
	}
	t.Fatalf("no %s stage called %q", platform, name)
	return stage{}
}

type event struct {
	Action string
	Test   string
	Output string
}

// testStream is what `go test -json` writes: one JSON object per line, with
// subtest names carrying their parent's.
func testStream(t *testing.T, parent string, events []event) string {
	t.Helper()
	lines := &strings.Builder{}
	for _, current := range events {
		if current.Test != "" {
			current.Test = parent + "/" + current.Test
		}
		encoded, err := json.Marshal(current)
		if err != nil {
			t.Fatal(err)
		}
		lines.Write(encoded)
		lines.WriteString("\n")
	}
	return lines.String()
}
