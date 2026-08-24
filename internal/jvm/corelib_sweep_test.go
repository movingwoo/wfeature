package jvm

import (
	"testing"
	"unicode/utf16"
)

// The class-library members a sweep of a local archive set reached for and this
// runtime did not have. Each is here because a title stopped on it.

// writeUTF and readUTF are one round trip: a title stores a name with the first
// and reads it back with the second, so the two have to agree on the encoding
// rather than on Go's.
func TestWriteUTFRoundTripsThroughReadUTF(t *testing.T) {
	vm := New(nil, Options{})
	sink, err := vm.NewObject(ByteArrayOutputStreamClass, "()V")
	if err != nil {
		t.Fatal(err)
	}
	output, err := vm.NewObject(DataOutputStreamClass, "(Ljava/io/OutputStream;)V", ReferenceValue(sink))
	if err != nil {
		t.Fatal(err)
	}
	// One ASCII character, one that needs two bytes, one that needs three, and
	// the null the modified encoding is modified for.
	const text = "aé가\x00"
	if _, err := vm.InvokeVirtual(output, "writeUTF", "(Ljava/lang/String;)V", ReferenceValue(vm.NewString(text))); err != nil {
		t.Fatal(err)
	}
	bytes, err := vm.InvokeVirtual(sink, "toByteArray", "()[B")
	if err != nil {
		t.Fatal(err)
	}
	array, err := bytes.Reference()
	if err != nil {
		t.Fatal(err)
	}
	data, err := ByteArraySnapshot(array)
	if err != nil {
		t.Fatal(err)
	}
	// Two bytes of length, then 1 + 2 + 3 + 2 bytes of content.
	if len(data) != 10 {
		t.Fatalf("writeUTF produced %d bytes, want 10", len(data))
	}
	if int(data[0])<<8|int(data[1]) != 8 {
		t.Fatalf("writeUTF wrote length %d, want 8", int(data[0])<<8|int(data[1]))
	}
	source, err := vm.NewObject(ByteArrayInputStreamClass, "([B)V", ReferenceValue(array))
	if err != nil {
		t.Fatal(err)
	}
	input, err := vm.NewObject(DataInputStreamClass, "(Ljava/io/InputStream;)V", ReferenceValue(source))
	if err != nil {
		t.Fatal(err)
	}
	result, err := vm.InvokeVirtual(input, "readUTF", "()Ljava/lang/String;")
	if err != nil {
		t.Fatal(err)
	}
	object, err := result.Reference()
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := StringText(object); got != text {
		t.Fatalf("readUTF() = %q, want %q", got, text)
	}
}

// readChar is two bytes without a sign, readUnsignedByte one byte without one.
// A title decodes a fixed-width record with these, so a sign that leaks in is a
// wrong number rather than an error.
func TestDataInputReadsCharsAndUnsignedBytes(t *testing.T) {
	vm := New(nil, Options{})
	array := NewByteArray([]byte{0xff, 0x80, 0xff})
	source, err := vm.NewObject(ByteArrayInputStreamClass, "([B)V", ReferenceValue(array))
	if err != nil {
		t.Fatal(err)
	}
	input, err := vm.NewObject(DataInputStreamClass, "(Ljava/io/InputStream;)V", ReferenceValue(source))
	if err != nil {
		t.Fatal(err)
	}
	character, err := vm.InvokeVirtual(input, "readChar", "()C")
	if err != nil {
		t.Fatal(err)
	}
	if value, _ := character.Int32(); value != 0xff80 {
		t.Fatalf("readChar() = %#x, want 0xff80", value)
	}
	unsigned, err := vm.InvokeVirtual(input, "readUnsignedByte", "()I")
	if err != nil {
		t.Fatal(err)
	}
	if value, _ := unsigned.Int32(); value != 0xff {
		t.Fatalf("readUnsignedByte() = %#x, want 0xff", value)
	}
	if _, err := vm.InvokeVirtual(input, "readUnsignedByte", "()I"); err == nil {
		t.Fatal("reading past the end answered with a value")
	}
}

