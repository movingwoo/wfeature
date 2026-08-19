package armcore

import "fmt"

type StopReason uint8

const (
	StopEnd StopReason = iota
	StopCountExhausted
	StopSupervisorCall
)

type SupervisorCall struct {
	Immediate uint32
	Address   uint32
	ResumePC  uint32
}

type RunResult struct {
	Reason         StopReason
	Steps          uint32
	SupervisorCall SupervisorCall
}

// Engine executes bounded instruction quanta. It owns no register or memory
// state, which lets Core save each cooperative guest thread independently.
type Engine struct{}

func (Engine) Run(context *Context, memory *Memory, end uint32, count uint32) (RunResult, error) {
	if context == nil {
		return RunResult{}, fmt.Errorf("ARM context is nil")
	}
	if memory == nil {
		return RunResult{}, fmt.Errorf("ARM memory is nil")
	}
	if count == 0 {
		return RunResult{Reason: StopCountExhausted}, nil
	}

	// The guest accessors below rely on this: the memory lock is taken once
	// for the quantum rather than once per instruction. See the Memory doc
	// comment. Every return from here on has to release it, which is what the
	// defer is for — a supervisor call leaves the quantum, and its handler
	// must run outside the lock.
	memory.beginQuantum()
	defer memory.endQuantum()

	for steps := uint32(0); steps < count; steps++ {
		pc := context.PC()
		// A watch hit has to name the instruction that caused it, and only the
		// engine knows where execution is. The compare is the whole cost while
		// nothing is watched.
		if memory.watchCount > 0 {
			memory.executingPC = pc
		}
		if pc == end {
			return RunResult{Reason: StopEnd, Steps: steps}, nil
		}

		if context.Thumb() {
			decoded, cached := memory.decodedThumbFast(pc)
			if !cached {
				var err error
				if decoded, err = memory.decodeThumb(pc); err != nil {
					return RunResult{Steps: steps}, err
				}
			}
			context.Registers[RegisterPC] = pc + 2
			value := uint32(decoded.instruction)

			// The switch routes the forms games actually spend their
			// instructions in straight to their handler, instead of reaching
			// them through executeThumbForm. That removes one Go call per guest
			// instruction, which is worth a tenth of a step; see armcore.md for
			// the measurements it came from.
			//
			// These cases route only. Every form's semantics live in its own
			// function, which executeThumbForm reaches the same way, so the two
			// cannot answer differently; the default arm covers every remaining
			// form. thumb_test.go executes both paths.
			//
			// They take the raw halfword rather than operands the decode cache
			// pulled out ahead of time. That was built and measured, and it is
			// slower both ways round; see armcore.md, "A wider decode cache
			// entry was built and lost, twice over".
			var supervisorCall *SupervisorCall
			var err error
			switch decoded.form {
			case thumbImmediate:
				err = executeThumbImmediate(context, value)
			case thumbAddSubtract:
				err = executeThumbAddSubtract(context, value)
			case thumbShift:
				err = executeThumbShift(context, value)
			case thumbALU:
				err = executeThumbALU(context, value)
			case thumbConditionalBranch:
				supervisorCall, err = executeThumbConditionalBranch(context, pc, value)
				// A branch that was taken backwards by a few instructions has
				// just closed a loop. That is the only moment worth asking
				// whether it is one the engine can stand in for, so the cost
				// on every other instruction in the program is this compare.
				// See fill_loop.go.
				if err == nil && supervisorCall == nil {
					if head := context.Registers[RegisterPC]; head < pc && pc-head <= maxStoreLoopBytes {
						stood, loopErr := memory.runStoreLoop(context, head, pc)
						if loopErr != nil {
							return RunResult{Steps: steps}, &InstructionError{PC: pc, Instruction: value, Thumb: true, Cause: loopErr}
						}
						// Charged as if every instruction had run, so a guest
						// that would have exhausted its budget still does.
						steps += stood
					}
				}
			case thumbBranch:
				err = executeThumbBranch(context, pc, value)
			case thumbImmediateTransfer:
				err = executeThumbImmediateTransfer(context, memory, value)
			case thumbHalfwordTransfer:
				err = executeThumbHalfwordTransfer(context, memory, value)
			case thumbRegisterTransfer:
				err = executeThumbRegisterTransfer(context, memory, value)
			case thumbStackRelativeTransfer:
				err = executeThumbStackRelativeTransfer(context, memory, value)
			default:
				supervisorCall, err = executeThumbForm(decoded.form, context, memory, pc, value)
			}
			if err != nil {
				return RunResult{Steps: steps}, &InstructionError{PC: pc, Instruction: uint32(decoded.instruction), Thumb: true, Cause: err}
			}
			if supervisorCall != nil {
				return RunResult{Reason: StopSupervisorCall, Steps: steps + 1, SupervisorCall: *supervisorCall}, nil
			}
			continue
		}

		instruction, err := memory.fetch32(pc)
		if err != nil {
			return RunResult{Steps: steps}, err
		}
		context.Registers[RegisterPC] = pc + 4
		supervisorCall, err := executeARM(context, memory, pc, instruction)
		if err != nil {
			return RunResult{Steps: steps}, &InstructionError{PC: pc, Instruction: instruction, Cause: err}
		}
		if supervisorCall != nil {
			return RunResult{Reason: StopSupervisorCall, Steps: steps + 1, SupervisorCall: *supervisorCall}, nil
		}
	}

	return RunResult{Reason: StopCountExhausted, Steps: count}, nil
}
