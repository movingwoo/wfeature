package armcore

// Recognising a two-stream modulate, and standing in for it.
//
// The fourth shape, and the longest. It walks **two** streams of packed pixels
// at once — two rows of the same sprite — and scales every channel of each by
// a factor it takes from a third stream, one byte a step. The packing is the
// usual one for a 16-bit screen: two pixels in a word, and the channels split
// into two groups by a pair of masks so that a single multiply does both
// pixels at once. Forty-two instructions a step, and 7.3% of every instruction
// one local title executed in a scene a person reported as slow.
//
// **This one is matched as a sequence, not as a walk**, which is a departure
// from the three recognisers before it and is worth being plain about. A role
// walk works when each instruction contributes one distinct effect; here the
// same thirteen-instruction computation appears twice over two different
// stream pointers, and "this instruction is the second half's third `ands`"
// is a position, not a role. So the body is read in order against the shape,
// with every register bound where it is first met and checked everywhere it is
// used again, every immediate and shift amount read out rather than assumed,
// and the two halves required to agree in everything but which stream they
// walk. Nothing is matched by encoding: a title that spilled its registers
// differently would bind different numbers and still be recognised.
//
// What the sequence costs is generality, and that is the honest trade: this is
// one title's blend, and the shape is written down in `docs/armcore.md` beside
// the two the corpus has that are not this.
//
// Everything else is the discipline the other three keep. Spans are validated
// whole before anything moves. What the loop re-reads every step — the two
// mask literals in its own code, the frame slot the factor pointer lives in,
// and the factor stream itself — must not lie inside either stream, or a
// stand-in that read them once would be reading something the guest would have
// seen change. And the last iteration is left to the interpreter, so every
// scratch register and every flag is whatever the guest's own instructions
// make it.

const (
	// wordModulateBytes is the exact length of the body this recognises. A
	// sequence match has one length by construction.
	wordModulateBytes = 84
	wordModulateSteps = wordModulateBytes / 2
)

// wordModulate is a recognised two-stream modulate.
type wordModulate struct {
	first, second uint32 // registers walking the two streams, a word a step
	maskFirst     uint32 // register holding the mask shifted before multiplying
	maskSecond    uint32 // register holding the mask shifted after
	shiftFirst    uint32
	shiftSecond   uint32
	counter       uint32 // register counting down to zero, high or low
	// slot is the frame-slot offset holding the pointer to the factor stream,
	// which the loop advances and writes back every step.
	slot uint32
	// literals are the addresses the two masks are read from, inside the
	// loop's own code.
	literals [2]uint32
	steps    uint32
}

// runWordModulate stands in for every remaining iteration but the last, and
// reports how many guest instructions it stood in for.
func (memory *Memory) runWordModulate(context *Context, loop *wordModulate) (uint32, error) {
	remaining := context.Registers[loop.counter]
	if remaining > maxStoreLoopIterations || remaining < 2 {
		return 0, nil
	}
	iterations := remaining - 1

	first := context.Registers[loop.first]
	second := context.Registers[loop.second]
	// A word transfer through a walking pointer is aligned in any real
	// rasteriser, and an unaligned one means something this has misread: the
	// load would rotate and the store-multiple is not defined at all.
	if first&3 != 0 || second&3 != 0 {
		return 0, nil
	}
	slot := context.Registers[RegisterSP] + loop.slot
	factors, err := memory.readData32(slot)
	if err != nil {
		// The fault the next iteration would have taken, where it would have.
		return 0, err
	}
	maskFirst := context.Registers[loop.maskFirst]
	maskSecond := context.Registers[loop.maskSecond]

	span := uint64(iterations) * 4
	if err := memory.validateLocked(first, span, PermissionReadWrite, "write"); err != nil {
		return 0, nil
	}
	if err := memory.validateLocked(second, span, PermissionReadWrite, "write"); err != nil {
		return 0, nil
	}
	if err := memory.validateLocked(factors, uint64(iterations), PermissionRead, "read"); err != nil {
		return 0, nil
	}
	// Everything the loop re-reads every step has to be somewhere the loop
	// does not write, or reading it once is not what the guest did.
	for _, stream := range [...]uint32{first, second} {
		for _, invariant := range [...]uint32{loop.literals[0], loop.literals[1], slot} {
			if spansOverlap(stream, span, invariant, 4) {
				return 0, nil
			}
		}
		if spansOverlap(stream, span, factors, uint64(iterations)) {
			return 0, nil
		}
	}

	for index := uint32(0); index < iterations; index++ {
		factor, err := memory.read8(factors + index)
		if err != nil {
			return 0, err
		}
		scale := uint32(factor)
		// The two streams in the order the guest takes them, so that two rows
		// which overlap come out as they would have.
		for _, stream := range [...]uint32{first, second} {
			address := stream + index*4
			value, err := memory.readData32(address)
			if err != nil {
				return 0, err
			}
			blended := ((((value & maskFirst) >> loop.shiftFirst) * scale) & maskFirst) |
				((((value & maskSecond) * scale) >> loop.shiftSecond) & maskSecond)
			if err := memory.writeData32(address, blended); err != nil {
				return 0, err
			}
		}
	}

	context.Registers[loop.first] = first + iterations*4
	context.Registers[loop.second] = second + iterations*4
	context.Registers[loop.counter] = remaining - iterations
	// The factor pointer lives in the frame, not in a register: the guest
	// reads it, advances it and writes it back every step.
	if err := memory.writeData32(slot, factors+iterations); err != nil {
		return 0, err
	}
	// The PC stays at the loop's head, where the taken branch left it, and the
	// iteration left over is interpreted from there.
	return iterations * loop.steps, nil
}

