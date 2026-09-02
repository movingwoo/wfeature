package skvm

import (
	"math"

	"github.com/movingwoo/wfeature/internal/jvm"
)

// The handset's 3D package, which is called `m`
//
// One local title draws its world through three classes in a package named
// `m`: a vector, an affine transform, and a renderer. It is this vendor's
// binding of the handset 3D middleware of the era — `.mbac` for a model,
// `.mtra` for a motion, a `.bmp` for the skin — and without the classes the
// title's Canvas dies in its own class initializer, before a pixel.
//
// **The maths is real and the renderer is not.** `V3` and `A3` are arithmetic:
// a title composes a camera, transforms a point and reads the result back as
// three integers it projects itself, and all of that is answered exactly.
// `XO_World` is the part that would need a rasterizer and the two model
// formats, and it keeps its state and draws nothing — the position
// `com.skt.m3d.Graphics3D` has been in since it was written, for the same
// reason (`docs/skvm.md`, "Deliberately incomplete").
//
// **The fixed point is the title's own arithmetic rather than a guess.** Its
// perspective is `4096 * sin(fov/2) / cos(fov/2)`, and it divides the product
// of a transformed coordinate and that value by 4096 — so one is 4096 here,
// and the field of view it asks for, 512, is 45 degrees on a circle of 4096.
// Both halves of the convention are read off the same two lines.
const (
	// micro3DOne is the fixed-point unit: 4096 is 1.0.
	micro3DOne = 4096
	// micro3DTurn is a full circle in the angle unit the trigonometry takes.
	micro3DTurn = 4096
)

const (
	vector3DClass = "m/V3"
	affine3DClass = "m/A3"
	world3DClass  = "m/XO_World"
)

