package backend

import (
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"unicode"
)

// Moving a save between installs.
//
// A save lives as loose files under `savedata/<profile>/<platform>/<owner>/`.
// Inside one install that is enough: the directory is named by the archive, so
// the game that wrote a file is the game that reads it back, and the claim in
// `internal/webhost/saveclaim.go` keeps two sessions off one directory.
//
// **Between installs nothing holds.** Copied to another machine those files
// carry no statement of what they are: not which game they belong to and not
// whether they arrived whole. Dropping one owner directory's contents into
// another owner's directory is a copy the file system performs without
// complaint, and the game then loads a save that was never written for it —
// which is not an error anywhere, only a game behaving strangely. A transfer
// truncated halfway is the same story: a save is written whole and read whole,
// so the front of one save on the back of another is a file the game reads as
// a save rather than as damage. That is the defect `DirectorySaveStore.StoreSave`
// takes such care to prevent within one machine, arriving by the road between
// two.
//
// So a save that leaves this tree is wrapped in a container that says both
// things, and the reader checks both before a single byte reaches the tree.
//
// The two refusals are kept apart because the person holding the file can act
// on each and the actions are different. "This backup belongs to a different
// game" means look for the right file — the bytes are fine. "This backup is
// damaged" means the file itself is no longer usable and the transfer has to
// be made again. A reader that answered "could not import" to both would send
// someone hunting for a second copy of a file that was never theirs, or
// retrying a file that will never load.

// The container, little-endian throughout:
//
//	magic     8B   savePackMagic
//	version   1B   savePackVersion
//	reserved  1B   0
//	identity  32B  raw SHA-256 of the archive this save belongs to
//	length    4B   payload length
//	crc32     4B   IEEE checksum of the payload
//	payload   length B
//
// The magic is checked before the version so that a file that is not a
// container at all is not reported as a container from the future.
const (
	savePackMagic      = "WFSAVEBK"
	savePackVersion    = 1
	savePackHeaderSize = 8 + 1 + 1 + 32 + 4 + 4

	// savePackIdentitySize is the width of the identity field. It is a raw
	// SHA-256 rather than its hex text: the field is fixed width either way,
	// and raw bytes cannot disagree with themselves over letter case.
	savePackIdentitySize = 32
)

// savePackLimit bounds a payload. Saves of this era are kilobytes and the
// largest owner directory in the local library is well under a megabyte, so
// this is generous rather than tight; it exists so that a damaged length field
// asks for a bounded allocation rather than an unbounded one.
const savePackLimit = 64 << 20

// The refusals a reader distinguishes. Every one of them is a fact about the
// file rather than about this program, so each is worth its own sentence to
// whoever is holding it.
var (
	// ErrNotSavePack means the bytes are not one of these containers. A file
	// picked by mistake lands here.
	ErrNotSavePack = errors.New("backend: this is not a save backup")
	// ErrSavePackVersion means it is one, written by a newer build.
	ErrSavePackVersion = errors.New("backend: this save backup is a newer version than this build reads")
	// ErrSavePackDamaged means the container is one of these and does not hold
	// together: a truncated transfer, a flipped bit, an edit.
	ErrSavePackDamaged = errors.New("backend: this save backup is damaged")
	// ErrSavePackIdentity means the container is intact and belongs to a
	// different game. It is deliberately not the same error as damage: the
	// bytes are fine and the fix is to find the right file.
	ErrSavePackIdentity = errors.New("backend: this save backup belongs to a different game")
)

// SaveIdentity is the identity a backup carries: the raw SHA-256 of the game
// archive the save belongs to.
//
// It is the archive rather than the save owner because the owner is not unique.
// `SaveOwnerCollisions` exists on all three platforms precisely because more
// than one distinct title can claim one owner directory — a KTF owner is the
// handset's program number and two builds ship the same one, an SKT owner can
// fall back to a MIDlet name — so a container keyed by the owner would be
// accepted by the very game it must be refused for. The archive's bytes are
// the one thing that is only ever this game.
//
// The cost is that a repacked archive is a different input and its own backups
// are refused. That is the intended reading: this build cannot tell a repack
// that changed nothing from one that changed the save format, and refusing is
// the answer that cannot silently ruin a save. The repo already treats a file
// digest as the strict key this way — `internal/cheat`'s table key carries one
// beside a looser image hash.
func SaveIdentity(archive []byte) [savePackIdentitySize]byte {
	return sha256.Sum256(archive)
}

