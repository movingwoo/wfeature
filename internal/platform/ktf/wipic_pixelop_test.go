package ktf

import (
	"context"
	"encoding/binary"
	"testing"
)

// writeGuestWords is the little-endian word writer these tests build records
// with.
func writeGuestWords(t *testing.T, runtime *initializationRuntime, address uint32, words ...uint32) {
	t.Helper()
	data := make([]byte, len(words)*4)
	for index, word := range words {
		binary.LittleEndian.PutUint32(data[index*4:], word)
	}
	if err := runtime.client.core.Memory().Write(address, data); err != nil {
		t.Fatalf("write guest words at %#x: %v", address, err)
	}
}

// installPixelOperation writes the colour-key operation one local title uses
// into the loaded module and answers its Thumb address:
//
//	if (srcpxl == param1) return orgpxl;
//	return srcpxl;
func installPixelOperation(t *testing.T, runtime *initializationRuntime, address uint32) uint32 {
	t.Helper()
	body := []uint16{
		0x4290, // cmp r0, r2
		0xd000, // beq +6
		0x4770, // bx lr
		0x1c08, // adds r0, r1, #0
		0x4770, // bx lr
	}
	data := make([]byte, len(body)*2)
	for index, instruction := range body {
		binary.LittleEndian.PutUint16(data[index*2:], instruction)
	}
	if err := runtime.client.core.Memory().Write(address, data); err != nil {
		t.Fatalf("write pixel operation at %#x: %v", address, err)
	}
	return address | 1
}

// A context field this platform did not write is not a function. One title
// sets a font through the word this handset keeps the operation in, and calling
// its handle would execute an address that is not code.
func TestPixelOperationIsOnlyReadFromACodeAddress(t *testing.T) {
	_, runtime := newTestRuntime(t)
	record, err := runtime.allocateWIPIC(64)
	if err != nil {
		t.Fatal(err)
	}
	context := record + wipicAllocationOverhead

	writeGuestWords(t, runtime, context+wipicContextPixelOpOffset, 8, 0x2484)
	op, err := runtime.wipicReadContextPixelOp(context)
	if err != nil {
		t.Fatal(err)
	}
	if op.active() {
		t.Fatalf("a font handle was read as a pixel operation: %+v", op)
	}

	function := installPixelOperation(t, runtime, ImageBase+0x40)
	writeGuestWords(t, runtime, context+wipicContextPixelOpOffset, function, 0x2484)
	op, err = runtime.wipicReadContextPixelOp(context)
	if err != nil {
		t.Fatal(err)
	}
	if op.function != function || op.param != 0x2484 {
		t.Fatalf("pixel operation = %+v, want function %#x param %#x", op, function, 0x2484)
	}
}

// The blit runs the guest's operation, which is the whole of one title's
// transparency: the pixels its parameter names keep the target.
func TestBlitRunsTheGuestPixelOperation(t *testing.T) {
	client, runtime := newTestRuntime(t)
	runtime.currentThread, runtime.currentContext = client.thread, context.Background()

	const key = uint16(0x2484)
	source, err := runtime.newWIPICFramebufferRecord(2, 1)
	if err != nil {
		t.Fatal(err)
	}
	destination, err := runtime.newWIPICFramebufferRecord(2, 1)
	if err != nil {
		t.Fatal(err)
	}
	sourceBuffer, err := runtime.readWIPICFramebuffer(source)
	if err != nil {
		t.Fatal(err)
	}
	destinationBuffer, err := runtime.readWIPICFramebuffer(destination)
	if err != nil {
		t.Fatal(err)
	}
	// The source is the backdrop colour beside a real one; the destination is
	// a colour neither of them is, so both outcomes are visible.
	pixels := make([]byte, 4)
	binary.LittleEndian.PutUint16(pixels[0:], key)
	binary.LittleEndian.PutUint16(pixels[2:], 0xf800)
	if err := runtime.client.core.Memory().Write(sourceBuffer.pixels, pixels); err != nil {
		t.Fatal(err)
	}
	binary.LittleEndian.PutUint16(pixels[0:], 0x001f)
	binary.LittleEndian.PutUint16(pixels[2:], 0x001f)
	if err := runtime.client.core.Memory().Write(destinationBuffer.pixels, pixels); err != nil {
		t.Fatal(err)
	}

	function := installPixelOperation(t, runtime, ImageBase+0x40)
	if err := runtime.wipicBlitClipped(
		destinationBuffer, wipicClip{}, wipicPixelOp{function: function, param: uint32(key)},
		0, 0, 2, 1, sourceBuffer, 0, 0, blitOpacity{},
	); err != nil {
		t.Fatal(err)
	}

	drawn := make([]byte, 4)
	if err := runtime.client.core.Memory().Read(destinationBuffer.pixels, drawn); err != nil {
		t.Fatal(err)
	}
	if got := binary.LittleEndian.Uint16(drawn[0:]); got != 0x001f {
		t.Errorf("the keyed pixel drew %#x, want the target's own %#x", got, 0x001f)
	}
	if got := binary.LittleEndian.Uint16(drawn[2:]); got != 0xf800 {
		t.Errorf("the opaque pixel drew %#x, want the source's %#x", got, 0xf800)
	}
}
