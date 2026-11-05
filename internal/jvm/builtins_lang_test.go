package jvm

import (
	"errors"
	"math"
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

// regionMatches and startsWith(prefix, toffset) answer every run that does not
// fit with false — offsets and lengths at the ends of an int included, which
// the class library compares as longs — and a length of zero or less with
// true. A null receiver or argument is the NullPointerException the call
// dereferences it into.
func TestStringRegionTestsAnswerAtTheEndsOfAnInt(t *testing.T) {
	vm := New(nil, Options{})
	text := vm.NewString("abcdef")
	other := ReferenceValue(vm.NewString("xCDx"))
	region := func(ignoreCase bool, from int32, against Value, otherFrom, length int32) (bool, error) {
		flag := IntValue(0)
		if ignoreCase {
			flag = IntValue(1)
		}
		result, err := vm.InvokeVirtual(text, "regionMatches", "(ZILjava/lang/String;II)Z", flag, IntValue(from), against, IntValue(otherFrom), IntValue(length))
		if err != nil {
			return false, err
		}
		value, _ := result.Int32()
		return value != 0, nil
	}
	for _, probe := range []struct {
		ignoreCase              bool
		from, otherFrom, length int32
		want                    bool
	}{
		{true, 2, 1, 2, true},
		{false, 2, 1, 2, false},
		{true, 2, 1, 3, false},
		{true, math.MaxInt32, 0, 1, false},
		{true, 0, math.MaxInt32, 1, false},
		{false, 3, 1, math.MinInt32, true},
		{false, 6, 4, 0, true},
		{false, 7, 0, 0, false},
		{false, 0, -1, 0, false},
	} {
		got, err := region(probe.ignoreCase, probe.from, other, probe.otherFrom, probe.length)
		if err != nil || got != probe.want {
			t.Errorf("regionMatches(%t, %d, \"xCDx\", %d, %d) = %t, %v, want %t", probe.ignoreCase, probe.from, probe.otherFrom, probe.length, got, err, probe.want)
		}
	}
	if _, err := region(false, 0, ReferenceValue(nil), 0, 0); !vm.IsGuestException(err, "java/lang/NullPointerException") {
		t.Errorf("regionMatches with a null other = %v, want NullPointerException", err)
	}
	// Ignoring case is Latin-1's mapping, both ways, and nothing outside it.
	latin := vm.NewString("ÿßà")
	if result, err := vm.InvokeVirtual(latin, "regionMatches", "(ZILjava/lang/String;II)Z", IntValue(1), IntValue(0),
		ReferenceValue(vm.NewString("ÿßÀ")), IntValue(0), IntValue(3)); err != nil || result != IntValue(1) {
		t.Errorf("Latin-1 regionMatches ignoring case = %v, %v", result, err)
	}
	if result, err := vm.InvokeVirtual(latin, "regionMatches", "(ZILjava/lang/String;II)Z", IntValue(1), IntValue(0),
		ReferenceValue(vm.NewString("Ÿ")), IntValue(0), IntValue(1)); err != nil || result != IntValue(0) {
		t.Errorf("ÿ against its capital outside Latin-1 = %v, %v, want false", result, err)
	}

	for _, probe := range []struct {
		prefix string
		at     int32
		want   int32
	}{
		{"cd", 2, 1},
		{"cd", 3, 0},
		{"", 6, 1},
		{"", 7, 0},
		{"abcdefg", 0, 0},
		{"a", math.MaxInt32, 0},
		{"a", math.MinInt32, 0},
	} {
		result, err := vm.InvokeVirtual(text, "startsWith", "(Ljava/lang/String;I)Z", ReferenceValue(vm.NewString(probe.prefix)), IntValue(probe.at))
		if err != nil || result != IntValue(probe.want) {
			t.Errorf("startsWith(%q, %d) = %v, %v, want %d", probe.prefix, probe.at, result, err, probe.want)
		}
	}
	if _, err := vm.InvokeVirtual(text, "startsWith", "(Ljava/lang/String;I)Z", ReferenceValue(nil), IntValue(0)); !vm.IsGuestException(err, "java/lang/NullPointerException") {
		t.Errorf("startsWith(null, 0) = %v, want NullPointerException", err)
	}
	for flag, want := range map[int32]string{0: "false", 1: "true", 7: "true"} {
		result, err := vm.InvokeStatic(StringClass, "valueOf", "(Z)Ljava/lang/String;", IntValue(flag))
		if err != nil {
			t.Fatal(err)
		}
		object, _ := result.Reference()
		if got, _ := StringText(object); got != want {
			t.Errorf("String.valueOf(%d as a boolean) = %q, want %q", flag, got, want)
		}
	}
}

// Class.newInstance makes an object through the class's own no-argument
// constructor, and refuses what CLDC 1.1 refuses: an array class, an interface
// or abstract class, and a class with no such constructor of its own with
// InstantiationException; a constructor the caller may not reach with
// IllegalAccessException. A call that does not come from interpreted code has
// no caller class to compare, and is taken to be the class's package's own.
func TestClassNewInstanceRefusesWhatCLDCRefuses(t *testing.T) {
	vm := New(libraryMembersSource(t), Options{})
	made := 0
	counting := func(call *Invocation, arguments []Value) (Value, error) {
		made++
		return emptyConstructor(call, arguments)
	}
	refusing := func(*Invocation, []Value) (Value, error) {
		return VoidValue(), guestException("java/lang/IllegalStateException", "refused")
	}
	for _, definition := range []ClassDefinition{
		{Name: "pkg/Plain", SuperName: ObjectClass, Access: AccessPublic, Methods: []MethodDefinition{{Name: "<init>", Descriptor: "()V", Access: AccessPublic, Body: counting}}},
		{Name: "pkg/Quiet", SuperName: ObjectClass, Methods: []MethodDefinition{{Name: "<init>", Descriptor: "()V", Body: counting}}},
		{Name: "pkg/Package", SuperName: ObjectClass, Access: AccessPublic, Methods: []MethodDefinition{{Name: "<init>", Descriptor: "()V", Body: counting}}},
		{Name: "pkg/Guarded", SuperName: ObjectClass, Access: AccessPublic, Methods: []MethodDefinition{{Name: "<init>", Descriptor: "()V", Access: AccessProtected, Body: counting}}},
		{Name: "pkg/Private", SuperName: ObjectClass, Access: AccessPublic, Methods: []MethodDefinition{{Name: "<init>", Descriptor: "()V", Access: AccessPrivate, Body: counting}}},
		{Name: "pkg/Abstract", SuperName: ObjectClass, Access: AccessPublic | AccessAbstract, Methods: []MethodDefinition{{Name: "<init>", Descriptor: "()V", Access: AccessPublic, Body: counting}}},
		{Name: "pkg/Shape", SuperName: ObjectClass, Access: AccessPublic | AccessInterface | AccessAbstract},
		{Name: "pkg/Arguments", SuperName: ObjectClass, Access: AccessPublic, Methods: []MethodDefinition{{Name: "<init>", Descriptor: "(I)V", Access: AccessPublic, Body: counting}}},
		{Name: "pkg/Inherits", SuperName: "pkg/Plain", Access: AccessPublic},
		{Name: "pkg/Failing", SuperName: ObjectClass, Access: AccessPublic, Methods: []MethodDefinition{{Name: "<init>", Descriptor: "()V", Access: AccessPublic, Body: refusing}}},
	} {
		if err := vm.DefineClass(definition); err != nil {
			t.Fatal(err)
		}
	}
	newInstance := func(name string) (*Object, error) {
		result, err := vm.InvokeVirtual(vm.classObject(name), "newInstance", "()Ljava/lang/Object;")
		if err != nil {
			return nil, err
		}
		return result.Reference()
	}

	// From the Host: no caller, so only a private constructor is out of reach.
	for _, name := range []string{"pkg/Plain", "pkg/Quiet", "pkg/Package", "pkg/Guarded"} {
		object, err := newInstance(name)
		if err != nil || object == nil || object.ClassName != name {
			t.Errorf("newInstance of %s = %v, %v", name, object, err)
		}
	}
	if made != 4 {
		t.Fatalf("the constructors ran %d times, want 4", made)
	}
	for name, want := range map[string]string{
		"[I":            instantiationExceptionClass,
		"[Lpkg/Plain;":  instantiationExceptionClass,
		"pkg/Abstract":  instantiationExceptionClass,
		"pkg/Shape":     instantiationExceptionClass,
		"pkg/Arguments": instantiationExceptionClass,
		"pkg/Inherits":  instantiationExceptionClass,
		"pkg/Private":   illegalAccessExceptionClass,
		"pkg/Failing":   "java/lang/IllegalStateException",
	} {
		if _, err := newInstance(name); !vm.IsGuestException(err, want) {
			t.Errorf("newInstance of %s = %v, want %s", name, err, want)
		}
	}
	if made != 4 {
		t.Fatalf("a refused class ran a constructor: %d", made)
	}
	if object, err := newInstance(ObjectClass); err != nil || object == nil || object.ClassName != ObjectClass {
		t.Fatalf("newInstance of Object = %v, %v", object, err)
	}

	// From bytecode in another package: the class and its constructor have to
	// be visible to that package, a protected constructor as a package one.
	for name, want := range map[string]string{
		"pkg.Plain":   "pkg.Plain",
		"pkg.Quiet":   "IllegalAccessException",
		"pkg.Package": "IllegalAccessException",
		"pkg.Guarded": "IllegalAccessException",
		"pkg.Private": "IllegalAccessException",
	} {
		result, err := vm.InvokeStatic("LibraryMembers", "made", "(Ljava/lang/String;)Ljava/lang/String;", ReferenceValue(vm.NewString(name)))
		if err != nil {
			t.Fatalf("made(%q) error = %v", name, err)
		}
		object, _ := result.Reference()
		if got, _ := StringText(object); got != want {
			t.Errorf("made(%q) from the default package = %q, want %q", name, got, want)
		}
	}
}

// A class that exists only as AOT metadata is a platform's compiled class. It
// is checked against that metadata and made by the platform's constructor when
// one is installed, or by running its <init>()V through the AOT invoker.
func TestClassNewInstanceMakesACompiledClassThroughThePlatform(t *testing.T) {
	register := func(vm *VM, address uint32, name string, flags uint16, methods ...AOTMethodMetadata) {
		t.Helper()
		if err := vm.RegisterAOTClass(AOTClassMetadata{Address: address, Name: name, SuperName: ObjectClass, AccessFlags: flags, Methods: methods}); err != nil {
			t.Fatal(err)
		}
	}
	constructor := func(address uint32, flags uint16) AOTMethodMetadata {
		return AOTMethodMetadata{Address: address, Name: "<init>", Descriptor: "()V", AccessFlags: flags, Body: address + 8}
	}
	vm := New(nil, Options{})
	register(vm, 0x101000, "aot/Made", 0x0021, constructor(0x101010, 0x0001))
	register(vm, 0x102000, "aot/Abstract", 0x0421, constructor(0x102010, 0x0001))
	register(vm, 0x103000, "aot/Arguments", 0x0021, AOTMethodMetadata{Address: 0x103010, Name: "<init>", Descriptor: "(I)V", AccessFlags: 1, Body: 0x103018})
	register(vm, 0x104000, "aot/Hidden", 0x0021, constructor(0x104010, 0x0002))
	var invoked []string
	vm.SetContextAOTInvoker(func(_ *Invocation, className, name, descriptor string, arguments []Value) (Value, error) {
		receiver, _ := arguments[0].Reference()
		invoked = append(invoked, className+"."+name+descriptor+" on "+receiver.ClassName)
		return VoidValue(), nil
	})
	result, err := vm.InvokeVirtual(vm.classObject("aot/Made"), "newInstance", "()Ljava/lang/Object;")
	if err != nil {
		t.Fatal(err)
	}
	if object, _ := result.Reference(); object == nil || object.ClassName != "aot/Made" {
		t.Fatalf("newInstance of aot/Made = %v", object)
	}
	if len(invoked) != 1 || invoked[0] != "aot/Made.<init>()V on aot/Made" {
		t.Fatalf("the invoker was asked %v", invoked)
	}

	var built []AOTClassMetadata
	made := &Object{ClassName: "aot/Made"}
	vm.SetAOTConstructor(func(call *Invocation, class AOTClassMetadata) (*Object, error) {
		if call == nil || call.state == nil {
			t.Error("the constructor was not handed the caller's execution")
		}
		built = append(built, class)
		return made, nil
	})
	result, err = vm.InvokeVirtual(vm.classObject("aot/Made"), "newInstance", "()Ljava/lang/Object;")
	if err != nil {
		t.Fatal(err)
	}
	if object, _ := result.Reference(); object != made || len(built) != 1 || built[0].Address != 0x101000 || len(invoked) != 1 {
		t.Fatalf("newInstance with a platform constructor = %v; built %v; invoked %v", object, built, invoked)
	}
	for name, want := range map[string]string{
		"aot/Abstract":  instantiationExceptionClass,
		"aot/Arguments": instantiationExceptionClass,
		"aot/Hidden":    illegalAccessExceptionClass,
	} {
		if _, err := vm.InvokeVirtual(vm.classObject(name), "newInstance", "()Ljava/lang/Object;"); !vm.IsGuestException(err, want) {
			t.Errorf("newInstance of %s = %v, want %s", name, err, want)
		}
	}
	if len(built) != 1 {
		t.Fatalf("a refused class reached the platform's constructor: %v", built)
	}
}

// Runtime.exit is the instance form of System.exit and ends a session the same
// way: through the platform's Exit, or with the same refusal where a platform
// installed none.
func TestRuntimeExitEndsAsSystemExitDoes(t *testing.T) {
	var statuses []int32
	stopped := errors.New("stopped")
	vm := New(nil, Options{Exit: func(status int32) error {
		statuses = append(statuses, status)
		return stopped
	}})
	handle, err := vm.InvokeStatic(RuntimeClass, "getRuntime", "()Ljava/lang/Runtime;")
	if err != nil {
		t.Fatal(err)
	}
	runtimeObject, _ := handle.Reference()
	if _, err := vm.InvokeVirtual(runtimeObject, "exit", "(I)V", IntValue(7)); !errors.Is(err, stopped) {
		t.Fatalf("Runtime.exit(7) = %v, want the platform's own ending", err)
	}
	if _, err := vm.InvokeStatic(SystemClass, "exit", "(I)V", IntValue(8)); !errors.Is(err, stopped) {
		t.Fatalf("System.exit(8) = %v, want the platform's own ending", err)
	}
	if len(statuses) != 2 || statuses[0] != 7 || statuses[1] != 8 {
		t.Fatalf("Exit was called with %v", statuses)
	}

	bare := New(nil, Options{})
	handle, err = bare.InvokeStatic(RuntimeClass, "getRuntime", "()Ljava/lang/Runtime;")
	if err != nil {
		t.Fatal(err)
	}
	runtimeObject, _ = handle.Reference()
	_, runtimeErr := bare.InvokeVirtual(runtimeObject, "exit", "(I)V", IntValue(3))
	_, systemErr := bare.InvokeStatic(SystemClass, "exit", "(I)V", IntValue(3))
	var guest *GuestException
	for name, err := range map[string]error{"Runtime.exit": runtimeErr, "System.exit": systemErr} {
		if err == nil || errors.As(err, &guest) || !strings.Contains(err.Error(), name+"(3) with no platform shutdown installed") {
			t.Errorf("%s(3) with no Exit = %v, want the refusal naming it", name, err)
		}
	}
}
