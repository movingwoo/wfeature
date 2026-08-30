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
// **The shape is one loop written several ways**, and the ways are the
// compiler's rather than the program's. A corpus sweep of the loops this hook
// refuses found the same fill in three more titles at 19%, 29% and 69% of
// everything they execute, each refused over a detail that changes nothing
// about what the loop does — see `armcore.md`, "The same fill, written four
// ways". So the analyser reads the parts that vary rather than a fixed
// sequence:
//
//   - **how it ends.** `subs rD, #1` read by `bhs`, or `cmp rD, #0` read by
//     `bne`. They differ in how many iterations are left to run and in where
//     the counter and the flags end up, which `loopTermination` carries.
//   - **how it addresses.** `[rB]` with rB advancing, or `[rB, rO]` with rB
//     advancing and rO an index the body never moves.
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
//
// A refusal is remembered **in the branch's own decode-cache entry**, in the
// padding byte the form leaves behind, rather than in a map keyed by the loop
// head. That matters because of what the hook costs where it never fires: a
// title sitting on its own menus takes a backward branch every seventeen
// instructions, so the answer is read tens of millions of times a minute for
// code no recogniser will ever help — the whole hook costs 2 to 6% of such a
// run. Reading the answer out of a word the decode had already loaded returns
// about half of that on some titles and nothing on others; see `armcore.md`,
// "What the hook costs where it never fires". The reason it is kept either way
// is that riding on the decode entry drops the answer the moment the page's
// instructions change, where a map keyed by the loop head never did — so a
// rewritten loop is analysed again rather than inheriting the old code's
// refusal.
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

// A loop of this shape ends in one of two ways, and which one it is decides
// how many iterations are left to run and where the counter and the flags are
// left. Both are the same loop; they differ only in how the compiler chose to
// ask whether the count has run out.
type loopTermination uint8

const (
	// terminateBorrow is `subs rD, #1` read by `bhs`: the loop continues while
	// the subtraction has not borrowed, so it runs one more time than the
	// counter's value and leaves the counter at -1.
	terminateBorrow loopTermination = iota
	// terminateNotZero is `cmp rD, #0` read by `bne`. It runs exactly the
	// counter's value of times and leaves the counter at zero. It is the same
	// fill: three titles in the local corpus spend 19%, 29% and 69% of their
	// instructions in a loop that differs from the one above in nothing else.
	terminateNotZero
)