// spansOverlap reports whether two spans share a byte.
func spansOverlap(first uint32, firstLength uint64, second uint32, secondLength uint64) bool {
	return uint64(first) < uint64(second)+secondLength && uint64(second) < uint64(first)+firstLength
}

// analyseWordModulate reads the body between head and branchPC and answers the
// modulate it is, or nil for one it cannot prove.
func (memory *Memory) analyseWordModulate(head, branchPC uint32) *wordModulate {
	if branchPC <= head || branchPC-head != wordModulateBytes {
		return nil
	}
	var body [wordModulateSteps]decodedThumb
	for index := range body {
		address := head + uint32(index)*2
		decoded, cached := memory.decodedThumbFast(address)
		if !cached {
			var err error
			if decoded, err = memory.decodeThumb(address); err != nil {
				return nil
			}
		}
		body[index] = decoded
	}

	loop := &memory.wordModulateScratch
	*loop = wordModulate{}
	// The first half's prologue: a word off the first stream, the two masks
	// out of the loop's own literals, and the factor pointer out of the frame.
	word, first, ok := thumbWordLoad(body[0])
	if !ok {
		return nil
	}
	maskFirst, literalFirst, ok := thumbLiteralAddress(body[1], head+2)
	if !ok {
		return nil
	}
	pointer, slot, ok := thumbFrameLoad(body[2])
	if !ok {
		return nil
	}
	maskSecond, literalSecond, ok := thumbLiteralAddress(body[3], head+6)
	if !ok {
		return nil
	}
	loop.first, loop.maskFirst, loop.maskSecond = first, maskFirst, maskSecond
	loop.slot, loop.literals = slot, [2]uint32{literalFirst, literalSecond}

	step := modulateStep{word: word, maskFirst: maskFirst, maskSecond: maskSecond, pointer: pointer}
	if !step.matches(body[4:17]) {
		return nil
	}
	if base, list, ok := thumbStoreMultiple(body[17]); !ok || base != first || list != 1<<step.out {
		return nil
	}

	// The second half is the same computation over the other stream, and it
	// has to agree with the first in every register it did not have to change.
	secondWord, second, ok := thumbWordLoad(body[18])
	if !ok || secondWord != word {
		return nil
	}
	secondPointer, secondSlot, ok := thumbFrameLoad(body[19])
	if !ok || secondPointer != pointer || secondSlot != slot {
		return nil
	}
	loop.second = second
	if !step.matches(body[20:33]) {
		return nil
	}
	// The decrement is written as a materialised minus one, because the only
	// add the high register the counter lives in can take is a register.
	negative, immediate, ok := thumbMoveImmediate(body[33])
	if !ok || immediate != 1 {
		return nil
	}
	if destination, source, ok := thumbNegate(body[34]); !ok || destination != negative || source != negative {
		return nil
	}
	if base, list, ok := thumbStoreMultiple(body[35]); !ok || base != second || list != 1<<step.out {
		return nil
	}

	// The tail: the factor pointer forward by one and back into its slot, the
	// counter down by one, and the compare the closing branch reads.
	advance, tailSlot, ok := thumbFrameLoad(body[36])
	if !ok || tailSlot != slot {
		return nil
	}
	counter, addend, ok := thumbHighAdd(body[37])
	if !ok || addend != negative {
		return nil
	}
	if register, forward, ok := thumbAddImmediate(body[38]); !ok || register != advance || forward != 1 {
		return nil
	}
	test, source, ok := thumbHighMove(body[39])
	if !ok || source != counter {
		return nil
	}
	if register, storedSlot, ok := thumbFrameStore(body[40]); !ok || register != advance || storedSlot != slot {
		return nil
	}
	if register, against, ok := thumbCompareImmediate(body[41]); !ok || register != test || against != 0 {
		return nil
	}
	if !memory.branchIsNotEqual(branchPC) {
		return nil
	}
	loop.counter = counter
	loop.shiftFirst, loop.shiftSecond = step.shiftFirst, step.shiftSecond

	// The counter may be a high register — it is r12 in the title this was
	// read from, which is where RVCT spills — but it may not be one of the
	// registers whose meaning this analysis did not derive.
	if counter >= RegisterSP {
		return nil
	}
	// The two streams and the counter each need a register of their own, and
	// none of them may be one the body rewrites every step.
	var seen uint32
	for _, register := range [...]uint32{first, second, counter} {
		if seen&(1<<register) != 0 {
			return nil
		}
		seen |= 1 << register
	}
	for _, register := range [...]uint32{word, pointer, step.alpha, step.temp, step.out, maskFirst, maskSecond, advance, test} {
		if seen&(1<<register) != 0 {
			return nil
		}
	}
	loop.steps = wordModulateSteps + 1
	return loop
}

