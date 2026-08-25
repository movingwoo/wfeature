package armcore

// Recognising a guarded byte blend, and standing in for it.
//
// The third shape a rasteriser spends its time in, after the counted fill and
// the palette blit. It walks a byte source and a byte destination together,
// subtracts a constant from each source byte, and keeps the result **only
// where it is greater than the byte already there** — a lighting or shadow
// pass over an 8-bit buffer. Eleven instructions a byte, and 7.2% of every
// instruction one local title executed in a scene a person reported as slow.
//
// Two things make it different from the two shapes already recognised, and
// both are worth stating because they are what the analysis had to grow.
//
// **The body branches.** The store is guarded by a forward conditional branch
// that jumps over it, so the loop is no longer a straight run of instructions
// each contributing one effect. The walk therefore adds a positional rule to
// the role rules: the compare, the branch and the store have to be adjacent,
// in that order, and the branch's target has to be the instruction after the
// store. A guard that guards something else is not this shape.
//
// **The last iteration is left to the interpreter.** The stand-in runs all but
// one of the iterations that remain and leaves the loop pointing at the last
// one, which the engine then interprets normally. That is what makes the
// scratch registers right for free: this body leaves the last source byte, the
// last destination byte and the last difference in registers the guest goes on
// to read, and a stand-in that computed the loop whole would have to reproduce
// each of them. One interpreted iteration out of thousands costs nothing and
// removes the whole class of mistake. The flags are right for the same reason
// — the interpreter's own compare sets them.

// maxByteBlendBytes bounds this body, which is a byte's worth of work and a
// guard around one store.
const maxByteBlendBytes = 32

// byteBlend is a recognised guarded byte blend.
type byteBlend struct {
	source      uint32 // register walking the source, one byte a step
	destination uint32 // register walking the destination, one byte a step
	counter     uint32 // register counting down to zero
	// constant is the frame-slot offset the subtracted value is read from.
	// The stack pointer is the one base a body of this shape cannot move,
	// which is what makes the address loop-invariant; see fill_loop.go.
	constant uint32
	// steps is one iteration's instructions with the guarded store included,
	// so a skipped store is charged as one instruction less.
	steps uint32
}

// runByteBlend stands in for every remaining iteration but the last, and
// reports how many guest instructions it stood in for.
func (memory *Memory) runByteBlend(context *Context, loop *byteBlend) (uint32, error) {
	remaining := context.Registers[loop.counter]
	if remaining > maxStoreLoopIterations || remaining < 2 {
		return 0, nil
	}
	iterations := remaining - 1

	source := context.Registers[loop.source]
	destination := context.Registers[loop.destination]
	slot := context.Registers[RegisterSP] + loop.constant

	// Both spans whole, before anything moves.
	if err := memory.validateLocked(source, uint64(iterations), PermissionRead, "read"); err != nil {
		return 0, nil
	}
	if err := memory.validateLocked(destination, uint64(iterations), PermissionReadWrite, "write"); err != nil {
		return 0, nil
	}
	// The constant is read once and the guest reads it every iteration, so a
	// destination that covers it would be a loop whose constant changes under
	// it. Refused rather than modelled: no title does this, and the cost of
	// being wrong about it is a whole scene of wrong pixels.
	if uint64(slot)+4 > uint64(destination) && uint64(slot) < uint64(destination)+uint64(iterations) {
		return 0, nil
	}
	value, err := memory.readData32(slot)
	if err != nil {
		// The fault the next iteration would have taken, where it would have.
		return 0, err
	}
	constant := int32(value)

	// Charged as though every instruction had run, which for this body means
	// counting the store only where the guard let it through.
	charged := uint32(0)
	for index := uint32(0); index < iterations; index++ {
		incoming, err := memory.read8(source + index)
		if err != nil {
			return charged, err
		}
		current, err := memory.read8(destination + index)
		if err != nil {
			return charged, err
		}
		blended := int32(incoming) - constant
		if blended > int32(current) {
			if err := memory.write8(destination+index, uint8(blended)); err != nil {
				return charged, err
			}
			charged += loop.steps
			continue
		}
		charged += loop.steps - 1
	}

	context.Registers[loop.source] = source + iterations
	context.Registers[loop.destination] = destination + iterations
	context.Registers[loop.counter] = remaining - iterations
	// The PC stays where the taken branch left it, at the loop's head: the
	// iteration left over is interpreted from there, and it is what sets every
	// scratch register and every flag the code after the loop reads.
	return charged, nil
}

