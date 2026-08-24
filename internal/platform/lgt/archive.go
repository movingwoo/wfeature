// Package lgt runs LGT (LG Telecom) handset games. An LGT title ships its
// code as a standard ARM ELF executable rather than KTF's raw image, and it
// reaches the platform through an import table instead of a callback struct.
// Those two differences are the whole platform.
package lgt

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/movingwoo/wfeature/internal/platform/detect"
	"github.com/movingwoo/wfeature/internal/zipentry"
)

// maxArchiveEntry bounds one file read out of an archive so a crafted zip
// cannot make the loader allocate without limit.
const maxArchiveEntry = 64 << 20

// binaryModuleName is the ELF executable inside the JAR.
const binaryModuleName = "binary.mod"

// Descriptor is the app_info file that sits beside the JAR: the identity the
// handset menu and the save store use.
type Descriptor struct {
	AID    string
	PID    string
	MClass string
	// Fields keeps every key app_info carried, because titles disagree about
	// which ones they set and a Host may want to show them.
	Fields map[string]string
}

// Archive is one LGT game: its descriptor, its ELF module, and the files the
// game reads at runtime.
type Archive struct {
	Descriptor Descriptor
	Module     []byte
	Resources  map[string][]byte
	// Packaged is what the archive carries *beside* the JAR. A handset stored
	// the whole download in the title's own directory, so a file packaged next
	// to the JAR is one the title can open by name — and three local archives
	// ship one: a title's own starting save, a certificate under a Korean
	// directory name, and a thirty-kilobyte data file. Dropping them made a
	// title that asks for its packaged save write a fresh empty one instead,
	// which is not what the handset did.
	//
	// It is kept apart from Resources rather than merged because the JAR is
	// the application and has to win a name collision.
	Packaged map[string][]byte
}

// SaveOwner names the directory this title's saves live in. It is the PID,
// which is the key the handset itself filed a title's databases under, so an
// imported save lands where it already belonged instead of having to be
// resolved onto some other identifier.
//
// The PID is the archive's claim about itself, and a repack can get it wrong:
// one archive here carried a PID, a purchase Objectid and a download URL
// copied wholesale from an unrelated title, with only the AID, name and icon
// names edited. Two titles claiming one PID would silently share a save
// directory, so the library checks for that rather than leaving it to be
// discovered by a corrupted save — see SaveOwnerCollisions.
//
// The AID is the fallback for an archive that omits the PID.
func SaveOwner(descriptor Descriptor) string {
	if descriptor.PID != "" {
		return descriptor.PID
	}
	if descriptor.AID != "" {
		return descriptor.AID
	}
	return "unnamed"
}

// ParseDescriptor reads an app_info file. It is a flat key=value list; the
// keys observed are AID, PID and MClass, and unknown ones are kept rather
// than rejected because a title that carries an extra one is not broken.
func ParseDescriptor(data []byte) (Descriptor, error) {
	descriptor := Descriptor{Fields: make(map[string]string)}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(strings.TrimSuffix(line, "\r"))
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, found := strings.Cut(line, "=")
		if !found {
			key, value, found = strings.Cut(line, ":")
		}
		if !found {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		descriptor.Fields[key] = value
		switch strings.ToLower(key) {
		case "aid":
			descriptor.AID = value
		case "pid":
			descriptor.PID = value
		case "mclass", "main_class", "mainclass":
			descriptor.MClass = value
		}
	}
	if descriptor.AID == "" && descriptor.PID == "" {
		return Descriptor{}, fmt.Errorf("LGT app_info names neither an AID nor a PID")
	}
	return descriptor, nil
}