// modulateStep is the thirteen instructions that scale one packed word, held
// apart because the body performs it twice and the two have to agree.
type modulateStep struct {
	word        uint32 // register the packed word was loaded into
	maskFirst   uint32
	maskSecond  uint32
	pointer     uint32 // register holding the factor pointer
	alpha       uint32 // register the factor is loaded into
	temp        uint32
	out         uint32 // register the blended word ends up in
	shiftFirst  uint32
	shiftSecond uint32
	bound       bool
}

// matches reads the thirteen instructions and answers whether they are this
// step, binding the registers it has not seen before and checking the ones it
// has.
func (step *modulateStep) matches(body []decodedThumb) bool {
	if len(body) != 13 {
		return false
	}
	temp, source, ok := thumbMoveRegister(body[0])
	if !ok || source != step.word {
		return false
	}
	alpha, base, ok := thumbByteLoad(body[1])
	if !ok || base != step.pointer {
		return false
	}
	if destination, operand, ok := thumbAnd(body[2]); !ok || destination != temp || operand != step.maskFirst {
		return false
	}
	shiftFirst, destination, shifted, ok := thumbShiftRight(body[3])
	if !ok || destination != temp || shifted != temp {
		return false
	}
	if destination, operand, ok := thumbAnd(body[4]); !ok || destination != step.word || operand != step.maskSecond {
		return false
	}
	out, factor, ok := thumbMoveRegister(body[5])
	if !ok || factor != alpha {
		return false
	}
	if destination, operand, ok := thumbMultiply(body[6]); !ok || destination != out || operand != temp {
		return false
	}
	if destination, factor, ok := thumbMoveRegister(body[7]); !ok || destination != temp || factor != alpha {
		return false
	}
	if destination, operand, ok := thumbMultiply(body[8]); !ok || destination != temp || operand != step.word {
		return false
	}
	shiftSecond, destination, shifted, ok := thumbShiftRight(body[9])
	if !ok || destination != temp || shifted != temp {
		return false
	}
	if destination, operand, ok := thumbAnd(body[10]); !ok || destination != temp || operand != step.maskSecond {
		return false
	}
	if destination, operand, ok := thumbAnd(body[11]); !ok || destination != out || operand != step.maskFirst {
		return false
	}
	if destination, operand, ok := thumbOr(body[12]); !ok || destination != out || operand != temp {
		return false
	}
	if step.bound {
		return step.alpha == alpha && step.temp == temp && step.out == out &&
			step.shiftFirst == shiftFirst && step.shiftSecond == shiftSecond
	}
	step.alpha, step.temp, step.out = alpha, temp, out
	step.shiftFirst, step.shiftSecond = shiftFirst, shiftSecond
	step.bound = true
	return true
}

// The readers below answer one instruction's operands, or ok false for an
// instruction that is not the form asked for. They exist so that the sequence
// above reads as the shape it matches rather than as bit arithmetic.

func thumbWordLoad(decoded decodedThumb) (data, base uint32, ok bool) {
	value := uint32(decoded.instruction)
	if decoded.form != thumbImmediateTransfer || value&(1<<12) != 0 || value&(1<<11) == 0 || value>>6&0x1f != 0 {
		return 0, 0, false
	}
	return value & 7, value >> 3 & 7, true
}

func thumbByteLoad(decoded decodedThumb) (data, base uint32, ok bool) {
	value := uint32(decoded.instruction)
	if decoded.form != thumbImmediateTransfer || value&(1<<12) == 0 || value&(1<<11) == 0 || value>>6&0x1f != 0 {
		return 0, 0, false
	}
	return value & 7, value >> 3 & 7, true
}

