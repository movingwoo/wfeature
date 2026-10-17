package armcore

import "testing"

// compareStandIn runs the same Thumb loop twice from the same start, once with
// the recognisers refused and once allowed, and fails on the first register,
// flag or byte of [from, from+length) that differs. build lays out a fresh
// memory and the context the loop starts in.
func compareStandIn(t *testing.T, end uint32, from, length uint32, build func() (*Memory, *Context)) {
	t.Helper()
	interpreted, reference := build()
	stood, subject := build()
	interpreted.standInsRefused = true
	if _, err := (Engine{}).Run(reference, interpreted, end, 10_000_000); err != nil {
		t.Fatalf("interpreting the loop: %v", err)
	}
	if _, err := (Engine{}).Run(subject, stood, end, 10_000_000); err != nil {
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

// A fill whose pointer advances before the store writes [p+2, p+2n], and one
// whose store goes through a register other than the one advanced writes the
// same halfword over and over. Neither is the counted fill the stand-in
// implements, so both have to be left to the interpreter.
func TestAFillThatAdvancesFirstOrStoresElsewhereIsRefused(t *testing.T) {
	const base, destination = 0x00100000, 0x00200000
	const count = uint32(40)
	for _, test := range []struct {
		name string
		body []uint16
	}{
		{"pre-increment", []uint16{
			0x3002, // adds r0, #2
			0x8001, // strh r1, [r0]
			0x3a01, // subs r2, #1
			0xd2fb, // bhs  head
		}},
		{"store through another register", []uint16{
			0x8019, // strh r1, [r3]
			0x3002, // adds r0, #2
			0x3a01, // subs r2, #1
			0xd2fb, // bhs  head
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			branchPC := base + uint32(len(test.body)-1)*2
			memory := fillLoopMemory(t, test.body, base, destination)
			memory.beginQuantum()
			loop := memory.analyseStoreLoop(base, branchPC)
			memory.endQuantum()
			if loop != nil {
				t.Errorf("the body was recognised as a counted fill: %+v", *loop)
			}
			compareStandIn(t, branchPC+2, destination, 0x200, func() (*Memory, *Context) {
				memory := fillLoopMemory(t, test.body, base, destination)
				value := NewContext()
				context := &value
				context.Registers[0] = destination
				context.Registers[1] = 0xbeef
				context.Registers[2] = count
				context.Registers[3] = destination + 0x100
				context.setThumbPC(base)
				return memory, context
			})
		})
	}
}

// The palette blit with its source pointer advanced before the byte is read:
// every stood-in pixel would come from one byte too early.
func TestATableBlitThatAdvancesItsSourceFirstIsRefused(t *testing.T) {
	const base, destination = 0x00100000, 0x00200000
	const palette, source = 0x70000000, 0x70000800
	const pixels = 40
	body := []uint16{
		0x3001, // adds r0, #1     — the source, before it is read
		0x7803, // ldrb r3, [r0]
		0x005b, // lsls r3, r3, #1
		0x5ad3, // ldrh r3, [r2, r3]
		0x800b, // strh r3, [r1]
		0x3102, // adds r1, #2
		0x1e63, // subs r3, r4, #1
		0x041b, // lsls r3, r3, #16
		0x0c1c, // lsrs r4, r3, #16
		0x4b04, // ldr  r3, [pc, #16]
		0x429c, // cmp  r4, r3
		0xd1f3, // bne  head
	}
	branchPC := base + uint32(len(body)-1)*2
	build := func() (*Memory, *Context) {
		memory := fillLoopMemory(t, body, base, destination)
		// The limit the counter runs down to, where `ldr r3, [pc, #16]`
		// reaches it.
		if err := memory.Load((base+18+4)&^3+16, []byte{0, 0, 0, 0}); err != nil {
			t.Fatal(err)
		}
		memory.beginQuantum()
		for index := 0; index < 256; index++ {
			if err := memory.writeData16(palette+uint32(index)*2, uint16(0x1000+index*7)); err != nil {
				t.Fatal(err)
			}
		}
		for pixel := 0; pixel <= pixels; pixel++ {
			if err := memory.write8(source+uint32(pixel), byte(pixel*5+3)); err != nil {
				t.Fatal(err)
			}
		}
		memory.endQuantum()
		value := NewContext()
		context := &value
		context.Registers[0] = source
		context.Registers[1] = destination
		context.Registers[2] = palette
		context.Registers[4] = pixels
		context.setThumbPC(base)
		return memory, context
	}
	memory, _ := build()
	memory.beginQuantum()
	loop := memory.analyseTableBlit(base, branchPC)
	memory.endQuantum()
	if loop != nil {
		t.Errorf("a pre-incremented source was recognised as a table blit: %+v", *loop)
	}
	compareStandIn(t, branchPC+2, destination, (pixels+2)*2, build)
}

// The guarded byte blend with its source advanced before the byte is read.
func TestAByteBlendThatAdvancesItsSourceFirstIsRefused(t *testing.T) {
	const base, destination = 0x00100000, 0x00200000
	const stack, source = 0x70000000, 0x70000400
	const bytes, constant = 61, 0x20
	body := []uint16{
		0x3401, // adds r4, #1     — the source, before it is read
		0x9a03, // ldr  r2, [sp, #12]
		0x7827, // ldrb r7, [r4]
		0x1abb, // subs r3, r7, r2
		0x780a, // ldrb r2, [r1]
		0x4293, // cmp  r3, r2
		0xdd00, // ble  +0 — over the store
		0x700b, // strb r3, [r1]
		0x3801, // subs r0, #1
		0x3101, // adds r1, #1
		0x2800, // cmp  r0, #0
		0xd1f3, // bne  head
	}
	branchPC := base + uint32(len(body)-1)*2
	build := func() (*Memory, *Context) {
		memory := fillLoopMemory(t, body, base, destination)
		memory.beginQuantum()
		if err := memory.writeData32(stack+12, constant); err != nil {
			t.Fatal(err)
		}
		for index := 0; index <= bytes; index++ {
			if err := memory.write8(source+uint32(index), byte(index*7)); err != nil {
				t.Fatal(err)
			}
			if err := memory.write8(destination+uint32(index), byte(200-index*3)); err != nil {
				t.Fatal(err)
			}
		}
		memory.endQuantum()
		value := NewContext()
		context := &value
		context.Registers[RegisterSP] = stack
		context.Registers[0] = bytes
		context.Registers[1] = destination
		context.Registers[4] = source
		context.setThumbPC(base)
		return memory, context
	}
	memory, _ := build()
	memory.beginQuantum()
	loop := memory.analyseByteBlend(base, branchPC)
	memory.endQuantum()
	if loop != nil {
		t.Errorf("a pre-incremented source was recognised as a byte blend: %+v", *loop)
	}
	compareStandIn(t, branchPC+2, destination, bytes+1, build)
}

// modulateCompare runs a modulate body both ways over the fixture the
// recogniser's own tests use.
func modulateCompare(t *testing.T, body []uint16) {
	t.Helper()
	const base, data = 0x00100000, 0x00200000
	const stack = 0x70000000
	const first, second, factors = data, data + 0x800, 0x70000400
	const steps = 27
	branchPC := base + uint32(len(body)-1)*2
	build := func() (*Memory, *Context) {
		memory := modulateMemory(t, body, base, data)
		memory.beginQuantum()
		if err := memory.writeData32(stack+8, factors); err != nil {
			t.Fatal(err)
		}
		for index := uint32(0); index < steps; index++ {
			if err := memory.writeData32(first+index*4, 0x5a3c<<16|index*37); err != nil {
				t.Fatal(err)
			}
			if err := memory.writeData32(second+index*4, 0xffff<<16|index*911); err != nil {
				t.Fatal(err)
			}
			if err := memory.write8(factors+index, byte(index*5)); err != nil {
				t.Fatal(err)
			}
		}
		memory.endQuantum()
		value := NewContext()
		context := &value
		context.Registers[RegisterSP] = stack
		context.Registers[6] = first
		context.Registers[7] = second
		context.Registers[12] = steps
		context.setThumbPC(base)
		return memory, context
	}
	compareStandIn(t, branchPC+2, data, 0x800+(steps+1)*4, build)
}

// The counter's minus one built in the register that also holds the first
// mask: at the loop head that register is -1, not the mask the guest reloads
// from its literal every step.
func TestAModulateWhoseMaskRegisterIsReusedMatchesRunningIt(t *testing.T) {
	const base, data = 0x00100000, 0x00200000
	body := wordModulateBody()
	body[33] = 0x2501 // movs r5, #1
	body[34] = 0x426d // rsbs r5, r5, #0
	body[37] = 0x44ac // add  r12, r5
	memory := modulateMemory(t, body, base, data)
	memory.beginQuantum()
	loop := memory.analyseWordModulate(base, base+uint32(len(body)-1)*2)
	memory.endQuantum()
	if loop == nil {
		t.Fatal("the modulate was not recognised; the comparison below would prove nothing")
	}
	modulateCompare(t, body)
}

// The first mask loaded into the register the step uses as scratch: the
// step's own copy overwrites it before the mask is used, so the arithmetic is
// not the modulate's and has to be left to the interpreter.
func TestAModulateWhoseMaskIsAlsoItsScratchIsRefused(t *testing.T) {
	const base, data = 0x00100000, 0x00200000
	body := wordModulateBody()
	body[1] = 0x4b1b  // ldr  r3, [pc, #108] — the first mask, in the scratch register
	body[6] = 0x401b  // ands r3, r3
	body[15] = 0x401a // ands r2, r3
	body[22] = 0x401b // ands r3, r3
	body[31] = 0x401a // ands r2, r3
	memory := modulateMemory(t, body, base, data)
	memory.beginQuantum()
	loop := memory.analyseWordModulate(base, base+uint32(len(body)-1)*2)
	memory.endQuantum()
	if loop != nil {
		t.Errorf("a mask sharing the scratch register was recognised: %+v", *loop)
	}
	modulateCompare(t, body)
}

// A blended clipped blit whose destination covers its own flag word, behind a
// writer that reads its mode from somewhere else: the guest's own store clears
// the flag partway through the row and the rest of it is drawn plain, so the
// stand-in, which treats the flag as fixed for the run, has to decline.
func TestABlendedBlitOverItsOwnFlagIsLeftToTheInterpreter(t *testing.T) {
	const base = 0x00100000
	const pixels = 5
	// The pixel the guest stores at the flag's low half has to come out zero
	// for the flag to clear, which a zero source byte (palette entry zero)
	// blended over the flag's own low half does.
	const destination = spilledRecord + clippedFlagAt - 4
	const modeAt = clippedFlagAt + 4
	program := blendedBlitProgram(base)
	writerAt := len(clippedBlitBody) + 10
	program[writerAt+4] = 0x6f9a // ldr r2, [r3, #0x78] — the mode, no longer the flag
	branchPC := base + uint32(len(clippedBlitBody)-1)*2

	build := func() (*Memory, *Context) {
		memory := clippedBlitMemory(t, program, base, destination, 0, pixels, pixels, 1)
		if err := memory.Map(uint32(spilledStack-memoryPageSize), memoryPageSize, PermissionRead|PermissionWrite); err != nil {
			t.Fatal(err)
		}
		memory.beginQuantum()
		for address, value := range map[uint32]uint32{
			blendedRecordSlot:      spilledRecord,
			blendedTable:           base + uint32(writerAt)*2 + 18 + 1,
			blendedTable + 4:       base + uint32(writerAt)*2 + 22 + 1,
			spilledRecord + modeAt: 1,
		} {
			if err := memory.writeData32(address, value); err != nil {
				t.Fatal(err)
			}
		}
		if err := memory.write8(spilledFrom+2, 0); err != nil {
			t.Fatal(err)
		}
		memory.endQuantum()
		return memory, clippedBlitContext(base, pixels)
	}

	// The comparison only means something if the guest really does clear its
	// flag partway, so that is checked first: the pixel after the one that
	// cleared it, which lands on the flag's high half, is the palette colour
	// drawn plain rather than a blend.
	interpreted, reference := build()
	interpreted.standInsRefused = true
	if _, err := (Engine{}).Run(reference, interpreted, branchPC+2, 1_000_000); err != nil {
		t.Fatalf("interpreting the blit: %v", err)
	}
	interpreted.beginQuantum()
	plain, err := interpreted.readData16(spilledRecord + clippedFlagAt + 2)
	interpreted.endQuantum()
	if err != nil {
		t.Fatal(err)
	}
	if want := uint16(0x0821 * 33 & 0xffff); plain != want {
		t.Fatalf("pixel 3 is %#x, want the plain colour %#x the cleared flag sends it to", plain, want)
	}
	compareStandIn(t, branchPC+2, destination, pixels*2, build)
}
