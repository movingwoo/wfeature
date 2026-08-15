package jvm

import (
	"fmt"
	"strings"
	"unicode/utf8"
	"weak"
)

const maxAOTMembers = 1 << 16

// AOTClassMetadata is validated class metadata supplied by a platform native
// image. Addresses are opaque guest identifiers; the JVM never dereferences
// them as Host pointers.
type AOTClassMetadata struct {
	Address       uint32
	Name          string
	SuperName     string
	AccessFlags   uint16
	InstanceSize  uint16
	VTableAddress uint32
	VTable        []uint32
	Methods       []AOTMethodMetadata
	Fields        []AOTFieldMetadata
}

type AOTMethodMetadata struct {
	Address          uint32
	Name             string
	Descriptor       string
	Body             uint32
	NativeBody       uint32
	AccessFlags      uint16
	VTableIndex      uint16
	ExceptionEntries uint16
}

type AOTFieldMetadata struct {
	Address     uint32
	Name        string
	Descriptor  string
	Offset      uint32
	AccessFlags uint32
}

// RegisterAOTClass records platform-validated native class metadata. A second
// registration from the same guest address refreshes its vtable snapshot;
// conflicting names or addresses are rejected.
func (vm *VM) RegisterAOTClass(metadata AOTClassMetadata) error {
	if vm == nil {
		return fmt.Errorf("JVM is nil")
	}
	if err := validateAOTClass(metadata); err != nil {
		return err
	}
	metadata = cloneAOTClass(metadata)
	vm.aotMu.Lock()
	defer vm.aotMu.Unlock()
	if existing, ok := vm.aotClasses[metadata.Name]; ok && existing.Address != metadata.Address {
		return fmt.Errorf("AOT class %s is already registered at %#x", metadata.Name, existing.Address)
	}
	if existingName, ok := vm.aotAddresses[metadata.Address]; ok && existingName != metadata.Name {
		return fmt.Errorf("AOT class address %#x is already registered as %s", metadata.Address, existingName)
	}
	vm.aotClasses[metadata.Name] = metadata
	vm.aotAddresses[metadata.Address] = metadata.Name
	return nil
}

// RegisterAOTAddressAlias maps an additional guest address to an
// already-registered AOT class name. Real runtimes materialize the same
// class, notably array classes, at more than one guest record; the first
// registration stays canonical.
func (vm *VM) RegisterAOTAddressAlias(address uint32, name string) error {
	if vm == nil {
		return fmt.Errorf("JVM is nil")
	}
	if address == 0 {
		return fmt.Errorf("AOT class alias address is null")
	}
	vm.aotMu.Lock()
	defer vm.aotMu.Unlock()
	if _, ok := vm.aotClasses[name]; !ok {
		return fmt.Errorf("AOT class %s is not registered", name)
	}
	if existing, ok := vm.aotAddresses[address]; ok && existing != name {
		return fmt.Errorf("AOT class address %#x is already registered as %s", address, existing)
	}
	vm.aotAddresses[address] = name
	return nil
}

// AOTClasses snapshots every registered AOT class for diagnostics.
func (vm *VM) AOTClasses() []AOTClassMetadata {
	if vm == nil {
		return nil
	}
	vm.aotMu.RLock()
	defer vm.aotMu.RUnlock()
	classes := make([]AOTClassMetadata, 0, len(vm.aotClasses))
	for _, metadata := range vm.aotClasses {
		classes = append(classes, cloneAOTClass(metadata))
	}
	return classes
}

func (vm *VM) AOTClass(name string) (AOTClassMetadata, bool) {
	if vm == nil {
		return AOTClassMetadata{}, false
	}
	vm.aotMu.RLock()
	metadata, ok := vm.aotClasses[name]
	vm.aotMu.RUnlock()
	if !ok {
		return AOTClassMetadata{}, false
	}
	return cloneAOTClass(metadata), true
}

func (vm *VM) AOTClassAt(address uint32) (AOTClassMetadata, bool) {
	if vm == nil || address == 0 {
		return AOTClassMetadata{}, false
	}
	vm.aotMu.RLock()
	name, ok := vm.aotAddresses[address]
	metadata := vm.aotClasses[name]
	vm.aotMu.RUnlock()
	if !ok {
		return AOTClassMetadata{}, false
	}
	return cloneAOTClass(metadata), true
}

