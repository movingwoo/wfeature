package lgt

import (
	"context"
	"encoding/binary"
	"testing"
)

// installGuestStreamClass lays out a class that extends `java/io/InputStream`
// the way a title's own stream subclass is laid out — an entry of its own in
// the slots it overrides, and the platform's inherited entry in the rest — and
// answers an instance of it.
func installGuestStreamClass(t *testing.T, client *Client, overrides map[uint32][]uint16) uint32 {
	t.Helper()
	platform, err := client.preparePlatformJavaClass(javaInputStreamClass)
	if err != nil {
		t.Fatal(err)
	}
	runtime := client.javaRuntimeState()
	class := &javaRuntimeClass{Name: "s", Slots: platform.Slots, Instance: 4, Super: platform}
	object, err := client.allocateJavaClassObject(class)
	if err != nil {
		t.Fatal(err)
	}
	class.Object = object
	class.dataBlock, _ = client.readWord(object + 8)
	runtime.byObject[object] = class
	runtime.byName[class.Name] = class
	if err := client.buildJavaVTable(class); err != nil {
		t.Fatal(err)
	}
	offset := uint32(0)
	for slot, code := range overrides {
		if err := client.writeWord(class.VTable+4+slot*4, installThumbAt(t, client, offset, code...)); err != nil {
			t.Fatal(err)
		}
		// Keep the next routine past this one. Some behavioral fixtures need
		// more than the old fixed eight-instruction allowance, and map order is
		// deliberately unspecified.
		offset += uint32(len(code)) * 2
		offset = (offset + 3) &^ 3
	}
	instance, err := client.allocateJavaObject(class)
	if err != nil {
		t.Fatal(err)
	}
	return instance
}

// installThumbAt is installThumb with room for more than one routine.
func installThumbAt(t *testing.T, client *Client, offset uint32, instructions ...uint16) uint32 {
	t.Helper()
	_, high := client.module.Span()
	address := (high - 256 + offset) &^ 1
	data := make([]byte, len(instructions)*2)
	for index, instruction := range instructions {
		binary.LittleEndian.PutUint16(data[index*2:], instruction)
	}
	if err := client.core.Memory().Write(address, data); err != nil {
		t.Fatal(err)
	}
	return address | 1
}

// A title that hands `DataInputStream` a stream of its own is wrapping the
// abstract class the constructor is declared over, which is legal Java. The
// wrapper reads it through the title's own `read`, so the numbers a data stream
// reads come out of guest code.
func TestDataInputStreamOverAStreamTheTitleWrote(t *testing.T) {
	client := fixtureClient(t)
	// `read()` answers 0x41 for ever: movs r0, #0x41; bx lr.
	instance := installGuestStreamClass(t, client, map[uint32][]uint16{
		javaStreamSlotRead: {0x2041, 0x4770},
	})
	wrapper, err := client.allocateJavaObject(client.javaRuntimeState().byName[javaInputStreamClass])
	if err != nil {
		t.Fatal(err)
	}
	if _, err := javaWrapStream(client, nil, nil, []uint32{wrapper, instance}); err != nil {
		t.Fatalf("wrapping a stream the title wrote: %v", err)
	}

	ctx := context.Background()
	value, err := javaStreamReadInt(client, ctx, client.thread, []uint32{wrapper})
	if err != nil {
		t.Fatalf("readInt() error = %v", err)
	}
	if value != 0x41414141 {
		t.Errorf("readInt() = %#x, want the four bytes the title's own read answered", value)
	}
	// The cursor is one cursor: both objects stand for the same open stream.
	if value, err := javaStreamRead(client, ctx, client.thread, []uint32{instance}); err != nil ||
		value != 0x41 {
		t.Errorf("read() through the stream itself = %#x (%v), want 0x41", value, err)
	}
}

