package armcore

import "testing"

// The blending arm of a clipped blit and the writer behind it, laid out after
// the loop's own body so the flag's `bne` reaches the arm, the arm's call
// reaches the writer, and the arm's `b` comes back into the body at the
// write-back. `clippedBlitBody` already branches to the halfword after itself,
// which is where this begins.
//
// The writer has the shape the reported title's has and none of its arithmetic:
// a prologue that reaches a record through a literal, a mode read out of that
// record, a dispatch through a jump table it reads out of memory, and behind
// the dispatch two handlers — one that stores the colour and one that blends.
// Everything down to the handler folds to constants, which is the claim this
// file makes; the blend is what is left to run per pixel.
//
// The blend is a half-and-half of the two pixels, masked so the channels do not
// carry into each other. What it computes does not matter to the test — the
// interpreter is the oracle — but it has to be arithmetic the fold cannot do,
// and it is: both of its inputs are the pixel.
func blendedBlitProgram(base uint32) []uint16 {
	arm := []uint16{
		0x990e, // ldr  r1, [sp, #0x38]  — the record the palette hangs off
		0x7823, // ldrb r3, [r4]         — the source byte
		0x688a, // ldr  r2, [r1, #8]     — the palette
		0x005b, // lsls r3, r3, #1
		0x5a99, // ldrh r1, [r3, r2]     — the colour, the writer's second argument
		0x980a, // ldr  r0, [sp, #0x28]  — the pixel, its first
		0xf000, // bl   the writer
		0xf801,
		0xe7ec, // b    back to the write-back
	}
	// The flag's own branch is moved on by one halfword, because the halfword
	// after the loop is where a run of it ends and putting the arm there would
	// have the test's own boundary swallow the arm.
	program := append([]uint16(nil), clippedBlitBody...)
	program[9] = 0xd110
	program = append(program, 0x46c0) // where a run of the loop ends
	program = append(program, arm...)
	program = append(program, blendedWriterBody()...)
	return program
}

// blendedWriterBody is the writer and the three words it reads, which both the
// blit and the fill lay out after their own code.
func blendedWriterBody() []uint16 {
	writer := []uint16{
		0xb530, // push {r4, r5, lr}
		0x4604, // mov  r4, r0           — the pixel
		0x4b08, // ldr  r3, [pc, #0x20]  — where the record pointer lives
		0x681b, // ldr  r3, [r3]         — the record
		0x6f5a, // ldr  r2, [r3, #0x74]  — the mode, which is the loop's own flag
		0x0092, // lsls r2, r2, #2
		0x4b07, // ldr  r3, [pc, #0x1c]  — the jump table
		0x58d2, // ldr  r2, [r2, r3]
		0x4697, // mov  pc, r2
		0x8021, // strh r1, [r4]         — mode 0: the colour, unblended
		0xe007, // b    the return
		0x8820, // ldrh r0, [r4]         — mode 1: the pixel it is overwriting
		0x4d05, // ldr  r5, [pc, #0x14]  — the mask that keeps the channels apart
		0x4028, // ands r0, r5
		0x0840, // lsrs r0, r0, #1
		0x4029, // ands r1, r5
		0x0849, // lsrs r1, r1, #1
		0x1840, // adds r0, r0, r1
		0x8020, // strh r0, [r4]
		0xbd30, // pop  {r4, r5, pc}
	}
	for _, value := range []uint32{blendedRecordSlot, blendedTable, 0x0000f7de} {
		writer = append(writer, uint16(value), uint16(value>>16))
	}
	return writer
}

// Where the writer's own two words live, in the read-write page the fill-loop
// memory maps beside the code.
const (
	blendedRecordSlot = 0x70000900
	blendedTable      = 0x70000920
)

