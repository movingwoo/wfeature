package jvm

import (
	"encoding/binary"
	"fmt"
	"strings"
	"weak"
)

func heapText(text string) bool {
	return text != "" && len(text) <= 65535 && !strings.ContainsRune(text, 0)
}
func heapClass(name string) bool {
	return heapText(name) && len(name)-len(strings.TrimLeft(name, "[")) <= 255 && validateAOTClassName(name) == nil
}
func heapType(descriptor string) (Type, error) {
	if !heapText(descriptor) || len(descriptor)-len(strings.TrimLeft(descriptor, "[")) > 255 {
		return Type{}, fmt.Errorf("JVM heap has an invalid field descriptor")
	}
	return ParseFieldDescriptor(descriptor)
}

func (saved HeapState) validate(options Options) error {
	return saved.validateThreads(options, false)
}

func (saved HeapState) validateThreads(options Options, threads bool) error {
	if saved.Version != heapStateVersion || saved.MaxArrayLength != options.MaxArrayLength || saved.CooperativeThreads != (options.GuestThreadStarter != nil) || saved.NextObject > 1<<31-1 || saved.NextExecution > maxRestoredExecutionID {
		return fmt.Errorf("JVM heap version, identity counters, or execution policy is incompatible")
	}
	if len(saved.Objects) > heapStateObjects || len(saved.Payloads) > heapStateObjects || len(saved.Classes) > maxAOTMembers {
		return fmt.Errorf("JVM heap record count exceeds limit")
	}
	budget := heapBudget{}
	for _, count := range []int{len(saved.Objects), len(saved.Payloads), len(saved.Statics), len(saved.ClassMonitors), len(saved.Classes), len(saved.Aliases), len(saved.Bindings)} {
		if err := budget.add(count, 128); err != nil {
			return err
		}
	}
	refOK := func(id uint32) bool { return uint64(id) <= uint64(len(saved.Objects)) }
	checkRefs := func(refs []uint32) error {
		if err := budget.add(len(refs), 8); err != nil {
			return err
		}
		for _, ref := range refs {
			if !refOK(ref) {
				return fmt.Errorf("JVM heap reference %d is out of range", ref)
			}
		}
		return nil
	}
	textCost := func(text string) error { return budget.add(len(text), 1) }
	valueOK := func(value HeapValueState) bool {
		if value.Kind < ValueInt || value.Kind > ValueReference || !refOK(value.Reference) {
			return false
		}
		if value.Kind == ValueReference {
			return value.Bits == 0
		}
		return value.Reference == 0 && (value.Kind != ValueInt && value.Kind != ValueFloat || value.Bits <= 1<<32-1)
	}
	if err := checkRefs(saved.Roots); err != nil {
		return err
	}
	if !refOK(saved.MainThread) {
		return fmt.Errorf("JVM main thread reference is out of range")
	}
	identities := make(map[uint32]bool)
	for _, object := range saved.Objects {
		if !heapClass(object.Class) || object.Identity > saved.NextObject || object.Native > uint32(len(saved.Payloads)) {
			return fmt.Errorf("JVM heap object has invalid class, identity, or native reference")
		}
		if err := textCost(object.Class); err != nil {
			return err
		}
		if object.Native != 0 {
			payload := saved.Payloads[object.Native-1]
			if payload.Kind == "array" && (payload.Array == nil || object.Class != "["+payload.Array.Component) {
				return fmt.Errorf("JVM array class and payload component differ")
			}
		}
		if object.Identity != 0 {
			if identities[object.Identity] {
				return fmt.Errorf("JVM heap has duplicate object identity")
			}
			identities[object.Identity] = true
		}
		if err := checkRefs(object.Retained); err != nil {
			return err
		}
		if err := budget.add(len(object.Fields), 64); err != nil {
			return err
		}
		for i, field := range object.Fields {
			if !heapText(field.Name) || !valueOK(field.Value) || i > 0 && object.Fields[i-1].Name >= field.Name {
				return fmt.Errorf("JVM object fields have invalid names, values, or order")
			}
			if err := textCost(field.Name); err != nil {
				return err
			}
		}
		if state := object.Thread; state != nil {
			if state.Alive && !state.Started || state.Registered != state.Alive || state.Registered && !saved.CooperativeThreads && !threads {
				return fmt.Errorf("JVM heap has inconsistent thread state")
			}
		}
	}
	for _, payload := range saved.Payloads {
		if !heapText(payload.Kind) {
			return fmt.Errorf("JVM native payload kind is invalid")
		}
		if err := textCost(payload.Kind); err != nil {
			return err
		}
		if err := budget.add(len(payload.Data), 1); err != nil {
			return err
		}
		if err := checkRefs(payload.References); err != nil {
			return err
		}
		if err := textCost(payload.ExternalKind); err != nil {
			return err
		}
		if payload.Kind != "array" && payload.Array != nil || payload.Kind != "array" && payload.Kind != "external" && payload.ExternalKind != "" {
			return fmt.Errorf("JVM heap payload has extra array or external state")
		}
		if payload.Kind != "objects" && payload.Kind != "array" && payload.Kind != "external" && len(payload.References) != 0 {
			return fmt.Errorf("JVM primitive payload contains references")
		}
		valid := true
		switch payload.Kind {
		case "string":
		case "int32":
			valid = len(payload.Data) == 4
		case "int64":
			valid = len(payload.Data) == 8
		case "objects":
			valid = len(payload.Data) == 4 && uint64(binary.LittleEndian.Uint32(payload.Data)) <= uint64(len(payload.References))
		case "buffer":
			valid = len(payload.Data)%2 == 0 && len(payload.Data)/2 <= options.MaxArrayLength
		case "timezone":
			valid = len(payload.Data) >= 4 && len(payload.Data) <= 65539
			if valid {
				offset := int32(binary.LittleEndian.Uint32(payload.Data))
				valid = offset >= -86400000 && offset <= 86400000
			}
		case "calendar":
			valid = len(payload.Data) >= 16 && len(payload.Data) <= 65551
			if valid {
				nanos, offset := binary.LittleEndian.Uint32(payload.Data[8:]), int32(binary.LittleEndian.Uint32(payload.Data[12:]))
				valid = nanos < 1e9 && offset >= -86400 && offset <= 86400
			}
		case "calendar-rules-v1":
			valid = len(payload.Data) >= 26 && len(payload.Data) <= maxHeapCalendarBytes
		case "external":
			valid = heapText(payload.ExternalKind)
		case "array":
			array := payload.Array
			if array == nil || array.Length < 0 || array.Length > options.MaxArrayLength {
				return fmt.Errorf("JVM heap has an invalid array shape")
			}
			component, err := heapType(array.Component)
			if err != nil {
				return err
			}
			if err := textCost(array.Component); err != nil {
				return err
			}
			if payload.ExternalKind == "" {
				if len(array.Values) != array.Length || len(payload.Data) != 0 || len(payload.References) != 0 {
					return fmt.Errorf("JVM heap array values do not match its shape")
				}
				if err := budget.add(len(array.Values), 32); err != nil {
					return err
				}
				for _, value := range array.Values {
					if !valueOK(value) || validateValue(Value{kind: value.Kind}, component) != nil {
						return fmt.Errorf("JVM heap array element has an invalid type or reference")
					}
				}
			} else {
				valid = heapText(payload.ExternalKind) && len(array.Values) == 0
			}
		default:
			valid = false
		}
		if !valid {
			return fmt.Errorf("JVM heap has an invalid native payload %q", payload.Kind)
		}
	}
	for i, field := range saved.Statics {
		typeInfo, err := heapType(field.Descriptor)
		if err != nil || !heapClass(field.Class) || !heapText(field.Name) || !valueOK(field.Value) || validateValue(Value{kind: field.Value.Kind}, typeInfo) != nil || i > 0 && compareHeapField(saved.Statics[i-1], field) >= 0 {
			return fmt.Errorf("JVM statics have invalid fields, values, or order")
		}
		for _, text := range []string{field.Class, field.Name, field.Descriptor} {
			if err := textCost(text); err != nil {
				return err
			}
		}
	}
	for i, name := range saved.Initialized {
		if !heapClass(name) || i > 0 && saved.Initialized[i-1] >= name {
			return fmt.Errorf("JVM initialized classes have invalid names or order")
		}
		if err := budget.add(1, 64); err != nil {
			return err
		}
		if err := textCost(name); err != nil {
			return err
		}
	}
	for i, monitor := range saved.ClassMonitors {
		if !heapClass(monitor.Class) || i > 0 && saved.ClassMonitors[i-1].Class >= monitor.Class {
			return fmt.Errorf("JVM class monitors have invalid names or order")
		}
		if err := textCost(monitor.Class); err != nil {
			return err
		}
	}
	classes := make(map[string]uint32, len(saved.Classes))
	canonical := make(map[uint32]string, len(saved.Classes))
	for i, class := range saved.Classes {
		if !heapClass(class.Name) || !heapClass(class.SuperName) && class.SuperName != "" || i > 0 && saved.Classes[i-1].Name >= class.Name {
			return fmt.Errorf("JVM AOT classes have invalid names or order")
		}
		for _, n := range []int{len(class.Methods), len(class.Fields), len(class.VTable)} {
			if err := budget.add(n, 64); err != nil {
				return err
			}
		}
		for _, name := range []string{class.Name, class.SuperName} {
			if err := textCost(name); err != nil {
				return err
			}
		}
		for _, method := range class.Methods {
			// A descriptor is bounded before the shared recursive parser sees it.
			if len(method.Descriptor) > 65535 || strings.Contains(method.Descriptor, strings.Repeat("[", 256)) {
				return fmt.Errorf("JVM AOT method descriptor exceeds limits")
			}
			for _, text := range []string{method.Name, method.Descriptor} {
				if err := textCost(text); err != nil {
					return err
				}
			}
		}
		for _, field := range class.Fields {
			if _, err := heapType(field.Descriptor); err != nil {
				return err
			}
			for _, text := range []string{field.Name, field.Descriptor} {
				if err := textCost(text); err != nil {
					return err
				}
			}
		}
		if err := validateAOTClass(class); err != nil {
			return err
		}
		if canonical[class.Address] != "" {
			return fmt.Errorf("JVM AOT classes share a canonical address")
		}
		classes[class.Name], canonical[class.Address] = class.Address, class.Name
	}
	aliases := make(map[uint32]string, len(saved.Aliases))
	for i, alias := range saved.Aliases {
		if alias.Address == 0 || classes[alias.Class] == 0 || i > 0 && saved.Aliases[i-1].Address >= alias.Address {
			return fmt.Errorf("JVM AOT aliases have invalid addresses, classes, or order")
		}
		if err := textCost(alias.Class); err != nil {
			return err
		}
		aliases[alias.Address] = alias.Class
	}
	for address, name := range canonical {
		if aliases[address] != name {
			return fmt.Errorf("JVM AOT canonical address is missing from its aliases")
		}
	}
	bound := make(map[uint32]bool)
	for i, binding := range saved.Bindings {
		if binding.Address == 0 || binding.Object == 0 || !refOK(binding.Object) || bound[binding.Object] || i > 0 && saved.Bindings[i-1].Address >= binding.Address || saved.Objects[binding.Object-1].Identity == 0 {
			return fmt.Errorf("JVM AOT binding has an invalid address, object, identity, or order")
		}
		bound[binding.Object] = true
	}
	return nil
}

