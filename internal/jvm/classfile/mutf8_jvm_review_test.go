package classfile

import "testing"

// CONSTANT_Utf8 encodes each UTF-16 unit on its own, so half a surrogate pair
// is legal there and a class carrying one has to parse. Each lone half decodes
// to one replacement character; a proper pair still decodes to its character.
func TestDecodeModifiedUTF8AcceptsLoneSurrogates(t *testing.T) {
	for _, probe := range []struct {
		data []byte
		want string
	}{
		{[]byte{0xed, 0xa0, 0x80}, "�"},
		{[]byte{0xed, 0xb0, 0x80}, "�"},
		{[]byte{'a', 0xed, 0xa0, 0x80, 'b'}, "a�b"},
		{[]byte{0xed, 0xa0, 0x80, 0xed, 0xa0, 0x80}, "��"},
		{[]byte{0xed, 0xb0, 0x80, 0xed, 0xa0, 0xbd, 0xed, 0xb8, 0x80}, "�😀"},
	} {
		got, err := decodeModifiedUTF8(probe.data)
		if err != nil {
			t.Fatalf("decodeModifiedUTF8(% x) error = %v", probe.data, err)
		}
		if got != probe.want {
			t.Fatalf("decodeModifiedUTF8(% x) = %q, want %q", probe.data, got, probe.want)
		}
	}
}
