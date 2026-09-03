package ktf

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"path"
	"strings"
)

// An earlier KTF download package carries no descriptor and no JAR. Where the
// package this platform grew up on holds an `__adf__` beside a JAR whose
// `client.bin<BSS-size>` entry is the executable, this one holds a module
// information file, a raw ARM module, and the title's own resource files.
// See docs/ktf.md, "An earlier KTF package".
const (
	nativeInfoExtension   = ".mif"
	nativeModuleExtension = ".mod"

	// Every module information file ends with these four bytes. It is the only
	// self-identifying mark the package has — there is no leading magic — so it
	// is what tells a truncated or unrelated file from a real one.
	nativeInfoTrailerMagic = "1fim"

	maxNativeInfoSize   = 1 << 20
	maxNativeModuleSize = 32 << 20
	maxNativeInfoSpans  = 64
	maxNativeInfoText   = 4 << 10

	// nativePageSize is the granularity the information file rounds the
	// module's mapped size to.
	nativePageSize = 0x1000
)

// NativeIcon is one length-prefixed, MIME-typed blob from the information file.
type NativeIcon struct {
	MIME string
	Data []byte
}

// NativeInfo is a parsed module information file.
//
// Only part of this file is understood. The span table, the MIME-typed icons,
// the text spans and the trailing identity record are decoded; the numeric
// spans are carried verbatim as Records because what each word means has not
// been established. One of them holds the same image base this platform
// already maps a KTF executable at, which is corroboration rather than a
// contract, so the loader does not read its address from here.
type NativeInfo struct {
	ApplicationID uint32
	Name          string
	Vendor        string
	Icons         []NativeIcon
	// Sections holds the two header sections that precede the span table, as
	// words. They are kept verbatim beside the decoded Fields because a
	// section whose length is not a whole number of records still says what
	// bytes were there.
	Sections [][]uint32
	// Fields holds the header sections decoded on the stride they are written
	// at. See NativeField.
	Fields []NativeField
	// Records holds every numeric span as words, in file order.
	Records [][]uint32
	// Trailer holds the identity record after the last span, words first.
	Trailer []uint32
}

// NativeField is one record of a header section.
//
// The stride is eight bytes and the tag is the last halfword, which is what an
// aligned word scan of the same bytes cannot see: it reads the boundary between
// two records as a value of its own, and that is how an earlier pass mapped
// 0x85000 for a module needing 0x25000.
//
//	value 0x00060001  extra 1  tag 2
//	value 0x03e80001  extra 0  tag 4
//	value 0x00005000  extra 0  tag 5
//	value 0x00025000  extra 0  tag 6
//
// **Nothing here is established.** Tag 6 read as the module's page-rounded
// length for as long as there was one archive of this package: the first one's
// module rounds to exactly 0x25000. Two later archives carry the same header
// word for word — the same tags and the same values — while their modules round
// to 0x2e000 and 0x29000, so tag 6 is a constant of the format and not a size
// of the module beside it. The records are carried so a later reading has them,
// and so a package whose numbers do not look like these can be told apart from
// one that does.
type NativeField struct {
	Value uint32
	Extra uint16
	Tag   uint16
}

// nativeFieldStride is how far apart the records are.
const nativeFieldStride = 8

// Field finds a header record by tag.
func (info NativeInfo) Field(tag uint16) (NativeField, bool) {
	for _, field := range info.Fields {
		if field.Tag == tag {
			return field, true
		}
	}
	return NativeField{}, false
}

// nativeFields decodes one header section on the record stride. A trailing
// part-record is dropped rather than guessed at: the sections are bounded by
// the next offset in the header, and nothing says that boundary lands on a
// record.
func nativeFields(section []byte) []NativeField {
	fields := make([]NativeField, 0, len(section)/nativeFieldStride)
	for offset := 0; offset+nativeFieldStride <= len(section); offset += nativeFieldStride {
		fields = append(fields, NativeField{
			Value: binary.LittleEndian.Uint32(section[offset:]),
			Extra: binary.LittleEndian.Uint16(section[offset+4:]),
			Tag:   binary.LittleEndian.Uint16(section[offset+6:]),
		})
	}
	return fields
}

// NativeArchive is an opened earlier-generation KTF package.
type NativeArchive struct {
	Info       NativeInfo
	ModuleName string
	Module     []byte
	Files      map[string][]byte
}

// GuestFiles composes the guest filesystem view. Unlike the descriptor package
// there is no private "P/" prefix here: the title's files sit beside its module
// and it opens them by bare name.
func (archive *NativeArchive) GuestFiles() map[string][]byte {
	if archive == nil {
		return nil
	}
	entries := make(map[string][]byte, len(archive.Files))
	for name, data := range archive.Files {
		entries[name] = data
	}
	return entries
}