// A reader decodes with the platform's charset and does it one character at a
// time, so a stream that has not ended does not have to before the first
// character comes back.
func TestInputStreamReaderDecodesWithThePlatformCharset(t *testing.T) {
	// A two-byte charset stands for the handset's own: the decoder is what
	// says how many bytes a character took, and nothing here has a table.
	decoder := func(data []byte) string {
		units := make([]uint16, 0, len(data))
		for index := 0; index < len(data); index++ {
			if data[index] < 0x80 {
				units = append(units, uint16(data[index]))
				continue
			}
			if index+1 >= len(data) {
				return string(utf16.Decode(append(units, 0xfffd)))
			}
			units = append(units, uint16(data[index])<<8|uint16(data[index+1]))
			index++
		}
		return string(utf16.Decode(units))
	}
	vm := New(nil, Options{ByteDecoder: decoder})
	array := NewByteArray([]byte{'o', 0xac, 0x00, 'k'})
	source, err := vm.NewObject(ByteArrayInputStreamClass, "([B)V", ReferenceValue(array))
	if err != nil {
		t.Fatal(err)
	}
	reader, err := vm.NewObject(InputStreamReaderClass, "(Ljava/io/InputStream;)V", ReferenceValue(source))
	if err != nil {
		t.Fatal(err)
	}
	var read []rune
	for {
		result, readErr := vm.InvokeVirtual(reader, "read", "()I")
		if readErr != nil {
			t.Fatal(readErr)
		}
		value, _ := result.Int32()
		if value < 0 {
			break
		}
		read = append(read, rune(value))
	}
	if string(read) != "o가k" {
		t.Fatalf("reader produced %q, want %q", string(read), "o가k")
	}
}

// An encoding this runtime does not have is refused rather than silently read
// with the platform's own.
func TestInputStreamReaderRefusesAnUnknownEncoding(t *testing.T) {
	vm := New(nil, Options{})
	source, err := vm.NewObject(ByteArrayInputStreamClass, "([B)V", ReferenceValue(NewByteArray([]byte{'a'})))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := vm.NewObject(InputStreamReaderClass, "(Ljava/io/InputStream;Ljava/lang/String;)V",
		ReferenceValue(source), ReferenceValue(vm.NewString("Shift_JIS"))); err == nil {
		t.Fatal("an unknown encoding was accepted")
	}
}

func TestStringBufferInsertsAtAnOffset(t *testing.T) {
	vm := New(nil, Options{})
	buffer, err := vm.NewObject(StringBufferClass, "(Ljava/lang/String;)V", ReferenceValue(vm.NewString("bc")))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := vm.InvokeVirtual(buffer, "insert", "(ILjava/lang/String;)Ljava/lang/StringBuffer;",
		IntValue(0), ReferenceValue(vm.NewString("a"))); err != nil {
		t.Fatal(err)
	}
	if _, err := vm.InvokeVirtual(buffer, "insert", "(IC)Ljava/lang/StringBuffer;", IntValue(3), IntValue('d')); err != nil {
		t.Fatal(err)
	}
	result, err := vm.InvokeVirtual(buffer, "toString", "()Ljava/lang/String;")
	if err != nil {
		t.Fatal(err)
	}
	object, _ := result.Reference()
	if text, _ := StringText(object); text != "abcd" {
		t.Fatalf("buffer = %q, want %q", text, "abcd")
	}
	if _, err := vm.InvokeVirtual(buffer, "insert", "(ILjava/lang/String;)Ljava/lang/StringBuffer;",
		IntValue(9), ReferenceValue(vm.NewString("x"))); err == nil {
		t.Fatal("an offset past the end was accepted")
	}
}

// Calendar.set moves the component get(I) reads back, and an out-of-range value
// normalizes the way the lenient mode a title relies on does.
func TestCalendarSetMovesTheInstant(t *testing.T) {
	vm := New(nil, Options{})
	calendar, err := vm.InvokeStatic(CalendarClass, "getInstance", "()Ljava/util/Calendar;")
	if err != nil {
		t.Fatal(err)
	}
	object, err := calendar.Reference()
	if err != nil {
		t.Fatal(err)
	}
	for _, set := range []struct{ field, value int32 }{{1, 2007}, {2, 0}, {5, 32}} {
		if _, err := vm.InvokeVirtual(object, "set", "(II)V", IntValue(set.field), IntValue(set.value)); err != nil {
			t.Fatal(err)
		}
	}
	for _, want := range []struct{ field, value int32 }{{1, 2007}, {2, 1}, {5, 1}} {
		result, err := vm.InvokeVirtual(object, "get", "(I)I", IntValue(want.field))
		if err != nil {
			t.Fatal(err)
		}
		if value, _ := result.Int32(); value != want.value {
			t.Fatalf("get(%d) = %d, want %d", want.field, value, want.value)
		}
	}
}

