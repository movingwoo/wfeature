package wipi

import (
	"testing"

	"github.com/movingwoo/wfeature/internal/api/midp"
	"github.com/movingwoo/wfeature/internal/jvm"
)

// The definitions are a table, so a mistake in one — a descriptor that does
// not parse, a method declared twice, a superclass that is not installed — is
// the same mistake in every session. Installing them is what catches it here
// rather than at a title's first call.
func TestDefineInstallsTheSurfaceOverMIDP(t *testing.T) {
	machine := newMachine(t)
	if err := Define(machine, nil); err == nil {
		t.Fatal("defining the library twice succeeded")
	}
}

// The whole design is that a WIPI class is its MIDP counterpart with the
// standard's own additions on top: the display path, the paint scheduling and
// the pixels are the ones already built. If one of these chains breaks, a Jlet
// stops reaching them and nothing else says so.
func TestEachClassExtendsItsMIDPCounterpart(t *testing.T) {
	machine := newMachine(t)
	for _, probe := range []struct{ class, parent string }{
		{JletClass, midp.MIDletClass},
		{CardClass, midp.CanvasClass},
		{CardClass, midp.DisplayableClass},
		{DisplayClass, midp.DisplayClass},
		{GraphicsClass, midp.GraphicsClass},
		{ImageClass, midp.ImageClass},
		{FontClass, midp.FontClass},
	} {
		subclass, err := machine.IsSubclassOf(probe.class, probe.parent)
		if err != nil {
			t.Fatalf("IsSubclassOf(%s, %s) error = %v", probe.class, probe.parent, err)
		}
		if !subclass {
			t.Fatalf("%s does not extend %s", probe.class, probe.parent)
		}
	}
}

// IsJlet is what a Host asks of an archive's main class, and the two kinds of
// application are packaged identically: only the class answers.
func TestIsJletSeparatesTheTwoApplicationClasses(t *testing.T) {
	machine := newMachine(t)
	jlet, err := IsJlet(machine, JletClass)
	if err != nil || !jlet {
		t.Fatalf("IsJlet(%s) = %v, %v", JletClass, jlet, err)
	}
	midlet, err := IsJlet(machine, midp.MIDletClass)
	if err != nil || midlet {
		t.Fatalf("IsJlet(%s) = %v, %v", midp.MIDletClass, midlet, err)
	}
	// A class nobody loaded is not a Jlet, and asking must not be an error: a
	// Host asks before it knows what the archive holds.
	if _, err := IsJlet(machine, ""); err != nil {
		t.Fatalf("IsJlet(\"\") error = %v", err)
	}
}

// The storage surface is the specification's signatures, every one: a title
// links against a member whether or not this corpus calls it, and a missing
// one is NoSuchMethodError at the moment a save is made.
func TestStorageSurfaceDeclaresTheSpecificationsMembers(t *testing.T) {
	const (
		text     = "Ljava/lang/String;"
		database = "Lorg/kwis/msp/db/DataBase;"
	)
	declared := make(map[string]jvm.MethodDefinition)
	for _, definition := range definitions(nil) {
		for _, method := range definition.Methods {
			declared[definition.Name+"."+method.Name+method.Descriptor] = method
		}
	}
	want := map[string][]string{
		FileClass: {"write(I)I"},
		FileSystemClass: {
			"<init>()V", "available()I", "totalSpace()I", "getMaxFilenameLength()I", "toCString(" + text + ")[B",
			"exists(" + text + ")Z", "exists(" + text + "I)Z",
			"isFile(" + text + ")Z", "isFile(" + text + "I)Z",
			"isDirectory(" + text + ")Z", "isDirectory(" + text + "I)Z",
			"getCreationTime(" + text + ")I", "getCreationTime(" + text + "I)I",
			"list(" + text + ")Ljava/util/Vector;", "list(" + text + "I)Ljava/util/Vector;",
			"mkdir(" + text + ")V", "mkdir(" + text + "I)V",
			"rmdir(" + text + ")V", "rmdir(" + text + "I)V",
			"remove(" + text + ")V", "remove(" + text + "I)V",
			"rename(" + text + text + ")V", "rename(" + text + text + "I)V",
		},
		DataBaseClass: {
			"openDataBase(" + text + "IZ)" + database, "openDataBase(" + text + "IZI)" + database,
			"closeDataBase()V", "deleteDataBase(" + text + ")V", "deleteDataBase(" + text + "I)V",
			"deleteRecord(I)V", "getAccessMode(" + text + ")I", "getDataBaseName()" + text,
			"getDataBaseSize()I", "getLastModified()J", "getNumberOfRecords()I", "getRecordSize()I",
			"getSizeAvailable()I", "insertRecord([B)I", "insertRecord([BII)I", "listDataBases()[" + text,
			"selectRecord(I)[B", "selectRecord(I[BI)V", "updateRecord(I[B)V", "updateRecord(I[BII)V",
			"sortRecord(Lorg/kwis/msp/db/DataFilter;Lorg/kwis/msp/db/DataComparator;)[I",
		},
		DataBaseExceptionClass:       {"<init>()V", "<init>(" + text + ")V"},
		DataBaseRecordExceptionClass: {"<init>()V", "<init>(" + text + ")V"},
		DataFilterClass:              {"filter([B)Z"},
		DataComparatorClass:          {"compare([B[B)I"},
	}
	for class, members := range want {
		for _, member := range members {
			if _, ok := declared[class+"."+member]; !ok {
				t.Errorf("%s.%s is not declared", class, member)
			}
		}
	}
	// The members a title calls on a DataBase it holds are instance members,
	// and the ones it calls on the class are static.
	for _, member := range []string{"openDataBase(" + text + "IZ)" + database, "listDataBases()[" + text, "deleteDataBase(" + text + ")V", "getAccessMode(" + text + ")I"} {
		if declared[DataBaseClass+"."+member].Access&jvm.AccessStatic == 0 {
			t.Errorf("DataBase.%s is not static", member)
		}
	}
	for _, member := range []string{"insertRecord([B)I", "selectRecord(I)[B", "sortRecord(Lorg/kwis/msp/db/DataFilter;Lorg/kwis/msp/db/DataComparator;)[I", "closeDataBase()V"} {
		if declared[DataBaseClass+"."+member].Access&jvm.AccessStatic != 0 {
			t.Errorf("DataBase.%s is static", member)
		}
	}
}

