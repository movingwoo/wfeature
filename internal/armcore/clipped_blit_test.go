package armcore

import "testing"

// The clipped blit as one title emits it: two bounds out of frame slots, a
// flag out of a record, and the same spilled blit behind them. It closes 95
// million times in a minute of that title's play.
//
// r8 walks across the row, r4 the source, r6 counts up to r5, r1 and r2 carry
// the bounds and then the record and the destination, r3 the flag and then the
// colour and then the constant the index advances by.
var clippedBlitBody = []uint16{
	0x9917, // ldr  r1, [sp, #0x5c]  — the low bound
	0x4588, // cmp  r8, r1
	0xdb10, // blt  join
	0x9a02, // ldr  r2, [sp, #8]     — the high bound
	0x4542, // cmp  r2, r8
	0xdd0d, // ble  join
	0x9904, // ldr  r1, [sp, #0x10]  — the record the flag is in
	0x6f4b, // ldr  r3, [r1, #0x74]
	0x2b00, // cmp  r3, #0
	0xd10f, // bne  out — to the blending form of the same blit
	0x990e, // ldr  r1, [sp, #0x38]  — the record the palette hangs off
	0x7823, // ldrb r3, [r4]
	0x688a, // ldr  r2, [r1, #8]
	0x005b, // lsls r3, r3, #1
	0x5a9b, // ldrh r3, [r3, r2]
	0x9a0a, // ldr  r2, [sp, #0x28]
	0x8013, // strh r3, [r2]
	0x9a0a, // ldr  r2, [sp, #0x28]
	0x3202, // adds r2, #2
	0x920a, // str  r2, [sp, #0x28]
	0x2301, // movs r3, #1           — join
	0x3601, // adds r6, #1
	0x3401, // adds r4, #1
	0x4498, // add  r8, r3
	0x42ae, // cmp  r6, r5
	0xdbe5, // blt  head
}

const (
	clippedLowSlot  = 0x5c
	clippedHighSlot = 0x08
	clippedFlagSlot = 0x10
	clippedFlagAt   = 0x74
)