func micro3DDefinitions() []jvm.ClassDefinition {
	return []jvm.ClassDefinition{
		{
			Name:      vector3DClass,
			SuperName: "java/lang/Object",
			Access:    jvm.AccessPublic,
			// The three coordinates are public fields, and a title reads and
			// writes them directly rather than through accessors.
			Fields: []jvm.FieldDefinition{
				{Name: "x", Descriptor: "I", Access: jvm.AccessPublic},
				{Name: "y", Descriptor: "I", Access: jvm.AccessPublic},
				{Name: "z", Descriptor: "I", Access: jvm.AccessPublic},
			},
			Methods: []jvm.MethodDefinition{
				{Name: "<init>", Descriptor: "()V", Access: jvm.AccessPublic, Body: vector3DInit},
				{Name: "<init>", Descriptor: "(III)V", Access: jvm.AccessPublic, Body: vector3DInit},
				{Name: "set", Descriptor: "(III)V", Access: jvm.AccessPublic, Body: vector3DSet},
			},
		},
		{
			Name:      affine3DClass,
			SuperName: "java/lang/Object",
			Access:    jvm.AccessPublic,
			// A 3x4 affine transform: nine rotation cells in fixed point and
			// three translation cells in the same units as a coordinate.
			Fields: affine3DFields(),
			Methods: []jvm.MethodDefinition{
				{Name: "<init>", Descriptor: "()V", Access: jvm.AccessPublic, Body: affine3DInit},
				{Name: "ident", Descriptor: "()V", Access: jvm.AccessPublic, Body: affine3DIdent},
				{Name: "set", Descriptor: "(Lm/A3;)V", Access: jvm.AccessPublic, Body: affine3DSet},
				{Name: "mul", Descriptor: "(Lm/A3;Lm/A3;)V", Access: jvm.AccessPublic, Body: affine3DMul},
				{Name: "trans", Descriptor: "(Lm/V3;Lm/V3;)V", Access: jvm.AccessPublic, Body: affine3DTrans},
			},
		},
		{
			Name:      world3DClass,
			SuperName: "java/lang/Object",
			Access:    jvm.AccessPublic,
			Methods: []jvm.MethodDefinition{
				{Name: "<init>", Descriptor: "()V", Access: jvm.AccessPublic, Body: emptyInit},
				// The trigonometry is the half of this class a title does its
				// own geometry with, so it is answered rather than stubbed.
				{Name: "sin", Descriptor: "(I)I", Access: jvm.AccessPublic | jvm.AccessStatic, Body: micro3DSin},
				{Name: "cos", Descriptor: "(I)I", Access: jvm.AccessPublic | jvm.AccessStatic, Body: micro3DCos},
				{Name: "getViewTrans", Descriptor: "(Lm/V3;Lm/V3;Lm/V3;Lm/A3;)V", Access: jvm.AccessPublic | jvm.AccessStatic, Body: micro3DViewTrans},
				{Name: "rotY", Descriptor: "(ILm/A3;)V", Access: jvm.AccessPublic | jvm.AccessStatic, Body: micro3DRotate(rotateY)},
				{Name: "rotZ", Descriptor: "(ILm/A3;)V", Access: jvm.AccessPublic | jvm.AccessStatic, Body: micro3DRotate(rotateZ)},
				// Everything below is the renderer. It keeps what it is told
				// and draws nothing; see the comment at the top of this file.
				{Name: "loadMBAC", Descriptor: "(Ljava/io/InputStream;)I", Access: jvm.AccessPublic, Body: micro3DLoad},
				{Name: "loadMTRA", Descriptor: "(Ljava/io/InputStream;)I", Access: jvm.AccessPublic, Body: micro3DLoad},
				{Name: "loadBMP", Descriptor: "(Ljava/io/InputStream;)I", Access: jvm.AccessPublic, Body: micro3DLoad},
				{Name: "shareData", Descriptor: "(Lm/XO_World;)V", Access: jvm.AccessPublic, Body: micro3DIgnore},
				{Name: "setVram", Descriptor: "(Ljavax/microedition/lcdui/Graphics;Lm/XO_World;II)V", Access: jvm.AccessPublic, Body: micro3DIgnore},
				{Name: "setView", Descriptor: "(Lm/A3;IIII)V", Access: jvm.AccessPublic, Body: micro3DIgnore},
				{Name: "setClip", Descriptor: "(IIII)V", Access: jvm.AccessPublic, Body: micro3DIgnore},
				{Name: "setPosture", Descriptor: "(II)V", Access: jvm.AccessPublic, Body: micro3DIgnore},
				{Name: "getMaxFrame", Descriptor: "(I)I", Access: jvm.AccessPublic, Body: micro3DNoFrames},
				{Name: "draw", Descriptor: "(Ljavax/microedition/lcdui/Graphics;)V", Access: jvm.AccessPublic, Body: micro3DIgnore},
				{Name: "dispose", Descriptor: "()V", Access: jvm.AccessPublic, Body: micro3DIgnore},
			},
		},
	}
}

// affine3DCells names the twelve fields in row-major order: three rows of a
// rotation and a translation.
var affine3DCells = [12]string{
	"m00", "m01", "m02", "m03",
	"m10", "m11", "m12", "m13",
	"m20", "m21", "m22", "m23",
}

func affine3DFields() []jvm.FieldDefinition {
	fields := make([]jvm.FieldDefinition, 0, len(affine3DCells))
	for _, name := range affine3DCells {
		fields = append(fields, jvm.FieldDefinition{Name: name, Descriptor: "I", Access: jvm.AccessPublic})
	}
	return fields
}

func vector3DInit(_ *jvm.Invocation, arguments []jvm.Value) (jvm.Value, error) {
	object, err := receiver(arguments)
	if err != nil {
		return jvm.VoidValue(), err
	}
	if len(arguments) == 1 {
		return jvm.VoidValue(), setVector3D(object, 0, 0, 0)
	}
	return jvm.VoidValue(), setVector3DFrom(object, arguments, 1)
}

func vector3DSet(_ *jvm.Invocation, arguments []jvm.Value) (jvm.Value, error) {
	object, err := receiver(arguments)
	if err != nil {
		return jvm.VoidValue(), err
	}
	return jvm.VoidValue(), setVector3DFrom(object, arguments, 1)
}

