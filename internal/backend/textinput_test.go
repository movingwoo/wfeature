package backend

import (
	"bytes"
	"testing"
)

// A title is handed the KSC5601 bytes of what a person typed, or nothing: the
// extension the Go encoder implements gives codes a title's own font has no
// glyph for, and one such name broke a slot screen for as long as it was saved.
func TestEncodeKSC5601TakesOnlyWhatTheHandsetEncodingHolds(t *testing.T) {
	for _, test := range []struct {
		text    string
		encoded []byte
	}{
		{"", []byte{}},
		{"ABC 123", []byte("ABC 123")},
		{"\uD64D\uAE38\uB3D9", []byte{0xc8, 0xab, 0xb1, 0xe6, 0xb5, 0xbf}},
		{"\u3131", []byte{0xa4, 0xa1}},
		{"two\nlines", []byte("two\nlines")},
	} {
		encoded, ok := EncodeKSC5601(test.text)
		if !ok || !bytes.Equal(encoded, test.encoded) {
			t.Fatalf("EncodeKSC5601(%q) = %x, %v; want %x", test.text, encoded, ok, test.encoded)
		}
	}
	for _, text := range []string{
		"\uB620",       // a syllable the extension codes with a lead byte below 0xA1
		"\uD58F",       // one it codes with a trail byte below 0xA1
		"A\uBDC1B",     // one of them between letters KSC5601 has
		"\U0001F642",   // no Korean encoding at all
		"caf\u00E9",    // a Latin-1 letter
		"\u1112\u1161", // conjoining jamo rather than a syllable
	} {
		if encoded, ok := EncodeKSC5601(text); ok {
			t.Fatalf("EncodeKSC5601(%q) = %x, want a refusal", text, encoded)
		}
	}
}
