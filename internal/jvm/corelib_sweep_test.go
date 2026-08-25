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

// A stream over part of an array stops at the end of its window rather than at
// the end of the array. A title that keeps several records in one buffer reads
// each of them this way, and a window that ran to the array's end would hand
// it the next record's bytes.
func TestByteArrayInputStreamReadsOnlyItsWindow(t *testing.T) {
	vm := New(nil, Options{})
	array := NewByteArray([]byte{0, 1, 2, 3, 4, 5})
	stream, err := vm.NewObject(ByteArrayInputStreamClass, "([BII)V", ReferenceValue(array), IntValue(2), IntValue(3))
	if err != nil {
		t.Fatal(err)
	}
	available, err := vm.InvokeVirtual(stream, "available", "()I")
	if err != nil {
		t.Fatal(err)
	}
	if value, _ := available.Int32(); value != 3 {
		t.Fatalf("available() = %d, want 3", value)
	}
	for _, want := range []int32{2, 3, 4, -1} {
		result, err := vm.InvokeVirtual(stream, "read", "()I")
		if err != nil {
			t.Fatal(err)
		}
		if value, _ := result.Int32(); value != want {
			t.Fatalf("read() = %d, want %d", value, want)
		}
	}
}

// A window longer than the array it is opened on is clamped rather than
// refused, which is what the specification does and what a title that asks for
// "the rest of it" with a generous length depends on.
func TestByteArrayInputStreamClampsAnOverlongWindow(t *testing.T) {
	vm := New(nil, Options{})
	array := NewByteArray([]byte{7, 8})
	stream, err := vm.NewObject(ByteArrayInputStreamClass, "([BII)V", ReferenceValue(array), IntValue(1), IntValue(999))
	if err != nil {
		t.Fatal(err)
	}
	skipped, err := vm.InvokeVirtual(stream, "skip", "(J)J", LongValue(100))
	if err != nil {
		t.Fatal(err)
	}
	if value, _ := skipped.Int64(); value != 1 {
		t.Fatalf("skip(100) = %d, want 1", value)
	}
}

// The whole-array constructor still reaches the end of the array: the window
// it opens is the array, and nothing about the field it now sets changes that.
func TestByteArrayInputStreamOverAWholeArrayIsUnchanged(t *testing.T) {
	vm := New(nil, Options{})
	array := NewByteArray([]byte{9, 9, 9})
	stream, err := vm.NewObject(ByteArrayInputStreamClass, "([B)V", ReferenceValue(array))
	if err != nil {
		t.Fatal(err)
	}
	available, err := vm.InvokeVirtual(stream, "available", "()I")
	if err != nil {
		t.Fatal(err)
	}
	if value, _ := available.Int32(); value != 3 {
		t.Fatalf("available() = %d, want 3", value)
	}
}

// A calendar asked for GMT breaks the same instant into different fields from
// one asked for the handset's own zone. The two agree on the instant, which is
// what getTime answers, and that is the whole of what a zone changes here.
func TestCalendarInAZoneReadsTheSameInstantDifferently(t *testing.T) {
	// A fixed clock, so the test does not depend on when it runs. The instant
	// is chosen away from midnight in both zones so the hour comparison below
	// cannot land on the same number by accident.
	const instant = int64(1_600_000_000_000)
	vm := New(nil, Options{Clock: func() int64 { return instant }})
	gmt, err := vm.InvokeStatic(TimeZoneClass, "getTimeZone", "(Ljava/lang/String;)Ljava/util/TimeZone;",
		ReferenceValue(vm.NewString("GMT")))
	if err != nil {
		t.Fatal(err)
	}
	zoned, err := vm.InvokeStatic("java/util/Calendar", "getInstance", "(Ljava/util/TimeZone;)Ljava/util/Calendar;", gmt)
	if err != nil {
		t.Fatal(err)
	}
	calendar, err := zoned.Reference()
	if err != nil {
		t.Fatal(err)
	}
	moment, err := vm.InvokeVirtual(calendar, "getTime", "()Ljava/util/Date;")
	if err != nil {
		t.Fatal(err)
	}
	date, err := moment.Reference()
	if err != nil {
		t.Fatal(err)
	}
	millis, err := vm.InvokeVirtual(date, "getTime", "()J")
	if err != nil {
		t.Fatal(err)
	}
	if value, _ := millis.Int64(); value != instant {
		t.Fatalf("getInstance(GMT).getTime() = %d, want %d", value, instant)
	}
	// 2020-09-13T12:26:40Z: the hour in GMT is fixed whatever zone the test
	// machine runs in, which the no-argument form's hour is not.
	hour, err := vm.InvokeVirtual(calendar, "get", "(I)I", IntValue(11))
	if err != nil {
		t.Fatal(err)
	}
	if value, _ := hour.Int32(); value != 12 {
		t.Fatalf("getInstance(GMT).get(HOUR_OF_DAY) = %d, want 12", value)
	}
}

