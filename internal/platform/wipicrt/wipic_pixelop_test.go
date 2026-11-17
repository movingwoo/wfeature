package wipicrt_test

import (
	"context"
	"encoding/binary"
	"testing"

	"github.com/movingwoo/wfeature/internal/armcore"
	"github.com/movingwoo/wfeature/internal/platform/wipicrt"
	"github.com/movingwoo/wfeature/internal/platform/wipicrt/wipicrttest"
)

// installPixelOperation writes the colour-key operation one local title uses
// into the loaded module and answers its Thumb address:
//
//	if (srcpxl == param1) return orgpxl;
//	return srcpxl;
func installPixelOperation(t *testing.T, runtime *wipicrt.Runtime, address uint32) uint32 {
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
	if err := runtime.Client.Core().Memory().Write(address, data); err != nil {
		t.Fatalf("write pixel operation at %#x: %v", address, err)
	}
	return address | 1
}

// A context field this platform did not write is not a function. One title
// sets a font through the word this handset keeps the operation in, and calling
// its handle would execute an address that is not code.
func TestPixelOperationIsOnlyReadFromACodeAddress(t *testing.T) {
	_, runtime := wipicrttest.NewRuntime(t)
	record, err := runtime.AllocateWIPIC(64)
	if err != nil {
		t.Fatal(err)
	}
	context := record + wipicrt.WIPICAllocationOverhead

	wipicrttest.WriteGuestWords(t, runtime, context+wipicrt.WIPICContextPixelOpOffset, 8, 0x2484)
	op, err := runtime.WIPICReadContextPixelOp(context)
	if err != nil {
		t.Fatal(err)
	}
	if op.Active() {
		t.Fatalf("a font handle was read as a pixel operation: %+v", op)
	}

	function := installPixelOperation(t, runtime, wipicrt.ImageBase+0x40)
	wipicrttest.WriteGuestWords(t, runtime, context+wipicrt.WIPICContextPixelOpOffset, function, 0x2484)
	op, err = runtime.WIPICReadContextPixelOp(context)
	if err != nil {
		t.Fatal(err)
	}
	if op.Function != function || op.Param != 0x2484 {
		t.Fatalf("pixel operation = %+v, want function %#x param %#x", op, function, 0x2484)
	}
}

// The blit runs the guest's operation, which is the whole of one title's
// transparency: the pixels its parameter names keep the target.
func TestBlitRunsTheGuestPixelOperation(t *testing.T) {
	client, runtime := wipicrttest.NewRuntime(t)
	runtime.CurrentThread, runtime.CurrentContext = client.Thread, context.Background()

	const key = uint16(0x2484)
	source, err := runtime.NewWIPICFramebufferRecord(2, 1)
	if err != nil {
		t.Fatal(err)
	}
	destination, err := runtime.NewWIPICFramebufferRecord(2, 1)
	if err != nil {
		t.Fatal(err)
	}
	sourceBuffer, err := runtime.ReadWIPICFramebuffer(source)
	if err != nil {
		t.Fatal(err)
	}
	destinationBuffer, err := runtime.ReadWIPICFramebuffer(destination)
	if err != nil {
		t.Fatal(err)
	}
	// The source is the backdrop colour beside a real one; the destination is
	// a colour neither of them is, so both outcomes are visible.
	pixels := make([]byte, 4)
	binary.LittleEndian.PutUint16(pixels[0:], key)
	binary.LittleEndian.PutUint16(pixels[2:], 0xf800)
	if err := runtime.Client.Core().Memory().Write(sourceBuffer.Pixels, pixels); err != nil {
		t.Fatal(err)
	}
	binary.LittleEndian.PutUint16(pixels[0:], 0x001f)
	binary.LittleEndian.PutUint16(pixels[2:], 0x001f)
	if err := runtime.Client.Core().Memory().Write(destinationBuffer.Pixels, pixels); err != nil {
		t.Fatal(err)
	}

	function := installPixelOperation(t, runtime, wipicrt.ImageBase+0x40)
	if err := runtime.WIPICBlitClipped(
		destinationBuffer, wipicrt.WIPICClip{}, wipicrt.WIPICPixelOp{Function: function, Param: uint32(key)},
		0, 0, 2, 1, sourceBuffer, 0, 0, wipicrt.BlitOpacity{},
	); err != nil {
		t.Fatal(err)
	}

	drawn := make([]byte, 4)
	if err := runtime.Client.Core().Memory().Read(destinationBuffer.Pixels, drawn); err != nil {
		t.Fatal(err)
	}
	if got := binary.LittleEndian.Uint16(drawn[0:]); got != 0x001f {
		t.Errorf("the keyed pixel drew %#x, want the target's own %#x", got, 0x001f)
	}
	if got := binary.LittleEndian.Uint16(drawn[2:]); got != 0xf800 {
		t.Errorf("the opaque pixel drew %#x, want the source's %#x", got, 0xf800)
	}
}

