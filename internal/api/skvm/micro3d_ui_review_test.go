package skvm

import (
	"testing"

	"github.com/movingwoo/wfeature/internal/jvm"
)

// A title reads and writes V3 and A3 fields directly, so what these bodies
// store has to be what getfield reads, and what putfield stores has to be what
// they read.
func TestMicro3DFieldsAreTheOnesBytecodeReadsAndWrites(t *testing.T) {
	machine := newMicro3DMachine(t)
	vector := newVector(t, machine, 7, 8, 9)
	assertGuestVector(t, machine, vector, 7, 8, 9)

	// putfield p.x = 100, then a transform applied to p: the identity hands
	// back what the title wrote, and the title reads the result with getfield.
	if err := machine.SetField(vector, vector3DClass, "x", "I", jvm.IntValue(100)); err != nil {
		t.Fatal(err)
	}
	transform, err := machine.NewObject(affine3DClass, "()V")
	if err != nil {
		t.Fatal(err)
	}
	result := newVector(t, machine, 0, 0, 0)
	if _, err := machine.InvokeVirtual(transform, "trans", "(Lm/V3;Lm/V3;)V",
		jvm.ReferenceValue(result), jvm.ReferenceValue(vector)); err != nil {
		t.Fatal(err)
	}
	assertGuestVector(t, machine, result, 100, 8, 9)

	// The identity is visible cell by cell, and a cell the title writes is
	// the one the transform applies.
	cell, err := machine.Field(transform, affine3DClass, "m00", "I")
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := cell.Int32(); got != micro3DOne {
		t.Fatalf("getfield A3.m00 = %d, want %d", got, micro3DOne)
	}
	if err := machine.SetField(transform, affine3DClass, "m03", "I", jvm.IntValue(5)); err != nil {
		t.Fatal(err)
	}
	if _, err := machine.InvokeVirtual(transform, "trans", "(Lm/V3;Lm/V3;)V",
		jvm.ReferenceValue(result), jvm.ReferenceValue(vector)); err != nil {
		t.Fatal(err)
	}
	assertGuestVector(t, machine, result, 105, 8, 9)
}

// A quicksave written before these fields were keyed the way getfield keys
// them holds them under the bare field names, and the transforms keep reading
// those values.
func TestMicro3DFieldsFromAnOlderCheckpointStillRead(t *testing.T) {
	machine := newMicro3DMachine(t)
	vector := &jvm.Object{ClassName: vector3DClass, Fields: map[string]jvm.Value{
		"x": jvm.IntValue(3), "y": jvm.IntValue(4), "z": jvm.IntValue(5),
	}}
	assertVector(t, vector, 3, 4, 5)
	old := map[string]jvm.Value{}
	for index, name := range affine3DCells {
		old[name] = jvm.IntValue(identityAffine3D()[index])
	}
	old["m13"] = jvm.IntValue(10)
	transform := &jvm.Object{ClassName: affine3DClass, Fields: old}
	result := newVector(t, machine, 0, 0, 0)
	if _, err := machine.InvokeVirtual(transform, "trans", "(Lm/V3;Lm/V3;)V",
		jvm.ReferenceValue(result), jvm.ReferenceValue(vector)); err != nil {
		t.Fatal(err)
	}
	assertGuestVector(t, machine, result, 3, 14, 5)
}

func assertGuestVector(t *testing.T, machine *jvm.VM, object *jvm.Object, x, y, z int32) {
	t.Helper()
	got := [3]int32{}
	for index, name := range []string{"x", "y", "z"} {
		value, err := machine.Field(object, vector3DClass, name, "I")
		if err != nil {
			t.Fatal(err)
		}
		got[index], _ = value.Int32()
	}
	if got != [3]int32{x, y, z} {
		t.Fatalf("getfield V3 = %v, want (%d, %d, %d)", got, x, y, z)
	}
}
