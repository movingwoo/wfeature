package jvm

import (
	"embed"
	"io/fs"
	"path"
	"strings"
	"testing"
	"time"
)

//go:embed testdata/LibraryMembers*.class
var libraryMembersClasses embed.FS

// libraryMembersSource is LibraryMembers.java's classes, by the names the
// loader asks for.
func libraryMembersSource(t *testing.T) mapClassSource {
	t.Helper()
	names, err := fs.Glob(libraryMembersClasses, "testdata/LibraryMembers*.class")
	if err != nil || len(names) == 0 {
		t.Fatalf("LibraryMembers classes: %v, %v", names, err)
	}
	source := mapClassSource{}
	for _, name := range names {
		data, err := libraryMembersClasses.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		source[strings.TrimSuffix(path.Base(name), ".class")] = data
	}
	return source
}

// The CLDC 1.1 members KTF and SKT share and a title links through its
// constant pool: each line is one method of LibraryMembers.java, which
// compares the runtime with itself wherever the charset decides the bytes.
// Every member on these lines resolved to nothing before it was declared, so
// each line also fails, naming its method, when a declaration goes missing.
//
// The Hashtable lines pin this runtime's enumeration order, which is the
// order of insertion: see hashtableDefinition.
func TestCompiledCodeReachesTheRestOfTheSharedCLDCLibrary(t *testing.T) {
	vm := New(libraryMembersSource(t), Options{})
	for _, line := range []struct{ method, want string }{
		{"text", "true,false,true,false,true,false,false,true,true,true,false,NPE|true,false,true,false,false,NPE|true/false"},
		{"buffer", "16,34,19,5,12,100,100,202,NASE|null#7true-+ce12345678901,10987654321ec+-eurt7#llun,SIOOBE,SIOOBE,NPE,true"},
		{"collections", "3,1,-1,-1,0,1,-1,AIOOBE,IOOBE|[#1, null, #2, #1, [x]]|[]|true,true,false,NPE,{one=#1, two=2},{a=1, b=2, c=3},abc,2,{}"},
		{"readers", "true,true,NPE,false,IOE,IOE|true,ab가b가c,false,IOE,IOE,false,IOE"},
		{"writers", "h/i!/abc/cde/,true,SIOOBE,NPE,NPE|true,true,true,c3a9f09f9880,IOE,UEE,NPE,NPE"},
		{"utf", "hié가,hié가,hié가,20,1,UTFDFE,UTFDFE,EOF,NPE"},
		{"classes", "LibraryMembers$Made,1,InstantiationException,InstantiationException,IllegalAccessException,IllegalStateException:refused,InstantiationException,InstantiationException,java.util.Vector"},
	} {
		result, err := vm.InvokeStatic("LibraryMembers", line.method, "()Ljava/lang/String;")
		if err != nil {
			t.Errorf("%s() error = %v", line.method, err)
			continue
		}
		object, err := result.Reference()
		if err != nil {
			t.Fatal(err)
		}
		if got, _ := StringText(object); got != line.want {
			t.Errorf("%s() =\n  %q\nwant\n  %q", line.method, got, line.want)
		}
	}
}

//go:embed testdata/CoreMembers.class
var coreMembersClass []byte

//go:embed testdata/CoreMembers$Source.class
var coreMembersSourceClass []byte