// thumbLiteralAddress answers the register loaded and the address it is loaded
// from, which is fixed by the instruction's own position.
func thumbLiteralAddress(decoded decodedThumb, pc uint32) (data, address uint32, ok bool) {
	value := uint32(decoded.instruction)
	if decoded.form != thumbLiteralLoad {
		return 0, 0, false
	}
	return value >> 8 & 7, (pc+4)&^3 + (value&0xff)*4, true
}

func thumbFrameLoad(decoded decodedThumb) (data, offset uint32, ok bool) {
	value := uint32(decoded.instruction)
	if decoded.form != thumbStackRelativeTransfer || value&(1<<11) == 0 {
		return 0, 0, false
	}
	return value >> 8 & 7, (value & 0xff) * 4, true
}

func thumbFrameStore(decoded decodedThumb) (data, offset uint32, ok bool) {
	value := uint32(decoded.instruction)
	if decoded.form != thumbStackRelativeTransfer || value&(1<<11) != 0 {
		return 0, 0, false
	}
	return value >> 8 & 7, (value & 0xff) * 4, true
}

func thumbStoreMultiple(decoded decodedThumb) (base, list uint32, ok bool) {
	value := uint32(decoded.instruction)
	if decoded.form != thumbMultipleTransfer || value&(1<<11) != 0 {
		return 0, 0, false
	}
	return value >> 8 & 7, value & 0xff, true
}

// thumbMoveRegister is `adds rD, rS, #0`, which is how a Thumb-1 compiler
// copies one low register into another.
func thumbMoveRegister(decoded decodedThumb) (destination, source uint32, ok bool) {
	value := uint32(decoded.instruction)
	if decoded.form != thumbAddSubtract || value&(1<<10) == 0 || value&(1<<9) != 0 || value>>6&7 != 0 {
		return 0, 0, false
	}
	return value & 7, value >> 3 & 7, true
}

func thumbShiftRight(decoded decodedThumb) (amount, destination, source uint32, ok bool) {
	value := uint32(decoded.instruction)
	if decoded.form != thumbShift || value>>11&3 != 1 {
		return 0, 0, 0, false
	}
	return value >> 6 & 0x1f, value & 7, value >> 3 & 7, true
}

func thumbALUOperation(decoded decodedThumb, opcode uint32) (destination, operand uint32, ok bool) {
	value := uint32(decoded.instruction)
	if decoded.form != thumbALU || value>>6&0xf != opcode {
		return 0, 0, false
	}
	return value & 7, value >> 3 & 7, true
}

func thumbAnd(decoded decodedThumb) (destination, operand uint32, ok bool) {
	return thumbALUOperation(decoded, 0)
}

func thumbOr(decoded decodedThumb) (destination, operand uint32, ok bool) {
	return thumbALUOperation(decoded, 12)
}

func thumbMultiply(decoded decodedThumb) (destination, operand uint32, ok bool) {
	return thumbALUOperation(decoded, 13)
}

func thumbNegate(decoded decodedThumb) (destination, source uint32, ok bool) {
	return thumbALUOperation(decoded, 9)
}

func thumbImmediateOperation(decoded decodedThumb, opcode uint32) (register, operand uint32, ok bool) {
	value := uint32(decoded.instruction)
	if decoded.form != thumbImmediate || value>>11&3 != opcode {
		return 0, 0, false
	}
	return value >> 8 & 7, value & 0xff, true
}

func thumbMoveImmediate(decoded decodedThumb) (register, operand uint32, ok bool) {
	return thumbImmediateOperation(decoded, 0)
}

func thumbCompareImmediate(decoded decodedThumb) (register, operand uint32, ok bool) {
	return thumbImmediateOperation(decoded, 1)
}

func thumbAddImmediate(decoded decodedThumb) (register, operand uint32, ok bool) {
	return thumbImmediateOperation(decoded, 2)
}

func thumbHighOperation(decoded decodedThumb, opcode uint32) (destination, source uint32, ok bool) {
	value := uint32(decoded.instruction)
	if decoded.form != thumbHighRegister || value>>8&3 != opcode {
		return 0, 0, false
	}
	return value&7 | value>>4&8, value >> 3 & 0xf, true
}

func thumbHighAdd(decoded decodedThumb) (destination, source uint32, ok bool) {
	return thumbHighOperation(decoded, 0)
}

func thumbHighMove(decoded decodedThumb) (destination, source uint32, ok bool) {
	return thumbHighOperation(decoded, 2)
}