// SaveEntry is one entry of a title's writable storage: the key a
// SaveStore addresses it by, and its bytes.
type SaveEntry struct {
	Key  string
	Data []byte
}

// SavePack is one title's writable storage together with the identity of the
// input it belongs to.
type SavePack struct {
	// Identity is the raw SHA-256 of the game archive. It is what makes the
	// container refuse to be restored over another game.
	Identity [savePackIdentitySize]byte
	// Entries are ordered by key. EncodeSavePack sorts them, so two encodes of
	// one directory produce the same bytes and therefore the same checksum.
	Entries []SaveEntry
}

// EncodeSavePack writes a container. The payload is the entry list in key
// order — for each entry a uint16 key length, the key, a uint32 data length,
// and the data — which is a shape with no timestamps, no file modes and no
// directory order in it. That matters more than being able to open the payload
// with an unzip tool: the checksum is over these bytes, and a payload that
// carried the day it was written would checksum differently every time one
// directory was exported, so nobody could tell two exports of one save apart
// from two different saves.
func EncodeSavePack(pack SavePack) ([]byte, error) {
	entries := append([]SaveEntry(nil), pack.Entries...)
	sort.Slice(entries, func(left, right int) bool { return entries[left].Key < entries[right].Key })
	// The import refuses a pack with more entries than a save tree may hold,
	// so an export that wrote one would hand somebody a backup that cannot
	// be restored.
	if len(entries) > maxSnapshotSaveEntries {
		return nil, fmt.Errorf("save backup has %d entries, over the %d a save tree may hold", len(entries), maxSnapshotSaveEntries)
	}

	payload := make([]byte, 0, savePackPayloadSize(entries))
	previous := ""
	for index, entry := range entries {
		// A key is validated on the way out as well as on the way in. An
		// export is the last moment this program can tell the difference
		// between a key it wrote and one that would climb out of a save
		// directory on the machine that receives it.
		key, err := NormalizeSaveKey(entry.Key)
		if err != nil {
			return nil, fmt.Errorf("save backup entry %d: %w", index, err)
		}
		if key == previous {
			return nil, fmt.Errorf("save backup names %q twice", key)
		}
		previous = key
		if len(key) > savePackMaxKey {
			return nil, fmt.Errorf("save backup key %q is too long", key)
		}
		payload = binary.LittleEndian.AppendUint16(payload, uint16(len(key)))
		payload = append(payload, key...)
		payload = binary.LittleEndian.AppendUint32(payload, uint32(len(entry.Data)))
		payload = append(payload, entry.Data...)
	}
	if len(payload) > savePackLimit {
		return nil, fmt.Errorf("save backup is %d bytes, over the %d byte limit", len(payload), savePackLimit)
	}

	container := make([]byte, 0, savePackHeaderSize+len(payload))
	container = append(container, savePackMagic...)
	container = append(container, savePackVersion, 0)
	container = append(container, pack.Identity[:]...)
	container = binary.LittleEndian.AppendUint32(container, uint32(len(payload)))
	container = binary.LittleEndian.AppendUint32(container, crc32.ChecksumIEEE(payload))
	return append(container, payload...), nil
}

// savePackMaxKey is the widest key the payload's uint16 length can carry. It
// is well above NormalizeSaveKey's own 512 byte limit, so this is a statement
// about the format rather than a second policy.
const savePackMaxKey = 0xffff

func savePackPayloadSize(entries []SaveEntry) int {
	size := 0
	for _, entry := range entries {
		size += 2 + len(entry.Key) + 4 + len(entry.Data)
	}
	return size
}

// DecodeSavePack reads a container and reports which of the refusals above
// applies. It does not look at the identity — that is the caller's comparison,
// because only the caller knows which game the save is being restored onto.
func DecodeSavePack(container []byte) (SavePack, error) {
	if len(container) < savePackHeaderSize || string(container[:len(savePackMagic)]) != savePackMagic {
		return SavePack{}, ErrNotSavePack
	}
	if container[8] != savePackVersion {
		return SavePack{}, ErrSavePackVersion
	}
	// The reserved byte is checked rather than ignored. It is the only room
	// this header has for a later meaning, and a build that skipped it would
	// silently accept a container whose reserved bit changed what the payload
	// means.
	if container[9] != 0 {
		return SavePack{}, ErrSavePackVersion
	}
	var pack SavePack
	copy(pack.Identity[:], container[10:10+savePackIdentitySize])
	length := binary.LittleEndian.Uint32(container[42:46])
	checksum := binary.LittleEndian.Uint32(container[46:50])
	// The length is checked against what actually arrived before it is used to
	// slice, so a truncated transfer is damage rather than a panic. Trailing
	// bytes are damage too: this container has a stated length and nothing is
	// defined after it, so something appended to one is a file that was
	// concatenated or a length that was corrupted, and neither should be
	// restored.
	if uint64(length) > uint64(savePackLimit) || uint64(len(container)-savePackHeaderSize) != uint64(length) {
		return SavePack{}, ErrSavePackDamaged
	}
	payload := container[savePackHeaderSize:]
	if crc32.ChecksumIEEE(payload) != checksum {
		return SavePack{}, ErrSavePackDamaged
	}
	entries, err := decodeSavePackPayload(payload)
	if err != nil {
		return SavePack{}, err
	}
	pack.Entries = entries
	return pack, nil
}