// The specification has both database exceptions extend Exception, so a
// handler for one is not handed the other; the flags and the comparator's
// answers are constants a title reads.
func TestStorageExceptionsAndConstants(t *testing.T) {
	machine := newMachine(t)
	for _, exception := range []string{DataBaseExceptionClass, DataBaseRecordExceptionClass} {
		if checked, err := machine.IsSubclassOf(exception, "java/lang/Exception"); err != nil || !checked {
			t.Fatalf("%s is not an Exception: %v", exception, err)
		}
		if unchecked, _ := machine.IsSubclassOf(exception, "java/lang/RuntimeException"); unchecked {
			t.Fatalf("%s is a RuntimeException", exception)
		}
	}
	if nested, _ := machine.IsSubclassOf(DataBaseRecordExceptionClass, DataBaseExceptionClass); nested {
		t.Fatal("DataBaseRecordException extends DataBaseException; the specification has them siblings")
	}
	exception, err := machine.NewObject(DataBaseRecordExceptionClass, "(Ljava/lang/String;)V", jvm.ReferenceValue(machine.NewString("record 7")))
	if err != nil {
		t.Fatal(err)
	}
	message, err := machine.InvokeVirtual(exception, "getMessage", "()Ljava/lang/String;")
	if err != nil {
		t.Fatal(err)
	}
	if object, _ := message.Reference(); object == nil {
		t.Fatal("a DataBaseRecordException lost its message")
	} else if text, _ := jvm.StringText(object); text != "record 7" {
		t.Fatalf("message = %q", text)
	}
	for _, constant := range []struct {
		class, name string
		want        int32
	}{
		{FileSystemClass, "PRIVATE_ACCESS", 1}, {FileSystemClass, "SHARED_ACCESS", 2},
		{FileSystemClass, "SYSTEM_ACCESS", 3}, {FileSystemClass, "MAX_FILENAME_LENGTH", MaxFilenameLength},
		{DataComparatorClass, "EQUIVALENT", 0}, {DataComparatorClass, "FOLLOWS", 1}, {DataComparatorClass, "PRECEDES", -1},
	} {
		value, err := machine.StaticField(constant.class, constant.name, "I")
		if err != nil {
			t.Fatalf("%s.%s: %v", constant.class, constant.name, err)
		}
		if got, _ := value.Int32(); got != constant.want {
			t.Fatalf("%s.%s = %d, want %d", constant.class, constant.name, got, constant.want)
		}
	}
	length, err := machine.InvokeStatic(FileSystemClass, "getMaxFilenameLength", "()I")
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := length.Int32(); got != MaxFilenameLength {
		t.Fatalf("getMaxFilenameLength() = %d, want MAX_FILENAME_LENGTH", got)
	}
	if _, err := machine.NewObject(FileSystemClass, "()V"); err != nil {
		t.Fatalf("new FileSystem(): %v", err)
	}
}

func newMachine(t *testing.T) *jvm.VM {
	t.Helper()
	machine := jvm.New(nil, jvm.Options{})
	if err := midp.Define(machine); err != nil {
		t.Fatalf("midp.Define() error = %v", err)
	}
	if err := Define(machine, nil); err != nil {
		t.Fatalf("Define() error = %v", err)
	}
	return machine
}
