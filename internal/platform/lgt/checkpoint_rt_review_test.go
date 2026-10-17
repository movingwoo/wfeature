package lgt

import (
	"strings"
	"testing"
)

// A class record's element width is what `System.arraycopy` multiplies an
// element count by to size the copy, so a record is refused when it carries a
// width no array class has. Every width a running title can lay out — zero
// for a class that is not an array, and one, two, four or eight bytes — is
// still taken, which is what keeps every record an earlier build wrote
// loadable.
func TestCheckpointRefusesAClassWithAnImpossibleElementWidth(t *testing.T) {
	state := func(width uint32) *javaState {
		return &javaState{Classes: []javaClassState{{Name: []byte("[B"), Super: -1, ElementBytes: width}}}
	}
	for _, width := range []uint32{0, 1, 2, 4, 8} {
		if err := state(width).validate(); err != nil {
			t.Errorf("an element width of %d was refused: %v", width, err)
		}
	}
	for _, width := range []uint32{3, 16, 0x80000000, 0xffffffff} {
		err := state(width).validate()
		if err == nil {
			t.Errorf("an element width of %#x was taken", width)
			continue
		}
		if !strings.Contains(err.Error(), "impossible size") {
			t.Errorf("an element width of %#x was refused for another reason: %v", width, err)
		}
	}
}
