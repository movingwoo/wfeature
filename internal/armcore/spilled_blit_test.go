package armcore

import "testing"

// The blit this recognises, as one title emits it: the destination in a frame
// slot, the palette reloaded every pixel through a record the frame points at,
// and the whole body the fall-through of a flag it re-tests every pixel. It
// closes 149 million times in a minute of that title's play and is 47.7% of
// every instruction it executes.
//
// r4 walks the source, r6 counts, r5 is the record the guard reads its flag
// from, r3 carries the index and then the colour, r1 the record the palette
// hangs off and r2 the palette and then the destination.
var spilledBlitBody = []uint16{
	0x6f6b, // ldr  r3, [r5, #0x74]  — the guard's flag
	0x2b00, // cmp  r3, #0
	0xd117, // bne  +0x2e — out of the loop, to the blending form of the same blit
	0x990e, // ldr  r1, [sp, #0x38]  — the record the palette hangs off
	0x7823, // ldrb r3, [r4]         — the source byte
	0x688a, // ldr  r2, [r1, #8]     — the palette
	0x005b, // lsls r3, r3, #1
	0x5a9b, // ldrh r3, [r3, r2]     — the colour
	0x9a0a, // ldr  r2, [sp, #0x28]  — the destination
	0x8013, // strh r3, [r2]
	0x9a0a, // ldr  r2, [sp, #0x28]
	0x3e01, // subs r6, #1
	0x3202, // adds r2, #2
	0x3401, // adds r4, #1
	0x920a, // str  r2, [sp, #0x28]  — and back to its slot
	0x2e00, // cmp  r6, #0
	0xd1ee, // bne  -36
}

// Where the test puts the three things the body reaches through memory. All of
// them are in the one read-write page `fillLoopMemory` maps beside the code.
const (
	spilledStack      = 0x70000000
	spilledFrom       = 0x70000400
	spilledRecord     = 0x70000600
	spilledPalette    = 0x70000800
	spilledSlot       = 0x28
	spilledRecordSlot = 0x38
)

// spilledBlitMemory lays out a memory the body can run in: the record pointing
// at the palette, the palette full of colours, the source full of indexes, and
// both frame slots filled in.
func spilledBlitMemory(t *testing.T, body []uint16, base, destination uint32, pixels int) *Memory {
	t.Helper()
	memory := fillLoopMemory(t, body, base, destination)
	memory.beginQuantum()
	defer memory.endQuantum()
	if err := memory.writeData32(spilledStack+spilledSlot, destination); err != nil {
		t.Fatal(err)
	}
	if err := memory.writeData32(spilledStack+spilledRecordSlot, spilledRecord); err != nil {
		t.Fatal(err)
	}
	if err := memory.writeData32(spilledRecord+8, spilledPalette); err != nil {
		t.Fatal(err)
	}
	for index := 0; index < 256; index++ {
		if err := memory.writeData16(spilledPalette+uint32(index)*2, uint16(0x0821*index)); err != nil {
			t.Fatal(err)
		}
	}
	for index := 0; index < pixels; index++ {
		if err := memory.write8(spilledFrom+uint32(index), byte(index*11)); err != nil {
			t.Fatal(err)
		}
	}
	return memory
}

// spilledBlitContext is the register state the body runs from: the guard's
// record in r5, the source in r4 and the pixel count in r6.
func spilledBlitContext(base, guard uint32, pixels int) *Context {
	value := NewContext()
	context := &value
	context.Registers[RegisterSP] = spilledStack
	context.Registers[4] = spilledFrom
	context.Registers[5] = guard
	context.Registers[6] = uint32(pixels)
	context.setThumbPC(base)
	return context
}

