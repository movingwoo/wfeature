// Package ktf detects and loads KTF application archives and their native
// client.bin images.
package ktf

import (
	"archive/zip"
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"path"
	"strconv"
	"strings"

	"github.com/movingwoo/wfeature/internal/zipentry"
	"unicode/utf8"
)

const (
	adfPath = "__adf__"

	maxArchiveInputSize = 128 << 20
	maxEntrySize        = uint64(128 << 20)
	maxExpandedSize     = uint64(512 << 20)
	maxEntryCount       = 8192
	maxEntryNameSize    = 4096
	maxADFSize          = 64 << 10
	maxClientMappedSize = uint64(256 << 20)
	maxDescriptorFields = 256
)

type Descriptor struct {
	AID       string
	PID       string
	MainClass string
	// Properties holds every descriptor field by upper-case name, including
	// the three above. Jlet.getAppProperty answers from it, which is what the
	// original runtime reads its manifest values from.
	Properties map[string]string
}

type ClientImage struct {
	Name    string
	Data    []byte
	BSSSize uint32
}

func (image ClientImage) MappedSize() uint64 {
	return uint64(len(image.Data)) + uint64(image.BSSSize)
}

type JAR struct {
	Entries map[string][]byte
	Client  ClientImage
}

type Archive struct {
	Descriptor Descriptor
	JARName    string
	Files      map[string][]byte
	JAR        *JAR
}

// GuestFiles composes the guest filesystem view of the outer archive: every
// file keeps its name, and the private "P/" or "p/" data prefix comes off so
// applications can open those entries by bare name.
func (archive *Archive) GuestFiles() map[string][]byte {
	if archive == nil {
		return nil
	}
	entries := make(map[string][]byte, len(archive.Files))
	for name, data := range archive.Files {
		trimmed := strings.TrimPrefix(strings.TrimPrefix(name, "P/"), "p/")
		entries[trimmed] = data
	}
	return entries
}

// SaveOwner names the directory a game's saves live under. It is the PID,
// not the AID: several distinct games ship the same AID — one local AID is
// shared by four unrelated titles — and a persisted save wins over packaged
// archive data, so an AID-keyed root lets one game's save shadow another
// game's shipped file. PIDs are per title, and the variants of one title (the
// same game reissued with modified drop rates) share a PID, which is the
// sharing we do want. Descriptors without a PID fall back to the AID
// rather than collapsing every such archive into one directory.
func SaveOwner(descriptor Descriptor) string {
	if descriptor.PID != "" {
		return descriptor.PID
	}
	return descriptor.AID
}

// ReadDescriptor parses only the archive descriptor. Tools that just need a
// game's identity — the save importer's PID to AID map, for one — skip the
// JAR and client image this way, so a title whose executable this build cannot
// load still contributes its identity.
func ReadDescriptor(data []byte) (Descriptor, error) {
	files, err := readOuterZIP(data)
	if err != nil {
		return Descriptor{}, err
	}
	adf, ok := findEntry(files, adfPath)
	if !ok {
		return Descriptor{}, fmt.Errorf("KTF archive has no %s", adfPath)
	}
	descriptor, err := ParseDescriptor(adf)
	if err != nil {
		return Descriptor{}, fmt.Errorf("parse %s: %w", adfPath, err)
	}
	return descriptor, nil
}

func Open(data []byte) (*Archive, error) {
	files, err := readOuterZIP(data)
	if err != nil {
		return nil, err
	}
	adf, ok := findEntry(files, adfPath)
	if !ok {
		return nil, fmt.Errorf("KTF archive has no %s", adfPath)
	}
	descriptor, err := ParseDescriptor(adf)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", adfPath, err)
	}
	jarName := descriptor.AID + ".jar"
	jarData, ok := findEntry(files, jarName)
	if !ok {
		return nil, fmt.Errorf("KTF archive has no AID JAR %q", jarName)
	}
	jar, err := OpenJAR(jarData)
	if err != nil {
		return nil, fmt.Errorf("open KTF JAR %q: %w", jarName, err)
	}
	return &Archive{
		Descriptor: descriptor,
		JARName:    jarName,
		Files:      files,
		JAR:        jar,
	}, nil
}

