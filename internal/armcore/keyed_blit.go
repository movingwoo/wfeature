package armcore

import "encoding/binary"

// Recognising a palette blit that skips a key colour, and standing in for it.
//
// One local title draws every sprite through this loop, and it was 41% of the
// guest instructions that title executed in a run a person reported as slow;
// a second title carries the same loop with its registers and frame slots
// chosen differently. It is the palette blit `table_blit.go` reads, with three
// differences that each refuse every earlier recogniser:
//
//   - **the palette holds words, and one of them is a key.** The index is
//     scaled by four and the colour loaded as a word, compared with a key the
//     caller left in a register, and stored as a halfword only where it is not
//     the key — a guarded store, as in `byte_blend.go`.
//   - **both bases come out of frame slots every pixel**: `ldr rS, [sp, #s]`
//     before the source byte and `ldr rT, [sp, #t]` before the palette word.
//     One title reuses a single register for both, which is why the two loads
//     are read as two roles rather than two registers.
//   - **the source is indexed by a counter that counts up**, `[rS, rI]`, to a
//     limit in a register the body never writes — a high register in one
//     title, so the closing compare is the high-register form there. A
//     mirrored sprite reads the same row backwards: `subs rV, rS, rI` and then
//     `ldrb rV, [rV]`.
//
// The whole body is twelve instructions, thirteen mirrored, and every one of
// them has a fixed place and form: the walk below reads them positionally and
// refuses anything else, which is the strictest proof the shape allows and the
// right one for a loop this specific.
//
// As in `byte_blend.go`, the last iteration is left to the interpreter. The
// two bases, the index byte, the colour and the flags are all rewritten by the
// body before they are read, so none of them is live at the loop's head; the
// interpreted iteration is what leaves them where the code after the loop finds
// them. The stand-in only has to leave the counter and the destination right.

// maxKeyedBlitBytes bounds this body: thirteen instructions in the mirrored
// form, the branch included.
const maxKeyedBlitBytes = 24

// keyedBlit is a recognised keyed palette blit.
type keyedBlit struct {
	// sourceSlot and tableSlot are the frame-slot offsets the two bases are
	// reloaded from every pixel. The stack pointer is the one base a body of
	// this shape cannot move; see fill_loop.go.
	sourceSlot uint32
	tableSlot  uint32
	// index counts up from where the loop head holds it to limit, which the
	// body never writes; destination walks two bytes a pixel; key is the
	// colour that is not stored.
	index       uint32
	limit       uint32
	destination uint32
	key         uint32
	// mirrored says the source is read backwards, at base minus index.
	mirrored bool
	// steps is one iteration's instructions with the store included, so a
	// keyed pixel is charged one instruction less.
	steps uint32
}

