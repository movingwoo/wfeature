package armcore

// Standing in for a blit whose per-pixel writer is a call.
//
// `clipped_blit.go` and `spilled_blit.go` both refuse the same thing, and it is
// the same thing a person reports as a full-screen effect being slow. Those
// blits are the fall-through of a mode word; when it is set the guest draws the
// pixel through a call instead, and the call is where all the cost is. On the
// title this was built for, one blended pixel is about ninety guest
// instructions where a plain one is six, and a heavy tick draws seventy
// thousand of them — a whole screen.
//
// A body with a `bl` in it is a body none of the walks can reason about, so the
// only stand-in that helps is one that performs what the callee performs. Two
// ways to do that were weighed and only one of them is general:
//
//   - **Reproduce the arithmetic in Go**, matched sequence by sequence the way
//     `word_modulate.go` matches its blend. That is the fastest possible answer
//     and it is one title's: the writer this was built against dispatches
//     sixteen modes through a jump table, two of which the reported scene uses,
//     each in a 5-5-5 and a 5-6-5 form. Four sequences, none of which reads
//     another, and a fifth title would need a fifth.
//   - **Fold the call and compile what is left**, which is this file. The
//     writer is walked once per stand-in with the destination halfword and the
//     colour as its only unknowns; everything else it touches — the module's
//     GOT, the mode, the jump table, the alpha, the mask literals, the format
//     flag — is a word the loop cannot move, so it folds to a constant and the
//     branches it decides fold with it. What survives is the arithmetic proper,
//     twenty to thirty operations, and those are evaluated per pixel.
//
// The second is what this does, and the reason is not only that it reads any
// mode rather than two. **Most of a per-pixel writer is not arithmetic.** It is
// a prologue that rebuilds the module's base, a dispatch, a format test and a
// re-read of values that were the same on the last pixel and will be the same
// on the next. Folding is what removes those, and no amount of sequence
// matching would have removed them from a title whose blend the matcher did not
// already know.
//
// What it is not: a translator. It compiles one straight-line leaf reached from
// one loop, under the rules the other stand-ins are held to — the spans are
// validated whole, the values treated as constant are checked against the
// destination, the step count is what the guest would have retired, and
// anything the walk cannot prove is handed back to the interpreter. See
// armcore.md, "Why a translator is not the answer even with unlimited effort",
// which this does not contradict: the win here is the folding, not the dispatch.
//
// The pixels go out through the ordinary checked write, not through a raw span,
// so a watched store is reported here the way it is anywhere else — the rule
// `TestAStandInStillReportsWatchedStores` holds the other shapes to. Per pixel
// that costs a page lookup the plain blits avoid, and against a writer this
// stands in for it is not worth reading.

// The bounds a leaf is walked under. A writer that needs more than this is one
// with a loop in it or one this cannot read, and either way it is refused.
const (
	maxLeafInstructions = 512
	maxLeafNodes        = 192
	maxLeafDepth        = 8
	// maxLeafInvariants bounds the addresses a leaf may treat as constant, all
	// of which the run checks against the destination.
	maxLeafInvariants = 24
)

// leafOp is one operation in a compiled writer.
type leafOp uint8

const (
	leafConst leafOp = iota
	// leafDst is the halfword the writer reads back from the pixel it is
	// about to overwrite, and leafSrc the colour it was handed.
	leafDst
	leafSrc
	leafAnd
	leafOrr
	leafEor
	leafAdd
	leafSub
	leafMul
	leafLsl
	leafLsr
	leafAsr
	// leafByte is a signed byte read from the sum of two values, which is how
	// a writer that blends through a lookup table reaches it. The table's base
	// folds to a constant; the index does not.
	leafByte
	leafUnsignedByte
	leafHalf
)

// leafNode is one value in a writer's dataflow. a and b are node indices.
type leafNode struct {
	op   leafOp
	a, b int32
	k    uint32
}

// leafProgram is a writer with everything invariant folded out of it: the
// operations that are left, in an order where every operand precedes its use.
type leafProgram struct {
	nodes []leafNode
	// result is the node whose value the writer stores.
	result int32
	// invariants are the addresses whose contents were read once and treated
	// as constant. A destination covering any of them is refused.
	invariants []uint32
	// steps is what one call retires, dispatch and prologue included.
	steps uint32
}

// leafValueKind separates the things a register can hold during the walk.
type leafValueKind uint8

const (
	// leafUnknown is a value the walk cannot reason about. It may be moved and
	// saved, which is what a callee-saved register is for, and using it for
	// anything else refuses the writer.
	leafUnknown leafValueKind = iota
	leafData
	// leafDestination is the pointer the writer was handed, plus an offset.
	leafDestination
	// leafFrame is the stack pointer as it was on entry, plus an offset.
	leafFrame
)

type leafValue struct {
	kind   leafValueKind
	node   int32
	offset int32
}

// leafReturn is the link register a walk starts with: a value no code can be
// at, so a `pop {pc}` that lands on it is the writer returning rather than a
// jump this has to follow.
const leafReturn = uint32(0xfffffffe)

// leafTrace is the state one walk carries.
type leafTrace struct {
	memory     *Memory
	nodes      []leafNode
	registers  [16]leafValue
	stack      map[int32]leafValue
	invariants []uint32
	// flags are only tracked where both sides of the compare were constant,
	// which is the only case a fold can decide a branch from.
	flagsKnown bool
	flagZ      bool
	flagC      bool
	stored     int32
	stores     int
	steps      uint32
	depth      int
}

func (trace *leafTrace) constant(value uint32) leafValue {
	return leafValue{kind: leafData, node: trace.push(leafNode{op: leafConst, k: value})}
}

// push adds a node, folding it where every operand is already constant. The
// fold is the whole point of the walk: a writer's prologue, its dispatch and
// its format test are all arithmetic over words it read out of memory it cannot
// move, so folding leaves only what depends on the pixel.
func (trace *leafTrace) push(node leafNode) int32 {
	if node.op != leafConst {
		a, aok := trace.constantValue(node.a)
		b, bok := trace.constantValue(node.b)
		if aok && (bok || node.b < 0) {
			if value, ok := foldLeaf(node.op, a, b); ok {
				node = leafNode{op: leafConst, k: value}
			}
		}
	}
	if len(trace.nodes) >= maxLeafNodes {
		return -1
	}
	trace.nodes = append(trace.nodes, node)
	return int32(len(trace.nodes) - 1)
}

func (trace *leafTrace) constantValue(index int32) (uint32, bool) {
	if index < 0 || int(index) >= len(trace.nodes) {
		return 0, false
	}
	node := trace.nodes[index]
	if node.op != leafConst {
		return 0, false
	}
	return node.k, true
}

// foldLeaf answers an operation over constants. Only the operations a fold can
// perform without reading guest memory are here; leafByte and its siblings are
// left to run time even when their address is constant, because the memory they
// read is memory the loop might write.
func foldLeaf(op leafOp, a, b uint32) (uint32, bool) {
	switch op {
	case leafAnd:
		return a & b, true
	case leafOrr:
		return a | b, true
	case leafEor:
		return a ^ b, true
	case leafAdd:
		return a + b, true
	case leafSub:
		return a - b, true
	case leafMul:
		return a * b, true
	case leafLsl:
		if b >= 32 {
			return 0, true
		}
		return a << b, true
	case leafLsr:
		if b >= 32 {
			return 0, true
		}
		return a >> b, true
	case leafAsr:
		if b >= 32 {
			return uint32(int32(a) >> 31), true
		}
		return uint32(int32(a) >> b), true
	}
	return 0, false
}

