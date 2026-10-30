// extracted: blend565, joinRemovalList
package clet

import (
	"sort"
	"strings"
)

// blend565 mixes a source pixel into a destination one by coverage, in the
// 5/6/5 space both already live in. Full coverage is the source untouched, so
// a glyph that lands on whole pixels — which every glyph on the 16-dot grid
// does — comes out of this exactly as it did when text was plotted rather than
// blended.
func blend565(destination, source uint16, coverage uint8) uint16 {
	if coverage == 0xff {
		return source
	}
	mix := func(from, to uint32) uint32 {
		return (from*uint32(0xff-coverage) + to*uint32(coverage) + 0x7f) / 0xff
	}
	red := mix(uint32(destination>>11&0x1f), uint32(source>>11&0x1f))
	green := mix(uint32(destination>>5&0x3f), uint32(source>>5&0x3f))
	blue := mix(uint32(destination&0x1f), uint32(source&0x1f))
	return uint16(red<<11 | green<<5 | blue)
}

// A removal list is one encoding, written here and read here, because it was
// four: the two database tables, the guest filesystem and the WIPI C file
// table each had their own pair, and the pairs disagreed about trimming. A
// name with a space around it went out as itself and came back trimmed, so
// deleting "save " hid "save" — found once per list, one review round at a
// time. Names are joined by newlines and read back exactly as written; what
// cannot survive that is refused by storableName rather than mangled here.
func joinRemovalList(names []string) []byte {
	sort.Strings(names)
	return []byte(strings.Join(names, "\n"))
}