// runKeyedBlit stands in for every remaining iteration but the last, and
// reports how many guest instructions it stood in for.
func (memory *Memory) runKeyedBlit(context *Context, loop *keyedBlit) (uint32, error) {
	// The closing branch is `blt`, a signed comparison, and it was taken, so
	// the index is below the limit and the difference is what is left.
	start := int64(int32(context.Registers[loop.index]))
	remaining := int64(int32(context.Registers[loop.limit])) - start
	if remaining < 2 || remaining > maxStoreLoopIterations {
		return 0, nil
	}
	iterations := uint32(remaining - 1)

	stack := context.Registers[RegisterSP]
	sourceSlot, tableSlot := stack+loop.sourceSlot, stack+loop.tableSlot
	// A word load from an unaligned slot rotates what it reads, and the guest
	// would do that every pixel; the stand-in does not model it.
	if sourceSlot&3 != 0 || tableSlot&3 != 0 {
		return 0, nil
	}
	base, err := memory.readData32(sourceSlot)
	if err != nil {
		// The fault belongs to the next iteration's first instruction, which
		// the interpreter is about to run.
		return 0, nil
	}
	table, err := memory.readData32(tableSlot)
	if err != nil {
		return 0, nil
	}
	destination := context.Registers[loop.destination]
	key := context.Registers[loop.key]
	// An odd destination is a halfword store that aligns downward onto the
	// previous pixel, and an unaligned palette entry is a rotated load; the
	// interpreter is what does both.
	if destination&1 != 0 || table&3 != 0 {
		return 0, nil
	}

	// The source bytes the stood-in pixels read: forwards from base plus the
	// index, or backwards from base minus it.
	first := int64(base) + start
	if loop.mirrored {
		first = int64(base) - start - int64(iterations-1)
	}
	if first < 0 || first+int64(iterations) > 1<<32 {
		return 0, nil
	}
	source := uint32(first)
	span := uint64(iterations) * 2
	if uint64(destination)+span > 1<<32 || uint64(table)+256*4 > 1<<32 {
		return 0, nil
	}

	// Every span whole, before anything moves.
	if err := memory.validateLocked(source, uint64(iterations), PermissionRead, "read"); err != nil {
		return 0, nil
	}
	if err := memory.validateLocked(destination, span, PermissionWrite, "write"); err != nil {
		return 0, nil
	}
	if err := memory.validateLocked(table, 256*4, PermissionRead, "read"); err != nil {
		return 0, nil
	}
	// What the guest reads every pixel has to be what it read on the first:
	// a destination over either frame slot, over the palette or over the
	// source would be a loop whose later pixels do not do what its first one
	// did. Refused rather than modelled.
	if spansOverlap(sourceSlot, 4, destination, span) || spansOverlap(tableSlot, 4, destination, span) ||
		spansOverlap(table, 256*4, destination, span) || spansOverlap(source, uint64(iterations), destination, span) {
		return 0, nil
	}

	// The palette is read once per index asked for, as table_blit.go does.
	var colours [256]uint32
	var known [256]bool
	colour := func(index uint8) (uint32, error) {
		if !known[index] {
			value, err := memory.readData32(table + uint32(index)*4)
			if err != nil {
				return 0, err
			}
			colours[index], known[index] = value, true
		}
		return colours[index], nil
	}

	charged := uint32(0)
	for pixel := uint32(0); pixel < iterations; {
		// The direct route takes a run of pixels whose source and destination
		// both sit in one page each; whatever it refuses goes a pixel at a
		// time through the checked accessors, as in table_blit.go.
		var from []byte
		if loop.mirrored {
			// Backwards, a run reads from this pixel's byte down to the start
			// of its page or of the span, so the slice is asked for from the
			// lowest of those and read from its end.
			top := source + iterations - 1 - pixel
			low := top &^ memoryPageMask
			if low < source {
				low = source
			}
			if run := memory.rawSpan(low, top-low+1, false); uint32(len(run)) == top-low+1 {
				from = run
			}
		} else {
			from = memory.rawSpan(source+pixel, iterations-pixel, false)
		}
		var to []byte
		if from != nil {
			to = memory.rawSpan(destination+pixel*2, (iterations-pixel)*2, true)
		}
		if from != nil && to != nil {
			count := uint32(len(from))
			if pixels := uint32(len(to)) / 2; pixels < count {
				count = pixels
			}
			for step := uint32(0); step < count; step++ {
				var index uint8
				if loop.mirrored {
					index = from[uint32(len(from))-1-step]
				} else {
					index = from[step]
				}
				value, err := colour(index)
				if err != nil {
					return charged, err
				}
				if value == key {
					charged += loop.steps - 1
					continue
				}
				binary.LittleEndian.PutUint16(to[step*2:], uint16(value))
				charged += loop.steps
			}
			pixel += count
			continue
		}
		at := source + pixel
		if loop.mirrored {
			at = source + iterations - 1 - pixel
		}
		index, err := memory.read8(at)
		if err != nil {
			return charged, err
		}
		value, err := colour(index)
		if err != nil {
			return charged, err
		}
		if value == key {
			charged += loop.steps - 1
		} else {
			if err := memory.writeData16(destination+pixel*2, uint16(value)); err != nil {
				return charged, err
			}
			charged += loop.steps
		}
		pixel++
	}

	context.Registers[loop.index] = uint32(start) + iterations
	context.Registers[loop.destination] = destination + iterations*2
	// The PC stays where the taken branch left it, at the loop's head: the
	// iteration left over is interpreted from there.
	return charged, nil
}

// thumbBody reads the instructions from head to branchPC, the branch
// included, through the decode cache, or answers false. The walk is counted
// rather than run up to branchPC, so a body at the top of the address space
// cannot wrap it, and a body longer than into holds is refused unread.
func (memory *Memory) thumbBody(head, branchPC uint32, into []decodedThumb) ([]decodedThumb, bool) {
	if branchPC < head || (branchPC-head)/2 >= uint32(cap(into)) {
		return nil, false
	}
	into = into[:0]
	for count, address := (branchPC-head)/2+1, head; count > 0; count, address = count-1, address+2 {
		decoded, cached := memory.decodedThumbFast(address)
		if !cached {
			var err error
			if decoded, err = memory.decodeThumb(address); err != nil {
				return nil, false
			}
		}
		into = append(into, decoded)
	}
	return into, true
}

