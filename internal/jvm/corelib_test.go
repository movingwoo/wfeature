package jvm

import (
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
	value, err := vm.InvokeStatic(ShortClass, "parseShort", "(Ljava/lang/String;)S", ReferenceValue(vm.NewString(" -32768 ")))
	if err != nil {
		t.Fatalf("Short.parseShort error = %v", err)
	}
	if number, err := value.Int32(); err != nil || number != -32768 {
		t.Fatalf("Short.parseShort(\" -32768 \") = %v/%v, want -32768", number, err)
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
