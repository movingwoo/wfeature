package armcore

import "testing"

// The loop this recognises, as one title actually emits it: the stack pointer
// copied into a low register, a colour read from a frame slot, a halfword
// stored through a walking pointer, and a counter branched on. It was 43.6% of
// that title's guest instructions in a scene a person reported as slow.
var countedFillBody = []uint16{
	0x4668, // mov  r0, sp
	0x8b00, // ldrh r0, [r0, #24]
	0x8020, // strh r0, [r4]
	0x3402, // adds r4, #2
	0x3d01, // subs r5, #1
	0xd2f9, // bhs  -14
}

// fillLoopMemory maps the body at base with a writable destination after it.
func fillLoopMemory(t *testing.T, body []uint16, base, destination uint32) *Memory {
	t.Helper()
	memory := NewMemory()
	if err := memory.Map(base, memoryPageSize, PermissionRead|PermissionExecute); err != nil {
		t.Fatal(err)
	}
	if err := memory.Map(destination, memoryPageSize, PermissionRead|PermissionWrite); err != nil {
		t.Fatal(err)
	}
	if err := memory.Map(0x70000000, memoryPageSize, PermissionRead|PermissionWrite); err != nil {
		t.Fatal(err)
	}
	code := make([]byte, len(body)*2)
	for index, halfword := range body {
		code[index*2] = byte(halfword)
		code[index*2+1] = byte(halfword >> 8)
	}
	if err := memory.Load(base, code); err != nil {
		t.Fatal(err)
	}
	return memory
}

func TestACountedFillIsRecognised(t *testing.T) {
	const base, destination = 0x00100000, 0x00200000
	memory := fillLoopMemory(t, countedFillBody, base, destination)
	memory.beginQuantum()
	defer memory.endQuantum()

	branchPC := base + uint32(len(countedFillBody)-1)*2
	loop := memory.analyseStoreLoop(base, branchPC)
	if loop == nil {
		t.Fatal("the counted fill was not recognised")
	}
	if loop.pointer != 4 || loop.counter != 5 || loop.width != 2 {
		t.Errorf("pointer=r%d counter=r%d width=%d, want r4/r5/2", loop.pointer, loop.counter, loop.width)
	}
	if !loop.reload || loop.offset != 24 || loop.value != 0 {
		t.Errorf("reload=%v offset=%d value=r%d, want a reload of r0 from sp+24", loop.reload, loop.offset, loop.value)
	}
	if loop.steps != 6 {
		t.Errorf("steps=%d, want the six instructions of one iteration", loop.steps)
	}
}

// Standing in for the loop has to leave memory and registers exactly where the
// guest would have left them, because the guest goes on reading both.
func TestStandingInForAFillMatchesRunningIt(t *testing.T) {
	const base, destination = 0x00100000, 0x00200000
	const stack, colour, count = 0x70000800, uint16(0xbeef), uint32(40)

	interpreted := fillLoopMemory(t, countedFillBody, base, destination)
	stood := fillLoopMemory(t, countedFillBody, base, destination)

	setup := func(memory *Memory) *Context {
		memory.beginQuantum()
		if err := memory.writeData16(stack+24, colour); err != nil {
			t.Fatal(err)
		}
		memory.endQuantum()
		value := NewContext()
		context := &value
		context.Registers[RegisterSP] = stack
		context.Registers[4] = destination
		context.Registers[5] = count
		context.setThumbPC(base)
		return context
	}

	// The interpreter's answer, one instruction at a time.
	reference := setup(interpreted)
	branchPC := base + uint32(len(countedFillBody)-1)*2
	if _, err := (Engine{}).Run(reference, interpreted, branchPC+2, 100000); err != nil {
		t.Fatalf("interpreting the loop: %v", err)
	}

	// The same loop with the stand-in reached through the engine's own hook.
	subject := setup(stood)
	if _, err := (Engine{}).Run(subject, stood, branchPC+2, 100000); err != nil {
		t.Fatalf("standing in for the loop: %v", err)
	}

	for register := 0; register < 16; register++ {
		if reference.Registers[register] != subject.Registers[register] {
			t.Errorf("r%d = %#x after standing in, %#x after interpreting",
				register, subject.Registers[register], reference.Registers[register])
		}
	}
	if reference.CPSR != subject.CPSR {
		t.Errorf("CPSR = %#x after standing in, %#x after interpreting", subject.CPSR, reference.CPSR)
	}

	interpreted.beginQuantum()
	stood.beginQuantum()
	defer interpreted.endQuantum()
	defer stood.endQuantum()
	for offset := uint32(0); offset < (count+2)*2; offset += 2 {
		want, err := interpreted.readData16(destination + offset)
		if err != nil {
			t.Fatal(err)
		}
		got, err := stood.readData16(destination + offset)
		if err != nil {
			t.Fatal(err)
		}
		if want != got {
			t.Fatalf("halfword at +%d is %#x after standing in, %#x after interpreting", offset, got, want)
		}
	}
}

