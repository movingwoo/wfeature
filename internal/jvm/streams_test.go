package jvm

import (
	"bytes"
	_ "embed"
	"fmt"
	"math"
	"slices"
	"strings"
	"testing"

	"golang.org/x/text/encoding/korean"
)

//go:embed testdata/Streams.class
var streamsClass []byte

// A title may write its save through java.io.DataOutput and read it back
// through java.io.DataInput, naming the interfaces and passing the streams.
// The interface calls have always dispatched on the class of the receiver, but
// `instanceof` asks the stream's declaration whether it implements the named
// interface. Both questions have to agree.
func TestInterfaceCallsLandOnTheStreamThatWasPassed(t *testing.T) {
	vm := New(mapClassSource{"Streams": streamsClass}, Options{})
	result, err := vm.InvokeStatic("Streams", "roundTrip", "()I")
	if err != nil {
		t.Fatalf("roundTrip() error = %v", err)
	}
	value, err := result.Int32()
	if err != nil {
		t.Fatal(err)
	}
	// 4660 + 5 + 1 + 1000, plus one and two for the exact float and double
	// round trips, written and read back in that order.
	if value != 5669 {
		t.Fatalf("roundTrip() = %d, want 5669", value)
	}
}

func TestDataStreamsImplementTheirCLDCInterfaces(t *testing.T) {
	vm := New(nil, Options{})
	for _, interfaceName := range []string{DataInputClass, DataOutputClass} {
		class, err := vm.loader.Load(interfaceName)
		if err != nil {
			t.Fatalf("Load(%s) error = %v", interfaceName, err)
		}
		if class.AccessFlags&AccessInterface == 0 {
			t.Errorf("%s is not declared as an interface", interfaceName)
		}
	}

	sink, err := vm.NewObject(ByteArrayOutputStreamClass, "()V")
	if err != nil {
		t.Fatalf("NewObject(ByteArrayOutputStream) error = %v", err)
	}
	output, err := vm.NewObject(DataOutputStreamClass, "(Ljava/io/OutputStream;)V", ReferenceValue(sink))
	if err != nil {
		t.Fatalf("NewObject(DataOutputStream) error = %v", err)
	}
	if !vm.IsInstance(output, DataOutputClass) {
		t.Error("DataOutputStream is not assignable to DataOutput")
	}

	source, err := vm.NewObject(ByteArrayInputStreamClass, "([B)V", ReferenceValue(NewByteArray(nil)))
	if err != nil {
		t.Fatalf("NewObject(ByteArrayInputStream) error = %v", err)
	}
	input, err := vm.NewObject(DataInputStreamClass, "(Ljava/io/InputStream;)V", ReferenceValue(source))
	if err != nil {
		t.Fatalf("NewObject(DataInputStream) error = %v", err)
	}
	if !vm.IsInstance(input, DataInputClass) {
		t.Error("DataInputStream is not assignable to DataInput")
	}
}

func TestDataOutputStreamCanonicalizesNaNThroughTheInterface(t *testing.T) {
	vm := New(mapClassSource{"Streams": streamsClass}, Options{})
	for _, probe := range []struct {
		name       string
		method     string
		descriptor string
		value      Value
		want       []byte
	}{
		{
			name:       "float",
			method:     "encodeFloat",
			descriptor: "(F)[B",
			value:      FloatValue(math.Float32frombits(0x7fa12345)),
			want:       []byte{0x7f, 0xc0, 0x00, 0x00},
		},
		{
			name:       "double",
			method:     "encodeDouble",
			descriptor: "(D)[B",
			value:      DoubleValue(math.Float64frombits(0x7ff0123456789abc)),
			want:       []byte{0x7f, 0xf8, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00},
		},
	} {
		t.Run(probe.name, func(t *testing.T) {
			result, err := vm.InvokeStatic("Streams", probe.method, probe.descriptor, probe.value)
			if err != nil {
				t.Fatalf("%s() error = %v", probe.method, err)
			}
			array, err := result.Reference()
			if err != nil {
				t.Fatal(err)
			}
			got, err := ByteArraySnapshot(array)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, probe.want) {
				t.Fatalf("%s(noncanonical NaN) = % x, want canonical % x", probe.method, got, probe.want)
			}
		})
	}
}