// blendedBlitMemory lays the loop's memory out and then the writer's: a record
// pointer, a jump table whose two entries are the writer's handlers, and the
// mode the loop's flag doubles as.
func blendedBlitMemory(t *testing.T, base, destination uint32, low, high, pixels int, mode uint32) *Memory {
	t.Helper()
	program := blendedBlitProgram(base)
	memory := clippedBlitMemory(t, program, base, destination, low, high, pixels, mode)
	// The writer builds a frame of its own, and the loop's stack pointer is at
	// the top of the page the other blit tests use.
	if err := memory.Map(uint32(spilledStack-memoryPageSize), memoryPageSize, PermissionRead|PermissionWrite); err != nil {
		t.Fatal(err)
	}
	writerAt := base + uint32(len(clippedBlitBody)+10)*2 // the loop, its end marker and the arm
	memory.beginQuantum()
	defer memory.endQuantum()
	words := map[uint32]uint32{
		blendedRecordSlot: spilledRecord,
		blendedTable:      writerAt + 18 + 1,
		blendedTable + 4:  writerAt + 22 + 1,
	}
	for address, value := range words {
		if err := memory.writeData32(address, value); err != nil {
			t.Fatal(err)
		}
	}
	return memory
}

// The bar every stand-in is held to: the guest cannot tell. Both arms run the
// same code over the same memory, one of them allowed to stand in, and every
// pixel, every register and the destination slot have to agree.
func TestStandingInForABlendedBlitMatchesRunningIt(t *testing.T) {
	const base, destination = 0x00100000, 0x00200000
	const pixels = 40
	branchPC := base + uint32(len(clippedBlitBody)-1)*2

	build := func(refuse bool, low, high int) (*Memory, *Context) {
		memory := blendedBlitMemory(t, base, destination, low, high, pixels, 1)
		memory.standInsRefused = refuse
		memory.beginQuantum()
		// Something to blend against, so a run that ignored the destination
		// would not come out the same by accident.
		for index := 0; index < pixels; index++ {
			if err := memory.writeData16(destination+uint32(index)*2, uint16(0x1234+index*77)); err != nil {
				t.Fatal(err)
			}
		}
		memory.endQuantum()
		return memory, clippedBlitContext(base, pixels)
	}

	for _, clip := range []struct {
		name      string
		low, high int
	}{
		{"the whole row", 0, pixels},
		{"clipped on both sides", 7, 31},
		{"clipped away entirely", 5, 5},
	} {
		t.Run(clip.name, func(t *testing.T) {
			interpreted, reference := build(true, clip.low, clip.high)
			stood, subject := build(false, clip.low, clip.high)
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
					t.Fatalf("pixel %d = %#x after standing in, %#x after interpreting", index, got, want)
				}
			}
			want, err := interpreted.readData32(spilledStack + spilledSlot)
			if err != nil {
				t.Fatal(err)
			}
			got, err := stood.readData32(spilledStack + spilledSlot)
			if err != nil {
				t.Fatal(err)
			}
			if want != got {
				t.Errorf("the destination slot is %#x after standing in, %#x after interpreting", got, want)
			}
		})
	}
}

// The step count has to be what the guest would have retired, or a run that
// should have exhausted its budget does not. A blended pixel is the whole body
// plus the arm plus everything inside the call, and the only honest way to know
// that number is to count what the interpreter charged for the same work.
func TestABlendedBlitChargesWhatItStoodInFor(t *testing.T) {
	const base, destination = 0x00100000, 0x00200000
	const pixels = 40
	branchPC := base + uint32(len(clippedBlitBody)-1)*2

	counted := make([]uint32, 0, 2)
	for _, refuse := range []bool{true, false} {
		memory := blendedBlitMemory(t, base, destination, 0, pixels, pixels, 1)
		memory.standInsRefused = refuse
		context := clippedBlitContext(base, pixels)
		result, err := (Engine{}).Run(context, memory, branchPC+2, 1_000_000)
		if err != nil {
			t.Fatal(err)
		}
		counted = append(counted, result.Steps)
	}
	if counted[0] != counted[1] {
		t.Errorf("standing in charged %d steps, interpreting retired %d", counted[1], counted[0])
	}
}

