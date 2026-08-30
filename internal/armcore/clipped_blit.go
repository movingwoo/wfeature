package armcore

import "encoding/binary"

// Recognising the same blit with a clip test per pixel, and standing in for it.
//
// `spilled_blit.go` reads the run a sprite draws when all of it is on screen.
// The run that is not takes a longer body, and it is the second-busiest loop in
// the title that one was built for — 95 million closings against that one's 149
// million. The difference is three tests at the top of the body rather than one:
//
//	ldr rL, [sp, #a]  ; cmp rX, rL ; blt tail   — off the left of the clip
//	ldr rH, [sp, #b]  ; cmp rH, rX ; ble tail   — off the right of it
//	ldr rR, [sp, #c]  ; ldr rF, [rR, #d] ; cmp rF, #0 ; bne blend
//
// where rX walks one pixel a time across the row. So the body has three exits,
// two of which rejoin it: a pixel outside the clip skips the store and still
// advances the source and the index, and the destination only advances where a
// pixel was written.
//
// **The clip is not an invariant, and that is the whole difference.** The guard
// in the file above reads only things the loop cannot move, so proving it falls
// through once proves it for the rest of the run. Here two of the three tests
// read the index as well, and the index is what the loop advances. What makes
// them tractable instead is that the index moves one way and by one: the pixels
// the tests let through are a single contiguous stretch of the run, and its
// bounds are arithmetic rather than a search. A run is at most three phases —
// skipped, drawn, skipped — and the middle one is the blit the other file
// already stands in for.
//
// The flag is an invariant of the kind that file describes, and it is handled
// the same way: the loop cannot move the record it is read through, so if it is
// set the guest is about to leave for the blending form of the same blit, and
// the stand-in refuses rather than reasoning about where it would land.
//
// **What each path costs is not one number**, which is new here. The step count
// has to be what the guest would have retired or a run that should have hit its
// budget does not, so the three phases are charged separately: a pixel off the
// left runs the first test and the tail, one off the right runs two tests and
// the tail, and a drawn pixel runs the whole body.

// maxClippedBlitBytes bounds this body, which is the longest of the blits: the
// three tests are ten instructions before the draw begins. The one measured is
// fifty bytes.
const maxClippedBlitBytes = 56

// clippedBlit is a recognised palette blit with a clip test per pixel.
type clippedBlit struct {
	// The draw, which is the body spilled_blit.go reads without its counter.
	source  uint32
	slot    uint32
	record  uint32
	colours uint32
	// The two bounds and the flag, each out of a frame slot the loop cannot
	// move, and the registers they are read into.
	lowSlot      uint32
	highSlot     uint32
	flagSlot     uint32
	flagOffset   uint32
	lowIn        uint32
	highIn       uint32
	flagRecordIn uint32
	flagIn       uint32
	recordIn     uint32
	tableIn      uint32
	scratch      uint32
	destIn       uint32
	// index is the register walking across the row, which this shape keeps in
	// a high register.
	index uint32
	// The tail every iteration runs, whether it drew or skipped.
	one   uint32
	count uint32
	limit uint32
	// What each of the three paths retires.
	drawSteps uint32
	lowSteps  uint32
	highSteps uint32
	after     uint32
}