// A record is read until the stream runs out, and the end is told apart from
// a broken stream by the exception's class: DataInput raises EOFException,
// and a handler written for it must see one.
func TestDataInputRaisesEOFExceptionAtTheEnd(t *testing.T) {
	vm := New(libraryContractsSource(), Options{})
	if got := contractInt(t, vm, "readIntPastEnd"); got != 1 {
		t.Fatalf("readInt past the end took branch %d, want 1 (EOFException)", got)
	}
	if got := contractInt(t, vm, "readRecordsUntilEnd"); got != 7 {
		t.Fatalf("readShort until the end = %d, want 7", got)
	}
	if got := contractInt(t, vm, "readUTFPastEnd"); got != 1 {
		t.Fatalf("readUTF past the end took branch %d, want 1 (EOFException)", got)
	}
}

func readerUnits(t *testing.T, vm *VM, data []byte) []uint16 {
	t.Helper()
	result, err := vm.InvokeStatic("LibraryContracts", "readerUnits", "([B)[C", ReferenceValue(NewByteArray(data)))
	if err != nil {
		t.Fatalf("readerUnits(% x) error = %v", data, err)
	}
	array, err := result.Reference()
	if err != nil {
		t.Fatal(err)
	}
	_, values, err := ArraySnapshot(array)
	if err != nil {
		t.Fatal(err)
	}
	units := make([]uint16, len(values))
	for index, value := range values {
		unit, _ := value.Int32()
		units[index] = uint16(unit)
	}
	return units
}

// A byte the charset cannot read is one replacement character, and the bytes
// after it are read as what they are. A character outside the basic plane is
// the two units of its surrogate pair.
func TestInputStreamReaderKeepsTheBytesAfterABadOne(t *testing.T) {
	utf8 := New(libraryContractsSource(), Options{})
	for _, probe := range []struct {
		data []byte
		want []uint16
	}{
		{[]byte{0xff, 'A', 'B', 'C'}, []uint16{0xfffd, 'A', 'B', 'C'}},
		{[]byte{0xe0, 'A'}, []uint16{0xfffd, 'A'}},
		{[]byte("가😀!"), []uint16{0xac00, 0xd83d, 0xde00, '!'}},
		{[]byte{'A', 0xe0}, []uint16{'A', 0xfffd}},
	} {
		if got := readerUnits(t, utf8, probe.data); !slices.Equal(got, probe.want) {
			t.Errorf("UTF-8 reader over % x = %x, want %x", probe.data, got, probe.want)
		}
	}

	euckr := New(libraryContractsSource(), Options{ByteDecoder: func(data []byte) string {
		decoded, err := korean.EUCKR.NewDecoder().Bytes(data)
		if err != nil {
			return strings.ToValidUTF8(string(data), "�")
		}
		return string(decoded)
	}})
	data := []byte{0xb0, 0xa1, 0xff, 'A', 'B', 0xb0, 0xa1}
	want := []uint16{0xac00, 0xfffd, 'A', 'B', 0xac00}
	if got := readerUnits(t, euckr, data); !slices.Equal(got, want) {
		t.Errorf("EUC-KR reader over % x = %x, want %x", data, got, want)
	}
}

func streamCall(t *testing.T, vm *VM, receiver *Object, name, descriptor string, arguments ...Value) Value {
	t.Helper()
	result, err := vm.InvokeVirtual(receiver, name, descriptor, arguments...)
	if err != nil {
		t.Fatalf("%s.%s%s error = %v", receiver.ClassName, name, descriptor, err)
	}
	return result
}

func readerOver(t *testing.T, vm *VM, data []byte) *Object {
	t.Helper()
	source, err := vm.NewObject(ByteArrayInputStreamClass, "([B)V", ReferenceValue(NewByteArray(data)))
	if err != nil {
		t.Fatal(err)
	}
	reader, err := vm.NewObject(InputStreamReaderClass, "(Ljava/io/InputStream;)V", ReferenceValue(source))
	if err != nil {
		t.Fatal(err)
	}
	return reader
}

func readUnits(t *testing.T, vm *VM, reader *Object, count int) []int32 {
	t.Helper()
	units := make([]int32, count)
	for index := range units {
		units[index], _ = streamCall(t, vm, reader, "read", "()I").Int32()
	}
	return units
}

