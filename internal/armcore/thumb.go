package armcore

import (
	"math/bits"
)

// A Thumb instruction is classified once and executed many times. The match
// tree below answers which form an encoding takes, and Memory caches that
// answer per address (see decodeThumb), so a loop that runs the same
// instruction a thousand times walks the tree once instead of a thousand
// times. The forms are numbered rather than re-derived because the number is
// what gets cached.
type thumbForm uint8

const (
	// thumbUndecoded is the zero value, so a fresh cache slot reads as "not
	// decoded yet" without anything having to clear it.
	thumbUndecoded thumbForm = iota
	thumbUndefined
	thumbAddSubtract
	thumbShift
	thumbImmediate
	thumbALU
	thumbHighRegister
	thumbLiteralLoad
	thumbRegisterTransfer
	thumbImmediateTransfer
	thumbHalfwordTransfer
	thumbStackRelativeTransfer
	thumbAddress
	thumbAdjustStack
	thumbPush
	thumbPop
	thumbMultipleTransfer
	thumbConditionalBranch
	thumbBranch
	thumbLongBranchPrefix
	thumbLongBranchSuffix
	thumbLongBranchExchangeSuffix
	thumbHint
)

func classifyThumb(value uint32) thumbForm {
	switch {
	case value&0xf800 == 0x1800:
		return thumbAddSubtract
	case value&0xe000 == 0x0000:
		return thumbShift
	case value&0xe000 == 0x2000:
		return thumbImmediate
	case value&0xfc00 == 0x4000:
		return thumbALU
	case value&0xfc00 == 0x4400:
		return thumbHighRegister
	case value&0xf800 == 0x4800:
		return thumbLiteralLoad
	case value&0xf000 == 0x5000:
		return thumbRegisterTransfer
	case value&0xe000 == 0x6000:
		return thumbImmediateTransfer
	case value&0xf000 == 0x8000:
		return thumbHalfwordTransfer
	case value&0xf000 == 0x9000:
		return thumbStackRelativeTransfer
	case value&0xf000 == 0xa000:
		return thumbAddress
	case value&0xff00 == 0xb000:
		return thumbAdjustStack
	case value&0xfe00 == 0xb400:
		return thumbPush
	case value&0xff0f == 0xbf00:
		// The hint space: `NOP`, `YIELD`, `WFE`, `WFI`, `SEV`, which are one
		// encoding with the condition field naming which. **There is nothing
		// for any of them to do here** — one core, no interrupts, no event
		// register — so all five are the same instruction to this engine, and
		// what matters is that they execute rather than fault.
		//
		// They are Thumb-2 encodings in a Thumb-1 module, which is what a
		// patched archive looks like: a cracker takes a branch out by writing
		// a two-byte `nop` over it. One local archive reaches one in its own
		// loading path. The rest of the 0xbfxx space is `IT`, which changes
		// how the instructions after it execute, and that stays undefined
		// rather than being run as if it were a hint.
		return thumbHint
	case value&0xfe00 == 0xbc00:
		return thumbPop
	case value&0xf000 == 0xc000:
		return thumbMultipleTransfer
	case value&0xf000 == 0xd000:
		return thumbConditionalBranch
	case value&0xf800 == 0xe000:
		return thumbBranch
	case value&0xf800 == 0xe800:
		return thumbLongBranchExchangeSuffix
	case value&0xf800 == 0xf000:
		return thumbLongBranchPrefix
	case value&0xf800 == 0xf800:
		return thumbLongBranchSuffix
	default:
		return thumbUndefined
	}
}

func executeThumb(context *Context, memory *Memory, pc uint32, instruction uint16) (*SupervisorCall, error) {
	value := uint32(instruction)
	return executeThumbForm(classifyThumb(value), context, memory, pc, value)
}

