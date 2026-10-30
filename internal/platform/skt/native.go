package skt

import (
	"archive/zip"
	"bytes"
	"fmt"
	"path"
	"strings"
)

// A native SKT package is the directory a handset installed a WIPI-C title
// into, copied out whole: the executable `<id>.daf`, the descriptor
// `<id>.mod`, the icons `<id>.wmr`, and whatever files the title keeps beside
// them — its data and the saves it had written. The ten-digit id is the
// title's program number, and it names both the files and, usually, the
// directory around them.
//
// The descriptor is the same binary record an SKT Java package carries (its
// media type says `application/x-wipi-app` and its package type `daf` where a
// Java title's says `application/x-wipi-jar`); it names no title, no screen
// and nothing the runtime needs, so the executable is what is read. See
// docs/skt-native.md.
const (
	nativeExecutableExtension = ".daf"
	nativeDescriptorExtension = ".mod"
	nativeIconsExtension      = ".wmr"
)

// NativeArchive is an opened native package.
type NativeArchive struct {
	// ID is the program number the executable is named by.
	ID string
	// Executable is the parsed `.daf`, before it is placed anywhere.
	Executable *NativeExecutable
	// Files are the title's own files, by the bare name it opens them with:
	// everything in the package except the executable, the descriptor and the
	// icons. They are what its storage held on the handset, saves included.
	Files map[string][]byte
}

// IsNativeArchive reports whether data is a native package. It reads the
// central directory and nothing else, for the reason the KTF loader's
// equivalent does: a Host asks it on the way to every other question.
func IsNativeArchive(data []byte) bool {
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return false
	}
	names := make([]string, 0, len(reader.File))
	for _, file := range reader.File {
		names = append(names, file.Name)
	}
	_, ok := nativeExecutableName(names)
	return ok
}

// nativeExecutableName finds the one executable whose descriptor sits beside
// it under the same name. The pair is what marks the package: a `.daf` alone
// could be anything, and a `.mod` alone is the Java package's descriptor too.
func nativeExecutableName(names []string) (string, bool) {
	found := ""
	descriptors := make(map[string]bool)
	for _, name := range names {
		clean := path.Clean(strings.ReplaceAll(name, "\\", "/"))
		if strings.EqualFold(path.Ext(clean), nativeDescriptorExtension) {
			descriptors[strings.ToLower(strings.TrimSuffix(clean, path.Ext(clean)))] = true
		}
	}
	for _, name := range names {
		clean := path.Clean(strings.ReplaceAll(name, "\\", "/"))
		if !strings.EqualFold(path.Ext(clean), nativeExecutableExtension) {
			continue
		}
		if found != "" {
			// Two executables are two titles, and choosing one would be a
			// guess about which the person meant.
			return "", false
		}
		if !descriptors[strings.ToLower(strings.TrimSuffix(clean, path.Ext(clean)))] {
			return "", false
		}
		found = name
	}
	return found, found != ""
}

// OpenNative reads a native package. It is bounded the way the Java package
// is: the input, the entry count, each entry and the total read.
func OpenNative(data []byte) (*NativeArchive, error) {
	return openNativeWithin(data, defaultArchiveLimits)
}

func openNativeWithin(data []byte, limits archiveLimits) (*NativeArchive, error) {
	reader, spent, err := openWithin(data, "SKT native package", limits)
	if err != nil {
		return nil, fmt.Errorf("open SKT native package: %w", err)
	}
	names := make([]string, 0, len(reader.File))
	for _, file := range reader.File {
		names = append(names, file.Name)
	}
	executableName, ok := nativeExecutableName(names)
	if !ok {
		return nil, fmt.Errorf("open SKT native package: no single %s beside a %s of the same name", nativeExecutableExtension, nativeDescriptorExtension)
	}
	clean := path.Clean(strings.ReplaceAll(executableName, "\\", "/"))
	id := strings.TrimSuffix(path.Base(clean), path.Ext(clean))
	if !validNativeID(id) {
		return nil, fmt.Errorf("open SKT native package: executable name %q is not a program number", path.Base(clean))
	}
	image, err := readArchiveEntry(reader, executableName, spent)
	if err != nil {
		return nil, fmt.Errorf("open SKT native package: %w", err)
	}
	executable, err := ParseNativeExecutable(image)
	if err != nil {
		return nil, fmt.Errorf("open SKT native package %s: %w", path.Base(clean), err)
	}
	files, err := nativeFiles(reader, spent, executableName)
	if err != nil {
		return nil, fmt.Errorf("open SKT native package: %w", err)
	}
	return &NativeArchive{ID: id, Executable: executable, Files: files}, nil
}