// data answers the node a register holds, and whether it holds one at all.
func (trace *leafTrace) data(register uint32) (int32, bool) {
	value := trace.registers[register]
	if value.kind != leafData || value.node < 0 {
		return 0, false
	}
	return value.node, true
}

func (trace *leafTrace) setData(register uint32, node int32) bool {
	if node < 0 {
		return false
	}
	trace.registers[register] = leafValue{kind: leafData, node: node}
	return true
}

// binary applies an operation to two registers and lands it in a third.
func (trace *leafTrace) binary(op leafOp, destination, left, right uint32) bool {
	a, ok := trace.data(left)
	if !ok {
		return false
	}
	b, ok := trace.data(right)
	if !ok {
		return false
	}
	trace.flagsKnown = false
	return trace.setData(destination, trace.push(leafNode{op: op, a: a, b: b}))
}

// walkLeaf reads a per-pixel writer from its entry and answers it as a program
// over the destination halfword and the colour, or nil for one it cannot prove.
//
// It is a walk of one path, not of a function: every branch it meets has to be
// decidable from what has already folded, which is what makes a writer with a
// loop in it, or one whose dispatch depends on the pixel, a refusal rather than
// an approximation.
func (memory *Memory) walkLeaf(entry, source uint32) *leafProgram {
	trace := &leafTrace{
		memory: memory,
		nodes:  make([]leafNode, 0, 64),
		stack:  make(map[int32]leafValue, 16),
		stored: -1,
	}
	for register := range trace.registers {
		trace.registers[register] = leafValue{kind: leafUnknown}
	}
	// The two arguments, and the frame the writer builds its own on.
	trace.registers[0] = leafValue{kind: leafDestination}
	if !trace.setData(1, trace.push(leafNode{op: leafSrc})) {
		return nil
	}
	trace.registers[RegisterSP] = leafValue{kind: leafFrame}
	trace.registers[RegisterLR] = trace.constant(leafReturn)

	address := entry
	for instructions := 0; ; instructions++ {
		if instructions >= maxLeafInstructions {
			return nil
		}
		next, done, ok := trace.step(address)
		if !ok {
			return nil
		}
		if done {
			break
		}
		address = next
	}

	// A writer that wrote anywhere but the pixel it was handed, or that did not
	// leave the caller's registers and stack where it found them, is not one
	// this can stand in for.
	if trace.stores != 1 || trace.stored < 0 {
		return nil
	}
	if trace.registers[RegisterSP].kind != leafFrame || trace.registers[RegisterSP].offset != 0 {
		return nil
	}
	for register := uint32(4); register <= 11; register++ {
		if trace.registers[register].kind != leafUnknown {
			return nil
		}
	}
	_ = source
	return trace.compile()
}

