package jvm

import "testing"

func TestVMRegistersAOTClassMetadata(t *testing.T) {
	vm := New(nil, Options{})
	metadata := AOTClassMetadata{
		Address:       0x101000,
		Name:          "game/Example",
		SuperName:     "java/lang/Object",
		AccessFlags:   0x21,
		InstanceSize:  8,
		VTableAddress: 0x101300,
		VTable:        []uint32{0x102001},
		Methods: []AOTMethodMetadata{{
			Address:     0x101100,
			Name:        "run",
			Descriptor:  "(I)I",
			Body:        0x102001,
			AccessFlags: 0x0001,
		}},
		Fields: []AOTFieldMetadata{{
			Address:     0x101200,
			Name:        "value",
			Descriptor:  "I",
			Offset:      4,
			AccessFlags: 0x0001,
		}},
	}
	if err := vm.RegisterAOTClass(metadata); err != nil {
		t.Fatalf("RegisterAOTClass() error = %v", err)
	}
	registered, ok := vm.AOTClass("game/Example")
	if !ok {
		t.Fatal("registered AOT class was not found")
	}
	if registered.Address != metadata.Address || registered.SuperName != metadata.SuperName || len(registered.Methods) != 1 || len(registered.Fields) != 1 {
		t.Fatalf("registered metadata = %+v", registered)
	}
	registered.VTable[0] = 0
	again, _ := vm.AOTClass("game/Example")
	if again.VTable[0] != 0x102001 {
		t.Fatal("AOTClass() exposed mutable registry storage")
	}
}

func TestVMCreatesAOTInstancesAndArrays(t *testing.T) {
	vm := New(nil, Options{MaxArrayLength: 4})
	if err := vm.RegisterAOTClass(AOTClassMetadata{
		Address:      0x1000,
		Name:         "game/Example",
		InstanceSize: 8,
	}); err != nil {
		t.Fatal(err)
	}
	if err := vm.RegisterAOTClass(AOTClassMetadata{
		Address: 0x2000,
		Name:    "[I",
	}); err != nil {
		t.Fatal(err)
	}

	instance, err := vm.NewAOTInstance(0x1000)
	if err != nil {
		t.Fatalf("NewAOTInstance() error = %v", err)
	}
	if instance.ClassName != "game/Example" || instance.Fields == nil {
		t.Fatalf("AOT instance = %+v", instance)
	}
	if err := vm.BindAOTObject(0x3000, instance); err != nil {
		t.Fatal(err)
	}

	array, err := vm.NewAOTArray(0x2000, 3)
	if err != nil {
		t.Fatalf("NewAOTArray() error = %v", err)
	}
	component, values, err := ArraySnapshot(array)
	if err != nil {
		t.Fatal(err)
	}
	if array.ClassName != "[I" || component.Kind != TypeInt || len(values) != 3 {
		t.Fatalf("AOT array = %s component=%s length=%d", array.ClassName, component.Descriptor(), len(values))
	}
	for index, value := range values {
		integer, err := value.Int32()
		if err != nil || integer != 0 {
			t.Fatalf("AOT array element %d = %d/%v, want zero int", index, integer, err)
		}
	}
	if _, err := vm.NewAOTArray(0x2000, 5); err == nil {
		t.Fatal("NewAOTArray() accepted a length above the JVM limit")
	}
	if _, err := vm.NewAOTInstance(0x2000); err == nil {
		t.Fatal("NewAOTInstance() accepted an array class")
	}
	if _, err := vm.NewAOTArray(0x1000, 1); err == nil {
		t.Fatal("NewAOTArray() accepted an ordinary class")
	}
}

func TestVMRejectsInvalidAOTClassMetadata(t *testing.T) {
	vm := New(nil, Options{})
	tests := []struct {
		name     string
		metadata AOTClassMetadata
	}{
		{name: "zero address", metadata: AOTClassMetadata{Name: "Example"}},
		{name: "invalid name", metadata: AOTClassMetadata{Address: 1, Name: "/Example"}},
		{name: "invalid method descriptor", metadata: AOTClassMetadata{Address: 1, Name: "Example", Methods: []AOTMethodMetadata{{Name: "run", Descriptor: "I"}}}},
		{name: "invalid field descriptor", metadata: AOTClassMetadata{Address: 1, Name: "Example", Fields: []AOTFieldMetadata{{Name: "value", Descriptor: "V"}}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := vm.RegisterAOTClass(test.metadata); err == nil {
				t.Fatal("RegisterAOTClass() accepted invalid metadata")
			}
		})
	}
}

