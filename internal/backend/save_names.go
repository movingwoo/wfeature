package backend

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"unicode"
	"unicode/utf8"
)

// Save keys on disk.
//
// A save key is a name a guest chose, and a guest's own storage kept names
// that differ only by case apart: "Save" and "save" are two record stores and
// two files. The key used to be its own path below the save directory, and a
// host file system that ignores case — macOS and iOS by default, Windows —
// opened one file for both, so there the two overwrote each other while on
// Linux they stayed apart. A name now means the same file on every host, and
// names that differ only by case are different files.
//
// So the store spells case out in the path. In each segment of a key, an
// upper-case letter is written as '#' followed by the letter, and '#' itself
// is written "##"; everything else is written as it is. A handset stored its
// own record store files this way (see internal/platform/skt/rms_packaged.go).
// A key with no upper-case letter and no '#' has exactly the path it always
// had. Keys do not change: platforms, save packs and the save API carry keys,
// and only this store turns one into a path. The owner directory a store is
// rooted at is the Host's name for a game, not a key, and is not encoded.
//
// A save directory an earlier build wrote holds its keys as raw paths. The
// first operation of this build on it converts them in place (see
// settleSaveNames). Nearly every name an earlier build wrote reads one way
// only: a name with no '#' is its own key under either reading, and an
// upper-case letter with no '#' before it, or a '#' before anything but '#' or
// such a letter, is not a name this encoding writes. A name like "#Top" reads
// both ways — the earlier key "#Top", or the key "Top" written here — and the
// file saveNamesRecord decides it: in a directory that holds it, names are
// encoded, and in one without it every name is the key it was written for.

// saveNameEncodes reports whether a rune is written with a '#' before it: an
// upper-case letter that is the upper case of its own lower case.
func saveNameEncodes(r rune) bool {
	return unicode.IsUpper(r) && unicode.ToUpper(unicode.ToLower(r)) == r
}

// encodeSaveName spells out the case of one segment of a key. Bytes that are
// not UTF-8 are kept as they are.
func encodeSaveName(name string) string {
	changed := false
	for index := 0; index < len(name) && !changed; {
		r, size := utf8.DecodeRuneInString(name[index:])
		changed = r == '#' || saveNameEncodes(r)
		index += size
	}
	if !changed {
		return name
	}
	var encoded strings.Builder
	encoded.Grow(len(name) + 8)
	for index := 0; index < len(name); {
		r, size := utf8.DecodeRuneInString(name[index:])
		switch {
		case r == '#':
			encoded.WriteString("##")
		case saveNameEncodes(r):
			encoded.WriteByte('#')
			encoded.WriteString(name[index : index+size])
		default:
			encoded.WriteString(name[index : index+size])
		}
		index += size
	}
	return encoded.String()
}

// decodeSaveName reverses encodeSaveName. A name it would not have written —
// an upper-case letter with no '#' before it, or a '#' before anything but '#'
// or such a letter — is not one of its names, and the answer says so.
func decodeSaveName(name string) (string, bool) {
	if !strings.ContainsRune(name, '#') {
		for _, r := range name {
			if saveNameEncodes(r) {
				return "", false
			}
		}
		return name, true
	}
	var decoded strings.Builder
	decoded.Grow(len(name))
	for index := 0; index < len(name); {
		r, size := utf8.DecodeRuneInString(name[index:])
		switch {
		case r == '#':
			if index+1 >= len(name) {
				return "", false
			}
			next, nextSize := utf8.DecodeRuneInString(name[index+1:])
			switch {
			case next == '#':
				decoded.WriteByte('#')
			case saveNameEncodes(next):
				decoded.WriteString(name[index+1 : index+1+nextSize])
			default:
				return "", false
			}
			index += 1 + nextSize
		case saveNameEncodes(r):
			return "", false
		default:
			decoded.WriteString(name[index : index+size])
			index += size
		}
	}
	return decoded.String(), true
}

// encodeSaveKey is the slash-separated path below a save directory a key is
// stored at.
func encodeSaveKey(key string) string {
	segments := strings.Split(key, "/")
	for index, segment := range segments {
		segments[index] = encodeSaveName(segment)
	}
	return strings.Join(segments, "/")
}