func OpenJAR(data []byte) (*JAR, error) {
	entries, err := readZIP(data, "KTF JAR")
	if err != nil {
		return nil, err
	}
	var client *ClientImage
	for name, data := range entries {
		if !strings.HasPrefix(name, "client.bin") {
			continue
		}
		bssSize, err := parseBSSSize(name)
		if err != nil {
			return nil, err
		}
		if client != nil {
			return nil, fmt.Errorf("KTF JAR contains multiple client.bin entries %q and %q", client.Name, name)
		}
		candidate := ClientImage{Name: name, Data: data, BSSSize: bssSize}
		if len(candidate.Data) == 0 {
			return nil, fmt.Errorf("KTF client image %q is empty", name)
		}
		if candidate.MappedSize() > maxClientMappedSize {
			return nil, fmt.Errorf("KTF client image %q maps %d bytes, limit %d", name, candidate.MappedSize(), maxClientMappedSize)
		}
		client = &candidate
	}
	if client == nil {
		return nil, fmt.Errorf("KTF JAR has no client.bin<BSS-size> entry")
	}
	return &JAR{Entries: entries, Client: *client}, nil
}

func ParseDescriptor(data []byte) (Descriptor, error) {
	if len(data) > maxADFSize {
		return Descriptor{}, fmt.Errorf("KTF descriptor exceeds %d bytes", maxADFSize)
	}
	values := make(map[string]string)
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 1024), maxADFSize)
	for scanner.Scan() {
		line := bytes.TrimSuffix(scanner.Bytes(), []byte{'\r'})
		keyBytes, valueBytes, found := bytes.Cut(line, []byte{':'})
		if !found {
			continue
		}
		key := strings.ToUpper(strings.TrimSpace(string(keyBytes)))
		if key == "" || len(values) >= maxDescriptorFields {
			continue
		}
		identifying := key == "AID" || key == "PID" || key == "MCLASS"
		valueBytes = bytes.TrimSpace(valueBytes)
		if !utf8.Valid(valueBytes) {
			if identifying {
				return Descriptor{}, fmt.Errorf("KTF descriptor field %s is not UTF-8", key)
			}
			// Descriptors are written on Korean handsets, so a free-form field
			// such as the application title arrives in the platform charset.
			values[key] = decodeEUCKR(valueBytes)
			continue
		}
		if _, duplicate := values[key]; duplicate && identifying {
			return Descriptor{}, fmt.Errorf("KTF descriptor repeats field %s", key)
		}
		values[key] = string(valueBytes)
	}
	if err := scanner.Err(); err != nil {
		return Descriptor{}, fmt.Errorf("read KTF descriptor: %w", err)
	}

	descriptor := Descriptor{
		AID:        values["AID"],
		PID:        values["PID"],
		MainClass:  strings.ReplaceAll(values["MCLASS"], ".", "/"),
		Properties: values,
	}
	if err := validateSimpleName("AID", descriptor.AID); err != nil {
		return Descriptor{}, err
	}
	if err := validateSimpleName("PID", descriptor.PID); err != nil {
		return Descriptor{}, err
	}
	if err := validateClassName(descriptor.MainClass); err != nil {
		return Descriptor{}, err
	}
	return descriptor, nil
}

func validateSimpleName(field, value string) error {
	if value == "" {
		return fmt.Errorf("KTF descriptor has no %s", field)
	}
	if len(value) > 255 {
		return fmt.Errorf("KTF descriptor %s exceeds 255 bytes", field)
	}
	if value == "." || value == ".." || strings.ContainsAny(value, "/\\\x00") {
		return fmt.Errorf("KTF descriptor %s %q is not a safe file name", field, value)
	}
	return nil
}

func validateClassName(name string) error {
	if name == "" {
		return fmt.Errorf("KTF descriptor has no MClass")
	}
	if len(name) > 1024 || strings.HasPrefix(name, "/") || strings.HasSuffix(name, "/") || strings.Contains(name, "//") || strings.ContainsRune(name, 0) {
		return fmt.Errorf("KTF descriptor MClass %q is invalid", name)
	}
	for _, component := range strings.Split(name, "/") {
		if component == "." || component == ".." {
			return fmt.Errorf("KTF descriptor MClass %q is invalid", name)
		}
	}
	return nil
}

func parseBSSSize(name string) (uint32, error) {
	suffix := strings.TrimPrefix(name, "client.bin")
	if suffix == "" {
		return 0, fmt.Errorf("KTF client image %q has no decimal BSS size", name)
	}
	value, err := strconv.ParseUint(suffix, 10, 32)
	if err != nil {
		return 0, fmt.Errorf("KTF client image %q has invalid BSS size: %w", name, err)
	}
	return uint32(value), nil
}

// readOuterZIP reads the archive itself, as opposed to the JAR inside it. The
// difference is that a repacked copy can have gained a containing folder, and
// the markers this loader looks up by exact name are then one level down. See
// internal/archive: a JAR never qualifies, so only the outer read asks.
func readOuterZIP(data []byte) (map[string][]byte, error) {
	files, err := readZIP(data, "KTF archive")
	if err != nil {
		return nil, err
	}
	return zipentry.Unwrap(files), nil
}

