package jvm

import (
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

// A platform's defined constant holds the value this build defines after a
// heap restore, whatever the snapshot carried: a quick save from an earlier
// build restored that build's numbers, or none for a constant it did not have.
func TestDefinedConstantsHoldThisBuildsValuesAfterARestore(t *testing.T) {
	const class = "test/SoftKeys"
	define := func(vm *VM, constants []FieldDefinition) {
		t.Helper()
		if err := vm.DefineClass(ClassDefinition{Name: class, SuperName: ObjectClass, Access: AccessPublic, Fields: constants}); err != nil {
			t.Fatal(err)
		}
	}
	constant := func(name string, value int32) FieldDefinition {
		return FieldDefinition{Name: name, Descriptor: "I", Access: AccessPublic | AccessStatic | AccessFinal, Constant: IntValue(value)}
	}
	earlier := New(nil, Options{})
	define(earlier, []FieldDefinition{constant("SOFT1", 21), constant("SOFT2", 22)})
	// Read as a title reads it, which initializes the class.
	if _, err := earlier.StaticField(class, "SOFT1", "I"); err != nil {
		t.Fatal(err)
	}
	saved, err := earlier.CaptureHeapState(nil, HeapCodec{})
	if err != nil {
		t.Fatal(err)
	}
	current := New(nil, Options{})
	define(current, []FieldDefinition{constant("SOFT1", 90), constant("SOFT2", 91), constant("SOFT3", 92)})
	if _, err := current.RestoreHeapState(saved, HeapCodec{}); err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]int32{"SOFT1": 90, "SOFT2": 91, "SOFT3": 92} {
		value, err := current.StaticField(class, name, "I")
		if err != nil {
			t.Fatal(err)
		}
		if got, _ := value.Int32(); got != want {
			t.Errorf("%s = %d after the restore, want %d", name, got, want)
		}
	}
}