func executeThumbForm(form thumbForm, context *Context, memory *Memory, pc uint32, value uint32) (*SupervisorCall, error) {
	switch form {
	case thumbAddSubtract:
		return nil, executeThumbAddSubtract(context, value)
	case thumbShift:
		return nil, executeThumbShift(context, value)
	case thumbImmediate:
		return nil, executeThumbImmediate(context, value)
	case thumbALU:
		return nil, executeThumbALU(context, value)
	case thumbHighRegister:
		return nil, executeThumbHighRegister(context, pc, value)
	case thumbLiteralLoad:
		rd := value >> 8 & 7
		address := (pc + 4) &^ 3
		loaded, err := memory.readData32(address + (value&0xff)*4)
		if err != nil {
			return nil, err
		}
		context.Registers[rd] = loaded
		return nil, nil
	case thumbRegisterTransfer:
		return nil, executeThumbRegisterTransfer(context, memory, value)
	case thumbImmediateTransfer:
		return nil, executeThumbImmediateTransfer(context, memory, value)
	case thumbHalfwordTransfer:
		return nil, executeThumbHalfwordTransfer(context, memory, value)
	case thumbStackRelativeTransfer:
		return nil, executeThumbStackRelativeTransfer(context, memory, value)
	case thumbAddress:
		rd := value >> 8 & 7
		base := (pc + 4) &^ 3
		if value&(1<<11) != 0 {
			base = context.Registers[RegisterSP]
		}
		context.Registers[rd] = base + (value&0xff)*4
		return nil, nil
	case thumbHint:
		return nil, nil
	case thumbAdjustStack:
		offset := (value & 0x7f) * 4
		if value&(1<<7) != 0 {
			context.Registers[RegisterSP] -= offset
		} else {
			context.Registers[RegisterSP] += offset
		}
		return nil, nil
	case thumbPush:
		return nil, executeThumbPush(context, memory, value)
	case thumbPop:
		return nil, executeThumbPop(context, memory, value)
	case thumbMultipleTransfer:
		return nil, executeThumbMultipleTransfer(context, memory, value)
	case thumbConditionalBranch:
		return executeThumbConditionalBranch(context, pc, value)
	case thumbBranch:
		return nil, executeThumbBranch(context, pc, value)
	case thumbLongBranchPrefix:
		offset := int32(value&0x7ff) << 21 >> 9
		context.Registers[RegisterLR] = uint32(int64(pc+4) + int64(offset))
		return nil, nil
	case thumbLongBranchSuffix:
		target := context.Registers[RegisterLR] + (value&0x7ff)*2
		context.Registers[RegisterLR] = (pc + 2) | 1
		context.setThumbPC(target)
		return nil, nil
	case thumbLongBranchExchangeSuffix:
		// The other half of the same pair, and the one that leaves Thumb: the
		// prefix put the high half of the offset in LR either way, and only
		// the suffix says which state the callee is in. Its target is word
		// aligned rather than halfword aligned, so the low two bits of the
		// sum are dropped rather than carried into an ARM PC.
		target := context.Registers[RegisterLR] + (value&0x7ff)*2
		context.Registers[RegisterLR] = (pc + 2) | 1
		context.setARMPC(target)
		return nil, nil
	default:
		return nil, ErrUndefinedInstruction
	}
}

// Conditional and unconditional branches are leaf functions rather than case
// bodies because the interpreter loop routes the forms real games spend their
// instructions in directly, and routing can only stay honest while every form's
// semantics live in exactly one function.
func executeThumbConditionalBranch(context *Context, pc, instruction uint32) (*SupervisorCall, error) {
	condition := instruction >> 8 & 0xf
	if condition == 0xf {
		return &SupervisorCall{Immediate: instruction & 0xff, Address: pc, ResumePC: pc + 2}, nil
	}
	if condition == 0xe {
		return nil, ErrUndefinedInstruction
	}
	if conditionPassed(context.CPSR, condition) {
		offset := int32(int8(instruction&0xff)) * 2
		context.setThumbPC(uint32(int64(pc+4) + int64(offset)))
	}
	return nil, nil
}