// decodeSavePackPayload walks the entry list. Everything it refuses is damage
// by the time it is reached: the checksum already agreed, so a payload that
// does not parse is one whose checksum was computed over these same broken
// bytes — a container built by something other than EncodeSavePack, or one
// edited by hand — rather than a transfer that lost bytes. Either way it must
// not reach the save tree.
func decodeSavePackPayload(payload []byte) ([]SaveEntry, error) {
	entries := []SaveEntry{}
	files := make(map[string]bool)
	previous := ""
	for offset := 0; offset < len(payload); {
		// The count is bounded the way a quick save's tree is, and checked
		// before the entry is read so that the bound is also the allocation's.
		// Every entry of a restore is a file written, flushed and renamed into
		// place, and a payload of millions of empty ones would hold an import
		// for hours; no title's save tree comes near the bound.
		if len(entries) == maxSnapshotSaveEntries {
			return nil, ErrSavePackDamaged
		}
		if offset+2 > len(payload) {
			return nil, ErrSavePackDamaged
		}
		keyLength := int(binary.LittleEndian.Uint16(payload[offset : offset+2]))
		offset += 2
		if offset+keyLength+4 > len(payload) {
			return nil, ErrSavePackDamaged
		}
		rawKey := string(payload[offset : offset+keyLength])
		offset += keyLength
		dataLength := int(binary.LittleEndian.Uint32(payload[offset : offset+4]))
		offset += 4
		if dataLength < 0 || offset+dataLength > len(payload) {
			return nil, ErrSavePackDamaged
		}
		data := append([]byte(nil), payload[offset:offset+dataLength]...)
		offset += dataLength

		// A key from another machine is a path this program is about to join
		// onto a directory, so it is checked here and refused rather than
		// repaired — the same rule the save API applies to a key arriving over
		// HTTP. A container is not a trusted input just because it has a
		// checksum: the checksum says the bytes are the ones that were sent,
		// not that whoever sent them meant well.
		key, err := NormalizeSaveKey(rawKey)
		if err != nil || key != rawKey {
			return nil, ErrSavePackDamaged
		}
		// Sorted and unique, which is what EncodeSavePack writes. Checking it
		// costs nothing and it is what keeps a duplicated key from making the
		// restore depend on which copy landed last.
		if previous != "" && key <= previous {
			return nil, ErrSavePackDamaged
		}
		previous = key
		// A name that is a file and also the directory of another entry is a
		// shape one directory cannot hold, so no export carries it — and a
		// restore of it fails on the second write after the first has
		// already replaced a save. In key order a directory's own name comes
		// before everything under it, so looking back is enough.
		for index := 0; index < len(key); index++ {
			if key[index] == '/' && files[key[:index]] {
				return nil, ErrSavePackDamaged
			}
		}
		files[key] = true
		entries = append(entries, SaveEntry{Key: key, Data: data})
	}
	return entries, nil
}

