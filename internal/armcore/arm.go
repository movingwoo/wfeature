package armcore

import (
	"fmt"
	"math/bits"
)

// An ARM instruction is classified once and executed many times, the same way
// a Thumb one is. classifyARM answers which form an encoding takes and Memory
// caches that answer per address (see decodeARM), so the match chain below is
// walked once per instruction in the program rather than once per execution.
//
// The chain is thirteen mask-and-compare tests deep and the forms games spend
// their time in — data processing, single transfer — are at the bottom of it.
// On the one local title that is 100% ARM that was the whole cost of a step
// after the fetch; see docs/armcore.md.
type armForm uint8

const (
	// armUndecoded is the zero value, so a fresh cache slot reads as "not
	// decoded yet" without anything having to clear it.
	armUndecoded armForm = iota
	// armUndefined is a conditional encoding this engine does not implement:
	// it faults, but only if its condition passes. armUnconditionalUndefined
	// is one of the 1111 encodings, which are not conditional at all and
	// therefore fault whatever the flags say. Neither is ever cached — the
	// slot is left undecoded so the fault is reported every time rather than
	// remembered as a form — but both are named because the dispatcher has to
	// tell them apart.
	armUndefined
	armUnconditionalUndefined
	armBLXImmediate
	armBranchExchange
	armSwap
	armStatusTransfer
	armLongMultiply
	armMultiply
	armHalfwordSignedTransfer
	armBranch
	armSupervisorCall
	armSingleTransfer
	armBlockTransfer
	armDataProcessing
	armCacheMaintenance
)

// classifyARM answers the form of an encoding. The order of the tests is the
// architecture's own and is not free to change: several of the masks overlap,
// and a multiply only reads as a multiply because the transfer tests below it
// have not been asked yet.
func classifyARM(instruction uint32) armForm {
	if instruction>>28 == 0xf {
		// The 1111 encodings are unconditional rather than never-executed, and
		// the only one a game here uses is BLX immediate: the ARM-to-Thumb call
		// a Thumb-compiled module makes into its own code. LGT modules are
		// built that way, so every LGT title stops at the first one.
		if instruction&0x0e000000 == 0x0a000000 {
			return armBLXImmediate
		}
		return armUnconditionalUndefined
	}
	switch {
	case instruction&0x0ffffff0 == 0x012fff10: // BX Rm
		return armBranchExchange
	case instruction&0x0fb00ff0 == 0x01000090: // SWP/SWPB
		return armSwap
	case instruction&0x0fb00000 == 0x03200000, // MSR immediate
		instruction&0x0f900ff0 == 0x01000000: // MRS/MSR register
		return armStatusTransfer
	case instruction&0x0f8000f0 == 0x00800090: // UMULL/UMLAL/SMULL/SMLAL
		return armLongMultiply
	case instruction&0x0fc000f0 == 0x00000090: // MUL/MLA
		return armMultiply
	case instruction&0x0e400f90 == 0x00000090, // halfword/signed, register offset
		instruction&0x0e400090 == 0x00400090: // halfword/signed, immediate offset
		return armHalfwordSignedTransfer
	case instruction&0x0e000000 == 0x0a000000: // B/BL
		return armBranch
	case instruction&0x0f000000 == 0x0f000000: // SVC/SWI
		return armSupervisorCall
	case instruction&0x0c000000 == 0x04000000: // LDR/STR
		return armSingleTransfer
	case instruction&0x0e000000 == 0x08000000: // LDM/STM
		return armBlockTransfer
	case instruction&0x0c000000 == 0: // data processing
		return armDataProcessing
	case isCacheMaintenance(instruction):
		return armCacheMaintenance
	}
	return armUndefined
}

// executeARM classifies and executes one instruction. It is the uncached path:
// the engine reaches the same handlers through executeARMForm with a form the
// decode cache already holds, and the two cannot answer differently because
// this one is that one with a classify in front.
func executeARM(context *Context, memory *Memory, pc, instruction uint32) (SupervisorCall, error) {
	return executeARMForm(classifyARM(instruction), context, memory, pc, instruction)
}

