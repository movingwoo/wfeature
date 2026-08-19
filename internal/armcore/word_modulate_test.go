package armcore

import (
	"encoding/binary"
	"testing"
)

// The modulate this recognises, as one title emits it: two streams of packed
// pixels walked together, each word split into two channel groups by a pair of
// masks, both groups scaled by one factor byte, and the counter kept in the
// high register RVCT spills through. It was 7.3% of that title's guest
// instructions in a scene a person reported as slow.
//
// r6 and r7 walk the two streams, r12 counts, the factor pointer lives at
// sp+8, r5 and r4 hold the masks, and r0/r1/r2/r3 carry the arithmetic.
func wordModulateBody() []uint16 {
	return []uint16{
		0x6831, // ldr  r1, [r6]
		0x4d1b, // ldr  r5, [pc, #108]  — the mask shifted first
		0x9a02, // ldr  r2, [sp, #8]    — the factor pointer
		0x4c1b, // ldr  r4, [pc, #108]  — the mask shifted second
		0x1c0b, // adds r3, r1, #0
		0x7810, // ldrb r0, [r2]
		0x402b, // ands r3, r5
		0x095b, // lsrs r3, r3, #5
		0x4021, // ands r1, r4
		0x1c02, // adds r2, r0, #0
		0x435a, // muls r2, r3
		0x1c03, // adds r3, r0, #0
		0x434b, // muls r3, r1
		0x095b, // lsrs r3, r3, #5
		0x4023, // ands r3, r4
		0x402a, // ands r2, r5
		0x431a, // orrs r2, r3
		0xc604, // stm  r6!, {r2}
		0x6839, // ldr  r1, [r7]
		0x9a02, // ldr  r2, [sp, #8]
		0x1c0b, // adds r3, r1, #0
		0x7810, // ldrb r0, [r2]
		0x402b, // ands r3, r5
		0x095b, // lsrs r3, r3, #5
		0x4021, // ands r1, r4
		0x1c02, // adds r2, r0, #0
		0x435a, // muls r2, r3
		0x1c03, // adds r3, r0, #0
		0x434b, // muls r3, r1
		0x095b, // lsrs r3, r3, #5
		0x4023, // ands r3, r4
		0x402a, // ands r2, r5
		0x431a, // orrs r2, r3
		0x2301, // movs r3, #1
		0x425b, // rsbs r3, r3, #0
		0xc704, // stm  r7!, {r2}
		0x9a02, // ldr  r2, [sp, #8]
		0x449c, // add  r12, r3
		0x3201, // adds r2, #1
		0x4663, // mov  r3, r12
		0x9202, // str  r2, [sp, #8]
		0x2b00, // cmp  r3, #0
		0xd1d4, // bne  -84
	}
}

const (
	modulateMaskFirst  = uint32(0xf81f07e0)
	modulateMaskSecond = uint32(0x07e0f81f)
)