// step performs one instruction and answers where the walk goes next.
func (trace *leafTrace) step(address uint32) (uint32, bool, bool) {
	decoded, cached := trace.memory.decodedThumbFast(address)
	if !cached {
		var err error
		if decoded, err = trace.memory.decodeThumb(address); err != nil {
			return 0, false, false
		}
	}
	instruction := uint32(decoded.instruction)
	trace.steps++
	next := address + 2

	switch decoded.form {
	case thumbShift:
		op := instruction >> 11 & 3
		amount := instruction >> 6 & 0x1f
		from := instruction >> 3 & 7
		to := instruction & 7
		operation := [...]leafOp{leafLsl, leafLsr, leafAsr}[op]
		if op != 0 && amount == 0 {
			amount = 32
		}
		value, ok := trace.data(from)
		if !ok {
			return 0, false, false
		}
		shift := trace.push(leafNode{op: leafConst, k: amount})
		trace.flagsKnown = false
		if !trace.setData(to, trace.push(leafNode{op: operation, a: value, b: shift})) {
			return 0, false, false
		}

	case thumbAddSubtract:
		immediate := instruction&(1<<10) != 0
		subtract := instruction&(1<<9) != 0
		field := instruction >> 6 & 7
		from := instruction >> 3 & 7
		to := instruction & 7
		// `adds rD, rS, #0` is how this compiler spells a move, and the
		// pointer it moves is not a number this can add to.
		if immediate && field == 0 && !subtract {
			trace.registers[to] = trace.registers[from]
			trace.flagsKnown = false
			break
		}
		base := trace.registers[from]
		if base.kind == leafDestination || base.kind == leafFrame {
			if !immediate {
				return 0, false, false
			}
			offset := int32(field)
			if subtract {
				offset = -offset
			}
			trace.registers[to] = leafValue{kind: base.kind, offset: base.offset + offset}
			trace.flagsKnown = false
			break
		}
		value, ok := trace.data(from)
		if !ok {
			return 0, false, false
		}
		operation := leafAdd
		if subtract {
			operation = leafSub
		}
		var other int32
		if immediate {
			other = trace.push(leafNode{op: leafConst, k: field})
		} else {
			other, ok = trace.data(field)
			if !ok {
				return 0, false, false
			}
		}
		trace.flagsKnown = false
		if !trace.setData(to, trace.push(leafNode{op: operation, a: value, b: other})) {
			return 0, false, false
		}

	case thumbImmediate:
		op := instruction >> 11 & 3
		register := instruction >> 8 & 7
		value := instruction & 0xff
		switch op {
		case 0: // MOV
			trace.flagsKnown = false
			trace.registers[register] = trace.constant(value)
		case 1: // CMP — the one comparison a fold can decide a branch from.
			left, ok := trace.constantValue(trace.registers[register].node)
			if !ok || trace.registers[register].kind != leafData {
				trace.flagsKnown = false
				break
			}
			trace.flagsKnown = true
			trace.flagZ = left == value
			trace.flagC = left >= value
		default: // ADD, SUB
			operation := leafAdd
			if op == 3 {
				operation = leafSub
			}
			current, ok := trace.data(register)
			if !ok {
				return 0, false, false
			}
			amount := trace.push(leafNode{op: leafConst, k: value})
			trace.flagsKnown = false
			if !trace.setData(register, trace.push(leafNode{op: operation, a: current, b: amount})) {
				return 0, false, false
			}
		}

	case thumbALU:
		op := instruction >> 6 & 0xf
		from := instruction >> 3 & 7
		to := instruction & 7
		switch op {
		case 0:
			if !trace.binary(leafAnd, to, to, from) {
				return 0, false, false
			}
		case 1:
			if !trace.binary(leafEor, to, to, from) {
				return 0, false, false
			}
		case 2:
			if !trace.binary(leafLsl, to, to, from) {
				return 0, false, false
			}
		case 3:
			if !trace.binary(leafLsr, to, to, from) {
				return 0, false, false
			}
		case 4:
			if !trace.binary(leafAsr, to, to, from) {
				return 0, false, false
			}
		case 12:
			if !trace.binary(leafOrr, to, to, from) {
				return 0, false, false
			}
		case 13:
			if !trace.binary(leafMul, to, to, from) {
				return 0, false, false
			}
		case 14: // BIC
			value, ok := trace.data(from)
			if !ok {
				return 0, false, false
			}
			current, ok := trace.data(to)
			if !ok {
				return 0, false, false
			}
			mask := trace.push(leafNode{op: leafEor, a: value, b: trace.push(leafNode{op: leafConst, k: 0xffffffff})})
			trace.flagsKnown = false
			if !trace.setData(to, trace.push(leafNode{op: leafAnd, a: current, b: mask})) {
				return 0, false, false
			}
		case 10: // CMP
			left, leftOK := trace.constantValue(trace.registers[to].node)
			right, rightOK := trace.constantValue(trace.registers[from].node)
			if !leftOK || !rightOK {
				trace.flagsKnown = false
				break
			}
			trace.flagsKnown = true
			trace.flagZ = left == right
			trace.flagC = left >= right
		default:
			return 0, false, false
		}

	case thumbHighRegister:
		op := instruction >> 8 & 3
		to := instruction&7 | instruction>>4&8
		from := instruction >> 3 & 15
		switch op {
		case 0: // ADD
			if from == RegisterPC {
				current, ok := trace.data(to)
				if !ok {
					return 0, false, false
				}
				pc := trace.push(leafNode{op: leafConst, k: address + 4})
				trace.flagsKnown = false
				if !trace.setData(to, trace.push(leafNode{op: leafAdd, a: current, b: pc})) {
					return 0, false, false
				}
				break
			}
			if !trace.binary(leafAdd, to, to, from) {
				return 0, false, false
			}
		case 2: // MOV
			if from == RegisterPC {
				trace.registers[to] = trace.constant(address + 4)
				break
			}
			if to == RegisterPC {
				target, ok := trace.constantValue(trace.registers[from].node)
				if !ok || trace.registers[from].kind != leafData {
					return 0, false, false
				}
				return target &^ 1, false, true
			}
			trace.registers[to] = trace.registers[from]
		case 1: // CMP
			left, leftOK := trace.constantValue(trace.registers[to].node)
			right, rightOK := trace.constantValue(trace.registers[from].node)
			if !leftOK || !rightOK {
				trace.flagsKnown = false
				break
			}
			trace.flagsKnown = true
			trace.flagZ = left == right
			trace.flagC = left >= right
		case 3: // BX
			target, ok := trace.constantValue(trace.registers[from].node)
			if !ok {
				return 0, false, false
			}
			if target&^1 == leafReturn&^1 {
				return 0, true, true
			}
			return target &^ 1, false, true
		}

	case thumbLiteralLoad:
		register := instruction >> 8 & 7
		at := (address+4)&^3 + (instruction&0xff)*4
		value, ok := trace.invariantWord(at)
		if !ok {
			return 0, false, false
		}
		trace.registers[register] = trace.constant(value)

	case thumbStackRelativeTransfer:
		load := instruction&(1<<11) != 0
		register := instruction >> 8 & 7
		offset := int32((instruction & 0xff) * 4)
		frame := trace.registers[RegisterSP]
		if frame.kind != leafFrame {
			return 0, false, false
		}
		slot := frame.offset + offset
		if load {
			held, ok := trace.stack[slot]
			if !ok {
				return 0, false, false
			}
			trace.registers[register] = held
			break
		}
		trace.stack[slot] = trace.registers[register]

	case thumbAdjustStack:
		frame := trace.registers[RegisterSP]
		if frame.kind != leafFrame {
			return 0, false, false
		}
		amount := int32((instruction & 0x7f) * 4)
		if instruction&(1<<7) != 0 {
			amount = -amount
		}
		trace.registers[RegisterSP] = leafValue{kind: leafFrame, offset: frame.offset + amount}

	case thumbPush, thumbPop:
		if !trace.transferMultiple(decoded.form, instruction) {
			return 0, false, false
		}
		if decoded.form == thumbPop && instruction&(1<<8) != 0 {
			target, ok := trace.constantValue(trace.registers[RegisterPC].node)
			if !ok {
				return 0, false, false
			}
			if target&^1 == leafReturn&^1 {
				return 0, true, true
			}
			return target &^ 1, false, true
		}

	case thumbImmediateTransfer, thumbHalfwordTransfer:
		if !trace.transferImmediate(decoded.form, instruction) {
			return 0, false, false
		}

	case thumbRegisterTransfer:
		if !trace.transferRegister(instruction) {
			return 0, false, false
		}

	case thumbBranch:
		offset := int32(instruction&0x7ff) << 21 >> 20
		return uint32(int64(address+4) + int64(offset)), false, true

	case thumbConditionalBranch:
		condition := instruction >> 8 & 0xf
		if !trace.flagsKnown {
			return 0, false, false
		}
		taken, ok := trace.decide(condition)
		if !ok {
			return 0, false, false
		}
		if taken {
			offset := int32(int8(instruction&0xff)) * 2
			return uint32(int64(address+4) + int64(offset)), false, true
		}

	case thumbLongBranchPrefix:
		suffix, cached := trace.memory.decodedThumbFast(address + 2)
		if !cached {
			var err error
			if suffix, err = trace.memory.decodeThumb(address + 2); err != nil {
				return 0, false, false
			}
		}
		if suffix.form != thumbLongBranchSuffix {
			return 0, false, false
		}
		if trace.depth >= maxLeafDepth {
			return 0, false, false
		}
		trace.depth++
		trace.steps++
		high := int32(instruction&0x7ff) << 21 >> 9
		low := int32(uint32(suffix.instruction)&0x7ff) << 1
		target := uint32(int64(address+4) + int64(high) + int64(low))
		trace.registers[RegisterLR] = trace.constant(address + 4 + 1)
		return target, false, true

	default:
		return 0, false, false
	}
	return next, false, true
}

// decide answers a condition from flags a fold established.
func (trace *leafTrace) decide(condition uint32) (bool, bool) {
	switch condition {
	case 0: // EQ
		return trace.flagZ, true
	case 1: // NE
		return !trace.flagZ, true
	case 2: // CS/HS
		return trace.flagC, true
	case 3: // CC/LO
		return !trace.flagC, true
	case 8: // HI
		return trace.flagC && !trace.flagZ, true
	case 9: // LS
		return !trace.flagC || trace.flagZ, true
	}
	return false, false
}

// invariantWord reads a word the writer treats as constant and remembers where
// it came from, so the run can refuse a destination that covers it.
func (trace *leafTrace) invariantWord(address uint32) (uint32, bool) {
	value, err := trace.memory.readData32(address)
	if err != nil {
		return 0, false
	}
	return value, trace.remember(address)
}

func (trace *leafTrace) remember(address uint32) bool {
	for _, seen := range trace.invariants {
		if seen == address {
			return true
		}
	}
	if len(trace.invariants) >= maxLeafInvariants {
		return false
	}
	trace.invariants = append(trace.invariants, address)
	return true
}

// transferMultiple performs a push or a pop over the frame the walk models.
// The writer's own frame is never written to guest memory: it dies with the
// call, so modelling it is what lets a walk prove the registers it was handed
// come back unchanged.
func (trace *leafTrace) transferMultiple(form thumbForm, instruction uint32) bool {
	frame := trace.registers[RegisterSP]
	if frame.kind != leafFrame {
		return false
	}
	list := instruction & 0xff
	extra := instruction&(1<<8) != 0
	count := int32(0)
	for register := uint32(0); register < 8; register++ {
		if list&(1<<register) != 0 {
			count++
		}
	}
	if extra {
		count++
	}
	if form == thumbPush {
		base := frame.offset - count*4
		at := base
		for register := uint32(0); register < 8; register++ {
			if list&(1<<register) == 0 {
				continue
			}
			trace.stack[at] = trace.registers[register]
			at += 4
		}
		if extra {
			trace.stack[at] = trace.registers[RegisterLR]
		}
		trace.registers[RegisterSP] = leafValue{kind: leafFrame, offset: base}
		return true
	}
	at := frame.offset
	for register := uint32(0); register < 8; register++ {
		if list&(1<<register) == 0 {
			continue
		}
		held, ok := trace.stack[at]
		if !ok {
			return false
		}
		trace.registers[register] = held
		at += 4
	}
	if extra {
		held, ok := trace.stack[at]
		if !ok {
			return false
		}
		trace.registers[RegisterPC] = held
		at += 4
	}
	trace.registers[RegisterSP] = leafValue{kind: leafFrame, offset: at}
	return true
}

