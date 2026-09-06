package detect

import (
	"encoding/binary"
	"strings"
)

// OMA DRM DCF recognition, header only.
//
// A wrapped package is content this emulator cannot open: the payload is
// encrypted and the key belongs to the network that issued it. That is not a
// reason to answer the same thing for it as for a file nobody recognises. A
// person holding one is told to find a different copy; a person holding a
// damaged zip is told to download it again; and a corpus sweep that counts
// both as "unknown" reports a number that says nothing about how much of it is
// this project's own work to do.
//
// **Nothing here decrypts, and nothing here reads a key.** The header is a
// declaration in the clear at the front of the file, and reading it answers
// the only question asked: is this a container that was locked, or is it a
// file this project has never seen the shape of.

// DCF is what a container's header declares about itself. The fields are the
// ones that are outside the encryption; the content is not touched.
type DCF struct {
	// Version is 1 for the header-prefixed layout and 2 for the box layout.
	Version int
	// ContentType is the media type the wrapper declares, which the version 1
	// layout carries in the clear. The version 2 layout keeps it inside a box
	// this does not descend into, so it is empty there.
	ContentType string
	// ContentID is the identifier the rights for this content are issued
	// against. It is the wrapper's own name for the payload rather than a file
	// name, so it survives a repack and is what a rights lookup would key on.
	ContentID string
}

// The version 1 layout is a fixed three-byte prefix — a version, and the
// lengths of the two strings after it — followed by those two strings. A file
// is only claimed when both strings are present, printable, and shaped like
// what they declare themselves to be, because three bytes on their own are far
// too little to claim a file on.
const (
	dcfVersion1        = 1
	dcfVersion1Prefix  = 3
	dcfMaxDeclaredText = 255
)

// The version 2 layout is the ISO base media box structure: a four-byte big
// endian size, a four-byte type, and the box's contents. Only the top of the
// file is walked, and only far enough to find either the brand box or the
// container box the wrapper is built out of.
const (
	dcfBoxHeader   = 8
	dcfMaxTopBoxes = 8
)

// dcfBrands are the major and compatible brands a wrapped container declares
// in its brand box. A brand is a four-character code, and these two are the
// discrete and the streamed layout of the same wrapper.
var dcfBrands = []string{"odcf", "opf2"}

// A container of the box layout may wear its brand at offset zero instead of
// declaring it in a brand box: the four characters, then a version and two
// bytes nobody reads, and then the boxes. Four characters at the front of a
// file are not on their own evidence of anything, so a file wearing them is
// claimed only when the boxes that follow are the wrapper's own -- which the
// walk below already insists on.
const dcfSignature = 8

// dcfMaxContainer bounds the length a holder may claim for a container it is
// asked about. The length comes off a zip entry's own header in the nested
// case, so it is whatever the file says; a box walk measured against a length
// nobody checked would step wherever that number pointed.
const dcfMaxContainer = int64(1) << 40

// DCFHeader reads the header of a DRM-wrapped container and reports whether
// the bytes are one. It reads no further than the declarations at the front of
// the file.
func DCFHeader(data []byte) (DCF, bool) {
	return dcfHeader(data, int64(len(data)))
}

// dcfHeader is DCFHeader asked of however much of the front of a container is
// in hand, with total the whole length its holder declares for it.
//
// The distinction matters because the payload of a package is a container the
// rest of which is a decompression away, and decompressing it to answer this
// would cost what the header exists to avoid. Box sizes are checked against
// total rather than against what was read, so the front of a file answers the
// same as the whole of it would.
func dcfHeader(data []byte, total int64) (DCF, bool) {
	if total < int64(len(data)) {
		total = int64(len(data))
	}
	if total > dcfMaxContainer {
		total = dcfMaxContainer
	}
	if header, ok := dcfVersion2(data, total); ok {
		return header, true
	}
	return dcfVersion1Header(data)
}

