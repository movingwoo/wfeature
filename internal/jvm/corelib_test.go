package jvm

import (
	"errors"
	"math"
	"strings"
	"testing"
)

// A record written through DataOutputStream has to come back byte for byte
// through DataInputStream: this pair is how a title encodes its own save data,
// so a wrong width or a wrong sign is a corrupted save rather than an error.
func TestDataStreamsRoundTripARecord(t *testing.T) {
	vm := New(nil, Options{})
	sink, err := vm.NewObject(ByteArrayOutputStreamClass, "()V")
	if err != nil {
		t.Fatalf("NewObject(ByteArrayOutputStream) error = %v", err)
	}
	output, err := vm.NewObject(DataOutputStreamClass, "(Ljava/io/OutputStream;)V", ReferenceValue(sink))
	if err != nil {
		t.Fatalf("NewObject(DataOutputStream) error = %v", err)
	}

	writes := []struct {
		name       string
		descriptor string
		value      Value
	}{
		{"writeBoolean", "(Z)V", IntValue(1)},
		{"writeByte", "(I)V", IntValue(-3)},
		{"writeShort", "(I)V", IntValue(-2)},
		{"writeChar", "(I)V", IntValue('A')},
		{"writeInt", "(I)V", IntValue(-559038737)},
		{"writeLong", "(J)V", LongValue(-81985529216486896)},
	}
	for _, write := range writes {
		if _, err := vm.InvokeVirtual(output, write.name, write.descriptor, write.value); err != nil {
			t.Fatalf("%s error = %v", write.name, err)
		}
	}
	if _, err := vm.InvokeVirtual(output, "flush", "()V"); err != nil {
		t.Fatalf("flush() error = %v", err)
	}

	encoded, err := vm.InvokeVirtual(sink, "toByteArray", "()[B")
	if err != nil {
		t.Fatalf("toByteArray() error = %v", err)
	}
	buffer, err := encoded.Reference()
	if err != nil {
		t.Fatal(err)
	}
	data, err := ByteArraySnapshot(buffer)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) != 1+1+2+2+4+8 {
		t.Fatalf("encoded %d bytes, want 18", len(data))
	}

	source, err := vm.NewObject(ByteArrayInputStreamClass, "([B)V", ReferenceValue(buffer))
	if err != nil {
		t.Fatalf("NewObject(ByteArrayInputStream) error = %v", err)
	}
	input, err := vm.NewObject(DataInputStreamClass, "(Ljava/io/InputStream;)V", ReferenceValue(source))
	if err != nil {
		t.Fatalf("NewObject(DataInputStream) error = %v", err)
	}
	reads := []struct {
		name       string
		descriptor string
		want       int64
	}{
		{"readBoolean", "()Z", 1},
		{"readByte", "()B", -3},
		{"readShort", "()S", -2},
		{"readUnsignedShort", "()I", 'A'},
		{"readInt", "()I", -559038737},
		{"readLong", "()J", -81985529216486896},
	}
	for _, read := range reads {
		result, err := vm.InvokeVirtual(input, read.name, read.descriptor)
		if err != nil {
			t.Fatalf("%s error = %v", read.name, err)
		}
		var value int64
		if read.descriptor == "()J" {
			value, err = result.Int64()
		} else {
			var narrow int32
			narrow, err = result.Int32()
			value = int64(narrow)
		}
		if err != nil {
			t.Fatal(err)
		}
		if value != read.want {
			t.Errorf("%s = %d, want %d", read.name, value, read.want)
		}
	}
	if _, err := vm.InvokeVirtual(input, "readByte", "()B"); err == nil {
		t.Error("reading past the end of a record succeeded")
	} else if !vm.IsGuestException(err, IOExceptionClass) {
		t.Errorf("reading past the end = %v, want an IOException", err)
	}
}

// available() and skip() are what a game uses to walk its own archive entry
// without reading it, and the byte array stream answers both from what it
// already holds rather than a byte at a time.
func TestByteArrayInputStreamSkipsAndReports(t *testing.T) {
	vm := New(nil, Options{})
	data := NewByteArray([]byte{1, 2, 3, 4, 5})
	stream, err := vm.NewObject(ByteArrayInputStreamClass, "([B)V", ReferenceValue(data))
	if err != nil {
		t.Fatalf("NewObject() error = %v", err)
	}
	assertInt := func(name, descriptor string, want int32, arguments ...Value) {
		t.Helper()
		result, err := vm.InvokeVirtual(stream, name, descriptor, arguments...)
		if err != nil {
			t.Fatalf("%s error = %v", name, err)
		}
		value, err := result.Int32()
		if err != nil {
			t.Fatal(err)
		}
		if value != want {
			t.Fatalf("%s = %d, want %d", name, value, want)
		}
	}
	assertInt("available", "()I", 5)
	assertInt("read", "()I", 1)
	skipped, err := vm.InvokeVirtual(stream, "skip", "(J)J", LongValue(2))
	if err != nil {
		t.Fatalf("skip() error = %v", err)
	}
	if count, _ := skipped.Int64(); count != 2 {
		t.Fatalf("skip(2) = %d, want 2", count)
	}
	assertInt("read", "()I", 4)
	assertInt("available", "()I", 1)
	// A skip past the end stops at the end rather than reporting bytes that
	// were never there.
	skipped, err = vm.InvokeVirtual(stream, "skip", "(J)J", LongValue(64))
	if err != nil {
		t.Fatalf("skip() error = %v", err)
	}
	if count, _ := skipped.Int64(); count != 1 {
		t.Fatalf("skip(64) at one byte left = %d, want 1", count)
	}
	assertInt("read", "()I", -1)
}