// earlierSaveSpelling reports whether a segment of a key could still be on
// disk under the name an earlier build gave it, and be told apart from an
// encoded name: the encoding changes it, and the encoding would not have
// written it.
func earlierSaveSpelling(segment string) bool {
	if encodeSaveName(segment) == segment {
		return false
	}
	_, encoded := decodeSaveName(segment)
	return !encoded
}

// saveNamesRecord is the file in a save directory that says its names are
// encoded. Its name is one the encoding never writes, and no spelling of it in
// another case is one either — its '#' is followed by a digit — so no key is
// ever stored over it, and walkers pass over it.
const saveNamesRecord = ".wfeature-names#1"

// saveNamesHeader is the record's first line. The second is its state; a
// conversion under way lists, one quoted path per line, the names it moves.
const saveNamesHeader = "wfeature save names 1"

// maxSaveNamesRecord bounds the record a store reads back: a conversion lists
// at most maxSnapshotSaveEntries paths of at most 512 bytes, quoted.
const maxSaveNamesRecord = maxSnapshotSaveEntries*(4*512+3) + 256

// maxSaveNameBytes is the longest name the file systems a Host runs on store.
// An earlier name whose encoding would be longer is left as it is.
const maxSaveNameBytes = 255

type saveNamesState uint8

const (
	saveNamesAbsent saveNamesState = iota
	saveNamesEncoded
	// A conversion first moves every name it converts to a temporary name,
	// then each temporary name to the encoded one. While names are temporary
	// none of them is mistaken for an earlier name or an encoded one, so a
	// conversion that stopped resumes from the record alone.
	saveNamesMoving
	saveNamesPlacing
)

var saveNamesStates = []struct {
	state saveNamesState
	word  string
}{
	{saveNamesEncoded, "encoded"},
	{saveNamesMoving, "converting: to temporary names"},
	{saveNamesPlacing, "converting: to encoded names"},
}

// saveNamesTemporary is the temporary name the entry at one index of a
// conversion takes. It is not a name the encoding writes either.
func saveNamesTemporary(index int) string {
	return saveNamesRecord + "-" + strconv.Itoa(index)
}

// saveNaming is how the names in one save directory read for one operation.
type saveNaming uint8

const (
	// namingPlain: the directory has no record. Every name in it is the key it
	// was written for, and none needs a '#'.
	namingPlain saveNaming = iota
	// namingEncoded: the directory has the record. A name the encoding would
	// not have written is one an earlier build wrote there afterwards, which is
	// its own key.
	namingEncoded
	// namingEarlier: the directory still holds names an earlier build wrote,
	// and they could not be converted, because it cannot be written. Every name
	// is the key it was written for, looked up by its exact spelling, and
	// nothing is written there.
	namingEarlier
)

// saveNames is the answer settleSaveNames gives for one operation.
type saveNames struct {
	naming saveNaming
	// blocked is why a directory reads as namingEarlier.
	blocked error
}

// The calls a conversion makes. They are variables so that a test can stand in
// for a process that stops partway.
var (
	saveNamesRename = os.Rename
	saveNamesWrite  = writeSaveFile
)

