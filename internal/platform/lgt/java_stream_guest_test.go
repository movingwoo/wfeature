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
		offset += 16
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
