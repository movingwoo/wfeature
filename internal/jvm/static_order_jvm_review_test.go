package jvm

import (
	_ "embed"
	"testing"
)

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