func setVector3DFrom(object *jvm.Object, arguments []jvm.Value, start int) error {
	if len(arguments) < start+3 {
		return jvm.Throw("java/lang/IllegalArgumentException", "a vector takes three coordinates")
	}
	values := [3]int32{}
	for index := range values {
		value, err := arguments[start+index].Int32()
		if err != nil {
			return err
		}
		values[index] = value
	}
	return setVector3D(object, values[0], values[1], values[2])
}

func setVector3D(object *jvm.Object, x, y, z int32) error {
	if object.Fields == nil {
		object.Fields = make(map[string]jvm.Value)
	}
	object.Fields["x"] = jvm.IntValue(x)
	object.Fields["y"] = jvm.IntValue(y)
	object.Fields["z"] = jvm.IntValue(z)
	return nil
}

func vector3DOf(object *jvm.Object) (x, y, z int32) {
	if object == nil {
		return 0, 0, 0
	}
	x, _ = intField(object, "x")
	y, _ = intField(object, "y")
	z, _ = intField(object, "z")
	return x, y, z
}

func intField(object *jvm.Object, name string) (int32, bool) {
	if object == nil || object.Fields == nil {
		return 0, false
	}
	value, ok := object.Fields[name]
	if !ok {
		return 0, false
	}
	number, err := value.Int32()
	if err != nil {
		return 0, false
	}
	return number, true
}

// A new transform is the identity, which is what a title that builds a camera
// out of a rotation and a translation starts from.
func affine3DInit(_ *jvm.Invocation, arguments []jvm.Value) (jvm.Value, error) {
	object, err := receiver(arguments)
	if err != nil {
		return jvm.VoidValue(), err
	}
	writeAffine3D(object, identityAffine3D())
	return jvm.VoidValue(), nil
}

func affine3DIdent(call *jvm.Invocation, arguments []jvm.Value) (jvm.Value, error) {
	return affine3DInit(call, arguments)
}

func affine3DSet(_ *jvm.Invocation, arguments []jvm.Value) (jvm.Value, error) {
	object, err := receiver(arguments)
	if err != nil {
		return jvm.VoidValue(), err
	}
	source, err := affine3DArgument(arguments, 1)
	if err != nil {
		return jvm.VoidValue(), err
	}
	writeAffine3D(object, source)
	return jvm.VoidValue(), nil
}

// mul(a, b) is this = a * b, with the rotation renormalized by the fixed point
// and the translation of `a` carried through `b`'s rotation.
func affine3DMul(_ *jvm.Invocation, arguments []jvm.Value) (jvm.Value, error) {
	object, err := receiver(arguments)
	if err != nil {
		return jvm.VoidValue(), err
	}
	left, err := affine3DArgument(arguments, 1)
	if err != nil {
		return jvm.VoidValue(), err
	}
	right, err := affine3DArgument(arguments, 2)
	if err != nil {
		return jvm.VoidValue(), err
	}
	var product [12]int32
	for row := 0; row < 3; row++ {
		for column := 0; column < 3; column++ {
			sum := int64(0)
			for index := 0; index < 3; index++ {
				sum += int64(left[row*4+index]) * int64(right[index*4+column])
			}
			product[row*4+column] = int32(sum / micro3DOne)
		}
		sum := int64(left[row*4+3])
		for index := 0; index < 3; index++ {
			sum += int64(left[row*4+index]) * int64(right[index*4+3]) / micro3DOne
		}
		product[row*4+3] = int32(sum)
	}
	writeAffine3D(object, product)
	return jvm.VoidValue(), nil
}