// settleSaveNames decides how the names in the store's directory read for one
// operation, converting an earlier build's names first when it can. Every
// operation on saves asks once, under the directory lock and after an
// interrupted replacement is settled.
//
// A directory with the record is encoded, and one with a conversion under way
// finishes it. A directory without the record is looked at whole. When no name
// in it needs a '#' it reads the same either way and is left exactly as it is:
// the record is written only when a key that needs one is stored. Otherwise
// its names are converted, which writes the record, and only a directory the
// conversion cannot write is read as the earlier build left it.
func (store *DirectorySaveStore) settleSaveNames() (saveNames, error) {
	if store == nil || store.root == "" {
		return saveNames{}, fmt.Errorf("save store has no root")
	}
	state, plan, err := readSaveNamesRecord(store.root)
	if err != nil {
		return saveNames{}, err
	}
	switch state {
	case saveNamesEncoded:
		return saveNames{naming: namingEncoded}, nil
	case saveNamesMoving, saveNamesPlacing:
		if err := convertSaveNames(store.root, plan, state); err != nil {
			return saveNames{}, fmt.Errorf("finish converting the save names in %s: %w", store.root, err)
		}
		return saveNames{naming: namingEncoded}, nil
	}
	info, err := os.Stat(store.root)
	if errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ENOTDIR) {
		return saveNames{naming: namingPlain}, nil
	}
	if err != nil {
		return saveNames{}, err
	}
	if !info.IsDir() {
		// Nothing below a file is a save, and every operation answers that as
		// it always did.
		return saveNames{naming: namingPlain}, nil
	}
	if plainSaveDirectoryKnown(store.root, info) {
		return saveNames{naming: namingPlain}, nil
	}
	plan, err = planSaveNames(store.root)
	if err != nil {
		return saveNames{naming: namingEarlier, blocked: err}, nil
	}
	if len(plan) == 0 {
		rememberPlainSaveDirectory(store.root, info)
		return saveNames{naming: namingPlain}, nil
	}
	if err := probeSaveNameDirectories(store.root, plan); err != nil {
		return saveNames{naming: namingEarlier, blocked: err}, nil
	}
	if err := writeSaveNamesRecord(store.root, saveNamesMoving, plan); err != nil {
		return saveNames{naming: namingEarlier, blocked: err}, nil
	}
	if err := convertSaveNames(store.root, plan, saveNamesMoving); err != nil {
		return saveNames{}, fmt.Errorf("convert the save names in %s: %w", store.root, err)
	}
	return saveNames{naming: namingEncoded}, nil
}

// encodeSaveNames makes a directory without the record an encoded one, which
// a write does before it stores the first name with a '#' in it. The
// directory is looked at again rather than trusted from an earlier look, and
// what an earlier build left in it is converted on the way.
func encodeSaveNames(root string) error {
	if err := os.MkdirAll(root, 0o755); err != nil {
		return err
	}
	plan, err := planSaveNames(root)
	if err != nil {
		return err
	}
	if len(plan) == 0 {
		return writeSaveNamesRecord(root, saveNamesEncoded, nil)
	}
	if err := probeSaveNameDirectories(root, plan); err != nil {
		return err
	}
	if err := writeSaveNamesRecord(root, saveNamesMoving, plan); err != nil {
		return err
	}
	return convertSaveNames(root, plan, saveNamesMoving)
}

// refuseEarlierSaveNames is the error a write into a namingEarlier directory
// answers.
func refuseEarlierSaveNames(root string, names saveNames) error {
	return fmt.Errorf("save directory %s holds names an earlier build wrote that could not be converted, so nothing is written there: %w", root, names.blocked)
}

// readSaveNamesRecord reads the record of one save directory. Anything at its
// name that is not a record this build wrote is refused rather than read as
// one: it may be a save an earlier build kept under that name, and a damaged
// record is no answer to how the names read.
func readSaveNamesRecord(root string) (saveNamesState, []string, error) {
	path := filepath.Join(root, saveNamesRecord)
	info, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ENOTDIR) {
		return saveNamesAbsent, nil, nil
	}
	if err != nil {
		return 0, nil, err
	}
	refused := fmt.Errorf("%s in save directory %s is not a record of how its names are written", saveNamesRecord, root)
	if !info.Mode().IsRegular() || info.Size() > maxSaveNamesRecord {
		return 0, nil, refused
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, nil, err
	}
	lines := strings.Split(string(data), "\n")
	if len(lines) < 3 || lines[0] != saveNamesHeader || lines[len(lines)-1] != "" {
		return 0, nil, refused
	}
	lines = lines[:len(lines)-1]
	state := saveNamesAbsent
	for _, known := range saveNamesStates {
		if lines[1] == known.word {
			state = known.state
		}
	}
	switch state {
	case saveNamesEncoded:
		if len(lines) != 2 {
			return 0, nil, refused
		}
		return state, nil, nil
	case saveNamesMoving, saveNamesPlacing:
		if len(lines) == 2 || len(lines)-2 > maxSnapshotSaveEntries {
			return 0, nil, refused
		}
		plan := make([]string, 0, len(lines)-2)
		for _, line := range lines[2:] {
			path, err := strconv.Unquote(line)
			if err != nil || !saveKeyShaped(path) || path == saveNamesRecord {
				return 0, nil, refused
			}
			plan = append(plan, path)
		}
		return state, plan, nil
	}
	return 0, nil, refused
}

