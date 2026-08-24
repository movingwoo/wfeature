package lgt

import "testing"

// The class-library members a sweep of a local archive set reached for and this
// platform did not have. Each is here because a title stopped on it, and each
// slot it is registered at is placed by the numbering rule in docs/lgt.md and
// then checked against what its own call site passes.

// newTestString is a String this platform built, which is the only kind any of
// these methods will take.
func newTestObject(t *testing.T, client *Client, name string) (uint32, error) {
	t.Helper()
	class, err := client.preparePlatformJavaClass(name)
	if err != nil {
		return 0, err
	}
	return client.allocateJavaObject(class)
}

func newTestString(t *testing.T, client *Client, text string) uint32 {
	t.Helper()
	object, err := client.newJavaString(text)
	if err != nil {
		t.Fatal(err)
	}
	return object
}

func TestJavaStringComparesByCodeUnit(t *testing.T) {
	client := fixtureClient(t)
	for _, probe := range []struct {
		left, right string
		negative    bool
		zero        bool
	}{
		{"abc", "abc", false, true},
		{"abc", "abd", true, false},
		{"ab", "abc", true, false},
		{"abd", "abc", false, false},
	} {
		result, err := javaStringComparison(client, nil, nil, []uint32{
			newTestString(t, client, probe.left), newTestString(t, client, probe.right),
		})
		if err != nil {
			t.Fatal(err)
		}
		value := int32(result)
		switch {
		case probe.zero && value != 0:
			t.Errorf("compareTo(%q, %q) = %d, want 0", probe.left, probe.right, value)
		case !probe.zero && probe.negative && value >= 0:
			t.Errorf("compareTo(%q, %q) = %d, want negative", probe.left, probe.right, value)
		case !probe.zero && !probe.negative && value <= 0:
			t.Errorf("compareTo(%q, %q) = %d, want positive", probe.left, probe.right, value)
		}
	}
}

func TestJavaStringIndexOfTextFromCountsCharacters(t *testing.T) {
	client := fixtureClient(t)
	// A two-byte character before the match, so an index counted in bytes and
	// one counted the way Java counts differ.
	held := newTestString(t, client, "가나다a나")
	needle := newTestString(t, client, "나")
	first, err := javaStringIndexOfTextFrom(client, nil, nil, []uint32{held, needle, 0})
	if err != nil {
		t.Fatal(err)
	}
	if int32(first) != 1 {
		t.Fatalf("indexOf from 0 = %d, want 1", int32(first))
	}
	next, err := javaStringIndexOfTextFrom(client, nil, nil, []uint32{held, needle, 2})
	if err != nil {
		t.Fatal(err)
	}
	if int32(next) != 4 {
		t.Fatalf("indexOf from 2 = %d, want 4", int32(next))
	}
	missing, err := javaStringIndexOfTextFrom(client, nil, nil, []uint32{
		held, newTestString(t, client, "zz"), 0,
	})
	if err != nil {
		t.Fatal(err)
	}
	if int32(missing) != -1 {
		t.Fatalf("indexOf of an absent needle = %d, want -1", int32(missing))
	}
}

// getBytes and the String byte constructor are one round trip: a title stores a
// name with the first and reads it back with the second, so the two have to
// agree on the handset's encoding rather than on Go's.
func TestJavaStringBytesRoundTripThroughTheDecoder(t *testing.T) {
	client := fixtureClient(t)
	const text = "가나다ABC"
	array, err := javaStringBytes(client, nil, nil, []uint32{newTestString(t, client, text)})
	if err != nil {
		t.Fatal(err)
	}
	data, err := client.readJavaArrayBytes(array)
	if err != nil {
		t.Fatal(err)
	}
	if decoded := decodeEUCKR(data); decoded != text {
		t.Fatalf("getBytes then decode = %q, want %q", decoded, text)
	}
}

