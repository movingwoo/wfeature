package backend

import (
	"strings"

	"golang.org/x/text/encoding/korean"
)

// DecodeEUCKR and EncodeEUCKR are the handset's default charset, which is what
// `new String(byte[])` and `String.getBytes()` mean on every platform here and
// what a C title's text is written in.
//
// Both are lenient. Bytes that are not EUC-KR decode as themselves rather than
// as an error, because a title also builds strings out of content it packed
// itself, and a byte array of digits has to stay readable; text the encoding
// cannot hold encodes as its UTF-8 bytes, which a title that gets them back
// hands to a String constructor that reads them the same way. EncodeKSC5601 is
// the strict form, for text a player types into a field.
//
// **They are not applied to every string read out of guest memory**, only to
// the ones that are text. A file name, a resource name and a property key are
// looked up by their bytes on both sides, so decoding one would turn a match
// into a miss.
func DecodeEUCKR(data []byte) string {
	decoded, err := korean.EUCKR.NewDecoder().Bytes(data)
	if err != nil {
		return strings.ToValidUTF8(string(data), "�")
	}
	return string(decoded)
}

// EncodeEUCKR is DecodeEUCKR's inverse; see there.
func EncodeEUCKR(text string) []byte {
	encoded, err := korean.EUCKR.NewEncoder().Bytes([]byte(text))
	if err != nil {
		return []byte(text)
	}
	return encoded
}