func writeSaveNamesRecord(root string, state saveNamesState, plan []string) error {
	var record strings.Builder
	record.WriteString(saveNamesHeader + "\n")
	for _, known := range saveNamesStates {
		if known.state == state {
			record.WriteString(known.word + "\n")
		}
	}
	for _, path := range plan {
		record.WriteString(strconv.Quote(path) + "\n")
	}
	return saveNamesWrite(filepath.Join(root, saveNamesRecord), []byte(record.String()))
}

// saveKeyShaped reports whether a slash-separated path below a save directory
// is one a key can name.
func saveKeyShaped(path string) bool {
	key, err := NormalizeSaveKey(path)
	return err == nil && key == path
}

// planSaveNames lists, as slash-separated paths, the names in a directory
// without the record that the encoding changes: the names an earlier build
// wrote that a conversion moves. They are in the order the conversion moves
// them, everything inside a directory before the directory itself, so a path
// in the list stays valid until its own turn.
//
// What is not a key's path is left alone, and so is a temporary file a killed
// write left: neither is a save. An earlier name whose encoded name would be
// longer than a file system stores is left too, where it still reads as its
// own key; one that could be mistaken for an encoded name cannot be, and
// refuses the conversion instead.
func planSaveNames(root string) ([]string, error) {
	var plan []string
	visited := 0
	var visit func(relative string) error
	visit = func(relative string) error {
		entries, err := os.ReadDir(filepath.Join(root, filepath.FromSlash(relative)))
		if err != nil {
			return err
		}
		var here []string
		for _, entry := range entries {
			if visited++; visited > 2*maxSnapshotSaveEntries {
				return fmt.Errorf("save directory %s has more entries than a save tree may hold", root)
			}
			name := entry.Name()
			path := name
			if relative != "" {
				path = relative + "/" + name
			}
			if path == saveNamesRecord || !saveKeyShaped(path) {
				continue
			}
			if entry.IsDir() {
				if err := visit(path); err != nil {
					return err
				}
			}
			encoded := encodeSaveName(name)
			if encoded == name || saveTemporaryName(name) {
				continue
			}
			if len(encoded) > maxSaveNameBytes {
				if _, ambiguous := decodeSaveName(name); ambiguous {
					return fmt.Errorf("save name %q is too long to convert", path)
				}
				continue
			}
			here = append(here, path)
		}
		plan = append(plan, here...)
		if len(plan) > maxSnapshotSaveEntries {
			return fmt.Errorf("save directory %s has more names to convert than a save tree may hold", root)
		}
		return nil
	}
	if err := visit(""); err != nil {
		return nil, err
	}
	for index, path := range plan {
		temporary := filepath.Join(root, filepath.FromSlash(saveNameParent(path)), saveNamesTemporary(index))
		taken, err := saveNameExists(temporary)
		if err != nil {
			return nil, err
		}
		if taken {
			return nil, fmt.Errorf("save directory already holds %s", temporary)
		}
	}
	return plan, nil
}

// probeSaveNameDirectories asks every directory a conversion would write in
// whether it can be written, before anything moves. A conversion that stopped
// at a directory it cannot write would leave names half moved, and every later
// operation would refuse until somebody fixed that directory; asked first, the
// saves are read as they are instead.
func probeSaveNameDirectories(root string, plan []string) error {
	seen := map[string]bool{root: true}
	directories := []string{root}
	for _, path := range plan {
		directory := filepath.Join(root, filepath.FromSlash(saveNameParent(path)))
		if !seen[directory] {
			seen[directory] = true
			directories = append(directories, directory)
		}
	}
	for _, directory := range directories {
		// The probe has the shape of a temporary file, which every walk over
		// a save tree passes over.
		file, err := os.CreateTemp(directory, ".wfeature-probe.*")
		if err != nil {
			return err
		}
		name := file.Name()
		if err := errors.Join(file.Close(), os.Remove(name)); err != nil {
			return err
		}
	}
	return nil
}

