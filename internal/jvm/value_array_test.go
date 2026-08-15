package jvm

import "testing"

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