// Open reads an LGT archive: a zip holding app_info and the game's JAR.
func Open(data []byte) (*Archive, error) {
	files, err := readZIP(data, "LGT archive")
	if err != nil {
		return nil, err
	}
	// A repacked copy can carry everything inside a folder named after the
	// game, which puts app_info one level below where this looks. See
	// internal/zipentry; the JAR read below deliberately does not ask, because
	// a JAR never qualifies.
	files = zipentry.Unwrap(files)
	info, ok := files["app_info"]
	if !ok {
		return nil, fmt.Errorf("LGT archive has no app_info")
	}
	descriptor, err := ParseDescriptor(info)
	if err != nil {
		return nil, err
	}

	jar, err := selectJAR(files, descriptor)
	if err != nil {
		return nil, err
	}
	entries, err := readZIP(jar, "LGT JAR")
	if err != nil {
		return nil, err
	}
	module, ok := entries[binaryModuleName]
	if !ok {
		return nil, fmt.Errorf("LGT JAR has no %s", binaryModuleName)
	}
	delete(entries, binaryModuleName)
	// The JAR itself is not a file the game opens, and neither is the
	// descriptor the loader already read.
	packaged := make(map[string][]byte, len(files))
	for name, data := range files {
		if strings.EqualFold(filepath.Ext(name), ".jar") || name == "app_info" {
			continue
		}
		packaged[name] = data
	}
	return &Archive{
		Descriptor: descriptor,
		Module:     module,
		Resources:  entries,
		Packaged:   packaged,
	}, nil
}

// selectJAR finds the game's JAR. It is normally named after the AID, but
// repacked archives keep the original JAR next to an edited app_info, and
// naming the classpath after an AID that is not in the archive would leave
// the loader with nothing to read — so the JAR that is actually there wins.
func selectJAR(files map[string][]byte, descriptor Descriptor) ([]byte, error) {
	if descriptor.AID != "" {
		if jar, ok := files[descriptor.AID+".jar"]; ok {
			return jar, nil
		}
	}
	var found []byte
	count := 0
	for name, data := range files {
		if strings.HasSuffix(strings.ToLower(name), ".jar") {
			found = data
			count++
		}
	}
	switch count {
	case 0:
		return nil, fmt.Errorf("LGT archive has no JAR")
	case 1:
		return found, nil
	}
	return nil, fmt.Errorf("LGT archive has no %s.jar and %d JARs to fall back to", descriptor.AID, count)
}

// Resource reads one packaged file, tolerating the leading slash a guest path
// carries.
func (archive *Archive) Resource(name string) ([]byte, bool) {
	if archive == nil {
		return nil, false
	}
	trimmed := strings.TrimPrefix(name, "/")
	if data, ok := archive.Resources[trimmed]; ok {
		return data, true
	}
	// Guests name resources without regard to case on a case-insensitive
	// handset filesystem.
	for key, data := range archive.Resources {
		if strings.EqualFold(key, trimmed) {
			return data, true
		}
	}
	// Then what the archive packaged beside the JAR. Second, so the
	// application's own resources win a name collision.
	if data, ok := archive.Packaged[trimmed]; ok {
		return data, true
	}
	for key, data := range archive.Packaged {
		if strings.EqualFold(key, trimmed) {
			return data, true
		}
	}
	return nil, false
}

func readZIP(data []byte, what string) (map[string][]byte, error) {
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", what, detect.ContainerError(data, err))
	}
	entries := make(map[string][]byte, len(reader.File))
	for _, file := range reader.File {
		if file.FileInfo().IsDir() {
			continue
		}
		name, err := safeEntryName(file.Name)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", what, err)
		}
		if file.UncompressedSize64 > maxArchiveEntry {
			return nil, fmt.Errorf("%s entry %q is %d bytes", what, name, file.UncompressedSize64)
		}
		opened, err := file.Open()
		if err != nil {
			return nil, fmt.Errorf("%s entry %q: %w", what, name, err)
		}
		content, err := io.ReadAll(io.LimitReader(opened, maxArchiveEntry+1))
		opened.Close()
		if err != nil {
			return nil, fmt.Errorf("%s entry %q: %w", what, name, err)
		}
		if len(content) > maxArchiveEntry {
			return nil, fmt.Errorf("%s entry %q exceeds %d bytes", what, name, maxArchiveEntry)
		}
		entries[name] = content
	}
	return entries, nil
}

// safeEntryName rejects the traversal names a hostile archive would use.
func safeEntryName(name string) (string, error) {
	cleaned := strings.TrimPrefix(strings.ReplaceAll(name, "\\", "/"), "./")
	if cleaned == "" || strings.HasPrefix(cleaned, "/") || strings.Contains(cleaned, "../") {
		return "", fmt.Errorf("entry name %q is unsafe", name)
	}
	return cleaned, nil
}
