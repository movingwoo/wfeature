package sgsvm

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"reflect"
	"sort"
)

const (
	stateResourceLimit = 16 << 20
	stateProgramLimit  = 128 << 10
	stateExternalLimit = 20
	stateBankLimit     = 65535
	stateStorageLimit  = stateResourceLimit + stateProgramLimit + stateExternalLimit*stateBankLimit
)

// State is an owned snapshot between complete SGS events. A new event resets
// the operand stack, return stack, instruction budget and host context.
type State struct {
	Version         uint32
	ProgramIdentity [32]byte
	PC              int
	Ended, Exited   bool
	Constants       []int16
	Variables       []VariableState
	Resources       []ResourceState
	ResourceViews   []ByteSliceState
	Buffers         [][]byte
	External        []ByteSliceState
}

// VariableState keeps constant aliases explicit instead of duplicating words.
type VariableState struct {
	Mutable, Constant bool
	Offset, Length    int
	Values            []int16
}

// ResourceState retains the archive's descriptor and its current allocation.
type ResourceState struct {
	Mutable bool
	Kind    byte
	Data    ByteSliceState
}

// ByteSliceState addresses an owned buffer by a one-based ID. Buffer zero is
// the empty slice; no record contains a Go pointer or an allocation capacity.
type ByteSliceState struct {
	Buffer, Offset, Length int
}

// CaptureState also retains external resource views, such as queued bitmaps.
// The caller serializes capture with Run and every platform service.
func (vm *VM) CaptureState(external [][]byte) (State, error) {
	if vm == nil || vm.err != nil || !vm.ended && (vm.ctx != nil || vm.PC != 0 || vm.exited) {
		return State{}, fmt.Errorf("SGS state requires a successful event boundary")
	}
	identity, err := stateProgramIdentity(vm.Program)
	if err != nil {
		return State{}, err
	}
	if len(vm.constants) != len(vm.Program.Constants) || len(vm.Variables) != len(vm.Program.Variables) ||
		len(vm.Resources) != len(vm.Program.Resources) || len(vm.resourceViews) != len(vm.Resources) || len(external) > stateExternalLimit {
		return State{}, fmt.Errorf("SGS state bank count disagrees with program")
	}
	saved := State{Version: 1, ProgramIdentity: identity, PC: vm.PC, Ended: vm.ended, Exited: vm.exited,
		Constants: append([]int16(nil), vm.constants...), Variables: make([]VariableState, len(vm.Variables)),
		Resources: make([]ResourceState, len(vm.Resources))}
	for i, variable := range vm.Variables {
		original := vm.Program.Variables[i]
		if variable.Mutable != original.Mutable || variable.Offset != original.Offset || len(variable.Values) != len(original.Values) {
			return State{}, fmt.Errorf("SGS variable %d disagrees with program", i)
		}
		constant := stateConstantAlias(original, len(vm.constants))
		if constant && len(variable.Values) != 0 && &variable.Values[0] != &vm.constants[variable.Offset] {
			return State{}, fmt.Errorf("SGS variable %d lost its constant alias", i)
		}
		v := VariableState{Mutable: variable.Mutable, Constant: constant, Offset: variable.Offset, Length: len(variable.Values)}
		if !constant {
			v.Values = append([]int16(nil), variable.Values...)
		}
		saved.Variables[i] = v
	}
	inputs := make([][]byte, 0, len(vm.Resources)*2+len(external))
	resourceBytes := 0
	for i, resource := range vm.Resources {
		original := vm.Program.Resources[i]
		if resource.Mutable != original.Mutable || resource.Kind != original.Kind || len(resource.Data) > stateBankLimit || len(resource.Data) > stateResourceLimit-resourceBytes {
			return State{}, fmt.Errorf("SGS resource %d descriptor or size is invalid", i)
		}
		resourceBytes += len(resource.Data)
		saved.Resources[i] = ResourceState{Mutable: resource.Mutable, Kind: resource.Kind}
		inputs = append(inputs, resource.Data)
	}
	for _, view := range vm.resourceViews {
		if len(view) > stateProgramLimit {
			return State{}, fmt.Errorf("SGS original resource view exceeds program limit")
		}
		inputs = append(inputs, view)
	}
	for _, view := range external {
		if len(view) > stateBankLimit {
			return State{}, fmt.Errorf("SGS external resource view exceeds bank limit")
		}
		inputs = append(inputs, view)
	}
	buffers, refs, err := captureByteSlices(inputs)
	if err != nil {
		return State{}, err
	}
	saved.Buffers = buffers
	for i := range saved.Resources {
		saved.Resources[i].Data = refs[i]
	}
	n := len(saved.Resources)
	saved.ResourceViews, saved.External = refs[n:2*n:2*n], refs[2*n:]
	if err := saved.validate(vm.Program, identity); err != nil {
		return State{}, err
	}
	return saved, nil
}

