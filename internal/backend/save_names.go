package backend

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

// Save keys on disk.
//
// A save key is a name a guest chose, and a guest's own storage kept names
// that differ only by case apart: "Save" and "save" are two record stores and
// two files. The key used to be its own path below the save directory, and a
// host file system that ignores case — macOS and iOS by default, Windows —
// opened one file for both, so there the two overwrote each other while on
// Linux they stayed apart. A name now means the same file on every host, and
// two keys are two files.
//
// So the store spells out in the path what a host would fold. In each segment
// of a key, an upper-case letter is written as '#' followed by the letter, and
// '#' itself is written "##". What a host would take for another character, or
// refuse, is written by its number, '#' '+' and six lower-case hex digits:
//
//   - any other letter outside ASCII that has case: file systems that ignore
//     case fold such letters in more ways than upper to lower — APFS takes "ß"
//     for "ss", the ligature "ﬁ" for "fi", the Kelvin sign for "k" and "ς" for
//     "σ" — and a name kept apart only by one of them would be one file there;
//   - a character outside ASCII that Unicode normalization would change, or
//     join to the one before it (see saveNameComposed): APFS and HFS+ ignore
//     normalization, so "é" and "e" with a combining acute, ";" and the Greek
//     question mark, or "가" and its two jamo are one file there;
//   - what Windows refuses or changes in a name: the controls below 0x20 and
//     <>:"\|?* anywhere, a '.' or ' ' at the end of a segment, which it drops,
//     and the first letter of a device name such as "con" or "com1.txt" (see
//     saveNameDevice);
//   - a '~' before a digit: NTFS gives a file a short name of that shape,
//     "SAVEFI~1.DAT" for "savefile.dat", and a lookup of the short name finds
//     the file it belongs to.
//
// A byte that is not UTF-8 is written as '#' '-' and two lower-case hex
// digits: macOS refuses such a name, and Windows turns the byte into U+FFFD,
// so two names would be one there. Everything else is written as it is: the
// rest of ASCII, and characters outside it that have no case and that
// normalization leaves alone — Hangul syllables, CJK ideographs and kana among
// them. A handset stored its own record store files with the '#' for upper
// case (see internal/platform/skt/rms_packaged.go). A key none of whose
// characters is changed has exactly the path it always had. Keys do not
// change: platforms, save packs and the save API carry keys, and only this
// store turns one into a path. The owner directory a store is rooted at is the
// Host's name for a game, not a key, and is not encoded.
//
// A name reads back as a key only when that key is written as exactly that
// name (see decodeSaveName), so a key has one name and a name at most one key.
// A key one of whose names, written out, is longer than a file system stores
// has no save and is refused one (see saveNamesFit).
//
// A save directory an earlier build wrote holds its keys as raw paths. The
// first operation of this build on it converts them in place (see
// settleSaveNames). Nearly every name an earlier build wrote reads one way
// only: a name the encoding leaves as it is is its own key under either
// reading, and a name holding bare a character the encoding changes, or a '#'
// before anything the encoding does not write after one, is not a name this
// encoding writes. A name like "#Top" reads both ways — the earlier key
// "#Top", or the key "Top" written here — and the file saveNamesRecord decides
// it: in a directory that holds it, names are encoded, and in one without it
// every name is the key it was written for.
//
// A build before this one wrote the record too, with a first form of the
// encoding that numbered only letters with case (firstSaveNameForm). In an
// encoded directory its names that this form writes otherwise read as the
// keys that form wrote them for, and they move to this form's names when they
// are read, as a name an earlier build wrote there does (see locateSave).

// saveNameForm is one form of the encoding: the runes it writes with a '#'
// before them, the runes it writes by their number, which may depend on where
// in its segment a rune stands, and whether it writes a byte that is not UTF-8
// by its number too. '#' is written "##", and every other rune as it is.
type saveNameForm struct {
	marked   func(r rune) bool
	numbered func(name string, index int, r rune, size int) bool
	bytes    bool
}

// currentSaveNameForm is the form this build writes.
var currentSaveNameForm = saveNameForm{marked: saveNameEncodes, numbered: saveNameNumbered, bytes: true}

// firstSaveNameForm is the form a build before this one wrote, under the same
// record: an upper-case letter with a '#' before it whether normalization
// keeps the letter or not, any other letter with case outside ASCII by its
// number, and everything else as it is.
var firstSaveNameForm = saveNameForm{marked: firstSaveNameMarked, numbered: firstSaveNameNumbered}

func firstSaveNameMarked(r rune) bool {
	return unicode.IsUpper(r) && unicode.ToUpper(unicode.ToLower(r)) == r
}

func firstSaveNameNumbered(_ string, _ int, r rune, _ int) bool {
	return r >= utf8.RuneSelf && r != utf8.RuneError && !firstSaveNameMarked(r) && saveNameCased(r)
}

// saveNameEncodes reports whether a rune is written with a '#' before it: an
// upper-case letter that is the upper case of its own lower case and, outside
// ASCII, one normalization leaves as it is. Seven Greek capitals with an
// accent, U+1FBB among them, are not: each is the other spelling of a capital
// with the tonos, and normalization turns one into the other.
func saveNameEncodes(r rune) bool {
	return firstSaveNameMarked(r) && (r < utf8.RuneSelf || saveNameComposed(r))
}