// trans(destination, source) applies the transform to a point: the rotation is
// fixed point and the translation is already in the coordinate's own units.
func affine3DTrans(_ *jvm.Invocation, arguments []jvm.Value) (jvm.Value, error) {
	object, err := receiver(arguments)
	if err != nil {
		return jvm.VoidValue(), err
	}
	transform, err := affineOf(object)
	if err != nil {
		return jvm.VoidValue(), err
	}
	destination, err := referenceAt(arguments, 1)
	if err != nil {
		return jvm.VoidValue(), err
	}
	source, err := referenceAt(arguments, 2)
	if err != nil {
		return jvm.VoidValue(), err
	}
	if destination == nil || source == nil {
		return jvm.VoidValue(), jvm.Throw("java/lang/NullPointerException", "a transform takes two vectors")
	}
	x, y, z := vector3DOf(source)
	var out [3]int32
	for row := 0; row < 3; row++ {
		sum := int64(transform[row*4])*int64(x) +
			int64(transform[row*4+1])*int64(y) +
			int64(transform[row*4+2])*int64(z)
		out[row] = int32(sum/micro3DOne) + transform[row*4+3]
	}
	return jvm.VoidValue(), setVector3D(destination, out[0], out[1], out[2])
}

func affine3DArgument(arguments []jvm.Value, index int) ([12]int32, error) {
	object, err := referenceAt(arguments, index)
	if err != nil {
		return [12]int32{}, err
	}
	return affineOf(object)
}

func affineOf(object *jvm.Object) ([12]int32, error) {
	if object == nil {
		return [12]int32{}, jvm.Throw("java/lang/NullPointerException", "transform is null")
	}
	var cells [12]int32
	for index, name := range affine3DCells {
		cells[index], _ = intField(object, name)
	}
	return cells, nil
}

func writeAffine3D(object *jvm.Object, cells [12]int32) {
	if object.Fields == nil {
		object.Fields = make(map[string]jvm.Value)
	}
	for index, name := range affine3DCells {
		object.Fields[name] = jvm.IntValue(cells[index])
	}
}

func identityAffine3D() [12]int32 {
	return [12]int32{
		micro3DOne, 0, 0, 0,
		0, micro3DOne, 0, 0,
		0, 0, micro3DOne, 0,
	}
}

func micro3DSin(_ *jvm.Invocation, arguments []jvm.Value) (jvm.Value, error) {
	return micro3DTrig(arguments, math.Sin)
}

func micro3DCos(_ *jvm.Invocation, arguments []jvm.Value) (jvm.Value, error) {
	return micro3DTrig(arguments, math.Cos)
}

func micro3DTrig(arguments []jvm.Value, of func(float64) float64) (jvm.Value, error) {
	if len(arguments) < 1 {
		return jvm.VoidValue(), jvm.Throw("java/lang/IllegalArgumentException", "an angle is one argument")
	}
	angle, err := arguments[0].Int32()
	if err != nil {
		return jvm.VoidValue(), err
	}
	radians := float64(angle) * 2 * math.Pi / micro3DTurn
	return jvm.IntValue(int32(math.Round(of(radians) * micro3DOne))), nil
}

type rotationAxis int

const (
	rotateY rotationAxis = iota
	rotateZ
)

// rotY and rotZ fill a transform with a rotation about one axis and no
// translation, which is how a title turns its camera or its model.
func micro3DRotate(axis rotationAxis) jvm.ContextMethod {
	return func(_ *jvm.Invocation, arguments []jvm.Value) (jvm.Value, error) {
		if len(arguments) < 2 {
			return jvm.VoidValue(), jvm.Throw("java/lang/IllegalArgumentException", "a rotation takes an angle and a transform")
		}
		angle, err := arguments[0].Int32()
		if err != nil {
			return jvm.VoidValue(), err
		}
		target, err := referenceAt(arguments, 1)
		if err != nil {
			return jvm.VoidValue(), err
		}
		if target == nil {
			return jvm.VoidValue(), jvm.Throw("java/lang/NullPointerException", "rotation target is null")
		}
		radians := float64(angle) * 2 * math.Pi / micro3DTurn
		sin := int32(math.Round(math.Sin(radians) * micro3DOne))
		cos := int32(math.Round(math.Cos(radians) * micro3DOne))
		cells := identityAffine3D()
		switch axis {
		case rotateY:
			cells[0], cells[2] = cos, sin
			cells[8], cells[10] = -sin, cos
		case rotateZ:
			cells[0], cells[1] = cos, -sin
			cells[4], cells[5] = sin, cos
		}
		writeAffine3D(target, cells)
		return jvm.VoidValue(), nil
	}
}