// Setting a stored date on a zoned calendar keeps the zone: a title that asked
// for GMT still means GMT when it reads the fields of the date it just set.
func TestCalendarKeepsItsZoneAcrossSetTime(t *testing.T) {
	const instant = int64(1_600_000_000_000)
	vm := New(nil, Options{Clock: func() int64 { return 0 }})
	gmt, err := vm.InvokeStatic(TimeZoneClass, "getTimeZone", "(Ljava/lang/String;)Ljava/util/TimeZone;",
		ReferenceValue(vm.NewString("GMT")))
	if err != nil {
		t.Fatal(err)
	}
	zoned, err := vm.InvokeStatic("java/util/Calendar", "getInstance", "(Ljava/util/TimeZone;)Ljava/util/Calendar;", gmt)
	if err != nil {
		t.Fatal(err)
	}
	calendar, _ := zoned.Reference()
	date, err := vm.NewObject(DateClass, "(J)V", LongValue(instant))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := vm.InvokeVirtual(calendar, "setTime", "(Ljava/util/Date;)V", ReferenceValue(date)); err != nil {
		t.Fatal(err)
	}
	hour, err := vm.InvokeVirtual(calendar, "get", "(I)I", IntValue(11))
	if err != nil {
		t.Fatal(err)
	}
	if value, _ := hour.Int32(); value != 12 {
		t.Fatalf("after setTime, get(HOUR_OF_DAY) = %d, want 12", value)
	}
}

// A buffer edited one character at a time. The bodies were already here; what
// was missing was the declaration, so this is a test that the pair is
// reachable as much as that it is right.
func TestStringBufferEditsOneCharacter(t *testing.T) {
	vm := New(nil, Options{})
	buffer, err := vm.NewObject(StringBufferClass, "(Ljava/lang/String;)V", ReferenceValue(vm.NewString("cat")))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := vm.InvokeVirtual(buffer, "setCharAt", "(IC)V", IntValue(1), IntValue('u')); err != nil {
		t.Fatal(err)
	}
	character, err := vm.InvokeVirtual(buffer, "charAt", "(I)C", IntValue(1))
	if err != nil {
		t.Fatal(err)
	}
	if value, _ := character.Int32(); value != 'u' {
		t.Fatalf("charAt(1) = %d, want %d", value, 'u')
	}
	result, err := vm.InvokeVirtual(buffer, "toString", "()Ljava/lang/String;")
	if err != nil {
		t.Fatal(err)
	}
	object, _ := result.Reference()
	if text, _ := StringText(object); text != "cut" {
		t.Fatalf("toString() = %q, want %q", text, "cut")
	}
}

// A title walks a vector with the Enumeration CLDC gives it rather than with
// an index, and the view is a snapshot: adding to the vector afterwards does
// not change what the walk hands back.
func TestVectorElementsWalksASnapshot(t *testing.T) {
	vm := New(nil, Options{})
	vector, err := vm.NewObject(VectorClass, "()V")
	if err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{"a", "b"} {
		if _, err := vm.InvokeVirtual(vector, "addElement", "(Ljava/lang/Object;)V",
			ReferenceValue(vm.NewString(text))); err != nil {
			t.Fatal(err)
		}
	}
	value, err := vm.InvokeVirtual(vector, "elements", "()Ljava/util/Enumeration;")
	if err != nil {
		t.Fatal(err)
	}
	view, err := value.Reference()
	if err != nil {
		t.Fatal(err)
	}
	// Adding after the view was taken must not reach it.
	if _, err := vm.InvokeVirtual(vector, "addElement", "(Ljava/lang/Object;)V",
		ReferenceValue(vm.NewString("c"))); err != nil {
		t.Fatal(err)
	}
	var walked []string
	for {
		more, err := vm.InvokeVirtual(view, "hasMoreElements", "()Z")
		if err != nil {
			t.Fatal(err)
		}
		if flag, _ := more.Int32(); flag == 0 {
			break
		}
		next, err := vm.InvokeVirtual(view, "nextElement", "()Ljava/lang/Object;")
		if err != nil {
			t.Fatal(err)
		}
		object, _ := next.Reference()
		text, _ := StringText(object)
		walked = append(walked, text)
	}
	if len(walked) != 2 || walked[0] != "a" || walked[1] != "b" {
		t.Fatalf("the enumeration walked %v, want [a b]", walked)
	}
}