// clippedBlitMemory lays out what the body reaches through memory: both bounds
// and the flag in their frame slots, the palette behind its record, and a
// source full of indexes.
func clippedBlitMemory(t *testing.T, body []uint16, base, destination uint32, low, high, pixels int, flag uint32) *Memory {
	t.Helper()
	memory := fillLoopMemory(t, body, base, destination)
	memory.beginQuantum()
	defer memory.endQuantum()
	words := map[uint32]uint32{
		spilledStack + spilledSlot:       destination,
		spilledStack + spilledRecordSlot: spilledRecord,
		spilledStack + clippedFlagSlot:   spilledRecord,
		spilledStack + clippedLowSlot:    uint32(low),
		spilledStack + clippedHighSlot:   uint32(high),
		spilledRecord + 8:                spilledPalette,
		spilledRecord + clippedFlagAt:    flag,
	}
	for address, value := range words {
		if err := memory.writeData32(address, value); err != nil {
			t.Fatal(err)
		}
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

func clippedBlitContext(base uint32, pixels int) *Context {
	value := NewContext()
	context := &value
	context.Registers[RegisterSP] = spilledStack
	context.Registers[4] = spilledFrom
	context.Registers[5] = uint32(pixels)
	context.Registers[6] = 0
	context.Registers[8] = 0
	context.setThumbPC(base)
	return context
}

func TestAClippedPaletteBlitIsRecognised(t *testing.T) {
	const base, destination = 0x00100000, 0x00200000
	memory := clippedBlitMemory(t, clippedBlitBody, base, destination, 10, 30, 50, 0)
	memory.beginQuantum()
	defer memory.endQuantum()

	branchPC := base + uint32(len(clippedBlitBody)-1)*2
	loop := memory.analyseClippedBlit(base, branchPC)
	if loop == nil {
		t.Fatal("the clipped palette blit was not recognised")
	}
	if loop.index != 8 || loop.source != 4 || loop.count != 6 || loop.limit != 5 || loop.one != 3 {
		t.Errorf("index=r%d source=r%d count=r%d limit=r%d one=r%d, want r8/r4/r6/r5/r3",
			loop.index, loop.source, loop.count, loop.limit, loop.one)
	}
	if loop.lowSlot != clippedLowSlot || loop.highSlot != clippedHighSlot {
		t.Errorf("bounds at sp+%#x and sp+%#x, want sp+0x5c and sp+0x8", loop.lowSlot, loop.highSlot)
	}
	if loop.flagSlot != clippedFlagSlot || loop.flagOffset != clippedFlagAt {
		t.Errorf("flag at [sp+%#x]+%#x, want [sp+0x10]+0x74", loop.flagSlot, loop.flagOffset)
	}
	if loop.slot != spilledSlot || loop.record != spilledRecordSlot || loop.colours != 8 {
		t.Errorf("destination at sp+%#x, record at sp+%#x, palette at +%#x",
			loop.slot, loop.record, loop.colours)
	}
	// A pixel off the left runs three instructions and the tail; one off the
	// right runs six and the tail; a drawn one runs the whole body.
	if loop.lowSteps != 9 || loop.highSteps != 12 || loop.drawSteps != 26 {
		t.Errorf("paths cost %d/%d/%d instructions, want 9/12/26",
			loop.lowSteps, loop.highSteps, loop.drawSteps)
	}
}

// The run this shape exists for has all three phases in it, and standing in for
// it has to be indistinguishable from running it — in every register, in the
// frame slot the destination lives in, and in the pixels either side of the
// stretch that was drawn.
func TestStandingInForAClippedBlitMatchesRunningIt(t *testing.T) {
	const base, destination = 0x00100000, 0x00200000
	const low, high, pixels = 10, 30, 50
	branchPC := base + uint32(len(clippedBlitBody)-1)*2

	build := func(refuse bool) (*Memory, *Context) {
		memory := clippedBlitMemory(t, clippedBlitBody, base, destination, low, high, pixels, 0)
		memory.standInsRefused = refuse
		return memory, clippedBlitContext(base, pixels)
	}
	interpreted, reference := build(true)
	stood, subject := build(false)
	control, err := (Engine{}).Run(reference, interpreted, branchPC+2, 1_000_000)
	if err != nil {
		t.Fatalf("interpreting the blit: %v", err)
	}
	result, err := (Engine{}).Run(subject, stood, branchPC+2, 1_000_000)
	if err != nil {
		t.Fatalf("standing in for the blit: %v", err)
	}
	if control.Steps != result.Steps {
		t.Errorf("charged %d steps, interpreting the same loop retired %d", result.Steps, control.Steps)
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
	if want != destination+(high-low)*2 {
		t.Errorf("the destination moved by %d pixels, want the %d inside the clip",
			(want-destination)/2, high-low)
	}
	// One pixel past the stretch at either end: a stand-in that gets a bound
	// wrong by one writes exactly there.
	for index := 0; index <= high-low; index++ {
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

// A run entirely outside the clip draws nothing and still has to leave the
// index, the source and the count where the guest left them.
func TestAClippedBlitEntirelyOffTheRowDrawsNothing(t *testing.T) {
	const base, destination = 0x00100000, 0x00200000
	const low, high, pixels = 200, 300, 40
	branchPC := base + uint32(len(clippedBlitBody)-1)*2

	build := func(refuse bool) (*Memory, *Context) {
		memory := clippedBlitMemory(t, clippedBlitBody, base, destination, low, high, pixels, 0)
		memory.standInsRefused = refuse
		return memory, clippedBlitContext(base, pixels)
	}
	interpreted, reference := build(true)
	stood, subject := build(false)
	control, err := (Engine{}).Run(reference, interpreted, branchPC+2, 1_000_000)
	if err != nil {
		t.Fatal(err)
	}
	result, err := (Engine{}).Run(subject, stood, branchPC+2, 1_000_000)
	if err != nil {
		t.Fatal(err)
	}
	if control.Steps != result.Steps {
		t.Errorf("charged %d steps, interpreting the same loop retired %d", result.Steps, control.Steps)
	}
	for register := 0; register < 16; register++ {
		if reference.Registers[register] != subject.Registers[register] {
			t.Errorf("r%d = %#x after standing in, %#x after interpreting",
				register, subject.Registers[register], reference.Registers[register])
		}
	}
	stood.beginQuantum()
	defer stood.endQuantum()
	first, err := stood.readData16(destination)
	if err != nil {
		t.Fatal(err)
	}
	if first != 0 {
		t.Errorf("a run entirely off the row wrote %#x at the destination", first)
	}
}

// The flag is what takes the guest out of this loop and into the blending form
// of the same blit. A stand-in that ran the blit anyway would draw a whole
// sprite opaque, so a set flag is refused.
func TestAClippedBlitWithItsBlendFlagSetIsRefused(t *testing.T) {
	const base, destination = 0x00100000, 0x00200000
	const low, high, pixels = 0, 100, 40
	branchPC := base + uint32(len(clippedBlitBody)-1)*2

	build := func(refuse bool) (*Memory, *Context) {
		memory := clippedBlitMemory(t, clippedBlitBody, base, destination, low, high, pixels, 1)
		memory.standInsRefused = refuse
		return memory, clippedBlitContext(base, pixels)
	}
	interpreted, reference := build(true)
	stood, subject := build(false)
	if _, err := (Engine{}).Run(reference, interpreted, branchPC+2, 1_000_000); err != nil {
		t.Fatal(err)
	}
	if _, err := (Engine{}).Run(subject, stood, branchPC+2, 1_000_000); err != nil {
		t.Fatal(err)
	}
	// The guest leaves on its first pixel, so the count is where it stopped
	// rather than at the limit.
	if reference.Registers[6] >= uint32(pixels) {
		t.Fatalf("the flag did not take the guest out of the loop, so this proves nothing")
	}
	for register := 0; register < 16; register++ {
		if reference.Registers[register] != subject.Registers[register] {
			t.Errorf("r%d = %#x after standing in, %#x after interpreting",
				register, subject.Registers[register], reference.Registers[register])
		}
	}
}

// Both clip tests have to leave for the same place, and that place has to be
// inside the body. A branch anywhere else is a loop this cannot read.
func TestAClippedBlitWhoseTestsPartCompanyIsRefused(t *testing.T) {
	const base, destination = 0x00100000, 0x00200000
	body := append([]uint16(nil), clippedBlitBody...)
	body[5] = 0xdd0e // ble one instruction past the join the first test uses
	memory := clippedBlitMemory(t, body, base, destination, 10, 30, 8, 0)
	memory.beginQuantum()
	defer memory.endQuantum()

	if loop := memory.analyseClippedBlit(base, base+uint32(len(body)-1)*2); loop != nil {
		t.Fatalf("two clip tests leaving for different places were recognised: %+v", loop)
	}
}

// The flag's branch goes out of the loop. One that lands back inside it is a
// second loop rather than a way out of this one.
func TestAClippedBlitWhoseFlagBranchStaysInsideIsRefused(t *testing.T) {
	const base, destination = 0x00100000, 0x00200000
	body := append([]uint16(nil), clippedBlitBody...)
	body[9] = 0xd101 // bne +2, still inside the draw
	memory := clippedBlitMemory(t, body, base, destination, 10, 30, 8, 0)
	memory.beginQuantum()
	defer memory.endQuantum()

	if loop := memory.analyseClippedBlit(base, base+uint32(len(body)-1)*2); loop != nil {
		t.Fatalf("a flag branch landing inside the body was recognised: %+v", loop)
	}
}

// A remembered decline must not write the loop off.
//
// The flag is the one decline worth not deriving twice — it holds for every
// pixel of a blended run, and re-deriving it costs the whole recogniser chain
// per pixel. What makes that safe is that the word is read back rather than
// trusted: the same loop with the flag clear has to be stood in for again.
// Caching the decline itself is the mistake this pins against.
func TestAFlagDeclineIsForgottenWhenTheFlagClears(t *testing.T) {
	const base, destination = 0x00100000, 0x00200000
	const pixels = 40
	branchPC := base + uint32(len(clippedBlitBody)-1)*2

	memory := clippedBlitMemory(t, clippedBlitBody, base, destination, 0, pixels, pixels, 1)
	memory.beginQuantum()
	loop := memory.analyseClippedBlit(base, branchPC)
	if loop == nil {
		t.Fatal("the clipped palette blit was not recognised")
	}
	context := clippedBlitContext(base, pixels)
	context.Registers[8] = 1
	context.Registers[6] = 1
	stood, err := memory.runClippedBlit(context, loop)
	if err != nil {
		t.Fatal(err)
	}
	if stood != 0 {
		t.Fatalf("the blit stood in for %d steps with its flag set", stood)
	}
	if !memory.haveDeclined || memory.declinedBranch != branchPC ||
		memory.declinedFlag != spilledRecord+clippedFlagAt {
		t.Fatalf("the decline was remembered as branch %#x flag %#x, want %#x and %#x",
			memory.declinedBranch, memory.declinedFlag, branchPC, uint32(spilledRecord+clippedFlagAt))
	}
	// The guest goes back to the plain form of the same blit, which is what a
	// remembered decline must not stop.
	if err := memory.writeData32(spilledRecord+clippedFlagAt, 0); err != nil {
		t.Fatal(err)
	}
	memory.endQuantum()
	memory.beginQuantum()
	defer memory.endQuantum()
	stood, err = memory.runStoreLoop(context, base, branchPC)
	if err != nil {
		t.Fatal(err)
	}
	if stood == 0 {
		t.Fatal("the blit was not stood in for once its flag had cleared")
	}
}