// executeARMForm executes an already-classified instruction.
//
// The condition test lives here rather than in the classifier because it reads
// flags, which change under an encoding that does not: a cached form says what
// an instruction *is*, never whether this execution of it runs.
func executeARMForm(form armForm, context *Context, memory *Memory, pc, instruction uint32) (SupervisorCall, error) {
	switch form {
	case armBLXImmediate:
		offset := int32(instruction<<8) >> 6
		// The H bit is the odd halfword: a BLX target is halfword-aligned
		// where a BL target is word-aligned.
		halfword := instruction >> 24 & 1
		context.Registers[RegisterLR] = pc + 4
		context.setThumbPC(uint32(int64(pc+8)+int64(offset)) + halfword*2)
		return noSupervisorCall, nil
	case armUnconditionalUndefined:
		return noSupervisorCall, ErrUndefinedInstruction
	}
	if !conditionPassed(context.CPSR, instruction>>28) {
		return noSupervisorCall, nil
	}
	switch form {
	case armDataProcessing:
		return noSupervisorCall, executeARMDataProcessing(context, pc, instruction)
	case armSingleTransfer:
		return noSupervisorCall, executeARMSingleTransfer(context, memory, pc, instruction)
	case armBranch:
		executeARMBranch(context, pc, instruction)
		return noSupervisorCall, nil
	case armBlockTransfer:
		return noSupervisorCall, executeARMBlockTransfer(context, memory, pc, instruction)
	case armBranchExchange:
		executeARMBranchExchange(context, pc, instruction)
		return noSupervisorCall, nil
	case armHalfwordSignedTransfer:
		return noSupervisorCall, executeARMHalfwordSignedTransfer(context, memory, pc, instruction)
	case armMultiply:
		return noSupervisorCall, executeARMMultiply(context, pc, instruction)
	case armLongMultiply:
		return noSupervisorCall, executeARMLongMultiply(context, instruction)
	case armSupervisorCall:
		return SupervisorCall{Immediate: instruction & 0x00ffffff, Address: pc, ResumePC: pc + 4}, nil
	case armStatusTransfer:
		return noSupervisorCall, executeARMProgramStatusTransfer(context, instruction)
	case armSwap:
		return noSupervisorCall, executeARMSwap(context, memory, instruction)
	case armCacheMaintenance:
		// A write to CP15 c7 is cache or write-buffer maintenance, which is
		// nothing here: there is no cache to flush, and every path that
		// changes a page's bytes already drops that page's decode cache, so
		// self-modifying code is coherent whether the guest asks or not.
		// Modules do this right after copying code into RAM, which is why
		// ignoring it is correct rather than merely convenient.
		return noSupervisorCall, nil
	}
	return noSupervisorCall, ErrUndefinedInstruction
}

// executeARMBranch is B and BL. It is a function rather than an arm of the
// dispatcher above because the engine routes it directly, and the two must not
// be two copies of the semantics.
func executeARMBranch(context *Context, pc, instruction uint32) {
	offset := int32(instruction<<8) >> 6
	if instruction&(1<<24) != 0 {
		context.Registers[RegisterLR] = pc + 4
	}
	context.setARMPC(uint32(int64(pc+8) + int64(offset)))
}

// executeARMBranchExchange is BX Rm, which in ARM code is mostly the return
// from a call: a module built for interworking leaves every function through
// one, so it is a seventh of the instructions the local ARM title retires.
func executeARMBranchExchange(context *Context, pc, instruction uint32) {
	context.branchExchange(armRegisterValue(context, pc, instruction&0xf))
}

// isCacheMaintenance reports whether an instruction is an MCR to CP15 c7.
// Only that one coprocessor register is answered: the rest of CP15 reports
// what the hardware is, and a made-up answer to an ID or control register
// would be a wrong answer rather than a missing one.
func isCacheMaintenance(instruction uint32) bool {
	const (
		coprocessorRegisterTransfer = 0x0e000010
		transferMask                = 0x0f000010
	)
	if instruction&transferMask != coprocessorRegisterTransfer {
		return false
	}
	load := instruction>>20&1 != 0
	coprocessor := instruction >> 8 & 0xf
	register := instruction >> 16 & 0xf
	return !load && coprocessor == 15 && register == 7
}

func conditionPassed(cpsr, condition uint32) bool {
	n := cpsr&flagNegative != 0
	z := cpsr&flagZero != 0
	c := cpsr&flagCarry != 0
	v := cpsr&flagOverflow != 0
	switch condition {
	case 0x0:
		return z
	case 0x1:
		return !z
	case 0x2:
		return c
	case 0x3:
		return !c
	case 0x4:
		return n
	case 0x5:
		return !n
	case 0x6:
		return v
	case 0x7:
		return !v
	case 0x8:
		return c && !z
	case 0x9:
		return !c || z
	case 0xa:
		return n == v
	case 0xb:
		return n != v
	case 0xc:
		return !z && n == v
	case 0xd:
		return z || n != v
	case 0xe:
		return true
	default:
		return false
	}
}

