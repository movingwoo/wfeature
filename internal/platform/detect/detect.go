// Package detect names the platform an archive belongs to.
//
// A Host handed a file has to answer this before it can do anything else, and
// the answer is in the archive's shape rather than its name: the same `.zip`
// extension covers a KTF game, an LGT game, and a bare MIDlet JAR, and a game
// picker listing a directory has only the bytes to go on.
package detect

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"path"
	"strings"

	"github.com/movingwoo/wfeature/internal/zipentry"
)

// Platform is the loader an archive belongs to. The values are the names the
// CLI subcommands and the browser exports use, so a detected platform can be
// reported to a user without translation.
type Platform string

const (
	KTF Platform = "ktf"
	LGT Platform = "lgt"
	SKT Platform = "skt"
	// Unknown is an archive none of the vendors claimed. It is a real answer
	// rather than an error because "I cannot tell what this is" and "this is
	// damaged" are different problems and a Host should be able to say which.
	//
	// It used to be J2ME: anything unclaimed was reported as a bare MIDlet.
	// That was wrong twice over — a plain MIDlet with no carrier behind it is
	// not something this emulator runs, and naming one hid every detection
	// failure behind a platform that would then fail to load it.
	Unknown Platform = "unknown"
)

// Reason says why no platform claimed a file. `Unknown` on its own is the
// answer to four different questions, and the comment above it already says
// what is wrong with that: "I cannot tell what this is" and "this is damaged"
// are different problems and a Host should be able to say which. Only one of
// the four is this project's own work to do — a file that is one of these
// packages and was not recognised — and without a reason beside the answer,
// a sweep over a corpus cannot say how many of the files it could not open are
// that one.
//
// The reason is a second return value rather than a second set of Platform
// values, so every caller that switches on the platform keeps working and only
// a caller that wants to count reaches for it.
type Reason string

const (
	// ReasonClaimed is the reason a file that a platform claimed has: none.
	ReasonClaimed Reason = ""
	// ReasonNotAnArchive is a file that is not a zip and is not a container
	// format this recognises either. A truncated download and a text file both
	// land here, and neither is a package that failed to be understood.
	ReasonNotAnArchive Reason = "not-an-archive"
	// ReasonKnownFormatUnsupported is a container this names but does not
	// read — see containerFormats below. The bytes are an archive of some
	// kind, so there may well be a package inside it, but not one reachable
	// from here.
	ReasonKnownFormatUnsupported Reason = "known-format-unsupported"
	// ReasonDRMWrapped is a package that was locked before it was distributed.
	// The header says so in the clear; the content is encrypted and the key is
	// not this project's to have. Counting these apart matters because no
	// amount of work on the loaders reaches them.
	ReasonDRMWrapped Reason = "drm-wrapped"
	// ReasonArchiveOfArchives is a zip holding nothing but whole packages.
	// There is no game in the outer file to run, only a choice of the ones
	// inside it, and that choice belongs to the person holding it.
	ReasonArchiveOfArchives Reason = "archive-of-archives"
	// ReasonNoMarker is a readable zip that carries no marker any platform
	// recognises. **This is the one worth investigating**: either it is not
	// one of these packages at all, or it is one whose shape is not yet known
	// here.
	ReasonNoMarker Reason = "no-marker"
)

// The entry each platform archive is required to carry. Every one of these
// loaders fails without it, which is what makes it a discriminator rather than
// a hint. KTF and LGT name theirs exactly; an SKT archive names its descriptor
// after the title, so the extension is the marker.
const (
	ktfEntry     = "__adf__"
	lgtEntry     = "app_info"
	sktExtension = ".msd"
)

// An earlier generation of the KTF download package carries no descriptor and
// no JAR: a module information file beside a raw module, and the title's own
// resource files. It is the same vendor and the same platform name — what
// differs is the package, and telling a Host otherwise would make it look for
// a loader that does not exist.
//
// The pair is the discriminator, and it has to be a pair: a lone `.mod` is a
// common enough extension that claiming an archive for it would be a guess,
// while a `.mif` beside exactly one `.mod` is this package's shape. See
// docs/ktf.md, "An earlier KTF package".
const (
	ktfNativeInfoExtension   = ".mif"
	ktfNativeModuleExtension = ".mod"
)

// skvmPrefixes are class-name prefixes only an SKT title refers to. A class
// file stores the names it references verbatim in its constant pool, so a
// title calling com.skt.m.Device carries the string; a plain MIDlet, whose
// world contains no such class, never does.
var skvmPrefixes = [][]byte{
	[]byte("com/skt/m/"),
	[]byte("com/skt/m3d/"),
	[]byte("com/xce/"),
}