// tableBlitProgram is the blit this recognises, as the same title emits it: a
// source byte indexes a palette, the colour goes out sixteen bits at a time,
// and a sixteen-bit counter runs to a limit held in a literal. It was 28.6% of
// that title's guest instructions in the scene a person reported as slow.
//
// r1 walks the source, r0 the destination, r6 holds the palette, r2 counts,
// and r3 carries the index and then the colour.
func tableBlitProgram() []uint16 {
	return []uint16{
		0x780b, // ldrb  r3, [r1]
		0x3101, // adds  r1, #1
		0x005b, // lsls  r3, r3, #1
		0x5b9b, // ldrh  r3, [r3, r6]
		0x8003, // strh  r3, [r0]
		0x1e53, // subs  r3, r2, #1
		0x041b, // lsls  r3, r3, #16
		0x0c1a, // lsrs  r2, r3, #16
		0x4b04, // ldr   r3, [pc, #16]
		0x3002, // adds  r0, #2
		0x429a, // cmp   r2, r3
		0xd1f3, // bne   -26
	}
}

func TestATableBlitIsRecognised(t *testing.T) {
	const base, destination = 0x00100000, 0x00200000
	body := tableBlitProgram()
	memory := fillLoopMemory(t, body, base, destination)
	memory.beginQuantum()
	defer memory.endQuantum()

	branchPC := base + uint32(len(body)-1)*2
	loop := memory.analyseTableBlit(base, branchPC)
	if loop == nil {
		t.Fatal("the table blit was not recognised")
	}
	if loop.source != 1 || loop.destination != 0 || loop.table != 6 {
		t.Errorf("source=r%d destination=r%d table=r%d, want r1/r0/r6",
			loop.source, loop.destination, loop.table)
	}
	if loop.counter != 2 || loop.limit != 3 {
		t.Errorf("counter=r%d limit=r%d, want r2/r3", loop.counter, loop.limit)
	}
	if loop.steps != 12 {
		t.Errorf("steps=%d, want the twelve instructions of one pixel", loop.steps)
	}
}

// Standing in for the blit has to be indistinguishable from running it: the
// pixels, the pointers it leaves behind, and the flags the next instruction
// reads.
func TestStandingInForATableBlitMatchesRunningIt(t *testing.T) {
	const base, destination = 0x00100000, 0x00200000
	const palette, source = 0x70000000, 0x70000800
	const pixels = 37

	body := tableBlitProgram()
	branchPC := base + uint32(len(body)-1)*2

	build := func() (*Memory, *Context) {
		memory := fillLoopMemory(t, body, base, destination)
		memory.beginQuantum()
		// The literal the counter is compared against sits where
		// `ldr r3, [pc, #16]` reaches it. It goes in with Load, because the
		// code it belongs to is mapped without write permission — which is
		// what makes it loop-invariant in the first place.
		memory.endQuantum()
		if err := memory.Load(((base+16)+4)&^3+16, []byte{0, 0, 0, 0}); err != nil {
			t.Fatal(err)
		}
		memory.beginQuantum()
		for index := 0; index < 256; index++ {
			if err := memory.writeData16(palette+uint32(index)*2, uint16(0x1000+index*7)); err != nil {
				t.Fatal(err)
			}
		}
		for pixel := 0; pixel < pixels; pixel++ {
			if err := memory.write8(source+uint32(pixel), byte(pixel*5+3)); err != nil {
				t.Fatal(err)
			}
		}
		memory.endQuantum()

		value := NewContext()
		context := &value
		context.Registers[0] = destination
		context.Registers[1] = source
		context.Registers[2] = pixels
		context.Registers[6] = palette
		context.setThumbPC(base)
		return memory, context
	}

	interpreted, reference := build()
	stood, subject := build()

	// The reference runs with the recogniser unable to fire, so the two sides
	// differ in nothing but whether the stand-in was allowed.
	interpreted.refusedLoops = map[uint32]bool{base: true}
	if _, err := (Engine{}).Run(reference, interpreted, branchPC+2, 1000000); err != nil {
		t.Fatalf("interpreting the blit: %v", err)
	}
	if _, err := (Engine{}).Run(subject, stood, branchPC+2, 1000000); err != nil {
		t.Fatalf("standing in for the blit: %v", err)
	}

	for register := 0; register < 16; register++ {
		if reference.Registers[register] != subject.Registers[register] {
			t.Errorf("r%d = %#x after standing in, %#x after interpreting",
				register, subject.Registers[register], reference.Registers[register])
		}
	}
	if reference.CPSR != subject.CPSR {
		t.Errorf("CPSR = %#x after standing in, %#x after interpreting", subject.CPSR, reference.CPSR)
	}

	interpreted.beginQuantum()
	stood.beginQuantum()
	defer interpreted.endQuantum()
	defer stood.endQuantum()
	for pixel := uint32(0); pixel < pixels+2; pixel++ {
		want, err := interpreted.readData16(destination + pixel*2)
		if err != nil {
			t.Fatal(err)
		}
		got, err := stood.readData16(destination + pixel*2)
		if err != nil {
			t.Fatal(err)
		}
		if want != got {
			t.Fatalf("pixel %d is %#x after standing in, %#x after interpreting", pixel, got, want)
		}
	}
}
