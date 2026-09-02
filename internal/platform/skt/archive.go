package skt

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"path"
	"strings"

	"github.com/movingwoo/wfeature/internal/jvm/classfile"
	"github.com/movingwoo/wfeature/internal/platform/detect"
)

const manifestPath = "META-INF/MANIFEST.MF"

// An SKT title arrives as two zips, one inside the other, and both are bounded
// the same way — the way KTF and LGT already bound theirs. The per-entry bound
// alone is not enough: it bounds one file, and a zip that declares eight
// thousand of them is eight thousand times that. So the input, the entry count
// and what the whole thing expands to are bounded too.
type archiveLimits struct {
	input uint64 // the archive itself
	entry uint64 // one file read out of it
	total uint64 // everything read out of it
	count int    // how many files it may declare
}

var defaultArchiveLimits = archiveLimits{
	input: 128 << 20,
	entry: 128 << 20,
	total: 512 << 20,
	count: 8192,
}

// budget is one archive's running total, so that a reader which takes its
// entries a few at a time — the outer container reads the descriptor, the JAR
// and then the installed files — still cannot be walked past the total.
type budget struct {
	limits   archiveLimits
	expanded uint64
	what     string
}

// spend accounts for bytes actually read rather than for the size the zip
// declares, because a zip may lie about either. The subtraction is the way
// round that cannot overflow.
func (b *budget) spend(read uint64) error {
	if read > b.limits.total-b.expanded {
		return fmt.Errorf("%s expands beyond %d bytes", b.what, b.limits.total)
	}
	b.expanded += read
	return nil
}

// openWithin checks what a zip declares before anything is read out of it: the
// input size, and then the entry count, which is already in the directory the
// reader parsed and is the cheap half of the answer.
func openWithin(data []byte, what string, limits archiveLimits) (*zip.Reader, *budget, error) {
	if uint64(len(data)) > limits.input {
		return nil, nil, fmt.Errorf("%s exceeds %d input bytes", what, limits.input)
	}
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, nil, err
	}
	if len(reader.File) > limits.count {
		return nil, nil, fmt.Errorf("%s contains %d entries, limit %d", what, len(reader.File), limits.count)
	}
	return reader, &budget{limits: limits, what: what}, nil
}

type Archive struct {
	Descriptor Descriptor
	MainClass  *classfile.Class
	Entries    map[string][]byte
}

type Summary struct {
	Name      string `json:"name"`
	MainClass string `json:"mainClass"`
	// SaveOwner is the directory this title's RMS record stores live in. The
	// page addresses the save API by it, exactly as it does for KTF.
	SaveOwner         string `json:"saveOwner"`
	ClassMajorVersion uint16 `json:"classMajorVersion"`
	ClassMinorVersion uint16 `json:"classMinorVersion"`
	Methods           int    `json:"methods"`
	Fields            int    `json:"fields"`
	Entries           int    `json:"entries"`
}

// Open reads a MIDlet JAR that carries its own identity, which is the standard
// MIDlet packaging: the manifest inside names the MIDlet and its main class.
func openJAR(data []byte) (*Archive, error) {
	entries, err := readJAR(data)
	if err != nil {
		return nil, err
	}
	manifest, ok := findEntry(entries, manifestPath)
	if !ok {
		return nil, fmt.Errorf("SKT JAR has no %s", manifestPath)
	}
	descriptor, err := ParseDescriptor(manifest)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", manifestPath, err)
	}
	return newArchive(descriptor, entries)
}

// OpenWithDescriptor reads a MIDlet JAR whose identity lives outside it. SKT
// packages the JAD-style descriptor beside the JAR rather than in it, and the
// manifest of such a title names no MIDlet at all, so the main class can only
// come from the descriptor the archive shipped next to it.
func OpenWithDescriptor(data []byte, descriptor Descriptor) (*Archive, error) {
	entries, err := readJAR(data)
	if err != nil {
		return nil, err
	}
	return newArchive(descriptor, entries)
}

func readJAR(data []byte) (map[string][]byte, error) {
	return readJARWithin(data, defaultArchiveLimits)
}