// RestoreState validates the complete record before allocating runtime banks.
// It executes no instruction and returns the restored external views in order.
func RestoreState(program *Program, services Services, saved State) (*VM, [][]byte, error) {
	identity, err := stateProgramIdentity(program)
	if err != nil {
		return nil, nil, err
	}
	if err := saved.validate(program, identity); err != nil {
		return nil, nil, err
	}
	buffers := make([][]byte, len(saved.Buffers))
	for i, buffer := range saved.Buffers {
		buffers[i] = bytes.Clone(buffer)
	}
	view := func(ref ByteSliceState) []byte {
		if ref.Buffer == 0 {
			return nil
		}
		return buffers[ref.Buffer-1][ref.Offset : ref.Offset+ref.Length : ref.Offset+ref.Length]
	}
	vm := &VM{Program: program, Services: services, PC: saved.PC, ended: saved.Ended, exited: saved.Exited,
		constants: append([]int16(nil), saved.Constants...), Variables: make([]Variable, len(saved.Variables)),
		Resources: make([]Resource, len(saved.Resources)), resourceViews: make([][]byte, len(saved.ResourceViews))}
	for i, variable := range saved.Variables {
		values := append([]int16(nil), variable.Values...)
		if variable.Constant {
			values = vm.constants[variable.Offset : variable.Offset+variable.Length]
		}
		vm.Variables[i] = Variable{Mutable: variable.Mutable, Offset: variable.Offset, Values: values}
	}
	for i, resource := range saved.Resources {
		vm.Resources[i] = Resource{Mutable: resource.Mutable, Kind: resource.Kind, Data: view(resource.Data)}
		vm.resourceViews[i] = view(saved.ResourceViews[i])
	}
	external := make([][]byte, len(saved.External))
	for i, ref := range saved.External {
		external[i] = view(ref)
	}
	return vm, external, nil
}

func stateConstantAlias(variable Variable, constants int) bool {
	return !variable.Mutable && variable.Offset >= 0 && variable.Offset <= constants && len(variable.Values) <= constants-variable.Offset
}