func (vm *VM) FindAOTMethod(classAddress uint32, name, descriptor string) (AOTMethodMetadata, bool, error) {
	if _, err := ParseMethodDescriptor(descriptor); err != nil {
		return AOTMethodMetadata{}, false, err
	}
	class, ok := vm.AOTClassAt(classAddress)
	if !ok {
		return AOTMethodMetadata{}, false, nil
	}
	visited := make(map[string]bool)
	for depth := 0; ; depth++ {
		if depth >= vm.config.MaxFrames {
			return AOTMethodMetadata{}, false, fmt.Errorf("AOT class hierarchy from %s exceeds limit %d", class.Name, vm.config.MaxFrames)
		}
		if visited[class.Name] {
			return AOTMethodMetadata{}, false, fmt.Errorf("AOT class hierarchy cycle at %s", class.Name)
		}
		visited[class.Name] = true
		for _, wantStatic := range []bool{false, true} {
			for _, method := range class.Methods {
				if method.Name == name && method.Descriptor == descriptor && method.AccessFlags&0x0008 != 0 == wantStatic {
					return method, true, nil
				}
			}
		}
		if class.SuperName == "" {
			return AOTMethodMetadata{}, false, nil
		}
		class, ok = vm.AOTClass(class.SuperName)
		if !ok {
			return AOTMethodMetadata{}, false, nil
		}
	}
}

func (vm *VM) FindAOTField(classAddress uint32, name, descriptor string) (AOTFieldMetadata, bool, error) {
	if _, err := ParseFieldDescriptor(descriptor); err != nil {
		return AOTFieldMetadata{}, false, err
	}
	class, ok := vm.AOTClassAt(classAddress)
	if !ok {
		return AOTFieldMetadata{}, false, nil
	}
	visited := make(map[string]bool)
	for depth := 0; ; depth++ {
		if depth >= vm.config.MaxFrames {
			return AOTFieldMetadata{}, false, fmt.Errorf("AOT class hierarchy from %s exceeds limit %d", class.Name, vm.config.MaxFrames)
		}
		if visited[class.Name] {
			return AOTFieldMetadata{}, false, fmt.Errorf("AOT class hierarchy cycle at %s", class.Name)
		}
		visited[class.Name] = true
		for _, wantStatic := range []bool{true, false} {
			for _, field := range class.Fields {
				if field.Name == name && field.Descriptor == descriptor && field.AccessFlags&0x0008 != 0 == wantStatic {
					return field, true, nil
				}
			}
		}
		if class.SuperName == "" {
			return AOTFieldMetadata{}, false, nil
		}
		class, ok = vm.AOTClass(class.SuperName)
		if !ok {
			return AOTFieldMetadata{}, false, nil
		}
	}
}

// StringText returns the Go text of a runtime-owned java/lang/String object.
// Platform bridges use it to decode string arguments crossing from guest code.
func StringText(object *Object) (string, bool) {
	return nativeStringObject(object)
}

// NewString creates the same runtime-owned java/lang/String object used by
// interpreted bytecode and native CLDC services.
func (vm *VM) NewString(value string) *Object {
	object := nativeStringValue(value)
	if vm != nil {
		vm.objectIdentity(object)
	}
	return object
}

func (vm *VM) NewClassObject(name string) (*Object, error) {
	if err := validateAOTClassName(name); err != nil {
		return nil, err
	}
	object := &Object{ClassName: "java/lang/Class", Native: name}
	if vm != nil {
		vm.objectIdentity(object)
	}
	return object, nil
}