// A writer this cannot read has to be handed back, not guessed at. The loop
// below reaches its arm the same way and the arm calls a writer that stores
// somewhere other than the pixel it was handed, which is the one thing a
// stand-in for a per-pixel writer must never assume away.
func TestAWriterThatStoresElsewhereIsRefused(t *testing.T) {
	const base, destination = 0x00100000, 0x00200000
	const pixels = 40
	program := blendedBlitProgram(base)
	// `strh r0, [r4]` in the blending handler becomes a store through the
	// record instead, which is not the pixel and not the writer's own frame.
	writerAt := len(clippedBlitBody) + 10
	program[writerAt+18] = 0x8018 // strh r0, [r3]

	memory := clippedBlitMemory(t, program, base, destination, 0, pixels, pixels, 1)
	if err := memory.Map(uint32(spilledStack-memoryPageSize), memoryPageSize, PermissionRead|PermissionWrite); err != nil {
		t.Fatal(err)
	}
	memory.beginQuantum()
	defer memory.endQuantum()
	for address, value := range map[uint32]uint32{
		blendedRecordSlot: spilledRecord,
		blendedTable:      base + uint32(writerAt)*2 + 18 + 1,
		blendedTable + 4:  base + uint32(writerAt)*2 + 22 + 1,
	} {
		if err := memory.writeData32(address, value); err != nil {
			t.Fatal(err)
		}
	}
	branchPC := base + uint32(len(clippedBlitBody)-1)*2
	loop := memory.analyseClippedBlit(base, branchPC)
	if loop == nil {
		t.Fatal("the clipped blit was not recognised")
	}
	if draw := memory.analyseBlendedDraw(loop); draw != nil {
		t.Fatalf("a writer storing outside the pixel was compiled: %d nodes", len(draw.program.nodes))
	}
}

// The claim the file makes, as a number: a writer of twenty instructions behind
// a prologue and a dispatch compiles to the handful of operations that actually
// depend on the pixel. If this ever reads twenty, the fold has stopped working
// and the stand-in is an interpreter with extra steps.
func TestAWriterFoldsToItsArithmetic(t *testing.T) {
	const base, destination = 0x00100000, 0x00200000
	const pixels = 40
	memory := blendedBlitMemory(t, base, destination, 0, pixels, pixels, 1)
	memory.beginQuantum()
	defer memory.endQuantum()

	branchPC := base + uint32(len(clippedBlitBody)-1)*2
	loop := memory.analyseClippedBlit(base, branchPC)
	if loop == nil {
		t.Fatal("the clipped blit was not recognised")
	}
	draw := memory.analyseBlendedDraw(loop)
	if draw == nil {
		t.Fatal("the blending arm was not compiled")
	}
	// The blend is: read the pixel, mask, shift, mask, shift, add — plus the
	// two masks and the two shift amounts as constants, and the colour.
	if len(draw.program.nodes) > 12 {
		t.Errorf("the writer compiled to %d operations, want the arithmetic alone", len(draw.program.nodes))
	}
	// Everything the walk read out of memory it cannot move: the record
	// pointer, the record's mode, the jump table's entry and the mask.
	if len(draw.program.invariants) < 3 {
		t.Errorf("the writer folded %d invariant addresses, want the record, the mode and the table",
			len(draw.program.invariants))
	}
	// And the charge is the whole chain, not the arithmetic that is left.
	if draw.steps < 30 {
		t.Errorf("a blended pixel is charged %d steps, want the body, the arm and the call", draw.steps)
	}

	context := clippedBlitContext(base, pixels)
	stood, err := memory.runClippedBlit(context, loop)
	if err != nil {
		t.Fatal(err)
	}
	if stood == 0 {
		t.Fatal("the blending form was not stood in for")
	}
}

