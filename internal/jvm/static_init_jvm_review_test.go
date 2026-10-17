package jvm

import (
	_ "embed"
	"testing"
)

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