// ReadSaveTree collects one owner directory as save entries, in key order. A
// directory that does not exist yet reads as no entries rather than an error:
// a game that has never saved is the ordinary first run, and the caller has a
// better sentence for it than this does.
//
// **The dotted names are not all skipped.** A store writes each entry through
// a dotted temporary file that is renamed into place, so a dotted name may be
// one a killed process left behind — but MIDP's record store index is itself
// named `rms/.index`, and it is what says a store exists at all
// (`docs/rms.md`). A backup that dropped every dotted name would restore an
// SKT save whose stores the game then cannot find, which reads to the player
// as a save that imported successfully and lost everything. So what is skipped
// is the temporary shape specifically; see saveTemporaryName.
//
// Paths are read back to keys the way the directory's names read (see
// saveEntryKey): an export carries keys, never the names on disk, so a backup
// this build writes is the one an earlier build wrote of the same saves.
func ReadSaveTree(root string) ([]SaveEntry, error) {
	if root == "" {
		return nil, errors.New("backend: the save tree has no root")
	}
	unlock, err := lockSaveTree(root)
	if err != nil {
		return nil, err
	}
	defer unlock()
	store := NewDirectorySaveStore(root)
	if err := store.recoverSaveReplacement(); err != nil {
		return nil, err
	}
	names, err := store.settleSaveNames()
	if err != nil {
		return nil, err
	}
	choice := saveEntryChoice{}
	err = walkSaveTree(root, func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			// The root not being there is the first run, and it is the only
			// missing path that is not a problem: anything below the root was
			// listed by this walk a moment ago.
			if name == root && errors.Is(err, fs.ErrNotExist) {
				return fs.SkipAll
			}
			return err
		}
		if entry.IsDir() {
			return nil
		}
		// A save is a file. A symbolic link in the tree points somewhere this
		// program did not put anything, so following it would put a file from
		// outside the save tree into a backup.
		if !entry.Type().IsRegular() {
			return nil
		}
		if saveTemporaryName(entry.Name()) || saveCompanionFile(name) {
			return nil
		}
		relative, err := filepath.Rel(root, name)
		if err != nil {
			return err
		}
		relative = filepath.ToSlash(relative)
		if relative == saveNamesRecord {
			return nil
		}
		key, encoded, err := saveEntryKey(composedSavePath(root, relative), names.naming)
		if err != nil {
			// A file the store did not write can sit in this tree, and it is
			// not a reason to fail an export of the saves that are here.
			return nil
		}
		choice.offer(key, name, encoded, entry)
		return nil
	})
	if err != nil {
		return nil, err
	}
	entries := make([]SaveEntry, 0, len(choice))
	for _, key := range choice.keys() {
		data, err := os.ReadFile(choice[key].path)
		if err != nil {
			return nil, err
		}
		entries = append(entries, SaveEntry{Key: key, Data: data})
	}
	return entries, nil
}

// walkSaveTree is filepath.WalkDir for an owner directory that may be a link.
// WalkDir does not descend through a link at its root — it reports the link
// and stops — so a linked owner directory would export as a game that never
// saved, and an import would leave every stale entry beside the restored ones.
// The root is read through the link here. Below it nothing is followed, the
// same as before, so a link inside the tree still cannot put a file from
// outside it into a backup. The visitor never sees the root as an entry: only
// what is under it, or the error that kept it from being read.
func walkSaveTree(root string, visit fs.WalkDirFunc) error {
	children, err := os.ReadDir(root)
	if err != nil {
		if err = visit(root, nil, err); err == fs.SkipDir || err == fs.SkipAll {
			return nil
		}
		return err
	}
	stopped := false
	for _, child := range children {
		err := filepath.WalkDir(filepath.Join(root, child.Name()), func(name string, entry fs.DirEntry, err error) error {
			err = visit(name, entry, err)
			stopped = stopped || err == fs.SkipAll
			return err
		})
		if err != nil || stopped {
			return err
		}
	}
	return nil
}

// saveTemporaryName reports whether a file name is one DirectorySaveStore left
// behind on its way to a rename, rather than a save.
//
// Telling the two apart matters in one direction only, and it is the direction
// that costs data: `rms/.index` is a real save whose name begins with a dot, so
// a rule of "skip the dotted ones" loses it. The temporary files have a shape
// the index does not — `os.CreateTemp` is given the pattern
// `"." + key + ".*"` and fills the star with decimal digits — so the test is
// for that shape rather than for the leading dot alone. A save whose own name
// happens to end in a dot and digits is misread by this, which is a name no
// store in this project writes and a smaller loss than the index.
func saveTemporaryName(name string) bool {
	if !strings.HasPrefix(name, ".") {
		return false
	}
	dot := strings.LastIndexByte(name, '.')
	if dot <= 0 {
		return false
	}
	digits := name[dot+1:]
	// os.CreateTemp fills the pattern with a fixed-width decimal number, so a
	// short run of digits is not one of its names.
	if len(digits) < 6 {
		return false
	}
	for _, character := range digits {
		if !unicode.IsDigit(character) {
			return false
		}
	}
	return true
}