// transferImmediate performs the `[rB, #n]` loads and stores.
func (trace *leafTrace) transferImmediate(form thumbForm, instruction uint32) bool {
	var load, byteWide bool
	var offset uint32
	if form == thumbHalfwordTransfer {
		load = instruction&(1<<11) != 0
		offset = (instruction >> 6 & 0x1f) * 2
	} else {
		byteWide = instruction&(1<<12) != 0
		load = instruction&(1<<11) != 0
		offset = instruction >> 6 & 0x1f
		if !byteWide {
			offset *= 4
		}
	}
	base := trace.registers[instruction>>3&7]
	data := instruction & 7

	switch base.kind {
	case leafDestination:
		// The pixel itself: read back once, written once, and nothing else.
		if base.offset != 0 || offset != 0 || form != thumbHalfwordTransfer {
			return false
		}
		if load {
			return trace.setData(data, trace.push(leafNode{op: leafDst}))
		}
		value, ok := trace.data(data)
		if !ok || trace.stores > 0 {
			return false
		}
		trace.stored, trace.stores = value, trace.stores+1
		return true

	case leafFrame:
		slot := base.offset + int32(offset)
		if load {
			held, ok := trace.stack[slot]
			if !ok {
				return false
			}
			trace.registers[data] = held
			return true
		}
		trace.stack[slot] = trace.registers[data]
		return true

	case leafData:
		if !load {
			return false
		}
		address, ok := trace.constantValue(base.node)
		if !ok {
			return false
		}
		return trace.loadInvariant(data, address+offset, form, byteWide)
	}
	return false
}

// transferRegister performs the `[rB, rO]` loads, which is the form a writer
// blending through a lookup table reaches its table with.
func (trace *leafTrace) transferRegister(instruction uint32) bool {
	code := instruction >> 9 & 7
	offsetIn := instruction >> 6 & 7
	base := trace.registers[instruction>>3&7]
	data := instruction & 7
	if base.kind != leafData {
		return false
	}
	index, ok := trace.data(offsetIn)
	if !ok {
		return false
	}
	baseNode := base.node
	if address, constant := trace.constantValue(baseNode); constant {
		if at, folded := trace.constantValue(index); folded {
			// Both halves known, which is how the dispatch reads its own jump
			// table: one word out of memory the loop cannot move.
			switch code {
			case 4:
				value, ok := trace.invariantWord(address + at)
				if !ok {
					return false
				}
				trace.registers[data] = trace.constant(value)
				return true
			}
		}
	}
	var op leafOp
	switch code {
	case 3:
		op = leafByte
	case 5:
		op = leafHalf
	case 6:
		op = leafUnsignedByte
	default:
		// A store, or a word load whose address moves with the pixel. Neither
		// is something this can prove is only about the pixel it was handed.
		return false
	}
	return trace.setData(data, trace.push(leafNode{op: op, a: baseNode, b: index}))
}

// loadInvariant reads a value the writer treats as constant for the run.
func (trace *leafTrace) loadInvariant(data, address uint32, form thumbForm, byteWide bool) bool {
	switch {
	case form == thumbHalfwordTransfer:
		value, err := trace.memory.readData16(address)
		if err != nil || !trace.remember(address) {
			return false
		}
		trace.registers[data] = trace.constant(uint32(value))
	case byteWide:
		value, err := trace.memory.read8(address)
		if err != nil || !trace.remember(address) {
			return false
		}
		trace.registers[data] = trace.constant(uint32(value))
	default:
		value, ok := trace.invariantWord(address)
		if !ok {
			return false
		}
		trace.registers[data] = trace.constant(value)
	}
	return true
}

// compile drops everything the stored value does not depend on. A walk builds a
// node for every operation the writer performed, and after folding most of them
// are constants nothing reads; what is left is the arithmetic proper.
func (trace *leafTrace) compile() *leafProgram {
	live := make([]bool, len(trace.nodes))
	live[trace.stored] = true
	for index := len(trace.nodes) - 1; index >= 0; index-- {
		if !live[index] {
			continue
		}
		node := trace.nodes[index]
		if node.op == leafConst || node.op == leafDst || node.op == leafSrc {
			continue
		}
		if node.a >= 0 {
			live[node.a] = true
		}
		if node.b >= 0 {
			live[node.b] = true
		}
	}
	mapped := make([]int32, len(trace.nodes))
	program := &leafProgram{
		nodes:      make([]leafNode, 0, 32),
		invariants: trace.invariants,
		steps:      trace.steps,
	}
	for index, node := range trace.nodes {
		mapped[index] = -1
		if !live[index] {
			continue
		}
		if node.a >= 0 {
			node.a = mapped[node.a]
		}
		if node.b >= 0 {
			node.b = mapped[node.b]
		}
		if (node.a < 0 && node.op != leafConst && node.op != leafDst && node.op != leafSrc) ||
			(node.b < 0 && needsTwo(node.op)) {
			return nil
		}
		program.nodes = append(program.nodes, node)
		mapped[index] = int32(len(program.nodes) - 1)
	}
	program.result = mapped[trace.stored]
	if program.result < 0 {
		return nil
	}
	return program
}

func needsTwo(op leafOp) bool {
	switch op {
	case leafConst, leafDst, leafSrc:
		return false
	}
	return true
}

// evaluate runs the compiled writer for one pixel. values is the caller's
// scratch, sized to the program once rather than per pixel.
func (program *leafProgram) evaluate(memory *Memory, values []uint32, destination uint16, colour uint32) (uint16, error) {
	for index := range program.nodes {
		node := &program.nodes[index]
		var value uint32
		switch node.op {
		case leafConst:
			value = node.k
		case leafDst:
			value = uint32(destination)
		case leafSrc:
			value = colour
		case leafAnd:
			value = values[node.a] & values[node.b]
		case leafOrr:
			value = values[node.a] | values[node.b]
		case leafEor:
			value = values[node.a] ^ values[node.b]
		case leafAdd:
			value = values[node.a] + values[node.b]
		case leafSub:
			value = values[node.a] - values[node.b]
		case leafMul:
			value = values[node.a] * values[node.b]
		case leafLsl:
			if shift := values[node.b]; shift < 32 {
				value = values[node.a] << shift
			}
		case leafLsr:
			if shift := values[node.b]; shift < 32 {
				value = values[node.a] >> shift
			}
		case leafAsr:
			shift := values[node.b]
			if shift >= 32 {
				shift = 31
			}
			value = uint32(int32(values[node.a]) >> shift)
		case leafByte:
			loaded, err := memory.read8(values[node.a] + values[node.b])
			if err != nil {
				return 0, err
			}
			value = uint32(int32(int8(loaded)))
		case leafUnsignedByte:
			loaded, err := memory.read8(values[node.a] + values[node.b])
			if err != nil {
				return 0, err
			}
			value = uint32(loaded)
		case leafHalf:
			loaded, err := memory.readData16(values[node.a] + values[node.b])
			if err != nil {
				return 0, err
			}
			value = uint32(loaded)
		}
		values[index] = value
	}
	return uint16(values[program.result]), nil
}