func TestASpilledPaletteBlitIsRecognised(t *testing.T) {
	const base, destination = 0x00100000, 0x00200000
	memory := spilledBlitMemory(t, spilledBlitBody, base, destination, 32)
	memory.beginQuantum()
	defer memory.endQuantum()

	branchPC := base + uint32(len(spilledBlitBody)-1)*2
	loop := memory.analyseSpilledBlit(base, branchPC)
	if loop == nil {
		t.Fatal("the spilled palette blit was not recognised")
	}
	if loop.source != 4 || loop.counter != 6 {
		t.Errorf("source=r%d counter=r%d, want r4/r6", loop.source, loop.counter)
	}
	if loop.slot != spilledSlot || loop.record != spilledRecordSlot || loop.colours != 8 {
		t.Errorf("destination at sp+%#x, record at sp+%#x, palette at +%#x; want sp+0x28, sp+0x38, +8",
			loop.slot, loop.record, loop.colours)
	}
	if !loop.haveGuard || loop.guardBase != 5 || loop.guardOffset != 0x74 {
		t.Errorf("guard=%v at r%d+%#x, want a guard at r5+0x74", loop.haveGuard, loop.guardBase, loop.guardOffset)
	}
	if loop.steps != uint32(len(spilledBlitBody)) {
		t.Errorf("steps=%d, want the %d instructions of one iteration", loop.steps, len(spilledBlitBody))
	}
	// The record's register is left holding what it holds now; the
	// destination's is left one pixel past the last store.
	if loop.ends[1] != spilledLeave {
		t.Errorf("r1 ends as %d, want the record pointer it already holds", loop.ends[1])
	}
	if loop.ends[2] != spilledDestination || loop.ends[3] != spilledColour {
		t.Errorf("r2 ends as %d and r3 as %d, want the advanced destination and the last colour",
			loop.ends[2], loop.ends[3])
	}
}

