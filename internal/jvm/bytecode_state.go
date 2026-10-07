package jvm

import (
	"encoding/binary"
	"errors"
	"fmt"
	"slices"

	"github.com/movingwoo/wfeature/internal/jvm/classfile"
)

// BytecodeExecutionState is the continuation of one parked interpreter entry.
// It records execution, not the heap or the scheduler. Reference numbers index
// the explicit roots returned by CaptureBytecodeState, starting at one. The
// caller must retain those roots in the same heap as its platform objects.
//
// A represented native tail is completed by the platform at resume. Other Go
// remainders and in-progress class initialization are refused. The core's
// forwarding Thread.run is supported because only a void return remains in it.
type BytecodeExecutionState struct {
	Version    uint32
	ID         uint64
	Steps      uint64
	MaxSteps   uint64
	MaxFrames  int
	Thread     uint32
	ThreadRuns int
	NativeTail bool
	RootCount  int
	Frames     []BytecodeFrameState
}

type BytecodeFrameState struct {
	Class, Method, Descriptor string
	PC, InvokePC              int
	ThreadRuns                int
	SynchronizedReceiver      uint32
	MonitorPending            bool
	Locals, Stack             []HeapValueState
}

// CaptureBytecodeState requires the entire execution to be parked. A native
// tail count of one declares that its caller separately represents the leaf
// native's remainder; zero denotes a boundary before the next instruction.
// Capture neither assigns identities nor mutates the source execution.
func (call *Invocation) CaptureBytecodeState(representedNativeCalls int) (BytecodeExecutionState, []*Object, error) {
	if call == nil || call.vm == nil || call.state == nil {
		return BytecodeExecutionState{}, nil, fmt.Errorf("capture JVM bytecode without an invocation")
	}
	s := call.state
	if representedNativeCalls < 0 || representedNativeCalls > 1 || s.nativeDepth != s.threadRuns+representedNativeCalls || len(s.initializing) != 0 {
		return BytecodeExecutionState{}, nil, fmt.Errorf("JVM bytecode has an unrepresented initializer or native remainder")
	}
	saved := BytecodeExecutionState{Version: 1, ID: s.id, Steps: s.steps, MaxSteps: call.vm.config.MaxSteps,
		MaxFrames: call.vm.config.MaxFrames, ThreadRuns: s.threadRuns, NativeTail: representedNativeCalls == 1}
	var roots []*Object
	ids := make(map[*Object]uint32)
	ref := func(object *Object) uint32 {
		if object == nil {
			return 0
		}
		if id := ids[object]; id != 0 {
			return id
		}
		roots = append(roots, object)
		id := uint32(len(roots))
		ids[object] = id
		return id
	}
	values := func(input []Value) []HeapValueState {
		result := make([]HeapValueState, len(input))
		for i, value := range input {
			result[i] = HeapValueState{Kind: value.kind, Bits: value.bits, Reference: ref(value.ref)}
		}
		return result
	}
	saved.Thread = ref(s.thread)
	budget := heapBudget{}
	for f := s.topFrame; f != nil; f = f.parent {
		if len(saved.Frames) >= call.vm.config.MaxFrames || f.nativeDepth != f.threadRuns {
			return BytecodeExecutionState{}, nil, fmt.Errorf("JVM bytecode frame has an unrepresented native caller")
		}
		if err := budget.add(len(f.locals)+len(f.stack), 32); err != nil {
			return BytecodeExecutionState{}, nil, err
		}
		saved.Frames = append(saved.Frames, BytecodeFrameState{Class: f.class.Name, Method: f.method.Name, Descriptor: f.method.Descriptor,
			PC: f.pc, InvokePC: f.opcodePC, ThreadRuns: f.threadRuns, SynchronizedReceiver: ref(f.syncReceiver), MonitorPending: f.monitorPending,
			Locals: values(f.locals), Stack: values(f.stack)})
	}
	if len(saved.Frames) != s.frames {
		return BytecodeExecutionState{}, nil, fmt.Errorf("JVM bytecode frame chain is incomplete")
	}
	slices.Reverse(saved.Frames)
	saved.RootCount = len(roots)
	if _, err := call.vm.prepareBytecodeState(saved, roots); err != nil {
		return BytecodeExecutionState{}, nil, err
	}
	return saved, roots, nil
}

// ValidateBytecodeState resolves immutable method bodies and checks the record
// before the platform starts any restored worker. It executes no guest code
// and reserves no execution identity. Monitor ownership belongs to the heap
// component and is checked again when execution is resumed.
func (vm *VM) ValidateBytecodeState(saved BytecodeExecutionState, roots []*Object) error {
	_, err := vm.prepareBytecodeState(saved, roots)
	return err
}

