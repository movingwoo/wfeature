package armcore

import (
	"context"
	"testing"
)

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

// The same counted fill, ended the other way: the count is tested against zero
// rather than read out of the subtraction's borrow. Three titles in the local
// corpus spend 19%, 29% and 69% of their instructions in a loop of this shape,
// and before the analyser knew this ending it refused every one of them.
var zeroTestedFillBody = []uint16{
	0x3b01, // subs r3, #1
	0x800f, // strh r7, [r1]
	0x3102, // adds r1, #2
	0x2b00, // cmp  r3, #0
	0xd1fa, // bne  -12
}

func TestAZeroTestedFillIsRecognised(t *testing.T) {
	const base, destination = 0x00100000, 0x00200000
	memory := fillLoopMemory(t, zeroTestedFillBody, base, destination)
	memory.beginQuantum()
	defer memory.endQuantum()

	branchPC := base + uint32(len(zeroTestedFillBody)-1)*2
	loop := memory.analyseStoreLoop(base, branchPC)
	if loop == nil {
		t.Fatal("the zero-tested fill was not recognised")
	}
	if loop.terminate != terminateNotZero {
		t.Errorf("terminate=%d, want the zero test", loop.terminate)
	}
	if loop.pointer != 1 || loop.counter != 3 || loop.value != 7 || loop.width != 2 {
		t.Errorf("pointer=r%d counter=r%d value=r%d width=%d, want r1/r3/r7/2",
			loop.pointer, loop.counter, loop.value, loop.width)
	}
	if loop.reload {
		t.Error("reload is set, but this body holds its value in a register")
	}
}

// A body carrying a zero test that the branch does not read, or reads the
// wrong way, proves nothing and has to be refused rather than guessed at.
func TestAZeroTestedFillIsRefusedWhenItProvesNothing(t *testing.T) {
	const base, destination = 0x00100000, 0x00200000
	for name, body := range map[string][]uint16{
		// `bhs` after a `cmp #0` never stops: the compare cannot borrow, so
		// the carry it leaves is always set.
		"the branch reads the carry the compare always sets": {
			0x3b01, 0x800f, 0x3102, 0x2b00, 0xd2fa,
		},
		// The test is not on the register the body counts down.
		"the compare is not on the counter": {
			0x3b01, 0x800f, 0x3102, 0x2c00, 0xd1fa,
		},
		// The test is not the last thing before the branch, so the flags the
		// branch reads came from somewhere else.
		"the compare is not last": {
			0x3b01, 0x2b00, 0x800f, 0x3102, 0xd1fa,
		},
		// A compare against anything but zero is a different loop.
		"the compare is against one": {
			0x3b01, 0x800f, 0x3102, 0x2b01, 0xd1fa,
		},
	} {
		memory := fillLoopMemory(t, body, base, destination)
		memory.beginQuantum()
		branchPC := base + uint32(len(body)-1)*2
		if loop := memory.analyseStoreLoop(base, branchPC); loop != nil {
			t.Errorf("%s: recognised, want refused", name)
		}
		memory.endQuantum()
	}
}

