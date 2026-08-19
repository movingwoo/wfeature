package armcore

import "encoding/binary"

// Recognising a counted store loop, and standing in for it.
//
// The library hooks answer `memcpy` and `memset` natively because interpreting
// a byte-at-a-time fill is the worst thing this emulator can be asked to do.
// A game's own rasteriser is the same shape with no entry point to hook: it is
// a loop the compiler emitted inline, and `armcore.md` measured that the
// corpus is full of them — 34 fill-shaped loops against 8 copy-shaped ones —
// and that they cannot be matched as byte patterns, because two titles doing
// the same fill share no instruction sequence. What is needed is a recogniser
// that reads the loop's structure instead, and this is that.
//
// It answers one shape: a counted store loop. A body that stores one value
// through a pointer, advances that pointer by the width of the store, counts a
// register down by one and branches back while the count has not run out. That
// is 43.6% of one title's guest instructions in a scene a person reported as
// slow, and the two halfword fills `armcore.md` profiled in other titles are
// the same shape.
//
// **The bar for standing in for guest code is that the guest cannot tell.** So
// the recogniser refuses everything it has not proved:
//
//   - every instruction in the body has to be one of the forms below, and each
//     one's role has to be the only role it plays. A body with a second store,
//     a call, a second counter, or a register written twice is refused.
//   - the value stored has to be the same on every iteration. It may be held
//     in a register, or reloaded each time from an address the loop does not
//     write and does not move.
//   - the destination span has to be writable in full, checked before a single
//     byte of it is written, so a loop that would have faulted partway still
//     faults there rather than after this has run to completion.
//   - the flags the branch reads have to be the ones the counter set.
//
// Recognition is attempted only when a backward branch is taken, which costs a
// compare on every other instruction in the program.
//
// **Only refusals are cached.** A loop this can stand in for is analysed again
// every time it is stood in for, which is once per execution of the loop and
// not once per iteration — six decode-cache reads against the thousands of
// stores they authorise. That is deliberately the expensive choice, because
// the alternative is a cache of positive answers that has to be invalidated
// whenever the guest rewrites code, and this platform really does rewrite it.
// A stale refusal only costs a missed stand-in, never a wrong one.
//
// The step count is charged as if every instruction had run. A guest that
// would have exhausted its budget still does, and `MaxSteps` keeps meaning
// what it meant.

// maxStoreLoopBytes bounds how far back a branch may reach and still be
// considered. A fill loop is a handful of instructions; anything longer is a
// different kind of loop and analysing it would cost more than it saves.
const maxStoreLoopBytes = 32

// maxTableBlitBytes bounds a blit body, which is longer than a fill's.
const maxTableBlitBytes = 48

// maxRecognisedLoopBytes is the longest body any of the recognisers reads, and
// so how far back a taken branch may reach before the engine stops asking. The
// cost of raising it is one analysis per loop head that is not one of these
// shapes, because a refusal is remembered.
const maxRecognisedLoopBytes = wordModulateBytes

// maxStoreLoopIterations bounds one stand-in. It is far past any real fill —
// a whole 240x320 screen is 76,800 halfwords — and exists so that a counter
// the recogniser has misread cannot become an unbounded write.
const maxStoreLoopIterations = 1 << 22

// storeLoop is a recognised counted store loop.
type storeLoop struct {
	// pointer is the register holding the destination, counter the register
	// counting down, and width the size of one store in bytes.
	pointer uint32
	counter uint32
	width   uint32
	// value is the register holding what is stored.
	value uint32
	// reload says the value is read from memory each iteration rather than
	// held in a register. The address is the stack pointer plus offset, which
	// is the form a compiler emits for a colour the caller left in a frame
	// slot — and the only one recognised, because the stack pointer is the one
	// base a body of this shape cannot move.
	reload bool
	offset uint32
	// steps is how many guest instructions one iteration is, including the
	// branch, so a stand-in can charge what it stood in for.
	steps uint32
	// after is the address execution continues at when the loop ends.
	after uint32
}