// NewAOTInstance creates an uninitialized JVM object for a platform-owned AOT
// class. Constructors remain guest method bodies and are invoked separately by
// the platform bridge.
func (vm *VM) NewAOTInstance(classAddress uint32) (*Object, error) {
	if vm == nil {
		return nil, fmt.Errorf("JVM is nil")
	}
	metadata, ok := vm.AOTClassAt(classAddress)
	if !ok {
		return nil, fmt.Errorf("AOT class at %#x is not registered", classAddress)
	}
	if strings.HasPrefix(metadata.Name, "[") {
		return nil, fmt.Errorf("AOT class %s is an array class", metadata.Name)
	}
	object := &Object{ClassName: metadata.Name, Fields: make(map[string]Value)}
	vm.objectIdentity(object)
	return object, nil
}

// NewAOTArray creates the JVM-owned zero-filled array paired with a platform
// guest-memory layout. The supplied address must identify a registered array
// class descriptor such as [I or [Ljava/lang/Object;.
func (vm *VM) NewAOTArray(classAddress, length uint32) (*Object, error) {
	if vm == nil {
		return nil, fmt.Errorf("JVM is nil")
	}
	metadata, ok := vm.AOTClassAt(classAddress)
	if !ok {
		return nil, fmt.Errorf("AOT array class at %#x is not registered", classAddress)
	}
	typeInfo, err := ParseFieldDescriptor(metadata.Name)
	if err != nil || typeInfo.Kind != TypeArray || typeInfo.Component == nil {
		return nil, fmt.Errorf("AOT class %s is not an array class", metadata.Name)
	}
	if uint64(length) > uint64(vm.config.MaxArrayLength) {
		return nil, fmt.Errorf("array size %d exceeds limit %d", length, vm.config.MaxArrayLength)
	}
	if length > 1<<31-1 {
		return nil, fmt.Errorf("array size %d exceeds signed JVM length range", length)
	}
	return vm.newArray(*typeInfo.Component, int32(length))
}

// aotBinding pairs a guest address with the Go object behind it. The strong
// reference is what keeps that object alive while the guest can still reach
// the address. Dropping it hands the liveness question to Go's own collector,
// which is the only thing that can see the references held in Go frames — a
// parked guest worker's stack among them.
type aotBinding struct {
	pinned  *Object
	tracked weak.Pointer[Object]
}

func (binding aotBinding) object() *Object {
	if binding.pinned != nil {
		return binding.pinned
	}
	return binding.tracked.Value()
}

// BindAOTObject pins a Go JVM object behind an opaque guest address. This is
// the boundary used when native AOT code stores references in memory the Go GC
// cannot scan directly.
func (vm *VM) BindAOTObject(address uint32, object *Object) error {
	if vm == nil {
		return fmt.Errorf("JVM is nil")
	}
	if address == 0 {
		return fmt.Errorf("AOT object address is null")
	}
	if object == nil {
		return fmt.Errorf("AOT object at %#x is nil", address)
	}
	vm.objectIdentity(object)
	vm.aotMu.Lock()
	defer vm.aotMu.Unlock()
	if existing := vm.aotObjects[address].object(); existing != nil && existing != object {
		return fmt.Errorf("AOT object address %#x is already bound", address)
	}
	if existing := object.aotAddress.Load(); existing != 0 && existing != address {
		return fmt.Errorf("AOT object is already bound at address %#x", existing)
	}
	vm.aotObjects[address] = aotBinding{pinned: object, tracked: weak.Make(object)}
	object.aotAddress.Store(address)
	return nil
}

// AOTObject resolves a guest address back to its object, re-pinning one the
// collector had released: an address the guest hands back is in use again.
func (vm *VM) AOTObject(address uint32) (*Object, bool) {
	if vm == nil || address == 0 {
		return nil, false
	}
	vm.aotMu.RLock()
	binding, bound := vm.aotObjects[address]
	vm.aotMu.RUnlock()
	if !bound {
		return nil, false
	}
	if binding.pinned != nil {
		return binding.pinned, true
	}
	object := binding.tracked.Value()
	if object == nil {
		return nil, false
	}
	vm.aotMu.Lock()
	if current, ok := vm.aotObjects[address]; ok && current.pinned == nil {
		current.pinned = object
		vm.aotObjects[address] = current
	}
	vm.aotMu.Unlock()
	return object, true
}