// A stream over an array can always be read twice. A title reads a header out
// of a resource, resets, and hands the same bytes to its own decoder; when the
// abstract superclass answered the reset the title caught an IOException its
// decoder never raised, kept a null image, and painted it.
func TestByteArrayInputStreamResetsToTheMark(t *testing.T) {
	vm := New(nil, Options{})
	array := NewByteArray([]byte{1, 2, 3, 4})
	stream, err := vm.NewObject(ByteArrayInputStreamClass, "([B)V", ReferenceValue(array))
	if err != nil {
		t.Fatal(err)
	}
	supported, err := vm.InvokeVirtual(stream, "markSupported", "()Z")
	if err != nil {
		t.Fatal(err)
	}
	if value, _ := supported.Int32(); value != 1 {
		t.Fatalf("markSupported() = %d, want 1", value)
	}
	if _, err := vm.InvokeVirtual(stream, "read", "()I"); err != nil {
		t.Fatal(err)
	}
	// Reset without a mark goes back to where reading started, which is the
	// case a title that never marks depends on.
	if _, err := vm.InvokeVirtual(stream, "reset", "()V"); err != nil {
		t.Fatal(err)
	}
	first, err := vm.InvokeVirtual(stream, "read", "()I")
	if err != nil {
		t.Fatal(err)
	}
	if value, _ := first.Int32(); value != 1 {
		t.Fatalf("read() after reset = %d, want 1", value)
	}
	if _, err := vm.InvokeVirtual(stream, "mark", "(I)V", IntValue(0)); err != nil {
		t.Fatal(err)
	}
	if _, err := vm.InvokeVirtual(stream, "read", "()I"); err != nil {
		t.Fatal(err)
	}
	if _, err := vm.InvokeVirtual(stream, "reset", "()V"); err != nil {
		t.Fatal(err)
	}
	marked, err := vm.InvokeVirtual(stream, "read", "()I")
	if err != nil {
		t.Fatal(err)
	}
	if value, _ := marked.Int32(); value != 2 {
		t.Fatalf("read() after marked reset = %d, want 2", value)
	}
}

// A windowed stream resets to the start of its window, not to the start of the
// array underneath it. The class documentation says otherwise, and a record
// reader that believed it would walk into the record before its own.
func TestByteArrayInputStreamResetsToTheStartOfItsWindow(t *testing.T) {
	vm := New(nil, Options{})
	array := NewByteArray([]byte{0, 1, 2, 3, 4, 5})
	stream, err := vm.NewObject(ByteArrayInputStreamClass, "([BII)V", ReferenceValue(array), IntValue(2), IntValue(3))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := vm.InvokeVirtual(stream, "read", "()I"); err != nil {
		t.Fatal(err)
	}
	if _, err := vm.InvokeVirtual(stream, "reset", "()V"); err != nil {
		t.Fatal(err)
	}
	result, err := vm.InvokeVirtual(stream, "read", "()I")
	if err != nil {
		t.Fatal(err)
	}
	if value, _ := result.Int32(); value != 2 {
		t.Fatalf("read() after reset = %d, want 2", value)
	}
}

// The wrapper hands mark and reset to what it wraps. A title that decodes
// through the wrapper and resets the same object it decodes with reaches the
// stream underneath rather than the abstract superclass.
func TestDataInputStreamResetsThroughToItsSource(t *testing.T) {
	vm := New(nil, Options{})
	array := NewByteArray([]byte{9, 8, 7})
	source, err := vm.NewObject(ByteArrayInputStreamClass, "([B)V", ReferenceValue(array))
	if err != nil {
		t.Fatal(err)
	}
	stream, err := vm.NewObject(DataInputStreamClass, "(Ljava/io/InputStream;)V", ReferenceValue(source))
	if err != nil {
		t.Fatal(err)
	}
	supported, err := vm.InvokeVirtual(stream, "markSupported", "()Z")
	if err != nil {
		t.Fatal(err)
	}
	if value, _ := supported.Int32(); value != 1 {
		t.Fatalf("markSupported() = %d, want 1", value)
	}
	if _, err := vm.InvokeVirtual(stream, "readByte", "()B"); err != nil {
		t.Fatal(err)
	}
	if _, err := vm.InvokeVirtual(stream, "reset", "()V"); err != nil {
		t.Fatal(err)
	}
	result, err := vm.InvokeVirtual(stream, "readByte", "()B")
	if err != nil {
		t.Fatal(err)
	}
	if value, _ := result.Int32(); value != 9 {
		t.Fatalf("readByte() after reset = %d, want 9", value)
	}
}
