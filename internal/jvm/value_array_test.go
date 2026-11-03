package jvm

import (
	"strings"
	"testing"
)

func TestNativeArraySnapshotAndValidatedStore(t *testing.T) {
	object := &Object{
		ClassName: "[I",
		Native: &Array{
			Component: Type{Kind: TypeInt},
			storage:   valueStorage([]Value{IntValue(1), IntValue(2), IntValue(3)}),
		},
	}
	component, snapshot, err := ArraySnapshot(object)
	if err != nil {
		t.Fatal(err)
	}
	if component.Kind != TypeInt || len(snapshot) != 3 {
		t.Fatalf("ArraySnapshot() = %+v, %v", component, snapshot)
	}
	snapshot[0] = IntValue(99)
	if err := SetArrayRange(object, 1, []Value{IntValue(7), IntValue(8)}); err != nil {
		t.Fatal(err)
	}
	_, got, err := ArraySnapshot(object)
	if err != nil {
		t.Fatal(err)
	}
	for index, want := range []int32{1, 7, 8} {
		value, valueErr := got[index].Int32()
		if valueErr != nil || value != want {
			t.Fatalf("stored array[%d] = %d, %v, want %d", index, value, valueErr, want)
		}
	}
	if err := SetArrayRange(object, 3, []Value{IntValue(9)}); err == nil {
		t.Fatal("SetArrayRange() accepted an out-of-bounds write")
	}
	if err := SetArrayRange(object, 0, []Value{LongValue(9)}); err == nil {
		t.Fatal("SetArrayRange() accepted a mismatched value kind")
	}
}

// One multianewarray is held to what one array of the maximum length may
// cost, counting the arrays it creates as well as their elements, and a
// negative dimension is refused even behind a zero one.
func TestMultiArrayAllocationIsBounded(t *testing.T) {
	grid := func(vm *VM, rows, columns int32) (*Object, error) {
		result, err := vm.InvokeStatic("LibraryContracts", "grid", "(II)[[I", IntValue(rows), IntValue(columns))
		if err != nil {
			return nil, err
		}
		return result.Reference()
	}
	small := New(libraryContractsSource(), Options{MaxArrayLength: 1000})
	if _, err := grid(small, 100, 100); err == nil {
		t.Fatal("100x100 grid under a 1000-element limit was allocated")
	}
	if _, err := grid(small, 1000, 0); err == nil {
		t.Fatal("1000 empty rows under a 1000-element limit were allocated")
	}
	vm := New(libraryContractsSource(), Options{})
	if _, err := grid(vm, 16384, 16384); err == nil || !strings.Contains(err.Error(), "exceeds limit") {
		t.Fatalf("16384x16384 grid error = %v, want the array limit", err)
	}
	if _, err := grid(vm, 0, -1); !vm.IsGuestException(err, "java/lang/NegativeArraySizeException") {
		t.Fatalf("grid(0, -1) error = %v, want NegativeArraySizeException", err)
	}
	array, err := grid(vm, 300, 200)
	if err != nil {
		t.Fatalf("300x200 grid error = %v", err)
	}
	_, rows, err := ArraySnapshot(array)
	if err != nil || len(rows) != 300 {
		t.Fatalf("grid rows = %d/%v, want 300", len(rows), err)
	}
	row, _ := rows[299].Reference()
	if _, columns, err := ArraySnapshot(row); err != nil || len(columns) != 200 {
		t.Fatalf("grid columns = %d/%v, want 200", len(columns), err)
	}
}