// TestPixelOperationArgumentOrderIsThePlatforms pins which pixel a platform
// hands its operation first: KTF the one being drawn, an SKT Clet the one
// already there. The operation here is `bx lr`, which answers its first
// argument.
func TestPixelOperationArgumentOrderIsThePlatforms(t *testing.T) {
	for _, destinationFirst := range []bool{false, true} {
		client, runtime := wipicrttest.NewRuntimeWith(t, armcore.CoreOptions{}, func(client *wipicrt.Client) {
			client.Platform.PixelOpDestinationFirst = destinationFirst
		})
		runtime.CurrentThread, runtime.CurrentContext = client.Thread, context.Background()
		if err := runtime.Client.Core().Memory().Write(wipicrt.ImageBase+0x40, []byte{0x70, 0x47}); err != nil {
			t.Fatal(err)
		}
		op := wipicrt.WIPICPixelOp{Function: (wipicrt.ImageBase + 0x40) | 1, Param: 7}
		got, err := runtime.ApplyPixelOp(op, 0xf800, 0x001f)
		if err != nil {
			t.Fatal(err)
		}
		want := uint16(0xf800)
		if destinationFirst {
			want = 0x001f
		}
		if got != want {
			t.Fatalf("destination first %v: operation answered %#x, want %#x", destinationFirst, got, want)
		}
	}
}

// TestPixelOperationAnswersLastOneFrame pins how long a cached answer is
// trusted. One title's operation reads a fade level from a global and steps it
// once a frame; an answer kept past the frame it was asked in froze its title
// screen at the first level. The operation here answers a global word.
func TestPixelOperationAnswersLastOneFrame(t *testing.T) {
	client, runtime := wipicrttest.NewRuntime(t)
	runtime.CurrentThread, runtime.CurrentContext = client.Thread, context.Background()
	global, err := runtime.AllocateWords([]uint32{0x1111})
	if err != nil {
		t.Fatal(err)
	}
	code := make([]byte, 12)
	for index, instruction := range []uint16{
		0x4801, // ldr r0, [pc, #4]
		0x6800, // ldr r0, [r0]
		0x4770, // bx lr
		0x46c0, // nop
	} {
		binary.LittleEndian.PutUint16(code[index*2:], instruction)
	}
	binary.LittleEndian.PutUint32(code[8:], global)
	if err := runtime.Client.Core().Memory().Write(wipicrt.ImageBase+0x40, code); err != nil {
		t.Fatal(err)
	}
	op := wipicrt.WIPICPixelOp{Function: (wipicrt.ImageBase + 0x40) | 1}
	ask := func() uint16 {
		t.Helper()
		got, err := runtime.ApplyPixelOp(op, 0xf800, 0x001f)
		if err != nil {
			t.Fatal(err)
		}
		return got
	}
	if got := ask(); got != 0x1111 {
		t.Fatalf("first answer %#x", got)
	}
	wipicrttest.WriteGuestWords(t, runtime, global, 0x2222)
	if got := ask(); got != 0x1111 {
		t.Fatalf("an answer within the frame was asked again: %#x", got)
	}
	screen, err := runtime.WIPICGetScreenFramebuffer()
	if err != nil {
		t.Fatal(err)
	}
	framebuffer, err := runtime.ReadWIPICFramebuffer(screen)
	if err != nil {
		t.Fatal(err)
	}
	if err := runtime.FlushLCDRegion(framebuffer, 0, 0, 1, 1); err != nil {
		t.Fatal(err)
	}
	if got := ask(); got != 0x2222 {
		t.Fatalf("the next frame kept the earlier answer %#x", got)
	}
}
