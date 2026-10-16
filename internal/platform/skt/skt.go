// Package skt composes the SKVM/MIDP Java and GNEX/GVM SGS runtimes for SKT
// handset games, using shared Go execution and backend services.
package skt

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/movingwoo/wfeature/internal/backend"
	"golang.org/x/text/encoding/korean"
)

// The descriptor an SKT archive ships beside its JAR. It is JAD-shaped —
// the same `MIDlet-1: <name>,<icon>,<class>` lines a manifest carries — which
// is why the shared SKT descriptor parser reads it.
const msdSuffix = ".msd"

// Open reads an SKT Java package or a GNEX/GVM SGS archive.
//
// A handset was sent an archive: a zip holding `<id>.jar` beside `<id>.msd`,
// and the JAR's own manifest names no MIDlet at all — the identity is in the
// .msd. A bare JAR that does name its MIDlet is the other shape, and it is
// what the fixtures and any repacked title look like.
func Open(data []byte) (opened *Archive, err error) {
	defer func() {
		if err == nil && opened != nil {
			opened.identity = sha256.Sum256(data)
		}
	}()
	if script, err := openScript(data); err != nil || script != nil {
		return script, err
	}
	descriptor, jar, installed, err := unpackArchive(data)
	if err != nil {
		return nil, err
	}
	if jar == nil {
		archive, err := openJAR(data)
		if err != nil {
			return nil, fmt.Errorf("open SKT archive: %w", err)
		}
		return archive, nil
	}
	archive, err := OpenWithDescriptor(jar, descriptor)
	if err != nil {
		return nil, fmt.Errorf("open SKT archive: %w", err)
	}
	archive.addInstalledFiles(installed)
	return archive, nil
}

// unpackArchive finds the descriptor, the JAR, and the title's installed files
// inside an SKT archive. A nil JAR means data was not an archive at all, which
// is not an error here: the caller then reads it as a bare MIDlet JAR.
func unpackArchive(data []byte) (Descriptor, []byte, map[string][]byte, error) {
	return unpackArchiveWithin(data, defaultArchiveLimits)
}

// unpackArchiveWithin is unpackArchive with its bounds named, so a test can
// reach the far side of each one without building half a gigabyte to get
// there. The outer container is bounded exactly as the JAR inside it is: an
// archive that is not a zip at all is still not an error here, but one that is
// a zip past a bound is.
func unpackArchiveWithin(data []byte, limits archiveLimits) (Descriptor, []byte, map[string][]byte, error) {
	if uint64(len(data)) > limits.input {
		return Descriptor{}, nil, nil, fmt.Errorf("SKT archive exceeds %d input bytes", limits.input)
	}
	// Whether these bytes are a container at all is decided first, and by the
	// reader rather than by the shape of an error: bytes that are not a zip
	// are not a failure here, because the caller then reads them as a bare
	// MIDlet JAR. Only once it is a zip do its declared bounds apply.
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return Descriptor{}, nil, nil, nil
	}
	if len(reader.File) > limits.count {
		return Descriptor{}, nil, nil, fmt.Errorf("SKT archive contains %d entries, limit %d", len(reader.File), limits.count)
	}
	spent := &budget{limits: limits, what: "SKT archive"}
	var msdName string
	for _, file := range reader.File {
		if strings.EqualFold(path.Ext(file.Name), msdSuffix) {
			msdName = file.Name
			break
		}
	}
	if msdName == "" {
		return Descriptor{}, nil, nil, nil
	}
	msd, err := readArchiveEntry(reader, msdName, spent)
	if err != nil {
		return Descriptor{}, nil, nil, fmt.Errorf("read SKT descriptor %q: %w", msdName, err)
	}
	// The .msd is EUC-KR: these were Korean handsets and the title name is
	// Korean text. Reading it as UTF-8 gives a name of replacement characters,
	// which is what a Host would then show a user.
	descriptor, err := ParseDescriptor(decodeArchiveText(msd))
	if err != nil {
		return Descriptor{}, nil, nil, fmt.Errorf("parse SKT descriptor %q: %w", msdName, err)
	}
	// The JAR is named after the descriptor. A title whose archive was
	// repacked can have lost the pairing, so the one JAR that is there wins —
	// the same rule the LGT loader applies for the same reason.
	jarName := strings.TrimSuffix(msdName, path.Ext(msdName)) + ".jar"
	jar, err := readArchiveEntry(reader, jarName, spent)
	if err != nil {
		jarName, err = onlyJARName(reader)
		if err != nil {
			return Descriptor{}, nil, nil, err
		}
		jar, err = readArchiveEntry(reader, jarName, spent)
		if err != nil {
			return Descriptor{}, nil, nil, err
		}
	}
	installed, err := installedFiles(reader, spent, jarName)
	if err != nil {
		return Descriptor{}, nil, nil, err
	}
	return descriptor, jar, installed, nil
}