// saveNameCased reports whether a rune has case: it is upper, lower or title
// case, or folds to another letter.
func saveNameCased(r rune) bool {
	return unicode.IsUpper(r) || unicode.IsLower(r) || unicode.IsTitle(r) || unicode.SimpleFold(r) != r
}

// saveNameComposed reports whether Unicode normalization leaves a rune
// outside ASCII as it is wherever it stands: the rune is its own NFC form,
// which a singleton such as the Greek question mark (NFC makes it ";"), the
// ohm sign or a CJK compatibility ideograph is not, and it does not combine
// with a rune before it, as a combining mark, a Hangul vowel or final
// consonant jamo and a kana voicing mark do.
//
// Every rune the encoding leaves unchanged is then ASCII or such a rune, and a
// string of them is its own NFC form: nothing in it changes alone, and nothing
// joins what stands before it. So every encoded name is its own NFC form. Two
// names a file system that ignores normalization takes for one have one NFC
// form, and two encoded names, each its own NFC form, have one only when they
// are one name.
//
// APFS normalizes by the table of an older Unicode version than the one here.
// A rune assigned since is one it leaves alone, and Unicode never gives a rune
// that already had a decomposition another one, so what the newer table
// numbers that the older one would not fold is a rune numbered without need,
// which costs a longer name and nothing else.
func saveNameComposed(r rune) bool {
	var buffer [utf8.UTFMax]byte
	encoded := utf8.AppendRune(buffer[:0], r)
	return norm.NFC.IsNormal(encoded) && norm.NFC.Properties(encoded).BoundaryBefore()
}

// saveNameWindowsRefused are the printable characters Windows refuses in a
// name. A key holds neither '\' nor NUL, and '/' separates its segments.
const saveNameWindowsRefused = `<>:"\|?*`

// saveNameDevice reports whether Windows takes a segment for a device: its
// stem, what is before its first '.' without the spaces at its end, is CON,
// PRN, AUX or NUL, the console's CONIN$ or CONOUT$, or COM or LPT and a digit
// or a superscript one, two or three, in any case. "con.txt" opens the console in any directory, so the
// first letter of such a segment is written by its number; one in upper case
// has its '#' already. Either way the stem then holds a '#', which no device
// name does.
func saveNameDevice(name string) bool {
	stem, _, _ := strings.Cut(name, ".")
	stem = strings.TrimRight(stem, " ")
	if len(stem) < 3 {
		return false
	}
	switch rest := stem[3:]; strings.ToLower(stem[:3]) {
	case "con":
		return rest == "" || strings.EqualFold(rest, "in$") || strings.EqualFold(rest, "out$")
	case "prn", "aux", "nul":
		return rest == ""
	case "com", "lpt":
		return len(rest) == 1 && '0' <= rest[0] && rest[0] <= '9' || rest == "¹" || rest == "²" || rest == "³"
	}
	return false
}

// saveNameNumbered reports whether the rune r, size bytes at index in a
// segment, is written by its number. A byte that is not UTF-8 is the form's
// to write (see saveNameForm.bytes).
func saveNameNumbered(name string, index int, r rune, size int) bool {
	switch {
	case r == '#' || saveNameEncodes(r):
		return false
	case r >= utf8.RuneSelf:
		return size > 1 && (saveNameCased(r) || !saveNameComposed(r))
	case r < 0x20 || strings.ContainsRune(saveNameWindowsRefused, r):
		return true
	case index+size == len(name) && (r == '.' || r == ' '):
		return true
	case r == '~' && index+1 < len(name) && '0' <= name[index+1] && name[index+1] <= '9':
		return true
	}
	return index == 0 && saveNameDevice(name)
}

// saveNameNumberDigits is how many hex digits a numbered rune is written with.
const saveNameNumberDigits = 6

// encodeSaveName writes one segment of a key as this build stores it.
func encodeSaveName(name string) string {
	return currentSaveNameForm.encode(name)
}

// decodeSaveName reverses encodeSaveName, and answers false for a name it
// would not have written.
func decodeSaveName(name string) (string, bool) {
	return currentSaveNameForm.decode(name)
}

// encode writes one segment of a key in this form. A segment nothing in
// which changes is answered as it is.
func (form saveNameForm) encode(name string) string {
	var encoded strings.Builder
	copied := 0
	const hex = "0123456789abcdef"
	for index := 0; index < len(name); {
		r, size := utf8.DecodeRuneInString(name[index:])
		invalid := form.bytes && r == utf8.RuneError && size == 1
		marked := r == '#' || form.marked(r)
		if !invalid && !marked && !form.numbered(name, index, r, size) {
			index += size
			continue
		}
		if copied == 0 {
			encoded.Grow(len(name) + 8)
		}
		encoded.WriteString(name[copied:index])
		switch {
		case invalid:
			encoded.Write([]byte{'#', '-', hex[name[index]>>4], hex[name[index]&0xf]})
		case r == '#':
			encoded.WriteString("##")
		case marked:
			encoded.WriteByte('#')
			encoded.WriteString(name[index : index+size])
		default:
			number := [2 + saveNameNumberDigits]byte{'#', '+'}
			for digit := range saveNameNumberDigits {
				number[len(number)-1-digit] = hex[r>>(4*digit)&0xf]
			}
			encoded.Write(number[:])
		}
		index += size
		copied = index
	}
	if copied == 0 {
		return name
	}
	encoded.WriteString(name[copied:])
	return encoded.String()
}