// getViewTrans builds the transform that takes a point into the camera's
// frame: the three basis vectors of a look-at, and the camera's own position
// carried through them.
func micro3DViewTrans(_ *jvm.Invocation, arguments []jvm.Value) (jvm.Value, error) {
	if len(arguments) < 4 {
		return jvm.VoidValue(), jvm.Throw("java/lang/IllegalArgumentException", "a view takes a position, a target, an up vector and a transform")
	}
	position, err := referenceAt(arguments, 0)
	if err != nil {
		return jvm.VoidValue(), err
	}
	target, err := referenceAt(arguments, 1)
	if err != nil {
		return jvm.VoidValue(), err
	}
	up, err := referenceAt(arguments, 2)
	if err != nil {
		return jvm.VoidValue(), err
	}
	out, err := referenceAt(arguments, 3)
	if err != nil {
		return jvm.VoidValue(), err
	}
	if position == nil || target == nil || up == nil || out == nil {
		return jvm.VoidValue(), jvm.Throw("java/lang/NullPointerException", "a view takes four objects")
	}
	eyeX, eyeY, eyeZ := vector3DOf(position)
	atX, atY, atZ := vector3DOf(target)
	upX, upY, upZ := vector3DOf(up)

	forward := normalize3D([3]float64{float64(atX - eyeX), float64(atY - eyeY), float64(atZ - eyeZ)})
	side := normalize3D(cross3D(forward, [3]float64{float64(upX), float64(upY), float64(upZ)}))
	trueUp := cross3D(side, forward)

	cells := identityAffine3D()
	rows := [3][3]float64{side, trueUp, {-forward[0], -forward[1], -forward[2]}}
	eye := [3]float64{float64(eyeX), float64(eyeY), float64(eyeZ)}
	for row := 0; row < 3; row++ {
		for column := 0; column < 3; column++ {
			cells[row*4+column] = int32(math.Round(rows[row][column] * micro3DOne))
		}
		translation := -(rows[row][0]*eye[0] + rows[row][1]*eye[1] + rows[row][2]*eye[2])
		cells[row*4+3] = int32(math.Round(translation))
	}
	writeAffine3D(out, cells)
	return jvm.VoidValue(), nil
}

func cross3D(left, right [3]float64) [3]float64 {
	return [3]float64{
		left[1]*right[2] - left[2]*right[1],
		left[2]*right[0] - left[0]*right[2],
		left[0]*right[1] - left[1]*right[0],
	}
}

func normalize3D(vector [3]float64) [3]float64 {
	length := math.Sqrt(vector[0]*vector[0] + vector[1]*vector[1] + vector[2]*vector[2])
	if length == 0 {
		return [3]float64{0, 0, 0}
	}
	return [3]float64{vector[0] / length, vector[1] / length, vector[2] / length}
}

// The renderer's own calls. A loader answers a handle the title discards, and
// a model with no frames is what an unloaded motion has.
func micro3DLoad(*jvm.Invocation, []jvm.Value) (jvm.Value, error) {
	return jvm.IntValue(0), nil
}

func micro3DNoFrames(*jvm.Invocation, []jvm.Value) (jvm.Value, error) {
	return jvm.IntValue(0), nil
}

func micro3DIgnore(*jvm.Invocation, []jvm.Value) (jvm.Value, error) {
	return jvm.VoidValue(), nil
}

func referenceAt(arguments []jvm.Value, index int) (*jvm.Object, error) {
	if index >= len(arguments) {
		return nil, jvm.Throw("java/lang/IllegalArgumentException", "argument is missing")
	}
	return arguments[index].Reference()
}
