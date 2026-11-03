package armcore

import (
	"testing"
)

// The keyed blit as one title emits it: both bases out of frame slots, the
// palette in words, a key in r1 and the limit in the high register r8.
var keyedBlitBody = []uint16{
	0x9e52, // ldr  r6, [sp, #0x148]  the source row
	0x5d73, // ldrb r3, [r6, r5]
	0x9f53, // ldr  r7, [sp, #0x14c]  the palette
	0x009b, // lsls r3, r3, #2
	0x59db, // ldr  r3, [r3, r7]
	0x428b, // cmp  r3, r1
	0xd000, // beq  +0 — over the store
	0x8013, // strh r3, [r2]
	0x3501, // adds r5, #1
	0x3202, // adds r2, #2
	0x4545, // cmp  r5, r8
	0xdbf3, // blt  head
}

// The same blit as a second title emits it, reading a mirrored row: one
// register for both bases, the source read backwards, and the limit in r4.
var mirroredKeyedBlitBody = []uint16{
	0x9f46, // ldr  r7, [sp, #0x118]  the source row
	0x1b7b, // subs r3, r7, r5
	0x781b, // ldrb r3, [r3]
	0x9f47, // ldr  r7, [sp, #0x11c]  the palette, in the same register
	0x009b, // lsls r3, r3, #2
	0x59db, // ldr  r3, [r3, r7]
	0x428b, // cmp  r3, r1
	0xd000, // beq  +0 — over the store
	0x8013, // strh r3, [r2]
	0x3501, // adds r5, #1
	0x3202, // adds r2, #2
	0x42a5, // cmp  r5, r4
	0xdbf2, // blt  head
}

// keyedBlitCase lays out one run of a keyed blit: a palette whose every
// seventh entry is the key, a source row, and a destination already holding a
// pattern, so a pixel the key skips keeps what was there.
type keyedBlitCase struct {
	body        []uint16
	sourceSlot  uint32
	tableSlot   uint32
	limit       int // register holding the limit
	start       uint32
	pixels      uint32
	destination uint32
	// untouched leaves the destination page with no storage committed, which
	// sends the stand-in down the checked path rather than the direct one.
	untouched bool
	// spill maps the page after the destination's too, for a row that crosses
	// into it.
	spill bool
}

const (
	keyedBlitBase    = 0x00100000
	keyedBlitStack   = 0x70000000
	keyedBlitTable   = 0x70000400
	keyedBlitSource  = 0x70000c00
	keyedBlitKey     = 0x0000f81f
	keyedBlitPainted = 0x00200000
)

func (test keyedBlitCase) build(t *testing.T) func() (*Memory, *Context) {
	return func() (*Memory, *Context) {
		// The data page the fixture always maps holds the palette, the source
		// and the frame; a destination inside it needs no page of its own.
		painted := test.destination &^ memoryPageMask
		if painted == keyedBlitStack {
			painted = keyedBlitPainted
		}
		memory := fillLoopMemory(t, test.body, keyedBlitBase, painted)
		if test.spill {
			if err := memory.Map(painted+uint32(memoryPageSize), memoryPageSize, PermissionRead|PermissionWrite); err != nil {
				t.Fatal(err)
			}
		}
		memory.beginQuantum()
		mustWrite32 := func(address, value uint32) {
			if err := memory.writeData32(address, value); err != nil {
				t.Fatal(err)
			}
		}
		for index := uint32(0); index < 256; index++ {
			colour := 0x10000 | index*0x0123
			if index%7 == 0 {
				colour = keyedBlitKey
			}
			mustWrite32(keyedBlitTable+index*4, colour)
		}
		for offset := uint32(0); offset < 0x400; offset++ {
			if err := memory.write8(keyedBlitSource+offset, byte(offset*13+5)); err != nil {
				t.Fatal(err)
			}
		}
		if !test.untouched {
			for pixel := uint32(0); pixel < test.pixels+8; pixel++ {
				if err := memory.writeData16(test.destination+pixel*2, uint16(0xa500+pixel)); err != nil {
					t.Fatal(err)
				}
			}
		}
		// The frame last, so a row laid over it starts from a sane frame and
		// only its own stores change it.
		mustWrite32(keyedBlitStack+test.sourceSlot, keyedBlitSource+0x200)
		mustWrite32(keyedBlitStack+test.tableSlot, keyedBlitTable)
		memory.endQuantum()
		value := NewContext()
		context := &value
		context.Registers[RegisterSP] = keyedBlitStack
		context.Registers[1] = keyedBlitKey
		context.Registers[2] = test.destination
		context.Registers[5] = test.start
		context.Registers[test.limit] = test.start + test.pixels
		context.setThumbPC(keyedBlitBase)
		return memory, context
	}
}