// The generator's sequence is the standard's, checked against values a JDK
// produces for the same seeds. A game that seeds it and expects a particular
// series — a map layout, a shuffled deck — is a different game otherwise.
func TestRandomMatchesTheStandardSequence(t *testing.T) {
	vm := New(nil, Options{})
	random, err := vm.NewObject(RandomClass, "(J)V", LongValue(42))
	if err != nil {
		t.Fatalf("NewObject() error = %v", err)
	}
	first, err := vm.InvokeVirtual(random, "nextInt", "()I")
	if err != nil {
		t.Fatalf("nextInt() error = %v", err)
	}
	if value, _ := first.Int32(); value != -1170105035 {
		t.Errorf("first nextInt() = %d, want -1170105035", value)
	}
	second, err := vm.InvokeVirtual(random, "nextInt", "()I")
	if err != nil {
		t.Fatalf("nextInt() error = %v", err)
	}
	if value, _ := second.Int32(); value != 234785527 {
		t.Errorf("second nextInt() = %d, want 234785527", value)
	}
	bounded, err := vm.InvokeVirtual(random, "nextInt", "(I)I", IntValue(100))
	if err != nil {
		t.Fatalf("nextInt(100) error = %v", err)
	}
	if value, _ := bounded.Int32(); value != 48 {
		t.Errorf("nextInt(100) = %d, want 48", value)
	}
	long, err := vm.InvokeVirtual(random, "nextLong", "()J")
	if err != nil {
		t.Fatalf("nextLong() error = %v", err)
	}
	if value, _ := long.Int64(); value != 884324181205335268 {
		t.Errorf("nextLong() = %d, want 884324181205335268", value)
	}

	// A power-of-two bound takes the other branch of nextInt.
	power, err := vm.NewObject(RandomClass, "(J)V", LongValue(7))
	if err != nil {
		t.Fatalf("NewObject() error = %v", err)
	}
	result, err := vm.InvokeVirtual(power, "nextInt", "(I)I", IntValue(64))
	if err != nil {
		t.Fatalf("nextInt(64) error = %v", err)
	}
	if value, _ := result.Int32(); value != 46 {
		t.Errorf("nextInt(64) = %d, want 46", value)
	}
	if _, err := vm.InvokeVirtual(power, "nextInt", "(I)I", IntValue(0)); err == nil {
		t.Error("nextInt(0) succeeded")
	}
}

// A vector that is full grows by exactly its increment, and doubles only when
// the increment is zero. One title walks a vector with capacity() as the bound
// and elementAt as the body, so a vector that grew by more than it was told
// reads past its elements.
func TestVectorGrowsByItsIncrement(t *testing.T) {
	vm := New(nil, Options{})
	vector, err := vm.NewObject(VectorClass, "(II)V", IntValue(1), IntValue(4))
	if err != nil {
		t.Fatalf("NewObject() error = %v", err)
	}
	capacity := func() int32 {
		t.Helper()
		result, err := vm.InvokeVirtual(vector, "capacity", "()I")
		if err != nil {
			t.Fatalf("capacity() error = %v", err)
		}
		value, _ := result.Int32()
		return value
	}
	if got := capacity(); got != 1 {
		t.Fatalf("capacity() = %d, want 1", got)
	}
	for index := 0; index < 2; index++ {
		if _, err := vm.InvokeVirtual(vector, "addElement", "(Ljava/lang/Object;)V", ReferenceValue(vm.NewString("x"))); err != nil {
			t.Fatalf("addElement() error = %v", err)
		}
	}
	if got := capacity(); got != 5 {
		t.Fatalf("capacity() after growing by an increment of 4 = %d, want 5", got)
	}
}

// The collections answer with the elements they were given, remove them again,
// and report an index nobody filled as out of bounds rather than as null.
func TestVectorAndHashtableKeepTheirContents(t *testing.T) {
	vm := New(nil, Options{})
	vector, err := vm.NewObject(VectorClass, "()V")
	if err != nil {
		t.Fatalf("NewObject() error = %v", err)
	}
	first := vm.NewString("first")
	second := vm.NewString("second")
	for _, element := range []*Object{second, first} {
		if _, err := vm.InvokeVirtual(vector, "addElement", "(Ljava/lang/Object;)V", ReferenceValue(element)); err != nil {
			t.Fatalf("addElement() error = %v", err)
		}
	}
	if _, err := vm.InvokeVirtual(vector, "insertElementAt", "(Ljava/lang/Object;I)V", ReferenceValue(first), IntValue(0)); err != nil {
		t.Fatalf("insertElementAt() error = %v", err)
	}
	assertSize := func(want int32) {
		t.Helper()
		result, err := vm.InvokeVirtual(vector, "size", "()I")
		if err != nil {
			t.Fatalf("size() error = %v", err)
		}
		if value, _ := result.Int32(); value != want {
			t.Fatalf("size() = %d, want %d", value, want)
		}
	}
	assertSize(3)
	found, err := vm.InvokeVirtual(vector, "indexOf", "(Ljava/lang/Object;)I", ReferenceValue(vm.NewString("second")))
	if err != nil {
		t.Fatalf("indexOf() error = %v", err)
	}
	if value, _ := found.Int32(); value != 1 {
		t.Errorf("indexOf(second) = %d, want 1 by equality rather than identity", value)
	}
	removed, err := vm.InvokeVirtual(vector, "removeElement", "(Ljava/lang/Object;)Z", ReferenceValue(vm.NewString("first")))
	if err != nil {
		t.Fatalf("removeElement() error = %v", err)
	}
	if value, _ := removed.Int32(); value != 1 {
		t.Error("removeElement() did not remove a present element")
	}
	assertSize(2)
	if _, err := vm.InvokeVirtual(vector, "elementAt", "(I)Ljava/lang/Object;", IntValue(5)); err == nil {
		t.Error("elementAt() past the end succeeded")
	} else if !strings.Contains(err.Error(), "5 of 2") {
		t.Errorf("elementAt(5) error = %v, want the index and the size", err)
	}

	table, err := vm.NewObject(HashtableClass, "()V")
	if err != nil {
		t.Fatalf("NewObject() error = %v", err)
	}
	// More entries than the initial capacity, so the growth path runs.
	for index := 0; index < 12; index++ {
		key := vm.NewString(strings.Repeat("k", index+1))
		value := vm.NewString(strings.Repeat("v", index+1))
		if _, err := vm.InvokeVirtual(table, "put", "(Ljava/lang/Object;Ljava/lang/Object;)Ljava/lang/Object;",
			ReferenceValue(key), ReferenceValue(value)); err != nil {
			t.Fatalf("put() error = %v", err)
		}
	}
	value, err := vm.InvokeVirtual(table, "get", "(Ljava/lang/Object;)Ljava/lang/Object;", ReferenceValue(vm.NewString("kkkkkkkkkkkk")))
	if err != nil {
		t.Fatalf("get() error = %v", err)
	}
	stored, err := value.Reference()
	if err != nil {
		t.Fatal(err)
	}
	if text, _ := StringText(stored); text != "vvvvvvvvvvvv" {
		t.Errorf("get() = %q, want the twelfth value", text)
	}
	if _, err := vm.InvokeVirtual(table, "remove", "(Ljava/lang/Object;)Ljava/lang/Object;", ReferenceValue(vm.NewString("k"))); err != nil {
		t.Fatalf("remove() error = %v", err)
	}
	present, err := vm.InvokeVirtual(table, "containsKey", "(Ljava/lang/Object;)Z", ReferenceValue(vm.NewString("k")))
	if err != nil {
		t.Fatalf("containsKey() error = %v", err)
	}
	if found, _ := present.Int32(); found != 0 {
		t.Error("a removed key is still present")
	}
}