func TestStandingInForAZeroTestedFillMatchesRunningIt(t *testing.T) {
	const base, destination = 0x00100000, 0x00200000
	const value, count = uint32(0xbeef), uint32(40)

	interpreted := fillLoopMemory(t, zeroTestedFillBody, base, destination)
	stood := fillLoopMemory(t, zeroTestedFillBody, base, destination)

	setup := func() *Context {
		holder := NewContext()
		context := &holder
		context.Registers[1] = destination
		context.Registers[3] = count
		context.Registers[7] = value
		context.setThumbPC(base)
		return context
	}

	branchPC := base + uint32(len(zeroTestedFillBody)-1)*2
	interpreted.standInsRefused = true
	reference := setup()
	if _, err := (Engine{}).Run(reference, interpreted, branchPC+2, 100000); err != nil {
		t.Fatalf("interpreting the loop: %v", err)
	}
	subject := setup()
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

// A count of one is the loop's own edge: the borrow ending runs twice there and
// the zero test once, and a stand-in that charged the wrong one would leave the
// pointer a halfword out. A count of zero is the guest's own runaway — the
// zero-tested loop stores four billion times — and the stand-in must decline it
// rather than reproduce it.
func TestAZeroTestedFillAtItsEdges(t *testing.T) {
	const base, destination = 0x00100000, 0x00200000
	for _, count := range []uint32{0, 1, 2, 3} {
		interpreted := fillLoopMemory(t, zeroTestedFillBody, base, destination)
		stood := fillLoopMemory(t, zeroTestedFillBody, base, destination)
		setup := func() *Context {
			holder := NewContext()
			context := &holder
			context.Registers[1] = destination
			context.Registers[3] = count
			context.Registers[7] = 0xbeef
			context.setThumbPC(base)
			return context
		}
		branchPC := base + uint32(len(zeroTestedFillBody)-1)*2
		budget := uint32(100000)
		if count == 0 {
			// The real loop never ends. Both sides get the same budget, and
			// what is checked is that they spent it in the same place.
			budget = 5000
		}
		interpreted.standInsRefused = true
		reference := setup()
		if _, err := (Engine{}).Run(reference, interpreted, branchPC+2, budget); err != nil {
			t.Fatalf("count %d: interpreting: %v", count, err)
		}
		subject := setup()
		if _, err := (Engine{}).Run(subject, stood, branchPC+2, budget); err != nil {
			t.Fatalf("count %d: standing in: %v", count, err)
		}
		for register := 0; register < 16; register++ {
			if reference.Registers[register] != subject.Registers[register] {
				t.Errorf("count %d: r%d = %#x after standing in, %#x after interpreting",
					count, register, subject.Registers[register], reference.Registers[register])
			}
		}
		if reference.CPSR != subject.CPSR {
			t.Errorf("count %d: CPSR = %#x after standing in, %#x after interpreting",
				count, subject.CPSR, reference.CPSR)
		}
	}
}

// The same fill again, reaching its destination through a base and an index
// the body never moves. One title spends 29% of its instructions here.
var indexedFillBody = []uint16{
	0x3d01, // subs r5, #1
	0x5298, // strh r0, [r3, r2]
	0x3302, // adds r3, #2
	0x2d00, // cmp  r5, #0
	0xd1fa, // bne  -12
}

func TestAnIndexedFillIsRecognised(t *testing.T) {
	const base, destination = 0x00100000, 0x00200000
	memory := fillLoopMemory(t, indexedFillBody, base, destination)
	memory.beginQuantum()
	defer memory.endQuantum()

	loop := memory.analyseStoreLoop(base, base+uint32(len(indexedFillBody)-1)*2)
	if loop == nil {
		t.Fatal("the indexed fill was not recognised")
	}
	if !loop.indexed || loop.index != 2 || loop.pointer != 3 || loop.counter != 5 || loop.value != 0 {
		t.Errorf("indexed=%v index=r%d pointer=r%d counter=r%d value=r%d, want r2/r3/r5/r0",
			loop.indexed, loop.index, loop.pointer, loop.counter, loop.value)
	}
}

// The index has to stand still, and the form only ever means a halfword store.
func TestAnIndexedFillIsRefusedWhenTheIndexMoves(t *testing.T) {
	const base, destination = 0x00100000, 0x00200000
	for name, body := range map[string][]uint16{
		// `adds r2, #2` moves the index the store reads.
		"the index is advanced too": {
			0x3d01, 0x5298, 0x3302, 0x3202, 0x2d00, 0xd1f9,
		},
		// `ldrh r0, [r3, r2]` is a load, not the store this stands in for.
		"the transfer is a load": {
			0x3d01, 0x5a98, 0x3302, 0x2d00, 0xd1fa,
		},
		// `str r0, [r3, r2]` is a word store, a different width.
		"the transfer is a word": {
			0x3d01, 0x5098, 0x3302, 0x2d00, 0xd1fa,
		},
	} {
		memory := fillLoopMemory(t, body, base, destination)
		memory.beginQuantum()
		if loop := memory.analyseStoreLoop(base, base+uint32(len(body)-1)*2); loop != nil {
			t.Errorf("%s: recognised, want refused", name)
		}
		memory.endQuantum()
	}
}

func TestStandingInForAnIndexedFillMatchesRunningIt(t *testing.T) {
	const base, destination = 0x00100000, 0x00200000
	const index, count = uint32(0x40), uint32(33)

	interpreted := fillLoopMemory(t, indexedFillBody, base, destination)
	stood := fillLoopMemory(t, indexedFillBody, base, destination)
	setup := func() *Context {
		holder := NewContext()
		context := &holder
		context.Registers[0] = 0x1234
		context.Registers[2] = index
		context.Registers[3] = destination
		context.Registers[5] = count
		context.setThumbPC(base)
		return context
	}
	branchPC := base + uint32(len(indexedFillBody)-1)*2
	interpreted.standInsRefused = true
	reference := setup()
	if _, err := (Engine{}).Run(reference, interpreted, branchPC+2, 100000); err != nil {
		t.Fatalf("interpreting: %v", err)
	}
	subject := setup()
	if _, err := (Engine{}).Run(subject, stood, branchPC+2, 100000); err != nil {
		t.Fatalf("standing in: %v", err)
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
	for offset := uint32(0); offset < index+(count+2)*2; offset += 2 {
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
	interpreted.standInsRefused = true
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

// fillLoopSpanMemory is fillLoopMemory with a destination wide enough for a
// stand-in to cross a page inside one run. Pages are what the direct route is
// handed out in, so a span that fits in one never exercises the seam.
func fillLoopSpanMemory(t *testing.T, body []uint16, base, destination uint32, pages uint64) *Memory {
	t.Helper()
	memory := NewMemory()
	if err := memory.Map(base, memoryPageSize, PermissionRead|PermissionExecute); err != nil {
		t.Fatal(err)
	}
	if err := memory.Map(destination, memoryPageSize*pages, PermissionRead|PermissionWrite); err != nil {
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

// A stand-in reaches guest bytes a page at a time, so a fill longer than a
// page is the case where the seam between two pages can be got wrong — a
// halfword dropped at the boundary, or a page's worth written twice.
func TestStandingInForAFillAcrossPagesMatchesRunningIt(t *testing.T) {
	const base, destination = 0x00100000, 0x00200000
	const stack, colour = 0x70000800, uint16(0xbeef)
	// Two and a half pages of halfwords, started part way into the first page
	// so that neither seam falls where the run begins.
	const start = destination + 0x800
	const count = uint32(memoryPageSize*2+memoryPageSize/2) / 2

	interpreted := fillLoopSpanMemory(t, countedFillBody, base, destination, 4)
	stood := fillLoopSpanMemory(t, countedFillBody, base, destination, 4)

	setup := func(memory *Memory) *Context {
		memory.beginQuantum()
		if err := memory.writeData16(stack+24, colour); err != nil {
			t.Fatal(err)
		}
		memory.endQuantum()
		value := NewContext()
		context := &value
		context.Registers[RegisterSP] = stack
		context.Registers[4] = start
		context.Registers[5] = count
		context.setThumbPC(base)
		return context
	}

	branchPC := base + uint32(len(countedFillBody)-1)*2
	reference := setup(interpreted)
	interpreted.standInsRefused = true
	if _, err := (Engine{}).Run(reference, interpreted, branchPC+2, 10_000_000); err != nil {
		t.Fatalf("interpreting the fill: %v", err)
	}
	subject := setup(stood)
	if _, err := (Engine{}).Run(subject, stood, branchPC+2, 10_000_000); err != nil {
		t.Fatalf("standing in for the fill: %v", err)
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
	// One halfword past each end as well, because a run that overshoots its
	// span is exactly what a page-at-a-time loop can do.
	for offset := -2; offset < int(count+1)*2; offset += 2 {
		address := uint32(int(start) + offset)
		want, err := interpreted.readData16(address)
		if err != nil {
			t.Fatal(err)
		}
		got, err := stood.readData16(address)
		if err != nil {
			t.Fatal(err)
		}
		if want != got {
			t.Fatalf("halfword at %#x is %#x after standing in, %#x after interpreting", address, got, want)
		}
	}
}

// A watched address has to report every store to it, and a stand-in is where
// that is easiest to lose: it writes thousands of halfwords without executing
// a store instruction. The direct route refuses a watched memory outright, so
// the stores still go through the path that records them.
func TestAStandInStillReportsWatchedStores(t *testing.T) {
	const base, destination = 0x00100000, 0x00200000
	const stack, colour, count = 0x70000800, uint16(0xbeef), uint32(64)
	const watched = destination + 8

	core := NewCore(CoreOptions{MaxSteps: 1_000_000})
	memory := core.Memory()
	for address, permission := range map[uint32]Permission{
		base:        PermissionReadExecute,
		destination: PermissionReadWrite,
		0x70000000:  PermissionReadWrite,
	} {
		if err := memory.Map(address, memoryPageSize, permission); err != nil {
			t.Fatal(err)
		}
	}
	code := make([]byte, len(countedFillBody)*2)
	for index, halfword := range countedFillBody {
		code[index*2] = byte(halfword)
		code[index*2+1] = byte(halfword >> 8)
	}
	if err := memory.Load(base, code); err != nil {
		t.Fatal(err)
	}
	if err := memory.Write(stack+24, []byte{byte(colour & 0xff), byte(colour >> 8)}); err != nil {
		t.Fatal(err)
	}
	core.Watch(watched)

	initial := NewContext()
	if err := initial.SetPC(base | 1); err != nil {
		t.Fatal(err)
	}
	initial.Registers[RegisterSP] = stack
	initial.Registers[4] = destination
	initial.Registers[5] = count
	branchPC := base + uint32(len(countedFillBody)-1)*2
	if _, err := core.Run(context.Background(), NewThread(initial), branchPC+2, nil); err != nil {
		t.Fatal(err)
	}

	hits := core.WatchHits()
	if len(hits) != 1 {
		t.Fatalf("got %d watch hits, want the one store the fill makes to %#x: %+v", len(hits), watched, hits)
	}
	if hits[0].Address != watched || hits[0].Value != uint32(colour) || hits[0].Size != 2 {
		t.Errorf("hit = %+v, want %#x written as %#x in two bytes", hits[0], watched, colour)
	}
	if hits[0].Origin != OriginGuest {
		t.Errorf("hit origin = %s, want the guest's own store", hits[0].Origin)
	}
}
