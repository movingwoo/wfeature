package lgt

import (
	"bytes"
	"testing"
)

func newTestBuffer(t *testing.T, client *Client, text string) uint32 {
	t.Helper()
	buffer, err := newTestObject(t, client, javaStringBufferClass)
	if err != nil {
		t.Fatal(err)
	}
	mustJavaCall(t, client, "java/lang/StringBuffer.<init>(Ljava/lang/String;)V", buffer, newTestString(t, client, text))
	return buffer
}

func newTestChars(t *testing.T, client *Client, text string) uint32 {
	t.Helper()
	array, err := client.newJavaCharArray(utf16Units(text))
	if err != nil {
		t.Fatal(err)
	}
	return array
}

// String's own members, each against what the class library answers.
func TestJavaStringMembers(t *testing.T) {
	client := fixtureClient(t)
	hello := newTestString(t, client, "Hello")
	for _, probe := range []struct {
		text string
		want int32
	}{{"", 0}, {"abc", 96354}, {"가", 0xac00}, {"Hello", 69609650}} {
		if got := mustSlot(t, client, javaStringClass, javaHashCodeSlot, newTestString(t, client, probe.text)); int32(got) != probe.want {
			t.Errorf("hashCode(%q) = %d, want %d", probe.text, int32(got), probe.want)
		}
	}
	if got := mustSlot(t, client, javaStringClass, javaToStringSlot, hello); got != hello {
		t.Error("toString() is not the String itself")
	}
	for _, probe := range []struct {
		other string
		want  uint32
	}{{"hELLO", javaTrue}, {"Hell", javaFalse}, {"Hellp", javaFalse}} {
		if got := mustSlot(t, client, javaStringClass, 15, hello, newTestString(t, client, probe.other)); got != probe.want {
			t.Errorf("equalsIgnoreCase(%q) = %d", probe.other, got)
		}
	}
	if got := mustSlot(t, client, javaStringClass, 15, newTestString(t, client, "é"), newTestString(t, client, "É")); got != javaTrue {
		t.Error("equalsIgnoreCase does not fold Latin-1")
	}
	if got := mustSlot(t, client, javaStringClass, 15, hello, 0); got != javaFalse {
		t.Error("equalsIgnoreCase(null) is not false")
	}
	if got := mustSlot(t, client, javaStringClass, 17, hello, javaTrue, 1, newTestString(t, client, "XELL"), 1, 3); got != javaTrue {
		t.Error("regionMatches(true, 1, \"XELL\", 1, 3) is false")
	}
	if got := mustSlot(t, client, javaStringClass, 17, hello, javaFalse, 1, newTestString(t, client, "XELL"), 1, 3); got != javaFalse {
		t.Error("regionMatches(false, ...) ignores case")
	}
	if got := mustSlot(t, client, javaStringClass, 17, hello, javaFalse, 4, newTestString(t, client, "ox"), 0, 2); got != javaFalse {
		t.Error("a region past the end matches")
	}
	for _, probe := range []struct {
		slot      uint32
		arguments []uint32
		want      int32
	}{
		{18, []uint32{newTestString(t, client, "ll"), 2}, 1},
		{18, []uint32{newTestString(t, client, "ll"), 0xffffffff}, 0},
		{20, []uint32{newTestString(t, client, "lo")}, 1},
		{23, []uint32{'l'}, 3},
		{24, []uint32{'l', 2}, 2},
		{24, []uint32{'l', 100}, 3},
		{24, []uint32{'l', 0xffffffff}, -1},
		{25, []uint32{newTestString(t, client, "ll")}, 2},
	} {
		got := mustSlot(t, client, javaStringClass, probe.slot, append([]uint32{hello}, probe.arguments...)...)
		if int32(got) != probe.want {
			t.Errorf("slot %d%v = %d, want %d", probe.slot, probe.arguments, int32(got), probe.want)
		}
	}
	if got := mustText(t, client, mustSlot(t, client, javaStringClass, 30, hello, 'l', 'L')); got != "HeLLo" {
		t.Errorf("replace = %q", got)
	}
	if got := mustSlot(t, client, javaStringClass, 30, hello, 'z', 'y'); got != hello {
		t.Error("replace with nothing to replace is not the String itself")
	}
	if got := mustText(t, client, mustSlot(t, client, javaStringClass, 32, newTestString(t, client, "abcé가"))); got != "ABCÉ가" {
		t.Errorf("toUpperCase = %q", got)
	}
	lower := newTestString(t, client, "abc")
	if got := mustSlot(t, client, javaStringClass, 31, lower); got != lower {
		t.Error("toLowerCase of lower-case text is not the String itself")
	}

	destination := newTestChars(t, client, "-----")
	mustSlot(t, client, javaStringClass, 12, hello, 1, 4, destination, 1)
	if units, _ := client.readJavaArrayChars(destination); javaTextOfUnits(units) != "-ell-" {
		t.Errorf("getChars wrote %q", javaTextOfUnits(units))
	}
	if _, err := slotCall(t, client, javaStringClass, 12, hello, 3, 9, destination, 0); thrownClass(err) != javaStringIndexClass {
		t.Errorf("getChars past the string = %v, want StringIndexOutOfBoundsException", err)
	}
	if _, err := slotCall(t, client, javaStringClass, 12, hello, 0, 5, destination, 1); thrownClass(err) != javaThrowArrayClass {
		t.Errorf("getChars past the array = %v, want ArrayIndexOutOfBoundsException", err)
	}

	korean := newTestString(t, client, "가")
	for _, probe := range []struct {
		encoding string
		want     []byte
	}{{"UTF-8", []byte{0xea, 0xb0, 0x80}}, {"KSC5601", []byte{0xb0, 0xa1}}, {"euc_kr", []byte{0xb0, 0xa1}}} {
		array := mustSlot(t, client, javaStringClass, 13, korean, newTestString(t, client, probe.encoding))
		if data, _ := client.readJavaArrayBytes(array); !bytes.Equal(data, probe.want) {
			t.Errorf("getBytes(%q) = % x, want % x", probe.encoding, data, probe.want)
		}
	}
	if _, err := slotCall(t, client, javaStringClass, 13, korean, newTestString(t, client, "Shift_JIS")); thrownClass(err) != javaUnsupportedEncodingName {
		t.Errorf("getBytes of an unknown encoding = %v, want UnsupportedEncodingException", err)
	}
	encoded, err := client.newJavaByteArray([]byte{'x', 0xea, 0xb0, 0x80, 'y'})
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := newTestObject(t, client, javaStringClass)
	if err != nil {
		t.Fatal(err)
	}
	mustJavaCall(t, client, "java/lang/String.<init>([BIILjava/lang/String;)V", decoded, encoded, 1, 3, newTestString(t, client, "utf8"))
	if got := mustText(t, client, decoded); got != "가" {
		t.Errorf("String(bytes, 1, 3, utf8) = %q", got)
	}

	for _, probe := range []struct {
		key       string
		arguments []uint32
		want      string
	}{
		{"java/lang/String.valueOf(Z)Ljava/lang/String;", []uint32{1}, "true"},
		{"java/lang/String.valueOf(C)Ljava/lang/String;", []uint32{0xac00}, "가"},
		{"java/lang/String.valueOf(J)Ljava/lang/String;", []uint32{0xffffffff, 0xffffffff}, "-1"},
		{"java/lang/String.valueOf([C)Ljava/lang/String;", []uint32{newTestChars(t, client, "chars")}, "chars"},
		{"java/lang/String.valueOf([CII)Ljava/lang/String;", []uint32{newTestChars(t, client, "chars"), 1, 3}, "har"},
	} {
		if got := mustText(t, client, mustJavaCall(t, client, probe.key, probe.arguments...)); got != probe.want {
			t.Errorf("%s = %q, want %q", probe.key, got, probe.want)
		}
	}
}

