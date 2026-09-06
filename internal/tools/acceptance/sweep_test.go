package main

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

// A group directory's name is a note somebody typed, and this project has
// classified archives by their content since before it had a picker. A sweep
// that trusted the folder would file an archive under a ladder that cannot
// load it and then report that loader's refusal as a defect.
func TestASweptFileTakesItsPlatformFromItsBytesRatherThanItsFolder(t *testing.T) {
	root := t.TempDir()
	// The folder says one platform and every file in it is another's.
	place(t, root, filepath.Join("games", "LGT WIPI 2.X", "actually-ktf.zip"), archiveWith(t, "__adf__"))
	place(t, root, filepath.Join("games", "LGT WIPI 2.X", "actually-lgt.zip"), archiveWith(t, "app_info"))

	corpora, survey, err := sweepTrees(root, []string{"games"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	platforms := map[string][]string{}
	for _, at := range corpora {
		for _, entry := range survey[at.name] {
			platforms[at.platform] = append(platforms[at.platform], entry.name)
		}
	}
	if got := strings.Join(platforms["ktf"], ","); got != "actually-ktf.zip" {
		t.Errorf("the ktf corpus holds %q, want the archive whose bytes say ktf", got)
	}
	if got := strings.Join(platforms["lgt"], ","); got != "actually-lgt.zip" {
		t.Errorf("the lgt corpus holds %q, want the archive whose bytes say lgt", got)
	}
}

// Half of what a sweep of an unsorted tree is for is the pile nothing ran, and
// only one of the reasons behind that pile is work this project can do. A file
// no platform claimed is a record with the reason rather than a discard.
func TestAFileNoPlatformClaimedIsSweptIntoARecordWithTheReason(t *testing.T) {
	root := t.TempDir()
	place(t, root, filepath.Join("games", "unsorted.txt"), []byte("this is not an archive"))
	place(t, root, filepath.Join("games", "locked.zip"), append([]byte("ALZ\x01"), 0, 0, 0, 0))
	place(t, root, filepath.Join("games", "claimed.zip"), archiveWith(t, "__adf__"))

	corpora, survey, err := sweepTrees(root, []string{"games"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	var unclaimed []corpusEntry
	for _, at := range corpora {
		if at.platform == "" {
			unclaimed = append(unclaimed, survey[at.name]...)
			if at.staged {
				t.Errorf("a corpus with no platform was staged for a probe: %q", at.name)
			}
		}
	}
	if len(unclaimed) != 2 {
		t.Fatalf("%d unclaimed files, want the two nothing claimed: %+v", len(unclaimed), unclaimed)
	}
	reasons := map[string]string{}
	for _, entry := range unclaimed {
		reasons[entry.name] = entry.facts.detectReason
	}
	if reasons["unsorted.txt"] != "not-an-archive" {
		t.Errorf("a file that is not an archive was recorded as %q", reasons["unsorted.txt"])
	}
	if reasons["locked.zip"] != "known-format-unsupported" {
		t.Errorf("a container of another format was recorded as %q", reasons["locked.zip"])
	}
}

// The tree files its archives one level below the root under a group name, so
// a sweep that stopped at the root would find nothing at all. The group is
// kept because it is how a row is walked back to a file: two groups may hold
// the same file name.
func TestASweepDescendsAndKeepsTheGroupAFileWasFiledUnder(t *testing.T) {
	root := t.TempDir()
	place(t, root, filepath.Join("games", "one", "same-name.zip"), archiveWith(t, "__adf__"))
	place(t, root, filepath.Join("games", "two", "same-name.zip"), archiveWith(t, "__adf__"))

	corpora, survey, err := sweepTrees(root, []string{"games"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(corpora) != 2 {
		t.Fatalf("%d corpora, want one per group: %+v", len(corpora), corpora)
	}
	for _, at := range corpora {
		if len(survey[at.name]) != 1 {
			t.Errorf("%s holds %d files, want the one in its own group", at.name, len(survey[at.name]))
		}
		if !strings.Contains(at.name, at.directory) || !strings.Contains(at.name, "ktf") {
			t.Errorf("the corpus name %q does not say which group and which platform it is", at.name)
		}
	}
	if corpora[0].name == corpora[1].name {
		t.Errorf("two groups share one corpus name, so their rows would be the same row: %q", corpora[0].name)
	}
}

// A tree holds deliberately broken files, bags of whole packages, and folders
// on their way out. Which of those is worth sweeping is the caller's judgment
// on the day rather than a list this tool would have to be edited to change.
func TestASweepDescendsOnlyWhereTheCallerLetIt(t *testing.T) {
	root := t.TempDir()
	place(t, root, filepath.Join("games", "wanted", "a.zip"), archiveWith(t, "__adf__"))
	place(t, root, filepath.Join("games", "broken", "b.zip"), archiveWith(t, "__adf__"))
	place(t, root, filepath.Join("games", "keep", "broken", "c.zip"), archiveWith(t, "__adf__"))

	// By base name: every folder of that name, wherever it is.
	_, survey, err := sweepTrees(root, []string{"games"}, []string{"broken"})
	if err != nil {
		t.Fatal(err)
	}
	if got := swept(survey); got != "a.zip" {
		t.Errorf("excluding a base name swept %q, want only the file outside it", got)
	}

	// By path relative to the root being swept: that one folder and no other.
	_, survey, err = sweepTrees(root, []string{"games"}, []string{"keep/broken"})
	if err != nil {
		t.Fatal(err)
	}
	if got := swept(survey); got != "a.zip,b.zip" {
		t.Errorf("excluding one path swept %q, want both files outside it", got)
	}

	// Nothing excluded is nothing excluded: the default sweeps the lot.
	_, survey, err = sweepTrees(root, []string{"games"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := swept(survey); got != "a.zip,b.zip,c.zip" {
		t.Errorf("an unrestricted sweep found %q, want every file", got)
	}
}

// The probes find their corpus from their own source location, which is the
// right thing for a probe to do and leaves a sweep of another tree nowhere to
// point them. What moves instead is the root they walk up to — and the one
// property that matters more than any of it is that the real corpus is never
// written to.
func TestAShadowRootPointsAProbeAtASweptCorpusWithoutTouchingTheCheckout(t *testing.T) {
	checkout := t.TempDir()
	place(t, checkout, "go.mod", []byte("module example.com/x\n"))
	place(t, checkout, filepath.Join("internal", "platform", "ktf", "ktf.go"), []byte("package ktf\n"))
	place(t, checkout, filepath.Join("var", "games", "ktf", "the-real-corpus.zip"), []byte("do not touch"))

	elsewhere := t.TempDir()
	place(t, elsewhere, "swept.zip", archiveWith(t, "__adf__"))

	shadow, err := newShadowRoot(checkout)
	if err != nil {
		t.Fatal(err)
	}
	defer shadow.close()
	if err := shadow.stage("ktf", []corpusEntry{{
		name: "swept.zip", path: filepath.Join(elsewhere, "swept.zip"),
	}}); err != nil {
		t.Fatal(err)
	}

	// A probe walks up from its own source to the root and reads var/games
	// there, so the shadow has to carry the source and a corpus of its own.
	if _, err := os.Stat(filepath.Join(shadow.root, "internal", "platform", "ktf", "ktf.go")); err != nil {
		t.Errorf("the shadow root does not carry the source the probes compile from: %v", err)
	}
	staged, err := os.ReadDir(filepath.Join(shadow.root, "var", "games", "ktf"))
	if err != nil {
		t.Fatal(err)
	}
	if len(staged) != 1 || staged[0].Name() != "swept.zip" {
		t.Fatalf("the staged corpus is %v, want the swept file", staged)
	}
	data, err := os.ReadFile(filepath.Join(shadow.root, "var", "games", "ktf", "swept.zip"))
	if err != nil || !bytes.Contains(data, []byte("__adf__")) {
		t.Errorf("the staged entry does not read back as the file it stands for: %v", err)
	}

	// The checkout's own corpus is exactly what it was.
	real, err := os.ReadDir(filepath.Join(checkout, "var", "games", "ktf"))
	if err != nil {
		t.Fatal(err)
	}
	if len(real) != 1 || real[0].Name() != "the-real-corpus.zip" {
		t.Fatalf("a sweep of another tree changed the corpus a release is checked against: %v", real)
	}

	// A second corpus replaces the first rather than joining it: the same
	// shadow root is reused so the compiled test binaries stay cached.
	if err := shadow.stage("ktf", []corpusEntry{{
		name: "another.zip", path: filepath.Join(elsewhere, "swept.zip"),
	}}); err != nil {
		t.Fatal(err)
	}
	staged, err = os.ReadDir(filepath.Join(shadow.root, "var", "games", "ktf"))
	if err != nil {
		t.Fatal(err)
	}
	if len(staged) != 1 || staged[0].Name() != "another.zip" {
		t.Errorf("staging a second corpus left the first behind: %v", staged)
	}
}

// A count of files that did not run says nothing on its own, because only one
// of the reasons behind it is this project's work. The report has to divide
// them, or a sweep reports a number about the tree where it meant to report a
// number about the emulator.
func TestTheReportDividesWhatNothingClaimedByWhyNothingDid(t *testing.T) {
	report := writeUnclaimed([]archiveRecord{
		{Corpus: "a (ktf)", Platform: "ktf", Archive: "ran.zip", Grade: "frame"},
		{Corpus: "a (unclaimed)", Archive: "locked.zip", DetectReason: "drm-wrapped"},
		{Corpus: "a (unclaimed)", Archive: "also-locked.zip", DetectReason: "drm-wrapped"},
		{Corpus: "a (unclaimed)", Archive: "unknown-shape.zip", DetectReason: "no-marker"},
	})
	for _, wanted := range []string{
		"3 file(s) reached no ladder",
		"| 2 | `drm-wrapped` |",
		"| 1 | `no-marker` |",
		// The names behind the one count that is ours, so the next
		// investigation starts from a list rather than another sweep.
		"`unknown-shape.zip`",
	} {
		if !strings.Contains(report, wanted) {
			t.Errorf("the report does not carry %q:\n%s", wanted, report)
		}
	}
	if strings.Contains(report, "ran.zip") {
		t.Errorf("an archive a platform claimed was counted among the ones nothing did:\n%s", report)
	}
	if writeUnclaimed(nil) != "" {
		t.Error("a sweep where every file was claimed still wrote the section")
	}
}

// One directory can be several corpora: a group holding two platforms'
// archives, and the files nothing claimed. A table that answered each of them
// by listing the directory again would report the same files three times, and
// the row a reader most wants — how many files this corpus actually holds —
// would be the one number it never gave.
func TestTheCorpusTableCountsACorpusRatherThanItsDirectoryOverAgain(t *testing.T) {
	root := t.TempDir()
	place(t, root, filepath.Join("games", "mixed", "one.zip"), archiveWith(t, "__adf__"))
	place(t, root, filepath.Join("games", "mixed", "two.zip"), archiveWith(t, "__adf__"))
	place(t, root, filepath.Join("games", "mixed", "elsewhere.zip"), archiveWith(t, "app_info"))
	place(t, root, filepath.Join("games", "mixed", "notes.md"), []byte("not an archive"))

	corpora, survey, err := sweepTrees(root, []string{"games"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	report := write(root, corpora, survey, nil, time.Date(2026, 9, 6, 0, 0, 0, 0, time.UTC), cachePlan{})
	for _, wanted := range []string{
		"| `games/mixed (ktf)` | `games/mixed` | ktf | 2 | 0 |",
		"| `games/mixed (lgt)` | `games/mixed` | lgt | 1 | 0 |",
		// The one file nothing claimed, counted as what it is rather than as
		// an archive that failed.
		"| `games/mixed (unclaimed)` | `games/mixed` | — | 0 | 1 |",
	} {
		if !strings.Contains(report, wanted) {
			t.Errorf("the corpus table does not carry %q:\n%s", wanted, report)
		}
	}
}

// Helpers.

// place writes one file under a root, creating the directories above it.
func place(t *testing.T, root, name string, data []byte) {
	t.Helper()
	path := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

// archiveWith is a zip carrying the one entry a platform is told apart by.
func archiveWith(t *testing.T, entry string) []byte {
	t.Helper()
	buffer := &bytes.Buffer{}
	writer := zip.NewWriter(buffer)
	file, err := writer.Create(entry)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.Write([]byte("x")); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

// swept is every file name a sweep found, in order, as one string.
func swept(survey map[string][]corpusEntry) string {
	var names []string
	for _, entries := range survey {
		for _, entry := range entries {
			names = append(names, entry.name)
		}
	}
	sort.Strings(names)
	return strings.Join(names, ",")
}
