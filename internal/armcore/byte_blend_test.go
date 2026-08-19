package armcore

import "testing"

// The blend this recognises, as one title emits it: a constant in a frame
// slot, two byte pointers walking together, and a store the guard lets through
// only where the incoming byte wins. It was 7.2% of that title's guest
// instructions in a scene a person reported as slow.
//
// r4 walks the source, r1 the destination, r0 counts, r2 carries the constant
// and then the destination byte, r7 the source byte and r3 the difference.
var byteBlendBody = []uint16{
	0x9a03, // ldr  r2, [sp, #12]
	0x7827, // ldrb r7, [r4]
	0x1abb, // subs r3, r7, r2
	0x780a, // ldrb r2, [r1]
	0x4293, // cmp  r3, r2
	0xdd00, // ble  +0 — over the store
	0x700b, // strb r3, [r1]
	0x3801, // subs r0, #1
	0x3101, // adds r1, #1
	0x3401, // adds r4, #1
	0x2800, // cmp  r0, #0
	0xd1f3, // bne  -26
}

func TestAGuardedByteBlendIsRecognised(t *testing.T) {
	const base, destination = 0x00100000, 0x00200000
	memory := fillLoopMemory(t, byteBlendBody, base, destination)
	memory.beginQuantum()
	defer memory.endQuantum()

	branchPC := base + uint32(len(byteBlendBody)-1)*2
	loop := memory.analyseByteBlend(base, branchPC)
	if loop == nil {
		t.Fatal("the guarded byte blend was not recognised")
	}
	if loop.source != 4 || loop.destination != 1 || loop.counter != 0 {
		t.Errorf("source=r%d destination=r%d counter=r%d, want r4/r1/r0",
			loop.source, loop.destination, loop.counter)
	}
	if loop.constant != 12 {
		t.Errorf("constant at sp+%d, want sp+12", loop.constant)
	}
	if loop.steps != 12 {
		t.Errorf("steps=%d, want the twelve instructions of one iteration", loop.steps)
	}
}

// A body whose guard jumps somewhere other than over the store is a different
// loop, and the analysis has to say so rather than blend with it.
func TestAByteBlendWhoseGuardSkipsMoreIsRefused(t *testing.T) {
	const base, destination = 0x00100000, 0x00200000
	body := append([]uint16(nil), byteBlendBody...)
	body[5] = 0xdd01 // ble +2, over the store and the counter as well
	memory := fillLoopMemory(t, body, base, destination)
	memory.beginQuantum()
	defer memory.endQuantum()

	if loop := memory.analyseByteBlend(base, base+uint32(len(body)-1)*2); loop != nil {
		t.Fatalf("a guard skipping two instructions was recognised as a blend: %+v", loop)
	}
}

// Standing in for the blend has to be indistinguishable from running it, and
// this shape has more to leave behind than the earlier two: the last source
// byte, the last destination byte and the last difference all stay in
// registers the code after the loop reads.
func TestStandingInForAByteBlendMatchesRunningIt(t *testing.T) {
	const base, destination = 0x00100000, 0x00200000
	const stack, source = 0x70000000, 0x70000400
	const bytes, constant = 61, 0x20

	branchPC := base + uint32(len(byteBlendBody)-1)*2

	build := func() (*Memory, *Context) {
		memory := fillLoopMemory(t, byteBlendBody, base, destination)
		memory.beginQuantum()
		if err := memory.writeData32(stack+12, constant); err != nil {
			t.Fatal(err)
		}
		// A source and a destination that cross above and below each other, so
		// the guard is exercised both ways rather than always taken.
		for index := 0; index < bytes; index++ {
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

	interpreted, reference := build()
	stood, subject := build()
	interpreted.standInsRefused = true
	if _, err := (Engine{}).Run(reference, interpreted, branchPC+2, 1_000_000); err != nil {
		t.Fatalf("interpreting the blend: %v", err)
	}
	if _, err := (Engine{}).Run(subject, stood, branchPC+2, 1_000_000); err != nil {
		t.Fatalf("standing in for the blend: %v", err)
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
	// One byte past the end as well: a stand-in that runs one iteration too
	// many is exactly the mistake this shape invites.
	for index := 0; index <= bytes; index++ {
		address := uint32(int(destination) + index)
		want, err := interpreted.read8(address)
		if err != nil {
			t.Fatal(err)
		}
		got, err := stood.read8(address)
		if err != nil {
			t.Fatal(err)
		}
		if want != got {
			t.Fatalf("byte at %+d is %#x after standing in, %#x after interpreting", index, got, want)
		}
	}
}

// The step count has to be what the guest would have retired, and this body is
// the first where that is not one number: the guarded store runs on some
// iterations and not others.
func TestAByteBlendChargesTheStoresItMade(t *testing.T) {
	const base, destination = 0x00100000, 0x00200000
	const stack, source = 0x70000000, 0x70000400
	const bytes = 40

	branchPC := base + uint32(len(byteBlendBody)-1)*2
	memory := fillLoopMemory(t, byteBlendBody, base, destination)
	memory.beginQuantum()
	// Every source byte loses to what is already there, so no store is made.
	for index := 0; index < bytes; index++ {
		if err := memory.write8(source+uint32(index), 0); err != nil {
			t.Fatal(err)
		}
		if err := memory.write8(destination+uint32(index), 0xff); err != nil {
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

	result, err := (Engine{}).Run(context, memory, branchPC+2, 1_000_000)
	if err != nil {
		t.Fatal(err)
	}
	// Twelve instructions an iteration, less the store the guard refused every
	// time. The closing branch runs on every iteration, the last one included,
	// where it is simply not taken.
	want := uint32(bytes * 11)
	if result.Steps != want {
		t.Errorf("charged %d steps, want %d", result.Steps, want)
	}
	// And the same loop interpreted has to retire exactly that many, or the
	// stand-in has quietly given the guest a longer run than its budget allows.
	interpreted := fillLoopMemory(t, byteBlendBody, base, destination)
	interpreted.standInsRefused = true
	interpreted.beginQuantum()
	for index := 0; index < bytes; index++ {
		if err := interpreted.write8(source+uint32(index), 0); err != nil {
			t.Fatal(err)
		}
		if err := interpreted.write8(destination+uint32(index), 0xff); err != nil {
			t.Fatal(err)
		}
	}
	interpreted.endQuantum()
	reference := NewContext()
	reference.Registers[RegisterSP] = stack
	reference.Registers[0] = bytes
	reference.Registers[1] = destination
	reference.Registers[4] = source
	reference.setThumbPC(base)
	control, err := (Engine{}).Run(&reference, interpreted, branchPC+2, 1_000_000)
	if err != nil {
		t.Fatal(err)
	}
	if control.Steps != result.Steps {
		t.Errorf("charged %d steps, interpreting the same loop retired %d", result.Steps, control.Steps)
	}
}