// decode reverses encode. The name is read as the encoding's tokens — "##"
// for '#', "#+" and six lower-case hex digits for a rune by its number, "#-"
// and two for a byte, '#' and a rune for that rune — and it is one of the
// form's names only when the segment it reads as is written as exactly the
// name again. What the encoding writes for a rune can depend on where the rune
// stands, so this is the one test that holds for every rule at once, and it
// makes the encoding a bijection: no two names read as one segment, and a name
// the encoding would not have written — a character it changes written bare,
// a '#' before anything it does not write after one, a number for a rune it
// writes otherwise — reads as none. Nor does a name whose segment no key can
// have: ".", ".." or one holding '\' or NUL, each of which the encoding
// numbers, so "#+00002e" is a name it writes and no key is ever stored at.
func (form saveNameForm) decode(name string) (string, bool) {
	decoded, ok := parseSaveName(name)
	if !ok || form.encode(decoded) != name || !saveKeySegment(decoded) {
		return "", false
	}
	return decoded, true
}

// saveKeySegment reports whether a segment is one a normalized key can hold
// (see NormalizeSaveKey).
func saveKeySegment(segment string) bool {
	return segment != "" && segment != "." && segment != ".." && !strings.ContainsAny(segment, "/\\\x00") &&
		!strings.ContainsRune(segment, os.PathSeparator)
}

// parseSaveName reads a name as the encoding's tokens, without asking whether
// the encoding would have written it. A '#' at the end, and "#+" without the
// six digits of a rune after it, are no tokens.
func parseSaveName(name string) (string, bool) {
	if strings.IndexByte(name, '#') < 0 {
		return name, true
	}
	var decoded strings.Builder
	decoded.Grow(len(name))
	for index := 0; index < len(name); {
		mark := strings.IndexByte(name[index:], '#')
		if mark < 0 {
			decoded.WriteString(name[index:])
			break
		}
		decoded.WriteString(name[index : index+mark])
		index += mark + 1
		if index == len(name) {
			return "", false
		}
		switch name[index] {
		case '#':
			decoded.WriteByte('#')
			index++
		case '-':
			value, ok := parseSaveByte(name[index+1:])
			if !ok {
				return "", false
			}
			decoded.WriteByte(value)
			index += 3
		case '+':
			number, ok := parseSaveNumber(name[index+1:])
			if !ok {
				return "", false
			}
			decoded.WriteRune(number)
			index += 1 + saveNameNumberDigits
		default:
			_, size := utf8.DecodeRuneInString(name[index:])
			decoded.WriteString(name[index : index+size])
			index += size
		}
	}
	return decoded.String(), true
}

// parseSaveByte reads the two lower-case hex digits at the start of a name as
// a byte.
func parseSaveByte(digits string) (byte, bool) {
	if len(digits) < 2 {
		return 0, false
	}
	value := byte(0)
	for index := range 2 {
		switch digit := digits[index]; {
		case '0' <= digit && digit <= '9':
			value = value<<4 | digit - '0'
		case 'a' <= digit && digit <= 'f':
			value = value<<4 | (digit - 'a' + 10)
		default:
			return 0, false
		}
	}
	return value, true
}

// parseSaveNumber reads the six lower-case hex digits at the start of a name
// as a rune.
func parseSaveNumber(digits string) (rune, bool) {
	if len(digits) < saveNameNumberDigits {
		return 0, false
	}
	value := rune(0)
	for index := range saveNameNumberDigits {
		switch digit := digits[index]; {
		case '0' <= digit && digit <= '9':
			value = value<<4 | rune(digit-'0')
		case 'a' <= digit && digit <= 'f':
			value = value<<4 | rune(digit-'a'+10)
		default:
			return 0, false
		}
	}
	return value, utf8.ValidRune(value)
}

