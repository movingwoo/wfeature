package armcore

import "testing"

// Holds answers what reading each span and comparing it would: equal bytes
// hold, a differing byte, an unmapped or unreadable span does not, an
// uncommitted page holds only zeros, and a span may cross pages.
func TestHoldsAnswersAsReadingAndComparingWould(t *testing.T) {
	memory := NewMemory()
	if err := memory.Map(0x10000, 0x3000, PermissionReadWrite); err != nil {
		t.Fatal(err)
	}
	if err := memory.Map(0x20000, 0x1000, PermissionWrite); err != nil {
		t.Fatal(err)
	}
	across := []byte{1, 2, 3, 4, 5, 6, 7, 8}
	if err := memory.Write(0x10ffc, across); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name  string
		spans []Span
		want  bool
	}{
		{"no spans", nil, true},
		{"a span across two pages", []Span{{0x10ffc, across}}, true},
		{"two spans", []Span{{0x10ffc, across[:2]}, {0x11002, across[6:]}}, true},
		{"a byte that differs", []Span{{0x10ffc, across}, {0x11003, []byte{9}}}, false},
		{"an uncommitted page", []Span{{0x12000, make([]byte, 16)}}, true},
		{"an uncommitted page with a byte set", []Span{{0x12000, []byte{0, 1}}}, false},
		{"an unmapped span", []Span{{0x30000, []byte{0}}}, false},
		{"a span past its mapping", []Span{{0x12fff, []byte{0, 0}}}, false},
		{"an unreadable span", []Span{{0x20000, []byte{0}}}, false},
		{"an empty span anywhere", []Span{{0x30000, nil}}, true},
	} {
		if got := memory.Holds(test.spans); got != test.want {
			t.Errorf("%s: Holds() = %t, want %t", test.name, got, test.want)
		}
	}
}