// An InputStreamReader marks and resets the stream it reads, and what it held
// past the stream's position at the mark — the second half of a pair, or a
// byte it read ahead of a replacement — comes back with the stream, so a read
// after reset answers what a read after mark did. A reset with no mark returns
// to the stream's own mark, its start, where the reader held nothing.
func TestInputStreamReaderMarksAndResetsTheStreamItReads(t *testing.T) {
	vm := New(nil, Options{})
	pair := readerOver(t, vm, []byte("x😀y"))
	if got := readUnits(t, vm, pair, 2); !slices.Equal(got, []int32{'x', 0xd83d}) {
		t.Fatalf("before the mark = %x", got)
	}
	streamCall(t, vm, pair, "mark", "(I)V", IntValue(10))
	if got := readUnits(t, vm, pair, 3); !slices.Equal(got, []int32{0xde00, 'y', -1}) {
		t.Fatalf("after the mark = %x", got)
	}
	streamCall(t, vm, pair, "reset", "()V")
	if got := readUnits(t, vm, pair, 3); !slices.Equal(got, []int32{0xde00, 'y', -1}) {
		t.Fatalf("after reset = %x, want the second half of the pair again", got)
	}

	ahead := readerOver(t, vm, []byte{0xc3, 'A', 'B'})
	if got := readUnits(t, vm, ahead, 1); got[0] != 0xfffd {
		t.Fatalf("a bad lead byte read as %x", got)
	}
	streamCall(t, vm, ahead, "mark", "(I)V", IntValue(1))
	if got := readUnits(t, vm, ahead, 3); !slices.Equal(got, []int32{'A', 'B', -1}) {
		t.Fatalf("after the mark = %x", got)
	}
	streamCall(t, vm, ahead, "reset", "()V")
	if got := readUnits(t, vm, ahead, 3); !slices.Equal(got, []int32{'A', 'B', -1}) {
		t.Fatalf("after reset = %x, want the byte read ahead again", got)
	}

	unmarked := readerOver(t, vm, []byte("ab"))
	readUnits(t, vm, unmarked, 1)
	streamCall(t, vm, unmarked, "reset", "()V")
	if got := readUnits(t, vm, unmarked, 2); !slices.Equal(got, []int32{'a', 'b'}) {
		t.Fatalf("reset with no mark = %x, want the start", got)
	}

	if got := streamCall(t, vm, unmarked, "markSupported", "()Z"); got != IntValue(1) {
		t.Fatal("a reader over a stream with a mark does not support one")
	}
	streamCall(t, vm, unmarked, "close", "()V")
	if got := streamCall(t, vm, unmarked, "markSupported", "()Z"); got != IntValue(0) {
		t.Fatal("a closed reader supports a mark")
	}
	for _, call := range []struct{ name, descriptor string }{{"mark", "(I)V"}, {"reset", "()V"}} {
		arguments := []Value{}
		if call.name == "mark" {
			arguments = append(arguments, IntValue(1))
		}
		if _, err := vm.InvokeVirtual(unmarked, call.name, call.descriptor, arguments...); !vm.IsGuestException(err, IOExceptionClass) {
			t.Errorf("%s of a closed reader = %v, want IOException", call.name, err)
		}
	}
}

// The limit a reader is marked with goes to the stream as it was given, as the
// CLDC reader and LGT's pass it; a stream that supports no mark is not asked at
// all.
func TestInputStreamReaderPassesItsMarkLimitToTheStream(t *testing.T) {
	vm := New(nil, Options{})
	var limits []int32
	supported := int32(1)
	if err := vm.DefineClass(ClassDefinition{Name: "test/Marked", SuperName: InputStreamClass, Access: AccessPublic,
		Methods: []MethodDefinition{
			{Name: "<init>", Descriptor: "()V", Access: AccessPublic, Body: emptyConstructor},
			{Name: "read", Descriptor: "()I", Access: AccessPublic, Body: constantInt(-1)},
			{Name: "markSupported", Descriptor: "()Z", Access: AccessPublic, Body: func(*Invocation, []Value) (Value, error) {
				return IntValue(supported), nil
			}},
			{Name: "mark", Descriptor: "(I)V", Access: AccessPublic, Body: func(_ *Invocation, arguments []Value) (Value, error) {
				limit, _ := arguments[1].Int32()
				limits = append(limits, limit)
				return VoidValue(), nil
			}},
		}}); err != nil {
		t.Fatal(err)
	}
	source, err := vm.NewObject("test/Marked", "()V")
	if err != nil {
		t.Fatal(err)
	}
	reader, err := vm.NewObject(InputStreamReaderClass, "(Ljava/io/InputStream;)V", ReferenceValue(source))
	if err != nil {
		t.Fatal(err)
	}
	for _, limit := range []int32{10, 0, -3, math.MaxInt32} {
		streamCall(t, vm, reader, "mark", "(I)V", IntValue(limit))
	}
	if want := []int32{10, 0, -3, math.MaxInt32}; !slices.Equal(limits, want) {
		t.Fatalf("the stream was asked to keep %v, want %v", limits, want)
	}
	supported = 0
	if _, err := vm.InvokeVirtual(reader, "mark", "(I)V", IntValue(1)); !vm.IsGuestException(err, IOExceptionClass) || len(limits) != 4 {
		t.Fatalf("mark over a stream with none = %v, asking %v, want IOException and no mark", err, limits)
	}
}