// containerCall invokes a collection method and answers what it answered.
func containerCall(t *testing.T, vm *VM, receiver *Object, name, descriptor string, arguments ...Value) Value {
	t.Helper()
	result, err := vm.InvokeVirtual(receiver, name, descriptor, arguments...)
	if err != nil {
		t.Fatalf("%s.%s%s error = %v", receiver.ClassName, name, descriptor, err)
	}
	return result
}

func textOfContainer(t *testing.T, vm *VM, receiver *Object) string {
	t.Helper()
	object, err := containerCall(t, vm, receiver, "toString", "()Ljava/lang/String;").Reference()
	if err != nil {
		t.Fatal(err)
	}
	text, _ := StringText(object)
	return text
}

func newVectorOf(t *testing.T, vm *VM, elements ...*Object) *Object {
	t.Helper()
	vector, err := vm.NewObject(VectorClass, "()V")
	if err != nil {
		t.Fatal(err)
	}
	for _, element := range elements {
		containerCall(t, vm, vector, "addElement", "(Ljava/lang/Object;)V", ReferenceValue(element))
	}
	return vector
}

func guestExceptionClass(err error) string {
	var guest *GuestException
	if errors.As(err, &guest) && guest.Object != nil {
		return guest.Object.ClassName
	}
	return ""
}

// The searches from an index ask the argument, as the class library's
// `elem.equals(elementData[i])` does, a null element included, and find a null
// by identity without asking anything. A negative start to indexOf is the
// array failure the library's loop runs into before it compares, and a start
// to lastIndexOf at or past the size is IndexOutOfBoundsException itself.
func TestVectorSearchesFromAnIndexAskTheArgument(t *testing.T) {
	vm := New(nil, Options{})
	var asked []*Object
	if err := vm.DefineClass(ClassDefinition{Name: "test/Wanted", SuperName: ObjectClass, Access: AccessPublic,
		Methods: []MethodDefinition{
			{Name: "<init>", Descriptor: "()V", Access: AccessPublic, Body: emptyConstructor},
			{Name: "equals", Descriptor: "(Ljava/lang/Object;)Z", Access: AccessPublic, Body: func(_ *Invocation, arguments []Value) (Value, error) {
				receiver, _ := arguments[0].Reference()
				other, _ := arguments[1].Reference()
				if receiver.ClassName != "test/Wanted" {
					t.Error("equals was asked of an element rather than of the argument")
				}
				asked = append(asked, other)
				text, _ := StringText(other)
				return booleanValue(text == "b"), nil
			}},
		}}); err != nil {
		t.Fatal(err)
	}
	wanted, err := vm.NewObject("test/Wanted", "()V")
	if err != nil {
		t.Fatal(err)
	}
	a, b, later := vm.NewString("a"), vm.NewString("b"), vm.NewString("b")
	vector := newVectorOf(t, vm, a, nil, b, later)
	search := func(name string, from int32) (int32, error) {
		result, err := vm.InvokeVirtual(vector, name, "(Ljava/lang/Object;I)I", ReferenceValue(wanted), IntValue(from))
		if err != nil {
			return 0, err
		}
		return result.Int32()
	}
	if got, err := search("indexOf", 1); err != nil || got != 2 || len(asked) != 2 || asked[0] != nil || asked[1] != b {
		t.Fatalf("indexOf(wanted, 1) = %d, %v, asking about %v", got, err, asked)
	}
	asked = nil
	if got, err := search("lastIndexOf", 2); err != nil || got != 2 || len(asked) != 1 || asked[0] != b {
		t.Fatalf("lastIndexOf(wanted, 2) = %d, %v, asking about %v", got, err, asked)
	}
	asked = nil
	if got, err := search("lastIndexOf", 1); err != nil || got != -1 || len(asked) != 2 || asked[0] != nil || asked[1] != a {
		t.Fatalf("lastIndexOf(wanted, 1) = %d, %v, asking about %v", got, err, asked)
	}
	for _, probe := range []struct {
		name string
		from int32
		want int32
	}{{"indexOf", 4, -1}, {"indexOf", math.MaxInt32, -1}, {"lastIndexOf", -1, -1}, {"lastIndexOf", math.MinInt32, -1}} {
		asked = nil
		if got, err := search(probe.name, probe.from); err != nil || got != probe.want || len(asked) != 0 {
			t.Errorf("%s(wanted, %d) = %d, %v, asking about %v", probe.name, probe.from, got, err, asked)
		}
	}
	asked = nil
	if _, err := search("indexOf", -1); guestExceptionClass(err) != "java/lang/ArrayIndexOutOfBoundsException" || len(asked) != 0 {
		t.Errorf("indexOf(wanted, -1) = %v, asking about %v, want ArrayIndexOutOfBoundsException first", err, asked)
	}
	if _, err := search("lastIndexOf", 4); guestExceptionClass(err) != "java/lang/IndexOutOfBoundsException" {
		t.Errorf("lastIndexOf(wanted, 4) of 4 = %v, want IndexOutOfBoundsException", err)
	}
	for _, probe := range []struct {
		name string
		from int32
		want int32
	}{{"indexOf", 0, 1}, {"indexOf", 2, -1}, {"lastIndexOf", 3, 1}, {"lastIndexOf", 0, -1}} {
		result, err := vm.InvokeVirtual(vector, probe.name, "(Ljava/lang/Object;I)I", ReferenceValue(nil), IntValue(probe.from))
		if got, _ := result.Int32(); err != nil || got != probe.want {
			t.Errorf("%s(null, %d) = %d, %v, want %d", probe.name, probe.from, got, err, probe.want)
		}
	}
}