// The fill the flag guards, and the arm it sends the guest to. Laid out the
// same way: the loop, a halfword where a run of it ends, the arm, and then the
// writer the arm calls.
//
// r5 walks the destination, r6 holds the colour the plain form stores, r4
// counts down to minus one and r7 is the record the flag is read through.
var flaggedFillBody = []uint16{
	0x6f7b, // ldr  r3, [r7, #0x74]  — the flag
	0x2b00, // cmp  r3, #0
	0xd107, // bne  the arm
	0x802e, // strh r6, [r5]         — the pixel, unblended
	0x2301, // movs r3, #1
	0x3c01, // subs r4, #1
	0x425b, // rsbs r3, r3, #0       — minus one, which is what the ending tests
	0x3502, // adds r5, #2
	0x429c, // cmp  r4, r3
	0xd1f5, // bne  the head
}

func flaggedFillProgram() []uint16 {
	arm := []uint16{
		0x46c0, // nop — where a run of the loop ends
		0x1c28, // adds r0, r5, #0   — the pixel, the writer's first argument
		0x1c31, // adds r1, r6, #0   — the colour, its second
		0xf000, // bl   the writer
		0xf801,
		0xe7f3, // b    back to the tail
	}
	program := append([]uint16(nil), flaggedFillBody...)
	program = append(program, arm...)
	program = append(program, blendedWriterBody()...)
	return program
}

// flaggedFillMemory lays out the loop's destination page, the record its flag
// lives in, and the writer's own two words.
func flaggedFillMemory(t *testing.T, base, destination uint32, mode uint32) *Memory {
	t.Helper()
	memory := fillLoopMemory(t, flaggedFillProgram(), base, destination)
	if err := memory.Map(uint32(spilledStack-memoryPageSize), memoryPageSize, PermissionRead|PermissionWrite); err != nil {
		t.Fatal(err)
	}
	writerAt := base + uint32(len(flaggedFillBody)+6)*2
	memory.beginQuantum()
	defer memory.endQuantum()
	words := map[uint32]uint32{
		spilledRecord + clippedFlagAt: mode,
		blendedRecordSlot:             spilledRecord,
		blendedTable:                  writerAt + 18 + 1,
		blendedTable + 4:              writerAt + 22 + 1,
	}
	for address, value := range words {
		if err := memory.writeData32(address, value); err != nil {
			t.Fatal(err)
		}
	}
	return memory
}

func flaggedFillContext(base, destination uint32, pixels int) *Context {
	value := NewContext()
	context := &value
	context.Registers[RegisterSP] = spilledStack
	context.Registers[4] = uint32(pixels - 1)
	context.Registers[5] = destination
	context.Registers[6] = 0x39ce
	context.Registers[7] = spilledRecord
	context.setThumbPC(base)
	return context
}

// Both forms, against the interpreter, on the same memory.
func TestStandingInForAFlaggedFillMatchesRunningIt(t *testing.T) {
	const base, destination = 0x00100000, 0x00200000
	const pixels = 50
	branchPC := base + uint32(len(flaggedFillBody)-1)*2

	for _, mode := range []uint32{0, 1} {
		t.Run(map[uint32]string{0: "the flag clear", 1: "the flag set"}[mode], func(t *testing.T) {
			build := func(refuse bool) (*Memory, *Context) {
				memory := flaggedFillMemory(t, base, destination, mode)
				memory.standInsRefused = refuse
				memory.beginQuantum()
				for index := 0; index < pixels; index++ {
					if err := memory.writeData16(destination+uint32(index)*2, uint16(0x2468+index*111)); err != nil {
						t.Fatal(err)
					}
				}
				memory.endQuantum()
				return memory, flaggedFillContext(base, destination, pixels)
			}
			interpreted, reference := build(true)
			stood, subject := build(false)
			if _, err := (Engine{}).Run(reference, interpreted, branchPC+2, 1_000_000); err != nil {
				t.Fatalf("interpreting the fill: %v", err)
			}
			if _, err := (Engine{}).Run(subject, stood, branchPC+2, 1_000_000); err != nil {
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
					t.Fatalf("pixel %d = %#x after standing in, %#x after interpreting", index, got, want)
				}
			}
		})
	}
}

