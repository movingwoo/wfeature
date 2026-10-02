package armcore

import (
	"fmt"
	"slices"
)

const (
	coreStateVersion   = 1
	maxStatePages      = (512 << 20) / memoryPageSize
	maxStateLocalWords = 4096
)

// CoreState contains architecture state, with caches and Host callbacks omitted.
// A platform must stop all execution and memory producers before capturing it.
// Call remainders and logical threads are separate records.
type CoreState struct {
	Version  uint32
	Quantum  uint32
	MaxSteps uint64
	Steps    uint64
	Memory   MemoryState
}

type MemoryState struct {
	Mappings    []MappingState
	Pages       []PageState
	ThreadLocal []LocalWordState
	ARMSteps    uint64
}

type MappingState struct {
	Address    uint32
	Size       uint64
	Permission Permission
}

type PageState struct {
	Address uint32
	Data    []byte
}

type LocalWordState struct {
	Address uint32
	Value   uint32
}

// RootThreadState is a logical parent whose execution lives in derived calls.
// Its instruction budget is checked against Host policy when it is restored.
type RootThreadState struct {
	Context     Context
	StepBudget  uint64
	ThreadLocal []LocalWordState
}

// ValidateRange checks a restored platform view without allocating a copy or
// touching bytes. A zero-length view still needs its separate object header.
func (memory *Memory) ValidateRange(address uint32, size uint64, permission Permission) error {
	if memory == nil || permission == 0 || permission & ^(PermissionRead|PermissionWrite|PermissionExecute) != 0 {
		return fmt.Errorf("invalid ARM memory validation request")
	}
	if size > guestAddressEnd-uint64(address) {
		return &AccessError{Operation: "validate restored view", Address: address, Size: size, Cause: ErrAddressOverflow}
	}
	// validateLocked may refresh the last-mapping cache.
	memory.mu.Lock()
	defer memory.mu.Unlock()
	return memory.validateLocked(address, size, permission, "validate restored view")
}

func (core *Core) CaptureState() (CoreState, error) {
	if core == nil || core.memory == nil {
		return CoreState{}, fmt.Errorf("capture state without an ARM core")
	}
	core.execute.Lock()
	defer core.execute.Unlock()
	memory := core.memory
	memory.mu.RLock()
	defer memory.mu.RUnlock()
	count := uint64(0)
	memory.eachPage(func(_ uint32, page *memoryPage) {
		if page.data != nil {
			count++
		}
	})
	if count > maxStatePages || len(memory.threadLocalDefaults) > maxStateLocalWords {
		return CoreState{}, fmt.Errorf("ARM state exceeds committed-page or private-word limits")
	}
	saved := CoreState{Version: coreStateVersion, Quantum: core.quantum, MaxSteps: core.maxSteps, Steps: core.steps.Load()}
	saved.Memory.ARMSteps = memory.armSteps
	for _, mapping := range memory.maps {
		saved.Memory.Mappings = append(saved.Memory.Mappings, MappingState{Address: uint32(mapping.start), Size: mapping.end - mapping.start, Permission: mapping.permission})
	}
	slices.SortFunc(saved.Memory.Mappings, func(a, b MappingState) int {
		if a.Address < b.Address {
			return -1
		}
		if a.Address > b.Address {
			return 1
		}
		if a.Size < b.Size {
			return -1
		}
		if a.Size > b.Size {
			return 1
		}
		return int(a.Permission) - int(b.Permission)
	})
	saved.Memory.Pages = make([]PageState, 0, int(count))
	memory.eachPage(func(index uint32, page *memoryPage) {
		if page.data != nil {
			saved.Memory.Pages = append(saved.Memory.Pages, PageState{Address: index << memoryPageShift, Data: append([]byte(nil), page.data...)})
		}
	})
	for address, value := range memory.threadLocalDefaults {
		saved.Memory.ThreadLocal = append(saved.Memory.ThreadLocal, LocalWordState{Address: address, Value: value})
	}
	sortLocalWords(saved.Memory.ThreadLocal)
	return saved, nil
}

// NewCoreFromState builds a detached core. Supplied execution limits must match
// the record: an untrusted checkpoint cannot grant itself a larger step budget.
// The caller reattaches platform callbacks before running any restored thread.
func NewCoreFromState(saved CoreState, options CoreOptions) (*Core, error) {
	core := NewCore(options)
	if saved.Version != coreStateVersion || saved.Quantum != core.quantum || saved.MaxSteps != core.maxSteps {
		return nil, fmt.Errorf("ARM state version or execution policy is incompatible")
	}
	memory, err := memoryFromState(saved.Memory)
	if err != nil {
		return nil, err
	}
	core.memory = memory
	core.steps.Store(saved.Steps)
	return core, nil
}