// Reader and Writer keep the object their subclasses synchronize on: the
// reader or writer itself, or the object it was constructed with, a null one
// being NullPointerException. A Reader that does not override the mark pair
// supports no mark and refuses both with IOException.
func TestReaderAndWriterKeepTheLockTheyAreGiven(t *testing.T) {
	vm := New(nil, Options{})
	for _, definition := range []ClassDefinition{
		{Name: "test/Lines", SuperName: ReaderClass, Access: AccessPublic, Methods: superConstructors(ReaderClass)},
		{Name: "test/Pen", SuperName: WriterClass, Access: AccessPublic, Methods: superConstructors(WriterClass)},
	} {
		if err := vm.DefineClass(definition); err != nil {
			t.Fatal(err)
		}
	}
	shared := vm.NewString("lock")
	for _, probe := range []struct{ class, base string }{{"test/Lines", ReaderClass}, {"test/Pen", WriterClass}} {
		own, err := vm.NewObject(probe.class, "()V")
		if err != nil {
			t.Fatal(err)
		}
		named, err := vm.NewObject(probe.class, "(Ljava/lang/Object;)V", ReferenceValue(shared))
		if err != nil {
			t.Fatal(err)
		}
		for object, want := range map[*Object]*Object{own: own, named: shared} {
			value, err := vm.Field(object, probe.base, "lock", "Ljava/lang/Object;")
			if err != nil {
				t.Fatal(err)
			}
			if lock, _ := value.Reference(); lock != want {
				t.Errorf("%s lock = %v, want %v", probe.class, lock, want)
			}
		}
		if _, err := vm.NewObject(probe.class, "(Ljava/lang/Object;)V", ReferenceValue(nil)); !vm.IsGuestException(err, "java/lang/NullPointerException") {
			t.Errorf("%s(null) = %v, want NullPointerException", probe.class, err)
		}
	}
	reader := readerOver(t, vm, []byte("a"))
	if value, _ := vm.Field(reader, ReaderClass, "lock", "Ljava/lang/Object;"); mustObject(value) != reader {
		t.Error("an InputStreamReader does not lock on itself")
	}
	lines, err := vm.NewObject("test/Lines", "()V")
	if err != nil {
		t.Fatal(err)
	}
	if got := streamCall(t, vm, lines, "markSupported", "()Z"); got != IntValue(0) {
		t.Error("a Reader that does not override mark supports one")
	}
	if _, err := vm.InvokeVirtual(lines, "mark", "(I)V", IntValue(1)); !vm.IsGuestException(err, IOExceptionClass) || !strings.Contains(err.Error(), "mark() not supported") {
		t.Errorf("Reader.mark = %v, want IOException", err)
	}
	if _, err := vm.InvokeVirtual(lines, "reset", "()V"); !vm.IsGuestException(err, IOExceptionClass) || !strings.Contains(err.Error(), "reset() not supported") {
		t.Errorf("Reader.reset = %v, want IOException", err)
	}
}

func mustObject(value Value) *Object {
	object, _ := value.Reference()
	return object
}

// superConstructors are a subclass's two constructors that only call its
// superclass's, as the compiler writes them for super() and super(lock).
func superConstructors(parent string) []MethodDefinition {
	return []MethodDefinition{
		{Name: "<init>", Descriptor: "()V", Access: AccessPublic, Body: superInit(parent, "()V")},
		{Name: "<init>", Descriptor: "(Ljava/lang/Object;)V", Access: AccessPublic, Body: superInit(parent, "(Ljava/lang/Object;)V")},
	}
}

