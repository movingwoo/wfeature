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

const (
	maxEntrySize   uint64 = 128 << 20
	maxArchiveSize uint64 = 512 << 20
)

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
	zipReader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("open SKT JAR: %w", detect.ContainerError(data, err))
	}

	entries := make(map[string][]byte, len(zipReader.File))
	var uncompressedSize uint64
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
		if file.UncompressedSize64 > maxEntrySize {
			return nil, fmt.Errorf("SKT JAR entry %q is too large (%d bytes)", name, file.UncompressedSize64)
		}
		if file.UncompressedSize64 > maxArchiveSize-uncompressedSize {
			return nil, fmt.Errorf("SKT JAR expands beyond %d bytes", maxArchiveSize)
		}
		uncompressedSize += file.UncompressedSize64
		contents, err := readEntry(file)
		if err != nil {
			return nil, fmt.Errorf("read SKT JAR entry %q: %w", name, err)
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

func readEntry(file *zip.File) ([]byte, error) {
	reader, err := file.Open()
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	data, err := io.ReadAll(io.LimitReader(reader, int64(maxEntrySize)+1))
	if err != nil {
		return nil, err
	}
	if uint64(len(data)) > maxEntrySize {
		return nil, fmt.Errorf("uncompressed entry exceeds %d bytes", maxEntrySize)
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