func executeThumbBranch(context *Context, pc, instruction uint32) error {
	offset := int32(instruction&0x7ff) << 21 >> 20
	context.setThumbPC(uint32(int64(pc+4) + int64(offset)))
	return nil
}

func executeThumbShift(context *Context, instruction uint32) error {
	shiftType := instruction >> 11 & 3
	if shiftType == 3 {
		return ErrUndefinedInstruction
	}
	amount := instruction >> 6 & 0x1f
	rs := instruction >> 3 & 7
	rd := instruction & 7
	result, carry, err := shiftImmediate(context.Registers[rs], shiftType, amount, context.carry())
	if err != nil {
		return err
	}
	context.Registers[rd] = result
	context.setNZC(result, carry)
	return nil
}

func executeThumbAddSubtract(context *Context, instruction uint32) error {
	immediate := instruction&(1<<10) != 0
	subtract := instruction&(1<<9) != 0
	operand := instruction >> 6 & 7
	rs := instruction >> 3 & 7
	rd := instruction & 7
	if !immediate {
		operand = context.Registers[operand]
	}
	var result uint32
	var carry, overflow bool
	if subtract {
		result, carry, overflow = subtractWithBorrow(context.Registers[rs], operand, 0)
	} else {
		result, carry, overflow = addWithCarry(context.Registers[rs], operand, false)
	}
	context.Registers[rd] = result
	context.setNZCV(result, carry, overflow)
	return nil
}

func executeThumbImmediate(context *Context, instruction uint32) error {
	opcode := instruction >> 11 & 3
	rd := instruction >> 8 & 7
	immediate := instruction & 0xff
	if opcode == 0 { // MOV leaves carry and overflow alone.
		context.Registers[rd] = immediate
		context.setNZ(immediate)
		return nil
	}
	var result uint32
	var carry, overflow bool
	if opcode == 2 { // ADD
		result, carry, overflow = addWithCarry(context.Registers[rd], immediate, false)
	} else { // CMP and SUB share the subtraction; only CMP discards the result.
		result, carry, overflow = subtractWithBorrow(context.Registers[rd], immediate, 0)
	}
	if opcode != 1 {
		context.Registers[rd] = result
	}
	context.setNZCV(result, carry, overflow)
	return nil
}

func executeThumbALU(context *Context, instruction uint32) error {
	opcode := instruction >> 6 & 0xf
	rs := instruction >> 3 & 7
	rd := instruction & 7
	left := context.Registers[rd]
	right := context.Registers[rs]
	result := uint32(0)
	carry := context.carry()
	overflow := context.CPSR&flagOverflow != 0
	writeResult := true
	updateCarry := false
	updateOverflow := false

	switch opcode {
	case 0x0:
		result = left & right
	case 0x1:
		result = left ^ right
	case 0x2:
		result, carry = shiftRegister(left, 0, right&0xff, carry)
		updateCarry = true
	case 0x3:
		result, carry = shiftRegister(left, 1, right&0xff, carry)
		updateCarry = true
	case 0x4:
		result, carry = shiftRegister(left, 2, right&0xff, carry)
		updateCarry = true
	case 0x5:
		result, carry, overflow = addWithCarry(left, right, carry)
		updateCarry, updateOverflow = true, true
	case 0x6:
		borrow := uint32(1)
		if carry {
			borrow = 0
		}
		result, carry, overflow = subtractWithBorrow(left, right, borrow)
		updateCarry, updateOverflow = true, true
	case 0x7:
		result, carry = shiftRegister(left, 3, right&0xff, carry)
		updateCarry = true
	case 0x8:
		result = left & right
		writeResult = false
	case 0x9:
		result, carry, overflow = subtractWithBorrow(0, right, 0)
		updateCarry, updateOverflow = true, true
	case 0xa:
		result, carry, overflow = subtractWithBorrow(left, right, 0)
		writeResult = false
		updateCarry, updateOverflow = true, true
	case 0xb:
		result, carry, overflow = addWithCarry(left, right, false)
		writeResult = false
		updateCarry, updateOverflow = true, true
	case 0xc:
		result = left | right
	case 0xd:
		result = left * right
	case 0xe:
		result = left &^ right
	case 0xf:
		result = ^right
	}
	if writeResult {
		context.Registers[rd] = result
	}
	// Every case that updates overflow also updates carry, so these three arms
	// cover the same flag sets the separate setters did.
	switch {
	case updateOverflow:
		context.setNZCV(result, carry, overflow)
	case updateCarry:
		context.setNZC(result, carry)
	default:
		context.setNZ(result)
	}
	return nil
}