// installedFiles reads the entries an SKT archive carries beside the JAR and
// its descriptor. They are the title's own files, not JAR resources: a handset
// was sent one archive and unpacked it into the title's storage, so a game
// opens them by the bare name they have here.
//
// Nothing but the packaging tells them apart from the descriptor and the
// module, so what is excluded is named rather than guessed at: the JAR, the
// .msd, and the two files the platform itself was sent. Additional JARs are
// installed resources that XFile can open independently of the class path.
func installedFiles(reader *zip.Reader, spent *budget, primaryJAR string) (map[string][]byte, error) {
	installed := make(map[string][]byte)
	for _, file := range reader.File {
		if file.FileInfo().IsDir() || file.Name == primaryJAR {
			continue
		}
		switch strings.ToLower(path.Ext(file.Name)) {
		case msdSuffix, ".mod", ".wmr":
			continue
		}
		name := path.Base(file.Name)
		if name == "" || name == "." {
			continue
		}
		data, err := readArchiveEntry(reader, file.Name, spent)
		if err != nil {
			return nil, fmt.Errorf("read SKT installed file %q: %w", file.Name, err)
		}
		installed[name] = data
		// A Korean file name is stored in the archive as EUC-KR bytes, while
		// the name the title opens it by is a string constant out of its own
		// class file, which is Unicode. Both spellings answer, because which
		// one a title asks with is its business and neither is wrong.
		if decoded := string(decodeArchiveText([]byte(name))); decoded != name {
			installed[decoded] = data
		}
	}
	return installed, nil
}

func onlyJARName(reader *zip.Reader) (string, error) {
	var found *zip.File
	for _, file := range reader.File {
		if !strings.EqualFold(path.Ext(file.Name), ".jar") {
			continue
		}
		if found != nil {
			return "", fmt.Errorf("SKT archive holds more than one JAR (%q and %q)", found.Name, file.Name)
		}
		found = file
	}
	if found == nil {
		return "", fmt.Errorf("SKT archive has no JAR")
	}
	return found.Name, nil
}

// readArchiveEntry reads one entry by its exact stored name. It walks the
// entry list rather than calling Reader.Open, because Open takes a path in the
// io/fs sense and refuses a name that is not valid UTF-8 — and a Korean
// handset's archive carries EUC-KR file names, which one local title's data
// file is. That refusal reads as "invalid argument" on a file that is right
// there in the archive.
func readArchiveEntry(reader *zip.Reader, name string, spent *budget) ([]byte, error) {
	var found *zip.File
	for _, file := range reader.File {
		if file.Name == name {
			found = file
			break
		}
	}
	if found == nil {
		return nil, fmt.Errorf("SKT archive has no entry %q", name)
	}
	if found.UncompressedSize64 > spent.limits.entry {
		return nil, fmt.Errorf("SKT archive entry %q is too large (%d bytes)", name, found.UncompressedSize64)
	}
	data, err := readEntryWithin(found, spent.limits.entry)
	if err != nil {
		return nil, fmt.Errorf("SKT archive entry %q: %w", name, err)
	}
	// The container is read a few entries at a time — the descriptor, the JAR,
	// then the installed files — so the total is carried between the calls
	// rather than counted inside any one of them.
	if err := spent.spend(uint64(len(data))); err != nil {
		return nil, err
	}
	return data, nil
}

