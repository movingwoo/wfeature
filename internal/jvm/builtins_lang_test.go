package jvm

import "testing"

// The switch reads a normalized name — upper case with the separators taken
// out — so every case has to be spelled that way. One was not, and a title
// naming that charset got a thrown IOException instead of the platform's own
// table.
func TestCharsetNamesAreMatchedAfterNormalizing(t *testing.T) {
	platform := []string{
		"EUC-KR", "euc-kr", "euckr",
		"KSC5601", "ksc5601-1987", "KSC5601_1989",
		"MS949", "ms_949", "CP949",
		"x-windows-949", "X-Windows-949", "windows-949",
	}
	for _, name := range platform {
		if got := charsetOf(name); got != charsetPlatform {
			t.Errorf("charsetOf(%q) = %v, want the platform charset", name, got)
		}
	}

	for _, name := range []string{"UTF-8", "utf8", "UTF_8"} {
		if got := charsetOf(name); got != charsetUTF8 {
			t.Errorf("charsetOf(%q) = %v, want UTF-8", name, got)
		}
	}

	// A name this runtime does not have a table for is refused rather than
	// guessed at: decoding with the wrong one produces plausible mistakes.
	for _, name := range []string{"ISO-8859-1", "Shift_JIS", ""} {
		if got := charsetOf(name); got != charsetUnknown {
			t.Errorf("charsetOf(%q) = %v, want it refused", name, got)
		}
	}
}