// AOTObjectAt resolves a guest address without re-pinning it. It is how the
// platform reads the object graph of something it is about to release, where
// pinning is exactly what must not happen.
func (vm *VM) AOTObjectAt(address uint32) (*Object, bool) {
	if vm == nil || address == 0 {
		return nil, false
	}
	vm.aotMu.RLock()
	defer vm.aotMu.RUnlock()
	binding, bound := vm.aotObjects[address]
	if !bound {
		return nil, false
	}
	object := binding.object()
	return object, object != nil
}

// ReleaseAOTObject drops the strong reference to a bound object without
// forgetting the binding, so Go's collector decides whether anything else
// still holds it. AOTObject re-pins it if the guest asks for it again.
//
// The question only means anything once the guest's own reference graph has
// been mirrored with RetainAOTGraph; see there.
func (vm *VM) ReleaseAOTObject(address uint32) {
	if vm == nil {
		return
	}
	vm.aotMu.Lock()
	defer vm.aotMu.Unlock()
	if binding, ok := vm.aotObjects[address]; ok {
		binding.pinned = nil
		vm.aotObjects[address] = binding
	}
}

// RetainAOTGraph records the objects a bound object's guest payload names, so
// that Go's own reachability answers for one of these objects the same way the
// guest's would.
//
// It is not an optimisation, it is what makes releasing safe. A bound object's
// references live in guest bytes, which Go cannot see: without the mirror, an
// object reachable only from another object's payload looks unreferenced to Go
// the moment the platform releases it, and Go frees it — while the holder
// survives, is handed back to the guest, and the guest follows the reference
// into an address whose object is gone. Mirroring makes Go's answer cover
// everything an object can reach, and because it is Go doing the asking, a
// group of dead objects that only reference each other is still collected: Go
// collects cycles.
//
// The platform refreshes this for every object it tracks, on every cycle,
// including the ones it is not releasing — a pinned object's payload names
// things just as a released one's does.
func (vm *VM) RetainAOTGraph(address uint32, retain []*Object) {
	if vm == nil || address == 0 {
		return
	}
	vm.aotMu.Lock()
	defer vm.aotMu.Unlock()
	if binding, ok := vm.aotObjects[address]; ok {
		if object := binding.object(); object != nil {
			object.aotRetain = retain
		}
	}
}

// AOTObjectRetained reports whether anything still holds the object bound at
// an address. A released binding whose object Go has collected is one the
// platform can reclaim the guest memory for.
func (vm *VM) AOTObjectRetained(address uint32) bool {
	if vm == nil {
		return false
	}
	vm.aotMu.RLock()
	binding, bound := vm.aotObjects[address]
	vm.aotMu.RUnlock()
	return bound && binding.object() != nil
}

// ForgetAOTObject removes a binding entirely. The caller has established that
// neither the guest nor Go can reach the object.
func (vm *VM) ForgetAOTObject(address uint32) {
	if vm == nil {
		return
	}
	vm.aotMu.Lock()
	defer vm.aotMu.Unlock()
	if binding, ok := vm.aotObjects[address]; ok {
		if object := binding.object(); object != nil {
			object.aotAddress.Store(0)
		}
		delete(vm.aotObjects, address)
	}
}

// AOTAddress returns the stable guest address paired with a JVM object. It is
// used when Java references cross from the Go execution layer into native AOT
// method arguments or guest-memory fields.
//
// It re-pins a released binding, for the same reason AOTObject does and in the
// other direction: this is the moment an address the collector had written off
// enters guest code, which can store it anywhere before the next cycle looks.
// Answering with the address while leaving the object weakly held would let Go
// free it in that window, and the guest would be holding a live address whose
// object no longer exists.
func (vm *VM) AOTAddress(object *Object) (uint32, bool) {
	if vm == nil || object == nil {
		return 0, false
	}
	address := object.aotAddress.Load()
	if address == 0 {
		return 0, false
	}
	vm.aotMu.Lock()
	if binding, ok := vm.aotObjects[address]; ok && binding.pinned == nil {
		binding.pinned = object
		vm.aotObjects[address] = binding
	}
	vm.aotMu.Unlock()
	return address, true
}

