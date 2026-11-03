package jvm

import (
	_ "embed"
	"runtime"
	"testing"
)

// A class whose initializer the heap budget refuses is left uninitialized
// rather than in error: once the title has let go of memory its next use
// initializes it, and quick saves are not refused for it meanwhile. Cached,
// the error came back at every use however much was freed.
func TestAClassWhoseInitializerIsRefusedInitializesOnItsNextUse(t *testing.T) {
	vm := newBudgetVM(nil, testHeapBudget)
	const class = "test/LazyTable"
	runs := 0
	err := vm.DefineClass(ClassDefinition{Name: class, SuperName: ObjectClass, Access: AccessPublic,
		Fields: []FieldDefinition{{Name: "table", Descriptor: "[I", Access: AccessPublic | AccessStatic}},
		Methods: []MethodDefinition{
			{Name: "<clinit>", Descriptor: "()V", Access: AccessStatic, Body: func(call *Invocation, _ []Value) (Value, error) {
				runs++
				table, err := call.VM().NewArray(Type{Kind: TypeInt}, 1<<16)
				if err != nil {
					return VoidValue(), err
				}
				return VoidValue(), call.SetStaticField(class, "table", "[I", ReferenceValue(table))
			}},
			{Name: "ready", Descriptor: "()Z", Access: AccessPublic | AccessStatic, Body: func(call *Invocation, _ []Value) (Value, error) {
				value, err := call.StaticField(class, "table", "[I")
				if err != nil {
					return VoidValue(), err
				}
				table, _ := value.Reference()
				return booleanValue(table != nil), nil
			}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	held, err := vm.makeArray(Type{Kind: TypeInt}, testHeapBudget/int32(valueHeapBytes), false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := vm.InvokeStatic(class, "ready", "()Z"); !vm.IsGuestException(err, "java/lang/OutOfMemoryError") {
		t.Fatalf("a use whose initializer the full budget refuses: %v, want OutOfMemoryError", err)
	}
	if _, err := vm.CaptureHeapState(nil, HeapCodec{}); err != nil {
		t.Fatalf("a quick save after the refused initializer: %v", err)
	}
	runtime.KeepAlive(held)
	held = nil
	runtime.GC()
	result, err := vm.InvokeStatic(class, "ready", "()Z")
	if err != nil {
		t.Fatalf("the use once the budget has room: %v", err)
	}
	if ready, _ := result.Int32(); ready != 1 || runs != 2 {
		t.Fatalf("after the second use the table is set %v, the initializer ran %d times; want set and twice", ready == 1, runs)
	}
}

//go:embed testdata/StaticInit.class
var staticInitClass []byte

//go:embed testdata/StaticInit$Table.class
var staticInitTableClass []byte

//go:embed testdata/StaticInit$Reader.class
var staticInitReaderClass []byte

//go:embed testdata/StaticInit$Base.class
var staticInitBaseClass []byte

//go:embed testdata/StaticInit$Derived.class
var staticInitDerivedClass []byte

func staticInitSource() mapClassSource {
	return mapClassSource{
		"StaticInit":         staticInitClass,
		"StaticInit$Table":   staticInitTableClass,
		"StaticInit$Reader":  staticInitReaderClass,
		"StaticInit$Base":    staticInitBaseClass,
		"StaticInit$Derived": staticInitDerivedClass,
	}
}

func staticInitInt(t *testing.T, vm *VM, method string) int32 {
	t.Helper()
	result, err := vm.InvokeStatic("StaticInit", method, "()I")
	if err != nil {
		t.Fatalf("%s() error = %v", method, err)
	}
	value, err := result.Int32()
	if err != nil {
		t.Fatal(err)
	}
	return value
}

// getstatic and putstatic initialize the class or interface that declares the
// field, and only that one. An interface's array constant read inside an
// implementing class is compiled as the class's own field, and it is null
// unless the interface's initializer runs; an inherited static read or written
// through a subclass runs the superclass's initializer and not the subclass's.
func TestStaticFieldAccessInitializesTheDeclaringClass(t *testing.T) {
	// Table's initializer marks 1, Reader's 2, Base's 4 and Derived's 8.
	for _, probe := range []struct {
		method      string
		want        int32
		initialized int32
	}{
		{"interfaceTable", 7, 1 | 2},
		{"inheritedRead", 5, 4},
		{"inheritedWrite", 6, 4},
	} {
		t.Run(probe.method, func(t *testing.T) {
			vm := New(staticInitSource(), Options{})
			if got := staticInitInt(t, vm, probe.method); got != probe.want {
				t.Fatalf("%s() = %d, want %d", probe.method, got, probe.want)
			}
			if got := staticInitInt(t, vm, "initialized"); got != probe.initialized {
				t.Fatalf("initializers run after %s() = %#x, want %#x", probe.method, got, probe.initialized)
			}
		})
	}
}

// The Host's own static access follows the same rule, because a platform
// reads and writes statics by the name it knows rather than the declaring one.
func TestHostStaticAccessInitializesTheDeclaringClass(t *testing.T) {
	vm := New(staticInitSource(), Options{})
	value, err := vm.StaticField("StaticInit$Reader", "VALUES", "[I")
	if err != nil {
		t.Fatalf("StaticField(Reader.VALUES) error = %v", err)
	}
	array, err := value.Reference()
	if err != nil || array == nil {
		t.Fatalf("Reader.VALUES = %v/%v, want the interface's array", array, err)
	}
	if err := vm.SetStaticField("StaticInit$Derived", "shared", "I", IntValue(9)); err != nil {
		t.Fatalf("SetStaticField(Derived.shared) error = %v", err)
	}
	if got := staticInitInt(t, vm, "initialized"); got != 1|4 {
		t.Fatalf("initializers run = %#x, want %#x", got, 1|4)
	}
	shared, err := vm.StaticField("StaticInit$Base", "shared", "I")
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := shared.Int32(); got != 9 {
		t.Fatalf("Base.shared = %d, want 9", got)
	}
}

//go:embed testdata/StaticOrder.class
var staticOrderClass []byte

//go:embed testdata/StaticOrder$Base.class
var staticOrderBaseClass []byte

//go:embed testdata/StaticOrder$Derived.class
var staticOrderDerivedClass []byte

//go:embed testdata/StaticOrder$Defaults.class
var staticOrderDefaultsClass []byte

//go:embed testdata/StaticOrder$Implementor.class
var staticOrderImplementorClass []byte

// A class's statics are prepared before its superclass and its default-method
// superinterfaces initialize, so a store one of their initializers makes into
// a static of the class whose initialization started the chain stands. They
// were prepared afterwards, which reset the store to zero.
func TestAStoreFromASuperInitializerStands(t *testing.T) {
	source := mapClassSource{
		"StaticOrder":             staticOrderClass,
		"StaticOrder$Base":        staticOrderBaseClass,
		"StaticOrder$Derived":     staticOrderDerivedClass,
		"StaticOrder$Defaults":    staticOrderDefaultsClass,
		"StaticOrder$Implementor": staticOrderImplementorClass,
	}
	for _, read := range []struct {
		method string
		want   int32
	}{
		{"fromSuperclass", 5},
		{"own", 7},
		{"fromSuperinterface", 6},
	} {
		vm := New(source, Options{})
		result, err := vm.InvokeStatic("StaticOrder", read.method, "()I")
		if err != nil {
			t.Fatalf("%s() error = %v", read.method, err)
		}
		if got, _ := result.Int32(); got != read.want {
			t.Errorf("%s() = %d, want %d", read.method, got, read.want)
		}
	}
}

// Initializing a class looks through its superinterfaces for a default method,
// and an interface the title names that the platform does not declare is not
// a class the title reached, so the census does not list it as missing.
func TestASuperinterfaceProbeIsNotAMissingClass(t *testing.T) {
	vm := New(nil, Options{})
	const class = "test/NamesAnAbsentInterface"
	if err := vm.DefineClass(ClassDefinition{Name: class, SuperName: ObjectClass, Interfaces: []string{"test/Absent"}, Access: AccessPublic,
		Fields: []FieldDefinition{{Name: "VALUE", Descriptor: "I", Access: AccessPublic | AccessStatic | AccessFinal, Constant: IntValue(3)}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := vm.StaticField(class, "VALUE", "I"); err != nil {
		t.Fatal(err)
	}
	if _, missing := vm.ClassCensus(); missing["test/Absent"] != "" {
		t.Fatalf("the census lists the absent interface as missing: %v", missing)
	}
}

//go:embed testdata/SuperinterfaceInit.class
var superinterfaceInitClass []byte

//go:embed testdata/SuperinterfaceInit$Plain.class
var superinterfaceInitPlainClass []byte

//go:embed testdata/SuperinterfaceInit$Statics.class
var superinterfaceInitStaticsClass []byte

//go:embed testdata/SuperinterfaceInit$Root.class
var superinterfaceInitRootClass []byte

//go:embed testdata/SuperinterfaceInit$Left.class
var superinterfaceInitLeftClass []byte

//go:embed testdata/SuperinterfaceInit$Right.class
var superinterfaceInitRightClass []byte

//go:embed testdata/SuperinterfaceInit$Base.class
var superinterfaceInitBaseClass []byte

//go:embed testdata/SuperinterfaceInit$Implementation.class
var superinterfaceInitImplementationClass []byte

// Initializing a class initializes its superclass and then the superinterfaces
// that declare a default method, an interface's own superinterfaces first
// (JVMS 5.5). The fixture's initializers append B for the superclass, R and G
// for the two interfaces with a default method and C for the class itself; P,
// S and L belong to interfaces that declare none and must not appear.
func TestClassInitializationInitializesDefaultMethodSuperinterfaces(t *testing.T) {
	vm := New(mapClassSource{
		"SuperinterfaceInit":                superinterfaceInitClass,
		"SuperinterfaceInit$Plain":          superinterfaceInitPlainClass,
		"SuperinterfaceInit$Statics":        superinterfaceInitStaticsClass,
		"SuperinterfaceInit$Root":           superinterfaceInitRootClass,
		"SuperinterfaceInit$Left":           superinterfaceInitLeftClass,
		"SuperinterfaceInit$Right":          superinterfaceInitRightClass,
		"SuperinterfaceInit$Base":           superinterfaceInitBaseClass,
		"SuperinterfaceInit$Implementation": superinterfaceInitImplementationClass,
	}, Options{})
	result, err := vm.InvokeStatic("SuperinterfaceInit", "order", "()Ljava/lang/String;")
	if err != nil {
		t.Fatalf("order() error = %v", err)
	}
	object, err := result.Reference()
	if err != nil {
		t.Fatal(err)
	}
	order, ok := StringText(object)
	if !ok {
		t.Fatalf("order() = %v, want a String", object)
	}
	if order != "BRGC" {
		t.Fatalf("initialization order = %q, want %q", order, "BRGC")
	}
}

// An interface the class names but nobody can load is passed over, as it was
// before interfaces were walked: the loader resolves them lazily and a title
// may implement one its platform does not declare.
func TestClassInitializationPassesOverAnUnloadableInterface(t *testing.T) {
	vm := New(mapClassSource{
		"SuperinterfaceInit":                superinterfaceInitClass,
		"SuperinterfaceInit$Base":           superinterfaceInitBaseClass,
		"SuperinterfaceInit$Implementation": superinterfaceInitImplementationClass,
	}, Options{})
	result, err := vm.InvokeStatic("SuperinterfaceInit", "order", "()Ljava/lang/String;")
	if err != nil {
		t.Fatalf("order() error = %v", err)
	}
	object, _ := result.Reference()
	if order, _ := StringText(object); order != "BC" {
		t.Fatalf("initialization order = %q, want %q", order, "BC")
	}
}