// A vector and a table write each element's own text — a String as itself,
// null as "null", anything else through its own toString, "null" when that
// answers null — in the order they enumerate: `[a, b]`, `{k=v, k2=v2}`, a
// key's text asked before its value's. String.valueOf and an append of the
// container answer the same, since they ask the container's toString now that
// it has one.
func TestContainersWriteEachElementsOwnText(t *testing.T) {
	vm := New(nil, Options{})
	var told []string
	if err := vm.DefineClass(ClassDefinition{Name: "test/Told", SuperName: ObjectClass, Access: AccessPublic,
		Methods: []MethodDefinition{
			{Name: "<init>", Descriptor: "()V", Access: AccessPublic, Body: emptyConstructor},
			{Name: "toString", Descriptor: "()Ljava/lang/String;", Access: AccessPublic, Body: func(call *Invocation, arguments []Value) (Value, error) {
				receiver, _ := arguments[0].Reference()
				label, _ := receiver.Native.(string)
				told = append(told, label)
				if label == "silent" {
					return ReferenceValue(nil), nil
				}
				return call.vm.stringResult("<" + label + ">")
			}},
		}}); err != nil {
		t.Fatal(err)
	}
	newTold := func(label string) *Object {
		object, err := vm.NewObject("test/Told", "()V")
		if err != nil {
			t.Fatal(err)
		}
		object.Native = label
		return object
	}
	five, err := vm.NewObject(IntegerClass, "(I)V", IntValue(5))
	if err != nil {
		t.Fatal(err)
	}
	buffer, err := vm.NewObject(StringBufferClass, "(Ljava/lang/String;)V", ReferenceValue(vm.NewString("sb")))
	if err != nil {
		t.Fatal(err)
	}
	inner := newVectorOf(t, vm, vm.NewString("x"))
	vector := newVectorOf(t, vm, vm.NewString("a"), five, nil, buffer, newTold("silent"), inner, newTold("t"))
	if got, want := textOfContainer(t, vm, vector), "[a, 5, null, sb, null, [x], <t>]"; got != want {
		t.Fatalf("Vector.toString() = %q, want %q", got, want)
	}
	for _, form := range []string{"valueOf", "append"} {
		var result Value
		if form == "valueOf" {
			result, err = vm.InvokeStatic(StringClass, "valueOf", "(Ljava/lang/Object;)Ljava/lang/String;", ReferenceValue(inner))
		} else {
			sink, newErr := vm.NewObject(StringBufferClass, "()V")
			if newErr != nil {
				t.Fatal(newErr)
			}
			containerCall(t, vm, sink, "append", "(Ljava/lang/Object;)Ljava/lang/StringBuffer;", ReferenceValue(inner))
			result, err = vm.InvokeVirtual(sink, "toString", "()Ljava/lang/String;")
		}
		object, _ := result.Reference()
		if got, _ := StringText(object); err != nil || got != "[x]" {
			t.Errorf("%s of a vector = %q, %v, want its own text", form, got, err)
		}
	}
	stack, err := vm.NewObject(StackClass, "()V")
	if err != nil {
		t.Fatal(err)
	}
	containerCall(t, vm, stack, "push", "(Ljava/lang/Object;)Ljava/lang/Object;", ReferenceValue(vm.NewString("p")))
	if got := textOfContainer(t, vm, stack); got != "[p]" {
		t.Fatalf("Stack.toString() = %q, want the vector's text", got)
	}
	if got := textOfContainer(t, vm, newVectorOf(t, vm)); got != "[]" {
		t.Fatalf("an empty vector = %q", got)
	}

	table, err := vm.NewObject(HashtableClass, "()V")
	if err != nil {
		t.Fatal(err)
	}
	put := func(key, value *Object) {
		t.Helper()
		containerCall(t, vm, table, "put", "(Ljava/lang/Object;Ljava/lang/Object;)Ljava/lang/Object;", ReferenceValue(key), ReferenceValue(value))
	}
	if got := textOfContainer(t, vm, table); got != "{}" {
		t.Fatalf("an empty table = %q", got)
	}
	put(vm.NewString("k1"), vm.NewString("v1"))
	put(five, inner)
	put(newTold("key"), newTold("value"))
	containerCall(t, vm, table, "remove", "(Ljava/lang/Object;)Ljava/lang/Object;", ReferenceValue(vm.NewString("k1")))
	told = nil
	if got, want := textOfContainer(t, vm, table), "{5=[x], <key>=<value>}"; got != want {
		t.Fatalf("Hashtable.toString() = %q, want %q", got, want)
	}
	if len(told) != 2 || told[0] != "key" || told[1] != "value" {
		t.Fatalf("the entry's texts were asked in the order %v, want the key's first", told)
	}
}

