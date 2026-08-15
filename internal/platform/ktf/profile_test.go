package ktf

import "testing"

func newTestSymbolTable(entries ...struct {
	address uint32
	name    string
}) *symbolTable {
	table := &symbolTable{}
	for _, entry := range entries {
		table.starts = append(table.starts, entry.address)
		table.names = append(table.names, entry.name)
	}
	table.inferEnds()
	return table
}

type symbolEntry = struct {
	address uint32
	name    string
}

func TestSymbolTableNamesAddressesInsideABody(t *testing.T) {
	table := newTestSymbolTable(
		symbolEntry{0x1000, "a.one()V"},
		symbolEntry{0x1080, "a.two()V"},
		symbolEntry{0x1100, "a.three()V"},
	)
	for _, testCase := range []struct {
		address uint32
		symbol  string
		offset  uint32
	}{
		{0x1000, "a.one()V", 0},
		{0x107e, "a.one()V", 0x7e},
		{0x1080, "a.two()V", 0},
		{0x10ff, "a.two()V", 0x7f},
	} {
		frame := table.resolve(testCase.address)
		if frame.Symbol != testCase.symbol || frame.Offset != testCase.offset {
			t.Errorf("resolve(%#x) = %s+%#x, want %s+%#x",
				testCase.address, frame.Symbol, frame.Offset, testCase.symbol, testCase.offset)
		}
	}
}

// An address below the first body belongs to nothing the table knows about.
func TestSymbolTableLeavesAddressesBelowTheFirstBodyUnnamed(t *testing.T) {
	table := newTestSymbolTable(symbolEntry{0x1000, "a.one()V"})
	if frame := table.resolve(0xfff); frame.Symbol != "" {
		t.Fatalf("resolve(0xfff) = %s, want no symbol", frame.Symbol)
	}
}

// The failure this guards against is the one that actually happened: what sits
// above the highest registered body is not game code but client.bin's runtime
// helpers, and an unbounded last body claimed them — the array-load helper came
// back named as a static initializer holding more than half the profile.
func TestSymbolTableDoesNotLetTheHighestBodyClaimTheRuntimeHelpersAboveIt(t *testing.T) {
	entries := make([]symbolEntry, 0, 12)
	for index := uint32(0); index < 11; index++ {
		entries = append(entries, symbolEntry{0x1000 + index*0x40, "a.small()V"})
	}
	entries = append(entries, symbolEntry{0x1000 + 11*0x40, "a.<clinit>()V"})
	table := newTestSymbolTable(entries...)

	last := 0x1000 + 11*0x40
	if frame := table.resolve(uint32(last)); frame.Symbol != "a.<clinit>()V" {
		t.Fatalf("resolve(%#x) = %q, want the body's own start to still be named", last, frame.Symbol)
	}
	// Bodies here are 0x40 long, so an address two kilobytes past the last one
	// is a runtime helper, not that method.
	if frame := table.resolve(uint32(last) + 0x800); frame.Symbol != "" {
		t.Fatalf("resolve(%#x) = %q, want an unnamed address", last+0x800, frame.Symbol)
	}
}

// A gap far larger than any real method is a hole between two classes' code.
// Naming the hole after the body below it would be a confident wrong answer.
func TestSymbolTableStopsAtTheSpanCapInsideALargeGap(t *testing.T) {
	table := newTestSymbolTable(
		symbolEntry{0x1000, "a.one()V"},
		symbolEntry{0x1000 + 8*maxSymbolSpan, "b.two()V"},
	)
	if frame := table.resolve(0x1000 + maxSymbolSpan - 2); frame.Symbol != "a.one()V" {
		t.Fatalf("resolve just under the cap = %q, want a.one()V", frame.Symbol)
	}
	if frame := table.resolve(0x1000 + maxSymbolSpan); frame.Symbol != "" {
		t.Fatalf("resolve at the cap = %q, want an unnamed address", frame.Symbol)
	}
}

func TestSymbolTableWithNoBodiesNamesNothing(t *testing.T) {
	table := newTestSymbolTable()
	if frame := table.resolve(0x1000); frame.Symbol != "" || frame.Address != 0x1000 {
		t.Fatalf("resolve on an empty table = %+v, want the bare address", frame)
	}
}