// A compiled title reaches a class-library member through its constant pool,
// which resolves the class and then the member on it. Every member this
// fixture touches had a working Go body and no declaration to resolve — the
// state four local archives stopped in — so the whole method is one call that
// either links or does not.
//
// It is the interpreted half of the same question the platform layers ask when
// they link ahead of time, and it is the cheaper half to keep: a declaration
// that goes missing again fails here rather than in a menu.
func TestCompiledCodeLinksTheCoreLibraryMembers(t *testing.T) {
	vm := New(mapClassSource{
		"CoreMembers":        coreMembersClass,
		"CoreMembers$Source": coreMembersSourceClass,
	}, Options{})
	result, err := vm.InvokeStatic("CoreMembers", "text", "()Ljava/lang/String;")
	if err != nil {
		t.Fatalf("text() error = %v", err)
	}
	object, err := result.Reference()
	if err != nil {
		t.Fatal(err)
	}
	// The window of the char array, the two boxed flags, an object append, the
	// byte the stream subclass read out of the protected buffer, and a long —
	// joined by a separator the replace then changes.
	text, _ := StringText(object)
	if text != "1.2:true:true:x:7:2" {
		t.Fatalf("text() = %q, want %q", text, "1.2:true:true:x:7:2")
	}
}

//go:embed testdata/Inherited.class
var inheritedClass []byte

//go:embed testdata/InheritedBase.class
var inheritedBaseClass []byte

// A field belongs to the class that declares it, whatever the code that
// touches it calls that class. A compiler names a field reference after the
// type of the expression it read, so a subclass reading an inherited field
// says its own name while the superclass that wrote it said the superclass's:
// storing under the name as written puts the two in different slots and the
// read answers a zero nothing ever wrote.
//
// This is not a corner of the library. It is every guest class that inherits a
// field — the shape a title's own class hierarchy has — and it was found by a
// stream subclass reaching for the buffer its superclass filled.
func TestAnInheritedFieldIsOneFieldUnderBothNames(t *testing.T) {
	vm := New(mapClassSource{
		"Inherited":     inheritedClass,
		"InheritedBase": inheritedBaseClass,
	}, Options{})
	object, err := vm.NewObject("Inherited", "(I)V", IntValue(21))
	if err != nil {
		t.Fatal(err)
	}
	result, err := vm.InvokeVirtual(object, "read", "()I")
	if err != nil {
		t.Fatal(err)
	}
	// The instance field the superclass wrote, plus the static it wrote beside
	// it: both are read back through the subclass's name for them.
	if value, _ := result.Int32(); value != 63 {
		t.Fatalf("read() = %d, want 63", value)
	}
}

//go:embed testdata/LibraryContracts.class
var libraryContractsClass []byte

//go:embed testdata/LibraryContracts$1.class
var libraryContracts1Class []byte

//go:embed testdata/LibraryContracts$2.class
var libraryContracts2Class []byte

//go:embed testdata/LibraryContracts$3.class
var libraryContracts3Class []byte

//go:embed testdata/LibraryContracts$Key.class
var libraryContractsKeyClass []byte

func libraryContractsSource() mapClassSource {
	return mapClassSource{
		"LibraryContracts":     libraryContractsClass,
		"LibraryContracts$1":   libraryContracts1Class,
		"LibraryContracts$2":   libraryContracts2Class,
		"LibraryContracts$3":   libraryContracts3Class,
		"LibraryContracts$Key": libraryContractsKeyClass,
	}
}

func contractInt(t *testing.T, vm *VM, method string) int32 {
	t.Helper()
	result, err := vm.InvokeStatic("LibraryContracts", method, "()I")
	if err != nil {
		t.Fatalf("%s() error = %v", method, err)
	}
	value, err := result.Int32()
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func contractString(t *testing.T, vm *VM, method string) string {
	t.Helper()
	result, err := vm.InvokeStatic("LibraryContracts", method, "()Ljava/lang/String;")
	if err != nil {
		t.Fatalf("%s() error = %v", method, err)
	}
	object, err := result.Reference()
	if err != nil {
		t.Fatal(err)
	}
	text, _ := StringText(object)
	return text
}

// waitForContract polls a static int until it leaves its starting value.
func waitForContract(t *testing.T, vm *VM, method string, unset int32) int32 {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if value := contractInt(t, vm, method); value != unset {
			return value
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s() still %d after the deadline", method, unset)
		}
		time.Sleep(time.Millisecond)
	}
}
