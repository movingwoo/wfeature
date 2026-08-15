package armcore

import (
	"errors"
	"fmt"
)

var (
	ErrAddressOverflow         = errors.New("guest address range overflows 32 bits")
	ErrCallArgumentLimit       = errors.New("ARM call argument limit exceeded")
	ErrInvalidMemoryRange      = errors.New("invalid guest memory range")
	ErrMappingLimit            = errors.New("guest memory mapping limit exceeded")
	ErrOverlappingMapping      = errors.New("guest memory mapping overlaps one with other permissions")
	ErrUnmapped                = errors.New("guest memory is not mapped")
	ErrPermission              = errors.New("guest memory permission denied")
	ErrUnaligned               = errors.New("unaligned guest memory access")
	ErrUndefinedInstruction    = errors.New("undefined ARM instruction")
	ErrStepLimit               = errors.New("ARM instruction limit exceeded")
	ErrUnhandledSupervisorCall = errors.New("unhandled ARM supervisor call")
	ErrThreadState             = errors.New("invalid ARM thread state")
)

type AccessError struct {
	Operation string
	Address   uint32
	Size      uint64
	Cause     error
}

func (err *AccessError) Error() string {
	return fmt.Sprintf("%s guest memory at %#x (%d bytes): %v", err.Operation, err.Address, err.Size, err.Cause)
}

func (err *AccessError) Unwrap() error {
	return err.Cause
}

type InstructionError struct {
	PC          uint32
	Instruction uint32
	Thumb       bool
	Cause       error
}

func (err *InstructionError) Error() string {
	state := "ARM"
	width := 8
	if err.Thumb {
		state = "Thumb"
		width = 4
	}
	return fmt.Sprintf("%s instruction %0*x at %#x: %v", state, width, err.Instruction, err.PC, err.Cause)
}

func (err *InstructionError) Unwrap() error {
	return err.Cause
}