// AOTObjectPinned reports whether a binding currently holds its object
// strongly. The platform's own record of what it has released goes stale
// whenever an address is handed back to the guest, and this is how it notices.
func (vm *VM) AOTObjectPinned(address uint32) bool {
	if vm == nil || address == 0 {
		return false
	}
	vm.aotMu.RLock()
	defer vm.aotMu.RUnlock()
	binding, bound := vm.aotObjects[address]
	return bound && binding.pinned != nil
}

func validateAOTClass(metadata AOTClassMetadata) error {
	if metadata.Address == 0 {
		return fmt.Errorf("AOT class address is null")
	}
	if err := validateAOTClassName(metadata.Name); err != nil {
		return fmt.Errorf("AOT class name: %w", err)
	}
	if metadata.SuperName != "" {
		if err := validateAOTClassName(metadata.SuperName); err != nil {
			return fmt.Errorf("AOT superclass name: %w", err)
		}
	}
	if metadata.VTableAddress&3 != 0 {
		return fmt.Errorf("AOT class %s vtable address %#x is not word-aligned", metadata.Name, metadata.VTableAddress)
	}
	if metadata.VTableAddress == 0 && len(metadata.VTable) != 0 {
		return fmt.Errorf("AOT class %s has vtable entries without an address", metadata.Name)
	}
	if len(metadata.Methods) > maxAOTMembers || len(metadata.Fields) > maxAOTMembers || len(metadata.VTable) > maxAOTMembers {
		return fmt.Errorf("AOT class %s exceeds %d members", metadata.Name, maxAOTMembers)
	}
	methods := make(map[string]bool, len(metadata.Methods))
	for index, method := range metadata.Methods {
		if method.Address == 0 || method.Name == "" || strings.IndexByte(method.Name, 0) >= 0 {
			return fmt.Errorf("AOT class %s method %d has invalid address or name", metadata.Name, index)
		}
		if _, err := ParseMethodDescriptor(method.Descriptor); err != nil {
			return fmt.Errorf("AOT class %s method %s: %w", metadata.Name, method.Name, err)
		}
		key := method.Name + "\x00" + method.Descriptor
		if methods[key] {
			return fmt.Errorf("AOT class %s has duplicate method %s%s", metadata.Name, method.Name, method.Descriptor)
		}
		methods[key] = true
	}
	fields := make(map[string]bool, len(metadata.Fields))
	for index, field := range metadata.Fields {
		if field.Address == 0 || field.Name == "" || strings.IndexByte(field.Name, 0) >= 0 {
			return fmt.Errorf("AOT class %s field %d has invalid address or name", metadata.Name, index)
		}
		if _, err := ParseFieldDescriptor(field.Descriptor); err != nil {
			return fmt.Errorf("AOT class %s field %s: %w", metadata.Name, field.Name, err)
		}
		key := field.Name + "\x00" + field.Descriptor
		if fields[key] {
			return fmt.Errorf("AOT class %s has duplicate field %s:%s", metadata.Name, field.Name, field.Descriptor)
		}
		fields[key] = true
	}
	return nil
}

func validateAOTClassName(name string) error {
	if name == "" || len(name) > 65535 || !utf8.ValidString(name) || strings.IndexByte(name, 0) >= 0 {
		return fmt.Errorf("invalid class name %q", name)
	}
	if name[0] == '[' {
		typeInfo, err := ParseFieldDescriptor(name)
		if err != nil || typeInfo.Kind != TypeArray {
			return fmt.Errorf("invalid array class name %q", name)
		}
		return nil
	}
	if name[0] == '/' || name[len(name)-1] == '/' || strings.Contains(name, "//") || strings.ContainsAny(name, ".;[") || strings.ContainsRune(name, '\\') {
		return fmt.Errorf("invalid class name %q", name)
	}
	return nil
}

func cloneAOTClass(metadata AOTClassMetadata) AOTClassMetadata {
	metadata.VTable = append([]uint32(nil), metadata.VTable...)
	metadata.Methods = append([]AOTMethodMetadata(nil), metadata.Methods...)
	metadata.Fields = append([]AOTFieldMetadata(nil), metadata.Fields...)
	return metadata
}