// A vector that holds itself, or two tables that hold each other, ask their
// own toString for ever. The class library would run out of stack; here the
// nesting is counted to 256 on the execution, as LGT counts it, and the call
// past that ends with an error rather than with the Go stack running out under
// the whole process. Nesting short of it answers, and the count is given back
// as each call returns, so the same execution formats the next one.
func TestContainersThatHoldThemselvesStopTheirToString(t *testing.T) {
	vm := New(nil, Options{})
	looped := newVectorOf(t, vm, vm.NewString("a"))
	containerCall(t, vm, looped, "addElement", "(Ljava/lang/Object;)V", ReferenceValue(looped))
	first, err := vm.NewObject(HashtableClass, "()V")
	if err != nil {
		t.Fatal(err)
	}
	second, err := vm.NewObject(HashtableClass, "()V")
	if err != nil {
		t.Fatal(err)
	}
	key := ReferenceValue(vm.NewString("k"))
	containerCall(t, vm, first, "put", "(Ljava/lang/Object;Ljava/lang/Object;)Ljava/lang/Object;", key, ReferenceValue(second))
	containerCall(t, vm, second, "put", "(Ljava/lang/Object;Ljava/lang/Object;)Ljava/lang/Object;", key, ReferenceValue(first))
	for _, container := range []*Object{looped, first} {
		_, err := vm.InvokeVirtual(container, "toString", "()Ljava/lang/String;")
		if err == nil || guestExceptionClass(err) != "" || !strings.Contains(err.Error(), "nested 256 calls deep") {
			t.Errorf("toString of a %s that holds itself = %v, want the nesting stopped", container.ClassName, err)
		}
	}

	// 256 vectors one inside the next answer; 257 do not.
	chain := newVectorOf(t, vm, vm.NewString("x"))
	for depth := 1; depth < maxContainerTextNesting; depth++ {
		chain = newVectorOf(t, vm, chain)
	}
	want := strings.Repeat("[", maxContainerTextNesting) + "x" + strings.Repeat("]", maxContainerTextNesting)
	if got := textOfContainer(t, vm, chain); got != want {
		t.Fatalf("toString of %d nested vectors = %.40q..., want them all", maxContainerTextNesting, got)
	}
	if _, err := vm.InvokeVirtual(newVectorOf(t, vm, chain), "toString", "()Ljava/lang/String;"); err == nil || !strings.Contains(err.Error(), "nested") {
		t.Fatalf("toString of %d nested vectors = %v, want the nesting stopped", maxContainerTextNesting+1, err)
	}

	plain := newVectorOf(t, vm, vm.NewString("y"))
	if err := vm.DefineClass(ClassDefinition{Name: "test/Twice", SuperName: ObjectClass, Access: AccessPublic,
		Methods: []MethodDefinition{{Name: "both", Descriptor: "()Ljava/lang/String;", Access: AccessPublic | AccessStatic, Body: func(call *Invocation, _ []Value) (Value, error) {
			if _, err := call.InvokeVirtual(looped, "toString", "()Ljava/lang/String;"); err == nil {
				t.Error("the looped vector answered")
			}
			if call.state.containerTexts != 0 {
				t.Errorf("the refusal left %d containers counted on the execution", call.state.containerTexts)
			}
			return call.InvokeVirtual(plain, "toString", "()Ljava/lang/String;")
		}}}}); err != nil {
		t.Fatal(err)
	}
	result, err := vm.InvokeStatic("test/Twice", "both", "()Ljava/lang/String;")
	if err != nil {
		t.Fatal(err)
	}
	if object, _ := result.Reference(); object == nil {
		t.Fatal("the execution could not format after a refusal")
	} else if text, _ := StringText(object); text != "[y]" {
		t.Fatalf("after a refusal the same execution answered %q", text)
	}
}

// A container's text is a String, and a String is no longer than an array may
// be: past that the text is the OutOfMemoryError the class library's buffer
// would have raised growing to hold it.
func TestContainerTextIsBoundedAsAStringIs(t *testing.T) {
	vm := New(nil, Options{MaxArrayLength: 16})
	word := vm.NewString("0123456789")
	if got := textOfContainer(t, vm, newVectorOf(t, vm, word)); got != "[0123456789]" {
		t.Fatalf("a short vector = %q", got)
	}
	if _, err := vm.InvokeVirtual(newVectorOf(t, vm, word, word), "toString", "()Ljava/lang/String;"); !vm.IsGuestException(err, "java/lang/OutOfMemoryError") {
		t.Fatalf("a vector whose text is longer than an array = %v, want OutOfMemoryError", err)
	}
}

// contains asks each value the table holds whether it equals the argument —
// the stored value asked, as the class library asks it — in the order
// elements() hands them out, and stops at the first that does. A null argument
// is NullPointerException before anything is asked.
func TestHashtableContainsAsksEachValueInOrder(t *testing.T) {
	vm := New(nil, Options{})
	var asked []string
	var argument *Object
	if err := vm.DefineClass(ClassDefinition{Name: "test/Stored", SuperName: ObjectClass, Access: AccessPublic,
		Methods: []MethodDefinition{
			{Name: "<init>", Descriptor: "()V", Access: AccessPublic, Body: emptyConstructor},
			{Name: "equals", Descriptor: "(Ljava/lang/Object;)Z", Access: AccessPublic, Body: func(_ *Invocation, arguments []Value) (Value, error) {
				receiver, _ := arguments[0].Reference()
				other, _ := arguments[1].Reference()
				label, _ := receiver.Native.(string)
				asked = append(asked, label)
				if other != argument {
					t.Error("a value was asked about something other than the argument")
				}
				return booleanValue(label == "2"), nil
			}},
		}}); err != nil {
		t.Fatal(err)
	}
	table, err := vm.NewObject(HashtableClass, "()V")
	if err != nil {
		t.Fatal(err)
	}
	for index, label := range []string{"1", "2", "3"} {
		value, err := vm.NewObject("test/Stored", "()V")
		if err != nil {
			t.Fatal(err)
		}
		value.Native = label
		containerCall(t, vm, table, "put", "(Ljava/lang/Object;Ljava/lang/Object;)Ljava/lang/Object;",
			ReferenceValue(vm.NewString(strings.Repeat("k", index+1))), ReferenceValue(value))
	}
	argument = vm.NewString("anything")
	if got := containerCall(t, vm, table, "contains", "(Ljava/lang/Object;)Z", ReferenceValue(argument)); got != IntValue(1) {
		t.Fatalf("contains = %v, want true", got)
	}
	if len(asked) != 2 || asked[0] != "1" || asked[1] != "2" {
		t.Fatalf("the values were asked in the order %v, want 1 then 2 and no further", asked)
	}
	asked = nil
	if _, err := vm.InvokeVirtual(table, "contains", "(Ljava/lang/Object;)Z", ReferenceValue(nil)); guestExceptionClass(err) != "java/lang/NullPointerException" || len(asked) != 0 {
		t.Fatalf("contains(null) = %v, asking %v, want NullPointerException first", err, asked)
	}
	plain, err := vm.NewObject(HashtableClass, "()V")
	if err != nil {
		t.Fatal(err)
	}
	containerCall(t, vm, plain, "put", "(Ljava/lang/Object;Ljava/lang/Object;)Ljava/lang/Object;",
		ReferenceValue(vm.NewString("key")), ReferenceValue(vm.NewString("value")))
	for text, want := range map[string]int32{"value": 1, "key": 0} {
		if got := containerCall(t, vm, plain, "contains", "(Ljava/lang/Object;)Z", ReferenceValue(vm.NewString(text))); got != IntValue(want) {
			t.Errorf("contains(%q) = %v, want %d: it searches values, not keys", text, got, want)
		}
	}
}

