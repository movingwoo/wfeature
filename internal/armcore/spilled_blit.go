package armcore

import "encoding/binary"

// Recognising a palette blit that keeps its destination in a frame slot, and
// standing in for it.
//
// `table_blit.go` reads the blit a compiler emits when it has registers to
// spare: a source pointer, a destination pointer, a palette base and a counter,
// each in one of the eight low registers for the whole loop. A title compiled
// with one register fewer does the same blit differently, and the difference is
// not cosmetic — it is what refuses that recogniser on every one of the loops
// the title actually runs:
//
//   - **the destination lives on the stack.** Every pixel loads it out of a
//     frame slot, stores through it, adds two and puts it back. So the
//     destination is not in any register when the loop closes, and the role
//     the other recogniser reads out of one is not there to read.
//   - **the palette base is reloaded every pixel**, through a record the frame
//     points at: `ldr rP, [sp, #a]` then `ldr rT, [rP, #b]`. Both addresses
//     are ones the loop cannot move, so the value is the same every time —
//     but the register holding it is reused for something else before the loop
//     closes, so reading it at the end reads the wrong thing.
//   - **a flag decides the loop's shape and is re-tested every pixel.** The
//     blit is the fall-through of `ldr rG, [rB, #c]; cmp rG, #0; bne blend`,
//     which the other recognisers refuse outright because a branch in the body
//     is a body they cannot reason about.
//
// That third one is the cheap one, and it is worth being precise about why.
// **A recogniser runs at the backward branch**, which is only reached by
// falling through the guard. So the guard has already been evaluated for this
// iteration and did not take. If everything it reads is something the loop
// cannot change, it will not take on any of the iterations that are left
// either — and proving that costs the walk one check per register rather than
// an evaluation.
//
// The rules are `fill_loop.go`'s and are not relaxed here: every instruction
// in the body has to be one of the forms below, each role is filled exactly
// once, both spans are validated whole before a byte moves, the registers and
// the flags are left where the guest would have left them, and the step count
// is charged as though every instruction had run. What this shape adds is that
// the values it treats as invariant are checked against the destination at run
// time: a blit writing over the flag it is guarded by, or over the pointer it
// reads its palette through, is a loop whose later iterations would not have
// done what its first one did, and it is handed back to the interpreter.

// maxSpilledBlitBytes bounds this body. It is longer than a blit that keeps
// its destination in a register — the frame slot costs three instructions a
// pixel and the guard three more — and the loop measured is 32 bytes.
const maxSpilledBlitBytes = 48

// spilledRole is where the loop leaves one low register. A register written by
// an invariant instruction already holds what it will hold at the end, so it
// has no role here; the rest are the values the last iteration computes.
type spilledRole uint8

const (
	// spilledLeave is both "never written" and "written only with a value the
	// loop cannot change", which come to the same thing at the end.
	spilledLeave spilledRole = iota
	spilledIndex
	spilledDoubled
	spilledColour
	// spilledDestinationAt is the address the last pixel was written to, which
	// is what a register left holding the slot's own contents ends up with.
	spilledDestinationAt
	// spilledDestination is that plus one pixel: the value written back to the
	// slot on the last iteration.
	spilledDestination
	spilledSource
	spilledCounter
)

// spilledBlit is a recognised palette blit with a spilled destination.
type spilledBlit struct {
	source  uint32 // register walking the source, one byte per pixel
	counter uint32 // register counting down to zero
	slot    uint32 // offset from SP of the frame slot holding the destination
	// record is the slot holding the pointer the palette is reached through,
	// and colours the offset of the palette inside what it points at.
	record  uint32
	colours uint32
	// guardBase and guardOffset are the address the guard re-reads. The loop
	// cannot move the register, and the run checks it cannot write the word.
	haveGuard   bool
	guardBase   uint32
	guardOffset uint32
	ends        [8]spilledRole
	steps       uint32
	after       uint32
	// branch is the loop's own closing branch, which is what a remembered
	// decline is keyed by. See Memory.declinedBranch.
	branch uint32
	// armTarget is where the guard sends the guest when the flag is set, and
	// storeAt where the fall-through's own store is. Both are read by
	// blended_blit.go, which follows that arm rather than refusing it.
	armTarget uint32
	storeAt   uint32
}