// ByteArrayOutputStream's text is the bytes written so far, decoded as
// `new String(buf, 0, count)` decodes them here — with the platform's charset
// — and refused as that constructor refuses a count the buffer does not hold
// and a buffer that is null.
func TestByteArrayOutputStreamTextIsWhatNewStringAnswers(t *testing.T) {
	vm := New(nil, Options{ByteDecoder: func(data []byte) string { return "<" + string(data) + ">" }})
	stream, err := vm.NewObject(ByteArrayOutputStreamClass, "()V")
	if err != nil {
		t.Fatal(err)
	}
	streamCall(t, vm, stream, "write", "([BII)V", ReferenceValue(NewByteArray([]byte("xabcx"))), IntValue(1), IntValue(3))
	text := func() string {
		t.Helper()
		got, _ := StringText(mustObject(streamCall(t, vm, stream, "toString", "()Ljava/lang/String;")))
		return got
	}
	if got := text(); got != "<abc>" {
		t.Fatalf("toString() = %q, want the platform's decoding", got)
	}
	built, err := vm.NewObject(StringClass, "([B)V", streamCall(t, vm, stream, "toByteArray", "()[B"))
	if err != nil {
		t.Fatal(err)
	}
	if want, _ := StringText(built); text() != want {
		t.Fatalf("toString() = %q, new String(toByteArray()) = %q", text(), want)
	}
	streamCall(t, vm, stream, "reset", "()V")
	if got := text(); got != "<>" {
		t.Fatalf("toString() after reset = %q", got)
	}
	if err := setIntField(vm, stream, ByteArrayOutputStreamClass, "count", 99); err != nil {
		t.Fatal(err)
	}
	if _, err := vm.InvokeVirtual(stream, "toString", "()Ljava/lang/String;"); !vm.IsGuestException(err, "java/lang/IndexOutOfBoundsException") {
		t.Fatalf("toString() with a count past the buffer = %v, want IndexOutOfBoundsException", err)
	}
	if err := vm.SetField(stream, ByteArrayOutputStreamClass, "buf", "[B", ReferenceValue(nil)); err != nil {
		t.Fatal(err)
	}
	if _, err := vm.InvokeVirtual(stream, "toString", "()Ljava/lang/String;"); !vm.IsGuestException(err, "java/lang/NullPointerException") {
		t.Fatalf("toString() with no buffer = %v, want NullPointerException", err)
	}
}

// The static readUTF reads through the DataInput it is given — its own
// readUnsignedShort, then its own readFully into an array of that length — and
// decodes what it read as the class library does: a zero byte or a long form
// is a character, a sequence it cannot read is UTFDataFormatException, and the
// input's own end is its own EOFException.
func TestReadUTFReadsThroughTheDataInputItIsGiven(t *testing.T) {
	vm := New(nil, Options{MaxArrayLength: 64})
	var calls []string
	length, content := int32(0), []byte(nil)
	var refusal error
	if err := vm.DefineClass(ClassDefinition{Name: "test/Input", SuperName: ObjectClass, Interfaces: []string{DataInputClass}, Access: AccessPublic,
		Methods: []MethodDefinition{
			{Name: "<init>", Descriptor: "()V", Access: AccessPublic, Body: emptyConstructor},
			{Name: "readUnsignedShort", Descriptor: "()I", Access: AccessPublic, Body: func(*Invocation, []Value) (Value, error) {
				calls = append(calls, "readUnsignedShort")
				return IntValue(length), nil
			}},
			{Name: "readFully", Descriptor: "([BII)V", Access: AccessPublic, Body: func(_ *Invocation, arguments []Value) (Value, error) {
				offset, _ := arguments[2].Int32()
				count, _ := arguments[3].Int32()
				array, _ := guestArray(mustObject(arguments[1]))
				calls = append(calls, fmt.Sprintf("readFully(%d of %d, %d, %d)", array.Length(), len(content), offset, count))
				if refusal != nil {
					return VoidValue(), refusal
				}
				values := make([]Value, len(content))
				for index, value := range content {
					values[index] = IntValue(int32(int8(value)))
				}
				return VoidValue(), SetArrayRange(mustObject(arguments[1]), int(offset), values)
			}},
		}}); err != nil {
		t.Fatal(err)
	}
	input, err := vm.NewObject("test/Input", "()V")
	if err != nil {
		t.Fatal(err)
	}
	readUTF := func(bytes []byte) (string, error) {
		length, content, calls = int32(len(bytes)), bytes, nil
		result, err := vm.InvokeStatic(DataInputStreamClass, "readUTF", "(Ljava/io/DataInput;)Ljava/lang/String;", ReferenceValue(input))
		if err != nil {
			return "", err
		}
		text, _ := StringText(mustObject(result))
		return text, nil
	}
	text, err := readUTF([]byte{'h', 'i', 0xc3, 0xa9, 0xea, 0xb0, 0x80, 0, 0xc0, 0x80})
	if err != nil || text != "hié가\x00\x00" {
		t.Fatalf("readUTF = %q, %v", text, err)
	}
	if want := []string{"readUnsignedShort", "readFully(10 of 10, 0, 10)"}; !slices.Equal(calls, want) {
		t.Fatalf("the input was asked %v, want %v", calls, want)
	}
	for _, bytes := range [][]byte{{0x80}, {0xe0, 0x80}, {0xc3}, {0xf0, 0x9f, 0x98, 0x80}, {'a', 0xc3, 'b'}} {
		if _, err := readUTF(bytes); guestExceptionClass(err) != "java/io/UTFDataFormatException" || !vm.IsGuestException(err, IOExceptionClass) {
			t.Errorf("readUTF of % x = %v, want UTFDataFormatException", bytes, err)
		}
	}
	refusal = guestException(eofExceptionClass, "the input ended")
	if _, err := readUTF([]byte("abc")); guestExceptionClass(err) != eofExceptionClass {
		t.Errorf("readUTF over an input that ends = %v, want its EOFException", err)
	}
	refusal = nil
	length, content = -1, nil
	if _, err := vm.InvokeStatic(DataInputStreamClass, "readUTF", "(Ljava/io/DataInput;)Ljava/lang/String;", ReferenceValue(input)); !vm.IsGuestException(err, "java/lang/NegativeArraySizeException") {
		t.Errorf("readUTF of a negative length = %v, want NegativeArraySizeException", err)
	}
	length = 65
	if _, err := vm.InvokeStatic(DataInputStreamClass, "readUTF", "(Ljava/io/DataInput;)Ljava/lang/String;", ReferenceValue(input)); err == nil {
		t.Error("readUTF of a length past the array limit was accepted")
	}
	if _, err := vm.InvokeStatic(DataInputStreamClass, "readUTF", "(Ljava/io/DataInput;)Ljava/lang/String;", ReferenceValue(nil)); !vm.IsGuestException(err, "java/lang/NullPointerException") {
		t.Errorf("readUTF(null) = %v, want NullPointerException", err)
	}
}