func (vm *VM) prepareBytecodeState(saved BytecodeExecutionState, roots []*Object) ([]*frame, error) {
	invalid := func(reason string) ([]*frame, error) {
		return nil, fmt.Errorf("JVM bytecode checkpoint: %s", reason)
	}
	if vm == nil || saved.Version != 1 || saved.ID == 0 || saved.ID > maxRestoredExecutionID || saved.Steps > saved.MaxSteps ||
		saved.MaxSteps != vm.config.MaxSteps || saved.MaxFrames != vm.config.MaxFrames || saved.ThreadRuns < 0 || saved.ThreadRuns > 64 ||
		saved.RootCount != len(roots) || saved.RootCount > heapStateObjects || uint64(saved.Thread) > uint64(len(roots)) ||
		len(saved.Frames) == 0 || len(saved.Frames) > saved.MaxFrames {
		return invalid("incompatible policy, ownership, roots or frame count")
	}
	ref := func(id uint32) *Object {
		if id == 0 {
			return nil
		}
		return roots[id-1]
	}
	budget := heapBudget{}
	frames := make([]*frame, 0, len(saved.Frames))
	for i, record := range saved.Frames {
		if !heapClass(record.Class) || !heapText(record.Method) || !heapText(record.Descriptor) || record.Method == "<clinit>" ||
			record.ThreadRuns < 0 || record.ThreadRuns > saved.ThreadRuns || i > 0 && record.ThreadRuns < saved.Frames[i-1].ThreadRuns ||
			uint64(record.SynchronizedReceiver) > uint64(len(roots)) {
			return invalid("invalid frame identity or forwarding depth")
		}
		class, err := vm.loader.Load(record.Class)
		if err != nil {
			return nil, fmt.Errorf("JVM bytecode checkpoint class: %w", err)
		}
		method := class.FindMethod(record.Method, record.Descriptor)
		if method == nil || method.CodeAttribute() == nil {
			return invalid("recorded bytecode method is missing")
		}
		code := method.CodeAttribute()
		if record.MonitorPending && (i != len(saved.Frames)-1 || saved.NativeTail || method.AccessFlags&AccessSynchronized == 0 || record.PC != 0 || record.InvokePC != 0 || len(record.Stack) != 0) {
			return invalid("invalid pending synchronized entry")
		}
		ends, err := bytecodeInstructionEnds(code.Bytecode)
		if err != nil {
			return nil, err
		}
		if _, ok := ends[record.PC]; !ok {
			return invalid("resume PC is not an instruction boundary")
		}
		if _, ok := ends[record.InvokePC]; !ok {
			return invalid("instruction PC is not an instruction boundary")
		}
		pending := i+1 < len(saved.Frames) || saved.NativeTail
		if pending {
			if ends[record.InvokePC] != record.PC {
				return invalid("pending invocation does not end at resume PC")
			}
			if _, err := bytecodePendingReturn(class, code, record.InvokePC); err != nil {
				return nil, err
			}
		}
		if len(record.Locals) != int(code.MaxLocals) || len(record.Stack) > int(code.MaxStack) {
			return invalid("locals or operands exceed method limits")
		}
		if err := budget.add(len(record.Locals)+len(record.Stack), 32); err != nil {
			return nil, err
		}
		returnType, err := ReturnTypeOf(method.Descriptor)
		if err != nil {
			return nil, err
		}
		f := &frame{class: class, method: method, code: code, pc: record.PC, opcodePC: record.InvokePC,
			returnType: returnType, nativeDepth: record.ThreadRuns, threadRuns: record.ThreadRuns,
			syncReceiver: ref(record.SynchronizedReceiver), monitorPending: record.MonitorPending}
		if method.AccessFlags&0x0020 != 0 && method.AccessFlags&0x0008 == 0 {
			if f.syncReceiver == nil {
				return invalid("synchronized method has no receiver")
			}
		} else if record.SynchronizedReceiver != 0 {
			return invalid("unexpected synchronized receiver")
		}
		decode := func(input []HeapValueState, locals bool) ([]Value, error) {
			result := make([]Value, len(input))
			for j, value := range input {
				valid := value.Kind <= valueTop && uint64(value.Reference) <= uint64(len(roots))
				if value.Kind == ValueReference {
					valid = valid && value.Bits == 0
				} else {
					valid = valid && value.Reference == 0
				}
				switch value.Kind {
				case ValueVoid, valueTop:
					valid = valid && locals && value.Bits == 0
				case ValueInt, ValueFloat:
					valid = valid && value.Bits <= 1<<32-1
				case valueReturnAddress:
					_, boundary := ends[int(value.Bits)]
					valid = valid && value.Bits < uint64(len(code.Bytecode)) && boundary
				}
				if !valid {
					return nil, fmt.Errorf("JVM bytecode checkpoint contains an invalid value")
				}
				result[j] = Value{kind: value.Kind, bits: value.Bits, ref: ref(value.Reference)}
			}
			return result, nil
		}
		if f.locals, err = decode(record.Locals, true); err != nil {
			return nil, err
		}
		stack, err := decode(record.Stack, false)
		if err != nil {
			return nil, err
		}
		for _, value := range stack {
			if err := f.push(value); err != nil {
				return nil, err
			}
		}
		if i > 0 {
			f.parent = frames[i-1]
			want, err := bytecodePendingReturn(f.parent.class, f.parent.code, f.parent.opcodePC)
			if err != nil {
				return nil, err
			}
			if want.Descriptor() != f.returnType.Descriptor() || record.ThreadRuns > saved.Frames[i-1].ThreadRuns && want.Kind != TypeVoid {
				return invalid("child return does not match pending invocation")
			}
		}
		frames = append(frames, f)
	}
	last := frames[len(frames)-1]
	if saved.ThreadRuns != last.threadRuns {
		want, err := bytecodePendingReturn(last.class, last.code, last.opcodePC)
		if !saved.NativeTail || err != nil || want.Kind != TypeVoid {
			return invalid("forwarding remainder does not return void")
		}
	}
	if frames[0].threadRuns > 0 && frames[0].returnType.Kind != TypeVoid {
		return invalid("Thread.run body does not return void")
	}
	return frames, nil
}