// A stream that has ended stays ended, and the readers above say so rather than
// answering a number made out of nothing.
func TestAGuestStreamThatEndsIsReportedRatherThanPadded(t *testing.T) {
	client := fixtureClient(t)
	// `read()` answers -1 at once: movs r0, #0; subs r0, #1; bx lr.
	instance := installGuestStreamClass(t, client, map[uint32][]uint16{
		javaStreamSlotRead: {0x2000, 0x3801, 0x4770},
	})
	wrapper, err := client.allocateJavaObject(client.javaRuntimeState().byName[javaInputStreamClass])
	if err != nil {
		t.Fatal(err)
	}
	if _, err := javaWrapStream(client, nil, nil, []uint32{wrapper, instance}); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if value, err := javaStreamRead(client, ctx, client.thread, []uint32{wrapper}); err != nil ||
		value != ^uint32(0) {
		t.Errorf("read() at the end = %#x (%v), want -1", value, err)
	}
	if _, err := javaStreamReadInt(client, ctx, client.thread, []uint32{wrapper}); err == nil {
		t.Error("readInt past the end of a stream the title wrote is not reported")
	}
}

// A DataInputStream delegates the mark contract to the InputStream it wraps.
// A guest subclass may implement that contract even though the platform's
// abstract InputStream does not, so the wrapper must inspect and call the
// guest's vtable rather than its own stream flag.
func TestDataInputStreamForwardsMarkContractToAGuestStream(t *testing.T) {
	client := fixtureClient(t)
	input := installGuestStreamClass(t, client, map[uint32][]uint16{
		javaStreamSlotRead: {0x2041, 0x4770},
		// Store the read limit in the instance's first field, making both
		// forwarding and the second argument observable from the host.
		javaStreamSlotMark: {0x6880, 0x6001, 0x4770},
		// Replace that field with 123, proving reset reached its own entry.
		javaStreamSlotReset:         {0x217b, 0x6880, 0x6001, 0x4770},
		javaStreamSlotMarkSupported: {0x2001, 0x4770},
	})
	class, err := client.preparePlatformJavaClass(javaDataInputStreamClass)
	if err != nil {
		t.Fatal(err)
	}
	wrapper, err := client.allocateJavaObject(class)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := javaWrapStream(client, nil, nil, []uint32{wrapper, input}); err != nil {
		t.Fatal(err)
	}

	stream, err := client.javaStreamOf(wrapper)
	if err != nil {
		t.Fatal(err)
	}
	if stream.Source == nil || stream.Source.Mark == 0 || stream.Source.Reset == 0 ||
		stream.Source.MarkSupported == 0 {
		t.Fatalf("guest mark entries were not retained: %+v", stream.Source)
	}
	if got, err := javaStreamMarkSupported(client, t.Context(), client.thread, []uint32{wrapper}); err != nil || got != 1 {
		t.Fatalf("markSupported = %d, %v", got, err)
	}
	if _, err := javaStreamMark(client, t.Context(), client.thread, []uint32{wrapper, 32}); err != nil {
		t.Fatalf("mark: %v", err)
	}
	data, err := client.readWord(input + 8)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := client.readWord(data); err != nil || got != 32 {
		t.Fatalf("guest mark field = %d, %v", got, err)
	}
	if _, err := javaStreamReset(client, t.Context(), client.thread, []uint32{wrapper}); err != nil {
		t.Fatalf("reset: %v", err)
	}
	if got, err := client.readWord(data); err != nil || got != 123 {
		t.Fatalf("guest reset field = %d, %v", got, err)
	}
}

// The wrapper and its guest source must have the same logical cursor when a
// mark is forwarded. Asking a block override for more than the caller needs
// would advance the guest past bytes still buffered on the host.
func TestGuestBlockStreamDoesNotReadAheadOfTheWrapper(t *testing.T) {
	client := fixtureClient(t)
	input := installGuestStreamClass(t, client, map[uint32][]uint16{
		// Record the requested count in the first instance field, then end.
		javaStreamSlotReadBlock: {0x6880, 0x6003, 0x2000, 0x3801, 0x4770},
	})
	class, err := client.preparePlatformJavaClass(javaDataInputStreamClass)
	if err != nil {
		t.Fatal(err)
	}
	wrapper, err := client.allocateJavaObject(class)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := javaWrapStream(client, nil, nil, []uint32{wrapper, input}); err != nil {
		t.Fatal(err)
	}
	if got, err := javaStreamRead(client, t.Context(), client.thread, []uint32{wrapper}); err != nil || got != ^uint32(0) {
		t.Fatalf("read = %#x, %v", got, err)
	}
	data, err := client.readWord(input + 8)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := client.readWord(data); err != nil || got != 1 {
		t.Fatalf("guest read request = %d, %v; want the wrapper's one byte", got, err)
	}
}

