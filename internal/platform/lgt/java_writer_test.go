package lgt

import (
	"bytes"
	"context"
	"testing"
)

// newTestSink is a ByteArrayOutputStream built the way a title builds one.
func newTestSink(t *testing.T, client *Client) uint32 {
	t.Helper()
	sink, err := newTestObject(t, client, javaByteSinkClass)
	if err != nil {
		t.Fatal(err)
	}
	mustJavaCall(t, client, "java/io/ByteArrayOutputStream.<init>()V", sink)
	return sink
}

// newTestWriter is an OutputStreamWriter over a stream, in the handset's own
// encoding or the one named.
func newTestWriter(t *testing.T, client *Client, stream uint32, encoding ...string) uint32 {
	t.Helper()
	writer, err := newTestObject(t, client, javaStreamWriterClass)
	if err != nil {
		t.Fatal(err)
	}
	if len(encoding) == 0 {
		mustJavaCall(t, client, "java/io/OutputStreamWriter.<init>(Ljava/io/OutputStream;)V", writer, stream)
	} else {
		mustJavaCall(t, client, "java/io/OutputStreamWriter.<init>(Ljava/io/OutputStream;Ljava/lang/String;)V",
			writer, stream, newTestString(t, client, encoding[0]))
	}
	return writer
}

// An OutputStreamWriter turns every write into bytes on its stream, in its
// encoding: the five it overrides, and the two it inherits from Writer, which
// dispatch reaches by walking up to Writer and which write through the
// writer's own overrides.
func TestJavaOutputStreamWriterWritesItsEncodingIntoItsStream(t *testing.T) {
	client := fixtureClient(t)
	ctx := context.Background()
	call := func(object, slot uint32, arguments ...uint32) {
		t.Helper()
		if _, err := client.javaCallVirtual(ctx, client.thread, object, slot, arguments...); err != nil {
			t.Fatalf("slot %d: %v", slot, err)
		}
	}
	for _, probe := range []struct {
		encoding []string
		want     []byte
	}{
		// "A가", "가", "나다" from "가나다", "A" from "AB" and "AB" again, in
		// the handset's own encoding and in UTF-8.
		{nil, []byte{'A', 0xb0, 0xa1, 0xb0, 0xa1, 0xb3, 0xaa, 0xb4, 0xd9, 'A', 'A', 'B'}},
		{[]string{"UTF-8"}, []byte{'A', 0xea, 0xb0, 0x80, 0xea, 0xb0, 0x80, 0xeb, 0x82, 0x98, 0xeb, 0x8b, 0xa4,
			'A', 'A', 'B'}},
		{[]string{"KSC5601"}, []byte{'A', 0xb0, 0xa1, 0xb0, 0xa1, 0xb3, 0xaa, 0xb4, 0xd9, 'A', 'A', 'B'}},
	} {
		sink := newTestSink(t, client)
		writer := newTestWriter(t, client, sink, probe.encoding...)
		call(writer, 10, 'A')
		call(writer, 10, 0xac00)
		call(writer, 11, newTestChars(t, client, "가"))
		call(writer, 12, newTestChars(t, client, "가나다"), 1, 2)
		call(writer, 14, newTestString(t, client, "AB"), 0, 1)
		call(writer, 13, newTestString(t, client, "AB"))
		call(writer, 15)
		if got := client.javaRuntimeState().sinks[sink]; !bytes.Equal(got, probe.want) {
			t.Errorf("%v wrote % x, want % x", probe.encoding, got, probe.want)
		}
	}
}

// What an OutputStreamWriter refuses: an encoding nobody accepts, a null name
// or stream, a run outside what it is handed, and every call once it is
// closed but another close.
func TestJavaOutputStreamWriterRefusals(t *testing.T) {
	client := fixtureClient(t)
	key := "java/io/OutputStreamWriter.<init>(Ljava/io/OutputStream;Ljava/lang/String;)V"
	sink := newTestSink(t, client)
	unopened, err := newTestObject(t, client, javaStreamWriterClass)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := javaCall(t, client, key, unopened, sink, newTestString(t, client, "ISO-8859-1")); thrownClass(err) != javaUnsupportedEncodingName {
		t.Errorf("an encoding nothing has = %v, want UnsupportedEncodingException", err)
	}
	if _, err := javaCall(t, client, key, unopened, sink, 0); thrownClass(err) != javaThrowNullClass {
		t.Errorf("a null encoding = %v, want NullPointerException", err)
	}
	if _, err := javaCall(t, client, "java/io/OutputStreamWriter.<init>(Ljava/io/OutputStream;)V", unopened, 0); thrownClass(err) != javaThrowNullClass {
		t.Errorf("a null stream = %v, want NullPointerException", err)
	}

	writer := newTestWriter(t, client, sink)
	for _, probe := range []struct {
		name      string
		slot      uint32
		arguments []uint32
		want      string
	}{
		{"chars past the end", 12, []uint32{newTestChars(t, client, "ab"), 1, 2}, javaIndexClass},
		{"a negative offset", 12, []uint32{newTestChars(t, client, "ab"), 0xffffffff, 1}, javaIndexClass},
		{"null chars", 12, []uint32{0, 0, 0}, javaThrowNullClass},
		{"text past the end", 14, []uint32{newTestString(t, client, "ab"), 2, 1}, javaIndexClass},
		{"a negative count", 14, []uint32{newTestString(t, client, "ab"), 0, 0xffffffff}, javaIndexClass},
		{"null text", 14, []uint32{0, 0, 0}, javaThrowNullClass},
	} {
		if _, err := slotCall(t, client, javaStreamWriterClass, probe.slot, append([]uint32{writer}, probe.arguments...)...); thrownClass(err) != probe.want {
			t.Errorf("%s = %v, want %s", probe.name, err, probe.want)
		}
	}
	if got := client.javaRuntimeState().sinks[sink]; len(got) != 0 {
		t.Errorf("a refused write wrote % x", got)
	}

	mustSlot(t, client, javaStreamWriterClass, 16, writer)
	mustSlot(t, client, javaStreamWriterClass, 16, writer)
	for _, probe := range []struct {
		slot      uint32
		arguments []uint32
	}{{10, []uint32{'A'}}, {12, []uint32{newTestChars(t, client, "a"), 0, 1}}, {14, []uint32{newTestString(t, client, "a"), 0, 1}}, {15, nil}} {
		if _, err := slotCall(t, client, javaStreamWriterClass, probe.slot, append([]uint32{writer}, probe.arguments...)...); thrownClass(err) != javaIOExceptionClass {
			t.Errorf("slot %d on a closed writer = %v, want IOException", probe.slot, err)
		}
	}
	// Closing the writer closed the stream under it and let go of it.
	if _, held := client.javaRuntimeState().wrapped[writer]; held {
		t.Error("a closed writer still holds its stream")
	}
}