// decodeEUCKR converts descriptor text to UTF-8, leaving bytes that are not
// EUC-KR as they are: a repacked archive can carry an ASCII or UTF-8 .msd, and
// those decode unchanged.
func decodeArchiveText(data []byte) []byte {
	decoded, err := korean.EUCKR.NewDecoder().Bytes(data)
	if err != nil {
		return data
	}
	return decoded
}

// decodePlatformBytes and encodePlatformString are the handset's default
// charset, which is what `new String(byte[])` and `String.getBytes()` mean on
// these titles. Bytes that are not EUC-KR are left as they are rather than
// replaced, because a title also builds Strings out of content it packed
// itself.
func decodePlatformBytes(data []byte) string {
	decoded, err := korean.EUCKR.NewDecoder().Bytes(data)
	if err != nil {
		return strings.ToValidUTF8(string(data), "�")
	}
	return string(decoded)
}

func encodePlatformString(value string) []byte {
	encoded, err := korean.EUCKR.NewEncoder().Bytes([]byte(value))
	if err != nil {
		return []byte(value)
	}
	return encoded
}

// SaveOwner names the directory this title's saves live in.
//
// The handset addressed a title by its program number, and that is what the
// saves are keyed by: an id that stays put, where the title name is Korean
// text whose bytes depend on how the archive was packed. The manifest rule is the
// fallback for a bare JAR, which carries no such id.
//
// **The owner is one the Hosts accept.** The browser Host refuses an owner
// directory name that could steer a path or not survive a copy to another
// system, and a title it refused ran with no save store at all: every save was
// gone when the session closed, and nothing said so. An owner that passes is
// returned unchanged, because that is where its saves already are; one that
// does not is mapped to one that does, and MigrateSaveOwner moves what the
// command line wrote under the old name.
func SaveOwner(descriptor Descriptor) string {
	return acceptedSaveOwner(rawSaveOwner(descriptor))
}

// rawSaveOwner is the owner as earlier releases named it, before the Host's
// rule was applied. The command line joined it into its save root as it was.
func rawSaveOwner(descriptor Descriptor) string {
	if id, ok := descriptor.Property("DD-ProgName"); ok && strings.TrimSpace(id) != "" {
		return strings.TrimSpace(id)
	}
	return jarSaveOwner(descriptor)
}

// saveOwnerMaxBytes is the longest owner the browser Host accepts.
const saveOwnerMaxBytes = 64

// saveOwnerRefusedRune is the browser Host's rule for one character of an
// owner name: the path and Windows-illegal characters, controls, and the
// invisible direction marks that make a listing lie. The two rules must agree;
// the Host's is ownerRejected in internal/webhost.
func saveOwnerRefusedRune(character rune) bool {
	switch {
	case character < 0x20, character == 0x7f:
		return true
	case strings.ContainsRune(`/\:*?"<>|`, character):
		return true
	case character == 0x200e || character == 0x200f || character == 0x061c:
		return true
	case character >= 0x202a && character <= 0x202e:
		return true
	case character >= 0x2066 && character <= 0x2069:
		return true
	case character == 0xfeff:
		return true
	}
	return false
}

// acceptedSaveOwner maps an owner the Host would refuse to one it accepts, and
// leaves every other owner exactly as it is. A refused character becomes '_',
// as jarSaveOwner already does for a separator, a name too long keeps its
// start, and the result always ends in `~` and a digest of the whole name:
// "A: B" and "A? B" would otherwise share one directory, and so would either
// and a title whose own name is "A_ B".
func acceptedSaveOwner(owner string) string {
	refused := owner == "" || owner == "." || owner == ".." || len(owner) > saveOwnerMaxBytes
	for _, character := range owner {
		refused = refused || saveOwnerRefusedRune(character)
	}
	if !refused {
		return owner
	}
	mapped := strings.Map(func(character rune) rune {
		if saveOwnerRefusedRune(character) {
			return '_'
		}
		return character
	}, owner)
	digest := sha256.Sum256([]byte(owner))
	suffix := fmt.Sprintf("~%x", digest[:4])
	if cut := saveOwnerMaxBytes - len(suffix); len(mapped) > cut {
		for cut > 0 && !utf8.RuneStart(mapped[cut]) {
			cut--
		}
		mapped = mapped[:cut]
	}
	if mapped = strings.TrimSpace(mapped); mapped == "" {
		mapped = "unnamed"
	}
	return mapped + suffix
}