func executeThumbHighRegister(context *Context, pc, instruction uint32) error {
	opcode := instruction >> 8 & 3
	rd := instruction&7 | instruction>>4&8
	rs := instruction >> 3 & 0xf
	left := thumbRegisterValue(context, pc, rd)
	right := thumbRegisterValue(context, pc, rs)
	switch opcode {
	case 0: // ADD
		result := left + right
		if rd == RegisterPC {
			context.setThumbPC(result)
		} else {
			context.Registers[rd] = result
		}
	case 1: // CMP
		result, carry, overflow := subtractWithBorrow(left, right, 0)
		context.setNZCV(result, carry, overflow)
	case 2: // MOV
		if rd == RegisterPC {
			context.setThumbPC(right)
		} else {
			context.Registers[rd] = right
		}
	case 3: // BX / BLX register
		if instruction&(1<<7) != 0 {
			// BLX register is ARMv5T, past the ARMv4T these handsets are
			// usually described as. LGT modules use it for every indirect
			// call, so refusing it stops each one at its first function
			// pointer. The return address carries the Thumb bit, because that
			// is the state the caller is in.
			context.Registers[RegisterLR] = pc + 2 | 1
		}
		context.branchExchange(right)
	}
	return nil
}

func executeThumbRegisterTransfer(context *Context, memory *Memory, instruction uint32) error {
	opcode := instruction >> 9 & 7
	ro := instruction >> 6 & 7
	rb := instruction >> 3 & 7
	rd := instruction & 7
	address := context.Registers[rb] + context.Registers[ro]
	switch opcode {
	case 0:
		return memory.writeData32(address, context.Registers[rd])
	case 1:
		return memory.writeData16(address, uint16(context.Registers[rd]))
	case 2:
		return memory.write8(address, uint8(context.Registers[rd]))
	case 3:
		value, err := memory.read8(address)
		context.Registers[rd] = uint32(int32(int8(value)))
		return err
	case 4:
		value, err := memory.readData32(address)
		context.Registers[rd] = value
		return err
	case 5:
		value, err := memory.readData16(address)
		context.Registers[rd] = uint32(value)
		return err
	case 6:
		value, err := memory.read8(address)
		context.Registers[rd] = uint32(value)
		return err
	case 7:
		value, err := memory.readData16(address)
		context.Registers[rd] = uint32(int32(int16(value)))
		return err
	default:
		return ErrUndefinedInstruction
	}
}

func executeThumbImmediateTransfer(context *Context, memory *Memory, instruction uint32) error {
	byteTransfer := instruction&(1<<12) != 0
	load := instruction&(1<<11) != 0
	offset := instruction >> 6 & 0x1f
	rb := instruction >> 3 & 7
	rd := instruction & 7
	if !byteTransfer {
		offset *= 4
	}
	address := context.Registers[rb] + offset
	if load {
		if byteTransfer {
			value, err := memory.read8(address)
			context.Registers[rd] = uint32(value)
			return err
		}
		value, err := memory.readData32(address)
		context.Registers[rd] = value
		return err
	}
	if byteTransfer {
		return memory.write8(address, uint8(context.Registers[rd]))
	}
	return memory.writeData32(address, context.Registers[rd])
}

