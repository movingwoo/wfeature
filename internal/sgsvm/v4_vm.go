package sgsvm

import (
	"context"
	"encoding/binary"
	"fmt"
)

// WideVM executes 16-bit opcodes over signed 32-bit words. Guest pointers
// count words from the beginning of the flat symbol-data section.
type WideVM struct {
	Variables [][]int32
	Resources []Resource
	PC        int
	Exited    bool

	program    *Program
	service    func(uint16, *WideVM) error
	data       []int32
	bases      []int
	stack      []int32
	returns    []int
	savedDepth int
	work       int
	ctx        context.Context
	err        error
	ended      bool
}

func NewWide(p *Program, service func(uint16, *WideVM) error) *WideVM {
	v := &WideVM{program: p, service: service}
	size := 0
	for _, bank := range p.Wide.Variables {
		size += len(bank)
	}
	v.data = make([]int32, size)
	off := 0
	for _, bank := range p.Wide.Variables {
		end := off + len(bank)
		values := v.data[off:end:end]
		copy(values, bank)
		v.bases = append(v.bases, off)
		v.Variables = append(v.Variables, values)
		off = end
	}
	for _, r := range p.Resources {
		if r.Mutable {
			r.Data = append([]byte(nil), r.Data...)
		}
		v.Resources = append(v.Resources, r)
	}
	return v
}

func (v *WideVM) fail(err error) {
	if v.err == nil {
		v.err = err
	}
}

func (v *WideVM) Address(addr int32, count int) ([]int32, error) {
	if addr < 0 || int(addr) > len(v.data) || count < 0 || count > len(v.data)-int(addr) {
		return nil, fmt.Errorf("SGS word address %d count %d outside symbol data", addr, count)
	}
	return v.data[int(addr) : int(addr)+count : int(addr)+count], nil
}

func (v *WideVM) Charge(n int) error {
	if n < 0 || n > MaxSteps-v.work {
		return fmt.Errorf("SGS execution work limit %d reached", MaxSteps)
	}
	v.work += n
	if v.ctx != nil {
		return v.ctx.Err()
	}
	return nil
}

func (v *WideVM) Push(word int32) error {
	if len(v.stack) >= 4096 {
		return fmt.Errorf("operand stack overflow")
	}
	v.stack = append(v.stack, word)
	return nil
}

func (v *WideVM) Args(n int) ([]int32, error) {
	if n < 0 || n > len(v.stack) {
		return nil, fmt.Errorf("operand stack underflow: need %d, have %d", n, len(v.stack))
	}
	first := len(v.stack) - n
	args := append([]int32(nil), v.stack[first:]...)
	v.stack = v.stack[:first]
	return args, nil
}

func (v *WideVM) push(x int32) {
	if v.err == nil {
		v.fail(v.Push(x))
	}
}
func (v *WideVM) pop() int32 {
	if len(v.stack) == 0 {
		v.fail(fmt.Errorf("operand stack underflow"))
		return 0
	}
	last := len(v.stack) - 1
	word := v.stack[last]
	v.stack = v.stack[:last]
	return word
}

func (v *WideVM) word() uint16 {
	if v.PC < v.program.CodeStart || v.PC > v.program.CodeEnd-2 || v.PC&1 != 0 {
		v.fail(fmt.Errorf("truncated instruction or invalid address"))
		return 0
	}
	word := binary.LittleEndian.Uint16(v.program.Data[v.PC:])
	v.PC += 2
	return word
}

func (v *WideVM) long() int32 {
	high := uint32(v.word())
	return int32(high<<16 | uint32(v.word()))
}

func (v *WideVM) jump(pc int) {
	if pc < v.program.CodeStart || pc >= v.program.CodeEnd || pc&1 != 0 {
		v.fail(fmt.Errorf("branch target %#x outside code", pc))
		return
	}
	v.PC = pc
}

// A direct symbol reference checks its declared array extent. An indirect
// pointer is relative to the full symbol-data section and may cross banks.
func (v *WideVM) symbol(id int, index int32) int {
	if id >= len(v.Variables) || index < 0 || int(index) >= len(v.Variables[id]) {
		v.fail(fmt.Errorf("symbol %d index %d outside declared extent", id, index))
		return 0
	}
	return v.bases[id] + int(index)
}

func (v *WideVM) location(mode uint16) int {
	id := int(v.word())
	var index int32
	switch mode {
	case 0:
		index = v.read(v.symbol(int(v.word()), int32(v.word())))
	case 1:
		index = v.read(v.symbol(int(v.word()), 0))
	case 2:
		index = int32(v.word())
	}
	return v.symbol(id, index)
}

func (v *WideVM) read(addr int) int32 {
	if v.err != nil {
		return 0
	}
	return v.data[addr]
}
func (v *WideVM) write(addr int, value int32) {
	if v.err == nil {
		v.data[addr] = value
	}
}

// EndEvent returns from the current callback while keeping the session alive.
func (v *WideVM) EndEvent() { v.ended = true }

func (v *WideVM) Run(ctx context.Context, entry int) error {
	if entry == 0 || v.Exited {
		return nil
	}
	v.ctx = ctx
	defer func() { v.ctx = nil }()
	v.err = nil
	v.ended = false
	v.work = 0
	v.stack = v.stack[:0]
	v.savedDepth = 0
	v.returns = v.returns[:0]
	v.jump(entry)
	for v.err == nil && !v.ended && !v.Exited {
		pc := v.PC
		if err := v.Charge(1); err != nil {
			return fmt.Errorf("SGS v4 at %#x: %w", pc, err)
		}
		op := v.word()
		if v.err == nil {
			v.step(op)
		}
		if v.err != nil {
			return fmt.Errorf("SGS v4 opcode %#x at %#x: %w", op, pc, v.err)
		}
	}
	return v.err
}

