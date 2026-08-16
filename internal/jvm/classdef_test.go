package jvm

import (
	"strings"
	"testing"
)

func TestDefineClassPublishesMetadataAndBody(t *testing.T) {
	vm := New(nil, Options{})
	err := vm.DefineClass(ClassDefinition{
		Name:      "test/Counter",
		SuperName: ObjectClass,
		Access:    AccessPublic,
		Fields: []FieldDefinition{
			{Name: "count", Descriptor: "I", Access: AccessProtected},
			{Name: "STEP", Descriptor: "I", Access: AccessPublic | AccessStatic | AccessFinal, Constant: IntValue(3)},
		},
		Methods: []MethodDefinition{
			{Name: "<init>", Descriptor: "()V", Access: AccessPublic, Body: emptyConstructor},
			{Name: "advance", Descriptor: "()I", Access: AccessPublic, Body: func(call *Invocation, arguments []Value) (Value, error) {
				receiver, err := requireObject(arguments, 0)
				if err != nil {
					return VoidValue(), err
				}
				count, err := intField(call.VM(), receiver, "test/Counter", "count")
				if err != nil {
					return VoidValue(), err
				}
				step, err := call.StaticField("test/Counter", "STEP", "I")
				if err != nil {
					return VoidValue(), err
				}
				increment, err := step.Int32()
				if err != nil {
					return VoidValue(), err
				}
				if err := setIntField(call.VM(), receiver, "test/Counter", "count", count+increment); err != nil {
					return VoidValue(), err
				}
				return IntValue(count + increment), nil
			}},
		},
	})
	if err != nil {
		t.Fatalf("DefineClass() error = %v", err)
	}

	object, err := vm.NewObject("test/Counter", "()V")
	if err != nil {
		t.Fatalf("NewObject() error = %v", err)
	}
	for want := int32(3); want <= 9; want += 3 {
		result, err := vm.InvokeVirtual(object, "advance", "()I")
		if err != nil {
			t.Fatalf("advance() error = %v", err)
		}
		value, err := result.Int32()
		if err != nil {
			t.Fatal(err)
		}
		if value != want {
			t.Fatalf("advance() = %d, want %d", value, want)
		}
	}

	// The metadata has to answer the questions a class file answered, or a
	// game's instanceof and its casts stop agreeing with its calls.
	if !vm.IsInstance(object, ObjectClass) {
		t.Error("a defined class is not an Object")
	}
	subclass, err := vm.IsSubclassOf("test/Counter", ObjectClass)
	if err != nil || !subclass {
		t.Errorf("IsSubclassOf() = %v, %v", subclass, err)
	}
	if !vm.HasMethodBody("test/Counter", "advance", "()I") {
		t.Error("a defined method with a body reports none")
	}
}

func TestDefineClassRejectsMistakes(t *testing.T) {
	cases := []struct {
		name       string
		definition ClassDefinition
		want       string
	}{
		{
			name:       "no superclass",
			definition: ClassDefinition{Name: "test/Orphan"},
			want:       "no superclass",
		},
		{
			name: "duplicate method",
			definition: ClassDefinition{Name: "test/Twice", SuperName: ObjectClass, Methods: []MethodDefinition{
				{Name: "run", Descriptor: "()V"},
				{Name: "run", Descriptor: "()V"},
			}},
			want: "declared twice",
		},
		{
			name: "unparsable descriptor",
			definition: ClassDefinition{Name: "test/Bad", SuperName: ObjectClass, Methods: []MethodDefinition{
				{Name: "run", Descriptor: "not a descriptor"},
			}},
			want: "method test/Bad.run",
		},
		{
			name: "abstract with a body",
			definition: ClassDefinition{Name: "test/Abstract", SuperName: ObjectClass, Methods: []MethodDefinition{
				{Name: "run", Descriptor: "()V", Access: AccessAbstract, Body: doNothing},
			}},
			want: "abstract method has a body",
		},
		{
			name: "constant on an instance field",
			definition: ClassDefinition{Name: "test/Constant", SuperName: ObjectClass, Fields: []FieldDefinition{
				{Name: "value", Descriptor: "I", Constant: IntValue(1)},
			}},
			want: "instance field has a constant",
		},
		{
			name: "constant of the wrong type",
			definition: ClassDefinition{Name: "test/Mistyped", SuperName: ObjectClass, Fields: []FieldDefinition{
				{Name: "value", Descriptor: "J", Access: AccessStatic, Constant: IntValue(1)},
			}},
			want: "constant of test/Mistyped.value",
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			vm := New(nil, Options{})
			err := vm.DefineClass(testCase.definition)
			if err == nil || !strings.Contains(err.Error(), testCase.want) {
				t.Fatalf("DefineClass() error = %v, want it to mention %q", err, testCase.want)
			}
		})
	}
}

