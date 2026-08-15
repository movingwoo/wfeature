package wipic

import (
	"fmt"
	"strings"
	"testing"
)

// arguments walks a fixed list the way a platform walks the guest's variadic
// slots, pairing words for the long-long modifiers.
func arguments(words ...uint32) func(int) (uint64, error) {
	index := 0
	return func(count int) (uint64, error) {
		if index+count > len(words) {
			return 0, fmt.Errorf("format asked for argument %d of %d", index+count, len(words))
		}
		low := uint64(words[index])
		index++
		if count == 1 {
			return low, nil
		}
		high := uint64(words[index])
		index++
		return high<<32 | low, nil
	}
}

// stringTable answers guest strings the way a platform does, honouring a
// precision as a bound rather than requiring a terminator inside it.
func stringTable(table map[uint32]string) ReadString {
	return func(address uint32, limit int) ([]byte, error) {
		text, ok := table[address]
		if !ok {
			return nil, fmt.Errorf("no string at %#x", address)
		}
		if limit >= 0 && limit < len(text) {
			return []byte(text[:limit]), nil
		}
		return []byte(text), nil
	}
}

func TestFormatRendersTheSubsetGamesUse(t *testing.T) {
	read := stringTable(map[uint32]string{0x100: "name"})
	for _, test := range []struct {
		format string
		words  []uint32
		want   string
	}{
		{"plain", nil, "plain"},
		{"100%%", nil, "100%"},
		{"%d", []uint32{7}, "7"},
		// %d is signed and one word wide, so a high bit is a negative number
		// rather than a large one; %u of the same word is not.
		{"%d", []uint32{0xffffffff}, "-1"},
		{"%u", []uint32{0xffffffff}, "4294967295"},
		{"%x/%X", []uint32{0xbeef, 0xbeef}, "beef/BEEF"},
		{"%04x", []uint32{0x2a}, "002a"},
		{"[%5d]", []uint32{42}, "[   42]"},
		// A zero-padded negative keeps its sign in front of the fill.
		{"%05d", []uint32{0xffffff9c}, "-0100"},
		{"%c", []uint32{'A'}, "A"},
		{"%s", []uint32{0x100}, "name"},
		// A null string argument renders as nothing rather than faulting.
		{"[%s]", []uint32{0}, "[]"},
		// The long-long modifier consumes two words, low first.
		{"%lld", []uint32{1, 2}, "8589934593"},
		// An unknown directive is emitted as written, which keeps the rest of
		// the line readable instead of losing it.
		{"%q", nil, "%q"},
		// A format that ends mid-specification keeps what it collected.
		{"end %", nil, "end %"},
	} {
		got, err := Format([]byte(test.format), arguments(test.words...), read)
		if err != nil {
			t.Fatalf("Format(%q) error = %v", test.format, err)
		}
		if string(got) != test.want {
			t.Errorf("Format(%q) = %q, want %q", test.format, got, test.want)
		}
	}
}