func TestVMBindsAOTAddressToRealStringObject(t *testing.T) {
	vm := New(nil, Options{})
	object := vm.NewString("KTF")
	if err := vm.BindAOTObject(0x30001000, object); err != nil {
		t.Fatalf("BindAOTObject() error = %v", err)
	}
	bound, ok := vm.AOTObject(0x30001000)
	if !ok || bound != object {
		t.Fatalf("AOTObject() = %p/%t, want %p/true", bound, ok, object)
	}
	address, ok := vm.AOTAddress(object)
	if !ok || address != 0x30001000 {
		t.Fatalf("AOTAddress() = %#x/%t, want %#x/true", address, ok, uint32(0x30001000))
	}
	result, err := vm.InvokeVirtual(bound, "length", "()I")
	if err != nil {
		t.Fatalf("InvokeVirtual(String.length) error = %v", err)
	}
	length, err := result.Int32()
	if err != nil || length != 3 {
		t.Fatalf("String.length = %d/%v, want 3", length, err)
	}
	if err := vm.BindAOTObject(0x30001000, vm.NewString("other")); err == nil {
		t.Fatal("BindAOTObject() replaced an existing address")
	}
	if err := vm.BindAOTObject(0x30002000, object); err == nil {
		t.Fatal("BindAOTObject() assigned a second address to one object")
	}
}

func TestVMResolvesAOTMembersThroughBoundedHierarchy(t *testing.T) {
	vm := New(nil, Options{MaxFrames: 4})
	parent := AOTClassMetadata{
		Address: 0x1000,
		Name:    "game/Parent",
		Methods: []AOTMethodMetadata{{Address: 0x1100, Name: "run", Descriptor: "()V"}},
		Fields:  []AOTFieldMetadata{{Address: 0x1200, Name: "value", Descriptor: "I"}},
	}
	child := AOTClassMetadata{Address: 0x2000, Name: "game/Child", SuperName: "game/Parent"}
	if err := vm.RegisterAOTClass(parent); err != nil {
		t.Fatal(err)
	}
	if err := vm.RegisterAOTClass(child); err != nil {
		t.Fatal(err)
	}
	method, ok, err := vm.FindAOTMethod(child.Address, "run", "()V")
	if err != nil || !ok || method.Address != 0x1100 {
		t.Fatalf("FindAOTMethod() = %+v/%t/%v", method, ok, err)
	}
	field, ok, err := vm.FindAOTField(child.Address, "value", "I")
	if err != nil || !ok || field.Address != 0x1200 {
		t.Fatalf("FindAOTField() = %+v/%t/%v", field, ok, err)
	}

	parent.SuperName = child.Name
	if err := vm.RegisterAOTClass(parent); err != nil {
		t.Fatal(err)
	}
	if _, _, err := vm.FindAOTMethod(child.Address, "missing", "()V"); err == nil {
		t.Fatal("FindAOTMethod() did not reject a class hierarchy cycle")
	}
}

func TestVMInvokesRuntimeNativeSpecialMethodOnAOTSubclass(t *testing.T) {
	vm := New(nil, Options{})
	called := false
	if err := vm.RegisterNative("runtime/Parent", "<init>", "()V", func(_ *VM, arguments []Value) (Value, error) {
		if len(arguments) != 1 || arguments[0].Kind() != ValueReference {
			t.Fatalf("native constructor arguments = %+v", arguments)
		}
		called = true
		return VoidValue(), nil
	}); err != nil {
		t.Fatal(err)
	}
	receiver := &Object{ClassName: "game/Child", Fields: make(map[string]Value)}
	if _, err := vm.InvokeSpecial(receiver, "runtime/Parent", "<init>", "()V"); err != nil {
		t.Fatalf("InvokeSpecial() error = %v", err)
	}
	if !called {
		t.Fatal("InvokeSpecial() did not reach runtime native")
	}
}

func TestVMResolvesAOTAndRuntimeAssignability(t *testing.T) {
	vm := New(nil, Options{MaxFrames: 8})
	for _, metadata := range []AOTClassMetadata{
		{Address: 0x1000, Name: "game/BaseError", SuperName: "java/lang/RuntimeException"},
		{Address: 0x2000, Name: "game/SpecificError", SuperName: "game/BaseError"},
	} {
		if err := vm.RegisterAOTClass(metadata); err != nil {
			t.Fatal(err)
		}
	}
	for _, test := range []struct {
		class string
		super string
		want  bool
	}{
		{class: "game/SpecificError", super: "game/BaseError", want: true},
		{class: "game/SpecificError", super: "java/lang/Throwable", want: true},
		{class: "java/lang/NullPointerException", super: "java/lang/Throwable", want: true},
		{class: "game/SpecificError", super: "java/lang/Error", want: false},
	} {
		got, err := vm.IsSubclassOf(test.class, test.super)
		if err != nil || got != test.want {
			t.Fatalf("IsSubclassOf(%q, %q) = %t/%v, want %t", test.class, test.super, got, err, test.want)
		}
	}

	base, _ := vm.AOTClass("game/BaseError")
	base.SuperName = "game/SpecificError"
	if err := vm.RegisterAOTClass(base); err != nil {
		t.Fatal(err)
	}
	if _, err := vm.IsSubclassOf("game/SpecificError", "java/lang/Object"); err == nil {
		t.Fatal("IsSubclassOf() did not reject an AOT hierarchy cycle")
	}
}