// analyseKeyedBlit reads the body between head and branchPC and answers the
// blit it is, or nil for one it cannot prove.
func (memory *Memory) analyseKeyedBlit(head, branchPC uint32) *keyedBlit {
	if branchPC <= head || branchPC-head > maxKeyedBlitBytes {
		return nil
	}
	var buffer [maxKeyedBlitBytes/2 + 1]decodedThumb
	code, ok := memory.thumbBody(head, branchPC, buffer[:])
	if !ok || len(code) != 12 && len(code) != 13 {
		return nil
	}
	loop := &memory.keyedBlitScratch
	*loop = keyedBlit{mirrored: len(code) == 13, steps: uint32(len(code))}
	at := 0
	nextDecoded := func() decodedThumb {
		decoded := code[at]
		at++
		return decoded
	}
	next := func() uint32 { return uint32(nextDecoded().instruction) }

	// ldr rS, [sp, #s]
	sourceBase, sourceSlot, ok := thumbFrameLoad(nextDecoded())
	if !ok {
		return nil
	}
	var instruction, value, index uint32
	if loop.mirrored {
		// subs rV, rS, rI, then ldrb rV, [rV]
		instruction = next()
		if instruction&0xfe00 != 0x1a00 || instruction>>3&7 != sourceBase {
			return nil
		}
		index, value = instruction>>6&7, instruction&7
		if next() != 0x7800|value<<3|value {
			return nil
		}
	} else {
		// ldrb rV, [rS, rI]
		instruction = next()
		if instruction&0xfe00 != 0x5c00 || instruction>>3&7 != sourceBase {
			return nil
		}
		index, value = instruction>>6&7, instruction&7
	}
	// ldr rT, [sp, #t]
	tableBase, tableSlot, ok := thumbFrameLoad(nextDecoded())
	if !ok {
		return nil
	}
	// lsls rV, rV, #2; ldr rV, [rV, rT]
	if next() != 2<<6|value<<3|value || next() != 0x5800|tableBase<<6|value<<3|value {
		return nil
	}
	// cmp rV, rK; beq over the store; strh rV, [rD]
	instruction = next()
	if instruction&0xffc7 != 0x4280|value {
		return nil
	}
	key := instruction >> 3 & 7
	if next() != 0xd000 {
		return nil
	}
	instruction = next()
	if instruction&0xffc7 != 0x8000|value {
		return nil
	}
	destination := instruction >> 3 & 7
	// adds rI, #1; adds rD, #2
	if next() != 0x3000|index<<8|1 || next() != 0x3000|destination<<8|2 {
		return nil
	}
	// cmp rI, rL, the low-register form or the high-register one.
	var limit uint32
	switch instruction = next(); {
	case instruction&0xffc7 == 0x4280|index:
		limit = instruction >> 3 & 7
	case instruction&0xff87 == 0x4500|index:
		limit = instruction >> 3 & 0xf
	default:
		return nil
	}
	// blt head
	instruction = next()
	if instruction&0xff00 != 0xdb00 {
		return nil
	}
	if uint32(int32(branchPC+4)+int32(int8(instruction))*2) != head {
		return nil
	}

	// The limit is a general register the body does not write: neither the
	// stack pointer, the link register nor the PC.
	if limit > 12 {
		return nil
	}
	// The registers the loop walks or reads each pixel need one of their own.
	// The two bases may share one, because each is reloaded right before its
	// use; neither may be the value, which the palette load reads after the
	// second base is loaded.
	var seen uint32
	for _, register := range [...]uint32{value, index, destination, key, limit} {
		if seen&(1<<register) != 0 {
			return nil
		}
		seen |= 1 << register
	}
	if seen&(1<<sourceBase) != 0 || seen&(1<<tableBase) != 0 {
		return nil
	}

	loop.sourceSlot, loop.tableSlot = sourceSlot, tableSlot
	loop.index, loop.limit, loop.destination, loop.key = index, limit, destination, key
	return loop
}
