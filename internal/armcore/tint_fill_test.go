package armcore

import (
	"testing"
)

const (
	tintFillBase    = 0x00100000
	tintFillStack   = 0x70000000
	tintFillPainted = 0x00200000
)

// tintFillCode is the template with its closing branch, as a guest carries it.
func tintFillCode() []uint16 {
	code := append([]uint16(nil), tintFillBody[:]...)
	// bne back to the first instruction: the branch's own address plus four,
	// less forty-one halfwords.
	offset := int8(-len(tintFillBody) - 2)
	return append(code, 0xd100|uint16(uint8(offset)))
}

// tintFillBuild lays out one tinted row: 5-6-5 masks, a target colour, a
// fraction, and pixels that cover the sign bit both ways, since the body reads
// them sign-extended.
func tintFillBuild(t *testing.T, code []uint16, destination, pixels uint32, alpha uint32) func() (*Memory, *Context) {
	return func() (*Memory, *Context) {
		// The fixture's data page holds the frame; a row inside it needs no
		// page of its own.
		painted := destination &^ memoryPageMask
		if painted == tintFillStack {
			painted = tintFillPainted
		}
		memory := fillLoopMemory(t, code, tintFillBase, painted)
		memory.beginQuantum()
		for pixel := uint32(0); pixel < pixels+8; pixel++ {
			if err := memory.writeData16(destination+pixel*2, uint16(pixel*0x1357+0x8421)); err != nil {
				t.Fatal(err)
			}
		}
		for offset, value := range map[uint32]uint32{
			tintFillRedMask: 0xf800,
			tintFillRed:     0x6800,
			tintFillGreen:   0x0320,
			tintFillBlue:    0x0019,
			0x08:            0x5555, // the two spill slots, which the body
			0x0c:            0xaaaa, // writes before it reads
		} {
			if err := memory.writeData32(tintFillStack+offset, value); err != nil {
				t.Fatal(err)
			}
		}
		memory.endQuantum()
		value := NewContext()
		context := &value
		context.Registers[RegisterSP] = tintFillStack
		context.Registers[4] = destination
		context.Registers[5] = pixels
		context.Registers[7] = alpha
		context.Registers[8] = 0x07e0
		context.Registers[12] = 0x001f
		context.setThumbPC(tintFillBase)
		return memory, context
	}
}

func TestATintFillIsRecognisedOnlyAsItself(t *testing.T) {
	code := tintFillCode()
	branchPC := tintFillBase + uint32(len(code)-1)*2
	memory := fillLoopMemory(t, code, tintFillBase, tintFillPainted)
	memory.beginQuantum()
	recognised := memory.analyseTintFill(tintFillBase, branchPC)
	memory.endQuantum()
	if !recognised {
		t.Fatal("the template was not recognised")
	}
	// Any one instruction changed is another loop: a shift by seven, the
	// other spill slot, a store of the wrong register, a branch elsewhere.
	for name, change := range map[string]struct {
		at          int
		instruction uint16
	}{
		"a shift by seven":       {13, 0x11db},
		"the other spill slot":   {10, 0x9303},
		"a store of the green":   {37, 0x8020},
		"a branch one short":     {40, code[40] + 1},
		"an unsigned pixel load": {1, 0x5ae2},
	} {
		changed := append([]uint16(nil), code...)
		changed[change.at] = change.instruction
		memory := fillLoopMemory(t, changed, tintFillBase, tintFillPainted)
		memory.beginQuantum()
		if memory.analyseTintFill(tintFillBase, branchPC) {
			t.Errorf("%s was recognised as the template", name)
		}
		memory.endQuantum()
	}
}

// Standing in has to be indistinguishable from running the loop, for a
// fraction that darkens and one that does not move at all, and across a page.
func TestStandingInForATintFillMatchesRunningIt(t *testing.T) {
	code := tintFillCode()
	branchPC := tintFillBase + uint32(len(code)-1)*2
	for name, test := range map[string]struct {
		destination, pixels, alpha uint32
	}{
		"a half fade":   {tintFillPainted, 77, 0x80},
		"no fade":       {tintFillPainted + 6, 31, 0},
		"a whole fade":  {tintFillPainted, 40, 0x100},
		"across a page": {tintFillPainted + uint32(memoryPageSize) - 30, 64, 0x60},
	} {
		t.Run(name, func(t *testing.T) {
			build := tintFillBuild(t, code, test.destination, test.pixels, test.alpha)
			if name == "across a page" {
				// The row starts thirty bytes before the end of its page.
				build = tintFillBuild(t, code, test.destination, 0, test.alpha)
				inner := build
				build = func() (*Memory, *Context) {
					memory, context := inner()
					if err := memory.Map(tintFillPainted+uint32(memoryPageSize), memoryPageSize, PermissionRead|PermissionWrite); err != nil {
						t.Fatal(err)
					}
					memory.beginQuantum()
					for pixel := uint32(0); pixel < test.pixels+8; pixel++ {
						if err := memory.writeData16(test.destination+pixel*2, uint16(pixel*0x2468+0x8001)); err != nil {
							t.Fatal(err)
						}
					}
					memory.endQuantum()
					context.Registers[5] = test.pixels
					return memory, context
				}
			}
			standInMatches(t, branchPC+2, test.destination, (test.pixels+8)*2, build)
			standInMatches(t, branchPC+2, tintFillStack, 0x48, build)

			memory, context := build()
			memory.beginQuantum()
			defer memory.endQuantum()
			steps, err := memory.runTintFill(context)
			if err != nil {
				t.Fatal(err)
			}
			if steps != (test.pixels-1)*tintFillSteps {
				t.Errorf("stood in for %d steps, want %d pixels of %d", steps, test.pixels-1, tintFillSteps)
			}
		})
	}
}

// A row laid over the frame the masks and the target come from, and a run
// with a watch set, are left to the interpreter.
func TestATintFillItCannotProveIsLeftToTheInterpreter(t *testing.T) {
	code := tintFillCode()
	branchPC := tintFillBase + uint32(len(code)-1)*2

	over := tintFillBuild(t, code, tintFillStack+0x30, 12, 0x80)
	memory, context := over()
	memory.beginQuantum()
	steps, err := memory.runTintFill(context)
	memory.endQuantum()
	if err != nil || steps != 0 {
		t.Fatalf("stood in for %d steps (error %v) over its own frame", steps, err)
	}

	watch := func(memory *Memory) {
		// What Core.Watch does, on a memory with no core around it.
		memory.watches = map[uint32]struct{}{tintFillStack + 8: {}}
		memory.watchHits = map[watchKey]*WatchHit{}
		memory.refreshWatchSpanLocked()
	}
	watched := func() (*Memory, *Context) {
		memory, context := tintFillBuild(t, code, tintFillPainted, 20, 0x80)()
		watch(memory)
		return memory, context
	}
	memory, context = watched()
	memory.beginQuantum()
	steps, err = memory.runTintFill(context)
	memory.endQuantum()
	if err != nil || steps != 0 {
		t.Fatalf("stood in for %d steps (error %v) under a watch", steps, err)
	}
	standInMatches(t, branchPC+2, tintFillPainted, 56, watched)
}