// maxBlendArmBytes bounds the block the flag sends the guest to. It is the same
// draw as the fall-through with the store replaced by a call, and the one
// measured is eighteen bytes.
const maxBlendArmBytes = 32

// blendedDraw is a per-pixel writer reached from a blit's blending arm.
type blendedDraw struct {
	program *leafProgram
	// steps is what one blended pixel retires: the chain that tested the flag,
	// the arm, the call and everything inside it, and the tail.
	steps uint32
}

// analyseBlendedDraw reads the arm a set flag sends the guest to, and the
// writer it calls.
//
// The arm is the fall-through's own draw with its store replaced by a call, so
// what it has to prove is that it draws the same pixel from the same source
// through the same palette, hands the writer the destination and the colour in
// that order, and rejoins the body where the destination is advanced — the one
// place a rejoin leaves the loop's own arithmetic intact.
func (memory *Memory) analyseBlendedDraw(loop *clippedBlit) *blendedDraw {
	return memory.analyseBlendArm(blendArm{
		armTarget: loop.armTarget,
		rejoin:    loop.tailAt - 6,
		branch:    loop.branch,
		record:    loop.record,
		colours:   loop.colours,
		slot:      loop.slot,
		source:    loop.source,
		held:      [4]uint32{loop.source, loop.index, loop.count, loop.limit},
		// Ten instructions test the clip twice and the flag once before the
		// arm is reached.
		lead:      10,
		writeBack: true,
	})
}

// blendArm is what an arm walk needs of the loop it belongs to. The two blits
// that have one differ in their tails and in nothing the arm itself reads, so
// the walk takes this rather than either shape.
type blendArm struct {
	armTarget uint32
	rejoin    uint32
	branch    uint32
	record    uint32
	colours   uint32
	slot      uint32
	source    uint32
	held      [4]uint32
	lead      uint32
	// writeBack says the rejoin is the destination's own three-instruction
	// advance, which the clipped shape's is and the spilled shape's is not:
	// there the counter and the source step through the same block.
	writeBack bool
}

func (memory *Memory) analyseBlendArm(loop blendArm) *blendedDraw {
	// Where the arm rejoins the body. **Read rather than assumed** — the
	// destination advances there and nowhere else, so an arm that rejoins
	// anywhere else is one whose pixels do not land where this would put them.
	rejoin := loop.rejoin
	if rejoin == 0 || rejoin >= loop.branch {
		return nil
	}
	if loop.writeBack && !memory.isWriteBack(rejoin, loop.slot) {
		return nil
	}

	var (
		recordIn, tableIn, indexIn, colourIn uint32
		haveRecord, haveIndex, haveTable     bool
		haveShift, haveColour, haveDest      bool
		steps                                uint32
	)
	written := uint32(0)
	claim := func(register uint32) bool {
		// Nothing the loop maintains may be written by the arm, or the
		// iterations after this one would walk somewhere else.
		for _, held := range loop.held {
			if register == held {
				return false
			}
		}
		written |= 1 << register
		return true
	}

	address := loop.armTarget
	for {
		if address-loop.armTarget > maxBlendArmBytes {
			return nil
		}
		decoded, cached := memory.decodedThumbFast(address)
		if !cached {
			var err error
			if decoded, err = memory.decodeThumb(address); err != nil {
				return nil
			}
		}
		instruction := uint32(decoded.instruction)
		steps++

		switch decoded.form {
		case thumbStackRelativeTransfer:
			if instruction&(1<<11) == 0 {
				return nil
			}
			data := instruction >> 8 & 7
			offset := (instruction & 0xff) * 4
			if !claim(data) {
				return nil
			}
			switch offset {
			case loop.record:
				haveRecord, recordIn = true, data
			case loop.slot:
				// The destination is the writer's first argument.
				if data != 0 {
					return nil
				}
				haveDest = true
			default:
				return nil
			}

		case thumbImmediateTransfer:
			load := instruction&(1<<11) != 0
			byteWide := instruction&(1<<12) != 0
			offset := instruction >> 6 & 0x1f
			base := instruction >> 3 & 7
			data := instruction & 7
			if !load || !claim(data) {
				return nil
			}
			if byteWide {
				if offset != 0 || base != loop.source || haveIndex {
					return nil
				}
				haveIndex, indexIn = true, data
				break
			}
			if !haveRecord || base != recordIn || offset*4 != loop.colours || haveTable {
				return nil
			}
			haveTable, tableIn = true, data

		case thumbShift:
			if instruction>>11&3 != 0 || instruction>>6&0x1f != 1 {
				return nil
			}
			if !haveIndex || haveShift ||
				instruction>>3&7 != indexIn || instruction&7 != indexIn {
				return nil
			}
			if !claim(indexIn) {
				return nil
			}
			haveShift = true

		case thumbRegisterTransfer:
			if instruction>>9&7 != 5 || !haveShift || !haveTable || haveColour {
				return nil
			}
			base, offsetIn := instruction>>3&7, instruction>>6&7
			if !(base == indexIn && offsetIn == tableIn) && !(base == tableIn && offsetIn == indexIn) {
				return nil
			}
			data := instruction & 7
			// The colour is the writer's second argument.
			if data != 1 || !claim(data) {
				return nil
			}
			haveColour, colourIn = true, data

		case thumbLongBranchPrefix:
			if !haveColour || !haveDest {
				return nil
			}
			suffix, cached := memory.decodedThumbFast(address + 2)
			if !cached {
				var err error
				if suffix, err = memory.decodeThumb(address + 2); err != nil {
					return nil
				}
			}
			if suffix.form != thumbLongBranchSuffix {
				return nil
			}
			steps++
			high := int32(instruction&0x7ff) << 21 >> 9
			low := int32(uint32(suffix.instruction)&0x7ff) << 1
			entry := uint32(int64(address+4) + int64(high) + int64(low))
			// And the branch back into the body, which has to land on the
			// write-back or the loop's own arithmetic would run twice.
			after, cached := memory.decodedThumbFast(address + 4)
			if !cached {
				var err error
				if after, err = memory.decodeThumb(address + 4); err != nil {
					return nil
				}
			}
			if after.form != thumbBranch {
				return nil
			}
			steps++
			offset := int32(uint32(after.instruction)&0x7ff) << 21 >> 20
			if uint32(int64(address+8)+int64(offset)) != rejoin {
				return nil
			}
			program := memory.walkLeaf(entry, colourIn)
			if program == nil {
				return nil
			}
			tail := (loop.branch-rejoin)/2 + 1
			return &blendedDraw{
				program: program,
				steps:   loop.lead + steps + program.steps + tail,
			}

		default:
			return nil
		}
		address += 2
	}
}

