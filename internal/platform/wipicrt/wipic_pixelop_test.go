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

// TestPixelOperationThatReadsMemoryAnswersForOneFrame pins how long a cached
// answer of an operation that reads the title's memory is trusted. One title's
// operation reads a fade level from a global and steps it once a frame; an
// answer kept past the frame it was asked in froze its title screen at the
// first level. The operation here answers a global word.
func TestPixelOperationThatReadsMemoryAnswersForOneFrame(t *testing.T) {
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
	publishFrame(t, runtime)
	if got := ask(); got != 0x2222 {
		t.Fatalf("the next frame kept the earlier answer %#x", got)
	}
}

// TestPixelOperationReadingOnlyItsArgumentsKeepsItsAnswers pins which
// operations keep their answers past a frame: those whose code touches
// nothing but their arguments, their stack and their literal pool, following
// both ways of every branch and into what they call. Each operation here
// answers 1 from an instruction that is rewritten after the frame to answer
// 2, so one that is asked again answers 2.
func TestPixelOperationReadingOnlyItsArgumentsKeepsItsAnswers(t *testing.T) {
	long := make([]uint16, 300)
	for index := range long {
		long[index] = 0x46c0 // nop
	}
	long = append(long, 0x2001, 0x4770)
	tests := []struct {
		name string
		code []uint16
		// answer is the halfword of the movs r0, #1; literal, when not
		// zero, the one a global's address is put at.
		answer, literal int
		kept            bool
	}{
		{name: "leaf", code: []uint16{
			0x2001, // movs r0, #1
			0x4770, // bx lr
		}, answer: 0, kept: true},
		{name: "stack", code: []uint16{
			0xb082, // sub sp, #8
			0x9000, // str r0, [sp]
			0x9800, // ldr r0, [sp]
			0xb002, // add sp, #8
			0x2001, // movs r0, #1
			0x4770, // bx lr
		}, answer: 4, kept: true},
		{name: "loop", code: []uint16{
			0x2103, // movs r1, #3
			0x3901, // subs r1, #1
			0xd1fd, // bne back to the subs
			0x2001, // movs r0, #1
			0x4770, // bx lr
		}, answer: 3, kept: true},
		{name: "call", code: []uint16{
			0xb500,         // push {lr}
			0xf000, 0xf802, // bl to the bx lr
			0x2001, // movs r0, #1
			0xbd00, // pop {pc}
			0x4770, // bx lr
		}, answer: 3, kept: true},
		{name: "call that reads memory", code: []uint16{
			0xb500,         // push {lr}
			0xf000, 0xf802, // bl to the ldr
			0x2001, // movs r0, #1
			0xbd00, // pop {pc}
			0x4b01, // ldr r3, [pc, #4]
			0x681b, // ldr r3, [r3]
			0x4770, // bx lr
			0, 0,   // the global's address
		}, answer: 3, literal: 8},
		{name: "untaken platform call", code: []uint16{
			0x2100, // movs r1, #0
			0x2900, // cmp r1, #0
			0xd000, // beq past the svc
			0xdf00, // svc #0
			0x2001, // movs r0, #1
			0x4770, // bx lr
		}, answer: 4},
		{name: "jump through a register", code: []uint16{
			0xa301, // add r3, pc, #4
			0x3301, // adds r3, #1
			0x4718, // bx r3
			0x46c0, // nop
			0x2001, // movs r0, #1
			0x4770, // bx lr
		}, answer: 4},
		{name: "longer than the reading", code: long, answer: 300},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			client, runtime := wipicrttest.NewRuntime(t)
			runtime.CurrentThread, runtime.CurrentContext = client.Thread, context.Background()
			code := make([]byte, 2*len(test.code))
			for index, instruction := range test.code {
				binary.LittleEndian.PutUint16(code[index*2:], instruction)
			}
			if test.literal != 0 {
				global, err := runtime.AllocateWords([]uint32{0x1111})
				if err != nil {
					t.Fatal(err)
				}
				binary.LittleEndian.PutUint32(code[test.literal*2:], global)
			}
			// The test image is too small for the longest, so the code
			// gets a page of its own.
			const at = 0x00200000
			memory := runtime.Client.Core().Memory()
			if err := memory.Map(at, 0x1000, armcore.PermissionReadWriteExecute); err != nil {
				t.Fatal(err)
			}
			if err := memory.Write(at, code); err != nil {
				t.Fatal(err)
			}
			op := wipicrt.WIPICPixelOp{Function: at | 1}
			ask := func() uint16 {
				t.Helper()
				got, err := runtime.ApplyPixelOp(op, 0xf800, 0x001f)
				if err != nil {
					t.Fatal(err)
				}
				return got
			}
			if got := ask(); got != 1 {
				t.Fatalf("first answer %#x", got)
			}
			publishFrame(t, runtime)
			if err := memory.Write(at+uint32(test.answer)*2, []byte{0x02, 0x20}); err != nil {
				t.Fatal(err)
			}
			want := uint16(2)
			if test.kept {
				want = 1
			}
			if got := ask(); got != want {
				t.Fatalf("after the frame the operation answered %#x, want %#x", got, want)
			}
		})
	}
}

// publishFrame flushes the screen as a title does at the end of a frame.
func publishFrame(t *testing.T, runtime *wipicrt.Runtime) {
	t.Helper()
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
}

// TestAlphaInstallsTheBuiltinBlend pins the specification's alpha: setting it
// installs the platform's blend as the operation, so a title that sets an
// alpha and draws with no operation of its own draws translucent — one SKT
// title's card slots came out as black squares — and a title that installs
// its own, or clears it, replaces the blend.
func TestAlphaInstallsTheBuiltinBlend(t *testing.T) {
	const red, blue = 0xf800, 0x001f
	fill := func(alpha uint32, clear bool) uint16 {
		_, runtime := wipicrttest.NewRuntime(t)
		destination, framebuffer := surface(t, runtime, blue)
		context, err := runtime.AllocateBytes(make([]byte, wipicrt.WIPICGraphicsContextSize))
		if err != nil {
			t.Fatal(err)
		}
		contextCall(t, runtime, true, context, 1, red)
		contextCall(t, runtime, true, context, 4, alpha)
		op, err := runtime.WIPICReadContextPixelOp(context)
		if err != nil {
			t.Fatal(err)
		}
		if op.Function != wipicrt.WIPICBuiltinAlphaOp || op.Param != alpha {
			t.Fatalf("operation after the alpha = %+v", op)
		}
		if clear {
			contextCall(t, runtime, true, context, 5, 0)
		}
		tableCall(t, runtime, 11, destination, 0, 0, 1, 1, context)
		return wipicrttest.ReadSurfacePixels(t, runtime, framebuffer)[0]
	}
	if got := fill(255, false); got != red {
		t.Errorf("alpha 255 drew %#x, want %#x", got, red)
	}
	if got := fill(0, false); got != blue {
		t.Errorf("alpha 0 drew %#x, want the surface's %#x", got, blue)
	}
	// Half of red over blue, a channel at a time: 16 of 31 red, 15 of 31 blue.
	if got, want := fill(128, false), uint16(16<<11|15); got != want {
		t.Errorf("alpha 128 drew %#x, want %#x", got, want)
	}
	if got := fill(128, true); got != red {
		t.Errorf("a cleared operation drew %#x, want %#x", got, red)
	}
}
