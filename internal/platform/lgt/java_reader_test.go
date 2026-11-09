package lgt

import "testing"

// The `read(char[], int, int)` of two Readers a title might write: one that
// puts an 'A' at the offset it is handed and answers one character —
// ldr r1, [r1, #8]; adds r1, #4; lsls r2, r2, #1; adds r1, r1, r2;
// movs r0, #0x41; strh r0, [r1]; movs r0, #1; bx lr — and one that has ended:
// movs r0, #0; subs r0, #1; bx lr.
var (
	readerOfAs   = []uint16{0x6889, 0x3104, 0x0052, 0x1889, 0x2041, 0x8008, 0x2001, 0x4770}
	readerAtEnd  = []uint16{0x2000, 0x3801, 0x4770}
	readerOfNone = []uint16{0x2000, 0x4770}
)

// A title's own Reader overrides read(char[], int, int) and inherits the rest,
// and every inherited body reads through the override, the way the class
// library's do.
func TestJavaReaderBaseReadsThroughTheTitlesOwnRead(t *testing.T) {
	client := fixtureClient(t)
	reader := installGuestSubclass(t, client, "game/Letters", javaReaderClass, 4, 0,
		guestRoutine{javaReaderSlotReadRange, readerOfAs})

	if got := mustSlot(t, client, javaReaderClass, 10, reader); got != 'A' {
		t.Errorf("read() = %#x, want the 'A' the title's read put in", got)
	}
	into := newTestChars(t, client, "...")
	if got := mustSlot(t, client, javaReaderClass, 11, reader, into); got != 1 {
		t.Errorf("read(char[]) = %d, want the one character the title's read answered", got)
	}
	if units, _ := client.readJavaArrayChars(into); javaTextOfUnits(units) != "A.." {
		t.Errorf("read(char[]) left %q", javaTextOfUnits(units))
	}
	// Five characters one at a time, then the long answer in two registers.
	if got := mustSlot(t, client, javaReaderClass, 13, reader, 5, 0); got != 5 {
		t.Errorf("skip(5) = %d", got)
	}
	if high, _ := client.thread.Register(1); high != 0 {
		t.Errorf("skip(5) high word = %#x", high)
	}
	if got := mustSlot(t, client, javaReaderClass, 13, reader, 0, 0); got != 0 {
		t.Errorf("skip(0) = %d", got)
	}
	if _, err := slotCall(t, client, javaReaderClass, 13, reader, 0xffffffff, 0xffffffff); thrownClass(err) != javaIllegalArgumentClass {
		t.Errorf("skip(-1) = %v, want IllegalArgumentException", err)
	}
	if _, err := slotCall(t, client, javaReaderClass, 11, reader, 0); thrownClass(err) != javaThrowNullClass {
		t.Errorf("read(null) = %v, want NullPointerException", err)
	}

	if got := mustSlot(t, client, javaReaderClass, 14, reader); got != javaFalse {
		t.Error("ready() is true for a reader that says nothing of its own")
	}
	if got := mustSlot(t, client, javaReaderClass, 15, reader); got != javaFalse {
		t.Error("markSupported() is true for a reader that says nothing of its own")
	}
	if _, err := slotCall(t, client, javaReaderClass, 16, reader, 8); thrownClass(err) != javaIOExceptionClass {
		t.Errorf("mark(8) = %v, want IOException", err)
	}
	if _, err := slotCall(t, client, javaReaderClass, 17, reader); thrownClass(err) != javaIOExceptionClass {
		t.Errorf("reset() = %v, want IOException", err)
	}
	// The abstract two are not served: a subclass that left one out has
	// nothing to inherit.
	for _, slot := range []uint32{javaReaderSlotReadRange, 18} {
		if _, served := javaBakedVirtualSlots[javaReaderClass][slot]; served {
			t.Errorf("Reader slot %d is abstract and is served", slot)
		}
	}
}

// At the end, the inherited bodies answer what the class library's do: -1
// from read(), and the characters skipped before the end from skip. A read
// that answers no characters at all ends a skip rather than asking again.
func TestJavaReaderBaseAtTheEnd(t *testing.T) {
	client := fixtureClient(t)
	ended := installGuestSubclass(t, client, "game/Ended", javaReaderClass, 4, 0,
		guestRoutine{javaReaderSlotReadRange, readerAtEnd})
	if got := mustSlot(t, client, javaReaderClass, 10, ended); int32(got) != -1 {
		t.Errorf("read() at the end = %d, want -1", int32(got))
	}
	if got := mustSlot(t, client, javaReaderClass, 11, ended, newTestChars(t, client, "..")); int32(got) != -1 {
		t.Errorf("read(char[]) at the end = %d, want -1", int32(got))
	}
	if got := mustSlot(t, client, javaReaderClass, 13, ended, 5, 0); got != 0 {
		t.Errorf("skip(5) at the end = %d, want 0", got)
	}
	idle := installGuestSubclass(t, client, "game/Idle", javaReaderClass, 4, 16,
		guestRoutine{javaReaderSlotReadRange, readerOfNone})
	if got := mustSlot(t, client, javaReaderClass, 13, idle, 1<<20, 0); got != 0 {
		t.Errorf("skip over a reader that answers nothing = %d, want 0", got)
	}
}