// runBlendedClipped stands in for the blending form of a clipped blit.
//
// It differs from the plain form in one thing besides the writer, and that one
// thing is what makes it safe: **the last iteration is left to the
// interpreter**. The writer clobbers registers the body reads back, and
// reproducing each of them by hand is the class of mistake that shows up as one
// title's shadows being wrong. Running all but one iteration and leaving the
// loop where it stood costs nothing and makes every one of them right for free —
// the same trade `byte_blend.go` and `word_modulate.go` make.
func (memory *Memory) runBlendedClipped(context *Context, loop *clippedBlit, draw *blendedDraw, low, high uint32) (uint32, error) {
	remaining := int64(int32(context.Registers[loop.limit])) - int64(int32(context.Registers[loop.count]))
	if remaining < 3 || remaining > maxStoreLoopIterations {
		return 0, nil
	}
	index := int64(int32(context.Registers[loop.index]))
	start := clampSpan(int64(int32(low))-index, remaining)
	end := clampSpan(int64(int32(high))-index, remaining)
	if end < start {
		end = start
	}
	// **The iteration left to the interpreter has to be one that draws.**
	// Leaving the run's last iteration is what makes the registers the writer
	// clobbers right for free — but only if that iteration calls the writer,
	// and the last iteration of a run that ends outside the clip does not. So
	// the stand-in stops one short of the last *drawn* pixel and leaves
	// everything after it, skipped pixels included, to the interpreter. Two
	// runs of the same blit differing only in where the clip fell is what
	// found this, and it showed as the writer's own two registers.
	run := end - 1
	drawn := run - start
	if drawn < 2 || run < 2 {
		return 0, nil
	}

	stack := context.Registers[RegisterSP]
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
	pixels := uint32(drawn)

	span := uint64(pixels) * 2
	// An odd destination would have the writer's halfword accesses align
	// downward, which is not what a pixel at that address means.
	if destination&1 != 0 {
		return 0, nil
	}
	if err := memory.validateLocked(source+uint32(start), uint64(pixels), PermissionRead, "read"); err != nil {
		return 0, nil
	}
	if err := memory.validateLocked(destination, span, PermissionWrite, "write"); err != nil {
		return 0, nil
	}
	// Everything read once and treated as constant for the run, the writer's
	// own invariants included: its module base, its mode, its jump table, its
	// masks and whatever it blends by. A blit that lands on one of them is a
	// blit whose later pixels would not do what its first one did.
	overlaps := func(address uint32) bool {
		return uint64(address)+4 > uint64(destination) && uint64(address) < uint64(destination)+span
	}
	if overlaps(slotAddress) || overlaps(stack+loop.record) || overlaps(record+loop.colours) {
		return 0, nil
	}
	if overlaps(stack+loop.lowSlot) || overlaps(stack+loop.highSlot) || overlaps(stack+loop.flagSlot) {
		return 0, nil
	}
	for _, at := range draw.program.invariants {
		if overlaps(at) {
			return 0, nil
		}
	}

	values := memory.leafValues(len(draw.program.nodes))
	var colours [256]uint16
	var known [256]bool
	from := source + uint32(start)
	for pixel := uint32(0); pixel < pixels; pixel++ {
		index, err := memory.read8(from + pixel)
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
		at := destination + pixel*2
		was, err := memory.readData16(at)
		if err != nil {
			return 0, err
		}
		value, err := draw.program.evaluate(memory, values, was, uint32(colours[index]))
		if err != nil {
			return 0, err
		}
		if err := memory.writeData16(at, value); err != nil {
			return 0, err
		}
	}
	if err := memory.writeData32(slotAddress, destination+pixels*2); err != nil {
		return 0, err
	}

	// Only what the loop itself maintains. Everything the body and the writer
	// touch is left to the iteration the interpreter is about to run, and the
	// PC is already the loop's head because the branch that brought us here
	// took it there.
	registers := &context.Registers
	registers[loop.source] = source + uint32(run)
	registers[loop.index] = uint32(index + run)
	registers[loop.count] = uint32(int64(int32(registers[loop.count])) + run)

	// Every iteration this stood in for either fell off the left of the clip or
	// drew; the ones past the right of it were left behind.
	return uint32(start)*loop.lowSteps + pixels*draw.steps, nil
}

// leafValues answers a scratch big enough for one writer's program. It lives on
// the memory rather than the stand-in because a stand-in runs once a row and an
// allocation there is an allocation per row.
func (memory *Memory) leafValues(size int) []uint32 {
	if cap(memory.leafScratch) < size {
		memory.leafScratch = make([]uint32, size)
	}
	return memory.leafScratch[:size]
}

// isWriteBack answers whether the three instructions at address are the
// destination's own advance: `ldr rD, [sp, #n]; adds rD, #2; str rD, [sp, #n]`.
func (memory *Memory) isWriteBack(address, slot uint32) bool {
	read := func(at uint32) (thumbForm, uint32, bool) {
		decoded, cached := memory.decodedThumbFast(at)
		if !cached {
			var err error
			if decoded, err = memory.decodeThumb(at); err != nil {
				return thumbUndecoded, 0, false
			}
		}
		return decoded.form, uint32(decoded.instruction), true
	}
	form, instruction, ok := read(address)
	if !ok || form != thumbStackRelativeTransfer || instruction&(1<<11) == 0 ||
		(instruction&0xff)*4 != slot {
		return false
	}
	register := instruction >> 8 & 7
	form, instruction, ok = read(address + 2)
	if !ok || form != thumbImmediate || instruction>>11&3 != 2 ||
		instruction>>8&7 != register || instruction&0xff != 2 {
		return false
	}
	form, instruction, ok = read(address + 4)
	if !ok || form != thumbStackRelativeTransfer || instruction&(1<<11) != 0 ||
		instruction>>8&7 != register || (instruction&0xff)*4 != slot {
		return false
	}
	return true
}

// analyseBlendedSpilled reads the same arm on the blit that keeps its
// destination in a frame slot. It differs from the clipped shape in its tail —
// the counter comes down and the source steps in the same block the destination
// advances in, so the rejoin is not the three instructions `isWriteBack` reads —
// and in having no clip, so every iteration draws.
func (memory *Memory) analyseBlendedSpilled(loop *spilledBlit) *blendedDraw {
	if loop.storeAt == 0 {
		return nil
	}
	return memory.analyseBlendArm(blendArm{
		armTarget: loop.armTarget,
		rejoin:    loop.storeAt + 2,
		branch:    loop.branch,
		record:    loop.record,
		colours:   loop.colours,
		slot:      loop.slot,
		source:    loop.source,
		held:      [4]uint32{loop.source, loop.counter, loop.counter, loop.counter},
		// Three instructions read the flag and test it before the arm.
		lead: 3,
	})
}