// A StringBuffer's capacity follows the class library's own rules, and its
// members change it the way the library's would.
func TestJavaStringBufferMembers(t *testing.T) {
	client := fixtureClient(t)
	buffer := newTestBuffer(t, client, "abc")
	if got := mustSlot(t, client, javaStringBufferClass, 11, buffer); got != 19 {
		t.Errorf("capacity of StringBuffer(\"abc\") = %d, want 19", got)
	}
	empty, err := newTestObject(t, client, javaStringBufferClass)
	if err != nil {
		t.Fatal(err)
	}
	mustJavaCall(t, client, "java/lang/StringBuffer.<init>()V", empty)
	mustSlot(t, client, javaStringBufferClass, 18, empty, newTestString(t, client, "seventeen letters"))
	if got := mustSlot(t, client, javaStringBufferClass, 11, empty); got != 34 {
		t.Errorf("capacity after growing past 16 = %d, want 34", got)
	}
	mustSlot(t, client, javaStringBufferClass, 12, empty, 100)
	if got := mustSlot(t, client, javaStringBufferClass, 11, empty); got != 100 {
		t.Errorf("ensureCapacity(100) = %d, want 100", got)
	}
	sized, err := newTestObject(t, client, javaStringBufferClass)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := javaCall(t, client, "java/lang/StringBuffer.<init>(I)V", sized, uint32(0xffffffff)); thrownClass(err) != javaNegativeArraySizeClass {
		t.Errorf("StringBuffer(-1) = %v, want NegativeArraySizeException", err)
	}

	if got := mustSlot(t, client, javaStringBufferClass, 14, buffer, 1); got != 'b' {
		t.Errorf("charAt(1) = %c", rune(got))
	}
	if _, err := slotCall(t, client, javaStringBufferClass, 14, buffer, 3); thrownClass(err) != javaIndexClass {
		t.Errorf("charAt(3) = %v, want IndexOutOfBoundsException", err)
	}
	mustSlot(t, client, javaStringBufferClass, 16, buffer, 0, 'A')
	mustSlot(t, client, javaStringBufferClass, 19, buffer, newTestChars(t, client, "de"))
	mustSlot(t, client, javaStringBufferClass, 20, buffer, newTestChars(t, client, "xfgx"), 1, 2)
	mustSlot(t, client, javaStringBufferClass, 24, buffer, 0xfffffffb, 0xffffffff)
	if got := mustText(t, client, buffer); got != "Abcdefg-5" {
		t.Fatalf("after the appends the buffer holds %q", got)
	}
	mustSlot(t, client, javaStringBufferClass, 28, buffer, 7)
	mustSlot(t, client, javaStringBufferClass, 32, buffer, 0, javaTrue)
	mustSlot(t, client, javaStringBufferClass, 33, buffer, 4, '+')
	mustSlot(t, client, javaStringBufferClass, 35, buffer, 0, 12, 0)
	mustSlot(t, client, javaStringBufferClass, 31, buffer, 2, newTestChars(t, client, "[]"))
	mustSlot(t, client, javaStringBufferClass, 29, buffer, 0, newTestInteger(t, client, 9))
	if got := mustText(t, client, buffer); got != "912[]true+Abcdefg5" {
		t.Fatalf("after the inserts the buffer holds %q", got)
	}
	if _, err := slotCall(t, client, javaStringBufferClass, 28, buffer, 99); thrownClass(err) != javaStringIndexClass {
		t.Errorf("deleteCharAt past the end = %v, want StringIndexOutOfBoundsException", err)
	}
	reversed := newTestBuffer(t, client, "abc")
	if mustSlot(t, client, javaStringBufferClass, 38, reversed) != reversed || mustText(t, client, reversed) != "cba" {
		t.Errorf("reverse() left %q", mustText(t, client, reversed))
	}
	// append(Object) is the object's own toString.
	mustSlot(t, client, javaStringBufferClass, 17, reversed, newTestInteger(t, client, -3))
	if got := mustText(t, client, reversed); got != "cba-3" {
		t.Errorf("append(Integer) = %q", got)
	}
}

