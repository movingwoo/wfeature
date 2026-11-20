package sgsvm

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"slices"
)

// WideState records the persistent guest memory between callbacks. A callback
// starts with fresh stacks, instruction budget and host context.
type WideState struct {
	Version         uint32
	ProgramIdentity [32]byte
	PC              int
	Exited          bool
	Words           []int32
	Mutable         []WideResourceState
}

// WideResourceState contains one mutable bank, in program descriptor order.
// The guest's allocation limit accounts for capacity even after a short write.
type WideResourceState struct {
	Data     []byte
	Capacity int
}

// CaptureState takes an owned snapshot. The caller serializes capture with
// guest execution and host services.
func (vm *WideVM) CaptureState() (WideState, error) {
	if vm == nil || vm.ctx != nil || vm.err != nil || vm.PC != 0 && !vm.ended && !vm.Exited {
		return WideState{}, fmt.Errorf("SGS v4 state requires a completed event")
	}
	id, words, err := wideStateIdentity(vm.program)
	if err != nil {
		return WideState{}, err
	}
	p := vm.program
	if len(vm.data) != words || len(vm.Variables) != len(p.Wide.Variables) ||
		len(vm.bases) != len(vm.Variables) || len(vm.Resources) != len(p.Resources) {
		return WideState{}, fmt.Errorf("SGS v4 bank count disagrees with program")
	}
	offset := 0
	for i, bank := range vm.Variables {
		if vm.bases[i] != offset || len(bank) != len(p.Wide.Variables[i]) ||
			len(bank) != 0 && &bank[0] != &vm.data[offset] {
			return WideState{}, fmt.Errorf("SGS v4 variable %d lost its word range", i)
		}
		offset += len(bank)
	}
	s := WideState{Version: 1, ProgramIdentity: id, PC: vm.PC, Exited: vm.Exited, Words: vm.data}
	for i, r := range vm.Resources {
		orig := p.Resources[i]
		if r.Mutable != orig.Mutable || r.Kind != orig.Kind {
			return WideState{}, fmt.Errorf("SGS v4 resource %d changed descriptor", i)
		}
		if r.Mutable {
			s.Mutable = append(s.Mutable, WideResourceState{Data: r.Data, Capacity: cap(r.Data)})
		} else if len(r.Data) != len(orig.Data) || cap(r.Data) != cap(orig.Data) ||
			len(r.Data) != 0 && &r.Data[0] != &orig.Data[0] {
			return WideState{}, fmt.Errorf("SGS v4 immutable resource %d changed storage", i)
		}
	}
	if err := s.validate(p, id, words); err != nil {
		return WideState{}, err
	}
	s.Words = slices.Clone(s.Words)
	for i := range s.Mutable {
		s.Mutable[i].Data = slices.Clone(s.Mutable[i].Data)
	}
	return s, nil
}

// RestoreWideState validates the record before allocating guest memory.
// The program supplies immutable media and the shape of each flat word bank.
func RestoreWideState(p *Program, service func(uint16, *WideVM) error, saved WideState) (*WideVM, error) {
	id, words, err := wideStateIdentity(p)
	if err != nil {
		return nil, err
	}
	if err := saved.validate(p, id, words); err != nil {
		return nil, err
	}
	vm := NewWide(p, service)
	vm.PC, vm.Exited, vm.ended = saved.PC, saved.Exited, saved.PC != 0 && !saved.Exited
	copy(vm.data, saved.Words)
	next := 0
	for i, r := range vm.Resources {
		if r.Mutable {
			bank := saved.Mutable[next]
			next++
			data := make([]byte, len(bank.Data), bank.Capacity)
			copy(data, bank.Data)
			vm.Resources[i].Data = data
		}
	}
	return vm, nil
}

func (s *WideState) validate(p *Program, identity [32]byte, words int) error {
	if s.Version != 1 || s.ProgramIdentity != identity || s.PC&1 != 0 ||
		s.PC == 0 && s.Exited || s.PC != 0 && (s.PC <= p.CodeStart || s.PC > p.CodeEnd) {
		return fmt.Errorf("SGS v4 state has an invalid version, program or event boundary")
	}
	if len(s.Words) != words || len(s.Mutable) > len(p.Resources) {
		return fmt.Errorf("SGS v4 state has an invalid word or resource count")
	}
	heap, next := 0, 0
	for i, r := range p.Resources {
		size := cap(r.Data)
		if r.Mutable {
			if next == len(s.Mutable) {
				return fmt.Errorf("SGS v4 state missing mutable resource %d", i)
			}
			bank := s.Mutable[next]
			next++
			size = bank.Capacity
			if size < len(bank.Data) || size > MaxWideSize {
				return fmt.Errorf("SGS v4 state resource %d has invalid capacity", i)
			}
		}
		if size > stateResourceLimit-heap {
			return fmt.Errorf("SGS v4 state exceeds media heap limit")
		}
		heap += size
	}
	if next != len(s.Mutable) {
		return fmt.Errorf("SGS v4 state has extra mutable resources")
	}
	return nil
}

func wideStateIdentity(p *Program) ([32]byte, int, error) {
	if p == nil || p.Wide == nil || len(p.Data) > MaxWideSize || len(p.Name) > MaxWideSize ||
		p.CodeStart < 0 || p.CodeStart >= p.CodeEnd || p.CodeEnd > len(p.Data) ||
		(p.CodeStart|p.CodeEnd)&1 != 0 || len(p.Wide.Variables) > 65536 || len(p.Resources) > 65536 {
		return [32]byte{}, 0, fmt.Errorf("SGS v4 state has an invalid program")
	}
	h := sha256.New()
	var scratch [8]byte
	number := func(n int) {
		binary.LittleEndian.PutUint64(scratch[:], uint64(n))
		h.Write(scratch[:])
	}
	number(len(p.Data))
	h.Write(p.Data)
	number(len(p.Name))
	h.Write([]byte(p.Name))
	for _, n := range []int{p.CodeStart, p.CodeEnd, p.Width, p.Height} {
		number(n)
	}
	for _, entry := range p.Wide.Entries {
		if entry != 0 && (entry < p.CodeStart || entry >= p.CodeEnd || entry&1 != 0) {
			return [32]byte{}, 0, fmt.Errorf("SGS v4 state has an invalid program callback")
		}
		number(entry)
	}
	number(len(p.Wide.Variables))
	words := 0
	for _, bank := range p.Wide.Variables {
		if len(bank) > 65535 || len(bank) > MaxWideSize/4-words {
			return [32]byte{}, 0, fmt.Errorf("SGS v4 state program exceeds word limit")
		}
		words += len(bank)
		number(len(bank))
		for _, word := range bank {
			number(int(word))
		}
	}
	number(len(p.Resources))
	total := 0
	for _, r := range p.Resources {
		if len(r.Data) > MaxWideSize-total || r.Kind > 2 || !r.Mutable && cap(r.Data) > MaxWideSize {
			return [32]byte{}, 0, fmt.Errorf("SGS v4 state program has invalid resource")
		}
		total += len(r.Data)
		number(int(truth(r.Mutable)))
		number(int(r.Kind))
		if !r.Mutable {
			number(cap(r.Data))
		}
		number(len(r.Data))
		h.Write(r.Data)
	}
	return [32]byte(h.Sum(nil)), words, nil
}