func (saved State) validate(program *Program, identity [32]byte) error {
	end := program.CodeEnd
	if end == 0 {
		end = len(program.Data)
	}
	if saved.Version != 1 || saved.ProgramIdentity != identity || saved.PC < 0 || saved.PC > end ||
		!saved.Ended && (saved.PC != 0 || saved.Exited) || saved.Ended && (saved.PC <= program.CodeStart || saved.PC == 0) {
		return fmt.Errorf("SGS state has an invalid version, program or event boundary")
	}
	if len(saved.Constants) != len(program.Constants) || len(saved.Variables) != len(program.Variables) ||
		len(saved.Resources) != len(program.Resources) || len(saved.ResourceViews) != len(program.Resources) ||
		len(saved.External) > stateExternalLimit || len(saved.Buffers) > len(saved.Resources)*2+len(saved.External) {
		return fmt.Errorf("SGS state has an invalid bank count")
	}
	for i, variable := range saved.Variables {
		original := program.Variables[i]
		if variable.Mutable != original.Mutable || variable.Offset != original.Offset || variable.Length != len(original.Values) ||
			variable.Constant != stateConstantAlias(original, len(program.Constants)) ||
			variable.Constant && len(variable.Values) != 0 || !variable.Constant && len(variable.Values) != variable.Length {
			return fmt.Errorf("SGS variable %d has an invalid descriptor or length", i)
		}
	}
	total := 0
	for _, buffer := range saved.Buffers {
		if len(buffer) == 0 || len(buffer) > stateProgramLimit || len(buffer) > stateStorageLimit-total {
			return fmt.Errorf("SGS state buffer allocation exceeds limit")
		}
		total += len(buffer)
	}
	used := make([]bool, len(saved.Buffers))
	check := func(ref ByteSliceState, limit int) error {
		if ref.Buffer == 0 {
			if ref.Offset != 0 || ref.Length != 0 {
				return fmt.Errorf("SGS state has a nonempty null buffer")
			}
			return nil
		}
		if ref.Buffer < 1 || ref.Buffer > len(saved.Buffers) || ref.Offset < 0 || ref.Length <= 0 || ref.Length > limit {
			return fmt.Errorf("SGS state has an invalid buffer reference")
		}
		length := len(saved.Buffers[ref.Buffer-1])
		if ref.Offset > length || ref.Length > length-ref.Offset {
			return fmt.Errorf("SGS state buffer range is out of bounds")
		}
		used[ref.Buffer-1] = true
		return nil
	}
	resourceBytes, originalBytes := 0, 0
	for _, resource := range program.Resources {
		originalBytes += len(resource.Data)
	}
	var firstView ByteSliceState
	firstOffset, cursor := 0, 0
	for i, resource := range saved.Resources {
		original := program.Resources[i]
		if resource.Mutable != original.Mutable || resource.Kind != original.Kind || resource.Data.Length > stateResourceLimit-resourceBytes {
			return fmt.Errorf("SGS resource %d has an invalid descriptor or allocation", i)
		}
		if err := check(resource.Data, stateBankLimit); err != nil {
			return err
		}
		resourceBytes += resource.Data.Length
		view := saved.ResourceViews[i]
		if err := check(view, stateProgramLimit); err != nil {
			return err
		}
		length := 0
		if !original.Mutable {
			length = originalBytes - cursor
		}
		if view.Length != length {
			return fmt.Errorf("SGS original resource view %d changed length", i)
		}
		if view.Length != 0 {
			if firstView.Buffer == 0 {
				firstView, firstOffset = view, cursor
			} else if view.Buffer != firstView.Buffer || view.Offset-firstView.Offset != cursor-firstOffset {
				return fmt.Errorf("SGS original resource views lost shared storage")
			}
		}
		cursor += len(original.Data)
	}
	for _, ref := range saved.External {
		if err := check(ref, stateBankLimit); err != nil {
			return err
		}
	}
	for _, referenced := range used {
		if !referenced {
			return fmt.Errorf("SGS state contains unreferenced storage")
		}
	}
	return nil
}