// The bar for standing in for guest code is that the guest cannot tell, and
// this shape leaves more behind than the ones before it: the last colour, the
// record pointer, the destination past the last pixel — in registers, and in
// the frame slot the loop reads its destination out of.
func TestStandingInForASpilledBlitMatchesRunningIt(t *testing.T) {
	const base, destination = 0x00100000, 0x00200000
	const pixels = 61
	branchPC := base + uint32(len(spilledBlitBody)-1)*2

	build := func(refuse bool) (*Memory, *Context) {
		memory := spilledBlitMemory(t, spilledBlitBody, base, destination, pixels)
		memory.standInsRefused = refuse
		return memory, spilledBlitContext(base, spilledStack, pixels)
	}

	interpreted, reference := build(true)
	stood, subject := build(false)
	if _, err := (Engine{}).Run(reference, interpreted, branchPC+2, 1_000_000); err != nil {
		t.Fatalf("interpreting the blit: %v", err)
	}
	if _, err := (Engine{}).Run(subject, stood, branchPC+2, 1_000_000); err != nil {
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
	// The frame slot is as much of the loop's state as any register.
	want, err := interpreted.readData32(spilledStack + spilledSlot)
	if err != nil {
		t.Fatal(err)
	}
	got, err := stood.readData32(spilledStack + spilledSlot)
	if err != nil {
		t.Fatal(err)
	}
	if want != got {
		t.Errorf("the destination slot holds %#x after standing in, %#x after interpreting", got, want)
	}
	// One pixel past the end as well: a stand-in that runs one iteration too
	// many is exactly the mistake this shape invites.
	for index := 0; index <= pixels; index++ {
		address := destination + uint32(index)*2
		want, err := interpreted.readData16(address)
		if err != nil {
			t.Fatal(err)
		}
		got, err := stood.readData16(address)
		if err != nil {
			t.Fatal(err)
		}
		if want != got {
			t.Fatalf("pixel %+d is %#x after standing in, %#x after interpreting", index, got, want)
		}
	}
}

// The step count has to be what the guest would have retired, or a guest that
// would have run out of budget no longer does.
func TestASpilledBlitChargesEveryInstruction(t *testing.T) {
	const base, destination = 0x00100000, 0x00200000
	const pixels = 40
	branchPC := base + uint32(len(spilledBlitBody)-1)*2

	memory := spilledBlitMemory(t, spilledBlitBody, base, destination, pixels)
	result, err := (Engine{}).Run(spilledBlitContext(base, spilledStack, pixels), memory, branchPC+2, 1_000_000)
	if err != nil {
		t.Fatal(err)
	}
	interpreted := spilledBlitMemory(t, spilledBlitBody, base, destination, pixels)
	interpreted.standInsRefused = true
	control, err := (Engine{}).Run(spilledBlitContext(base, spilledStack, pixels), interpreted, branchPC+2, 1_000_000)
	if err != nil {
		t.Fatal(err)
	}
	if result.Steps != control.Steps {
		t.Errorf("charged %d steps, interpreting the same loop retired %d", result.Steps, control.Steps)
	}
}

// A guard that jumps back inside the body is a second loop rather than a way
// out of this one, and nothing here can reason about that.
func TestASpilledBlitWhoseGuardJumpsInsideIsRefused(t *testing.T) {
	const base, destination = 0x00100000, 0x00200000
	body := append([]uint16(nil), spilledBlitBody...)
	body[2] = 0xd102 // bne +4, over two instructions of the body
	memory := spilledBlitMemory(t, body, base, destination, 8)
	memory.beginQuantum()
	defer memory.endQuantum()

	if loop := memory.analyseSpilledBlit(base, base+uint32(len(body)-1)*2); loop != nil {
		t.Fatalf("a guard branching inside the body was recognised: %+v", loop)
	}
}

// A body that writes the register the guard reads through has moved something
// the analysis assumed stood still.
func TestASpilledBlitWhoseGuardBaseMovesIsRefused(t *testing.T) {
	const base, destination = 0x00100000, 0x00200000
	body := append([]uint16(nil), spilledBlitBody...)
	body[13] = 0x3501 // adds r5, #1 — the guard's own base, instead of the source
	memory := spilledBlitMemory(t, body, base, destination, 8)
	memory.beginQuantum()
	defer memory.endQuantum()

	if loop := memory.analyseSpilledBlit(base, base+uint32(len(body)-1)*2); loop != nil {
		t.Fatalf("a body moving the guard's base was recognised: %+v", loop)
	}
}

// The guard's flag is only invariant while the blit cannot write it. A blit
// whose destination covers it is handed back to the interpreter, which takes
// the guard's exit on the iteration the flag changes — and the two arms of
// this test are what says the stand-in would not have.
func TestASpilledBlitOverItsOwnGuardIsRefused(t *testing.T) {
	const base = 0x00100000
	// The destination is the page the guard's flag is in, positioned so the
	// blit runs over it well before it runs out of pixels.
	const destination = spilledStack + 0x100
	const guard = spilledStack + 0x100
	const pixels = 200
	branchPC := base + uint32(len(spilledBlitBody)-1)*2

	// The guard's own exit, pointed at the instruction after the loop so that
	// taking it ends the run rather than falling into a blending routine this
	// test does not have.
	body := append([]uint16(nil), spilledBlitBody...)
	body[2] = 0xd10d
	build := func(refuse bool) (*Memory, *Context) {
		memory := spilledBlitMemory(t, body, base, destination, pixels)
		memory.standInsRefused = refuse
		return memory, spilledBlitContext(base, guard, pixels)
	}
	interpreted, reference := build(true)
	stood, subject := build(false)
	if _, err := (Engine{}).Run(reference, interpreted, branchPC+2, 1_000_000); err != nil {
		t.Fatalf("interpreting the blit: %v", err)
	}
	if _, err := (Engine{}).Run(subject, stood, branchPC+2, 1_000_000); err != nil {
		t.Fatalf("standing in for the blit: %v", err)
	}
	// The point of the arrangement is that the flag does change, so a run that
	// went all the way through has not tested anything.
	if reference.Registers[6] == 0 {
		t.Fatal("the blit never wrote over its own guard, so this proves nothing")
	}
	for register := 0; register < 16; register++ {
		if reference.Registers[register] != subject.Registers[register] {
			t.Errorf("r%d = %#x after standing in, %#x after interpreting",
				register, subject.Registers[register], reference.Registers[register])
		}
	}
	interpreted.beginQuantum()
	stood.beginQuantum()
	defer interpreted.endQuantum()
	defer stood.endQuantum()
	for index := 0; index < pixels; index++ {
		address := uint32(destination) + uint32(index)*2
		want, err := interpreted.readData16(address)
		if err != nil {
			t.Fatal(err)
		}
		got, err := stood.readData16(address)
		if err != nil {
			t.Fatal(err)
		}
		if want != got {
			t.Fatalf("pixel %+d is %#x after standing in, %#x after interpreting", index, got, want)
		}
	}
}