func TestJavaStringPrefixAndConcat(t *testing.T) {
	client := fixtureClient(t)
	held := newTestString(t, client, "menu/main.png")
	yes, err := javaStringStartsWith(client, nil, nil, []uint32{held, newTestString(t, client, "menu/")})
	if err != nil {
		t.Fatal(err)
	}
	no, err := javaStringStartsWith(client, nil, nil, []uint32{held, newTestString(t, client, "data/")})
	if err != nil {
		t.Fatal(err)
	}
	if yes != 1 || no != 0 {
		t.Fatalf("startsWith answered %d and %d, want 1 and 0", yes, no)
	}
	joined, err := javaStringConcat(client, nil, nil, []uint32{
		newTestString(t, client, "menu/"), newTestString(t, client, "main.png"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if text, _ := client.javaText(joined); text != "menu/main.png" {
		t.Fatalf("concat = %q, want %q", text, "menu/main.png")
	}
	// Nothing to add is the receiver back, which is one allocation less for a
	// title concatenating in a loop.
	same, err := javaStringConcat(client, nil, nil, []uint32{held, newTestString(t, client, "")})
	if err != nil {
		t.Fatal(err)
	}
	if same != held {
		t.Fatalf("concat of nothing built %#x, want the receiver %#x", same, held)
	}
}

// A buffer emptied for the next line, which is what the call site passes: a
// zero and the length it has just measured.
func TestJavaBufferDeleteEmptiesAndClamps(t *testing.T) {
	client := fixtureClient(t)
	buffer := newTestString(t, client, "one two")
	if _, err := javaBufferDelete(client, nil, nil, []uint32{buffer, 0, 4}); err != nil {
		t.Fatal(err)
	}
	if text, _ := client.javaText(buffer); text != "two" {
		t.Fatalf("delete(0, 4) left %q, want %q", text, "two")
	}
	// An end past what the buffer holds is clamped rather than refused.
	if _, err := javaBufferDelete(client, nil, nil, []uint32{buffer, 0, 400}); err != nil {
		t.Fatal(err)
	}
	if text, _ := client.javaText(buffer); text != "" {
		t.Fatalf("delete(0, 400) left %q, want it empty", text)
	}
}

// append(Object) has no toString to call on a guest object, so it names the
// object by its class — which is what the one shape reaching it locally, an
// exception written into a line, is for.
func TestJavaBufferAppendObjectNamesWhatItCannotRead(t *testing.T) {
	client := fixtureClient(t)
	buffer := newTestString(t, client, "failed: ")
	if _, err := javaBufferAppendObject(client, nil, nil, []uint32{buffer, 0}); err != nil {
		t.Fatal(err)
	}
	if text, _ := client.javaText(buffer); text != "failed: null" {
		t.Fatalf("append(null) left %q", text)
	}
	inner := newTestString(t, client, "why")
	if _, err := javaBufferAppendObject(client, nil, nil, []uint32{buffer, inner}); err != nil {
		t.Fatal(err)
	}
	if text, _ := client.javaText(buffer); text != "failed: nullwhy" {
		t.Fatalf("append of a String left %q", text)
	}
}

// A record written with the data stream's writes and read back with the data
// stream's reads. The pair is one round trip, so the test is the round trip.
func TestJavaByteSinkRoundTripsThroughTheDataStreams(t *testing.T) {
	client := fixtureClient(t)
	sink, err := newTestObject(t, client, javaByteSinkClass)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := javaByteSinkConstructor(client, nil, nil, []uint32{sink}); err != nil {
		t.Fatal(err)
	}
	output, err := newTestObject(t, client, javaDataOutputStreamClass)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := javaWrapSink(client, nil, nil, []uint32{output, sink}); err != nil {
		t.Fatal(err)
	}
	// A byte, a halfword and a word, written through the wrapper.
	if _, err := javaSinkAppend(1)(client, nil, nil, []uint32{output, 0x7f}); err != nil {
		t.Fatal(err)
	}
	if _, err := javaSinkAppend(2)(client, nil, nil, []uint32{output, 0x1234}); err != nil {
		t.Fatal(err)
	}
	if _, err := javaSinkAppend(4)(client, nil, nil, []uint32{output, 0xdeadbeef}); err != nil {
		t.Fatal(err)
	}
	if _, err := javaSinkWriteUTF(client, nil, nil, []uint32{output, newTestString(t, client, "가A")}); err != nil {
		t.Fatal(err)
	}
	// The sink underneath saw everything the wrapper wrote.
	size, err := javaByteSinkSize(client, nil, nil, []uint32{sink})
	if err != nil {
		t.Fatal(err)
	}
	if size != 1+2+4+2+4 {
		t.Fatalf("the sink holds %d bytes, want %d", size, 1+2+4+2+4)
	}
	array, err := javaByteSinkBytes(client, nil, nil, []uint32{sink})
	if err != nil {
		t.Fatal(err)
	}
	data, err := client.readJavaArrayBytes(array)
	if err != nil {
		t.Fatal(err)
	}
	want := []byte{0x7f, 0x12, 0x34, 0xde, 0xad, 0xbe, 0xef, 0x00, 0x04, 0xea, 0xb0, 0x80, 'A'}
	if len(data) != len(want) {
		t.Fatalf("the sink holds %d bytes, want %d", len(data), len(want))
	}
	for index := range want {
		if data[index] != want[index] {
			t.Fatalf("byte %d is %#x, want %#x", index, data[index], want[index])
		}
	}
	// reset empties it and it is used again, which is how a title writes one
	// record after another without building a sink per record.
	if _, err := javaByteSinkReset(client, nil, nil, []uint32{output}); err != nil {
		t.Fatal(err)
	}
	if size, err := javaByteSinkSize(client, nil, nil, []uint32{sink}); err != nil || size != 0 {
		t.Fatalf("after reset the sink holds %d bytes (%v), want none", size, err)
	}
}

// The date a title shows, broken into the components get(int) names. The clock
// behind it is the one every other call on this platform reads.
func TestJavaCalendarBreaksTheClockIntoFields(t *testing.T) {
	client := fixtureClient(t)
	calendar, err := javaCalendarGetInstance(client, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	moment, err := client.javaCalendarOf(calendar)
	if err != nil {
		t.Fatal(err)
	}
	for _, probe := range []struct {
		field int
		want  int32
	}{
		{javaCalendarYear, int32(moment.Year())},
		{javaCalendarMonth, int32(moment.Month()) - 1},
		{javaCalendarDate, int32(moment.Day())},
		{javaCalendarDayOfWeek, int32(moment.Weekday()) + 1},
		{javaCalendarHourOfDay, int32(moment.Hour())},
		{javaCalendarMinute, int32(moment.Minute())},
	} {
		value, err := javaCalendarGet(client, nil, nil, []uint32{calendar, uint32(probe.field)})
		if err != nil {
			t.Fatal(err)
		}
		if int32(value) != probe.want {
			t.Errorf("get(%d) = %d, want %d", probe.field, int32(value), probe.want)
		}
	}
	if _, err := javaCalendarGet(client, nil, nil, []uint32{calendar, 99}); err == nil {
		t.Error("get of a field this platform does not know answered instead of reporting")
	}
}

// A Stack is a Vector, so its own methods reach Vector's slots. Nothing here
// says more than that the constructor opens a list the Vector calls will take.
func TestJavaStackIsAVector(t *testing.T) {
	client := fixtureClient(t)
	stack, err := newTestObject(t, client, "java/util/Stack")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := javaVectorConstructor(client, nil, nil, []uint32{stack}); err != nil {
		t.Fatal(err)
	}
	if _, err := javaVectorAdd(client, nil, nil, []uint32{stack, 0x1234}); err != nil {
		t.Fatal(err)
	}
	size, err := javaVectorSize(client, nil, nil, []uint32{stack})
	if err != nil {
		t.Fatal(err)
	}
	if size != 1 {
		t.Fatalf("the stack holds %d, want 1", size)
	}
	if javaPlatformSupers["java/util/Stack"] != javaVectorClass {
		t.Fatalf("a Stack extends %q, want %q", javaPlatformSupers["java/util/Stack"], javaVectorClass)
	}
}

// insertElementAt puts an element in at an index rather than on the end, and
// the index the call site passes is zero.
func TestJavaVectorInsertsAtAnIndex(t *testing.T) {
	client := fixtureClient(t)
	vector, err := newTestObject(t, client, javaVectorClass)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := javaVectorConstructor(client, nil, nil, []uint32{vector}); err != nil {
		t.Fatal(err)
	}
	for _, value := range []uint32{0x10, 0x20} {
		if _, err := javaVectorAdd(client, nil, nil, []uint32{vector, value}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := javaVectorInsertAt(client, nil, nil, []uint32{vector, 0x30, 0}); err != nil {
		t.Fatal(err)
	}
	held, err := client.javaVectorOf(vector)
	if err != nil {
		t.Fatal(err)
	}
	for index, want := range []uint32{0x30, 0x10, 0x20} {
		if held[index] != want {
			t.Fatalf("element %d is %#x, want %#x", index, held[index], want)
		}
	}
	// The size is allowed as an index, which appends; past it is not.
	if _, err := javaVectorInsertAt(client, nil, nil, []uint32{vector, 0x40, 3}); err != nil {
		t.Fatal(err)
	}
	if _, err := javaVectorInsertAt(client, nil, nil, []uint32{vector, 0x50, 9}); err == nil {
		t.Error("insert past the end answered instead of reporting")
	}
}

// The read side of a title's own save file: a signed byte, a big-endian word,
// and a block that has to arrive whole. `readFully` differs from the
// `read([BII)` beside it in exactly the way a caller depends on — it fills the
// array or reports, where the other answers a short count.
func TestJavaDataStreamReadsNumbersAndFillsAnArray(t *testing.T) {
	client := fixtureClient(t)
	client.archive = &Archive{Resources: map[string][]byte{
		"t.dat": {0xff, 0xde, 0xad, 0xbe, 0xef, 'a', 'b', 'c'},
	}}
	name, err := client.newJavaString("/t.dat")
	if err != nil {
		t.Fatal(err)
	}
	stream, err := javaGetResourceAsStream(client, nil, nil, []uint32{0, name})
	if err != nil {
		t.Fatal(err)
	}

	value, err := javaStreamReadByte(client, nil, nil, []uint32{stream})
	if err != nil {
		t.Fatal(err)
	}
	if int32(value) != -1 {
		t.Fatalf("readByte() = %d, want -1: 0xff is signed here", int32(value))
	}
	word, err := javaStreamReadInt(client, nil, nil, []uint32{stream})
	if err != nil {
		t.Fatal(err)
	}
	if word != 0xdeadbeef {
		t.Fatalf("readInt() = %#x, want 0xdeadbeef", word)
	}

	arrayType, err := client.javaArrayType(1, "B", 1)
	if err != nil {
		t.Fatal(err)
	}
	buffer, err := client.allocateJavaArray(arrayType.Object, 3)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := javaStreamReadFully(client, nil, nil, []uint32{stream, buffer, 0, 3}); err != nil {
		t.Fatal(err)
	}
	data, err := client.readJavaArrayBytes(buffer)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "abc" {
		t.Fatalf("readFully filled %q, want %q", string(data), "abc")
	}
	// Past the end is a truncated file rather than a short read, and it is
	// reported where the truncation is.
	if _, err := javaStreamReadFully(client, nil, nil, []uint32{stream, buffer, 0, 3}); err == nil {
		t.Error("readFully past the end answered instead of reporting")
	}
	if _, err := javaStreamReadInt(client, nil, nil, []uint32{stream}); err == nil {
		t.Error("readInt past the end answered instead of reporting")
	}
}

// `readUTF` reads what `writeUTF` writes, which is the pair a title stores a
// name with and reads it back with. The two-byte count is bytes rather than
// characters, and a stream too short for it is a truncated file rather than a
// short string.
func TestJavaDataStreamReadsBackWhatWriteUTFWrote(t *testing.T) {
	client := fixtureClient(t)
	const text = "가A"
	encoded := modifiedUTF8(text)
	resource := append([]byte{byte(len(encoded) >> 8), byte(len(encoded))}, encoded...)
	client.archive = &Archive{Resources: map[string][]byte{
		"u.dat": resource,
		"v.dat": {0x00, 0x40, 'a'}, // a count of 64 with one byte behind it
	}}

	name, err := client.newJavaString("/u.dat")
	if err != nil {
		t.Fatal(err)
	}
	stream, err := javaGetResourceAsStream(client, nil, nil, []uint32{0, name})
	if err != nil {
		t.Fatal(err)
	}
	object, err := javaStreamReadUTF(client, nil, nil, []uint32{stream})
	if err != nil {
		t.Fatalf("readUTF() error = %v", err)
	}
	if held, ok := client.javaText(object); !ok || held != text {
		t.Errorf("readUTF() = %q (held %t), want %q", held, ok, text)
	}
	if _, err := javaStreamReadUTF(client, nil, nil, []uint32{stream}); err == nil {
		t.Error("readUTF past the end answered instead of reporting")
	}

	short, err := client.newJavaString("/v.dat")
	if err != nil {
		t.Fatal(err)
	}
	truncated, err := javaGetResourceAsStream(client, nil, nil, []uint32{0, short})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := javaStreamReadUTF(client, nil, nil, []uint32{truncated}); err == nil {
		t.Error("a count longer than the stream answered instead of reporting")
	}
}

// The one-argument `substring` runs to the end of the string, which is the form
// the class library declares immediately before the two-argument one.
func TestJavaStringSubstringTakesOneBoundOrTwo(t *testing.T) {
	client := fixtureClient(t)
	text := newTestString(t, client, "menu.pzx")
	tail, err := javaStringSubstring(client, nil, nil, []uint32{text, 5})
	if err != nil {
		t.Fatal(err)
	}
	if held, _ := client.javaText(tail); held != "pzx" {
		t.Errorf("substring(5) = %q, want %q", held, "pzx")
	}
	head, err := javaStringSubstring(client, nil, nil, []uint32{text, 0, 4})
	if err != nil {
		t.Fatal(err)
	}
	if held, _ := client.javaText(head); held != "menu" {
		t.Errorf("substring(0, 4) = %q, want %q", held, "menu")
	}
	if _, err := javaStringSubstring(client, nil, nil, []uint32{text, 9}); err == nil {
		t.Error("a beginning past the end answered instead of reporting")
	}
}

// `FileSystem.isFile` asks the narrower question `exists` asks, and here they
// are the same question: nothing on this platform creates a directory, so a
// name that resolves is a file's name. Both forms answer it — the flag chooses
// a directory a title here has only one of.
func TestJavaFileSystemIsFileAnswersLikeExists(t *testing.T) {
	client := fixtureClient(t)
	client.archive = &Archive{Resources: map[string][]byte{"there.dat": {1, 2, 3}}}

	present := newTestString(t, client, "there.dat")
	absent := newTestString(t, client, "gone.dat")
	for _, probe := range []struct {
		name      string
		key       string
		arguments []uint32
		want      uint32
	}{
		{"a file that is there", "org/kwis/msp/io/FileSystem.isFile(Ljava/lang/String;)Z", []uint32{present}, 1},
		{"a file that is not", "org/kwis/msp/io/FileSystem.isFile(Ljava/lang/String;)Z", []uint32{absent}, 0},
		{"with the area flag", "org/kwis/msp/io/FileSystem.isFile(Ljava/lang/String;I)Z", []uint32{present, 1}, 1},
	} {
		method, registered := javaPlatformMethods[probe.key]
		if !registered {
			t.Fatalf("%s is not registered", probe.key)
		}
		got, err := method.Implementat(client, nil, nil, probe.arguments)
		if err != nil {
			t.Fatalf("%s: %v", probe.name, err)
		}
		if got != probe.want {
			t.Errorf("%s answered %d, want %d", probe.name, got, probe.want)
		}
	}
}

// Setting the font is accepted and changes nothing, because there is one face
// here. What it must not be is a refusal: a title sets the font before it draws
// its own text, and stopping there costs the screen the text was on.
func TestJavaGraphicsSetFontIsAccepted(t *testing.T) {
	client := fixtureClient(t)
	const key = "org/kwis/msp/lcdui/Graphics.setFont(Lorg/kwis/msp/lcdui/Font;)V"
	method, registered := javaGraphicsMethods[key]
	if !registered {
		t.Fatalf("%s is not registered", key)
	}
	font, err := javaPlatformSingleton(javaFontClass)(client, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := method.Implementat(client, nil, nil, []uint32{0, font}); err != nil {
		t.Errorf("setFont refused: %v", err)
	}
}