// ResumeBytecodeExecution resumes the saved leaf, then returns through each
// caller with the same exception tables, operand stack and instruction budget.
// The VM and roots must be detached or parked and belong to the saved heap.
func (vm *VM) ResumeBytecodeExecution(saved BytecodeExecutionState, roots []*Object, complete func(*Invocation) (Value, error)) (Value, error) {
	return vm.resumeBytecodeExecution(saved, roots, complete, nil)
}

func (vm *VM) resumeBytecodeExecution(saved BytecodeExecutionState, roots []*Object, complete func(*Invocation) (Value, error), state *execution) (Value, error) {
	frames, err := vm.prepareBytecodeState(saved, roots)
	if err != nil {
		return VoidValue(), err
	}
	if saved.NativeTail != (complete != nil) {
		return VoidValue(), fmt.Errorf("JVM bytecode checkpoint native remainder is missing or unexpected")
	}
	// A synchronized frame already acquired its monitor before capture. Never
	// acquire it twice or invent ownership on a heap restored without monitors.
	var released *bytecodeThreadWait
	if state != nil {
		released = state.wait
	}
	if err := vm.validateBytecodeMonitors(frames, saved.ID, released); err != nil {
		return VoidValue(), err
	}
	for next := vm.nextExecution.Load(); next < saved.ID; next = vm.nextExecution.Load() {
		if vm.nextExecution.CompareAndSwap(next, saved.ID) {
			break
		}
	}
	if state == nil {
		state = &execution{id: saved.ID, steps: saved.Steps, initializing: make(map[string]bool)}
	}
	if saved.Thread != 0 {
		state.thread = roots[saved.Thread-1]
	}
	var resume func(int) (Value, error)
	resume = func(index int) (result Value, err error) {
		f := frames[index]
		state.frames = index + 1
		state.topFrame = f
		state.nativeDepth, state.threadRuns = f.threadRuns, f.threadRuns
		if f.monitorPending {
			if err := vm.enterMonitor(state, vm.bytecodeFrameMonitor(f)); err != nil {
				return VoidValue(), err
			}
			f.monitorPending = false
		}
		defer func() {
			if m := vm.bytecodeFrameMonitor(f); m != nil {
				if exitErr := m.exit(state.id); err == nil {
					err = exitErr
				}
			}
			state.topFrame = f.parent
			state.frames = index
			frames[index] = nil
			state.releaseFrame(f)
		}()
		pending := index+1 < len(frames) || saved.NativeTail
		if index+1 < len(frames) {
			result, err = resume(index + 1)
		} else if saved.NativeTail {
			state.nativeDepth, state.threadRuns = saved.ThreadRuns+1, saved.ThreadRuns
			result, err = complete(&Invocation{vm: vm, state: state})
			complete = nil
		}
		state.frames = index + 1
		state.topFrame = f
		state.nativeDepth, state.threadRuns = f.threadRuns, f.threadRuns
		if pending {
			if err == nil {
				want, _ := bytecodePendingReturn(f.class, f.code, f.opcodePC)
				err = validateValue(result, want)
				if err == nil && want.Kind != TypeVoid {
					err = f.push(result)
				}
			}
			if err != nil {
				var guest *GuestException
				if errors.As(err, &guest) {
					handled, handleErr := vm.handleException(f, f.opcodePC, guest)
					if handleErr != nil {
						return VoidValue(), handleErr
					}
					if handled {
						return vm.executeFrame(state, f)
					}
				}
				return VoidValue(), &ExecutionError{Class: f.class.Name, Method: f.method.Name, Descriptor: f.method.Descriptor,
					PC: f.opcodePC, Opcode: f.code.Bytecode[f.opcodePC], Cause: err}
			}
		}
		return vm.executeFrame(state, f)
	}
	return resume(0)
}

