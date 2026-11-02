package jvm

import (
	"strings"
	"testing"

	"golang.org/x/text/encoding/korean"
)

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

// The three String constructors that take a charset name have to answer the
// same way. Two of them went through charsetOf and the third had a test of its
// own that accepted UTF-8 and nothing else, so a title decoding a record it had
// written in the handset's charset was handed an IOException — which one of
// them caught, printed, and then drew nothing for the rest of the run.
func TestStringConstructorsAgreeOnTheCharsetsTheyAccept(t *testing.T) {
	// The handset's charset is the Host's table, the way every platform here
	// installs it.
	vm := New(nil, Options{
		ByteDecoder: func(data []byte) string {
			text, err := korean.EUCKR.NewDecoder().Bytes(data)
			if err != nil {
				return ""
			}
			return string(text)
		},
	})
	// "가" in EUC-KR, which is what a handset title writes and reads back.
	encoded := []byte{0xb0, 0xa1}
	const decoded = "가"

	for _, name := range []string{"EUC_KR", "EUC-KR", "KSC5601", "UTF-8"} {
		wanted := decoded
		payload := encoded
		if name == "UTF-8" {
			payload, wanted = []byte(decoded), decoded
		}
		whole, err := vm.NewObject(StringClass, "([BLjava/lang/String;)V",
			ReferenceValue(NewByteArray(payload)), ReferenceValue(vm.NewString(name)))
		if err != nil {
			t.Fatalf("new String(byte[], %q) = %v", name, err)
		}
		if text, _ := StringText(whole); text != wanted {
			t.Errorf("new String(byte[], %q) = %q, want %q", name, text, wanted)
		}
		ranged, err := vm.NewObject(StringClass, "([BIILjava/lang/String;)V",
			ReferenceValue(NewByteArray(payload)), IntValue(0), IntValue(int32(len(payload))),
			ReferenceValue(vm.NewString(name)))
		if err != nil {
			t.Fatalf("new String(byte[], 0, n, %q) = %v", name, err)
		}
		if text, _ := StringText(ranged); text != wanted {
			t.Errorf("new String(byte[], 0, n, %q) = %q, want %q", name, text, wanted)
		}
	}

	// A charset with no table behind it is still refused, by both.
	if _, err := vm.NewObject(StringClass, "([BLjava/lang/String;)V",
		ReferenceValue(NewByteArray(encoded)), ReferenceValue(vm.NewString("Shift_JIS"))); err == nil {
		t.Error("new String(byte[], \"Shift_JIS\") was accepted")
	} else if !vm.IsGuestException(err, IOExceptionClass) {
		t.Errorf("new String(byte[], \"Shift_JIS\") = %v, want an IOException", err)
	}
}

// One class is one Class object, whichever way it was reached: getClass,
// a class literal, or Class.forName. A key class whose equals compares classes
// is otherwise never equal to anything.
func TestClassObjectsAreCanonical(t *testing.T) {
	vm := New(libraryContractsSource(), Options{})
	for _, method := range []string{"classIdentity", "equalByClass"} {
		result, err := vm.InvokeStatic("LibraryContracts", method, "()Z")
		if err != nil {
			t.Fatalf("%s() error = %v", method, err)
		}
		if got, _ := result.Int32(); got != 1 {
			t.Fatalf("%s() = %d, want true", method, got)
		}
	}
}

// A buffer length past what any array may hold is refused before anything is
// allocated for it.
func TestStringBufferSetLengthIsBounded(t *testing.T) {
	small := New(nil, Options{MaxArrayLength: 1024})
	buffer, err := small.NewObject(StringBufferClass, "()V")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := small.InvokeVirtual(buffer, "setLength", "(I)V", IntValue(4096)); err == nil {
		t.Fatal("setLength past MaxArrayLength was accepted")
	}
	if _, err := small.InvokeVirtual(buffer, "setLength", "(I)V", IntValue(1024)); err != nil {
		t.Fatalf("setLength at MaxArrayLength error = %v", err)
	}

	vm := New(libraryContractsSource(), Options{})
	_, err = vm.InvokeStatic("LibraryContracts", "setHugeLength", "()I")
	if err == nil || !strings.Contains(err.Error(), "exceeds limit") {
		t.Fatalf("setLength(Integer.MAX_VALUE) error = %v, want the array limit", err)
	}
}

// A null where a String method wants a String is a NullPointerException the
// title can catch, and equalsIgnoreCase(null) is simply false.
func TestNullStringArgumentsRaiseNullPointerException(t *testing.T) {
	vm := New(libraryContractsSource(), Options{})
	if got := contractInt(t, vm, "nullArguments"); got != 127 {
		t.Fatalf("nullArguments() = %#b, want %#b", got, 127)
	}
}

// A StringBuffer shared between two threads is read and written under its own
// lock by every method. Run with -race: the read side used to skip the lock.
func TestStringBufferReadsTakeTheBufferLock(t *testing.T) {
	vm := New(libraryContractsSource(), Options{})
	defer vm.Close()
	if got := contractInt(t, vm, "sharedBuffer"); got != 2000 {
		t.Fatalf("sharedBuffer() = %d, want 2000", got)
	}
}

// The parses refuse what CLDC's refuse: a leading plus sign and surrounding
// spaces are not digits.
func TestNumberParsesAreStrict(t *testing.T) {
	vm := New(libraryContractsSource(), Options{})
	if got := contractInt(t, vm, "parseStrictly"); got != 63 {
		t.Fatalf("parseStrictly() = %#b, want %#b", got, 63)
	}
	for _, text := range []string{"+5", " 5", "5 "} {
		_, err := vm.InvokeStatic(IntegerClass, "parseInt", "(Ljava/lang/String;I)I", ReferenceValue(vm.NewString(text)), IntValue(10))
		if !vm.IsGuestException(err, "java/lang/NumberFormatException") {
			t.Errorf("Integer.parseInt(%q, 10) error = %v, want NumberFormatException", text, err)
		}
		_, err = vm.InvokeStatic(LongClass, "parseLong", "(Ljava/lang/String;I)J", ReferenceValue(vm.NewString(text)), IntValue(16))
		if !vm.IsGuestException(err, "java/lang/NumberFormatException") {
			t.Errorf("Long.parseLong(%q, 16) error = %v, want NumberFormatException", text, err)
		}
	}
}

// A StringBuffer that reuses its storage still reads zeros where setLength
// lengthened it.
func TestStringBufferSetLengthZeroesReusedStorage(t *testing.T) {
	vm := New(heapBudgetSource(), Options{})
	if got := heapBudgetString(t, vm, "refill"); got != "axy\x00\x00" {
		t.Fatalf("refill() = %q, want %q", got, "axy\x00\x00")
	}
}