// analyseByteBlend reads the body between head and branchPC and answers the
// blend it is, or nil for one it cannot prove.
func (memory *Memory) analyseByteBlend(head, branchPC uint32) *byteBlend {
	if branchPC <= head || branchPC-head > maxByteBlendBytes {
		return nil
	}

	loop := &memory.byteBlendScratch
	*loop = byteBlend{}
	var (
		constant   uint32 // register the subtracted value is loaded into
		difference uint32 // register holding source byte less the constant
		incoming   uint32 // register holding the source byte
		current    uint32 // register holding the destination byte
		// The two byte loads are told apart by which base the store uses, so
		// they are collected first and assigned after the walk.
		loadBases   [2]uint32
		loadTargets [2]uint32
		loads       int
		storeBase   uint32
		storePC     uint32
		guardPC     uint32
		comparePC   uint32
		counterPC   uint32

		haveConstant   bool
		haveDifference bool
		haveStore      bool
		haveGuard      bool
		haveCompare    bool
		haveSource     bool // the source pointer advanced by one
		haveDest       bool // the destination pointer advanced by one
		haveDecrement  bool
		haveCounter    bool // the counter was compared against zero
	)

	for address := head; address < branchPC; address += 2 {
		decoded, cached := memory.decodedThumbFast(address)
		if !cached {
			var err error
			if decoded, err = memory.decodeThumb(address); err != nil {
				return nil
			}
		}
		instruction := uint32(decoded.instruction)

		switch decoded.form {
		case thumbStackRelativeTransfer:
			// `ldr rD, [sp, #imm]` — the constant, in the frame slot its
			// caller left it in.
			if instruction&(1<<11) == 0 || haveConstant {
				return nil
			}
			haveConstant = true
			constant, loop.constant = instruction>>8&7, (instruction&0xff)*4

		case thumbImmediateTransfer:
			// The two byte loads and the byte store, all at no offset because
			// the pointers are what walk.
			byteTransfer := instruction&(1<<12) != 0
			load := instruction&(1<<11) != 0
			offset := instruction >> 6 & 0x1f
			base := instruction >> 3 & 7
			data := instruction & 7
			if !byteTransfer || offset != 0 {
				return nil
			}
			if load {
				if loads == len(loadBases) {
					return nil
				}
				loadBases[loads], loadTargets[loads] = base, data
				loads++
				continue
			}
			if haveStore || !haveDifference || data != difference {
				return nil
			}
			haveStore, storeBase, storePC = true, base, address

		case thumbAddSubtract:
			// `subs rD, rS, rC` — the source byte less the constant.
			immediate := instruction&(1<<10) != 0
			subtract := instruction&(1<<9) != 0
			operand := instruction >> 6 & 7
			source := instruction >> 3 & 7
			data := instruction & 7
			if immediate || !subtract || haveDifference || !haveConstant || operand != constant {
				return nil
			}
			haveDifference, difference, incoming = true, data, source

		case thumbALU:
			// `cmp rV, rC` — the guard's comparison, and the only ALU
			// operation this shape performs.
			if instruction>>6&0xf != 0xa || haveCompare || !haveDifference {
				return nil
			}
			if instruction&7 != difference {
				return nil
			}
			haveCompare, current, comparePC = true, instruction>>3&7, address

		case thumbConditionalBranch:
			// The guard: a forward branch over the store, taken when the
			// difference is not greater than what is already there.
			if haveGuard || uint32(instruction)>>8&0xf != 0xd { // LE
				return nil
			}
			// It has to land on the instruction after the store, which the
			// adjacency rule below pins to two halfwords past the branch.
			offset := int32(instruction&0xff) << 24 >> 23
			if uint32(int32(address+4)+offset) != address+4 {
				return nil
			}
			haveGuard, guardPC = true, address

		case thumbImmediate:
			opcode := instruction >> 11 & 3
			register := instruction >> 8 & 7
			operand := instruction & 0xff
			switch {
			case opcode == 3 && operand == 1 && !haveDecrement:
				// `subs rN, #1` — the counter.
				haveDecrement, loop.counter = true, register
			case opcode == 2 && operand == 1 && !haveSource && !haveDest:
				// The first of the two pointers to advance; which is which is
				// settled by the store's base, below.
				haveSource, loop.source = true, register
			case opcode == 2 && operand == 1 && !haveDest:
				haveDest, loop.destination = true, register
			case opcode == 1 && operand == 0 && !haveCounter:
				// `cmp rN, #0` — what the closing branch reads.
				if !haveDecrement || register != loop.counter {
					return nil
				}
				haveCounter, counterPC = true, address
			default:
				return nil
			}

		default:
			return nil
		}
	}

	if !haveConstant || !haveDifference || !haveStore || !haveGuard || !haveCompare ||
		!haveSource || !haveDest || !haveDecrement || !haveCounter || loads != 2 {
		return nil
	}
	// The guard has to guard this store and nothing else, and the compare has
	// to be the one it reads: three instructions, adjacent, in that order.
	if comparePC+2 != guardPC || guardPC+2 != storePC {
		return nil
	}
	// The counter's compare has to be the last thing to set the flags the
	// closing branch reads.
	if counterPC+2 != branchPC {
		return nil
	}
	if !memory.branchIsNotEqual(branchPC) {
		return nil
	}

	// The destination is the pointer the store walks; the source is the other.
	if loop.destination != storeBase {
		if loop.source != storeBase {
			return nil
		}
		loop.source, loop.destination = loop.destination, loop.source
	}
	// One byte load reads the source and the other the destination, and the
	// difference has to be the one taken from the source.
	switch {
	case loadBases[0] == loop.source && loadBases[1] == loop.destination:
		if loadTargets[0] != incoming || loadTargets[1] != current {
			return nil
		}
	case loadBases[1] == loop.source && loadBases[0] == loop.destination:
		if loadTargets[1] != incoming || loadTargets[0] != current {
			return nil
		}
	default:
		return nil
	}

	// The three registers the loop walks each need one of their own, and none
	// of them may be a register the body overwrites every iteration.
	var seen uint32
	for _, register := range [...]uint32{loop.source, loop.destination, loop.counter} {
		if seen&(1<<register) != 0 {
			return nil
		}
		seen |= 1 << register
	}
	for _, register := range [...]uint32{constant, difference, incoming, current} {
		if seen&(1<<register) != 0 {
			return nil
		}
	}
	// The difference and the source byte are separate values held at the same
	// time; the constant's register may be reused for the destination byte,
	// which is what the title this was read from does.
	if difference == incoming || difference == current || difference == constant || incoming == current {
		return nil
	}

	loop.steps = (branchPC-head)/2 + 1
	return loop
}