// The identity binds the archive's initialized banks and descriptors too, so
// an authored Program cannot reuse a record merely by sharing its code bytes.
func stateProgramIdentity(program *Program) ([32]byte, error) {
	if program == nil || len(program.Data) > stateProgramLimit || len(program.Name) > stateProgramLimit ||
		len(program.Constants) > stateProgramLimit/2 || len(program.Variables) > 16384/6 || len(program.Resources) > 16384/8 {
		return [32]byte{}, fmt.Errorf("SGS state program exceeds limits")
	}
	end := program.CodeEnd
	if end == 0 {
		end = len(program.Data)
	}
	if program.CodeStart < 0 || program.CodeStart > end || end > len(program.Data) {
		return [32]byte{}, fmt.Errorf("SGS state program code bounds are invalid")
	}
	h := sha256.New()
	var word [8]byte
	number := func(n int) {
		binary.LittleEndian.PutUint64(word[:], uint64(n))
		h.Write(word[:])
	}
	blob := func(data []byte) { number(len(data)); h.Write(data) }
	blob([]byte("SGS program state 1"))
	blob(program.Data)
	blob([]byte(program.Name))
	for _, n := range []int{program.CodeStart, program.CodeEnd, program.Width, program.Height} {
		number(n)
	}
	for _, entry := range program.Entries {
		if entry != 0 && (int(entry) < program.CodeStart || int(entry) >= end) {
			return [32]byte{}, fmt.Errorf("SGS state program entry is out of bounds")
		}
		number(int(entry))
	}
	number(len(program.Constants))
	for _, value := range program.Constants {
		number(int(value))
	}
	budget := len(program.Variables)*6 + len(program.Resources)*8
	number(len(program.Variables))
	for _, variable := range program.Variables {
		if variable.Offset < 0 || variable.Offset > stateProgramLimit/2 || len(variable.Values) > 255 {
			return [32]byte{}, fmt.Errorf("SGS state program variable is invalid")
		}
		number(int(truth(variable.Mutable)))
		number(variable.Offset)
		number(len(variable.Values))
		for _, value := range variable.Values {
			number(int(value))
		}
		if variable.Mutable {
			budget += len(variable.Values) * 2
		}
	}
	number(len(program.Resources))
	resources := 0
	for _, resource := range program.Resources {
		if len(resource.Data) > stateBankLimit || len(resource.Data) > stateProgramLimit-resources {
			return [32]byte{}, fmt.Errorf("SGS state program resource exceeds limits")
		}
		resources += len(resource.Data)
		if resource.Mutable {
			budget += len(resource.Data)
		}
		number(int(truth(resource.Mutable)))
		number(int(resource.Kind))
		blob(resource.Data)
	}
	if budget > 16384 {
		return [32]byte{}, fmt.Errorf("SGS state program runtime banks exceed limits")
	}
	return [32]byte(h.Sum(nil)), nil
}

func captureByteSlices(inputs [][]byte) ([][]byte, []ByteSliceState, error) {
	type span struct {
		start, end uintptr
		input      int
	}
	type region struct {
		start, end uintptr
		first      int
		spans      []span
	}
	spans := make([]span, 0, len(inputs))
	for i, data := range inputs {
		if len(data) == 0 {
			continue
		}
		start := reflect.ValueOf(data).Pointer()
		end := start + uintptr(len(data))
		if start == 0 || end < start {
			return nil, nil, fmt.Errorf("SGS state byte storage range is invalid")
		}
		spans = append(spans, span{start: start, end: end, input: i})
	}
	sort.Slice(spans, func(i, j int) bool {
		if spans[i].start != spans[j].start {
			return spans[i].start < spans[j].start
		}
		return spans[i].input < spans[j].input
	})
	var regions []region
	for _, current := range spans {
		if len(regions) == 0 || current.start >= regions[len(regions)-1].end {
			regions = append(regions, region{start: current.start, end: current.end, first: current.input, spans: []span{current}})
			continue
		}
		group := &regions[len(regions)-1]
		group.end = max(group.end, current.end)
		group.first = min(group.first, current.input)
		group.spans = append(group.spans, current)
	}
	// Addresses locate overlaps only. IDs follow caller order, so another
	// process produces the same record with completely different allocations.
	sort.Slice(regions, func(i, j int) bool { return regions[i].first < regions[j].first })
	refs := make([]ByteSliceState, len(inputs))
	buffers := make([][]byte, len(regions))
	total := 0
	for i, group := range regions {
		size := group.end - group.start
		if size > stateProgramLimit || size > uintptr(stateStorageLimit-total) {
			return nil, nil, fmt.Errorf("SGS state byte storage exceeds limit")
		}
		total += int(size)
		buffer := make([]byte, int(size))
		for _, current := range group.spans {
			offset := int(current.start - group.start)
			copy(buffer[offset:], inputs[current.input])
			refs[current.input] = ByteSliceState{Buffer: i + 1, Offset: offset, Length: len(inputs[current.input])}
		}
		buffers[i] = buffer
	}
	return buffers, refs, nil
}