// WriteSaveTree replaces one owner directory's contents with the entries
// given, and reports how many entries it wrote and how many it removed.
//
// **It replaces rather than merges.** The container holds the whole of a
// title's writable storage, so an entry that is in the tree and not in the
// container is one the game deleted, or one from a later point in the story
// than the backup describes. Leaving it beside the restored entries builds a
// state the game never wrote — one save's index over another save's records —
// and a game reading that is not obviously broken, only wrong. The person
// asking for a restore is asking for the moment the backup describes.
//
// Each entry goes through StoreSave, so every file is replaced by a rename the
// way a guest's own save is: a restore interrupted halfway leaves whole files,
// some old and some new, rather than a truncated one.
//
// **What can be refused before the first write is.** The entries are checked
// as a whole — their number, and names that would be a file and a directory
// at once — and then against the live tree, which can hold a file where an
// entry needs a directory or the other way round. Found on the second entry,
// either of those used to leave the first one already replaced.
func WriteSaveTree(root string, entries []SaveEntry) (written int, removed int, err error) {
	if root == "" {
		return 0, 0, errors.New("backend: the save tree has no root")
	}
	normalized := make([]SaveEntry, 0, len(entries))
	for _, entry := range entries {
		key, err := NormalizeSaveKey(entry.Key)
		if err != nil {
			return 0, 0, err
		}
		normalized = append(normalized, SaveEntry{Key: key, Data: entry.Data})
	}
	normalized, err = keysOfAConvertedFolder(normalized)
	if err != nil {
		return 0, 0, fmt.Errorf("backend: the save backup cannot be restored whole: %w", err)
	}
	ordered, err := validateSnapshotSaves(normalized)
	if err != nil {
		return 0, 0, fmt.Errorf("backend: the save backup cannot be restored whole: %w", err)
	}
	unlock, err := lockSaveTree(root)
	if err != nil {
		return 0, 0, err
	}
	defer unlock()
	keep := make(map[string]bool, len(ordered))
	store := NewDirectorySaveStore(root)
	if err := store.recoverSaveReplacement(); err != nil {
		return 0, 0, err
	}
	names, err := store.settleSaveNames()
	if err != nil {
		return 0, 0, err
	}
	if names.naming == namingEarlier {
		return 0, 0, refuseEarlierSaveNames(root, names)
	}
	judged, err := store.storableSaveEntries(ordered, names)
	if err != nil {
		return 0, 0, err
	}
	if err := saveTreeHasRoomFor(root, judged); err != nil {
		return 0, 0, err
	}
	for _, entry := range ordered {
		if err := store.storeSave(entry.Key, entry.Data, &names); err != nil {
			return written, 0, err
		}
		keep[entry.Key] = true
		written++
	}
	// The removal runs after every write, so a restore that fails partway has
	// added files and taken none away. That is the recoverable half: the
	// previous save is still there to be found, while the reverse — a tree
	// emptied and then not refilled — is a player's progress gone to a failure
	// they did not cause.
	removed, err = pruneSaveTree(root, keep, names.naming)
	return written, removed, err
}

// keysOfAConvertedFolder answers the keys of the entries of a backup an earlier
// build made of a folder this build had converted. That build read every name
// in the folder as its own key, the names record among them, so the backup
// carries the folder's names rather than its keys — "rms/#Save" for the key
// "rms/Save" — and the record. Restored as they were, the names became keys
// no title asks for and the record a save of its own, and the restore then
// removed the save under the key the title does read. The record says of the
// backup what it says of a folder, that the names beside it are encoded, so
// they are read as an encoded folder's names are (see saveEntryKey) and the
// record is no entry. Of two entries that read as one key the encoded one is
// kept, as a listing keeps it when it cannot tell which was written later. A
// backup without the record is answered as it is.
func keysOfAConvertedFolder(entries []SaveEntry) ([]SaveEntry, error) {
	if !slices.ContainsFunc(entries, func(entry SaveEntry) bool { return entry.Key == saveNamesRecord }) {
		return entries, nil
	}
	type read struct {
		entry   SaveEntry
		encoded bool
	}
	keys := make(map[string]read, len(entries))
	order := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.Key == saveNamesRecord {
			continue
		}
		key, encoded, err := saveEntryKey(entry.Key, namingEncoded)
		if err != nil {
			return nil, err
		}
		kept, seen := keys[key]
		if seen && (kept.encoded || !encoded) {
			continue
		}
		if !seen {
			order = append(order, key)
		}
		keys[key] = read{entry: SaveEntry{Key: key, Data: entry.Data}, encoded: encoded}
	}
	answered := make([]SaveEntry, 0, len(order))
	for _, key := range order {
		answered = append(answered, keys[key].entry)
	}
	return answered, nil
}