// convertSaveNames carries out a conversion from the state its record is in
// and leaves the directory encoded. Each step asks the disk whether it is
// done, so a conversion that stopped anywhere resumes from its record.
//
// The first pass moves every listed name to its temporary name in the
// directory it is in; the second moves each temporary name to the encoded
// name. While a directory of the list is under its temporary name, the
// entries below it are reached through that name. Nothing is removed and
// nothing is written over: a name the encoded name is already taken by stops
// the conversion with the names where they are.
func convertSaveNames(root string, plan []string, state saveNamesState) error {
	if state == saveNamesMoving {
		touched := make(map[string]bool)
		for index, path := range plan {
			directory := filepath.Join(root, filepath.FromSlash(saveNameParent(path)))
			temporary := filepath.Join(directory, saveNamesTemporary(index))
			done, err := saveNameExists(temporary)
			if err != nil {
				return err
			}
			if done {
				continue
			}
			source := filepath.Join(directory, saveNameBase(path))
			present, err := saveNameExists(source)
			if err != nil {
				return err
			}
			// Not there under its own name: a directory above it has already
			// moved, and it moved before that directory did.
			if !present {
				continue
			}
			if err := saveNamesRename(source, temporary); err != nil {
				return err
			}
			touched[directory] = true
		}
		syncSaveNameDirectories(touched)
		if err := writeSaveNamesRecord(root, saveNamesPlacing, plan); err != nil {
			return err
		}
	}
	listed := make(map[string]int, len(plan))
	for index, path := range plan {
		listed[path] = index
	}
	touched := make(map[string]bool)
	for index, path := range plan {
		parts := strings.Split(path, "/")
		directory := root
		for depth := range len(parts) - 1 {
			if ancestor, moved := listed[strings.Join(parts[:depth+1], "/")]; moved {
				directory = filepath.Join(directory, saveNamesTemporary(ancestor))
			} else {
				directory = filepath.Join(directory, parts[depth])
			}
		}
		temporary := filepath.Join(directory, saveNamesTemporary(index))
		present, err := saveNameExists(temporary)
		if err != nil {
			return err
		}
		if !present {
			continue
		}
		destination := filepath.Join(directory, encodeSaveName(parts[len(parts)-1]))
		taken, err := saveNameExists(destination)
		if err != nil {
			return err
		}
		if taken {
			return fmt.Errorf("%s cannot take its encoded name: %s is already there", path, destination)
		}
		if err := saveNamesRename(temporary, destination); err != nil {
			return err
		}
		touched[directory] = true
	}
	syncSaveNameDirectories(touched)
	return writeSaveNamesRecord(root, saveNamesEncoded, nil)
}

// syncSaveNameDirectories flushes the directories a pass renamed in, so the
// record that follows never reaches the disk ahead of the names it describes.
func syncSaveNameDirectories(directories map[string]bool) {
	for directory := range directories {
		syncDirectory(directory)
	}
}

func saveNameParent(path string) string {
	if slash := strings.LastIndexByte(path, '/'); slash >= 0 {
		return path[:slash]
	}
	return ""
}

func saveNameBase(path string) string {
	return path[strings.LastIndexByte(path, '/')+1:]
}

// saveNameExists reports whether anything is at a path, without following a
// link at its end. A path below a file is not there.
func saveNameExists(path string) (bool, error) {
	_, err := os.Lstat(path)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ENOTDIR) {
		return false, nil
	}
	return false, err
}

// directoryNames lists one directory's names as the disk spells them. A path
// that is not a directory lists nothing.
func directoryNames(directory string) ([]string, error) {
	file, err := os.Open(directory)
	if errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ENOTDIR) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, nil
	}
	return file.Readdirnames(-1)
}

// spelledExactly reports whether a directory holds a name spelled exactly so.
// A file system that ignores case answers a lookup of "Save" with the file
// "save", so a lookup alone cannot say which name is there; the listing can.
// The lookup goes first, because a name nothing answers to is not there at all.
func spelledExactly(directory, name string) (bool, error) {
	present, err := saveNameExists(filepath.Join(directory, name))
	if err != nil || !present {
		return false, err
	}
	names, err := directoryNames(directory)
	if err != nil {
		return false, err
	}
	return slices.Contains(names, name), nil
}

// exactSavePath finds a key in a directory read as an earlier build left it:
// each segment is the name exactly as written.
func exactSavePath(root string, segments []string) (string, bool, error) {
	path := root
	for _, segment := range segments {
		present, err := spelledExactly(path, segment)
		if err != nil || !present {
			return "", false, err
		}
		path = filepath.Join(path, segment)
	}
	return path, true, nil
}