// readJARWithin is readJAR with its bounds named, so a test can reach the far
// side of each one without building half a gigabyte to get there.
func readJARWithin(data []byte, limits archiveLimits) (map[string][]byte, error) {
	zipReader, spent, err := openWithin(data, "SKT JAR", limits)
	if err != nil {
		return nil, fmt.Errorf("open SKT JAR: %w", detect.ContainerError(data, err))
	}

	entries := make(map[string][]byte, len(zipReader.File))
	for _, file := range zipReader.File {
		if file.FileInfo().IsDir() {
			continue
		}
		name, err := safeEntryName(file.Name)
		if err != nil {
			return nil, err
		}
		if _, duplicate := entries[name]; duplicate {
			return nil, fmt.Errorf("SKT JAR contains duplicate entry %q", name)
		}
		if file.UncompressedSize64 > limits.entry {
			return nil, fmt.Errorf("SKT JAR entry %q is too large (%d bytes)", name, file.UncompressedSize64)
		}
		contents, err := readEntryWithin(file, limits.entry)
		if err != nil {
			return nil, fmt.Errorf("read SKT JAR entry %q: %w", name, err)
		}
		if err := spent.spend(uint64(len(contents))); err != nil {
			return nil, err
		}
		entries[name] = contents
	}
	return entries, nil
}

func newArchive(descriptor Descriptor, entries map[string][]byte) (*Archive, error) {
	classPath := descriptor.MainClass + ".class"
	classData, ok := entries[classPath]
	if !ok {
		return nil, fmt.Errorf("SKT JAR has no main class %q", classPath)
	}
	mainClass, err := classfile.Parse(classData)
	if err != nil {
		return nil, fmt.Errorf("parse main class %q: %w", classPath, err)
	}
	if mainClass.Name != descriptor.MainClass {
		return nil, fmt.Errorf("main class path names %q but class file declares %q", descriptor.MainClass, mainClass.Name)
	}

	return &Archive{
		Descriptor: descriptor,
		MainClass:  mainClass,
		Entries:    entries,
	}, nil
}

func (a *Archive) Summary() Summary {
	return Summary{
		Name:              a.Descriptor.Name,
		MainClass:         a.MainClass.Name,
		SaveOwner:         SaveOwner(a.Descriptor),
		ClassMajorVersion: a.MainClass.MajorVersion,
		ClassMinorVersion: a.MainClass.MinorVersion,
		Methods:           len(a.MainClass.Methods),
		Fields:            len(a.MainClass.Fields),
		Entries:           len(a.Entries),
	}
}

func (a *Archive) ClassBytes(name string) ([]byte, bool) {
	data, ok := a.Entries[name+".class"]
	return data, ok
}

// addInstalledFiles records the files an SKT container carried beside the JAR.
// They are what the title's own filesystem held on a handset, and a game opens
// them for writing without asking for creation because on a handset they were
// already there. A JAR entry of the same name wins: the JAR is the title's
// code and resources, and a container file has no business shadowing one.
func (a *Archive) addInstalledFiles(installed map[string][]byte) {
	for name, data := range installed {
		if _, packaged := a.Entries[name]; packaged {
			continue
		}
		a.Entries[name] = data
	}
}

func (a *Archive) Resource(name string) ([]byte, bool) {
	data, ok := a.Entries[name]
	return data, ok
}

func safeEntryName(name string) (string, error) {
	name = strings.ReplaceAll(name, "\\", "/")
	cleaned := path.Clean(name)
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") || strings.HasPrefix(cleaned, "/") {
		return "", fmt.Errorf("SKT JAR contains unsafe entry path %q", name)
	}
	return cleaned, nil
}

func readEntryWithin(file *zip.File, limit uint64) ([]byte, error) {
	reader, err := file.Open()
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	data, err := io.ReadAll(io.LimitReader(reader, int64(limit)+1))
	if err != nil {
		return nil, err
	}
	if uint64(len(data)) > limit {
		return nil, fmt.Errorf("uncompressed entry exceeds %d bytes", limit)
	}
	return data, nil
}

func findEntry(entries map[string][]byte, wanted string) ([]byte, bool) {
	if entry, ok := entries[wanted]; ok {
		return entry, true
	}
	for name, entry := range entries {
		if strings.EqualFold(name, wanted) {
			return entry, true
		}
	}
	return nil, false
}
