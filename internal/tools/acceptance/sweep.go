package main

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/movingwoo/wfeature/internal/platform/detect"
)

// Pointing a sweep at a corpus this repository does not keep.
//
// By default the three corpus directories are the three a Host reads, and the
// platform of each is the directory it is in. That is the corpus a release is
// checked against, and it is deliberately fixed.
//
// It is not the only set of archives on a machine. A person investigating a
// platform accumulates a much larger diagnostic tree, filed under group
// directories somebody named by hand, and the counts over *that* tree are what
// decide which defect is worth fixing next. `checkgames` already has the shape
// of the answer: a `-games` flag that points the same scan at another root.
// This is the same flag on this tool, with three differences that follow from
// what a sweep is for.
//
// **The platform is what `detect.Classify` says, not what the folder is
// called.** A group directory's name is a note somebody typed; the bytes are
// the evidence, and this project has classified by content since before it had
// a picker. A sweep that trusted the folder name would file an archive under a
// ladder that cannot load it and report the loader's refusal as a defect.
//
// **A file no platform claimed is a record rather than a discard.** Half of
// what a sweep of an unsorted tree is for is the pile nothing ran: only one of
// the reasons behind that pile — a package of one of these shapes that was not
// recognised — is work this project can do, and the others (a locked package,
// a container of another format, a zip of whole packages) are not. Counting
// them together says a number about the tree; counting them apart says which
// number is ours. `detect.Reason` exists to make that division, and this is
// the first thing that spends it.
//
// **It recurses, and it keeps the group.** The tree files archives one level
// below the root under a group name, so a sweep that stopped at the root would
// find nothing at all. (`checkgames` stops at one level for the opposite
// reason: it answers what a Host can *start*, and an archive filed deeper
// cannot be started. What this asks — what does this build make of these bytes
// — does not change with the file's depth.) The group is kept because it is
// how a row is walked back to a file: two groups may hold the same file name,
// and a row that named only the file would name both.
//
// Nothing is excluded here. A tree holds deliberately broken files, bags of
// whole packages, and folders on their way out, and which of those is worth
// sweeping is the caller's judgment on the day rather than a list this file
// would have to be edited to change.

// corpusDir is one directory of archives and the platform whose ladder is
// asked of them. The default three are a platform each; a swept tree produces
// one per group directory per platform found in it, plus one for the files no
// platform claimed.
type corpusDir struct {
	// name is what a record files a row under and what the cache looks a row
	// up by, so it has to be stable against the rest of the corpus changing.
	// For the default three it is the platform, which is what every earlier
	// run wrote. For a swept group it is the directory and the platform, both
	// — a group that gains its first archive of a second platform must not
	// rename the rows of the first.
	name string
	// platform is whose ladder is climbed. Empty for the files nothing
	// claimed: there is no ladder to put them on, which is the finding.
	platform string
	// directory is what the report prints, relative to the repository root
	// when it is under it.
	directory string
	// read is the absolute directory the files are actually in.
	read string
	// staged says the probes cannot read `read` where it is and a staging
	// directory has to be built for them. See stagedCorpus.
	staged bool
}

// unclaimedPlatform is the corpus name suffix for the files no platform
// claimed. It is not a platform and never reaches a probe.
const unclaimedPlatform = "unclaimed"

// defaultCorpora is the corpus a release is checked against: the three
// directories a Host reads, each one a platform.
func defaultCorpora(platforms []string) []corpusDir {
	var corpora []corpusDir
	for _, platform := range platforms {
		corpora = append(corpora, corpusDir{
			name:      platform,
			platform:  platform,
			directory: corpus[platform],
		})
	}
	return corpora
}

// sweepTrees walks the named roots and divides what it finds into corpora by
// group directory and by what detection made of each file.
//
// The bytes are read once. The same read answers all three questions a sweep
// asks of a file — what platform claims it, what its digest is so an earlier
// answer can be carried forward, and how large it is — and reading it three
// times is how those three come to disagree.
func sweepTrees(repository string, roots, exclude []string) ([]corpusDir, map[string][]corpusEntry, error) {
	type group struct {
		directory string
		read      string
	}
	// Every file found, keyed by the group directory it is in and the
	// platform that claimed it.
	found := map[group]map[string][]corpusEntry{}
	order := []group{}

	for _, root := range roots {
		absolute := root
		if !filepath.IsAbs(absolute) {
			absolute = filepath.Join(repository, root)
		}
		info, err := os.Stat(absolute)
		if err != nil {
			return nil, nil, fmt.Errorf("sweep %s: %w", root, err)
		}
		if !info.IsDir() {
			return nil, nil, fmt.Errorf("sweep %s: not a directory", root)
		}
		walkErr := filepath.WalkDir(absolute, func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				// A directory that cannot be read is reported and stepped
				// over: a sweep of three hundred archives should not be lost
				// to one unreadable folder.
				fmt.Fprintf(os.Stderr, "acceptance: %v\n", err)
				return nil
			}
			if entry.IsDir() {
				if path != absolute && excluded(repository, absolute, path, exclude) {
					return fs.SkipDir
				}
				return nil
			}
			// A dot file is the operating system's, not the corpus's.
			if strings.HasPrefix(entry.Name(), ".") {
				return nil
			}
			directory := filepath.Dir(path)
			at := group{directory: displayPath(repository, directory), read: directory}
			if found[at] == nil {
				found[at] = map[string][]corpusEntry{}
				order = append(order, at)
			}
			file := corpusEntry{
				name:    entry.Name(),
				subtest: subtestName(entry.Name()),
				path:    path,
			}
			file.facts = readFacts(path)
			platform := file.facts.detected
			if platform == "" || platform == string(detect.Unknown) {
				platform = unclaimedPlatform
			}
			found[at][platform] = append(found[at][platform], file)
			return nil
		})
		if walkErr != nil {
			return nil, nil, fmt.Errorf("sweep %s: %w", root, walkErr)
		}
	}

	sort.Slice(order, func(one, two int) bool { return order[one].directory < order[two].directory })
	var corpora []corpusDir
	survey := map[string][]corpusEntry{}
	for _, at := range order {
		platforms := make([]string, 0, len(found[at]))
		for platform := range found[at] {
			platforms = append(platforms, platform)
		}
		sort.Strings(platforms)
		for _, platform := range platforms {
			entries := found[at][platform]
			sort.Slice(entries, func(one, two int) bool { return entries[one].name < entries[two].name })
			directory := corpusDir{
				name:      fmt.Sprintf("%s (%s)", at.directory, platform),
				directory: at.directory,
				read:      at.read,
				staged:    true,
			}
			if platform != unclaimedPlatform {
				directory.platform = platform
			} else {
				// Nothing runs for these, so nothing has to be staged.
				directory.staged = false
			}
			corpora = append(corpora, directory)
			survey[directory.name] = entries
		}
	}
	return corpora, survey, nil
}

