package jvm

import (
	"strings"
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

// The boxed flag. A title puts one in a Vector and reads Boolean.TRUE to make
// it, and the class existed nowhere until it did: `new Boolean(true)` resolves
// the class before it can call anything, so the whole class was one stop.
func TestBooleanBoxesAFlagAndPublishesItsInstances(t *testing.T) {
	vm := New(nil, Options{})
	for _, probe := range []struct {
		value int32
		text  string
		hash  int32
	}{{1, "true", 1231}, {0, "false", 1237}} {
		object, err := vm.NewObject(BooleanClass, "(Z)V", IntValue(probe.value))
		if err != nil {
			t.Fatal(err)
		}
		flag, err := vm.InvokeVirtual(object, "booleanValue", "()Z")
		if err != nil {
			t.Fatal(err)
		}
		if value, _ := flag.Int32(); value != probe.value {
			t.Fatalf("booleanValue() = %d, want %d", value, probe.value)
		}
		text, err := vm.InvokeVirtual(object, "toString", "()Ljava/lang/String;")
		if err != nil {
			t.Fatal(err)
		}
		reference, _ := text.Reference()
		if got, _ := StringText(reference); got != probe.text {
			t.Fatalf("toString() = %q, want %q", got, probe.text)
		}
		// The hash is the specification's two constants rather than the value,
		// which is what a title hashing a flag into a Hashtable depends on.
		hash, err := vm.InvokeVirtual(object, "hashCode", "()I")
		if err != nil {
			t.Fatal(err)
		}
		if value, _ := hash.Int32(); value != probe.hash {
			t.Fatalf("hashCode() = %d, want %d", value, probe.hash)
		}
	}

	// TRUE and FALSE are the same object every read, because a title compares
	// a boxed flag against the field rather than calling equals.
	first, err := vm.StaticField(BooleanClass, "TRUE", "Ljava/lang/Boolean;")
	if err != nil {
		t.Fatal(err)
	}
	again, err := vm.StaticField(BooleanClass, "TRUE", "Ljava/lang/Boolean;")
	if err != nil {
		t.Fatal(err)
	}
	published, _ := first.Reference()
	repeated, _ := again.Reference()
	if published == nil || published != repeated {
		t.Fatalf("Boolean.TRUE read twice gave %p and %p", published, repeated)
	}
	value, err := vm.InvokeVirtual(published, "booleanValue", "()Z")
	if err != nil {
		t.Fatal(err)
	}
	if flag, _ := value.Int32(); flag != 1 {
		t.Fatalf("Boolean.TRUE.booleanValue() = %d, want 1", flag)
	}
	// A guest may pass any non-zero int for true, and two boxes of the same
	// flag have to compare equal however they were made.
	other, err := vm.NewObject(BooleanClass, "(Z)V", IntValue(7))
	if err != nil {
		t.Fatal(err)
	}
	equal, err := vm.InvokeVirtual(other, "equals", "(Ljava/lang/Object;)Z", ReferenceValue(published))
	if err != nil {
		t.Fatal(err)
	}
	if flag, _ := equal.Int32(); flag != 1 {
		t.Fatal("two boxes of true do not compare equal")
	}
}

// A title that keeps its text in a char array appends a window of it to a line
// rather than building a String in between.
func TestStringBufferAppendsACharArrayWindow(t *testing.T) {
	vm := New(nil, Options{})
	array, err := vm.InvokeVirtual(vm.NewString("WIPI 1.2"), "toCharArray", "()[C")
	if err != nil {
		t.Fatal(err)
	}
	characters, err := array.Reference()
	if err != nil {
		t.Fatal(err)
	}
	buffer, err := vm.NewObject(StringBufferClass, "()V")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := vm.InvokeVirtual(buffer, "append", "([CII)Ljava/lang/StringBuffer;", ReferenceValue(characters), IntValue(5), IntValue(3)); err != nil {
		t.Fatal(err)
	}
	if _, err := vm.InvokeVirtual(buffer, "append", "([C)Ljava/lang/StringBuffer;", ReferenceValue(characters)); err != nil {
		t.Fatal(err)
	}
	result, err := vm.InvokeVirtual(buffer, "toString", "()Ljava/lang/String;")
	if err != nil {
		t.Fatal(err)
	}
	text, _ := result.Reference()
	if got, _ := StringText(text); got != "1.2WIPI 1.2" {
		t.Fatalf("append([CII) then append([C) built %q", got)
	}
	// A window the array cannot hold is the caller's own error rather than a
	// silently shortened append.
	if _, err := vm.InvokeVirtual(buffer, "append", "([CII)Ljava/lang/StringBuffer;", ReferenceValue(characters), IntValue(6), IntValue(9)); err == nil {
		t.Fatal("an over-long window was appended")
	}
}

// Five StringBuffer members and four String members had working bodies and no
// declaration, so nothing could resolve them. These are the calls a title
// makes through the class rather than through a native dispatch.
func TestStringAndBufferMembersResolveThroughTheirClass(t *testing.T) {
	vm := New(nil, Options{})
	buffer, err := vm.NewObject(StringBufferClass, "(I)V", IntValue(16))
	if err != nil {
		t.Fatal(err)
	}
	for _, form := range []struct {
		descriptor string
		argument   Value
	}{
		{"(J)Ljava/lang/StringBuffer;", LongValue(12)},
		{"(Z)Ljava/lang/StringBuffer;", IntValue(1)},
	} {
		if _, err := vm.InvokeVirtual(buffer, "append", form.descriptor, form.argument); err != nil {
			t.Fatalf("append%s: %v", form.descriptor, err)
		}
	}
	if _, err := vm.InvokeVirtual(buffer, "append", "(Ljava/lang/Object;)Ljava/lang/StringBuffer;", ReferenceValue(vm.NewString("x"))); err != nil {
		t.Fatal(err)
	}
	if _, err := vm.InvokeVirtual(buffer, "setLength", "(I)V", IntValue(2)); err != nil {
		t.Fatal(err)
	}
	result, err := vm.InvokeVirtual(buffer, "toString", "()Ljava/lang/String;")
	if err != nil {
		t.Fatal(err)
	}
	text, _ := result.Reference()
	if got, _ := StringText(text); got != "12" {
		t.Fatalf("the buffer holds %q, want %q", got, "12")
	}

	replaced, err := vm.InvokeVirtual(vm.NewString("a.b.c"), "replace", "(CC)Ljava/lang/String;", IntValue('.'), IntValue('/'))
	if err != nil {
		t.Fatal(err)
	}
	object, _ := replaced.Reference()
	if got, _ := StringText(object); got != "a/b/c" {
		t.Fatalf("replace('.', '/') = %q", got)
	}
}

// The four fields the specification declares protected on a byte source carry
// the specification's own names, because a title subclasses the stream and
// reads `buf` rather than copying the array back out of it.
func TestByteArrayInputStreamPublishesTheSpecifiedFieldNames(t *testing.T) {
	vm := New(nil, Options{})
	array := NewByteArray([]byte{1, 2, 3, 4, 5})
	stream, err := vm.NewObject(ByteArrayInputStreamClass, "([BII)V", ReferenceValue(array), IntValue(1), IntValue(3))
	if err != nil {
		t.Fatal(err)
	}
	buffer, err := vm.Field(stream, ByteArrayInputStreamClass, "buf", "[B")
	if err != nil {
		t.Fatal(err)
	}
	if held, _ := buffer.Reference(); held != array {
		t.Fatal("buf does not name the array the stream was opened on")
	}
	if _, err := vm.InvokeVirtual(stream, "read", "()I"); err != nil {
		t.Fatal(err)
	}
	for _, field := range []struct {
		name string
		want int32
	}{{"pos", 2}, {"count", 4}, {"mark", 1}} {
		value, err := vm.Field(stream, ByteArrayInputStreamClass, field.name, "I")
		if err != nil {
			t.Fatalf("%s: %v", field.name, err)
		}
		if held, _ := value.Int32(); held != field.want {
			t.Fatalf("%s = %d, want %d", field.name, held, field.want)
		}
	}
}

// The long form of valueOf. Five KTF archives name it in their client image's
// pool — a score, a coin count, a clock — and this library had every other
// form of it, so each of those five would have stopped at the call rather than
// at anything to do with the number.
func TestStringValueOfFormatsALong(t *testing.T) {
	vm := New(nil, Options{})
	for _, probe := range []struct {
		value int64
		want  string
	}{
		{0, "0"},
		{1234567890123, "1234567890123"},
		{-9223372036854775808, "-9223372036854775808"},
	} {
		result, err := vm.InvokeStatic(StringClass, "valueOf", "(J)Ljava/lang/String;", LongValue(probe.value))
		if err != nil {
			t.Fatalf("valueOf(%d): %v", probe.value, err)
		}
		object, err := result.Reference()
		if err != nil {
			t.Fatal(err)
		}
		if text, _ := StringText(object); text != probe.want {
			t.Fatalf("valueOf(%d) = %q, want %q", probe.value, text, probe.want)
		}
	}
}

// A title that builds a path by appending into a StringBuffer and then calling
// String.valueOf on the buffer gets the text it built, not the buffer's name.
// Both forms of the object argument answer through the object's own toString,
// and a class that does not override it still answers its identity — which is
// what keeps the dispatch off the path every other caller takes.
func TestObjectFormsAskForTheObjectsOwnText(t *testing.T) {
	vm := New(nil, Options{})
	buffer, err := vm.NewObject(StringBufferClass, "(Ljava/lang/String;)V", ReferenceValue(vm.NewString("/image/i_intro_")))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := vm.InvokeVirtual(buffer, "append", "(I)Ljava/lang/StringBuffer;", IntValue(0)); err != nil {
		t.Fatal(err)
	}
	if _, err := vm.InvokeVirtual(buffer, "append", "(Ljava/lang/String;)Ljava/lang/StringBuffer;", ReferenceValue(vm.NewString(".png"))); err != nil {
		t.Fatal(err)
	}

	result, err := vm.InvokeStatic(StringClass, "valueOf", "(Ljava/lang/Object;)Ljava/lang/String;", ReferenceValue(buffer))
	if err != nil {
		t.Fatal(err)
	}
	got, _ := result.Reference()
	if text, _ := StringText(got); text != "/image/i_intro_0.png" {
		t.Fatalf("String.valueOf(StringBuffer) = %q, want the text the buffer holds", text)
	}

	appended, err := vm.NewObject(StringBufferClass, "()V")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := vm.InvokeVirtual(appended, "append", "(Ljava/lang/Object;)Ljava/lang/StringBuffer;", ReferenceValue(buffer)); err != nil {
		t.Fatal(err)
	}
	result, err = vm.InvokeVirtual(appended, "toString", "()Ljava/lang/String;")
	if err != nil {
		t.Fatal(err)
	}
	got, _ = result.Reference()
	if text, _ := StringText(got); text != "/image/i_intro_0.png" {
		t.Fatalf("StringBuffer.append(StringBuffer) built %q", text)
	}

	// A class with no toString of its own is still named rather than asked, so
	// nothing is invoked for the case every other caller in the corpus takes.
	plain, err := vm.NewObject(VectorClass, "()V")
	if err != nil {
		t.Fatal(err)
	}
	result, err = vm.InvokeStatic(StringClass, "valueOf", "(Ljava/lang/Object;)Ljava/lang/String;", ReferenceValue(plain))
	if err != nil {
		t.Fatal(err)
	}
	got, _ = result.Reference()
	text, _ := StringText(got)
	if !strings.HasPrefix(text, VectorClass+"@") {
		t.Fatalf("String.valueOf(Vector) = %q, want its identity", text)
	}
}

// The members an SKT corpus of ninety archives named and this runtime did not
// have. Each is here because a title links against it; two of them are what a
// title stopped on.
func TestStringBufferDeletesAndCopiesByIndex(t *testing.T) {
	vm := New(nil, Options{})
	buffer, err := vm.NewObject(StringBufferClass, "(Ljava/lang/String;)V", ReferenceValue(vm.NewString("abcd")))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := vm.InvokeVirtual(buffer, "deleteCharAt", "(I)Ljava/lang/StringBuffer;", IntValue(1)); err != nil {
		t.Fatal(err)
	}
	result, err := vm.InvokeVirtual(buffer, "toString", "()Ljava/lang/String;")
	if err != nil {
		t.Fatal(err)
	}
	got, _ := result.Reference()
	if text, _ := StringText(got); text != "acd" {
		t.Fatalf("after deleteCharAt(1) the buffer holds %q, want \"acd\"", text)
	}
	// The index the ranged form clamps is the one this one refuses.
	if _, err := vm.InvokeVirtual(buffer, "deleteCharAt", "(I)Ljava/lang/StringBuffer;", IntValue(3)); !vm.IsGuestException(err, "java/lang/StringIndexOutOfBoundsException") {
		t.Fatalf("deleteCharAt at the length answered %v", err)
	}

	destination, err := vm.InvokeVirtual(vm.NewString("....."), "toCharArray", "()[C")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := vm.InvokeVirtual(buffer, "getChars", "(II[CI)V", IntValue(1), IntValue(3), destination, IntValue(2)); err != nil {
		t.Fatal(err)
	}
	characters, err := destination.Reference()
	if err != nil {
		t.Fatal(err)
	}
	array, err := guestArray(characters)
	if err != nil {
		t.Fatal(err)
	}
	for index, want := range []int32{'.', '.', 'c', 'd', '.'} {
		element, err := array.Load(index)
		if err != nil {
			t.Fatal(err)
		}
		if value, _ := element.Int32(); value != want {
			t.Fatalf("getChars wrote %d at %d, want %d", value, index, want)
		}
	}
}

// Byte.toString(byte) is the static form, and the sign matters: a title
// rendering a table of small negative numbers would print 255 for -1 if the
// argument were read as the unsigned byte it arrives in.
func TestByteToStringKeepsTheSign(t *testing.T) {
	vm := New(nil, Options{})
	for _, probe := range []struct {
		value int32
		want  string
	}{{7, "7"}, {-1, "-1"}, {-128, "-128"}, {127, "127"}} {
		result, err := vm.InvokeStatic(ByteClass, "toString", "(B)Ljava/lang/String;", IntValue(probe.value))
		if err != nil {
			t.Fatal(err)
		}
		got, _ := result.Reference()
		if text, _ := StringText(got); text != probe.want {
			t.Fatalf("Byte.toString(%d) = %q, want %q", probe.value, text, probe.want)
		}
	}
}

// A title guarding a large allocation catches OutOfMemoryError by name, so the
// class and its two parents have to resolve even though nothing raises it.
func TestOutOfMemoryErrorResolvesUnderError(t *testing.T) {
	vm := New(nil, Options{})
	for _, probe := range []struct{ class, parent string }{
		{"java/lang/OutOfMemoryError", "java/lang/VirtualMachineError"},
		{"java/lang/OutOfMemoryError", "java/lang/Error"},
		{"java/lang/OutOfMemoryError", ThrowableClass},
	} {
		subclass, err := vm.IsSubclassOf(probe.class, probe.parent)
		if err != nil {
			t.Fatalf("IsSubclassOf(%s, %s) error = %v", probe.class, probe.parent, err)
		}
		if !subclass {
			t.Fatalf("%s is not a %s", probe.class, probe.parent)
		}
	}
	if _, err := vm.NewObject("java/lang/OutOfMemoryError", "(Ljava/lang/String;)V", ReferenceValue(vm.NewString("out"))); err != nil {
		t.Fatalf("new OutOfMemoryError(String) error = %v", err)
	}
}

// setTimeZone is the other way a title names a zone: it takes the default
// calendar and moves it, rather than asking the factory for one. The instant
// does not move — what changes is which fields get(I) breaks it into.
func TestCalendarSetTimeZoneMovesTheFieldsAndNotTheInstant(t *testing.T) {
	const instant = int64(1_600_000_000_000)
	vm := New(nil, Options{Clock: func() int64 { return instant }})
	calendarValue, err := vm.InvokeStatic(CalendarClass, "getInstance", "()Ljava/util/Calendar;")
	if err != nil {
		t.Fatal(err)
	}
	calendar, err := calendarValue.Reference()
	if err != nil {
		t.Fatal(err)
	}
	gmt, err := vm.InvokeStatic(TimeZoneClass, "getTimeZone", "(Ljava/lang/String;)Ljava/util/TimeZone;",
		ReferenceValue(vm.NewString("GMT")))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := vm.InvokeVirtual(calendar, "setTimeZone", "(Ljava/util/TimeZone;)V", gmt); err != nil {
		t.Fatal(err)
	}
	// 2020-09-13T12:26:40Z, whatever zone the machine running this stands in.
	hour, err := vm.InvokeVirtual(calendar, "get", "(I)I", IntValue(11))
	if err != nil {
		t.Fatal(err)
	}
	if value, _ := hour.Int32(); value != 12 {
		t.Fatalf("after setTimeZone(GMT), get(HOUR_OF_DAY) = %d, want 12", value)
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
		t.Fatalf("setTimeZone moved the instant to %d, want %d", value, instant)
	}
	if _, err := vm.InvokeVirtual(calendar, "setTimeZone", "(Ljava/util/TimeZone;)V", ReferenceValue(nil)); !vm.IsGuestException(err, "java/lang/NullPointerException") {
		t.Fatalf("setTimeZone(null) answered %v", err)
	}
}

// A title counts its own workers before starting another, and the count has to
// include the thread the count is taken on.
func TestThreadActiveCountCountsTheRunningThreads(t *testing.T) {
	vm := New(nil, Options{})
	result, err := vm.InvokeStatic(ThreadClass, "activeCount", "()I")
	if err != nil {
		t.Fatal(err)
	}
	if value, _ := result.Int32(); value < 1 {
		t.Fatalf("activeCount() = %d with a machine running, want at least 1", value)
	}
}