// A reader reads one character at a time, into part of an array, past what it
// skips, and back to a mark, with the cursor in the bytes underneath.
func TestJavaReaderMembers(t *testing.T) {
	client := fixtureClient(t)
	array, err := client.newJavaByteArray([]byte{'A', 0xb0, 0xa1, 'B', 'C', 'D'})
	if err != nil {
		t.Fatal(err)
	}
	source, err := newTestObject(t, client, javaInputStreamClass)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := javaByteStreamConstructor(client, nil, nil, []uint32{source, array}); err != nil {
		t.Fatal(err)
	}
	reader, err := newTestObject(t, client, javaInputStreamReaderClass)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := javaWrapStream(client, nil, nil, []uint32{reader, source}); err != nil {
		t.Fatal(err)
	}
	if got := mustSlot(t, client, javaInputStreamReaderClass, 15, reader); got != javaTrue {
		t.Fatal("a stream over an array cannot mark")
	}
	mustSlot(t, client, javaInputStreamReaderClass, 16, reader, 16)
	if got := mustSlot(t, client, javaInputStreamReaderClass, 10, reader); got != 'A' {
		t.Errorf("read() = %c", rune(got))
	}
	if got := mustSlot(t, client, javaInputStreamReaderClass, 10, reader); got != 0xac00 {
		t.Errorf("read() of a two-byte character = %#x", got)
	}
	if got := mustSlot(t, client, javaInputStreamReaderClass, 13, reader, 1, 0); got != 1 {
		t.Errorf("skip(1) = %d", got)
	}
	into := newTestChars(t, client, "....")
	if got := mustSlot(t, client, javaInputStreamReaderClass, 12, reader, into, 1, 2); got != 2 {
		t.Errorf("read(chars, 1, 2) = %d", got)
	}
	if units, _ := client.readJavaArrayChars(into); javaTextOfUnits(units) != ".CD." {
		t.Errorf("read(chars, 1, 2) wrote %q", javaTextOfUnits(units))
	}
	if got := mustSlot(t, client, javaInputStreamReaderClass, 14, reader); got != javaFalse {
		t.Error("ready() at the end is true")
	}
	if got := mustSlot(t, client, javaInputStreamReaderClass, 10, reader); int32(got) != -1 {
		t.Errorf("read() at the end = %d", int32(got))
	}
	mustSlot(t, client, javaInputStreamReaderClass, 17, reader)
	if got := mustSlot(t, client, javaInputStreamReaderClass, 10, reader); got != 'A' {
		t.Errorf("read() after reset = %c", rune(got))
	}
	if _, err := slotCall(t, client, javaInputStreamReaderClass, 12, reader, into, 3, 2); thrownClass(err) != javaIndexClass {
		t.Errorf("read past the array = %v, want IndexOutOfBoundsException", err)
	}

	data, err := client.newJavaByteArray([]byte{0xff, 0xfe})
	if err != nil {
		t.Fatal(err)
	}
	stream, err := newTestObject(t, client, javaDataInputStreamClass)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := javaByteStreamConstructor(client, nil, nil, []uint32{stream, data}); err != nil {
		t.Fatal(err)
	}
	if got := mustSlot(t, client, javaDataInputStreamClass, 26, stream); got != 0xfffe {
		t.Errorf("readUnsignedShort() = %#x, want 0xfffe", got)
	}
}