// locateSave answers the path an operation reads one key's entry at, or false
// when a directory read as an earlier build left it has no such entry.
//
// In an encoded directory, an entry still under the name an earlier build
// gave it — written there by an earlier build after this one converted the
// directory, or copied in by hand — is moved to its encoded name first. A
// write writes the encoded path whether or not the move succeeded; a read of
// an entry that could not be moved reads it where it is.
func (store *DirectorySaveStore) locateSave(name string, names saveNames, write bool) (string, bool, error) {
	key, err := NormalizeSaveKey(name)
	if err != nil {
		return "", false, err
	}
	segments := strings.Split(key, "/")
	if names.naming == namingEarlier {
		return exactSavePath(store.root, segments)
	}
	encodedKey := encodeSaveKey(key)
	path := filepath.Join(store.root, filepath.FromSlash(encodedKey))
	if encodedKey == key || (names.naming == namingEncoded && !slices.ContainsFunc(segments, earlierSaveSpelling)) {
		return path, true, nil
	}
	// An encoded path the system cannot even look up — longer than it stores —
	// is not there either, and an earlier name left unconverted for that
	// reason is found below.
	if present, _ := saveNameExists(path); present {
		return path, true, nil
	}
	if names.naming == namingPlain {
		// Every name in a directory without the record is its own key, and the
		// only ones a '#' would change are those a conversion left as they
		// were.
		if earlier, found, _ := exactSavePath(store.root, segments); found {
			return earlier, true, nil
		}
		return path, true, nil
	}
	earlier, found := findEarlierSave(store.root, segments)
	if !found {
		return path, true, nil
	}
	if err := moveEarlierSave(store.root, segments, earlier); err != nil && !write {
		return earlier, true, nil
	}
	return path, true, nil
}

// findEarlierSave looks for one key's entry under the spellings an earlier
// build could have left in an encoded directory: at each segment the encoded
// name first, and then the earlier one, spelled exactly. Looking is a courtesy
// to a name nobody should have put there, so a path that cannot be looked at
// is passed over, and the operation is left to the encoded path as for any
// other key.
func findEarlierSave(root string, segments []string) (string, bool) {
	var search func(directory string, depth int, encoded bool) (string, bool)
	search = func(directory string, depth int, encoded bool) (string, bool) {
		segment := segments[depth]
		spellings := []string{encodeSaveName(segment)}
		if earlierSaveSpelling(segment) {
			spellings = append(spellings, segment)
		}
		for index, spelling := range spellings {
			var present bool
			if index == 0 {
				present, _ = saveNameExists(filepath.Join(directory, spelling))
			} else {
				present, _ = spelledExactly(directory, spelling)
			}
			if !present {
				continue
			}
			path := filepath.Join(directory, spelling)
			allEncoded := encoded && index == 0
			if depth == len(segments)-1 {
				// The encoded path itself was asked for before the search.
				if info, err := os.Lstat(path); !allEncoded && err == nil && !info.IsDir() {
					return path, true
				}
				continue
			}
			if info, err := os.Stat(path); err != nil || !info.IsDir() {
				continue
			}
			if found, ok := search(path, depth+1, allEncoded); ok {
				return found, true
			}
		}
		return "", false
	}
	return search(root, 0, true)
}