// standInMatches runs a loop twice from the same start, once with the
// recognisers refused, and fails on any register, flag, byte of the given
// span or charged instruction that differs.
func standInMatches(t *testing.T, end, from, length uint32, build func() (*Memory, *Context)) {
	t.Helper()
	interpreted, reference := build()
	stood, subject := build()
	interpreted.standInsRefused = true
	control, err := (Engine{}).Run(reference, interpreted, end, 10_000_000)
	if err != nil {
		t.Fatalf("interpreting the loop: %v", err)
	}
	result, err := (Engine{}).Run(subject, stood, end, 10_000_000)
	if err != nil {
		t.Fatalf("standing in for the loop: %v", err)
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
	for offset := uint32(0); offset < length; offset++ {
		want, err := interpreted.read8(from + offset)
		if err != nil {
			t.Fatal(err)
		}
		got, err := stood.read8(from + offset)
		if err != nil {
			t.Fatal(err)
		}
		if want != got {
			t.Fatalf("byte at %#x is %#x after standing in, %#x after interpreting", from+offset, got, want)
		}
	}
}

func TestAKeyedBlitIsRecognised(t *testing.T) {
	for name, test := range map[string]struct {
		body                    []uint16
		sourceSlot, tableSlot   uint32
		limit                   uint32
		mirrored                bool
		index, destination, key uint32
	}{
		"forward, a high limit":                 {keyedBlitBody, 0x148, 0x14c, 8, false, 5, 2, 1},
		"mirrored, one register for both bases": {mirroredKeyedBlitBody, 0x118, 0x11c, 4, true, 5, 2, 1},
	} {
		t.Run(name, func(t *testing.T) {
			memory := fillLoopMemory(t, test.body, keyedBlitBase, keyedBlitPainted)
			memory.beginQuantum()
			defer memory.endQuantum()
			loop := memory.analyseKeyedBlit(keyedBlitBase, keyedBlitBase+uint32(len(test.body)-1)*2)
			if loop == nil {
				t.Fatal("the keyed blit was not recognised")
			}
			if loop.sourceSlot != test.sourceSlot || loop.tableSlot != test.tableSlot || loop.mirrored != test.mirrored {
				t.Errorf("slots %#x/%#x mirrored %v, want %#x/%#x %v",
					loop.sourceSlot, loop.tableSlot, loop.mirrored, test.sourceSlot, test.tableSlot, test.mirrored)
			}
			if loop.index != test.index || loop.limit != test.limit || loop.destination != test.destination || loop.key != test.key {
				t.Errorf("index r%d limit r%d destination r%d key r%d", loop.index, loop.limit, loop.destination, loop.key)
			}
			if loop.steps != uint32(len(test.body)) {
				t.Errorf("steps=%d, want the %d instructions of one iteration", loop.steps, len(test.body))
			}
		})
	}
}

// Each of these is the same loop with one thing changed that changes what it
// does, and the analysis has to refuse every one rather than blit with it.
func TestAKeyedBlitThatIsNotThisLoopIsRefused(t *testing.T) {
	change := func(body []uint16, at int, instruction uint16) []uint16 {
		changed := append([]uint16(nil), body...)
		changed[at] = instruction
		return changed
	}
	for name, body := range map[string][]uint16{
		"a guard that skips the counter too": change(keyedBlitBody, 6, 0xd001),
		"a store with no guard":              change(keyedBlitBody, 6, 0x46c0),
		"a palette of halfwords":             change(keyedBlitBody, 3, 0x005b),
		"a limit in the stack pointer":       change(keyedBlitBody, 10, 0x456d),
		"the destination as the key":         change(keyedBlitBody, 5, 0x4293),
		"the value as the palette base":      change(keyedBlitBody, 2, 0x9b53),
		"a counter that steps by two":        change(keyedBlitBody, 8, 0x3502),
		"a signed limit read unsigned":       change(keyedBlitBody, 11, 0xd3f3),
		"a branch to the wrong head":         change(keyedBlitBody, 11, 0xdbf4),
	} {
		t.Run(name, func(t *testing.T) {
			memory := fillLoopMemory(t, body, keyedBlitBase, keyedBlitPainted)
			memory.beginQuantum()
			defer memory.endQuantum()
			if loop := memory.analyseKeyedBlit(keyedBlitBase, keyedBlitBase+uint32(len(body)-1)*2); loop != nil {
				t.Fatalf("recognised as a keyed blit: %+v", *loop)
			}
		})
	}
}

// Standing in has to be indistinguishable from running the loop: every pixel,
// the key's gaps, the counter, the destination, the scratch registers and
// flags the interpreted last iteration leaves, and the instructions charged.
func TestStandingInForAKeyedBlitMatchesRunningIt(t *testing.T) {
	for name, test := range map[string]keyedBlitCase{
		"forward":            {body: keyedBlitBody, sourceSlot: 0x148, tableSlot: 0x14c, limit: 8, start: 3, pixels: 61, destination: keyedBlitPainted},
		"mirrored":           {body: mirroredKeyedBlitBody, sourceSlot: 0x118, tableSlot: 0x11c, limit: 4, start: 2, pixels: 57, destination: keyedBlitPainted},
		"across a page":      {body: keyedBlitBody, sourceSlot: 0x148, tableSlot: 0x14c, limit: 8, start: 0, pixels: 90, destination: keyedBlitPainted + uint32(memoryPageSize) - 40, spill: true},
		"an uncommitted row": {body: mirroredKeyedBlitBody, sourceSlot: 0x118, tableSlot: 0x11c, limit: 4, start: 1, pixels: 33, destination: keyedBlitPainted, untouched: true},
	} {
		t.Run(name, func(t *testing.T) {
			branchPC := keyedBlitBase + uint32(len(test.body)-1)*2
			standInMatches(t, branchPC+2, test.destination, (test.pixels+8)*2, test.build(t))
		})
	}
}

// What the loop reads every pixel has to be what it read first. A row drawn
// over its own palette or over a frame slot is a different loop from the
// second pixel on, so the stand-in hands it to the interpreter, and the result
// is the interpreter's.
func TestAKeyedBlitOverWhatItReadsIsLeftToTheInterpreter(t *testing.T) {
	for name, destination := range map[string]uint32{
		"over the palette":    keyedBlitTable + 0x20,
		"over a frame slot":   keyedBlitStack + 0x140,
		"over its own source": keyedBlitSource + 0x210,
	} {
		t.Run(name, func(t *testing.T) {
			test := keyedBlitCase{body: keyedBlitBody, sourceSlot: 0x148, tableSlot: 0x14c, limit: 8, start: 0, pixels: 24, destination: destination}
			branchPC := keyedBlitBase + uint32(len(test.body)-1)*2
			// Asked directly, from the loop's head, the stand-in declines.
			memory, context := test.build(t)()
			memory.beginQuantum()
			loop := memory.analyseKeyedBlit(keyedBlitBase, branchPC)
			if loop == nil {
				t.Fatal("the keyed blit was not recognised")
			}
			steps, err := memory.runKeyedBlit(context, loop)
			memory.endQuantum()
			if err != nil || steps != 0 {
				t.Fatalf("stood in for %d steps (error %v) over what it reads", steps, err)
			}
			// A row over its own frame slot overwrites the base it reloads, and
			// where that leads is the guest's business: only the decline is
			// asserted for it.
			if name != "over a frame slot" {
				standInMatches(t, branchPC+2, destination&^0xff, 0x200, test.build(t))
			}
		})
	}
}

// The comparisons above would pass just as well if the stand-in never ran, so
// this asks it directly, from a loop head, and wants it to take every pixel
// but the last, charged as the interpreter would charge them.
func TestAKeyedBlitIsStoodInFor(t *testing.T) {
	for name, test := range map[string]keyedBlitCase{
		"forward":            {body: keyedBlitBody, sourceSlot: 0x148, tableSlot: 0x14c, limit: 8, start: 3, pixels: 61, destination: keyedBlitPainted},
		"mirrored":           {body: mirroredKeyedBlitBody, sourceSlot: 0x118, tableSlot: 0x11c, limit: 4, start: 2, pixels: 57, destination: keyedBlitPainted},
		"an uncommitted row": {body: mirroredKeyedBlitBody, sourceSlot: 0x118, tableSlot: 0x11c, limit: 4, start: 1, pixels: 33, destination: keyedBlitPainted, untouched: true},
	} {
		t.Run(name, func(t *testing.T) {
			memory, context := test.build(t)()
			memory.beginQuantum()
			defer memory.endQuantum()
			loop := memory.analyseKeyedBlit(keyedBlitBase, keyedBlitBase+uint32(len(test.body)-1)*2)
			if loop == nil {
				t.Fatal("the keyed blit was not recognised")
			}
			steps, err := memory.runKeyedBlit(context, loop)
			if err != nil {
				t.Fatal(err)
			}
			pixels := test.pixels - 1
			if steps < pixels*(loop.steps-1) || steps > pixels*loop.steps {
				t.Errorf("charged %d steps for %d pixels of %d instructions", steps, pixels, loop.steps)
			}
			if got := context.Registers[loop.index]; got != test.start+pixels {
				t.Errorf("index r%d = %d, want %d: the last pixel is the interpreter's", loop.index, got, test.start+pixels)
			}
			if got := context.Registers[loop.destination]; got != test.destination+pixels*2 {
				t.Errorf("destination = %#x, want %#x", got, test.destination+pixels*2)
			}
		})
	}
}
