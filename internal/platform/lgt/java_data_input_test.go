package lgt

import (
	"testing"
)

const readUTFOfDataInput = "java/io/DataInputStream.readUTF(Ljava/io/DataInput;)Ljava/lang/String;"

// newTestDataStream is a DataInputStream over bytes a title holds.
func newTestDataStream(t *testing.T, client *Client, data []byte) uint32 {
	t.Helper()
	array, err := client.newJavaByteArray(data)
	if err != nil {
		t.Fatal(err)
	}
	source, err := newTestObject(t, client, "java/io/ByteArrayInputStream")
	if err != nil {
		t.Fatal(err)
	}
	mustJavaCall(t, client, "java/io/ByteArrayInputStream.<init>([B)V", source, array)
	stream, err := newTestObject(t, client, javaDataInputStreamClass)
	if err != nil {
		t.Fatal(err)
	}
	mustJavaCall(t, client, "java/io/DataInputStream.<init>(Ljava/io/InputStream;)V", stream, source)
	return stream
}

// The static readUTF reads a length and that many bytes of modified UTF-8 off
// a DataInputStream, and leaves the stream after them: a null character in
// either of its forms, a two- and a three-byte character, and the text that
// follows untouched.
func TestJavaDataInputReadUTFFromADataInputStream(t *testing.T) {
	client := fixtureClient(t)
	stream := newTestDataStream(t, client, []byte{
		0x00, 0x09, 'A', 0x00, 0xc0, 0x80, 0xc2, 0xa9, 0xea, 0xb0, 0x80,
		0x00, 0x00,
		0x00, 0x03, 0xed, 0xa0, 0x80,
		'Z',
	})
	if got := mustText(t, client, mustJavaCall(t, client, readUTFOfDataInput, stream)); got != "A\x00\x00©가" {
		t.Errorf("readUTF = %q", got)
	}
	if got := mustText(t, client, mustJavaCall(t, client, readUTFOfDataInput, stream)); got != "" {
		t.Errorf("readUTF of nothing = %q", got)
	}
	// An unpaired surrogate is a character the String keeps, as new
	// String(char[]) keeps it.
	lone := mustJavaCall(t, client, readUTFOfDataInput, stream)
	if units := utf16Units(mustText(t, client, lone)); len(units) != 1 || units[0] != 0xd800 {
		t.Errorf("readUTF of an unpaired surrogate = %#x", units)
	}
	if got := mustSlot(t, client, javaDataInputStreamClass, 23, stream); got != 'Z' {
		t.Errorf("the byte after the text = %#x, want 'Z'", got)
	}
}

// What readUTF throws: EOFException for a stream that ends inside the length
// or inside the text, UTFDataFormatException for bytes that are not modified
// UTF-8, and NullPointerException for no DataInput at all.
func TestJavaDataInputReadUTFRefusals(t *testing.T) {
	client := fixtureClient(t)
	for _, probe := range []struct {
		name string
		data []byte
		want string
	}{
		{"a length cut short", []byte{0x00}, javaEOFClass},
		{"text cut short", []byte{0x00, 0x05, 'a', 'b'}, javaEOFClass},
		{"a continuation byte first", []byte{0x00, 0x01, 0x80}, javaUTFDataFormatClass},
		{"a two-byte character without its second", []byte{0x00, 0x02, 0xc2, 'A'}, javaUTFDataFormatClass},
		{"a three-byte character cut by the length", []byte{0x00, 0x02, 0xea, 0xb0, 0x80}, javaUTFDataFormatClass},
		{"a four-byte lead", []byte{0x00, 0x04, 0xf0, 0x9f, 0x98, 0x80}, javaUTFDataFormatClass},
	} {
		stream := newTestDataStream(t, client, probe.data)
		if _, err := javaCall(t, client, readUTFOfDataInput, stream); thrownClass(err) != probe.want {
			t.Errorf("%s = %v, want %s", probe.name, err, probe.want)
		}
	}
	if _, err := javaCall(t, client, readUTFOfDataInput, 0); thrownClass(err) != javaThrowNullClass {
		t.Errorf("readUTF(null) = %v, want NullPointerException", err)
	}
}

// The two calls readUTF makes are the DataInput's own: a title's
// DataInputStream subclass that overrides readUnsignedShort answers the length
// itself, and the bytes still come off the stream it stands for.
func TestJavaDataInputReadUTFAsksADataInputStreamSubclass(t *testing.T) {
	client := fixtureClient(t)
	// readUnsignedShort: movs r0, #3; bx lr.
	stream := installGuestSubclass(t, client, "game/Records", javaDataInputStreamClass, 4, 0,
		guestRoutine{javaDataStreamSlotReadUnsignedShort, []uint16{0x2003, 0x4770}})
	array, err := client.newJavaByteArray([]byte("abcdef"))
	if err != nil {
		t.Fatal(err)
	}
	source, err := newTestObject(t, client, "java/io/ByteArrayInputStream")
	if err != nil {
		t.Fatal(err)
	}
	mustJavaCall(t, client, "java/io/ByteArrayInputStream.<init>([B)V", source, array)
	mustJavaCall(t, client, "java/io/DataInputStream.<init>(Ljava/io/InputStream;)V", stream, source)
	if got := mustText(t, client, mustJavaCall(t, client, readUTFOfDataInput, stream)); got != "abc" {
		t.Errorf("readUTF through the title's readUnsignedShort = %q, want \"abc\"", got)
	}
}

// A title's own class that implements DataInput is read through the interface
// table its record carries, each method at its place in the interface.
func TestJavaDataInputReadUTFThroughATitlesOwnDataInput(t *testing.T) {
	client := fixtureClient(t)
	const first = 20
	input := installGuestSubclass(t, client, "game/Input", javaObjectClass, 4, 0,
		// readUnsignedShort: movs r0, #3; bx lr.
		guestRoutine{first + javaDataInputReadUnsignedShort, []uint16{0x2003, 0x4770}},
		// readFully(byte[], int, int): "abc" at the offset — ldr r1, [r1, #8];
		// adds r1, #4; adds r1, r1, r2; then movs and strb for each byte.
		guestRoutine{first + javaDataInputReadFully, []uint16{0x6889, 0x3104, 0x1889,
			0x2061, 0x7008, 0x2062, 0x7048, 0x2063, 0x7088, 0x4770}})
	class, _ := client.javaClassOfObject(input)
	class.Record.Interfaces = []javaInterface{{Name: javaDataInputInterface, Slot: first}}
	if got := mustText(t, client, mustJavaCall(t, client, readUTFOfDataInput, input)); got != "abc" {
		t.Errorf("readUTF through the title's own DataInput = %q, want \"abc\"", got)
	}
	// A class that implements no DataInput is not one.
	other := installGuestSubclass(t, client, "game/Other", javaObjectClass, 4, 64)
	if _, err := javaCall(t, client, readUTFOfDataInput, other); err == nil || thrownClass(err) != "" {
		t.Errorf("readUTF of an object that is no DataInput = %v, want it refused", err)
	}
}