// IsNativePackage reports whether an outer archive's entries look like this
// package rather than the descriptor package. It answers from names alone, so
// an archive a Host has already read costs nothing more to classify.
func IsNativePackage(files map[string][]byte) bool {
	if _, ok := findEntry(files, adfPath); ok {
		return false
	}
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	return isNativeShape(names)
}

// isNativeShape reports whether a set of unwrapped entry names is this
// package's. The pair is the discriminator and it has to be a pair: a lone
// `.mod` is a common enough extension that claiming an archive for it would be
// a guess, while a `.mif` beside exactly one `.mod` is this package's shape.
func isNativeShape(names []string) bool {
	info, module := 0, 0
	for _, name := range names {
		switch strings.ToLower(path.Ext(name)) {
		case nativeInfoExtension:
			info++
		case nativeModuleExtension:
			module++
		}
	}
	return info == 1 && module == 1
}

// OpenNative reads an earlier-generation KTF package.
func OpenNative(data []byte) (*NativeArchive, error) {
	files, err := readOuterZIP(data)
	if err != nil {
		return nil, err
	}
	if _, ok := findEntry(files, adfPath); ok {
		return nil, fmt.Errorf("KTF archive carries %s and is not a native package", adfPath)
	}
	archive := &NativeArchive{Files: make(map[string][]byte, len(files))}
	infoName := ""
	for name, contents := range files {
		switch strings.ToLower(path.Ext(name)) {
		case nativeInfoExtension:
			if infoName != "" {
				return nil, fmt.Errorf("KTF native package has two module information files %q and %q", infoName, name)
			}
			infoName = name
			if len(contents) > maxNativeInfoSize {
				return nil, fmt.Errorf("KTF module information file %q is %d bytes, limit %d", name, len(contents), maxNativeInfoSize)
			}
			archive.Info, err = ParseNativeInfo(contents)
			if err != nil {
				return nil, fmt.Errorf("parse %s: %w", name, err)
			}
		case nativeModuleExtension:
			if archive.ModuleName != "" {
				return nil, fmt.Errorf("KTF native package has two modules %q and %q", archive.ModuleName, name)
			}
			if len(contents) == 0 {
				return nil, fmt.Errorf("KTF native module %q is empty", name)
			}
			if len(contents) > maxNativeModuleSize {
				return nil, fmt.Errorf("KTF native module %q is %d bytes, limit %d", name, len(contents), maxNativeModuleSize)
			}
			archive.ModuleName, archive.Module = name, contents
		default:
			archive.Files[name] = contents
		}
	}
	if infoName == "" {
		return nil, fmt.Errorf("KTF native package has no %s", nativeInfoExtension)
	}
	if archive.ModuleName == "" {
		return nil, fmt.Errorf("KTF native package has no %s", nativeModuleExtension)
	}
	return archive, nil
}

