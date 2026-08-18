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
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", fmt.Errorf("read archive: %w", err)
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
		case strings.EqualFold(name, ktfEntry):
			return KTF, nil
		case strings.EqualFold(name, lgtEntry):
			return LGT, nil
		case strings.EqualFold(path.Ext(name), sktExtension):
			return SKT, nil
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
		return "", err
	}
	if skvm {
		return SKT, nil
	}
	if isKTFNativePackage(names, wrapper) {
		return KTF, nil
	}
	return Unknown, nil
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