// Scanning for the SKVM surface has to decompress class files, so it is
// bounded: a JAR is small next to a platform archive, but nothing here should
// be able to turn a malformed upload into unbounded work.
const (
	maxScannedEntry = 4 << 20
	maxScannedTotal = 64 << 20
)

// Archive names the platform that loads data, which is a zip in every case.
//
// The platform archives are told apart by an entry name alone, which costs
// only the central directory the reader has already parsed — the tens of
// megabytes of a KTF archive are never decompressed to answer this. Anything
// else is a bare JAR, and the only question left is whether the SKVM class
// surface belongs in its world: an SKT title repacked as a plain JAR has lost
// the .msd that would have named it. A JAR without that surface is claimed by
// nobody.
func Archive(data []byte) (Platform, error) {
	platform, _, err := Classify(data)
	return platform, err
}

// Classify is Archive with the reason no platform claimed the file. A caller
// that only wants to load a game asks Archive; a caller counting what a
// directory holds asks this, because the count that matters is not how many
// files were unclaimed but which of them were unclaimed for a reason this
// project could do something about.
//
// The reason is filled in for a refusal as well as for a plain answer: a file
// that is not a zip comes back with the error the reader gave and with the
// reason that says whether it was a container of another kind, a locked
// package, or nothing recognisable at all.
func Classify(data []byte) (Platform, Reason, error) {
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", unreadableReason(data), fmt.Errorf("read archive: %w", ContainerError(data, err))
	}
	// A repacked copy can carry the whole archive inside a folder named after
	// the game, which leaves every marker one level down from where the
	// loaders look. Removing that folder here keeps detection agreeing with
	// the loaders, which remove it too; see internal/zipentry for why it cannot
	// affect an archive that already works.
	names := make([]string, 0, len(reader.File))
	for _, file := range reader.File {
		names = append(names, entryName(file.Name))
	}
	wrapper := zipentry.Directory(names)

	for _, name := range names {
		// Both loaders match their marker case-insensitively against the whole
		// entry name, so the same rule decides here. Naming a platform whose
		// loader would then reject the archive helps nobody.
		if wrapper != "" {
			name = strings.TrimPrefix(name, wrapper+"/")
		}
		switch {
		// The two named markers are matched on the last element rather than
		// the whole name, because one local copy is a dump of the handset's
		// own application directory and keeps the whole title under
		// `W/apps/<AID>/`. The loader roots such an archive at its descriptor;
		// this is the same rule at detection, and the names are distinctive
		// enough that nothing else wears one.
		case strings.EqualFold(path.Base(name), ktfEntry):
			return KTF, ReasonClaimed, nil
		case strings.EqualFold(path.Base(name), lgtEntry):
			return LGT, ReasonClaimed, nil
		case strings.EqualFold(path.Ext(name), sktExtension):
			return SKT, ReasonClaimed, nil
		}
	}
	// Evidence before a guess. The pair below is a shape rather than a marker,
	// and `.mod` is a common enough extension that an SKT title repacked
	// without its `.msd` could wear it by accident; reading the constant pool
	// says what a class file actually links against. The order costs nothing
	// on a real package of the earlier generation, which has no classes to
	// scan at all.
	skvm, err := referencesSKVM(reader)
	if err != nil {
		return "", ReasonNoMarker, err
	}
	if skvm {
		return SKT, ReasonClaimed, nil
	}
	if isKTFNativePackage(names, wrapper) {
		return KTF, ReasonClaimed, nil
	}
	// A zip of zips is refused by its own shape rather than by a marker it has
	// no room for, and saying so is the difference between "there is a choice
	// to make here" and "this is not a package".
	if archiveOfArchives(names) {
		return Unknown, ReasonArchiveOfArchives, nil
	}
	return Unknown, ReasonNoMarker, nil
}

// unreadableReason says what a file the zip reader refused was instead. The
// order is what the bytes at the front of it declare: a locked container names
// itself, another archive format names itself, and a file that names nothing
// is a file this does not recognise.
func unreadableReason(data []byte) Reason {
	if _, wrapped := DCFHeader(data); wrapped {
		return ReasonDRMWrapped
	}
	if ContainerFormat(data) != "" {
		return ReasonKnownFormatUnsupported
	}
	return ReasonNotAnArchive
}