// runBlendedSpilled stands in for the blending form of the unclipped blit.
// Every iteration draws, so what it leaves to the interpreter is simply the
// last one — the same trade the clipped form makes, for the same reason.
func (memory *Memory) runBlendedSpilled(context *Context, loop *spilledBlit, draw *blendedDraw) (uint32, error) {
	iterations := context.Registers[loop.counter]
	if iterations > maxStoreLoopIterations || iterations < 3 {
		return 0, nil
	}
	run := iterations - 1

	stack := context.Registers[RegisterSP]
	slotAddress := stack + loop.slot
	destination, err := memory.readData32(slotAddress)
	if err != nil {
		return 0, nil
	}
	recordAddress := stack + loop.record
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

	if destination&1 != 0 {
		return 0, nil
	}
	if err := memory.validateLocked(source, uint64(run), PermissionRead, "read"); err != nil {
		return 0, nil
	}
	span := uint64(run) * 2
	if err := memory.validateLocked(destination, span, PermissionWrite, "write"); err != nil {
		return 0, nil
	}
	overlaps := func(address uint32) bool {
		return uint64(address)+4 > uint64(destination) && uint64(address) < uint64(destination)+span
	}
	if overlaps(slotAddress) || overlaps(recordAddress) || overlaps(tableAddress) {
		return 0, nil
	}
	if loop.haveGuard && overlaps(context.Registers[loop.guardBase]+loop.guardOffset) {
		return 0, nil
	}
	for _, at := range draw.program.invariants {
		if overlaps(at) {
			return 0, nil
		}
	}

	values := memory.leafValues(len(draw.program.nodes))
	var colours [256]uint16
	var known [256]bool
	for pixel := uint32(0); pixel < run; pixel++ {
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
		at := destination + pixel*2
		was, err := memory.readData16(at)
		if err != nil {
			return 0, err
		}
		value, err := draw.program.evaluate(memory, values, was, uint32(colours[index]))
		if err != nil {
			return 0, err
		}
		if err := memory.writeData16(at, value); err != nil {
			return 0, err
		}
	}
	if err := memory.writeData32(slotAddress, destination+run*2); err != nil {
		return 0, err
	}

	registers := &context.Registers
	registers[loop.source] = source + run
	registers[loop.counter] = iterations - run
	return run * draw.steps, nil
}

// The ninth shape: a counted halfword fill behind the same flag.
//
// `armcore.md`, "The seventh was built, measured, and is not kept", is about
// this loop. A recogniser for its *plain* form was built and thrown away
// because the form almost never runs: the analyser accepted the loop 13.9
// million times and the stand-in ran 78 thousand of them, because this fill
// spends its life in its blending arm — the one a stand-in could not follow.
//
// Following it is what changed. The arm draws one pixel through the same call
// `blended_blit.go` already compiles, so the shape is now worth recognising for
// the reason it was not before, and **both forms are stood in for rather than
// one**: a decline is what made the seventh shape cost more than it saved, and
// there is no decline left when the recogniser answers for the flag set and
// clear alike.
//
// It is the busiest loop in the title by a distance — 19.4 million closings and
// 28% of every instruction a replay of the reported route retires.

// maxFlaggedFillBytes bounds this body; the one measured is twenty.
const maxFlaggedFillBytes = 40

// flaggedFill is a halfword fill whose store is the fall-through of a flag.
type flaggedFill struct {
	head      uint32
	branch    uint32
	armTarget uint32
	rejoin    uint32
	// The flag, read through a register the body cannot move.
	guardBase   uint32
	guardOffset uint32
	// The store: a value the loop does not compute, through a pointer it
	// advances by one pixel.
	value       uint32
	destination uint32
	// The ending: a counter the body takes down, against a register it builds
	// minus one in.
	counter uint32
	// steps is what one iteration of the plain form retires.
	steps uint32
	// lead is the instructions before the arm, and tail the ones after it.
	lead uint32
	tail uint32
}

// analyseFlaggedFill reads the body between head and branchPC and answers the
// fill it is, or nil for one it cannot prove.
//
// The first four instructions are read by position because each depends on the
// one before it — a flag is a load, a test of that load, and a branch on that
// test, and the store is what the branch falls through to. The tail is a role
// walk: the counter, the pointer, the constant the ending compares against and
// that comparison, each exactly once and none of them twice.
func (memory *Memory) analyseFlaggedFill(head, branchPC uint32) *flaggedFill {
	if branchPC <= head || branchPC-head > maxFlaggedFillBytes || branchPC-head < 16 {
		return nil
	}
	read := func(at uint32) (thumbForm, uint32, bool) {
		decoded, cached := memory.decodedThumbFast(at)
		if !cached {
			var err error
			if decoded, err = memory.decodeThumb(at); err != nil {
				return thumbUndecoded, 0, false
			}
		}
		return decoded.form, uint32(decoded.instruction), true
	}
	fill := &flaggedFill{head: head, branch: branchPC}
	var written [8]int

	// `ldr rF, [rB, #n]` — the flag.
	form, instruction, ok := read(head)
	if !ok || form != thumbImmediateTransfer ||
		instruction&(1<<12) != 0 || instruction&(1<<11) == 0 {
		return nil
	}
	flagIn := instruction & 7
	fill.guardBase, fill.guardOffset = instruction>>3&7, (instruction>>6&0x1f)*4
	written[flagIn]++

	// `cmp rF, #0`.
	form, instruction, ok = read(head + 2)
	if !ok || form != thumbImmediate || instruction>>11&3 != 1 ||
		instruction>>8&7 != flagIn || instruction&0xff != 0 {
		return nil
	}
	// `bne arm`, which has to leave the body.
	form, instruction, ok = read(head + 4)
	if !ok || form != thumbConditionalBranch || instruction>>8&0xf != 1 {
		return nil
	}
	target := uint32(int64(head+8) + int64(int8(instruction&0xff))*2)
	if target >= head && target <= branchPC {
		return nil
	}
	fill.armTarget = target

	// `strh rV, [rD]` — the pixel.
	form, instruction, ok = read(head + 6)
	if !ok || form != thumbHalfwordTransfer || instruction&(1<<11) != 0 ||
		instruction>>6&0x1f != 0 {
		return nil
	}
	fill.value, fill.destination = instruction&7, instruction>>3&7
	fill.rejoin = head + 8

	var (
		scratch                       uint32
		haveOne, haveNegate, haveStep bool
		haveCount                     bool
	)
	// The compare and the branch that close the loop are read after this, so
	// the walk stops short of them.
	for address := head + 8; address < branchPC-2; address += 2 {
		form, instruction, ok := read(address)
		if !ok {
			return nil
		}
		switch form {
		case thumbImmediate:
			register := instruction >> 8 & 7
			value := instruction & 0xff
			switch instruction >> 11 & 3 {
			case 0: // MOVS — the one the ending's minus one is built from.
				if haveOne || value != 1 {
					return nil
				}
				haveOne, scratch = true, register
				written[register]++
			case 2: // ADDS — the pointer, one pixel on.
				if haveStep || register != fill.destination || value != 2 {
					return nil
				}
				haveStep = true
				written[register]++
			case 3: // SUBS — the counter.
				if haveCount || value != 1 {
					return nil
				}
				haveCount, fill.counter = true, register
				written[register]++
			default:
				return nil
			}

		case thumbALU:
			// `rsbs rZ, rZ, #0`, which is how the ending's minus one is made.
			if instruction>>6&0xf != 9 || haveNegate || !haveOne {
				return nil
			}
			if instruction&7 != scratch || instruction>>3&7 != scratch {
				return nil
			}
			haveNegate = true
			written[scratch]++

		case thumbAddSubtract:
			// The same two steps, spelled with a register operand.
			return nil

		default:
			return nil
		}
	}
	// `cmp rC, rZ` and the branch back are the last two, and the walk above
	// read the compare as part of the tail only if it is that shape.
	form, instruction, ok = read(branchPC - 2)
	if !ok || form != thumbALU || instruction>>6&0xf != 10 {
		return nil
	}
	if instruction&7 != fill.counter || instruction>>3&7 != scratch {
		return nil
	}
	form, instruction, ok = read(branchPC)
	if !ok || form != thumbConditionalBranch || instruction>>8&0xf != 1 {
		return nil
	}
	if !haveOne || !haveNegate || !haveStep || !haveCount {
		return nil
	}
	// Nothing the loop treats as fixed may be something it writes.
	if written[fill.value] != 0 || written[fill.guardBase] != 0 {
		return nil
	}
	if fill.value == fill.destination || fill.counter == fill.destination {
		return nil
	}
	fill.steps = (branchPC-head)/2 + 1
	fill.lead = 3
	fill.tail = (branchPC-fill.rejoin)/2 + 1
	return fill
}