func executeARMDataProcessing(context *Context, pc, instruction uint32) error {
	operand2, shifterCarry, err := armOperand2(context, pc, instruction)
	if err != nil {
		return err
	}
	opcode := instruction >> 21 & 0xf
	setFlags := instruction&(1<<20) != 0
	rn := instruction >> 16 & 0xf
	rd := instruction >> 12 & 0xf
	left := armRegisterValue(context, pc, rn)
	result := uint32(0)
	writeResult := true
	arithmetic := false
	carry := false
	overflow := false

	switch opcode {
	case 0x0: // AND
		result = left & operand2
	case 0x1: // EOR
		result = left ^ operand2
	case 0x2: // SUB
		result, carry, overflow = subtractWithBorrow(left, operand2, 0)
		arithmetic = true
	case 0x3: // RSB
		result, carry, overflow = subtractWithBorrow(operand2, left, 0)
		arithmetic = true
	case 0x4: // ADD
		result, carry, overflow = addWithCarry(left, operand2, false)
		arithmetic = true
	case 0x5: // ADC
		result, carry, overflow = addWithCarry(left, operand2, context.carry())
		arithmetic = true
	case 0x6: // SBC
		borrow := uint32(1)
		if context.carry() {
			borrow = 0
		}
		result, carry, overflow = subtractWithBorrow(left, operand2, borrow)
		arithmetic = true
	case 0x7: // RSC
		borrow := uint32(1)
		if context.carry() {
			borrow = 0
		}
		result, carry, overflow = subtractWithBorrow(operand2, left, borrow)
		arithmetic = true
	case 0x8: // TST
		result = left & operand2
		writeResult = false
		setFlags = true
	case 0x9: // TEQ
		result = left ^ operand2
		writeResult = false
		setFlags = true
	case 0xa: // CMP
		result, carry, overflow = subtractWithBorrow(left, operand2, 0)
		writeResult = false
		setFlags = true
		arithmetic = true
	case 0xb: // CMN
		result, carry, overflow = addWithCarry(left, operand2, false)
		writeResult = false
		setFlags = true
		arithmetic = true
	case 0xc: // ORR
		result = left | operand2
	case 0xd: // MOV
		result = operand2
	case 0xe: // BIC
		result = left &^ operand2
	case 0xf: // MVN
		result = ^operand2
	}

	if setFlags {
		if arithmetic {
			context.setNZCV(result, carry, overflow)
		} else {
			context.setNZC(result, shifterCarry)
		}
	}
	if !writeResult {
		return nil
	}
	if rd == RegisterPC {
		if setFlags {
			return fmt.Errorf("data-processing exception return is not implemented: %w", ErrUndefinedInstruction)
		}
		context.setARMPC(result)
		return nil
	}
	context.Registers[rd] = result
	return nil
}

func armOperand2(context *Context, pc, instruction uint32) (uint32, bool, error) {
	if instruction&(1<<25) != 0 {
		rotate := instruction >> 8 & 0xf
		value := bits.RotateLeft32(instruction&0xff, -int(rotate*2))
		if rotate == 0 {
			return value, context.carry(), nil
		}
		return value, value&0x80000000 != 0, nil
	}

	value := armRegisterValue(context, pc, instruction&0xf)
	shiftType := instruction >> 5 & 0x3
	if instruction&(1<<4) == 0 {
		amount := instruction >> 7 & 0x1f
		return shiftImmediate(value, shiftType, amount, context.carry())
	}
	if instruction&(1<<7) != 0 {
		return 0, false, ErrUndefinedInstruction
	}
	amountRegister := instruction >> 8 & 0xf
	amount := armRegisterValue(context, pc, amountRegister) & 0xff
	result, carry := shiftRegister(value, shiftType, amount, context.carry())
	return result, carry, nil
}

