package ktf

import (
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// Importing an external emulator's save data
//
// One other emulator of these games keeps browser saves in
// one tree per scope, `savedata/<scope>/<owner>/…`, where `fs` owners are
// archive AIDs and `db` owners are PIDs — its database repository is opened
// with the app's PID, its filesystem with the AID. Inside a `db` owner every
// *record* is its own file named `<database><record id>`, percent-encoded so a
// database called "data/XlsItem.zt1" stays one flat name.
//
// wfeature keys everything by PID (see SaveOwner) and splits scopes below it
// instead: `<save root>/<PID>/<db|jdb|fs>/<name>`, with a WIPI C database as
// one file holding its single record and a Java DataBase as one file holding
// every record (encodeSaveRecords). Neither the directory shape nor the file
// names line up, so copying the external tree in place leaves every save
// invisible; this importer performs the translation.
//
// The AID-keyed `fs` scope is the one lossy direction: when several games
// share an AID, the source already mixed their guest files in one directory
// and nothing in the file says which game wrote what. The importer resolves
// such an owner by asking which candidate archive packages files of those
// names, and reports the owner rather than guessing when that does not decide
// it.

// ImportedSave records one converted entry, in wfeature terms.
type ImportedSave struct {
	Source string // path under the external save root
	Owner  string // save root directory the entry landed in
	Key    string // save key, e.g. "db/save0.dat"
	Bytes  int
}

// ImportReport is the outcome of one import: what moved and what did not.
type ImportReport struct {
	Imported []ImportedSave
	Skipped  []string
}

// skip appends one human-readable reason an entry stayed behind.
func (report *ImportReport) skip(format string, arguments ...any) {
	report.Skipped = append(report.Skipped, fmt.Sprintf(format, arguments...))
}

// GameIdentity is one archive's identity and where it lives, so an importer
// can reopen it when the descriptor alone does not settle a question.
type GameIdentity struct {
	Descriptor Descriptor
	Path       string
}

// GameIdentities collects every KTF archive under root. It is how the importer
// resolves an external owner — a PID for databases, an AID for guest files — to
// the directory wfeature stores that game's saves under.
func GameIdentities(root string) ([]GameIdentity, error) {
	var identities []GameIdentity
	seen := make(map[string]bool)
	walkErr := filepath.WalkDir(root, func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.EqualFold(filepath.Ext(name), ".zip") {
			return nil
		}
		data, readErr := os.ReadFile(name)
		if readErr != nil {
			return readErr
		}
		descriptor, parseErr := ReadDescriptor(data)
		if parseErr != nil {
			// Only KTF archives carry a descriptor; other platforms' archives
			// sit beside them and are not an error here.
			return nil
		}
		// Variants of one title (a game shipped again with modified drop
		// rates, say) repeat an identity; one archive per identity is enough
		// to answer with.
		identity := descriptor.AID + "/" + descriptor.PID
		if seen[identity] {
			return nil
		}
		seen[identity] = true
		identities = append(identities, GameIdentity{Descriptor: descriptor, Path: name})
		return nil
	})
	if walkErr != nil {
		return nil, walkErr
	}
	return identities, nil
}

// ImportOptions configures ImportExternalSaves.
type ImportOptions struct {
	// SourceRoot is the external savedata directory, the parent of fs/ and db/.
	SourceRoot string
	// SaveRoot is wfeature's save root, the parent of the per-game directories.
	SaveRoot string
	// Identities are the archives an external owner can be resolved against.
	Identities []GameIdentity
	// DryRun reports what would be written without touching the save root.
	DryRun bool
}

// ImportExternalSaves converts an external emulator's save tree into the layout
// wfeature's DirectorySaveStore reads. Entries it cannot place are reported
// rather than guessed at, so an incomplete import is visible instead of silent.
func ImportExternalSaves(options ImportOptions) (ImportReport, error) {
	report := ImportReport{}
	if options.SourceRoot == "" || options.SaveRoot == "" {
		return report, fmt.Errorf("import needs both a source root and a save root")
	}
	if err := importFileScope(options, &report); err != nil {
		return report, err
	}
	if err := importDatabaseScope(options, &report); err != nil {
		return report, err
	}
	sort.Slice(report.Imported, func(left, right int) bool {
		if report.Imported[left].Owner != report.Imported[right].Owner {
			return report.Imported[left].Owner < report.Imported[right].Owner
		}
		return report.Imported[left].Key < report.Imported[right].Key
	})
	sort.Strings(report.Skipped)
	return report, nil
}

