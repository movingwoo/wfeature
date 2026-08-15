package jvm

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"testing"
)

type mapClassSource map[string][]byte

func (s mapClassSource) ClassBytes(name string) ([]byte, bool) {
	data, ok := s[name]
	return data, ok
}

func TestInvokeStaticRunsLoopAndNestedMethod(t *testing.T) {
	builder := newTestClassBuilder("Example")
	helperReference := builder.methodReference("Example", "twice", "(I)I")
	runCode := []byte{
		0x03,
		0x3c,
		0x03,
		0x3d,
		0x1c,
		0x1a,
		0xa2, 0x00, 0x0d,
		0x1b,
		0x1c,
		0x60,
		0x3c,
		0x84, 0x02, 0x01,
		0xa7, 0xff, 0xf4,
		0x1b,
		0xb8, byte(helperReference >> 8), byte(helperReference),
		0xac,
	}
	classData := builder.build(
		nil,
		[]testMethod{
			{name: "run", descriptor: "(I)I", maxStack: 2, maxLocals: 3, code: runCode},
			{name: "twice", descriptor: "(I)I", maxStack: 2, maxLocals: 1, code: []byte{0x1a, 0x05, 0x68, 0xac}},
		},
	)
	vm := New(mapClassSource{"Example": classData}, Options{})
	result, err := vm.InvokeStatic("Example", "run", "(I)I", IntValue(5))
	if err != nil {
		t.Fatalf("InvokeStatic() error = %v", err)
	}
	value, err := result.Int32()
	if err != nil {
		t.Fatal(err)
	}
	if value != 20 {
		t.Fatalf("result = %d, want 20", value)
	}
}

func TestInvokeStaticReadsAndWritesStaticField(t *testing.T) {
	builder := newTestClassBuilder("Counter")
	fieldReference := builder.fieldReference("Counter", "value", "I")
	code := []byte{
		0xb2, byte(fieldReference >> 8), byte(fieldReference),
		0x04,
		0x60,
		0x59,
		0xb3, byte(fieldReference >> 8), byte(fieldReference),
		0xac,
	}
	classData := builder.build(
		[]testField{{name: "value", descriptor: "I"}},
		[]testMethod{{name: "increment", descriptor: "()I", maxStack: 2, code: code}},
	)
	vm := New(mapClassSource{"Counter": classData}, Options{})
	for want := int32(1); want <= 2; want++ {
		result, err := vm.InvokeStatic("Counter", "increment", "()I")
		if err != nil {
			t.Fatalf("InvokeStatic() error = %v", err)
		}
		got, _ := result.Int32()
		if got != want {
			t.Fatalf("increment result = %d, want %d", got, want)
		}
	}
}