// validNativeID accepts the decimal program number a package names its files
// by. It becomes a save directory, so nothing else is let through.
func validNativeID(id string) bool {
	if id == "" || len(id) > 32 {
		return false
	}
	for _, symbol := range id {
		if symbol < '0' || symbol > '9' {
			return false
		}
	}
	return true
}

// nativeFiles reads the title's own files. They sit in the executable's
// directory on the handset, and a dump keeps that directory or flattens it;
// either way a file is filed by its path below the executable's directory,
// which is the name the title opens it by. A file outside that directory is
// not the title's and is left out.
func nativeFiles(reader *zip.Reader, spent *budget, executableName string) (map[string][]byte, error) {
	home := path.Dir(path.Clean(strings.ReplaceAll(executableName, "\\", "/")))
	files := make(map[string][]byte)
	for _, file := range reader.File {
		if file.FileInfo().IsDir() || file.Name == executableName {
			continue
		}
		name, err := safeEntryName(file.Name)
		if err != nil {
			return nil, err
		}
		if home != "." {
			if !strings.HasPrefix(name, home+"/") {
				continue
			}
			name = strings.TrimPrefix(name, home+"/")
		}
		if !strings.Contains(name, "/") {
			switch strings.ToLower(path.Ext(name)) {
			case nativeExecutableExtension, nativeDescriptorExtension, nativeIconsExtension:
				continue
			}
		}
		data, err := readArchiveEntry(reader, file.Name, spent)
		if err != nil {
			return nil, fmt.Errorf("read SKT native file %q: %w", file.Name, err)
		}
		files[name] = data
	}
	return files, nil
}

// ProgramFiles is the file tree the title sees: its own files, and the
// resources its executable carries, at their names below the program's
// directory. A title reads the second through the file calls as well as the
// resource calls — one lists `data/mob`, asks whether `png/iequip/0.sbmp`
// exists and opens `data/str/gl.dat`, all of them resources named
// `/data/...` and `/png/...`, and stops with a data error when they are not
// there; the other asks the size of `0_info_res_wc.dat` before it reads it.
// A file the title's directory holds wins over a resource of the same name,
// because the directory is where the title writes.
func (archive *NativeArchive) ProgramFiles() map[string][]byte {
	if archive == nil {
		return nil
	}
	files := make(map[string][]byte, len(archive.Files))
	if archive.Executable != nil {
		for name, data := range archive.Executable.Resources {
			name = strings.TrimPrefix(path.Clean("/"+name), "/")
			if name != "" {
				files[name] = data
			}
		}
	}
	// A device dump carries the previous owner's install-directory state —
	// its saves, device-bound to the handset it was dumped from. A recognized
	// dump withholds that state so the game starts fresh and writes its own,
	// rather than stopping on another handset's saves; its read-only resources
	// above are still provided. See docs/skt-native.md#skt-native-identity-discard.
	if archive.Executable != nil && archive.Executable.DiscardsOwnerState() {
		return files
	}
	for name, data := range archive.Files {
		files[name] = data
	}
	return files
}

// NativeSaveOwner names the directory a native title's saves belong in: its
// program number, as an SKT Java title's is.
func NativeSaveOwner(archive *NativeArchive) string {
	if archive == nil {
		return ""
	}
	return archive.ID
}