func shiftImmediate(value, shiftType, amount uint32, oldCarry bool) (uint32, bool, error) {
	switch shiftType {
	case 0: // LSL
		if amount == 0 {
			return value, oldCarry, nil
		}
		return value << amount, value&(1<<(32-amount)) != 0, nil
	case 1: // LSR, #0 means #32
		if amount == 0 {
			return 0, value&0x80000000 != 0, nil
		}
		return value >> amount, value&(1<<(amount-1)) != 0, nil
	case 2: // ASR, #0 means #32
		if amount == 0 {
			if value&0x80000000 != 0 {
				return ^uint32(0), true, nil
			}
			return 0, false, nil
		}
		return uint32(int32(value) >> amount), value&(1<<(amount-1)) != 0, nil
	case 3: // ROR, #0 is RRX
		if amount == 0 {
			result := value >> 1
			if oldCarry {
				result |= 0x80000000
			}
			return result, value&1 != 0, nil
		}
		result := bits.RotateLeft32(value, -int(amount))
		return result, result&0x80000000 != 0, nil
	default:
		return 0, false, ErrUndefinedInstruction
	}
}

func shiftRegister(value, shiftType, amount uint32, oldCarry bool) (uint32, bool) {
	if amount == 0 {
		return value, oldCarry
	}
	switch shiftType {
	case 0:
		if amount < 32 {
			return value << amount, value&(1<<(32-amount)) != 0
		}
		return 0, amount == 32 && value&1 != 0
	case 1:
		if amount < 32 {
			return value >> amount, value&(1<<(amount-1)) != 0
		}
		return 0, amount == 32 && value&0x80000000 != 0
	case 2:
		if amount < 32 {
			return uint32(int32(value) >> amount), value&(1<<(amount-1)) != 0
		}
		return uint32(int32(value) >> 31), value&0x80000000 != 0
	case 3:
		rotation := amount & 31
		if rotation == 0 {
			return value, value&0x80000000 != 0
		}
		result := bits.RotateLeft32(value, -int(rotation))
		return result, result&0x80000000 != 0
	default:
		return value, oldCarry
	}
}

func executeARMMultiply(context *Context, pc, instruction uint32) error {
	accumulate := instruction&(1<<21) != 0
	setFlags := instruction&(1<<20) != 0
	rd := instruction >> 16 & 0xf
	rn := instruction >> 12 & 0xf
	rs := instruction >> 8 & 0xf
	rm := instruction & 0xf
	if rd == RegisterPC || rn == RegisterPC || rs == RegisterPC || rm == RegisterPC {
		return ErrUndefinedInstruction
	}
	result := armRegisterValue(context, pc, rm) * armRegisterValue(context, pc, rs)
	if accumulate {
		result += armRegisterValue(context, pc, rn)
	}
	context.Registers[rd] = result
	if setFlags {
		context.setNZ(result)
	}
	return nil
}

func executeARMLongMultiply(context *Context, instruction uint32) error {
	signed := instruction&(1<<22) != 0
	accumulate := instruction&(1<<21) != 0
	setFlags := instruction&(1<<20) != 0
	rdHigh := instruction >> 16 & 0xf
	rdLow := instruction >> 12 & 0xf
	rs := instruction >> 8 & 0xf
	rm := instruction & 0xf
	if rdHigh == RegisterPC || rdLow == RegisterPC || rs == RegisterPC || rm == RegisterPC || rdHigh == rdLow {
		return ErrUndefinedInstruction
	}

	var result uint64
	if signed {
		result = uint64(int64(int32(context.Registers[rm])) * int64(int32(context.Registers[rs])))
	} else {
		result = uint64(context.Registers[rm]) * uint64(context.Registers[rs])
	}
	if accumulate {
		result += uint64(context.Registers[rdHigh])<<32 | uint64(context.Registers[rdLow])
	}
	context.Registers[rdHigh] = uint32(result >> 32)
	context.Registers[rdLow] = uint32(result)
	if setFlags {
		context.setNZ64(result)
	}
	return nil
}

func executeARMSwap(context *Context, memory *Memory, instruction uint32) error {
	byteTransfer := instruction&(1<<22) != 0
	rn := instruction >> 16 & 0xf
	rd := instruction >> 12 & 0xf
	rm := instruction & 0xf
	if rn == RegisterPC || rd == RegisterPC || rm == RegisterPC {
		return ErrUndefinedInstruction
	}
	address := context.Registers[rn]
	if byteTransfer {
		loaded, err := memory.read8(address)
		if err != nil {
			return err
		}
		if err := memory.write8(address, uint8(context.Registers[rm])); err != nil {
			return err
		}
		context.Registers[rd] = uint32(loaded)
		return nil
	}
	address &^= 3
	loaded, err := memory.read32(address)
	if err != nil {
		return err
	}
	if err := memory.write32(address, context.Registers[rm]); err != nil {
		return err
	}
	context.Registers[rd] = loaded
	return nil
}