// A title asks for the identifiers and hands one straight back to getTimeZone,
// so that round trip is what has to hold. Anything else is GMT.
func TestTimeZoneAnswersItsOwnIdentifiers(t *testing.T) {
	vm := New(nil, Options{})
	result, err := vm.InvokeStatic(TimeZoneClass, "getAvailableIDs", "()[Ljava/lang/String;")
	if err != nil {
		t.Fatal(err)
	}
	array, err := result.Reference()
	if err != nil {
		t.Fatal(err)
	}
	_, values, err := ArraySnapshot(array)
	if err != nil {
		t.Fatal(err)
	}
	if len(values) == 0 {
		t.Fatal("no time zone identifiers were offered")
	}
	for _, value := range values {
		object, _ := value.Reference()
		name, _ := StringText(object)
		zone, zoneErr := vm.InvokeStatic(TimeZoneClass, "getTimeZone", "(Ljava/lang/String;)Ljava/util/TimeZone;", value)
		if zoneErr != nil {
			t.Fatal(zoneErr)
		}
		zoneObject, _ := zone.Reference()
		identifier, idErr := vm.InvokeVirtual(zoneObject, "getID", "()Ljava/lang/String;")
		if idErr != nil {
			t.Fatal(idErr)
		}
		got, _ := identifier.Reference()
		if text, _ := StringText(got); text != name {
			t.Fatalf("getTimeZone(%q).getID() = %q", name, text)
		}
	}
	unknown, err := vm.InvokeStatic(TimeZoneClass, "getTimeZone", "(Ljava/lang/String;)Ljava/util/TimeZone;",
		ReferenceValue(vm.NewString("Mars/Olympus")))
	if err != nil {
		t.Fatal(err)
	}
	object, _ := unknown.Reference()
	identifier, err := vm.InvokeVirtual(object, "getID", "()Ljava/lang/String;")
	if err != nil {
		t.Fatal(err)
	}
	got, _ := identifier.Reference()
	if text, _ := StringText(got); text != gmtTimeZoneID {
		t.Fatalf("an unknown identifier answered %q, want %q", text, gmtTimeZoneID)
	}
}

func TestHashtableAcceptsACapacityHint(t *testing.T) {
	vm := New(nil, Options{})
	table, err := vm.NewObject(HashtableClass, "(I)V", IntValue(16))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := vm.InvokeVirtual(table, "put", "(Ljava/lang/Object;Ljava/lang/Object;)Ljava/lang/Object;",
		ReferenceValue(vm.NewString("k")), ReferenceValue(vm.NewString("v"))); err != nil {
		t.Fatal(err)
	}
	view, err := vm.InvokeVirtual(table, "keys", "()Ljava/util/Enumeration;")
	if err != nil {
		t.Fatal(err)
	}
	object, _ := view.Reference()
	more, err := vm.InvokeVirtual(object, "hasMoreElements", "()Z")
	if err != nil {
		t.Fatal(err)
	}
	if value, _ := more.Int32(); value != 1 {
		t.Fatal("the key view of a table with one entry reported no elements")
	}
	if _, err := vm.NewObject(HashtableClass, "(I)V", IntValue(-1)); err == nil {
		t.Fatal("a negative capacity was accepted")
	}
}

func TestStringLastIndexOfSearchesBackFromAnIndex(t *testing.T) {
	vm := New(nil, Options{})
	text := vm.NewString("a/b/c")
	for _, probe := range []struct{ from, want int32 }{{4, 3}, {2, 1}, {0, -1}} {
		result, err := vm.InvokeVirtual(text, "lastIndexOf", "(II)I", IntValue('/'), IntValue(probe.from))
		if err != nil {
			t.Fatal(err)
		}
		if value, _ := result.Int32(); value != probe.want {
			t.Fatalf("lastIndexOf('/', %d) = %d, want %d", probe.from, value, probe.want)
		}
	}
}

// The root toString is what a title reaches through an append or a valueOf on
// an object whose class does not override it.
func TestObjectToStringNamesItsClass(t *testing.T) {
	vm := New(nil, Options{})
	object, err := vm.NewObject(VectorClass, "()V")
	if err != nil {
		t.Fatal(err)
	}
	result, err := vm.InvokeVirtual(object, "toString", "()Ljava/lang/String;")
	if err != nil {
		t.Fatal(err)
	}
	got, _ := result.Reference()
	text, _ := StringText(got)
	if len(text) <= len(VectorClass) || text[:len(VectorClass)] != VectorClass {
		t.Fatalf("toString() = %q, want it to start with %q", text, VectorClass)
	}
}