// runClippedBlit stands in for the loop closed by a branch at branchPC back to
// head, and reports how many guest instructions it stood in for.
func (memory *Memory) runClippedBlit(context *Context, loop *clippedBlit) (uint32, error) {
	// The count has already been taken up for the iteration that reached the
	// branch, so the distance to the limit is what is left to run.
	remaining := int64(int32(context.Registers[loop.limit])) - int64(int32(context.Registers[loop.count]))
	if remaining < 2 || remaining > maxStoreLoopIterations {
		return 0, nil
	}

	// Everything the body reaches through memory, read once. A fault on any of
	// them is one the next iteration would have taken, so the loop goes back to
	// the interpreter to take it there.
	stack := context.Registers[RegisterSP]
	low, err := memory.readData32(stack + loop.lowSlot)
	if err != nil {
		return 0, nil
	}
	high, err := memory.readData32(stack + loop.highSlot)
	if err != nil {
		return 0, nil
	}
	flagRecord, err := memory.readData32(stack + loop.flagSlot)
	if err != nil {
		return 0, nil
	}
	flag, err := memory.readData32(flagRecord + loop.flagOffset)
	if err != nil {
		return 0, nil
	}
	if flag != 0 {
		// The guest is about to leave this loop for the blending form of the
		// same blit, which is somewhere this cannot follow it to.
		return 0, nil
	}
	record, err := memory.readData32(stack + loop.record)
	if err != nil {
		return 0, nil
	}
	table, err := memory.readData32(record + loop.colours)
	if err != nil {
		return 0, nil
	}
	slotAddress := stack + loop.slot
	destination, err := memory.readData32(slotAddress)
	if err != nil {
		return 0, nil
	}
	source := context.Registers[loop.source]
	index := int64(int32(context.Registers[loop.index]))

	// The index moves one way and by one, so the pixels both tests let through
	// are one contiguous stretch: from where the index reaches the low bound to
	// where it reaches the high one, each clamped to the run.
	start := clampSpan(int64(int32(low))-index, remaining)
	end := clampSpan(int64(int32(high))-index, remaining)
	if end < start {
		end = start
	}
	drawn := uint32(end - start)

	// Only the drawn stretch is read and written, which is what the guest does
	// too; both spans whole, before anything moves.
	span := uint64(drawn) * 2
	if drawn > 0 {
		if err := memory.validateLocked(source+uint32(start), uint64(drawn), PermissionRead, "read"); err != nil {
			return 0, nil
		}
		if err := memory.validateLocked(destination, span, PermissionWrite, "write"); err != nil {
			return 0, nil
		}
	}
	// The words above are read once and treated as constants for the run, which
	// they are only as long as the run cannot write them. See spilled_blit.go.
	overlaps := func(address uint32) bool {
		return uint64(address)+4 > uint64(destination) && uint64(address) < uint64(destination)+span
	}
	if overlaps(slotAddress) || overlaps(stack+loop.record) || overlaps(record+loop.colours) {
		return 0, nil
	}
	if overlaps(stack+loop.lowSlot) || overlaps(stack+loop.highSlot) {
		return 0, nil
	}
	if overlaps(stack+loop.flagSlot) || overlaps(flagRecord+loop.flagOffset) {
		return 0, nil
	}

	// The drawn stretch, the way table_blit.go draws one.
	var colours [256]uint16
	var known [256]bool
	lastIndex := uint8(0)
	from := source + uint32(start)
	for pixel := uint32(0); pixel < drawn; {
		if destination&1 == 0 {
			read := memory.rawSpan(from+pixel, drawn-pixel, false)
			write := memory.rawSpan(destination+pixel*2, (drawn-pixel)*2, true)
			if read != nil && write != nil {
				count := uint32(len(read))
				if pixels := uint32(len(write)) / 2; pixels < count {
					count = pixels
				}
				for at := uint32(0); at < count; at++ {
					colour := read[at]
					if !known[colour] {
						value, err := memory.readData16(table + uint32(colour)*2)
						if err != nil {
							return 0, err
						}
						colours[colour], known[colour] = value, true
					}
					binary.LittleEndian.PutUint16(write[at*2:], colours[colour])
				}
				lastIndex = read[count-1]
				pixel += count
				continue
			}
		}
		value, err := memory.read8(from + pixel)
		if err != nil {
			return 0, err
		}
		if !known[value] {
			colour, err := memory.readData16(table + uint32(value)*2)
			if err != nil {
				return 0, err
			}
			colours[value], known[value] = colour, true
		}
		if err := memory.writeData16(destination+pixel*2, colours[value]); err != nil {
			return 0, err
		}
		lastIndex = value
		pixel++
	}
	if drawn > 0 {
		if err := memory.writeData32(slotAddress, destination+drawn*2); err != nil {
			return 0, err
		}
	}

	// The registers are left where the guest would have left them, and the way
	// to be sure of that with registers this shape reuses three times over is to
	// write them in the order the guest writes them, on the path its last
	// iteration took. The index only moves one way, so which path that is falls
	// out of where it ended.
	last := index + remaining - 1
	registers := &context.Registers
	if last >= int64(int32(low)) {
		registers[loop.lowIn] = low
		if last < int64(int32(high)) {
			registers[loop.highIn] = high
			registers[loop.flagRecordIn] = flagRecord
			registers[loop.flagIn] = flag
			registers[loop.recordIn] = record
			registers[loop.scratch] = uint32(lastIndex)
			registers[loop.tableIn] = table
			registers[loop.scratch] = uint32(lastIndex) * 2
			registers[loop.scratch] = uint32(colours[lastIndex])
			registers[loop.destIn] = destination + (drawn-1)*2
			registers[loop.destIn] = destination + drawn*2
		} else {
			registers[loop.highIn] = high
		}
	} else {
		registers[loop.lowIn] = low
	}
	// The tail, which every path runs.
	registers[loop.one] = 1
	registers[loop.count] = registers[loop.limit]
	registers[loop.source] = source + uint32(remaining)
	registers[loop.index] = uint32(index + remaining)
	// The flags of the `cmp rC, rL` that found them equal: a subtraction that
	// leaves nothing and never borrows.
	context.setNZCV(0, true, false)
	context.Registers[RegisterPC] = loop.after

	skippedLow := uint32(start)
	skippedHigh := uint32(remaining - end)
	return skippedLow*loop.lowSteps + skippedHigh*loop.highSteps + drawn*loop.drawSteps, nil
}