// runSpilledBlit stands in for the loop closed by a branch at branchPC back to
// head, and reports how many guest instructions it stood in for.
func (memory *Memory) runSpilledBlit(context *Context, loop *spilledBlit) (uint32, error) {
	// The counter has already been taken down for the iteration that reached
	// the branch, so what it holds is what is left to run.
	iterations := context.Registers[loop.counter]
	if iterations > maxStoreLoopIterations || iterations < 2 {
		return 0, nil
	}

	// The guard, read rather than assumed.
	//
	// A recogniser runs at the backward branch, and the comment above says the
	// branch is only reached by falling through the guard. That is true of the
	// guard's *own* exit, which the walk proves leaves the body — but not of
	// where the exit goes next. The form this shape was built against sends the
	// blending arm of the same blit to a block that draws one pixel through a
	// call and then branches back into the body *after* the draw, so the loop
	// closes with the flag set and the analysis, which reads only the
	// fall-through, is the analysis of the arm that did not run. Standing in
	// then blits the rest of the row unblended.
	//
	// Measured on the title this shape was built for, it is 71 stand-ins in a
	// route of 806 ticks: rare, because the flag is usually clear, and wrong
	// every time it is not. `clipped_blit.go` reads its flag for this reason;
	// this one now does the same.
	if loop.haveGuard {
		address := context.Registers[loop.guardBase] + loop.guardOffset
		flag, err := memory.readData32(address)
		if err != nil {
			return 0, nil
		}
		if flag != 0 {
			// The guest is leaving for the blending form of this blit, which
			// draws its pixel through a call. `blended_blit.go` reads that arm
			// and the writer behind it; what it cannot read is remembered with
			// the branch and the flag it declined on, or the whole chain is
			// walked again for every pixel of the run. See
			// Memory.declinedBranch.
			draw := memory.analyseBlendedSpilled(loop)
			if draw == nil {
				memory.declinedBranch, memory.declinedFlag = loop.branch, address
				memory.declinedValue, memory.haveDeclined = flag, true
				return 0, nil
			}
			return memory.runBlendedSpilled(context, loop, draw)
		}
	}

	// Everything this shape reads out of memory rather than out of a register.
	// A fault on any of them is one the next iteration would have taken, so
	// the loop goes back to the interpreter to take it there.
	slotAddress := context.Registers[RegisterSP] + loop.slot
	destination, err := memory.readData32(slotAddress)
	if err != nil {
		return 0, nil
	}
	recordAddress := context.Registers[RegisterSP] + loop.record
	record, err := memory.readData32(recordAddress)
	if err != nil {
		return 0, nil
	}
	tableAddress := record + loop.colours
	table, err := memory.readData32(tableAddress)
	if err != nil {
		return 0, nil
	}
	source := context.Registers[loop.source]

	// Both spans whole, before anything moves.
	if err := memory.validateLocked(source, uint64(iterations), PermissionRead, "read"); err != nil {
		return 0, nil
	}
	span := uint64(iterations) * 2
	if err := memory.validateLocked(destination, span, PermissionWrite, "write"); err != nil {
		return 0, nil
	}

	// The words above are read once and then treated as constants for the
	// whole run, which they are only as long as the run cannot write them.
	// These are control rather than data — the guard decides whether the loop
	// runs at all, the record decides where it reads its colours — so a blit
	// that lands on one is refused rather than reasoned about.
	overlaps := func(address uint32) bool {
		return uint64(address)+4 > uint64(destination) && uint64(address) < uint64(destination)+span
	}
	if overlaps(slotAddress) || overlaps(recordAddress) || overlaps(tableAddress) {
		return 0, nil
	}
	if loop.haveGuard && overlaps(context.Registers[loop.guardBase]+loop.guardOffset) {
		return 0, nil
	}

	// The table is read once per distinct index rather than once per pixel,
	// and both spans are reached through their pages where that is allowed.
	// See table_blit.go, which does the same for the same reasons.
	var colours [256]uint16
	var known [256]bool
	lastIndex := uint8(0)
	for pixel := uint32(0); pixel < iterations; {
		if destination&1 == 0 {
			from := memory.rawSpan(source+pixel, iterations-pixel, false)
			to := memory.rawSpan(destination+pixel*2, (iterations-pixel)*2, true)
			if from != nil && to != nil {
				count := uint32(len(from))
				if pixels := uint32(len(to)) / 2; pixels < count {
					count = pixels
				}
				for index := uint32(0); index < count; index++ {
					colour := from[index]
					if !known[colour] {
						value, err := memory.readData16(table + uint32(colour)*2)
						if err != nil {
							return 0, err
						}
						colours[colour], known[colour] = value, true
					}
					binary.LittleEndian.PutUint16(to[index*2:], colours[colour])
				}
				lastIndex = from[count-1]
				pixel += count
				continue
			}
		}
		index, err := memory.read8(source + pixel)
		if err != nil {
			return 0, err
		}
		if !known[index] {
			colour, err := memory.readData16(table + uint32(index)*2)
			if err != nil {
				return 0, err
			}
			colours[index], known[index] = colour, true
		}
		if err := memory.writeData16(destination+pixel*2, colours[index]); err != nil {
			return 0, err
		}
		lastIndex = index
		pixel++
	}

	// The slot is where the guest's own last iteration would have left it.
	if err := memory.writeData32(slotAddress, destination+iterations*2); err != nil {
		return 0, err
	}
	// And every register the body writes gets the value the last iteration
	// computed for it. A register the walk marked spilledLeave was either
	// never written or written with something the loop cannot change, and in
	// both cases it already holds it.
	for register, role := range loop.ends {
		switch role {
		case spilledIndex:
			context.Registers[register] = uint32(lastIndex)
		case spilledDoubled:
			context.Registers[register] = uint32(lastIndex) * 2
		case spilledColour:
			context.Registers[register] = uint32(colours[lastIndex])
		case spilledDestinationAt:
			context.Registers[register] = destination + (iterations-1)*2
		case spilledDestination:
			context.Registers[register] = destination + iterations*2
		case spilledSource:
			context.Registers[register] = source + iterations
		case spilledCounter:
			context.Registers[register] = 0
		}
	}
	// The flags of the `cmp rD, #0` that found the counter at zero: a
	// subtraction of nothing from nothing sets Z and never borrows.
	context.setNZCV(0, true, false)
	context.Registers[RegisterPC] = loop.after
	return iterations * loop.steps, nil
}

