package armcore

import "encoding/binary"

// Recognising a table-lookup blit, and standing in for it.
//
// The counted fill in fill_loop.go is the simple half of what a game's
// rasteriser spends its time in. The other half, and on the title this was
// measured against much the larger one, is a *blit through a palette*: a byte
// of source is an index, the index picks a 16-bit colour out of a table, and
// the colour goes to a destination that walks forward two bytes at a time.
// Twelve guest instructions per pixel, and 28.6% of every instruction one
// local title executed in a scene a person reported as slow.
//
// It is not a fill and it is not a copy — the value written is neither
// constant nor the value read — so neither of the library hooks can serve it.
// What it is is regular: source forward by one, destination forward by two,
// one lookup, one store, a counter, and nothing else. That is enough to stand
// in for, on exactly the terms fill_loop.go sets out: the recogniser refuses
// everything it has not proved, only refusals are cached, and the step count
// is charged as though every instruction had run.
//
// The extra care this one needs is that it reads three places rather than one.
// The source span and the destination span are both validated whole before a
// byte moves, so a blit that would have faulted partway still faults where it
// would have. The palette is not: its entries are picked by data and a bounded
// read of each is what the guest would have done anyway.

// tableBlit is a recognised table-lookup blit.
type tableBlit struct {
	source      uint32 // register walking the source, one byte per pixel
	destination uint32 // register walking the destination, two bytes per pixel
	table       uint32 // register holding the base of the lookup table
	counter     uint32 // register counting down, sixteen bits wide
	// limit is the register the counter is compared against, and the loop ends
	// when they are equal.
	limit uint32
	steps uint32
	after uint32
}

// runTableBlit stands in for the loop closed by a branch at branchPC back to
// head, and reports how many guest instructions it stood in for.
func (memory *Memory) runTableBlit(context *Context, loop *tableBlit) (uint32, error) {
	// The counter is sixteen bits and the loop ends when it reaches the limit,
	// so the distance between them is how many pixels are left. A counter that
	// already equals the limit wraps the whole way round, which is what the
	// guest would do too.
	iterations := (context.Registers[loop.counter] - context.Registers[loop.limit]) & 0xffff
	if iterations == 0 {
		iterations = 0x10000
	}
	if iterations > maxStoreLoopIterations || iterations < 2 {
		return 0, nil
	}

	source := context.Registers[loop.source]
	destination := context.Registers[loop.destination]
	table := context.Registers[loop.table]

	// Both spans whole, before anything moves.
	if err := memory.validateLocked(source, uint64(iterations), PermissionRead, "read"); err != nil {
		return 0, nil
	}
	if err := memory.validateLocked(destination, uint64(iterations)*2, PermissionWrite, "write"); err != nil {
		return 0, nil
	}

	// The table is read once per distinct index rather than once per pixel.
	// A sprite row names far fewer colours than it has pixels — that is what a
	// palette is for — and the entries cannot move underneath the loop, which
	// walks a source and a destination that are neither the table nor written
	// through it. Filling all 256 up front would be a loss on a short row, so
	// it is remembered as it is asked for.
	var colours [256]uint16
	var known [256]bool
	// Both spans are reached through their pages where that is allowed, a page
	// of destination at a time, so the bounds and the permission the whole
	// span already proved are not proved again per pixel. See raw_span.go.
	// An odd destination is not: a halfword store aligns its address downward,
	// so the guest would be writing over its own previous pixel and the
	// checked path is what does that.
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
				pixel += count
				continue
			}
		}
		// Whatever the direct route refused — a watched address, a page with
		// no storage yet — one pixel at a time, which also commits the page
		// the next span may then be able to reach directly.
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
		pixel++
	}

	context.Registers[loop.source] = source + iterations
	context.Registers[loop.destination] = destination + iterations*2
	context.Registers[loop.counter] = context.Registers[loop.limit]
	// The comparison the branch read found them equal, which is what ends the
	// loop: zero, with no borrow.
	context.setNZCV(0, true, false)
	context.Registers[RegisterPC] = loop.after
	return iterations * loop.steps, nil
}