// clampSpan holds where a bound falls inside a run of the given length.
func clampSpan(offset, length int64) int64 {
	if offset < 0 {
		return 0
	}
	if offset > length {
		return length
	}
	return offset
}

// analyseClippedBlit reads the body between head and branchPC and answers the
// blit it is, or nil for one it cannot prove.
//
// The three tests come in one order because each depends on the one before it
// having decided not to skip, so the chain is read by position. What follows
// them is the body `spilled_blit.go` reads, and the tail every path rejoins is
// read as a walk: which of its two increments is the source is already known,
// and the other is the count.
func (memory *Memory) analyseClippedBlit(head, branchPC uint32) *clippedBlit {
	if branchPC <= head || branchPC-head > maxClippedBlitBytes {
		return nil
	}
	loop := &memory.clippedBlitScratch
	*loop = clippedBlit{after: branchPC + 2}
	var written [16]int
	// The chain is ten instructions, the draw ten more, and the tail at least
	// the branch. A body with no room for all three is not this shape.
	if branchPC-head < 20+2 {
		return nil
	}

	read := func(index uint32) (thumbForm, uint32, bool) {
		address := head + index*2
		decoded, cached := memory.decodedThumbFast(address)
		if !cached {
			var err error
			if decoded, err = memory.decodeThumb(address); err != nil {
				return thumbUndecoded, 0, false
			}
		}
		return decoded.form, uint32(decoded.instruction), true
	}
	// frameLoad answers the register and slot of `ldr rD, [sp, #n]`.
	frameLoad := func(index uint32) (uint32, uint32, bool) {
		form, instruction, ok := read(index)
		if !ok || form != thumbStackRelativeTransfer || instruction&(1<<11) == 0 {
			return 0, 0, false
		}
		return instruction >> 8 & 7, (instruction & 0xff) * 4, true
	}
	// highOperands answers the two registers of a high-register compare or add,
	// which is the only place this shape reaches past the low eight.
	highOperands := func(index uint32, opcode uint32) (uint32, uint32, bool) {
		form, instruction, ok := read(index)
		if !ok || form != thumbHighRegister || instruction>>8&3 != opcode {
			return 0, 0, false
		}
		destination := instruction&7 | instruction>>4&8
		source := instruction >> 3 & 15
		if destination == RegisterPC || destination == RegisterSP ||
			source == RegisterPC || source == RegisterSP {
			return 0, 0, false
		}
		return destination, source, true
	}
	// branchTo answers where a conditional branch of the given condition goes.
	branchTo := func(index, condition uint32) (uint32, bool) {
		form, instruction, ok := read(index)
		if !ok || form != thumbConditionalBranch || instruction>>8&0xf != condition {
			return 0, false
		}
		address := head + index*2
		return uint32(int64(address+4) + int64(int8(instruction&0xff))*2), true
	}

	// `ldr rL, [sp, #a]; cmp rX, rL; blt tail` — off the left of the clip.
	lowIn, lowSlot, ok := frameLoad(0)
	if !ok {
		return nil
	}
	index, against, ok := highOperands(1, 1)
	if !ok || against != lowIn {
		return nil
	}
	join, ok := branchTo(2, 11)
	if !ok || join <= head+20 || join >= branchPC {
		return nil
	}
	// `ldr rH, [sp, #b]; cmp rH, rX; ble tail` — off the right of it.
	highIn, highSlot, ok := frameLoad(3)
	if !ok {
		return nil
	}
	left, right, ok := highOperands(4, 1)
	if !ok || left != highIn || right != index {
		return nil
	}
	if target, ok := branchTo(5, 13); !ok || target != join {
		return nil
	}
	// `ldr rR, [sp, #c]; ldr rF, [rR, #d]; cmp rF, #0; bne blend` — the flag,
	// which is invariant and takes the guest out of this loop entirely.
	flagRecordIn, flagSlot, ok := frameLoad(6)
	if !ok {
		return nil
	}
	form, instruction, ok := read(7)
	if !ok || form != thumbImmediateTransfer ||
		instruction&(1<<12) != 0 || instruction&(1<<11) == 0 ||
		instruction>>3&7 != flagRecordIn {
		return nil
	}
	flagIn, flagOffset := instruction&7, (instruction>>6&0x1f)*4
	form, instruction, ok = read(8)
	if !ok || form != thumbImmediate || instruction>>11&3 != 1 ||
		instruction>>8&7 != flagIn || instruction&0xff != 0 {
		return nil
	}
	if target, ok := branchTo(9, 1); !ok || (target >= head && target <= branchPC) {
		return nil
	}
	loop.lowIn, loop.lowSlot = lowIn, lowSlot
	loop.highIn, loop.highSlot = highIn, highSlot
	loop.flagRecordIn, loop.flagSlot = flagRecordIn, flagSlot
	loop.flagIn, loop.flagOffset = flagIn, flagOffset
	loop.index = index
	written[lowIn]++
	written[highIn]++
	written[flagRecordIn]++
	written[flagIn]++

	if !memory.walkClippedDraw(loop, head+20, join, &written) {
		return nil
	}
	if !memory.walkClippedTail(loop, join, branchPC, &written) {
		return nil
	}

	// The bounds and the flag are only invariant while the loop cannot write
	// them, and the destination is the one thing it does write. Its slot has to
	// be its own; the addresses are checked against the span at run time.
	for _, slot := range [...]uint32{loop.lowSlot, loop.highSlot, loop.flagSlot, loop.record} {
		if slot == loop.slot {
			return nil
		}
	}
	// One role each for the registers that walk, and nothing may move the limit
	// the count is measured against.
	if written[loop.limit] != 0 || written[loop.source] != 1 ||
		written[loop.count] != 1 || written[loop.index] != 1 {
		return nil
	}
	if loop.source == loop.count || loop.source == loop.limit || loop.count == loop.limit {
		return nil
	}
	if loop.index == loop.source || loop.index == loop.count || loop.index == loop.limit {
		return nil
	}

	tail := (branchPC-join)/2 + 1
	loop.lowSteps = 3 + tail
	loop.highSteps = 6 + tail
	loop.drawSteps = (branchPC-head)/2 + 1
	return loop
}