// The charge, both ways round. A fill that stands in for the blending form
// retires the writer's instructions too, and a run that should have hit its
// budget still has to.
func TestAFlaggedFillChargesWhatItStoodInFor(t *testing.T) {
	const base, destination = 0x00100000, 0x00200000
	const pixels = 50
	branchPC := base + uint32(len(flaggedFillBody)-1)*2

	for _, mode := range []uint32{0, 1} {
		counted := make([]uint32, 0, 2)
		for _, refuse := range []bool{true, false} {
			memory := flaggedFillMemory(t, base, destination, mode)
			memory.standInsRefused = refuse
			context := flaggedFillContext(base, destination, pixels)
			result, err := (Engine{}).Run(context, memory, branchPC+2, 1_000_000)
			if err != nil {
				t.Fatal(err)
			}
			counted = append(counted, result.Steps)
		}
		if counted[0] != counted[1] {
			t.Errorf("mode %d: standing in charged %d steps, interpreting retired %d",
				mode, counted[1], counted[0])
		}
	}
}

// The colour the plain form stores is one the loop must not compute, and the
// record its flag hangs off is one it must not move. A body that writes either
// is a body whose later iterations would not do what its first one did.
func TestAFlaggedFillThatMovesWhatItReadsIsRefused(t *testing.T) {
	const base, destination = 0x00100000, 0x00200000
	for _, patch := range []struct {
		name  string
		at    int
		value uint16
	}{
		{"the colour it stores", 7, 0x3602},      // adds r6, #2 instead of r5
		{"the record its flag is in", 7, 0x3702}, // adds r7, #2
	} {
		t.Run(patch.name, func(t *testing.T) {
			program := flaggedFillProgram()
			program[patch.at] = patch.value
			memory := fillLoopMemory(t, program, base, destination)
			memory.beginQuantum()
			defer memory.endQuantum()
			branchPC := base + uint32(len(flaggedFillBody)-1)*2
			if fill := memory.analyseFlaggedFill(base, branchPC); fill != nil {
				t.Fatalf("a fill moving %s was recognised: %+v", patch.name, fill)
			}
		})
	}
}

// The shape is read, and both of its forms are stood in for. That last part is
// what keeps this loop out of the refusal that made the shape before it cost
// more than it saved: a recogniser that accepts and then declines pays every
// analyser in the chain on every execution, for ever.
func TestAFlaggedFillIsRecognisedInBothForms(t *testing.T) {
	const base, destination = 0x00100000, 0x00200000
	const pixels = 50
	branchPC := base + uint32(len(flaggedFillBody)-1)*2

	for _, mode := range []uint32{0, 1} {
		memory := flaggedFillMemory(t, base, destination, mode)
		memory.beginQuantum()
		fill := memory.analyseFlaggedFill(base, branchPC)
		if fill == nil {
			t.Fatalf("mode %d: the flagged fill was not recognised", mode)
		}
		if fill.value != 6 || fill.destination != 5 || fill.counter != 4 {
			t.Errorf("value=r%d destination=r%d counter=r%d, want r6/r5/r4",
				fill.value, fill.destination, fill.counter)
		}
		if fill.guardBase != 7 || fill.guardOffset != 0x74 {
			t.Errorf("the flag is at r%d+%#x, want r7+0x74", fill.guardBase, fill.guardOffset)
		}
		context := flaggedFillContext(base, destination, pixels)
		stood, err := memory.runFlaggedFill(context, fill)
		if err != nil {
			t.Fatal(err)
		}
		if stood == 0 {
			t.Fatalf("mode %d: the fill was not stood in for", mode)
		}
		memory.endQuantum()
	}
}