// importFileScope moves `fs/<AID>/<path>` across. The key suffix is already
// wfeature's, but the owner is not: the source keys guest files by AID, and
// several games can share one, so the AID has to be resolved to the game that
// wrote the files.
func importFileScope(options ImportOptions, report *ImportReport) error {
	scopeRoot := filepath.Join(options.SourceRoot, "fs")
	owners, err := readOwners(scopeRoot)
	if err != nil {
		return err
	}
	for _, aid := range owners {
		ownerRoot := filepath.Join(scopeRoot, aid)
		files, walkErr := collectFiles(ownerRoot)
		if walkErr != nil {
			return walkErr
		}
		if len(files) == 0 {
			continue
		}
		owner, resolveErr := resolveFileOwner(options.Identities, aid, files)
		if resolveErr != nil {
			report.skip("fs/%s: %v", aid, resolveErr)
			continue
		}
		for _, relative := range files {
			data, readErr := os.ReadFile(filepath.Join(ownerRoot, filepath.FromSlash(relative)))
			if readErr != nil {
				return readErr
			}
			source := filepath.ToSlash(filepath.Join("fs", aid)) + "/" + relative
			if err := writeImported(options, report, source, owner, "fs/"+relative, data); err != nil {
				return err
			}
		}
	}
	return nil
}

// resolveFileOwner names the game an external `fs` owner's files belong to. One
// archive with that AID answers it outright. When several share the AID, the
// files decide: a save shadows the packaged file of the same name, so the
// archive that ships the most of these names is the game that was writing
// them. Nothing to go on stays an error rather than a guess.
func resolveFileOwner(identities []GameIdentity, aid string, files []string) (string, error) {
	var candidates []GameIdentity
	for _, identity := range identities {
		if identity.Descriptor.AID == aid {
			candidates = append(candidates, identity)
		}
	}
	switch len(candidates) {
	case 0:
		return "", fmt.Errorf("no KTF archive carries AID %s (%d files)", aid, len(files))
	case 1:
		return SaveOwner(candidates[0].Descriptor), nil
	}
	best, bestScore, tied := "", 0, false
	var names []string
	for _, candidate := range candidates {
		packaged, err := packagedFileNames(candidate.Path)
		if err != nil {
			continue
		}
		score := 0
		for _, file := range files {
			if packaged[file] {
				score++
			}
		}
		names = append(names, fmt.Sprintf("%s (%d/%d)", SaveOwner(candidate.Descriptor), score, len(files)))
		switch {
		case score > bestScore:
			best, bestScore, tied = SaveOwner(candidate.Descriptor), score, false
		case score == bestScore && score > 0:
			tied = true
		}
	}
	if bestScore == 0 || tied {
		return "", fmt.Errorf("AID %s is shared and its %d files do not name one game — candidates %s", aid, len(files), strings.Join(names, ", "))
	}
	return best, nil
}

// packagedFileNames is the guest filesystem view of one archive: the names a
// save can shadow.
func packagedFileNames(path string) (map[string]bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	archive, err := Open(data)
	if err != nil {
		return nil, err
	}
	names := make(map[string]bool)
	for name := range archive.GuestFiles() {
		names[name] = true
	}
	return names, nil
}

