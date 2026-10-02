package ktf

import (
	"bytes"
	"cmp"
	"fmt"
	"maps"
	"slices"

	"github.com/movingwoo/wfeature/internal/armcore"
	"github.com/movingwoo/wfeature/internal/jvm"
)

// Runtime metadata accompanies saved ARM bytes. Native implementations are
// registered on the fresh VM; these records retain their guest dispatch IDs.
// The caller must hold the same barrier as memory, heap and worker capture.
type runtimeMetadataState struct {
	Arena                 arenaState
	ClassArena            *arenaState
	CodeCursor            uint64
	Stubs                 []runtimeStubState
	Classes               []runtimeClassAddressState
	LoadedClasses         []runtimeClassAddressState
	ModuleClasses         []runtimeClassAddressState
	NativeMethods         []runtimeNativeMethodState
	NextNativeMethod      uint32
	InitializedClasses    []runtimeClassFlagState
	LinkedModuleClasses   []runtimeClassFlagState
	ClassAliases          []runtimeAddressPairState
	ClassSummaries        []runtimeClassSummaryState
	Objects               []runtimeObjectState
	CollectAt             uint64
	WIPICAllocations      []runtimeAllocationState
	ImageSourceBuffers    []runtimeAddressPairState
	ReleasedWIPIC         []uint32
	ReleasedWIPICOrder    []uint32
	UserMemoryPools       []runtimeUserPoolState
	PixelOp               *runtimePixelOpState
	Bindings              runtimeBindingState
	LEDs                  int32
	MenuTextCompatibility bool
}

type runtimeStubState struct {
	Key     uint64
	Address uint32
}
type runtimeClassAddressState struct {
	Name    []byte
	Address uint32
}
type runtimeClassFlagState struct {
	Address uint32
	Value   bool
}
type runtimeAddressPairState struct{ Address, Target uint32 }
type runtimeNativeMethodState struct {
	ID                      uint32
	Class, Name, Descriptor []byte
	AccessFlags             uint16
	Container               bool
}
type runtimeClassSummaryState struct {
	Address, Parent uint32
	Name            []byte
	Interfaces      []uint32
}
type runtimeObjectState struct {
	Address, Size uint32
	Released      bool
}
type runtimeAllocationState struct {
	Address uint32
	Size    uint64
}
type runtimeUserPoolState struct{ Base, Size, Cursor, FreeCalls uint32 }
type runtimePixelResultState struct {
	Pair   uint32
	Result uint16
}
type runtimePixelOpState struct {
	Function, Param uint32
	Results         []runtimePixelResultState
}
type runtimeBindingState struct {
	Kernel, Java, WIPIC, Module, ModuleContext, ModuleJumpTable uint32
	JVMContext, ExceptionContext, ScreenFramebuffer             uint32
	UserMemory, InputModeTable, CameraBoundsStore               uint32
}

const (
	maxMetadataEntries = 1 << 20
	maxMetadataBytes   = 64 << 20
	maxMetadataName    = 65535
)

func metadataKeys[K cmp.Ordered, V any](values map[K]V) []K {
	return slices.Sorted(maps.Keys(values))
}