func TestInvokeStaticCallsRegisteredNative(t *testing.T) {
	builder := newTestClassBuilder("NativeCaller")
	nativeReference := builder.methodReference("host/Math", "twice", "(I)I")
	classData := builder.build(nil, []testMethod{{
		name:       "run",
		descriptor: "(I)I",
		maxStack:   1,
		maxLocals:  1,
		code:       []byte{0x1a, 0xb8, byte(nativeReference >> 8), byte(nativeReference), 0xac},
	}})
	vm := New(mapClassSource{"NativeCaller": classData}, Options{})
	err := vm.RegisterNative("host/Math", "twice", "(I)I", func(_ *VM, arguments []Value) (Value, error) {
		value, err := arguments[0].Int32()
		if err != nil {
			return VoidValue(), err
		}
		return IntValue(value * 2), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := vm.InvokeStatic("NativeCaller", "run", "(I)I", IntValue(21))
	if err != nil {
		t.Fatalf("InvokeStatic() error = %v", err)
	}
	value, _ := result.Int32()
	if value != 42 {
		t.Fatalf("result = %d, want 42", value)
	}
}

func TestInvokeStaticSupportsCategoryTwoArithmetic(t *testing.T) {
	builder := newTestClassBuilder("WideValues")
	classData := builder.build(nil, []testMethod{{
		name:       "add",
		descriptor: "()J",
		maxStack:   4,
		code:       []byte{0x0a, 0x0a, 0x61, 0xad},
	}})
	vm := New(mapClassSource{"WideValues": classData}, Options{})
	result, err := vm.InvokeStatic("WideValues", "add", "()J")
	if err != nil {
		t.Fatalf("InvokeStatic() error = %v", err)
	}
	value, _ := result.Int64()
	if value != 2 {
		t.Fatalf("result = %d, want 2", value)
	}
}

func TestInvokeStaticEnforcesStepLimit(t *testing.T) {
	builder := newTestClassBuilder("Loop")
	classData := builder.build(nil, []testMethod{{
		name:       "forever",
		descriptor: "()V",
		code:       []byte{0xa7, 0x00, 0x00},
	}})
	vm := New(mapClassSource{"Loop": classData}, Options{MaxSteps: 10})
	_, err := vm.InvokeStatic("Loop", "forever", "()V")
	if !errors.Is(err, ErrStepLimit) {
		t.Fatalf("InvokeStatic() error = %v, want ErrStepLimit", err)
	}
}

// A platform whose guest threads are the game itself makes the step limit a
// window: the loop keeps running while the platform grants windows and ends
// with whatever the platform answers when it stops granting them.
func TestStepLimitIsAWindowWhenAPlatformRenewsIt(t *testing.T) {
	builder := newTestClassBuilder("Loop")
	classData := builder.build(nil, []testMethod{{
		name:       "forever",
		descriptor: "()V",
		code:       []byte{0xa7, 0x00, 0x00},
	}})
	stop := errors.New("platform stopped the guest")
	windows := 0
	vm := New(mapClassSource{"Loop": classData}, Options{MaxSteps: 10, RenewSteps: func() error {
		windows++
		if windows > 3 {
			return stop
		}
		return nil
	}})
	_, err := vm.InvokeStatic("Loop", "forever", "()V")
	if !errors.Is(err, stop) {
		t.Fatalf("InvokeStatic() error = %v, want the platform's own error", err)
	}
	if windows != 4 {
		t.Fatalf("renewals = %d, want the loop to have asked four times", windows)
	}
}

func TestInvokeStaticEnforcesFrameLimit(t *testing.T) {
	builder := newTestClassBuilder("Recursive")
	reference := builder.methodReference("Recursive", "call", "()V")
	classData := builder.build(nil, []testMethod{{
		name:       "call",
		descriptor: "()V",
		code:       []byte{0xb8, byte(reference >> 8), byte(reference), 0xb1},
	}})
	vm := New(mapClassSource{"Recursive": classData}, Options{MaxFrames: 4})
	_, err := vm.InvokeStatic("Recursive", "call", "()V")
	if !errors.Is(err, ErrFrameLimit) {
		t.Fatalf("InvokeStatic() error = %v, want ErrFrameLimit", err)
	}
}

func TestInvokeStaticSupportsLegacySubroutines(t *testing.T) {
	builder := newTestClassBuilder("Subroutine")
	classData := builder.build(nil, []testMethod{{
		name:       "run",
		descriptor: "()V",
		maxStack:   1,
		maxLocals:  1,
		code:       []byte{0xa8, 0x00, 0x04, 0xb1, 0x4b, 0xa9, 0x00},
	}})
	vm := New(mapClassSource{"Subroutine": classData}, Options{})
	if _, err := vm.InvokeStatic("Subroutine", "run", "()V"); err != nil {
		t.Fatalf("InvokeStatic() error = %v", err)
	}
}

type testClassBuilder struct {
	name    string
	entries [][]byte
	indices map[string]uint16
}

type testMethod struct {
	name       string
	descriptor string
	maxStack   uint16
	maxLocals  uint16
	code       []byte
}

type testField struct {
	name       string
	descriptor string
}

func newTestClassBuilder(name string) *testClassBuilder {
	return &testClassBuilder{name: name, indices: make(map[string]uint16)}
}

func (b *testClassBuilder) add(key string, entry []byte) uint16 {
	if index := b.indices[key]; index != 0 {
		return index
	}
	b.entries = append(b.entries, entry)
	index := uint16(len(b.entries))
	b.indices[key] = index
	return index
}

func (b *testClassBuilder) utf8(value string) uint16 {
	var entry bytes.Buffer
	entry.WriteByte(1)
	writeTestU2(&entry, uint16(len(value)))
	entry.WriteString(value)
	return b.add("utf8:"+value, entry.Bytes())
}

func (b *testClassBuilder) class(name string) uint16 {
	nameIndex := b.utf8(name)
	var entry bytes.Buffer
	entry.WriteByte(7)
	writeTestU2(&entry, nameIndex)
	return b.add("class:"+name, entry.Bytes())
}

func (b *testClassBuilder) nameAndType(name, descriptor string) uint16 {
	nameIndex := b.utf8(name)
	descriptorIndex := b.utf8(descriptor)
	var entry bytes.Buffer
	entry.WriteByte(12)
	writeTestU2(&entry, nameIndex)
	writeTestU2(&entry, descriptorIndex)
	return b.add("nat:"+name+descriptor, entry.Bytes())
}

func (b *testClassBuilder) methodReference(class, name, descriptor string) uint16 {
	classIndex := b.class(class)
	nameAndTypeIndex := b.nameAndType(name, descriptor)
	var entry bytes.Buffer
	entry.WriteByte(10)
	writeTestU2(&entry, classIndex)
	writeTestU2(&entry, nameAndTypeIndex)
	return b.add("method:"+class+"."+name+descriptor, entry.Bytes())
}

func (b *testClassBuilder) fieldReference(class, name, descriptor string) uint16 {
	classIndex := b.class(class)
	nameAndTypeIndex := b.nameAndType(name, descriptor)
	var entry bytes.Buffer
	entry.WriteByte(9)
	writeTestU2(&entry, classIndex)
	writeTestU2(&entry, nameAndTypeIndex)
	return b.add("field:"+class+"."+name+descriptor, entry.Bytes())
}

func (b *testClassBuilder) build(fields []testField, methods []testMethod) []byte {
	thisClass := b.class(b.name)
	superClass := b.class("java/lang/Object")
	codeName := b.utf8("Code")
	for _, field := range fields {
		b.utf8(field.name)
		b.utf8(field.descriptor)
	}
	for _, method := range methods {
		b.utf8(method.name)
		b.utf8(method.descriptor)
	}

	var out bytes.Buffer
	writeTestU4(&out, 0xcafebabe)
	writeTestU2(&out, 0)
	writeTestU2(&out, 48)
	writeTestU2(&out, uint16(len(b.entries)+1))
	for _, entry := range b.entries {
		out.Write(entry)
	}
	writeTestU2(&out, 0x0021)
	writeTestU2(&out, thisClass)
	writeTestU2(&out, superClass)
	writeTestU2(&out, 0)
	writeTestU2(&out, uint16(len(fields)))
	for _, field := range fields {
		writeTestU2(&out, 0x0009)
		writeTestU2(&out, b.utf8(field.name))
		writeTestU2(&out, b.utf8(field.descriptor))
		writeTestU2(&out, 0)
	}
	writeTestU2(&out, uint16(len(methods)))
	for _, method := range methods {
		writeTestU2(&out, 0x0009)
		writeTestU2(&out, b.utf8(method.name))
		writeTestU2(&out, b.utf8(method.descriptor))
		writeTestU2(&out, 1)
		writeTestU2(&out, codeName)
		writeTestU4(&out, uint32(12+len(method.code)))
		writeTestU2(&out, method.maxStack)
		writeTestU2(&out, method.maxLocals)
		writeTestU4(&out, uint32(len(method.code)))
		out.Write(method.code)
		writeTestU2(&out, 0)
		writeTestU2(&out, 0)
	}
	writeTestU2(&out, 0)
	return out.Bytes()
}

func writeTestU2(out *bytes.Buffer, value uint16) {
	if err := binary.Write(out, binary.BigEndian, value); err != nil {
		panic(fmt.Sprintf("write u2: %v", err))
	}
}

func writeTestU4(out *bytes.Buffer, value uint32) {
	if err := binary.Write(out, binary.BigEndian, value); err != nil {
		panic(fmt.Sprintf("write u4: %v", err))
	}
}