// importDatabaseScope rebuilds databases from the source's one-file-per-record
// names. A database with several records can only be a Java DataBase, so it
// lands in "jdb/"; a single-record database is written to both "db/" and
// "jdb/", because the source file says nothing about which API wrote it and the
// two scopes never collide — whichever one the game opens finds its save.
func importDatabaseScope(options ImportOptions, report *ImportReport) error {
	scopeRoot := filepath.Join(options.SourceRoot, "db")
	owners, err := readOwners(scopeRoot)
	if err != nil {
		return err
	}
	for _, pid := range owners {
		ownerRoot := filepath.Join(scopeRoot, pid)
		files, walkErr := collectFiles(ownerRoot)
		if walkErr != nil {
			return walkErr
		}
		if len(files) == 0 {
			continue
		}
		// A database owner is already the PID this project keys saves by, so
		// the archive only has to confirm the game is present.
		known := false
		for _, identity := range options.Identities {
			if identity.Descriptor.PID == pid {
				known = true
				break
			}
		}
		if !known {
			report.skip("db/%s: no KTF archive carries PID %s (%d files)", pid, pid, len(files))
			continue
		}
		databases, warnings := groupRecords(files)
		for _, warning := range warnings {
			report.skip("db/%s: %s", pid, warning)
		}
		for _, name := range sortedNames(databases) {
			slots := databases[name]
			records := make([][]byte, len(slots))
			for index, file := range slots {
				if file == "" {
					continue
				}
				data, readErr := os.ReadFile(filepath.Join(ownerRoot, filepath.FromSlash(file)))
				if readErr != nil {
					return readErr
				}
				records[index] = data
			}
			source := filepath.ToSlash(filepath.Join("db", pid)) + "/" + name + "<record>"
			if err := writeImported(options, report, source, pid, "jdb/"+name, encodeSaveRecords(records)); err != nil {
				return err
			}
			if len(records) == 1 && records[0] != nil {
				if err := writeImported(options, report, source, pid, "db/"+name, records[0]); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// writeImported persists one converted entry through the same store the
// runtime reads with, so key validation and directory creation cannot drift.
func writeImported(options ImportOptions, report *ImportReport, source, owner, key string, data []byte) error {
	report.Imported = append(report.Imported, ImportedSave{Source: source, Owner: owner, Key: key, Bytes: len(data)})
	if options.DryRun {
		return nil
	}
	store := NewDirectorySaveStore(filepath.Join(options.SaveRoot, owner))
	if err := store.StoreSave(key, data); err != nil {
		return fmt.Errorf("write %s/%s: %w", owner, key, err)
	}
	return nil
}

// recordSplit is one reading of an external store key as a database name and
// the one-based record identifier glued to it.
type recordSplit struct {
	name string
	id   int
}

// recordSplits enumerates every reading of a key, shortest identifier first.
// "NOM210" reads as either NOM21 record 0 — impossible, identifiers start at
// one — or NOM2 record 10.
func recordSplits(key string) []recordSplit {
	var splits []recordSplit
	for width := 1; width < len(key); width++ {
		name, id, ok := splitRecordKey(key, width)
		if !ok {
			break
		}
		// The source writes identifiers without padding, so a leading zero
		// means the digit belongs to the name, not to this reading of the
		// identifier.
		if id < 1 || (width > 1 && key[len(key)-width] == '0') {
			continue
		}
		splits = append(splits, recordSplit{name: name, id: id})
	}
	return splits
}

// groupRecords turns the source's `<database><record id>` file names back into
// databases and their record slots, each slot holding the file that carries
// that record or "" where a record was deleted. The name and the identifier
// run together, so a key alone is ambiguous once identifiers reach ten; the
// records of one database break the tie, and each key goes to the database
// most of its siblings agree on, shortest identifier first among equals.
func groupRecords(files []string) (map[string][]string, []string) {
	type record struct {
		file   string
		key    string
		splits []recordSplit
		chosen int
	}
	parsed := make([]record, 0, len(files))
	var warnings []string
	for _, file := range files {
		key, err := url.PathUnescape(file)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("%s is not a percent-encoded database key", file))
			continue
		}
		splits := recordSplits(key)
		if len(splits) == 0 {
			warnings = append(warnings, fmt.Sprintf("%s does not end in a record identifier", key))
			continue
		}
		parsed = append(parsed, record{file: file, key: key, splits: splits})
	}

	// Two rounds settle it: the first counts the shortest reading of every
	// key, the second moves each key to whichever of its readings the counted
	// names support best.
	for round := 0; round < 2; round++ {
		counts := make(map[string]int, len(parsed))
		for _, entry := range parsed {
			counts[entry.splits[entry.chosen].name]++
		}
		for index, entry := range parsed {
			best := entry.chosen
			for candidate, split := range entry.splits {
				if counts[split.name] > counts[entry.splits[best].name] {
					best = candidate
				}
			}
			parsed[index].chosen = best
		}
	}

	databases := make(map[string][]string)
	for _, entry := range parsed {
		split := entry.splits[entry.chosen]
		if split.id > maxDataBaseRecords {
			warnings = append(warnings, fmt.Sprintf("%s has record identifier %d, above the limit", entry.key, split.id))
			continue
		}
		slots := databases[split.name]
		for len(slots) < split.id {
			slots = append(slots, "")
		}
		slots[split.id-1] = entry.file
		databases[split.name] = slots
	}
	return databases, warnings
}

// splitRecordKey cuts an external store key into the database name and the record
// identifier carried by its last `width` digits.
func splitRecordKey(key string, width int) (string, int, bool) {
	if len(key) <= width {
		return "", 0, false
	}
	digits := key[len(key)-width:]
	for index := 0; index < len(digits); index++ {
		if digits[index] < '0' || digits[index] > '9' {
			return "", 0, false
		}
	}
	id, err := strconv.Atoi(digits)
	if err != nil {
		return "", 0, false
	}
	return key[:len(key)-width], id, true
}

// readOwners lists the owner directories of one external scope; a missing scope is
// simply empty.
func readOwners(scopeRoot string) ([]string, error) {
	entries, err := os.ReadDir(scopeRoot)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	owners := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			owners = append(owners, entry.Name())
		}
	}
	sort.Strings(owners)
	return owners, nil
}

// collectFiles lists every file below root, as slash-separated paths relative
// to it.
func collectFiles(root string) ([]string, error) {
	var files []string
	err := filepath.WalkDir(root, func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		relative, relErr := filepath.Rel(root, name)
		if relErr != nil {
			return relErr
		}
		files = append(files, filepath.ToSlash(relative))
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(files)
	return files, nil
}

// sortedNames orders database names so an import reports in a stable order.
func sortedNames(databases map[string][]string) []string {
	names := make([]string, 0, len(databases))
	for name := range databases {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
