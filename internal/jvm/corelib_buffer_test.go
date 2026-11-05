package jvm

import (
	"errors"
	"math"
	"testing"
)

// newBuffer builds a StringBuffer through one of its constructors.
func newBuffer(t *testing.T, vm *VM, descriptor string, arguments ...Value) *Object {
	t.Helper()
	buffer, err := vm.NewObject(StringBufferClass, descriptor, arguments...)
	if err != nil {
		t.Fatal(err)
	}
	return buffer
}

func bufferInt(t *testing.T, vm *VM, buffer *Object, name, descriptor string, arguments ...Value) int32 {
	t.Helper()
	result, err := vm.InvokeVirtual(buffer, name, descriptor, arguments...)
	if err != nil {
		t.Fatalf("%s%s error = %v", name, descriptor, err)
	}
	value, err := result.Int32()
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func bufferCall(t *testing.T, vm *VM, buffer *Object, name, descriptor string, arguments ...Value) {
	t.Helper()
	if _, err := vm.InvokeVirtual(buffer, name, descriptor, arguments...); err != nil {
		t.Fatalf("%s%s error = %v", name, descriptor, err)
	}
}

// charArray is a char[] holding units exactly, a lone half of a pair included.
func charArray(t *testing.T, vm *VM, units []uint16) *Object {
	t.Helper()
	array, err := vm.NewArray(Type{Kind: TypeChar}, int32(len(units)))
	if err != nil {
		t.Fatal(err)
	}
	values := make([]Value, len(units))
	for index, unit := range units {
		values[index] = IntValue(int32(unit))
	}
	if err := SetArrayRange(array, 0, values); err != nil {
		t.Fatal(err)
	}
	return array
}

func bufferText(t *testing.T, vm *VM, buffer *Object) string {
	t.Helper()
	result, err := vm.InvokeVirtual(buffer, "toString", "()Ljava/lang/String;")
	if err != nil {
		t.Fatal(err)
	}
	object, err := result.Reference()
	if err != nil {
		t.Fatal(err)
	}
	text, _ := StringText(object)
	return text
}

// capacity() is the class library's number, which LGT answers the same way: 16
// for an empty buffer, a String's length and 16 more for one built from it,
// the count a title names, and on outgrowing it (capacity + 1) * 2, or the
// length needed when that is more. Nothing a title does to shorten a buffer
// gives any of it back, and asking for room allocates nothing.
func TestStringBufferCapacityFollowsTheClassLibrary(t *testing.T) {
	vm := New(nil, Options{})
	str := func(text string) Value { return ReferenceValue(vm.NewString(text)) }
	capacity := func(buffer *Object) int32 {
		t.Helper()
		return bufferInt(t, vm, buffer, "capacity", "()I")
	}
	appendText := func(buffer *Object, text string) {
		t.Helper()
		bufferCall(t, vm, buffer, "append", "(Ljava/lang/String;)Ljava/lang/StringBuffer;", str(text))
	}

	empty := newBuffer(t, vm, "()V")
	if got := capacity(empty); got != 16 {
		t.Fatalf("StringBuffer() capacity = %d, want 16", got)
	}
	appendText(empty, "0123456789abcdef")
	if got := capacity(empty); got != 16 {
		t.Fatalf("capacity holding 16 = %d, want 16", got)
	}
	appendText(empty, "g")
	if got := capacity(empty); got != 34 {
		t.Fatalf("capacity after the 17th unit = %d, want (16+1)*2 = 34", got)
	}
	// A growth by more than doubling gives the length needed.
	appendText(empty, string(make([]byte, 100)))
	if got := capacity(empty); got != 117 {
		t.Fatalf("capacity after growing to 117 = %d, want 117", got)
	}

	if got := capacity(newBuffer(t, vm, "(Ljava/lang/String;)V", str("abc"))); got != 19 {
		t.Fatalf("StringBuffer(\"abc\") capacity = %d, want 3+16", got)
	}
	none := newBuffer(t, vm, "(I)V", IntValue(0))
	if got := capacity(none); got != 0 {
		t.Fatalf("StringBuffer(0) capacity = %d", got)
	}
	bufferCall(t, vm, none, "append", "(C)Ljava/lang/StringBuffer;", IntValue('a'))
	if got := capacity(none); got != 2 {
		t.Fatalf("StringBuffer(0) after one append = %d, want (0+1)*2", got)
	}
	if _, err := vm.NewObject(StringBufferClass, "(I)V", IntValue(-1)); !vm.IsGuestException(err, "java/lang/NegativeArraySizeException") {
		t.Fatalf("StringBuffer(-1) = %v, want NegativeArraySizeException", err)
	}

	sized := newBuffer(t, vm, "(I)V", IntValue(5))
	bufferCall(t, vm, sized, "ensureCapacity", "(I)V", IntValue(7))
	if got := capacity(sized); got != 12 {
		t.Fatalf("ensureCapacity(7) of 5 = %d, want (5+1)*2", got)
	}
	bufferCall(t, vm, sized, "ensureCapacity", "(I)V", IntValue(-9))
	bufferCall(t, vm, sized, "ensureCapacity", "(I)V", IntValue(12))
	if got := capacity(sized); got != 12 {
		t.Fatalf("ensureCapacity of what it has, or less, = %d, want 12", got)
	}
	_, data, err := stringBufferArgument([]Value{ReferenceValue(sized)})
	if err != nil {
		t.Fatal(err)
	}
	room := cap(data.units)
	bufferCall(t, vm, sized, "ensureCapacity", "(I)V", IntValue(math.MaxInt32))
	if got := capacity(sized); got != math.MaxInt32 {
		t.Fatalf("ensureCapacity(MAX_VALUE) = %d", got)
	}
	if cap(data.units) != room {
		t.Fatal("ensureCapacity allocated the room it named")
	}

	// setLength grows the number as an append would; shortening, deleting
	// and a setLength(0) keep it.
	short := newBuffer(t, vm, "()V")
	bufferCall(t, vm, short, "setLength", "(I)V", IntValue(40))
	if got := capacity(short); got != 40 {
		t.Fatalf("setLength(40) of 16 = %d, want 40", got)
	}
	bufferCall(t, vm, short, "delete", "(II)Ljava/lang/StringBuffer;", IntValue(0), IntValue(30))
	bufferCall(t, vm, short, "setLength", "(I)V", IntValue(0))
	if got := capacity(short); got != 40 {
		t.Fatalf("capacity after the buffer was emptied = %d, want 40", got)
	}
	// The existing insert forms grow it too.
	full := newBuffer(t, vm, "(Ljava/lang/String;)V", str(""))
	appendText(full, "0123456789abcde")
	bufferCall(t, vm, full, "insert", "(ILjava/lang/String;)Ljava/lang/StringBuffer;", IntValue(0), str("xy"))
	if got := capacity(full); got != 34 {
		t.Fatalf("capacity after an insert past 16 = %d, want 34", got)
	}
}

// The growth stops at the largest int, where the class library's arithmetic
// went negative and was replaced by it.
func TestBufferGrowthStopsAtTheLargestInt(t *testing.T) {
	for _, probe := range []struct {
		capacity int32
		length   int
		want     int32
	}{
		{16, 16, 16},
		{16, 17, 34},
		{16, 100, 100},
		{0, 1, 2},
		{1 << 30, 1<<30 + 1, math.MaxInt32},
		{math.MaxInt32 - 1, math.MaxInt32, math.MaxInt32},
	} {
		if got := bufferGrown(probe.capacity, probe.length); got != probe.want {
			t.Errorf("bufferGrown(%d, %d) = %d, want %d", probe.capacity, probe.length, got, probe.want)
		}
	}
	if got := restoredBufferCapacity(3); got != 16 {
		t.Errorf("restoredBufferCapacity(3) = %d, want 16", got)
	}
	if got := restoredBufferCapacity(40); got != 40 {
		t.Errorf("restoredBufferCapacity(40) = %d, want 40", got)
	}
}

// The four insert forms put in what the matching append adds. The object form
// asks the object's own toString before it checks the offset, as
// insert(offset, String.valueOf(obj)) evaluates; the char-array form checks
// the offset before it reads the array. reverse turns every unit round, a pair
// included, and none of these is StringBuilder's.
func TestStringBufferInsertFormsAndReverse(t *testing.T) {
	vm := New(nil, Options{})
	asked := 0
	if err := vm.DefineClass(ClassDefinition{Name: "test/Named", SuperName: ObjectClass, Access: AccessPublic,
		Methods: []MethodDefinition{
			{Name: "<init>", Descriptor: "()V", Access: AccessPublic, Body: emptyConstructor},
			{Name: "toString", Descriptor: "()Ljava/lang/String;", Access: AccessPublic, Body: func(call *Invocation, _ []Value) (Value, error) {
				asked++
				return call.vm.stringResult("named")
			}},
		}}); err != nil {
		t.Fatal(err)
	}
	named, err := vm.NewObject("test/Named", "()V")
	if err != nil {
		t.Fatal(err)
	}
	buffer := newBuffer(t, vm, "(Ljava/lang/String;)V", ReferenceValue(vm.NewString("..")))
	const self = "Ljava/lang/StringBuffer;"
	bufferCall(t, vm, buffer, "insert", "(IZ)"+self, IntValue(1), IntValue(1))
	bufferCall(t, vm, buffer, "insert", "(IZ)"+self, IntValue(0), IntValue(0))
	bufferCall(t, vm, buffer, "insert", "(IJ)"+self, IntValue(0), LongValue(math.MinInt64))
	bufferCall(t, vm, buffer, "insert", "(ILjava/lang/Object;)"+self, IntValue(0), ReferenceValue(named))
	bufferCall(t, vm, buffer, "insert", "(ILjava/lang/Object;)"+self, IntValue(0), ReferenceValue(nil))
	chars := charArray(t, vm, []uint16{'[', 0xd800, ']'})
	bufferCall(t, vm, buffer, "insert", "(I[C)"+self, IntValue(0), ReferenceValue(chars))
	if got := bufferInt(t, vm, buffer, "charAt", "(I)C", IntValue(1)); got != 0xd800 {
		t.Fatalf("the half of a pair an inserted array held = %#x, want 0xd800", got)
	}
	bufferCall(t, vm, buffer, "setCharAt", "(IC)V", IntValue(1), IntValue('|'))
	if got, want := bufferText(t, vm, buffer), "[|]nullnamed-9223372036854775808false.true."; got != want {
		t.Fatalf("after the inserts = %q, want %q", got, want)
	}
	if asked != 1 {
		t.Fatalf("the object's toString was asked %d times, want once", asked)
	}

	// The object form evaluates the object before refusing the offset.
	if _, err := vm.InvokeVirtual(buffer, "insert", "(ILjava/lang/Object;)"+self, IntValue(-1), ReferenceValue(named)); !vm.IsGuestException(err, "java/lang/StringIndexOutOfBoundsException") {
		t.Fatalf("insert(-1, obj) = %v, want StringIndexOutOfBoundsException", err)
	}
	if asked != 2 {
		t.Fatal("insert(-1, obj) refused the offset before asking the object")
	}
	length := bufferInt(t, vm, buffer, "length", "()I")
	for _, probe := range []struct {
		offset int32
		array  *Object
		want   string
	}{
		{length + 1, nil, "java/lang/StringIndexOutOfBoundsException"},
		{-1, chars, "java/lang/StringIndexOutOfBoundsException"},
		{0, nil, "java/lang/NullPointerException"},
	} {
		if _, err := vm.InvokeVirtual(buffer, "insert", "(I[C)"+self, IntValue(probe.offset), ReferenceValue(probe.array)); !vm.IsGuestException(err, probe.want) {
			t.Errorf("insert(%d, %v) = %v, want %s", probe.offset, probe.array, err, probe.want)
		}
	}
	for _, descriptor := range []string{"(IZ)", "(IJ)"} {
		argument := IntValue(1)
		if descriptor == "(IJ)" {
			argument = LongValue(1)
		}
		if _, err := vm.InvokeVirtual(buffer, "insert", descriptor+self, IntValue(length+1), argument); !vm.IsGuestException(err, "java/lang/StringIndexOutOfBoundsException") {
			t.Errorf("insert%s past the end = %v, want StringIndexOutOfBoundsException", descriptor, err)
		}
	}
	if got, want := bufferText(t, vm, buffer), "[|]nullnamed-9223372036854775808false.true."; got != want {
		t.Fatalf("a refused insert changed the buffer to %q", got)
	}

	pair := newBuffer(t, vm, "()V")
	bufferCall(t, vm, pair, "append", "([C)"+self, ReferenceValue(charArray(t, vm, []uint16{'a', 0xd83d, 0xde00, 'b'})))
	result, err := vm.InvokeVirtual(pair, "reverse", "()"+self)
	if err != nil {
		t.Fatal(err)
	}
	if answer, _ := result.Reference(); answer != pair {
		t.Fatal("reverse did not answer its receiver")
	}
	for index, want := range []int32{'b', 0xde00, 0xd83d, 'a'} {
		if got := bufferInt(t, vm, pair, "charAt", "(I)C", IntValue(int32(index))); got != want {
			t.Fatalf("reversed unit %d = %#x, want %#x", index, got, want)
		}
	}
	empty := newBuffer(t, vm, "()V")
	bufferCall(t, vm, empty, "reverse", "()"+self)
	if got := bufferText(t, vm, empty); got != "" {
		t.Fatalf("an empty buffer reversed = %q", got)
	}

	// StringBuilder shares the buffer and none of these members.
	builder, err := vm.NewObject(StringBuilderClass, "()V")
	if err != nil {
		t.Fatal(err)
	}
	var guest *GuestException
	if _, err := vm.InvokeVirtual(builder, "capacity", "()I"); err == nil || errors.As(err, &guest) {
		t.Fatalf("StringBuilder.capacity() = %v, want no such member", err)
	}
}