func TestDefineClassRefusesASecondDefinition(t *testing.T) {
	vm := New(nil, Options{})
	definition := ClassDefinition{Name: "test/Once", SuperName: ObjectClass}
	if err := vm.DefineClass(definition); err != nil {
		t.Fatalf("DefineClass() error = %v", err)
	}
	if err := vm.DefineClass(definition); err == nil || !strings.Contains(err.Error(), "already defined") {
		t.Fatalf("second DefineClass() error = %v, want it to refuse", err)
	}
}

// A runtime-owned class outranks one an archive ships under the same name.
// A game that carries its own java/util/Vector must not replace the one its
// platform's other classes are written against.
func TestDefinedClassOutranksTheArchive(t *testing.T) {
	builder := newTestClassBuilder("test/Owned")
	classData := builder.build(nil, []testMethod{
		{name: "answer", descriptor: "()I", maxStack: 1, maxLocals: 0, code: []byte{0x05, 0xac}},
	})
	vm := New(mapClassSource{"test/Owned": classData}, Options{})
	err := vm.DefineClass(ClassDefinition{
		Name:      "test/Owned",
		SuperName: ObjectClass,
		Methods: []MethodDefinition{
			{Name: "answer", Descriptor: "()I", Access: AccessPublic | AccessStatic, Body: func(*Invocation, []Value) (Value, error) {
				return IntValue(42), nil
			}},
		},
	})
	if err != nil {
		t.Fatalf("DefineClass() error = %v", err)
	}
	result, err := vm.InvokeStatic("test/Owned", "answer", "()I")
	if err != nil {
		t.Fatalf("InvokeStatic() error = %v", err)
	}
	value, err := result.Int32()
	if err != nil {
		t.Fatal(err)
	}
	if value != 42 {
		t.Fatalf("answer() = %d, want the definition's 42", value)
	}
}

// A method that says synchronized is still synchronized with a Go body. The
// interpreter takes the monitor for a bytecode body after the native check, so
// this is the one thing moving a body out of bytecode could quietly drop.
func TestSynchronizedBodyHoldsTheMonitor(t *testing.T) {
	vm := New(nil, Options{})
	owner := make(chan uint64, 1)
	err := vm.DefineClass(ClassDefinition{
		Name:      "test/Locked",
		SuperName: ObjectClass,
		Methods: []MethodDefinition{
			{Name: "<init>", Descriptor: "()V", Access: AccessPublic, Body: emptyConstructor},
			{Name: "hold", Descriptor: "()V", Access: AccessPublic | AccessSynchronized, Body: func(call *Invocation, arguments []Value) (Value, error) {
				receiver, err := requireObject(arguments, 0)
				if err != nil {
					return VoidValue(), err
				}
				receiver.monitor.mu.Lock()
				owner <- receiver.monitor.owner
				receiver.monitor.mu.Unlock()
				// A second call on the same execution must not deadlock: Java
				// monitors are reentrant and a library method calling its own
				// class is ordinary.
				if len(arguments) == 1 {
					_, err = call.InvokeVirtual(receiver, "hold", "(I)V", IntValue(0))
				}
				return VoidValue(), err
			}},
			{Name: "hold", Descriptor: "(I)V", Access: AccessPublic | AccessSynchronized, Body: doNothing},
		},
	})
	if err != nil {
		t.Fatalf("DefineClass() error = %v", err)
	}
	object, err := vm.NewObject("test/Locked", "()V")
	if err != nil {
		t.Fatalf("NewObject() error = %v", err)
	}
	if _, err := vm.InvokeVirtual(object, "hold", "()V"); err != nil {
		t.Fatalf("hold() error = %v", err)
	}
	if held := <-owner; held == 0 {
		t.Fatal("a synchronized Go body ran without the receiver's monitor")
	}
	object.monitor.mu.Lock()
	depth := object.monitor.depth
	object.monitor.mu.Unlock()
	if depth != 0 {
		t.Fatalf("monitor depth after the call = %d, want 0", depth)
	}
}