// storeLoopAt answers the analysis of the loop closed by a branch at
// branchPC back to head, running it if it is one this can stand in for. It
// reports how many guest instructions it stood in for, and whether it did.
func (memory *Memory) runStoreLoop(context *Context, head, branchPC uint32) (uint32, error) {
	if memory.refusedLoops[head] {
		return 0, nil
	}
	loop := memory.analyseStoreLoop(head, branchPC)
	if loop == nil {
		// The other shapes worth standing in for, each in a file of its own:
		// a blit through a lookup table, a guarded byte blend, a modulate of
		// two packed streams.
		if blit := memory.analyseTableBlit(head, branchPC); blit != nil {
			return memory.runTableBlit(context, blit)
		}
		if blend := memory.analyseByteBlend(head, branchPC); blend != nil {
			return memory.runByteBlend(context, blend)
		}
		modulate := memory.analyseWordModulate(head, branchPC)
		if modulate == nil {
			// Refused by analysis, which is the only answer that cannot
			// change while the code does not. A run that merely declines —
			// too few iterations to be worth it, a span it could not
			// validate — must not be remembered, or one unlucky first
			// encounter would write the loop off for the life of the session.
			if memory.refusedLoops == nil {
				memory.refusedLoops = map[uint32]bool{}
			}
			memory.refusedLoops[head] = true
			return 0, nil
		}
		return memory.runWordModulate(context, modulate)
	}

	iterations := context.Registers[loop.counter] + 1
	if iterations > maxStoreLoopIterations {
		return 0, nil
	}
	// One iteration is what the interpreter would do anyway, and standing in
	// for it would only add the checks below to it.
	if iterations < 2 {
		return 0, nil
	}

	value := context.Registers[loop.value]
	if loop.reload {
		// The address is loop-invariant, so it is read once. A fault here is
		// the fault the first iteration would have taken.
		address := context.Registers[RegisterSP] + loop.offset
		loaded, err := memory.readData16(address)
		if err != nil {
			return 0, err
		}
		value = uint32(loaded)
	}

	start := context.Registers[loop.pointer]
	span := iterations * loop.width
	// Checked whole, before anything is written: a loop that would have
	// faulted partway has to fault where it would have, not after this has
	// filled everything before it.
	if err := memory.validateLocked(start, uint64(span), PermissionWrite, "write"); err != nil {
		return 0, nil
	}

	// The span is reached through its pages where that is allowed, a page at a
	// time, so what the whole span already proved is not proved again per
	// store. See raw_span.go. An odd destination is not: a halfword store
	// aligns its address downward, so consecutive stores would land on top of
	// each other and the checked path is what does that.
	for offset := uint32(0); offset < span; {
		if start&1 == 0 {
			if to := memory.rawSpan(start+offset, span-offset, true); to != nil {
				for at := 0; at+1 < len(to); at += 2 {
					binary.LittleEndian.PutUint16(to[at:], uint16(value))
				}
				offset += uint32(len(to))
				continue
			}
		}
		if err := memory.writeData16(start+offset, uint16(value)); err != nil {
			return 0, err
		}
		offset += loop.width
	}

	// The registers are left where the guest would have left them: the pointer
	// past the last store, the counter one below zero — which is what the
	// borrow the branch read means — and the flags saying the count ran out.
	context.Registers[loop.pointer] = start + span
	context.Registers[loop.counter] = ^uint32(0)
	context.setNZCV(^uint32(0), false, false)
	context.Registers[RegisterPC] = loop.after
	return iterations * loop.steps, nil
}