func (v *WideVM) step(op uint16) {
	switch {
	case op == 0 || op == 0x88:
	case op >= 1 && op <= 4:
		v.push(v.read(v.location(op - 1)))
	case op == 5:
		v.push(int32(int16(v.word())))
	case op == 6:
		v.push(int32(v.word()))
	case op == 7:
		v.push(v.long())
	case op >= 8 && op <= 11:
		v.write(v.location(op-8), v.pop())
	case op == 0x0c:
		v.savedDepth = len(v.stack)
	case op == 0x0d:
		v.stack = v.stack[:v.savedDepth]
	case op == 0x0e:
		v.push(v.pop() + 1)
	case op == 0x0f:
		v.push(v.pop() - 1)
	case op == 0x10:
		x := v.pop()
		v.push(x)
		v.push(x)
	case op == 0x11:
		v.push(-v.pop())
	case op == 0x12:
		b, a := v.pop(), v.pop()
		v.push(b)
		v.push(a)
	case op == 0x55:
		v.push(int32(truth(v.pop() == 0)))
	case op == 0x13 || op == 0x14 || op >= 0x50 && op <= 0x5e:
		v.binary(op)
	case op >= 0x5f && op <= 0x76:
		// Four destination modes, each with six source modes. The last
		// two sources are a signed constant or an unsigned media index.
		dest := v.location((op - 0x5f) / 6)
		src := (op - 0x5f) % 6
		var value int32
		switch src {
		case 4:
			value = int32(int16(v.word()))
		case 5:
			value = int32(v.word())
		default:
			value = v.read(v.location(src))
		}
		v.write(dest, value)
	case op >= 0x77 && op <= 0x7a:
		addr := v.location(op - 0x77)
		v.write(addr, v.read(addr)+int32(int16(v.word())))
	case op >= 0x7b && op <= 0x80:
		b := int32(int16(v.word()))
		dest := int(v.long())
		a := v.pop()
		if compareWide(op-0x7b, a, b) {
			v.jump(dest)
		}
	case op >= 0x81 && op <= 0x83 || op == 0x85:
		dest := int(v.long())
		switch op {
		case 0x82:
			if v.pop() == 0 {
				return
			}
		case 0x83:
			if v.pop() != 0 {
				return
			}
		case 0x85:
			if len(v.returns) >= 1024 {
				v.fail(fmt.Errorf("return stack overflow"))
				return
			}
			v.returns = append(v.returns, v.PC)
		}
		v.jump(dest)
	case op == 0x86:
		last := len(v.returns) - 1
		if last < 0 {
			v.ended = true
			return
		}
		v.jump(v.returns[last])
		v.returns = v.returns[:last]
	case op == 0x87:
		v.Exited = true
	case op >= 0x89 && op <= 0x8c:
		addr := v.location(op - 0x89)
		x := v.pop()
		v.write(addr, x)
		v.push(x)
	case op == 0x8d || op == 0x8e:
		v.push(int32(v.location(op - 0x8d + 2)))
	case op == 0x8f:
		data, err := v.Address(v.pop(), 1)
		if err != nil {
			v.fail(err)
			return
		}
		v.push(data[0])
	case op == 0x90 || op == 0x91:
		x, addr := v.pop(), v.pop()
		// The indirect-store resolver reserves the first twenty system
		// words. The host still publishes event and display data there.
		if addr < 20 {
			v.fail(fmt.Errorf("invalid indirect store to word %d", addr))
			return
		}
		data, err := v.Address(addr, 1)
		if err != nil {
			v.fail(err)
			return
		}
		if v.err != nil {
			return
		}
		data[0] = x
		if op == 0x91 {
			v.push(x)
		}
	case op == 0x92 || op >= 0x94 && op <= 0x99:
		id := uint16(0xf000) | op
		if op == 0x92 {
			id = v.word()
		}
		if v.err != nil {
			return
		}
		if v.service == nil {
			v.fail(fmt.Errorf("unsupported service %#x", id))
			return
		}
		v.fail(v.service(id, v))
	case op == 0xe8 || op == 0xeb:
		a := v.read(v.location(3))
		b := v.read(v.location(3))
		if op == 0xe8 {
			b += int32(int16(v.word()))
		}
		v.push(a + b)
	case op == 0xee:
		v.push(v.pop() + v.read(v.location(3)))
	case op == 0xf1:
		base := v.location(3)
		v.push(int32(base) + int32(int16(v.word())))
	case op == 0xff:
		v.ended = true
	default:
		v.fail(fmt.Errorf("unknown instruction"))
	}
}

func compareWide(op uint16, a, b int32) bool {
	switch op {
	case 0:
		return a > b
	case 1:
		return a < b
	case 2:
		return a >= b
	case 3:
		return a <= b
	case 4:
		return a == b
	default:
		return a != b
	}
}

func (v *WideVM) binary(op uint16) {
	b, a := v.pop(), v.pop()
	var result int32
	switch op {
	case 0x13:
		result = a + b
	case 0x14:
		result = a - b
	case 0x50:
		result = a * b
	case 0x51, 0x52:
		if b == 0 {
			v.fail(fmt.Errorf("division by zero"))
			return
		}
		if op == 0x51 {
			result = a / b
		} else {
			result = a % b
		}
	case 0x53:
		result = a & b
	case 0x54:
		result = a | b
	case 0x56:
		result = a ^ b
	case 0x57:
		result = a >> uint8(b)
	case 0x58:
		result = a << uint8(b)
	default:
		result = int32(truth(compareWide(op-0x59, a, b)))
	}
	v.push(result)
}