func (runtime *initializationRuntime) captureMetadataState() (runtimeMetadataState, error) {
	if runtime.collecting || runtime.saveReadError != nil {
		return runtimeMetadataState{}, fmt.Errorf("KTF metadata capture requires an idle collector and successful storage reads")
	}
	count := uint64(len(runtime.stubs)) + uint64(len(runtime.classes)) + uint64(len(runtime.loadedClasses)) + uint64(len(runtime.moduleClassByName)) + uint64(len(runtime.nativeMethods)) + uint64(len(runtime.initializedClasses)) + uint64(len(runtime.linkedModuleClasses)) + uint64(len(runtime.classAliases)) + uint64(len(runtime.classSummaries)) + uint64(len(runtime.objects)) + uint64(len(runtime.wipicAllocations)) + uint64(len(runtime.imageSourceBuffers)) + uint64(len(runtime.releasedWIPIC)) + uint64(len(runtime.releasedWIPICOrder)) + uint64(len(runtime.userMemoryPools))
	if runtime.pixelOps != nil {
		count += uint64(len(runtime.pixelOps.results))
	}
	if count > maxMetadataEntries {
		return runtimeMetadataState{}, fmt.Errorf("KTF metadata records exceed limit")
	}
	saved := runtimeMetadataState{
		CodeCursor: runtime.codeCursor, NextNativeMethod: runtime.nextNativeMethod, CollectAt: runtime.collectAt,
		LEDs: runtime.leds, MenuTextCompatibility: runtime.menuTextCompatibility,
		Bindings: runtimeBindingState{Kernel: runtime.kernelInterface, Java: runtime.javaInterface, WIPIC: runtime.wipicInterface,
			Module: runtime.moduleInterface, ModuleContext: runtime.moduleContext, ModuleJumpTable: runtime.moduleJumpTable,
			JVMContext: runtime.jvmContext, ExceptionContext: runtime.exceptionContext, ScreenFramebuffer: runtime.screenFramebuffer,
			UserMemory: runtime.userMemoryInterface, InputModeTable: runtime.inputModeTableAddress, CameraBoundsStore: runtime.cameraBoundsStore},
	}
	var err error
	saved.Arena, err = runtime.arena.captureState()
	if err != nil {
		return runtimeMetadataState{}, err
	}
	if runtime.classArena != nil {
		arena, err := runtime.classArena.captureState()
		if err != nil {
			return runtimeMetadataState{}, err
		}
		saved.ClassArena = &arena
	}
	for _, key := range metadataKeys(runtime.stubs) {
		saved.Stubs = append(saved.Stubs, runtimeStubState{Key: key, Address: runtime.stubs[key]})
	}
	var nameBytes uint64
	name := func(value string) []byte {
		nameBytes += uint64(len(value))
		if len(value) > maxMetadataName || nameBytes > maxMetadataBytes {
			err = fmt.Errorf("KTF metadata names exceed limit")
			return nil
		}
		return []byte(value)
	}
	classes := func(values map[string]uint32) (result []runtimeClassAddressState) {
		for _, key := range metadataKeys(values) {
			result = append(result, runtimeClassAddressState{Name: name(key), Address: values[key]})
		}
		return result
	}
	saved.Classes, saved.LoadedClasses, saved.ModuleClasses = classes(runtime.classes), classes(runtime.loadedClasses), classes(runtime.moduleClassByName)
	for _, id := range metadataKeys(runtime.nativeMethods) {
		invocation := runtime.nativeMethods[id]
		method := invocation.method
		saved.NativeMethods = append(saved.NativeMethods, runtimeNativeMethodState{ID: id, Class: name(method.class), Name: name(method.name), Descriptor: name(method.descriptor), AccessFlags: method.accessFlags, Container: invocation.container})
	}
	flags := func(values map[uint32]bool) (result []runtimeClassFlagState) {
		for _, address := range metadataKeys(values) {
			result = append(result, runtimeClassFlagState{Address: address, Value: values[address]})
		}
		return result
	}
	saved.InitializedClasses, saved.LinkedModuleClasses = flags(runtime.initializedClasses), flags(runtime.linkedModuleClasses)
	pairs := func(values map[uint32]uint32) (result []runtimeAddressPairState) {
		for _, address := range metadataKeys(values) {
			result = append(result, runtimeAddressPairState{Address: address, Target: values[address]})
		}
		return result
	}
	saved.ClassAliases, saved.ImageSourceBuffers = pairs(runtime.classAliases), pairs(runtime.imageSourceBuffers)
	for _, address := range metadataKeys(runtime.classSummaries) {
		summary := runtime.classSummaries[address]
		nameBytes += uint64(len(summary.interfaces)) * 4
		if len(summary.interfaces) > maxAOTInterfaces || nameBytes > maxMetadataBytes {
			return runtimeMetadataState{}, fmt.Errorf("KTF cached class interfaces exceed limit")
		}
		className := name(summary.name)
		if err != nil {
			return runtimeMetadataState{}, err
		}
		saved.ClassSummaries = append(saved.ClassSummaries, runtimeClassSummaryState{Address: address, Parent: summary.parent, Name: className, Interfaces: slices.Clone(summary.interfaces)})
	}
	if err != nil {
		return runtimeMetadataState{}, err
	}
	for _, address := range metadataKeys(runtime.objects) {
		record := runtime.objects[address]
		saved.Objects = append(saved.Objects, runtimeObjectState{Address: address, Size: record.size, Released: record.released})
	}
	for _, address := range metadataKeys(runtime.wipicAllocations) {
		saved.WIPICAllocations = append(saved.WIPICAllocations, runtimeAllocationState{Address: address, Size: runtime.wipicAllocations[address]})
	}
	saved.ReleasedWIPIC, saved.ReleasedWIPICOrder = metadataKeys(runtime.releasedWIPIC), slices.Clone(runtime.releasedWIPICOrder)
	for _, address := range metadataKeys(runtime.userMemoryPools) {
		pool := runtime.userMemoryPools[address]
		if pool == nil || pool.base != address {
			return runtimeMetadataState{}, fmt.Errorf("KTF user memory pool has invalid ownership")
		}
		saved.UserMemoryPools = append(saved.UserMemoryPools, runtimeUserPoolState{Base: pool.base, Size: pool.size, Cursor: pool.cursor, FreeCalls: pool.freeCalls})
	}
	if cache := runtime.pixelOps; cache != nil {
		saved.PixelOp = &runtimePixelOpState{Function: cache.function, Param: cache.param}
		for _, pair := range metadataKeys(cache.results) {
			saved.PixelOp.Results = append(saved.PixelOp.Results, runtimePixelResultState{Pair: pair, Result: cache.results[pair]})
		}
	}
	if _, err := decodeRuntimeMetadata(runtime.client, saved); err != nil {
		return runtimeMetadataState{}, err
	}
	return saved, nil
}