// readSaveName answers the key segment a name in an encoded directory is the
// entry of, and whether the name is that segment's encoded name. A name this
// encoding would not have written is one an earlier build wrote there: a name
// of the first form of the encoding reads as that form wrote it, and any other
// as its own key.
func readSaveName(name string) (string, bool) {
	if decoded, ok := decodeSaveName(name); ok {
		return decoded, true
	}
	if decoded, ok := firstSaveNameForm.decode(name); ok {
		return decoded, false
	}
	return name, false
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

// saveNamesFit reports whether every segment of a slash-separated path is a
// name the file systems the Host runs on store.
//
// A key may be 512 bytes, and a segment written out may be eight times its
// length: a segment that does not fit is one the host cannot store, so its key
// has no save. A read of it finds none and a write is refused, rather than
// meeting the file system's error, which a platform takes for a store that
// failed — a KTF title asking whether such a file exists would otherwise lose
// every save it makes for the rest of the session. The one exception is a key
// an earlier build already stored under a name that fits (see locateSave).
func saveNamesFit(path string) bool {
	for segment := range strings.SplitSeq(path, "/") {
		if saveNameLength(segment) > maxSaveNameLength {
			return false
		}
	}
	return true
}

// saveNameLength is a name's length in the unit the host's file systems limit
// a name by: UTF-16 units where APFS, HFS+, exFAT and NTFS store 255 of them
// (on this Mac, 255 Hangul syllables of 765 bytes are a name and 256 are not;
// 127 emoji of 254 units are and 128 are not), and bytes where ext4 and f2fs
// store 255. Measured in bytes everywhere, a name an earlier build stored on
// macOS or Windows of 256 to 765 bytes could not be read, written or restored.
func saveNameLength(name string) int {
	if !saveNamesInUnits {
		return len(name)
	}
	units := 0
	for _, r := range name {
		if r >= 0x10000 {
			units += 2
		} else {
			units++
		}
	}
	return units
}

// saveNamesInUnits says the host measures a name in UTF-16 units.
var saveNamesInUnits = runtime.GOOS == "darwin" || runtime.GOOS == "ios" || runtime.GOOS == "windows"

// maxSavePathBytes is the longest path the host opens, in bytes, and zero for
// none to check: macOS and iOS open 1,023 and refuse 1,024 (measured on this
// Mac), Linux and Android open 4,095, and Go opens a longer path on Windows.
// A key's path, below the save directory's own, can be eight times the 512
// bytes of a key. The path of the temporary file a write makes beside the
// entry is at most 12 bytes longer than a short entry's.
var maxSavePathBytes = savePathLimit(runtime.GOOS)

func savePathLimit(goos string) int {
	switch goos {
	case "darwin", "ios":
		return 1023
	case "windows":
		return 0
	}
	return 4095
}

// savePathFits reports whether the host opens a path and the temporary file a
// write makes beside it.
func savePathFits(path string) bool {
	return maxSavePathBytes == 0 || len(path)+12 <= maxSavePathBytes
}

// errSaveNameTooLong marks the refusal of a key saveNamesFit or savePathFits
// refuses.
var errSaveNameTooLong = errors.New("save name is too long")

func refuseLongSaveName(key string) error {
	unit := "bytes"
	if saveNamesInUnits {
		unit = "characters"
	}
	return fmt.Errorf("%w: save key %q, written out, has a name longer than the %d %s a file name may have here or a path longer than this host opens",
		errSaveNameTooLong, key, maxSaveNameLength, unit)
}

// earlierSaveSpellings lists the names other than its encoded one under which
// a segment of a key may be in an encoded directory and read as that segment:
// the name the first form of the encoding gave it, and the segment itself as
// an earlier build wrote it. Each is listed only when it is another name than
// the encoded one and reads back as the segment (see readSaveName): "#Top"
// that an earlier build wrote reads as "Top", and is no name of "#Top".
func earlierSaveSpellings(segment string) []string {
	encoded := encodeSaveName(segment)
	if encoded == segment {
		// The first form changes a subset of what this one does.
		return nil
	}
	var spellings []string
	for _, spelling := range [...]string{firstSaveNameForm.encode(segment), segment} {
		if spelling == encoded || slices.Contains(spellings, spelling) {
			continue
		}
		if key, _ := readSaveName(spelling); key == segment {
			spellings = append(spellings, spelling)
		}
	}
	return spellings
}

func hasEarlierSaveSpelling(segment string) bool {
	return len(earlierSaveSpellings(segment)) > 0
}

// saveNamesRecord is the file in a save directory that says its names are
// encoded. Its name is one neither form of the encoding writes, and no spelling
// of it in another case is one either — its '#' is followed by a digit — so no
// key is ever stored over it, and walkers pass over it.
//
// The record keeps the name and the first line the first form of the encoding
// gave it. What it says holds for a directory either form wrote: its names are
// encoded, and in it a name of the first form reads as the key it was written
// for, at a name that is not yet its encoded one. So nothing about a directory
// the first form converted has to be told apart, and its names move to this
// form's one at a time as they are read. A record of another name would also
// have told a build of the first form nothing: it would have found no record,
// read every name with a '#' as an earlier build's key, and converted them
// again.
const saveNamesRecord = ".wfeature-names#1"

// saveNamesHeader is the record's first line. The second is its state; a
// conversion under way lists, one quoted path per line, the names it moves.
const saveNamesHeader = "wfeature save names 1"

// maxSaveNamesRecord bounds the record a store reads back: a conversion lists
// at most maxSnapshotSaveEntries paths of at most 512 bytes, quoted.
const maxSaveNamesRecord = maxSnapshotSaveEntries*(4*512+3) + 256

// maxSaveNameLength is the longest name the file systems a Host runs on
// store: 255 bytes on ext4 and f2fs, and 255 UTF-16 units on APFS, HFS+, exFAT
// and NTFS (see saveNameLength). An earlier name whose encoding would be
// longer is left as it is.
const maxSaveNameLength = 255

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
	// and they could not be converted, because it cannot be written or one of
	// them can neither move nor stay where it is. Every name is the key it was
	// written for, looked up by its exact spelling, and nothing is written
	// there.
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
// conversion cannot write, or one holding a name that could neither move nor
// stay (see convertSaveNames), is read as the earlier build left it.
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
			if undone := (*saveNamesUndone)(nil); errors.As(err, &undone) {
				return saveNames{naming: namingEarlier, blocked: undone.cause}, nil
			}
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
		if undone := (*saveNamesUndone)(nil); errors.As(err, &undone) {
			return saveNames{naming: namingEarlier, blocked: undone.cause}, nil
		}
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
	if encodedSaveRecordKnown(path, info) {
		return saveNamesEncoded, nil, nil
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
		rememberEncodedSaveRecord(path, info)
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
// own key; one that would read as another key in an encoded directory — an
// encoded name, or one of the first form of the encoding — cannot be, and
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
			name := listedSaveName(filepath.Join(root, filepath.FromSlash(relative)), entry.Name())
			path := name
			if relative != "" {
				path = relative + "/" + name
			}
			if path == saveNamesRecord || !saveKeyShaped(path) || !entry.IsDir() && saveCompanionFile(filepath.Join(root, filepath.FromSlash(path))) {
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
			if saveNameLength(encoded) > maxSaveNameLength {
				if key, _ := readSaveName(name); key != name {
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
//
// A name the first pass cannot move stays under its earlier name, with the
// names below it, when they all read as their own keys in an encoded
// directory; otherwise every name goes back and the directory reads as the
// earlier build left it (see undoSaveNames). Either way one name that cannot
// move — a file locked in the Finder, or one another program holds open on
// Windows — no longer takes every other save of the game with it, as it did
// when the conversion stopped there and every later operation failed.
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
				// A name that cannot move — a file locked in the Finder, one
				// another program holds open on Windows — stays where it is
				// when it reads as its own key in an encoded directory, with
				// everything below it: it is read there and a write goes to
				// its encoded name (see locateSave), so it costs no other save
				// anything. One that would read as another key cannot stay,
				// and the conversion is undone.
				if !saveNamesStay(plan, index) {
					return undoSaveNames(root, plan, err)
				}
				if err := moveSaveNamesBack(root, plan, func(other string) bool {
					return strings.HasPrefix(other, path+"/")
				}); err != nil {
					return err
				}
				continue
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

// saveNamesStay reports whether the name at one index of a conversion, which
// could not move, can stay where it is in the encoded directory the
// conversion leaves: it and every name below it that the conversion moves read
// there as their own keys (see readSaveName). "#Top", which an encoded
// directory reads as the key "Top", cannot.
func saveNamesStay(plan []string, index int) bool {
	path := plan[index]
	for _, other := range plan {
		if other != path && !strings.HasPrefix(other, path+"/") {
			continue
		}
		name := saveNameBase(other)
		if key, _ := readSaveName(name); key != name {
			return false
		}
	}
	return true
}

// moveSaveNamesBack gives the names of a conversion that have moved to their
// temporary names, and that the test picks, their own names back. It goes
// from the end of the list: a directory comes back before the names below it,
// which then lie under its own name again.
func moveSaveNamesBack(root string, plan []string, picked func(path string) bool) error {
	touched := make(map[string]bool)
	for index := len(plan) - 1; index >= 0; index-- {
		path := plan[index]
		if !picked(path) {
			continue
		}
		directory := filepath.Join(root, filepath.FromSlash(saveNameParent(path)))
		temporary := filepath.Join(directory, saveNamesTemporary(index))
		moved, err := saveNameExists(temporary)
		if err != nil {
			return err
		}
		if !moved {
			continue
		}
		if err := saveNamesRename(temporary, filepath.Join(directory, saveNameBase(path))); err != nil {
			return err
		}
		touched[directory] = true
	}
	syncSaveNameDirectories(touched)
	return nil
}

// undoSaveNames puts every name of a conversion back where an earlier build
// left it and removes the record, so that the directory reads as that build
// left it (namingEarlier) rather than refusing every operation until the name
// that stopped it can move. The answer is a saveNamesUndone carrying what
// stopped it; a name that cannot be put back leaves the record, and the next
// operation takes the conversion up again.
func undoSaveNames(root string, plan []string, cause error) error {
	if err := moveSaveNamesBack(root, plan, func(string) bool { return true }); err != nil {
		return errors.Join(cause, err)
	}
	if err := os.Remove(filepath.Join(root, saveNamesRecord)); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return errors.Join(cause, err)
	}
	syncDirectory(root)
	return &saveNamesUndone{cause: cause}
}

// saveNamesUndone is a conversion that met a name it could not move and that
// could not stay, and put every name back.
type saveNamesUndone struct{ cause error }

func (undone *saveNamesUndone) Error() string {
	return fmt.Sprintf("a save name could not be converted, and the conversion was undone: %v", undone.cause)
}

func (undone *saveNamesUndone) Unwrap() error { return undone.cause }

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
	if errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ENOTDIR) || saveNameRefused(err) {
		return false, nil
	}
	return false, err
}

// listedSaveName is the name a directory's entry is read under. The volumes
// macOS keeps names decomposed on — HFS+, exFAT and FAT, measured on disk
// images on 2026-10-18 — list every name in NFD, one the store wrote composed
// included, with its Hangul jamo and kana voicing marks apart, and there the
// composed name is the same file; a folder copied from one of them onto APFS
// keeps the decomposed names. Every name the encoding writes is its own NFC
// form, so an entry whose name is decomposed, and whose composed name is the
// same file, is read under the composed one: read as listed, a Hangul or kana
// save planned a move to a name with its jamo numbered, and the key's own
// lookup found nothing, even for a save this build had just written.
//
// What decomposition never makes is read as it is: a name with a singleton
// such as the Greek question mark or the ohm sign, which NFD replaces, was
// written so. So is a decomposed name on a volume that keeps it apart from its
// composed one (ext4, NTFS), where the composed name is another file or none.
// A decomposed key an earlier build stored on APFS is read as its composed
// key: on APFS the two were one file for that build, and the composed one is
// what a title's text decodes to.
func listedSaveName(directory, name string) string {
	if norm.NFC.IsNormalString(name) || !norm.NFD.IsNormalString(name) {
		return name
	}
	composed := norm.NFC.String(name)
	listed, err := os.Lstat(filepath.Join(directory, name))
	if err != nil {
		return name
	}
	other, err := os.Lstat(filepath.Join(directory, composed))
	if err != nil || !os.SameFile(listed, other) {
		return name
	}
	return composed
}

// saveCompanionFile reports whether a file is the companion macOS writes beside
// a file on a volume with no place for its metadata — exFAT and FAT among
// them, measured on disk images on 2026-10-18 — named "._" and the file's
// name, in the AppleDouble format. It is no save. Read as one, every save on
// such a volume put a 4 KiB key beside it in a backup, and a conversion moved
// its file's companion along with the file and then found it gone.
func saveCompanionFile(path string) bool {
	if !strings.HasPrefix(filepath.Base(path), "._") {
		return false
	}
	file, err := os.Open(path)
	if err != nil {
		return false
	}
	defer file.Close()
	var magic [4]byte
	if _, err := io.ReadFull(file, magic[:]); err != nil {
		return false
	}
	return binary.BigEndian.Uint32(magic[:]) == appleDoubleMagic
}

// appleDoubleMagic opens every AppleDouble file.
const appleDoubleMagic = 0x00051607

// composedSavePath reads each segment of a slash-separated path below root as
// listedSaveName reads a directory's entry.
func composedSavePath(root, relative string) string {
	if norm.NFC.IsNormalString(relative) {
		return relative
	}
	segments := strings.Split(relative, "/")
	directory := root
	for index, segment := range segments {
		segments[index] = listedSaveName(directory, segment)
		directory = filepath.Join(directory, segment)
	}
	return strings.Join(segments, "/")
}

// directoryNames lists one directory's names as the disk spells them. A path
// that is not a directory lists nothing. A listing is kept for the directory
// it was made of until the directory changes (see savedListings).
func directoryNames(directory string) ([]string, error) {
	file, err := os.Open(directory)
	if errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ENOTDIR) || saveNameRefused(err) {
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
	if names, known := savedListings.lookup(directory, info); known {
		return names, nil
	}
	names, err := file.Readdirnames(-1)
	if err != nil {
		return nil, err
	}
	for index, name := range names {
		names[index] = listedSaveName(directory, name)
	}
	savedListings.remember(directory, info, names)
	return names, nil
}

// savedListings keeps the listings spelledExactly made, so a title that asks
// for a missing key every frame, beside a name that differs from it only by
// case, does not list the directory every frame. A listing holds while the
// directory is the same one with the same modification time, which every
// name added, removed or renamed in it moves. A name removed within the same
// tick of a coarse clock is still caught, because spelledExactly looks the
// name up first; only a name another program adds within that tick is missed,
// and that is a name only an earlier build writes.
var savedListings = savedListingCache{known: make(map[string]savedListing)}

// maxSavedListings bounds the listings kept; past it they are all let go.
const maxSavedListings = 256

type savedListingCache struct {
	sync.Mutex
	known map[string]savedListing
}

type savedListing struct {
	info  os.FileInfo
	names []string
}

func (cache *savedListingCache) lookup(directory string, info os.FileInfo) ([]string, bool) {
	cache.Lock()
	defer cache.Unlock()
	known, ok := cache.known[directory]
	if !ok || !os.SameFile(known.info, info) || !known.info.ModTime().Equal(info.ModTime()) {
		return nil, false
	}
	return known.names, true
}

func (cache *savedListingCache) remember(directory string, info os.FileInfo, names []string) {
	cache.Lock()
	defer cache.Unlock()
	if len(cache.known) >= maxSavedListings {
		clear(cache.known)
	}
	cache.known[directory] = savedListing{info: info, names: names}
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
// each segment is the name exactly as written. A segment longer than a file
// system stores is no name there.
func exactSavePath(root string, segments []string) (string, bool, error) {
	path := root
	for _, segment := range segments {
		if saveNameLength(segment) > maxSaveNameLength {
			return "", false, nil
		}
		present, err := spelledExactly(path, segment)
		if err != nil || !present {
			return "", false, err
		}
		path = filepath.Join(path, segment)
	}
	return path, true, nil
}

// locateSave answers the path an operation reads one key's entry at, or false
// when the key has no entry: a directory read as an earlier build left it
// holds none, or the key's encoded path is longer than a file system stores
// and no earlier name of it is there (see saveNamesFit), which a write is
// refused for.
//
// In an encoded directory, an entry still under a name an earlier build gave
// it — written there by an earlier build after this one converted the
// directory, a name of the first form of the encoding, or one copied in by
// hand — is moved to its encoded name first. Only an earlier build writes
// such a name, so when the encoded entry is there too, the one written later
// is the save: an earlier entry newer than the encoded one is progress made on
// an earlier build after this one converted the directory, and it replaces the
// encoded entry it continued from; an older one is left where it is and not
// read. A write writes the encoded path whether or not the move succeeded; a
// read of an entry that could not be moved reads it where it is. An earlier
// name whose encoded path would be too long is read and written where it is.
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
	fits := saveNamesFit(encodedKey) && savePathFits(path)
	if fits && (encodedKey == key || names.naming == namingEncoded && !slices.ContainsFunc(segments, hasEarlierSaveSpelling)) {
		return path, true, nil
	}
	present := false
	if fits {
		present, _ = saveNameExists(path)
	}
	var earlier string
	var found bool
	if names.naming == namingPlain {
		if present {
			return path, true, nil
		}
		// Every name in a directory without the record is its own key, and the
		// only ones a '#' would change are those a conversion left as they
		// were.
		earlier, found, _ = exactSavePath(store.root, segments)
	} else {
		earlier, found = findEarlierSave(store.root, segments)
	}
	if !found && !present && !write && saveNamesWin32 {
		if at, spelled := store.win32Save(segments, names); spelled {
			return at, true, nil
		}
	}
	switch {
	case !fits && found:
		return earlier, true, nil
	case !fits && write:
		return "", false, refuseLongSaveName(key)
	case !fits:
		return "", false, nil
	case names.naming == namingPlain && found:
		return earlier, true, nil
	case !found || names.naming == namingPlain || present && !laterSaveFile(earlier, path):
		return path, true, nil
	}
	if err := moveEarlierSave(store.root, segments, earlier, present); err != nil && !write {
		return earlier, true, nil
	}
	return path, true, nil
}

// saveNamesWin32 says the host changes a path segment as Win32 does (see
// win32SaveName): an earlier build there stored the key "save." as "save",
// "SAVE    " as "SAVE", and an LGT or KTF C title's EUC-KR file name, whose
// bytes are not UTF-8, with U+FFFD for them.
var saveNamesWin32 = runtime.GOOS == "windows"

// win32Save finds, for a key that has no save, the entry an earlier build on
// a host that changes names as Win32 does left for it: under each segment as
// Win32 spelled it, as written then or encoded since. That file is also the
// save of every other key Win32 spelled the same — the key without the
// ending, a key with other bytes outside UTF-8 where this one has its own —
// which shared it there, so it is read where it is and never moved: the key's
// own first write goes to its encoded name, and the others keep the file.
func (store *DirectorySaveStore) win32Save(segments []string, names saveNames) (string, bool) {
	spelled := make([]string, len(segments))
	changed := false
	for index, segment := range segments {
		spelled[index] = win32SaveName(segment)
		if spelled[index] == "" {
			return "", false
		}
		changed = changed || spelled[index] != segment
	}
	if !changed {
		return "", false
	}
	if names.naming == namingEncoded {
		at := filepath.Join(store.root, filepath.FromSlash(encodeSaveKey(strings.Join(spelled, "/"))))
		if regularSaveFile(at) {
			return at, true
		}
	}
	if at, exists, _ := exactSavePath(store.root, spelled); exists && regularSaveFile(at) {
		return at, true
	}
	return "", false
}

// win32SaveName is the name Win32 stored a segment under for an earlier build.
// Go hands Win32 a path in UTF-16, where every byte that is not UTF-8 becomes
// U+FFFD and three that encode a lone surrogate stay that surrogate, which a
// listing reads back as the same three bytes; Win32 then drops the dots and
// spaces at the end.
func win32SaveName(segment string) string {
	var name strings.Builder
	for index := 0; index < len(segment); {
		r, size := utf8.DecodeRuneInString(segment[index:])
		switch rest := segment[index:]; {
		case r != utf8.RuneError || size != 1:
			name.WriteString(rest[:size])
		case len(rest) >= 3 && rest[0] == 0xed && rest[1] >= 0xa0 && rest[1] <= 0xbf && rest[2] >= 0x80 && rest[2] <= 0xbf:
			name.WriteString(rest[:3])
			size = 3
		default:
			name.WriteRune(utf8.RuneError)
		}
		index += size
	}
	return strings.TrimRight(name.String(), ". ")
}

// laterSaveFile reports whether the file at one path was written after the
// file at another. Equal times, and a time that cannot be read, are not later.
func laterSaveFile(path, than string) bool {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() {
		return false
	}
	other, err := os.Lstat(than)
	if err != nil {
		return false
	}
	return info.ModTime().After(other.ModTime())
}

// findEarlierSave looks for one key's entry under the names an earlier build
// could have left in an encoded directory: at each segment the encoded name,
// and then the earlier ones (see earlierSaveSpellings), spelled exactly. Of
// the entries it finds, the one written last is answered, and of two written
// at once the one a walk over the directory meets first, which is the one a
// listing chooses among them (see saveEntryChoice). Looking is a courtesy to a
// name nobody should have put there, so a path that cannot be looked at is
// passed over, and the operation is left to the encoded path as for any other
// key.
func findEarlierSave(root string, segments []string) (string, bool) {
	var found string
	var foundAt []string
	var foundTime time.Time
	spelled := make([]string, len(segments))
	var search func(directory string, depth int, encoded bool)
	search = func(directory string, depth int, encoded bool) {
		segment := segments[depth]
		spellings := append([]string{encodeSaveName(segment)}, earlierSaveSpellings(segment)...)
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
			spelled[depth] = spelling
			if depth == len(segments)-1 {
				// The encoded path itself was asked for before the search.
				info, err := os.Lstat(path)
				if allEncoded || err != nil || info.IsDir() {
					continue
				}
				if when := info.ModTime(); found == "" || when.After(foundTime) || when.Equal(foundTime) && slices.Compare(spelled, foundAt) < 0 {
					found, foundAt, foundTime = path, slices.Clone(spelled), when
				}
				continue
			}
			if info, err := os.Stat(path); err != nil || !info.IsDir() {
				continue
			}
			search(path, depth+1, allEncoded)
		}
	}
	search(root, 0, true)
	return found, found != ""
}

// moveEarlierSave gives an entry found under an earlier spelling its encoded
// path. A directory on the way that is still under its earlier name is renamed
// whole, which converts the names of everything below it at once. Where a
// directory of the encoded name is already there, the entry moves into it on
// its own, and the earlier directories that leaves empty are removed; a
// directory that still holds anything stays. Nothing is written over, except,
// when replace is set, the older encoded file of the same key.
func moveEarlierSave(root string, segments []string, earlier string, replace bool) error {
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
			if !replace || !regularSaveFile(target) {
				return fmt.Errorf("save %s is already there", target)
			}
			if err := os.Rename(current, target); err != nil {
				return err
			}
			syncDirectory(directory)
			return nil
		}
		// The encoded directory must be the one spelled so, not another name
		// this file system takes for it.
		if exact, err := spelledExactly(directory, encoded); err != nil || !exact {
			return errors.Join(err, fmt.Errorf("%s is another name to this file system", target))
		}
		from := filepath.Join(append([]string{current}, spelled[depth+1:]...)...)
		to := filepath.Join(target, filepath.FromSlash(encodeSaveKey(strings.Join(segments[depth+1:], "/"))))
		if taken, err := saveNameExists(to); err != nil || taken && (!replace || !regularSaveFile(to)) {
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

// storableSaveEntries settles, before a restore writes anything, the entries
// whose encoded paths are too long (see saveNamesFit). One that an earlier
// build left a name for is written there, and that name must be a file; any
// other is refused here, rather than by its own write after the entries
// before it have been written. The answer is the entries a restore writes at
// their encoded paths, which the tree is then judged against.
func (store *DirectorySaveStore) storableSaveEntries(entries []SaveEntry, names saveNames) ([]SaveEntry, error) {
	judged := make([]SaveEntry, 0, len(entries))
	for _, entry := range entries {
		if encoded := encodeSaveKey(entry.Key); saveNamesFit(encoded) && savePathFits(filepath.Join(store.root, filepath.FromSlash(encoded))) {
			judged = append(judged, entry)
			continue
		}
		path, found, err := store.locateSave(entry.Key, names, false)
		if err != nil {
			return nil, err
		}
		if !found {
			return nil, refuseLongSaveName(entry.Key)
		}
		if keyIsDirectory(path) {
			return nil, fmt.Errorf("backend: save backup entry %q is a file where the save tree has a directory", entry.Key)
		}
	}
	return judged, nil
}

// regularSaveFile reports whether a path is a file and not a directory or a
// link, which is the only thing a move replaces.
func regularSaveFile(path string) bool {
	info, err := os.Lstat(path)
	return err == nil && info.Mode().IsRegular()
}

// saveEntryKey is the key a path below a save directory is the entry of, and
// whether the path is that key's encoded path. In an encoded directory each
// name is read back (see readSaveName), and a name the encoding would not have
// written is one an earlier build wrote there, which is the key the first form
// of the encoding wrote it for or else its own; elsewhere every name is its
// own key. A path no key can name is an error.
func saveEntryKey(relative string, naming saveNaming) (string, bool, error) {
	segments := strings.Split(relative, "/")
	encoded := true
	if naming == namingEncoded {
		for index, segment := range segments {
			decoded, isEncoded := readSaveName(segment)
			segments[index] = decoded
			encoded = encoded && isEncoded
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
// beside the encoded one. The one written later is the entry, as it is for a
// read (locateSave), and the encoded path when that cannot be told; the other
// is left where it is, neither read nor removed.
type saveEntryChoice map[string]saveEntryAt

type saveEntryAt struct {
	path    string
	encoded bool
	entry   fs.DirEntry
}

// offer reports whether the path is now the one kept for the key.
func (choice saveEntryChoice) offer(key, path string, encoded bool, entry fs.DirEntry) bool {
	if kept, seen := choice[key]; seen && !laterSaveEntry(entry, encoded, kept) {
		return false
	}
	choice[key] = saveEntryAt{path: path, encoded: encoded, entry: entry}
	return true
}

// laterSaveEntry reports whether an entry a walk met is kept over the one kept
// for the same key before it.
func laterSaveEntry(entry fs.DirEntry, encoded bool, kept saveEntryAt) bool {
	if entry != nil && kept.entry != nil {
		info, err := entry.Info()
		keptInfo, keptErr := kept.entry.Info()
		if err == nil && keptErr == nil && !info.ModTime().Equal(keptInfo.ModTime()) {
			return info.ModTime().After(keptInfo.ModTime())
		}
	}
	return encoded && !kept.encoded
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

// The record of an encoded directory is read once and then known by its
// identity, size and modification time, so every operation on saves costs one
// look at it rather than a read. Every change to the record replaces the file,
// so a changed record is never mistaken for the one that was read.
var encodedSaveRecords = struct {
	sync.Mutex
	known map[string]os.FileInfo
}{known: make(map[string]os.FileInfo)}

func encodedSaveRecordKnown(path string, info os.FileInfo) bool {
	encodedSaveRecords.Lock()
	defer encodedSaveRecords.Unlock()
	known := encodedSaveRecords.known[path]
	return known != nil && os.SameFile(known, info) && known.Size() == info.Size() && known.ModTime().Equal(info.ModTime())
}

func rememberEncodedSaveRecord(path string, info os.FileInfo) {
	encodedSaveRecords.Lock()
	defer encodedSaveRecords.Unlock()
	if len(encodedSaveRecords.known) >= maxSavedListings {
		clear(encodedSaveRecords.known)
	}
	encodedSaveRecords.known[path] = info
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
