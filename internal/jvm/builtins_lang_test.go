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

// A title reaches Class.forName with three kinds of name, and only one of them
// is a class file. A class the platform compiled ahead of time lives in the AOT
// registry and never in the loader, so asking the loader alone answered "not
// found" for a title's own main class — which is what one of them names while
// it builds its first card. An array type has no class file at all.
func TestClassForNameResolvesAOTClassesAndArrayTypes(t *testing.T) {
	vm := New(nil, Options{})
	if err := vm.RegisterAOTClass(AOTClassMetadata{
		Address:      0x101000,
		Name:         "GameMain",
		SuperName:    "java/lang/Object",
		AccessFlags:  0x21,
		InstanceSize: 8,
	}); err != nil {
		t.Fatal(err)
	}
	if err := vm.DefineClass(ClassDefinition{Name: "game/Loaded", SuperName: "java/lang/Object", Access: 0x21}); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{
		"GameMain",     // compiled ahead of time, not in the loader
		"[LGameMain;",  // an array of it
		"[[LGameMain;", // and an array of that
		"[I",           // a primitive element always exists
		"game/Loaded",  // and the loader's own classes still answer
		"[Lgame/Loaded;",
	} {
		if !vm.classForNameExists(name) {
			t.Errorf("Class.forName(%q) found nothing", name)
		}
	}
	for _, name := range []string{"GameOther", "[LGameOther;", "[", "[L;"} {
		if vm.classForNameExists(name) {
			t.Errorf("Class.forName(%q) answered for a type that does not exist", name)
		}
	}
}
