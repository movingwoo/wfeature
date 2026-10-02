package jvm

import (
	"fmt"
	"slices"
)

const (
	heapStateVersion = 1
	heapStateBudget  = 128 << 20
	heapStateObjects = 1 << 18
)

// HeapState is a detached object graph. References are one-based indexes, with
// zero denoting null; guest identity hashes and AOT addresses are separate.
// The owner must park all execution, monitor waiters, and platform producers.
// Loader sources, native registrations, and Host callbacks are rebuilt by the
// destination before this component is adopted.
type HeapState struct {
	Version            uint32
	MaxArrayLength     int
	CooperativeThreads bool
	NextObject         uint32
	NextExecution      uint64
	Roots              []uint32
	Objects            []HeapObjectState
	Payloads           []HeapPayloadState
	Statics            []HeapStaticState
	Initialized        []string
	ClassMonitors      []HeapClassMonitorState
	MainThread         uint32
	Classes            []AOTClassMetadata
	Aliases            []HeapClassAliasState
	Bindings           []HeapBindingState
}

type HeapValueState struct {
	Kind      ValueKind
	Bits      uint64
	Reference uint32
}
type HeapFieldState struct {
	Name  string
	Value HeapValueState
}
type HeapStaticState struct {
	Class, Name, Descriptor string
	Value                   HeapValueState
}
type HeapClassMonitorState struct {
	Class  string
	Signal uint64
}
type HeapClassAliasState struct {
	Address uint32
	Class   string
}
type HeapBindingState struct {
	Address, Object uint32
	Pinned          bool
}
type HeapObjectState struct {
	Class         string
	Identity      uint32
	Fields        []HeapFieldState
	Native        uint32
	Retained      []uint32
	MonitorSignal uint64
	Thread        *HeapThreadState
}
type HeapThreadState struct{ Started, Alive, Interrupted, Wake, Registered bool }

// HeapExternalPayload is the platform boundary for native payloads and arrays.
// Kind is a stable platform-defined identifier. Capture must not mutate its
// input; restoration may only construct detached state and must not run guest
// code. References may point at objects whose payloads are not filled yet.
type HeapExternalPayload struct {
	Kind       string
	Data       []byte
	References []*Object
}
type HeapCodec struct {
	CaptureNative func(any) (HeapExternalPayload, error)
	RestoreNative func(HeapExternalPayload) (any, error)
	CaptureArray  func(ArrayStorage) (HeapExternalPayload, error)
	RestoreArray  func(Type, int, HeapExternalPayload) (ArrayStorage, error)
}
type HeapPayloadState struct {
	Kind         string
	Data         []byte
	References   []uint32
	Array        *HeapArrayState
	ExternalKind string
}
type HeapArrayState struct {
	Component string
	Length    int
	Values    []HeapValueState
}

type heapBudget struct{ used uint64 }

func (budget *heapBudget) add(count int, width uint64) error {
	if count < 0 || uint64(count) > (heapStateBudget-budget.used)/width {
		return fmt.Errorf("JVM heap state exceeds %d bytes", heapStateBudget)
	}
	budget.used += uint64(count) * width
	return nil
}

type heapCapture struct {
	vm         *VM
	codec      HeapCodec
	saved      HeapState
	objects    []*Object
	ids        map[*Object]uint32
	payloadIDs map[any]uint32
	lists      []heapListRange
	budget     heapBudget
	err        error
}

func (capture *heapCapture) charge(count int, width uint64) {
	if capture.err == nil {
		capture.err = capture.budget.add(count, width)
	}
}

func (capture *heapCapture) ref(object *Object) uint32 {
	if object == nil || capture.err != nil {
		return 0
	}
	if id := capture.ids[object]; id != 0 {
		return id
	}
	if len(capture.objects) >= heapStateObjects {
		capture.err = fmt.Errorf("JVM heap state exceeds object limit")
		return 0
	}
	if capture.err = capture.budget.add(1, 128); capture.err != nil {
		return 0
	}
	id := uint32(len(capture.objects) + 1)
	capture.ids[object] = id
	capture.objects = append(capture.objects, object)
	return id
}
func (capture *heapCapture) value(value Value) HeapValueState {
	if value.kind < ValueInt || value.kind > ValueReference {
		capture.err = fmt.Errorf("JVM heap contains non-field value %s", value.kind)
	}
	return HeapValueState{Kind: value.kind, Bits: value.bits, Reference: capture.ref(value.ref)}
}
func (capture *heapCapture) references(objects []*Object) []uint32 {
	if capture.err != nil {
		return nil
	}
	if capture.err = capture.budget.add(len(objects), 4); capture.err != nil {
		return nil
	}
	refs := make([]uint32, len(objects))
	for i, object := range objects {
		refs[i] = capture.ref(object)
	}
	return refs
}