// walkClippedDraw reads the block a pixel inside the clip runs, which is the
// body of spilled_blit.go without its counter.
func (memory *Memory) walkClippedDraw(loop *clippedBlit, from, to uint32, written *[16]int) bool {
	var (
		frameWord [8]bool
		frameSlot [8]uint32
		advanced  [8]bool
		palette   [8]bool
	)
	var (
		scratch                               uint32
		haveIndex, haveShift, haveLookup      bool
		haveStore, haveAdvance, haveWriteBack bool
		havePalette                           bool
		storeSlot                             uint32
	)
	forget := func(register uint32) {
		frameWord[register], advanced[register], palette[register] = false, false, false
	}

	for address := from; address < to; address += 2 {
		decoded, cached := memory.decodedThumbFast(address)
		if !cached {
			var err error
			if decoded, err = memory.decodeThumb(address); err != nil {
				return false
			}
		}
		instruction := uint32(decoded.instruction)

		switch decoded.form {
		case thumbStackRelativeTransfer:
			load := instruction&(1<<11) != 0
			data := instruction >> 8 & 7
			offset := (instruction & 0xff) * 4
			if load {
				forget(data)
				(*written)[data]++
				frameWord[data], frameSlot[data] = true, offset
				loop.destIn = data
				break
			}
			if haveWriteBack || !frameWord[data] || !advanced[data] || frameSlot[data] != offset {
				return false
			}
			haveWriteBack, loop.slot = true, offset

		case thumbImmediateTransfer:
			byteTransfer := instruction&(1<<12) != 0
			load := instruction&(1<<11) != 0
			offset := instruction >> 6 & 0x1f
			base := instruction >> 3 & 7
			data := instruction & 7
			if !load {
				return false
			}
			if byteTransfer {
				// `ldrb rD, [rB]` — the source byte.
				if offset != 0 || haveIndex || frameWord[base] {
					return false
				}
				forget(data)
				(*written)[data]++
				haveIndex, loop.source, scratch = true, base, data
				loop.scratch = data
				break
			}
			// The palette, reached through the record the frame points at.
			if havePalette || !frameWord[base] || advanced[base] {
				return false
			}
			forget(data)
			(*written)[data]++
			havePalette, palette[data] = true, true
			loop.recordIn, loop.record, loop.colours = base, frameSlot[base], offset*4
			loop.tableIn = data

		case thumbShift:
			if instruction>>11&3 != 0 || instruction>>6&0x1f != 1 {
				return false
			}
			source := instruction >> 3 & 7
			data := instruction & 7
			if !haveIndex || haveShift || source != scratch || data != scratch {
				return false
			}
			forget(data)
			(*written)[data]++
			haveShift = true

		case thumbRegisterTransfer:
			if instruction>>9&7 != 5 || haveLookup || !haveShift {
				return false
			}
			offsetRegister := instruction >> 6 & 7
			base := instruction >> 3 & 7
			data := instruction & 7
			switch {
			case palette[base] && offsetRegister == scratch:
			case palette[offsetRegister] && base == scratch:
			default:
				return false
			}
			if data != scratch {
				return false
			}
			forget(data)
			(*written)[data]++
			haveLookup = true

		case thumbHalfwordTransfer:
			load := instruction&(1<<11) != 0
			offset := (instruction >> 6 & 0x1f) * 2
			base := instruction >> 3 & 7
			data := instruction & 7
			if load || offset != 0 || haveStore || !haveLookup || data != scratch {
				return false
			}
			if !frameWord[base] || advanced[base] {
				return false
			}
			haveStore, storeSlot = true, frameSlot[base]

		case thumbImmediate:
			// The destination gaining a pixel, and nothing else: the count and
			// the index are the tail's.
			if instruction>>11&3 != 2 || instruction&0xff != 2 {
				return false
			}
			register := instruction >> 8 & 7
			if haveAdvance || !frameWord[register] || advanced[register] {
				return false
			}
			haveAdvance, advanced[register] = true, true
			(*written)[register]++
			loop.destIn = register

		default:
			return false
		}
	}
	if !haveIndex || !haveShift || !haveLookup || !haveStore || !havePalette {
		return false
	}
	if !haveAdvance || !haveWriteBack || storeSlot != loop.slot {
		return false
	}
	return true
}