// dcfVersion1Header reads the header-prefixed layout: version, the two
// declared lengths, then the media type and the content identifier.
func dcfVersion1Header(data []byte) (DCF, bool) {
	if len(data) < dcfVersion1Prefix || data[0] != dcfVersion1 {
		return DCF{}, false
	}
	typeLength, idLength := int(data[1]), int(data[2])
	if typeLength == 0 || idLength == 0 {
		return DCF{}, false
	}
	if typeLength > dcfMaxDeclaredText || idLength > dcfMaxDeclaredText {
		return DCF{}, false
	}
	end := dcfVersion1Prefix + typeLength + idLength
	// The two lengths declare what follows them, and a file that stops inside
	// its own declaration is not one of these however much its first byte
	// agrees. There is a length pair after the strings as well; it is not read
	// here because it says how large the encrypted payload is, which is not a
	// question this answers.
	if len(data) < end+1 {
		return DCF{}, false
	}
	mediaType := string(data[dcfVersion1Prefix : dcfVersion1Prefix+typeLength])
	identifier := string(data[dcfVersion1Prefix+typeLength : end])
	// A media type is the discriminator that makes three bytes into evidence:
	// it is printable, it has the one slash a type and its subtype are
	// separated by, and it is at the front of a file whose first byte is a 1.
	if !printableASCII(mediaType) || !strings.Contains(mediaType, "/") {
		return DCF{}, false
	}
	if !printableASCII(identifier) {
		return DCF{}, false
	}
	return DCF{
		Version:     1,
		ContentType: mediaType,
		// The identifier is written as a URI, and the scheme in front of it is
		// the same for every one of these. What a rights lookup keys on is
		// what comes after it.
		ContentID: strings.TrimPrefix(identifier, "cid:"),
	}, true
}

// dcfVersion2 walks the top-level boxes far enough to recognise the wrapper:
// either the brand box declares one of the wrapper's brands, or the container
// box the wrapper is built out of is present.
func dcfVersion2(data []byte, total int64) (DCF, bool) {
	offset := 0
	// A signature at offset zero says what a brand box says, and the boxes
	// begin after it. Stepping over it costs nothing when it is absent: the
	// four bytes are then not a brand, and the walk starts where it did.
	if len(data) >= dcfSignature && brandsInclude(data[:4]) {
		offset = dcfSignature
	}
	for boxes := 0; boxes < dcfMaxTopBoxes; boxes++ {
		kind, body, next, ok := dcfBox(data, offset, total)
		if !ok {
			return DCF{}, false
		}
		switch kind {
		case "ftyp":
			// The brand box is a major brand, a version, and then the
			// compatible brands, all four characters each. A wrapper may
			// declare its brand in either place.
			for at := 0; at+4 <= len(body); at += 4 {
				if at == 4 {
					// The four bytes after the major brand are a version
					// number rather than a brand.
					continue
				}
				if brandsInclude(body[at : at+4]) {
					return DCF{Version: 2}, true
				}
			}
		case "odrm":
			return DCF{Version: 2}, true
		}
		offset = next
	}
	return DCF{}, false
}

// dcfBox reads one box header and returns the box's type, its contents, and
// where the next box starts. Sizes are measured against total, the length of
// the whole container: a box declares itself against the file it is in rather
// than against however much of that file was read.
func dcfBox(data []byte, offset int, total int64) (kind string, body []byte, next int, ok bool) {
	if offset < 0 || offset+dcfBoxHeader > len(data) {
		return "", nil, 0, false
	}
	size := int64(binary.BigEndian.Uint32(data[offset : offset+4]))
	kind = string(data[offset+4 : offset+dcfBoxHeader])
	if !printableASCII(kind) {
		return "", nil, 0, false
	}
	header := int64(dcfBoxHeader)
	switch size {
	case 0:
		// A size of zero means the box runs to the end of the file, so there
		// is no box after it.
		size = total - int64(offset)
	case 1:
		// A size of one means the real size is the 64-bit value after the
		// type. A file this large is not one of these, but the field still has
		// to be stepped over correctly to read the type that follows.
		if offset+dcfBoxHeader+8 > len(data) {
			return "", nil, 0, false
		}
		size = int64(binary.BigEndian.Uint64(data[offset+dcfBoxHeader : offset+dcfBoxHeader+8]))
		header += 8
	}
	if size < header || int64(offset)+size > total {
		return "", nil, 0, false
	}
	// A box runs past what was read when only the front of the container is in
	// hand. Its contents are then read as far as they are here and no further,
	// and the walk ends at the first box header that is not.
	end := int64(offset) + size
	if end > int64(len(data)) {
		end = int64(len(data))
	}
	body = data[int64(offset)+header : end]
	return kind, body, offset + int(size), true
}

// brandsInclude reports whether four bytes are one of the wrapper's brands.
func brandsInclude(code []byte) bool {
	for _, brand := range dcfBrands {
		if string(code) == brand {
			return true
		}
	}
	return false
}

func printableASCII(text string) bool {
	for index := 0; index < len(text); index++ {
		if text[index] < 0x20 || text[index] > 0x7e {
			return false
		}
	}
	return true
}