// CaptureHeapState includes explicit platform roots, statics, cooperative thread
// records, and live AOT bindings. It neither assigns identities nor re-pins weak
// bindings. In-progress/failed initialization and held monitors are refused.
func (vm *VM) CaptureHeapState(roots []*Object, codec HeapCodec) (HeapState, error) {
	if vm == nil {
		return HeapState{}, fmt.Errorf("capture JVM heap without a VM")
	}
	select {
	case <-vm.closed:
		return HeapState{}, ErrClosed
	default:
	}
	vm.initMu.Lock()
	defer vm.initMu.Unlock()
	if len(vm.initializing) != 0 || len(vm.initErrors) != 0 || vm.toStringDepth.Load() != 0 {
		return HeapState{}, fmt.Errorf("JVM heap has an active or failed initializer or native conversion")
	}
	capture := &heapCapture{vm: vm, codec: codec, ids: make(map[*Object]uint32), payloadIDs: make(map[any]uint32)}
	capture.saved = HeapState{Version: heapStateVersion, MaxArrayLength: vm.config.MaxArrayLength, CooperativeThreads: vm.config.GuestThreadStarter != nil, NextObject: vm.nextObject.Load(), NextExecution: vm.nextExecution.Load()}
	capture.saved.Roots = capture.references(roots)
	capture.charge(len(vm.initialized), 64)
	if capture.err != nil {
		return HeapState{}, capture.err
	}
	for name, initialized := range vm.initialized {
		if initialized {
			capture.charge(len(name), 1)
			capture.saved.Initialized = append(capture.saved.Initialized, name)
		}
	}
	slices.Sort(capture.saved.Initialized)
	vm.mu.RLock()
	capture.charge(len(vm.statics)+len(vm.classMonitors), 128)
	if capture.err == nil {
		for key := range vm.statics {
			capture.charge(len(key.class)+len(key.name)+len(key.descriptor), 1)
			capture.saved.Statics = append(capture.saved.Statics, HeapStaticState{Class: key.class, Name: key.name, Descriptor: key.descriptor})
		}
	}
	slices.SortFunc(capture.saved.Statics, compareHeapField)
	for index := range capture.saved.Statics {
		field := &capture.saved.Statics[index]
		field.Value = capture.value(vm.statics[fieldKey{class: field.Class, name: field.Name, descriptor: field.Descriptor}])
	}
	for name, monitor := range vm.classMonitors {
		if capture.err != nil {
			break
		}
		capture.charge(len(name), 1)
		monitor.mu.Lock()
		if monitor.owner != 0 || monitor.depth != 0 {
			capture.err = fmt.Errorf("JVM class monitor is held")
		}
		capture.saved.ClassMonitors = append(capture.saved.ClassMonitors, HeapClassMonitorState{Class: name, Signal: monitor.signal})
		monitor.mu.Unlock()
	}
	vm.mu.RUnlock()
	slices.SortFunc(capture.saved.Statics, func(a, b HeapStaticState) int { return compareHeapField(a, b) })
	slices.SortFunc(capture.saved.ClassMonitors, func(a, b HeapClassMonitorState) int { return compareHeapText(a.Class, b.Class) })
	vm.threadMu.Lock()
	capture.charge(len(vm.threads), 64)
	if capture.err != nil {
		vm.threadMu.Unlock()
		return HeapState{}, capture.err
	}
	if len(vm.threads) != 0 && vm.config.GuestThreadStarter == nil {
		capture.err = fmt.Errorf("JVM heap has independently scheduled threads")
	}
	registered := make(map[*Object]bool, len(vm.threads))
	for object := range vm.threads {
		registered[object] = true
		capture.ref(object)
	}
	capture.saved.MainThread = capture.ref(vm.mainThread)
	vm.threadMu.Unlock()
	vm.aotMu.RLock()
	capture.charge(len(vm.aotClasses)+len(vm.aotAddresses)+len(vm.aotObjects), 128)
	if capture.err != nil {
		vm.aotMu.RUnlock()
		return HeapState{}, capture.err
	}
	for _, class := range vm.aotClasses {
		capture.charge(len(class.Name)+len(class.SuperName), 1)
		capture.charge(len(class.Methods)+len(class.Fields)+len(class.VTable), 64)
		for _, method := range class.Methods {
			capture.charge(len(method.Name)+len(method.Descriptor), 1)
		}
		for _, field := range class.Fields {
			capture.charge(len(field.Name)+len(field.Descriptor), 1)
		}
		if capture.err != nil {
			break
		}
		capture.saved.Classes = append(capture.saved.Classes, cloneAOTClass(class))
	}
	slices.SortFunc(capture.saved.Classes, func(a, b AOTClassMetadata) int { return compareHeapText(a.Name, b.Name) })
	for address, name := range vm.aotAddresses {
		if capture.err != nil {
			break
		}
		capture.charge(len(name), 1)
		capture.saved.Aliases = append(capture.saved.Aliases, HeapClassAliasState{Address: address, Class: name})
	}
	slices.SortFunc(capture.saved.Aliases, func(a, b HeapClassAliasState) int { return compareHeapNumber(a.Address, b.Address) })
	addresses := make([]uint32, 0, len(vm.aotObjects))
	for address := range vm.aotObjects {
		addresses = append(addresses, address)
	}
	slices.Sort(addresses)
	for _, address := range addresses {
		binding := vm.aotObjects[address]
		if object := binding.object(); object != nil {
			capture.saved.Bindings = append(capture.saved.Bindings, HeapBindingState{Address: address, Object: capture.ref(object), Pinned: binding.pinned != nil})
		}
	}
	vm.aotMu.RUnlock()
	for index := 0; index < len(capture.objects) && capture.err == nil; index++ {
		object := capture.objects[index]
		capture.charge(len(object.ClassName), 1)
		vm.aotMu.RLock()
		retained := capture.references(object.aotRetain)
		vm.aotMu.RUnlock()
		record := HeapObjectState{Class: object.ClassName, Identity: object.identity.Load(), Retained: retained}
		object.monitor.mu.Lock()
		if object.monitor.owner != 0 || object.monitor.depth != 0 {
			capture.err = fmt.Errorf("JVM object monitor is held")
		}
		record.MonitorSignal = object.monitor.signal
		object.monitor.mu.Unlock()
		object.fieldMu.RLock()
		if capture.err == nil {
			capture.err = capture.budget.add(len(object.Fields), 64)
		}
		if capture.err == nil {
			for name := range object.Fields {
				capture.charge(len(name), 1)
				record.Fields = append(record.Fields, HeapFieldState{Name: name})
			}
			slices.SortFunc(record.Fields, func(a, b HeapFieldState) int { return compareHeapText(a.Name, b.Name) })
			for i := range record.Fields {
				record.Fields[i].Value = capture.value(object.Fields[record.Fields[i].Name])
			}
		}
		object.fieldMu.RUnlock()
		slices.SortFunc(record.Fields, func(a, b HeapFieldState) int { return compareHeapText(a.Name, b.Name) })
		if state := object.thread.Load(); state != nil {
			state.mu.Lock()
			record.Thread = &HeapThreadState{Started: state.started, Alive: state.alive, Interrupted: state.interrupted, Wake: len(state.wake) != 0, Registered: registered[object]}
			state.mu.Unlock()
		}
		if capture.err == nil {
			record.Native, capture.err = capture.native(object.Native)
		}
		capture.saved.Objects = append(capture.saved.Objects, record)
	}
	if capture.err != nil {
		return HeapState{}, capture.err
	}
	if err := capture.validateListRanges(); err != nil {
		return HeapState{}, err
	}
	if err := capture.saved.validate(vm.config); err != nil {
		return HeapState{}, err
	}
	return capture.saved, nil
}

func compareHeapText(a, b string) int {
	if a < b {
		return -1
	}
	if a > b {
		return 1
	}
	return 0
}
func compareHeapNumber(a, b uint32) int {
	if a < b {
		return -1
	}
	if a > b {
		return 1
	}
	return 0
}
func compareHeapField(a, b HeapStaticState) int {
	if c := compareHeapText(a.Class, b.Class); c != 0 {
		return c
	}
	if c := compareHeapText(a.Name, b.Name); c != 0 {
		return c
	}
	return compareHeapText(a.Descriptor, b.Descriptor)
}
