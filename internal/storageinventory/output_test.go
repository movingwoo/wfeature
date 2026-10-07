package storageinventory

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestRecordsAreOneRunTheArchivesAndTheSummary(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "not", "made", "yet")
	records := libraryRecords()
	run := testLayout.Run("TestProbe", 200, 150)
	path, err := testLayout.Write(directory, run, records, testLayout.Summarize(records))
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(path) != "storage-inventory-test.ndjson" {
		t.Errorf("the file is %s, want it named for its platform", path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var kinds []string
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		var line struct {
			Schema int    `json:"schema"`
			Kind   string `json:"kind"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &line); err != nil {
			t.Fatalf("a line is not a JSON object: %v\n%s", err, scanner.Text())
		}
		if line.Schema != Schema {
			t.Errorf("a %s line carries schema %d, want %d", line.Kind, line.Schema, Schema)
		}
		kinds = append(kinds, line.Kind)
	}
	if want := []string{RunKind, ArchiveKind, ArchiveKind, ArchiveKind, ArchiveKind, SummaryKind}; !reflect.DeepEqual(kinds, want) {
		t.Fatalf("lines = %v, want %v", kinds, want)
	}

	lines := bytes.Split(bytes.TrimSpace(data), []byte("\n"))
	var decodedRun RunRecord
	if err := json.Unmarshal(lines[0], &decodedRun); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(decodedRun, run) || decodedRun.Probe != "TestProbe" || decodedRun.Warm != 200 || decodedRun.Rounds != 150 ||
		!reflect.DeepEqual(decodedRun.Counters, testLayout.Counters) {
		t.Errorf("the run line read back as %+v, want %+v", decodedRun, run)
	}
	var first ArchiveRecord
	if err := json.Unmarshal(lines[1], &first); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, records[0]) {
		t.Errorf("the first archive read back as %+v, want %+v", first, records[0])
	}
	var summary SummaryRecord
	if err := json.Unmarshal(lines[len(lines)-1], &summary); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(summary, testLayout.Summarize(records)) {
		t.Errorf("the summary read back as %+v", summary)
	}

	// A second run of the same probe replaces the file rather than adding to
	// it: two runs in one file would be summed by whoever read it.
	if _, err := testLayout.Write(directory, run, nil, testLayout.Summarize(nil)); err != nil {
		t.Fatal(err)
	}
	data, err = os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := bytes.Count(data, []byte("\n")); got != 2 {
		t.Errorf("the rewritten file has %d lines, want the run and the summary", got)
	}
}

// repositoryRoot is the checkout this test is running from, found the way the
// refusal finds one.
func repositoryRoot(t *testing.T) string {
	t.Helper()
	working, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := resolve(working)
	if err != nil {
		t.Fatal(err)
	}
	root, inside := checkout(resolved)
	if !inside {
		t.Fatalf("the test's own directory %s is not inside a checkout of %s", resolved, modulePath())
	}
	return root
}

// A record names the archive it came from, so it is never written where it
// could be committed. The refusal covers the path as typed, a path that does
// not exist yet, and a link from outside that leads in.
func TestRecordsAreNeverWrittenUnderTheRepository(t *testing.T) {
	root := repositoryRoot(t)
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("the checkout root %s has no go.mod: %v", root, err)
	}
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(root, link); err != nil {
		t.Skipf("cannot make a symbolic link here: %v", err)
	}
	const made = "storage-inventory-refusal-probe"
	for name, directory := range map[string]string{
		"the root":                    root,
		"a directory not made yet":    filepath.Join(root, made, "out"),
		"a relative path":             made,
		"a link from outside":         link,
		"a new directory past a link": filepath.Join(link, made),
	} {
		if _, err := testLayout.Write(directory, testLayout.Run("TestProbe", 0, 0), nil, testLayout.Summarize(nil)); err == nil {
			t.Errorf("%s (%s) was written to", name, directory)
		} else if !strings.Contains(err.Error(), "never written under the repository") {
			t.Errorf("%s was refused without saying why: %v", name, err)
		}
		t.Setenv(OutputVariable, directory)
		if got, err := OutputDirectory(); err == nil {
			t.Errorf("%s was accepted as the output directory %q", name, got)
		}
	}
	for _, left := range []string{filepath.Join(root, made), made, filepath.Join(root, "storage-inventory-test.ndjson")} {
		if _, err := os.Lstat(left); err == nil {
			t.Errorf("a refused write left %s behind", left)
		}
	}
}

func TestOutputDirectoryIsOptional(t *testing.T) {
	t.Setenv(OutputVariable, "")
	if directory, err := OutputDirectory(); err != nil || directory != "" {
		t.Fatalf("with the variable unset the directory is %q, %v; want none", directory, err)
	}
	outside := filepath.Join(t.TempDir(), "records")
	t.Setenv(OutputVariable, outside)
	directory, err := OutputDirectory()
	if err != nil {
		t.Fatal(err)
	}
	// The answer is the resolved path, which on a system whose temporary
	// directory is itself a link differs from the one that was typed.
	want, err := resolve(outside)
	if err != nil {
		t.Fatal(err)
	}
	if directory != want {
		t.Errorf("directory = %q, want %q", directory, want)
	}
	if _, err := os.Stat(directory); err == nil {
		t.Error("asking where records go made the directory")
	}
}

func TestCountReadsANonNegativeNumber(t *testing.T) {
	const name = "WFEATURE_STORAGE_INVENTORY_TEST_COUNT"
	t.Setenv(name, "")
	if got, err := Count(name, 7); err != nil || got != 7 {
		t.Errorf("unset = %d, %v; want the fallback", got, err)
	}
	t.Setenv(name, "0")
	if got, err := Count(name, 7); err != nil || got != 0 {
		t.Errorf("0 = %d, %v; want 0, which is a count and not an absence", got, err)
	}
	for _, value := range []string{"-1", "many", "1.5"} {
		t.Setenv(name, value)
		if _, err := Count(name, 7); err == nil {
			t.Errorf("%q was read as a count", value)
		}
	}
}

func TestArchivesListsWhatADirectoryHolds(t *testing.T) {
	root := t.TempDir()
	first, second := filepath.Join(root, "first"), filepath.Join(root, "second")
	for _, path := range []string{
		filepath.Join(first, "b.zip"), filepath.Join(first, "a.ZIP"), filepath.Join(first, "notes.txt"),
		filepath.Join(first, "folder.zip", "inner.zip"), filepath.Join(second, "c.zip"),
	} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	missing := filepath.Join(root, "never-made")
	got := Archives([]string{second, missing, first}, "")
	want := []string{filepath.Join(first, "a.ZIP"), filepath.Join(first, "b.zip"), filepath.Join(second, "c.zip")}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("archives = %v, want %v", got, want)
	}
	if got := Archives([]string{first, second}, "b."); !reflect.DeepEqual(got, []string{filepath.Join(first, "b.zip")}) {
		t.Errorf("filtered archives = %v", got)
	}
	if got := Archives([]string{t.TempDir()}, ""); len(got) != 0 {
		t.Errorf("an empty directory holds %v", got)
	}

	// A name typed composed finds a file stored decomposed, and the other way
	// round.
	composed, decomposed := "가.zip", "가.zip"
	hangul := t.TempDir()
	if err := os.WriteFile(filepath.Join(hangul, decomposed), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if got := Archives([]string{hangul}, strings.TrimSuffix(composed, ".zip")); len(got) != 1 {
		t.Errorf("a composed filter found %v beside a decomposed name", got)
	}
}
