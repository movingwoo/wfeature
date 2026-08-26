// Package armcore implements the pure-Go ARMv4T execution layer used by KTF
// and, later, LGT runtimes.
package armcore

import "fmt"

const (
	RegisterCount = 16
	// RegisterFP is r11. It is named because one guest generation keeps its
	// whole runtime context there and expects a caller to hold it.
	RegisterFP = 11
	RegisterSP = 13
	RegisterLR = 14
	RegisterPC = 15
)

const (
	flagNegative uint32 = 1 << 31
	flagZero     uint32 = 1 << 30
	flagCarry    uint32 = 1 << 29
	flagOverflow uint32 = 1 << 28
	flagThumb    uint32 = 1 << 5
	modeMask     uint32 = 0x1f
	modeUser     uint32 = 0x10
)

// Context is the complete guest-visible state saved at a cooperative thread
// switch. PC always stores an aligned instruction address; Thumb state is kept
// in CPSR.
type Context struct {
	Registers [RegisterCount]uint32
	CPSR      uint32
}

// NewContext returns a zeroed ARM user-mode context.
func NewContext() Context {
	return Context{CPSR: modeUser}
}

func (context *Context) Register(index int) (uint32, error) {
	if index < 0 || index >= RegisterCount {
		return 0, fmt.Errorf("register index %d is outside r0-r15", index)
	}
	return context.Registers[index], nil
}

func (context *Context) SetRegister(index int, value uint32) error {
	if index < 0 || index >= RegisterCount {
		return fmt.Errorf("register index %d is outside r0-r15", index)
	}
	if index == RegisterPC {
		return context.SetPC(value)
	}
	context.Registers[index] = value
	return nil
}

func (context *Context) PC() uint32 {
	return context.Registers[RegisterPC]
}

func (context *Context) Thumb() bool {
	return context.CPSR&flagThumb != 0
}

// SetPC applies ARM's interworking address convention: bit zero selects Thumb
// state and is not retained in the aligned PC.
func (context *Context) SetPC(address uint32) error {
	if address&1 != 0 {
		context.CPSR |= flagThumb
		context.Registers[RegisterPC] = address &^ 1
		return nil
	}
	if address&3 != 0 {
		return fmt.Errorf("ARM PC %#x is not 4-byte aligned", address)
	}
	context.CPSR &^= flagThumb
	context.Registers[RegisterPC] = address
	return nil
}

func (context *Context) setThumbPC(address uint32) {
	context.CPSR |= flagThumb
	context.Registers[RegisterPC] = address &^ 1
}

func (context *Context) setARMPC(address uint32) {
	context.CPSR &^= flagThumb
	context.Registers[RegisterPC] = address &^ 3
}

func (context *Context) branchExchange(address uint32) {
	if address&1 != 0 {
		context.setThumbPC(address)
		return
	}
	context.setARMPC(address)
}

func (context *Context) setNZ(value uint32) {
	context.CPSR &^= flagNegative | flagZero
	if value&0x80000000 != 0 {
		context.CPSR |= flagNegative
	}
	if value == 0 {
		context.CPSR |= flagZero
	}
}

func (context *Context) setNZ64(value uint64) {
	context.CPSR &^= flagNegative | flagZero
	if value&0x8000000000000000 != 0 {
		context.CPSR |= flagNegative
	}
	if value == 0 {
		context.CPSR |= flagZero
	}
}

// setNZCV writes all four condition flags in one update. The single-flag
// setters each read CPSR, mask it, and write it back, so a data-processing
// instruction that sets N, Z, C, and V pays four round trips through the
// context pointer to produce one value. Three of the four are pure waste on
// every target.
func (context *Context) setNZCV(result uint32, carry, overflow bool) {
	cpsr := context.CPSR &^ (flagNegative | flagZero | flagCarry | flagOverflow)
	if result&0x80000000 != 0 {
		cpsr |= flagNegative
	}
	if result == 0 {
		cpsr |= flagZero
	}
	if carry {
		cpsr |= flagCarry
	}
	if overflow {
		cpsr |= flagOverflow
	}
	context.CPSR = cpsr
}

// setNZC is setNZCV for the shift and logical forms, which leave V alone.
func (context *Context) setNZC(result uint32, carry bool) {
	cpsr := context.CPSR &^ (flagNegative | flagZero | flagCarry)
	if result&0x80000000 != 0 {
		cpsr |= flagNegative
	}
	if result == 0 {
		cpsr |= flagZero
	}
	if carry {
		cpsr |= flagCarry
	}
	context.CPSR = cpsr
}

func (context *Context) carry() bool {
	return context.CPSR&flagCarry != 0
}