// walkClippedTail reads the block every path rejoins: the constant the index is
// advanced by, the two increments, and the compare the closing branch reads.
func (memory *Memory) walkClippedTail(loop *clippedBlit, from, to uint32, written *[16]int) bool {
	var haveOne, haveCount, haveSource, haveStep, haveCompare bool
	for address := from; address < to; address += 2 {
		decoded, cached := memory.decodedThumbFast(address)
		if !cached {
			var err error
			if decoded, err = memory.decodeThumb(address); err != nil {
				return false
			}
		}
		instruction := uint32(decoded.instruction)

		switch decoded.form {
		case thumbImmediate:
			opcode := instruction >> 11 & 3
			register := instruction >> 8 & 7
			immediate := instruction & 0xff
			switch {
			case opcode == 0 && immediate == 1 && !haveOne:
				// `movs rD, #1` — what the index is advanced by.
				haveOne, loop.one = true, register
				(*written)[register]++
			case opcode == 2 && immediate == 1 && register == loop.source && !haveSource:
				haveSource = true
				(*written)[register]++
			case opcode == 2 && immediate == 1 && !haveCount:
				haveCount, loop.count = true, register
				(*written)[register]++
			default:
				return false
			}

		case thumbHighRegister:
			// `add rX, rOne` — the index across the row.
			if haveStep || instruction>>8&3 != 0 {
				return false
			}
			destination := instruction&7 | instruction>>4&8
			source := instruction >> 3 & 15
			if destination != loop.index || !haveOne || source != loop.one {
				return false
			}
			haveStep = true
			(*written)[destination]++

		case thumbALU:
			// `cmp rC, rL` — the loop's own ending, which has to be the last
			// thing before the branch or the flags are not the ones it set.
			if haveCompare || instruction>>6&0xf != 10 || address+2 != to {
				return false
			}
			data := instruction & 7
			if !haveCount || data != loop.count {
				return false
			}
			haveCompare, loop.limit = true, instruction>>3&7

		default:
			return false
		}
	}
	if !haveOne || !haveCount || !haveSource || !haveStep || !haveCompare {
		return false
	}
	// The branch has to be the one that continues while the count is short of
	// the limit.
	decoded, cached := memory.decodedThumbFast(to)
	if !cached {
		var err error
		if decoded, err = memory.decodeThumb(to); err != nil {
			return false
		}
	}
	// Condition 11 is LT.
	return decoded.form == thumbConditionalBranch && uint32(decoded.instruction)>>8&0xf == 11
}