func (vm *VM) validateBytecodeMonitors(frames []*frame, owner uint64, released ...*bytecodeThreadWait) error {
	depths := make(map[*monitor]int)
	for _, f := range frames {
		if m := vm.bytecodeFrameMonitor(f); m != nil && !f.monitorPending {
			depths[m]++
		}
	}
	for m, depth := range depths {
		if len(released) > 0 && released[0] != nil && released[0].kind == "wait" && m == &released[0].object.monitor {
			if released[0].depth < depth {
				return fmt.Errorf("JVM bytecode checkpoint released monitor depth is too small")
			}
			continue
		}
		m.mu.Lock()
		owned := m.owner == owner && m.depth >= depth
		m.mu.Unlock()
		if !owned {
			return fmt.Errorf("JVM bytecode checkpoint synchronized monitor is not restored")
		}
	}
	return nil
}

func (vm *VM) bytecodeFrameMonitor(f *frame) *monitor {
	if f.method.AccessFlags&0x0020 == 0 {
		return nil
	}
	if f.method.AccessFlags&0x0008 != 0 {
		return vm.classMonitor(f.class.Name)
	}
	return &f.syncReceiver.monitor
}

func bytecodePendingReturn(class *classfile.Class, code *classfile.Code, pc int) (Type, error) {
	if pc < 0 || pc+3 > len(code.Bytecode) || code.Bytecode[pc] < 0xb6 || code.Bytecode[pc] > 0xb9 {
		return Type{}, fmt.Errorf("JVM bytecode checkpoint pending instruction is not an invocation")
	}
	reference, err := class.ConstantPool.ReferenceAt(binary.BigEndian.Uint16(code.Bytecode[pc+1:]))
	if err != nil {
		return Type{}, err
	}
	return ReturnTypeOf(reference.Descriptor)
}

// bytecodeInstructionEnds validates instruction boundaries without executing
// them. Switch counts and wide operands are bounded before indexing the record.
func bytecodeInstructionEnds(code []byte) (map[int]int, error) {
	if len(code) == 0 || len(code) > 65535 {
		return nil, fmt.Errorf("JVM bytecode checkpoint method length is invalid")
	}
	ends := make(map[int]int)
	for pc := 0; pc < len(code); {
		op := code[pc]
		n := 1
		switch {
		case op == 0x10 || op == 0x12 || op >= 0x15 && op <= 0x19 || op >= 0x36 && op <= 0x3a || op == 0xa9 || op == 0xbc:
			n = 2
		case op == 0x11 || op == 0x13 || op == 0x14 || op == 0x84 || op >= 0x99 && op <= 0xa8 || op >= 0xb2 && op <= 0xb8 || op == 0xbb || op == 0xbd || op == 0xc0 || op == 0xc1 || op == 0xc6 || op == 0xc7:
			n = 3
		case op == 0xc5:
			n = 4
		case op == 0xb9 || op == 0xba || op == 0xc8 || op == 0xc9:
			n = 5
		case op == 0xc4:
			if pc+1 >= len(code) {
				return nil, fmt.Errorf("JVM bytecode checkpoint has truncated wide instruction")
			}
			wide := code[pc+1]
			if wide == 0x84 {
				n = 6
			} else if wide >= 0x15 && wide <= 0x19 || wide >= 0x36 && wide <= 0x3a || wide == 0xa9 {
				n = 4
			} else {
				return nil, fmt.Errorf("JVM bytecode checkpoint has invalid wide instruction")
			}
		case op == 0xaa || op == 0xab:
			base := (pc + 4) &^ 3
			header := 12
			if op == 0xab {
				header = 8
			}
			if base+header > len(code) {
				return nil, fmt.Errorf("JVM bytecode checkpoint has truncated switch")
			}
			count := int64(int32(binary.BigEndian.Uint32(code[base+4:])))
			width := int64(8)
			if op == 0xaa {
				count = int64(int32(binary.BigEndian.Uint32(code[base+8:]))) - count + 1
				width = 4
			}
			if count < 0 || count > int64(len(code)-base-header)/width {
				return nil, fmt.Errorf("JVM bytecode checkpoint has invalid switch bounds")
			}
			n = base + header + int(count*width) - pc
		case op > 0xc9:
			return nil, fmt.Errorf("JVM bytecode checkpoint contains unsupported opcode")
		}
		if n > len(code)-pc {
			return nil, fmt.Errorf("JVM bytecode checkpoint has truncated instruction")
		}
		ends[pc] = pc + n
		pc += n
	}
	return ends, nil
}