// saveTreeHasRoomFor reports an entry the live tree has the wrong shape for: a
// file where the entry needs a directory, or a directory where it needs a
// file. The entries are judged at their encoded paths, and the file system
// answers, so a name it folds to another by Unicode normalization is judged
// the way the writes will be. A key that is not UTF-8 is no exception: its
// encoded name writes the byte by its number, which macOS stores too.
func saveTreeHasRoomFor(root string, entries []SaveEntry) error {
	directories := make(map[string]bool)
	for _, entry := range entries {
		parts := strings.Split(encodeSaveKey(entry.Key), "/")
		path := root
		missing := false
		for _, part := range parts[:len(parts)-1] {
			path = filepath.Join(path, part)
			if directories[path] {
				continue
			}
			info, err := os.Stat(path)
			if errors.Is(err, fs.ErrNotExist) {
				// Nothing below a missing directory exists either.
				missing = true
				break
			}
			if err != nil {
				return err
			}
			if !info.IsDir() {
				return fmt.Errorf("backend: save backup entry %q needs a directory where the save tree has a file", entry.Key)
			}
			directories[path] = true
		}
		if missing {
			continue
		}
		info, err := os.Lstat(filepath.Join(path, parts[len(parts)-1]))
		if err == nil && info.IsDir() {
			return fmt.Errorf("backend: save backup entry %q is a file where the save tree has a directory", entry.Key)
		}
	}
	return nil
}

// pruneSaveTree removes the files under root whose keys the restore did not
// name, and then the directories those files left empty. Paths are read back
// to keys the way the directory's names read (see saveEntryKey), and the
// record of how they read is not an entry.
//
// **A name is not the whole test.** On a file system that ignores Unicode
// normalization — macOS by default — a rename onto an existing entry replaces
// its contents and keeps its old spelling, so a save just restored can sit
// under a name the restore never used. A file whose key was not named is kept
// when it is the very file a named key opens.
func pruneSaveTree(root string, keep map[string]bool, naming saveNaming) (int, error) {
	removed := 0
	// The kept files, by size, read only once a file that was not named turns
	// up — which on a file system that keeps names apart is a removal, and
	// on one that folds them may be a restored save.
	var kept map[int64][]os.FileInfo
	restored := func(entry fs.DirEntry) bool {
		if kept == nil {
			kept = make(map[int64][]os.FileInfo)
			for key := range keep {
				if info, err := os.Lstat(filepath.Join(root, filepath.FromSlash(encodeSaveKey(key)))); err == nil {
					kept[info.Size()] = append(kept[info.Size()], info)
				}
			}
		}
		info, err := entry.Info()
		if err != nil {
			return false
		}
		for _, candidate := range kept[info.Size()] {
			if os.SameFile(info, candidate) {
				return true
			}
		}
		return false
	}
	var directories []string
	err := walkSaveTree(root, func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			if name == root && errors.Is(err, fs.ErrNotExist) {
				return fs.SkipAll
			}
			return err
		}
		if entry.IsDir() {
			if name != root {
				directories = append(directories, name)
			}
			return nil
		}
		if !entry.Type().IsRegular() {
			return nil
		}
		relative, err := filepath.Rel(root, name)
		if err != nil {
			return err
		}
		relative = filepath.ToSlash(relative)
		if relative == saveNamesRecord {
			return nil
		}
		key, _, err := saveEntryKey(composedSavePath(root, relative), naming)
		// A leftover temporary file is removed with the rest: it is not a save
		// and the restore it belonged to is over. A file whose name is not a
		// key at all is left alone — this program did not write it and does
		// not know what it is — and so is the companion macOS keeps beside a
		// file, which goes with its file.
		if err != nil || saveCompanionFile(name) {
			return nil
		}
		if !saveTemporaryName(entry.Name()) && (keep[key] || restored(entry)) {
			return nil
		}
		if err := os.Remove(name); err != nil {
			return err
		}
		removed++
		return nil
	})
	if err != nil {
		return removed, err
	}
	// Deepest first, so a directory whose only child was also emptied goes
	// too. Remove refuses a directory that is not empty, which is exactly the
	// test wanted here, so the error is the answer rather than a failure.
	sort.Sort(sort.Reverse(sort.StringSlice(directories)))
	for _, directory := range directories {
		_ = os.Remove(directory)
	}
	return removed, nil
}