func memoryFromState(saved MemoryState) (*Memory, error) {
	if len(saved.Mappings) > maxMappings || uint64(len(saved.Pages)) > maxStatePages || len(saved.ThreadLocal) > maxStateLocalWords {
		return nil, fmt.Errorf("ARM state exceeds mapping, committed-page, or private-word limits")
	}
	memory := NewMemory()
	for _, mapping := range saved.Mappings {
		if mapping.Size == 0 || mapping.Size > guestAddressEnd || uint64(mapping.Address) > guestAddressEnd-mapping.Size {
			return nil, fmt.Errorf("ARM state mapping exceeds the address space")
		}
		if err := memory.Map(mapping.Address, mapping.Size, mapping.Permission); err != nil {
			return nil, err
		}
	}
	// Merge coverage separately from permissions. A committed page may contain
	// several partial mappings and unmapped zero bytes between them.
	mappings := append([]memoryMapping(nil), memory.maps...)
	slices.SortFunc(mappings, func(a, b memoryMapping) int {
		if a.start < b.start {
			return -1
		}
		if a.start > b.start {
			return 1
		}
		return 0
	})
	coverage := make([]memoryMapping, 0, len(mappings))
	for _, mapping := range mappings {
		if len(coverage) == 0 || mapping.start > coverage[len(coverage)-1].end {
			coverage = append(coverage, mapping)
		} else {
			coverage[len(coverage)-1].end = max(coverage[len(coverage)-1].end, mapping.end)
		}
	}
	first := 0
	for index, page := range saved.Pages {
		if page.Address&memoryPageMask != 0 || len(page.Data) != int(memoryPageSize) || index > 0 && saved.Pages[index-1].Address >= page.Address {
			return nil, fmt.Errorf("ARM state page has invalid alignment, size, or ordering")
		}
		start, end := uint64(page.Address), uint64(page.Address)+memoryPageSize
		for first < len(coverage) && coverage[first].end <= start {
			first++
		}
		if first == len(coverage) || coverage[first].start >= end {
			return nil, fmt.Errorf("ARM state page has no mapped bytes")
		}
		cursor := start
		for i := first; i < len(coverage) && coverage[i].start < end; i++ {
			begin := max(cursor, coverage[i].start)
			if !zeroBytes(page.Data[cursor-start : begin-start]) {
				return nil, fmt.Errorf("ARM state contains bytes outside a mapping")
			}
			cursor = min(end, max(cursor, coverage[i].end))
		}
		if !zeroBytes(page.Data[cursor-start:]) {
			return nil, fmt.Errorf("ARM state contains bytes outside a mapping")
		}
	}
	// Validate all records before committing any page storage.
	for index, word := range saved.ThreadLocal {
		if word.Address&3 != 0 || index > 0 && saved.ThreadLocal[index-1].Address >= word.Address {
			return nil, fmt.Errorf("ARM private defaults have invalid alignment or ordering")
		}
		if err := memory.validateLocked(word.Address, 4, PermissionReadWrite, "restore private default"); err != nil {
			return nil, err
		}
	}
	for _, page := range saved.Pages {
		copy(memory.commitPage(page.Address).data, page.Data)
	}
	for _, word := range saved.ThreadLocal {
		if err := memory.registerThreadLocalWord(word.Address); err != nil {
			return nil, err
		}
		memory.threadLocalDefaults[word.Address] = word.Value
	}
	memory.armSteps = saved.ARMSteps
	return memory, nil
}

func zeroBytes(data []byte) bool {
	for _, value := range data {
		if value != 0 {
			return false
		}
	}
	return true
}

func sortLocalWords(words []LocalWordState) {
	slices.SortFunc(words, func(a, b LocalWordState) int {
		if a.Address < b.Address {
			return -1
		}
		if a.Address > b.Address {
			return 1
		}
		return 0
	})
}

func (core *Core) CaptureRootThread(thread *Thread) (RootThreadState, error) {
	if core == nil || core.memory == nil || thread == nil {
		return RootThreadState{}, fmt.Errorf("capture ARM root without a core or thread")
	}
	thread.mu.Lock()
	if thread.state != ThreadReady || thread.entryKnown || thread.threadLocal == nil {
		thread.mu.Unlock()
		return RootThreadState{}, fmt.Errorf("ARM logical parent is not ready")
	}
	saved := RootThreadState{Context: thread.context, StepBudget: thread.stepBudget}
	local := thread.threadLocal
	thread.mu.Unlock()
	core.memory.mu.RLock()
	defer core.memory.mu.RUnlock()
	if len(core.memory.threadLocalDefaults) > maxStateLocalWords {
		return RootThreadState{}, fmt.Errorf("ARM root exceeds private-word limit")
	}
	local.mu.RLock()
	defer local.mu.RUnlock()
	for address, value := range core.memory.threadLocalDefaults {
		if current, ok := local.words[address]; ok {
			value = current
		}
		saved.ThreadLocal = append(saved.ThreadLocal, LocalWordState{Address: address, Value: value})
	}
	sortLocalWords(saved.ThreadLocal)
	return saved, nil
}

func (core *Core) RestoreRootThread(saved RootThreadState, expectedBudget uint64) (*Thread, error) {
	if core == nil || core.memory == nil || saved.StepBudget != expectedBudget || saved.Context.CPSR&modeMask != modeUser || saved.Context.PC()&1 != 0 || !saved.Context.Thumb() && saved.Context.PC()&3 != 0 {
		return nil, fmt.Errorf("ARM logical parent context or budget is incompatible")
	}
	core.memory.mu.RLock()
	defer core.memory.mu.RUnlock()
	if len(saved.ThreadLocal) != len(core.memory.threadLocalDefaults) || len(saved.ThreadLocal) > maxStateLocalWords {
		return nil, fmt.Errorf("ARM logical parent private words are incomplete")
	}
	for index, word := range saved.ThreadLocal {
		_, ok := core.memory.threadLocalDefaults[word.Address]
		if !ok || index > 0 && saved.ThreadLocal[index-1].Address >= word.Address {
			return nil, fmt.Errorf("ARM logical parent has unknown or duplicate private words")
		}
	}
	thread := NewThread(saved.Context)
	thread.stepBudget = expectedBudget
	for _, word := range saved.ThreadLocal {
		thread.threadLocal.words[word.Address] = word.Value
	}
	return thread, nil
}