// MigrateSaveOwner moves a title's saves from the directory an earlier release
// named it by to the one SaveOwner names now, below one platform save root.
// Only the command line ever wrote there — the browser Host refused those
// names and ran without saves — so it belongs where a Host joins the owner to
// its root, before it claims the title's save directory.
//
// The quick-save slots and a set-aside generation of saves, which the save
// store keeps in its reserved directory named after the owner, move with
// them. Nothing is merged and nothing is replaced: when the current owner
// directory already exists the old one stays where it is, and so does a slot
// the current owner already has. An old name that is not a single directory
// directly below the root is never followed, and one a session or tool holds
// is left for the next start.
func MigrateSaveOwner(root string, descriptor Descriptor) error {
	raw, owner := rawSaveOwner(descriptor), SaveOwner(descriptor)
	if root == "" || raw == owner || !filepath.IsLocal(raw) || filepath.Base(raw) != raw ||
		strings.ContainsAny(raw, `/\`) || raw == saveReservedDirectory {
		return nil
	}
	source, target := filepath.Join(root, raw), filepath.Join(root, owner)
	if info, err := os.Lstat(source); errors.Is(err, fs.ErrNotExist) {
		return nil
	} else if err != nil {
		return fmt.Errorf("SKT save owner %q: %w", raw, err)
	} else if !info.IsDir() {
		return nil
	}
	if _, err := os.Lstat(target); err == nil {
		return nil
	} else if !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("SKT save owner %q: %w", owner, err)
	}
	release, err := backend.ClaimSaveDirectory(source)
	if err != nil {
		return fmt.Errorf("SKT save owner %q: %w", raw, err)
	}
	defer release()
	// An interrupted quick load is settled under the old name first, so what
	// moves is the tree it left rather than half of a replacement.
	if _, err := backend.NewDirectorySaveStore(source).DisplacedGeneration(); err != nil {
		return fmt.Errorf("SKT save owner %q: %w", raw, err)
	}
	// The reserved entries move before the saves: until the saves have moved,
	// a later start finds the old owner again and finishes the move.
	reserved := filepath.Join(root, saveReservedDirectory, "owners")
	entries, err := os.ReadDir(filepath.Join(reserved, raw))
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("SKT save owner %q: %w", raw, err)
	}
	for _, entry := range entries {
		name := entry.Name()
		slot := entry.Type().IsRegular() && strings.HasSuffix(name, ".wfq")
		if !slot && !(name == "previous" && entry.IsDir()) {
			continue
		}
		destination := filepath.Join(reserved, owner, name)
		if _, err := os.Lstat(destination); err == nil {
			continue
		}
		if err := os.MkdirAll(filepath.Join(reserved, owner), 0o700); err != nil {
			return fmt.Errorf("SKT save owner %q: %w", owner, err)
		}
		if err := os.Rename(filepath.Join(reserved, raw, name), destination); err != nil {
			return fmt.Errorf("move SKT quick save %q from %q to %q: %w", name, raw, owner, err)
		}
	}
	if err := os.Rename(source, target); err != nil {
		return fmt.Errorf("move SKT saves from %q to %q: %w", raw, owner, err)
	}
	return nil
}

// saveReservedDirectory is where a save store keeps quick-save slots and
// replacement state beside the owner directories of one platform root, in
// owners/<owner>. It is the save store's layout, restated here because a
// migration has to move what it keeps under an owner's old name.
const saveReservedDirectory = ".wfeature-quicksave"
