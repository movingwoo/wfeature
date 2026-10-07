package storageinventory

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strconv"
	"strings"

	"golang.org/x/text/unicode/norm"
)

// OutputVariable names the directory a probe writes its records to, one file
// per platform. Unset, a probe writes its test log and nothing else.
//
// **The directory is never inside a checkout of this repository.** A record
// names the archive it was taken from, and an archive's name is a game's name;
// the same rule keeps the acceptance sweep's records under the ignored var/.
// A probe has no ignored directory of its own to write to, so it refuses the
// repository outright rather than trusting a path somebody typed.
const OutputVariable = "WFEATURE_STORAGE_INVENTORY_OUT"

// RunRecord is the first line of a record file: which probe ran, how far it
// ran each archive, and what its counters are called, so that a file read
// later explains itself.
type RunRecord struct {
	Schema   int    `json:"schema"`
	Kind     string `json:"kind"`
	Platform string `json:"platform"`
	Probe    string `json:"probe"`
	// Warm is how many ticks an archive ran before its first boundary was
	// counted, and Rounds how many boundaries it was asked for.
	Warm     int      `json:"warm"`
	Rounds   int      `json:"rounds"`
	Counters []string `json:"counters"`
	Groups   []Group  `json:"groups,omitempty"`
	Limits   []Limit  `json:"limits,omitempty"`
	GOOS     string   `json:"goos"`
	GOARCH   string   `json:"goarch"`
	Go       string   `json:"go"`
}

// Run describes one probe's run over this layout.
func (layout Layout) Run(probe string, warm, rounds int) RunRecord {
	return RunRecord{
		Schema: Schema, Kind: RunKind, Platform: layout.Platform, Probe: probe,
		Warm: warm, Rounds: rounds,
		Counters: append([]string(nil), layout.Counters...),
		Groups:   append([]Group(nil), layout.Groups...),
		Limits:   append([]Limit(nil), layout.Limits...),
		GOOS:     runtime.GOOS, GOARCH: runtime.GOARCH, Go: runtime.Version(),
	}
}

// modulePath is this repository's module, taken from this package's own import
// path so that it cannot drift from go.mod.
func modulePath() string {
	return strings.TrimSuffix(reflect.TypeOf(Tally{}).PkgPath(), "/internal/storageinventory")
}

// resolve makes a path absolute and follows its symbolic links as far as the
// path exists, so that a link into a checkout is seen for what it is. What
// does not exist yet is appended as it was written.
func resolve(path string) (string, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	var missing []string
	for current := absolute; ; {
		resolved, err := filepath.EvalSymlinks(current)
		if err == nil {
			for index := len(missing) - 1; index >= 0; index-- {
				resolved = filepath.Join(resolved, missing[index])
			}
			return resolved, nil
		}
		if !errors.Is(err, fs.ErrNotExist) {
			return "", err
		}
		parent := filepath.Dir(current)
		if parent == current {
			return absolute, nil
		}
		missing = append(missing, filepath.Base(current))
		current = parent
	}
}

// checkout answers the root of the checkout of this module a directory is
// inside, if it is inside one. Any checkout counts, including a worktree other
// than the one the test binary was built from: what must not happen is a
// game's name landing where it could be committed.
func checkout(directory string) (string, bool) {
	module := modulePath()
	for current := directory; ; {
		if data, err := os.ReadFile(filepath.Join(current, "go.mod")); err == nil {
			for _, line := range strings.Split(string(data), "\n") {
				fields := strings.Fields(line)
				if len(fields) >= 2 && fields[0] == "module" && strings.Trim(fields[1], `"`) == module {
					return current, true
				}
			}
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", false
		}
		current = parent
	}
}

// OutputDirectory answers where records go: the resolved directory
// OutputVariable names, or nothing when it is unset. A probe asks before it
// runs anything, so a directory that will be refused costs no measurement.
func OutputDirectory() (string, error) {
	value := os.Getenv(OutputVariable)
	if value == "" {
		return "", nil
	}
	directory, err := resolve(value)
	if err != nil {
		return "", fmt.Errorf("%s=%q: %w", OutputVariable, value, err)
	}
	if root, inside := checkout(directory); inside {
		return "", fmt.Errorf("%s=%q is inside the checkout at %s; records name local archives and are never written under the repository", OutputVariable, value, root)
	}
	return directory, nil
}

// Write puts one platform's records in directory as NDJSON — the run, one
// line per archive in the order given, then the summary — and answers the
// file's path. It refuses a directory inside a checkout however it was
// arrived at, and replaces a file an earlier run of the same probe left.
func (layout Layout) Write(directory string, run RunRecord, records []ArchiveRecord, summary SummaryRecord) (string, error) {
	resolved, err := resolve(directory)
	if err != nil {
		return "", err
	}
	if root, inside := checkout(resolved); inside {
		return "", fmt.Errorf("storage inventory records are never written under the repository: %s is inside the checkout at %s", directory, root)
	}
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	lines := make([]any, 0, len(records)+2)
	lines = append(lines, run)
	for _, record := range records {
		lines = append(lines, record)
	}
	lines = append(lines, summary)
	for _, line := range lines {
		if err := encoder.Encode(line); err != nil {
			return "", err
		}
	}
	if err := os.MkdirAll(resolved, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(resolved, "storage-inventory-"+layout.Platform+".ndjson")
	if err := os.WriteFile(path, buffer.Bytes(), 0o644); err != nil {
		return "", err
	}
	return path, nil
}

// Count reads a non-negative count from the environment, or answers the
// fallback when the variable is unset.
func Count(name string, fallback int) (int, error) {
	value := os.Getenv(name)
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed < 0 {
		return 0, fmt.Errorf("%s=%q is not a count", name, value)
	}
	return parsed, nil
}

// Archives lists the .zip files directly inside each directory whose name
// contains the given text, sorted. A directory that is not there contributes
// nothing: a library is whatever a person has put in it.
//
// Both sides of the comparison are composed first. A file name read back from
// a filesystem may be decomposed where the one typed into a shell is not, and
// a filter that compared them as written would match nothing.
func Archives(directories []string, contains string) []string {
	wanted := norm.NFC.String(contains)
	var files []string
	for _, directory := range directories {
		entries, err := os.ReadDir(directory)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if entry.IsDir() || !strings.EqualFold(filepath.Ext(entry.Name()), ".zip") ||
				!strings.Contains(norm.NFC.String(entry.Name()), wanted) {
				continue
			}
			files = append(files, filepath.Join(directory, entry.Name()))
		}
	}
	sort.Strings(files)
	return files
}
