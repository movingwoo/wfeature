package skt

import "testing"

// The chunk a title hands the converter is not always whole double-byte text,
// and where a character starts is what decides whether the last byte is half
// of one. Counting the chunk instead got both of these wrong.
func TestDanglingLeadByteReadsFromTheStart(t *testing.T) {
	// "가" is 0xB0 0xA1 in EUC-KR, and 0xA1 is itself in the lead range.
	const lead, trail = 0xb0, 0xa1

	tests := []struct {
		name  string
		bytes []byte
		want  int
	}{
		{"empty", nil, -1},
		{"ascii only", []byte("name:"), -1},
		{"one whole character", []byte{lead, trail}, -1},
		{"ascii then a whole character", []byte{'a', lead, trail}, -1},
		{"a lead byte with nothing after it", []byte{lead}, 0},
		{"one whole character then a lead byte", []byte{lead, trail, lead}, 2},
		// Even length, and it really does end on half a character: parity said
		// it was whole, so the lead byte decoded alone and its trail arrived at
		// the head of the next chunk with nothing to join.
		{"odd ascii run then a lead byte", []byte{'a', 'b', 'c', lead}, 3},
		// Odd length ending on a trail byte that is in the lead range: parity
		// said it was half a character and tore the trail off a whole one.
		{"ascii then a whole character, odd length", []byte{'a', lead, trail}, -1},
	}
	for _, test := range tests {
		if got := danglingLeadByte(test.bytes); got != test.want {
			t.Errorf("%s: danglingLeadByte(%v) = %d, want %d", test.name, test.bytes, got, test.want)
		}
	}
}