// Decode into detached tables before changing any target field. Only metadata
// is covered here; a session restore must also validate its heap and workers
// before exposing the newly constructed runtime.
func decodeRuntimeMetadata(client *Client, saved runtimeMetadataState) (*initializationRuntime, error) {
	count := uint64(len(saved.Stubs)) + uint64(len(saved.Classes)) + uint64(len(saved.LoadedClasses)) + uint64(len(saved.ModuleClasses)) + uint64(len(saved.NativeMethods)) + uint64(len(saved.InitializedClasses)) + uint64(len(saved.LinkedModuleClasses)) + uint64(len(saved.ClassAliases)) + uint64(len(saved.ClassSummaries)) + uint64(len(saved.Objects)) + uint64(len(saved.WIPICAllocations)) + uint64(len(saved.ImageSourceBuffers)) + uint64(len(saved.ReleasedWIPIC)) + uint64(len(saved.ReleasedWIPICOrder)) + uint64(len(saved.UserMemoryPools))
	if saved.PixelOp != nil {
		count += uint64(len(saved.PixelOp.Results))
	}
	if count > maxMetadataEntries || len(saved.ReleasedWIPIC) > maxReleasedWIPIC || len(saved.ReleasedWIPICOrder) > maxReleasedWIPIC {
		return nil, fmt.Errorf("KTF metadata records exceed limit")
	}
	if saved.CodeCursor < uint64(platformCodeBase) || saved.CodeCursor > uint64(platformCodeBase)+platformCodeSize || saved.CodeCursor&3 != 0 || saved.CollectAt > platformDataSize+collectionFloor {
		return nil, fmt.Errorf("KTF metadata code cursor or collector trigger is invalid")
	}
	result := &initializationRuntime{client: client, codeCursor: saved.CodeCursor, nextNativeMethod: saved.NextNativeMethod, collectAt: saved.CollectAt,
		leds: saved.LEDs, menuTextCompatibility: saved.MenuTextCompatibility}
	var err error
	result.arena, err = restoreArenaState(saved.Arena, platformDataBase, platformDataSize)
	if err != nil {
		return nil, err
	}
	memory := client.core.Memory()
	if err := memory.ValidateRange(platformDataBase, platformDataSize, armcore.PermissionReadWrite); err != nil {
		return nil, err
	}
	if err := memory.ValidateRange(platformCodeBase, platformCodeSize, armcore.PermissionReadExecute); err != nil {
		return nil, err
	}
	if saved.ClassArena != nil {
		base := (uint64(ImageBase) + client.mapped + 4095) &^ uint64(4095)
		const size = 4 << 20
		if client.module || base+size > uint64(ThreadStackBase) {
			return nil, fmt.Errorf("KTF class arena differs from the client policy")
		}
		result.classArena, err = restoreArenaState(*saved.ClassArena, uint32(base), size)
		if err != nil {
			return nil, err
		}
		if err := memory.ValidateRange(uint32(base), size, armcore.PermissionReadWrite); err != nil {
			return nil, err
		}
	}
	word := func(address uint32, optional bool) error {
		if address == 0 && optional {
			return nil
		}
		if address == 0 || address&3 != 0 {
			return fmt.Errorf("KTF metadata has an invalid word address")
		}
		return memory.ValidateRange(address, 4, armcore.PermissionRead)
	}
	var totalBytes uint64
	name := func(value []byte) (string, error) {
		totalBytes += uint64(len(value))
		if len(value) == 0 || len(value) > maxMetadataName || totalBytes > maxMetadataBytes || bytes.IndexByte(value, 0) >= 0 {
			return "", fmt.Errorf("KTF metadata name is invalid or exceeds limit")
		}
		return string(value), nil
	}
	result.stubs = make(map[uint64]uint32, len(saved.Stubs))
	stubSpans := make([]arenaBlock, 0, len(saved.Stubs))
	for i, stub := range saved.Stubs {
		start := uint64(stub.Address &^ 1)
		if stub.Key>>32 > 255 || stub.Address&1 == 0 || start&3 != 0 || start < uint64(platformCodeBase) || start+svcStubSize > saved.CodeCursor || i > 0 && saved.Stubs[i-1].Key >= stub.Key {
			return nil, fmt.Errorf("KTF native stub record is invalid")
		}
		result.stubs[stub.Key] = stub.Address
		stubSpans = append(stubSpans, arenaBlock{start: start, end: start + svcStubSize})
	}
	if err := validateMetadataSpans(stubSpans); err != nil {
		return nil, err
	}
	classes := func(records []runtimeClassAddressState) (map[string]uint32, error) {
		values := make(map[string]uint32, len(records))
		for i, record := range records {
			key, err := name(record.Name)
			if err != nil {
				return nil, err
			}
			if i > 0 && bytes.Compare(records[i-1].Name, record.Name) >= 0 {
				return nil, fmt.Errorf("KTF class names are repeated or unordered")
			}
			if err := word(record.Address, false); err != nil {
				return nil, err
			}
			values[key] = record.Address
		}
		return values, nil
	}
	if result.classes, err = classes(saved.Classes); err != nil {
		return nil, err
	}
	if result.loadedClasses, err = classes(saved.LoadedClasses); err != nil {
		return nil, err
	}
	if result.moduleClassByName, err = classes(saved.ModuleClasses); err != nil {
		return nil, err
	}
	result.nativeMethods = make(map[uint32]runtimeJavaInvocation, len(saved.NativeMethods))
	for i, record := range saved.NativeMethods {
		if record.ID == 0 || record.ID > saved.NextNativeMethod || i > 0 && saved.NativeMethods[i-1].ID >= record.ID {
			return nil, fmt.Errorf("KTF native method ID is invalid")
		}
		class, err := name(record.Class)
		if err != nil {
			return nil, err
		}
		method, err := name(record.Name)
		if err != nil {
			return nil, err
		}
		descriptor, err := name(record.Descriptor)
		if err != nil {
			return nil, err
		}
		if _, err := jvm.ParseMethodDescriptor(descriptor); err != nil {
			return nil, err
		}
		result.nativeMethods[record.ID] = runtimeJavaInvocation{method: runtimeJavaMethod{class: class, name: method, descriptor: descriptor, accessFlags: record.AccessFlags}, container: record.Container}
	}
	flags := func(records []runtimeClassFlagState) (map[uint32]bool, error) {
		values := make(map[uint32]bool, len(records))
		for i, record := range records {
			if i > 0 && records[i-1].Address >= record.Address {
				return nil, fmt.Errorf("KTF class flags have duplicate or unordered addresses")
			}
			if err := word(record.Address, false); err != nil {
				return nil, err
			}
			values[record.Address] = record.Value
		}
		return values, nil
	}
	if result.initializedClasses, err = flags(saved.InitializedClasses); err != nil {
		return nil, err
	}
	if result.linkedModuleClasses, err = flags(saved.LinkedModuleClasses); err != nil {
		return nil, err
	}
	pairs := func(records []runtimeAddressPairState) (map[uint32]uint32, error) {
		values := make(map[uint32]uint32, len(records))
		for i, record := range records {
			if i > 0 && records[i-1].Address >= record.Address {
				return nil, fmt.Errorf("KTF metadata address pairs are repeated or unordered")
			}
			if err := word(record.Address, false); err != nil {
				return nil, err
			}
			if err := word(record.Target, false); err != nil {
				return nil, err
			}
			values[record.Address] = record.Target
		}
		return values, nil
	}
	if result.classAliases, err = pairs(saved.ClassAliases); err != nil {
		return nil, err
	}
	if result.imageSourceBuffers, err = pairs(saved.ImageSourceBuffers); err != nil {
		return nil, err
	}
	result.classSummaries = make(map[uint32]aotClassSummary, len(saved.ClassSummaries))
	for i, record := range saved.ClassSummaries {
		if len(record.Interfaces) > maxAOTInterfaces || i > 0 && saved.ClassSummaries[i-1].Address >= record.Address {
			return nil, fmt.Errorf("KTF cached class summary is invalid")
		}
		totalBytes += uint64(len(record.Interfaces)) * 4
		text, err := name(record.Name)
		if err != nil {
			return nil, err
		}
		if err := word(record.Address, false); err != nil {
			return nil, err
		}
		if err := word(record.Parent, true); err != nil {
			return nil, err
		}
		for _, address := range record.Interfaces {
			if err := word(address, true); err != nil {
				return nil, err
			}
		}
		result.classSummaries[record.Address] = aotClassSummary{name: text, parent: record.Parent, interfaces: slices.Clone(record.Interfaces)}
	}
	allocations := make([]arenaBlock, 0, len(saved.Objects)+len(saved.WIPICAllocations))
	allocation := func(address uint32, size uint64) error {
		end := uint64(address) + alignArenaSize(size)
		if address < platformDataBase || address&3 != 0 || size == 0 || size > maxPlatformAllocation || end > saved.Arena.Cursor {
			return fmt.Errorf("KTF tracked allocation has invalid bounds")
		}
		index, _ := slices.BinarySearchFunc(saved.Arena.Free, uint64(address), func(block arenaBlockState, start uint64) int { return cmp.Compare(block.End, start) })
		for index < len(saved.Arena.Free) && saved.Arena.Free[index].End <= uint64(address) {
			index++
		}
		if index < len(saved.Arena.Free) && saved.Arena.Free[index].Start < end {
			return fmt.Errorf("KTF tracked allocation overlaps free space")
		}
		allocations = append(allocations, arenaBlock{start: uint64(address), end: end})
		return nil
	}
	result.objects = make(map[uint32]objectRecord, len(saved.Objects))
	for i, record := range saved.Objects {
		if i > 0 && saved.Objects[i-1].Address >= record.Address {
			return nil, fmt.Errorf("KTF tracked objects are repeated or unordered")
		}
		if err := allocation(record.Address, uint64(record.Size)); err != nil {
			return nil, err
		}
		result.objects[record.Address] = objectRecord{size: record.Size, released: record.Released}
	}
	result.wipicAllocations = make(map[uint32]uint64, len(saved.WIPICAllocations))
	for i, record := range saved.WIPICAllocations {
		if record.Size <= uint64(wipicAllocationOverhead) || i > 0 && saved.WIPICAllocations[i-1].Address >= record.Address {
			return nil, fmt.Errorf("KTF WIPI allocations are invalid, repeated or unordered")
		}
		if err := allocation(record.Address, record.Size); err != nil {
			return nil, err
		}
		result.wipicAllocations[record.Address] = record.Size
	}
	if err := validateMetadataSpans(allocations); err != nil {
		return nil, err
	}
	for image, source := range result.imageSourceBuffers {
		if result.wipicAllocations[image] == 0 || result.wipicAllocations[source] == 0 {
			return nil, fmt.Errorf("KTF image source ownership has no live allocation")
		}
	}
	result.releasedWIPIC = make(map[uint32]struct{}, len(saved.ReleasedWIPIC))
	validReleased := func(address uint32) bool {
		return address >= platformDataBase && uint64(address) < saved.Arena.HighWater && address&3 == 0
	}
	for i, address := range saved.ReleasedWIPIC {
		if !validReleased(address) || result.wipicAllocations[address] != 0 || i > 0 && saved.ReleasedWIPIC[i-1] >= address || !slices.Contains(saved.ReleasedWIPICOrder, address) {
			return nil, fmt.Errorf("KTF released allocation set is invalid")
		}
		result.releasedWIPIC[address] = struct{}{}
	}
	for _, address := range saved.ReleasedWIPICOrder {
		if !validReleased(address) {
			return nil, fmt.Errorf("KTF released allocation order is invalid")
		}
	}
	result.releasedWIPICOrder = slices.Clone(saved.ReleasedWIPICOrder)
	result.userMemoryPools = make(map[uint32]*userMemoryPool, len(saved.UserMemoryPools))
	for i, pool := range saved.UserMemoryPools {
		if pool.Base == 0 || pool.Size == 0 || pool.Cursor > pool.Size || i > 0 && saved.UserMemoryPools[i-1].Base >= pool.Base {
			return nil, fmt.Errorf("KTF user memory pool is invalid")
		}
		if err := memory.ValidateRange(pool.Base, uint64(pool.Size), armcore.PermissionReadWrite); err != nil {
			return nil, err
		}
		result.userMemoryPools[pool.Base] = &userMemoryPool{base: pool.Base, size: pool.Size, cursor: pool.Cursor, freeCalls: pool.FreeCalls}
	}
	if cache := saved.PixelOp; cache != nil {
		if cache.Function == 0 {
			return nil, fmt.Errorf("KTF pixel operation cache has no function")
		}
		if err := memory.ValidateRange(cache.Function&^1, 2, armcore.PermissionExecute); err != nil {
			return nil, err
		}
		result.pixelOps = &pixelOpCache{function: cache.Function, param: cache.Param, results: make(map[uint32]uint16, len(cache.Results))}
		for i, record := range cache.Results {
			if i > 0 && cache.Results[i-1].Pair >= record.Pair {
				return nil, fmt.Errorf("KTF pixel results are repeated or unordered")
			}
			result.pixelOps.results[record.Pair] = record.Result
		}
	}
	b := saved.Bindings
	for _, address := range []uint32{b.Kernel, b.Java, b.WIPIC, b.Module, b.ModuleContext, b.ModuleJumpTable, b.JVMContext, b.ExceptionContext, b.ScreenFramebuffer, b.UserMemory, b.InputModeTable} {
		if err := word(address, true); err != nil {
			return nil, err
		}
	}
	if b.ExceptionContext != 0 {
		if err := memory.ValidateRange(b.ExceptionContext, javaExceptionContextWords*4, armcore.PermissionReadWrite); err != nil {
			return nil, err
		}
	}
	if b.CameraBoundsStore != 0 {
		if err := memory.ValidateRange(b.CameraBoundsStore, 2, armcore.PermissionExecute); err != nil {
			return nil, err
		}
	}
	result.kernelInterface, result.javaInterface, result.wipicInterface, result.moduleInterface = b.Kernel, b.Java, b.WIPIC, b.Module
	result.moduleContext, result.moduleJumpTable, result.jvmContext, result.exceptionContext = b.ModuleContext, b.ModuleJumpTable, b.JVMContext, b.ExceptionContext
	result.screenFramebuffer, result.userMemoryInterface, result.inputModeTableAddress, result.cameraBoundsStore = b.ScreenFramebuffer, b.UserMemory, b.InputModeTable, b.CameraBoundsStore
	return result, nil
}