// excluded reports whether a directory is one the caller asked not to descend
// into. Both forms are matched — the directory's own name and its path
// relative to the root being swept — so a folder that appears under several
// groups can be named once and a single one of them can be named exactly.
func excluded(repository, root, path string, exclude []string) bool {
	relative, err := filepath.Rel(root, path)
	if err != nil {
		relative = path
	}
	base := filepath.Base(path)
	display := displayPath(repository, path)
	for _, name := range exclude {
		name = strings.TrimSuffix(filepath.ToSlash(strings.TrimSpace(name)), "/")
		if name == "" {
			continue
		}
		if name == base || name == filepath.ToSlash(relative) || name == display {
			return true
		}
	}
	return false
}

// displayPath is how a path is written in a report: relative to the repository
// when it is inside it, the way `var/games/ktf` already is, and absolute
// otherwise.
func displayPath(repository, path string) string {
	relative, err := filepath.Rel(repository, path)
	if err != nil || strings.HasPrefix(relative, "..") {
		return filepath.ToSlash(path)
	}
	return filepath.ToSlash(relative)
}

// A shadow root: how a probe is pointed at a directory it does not know about.
//
// Each probe finds its corpus from its own source location — `runtime.Caller`,
// then up to the repository root, then `var/games/<platform>`. That is the
// right thing for a probe to do: a test that took a directory from the
// environment could be run against a corpus nobody recorded, and the corpus a
// release is checked against is not a parameter.
//
// So a sweep of another tree does not tell the probe where to look. It changes
// what "up to the repository root" reaches: a temporary module root whose
// every top-level entry is a symbolic link to this checkout's, except `var`,
// which the sweep owns. The probes compile from the same source and read a
// `var/games/<platform>` this tool filled in with links to the files it wants
// asked about.
//
// Two properties are the point of doing it this way:
//
//   - **The real corpus is never written to.** Nothing here creates, moves or
//     deletes anything under the checkout; the only directory it writes is one
//     it made in the temporary directory and removes when the run ends. A
//     sweep of a diagnostic tree must not be able to disturb the corpus a
//     release is checked against, and this cannot.
//   - **The build identity stays this checkout's.** Only `go test`'s working
//     directory moves. The revision, the diff and the untracked files that
//     identify what code answered are still read from the checkout, so a
//     carried row still means what it says.
//
// The links are per file rather than per directory because a group directory
// holds several platforms' archives and a probe reads one platform's.
type shadowRoot struct {
	root string
}

// newShadowRoot builds the temporary module root described above.
func newShadowRoot(repository string) (*shadowRoot, error) {
	root, err := os.MkdirTemp("", "wfeature-acceptance-")
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(repository)
	if err != nil {
		os.RemoveAll(root)
		return nil, err
	}
	for _, entry := range entries {
		// `var` is what the sweep is replacing, and the git directory is not
		// needed to compile: the checkout is asked about the build, not this.
		if entry.Name() == "var" || entry.Name() == ".git" {
			continue
		}
		if err := os.Symlink(filepath.Join(repository, entry.Name()), filepath.Join(root, entry.Name())); err != nil {
			os.RemoveAll(root)
			return nil, err
		}
	}
	return &shadowRoot{root: root}, nil
}

// stage fills in `var/games/<platform>` with links to the files a corpus holds,
// replacing whatever the previous corpus left there. One shadow root is reused
// across every corpus so the compiled test binaries stay cached; the staging
// directory is what changes between them.
func (shadow *shadowRoot) stage(platform string, entries []corpusEntry) error {
	directory := filepath.Join(shadow.root, "var", "games", platform)
	if err := os.RemoveAll(directory); err != nil {
		return err
	}
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return err
	}
	for _, entry := range entries {
		if err := os.Symlink(entry.path, filepath.Join(directory, entry.name)); err != nil {
			return err
		}
	}
	return nil
}

func (shadow *shadowRoot) close() {
	if shadow == nil {
		return
	}
	os.RemoveAll(shadow.root)
}

// dirOf is the working directory a stage's `go test` is run from: the shadow
// root for a swept corpus, and the checkout itself for the default three.
func (shadow *shadowRoot) dirOf(repository string, at corpusDir) string {
	if at.staged && shadow != nil {
		return shadow.root
	}
	return repository
}