// An OutputStreamWriter sends the bytes String.getBytes answers for the same
// characters and encoding — the platform's by default — to its stream, one
// write(byte[], int, int) per write and nothing kept back, except the first
// half of a pair, which waits for its second. It takes exactly the encoding
// names InputStreamReader takes.
func TestOutputStreamWriterSendsWhatGetBytesAnswers(t *testing.T) {
	vm := New(nil, Options{ByteEncoder: func(text string) []byte { return []byte(strings.ToUpper(text)) }})
	var sent []string
	flushes, closes := 0, 0
	if err := vm.DefineClass(ClassDefinition{Name: "test/Sink", SuperName: OutputStreamClass, Access: AccessPublic,
		Methods: []MethodDefinition{
			{Name: "<init>", Descriptor: "()V", Access: AccessPublic, Body: emptyConstructor},
			{Name: "write", Descriptor: "(I)V", Access: AccessPublic, Body: func(*Invocation, []Value) (Value, error) {
				t.Error("a writer sent a byte at a time")
				return VoidValue(), nil
			}},
			{Name: "write", Descriptor: "([BII)V", Access: AccessPublic, Body: func(_ *Invocation, arguments []Value) (Value, error) {
				data, _ := ByteArraySnapshot(mustObject(arguments[1]))
				offset, _ := arguments[2].Int32()
				length, _ := arguments[3].Int32()
				sent = append(sent, fmt.Sprintf("%x", data[offset:offset+length]))
				return VoidValue(), nil
			}},
			{Name: "flush", Descriptor: "()V", Access: AccessPublic, Body: func(*Invocation, []Value) (Value, error) {
				flushes++
				return VoidValue(), nil
			}},
			{Name: "close", Descriptor: "()V", Access: AccessPublic, Body: func(*Invocation, []Value) (Value, error) {
				closes++
				return VoidValue(), nil
			}},
		}}); err != nil {
		t.Fatal(err)
	}
	sink, err := vm.NewObject("test/Sink", "()V")
	if err != nil {
		t.Fatal(err)
	}
	str := func(text string) Value { return ReferenceValue(vm.NewString(text)) }
	writer, err := vm.NewObject(OutputStreamWriterClass, "(Ljava/io/OutputStream;)V", ReferenceValue(sink))
	if err != nil {
		t.Fatal(err)
	}
	streamCall(t, vm, writer, "write", "(Ljava/lang/String;)V", str("ab"))
	streamCall(t, vm, writer, "write", "(I)V", IntValue('c'))
	streamCall(t, vm, writer, "write", "([CII)V", ReferenceValue(charArray(t, vm, []uint16{'x', 'y', 'z'})), IntValue(1), IntValue(2))
	streamCall(t, vm, writer, "write", "(Ljava/lang/String;II)V", str("0123"), IntValue(1), IntValue(2))
	streamCall(t, vm, writer, "flush", "()V")
	if want := []string{"4142", "43", "595a", "3132"}; !slices.Equal(sent, want) || flushes != 1 {
		t.Fatalf("the platform writer sent %v and flushed %d times, want %v and once", sent, flushes, want)
	}

	sent = nil
	utf8, err := vm.NewObject(OutputStreamWriterClass, "(Ljava/io/OutputStream;Ljava/lang/String;)V", ReferenceValue(sink), str("utf8"))
	if err != nil {
		t.Fatal(err)
	}
	streamCall(t, vm, utf8, "write", "(Ljava/lang/String;)V", str("é"))
	streamCall(t, vm, utf8, "write", "(I)V", IntValue(0xd83d))
	if len(sent) != 1 {
		t.Fatalf("the first half of a pair was sent on its own: %v", sent)
	}
	streamCall(t, vm, utf8, "write", "(I)V", IntValue(0xde00))
	streamCall(t, vm, utf8, "write", "([CII)V", ReferenceValue(charArray(t, vm, []uint16{'!', 0xd83d})), IntValue(0), IntValue(2))
	streamCall(t, vm, utf8, "close", "()V")
	streamCall(t, vm, utf8, "close", "()V")
	if want := []string{"c3a9", "f09f9880", "21", "efbfbd"}; !slices.Equal(sent, want) || closes != 1 {
		t.Fatalf("the UTF-8 writer sent %v and closed %d times, want %v and once", sent, closes, want)
	}
	for _, call := range []struct {
		name, descriptor string
		arguments        []Value
	}{
		{"write", "(I)V", []Value{IntValue('x')}},
		{"write", "(Ljava/lang/String;II)V", []Value{ReferenceValue(nil), IntValue(0), IntValue(1)}},
		{"write", "([CII)V", []Value{ReferenceValue(nil), IntValue(0), IntValue(1)}},
		{"flush", "()V", nil},
	} {
		if _, err := vm.InvokeVirtual(utf8, call.name, call.descriptor, call.arguments...); guestExceptionClass(err) != IOExceptionClass {
			t.Errorf("%s%s of a closed writer = %v, want IOException before anything else", call.name, call.descriptor, err)
		}
	}

	for _, probe := range []struct {
		descriptor string
		arguments  []Value
		want       string
	}{
		{"(Ljava/lang/String;II)V", []Value{ReferenceValue(nil), IntValue(0), IntValue(0)}, "java/lang/NullPointerException"},
		{"(Ljava/lang/String;II)V", []Value{str("ab"), IntValue(1), IntValue(2)}, "java/lang/StringIndexOutOfBoundsException"},
		{"(Ljava/lang/String;II)V", []Value{str("ab"), IntValue(1), IntValue(-1)}, "java/lang/StringIndexOutOfBoundsException"},
		{"([CII)V", []Value{ReferenceValue(nil), IntValue(0), IntValue(0)}, "java/lang/NullPointerException"},
		{"([CII)V", []Value{ReferenceValue(charArray(t, vm, []uint16{'a'})), IntValue(-1), IntValue(1)}, "java/lang/IndexOutOfBoundsException"},
		{"([CII)V", []Value{ReferenceValue(charArray(t, vm, []uint16{'a'})), IntValue(1), IntValue(1)}, "java/lang/IndexOutOfBoundsException"},
	} {
		if _, err := vm.InvokeVirtual(writer, "write", probe.descriptor, probe.arguments...); guestExceptionClass(err) != probe.want {
			t.Errorf("write%s = %v, want %s", probe.descriptor, err, probe.want)
		}
	}

	for _, name := range []string{"UTF-8", "utf_8", "EUC-KR", "ksc5601", "x-windows-949", "CP949", "ISO-8859-1", "UTF-16", ""} {
		_, readerErr := vm.NewObject(InputStreamReaderClass, "(Ljava/io/InputStream;Ljava/lang/String;)V", ReferenceValue(sink), str(name))
		_, writerErr := vm.NewObject(OutputStreamWriterClass, "(Ljava/io/OutputStream;Ljava/lang/String;)V", ReferenceValue(sink), str(name))
		if (readerErr == nil) != (writerErr == nil) {
			t.Errorf("encoding %q: InputStreamReader %v, OutputStreamWriter %v", name, readerErr, writerErr)
		}
		if writerErr != nil && guestExceptionClass(writerErr) != "java/io/UnsupportedEncodingException" {
			t.Errorf("OutputStreamWriter(%q) = %v, want UnsupportedEncodingException", name, writerErr)
		}
	}
	for _, arguments := range [][]Value{{ReferenceValue(nil), str("UTF-8")}, {ReferenceValue(sink), ReferenceValue(nil)}, {ReferenceValue(nil), str("bogus")}} {
		if _, err := vm.NewObject(OutputStreamWriterClass, "(Ljava/io/OutputStream;Ljava/lang/String;)V", arguments...); guestExceptionClass(err) != "java/lang/NullPointerException" {
			t.Errorf("OutputStreamWriter%v = %v, want NullPointerException", arguments, err)
		}
	}
	if _, err := vm.NewObject(OutputStreamWriterClass, "(Ljava/io/OutputStream;)V", ReferenceValue(nil)); guestExceptionClass(err) != "java/lang/NullPointerException" {
		t.Errorf("OutputStreamWriter(null) = %v, want NullPointerException", err)
	}
}