// storeLoop is a recognised counted store loop.
type storeLoop struct {
	// pointer is the register holding the destination, counter the register
	// counting down, and width the size of one store in bytes.
	pointer uint32
	counter uint32
	width   uint32
	// value is the register holding what is stored.
	value uint32
	// index is a second register added to the pointer to reach the
	// destination, for the form that stores through `[rB, rO]` and advances
	// rB. It is the address the guest computes, not a register the loop
	// touches: indexed says whether there is one, and the body has to leave it
	// alone for the whole loop.
	indexed bool
	index   uint32
	// reload says the value is read from memory each iteration rather than
	// held in a register. The address is the stack pointer plus offset, which
	// is the form a compiler emits for a colour the caller left in a frame
	// slot — and the only one recognised, because the stack pointer is the one
	// base a body of this shape cannot move.
	reload bool
	offset uint32
	// terminate is which of the two endings the body proved.
	terminate loopTermination
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
	if memory.standInsRefused {
		return 0, nil
	}
	loop := memory.analyseStoreLoop(head, branchPC)
	if loop == nil {
		// The other shapes worth standing in for, each in a file of its own:
		// a blit through a lookup table, a guarded byte blend, the same blit
		// with its destination in a frame slot, a modulate of two packed
		// streams.
		if blit := memory.analyseTableBlit(head, branchPC); blit != nil {
			return memory.runTableBlit(context, blit)
		}
		if blend := memory.analyseByteBlend(head, branchPC); blend != nil {
			return memory.runByteBlend(context, blend)
		}
		if spilled := memory.analyseSpilledBlit(head, branchPC); spilled != nil {
			return memory.runSpilledBlit(context, spilled)
		}
		modulate := memory.analyseWordModulate(head, branchPC)
		if modulate == nil {
			// Refused by analysis, which is the only answer that cannot
			// change while the code does not. A run that merely declines —
			// too few iterations to be worth it, a span it could not
			// validate — must not be remembered, or one unlucky first
			// encounter would write the loop off for the life of the session.
			memory.markLoopRefused(branchPC)
			return 0, nil
		}
		return memory.runWordModulate(context, modulate)
	}

	iterations := context.Registers[loop.counter]
	if loop.terminate == terminateBorrow {
		// The borrow form runs once more than the counter says, because the
		// iteration that takes the count to -1 has already stored.
		iterations++
	}
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

	// The destination is where the guest's own store would have gone, which
	// for the indexed form is the base plus the index it never moves.
	start := context.Registers[loop.pointer]
	if loop.indexed {
		start += context.Registers[loop.index]
	}
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
	// past the last store, and the counter and the flags exactly as the last
	// test the branch read would have set them.
	context.Registers[loop.pointer] += span
	if loop.terminate == terminateBorrow {
		// One below zero, which is what the borrow the branch read means.
		context.Registers[loop.counter] = ^uint32(0)
		context.setNZCV(^uint32(0), false, false)
	} else {
		// Zero, and the flags of the `cmp rD, #0` that found it there: a
		// subtraction of nothing from nothing sets Z and never borrows.
		context.Registers[loop.counter] = 0
		context.setNZCV(0, true, false)
	}
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

	loop := &memory.storeLoopScratch
	*loop = storeLoop{after: branchPC + 2}
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
		// zeroTest is the `cmp rD, #0` of the second ending, and zeroTestAt
		// where it was, because it only proves anything as the last
		// instruction before the branch.
		haveZeroTest bool
		zeroTest     uint32
		zeroTestAt   uint32
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

		case thumbRegisterTransfer:
			// `strh rD, [rB, rO]` — the same store reaching its destination
			// through a base and an index instead of a base alone. Only the
			// halfword store is this shape; every other operation the form
			// encodes, including every load, is refused.
			if instruction>>9&7 != 1 {
				return nil
			}
			offsetRegister := instruction >> 6 & 7
			base := instruction >> 3 & 7
			data := instruction & 7
			if haveStore || holdsSP[base] || holdsSP[offsetRegister] {
				return nil
			}
			haveStore, loop.pointer, loop.width = true, base, 2
			loop.indexed, loop.index = true, offsetRegister
			if !loop.reload {
				loop.value = data
			} else if loop.value != data {
				return nil
			}

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
			case 1: // CMP rD, #0 — the second ending's test. It writes no
				// register, so it changes nothing the walk is tracking; what
				// it has to prove is checked below, where its position and
				// the branch that reads it are both known.
				if haveZeroTest || immediate != 0 {
					return nil
				}
				haveZeroTest, zeroTest, zeroTestAt = true, register, address
			default: // MOV is not part of this shape.
				return nil
			}

		default:
			return nil
		}
	}

	if !haveStore || !haveAdvance || !haveCounter {
		return nil
	}
	// Whichever ending this is, the test the branch reads has to be the last
	// instruction before it, on the counter, and the branch has to be the one
	// that reads that test. A body carrying a zero test is only ever read the
	// second way: `cmp rD, #0` leaves the carry set whatever the count is, so
	// a `bhs` after one would never stop.
	if haveZeroTest {
		if zeroTest != loop.counter || zeroTestAt != branchPC-2 {
			return nil
		}
		if !memory.branchIsNotEqual(branchPC) {
			return nil
		}
		loop.terminate = terminateNotZero
	} else {
		// The counter's subtraction has to be the last thing before the
		// branch, or the flags the branch reads are not the ones it set.
		if !memory.subtractIsLast(branchPC, loop.counter) {
			return nil
		}
		// The branch has to continue while the count has not run out. `bhs`
		// reads the carry the subtraction leaves set until it borrows.
		if !memory.branchIsUnsignedHigherOrSame(branchPC) {
			return nil
		}
		loop.terminate = terminateBorrow
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
	// The index is an address the loop reads and never moves. A body that
	// writes it, or that reached it through the stack pointer, has moved
	// something this analysis assumed stood still — and it cannot be the
	// pointer it is added to either.
	if loop.indexed {
		if loop.index == loop.pointer || written[loop.index] != 0 || spCopy[loop.index] {
			return nil
		}
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