// rehash, which a subclass may call, moves the contents into arrays of 2n + 1
// — the growth put makes on its own — and moves no entry: keys() and
// elements() enumerate as they did, and every lookup answers as it did.
func TestHashtableRehashGrowsWithoutMovingAnEntry(t *testing.T) {
	vm := New(nil, Options{})
	table, err := vm.NewObject(HashtableClass, "()V")
	if err != nil {
		t.Fatal(err)
	}
	for _, pair := range [][2]string{{"a", "1"}, {"b", "2"}, {"c", "3"}} {
		containerCall(t, vm, table, "put", "(Ljava/lang/Object;Ljava/lang/Object;)Ljava/lang/Object;",
			ReferenceValue(vm.NewString(pair[0])), ReferenceValue(vm.NewString(pair[1])))
	}
	length := func() int {
		t.Helper()
		keys, _, _, err := hashtableState(vm, table)
		if err != nil {
			t.Fatal(err)
		}
		return keys.Length()
	}
	before := length()
	for _, want := range []int{2*before + 1, 2*(2*before+1) + 1} {
		containerCall(t, vm, table, "rehash", "()V")
		if got := length(); got != want {
			t.Fatalf("after rehash the arrays are %d long, want %d", got, want)
		}
		if got := textOfContainer(t, vm, table); got != "{a=1, b=2, c=3}" {
			t.Fatalf("after rehash the table is %q", got)
		}
		value, _ := containerCall(t, vm, table, "get", "(Ljava/lang/Object;)Ljava/lang/Object;", ReferenceValue(vm.NewString("b"))).Reference()
		if text, _ := StringText(value); text != "2" {
			t.Fatalf("get(b) after rehash = %q", text)
		}
	}
}

// System.out is an object of the right class rather than a Go-side singleton,
// because a game may hand it to anything that takes an OutputStream.
func TestSystemPublishesItsStreams(t *testing.T) {
	printed := make([]string, 0, 2)
	vm := New(nil, Options{})
	if err := vm.RegisterNative(PrintStreamClass, "emit", "(ILjava/lang/String;)V", func(_ *VM, arguments []Value) (Value, error) {
		text, err := nativeString(arguments, 1)
		if err != nil {
			return VoidValue(), err
		}
		printed = append(printed, text)
		return VoidValue(), nil
	}); err != nil {
		t.Fatalf("RegisterNative() error = %v", err)
	}

	value, err := vm.StaticField(SystemClass, "out", "Ljava/io/PrintStream;")
	if err != nil {
		t.Fatalf("StaticField() error = %v", err)
	}
	stream, err := value.Reference()
	if err != nil {
		t.Fatal(err)
	}
	if stream == nil || stream.ClassName != PrintStreamClass {
		t.Fatalf("System.out = %v, want a PrintStream", stream)
	}
	if !vm.IsInstance(stream, OutputStreamClass) {
		t.Error("System.out is not an OutputStream")
	}
	if _, err := vm.InvokeVirtual(stream, "println", "(Ljava/lang/String;)V", ReferenceValue(vm.NewString("hello"))); err != nil {
		t.Fatalf("println() error = %v", err)
	}
	if _, err := vm.InvokeVirtual(stream, "print", "(Z)V", IntValue(1)); err != nil {
		t.Fatalf("print(boolean) error = %v", err)
	}
	if _, err := vm.InvokeVirtual(stream, "print", "(C)V", IntValue('!')); err != nil {
		t.Fatalf("print(char) error = %v", err)
	}
	want := []string{"hello", "true", "!"}
	if len(printed) != len(want) {
		t.Fatalf("printed %q, want %q", printed, want)
	}
	for index, text := range want {
		if printed[index] != text {
			t.Errorf("printed[%d] = %q, want %q", index, printed[index], text)
		}
	}
}

// The inherited InputStream.read(byte[], int, int) fills the caller's array by
// calling read() one byte at a time, and what it stores has to be the signed
// byte the array holds: a game that reads back 255 where it wrote -1 decodes a
// different number. The stream here implements only the abstract read(), which
// is what makes the inherited loop run.
func TestInheritedRangedReadStoresSignedBytes(t *testing.T) {
	vm := New(nil, Options{})
	source := []byte{0x00, 0x7f, 0x80, 0xff}
	position := 0
	err := vm.DefineClass(ClassDefinition{
		Name:      "test/CountingStream",
		SuperName: InputStreamClass,
		Access:    AccessPublic,
		Methods: []MethodDefinition{
			{Name: "<init>", Descriptor: "()V", Access: AccessPublic, Body: emptyConstructor},
			{Name: "read", Descriptor: "()I", Access: AccessPublic, Body: func(*Invocation, []Value) (Value, error) {
				if position >= len(source) {
					return IntValue(-1), nil
				}
				value := source[position]
				position++
				return IntValue(int32(value)), nil
			}},
		},
	})
	if err != nil {
		t.Fatalf("DefineClass() error = %v", err)
	}
	stream, err := vm.NewObject("test/CountingStream", "()V")
	if err != nil {
		t.Fatalf("NewObject() error = %v", err)
	}

	buffer := NewByteArray(make([]byte, 6))
	result, err := vm.InvokeVirtual(stream, "read", "([BII)I", ReferenceValue(buffer), IntValue(1), IntValue(5))
	if err != nil {
		t.Fatalf("read() error = %v", err)
	}
	if count, _ := result.Int32(); count != 4 {
		t.Fatalf("read() = %d, want the 4 bytes the stream had", count)
	}
	_, values, err := ArraySnapshot(buffer)
	if err != nil {
		t.Fatal(err)
	}
	want := []int32{0, 0, 127, -128, -1, 0}
	for index, expected := range want {
		got, err := values[index].Int32()
		if err != nil {
			t.Fatal(err)
		}
		if got != expected {
			t.Errorf("buffer[%d] = %d, want %d", index, got, expected)
		}
	}

	// The stream is spent, so the next read reports the end rather than zero
	// bytes.
	empty, err := vm.InvokeVirtual(stream, "read", "([B)I", ReferenceValue(buffer))
	if err != nil {
		t.Fatalf("read(byte[]) error = %v", err)
	}
	if count, _ := empty.Int32(); count != -1 {
		t.Errorf("read() at the end = %d, want -1", count)
	}
}