// analyseTableBlit reads the body between head and branchPC and answers the
// blit it is, or nil for one it cannot prove.
//
// The walk assigns each instruction a role and refuses a body where any role
// is filled twice or left empty. A scratch register is allowed — the shape
// needs one to carry the index and then the colour — but only one, and only
// where the analysis can see what it holds.
func (memory *Memory) analyseTableBlit(head, branchPC uint32) *tableBlit {
	if branchPC <= head || branchPC-head > maxTableBlitBytes {
		return nil
	}

	loop := &tableBlit{after: branchPC + 2}
	var (
		scratch       uint32
		haveIndex     bool // the source byte was read
		haveShift     bool // the index was doubled into a table offset
		haveLookup    bool // the colour was read from the table
		haveStore     bool // the colour was written
		haveSource    bool // the source pointer advanced by one
		haveDest      bool // the destination pointer advanced by two
		haveDecrement bool
		haveMask      bool // the counter was narrowed to sixteen bits
		haveLimit     bool // the limit was loaded
		haveCompare   bool
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
		case thumbImmediateTransfer:
			// `ldrb rD, [rB]` — the source byte, at no offset because the
			// pointer is what walks.
			byteTransfer := instruction&(1<<12) != 0
			load := instruction&(1<<11) != 0
			offset := instruction >> 6 & 0x1f
			base := instruction >> 3 & 7
			data := instruction & 7
			if !byteTransfer || !load || offset != 0 || haveIndex {
				return nil
			}
			haveIndex, loop.source, scratch = true, base, data

		case thumbShift:
			// `lsls rD, rS, #1` doubling the index into a halfword offset, and
			// the `lsls`/`lsrs` pair that narrows the counter to sixteen bits.
			opcode := instruction >> 11 & 3
			amount := instruction >> 6 & 0x1f
			source := instruction >> 3 & 7
			data := instruction & 7
			switch {
			case opcode == 0 && amount == 1:
				if !haveIndex || haveShift || source != scratch || data != scratch {
					return nil
				}
				haveShift = true
			case opcode == 0 && amount == 16:
				if source != scratch || data != scratch {
					return nil
				}
			case opcode == 1 && amount == 16:
				// The result of the narrowing is the counter itself.
				if haveMask || source != scratch {
					return nil
				}
				haveMask, loop.counter = true, data
			default:
				return nil
			}

		case thumbRegisterTransfer:
			// `ldrh rD, [rB, rO]` — the palette lookup, the one place this
			// shape reads with a register offset.
			opcode := instruction >> 9 & 7
			offsetRegister := instruction >> 6 & 7
			base := instruction >> 3 & 7
			data := instruction & 7
			if opcode != 5 || haveLookup || !haveShift {
				return nil
			}
			// Either operand may be the table and the other the doubled index.
			switch scratch {
			case base:
				loop.table = offsetRegister
			case offsetRegister:
				loop.table = base
			default:
				return nil
			}
			if data != scratch {
				return nil
			}
			haveLookup = true

		case thumbHalfwordTransfer:
			// `strh rD, [rB]` — the colour reaching the destination.
			load := instruction&(1<<11) != 0
			offset := (instruction >> 6 & 0x1f) * 2
			base := instruction >> 3 & 7
			data := instruction & 7
			if load || offset != 0 || haveStore || !haveLookup || data != scratch {
				return nil
			}
			haveStore, loop.destination = true, base

		case thumbAddSubtract:
			// `subs rD, rS, #1` — the counter, before it is narrowed.
			immediate := instruction&(1<<10) != 0
			subtract := instruction&(1<<9) != 0
			operand := instruction >> 6 & 7
			source := instruction >> 3 & 7
			data := instruction & 7
			if !immediate || !subtract || operand != 1 || haveDecrement || data != scratch {
				return nil
			}
			haveDecrement, loop.counter = true, source

		case thumbImmediate:
			// The two pointers advancing.
			opcode := instruction >> 11 & 3
			register := instruction >> 8 & 7
			step := instruction & 0xff
			if opcode != 2 {
				return nil
			}
			switch {
			case step == 1 && !haveSource:
				if register != loop.source {
					return nil
				}
				haveSource = true
			case step == 2 && !haveDest:
				if register != loop.destination {
					return nil
				}
				haveDest = true
			default:
				return nil
			}

		case thumbLiteralLoad:
			// `ldr rD, [pc, #imm]` — the value the counter is compared
			// against. It is read from the loop's own code, which does not
			// change, so it is loop-invariant.
			if haveLimit {
				return nil
			}
			haveLimit, loop.limit = true, instruction>>8&7

		case thumbALU:
			// `cmp rA, rB` — the only ALU operation this shape performs.
			if instruction>>6&0xf != 0xa || haveCompare {
				return nil
			}
			left := instruction & 7
			right := instruction >> 3 & 7
			if left != loop.counter || right != loop.limit {
				return nil
			}
			haveCompare = true

		default:
			return nil
		}
	}

	if !haveIndex || !haveShift || !haveLookup || !haveStore ||
		!haveSource || !haveDest || !haveDecrement || !haveMask || !haveLimit || !haveCompare {
		return nil
	}
	// The branch has to continue while the counter has not reached the limit.
	if !memory.branchIsNotEqual(branchPC) {
		return nil
	}
	// The four registers the loop walks each need a register of their own, and
	// none of them may be the scratch one — it is overwritten every pixel.
	// A bit per low register rather than a set: this runs once per stand-in,
	// which is often enough that allocating a map for five entries showed up
	// in a profile of the run it was meant to speed up.
	var seen uint32
	for _, register := range [...]uint32{loop.source, loop.destination, loop.table, loop.counter, scratch} {
		if seen&(1<<register) != 0 {
			return nil
		}
		seen |= 1 << register
	}
	// The limit may share the scratch register, and in the title this was read
	// from it does: r3 carries the index, then the colour, then the limit, and
	// the three never overlap in time because each is dead before the next is
	// written. What it may not be is one of the four the loop walks, and a
	// limit that is the counter would compare a register against itself.
	if loop.limit != scratch && seen&(1<<loop.limit) != 0 {
		return nil
	}

	loop.steps = (branchPC-head)/2 + 1
	return loop
}

// branchIsNotEqual reports whether the branch closing the loop is the one that
// continues while the comparison found the two unequal.
func (memory *Memory) branchIsNotEqual(branchPC uint32) bool {
	decoded, cached := memory.decodedThumbFast(branchPC)
	if !cached {
		var err error
		if decoded, err = memory.decodeThumb(branchPC); err != nil {
			return false
		}
	}
	// Condition 1 is NE.
	return decoded.form == thumbConditionalBranch && uint32(decoded.instruction)>>8&0xf == 1
}