func executeARMProgramStatusTransfer(context *Context, instruction uint32) error {
	useSavedStatus := instruction&(1<<22) != 0
	if useSavedStatus { // User-mode execution has no SPSR.
		return ErrUndefinedInstruction
	}
	write := instruction&(1<<21) != 0
	if !write { // MRS
		rd := instruction >> 12 & 0xf
		if rd == RegisterPC {
			return ErrUndefinedInstruction
		}
		context.Registers[rd] = context.CPSR
		return nil
	}

	var value uint32
	if instruction&(1<<25) != 0 {
		rotate := instruction >> 8 & 0xf
		value = bits.RotateLeft32(instruction&0xff, -int(rotate*2))
	} else {
		rm := instruction & 0xf
		if rm == RegisterPC {
			return ErrUndefinedInstruction
		}
		value = context.Registers[rm]
	}
	fieldMask := instruction >> 16 & 0xf
	if fieldMask == 0 {
		return ErrUndefinedInstruction
	}
	mask := uint32(0)
	if fieldMask&8 != 0 {
		mask = flagNegative | flagZero | flagCarry | flagOverflow
	}
	// ARMv4T user mode cannot change the CPSR control field. Status and
	// extension fields contain no state modeled by this execution core.
	context.CPSR = context.CPSR&^mask | value&mask
	return nil
}

func executeARMHalfwordSignedTransfer(context *Context, memory *Memory, pc, instruction uint32) error {
	preIndex := instruction&(1<<24) != 0
	addOffset := instruction&(1<<23) != 0
	immediate := instruction&(1<<22) != 0
	writeBack := instruction&(1<<21) != 0
	load := instruction&(1<<20) != 0
	rn := instruction >> 16 & 0xf
	rd := instruction >> 12 & 0xf
	transfer := instruction >> 5 & 0x3
	if transfer == 0 || (!load && transfer != 1) || rd == RegisterPC {
		return ErrUndefinedInstruction
	}

	var offset uint32
	if immediate {
		offset = instruction>>4&0xf0 | instruction&0xf
	} else {
		rm := instruction & 0xf
		if rm == RegisterPC {
			return ErrUndefinedInstruction
		}
		offset = context.Registers[rm]
	}
	base := armRegisterValue(context, pc, rn)
	indexed := base - offset
	if addOffset {
		indexed = base + offset
	}
	address := base
	if preIndex {
		address = indexed
	}

	if load {
		var value uint32
		var err error
		switch transfer {
		case 1: // LDRH
			var halfword uint16
			halfword, err = memory.readData16(address)
			value = uint32(halfword)
		case 2: // LDRSB
			var byteValue uint8
			byteValue, err = memory.read8(address)
			value = uint32(int32(int8(byteValue)))
		case 3: // LDRSH
			var halfword uint16
			halfword, err = memory.readData16(address)
			value = uint32(int32(int16(halfword)))
		}
		if err != nil {
			return err
		}
		context.Registers[rd] = value
	} else { // STRH
		if err := memory.writeData16(address, uint16(context.Registers[rd])); err != nil {
			return err
		}
	}
	if (!preIndex || writeBack) && rn != RegisterPC && (!load || rn != rd) {
		context.Registers[rn] = indexed
	}
	return nil
}

func executeARMSingleTransfer(context *Context, memory *Memory, pc, instruction uint32) error {
	preIndex := instruction&(1<<24) != 0
	addOffset := instruction&(1<<23) != 0
	byteTransfer := instruction&(1<<22) != 0
	writeBack := instruction&(1<<21) != 0
	load := instruction&(1<<20) != 0
	rn := instruction >> 16 & 0xf
	rd := instruction >> 12 & 0xf
	base := armRegisterValue(context, pc, rn)
	var offset uint32
	if instruction&(1<<25) == 0 {
		offset = instruction & 0xfff
	} else {
		if instruction&(1<<4) != 0 {
			return ErrUndefinedInstruction
		}
		value := armRegisterValue(context, pc, instruction&0xf)
		var err error
		offset, _, err = shiftImmediate(value, instruction>>5&3, instruction>>7&0x1f, context.carry())
		if err != nil {
			return err
		}
	}
	indexed := base - offset
	if addOffset {
		indexed = base + offset
	}
	address := base
	if preIndex {
		address = indexed
	}

	if load {
		var value uint32
		var err error
		if byteTransfer {
			var byteValue uint8
			byteValue, err = memory.read8(address)
			value = uint32(byteValue)
		} else {
			value, err = memory.readData32(address)
		}
		if err != nil {
			return err
		}
		if rd == RegisterPC {
			context.setARMPC(value)
		} else {
			context.Registers[rd] = value
		}
	} else {
		value := armRegisterValue(context, pc, rd)
		if byteTransfer {
			if err := memory.write8(address, uint8(value)); err != nil {
				return err
			}
		} else if err := memory.writeData32(address, value); err != nil {
			return err
		}
	}
	if (!preIndex || writeBack) && rn != RegisterPC {
		context.Registers[rn] = indexed
	}
	return nil
}