// RestoreHeapState validates and builds detached objects before changing the VM.
// Callers must exclude all execution and rebind every platform-owned root to the
// returned objects. A failed decode leaves the destination VM unchanged.
func (vm *VM) RestoreHeapState(saved HeapState, codec HeapCodec) ([]*Object, error) {
	return vm.restoreHeapState(saved, codec, false)
}

func (vm *VM) restoreHeapState(saved HeapState, codec HeapCodec, threadsAllowed bool) ([]*Object, error) {
	if vm == nil {
		return nil, fmt.Errorf("restore JVM heap without a VM")
	}
	if err := saved.validateThreads(vm.config, threadsAllowed); err != nil {
		return nil, err
	}
	select {
	case <-vm.closed:
		return nil, ErrClosed
	default:
	}
	vm.threadMu.Lock()
	defer vm.threadMu.Unlock()
	vm.initMu.Lock()
	defer vm.initMu.Unlock()
	if len(vm.threads) != 0 || len(vm.initializing) != 0 || vm.toStringDepth.Load() != 0 {
		return nil, fmt.Errorf("restore JVM heap requires a detached destination")
	}
	objects := make([]*Object, len(saved.Objects)+1)
	for i, record := range saved.Objects {
		objects[i+1] = &Object{ClassName: record.Class, Fields: make(map[string]Value, len(record.Fields))}
		objects[i+1].identity.Store(record.Identity)
	}
	payloads := make([]any, len(saved.Payloads)+1)
	for i, record := range saved.Payloads {
		payload, err := restoreHeapPayload(record, objects, codec)
		if err != nil {
			return nil, fmt.Errorf("restore JVM native payload %d: %w", i+1, err)
		}
		payloads[i+1] = payload
	}
	threads := make(map[*Object]*guestThread)
	for i, record := range saved.Objects {
		object := objects[i+1]
		object.Native = payloads[record.Native]
		for _, field := range record.Fields {
			object.Fields[field.Name] = restoreHeapValue(field.Value, objects)
		}
		for _, ref := range record.Retained {
			object.aotRetain = append(object.aotRetain, objects[ref])
		}
		object.monitor.signal = record.MonitorSignal
		if thread := record.Thread; thread != nil {
			state := &guestThread{started: thread.Started, alive: thread.Alive, interrupted: thread.Interrupted, wake: make(chan struct{}, 1), done: make(chan struct{})}
			if thread.Wake {
				state.wake <- struct{}{}
			}
			if thread.Started && !thread.Alive {
				close(state.done)
			}
			object.thread.Store(state)
			if thread.Registered {
				threads[object] = state
			}
		}
	}
	statics := make(map[fieldKey]Value, len(saved.Statics))
	for _, field := range saved.Statics {
		statics[fieldKey{class: field.Class, name: field.Name, descriptor: field.Descriptor}] = restoreHeapValue(field.Value, objects)
	}
	monitors := make(map[string]*monitor, len(saved.ClassMonitors))
	for _, record := range saved.ClassMonitors {
		monitors[record.Class] = &monitor{signal: record.Signal}
	}
	classes := make(map[string]AOTClassMetadata, len(saved.Classes))
	for _, class := range saved.Classes {
		classes[class.Name] = cloneAOTClass(class)
	}
	aliases := make(map[uint32]string, len(saved.Aliases))
	for _, alias := range saved.Aliases {
		aliases[alias.Address] = alias.Class
	}
	bindings := make(map[uint32]aotBinding, len(saved.Bindings))
	for _, record := range saved.Bindings {
		object := objects[record.Object]
		object.aotAddress.Store(record.Address)
		binding := aotBinding{tracked: weak.Make(object)}
		if record.Pinned {
			binding.pinned = object
		}
		bindings[record.Address] = binding
	}
	initialized := make(map[string]bool, len(saved.Initialized))
	for _, name := range saved.Initialized {
		initialized[name] = true
	}
	roots := make([]*Object, len(saved.Roots))
	for i, id := range saved.Roots {
		roots[i] = objects[id]
	}
	vm.mu.Lock()
	vm.aotMu.Lock()
	vm.statics, vm.classMonitors, vm.declaringFields = statics, monitors, make(map[fieldKey]fieldResolution)
	vm.aotClasses, vm.aotAddresses, vm.aotObjects = classes, aliases, bindings
	vm.threads, vm.mainThread = threads, objects[saved.MainThread]
	vm.initialized, vm.initErrors = initialized, make(map[string]error)
	vm.nextObject.Store(saved.NextObject)
	vm.nextExecution.Store(saved.NextExecution)
	vm.aotMu.Unlock()
	vm.mu.Unlock()
	return roots, nil
}