func validateMetadataSpans(spans []arenaBlock) error {
	slices.SortFunc(spans, func(a, b arenaBlock) int { return cmp.Compare(a.start, b.start) })
	for i := 1; i < len(spans); i++ {
		if spans[i-1].end > spans[i].start {
			return fmt.Errorf("KTF metadata allocation spans overlap")
		}
	}
	return nil
}

func (runtime *initializationRuntime) restoreMetadataState(saved runtimeMetadataState) error {
	result, err := decodeRuntimeMetadata(runtime.client, saved)
	if err != nil {
		return err
	}
	runtime.arena, runtime.classArena = result.arena, result.classArena
	runtime.codeCursor, runtime.stubs = result.codeCursor, result.stubs
	runtime.classes, runtime.loadedClasses, runtime.moduleClassByName = result.classes, result.loadedClasses, result.moduleClassByName
	runtime.nativeMethods, runtime.nextNativeMethod = result.nativeMethods, result.nextNativeMethod
	runtime.initializedClasses, runtime.linkedModuleClasses = result.initializedClasses, result.linkedModuleClasses
	runtime.classAliases, runtime.classSummaries = result.classAliases, result.classSummaries
	runtime.objects, runtime.collectAt = result.objects, result.collectAt
	runtime.wipicAllocations, runtime.imageSourceBuffers = result.wipicAllocations, result.imageSourceBuffers
	runtime.releasedWIPIC, runtime.releasedWIPICOrder = result.releasedWIPIC, result.releasedWIPICOrder
	runtime.userMemoryPools, runtime.pixelOps = result.userMemoryPools, result.pixelOps
	runtime.kernelInterface, runtime.javaInterface, runtime.wipicInterface, runtime.moduleInterface = result.kernelInterface, result.javaInterface, result.wipicInterface, result.moduleInterface
	runtime.moduleContext, runtime.moduleJumpTable, runtime.jvmContext, runtime.exceptionContext = result.moduleContext, result.moduleJumpTable, result.jvmContext, result.exceptionContext
	runtime.screenFramebuffer, runtime.userMemoryInterface, runtime.inputModeTableAddress, runtime.cameraBoundsStore = result.screenFramebuffer, result.userMemoryInterface, result.inputModeTableAddress, result.cameraBoundsStore
	runtime.leds, runtime.menuTextCompatibility = result.leds, result.menuTextCompatibility
	runtime.rebaseArenaShadow()
	return nil
}
