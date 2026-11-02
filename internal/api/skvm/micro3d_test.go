package skvm

import (
	"testing"

	"github.com/movingwoo/wfeature/internal/jvm"
)

// The trigonometry is the half of this surface a title does its own geometry
// with, and both halves of its convention come from one line of a local
// title's own arithmetic: it computes `4096 * sin(fov/2) / cos(fov/2)` and
// asks for a field of view of 512. If either the unit or the circle were
// wrong, that perspective would come out as a number nothing on screen could
// be built from.
func TestMicro3DTrigonometryIsFixedPointOverAFourThousandNinetySixCircle(t *testing.T) {
	machine := newMicro3DMachine(t)
	for _, probe := range []struct {
		angle    int32
		sin, cos int32
	}{
		{angle: 0, sin: 0, cos: micro3DOne},
		{angle: micro3DTurn / 4, sin: micro3DOne, cos: 0},
		{angle: micro3DTurn / 2, sin: 0, cos: -micro3DOne},
		{angle: 3 * micro3DTurn / 4, sin: -micro3DOne, cos: 0},
		{angle: micro3DTurn, sin: 0, cos: micro3DOne},
	} {
		assertInt(t, machine, world3DClass, "sin", "(I)I", probe.sin, jvm.IntValue(probe.angle))
		assertInt(t, machine, world3DClass, "cos", "(I)I", probe.cos, jvm.IntValue(probe.angle))
	}

	// The title's own perspective: tan(22.5 degrees) is a little over 0.414,
	// which is a little over 1696 in this fixed point.
	sin, err := machine.InvokeStatic(world3DClass, "sin", "(I)I", jvm.IntValue(256))
	if err != nil {
		t.Fatal(err)
	}
	cos, err := machine.InvokeStatic(world3DClass, "cos", "(I)I", jvm.IntValue(256))
	if err != nil {
		t.Fatal(err)
	}
	sinValue, _ := sin.Int32()
	cosValue, _ := cos.Int32()
	tangent := micro3DOne * sinValue / cosValue
	if tangent < 1690 || tangent > 1700 {
		t.Fatalf("tan(45/2 degrees) = %d, want about 1696", tangent)
	}
}

// A transform composes and applies. A title builds a camera out of a rotation
// and a translation and then reads three integers back out of it, so what has
// to be right is the whole round trip rather than any one cell.
func TestMicro3DTransformComposesAndApplies(t *testing.T) {
	machine := newMicro3DMachine(t)
	transform, err := machine.NewObject(affine3DClass, "()V")
	if err != nil {
		t.Fatal(err)
	}
	point := newVector(t, machine, 100, 0, 0)
	result := newVector(t, machine, 0, 0, 0)

	// A fresh transform is the identity, so a point comes back unchanged.
	if _, err := machine.InvokeVirtual(transform, "trans", "(Lm/V3;Lm/V3;)V",
		jvm.ReferenceValue(result), jvm.ReferenceValue(point)); err != nil {
		t.Fatal(err)
	}
	assertVector(t, result, 100, 0, 0)

	// A quarter turn about Z takes the x axis onto the y axis.
	if _, err := machine.InvokeStatic(world3DClass, "rotZ", "(ILm/A3;)V",
		jvm.IntValue(micro3DTurn/4), jvm.ReferenceValue(transform)); err != nil {
		t.Fatal(err)
	}
	if _, err := machine.InvokeVirtual(transform, "trans", "(Lm/V3;Lm/V3;)V",
		jvm.ReferenceValue(result), jvm.ReferenceValue(point)); err != nil {
		t.Fatal(err)
	}
	assertVector(t, result, 0, 100, 0)

	// Two quarter turns compose into a half turn rather than into a transform
	// four thousand times too large, which is what a missing renormalization
	// would give.
	quarter, err := machine.NewObject(affine3DClass, "()V")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := machine.InvokeStatic(world3DClass, "rotZ", "(ILm/A3;)V",
		jvm.IntValue(micro3DTurn/4), jvm.ReferenceValue(quarter)); err != nil {
		t.Fatal(err)
	}
	if _, err := machine.InvokeVirtual(transform, "mul", "(Lm/A3;Lm/A3;)V",
		jvm.ReferenceValue(quarter), jvm.ReferenceValue(quarter)); err != nil {
		t.Fatal(err)
	}
	if _, err := machine.InvokeVirtual(transform, "trans", "(Lm/V3;Lm/V3;)V",
		jvm.ReferenceValue(result), jvm.ReferenceValue(point)); err != nil {
		t.Fatal(err)
	}
	assertVector(t, result, -100, 0, 0)
}

// A camera looking down its own axis puts the point it is looking at straight
// ahead of it, at the distance between them.
func TestMicro3DViewTransPutsTheTargetAhead(t *testing.T) {
	machine := newMicro3DMachine(t)
	view, err := machine.NewObject(affine3DClass, "()V")
	if err != nil {
		t.Fatal(err)
	}
	position := newVector(t, machine, 0, 0, 300)
	target := newVector(t, machine, 0, 0, 0)
	up := newVector(t, machine, 0, micro3DOne, 0)
	if _, err := machine.InvokeStatic(world3DClass, "getViewTrans", "(Lm/V3;Lm/V3;Lm/V3;Lm/A3;)V",
		jvm.ReferenceValue(position), jvm.ReferenceValue(target),
		jvm.ReferenceValue(up), jvm.ReferenceValue(view)); err != nil {
		t.Fatal(err)
	}
	result := newVector(t, machine, 0, 0, 0)
	if _, err := machine.InvokeVirtual(view, "trans", "(Lm/V3;Lm/V3;)V",
		jvm.ReferenceValue(result), jvm.ReferenceValue(target)); err != nil {
		t.Fatal(err)
	}
	// The camera's frame looks down its own negative z, so the target it is
	// three hundred units from sits at -300.
	assertVector(t, result, 0, 0, -300)
}

func newMicro3DMachine(t *testing.T) *jvm.VM {
	t.Helper()
	machine := jvm.New(nil, jvm.Options{})
	if err := Define(machine); err != nil {
		t.Fatalf("Define() error = %v", err)
	}
	return machine
}

func newVector(t *testing.T, machine *jvm.VM, x, y, z int32) *jvm.Object {
	t.Helper()
	object, err := machine.NewObject(vector3DClass, "(III)V", jvm.IntValue(x), jvm.IntValue(y), jvm.IntValue(z))
	if err != nil {
		t.Fatalf("new V3(%d, %d, %d) error = %v", x, y, z, err)
	}
	return object
}

func assertVector(t *testing.T, object *jvm.Object, x, y, z int32) {
	t.Helper()
	gotX, gotY, gotZ := vector3DOf(object)
	if gotX != x || gotY != y || gotZ != z {
		t.Fatalf("vector = (%d, %d, %d), want (%d, %d, %d)", gotX, gotY, gotZ, x, y, z)
	}
}

func assertInt(t *testing.T, machine *jvm.VM, class, name, descriptor string, want int32, arguments ...jvm.Value) {
	t.Helper()
	result, err := machine.InvokeStatic(class, name, descriptor, arguments...)
	if err != nil {
		t.Fatalf("%s.%s%s error = %v", class, name, descriptor, err)
	}
	value, err := result.Int32()
	if err != nil {
		t.Fatal(err)
	}
	if value != want {
		t.Fatalf("%s.%s%v = %d, want %d", class, name, arguments, value, want)
	}
}

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