// Reader's two constructors: the lock is the reader or the object given, and
// is published where the module was told the protected field sits; null is a
// NullPointerException.
func TestJavaReaderBaseConstructorTakesItsLock(t *testing.T) {
	client := fixtureClient(t)
	surface := &javaSurface{
		Fields:  []javaMemberRef{{Name: "lock", Descriptor: "Ljava/lang/Object;"}},
		Classes: []javaAPIClass{{Name: javaReaderClass, Fields: javaRun{Start: 0, Count: 1}}},
	}
	layout := newJavaLayout()
	if _, err := layout.layoutPlatformFields(surface); err != nil {
		t.Fatal(err)
	}
	client.javaLink = &javaLink{surface: surface, layout: layout}
	word, ok := layout.platformFieldWord(javaReaderClass, "lock", "Ljava/lang/Object;")
	if !ok {
		t.Fatal("the layout did not number Reader.lock")
	}
	reader := installGuestSubclass(t, client, "game/Locked", javaReaderClass, 4, 0)
	lockOf := func() uint32 {
		t.Helper()
		block, _ := client.readWord(reader + 8)
		lock, err := client.readWord(block + word*4)
		if err != nil {
			t.Fatal(err)
		}
		return lock
	}
	mustJavaCall(t, client, "java/io/Reader.<init>()V", reader)
	if got := lockOf(); got != reader {
		t.Errorf("Reader() locks on %#x, want the reader %#x", got, reader)
	}
	other := newTestString(t, client, "a lock")
	mustJavaCall(t, client, "java/io/Reader.<init>(Ljava/lang/Object;)V", reader, other)
	if got := lockOf(); got != other {
		t.Errorf("Reader(lock) locks on %#x, want %#x", got, other)
	}
	if _, err := javaCall(t, client, "java/io/Reader.<init>(Ljava/lang/Object;)V", reader, 0); thrownClass(err) != javaThrowNullClass {
		t.Errorf("Reader(null) = %v, want NullPointerException", err)
	}
}

// The named-encoding InputStreamReader takes the names the KTF and SKT
// runtimes take, refuses the rest and null, and reads the stream it was built
// on with the handset's own encoding, as theirs does.
func TestJavaInputStreamReaderNamesAnEncoding(t *testing.T) {
	client := fixtureClient(t)
	key := "java/io/InputStreamReader.<init>(Ljava/io/InputStream;Ljava/lang/String;)V"
	newSource := func() uint32 {
		t.Helper()
		array, err := client.newJavaByteArray([]byte{0xb0, 0xa1, 'A'})
		if err != nil {
			t.Fatal(err)
		}
		source, err := newTestObject(t, client, "java/io/ByteArrayInputStream")
		if err != nil {
			t.Fatal(err)
		}
		mustJavaCall(t, client, "java/io/ByteArrayInputStream.<init>([B)V", source, array)
		return source
	}
	for _, name := range []string{"KSC5601", "EUC-KR", "euc_kr", "UTF-8", "utf8", "MS949"} {
		reader, err := newTestObject(t, client, javaInputStreamReaderClass)
		if err != nil {
			t.Fatal(err)
		}
		mustJavaCall(t, client, key, reader, newSource(), newTestString(t, client, name))
		if got := mustSlot(t, client, javaInputStreamReaderClass, 10, reader); got != 0xac00 {
			t.Errorf("a reader named %q read %#x first, want 0xac00", name, got)
		}
		if got := mustSlot(t, client, javaInputStreamReaderClass, 10, reader); got != 'A' {
			t.Errorf("a reader named %q read %#x second, want 'A'", name, got)
		}
	}
	reader, err := newTestObject(t, client, javaInputStreamReaderClass)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := javaCall(t, client, key, reader, newSource(), newTestString(t, client, "ISO-8859-1")); thrownClass(err) != javaUnsupportedEncodingName {
		t.Errorf("an encoding nothing has = %v, want UnsupportedEncodingException", err)
	}
	if _, err := javaCall(t, client, key, reader, newSource(), 0); thrownClass(err) != javaThrowNullClass {
		t.Errorf("a null encoding = %v, want NullPointerException", err)
	}
	if _, err := javaCall(t, client, key, reader, 0, newTestString(t, client, "UTF-8")); thrownClass(err) != javaThrowNullClass {
		t.Errorf("a null stream = %v, want NullPointerException", err)
	}
}

// The one-argument InputStreamReader over a null stream is the
// NullPointerException the class library and the KTF and SKT runtimes throw,
// which a title's handler can take, rather than a stop on an object nothing
// can name; over a stream it is the reader it was.
func TestJavaInputStreamReaderOverANullStream(t *testing.T) {
	client := fixtureClient(t)
	key := "java/io/InputStreamReader.<init>(Ljava/io/InputStream;)V"
	reader, err := newTestObject(t, client, javaInputStreamReaderClass)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := javaCall(t, client, key, reader, 0); thrownClass(err) != javaThrowNullClass {
		t.Errorf("InputStreamReader(null) = %v, want NullPointerException", err)
	}
	array, err := client.newJavaByteArray([]byte{'A'})
	if err != nil {
		t.Fatal(err)
	}
	source, err := newTestObject(t, client, "java/io/ByteArrayInputStream")
	if err != nil {
		t.Fatal(err)
	}
	mustJavaCall(t, client, "java/io/ByteArrayInputStream.<init>([B)V", source, array)
	mustJavaCall(t, client, key, reader, source)
	if got := mustSlot(t, client, javaInputStreamReaderClass, 10, reader); got != 'A' {
		t.Errorf("read() = %#x, want 'A'", got)
	}
}

// An InputStreamReader is a Reader: a type check against the class it extends
// passes, which a title's `(Reader) in` asks.
func TestJavaInputStreamReaderIsAReader(t *testing.T) {
	client := fixtureClient(t)
	reader, err := newTestObject(t, client, javaInputStreamReaderClass)
	if err != nil {
		t.Fatal(err)
	}
	if !client.javaObjectIs(reader, javaReaderClass) {
		t.Error("an InputStreamReader is not a Reader")
	}
}