func executeARMBlockTransfer(context *Context, memory *Memory, pc, instruction uint32) error {
	preIndex := instruction&(1<<24) != 0
	addOffset := instruction&(1<<23) != 0
	userRegisters := instruction&(1<<22) != 0
	writeBack := instruction&(1<<21) != 0
	load := instruction&(1<<20) != 0
	rn := instruction >> 16 & 0xf
	registerList := uint16(instruction)
	if userRegisters || rn == RegisterPC || registerList == 0 {
		return ErrUndefinedInstruction
	}
	count := uint32(bits.OnesCount16(registerList))
	base := context.Registers[rn]
	writeBackValue := base - count*4
	if addOffset {
		writeBackValue = base + count*4
	}
	address := base
	if addOffset {
		if preIndex {
			address += 4
		}
	} else {
		address -= count * 4
		if !preIndex {
			address += 4
		}
	}
	// ARM7TDMI performs the base modification before transferring register
	// results. A load of the base register therefore replaces the writeback
	// value, while a store sees the modified base unless Rn is the first
	// register transferred. Preserve that observable ordering for clients that
	// contain Rn in the list even though later architectures call some of these
	// combinations unpredictable.
	if writeBack {
		context.Registers[rn] = writeBackValue
	}

	transferIndex := uint32(0)
	for register := uint32(0); register < RegisterCount; register++ {
		if registerList&(1<<register) == 0 {
			continue
		}
		if load {
			value, err := memory.readData32(address)
			if err != nil {
				return err
			}
			if register == RegisterPC {
				context.setARMPC(value)
			} else {
				context.Registers[register] = value
			}
		} else {
			value := armRegisterValue(context, pc, register)
			if register == RegisterPC {
				// R15 is two pipeline stages beyond the current instruction during
				// an ARM7TDMI store-multiple transfer.
				value = pc + 12
			} else if writeBack && register == rn && transferIndex == 0 {
				value = base
			}
			if err := memory.writeData32(address, value); err != nil {
				return err
			}
		}
		address += 4
		transferIndex++
	}
	return nil
}

func armRegisterValue(context *Context, pc, register uint32) uint32 {
	if register == RegisterPC {
		return pc + 8
	}
	return context.Registers[register]
}

// The flags come out of the 32-bit result rather than out of a wider
// computation of it.
//
// Widening is the obvious way to write these — do the sum in 64 bits and look
// at what fell off the top — but it costs two extra additions, and the signed
// one needs both operands sign-extended first. That is four 64-bit operations
// per arithmetic instruction, which are the most frequent instructions a game
// runs, and this is an interpreter whose cost is the number of operations it
// performs rather than the difficulty of any one of them.
//
// Carry out is the sum coming out no larger than an operand, which can only
// happen if it wrapped. Overflow is both operands agreeing on a sign that the
// result disagrees with, which is what the two exclusive-ors test.

func addWithCarry(left, right uint32, carryIn bool) (uint32, bool, bool) {
	carry := uint32(0)
	if carryIn {
		carry = 1
	}
	result := left + right + carry
	// With a carry in, a result merely equal to an operand has also wrapped.
	carryOut := result < left || (carryIn && result == left)
	overflow := (left^result)&(right^result)>>31&1 != 0
	return result, carryOut, overflow
}

func subtractWithBorrow(left, right, borrow uint32) (uint32, bool, bool) {
	result := left - right - borrow
	// ARM reports the carry of a subtraction as "no borrow was needed".
	carryOut := left > right || (left == right && borrow == 0)
	overflow := (left^right)&(left^result)>>31&1 != 0
	return result, carryOut, overflow
}