// moveEarlierSave gives an entry found under an earlier spelling its encoded
// path. A directory on the way that is still under its earlier name is renamed
// whole, which converts the names of everything below it at once. Where a
// directory of the encoded name is already there, the entry moves into it on
// its own, and the earlier directories that leaves empty are removed; a
// directory that still holds anything stays. Nothing is written over.
func moveEarlierSave(root string, segments []string, earlier string) error {
	relative, err := filepath.Rel(root, earlier)
	if err != nil {
		return err
	}
	spelled := strings.Split(filepath.ToSlash(relative), "/")
	if len(spelled) != len(segments) {
		return fmt.Errorf("save %s is not at the depth of its key", earlier)
	}
	directory := root
	for depth, segment := range segments {
		encoded := encodeSaveName(segment)
		target := filepath.Join(directory, encoded)
		if spelled[depth] == encoded {
			directory = target
			continue
		}
		current := filepath.Join(directory, spelled[depth])
		taken, err := saveNameExists(target)
		if err != nil {
			return err
		}
		if !taken {
			if err := os.Rename(current, target); err != nil {
				return err
			}
			syncDirectory(directory)
			directory = target
			continue
		}
		last := depth == len(segments)-1
		if last {
			return fmt.Errorf("save %s is already there", target)
		}
		// The encoded directory must be the one spelled so, not another name
		// this file system takes for it.
		if exact, err := spelledExactly(directory, encoded); err != nil || !exact {
			return errors.Join(err, fmt.Errorf("%s is another name to this file system", target))
		}
		from := filepath.Join(append([]string{current}, spelled[depth+1:]...)...)
		to := filepath.Join(target, filepath.FromSlash(encodeSaveKey(strings.Join(segments[depth+1:], "/"))))
		if taken, err := saveNameExists(to); err != nil || taken {
			return errors.Join(err, fmt.Errorf("save %s is already there", to))
		}
		if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
			return err
		}
		if err := os.Rename(from, to); err != nil {
			return err
		}
		syncDirectory(filepath.Dir(to))
		syncDirectory(filepath.Dir(from))
		for emptied := filepath.Dir(from); emptied != directory; emptied = filepath.Dir(emptied) {
			if os.Remove(emptied) != nil {
				break
			}
		}
		return nil
	}
	return nil
}

// saveEntryKey is the key a path below a save directory is the entry of, and
// whether the path is that key's encoded path. In an encoded directory each
// name is decoded, and a name the encoding would not have written is one an
// earlier build wrote there, which is its own key; elsewhere every name is.
// A path no key can name is an error.
func saveEntryKey(relative string, naming saveNaming) (string, bool, error) {
	segments := strings.Split(relative, "/")
	encoded := true
	if naming == namingEncoded {
		for index, segment := range segments {
			if decoded, ok := decodeSaveName(segment); ok {
				segments[index] = decoded
			} else {
				encoded = false
			}
		}
	}
	key := strings.Join(segments, "/")
	if !saveKeyShaped(key) {
		return "", false, fmt.Errorf("save path %q names no key", relative)
	}
	return key, encoded, nil
}

// saveEntryChoice keeps, for each key a walk meets, the path its entry is read
// from. Two paths meet at one key only when an earlier build's name was left
// beside the encoded one; the encoded path is the entry, and the other is left
// where it is, neither read nor removed.
type saveEntryChoice map[string]saveEntryAt

type saveEntryAt struct {
	path    string
	encoded bool
}

// offer reports whether the path is now the one kept for the key.
func (choice saveEntryChoice) offer(key, path string, encoded bool) bool {
	if kept, seen := choice[key]; seen && (kept.encoded || !encoded) {
		return false
	}
	choice[key] = saveEntryAt{path: path, encoded: encoded}
	return true
}

// keys answers the keys chosen, in order.
func (choice saveEntryChoice) keys() []string {
	keys := make([]string, 0, len(choice))
	for key := range choice {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// A directory found without a name to convert stays without one until a write
// stores a name that needs a '#', and that write writes the record first. So
// a look that found nothing to convert holds for the directory that was looked
// at, which is remembered by its identity and its modification time: a
// directory put in its place, or one whose own entries changed, is looked at
// again. This keeps every operation on a directory without the record from
// walking all of it.
var plainSaveDirectories = struct {
	sync.Mutex
	known map[string]os.FileInfo
}{known: make(map[string]os.FileInfo)}

func plainSaveDirectoryKnown(root string, info os.FileInfo) bool {
	absolute, err := filepath.Abs(root)
	if err != nil {
		return false
	}
	plainSaveDirectories.Lock()
	defer plainSaveDirectories.Unlock()
	known := plainSaveDirectories.known[absolute]
	return known != nil && os.SameFile(known, info) && known.ModTime().Equal(info.ModTime())
}

func rememberPlainSaveDirectory(root string, info os.FileInfo) {
	absolute, err := filepath.Abs(root)
	if err != nil {
		return
	}
	plainSaveDirectories.Lock()
	defer plainSaveDirectories.Unlock()
	plainSaveDirectories.known[absolute] = info
}