// Writer's own write forms all come down to the write(char[], int, int) a
// subclass implements, reached through the VM: write(int) as its low sixteen
// bits, write(char[]) as the whole array, write(String) through the
// subclass's write(String, int, int) when it has one.
func TestWriterHandsEveryFormToItsOwnWrite(t *testing.T) {
	vm := New(nil, Options{})
	var written []string
	record := func(entry string) { written = append(written, entry) }
	if err := vm.DefineClass(ClassDefinition{Name: "test/Collect", SuperName: WriterClass, Access: AccessPublic,
		Methods: []MethodDefinition{
			{Name: "<init>", Descriptor: "()V", Access: AccessPublic, Body: superInit(WriterClass, "()V")},
			{Name: "write", Descriptor: "([CII)V", Access: AccessPublic, Body: func(_ *Invocation, arguments []Value) (Value, error) {
				units, _ := charArrayUnits(mustObject(arguments[1]))
				offset, _ := arguments[2].Int32()
				length, _ := arguments[3].Int32()
				record(fmt.Sprintf("%x", units[offset:offset+length]))
				return VoidValue(), nil
			}},
			{Name: "flush", Descriptor: "()V", Access: AccessPublic, Body: doNothing},
			{Name: "close", Descriptor: "()V", Access: AccessPublic, Body: doNothing},
		}}); err != nil {
		t.Fatal(err)
	}
	if err := vm.DefineClass(ClassDefinition{Name: "test/Ranges", SuperName: "test/Collect", Access: AccessPublic,
		Methods: []MethodDefinition{
			{Name: "<init>", Descriptor: "()V", Access: AccessPublic, Body: superInit("test/Collect", "()V")},
			{Name: "write", Descriptor: "(Ljava/lang/String;II)V", Access: AccessPublic, Body: func(_ *Invocation, arguments []Value) (Value, error) {
				offset, _ := arguments[2].Int32()
				length, _ := arguments[3].Int32()
				record(fmt.Sprintf("range %d %d", offset, length))
				return VoidValue(), nil
			}},
		}}); err != nil {
		t.Fatal(err)
	}
	collect, err := vm.NewObject("test/Collect", "()V")
	if err != nil {
		t.Fatal(err)
	}
	str := func(text string) Value { return ReferenceValue(vm.NewString(text)) }
	streamCall(t, vm, collect, "write", "(I)V", IntValue(0x1f600))
	streamCall(t, vm, collect, "write", "([C)V", ReferenceValue(charArray(t, vm, []uint16{'a', 0xd800})))
	streamCall(t, vm, collect, "write", "(Ljava/lang/String;)V", str("bc"))
	streamCall(t, vm, collect, "write", "(Ljava/lang/String;II)V", str("defg"), IntValue(1), IntValue(2))
	if want := []string{"[f600]", "[61 d800]", "[62 63]", "[65 66]"}; !slices.Equal(written, want) {
		t.Fatalf("the writer was handed %v, want %v", written, want)
	}
	ranges, err := vm.NewObject("test/Ranges", "()V")
	if err != nil {
		t.Fatal(err)
	}
	written = nil
	streamCall(t, vm, ranges, "write", "(Ljava/lang/String;)V", str("hij"))
	if want := []string{"range 0 3"}; !slices.Equal(written, want) {
		t.Fatalf("write(String) reached %v, want the subclass's own range form", written)
	}
	for _, probe := range []struct {
		descriptor string
		arguments  []Value
		want       string
	}{
		{"(Ljava/lang/String;)V", []Value{ReferenceValue(nil)}, "java/lang/NullPointerException"},
		{"([C)V", []Value{ReferenceValue(nil)}, "java/lang/NullPointerException"},
		{"(Ljava/lang/String;II)V", []Value{str("ab"), IntValue(2), IntValue(1)}, "java/lang/StringIndexOutOfBoundsException"},
		{"(Ljava/lang/String;II)V", []Value{str("ab"), IntValue(math.MaxInt32), IntValue(math.MaxInt32)}, "java/lang/StringIndexOutOfBoundsException"},
	} {
		if _, err := vm.InvokeVirtual(collect, "write", probe.descriptor, probe.arguments...); guestExceptionClass(err) != probe.want {
			t.Errorf("Writer.write%s = %v, want %s", probe.descriptor, err, probe.want)
		}
	}
}