// ParseNativeInfo decodes a module information file.
//
// The layout is a fixed header, a table of span offsets, and the spans
// themselves, with an identity record after the last one:
//
//	0x00  four 16-bit header fields
//	0x08  offset of the first header section
//	0x0c  offset of the second header section
//	0x10  offset of the span table
//	0x14  span count
//	0x18  offset of the first span, repeating the span table's first entry
//	0x1c  a further offset
//
// The span table holds count+1 offsets: one per span, then the end of the last
// one. Everything after that end is the identity record, which finishes with
// the trailer magic.
func ParseNativeInfo(data []byte) (NativeInfo, error) {
	// One archive's information file carries CR LF past its trailer, from
	// something that moved the file in text mode before it was packed. Its zip
	// entry's CRC is correct, so those two bytes are the package as it was
	// distributed rather than damage a reader should refuse. Line endings after
	// the trailer are dropped and the file is read up to it; anything else
	// after it still is not this format.
	data = bytes.TrimRight(data, "\r\n")
	if len(data) < 0x20 {
		return NativeInfo{}, fmt.Errorf("module information file is %d bytes, too short for its header", len(data))
	}
	if !bytes.HasSuffix(data, []byte(nativeInfoTrailerMagic)) {
		return NativeInfo{}, fmt.Errorf("module information file does not end with %q", nativeInfoTrailerMagic)
	}
	tableOffset := binary.LittleEndian.Uint32(data[0x10:])
	spanCount := binary.LittleEndian.Uint32(data[0x14:])
	if spanCount == 0 || spanCount > maxNativeInfoSpans {
		return NativeInfo{}, fmt.Errorf("module information file declares %d spans, limit %d", spanCount, maxNativeInfoSpans)
	}
	// The table holds one more offset than there are spans, so the last span
	// has an end without a span of its own following it.
	tableBytes := uint64(spanCount+1) * 4
	if uint64(tableOffset)+tableBytes > uint64(len(data)) {
		return NativeInfo{}, fmt.Errorf("module information file span table at %#x runs past its %d bytes", tableOffset, len(data))
	}
	offsets := make([]uint32, spanCount+1)
	for index := range offsets {
		offsets[index] = binary.LittleEndian.Uint32(data[uint64(tableOffset)+uint64(index)*4:])
	}
	if declared := binary.LittleEndian.Uint32(data[0x18:]); declared != offsets[0] {
		return NativeInfo{}, fmt.Errorf("module information file names first span %#x but its table names %#x", declared, offsets[0])
	}
	for index := 1; index < len(offsets); index++ {
		if offsets[index] < offsets[index-1] {
			return NativeInfo{}, fmt.Errorf("module information file span %d ends at %#x, before it starts at %#x", index-1, offsets[index], offsets[index-1])
		}
	}
	// The identity record sits between the last span and the trailer magic, so
	// the last span has to end before the magic rather than merely inside the
	// file. A span that runs into it leaves the record a negative length, which
	// is a slice this cannot take: the bound is what keeps a truncated file a
	// refusal rather than a panic in whoever asked what the package is.
	recordEnd := uint64(len(data) - len(nativeInfoTrailerMagic))
	if uint64(offsets[len(offsets)-1]) > recordEnd {
		return NativeInfo{}, fmt.Errorf("module information file last span ends at %#x, past the %d bytes before its trailer", offsets[len(offsets)-1], recordEnd)
	}

	info := NativeInfo{}
	for _, bounds := range [][2]uint32{
		{binary.LittleEndian.Uint32(data[0x08:]), binary.LittleEndian.Uint32(data[0x0c:])},
		{binary.LittleEndian.Uint32(data[0x0c:]), tableOffset},
	} {
		if bounds[1] < bounds[0] || uint64(bounds[1]) > uint64(len(data)) {
			return NativeInfo{}, fmt.Errorf("module information file header section %#x..%#x is not inside its %d bytes", bounds[0], bounds[1], len(data))
		}
		info.Sections = append(info.Sections, nativeWords(data[bounds[0]:bounds[1]]))
		info.Fields = append(info.Fields, nativeFields(data[bounds[0]:bounds[1]])...)
	}
	for index := 0; index < int(spanCount); index++ {
		span := data[offsets[index]:offsets[index+1]]
		switch classifyNativeSpan(span) {
		case nativeSpanTyped:
			icon, err := parseNativeTypedSpan(span)
			if err != nil {
				return NativeInfo{}, fmt.Errorf("span %d: %w", index, err)
			}
			info.Icons = append(info.Icons, icon)
		case nativeSpanText:
			text := decodeEUCKR(span[2:])
			if len(text) > maxNativeInfoText {
				return NativeInfo{}, fmt.Errorf("span %d text is %d bytes, limit %d", index, len(text), maxNativeInfoText)
			}
			// The vendor is written twice and the title once, so the first
			// distinct text after a repeat is the name. Taking the first as the
			// vendor and the first different one as the name holds for both.
			switch {
			case info.Vendor == "":
				info.Vendor = text
			case text != info.Vendor && info.Name == "":
				info.Name = text
			}
		default:
			info.Records = append(info.Records, nativeWords(span))
		}
	}
	trailer := data[offsets[len(offsets)-1]:recordEnd]
	info.Trailer = nativeWords(trailer)
	// The identity record's second word is the application identifier, and the
	// package names its information file after it.
	if len(info.Trailer) > 1 {
		info.ApplicationID = info.Trailer[1]
	}
	return info, nil
}

type nativeSpanKind int

const (
	nativeSpanNumeric nativeSpanKind = iota
	nativeSpanTyped
	nativeSpanText
)

// A text span opens with two 0xfe bytes and a typed span with a 16-bit length
// covering itself, the MIME name and its terminator. Neither mark can be the
// other, and a numeric span is whatever carries no mark at all.
func classifyNativeSpan(span []byte) nativeSpanKind {
	if len(span) >= 2 && span[0] == 0xfe && span[1] == 0xfe {
		return nativeSpanText
	}
	if len(span) >= 4 {
		if length := binary.LittleEndian.Uint16(span); int(length) <= len(span) && length > 3 && span[length-1] == 0 {
			return nativeSpanTyped
		}
	}
	return nativeSpanNumeric
}

func parseNativeTypedSpan(span []byte) (NativeIcon, error) {
	length := binary.LittleEndian.Uint16(span)
	if int(length) > len(span) || length < 4 {
		return NativeIcon{}, fmt.Errorf("typed span declares %d bytes of type in %d", length, len(span))
	}
	name := span[2 : length-1]
	if len(name) > maxNativeInfoText {
		return NativeIcon{}, fmt.Errorf("typed span names a %d byte type, limit %d", len(name), maxNativeInfoText)
	}
	return NativeIcon{MIME: string(name), Data: span[length:]}, nil
}

func nativeWords(span []byte) []uint32 {
	words := make([]uint32, len(span)/4)
	for index := range words {
		words[index] = binary.LittleEndian.Uint32(span[index*4:])
	}
	return words
}