// outerZIPNames lists an archive's entries without decompressing any of them,
// normalized the way readOuterZIP normalizes them: folders are dropped, an
// unsafe entry refuses the archive, and a folder every entry sits inside is
// removed.
//
// It exists because a Host has to know which generation of package it is
// holding before it opens one, and opening one to find out costs every byte in
// it. The question is answered by the central directory alone, which the zip
// reader has already parsed to find anything at all.
func outerZIPNames(data []byte) ([]string, error) {
	if len(data) > maxArchiveInputSize {
		return nil, fmt.Errorf("KTF archive exceeds %d input bytes", maxArchiveInputSize)
	}
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("open KTF archive: %w", err)
	}
	if len(reader.File) > maxEntryCount {
		return nil, fmt.Errorf("KTF archive contains %d entries, limit %d", len(reader.File), maxEntryCount)
	}
	names := make([]string, 0, len(reader.File))
	for _, file := range reader.File {
		if len(file.Name) > maxEntryNameSize {
			return nil, fmt.Errorf("KTF archive entry name exceeds %d bytes", maxEntryNameSize)
		}
		name, err := safeEntryName(file.Name)
		if err != nil {
			return nil, fmt.Errorf("KTF archive contains unsafe entry: %w", err)
		}
		if file.FileInfo().IsDir() {
			continue
		}
		names = append(names, name)
	}
	if wrapper := zipentry.Directory(names); wrapper != "" {
		for index, name := range names {
			names[index] = strings.TrimPrefix(name, wrapper+"/")
		}
	}
	return names, nil
}

func readZIP(data []byte, label string) (map[string][]byte, error) {
	if len(data) > maxArchiveInputSize {
		return nil, fmt.Errorf("%s exceeds %d input bytes", label, maxArchiveInputSize)
	}
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", label, err)
	}
	if len(reader.File) > maxEntryCount {
		return nil, fmt.Errorf("%s contains %d entries, limit %d", label, len(reader.File), maxEntryCount)
	}

	entries := make(map[string][]byte, len(reader.File))
	names := make(map[string]string, len(reader.File))
	var expanded uint64
	for _, file := range reader.File {
		if len(file.Name) > maxEntryNameSize {
			return nil, fmt.Errorf("%s entry name exceeds %d bytes", label, maxEntryNameSize)
		}
		name, err := safeEntryName(file.Name)
		if err != nil {
			return nil, fmt.Errorf("%s contains unsafe entry: %w", label, err)
		}
		if file.FileInfo().IsDir() {
			continue
		}
		folded := strings.ToLower(name)
		if previous, duplicate := names[folded]; duplicate {
			return nil, fmt.Errorf("%s contains duplicate entries %q and %q", label, previous, name)
		}
		names[folded] = name
		if file.UncompressedSize64 > maxEntrySize {
			return nil, fmt.Errorf("%s entry %q is too large (%d bytes)", label, name, file.UncompressedSize64)
		}
		if file.UncompressedSize64 > maxExpandedSize-expanded {
			return nil, fmt.Errorf("%s expands beyond %d bytes", label, maxExpandedSize)
		}
		expanded += file.UncompressedSize64
		contents, err := readEntry(file)
		if err != nil {
			return nil, fmt.Errorf("read %s entry %q: %w", label, name, err)
		}
		entries[name] = contents
	}
	return entries, nil
}

func safeEntryName(name string) (string, error) {
	if strings.ContainsRune(name, 0) {
		return "", fmt.Errorf("entry path contains NUL")
	}
	normalized := strings.ReplaceAll(name, "\\", "/")
	cleaned := path.Clean(normalized)
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") || strings.HasPrefix(cleaned, "/") {
		return "", fmt.Errorf("entry path %q escapes the archive", name)
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
	if err != nil && (!errors.Is(err, zip.ErrChecksum) || uint64(len(data)) != file.UncompressedSize64 || crc32.ChecksumIEEE(data) != file.CRC32) {
		return nil, err
	}
	if uint64(len(data)) > maxEntrySize {
		return nil, fmt.Errorf("uncompressed entry exceeds %d bytes", maxEntrySize)
	}
	return data, nil
}

func findEntry(entries map[string][]byte, wanted string) ([]byte, bool) {
	for name, entry := range entries {
		if strings.EqualFold(name, wanted) {
			return entry, true
		}
	}
	return nil, false
}