// Reset must rewind both halves of a guest-backed wrapper. The guest owns the
// source cursor, while the host can still hold a partial multi-byte read and
// remember that the source reached EOF.
func TestGuestStreamResetRepeatsTheMarkedBytes(t *testing.T) {
	client := fixtureClient(t)
	input := installGuestStreamClass(t, client, map[uint32][]uint16{
		// Cursor 0 and 1 answer 'A' and 'B'; later reads answer -1. The cursor
		// is the first instance field.
		javaStreamSlotRead: {
			0x6881, 0x6808, 0x2802, 0xd204, 0x4602, 0x3201,
			0x600a, 0x3041, 0x4770, 0x2000, 0x3801, 0x4770,
		},
		// Save the cursor in the second instance field.
		javaStreamSlotMark: {0x6882, 0x6813, 0x6053, 0x4770},
		// Restore the cursor from the second instance field.
		javaStreamSlotReset:         {0x6881, 0x684a, 0x600a, 0x4770},
		javaStreamSlotMarkSupported: {0x2001, 0x4770},
	})
	class, err := client.preparePlatformJavaClass(javaDataInputStreamClass)
	if err != nil {
		t.Fatal(err)
	}
	wrapper, err := client.allocateJavaObject(class)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := javaWrapStream(client, nil, nil, []uint32{wrapper, input}); err != nil {
		t.Fatal(err)
	}

	if got, err := javaStreamRead(client, t.Context(), client.thread, []uint32{wrapper}); err != nil || got != 'A' {
		t.Fatalf("first read = %#x, %v", got, err)
	}
	if _, err := javaStreamMark(client, t.Context(), client.thread, []uint32{wrapper, 8}); err != nil {
		t.Fatal(err)
	}
	if _, err := javaStreamReadInt(client, t.Context(), client.thread, []uint32{wrapper}); err == nil {
		t.Fatal("a partial four-byte read did not reach EOF")
	}
	stream, err := client.javaStreamOf(wrapper)
	if err != nil {
		t.Fatal(err)
	}
	if len(stream.Data)-stream.Read != 1 || stream.Data[stream.Read] != 'B' || !stream.Source.Ended {
		t.Fatalf("partial read state = data %v read %d ended %v", stream.Data, stream.Read, stream.Source.Ended)
	}

	if _, err := javaStreamReset(client, t.Context(), client.thread, []uint32{wrapper}); err != nil {
		t.Fatal(err)
	}
	if len(stream.Data) != 0 || stream.Read != 0 || stream.Source.Ended {
		t.Fatalf("reset state = data %v read %d ended %v", stream.Data, stream.Read, stream.Source.Ended)
	}
	if got, err := javaStreamRead(client, t.Context(), client.thread, []uint32{wrapper}); err != nil || got != 'B' {
		t.Fatalf("read after reset = %#x, %v; want the byte at the mark", got, err)
	}
}

// An object that overrides neither `read` is not a stream, and saying which
// object and which class is what separates that from "this platform did not
// open it".
func TestAnObjectThatOverridesNoReadIsNotAStream(t *testing.T) {
	client := fixtureClient(t)
	instance := installGuestStreamClass(t, client, nil)
	wrapper, err := client.allocateJavaObject(client.javaRuntimeState().byName[javaInputStreamClass])
	if err != nil {
		t.Fatal(err)
	}
	_, err = javaWrapStream(client, nil, nil, []uint32{wrapper, instance})
	if err == nil {
		t.Fatal("an object with no read of its own was accepted as a stream")
	}
	if got := err.Error(); got == "" {
		t.Errorf("the refusal says nothing: %q", got)
	}
}