// A UI form's attributes are parsed one class per attribute type — a
// coordinate through java/lang/Short, a flag through java/lang/Byte — so the
// third boxed number has to be there and has to truncate like its own width.
func TestShortParsesAndTruncatesToItsOwnWidth(t *testing.T) {
	vm := New(nil, Options{})
	value, err := vm.InvokeStatic(ShortClass, "parseShort", "(Ljava/lang/String;)S", ReferenceValue(vm.NewString("-32768")))
	if err != nil {
		t.Fatalf("Short.parseShort error = %v", err)
	}
	if number, err := value.Int32(); err != nil || number != -32768 {
		t.Fatalf("Short.parseShort(\"-32768\") = %v/%v, want -32768", number, err)
	}
	if _, err := vm.InvokeStatic(ShortClass, "parseShort", "(Ljava/lang/String;)S", ReferenceValue(vm.NewString("32768"))); err == nil {
		t.Fatal("Short.parseShort(\"32768\") accepted a value one past the width")
	}
	boxed, err := vm.NewObject(ShortClass, "(S)V", IntValue(-1))
	if err != nil {
		t.Fatalf("NewObject(Short) error = %v", err)
	}
	text, err := vm.InvokeVirtual(boxed, "toString", "()Ljava/lang/String;")
	if err != nil {
		t.Fatalf("Short.toString error = %v", err)
	}
	reference, err := text.Reference()
	if err != nil {
		t.Fatalf("Short.toString reference error = %v", err)
	}
	if content, ok := StringText(reference); !ok || content != "-1" {
		t.Fatalf("Short.toString() = %q/%v, want \"-1\"", content, ok)
	}
}

// String.valueOf(char[]) is the whole-array half of the pair whose ranged
// form was already here. A guest that builds a string from a character buffer
// calls whichever one it was compiled against.
func TestStringValueOfTakesAWholeCharacterArray(t *testing.T) {
	vm := New(nil, Options{})
	characters, err := vm.InvokeVirtual(vm.NewString("WIPI"), "toCharArray", "()[C")
	if err != nil {
		t.Fatalf("String.toCharArray error = %v", err)
	}
	value, err := vm.InvokeStatic(StringClass, "valueOf", "([C)Ljava/lang/String;", characters)
	if err != nil {
		t.Fatalf("String.valueOf([C) error = %v", err)
	}
	reference, err := value.Reference()
	if err != nil {
		t.Fatalf("String.valueOf([C) reference error = %v", err)
	}
	if content, ok := StringText(reference); !ok || content != "WIPI" {
		t.Fatalf("String.valueOf([C) = %q/%v, want \"WIPI\"", content, ok)
	}
}

// CLDC 1.1's Math is the integer half plus a floating-point one, and a title
// that scales a sprite or turns a heading into a step asks for the second.
// The cases worth naming are the ones a comparison would get wrong: a NaN
// poisons max and min rather than losing to a number, and the two zeroes are
// ordered.
func TestMathAnswersTheFloatingPointHalfOfCLDC(t *testing.T) {
	vm := New(nil, Options{})
	call := func(name, descriptor string, arguments ...Value) float64 {
		t.Helper()
		value, err := vm.InvokeStatic(MathClass, name, descriptor, arguments...)
		if err != nil {
			t.Fatalf("Math.%s%s error = %v", name, descriptor, err)
		}
		number, err := value.Float64()
		if err != nil {
			t.Fatalf("Math.%s%s answered %v", name, descriptor, err)
		}
		return number
	}

	for _, probe := range []struct {
		name string
		in   float64
		want float64
	}{
		{"sqrt", 144, 12},
		{"ceil", -0.5, 0}, // a negative zero, which compares equal to zero
		{"ceil", 1.25, 2},
		{"floor", -1.25, -2},
		{"sin", 0, 0},
		{"cos", 0, 1},
		{"tan", 0, 0},
		{"toDegrees", math.Pi, 180},
		{"toRadians", 180, math.Pi},
	} {
		if got := call(probe.name, "(D)D", DoubleValue(probe.in)); got != probe.want {
			t.Errorf("Math.%s(%v) = %v, want %v", probe.name, probe.in, got, probe.want)
		}
	}

	if got := call("max", "(DD)D", DoubleValue(math.NaN()), DoubleValue(1)); !math.IsNaN(got) {
		t.Errorf("Math.max(NaN, 1) = %v, want NaN", got)
	}
	if got := call("min", "(DD)D", DoubleValue(1), DoubleValue(math.NaN())); !math.IsNaN(got) {
		t.Errorf("Math.min(1, NaN) = %v, want NaN", got)
	}
	// The zeroes are ordered, so max answers the positive one and min the
	// negative one — which a plain `<` cannot tell apart.
	if got := call("max", "(DD)D", DoubleValue(math.Copysign(0, -1)), DoubleValue(0)); math.Signbit(got) {
		t.Error("Math.max(-0.0, 0.0) answered the negative zero")
	}
	if got := call("min", "(DD)D", DoubleValue(0), DoubleValue(math.Copysign(0, -1))); !math.Signbit(got) {
		t.Error("Math.min(0.0, -0.0) answered the positive zero")
	}

	value, err := vm.InvokeStatic(MathClass, "max", "(FF)F", FloatValue(1.5), FloatValue(-2.5))
	if err != nil {
		t.Fatalf("Math.max(FF)F error = %v", err)
	}
	if got, err := value.Float32(); err != nil || got != 1.5 {
		t.Fatalf("Math.max(1.5f, -2.5f) = %v/%v, want 1.5", got, err)
	}
	value, err = vm.InvokeStatic(MathClass, "min", "(FF)F", FloatValue(1.5), FloatValue(-2.5))
	if err != nil {
		t.Fatalf("Math.min(FF)F error = %v", err)
	}
	if got, err := value.Float32(); err != nil || got != -2.5 {
		t.Fatalf("Math.min(1.5f, -2.5f) = %v/%v, want -2.5", got, err)
	}
}