// maxFillArmSteps bounds the block a set flag sends the fill to.
const maxFillArmSteps = 6

// fillArm is the arm of a flagged fill: a handful of moves and shifts that put
// the pixel in the writer's first argument and a colour in its second, and the
// call. Everything it reads is loop-invariant, so the colour is computed once.
type fillArm struct {
	// from and to bound the instructions before the call, which the run
	// replays over the loop's own registers to get the colour.
	from, to uint32
	program  *leafProgram
	// steps is what one blended pixel retires, the call included.
	steps uint32
}

// analyseFillArm reads that block. The three forms it allows are the three the
// compiler emits to move a value into place — a high-register move, an `adds
// rD, rS, #0`, and a shift by a constant — and nothing else, because anything
// else could read something the loop moves.
func (memory *Memory) analyseFillArm(fill *flaggedFill) *fillArm {
	steps := uint32(0)
	for address := fill.armTarget; ; address += 2 {
		if steps > maxFillArmSteps {
			return nil
		}
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
			if instruction>>8&3 != 2 {
				return nil
			}
			to := instruction&7 | instruction>>4&8
			from := instruction >> 3 & 15
			if to > 7 || from == RegisterPC || from == RegisterSP {
				return nil
			}

		case thumbAddSubtract:
			// `adds rD, rS, #0`, which is this compiler's move.
			if instruction&(1<<10) == 0 || instruction&(1<<9) != 0 || instruction>>6&7 != 0 {
				return nil
			}

		case thumbShift:

		case thumbLongBranchPrefix:
			suffix, cached := memory.decodedThumbFast(address + 2)
			if !cached {
				var err error
				if suffix, err = memory.decodeThumb(address + 2); err != nil {
					return nil
				}
			}
			if suffix.form != thumbLongBranchSuffix {
				return nil
			}
			high := int32(instruction&0x7ff) << 21 >> 9
			low := int32(uint32(suffix.instruction)&0x7ff) << 1
			entry := uint32(int64(address+4) + int64(high) + int64(low))
			after, cached := memory.decodedThumbFast(address + 4)
			if !cached {
				var err error
				if after, err = memory.decodeThumb(address + 4); err != nil {
					return nil
				}
			}
			if after.form != thumbBranch {
				return nil
			}
			offset := int32(uint32(after.instruction)&0x7ff) << 21 >> 20
			if uint32(int64(address+8)+int64(offset)) != fill.rejoin {
				return nil
			}
			program := memory.walkLeaf(entry, 1)
			if program == nil {
				return nil
			}
			return &fillArm{
				from:    fill.armTarget,
				to:      address,
				program: program,
				// The moves, the call's two halfwords, the branch back, and
				// the flag's own three instructions and the tail around them.
				steps: fill.lead + steps + 2 + 1 + program.steps + fill.tail,
			}

		default:
			return nil
		}
		steps++
	}
}

// arguments replays the arm's moves over the loop's registers and answers what
// the writer would have been handed. The instructions are the three forms
// analyseFillArm allows and nothing else, so this is the whole of their
// meaning.
func (arm *fillArm) arguments(memory *Memory, context *Context) (uint32, uint32, bool) {
	var registers [16]uint32
	copy(registers[:], context.Registers[:])
	for address := arm.from; address < arm.to; address += 2 {
		decoded, cached := memory.decodedThumbFast(address)
		if !cached {
			var err error
			if decoded, err = memory.decodeThumb(address); err != nil {
				return 0, 0, false
			}
		}
		instruction := uint32(decoded.instruction)
		switch decoded.form {
		case thumbHighRegister:
			registers[instruction&7|instruction>>4&8] = registers[instruction>>3&15]
		case thumbAddSubtract:
			registers[instruction&7] = registers[instruction>>3&7]
		case thumbShift:
			amount := instruction >> 6 & 0x1f
			value := registers[instruction>>3&7]
			switch instruction >> 11 & 3 {
			case 0:
				registers[instruction&7] = value << amount
			case 1:
				if amount == 0 {
					registers[instruction&7] = 0
				} else {
					registers[instruction&7] = value >> amount
				}
			default:
				if amount == 0 {
					amount = 32
				}
				if amount >= 32 {
					amount = 31
				}
				registers[instruction&7] = uint32(int32(value) >> amount)
			}
		default:
			return 0, 0, false
		}
	}
	return registers[0], registers[1], true
}

// runFlaggedFill stands in for the fill, in whichever form the flag selects.
// Both are answered rather than one, which is what keeps the loop out of the
// refusal that made the shape before this one cost more than it saved.
func (memory *Memory) runFlaggedFill(context *Context, fill *flaggedFill) (uint32, error) {
	// The counter has already come down for the iteration that reached the
	// branch, and the loop ends when it reaches minus one, so what it holds is
	// one less than what is left.
	remaining := uint64(context.Registers[fill.counter]) + 1
	if remaining > maxStoreLoopIterations || remaining < 3 {
		return 0, nil
	}
	// The last iteration is the interpreter's, which is what makes the
	// registers the writer clobbers right for free.
	run := uint32(remaining - 1)

	flagAddress := context.Registers[fill.guardBase] + fill.guardOffset
	flag, err := memory.readData32(flagAddress)
	if err != nil {
		return 0, nil
	}

	destination := context.Registers[fill.destination]
	if destination&1 != 0 {
		return 0, nil
	}
	span := uint64(run) * 2
	if err := memory.validateLocked(destination, span, PermissionWrite, "write"); err != nil {
		return 0, nil
	}
	overlaps := func(address uint32) bool {
		return uint64(address)+4 > uint64(destination) && uint64(address) < uint64(destination)+span
	}
	if overlaps(flagAddress) {
		return 0, nil
	}

	charge := run * fill.steps
	if flag == 0 {
		// The plain form: the same halfword, over and over.
		value := uint16(context.Registers[fill.value])
		for pixel := uint32(0); pixel < run; pixel++ {
			if err := memory.writeData16(destination+pixel*2, value); err != nil {
				return 0, err
			}
		}
	} else {
		arm := memory.analyseFillArm(fill)
		if arm == nil {
			return 0, nil
		}
		for _, at := range arm.program.invariants {
			if overlaps(at) {
				return 0, nil
			}
		}
		pointer, colour, ok := arm.arguments(memory, context)
		if !ok || pointer != destination {
			return 0, nil
		}
		values := memory.leafValues(len(arm.program.nodes))
		for pixel := uint32(0); pixel < run; pixel++ {
			at := destination + pixel*2
			was, err := memory.readData16(at)
			if err != nil {
				return 0, err
			}
			value, err := arm.program.evaluate(memory, values, was, colour)
			if err != nil {
				return 0, err
			}
			if err := memory.writeData16(at, value); err != nil {
				return 0, err
			}
		}
		charge = run * arm.steps
	}

	registers := &context.Registers
	registers[fill.destination] = destination + run*2
	registers[fill.counter] -= run
	return charge, nil
}