// isKTFNativePackage reports whether the entry names are the earlier KTF
// package's. It is only asked after every marker has failed and after the
// class scan, so it costs nothing on an archive that named itself and cannot
// claim one whose classes say otherwise.
func isKTFNativePackage(names []string, wrapper string) bool {
	info, module := 0, 0
	for _, name := range names {
		if wrapper != "" {
			name = strings.TrimPrefix(name, wrapper+"/")
		}
		switch {
		case strings.EqualFold(path.Ext(name), ktfNativeInfoExtension):
			info++
		case strings.EqualFold(path.Ext(name), ktfNativeModuleExtension):
			module++
		}
	}
	return info == 1 && module == 1
}

// entryName normalizes a zip entry the way the platform loaders do, so a
// backslash-separated or `./`-prefixed marker is recognized here exactly when
// it would be recognized there.
func entryName(name string) string {
	return path.Clean(strings.ReplaceAll(name, "\\", "/"))
}

func referencesSKVM(reader *zip.Reader) (bool, error) {
	scanned := 0
	for _, file := range reader.File {
		if !strings.HasSuffix(strings.ToLower(entryName(file.Name)), ".class") {
			continue
		}
		if file.UncompressedSize64 > maxScannedEntry || scanned >= maxScannedTotal {
			continue
		}
		opened, err := file.Open()
		if err != nil {
			return false, fmt.Errorf("read archive entry %q: %w", file.Name, err)
		}
		contents, err := io.ReadAll(io.LimitReader(opened, maxScannedEntry))
		opened.Close()
		if err != nil {
			// A corrupt entry says nothing about the platform; another class
			// in the same JAR still can.
			continue
		}
		scanned += len(contents)
		for _, prefix := range skvmPrefixes {
			if bytes.Contains(contents, prefix) {
				return true, nil
			}
		}
	}
	return false, nil
}

// containerFormats are the archive formats a person plausibly has a game in
// that are not the one this reads. A local set of packages holds three ALZ
// files and one RAR whose extension had been changed to `.zip`, and every one
// of them came back as "not a valid zip file" — which is true and tells the
// person nothing about what to do. Naming the format does.
var containerFormats = []struct {
	magic []byte
	name  string
}{
	{[]byte("Rar!\x1a\x07"), "a RAR archive"},
	{[]byte("ALZ\x01"), "an ALZ archive"},
	{[]byte("7z\xbc\xaf\x27\x1c"), "a 7-Zip archive"},
	{[]byte("\x1f\x8b"), "a gzip stream"},
	{[]byte("BZh"), "a bzip2 stream"},
	{[]byte("\xfd7zXZ\x00"), "an XZ stream"},
}

// ContainerFormat names the archive format some bytes are in when it is one
// this emulator does not read, and answers empty otherwise — including for a
// zip, which every caller is already trying to read as one.
func ContainerFormat(data []byte) string {
	for _, format := range containerFormats {
		if bytes.HasPrefix(data, format.magic) {
			return format.name
		}
	}
	return ""
}

// ArchiveOfArchives reports whether a zip holds nothing but other zips.
//
// A person collecting these ends up with one sooner or later: somebody bagged
// three episodes of the same game into a single file, and each of the three is
// a whole archive that runs on its own. This is not a container to learn to
// read — there is no game in the outer zip to run, only a choice of three that
// belongs to the person rather than to a loader — so what a Host owes them is
// the shape of what they picked. Without it the refusal is the descriptor's
// ("no __adf__"), which reads as damage.
func ArchiveOfArchives(data []byte) bool {
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return false
	}
	names := make([]string, 0, len(reader.File))
	for _, file := range reader.File {
		names = append(names, entryName(file.Name))
	}
	return archiveOfArchives(names)
}

// archiveOfArchives is the same question asked of entry names that have
// already been read, which is what Classify has in front of it by the time it
// reaches this.
func archiveOfArchives(names []string) bool {
	inner := 0
	for _, name := range names {
		if name == "" || strings.HasSuffix(name, "/") {
			continue
		}
		// A repack carries the packer's own leavings, and they say nothing
		// about what is in the bag.
		if base := path.Base(name); base == ".DS_Store" || strings.HasPrefix(name, "__MACOSX/") {
			continue
		}
		if !strings.EqualFold(path.Ext(name), ".zip") {
			return false
		}
		inner++
	}
	return inner > 0
}

// ContainerError turns a zip reader's refusal into one that names the format
// when the bytes are a different kind of archive. A loader wraps its own
// refusal with this so the message a person is shown says which file they
// picked rather than only that it was not the right one.
func ContainerError(data []byte, err error) error {
	if format := ContainerFormat(data); format != "" {
		return fmt.Errorf("%w: this is %s, which this emulator does not read", err, format)
	}
	return err
}