// java/lang/Character was the one CLDC class this library did not publish at
// all, and a missing class is worse than a missing method: a member nothing
// answers stops the call, while a class nothing declares stops the resolution
// — so a title that puts a char in a Vector, or asks whether a key it was
// handed is a digit, dies before it reaches anything.
func TestCharacterAnswersTheTestsAndConversionsCLDCNames(t *testing.T) {
	vm := New(nil, Options{})

	boxed, err := vm.NewObject(CharacterClass, "(C)V", IntValue('Q'))
	if err != nil {
		t.Fatalf("new Character: %v", err)
	}
	value, err := vm.InvokeVirtual(boxed, "charValue", "()C")
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := value.Int32(); got != 'Q' {
		t.Fatalf("charValue = %d, want %d", got, 'Q')
	}
	text, err := vm.InvokeVirtual(boxed, "toString", "()Ljava/lang/String;")
	if err != nil {
		t.Fatal(err)
	}
	if object, _ := text.Reference(); object == nil {
		t.Fatal("toString answered null")
	} else if got, _ := StringText(object); got != "Q" {
		t.Fatalf("toString = %q, want %q", got, "Q")
	}
	// Equality is the value, so a title finds the character it stored rather
	// than only the object it stored.
	same, err := vm.NewObject(CharacterClass, "(C)V", IntValue('Q'))
	if err != nil {
		t.Fatal(err)
	}
	equal, err := vm.InvokeVirtual(boxed, "equals", "(Ljava/lang/Object;)Z", ReferenceValue(same))
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := equal.Int32(); got != 1 {
		t.Fatal("two boxes of one character are not equal")
	}

	static := func(name, descriptor string, arguments ...Value) int32 {
		t.Helper()
		answer, err := vm.InvokeStatic(CharacterClass, name, descriptor, arguments...)
		if err != nil {
			t.Fatalf("Character.%s%s: %v", name, descriptor, err)
		}
		number, err := answer.Int32()
		if err != nil {
			t.Fatal(err)
		}
		return number
	}

	for _, probe := range []struct {
		name string
		in   int32
		want int32
	}{
		{"isDigit", '7', 1},
		{"isDigit", 'a', 0},
		// The tests answer over ISO Latin-1, which is what CLDC says a handset
		// provides: a Hangul syllable is neither upper nor lower case here,
		// and answering otherwise would be a divergence with no caller.
		{"isUpperCase", 'A', 1},
		{"isUpperCase", 0xC9, 1}, // É
		{"isUpperCase", '가', 0},
		{"isLowerCase", 'z', 1},
		{"isLowerCase", 0xE9, 1}, // é
		{"isLowerCase", '가', 0},
	} {
		if got := static(probe.name, "(C)Z", IntValue(probe.in)); got != probe.want {
			t.Errorf("Character.%s(%d) = %d, want %d", probe.name, probe.in, got, probe.want)
		}
	}
	for _, probe := range []struct {
		name string
		in   int32
		want int32
	}{
		{"toLowerCase", 'A', 'a'},
		{"toLowerCase", 0xC9, 0xE9},
		// The multiplication sign sits inside the accented run and is not a
		// letter, so it converts to itself.
		{"toLowerCase", 0xD7, 0xD7},
		{"toUpperCase", 'a', 'A'},
		{"toUpperCase", 0xE9, 0xC9},
		{"toUpperCase", '가', '가'},
	} {
		if got := static(probe.name, "(C)C", IntValue(probe.in)); got != probe.want {
			t.Errorf("Character.%s(%d) = %d, want %d", probe.name, probe.in, got, probe.want)
		}
	}

	for _, probe := range []struct {
		character int32
		radix     int32
		want      int32
	}{
		{'7', 10, 7},
		{'f', 16, 15},
		{'F', 16, 15},
		// A digit the radix does not carry, and a radix outside the two the
		// class publishes, are both −1 rather than an exception.
		{'8', 8, -1},
		{'1', 1, -1},
		{'z', 37, -1},
		{'가', 16, -1},
	} {
		if got := static("digit", "(CI)I", IntValue(probe.character), IntValue(probe.radix)); got != probe.want {
			t.Errorf("Character.digit(%d, %d) = %d, want %d", probe.character, probe.radix, got, probe.want)
		}
	}
}

// The radix half of the boxed numbers: the parses, the formats, and the
// equality that is about the value. Nothing local reaches any of them — which
// is the reason to have them, because the call that would is on a title nobody
// has run and a member nothing answers stops it.
func TestTheBoxedNumbersAnswerTheirRadixForms(t *testing.T) {
	vm := New(nil, Options{})

	parsed, err := vm.InvokeStatic(LongClass, "parseLong", "(Ljava/lang/String;I)J",
		ReferenceValue(vm.NewString("-7fffffffffffffff")), IntValue(16))
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := parsed.Int64(); got != -9223372036854775807 {
		t.Fatalf("Long.parseLong(hex) = %d", got)
	}
	formatted, err := vm.InvokeStatic(LongClass, "toString", "(JI)Ljava/lang/String;",
		LongValue(255), IntValue(16))
	if err != nil {
		t.Fatal(err)
	}
	object, _ := formatted.Reference()
	if got, _ := StringText(object); got != "ff" {
		t.Fatalf("Long.toString(255, 16) = %q, want %q", got, "ff")
	}
	// A radix outside the bounds means ten when formatting, which is what the
	// standard says, and is refused when parsing, which is the exception it
	// names.
	formatted, err = vm.InvokeStatic(IntegerClass, "toString", "(II)Ljava/lang/String;",
		IntValue(255), IntValue(99))
	if err != nil {
		t.Fatal(err)
	}
	object, _ = formatted.Reference()
	if got, _ := StringText(object); got != "255" {
		t.Fatalf("Integer.toString(255, 99) = %q, want %q", got, "255")
	}
	if _, err := vm.InvokeStatic(IntegerClass, "valueOf", "(Ljava/lang/String;I)Ljava/lang/Integer;",
		ReferenceValue(vm.NewString("10")), IntValue(99)); err == nil {
		t.Fatal("a radix outside the bounds was accepted by a parse")
	}

	// A boxed long compares by value, and its hash is the two halves folded
	// together — the number the standard names rather than one this runtime
	// picks.
	first, err := vm.NewObject(LongClass, "(J)V", LongValue(0x0000_0001_0000_0002))
	if err != nil {
		t.Fatal(err)
	}
	second, err := vm.NewObject(LongClass, "(J)V", LongValue(0x0000_0001_0000_0002))
	if err != nil {
		t.Fatal(err)
	}
	equal, err := vm.InvokeVirtual(first, "equals", "(Ljava/lang/Object;)Z", ReferenceValue(second))
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := equal.Int32(); got != 1 {
		t.Fatal("two boxes of one long are not equal")
	}
	hash, err := vm.InvokeVirtual(first, "hashCode", "()I")
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := hash.Int32(); got != 3 {
		t.Fatalf("Long.hashCode = %d, want the halves folded together", got)
	}
}