// analyseStoreLoop reads the body between head and branchPC and answers the
// loop it is, or nil for one this cannot prove.
//
// It is a walk rather than a match: each instruction contributes one effect,
// and a body is recognised only when the effects add up to exactly a counted
// store and nothing else. Every unhandled form refuses the loop, which is what
// keeps a new one from being silently mistaken for a known one.
func (memory *Memory) analyseStoreLoop(head, branchPC uint32) *storeLoop {
	if branchPC <= head || branchPC-head > maxStoreLoopBytes {
		return nil
	}

	loop := &storeLoop{after: branchPC + 2}
	// written counts the body's assignments to each low register, and holdsSP
	// tracks which of them currently hold the stack pointer. A body of this
	// shape reaches a frame slot by copying the stack pointer into a low
	// register first, so knowing which registers hold it is what says an
	// address is one the loop cannot move.
	// written counts assignments that change a register's value in the sense
	// the loop cares about. Copying the stack pointer in is counted apart, in
	// spCopy: it is how an address is prepared rather than a value being
	// changed, and the register it lands in is read straight back.
	var written [8]int
	var spCopy [8]bool
	var holdsSP [8]bool
	var (
		haveStore   bool
		haveAdvance bool
		haveCounter bool
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
		case thumbHighRegister:
			// `mov rD, sp` into a low register, which is how a frame slot
			// becomes reachable. Any other high-register operation — an add,
			// a branch-exchange — is not something this can reason about.
			if instruction>>8&0xff != 0x46 {
				return nil
			}
			destination := instruction&7 | instruction>>4&8
			source := instruction >> 3 & 15
			if destination > 7 || source != RegisterSP {
				return nil
			}
			spCopy[destination] = true
			holdsSP[destination] = true

		case thumbHalfwordTransfer:
			load := instruction&(1<<11) != 0
			offset := (instruction >> 6 & 0x1f) * 2
			base := instruction >> 3 & 7
			data := instruction & 7
			if load {
				// The value the loop stores. Its address has to be one the
				// body cannot move, which here means the stack pointer.
				if loop.reload || !holdsSP[base] {
					return nil
				}
				loop.reload, loop.offset, loop.value = true, offset, data
				written[data]++
				holdsSP[data] = false
				break
			}
			// The store. One per body, at no offset from the pointer, because
			// the pointer is what advances.
			if haveStore || offset != 0 || holdsSP[base] {
				return nil
			}
			haveStore, loop.pointer, loop.width = true, base, 2
			if !loop.reload {
				loop.value = data
			} else if loop.value != data {
				return nil
			}

		case thumbImmediate:
			opcode := instruction >> 11 & 3
			register := instruction >> 8 & 7
			immediate := instruction & 0xff
			switch opcode {
			case 2: // ADD rD, #imm — the pointer advancing.
				if haveAdvance || immediate != 2 {
					return nil
				}
				haveAdvance = true
				loop.pointer = register
				written[register]++
				holdsSP[register] = false
			case 3: // SUB rD, #imm — the counter.
				if haveCounter || immediate != 1 {
					return nil
				}
				haveCounter = true
				loop.counter = register
				written[register]++
				holdsSP[register] = false
			default: // MOV and CMP are not part of this shape.
				return nil
			}

		default:
			return nil
		}
	}

	if !haveStore || !haveAdvance || !haveCounter {
		return nil
	}
	// The counter's subtraction has to be the last thing before the branch, or
	// the flags the branch reads are not the ones it set.
	if !memory.subtractIsLast(branchPC, loop.counter) {
		return nil
	}
	// The branch has to continue while the count has not run out. `bhs` reads
	// the carry the subtraction leaves set until it borrows.
	if !memory.branchIsUnsignedHigherOrSame(branchPC) {
		return nil
	}
	// One role each: the register the store walks is the one the add advances,
	// and neither it nor the counter is anything else.
	if loop.pointer == loop.counter || written[loop.pointer] != 1 || written[loop.counter] != 1 {
		return nil
	}
	// Neither the pointer nor the counter may be the register an address was
	// prepared in: a body that copies the stack pointer over either has moved
	// something this analysis assumed stood still.
	if spCopy[loop.pointer] || spCopy[loop.counter] {
		return nil
	}
	if loop.value == loop.pointer || loop.value == loop.counter {
		return nil
	}
	if loop.reload {
		if written[loop.value] != 1 {
			return nil
		}
	} else if written[loop.value] != 0 {
		return nil
	}

	loop.steps = (branchPC-head)/2 + 1
	return loop
}

// subtractIsLast reports whether the instruction before the branch is the
// counter's own subtraction, which is what makes the flags the branch reads
// the ones the count set.
func (memory *Memory) subtractIsLast(branchPC, counter uint32) bool {
	decoded, cached := memory.decodedThumbFast(branchPC - 2)
	if !cached {
		var err error
		if decoded, err = memory.decodeThumb(branchPC - 2); err != nil {
			return false
		}
	}
	instruction := uint32(decoded.instruction)
	return decoded.form == thumbImmediate &&
		instruction>>11&3 == 3 &&
		instruction>>8&7 == counter
}

// branchIsUnsignedHigherOrSame reports whether the branch closing the loop is
// the one that continues while the counter's subtraction has not borrowed.
func (memory *Memory) branchIsUnsignedHigherOrSame(branchPC uint32) bool {
	decoded, cached := memory.decodedThumbFast(branchPC)
	if !cached {
		var err error
		if decoded, err = memory.decodeThumb(branchPC); err != nil {
			return false
		}
	}
	// Condition 2 is HS/CS.
	return decoded.form == thumbConditionalBranch && uint32(decoded.instruction)>>8&0xf == 2
}