// A writer over a stream the title wrote hands it every byte through the
// stream's own write(int), by way of OutputStream's write(byte[], int, int),
// each byte widened with its sign as the language widens one.
func TestJavaOutputStreamWriterOverATitlesOwnStream(t *testing.T) {
	client := fixtureClient(t)
	stream := installGuestSubclass(t, client, "game/Out", javaOutputStreamClass, 64, 0,
		guestRoutine{javaOutputStreamSlotWrite, recordingWrite})
	writer := newTestWriter(t, client, stream)
	mustSlot(t, client, javaStreamWriterClass, 14, writer, newTestString(t, client, "A가"), 0, 2)
	if got := recorded(t, client, stream); len(got) != 3 || got[0] != 'A' || got[1] != -0x50 || got[2] != -0x5f {
		t.Errorf("the title's write(int) was handed %v, want [65 -80 -95]", got)
	}
	// flush and close reach the stream's own, which OutputStream gives an
	// empty body.
	mustSlot(t, client, javaStreamWriterClass, 15, writer)
	mustSlot(t, client, javaStreamWriterClass, 16, writer)
}

// Writer's own bodies, for a title's Writer subclass: each write comes to the
// subclass's write(char[], int, int) with the characters it covers. The
// subclass here borrows OutputStreamWriter's range write, so what reached it is
// readable as bytes in a sink.
func TestJavaWriterBaseWritesThroughTheRangeWrite(t *testing.T) {
	client := fixtureClient(t)
	ctx := context.Background()
	writerClass, err := client.preparePlatformJavaClass(javaStreamWriterClass)
	if err != nil {
		t.Fatal(err)
	}
	rangeWrite, err := client.readWord(writerClass.VTable + 4 + javaWriterSlotWriteRange*4)
	if err != nil {
		t.Fatal(err)
	}
	writer := installGuestSubclass(t, client, "game/Text", javaWriterClass, 4, 0)
	guest, _ := client.javaClassOfObject(writer)
	if err := client.writeWord(guest.VTable+4+javaWriterSlotWriteRange*4, rangeWrite); err != nil {
		t.Fatal(err)
	}
	mustJavaCall(t, client, "java/io/Writer.<init>()V", writer)
	sink := newTestSink(t, client)
	client.javaRuntimeState().wrapped[writer] = sink

	mustSlot(t, client, javaWriterClass, 10, writer, 'x')
	mustSlot(t, client, javaWriterClass, 11, writer, newTestChars(t, client, "yz"))
	mustSlot(t, client, javaWriterClass, 14, writer, newTestString(t, client, "0123"), 1, 2)
	// write(String) reaches the receiver's own write(String, int, int), which
	// this subclass did not override: Writer's, which comes back round to the
	// range write.
	if _, err := client.javaCallVirtual(ctx, client.thread, writer, 13, newTestString(t, client, "!")); err != nil {
		t.Fatal(err)
	}
	if got := string(client.javaRuntimeState().sinks[sink]); got != "xyz12!" {
		t.Errorf("the range write was handed %q, want \"xyz12!\"", got)
	}
	for _, probe := range []struct {
		name      string
		slot      uint32
		arguments []uint32
		want      string
	}{
		{"a run past the end", 14, []uint32{newTestString(t, client, "ab"), 1, 2}, javaStringIndexClass},
		{"a negative count", 14, []uint32{newTestString(t, client, "ab"), 0, 0xffffffff}, javaStringIndexClass},
		{"null text", 13, []uint32{0}, javaThrowNullClass},
		{"null chars", 11, []uint32{0}, javaThrowNullClass},
	} {
		if _, err := slotCall(t, client, javaWriterClass, probe.slot, append([]uint32{writer}, probe.arguments...)...); thrownClass(err) != probe.want {
			t.Errorf("%s = %v, want %s", probe.name, err, probe.want)
		}
	}
	if _, err := javaCall(t, client, "java/io/Writer.<init>(Ljava/lang/Object;)V", writer, 0); thrownClass(err) != javaThrowNullClass {
		t.Errorf("Writer(null) = %v, want NullPointerException", err)
	}
	for _, slot := range []uint32{javaWriterSlotWriteRange, 15, 16} {
		if _, served := javaBakedVirtualSlots[javaWriterClass][slot]; served {
			t.Errorf("Writer slot %d is abstract and is served", slot)
		}
	}
}
