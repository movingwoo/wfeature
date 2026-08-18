// Package skt runs SKT handset games. There is no native code and no custom
// executable format here: an SKT title is a MIDlet JAR whose world contains
// the SKVM class surface on top of standard MIDP, so this package is a Java
// runtime — class loading through the shared JVM, the MIDP surface the title
// draws and saves through, and the SKVM classes on top.
//
// That runtime used to be a vendor-neutral "j2me" package with SKT as a thin
// layer over it. It is all here now, because SKT is the only vendor that ever
// asked for it: a bare MIDlet with no carrier behind it is not something this
// emulator supports, and a neutral package with one consumer only hid where
// its contracts really came from.
package skt

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"path"
	"strings"

	"golang.org/x/text/encoding/korean"
)

// The descriptor an SKT archive ships beside its JAR. It is JAD-shaped —
// the same `MIDlet-1: <name>,<icon>,<class>` lines a manifest carries — which
// is why the shared SKT descriptor parser reads it.
const msdSuffix = ".msd"

// Open reads an SKT title, in either of the two shapes one arrives in.
//
// A handset was sent an archive: a zip holding `<id>.jar` beside `<id>.msd`,
// and the JAR's own manifest names no MIDlet at all — the identity is in the
// .msd. A bare JAR that does name its MIDlet is the other shape, and it is
// what the fixtures and any repacked title look like.
func Open(data []byte) (*Archive, error) {
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
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return Descriptor{}, nil, nil, nil
	}
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
	msd, err := readArchiveEntry(reader, msdName)
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
	jar, err := readArchiveEntry(reader, strings.TrimSuffix(msdName, path.Ext(msdName))+".jar")
	if err != nil {
		jar, err = onlyJAR(reader)
		if err != nil {
			return Descriptor{}, nil, nil, err
		}
	}
	installed, err := installedFiles(reader)
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
// .msd, and the two files the platform itself was sent.
func installedFiles(reader *zip.Reader) (map[string][]byte, error) {
	installed := make(map[string][]byte)
	for _, file := range reader.File {
		if file.FileInfo().IsDir() {
			continue
		}
		switch strings.ToLower(path.Ext(file.Name)) {
		case ".jar", msdSuffix, ".mod", ".wmr":
			continue
		}
		name := path.Base(file.Name)
		if name == "" || name == "." {
			continue
		}
		data, err := readArchiveEntry(reader, file.Name)
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

func onlyJAR(reader *zip.Reader) ([]byte, error) {
	var found *zip.File
	for _, file := range reader.File {
		if !strings.EqualFold(path.Ext(file.Name), ".jar") {
			continue
		}
		if found != nil {
			return nil, fmt.Errorf("SKT archive holds more than one JAR (%q and %q)", found.Name, file.Name)
		}
		found = file
	}
	if found == nil {
		return nil, fmt.Errorf("SKT archive has no JAR")
	}
	return readArchiveEntry(reader, found.Name)
}

// readArchiveEntry reads one entry by its exact stored name. It walks the
// entry list rather than calling Reader.Open, because Open takes a path in the
// io/fs sense and refuses a name that is not valid UTF-8 — and a Korean
// handset's archive carries EUC-KR file names, which one local title's data
// file is. That refusal reads as "invalid argument" on a file that is right
// there in the archive.
func readArchiveEntry(reader *zip.Reader, name string) ([]byte, error) {
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
	opened, err := found.Open()
	if err != nil {
		return nil, err
	}
	defer opened.Close()
	data, err := io.ReadAll(io.LimitReader(opened, maxArchiveEntrySize+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maxArchiveEntrySize {
		return nil, fmt.Errorf("SKT archive entry %q exceeds %d bytes", name, maxArchiveEntrySize)
	}
	return data, nil
}

const maxArchiveEntrySize int64 = 128 << 20

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
func SaveOwner(descriptor Descriptor) string {
	if id, ok := descriptor.Property("DD-ProgName"); ok && strings.TrimSpace(id) != "" {
		return strings.TrimSpace(id)
	}
	return jarSaveOwner(descriptor)
}