// modulateMemory lays the body out with both mask literals where the two
// `ldr rD, [pc, #108]` instructions reach them, and two streams and a factor
// stream in writable memory.
func modulateMemory(t *testing.T, body []uint16, base, data uint32) *Memory {
	t.Helper()
	memory := NewMemory()
	if err := memory.Map(base, memoryPageSize, PermissionRead|PermissionExecute); err != nil {
		t.Fatal(err)
	}
	if err := memory.Map(data, memoryPageSize*4, PermissionRead|PermissionWrite); err != nil {
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
	// The literals the two mask loads reach: `ldr r5, [pc, #108]` at base+2
	// reads (base+2+4)&^3 + 108, and the one at base+6 reads four bytes past
	// that. They go in with Load, because the code they belong to is mapped
	// without write permission — which is what makes them loop-invariant.
	first := (base+2+4)&^3 + 108
	literals := make([]byte, 8)
	binary.LittleEndian.PutUint32(literals, modulateMaskFirst)
	binary.LittleEndian.PutUint32(literals[4:], modulateMaskSecond)
	if err := memory.Load(first, literals); err != nil {
		t.Fatal(err)
	}
	return memory
}

func TestATwoStreamModulateIsRecognised(t *testing.T) {
	const base, data = 0x00100000, 0x00200000
	body := wordModulateBody()
	memory := modulateMemory(t, body, base, data)
	memory.beginQuantum()
	defer memory.endQuantum()

	branchPC := base + uint32(len(body)-1)*2
	loop := memory.analyseWordModulate(base, branchPC)
	if loop == nil {
		t.Fatal("the two-stream modulate was not recognised")
	}
	if loop.first != 6 || loop.second != 7 || loop.counter != 12 {
		t.Errorf("first=r%d second=r%d counter=r%d, want r6/r7/r12",
			loop.first, loop.second, loop.counter)
	}
	if loop.maskFirst != 5 || loop.maskSecond != 4 {
		t.Errorf("masks in r%d and r%d, want r5 and r4", loop.maskFirst, loop.maskSecond)
	}
	if loop.shiftFirst != 5 || loop.shiftSecond != 5 {
		t.Errorf("shifts %d and %d, want 5 and 5", loop.shiftFirst, loop.shiftSecond)
	}
	if loop.slot != 8 {
		t.Errorf("factor pointer at sp+%d, want sp+8", loop.slot)
	}
	if loop.steps != 43 {
		t.Errorf("steps=%d, want the forty-three instructions of one iteration", loop.steps)
	}
}

// The two halves have to agree in everything but which stream they walk. A
// body whose second half scales by a different shift is a different loop.
func TestAModulateWhoseHalvesDisagreeIsRefused(t *testing.T) {
	const base, data = 0x00100000, 0x00200000
	body := wordModulateBody()
	body[23] = 0x099b // lsrs r3, r3, #6 in the second half only
	memory := modulateMemory(t, body, base, data)
	memory.beginQuantum()
	defer memory.endQuantum()

	if loop := memory.analyseWordModulate(base, base+uint32(len(body)-1)*2); loop != nil {
		t.Fatalf("two halves that disagree were recognised as one modulate: %+v", loop)
	}
}

// Standing in for it has to be indistinguishable from running it: both
// streams, the factor pointer the loop keeps in its frame, the registers, and
// the flags the instruction after the loop reads.
func TestStandingInForAModulateMatchesRunningIt(t *testing.T) {
	const base, data = 0x00100000, 0x00200000
	const stack = 0x70000000
	const first, second, factors = data, data + 0x800, 0x70000400
	const steps = 53

	body := wordModulateBody()
	branchPC := base + uint32(len(body)-1)*2

	build := func() (*Memory, *Context) {
		memory := modulateMemory(t, body, base, data)
		memory.beginQuantum()
		if err := memory.writeData32(stack+8, factors); err != nil {
			t.Fatal(err)
		}
		for index := uint32(0); index < steps; index++ {
			if err := memory.writeData32(first+index*4, 0x5a3c<<16|uint32(index)*37); err != nil {
				t.Fatal(err)
			}
			if err := memory.writeData32(second+index*4, 0xffff<<16|uint32(index)*911); err != nil {
				t.Fatal(err)
			}
			// Factors from nothing to full brightness, so the multiply is
			// exercised where it wraps as well as where it does not.
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
		// The masks are in their registers before the loop is entered, which
		// is what the guest's own first iteration would have done.
		context.Registers[5] = modulateMaskFirst
		context.Registers[4] = modulateMaskSecond
		context.setThumbPC(base)
		return memory, context
	}

	interpreted, reference := build()
	stood, subject := build()
	interpreted.standInsRefused = true
	if _, err := (Engine{}).Run(reference, interpreted, branchPC+2, 10_000_000); err != nil {
		t.Fatalf("interpreting the modulate: %v", err)
	}
	if _, err := (Engine{}).Run(subject, stood, branchPC+2, 10_000_000); err != nil {
		t.Fatalf("standing in for the modulate: %v", err)
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
	for _, stream := range []uint32{first, second} {
		// One word past the end as well, which is where a stand-in that ran an
		// iteration too many would show.
		for index := uint32(0); index <= steps; index++ {
			want, err := interpreted.readData32(stream + index*4)
			if err != nil {
				t.Fatal(err)
			}
			got, err := stood.readData32(stream + index*4)
			if err != nil {
				t.Fatal(err)
			}
			if want != got {
				t.Fatalf("word %d of the stream at %#x is %#x after standing in, %#x after interpreting",
					index, stream, got, want)
			}
		}
	}
	// The factor pointer is the loop's own state and lives in memory, not in a
	// register: a stand-in that forgot to write it back would leave the next
	// caller reading the same factors again.
	want, err := interpreted.readData32(stack + 8)
	if err != nil {
		t.Fatal(err)
	}
	got, err := stood.readData32(stack + 8)
	if err != nil {
		t.Fatal(err)
	}
	if want != got {
		t.Errorf("the factor pointer is %#x after standing in, %#x after interpreting", got, want)
	}
}

// The step count has to be what the guest would have retired, or a title that
// would have run out of budget no longer does.
func TestAModulateChargesWhatItStoodInFor(t *testing.T) {
	const base, data = 0x00100000, 0x00200000
	const stack = 0x70000000
	const first, second, factors = data, data + 0x800, 0x70000400
	const steps = 25

	body := wordModulateBody()
	branchPC := base + uint32(len(body)-1)*2

	run := func(refuse bool) uint32 {
		memory := modulateMemory(t, body, base, data)
		if refuse {
			memory.standInsRefused = true
		}
		memory.beginQuantum()
		if err := memory.writeData32(stack+8, factors); err != nil {
			t.Fatal(err)
		}
		memory.endQuantum()
		value := NewContext()
		context := &value
		context.Registers[RegisterSP] = stack
		context.Registers[6] = first
		context.Registers[7] = second
		context.Registers[12] = steps
		context.Registers[5] = modulateMaskFirst
		context.Registers[4] = modulateMaskSecond
		context.setThumbPC(base)
		result, err := (Engine{}).Run(context, memory, branchPC+2, 10_000_000)
		if err != nil {
			t.Fatal(err)
		}
		return result.Steps
	}

	if stood, interpreted := run(false), run(true); stood != interpreted {
		t.Errorf("charged %d steps, interpreting the same loop retired %d", stood, interpreted)
	}
}