// String arguments stay raw bytes so guest text keeps its original encoding for
// the caller to decode; rendering must not touch them.
func TestFormatLeavesStringBytesAlone(t *testing.T) {
	raw := []byte{0xb0, 0xa1, 0xb0, 0xa2}
	got, err := Format([]byte("%s"), arguments(0x100), func(uint32, int) ([]byte, error) {
		return raw, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(raw) {
		t.Fatalf("Format() = % x, want % x", got, raw)
	}
}

// A precision is how a game prints a slice of a buffer rather than a whole
// string, which is what a title's dialogue is drawn from: the text is one long
// block and each line is a count of bytes into it. Reading `%.*s` as an unknown
// directive puts the specification itself on screen in place of every line.
func TestFormatAppliesPrecision(t *testing.T) {
	read := stringTable(map[uint32]string{0x100: "abcdefgh", 0x200: "0123456789abcdef"})
	for _, test := range []struct {
		format string
		words  []uint32
		want   string
	}{
		// The precision argument is read before the pointer it bounds.
		{"%.*s", []uint32{3, 0x100}, "abc"},
		{"%.3s", []uint32{0x100}, "abc"},
		// A precision longer than the string is not padding.
		{"%.20s", []uint32{0x100}, "abcdefgh"},
		{"%.0s|", []uint32{0x100}, "|"},
		// Width still pads what the precision cut, on either side.
		{"[%6.3s]", []uint32{0x100}, "[   abc]"},
		{"[%-6.3s]", []uint32{0x100}, "[abc   ]"},
		// A card number is masked with two precisions and no arguments between.
		{"%.3s-****-%.4s", []uint32{0x200, 0x200}, "012-****-0123"},
		{"%s:%.*s", []uint32{0x100, 4, 0x200}, "abcdefgh:0123"},
		// A negative * precision is C's way of saying there is none; a negative
		// * width is a left-aligned field of that magnitude.
		{"%.*s", []uint32{0xffffffff, 0x100}, "abcdefgh"},
		{"[%*.3s]", []uint32{0xfffffffa, 0x100}, "[abc   ]"},
		// On a number a precision is a minimum digit count, and it displaces
		// the zero flag rather than adding to it.
		{"%.5d", []uint32{42}, "00042"},
		{"%.5d", []uint32{0xffffffd6}, "-00042"},
		{"%8.5d|", []uint32{42}, "   00042|"},
		{"%08.5d|", []uint32{42}, "   00042|"},
		{"%.3x", []uint32{0xa}, "00a"},
		// A zero rendered at precision zero is nothing at all.
		{"[%.0d]", []uint32{0}, "[]"},
	} {
		got, err := Format([]byte(test.format), arguments(test.words...), read)
		if err != nil {
			t.Fatalf("Format(%q) error = %v", test.format, err)
		}
		if string(got) != test.want {
			t.Errorf("Format(%q) = %q, want %q", test.format, got, test.want)
		}
	}
}

// The remaining printf flags, which a title mixes with the ones already
// modelled and which must not send the whole specification to the fallback.
func TestFormatAppliesFlags(t *testing.T) {
	read := stringTable(map[uint32]string{0x100: "name"})
	for _, test := range []struct {
		format string
		words  []uint32
		want   string
	}{
		{"[%-6d]", []uint32{42}, "[42    ]"},
		{"[%-6s]", []uint32{0x100}, "[name  ]"},
		// A left-aligned field fills with spaces even when zero is asked for:
		// zeroes on the right would change the value.
		{"[%-06d]", []uint32{42}, "[42    ]"},
		{"%+d/%+d", []uint32{42, 0xffffffd6}, "+42/-42"},
		{"[% d]", []uint32{42}, "[ 42]"},
		{"[%+05d]", []uint32{42}, "[+0042]"},
		{"%#x/%#X/%#o", []uint32{0x2a, 0x2a, 8}, "0x2a/0X2A/010"},
		// A width given as * reads a word of its own, before the value.
		{"[%*d]", []uint32{5, 42}, "[   42]"},
		// The short modifiers are accepted and change nothing, rather than
		// reaching the fallback and printing themselves.
		{"%hd/%hhd", []uint32{42, 42}, "42/42"},
	} {
		got, err := Format([]byte(test.format), arguments(test.words...), read)
		if err != nil {
			t.Fatalf("Format(%q) error = %v", test.format, err)
		}
		if string(got) != test.want {
			t.Errorf("Format(%q) = %q, want %q", test.format, got, test.want)
		}
	}
}

// A hostile format string cannot ask for an unbounded run of padding or an
// unbounded result.
func TestFormatBoundsWidthAndOutput(t *testing.T) {
	got, err := Format([]byte("%999999999d"), arguments(1), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) > MaxWidth {
		t.Fatalf("width produced %d bytes, want at most %d", len(got), MaxWidth)
	}

	repeated := strings.Repeat("%4096d", 32)
	words := make([]uint32, 32)
	if _, err := Format([]byte(repeated), arguments(words...), nil); err == nil {
		t.Fatal("an oversized result was rendered instead of refused")
	}
}