func executeThumbHalfwordTransfer(context *Context, memory *Memory, instruction uint32) error {
	load := instruction&(1<<11) != 0
	offset := (instruction >> 6 & 0x1f) * 2
	rb := instruction >> 3 & 7
	rd := instruction & 7
	address := context.Registers[rb] + offset
	if load {
		value, err := memory.readData16(address)
		context.Registers[rd] = uint32(value)
		return err
	}
	return memory.writeData16(address, uint16(context.Registers[rd]))
}

func executeThumbStackRelativeTransfer(context *Context, memory *Memory, instruction uint32) error {
	load := instruction&(1<<11) != 0
	rd := instruction >> 8 & 7
	address := context.Registers[RegisterSP] + (instruction&0xff)*4
	if load {
		value, err := memory.readData32(address)
		context.Registers[rd] = value
		return err
	}
	return memory.writeData32(address, context.Registers[rd])
}

func executeThumbPush(context *Context, memory *Memory, instruction uint32) error {
	registerList := uint8(instruction)
	count := uint32(bits.OnesCount8(registerList))
	if instruction&(1<<8) != 0 {
		count++
	}
	if count == 0 {
		return ErrUndefinedInstruction
	}
	address := context.Registers[RegisterSP] - count*4
	context.Registers[RegisterSP] = address
	for register := uint32(0); register < 8; register++ {
		if registerList&(1<<register) != 0 {
			if err := memory.writeData32(address, context.Registers[register]); err != nil {
				return err
			}
			address += 4
		}
	}
	if instruction&(1<<8) != 0 {
		return memory.writeData32(address, context.Registers[RegisterLR])
	}
	return nil
}

func executeThumbPop(context *Context, memory *Memory, instruction uint32) error {
	registerList := uint8(instruction)
	count := uint32(bits.OnesCount8(registerList))
	if instruction&(1<<8) != 0 {
		count++
	}
	if count == 0 {
		return ErrUndefinedInstruction
	}
	address := context.Registers[RegisterSP]
	for register := uint32(0); register < 8; register++ {
		if registerList&(1<<register) != 0 {
			value, err := memory.readData32(address)
			if err != nil {
				return err
			}
			context.Registers[register] = value
			address += 4
		}
	}
	if instruction&(1<<8) != 0 {
		value, err := memory.readData32(address)
		if err != nil {
			return err
		}
		context.branchExchange(value)
	}
	context.Registers[RegisterSP] += count * 4
	return nil
}

func executeThumbMultipleTransfer(context *Context, memory *Memory, instruction uint32) error {
	load := instruction&(1<<11) != 0
	rb := instruction >> 8 & 7
	registerList := uint8(instruction)
	if registerList == 0 {
		return ErrUndefinedInstruction
	}
	base := context.Registers[rb]
	address := base
	// Thumb LDMIA/STMIA always writes the incremented base. On ARM7TDMI the
	// modification happens before loaded register results are committed, so an
	// LDM that includes Rb replaces it with the loaded word. For STM, a lowest
	// listed Rb stores its original value; a later Rb observes writeback.
	context.Registers[rb] = base + uint32(bits.OnesCount8(registerList))*4
	transferIndex := uint32(0)
	for register := uint32(0); register < 8; register++ {
		if registerList&(1<<register) == 0 {
			continue
		}
		if load {
			value, err := memory.readData32(address)
			if err != nil {
				return err
			}
			context.Registers[register] = value
		} else {
			value := context.Registers[register]
			if register == rb && transferIndex == 0 {
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

func thumbRegisterValue(context *Context, pc, register uint32) uint32 {
	if register == RegisterPC {
		return pc + 4
	}
	return context.Registers[register]
}