// analyseSpilledBlit reads the body between head and branchPC and answers the
// blit it is, or nil for one it cannot prove.
//
// The walk carries where each low register's value came from, the way
// `analyseStoreLoop` carries which of them hold the stack pointer: a register
// loaded out of a frame slot is the destination or the record depending on
// what the body does with it next, and the same register is often both in turn.
// Provenance is dropped the moment something writes the register, so a role
// that reads it has to be reading what it thinks it is.
func (memory *Memory) analyseSpilledBlit(head, branchPC uint32) *spilledBlit {
	if branchPC <= head || branchPC-head > maxSpilledBlitBytes {
		return nil
	}

	loop := &memory.spilledBlitScratch
	*loop = spilledBlit{after: branchPC + 2, branch: branchPC}
	var (
		written [8]int
		// frameWord says the register holds the word at SP+frameSlot, and
		// advanced that it has since had one pixel added to it.
		frameWord [8]bool
		frameSlot [8]uint32
		advanced  [8]bool
		// palette says the register holds the base the colours are read from.
		palette [8]bool
	)
	var (
		scratch        uint32
		haveIndex      bool
		haveShift      bool
		haveLookup     bool
		haveStore      bool
		haveAdvance    bool // the destination gained a pixel
		haveWriteBack  bool // and went back to its slot
		haveSourceStep bool
		haveCounter    bool
		havePalette    bool
		haveZeroTest   bool
		zeroTest       uint32
		zeroTestAt     uint32
		// endsSlot is which frame slot a register was last loaded from, kept
		// because what the loop leaves in it depends on which of the two slots
		// this shape has it is.
		endsSlot [8]uint32
		// The guard: its load, the compare that reads it, and the branch that
		// reads the compare, in that order and nowhere else.
		guardRegister uint32
		guardLoadedAt uint32
		guardTested   bool
		guardTestedAt uint32
		haveGuardExit bool
		storeSlot     uint32
	)
	// clear drops everything the walk knew about a register, which is what a
	// write to it means.
	forget := func(register uint32) {
		frameWord[register], advanced[register], palette[register] = false, false, false
	}

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
			load := instruction&(1<<11) != 0
			data := instruction >> 8 & 7
			offset := (instruction & 0xff) * 4
			if load {
				// The destination, or the record the palette hangs off. Which
				// one it is is decided by what the body reads it as.
				forget(data)
				written[data]++
				frameWord[data], frameSlot[data] = true, offset
				loop.ends[data], endsSlot[data] = spilledDestinationAt, offset
				break
			}
			// The destination going back to its slot, which is the last thing
			// that happens to it.
			if haveWriteBack || !frameWord[data] || !advanced[data] || frameSlot[data] != offset {
				return nil
			}
			haveWriteBack, loop.slot = true, offset

		case thumbImmediateTransfer:
			byteTransfer := instruction&(1<<12) != 0
			load := instruction&(1<<11) != 0
			offset := instruction >> 6 & 0x1f
			base := instruction >> 3 & 7
			data := instruction & 7
			if !load {
				return nil
			}
			if byteTransfer {
				// `ldrb rD, [rB]` — the source byte, at no offset because the
				// pointer is what walks.
				if offset != 0 || haveIndex || frameWord[base] {
					return nil
				}
				forget(data)
				written[data]++
				haveIndex, loop.source, scratch = true, base, data
				loop.ends[data] = spilledIndex
				break
			}
			// A word load is either the palette, reached through the record
			// the frame points at, or the guard's flag.
			if frameWord[base] && !advanced[base] {
				if havePalette {
					return nil
				}
				forget(data)
				written[data]++
				havePalette, palette[data] = true, true
				loop.record, loop.colours = frameSlot[base], offset*4
				// Reloaded from somewhere the loop cannot move, so it holds
				// the same thing at the end as it does now.
				loop.ends[data] = spilledLeave
				break
			}
			if loop.haveGuard {
				return nil
			}
			forget(data)
			written[data]++
			loop.haveGuard, loop.guardBase, loop.guardOffset = true, base, offset*4
			guardRegister, guardLoadedAt = data, address
			loop.ends[data] = spilledLeave

		case thumbShift:
			// `lsls rD, rS, #1` doubling the index into a halfword offset.
			if instruction>>11&3 != 0 || instruction>>6&0x1f != 1 {
				return nil
			}
			source := instruction >> 3 & 7
			data := instruction & 7
			if !haveIndex || haveShift || source != scratch || data != scratch {
				return nil
			}
			forget(data)
			written[data]++
			haveShift = true
			loop.ends[data] = spilledDoubled

		case thumbRegisterTransfer:
			// `ldrh rD, [rB, rO]` — the palette lookup, the one place this
			// shape reads with a register offset.
			if instruction>>9&7 != 5 || haveLookup || !haveShift {
				return nil
			}
			offsetRegister := instruction >> 6 & 7
			base := instruction >> 3 & 7
			data := instruction & 7
			// Either operand may be the palette and the other the doubled
			// index.
			switch {
			case palette[base] && offsetRegister == scratch:
			case palette[offsetRegister] && base == scratch:
			default:
				return nil
			}
			if data != scratch {
				return nil
			}
			forget(data)
			written[data]++
			haveLookup = true
			loop.ends[data] = spilledColour

		case thumbHalfwordTransfer:
			// `strh rD, [rB]` — the colour reaching the destination, through a
			// register still holding what the slot held. That it has not been
			// advanced yet is what puts the pixel where the guest put it.
			load := instruction&(1<<11) != 0
			offset := (instruction >> 6 & 0x1f) * 2
			base := instruction >> 3 & 7
			data := instruction & 7
			if load || offset != 0 || haveStore || !haveLookup || data != scratch {
				return nil
			}
			if !frameWord[base] || advanced[base] {
				return nil
			}
			haveStore, storeSlot, loop.storeAt = true, frameSlot[base], address

		case thumbImmediate:
			opcode := instruction >> 11 & 3
			register := instruction >> 8 & 7
			immediate := instruction & 0xff
			switch opcode {
			case 2: // ADD rD, #imm — the destination, or the source.
				switch {
				case immediate == 2 && frameWord[register] && !advanced[register]:
					if haveAdvance {
						return nil
					}
					haveAdvance, advanced[register] = true, true
					written[register]++
					loop.ends[register] = spilledDestination
				case immediate == 1 && haveIndex && register == loop.source:
					if haveSourceStep {
						return nil
					}
					haveSourceStep = true
					forget(register)
					written[register]++
					loop.ends[register] = spilledSource
				default:
					return nil
				}
			case 3: // SUB rD, #1 — the counter.
				if haveCounter || immediate != 1 {
					return nil
				}
				haveCounter, loop.counter = true, register
				forget(register)
				written[register]++
				loop.ends[register] = spilledCounter
			case 1: // CMP rD, #0 — the guard's test, or the loop's own.
				if immediate != 0 {
					return nil
				}
				if loop.haveGuard && register == guardRegister && !guardTested && !haveGuardExit &&
					guardLoadedAt+2 == address {
					guardTested, guardTestedAt = true, address
					break
				}
				if haveZeroTest {
					return nil
				}
				haveZeroTest, zeroTest, zeroTestAt = true, register, address
			default: // MOV is not part of this shape.
				return nil
			}

		case thumbConditionalBranch:
			// The guard leaving the loop. It reads the compare immediately
			// before it, and it goes somewhere outside the body — a branch
			// back inside would be a second loop this cannot reason about.
			if haveGuardExit || !guardTested || guardTestedAt+2 != address {
				return nil
			}
			condition := instruction >> 8 & 0xf
			if condition >= 0xe {
				return nil
			}
			offset := int32(int8(instruction&0xff)) * 2
			target := uint32(int64(address+4) + int64(offset))
			if target >= head && target <= branchPC {
				return nil
			}
			haveGuardExit, loop.armTarget = true, target

		default:
			return nil
		}
	}

	if !haveIndex || !haveShift || !haveLookup || !haveStore || !havePalette {
		return nil
	}
	if !haveAdvance || !haveWriteBack || !haveSourceStep || !haveCounter {
		return nil
	}
	// The pixel and the write-back have to be the same frame slot, or the loop
	// is walking one destination and writing another.
	if storeSlot != loop.slot || loop.record == loop.slot {
		return nil
	}
	// A guard is all three of its instructions or none of them.
	if loop.haveGuard != guardTested || loop.haveGuard != haveGuardExit {
		return nil
	}
	// Nothing the guard reads may be a register the body writes, which is what
	// makes the branch that reads it answer the same way every iteration. The
	// word it reads is checked against the destination at run time.
	if loop.haveGuard && written[loop.guardBase] != 0 {
		return nil
	}
	// The loop's own test has to be the last instruction before the branch, on
	// the counter, and the branch has to be the one that reads it.
	if !haveZeroTest || zeroTest != loop.counter || zeroTestAt != branchPC-2 {
		return nil
	}
	if !memory.branchIsNotEqual(branchPC) {
		return nil
	}
	// One role each for the registers that walk. The scratch register carries
	// the index, the doubling and the colour in turn, which is one chain and
	// is checked by position above.
	if loop.source == loop.counter || loop.source == scratch || loop.counter == scratch {
		return nil
	}
	if written[loop.source] != 1 || written[loop.counter] != 1 {
		return nil
	}
	// A register still holding what a frame slot held is left with the
	// destination the last pixel went to — but only if it is the destination's
	// slot. The record's slot holds the same pointer every iteration, so a
	// register left holding that is left alone; and a third slot is a value
	// this shape cannot account for, which refuses the body rather than
	// guessing at it.
	for register := range loop.ends {
		if loop.ends[register] != spilledDestinationAt {
			continue
		}
		switch endsSlot[register] {
		case loop.slot:
		case loop.record:
			loop.ends[register] = spilledLeave
		default:
			return nil
		}
	}

	loop.steps = (branchPC-head)/2 + 1
	return loop
}
