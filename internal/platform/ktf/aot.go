package ktf

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"strings"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/movingwoo/wfeature/internal/armcore"
	"github.com/movingwoo/wfeature/internal/jvm"
)

const (
	maxAOTMemberPointers = 1 << 16
	maxAOTNameBytes      = 1024
	javaClassSize        = 20
	javaDescriptorSize   = 36
	javaMethodSize       = 28
	javaFieldSize        = 16
	javaInstanceSize     = 8
	javaInstanceHeader   = 4
	javaArrayLengthSize  = 4
	javaExceptionEntry   = 16
	javaExceptionHandler = 68
	// javaExceptionObject is where a matched handler is handed what was
	// thrown. A catch block reads it from there; see findAOTExceptionHandler.
	javaExceptionObject  = 16
	javaExceptionContext = 24
	javaExceptionHead    = 32
	maxExceptionHandlers = 64
)

type aotExceptionUnwind struct {
	contextBase uint32
	target      uint32
	nextPC      uint32
}

// UncaughtAOTException transports a guest-created exception back across the
// ARM bridge when no native AOT handler matches it. Address is the pinned guest
// object address; errors.As also reaches the wrapped JVM GuestException.
type UncaughtAOTException struct {
	Address   uint32
	Exception *jvm.GuestException
}

func (exception *UncaughtAOTException) Error() string {
	if exception == nil || exception.Exception == nil {
		return "uncaught KTF Java exception"
	}
	return "uncaught KTF Java exception " + exception.Exception.Error()
}

func (exception *UncaughtAOTException) Unwrap() error {
	if exception == nil {
		return nil
	}
	return exception.Exception
}

func (unwind *aotExceptionUnwind) Error() string {
	return fmt.Sprintf(
		"KTF AOT Java exception unwind: context=%#x target=%#x next PC=%#x",
		unwind.contextBase,
		unwind.target,
		unwind.nextPC,
	)
}

func (runtime *initializationRuntime) registerAOTClass(ctx context.Context, thread *armcore.Thread) (uint32, error) {
	address, err := thread.Register(0)
	if err != nil {
		return 0, err
	}
	metadata, err := runtime.readAOTClass(address)
	if err != nil {
		return 0, fmt.Errorf("read registered KTF Java class at %#x: %w", address, err)
	}
	if err := runtime.client.vm.RegisterAOTClass(metadata); err != nil {
		return 0, fmt.Errorf("register KTF AOT class %s: %w", metadata.Name, err)
	}
	runtime.registerAOTAncestors(address)
	runtime.callbacks.RegisteredClasses++
	if err := runtime.runAOTClassInitializer(ctx, thread, metadata); err != nil {
		return 0, err
	}
	return 0, nil
}

// runAOTClassInitializer executes a registered class's own <clinit> exactly
// once, matching the original runtime that initializes a class as part of
// registration. KTF static field words live inside guest field records, so
// skipping this leaves every guest static zero.
func (runtime *initializationRuntime) runAOTClassInitializer(ctx context.Context, thread *armcore.Thread, metadata jvm.AOTClassMetadata) error {
	if runtime.initializedClasses[metadata.Address] {
		return nil
	}
	if runtime.initializedClasses == nil {
		runtime.initializedClasses = make(map[uint32]bool)
	}
	runtime.initializedClasses[metadata.Address] = true
	var initializer *jvm.AOTMethodMetadata
	for index := range metadata.Methods {
		method := &metadata.Methods[index]
		if method.Name == "<clinit>" && method.Descriptor == "()V" && method.AccessFlags&0x0008 != 0 {
			initializer = method
			break
		}
	}
	if initializer == nil {
		return nil
	}
	runtime.countDiagnostic("clinit " + metadata.Name)
	body := initializer.Body
	arguments := []uint32{0}
	if initializer.AccessFlags&0x0100 != 0 {
		body = initializer.NativeBody
		container, err := runtime.allocateWords([]uint32{0, 0})
		if err != nil {
			return fmt.Errorf("allocate KTF class initializer container for %s: %w", metadata.Name, err)
		}
		arguments = []uint32{0, container}
	}
	if body == 0 {
		return fmt.Errorf("KTF AOT class %s <clinit> body is null", metadata.Name)
	}
	if err := runtime.enterAOTCall(); err != nil {
		return err
	}
	defer runtime.leaveAOTCall()
	for {
		_, callErr := runtime.client.core.Call(ctx, thread, body, ReturnAddress, arguments, runtime.handleSupervisorCall)
		if callErr == nil {
			return nil
		}
		var unwind *aotExceptionUnwind
		if errors.As(callErr, &unwind) {
			body = unwind.nextPC
			arguments = []uint32{unwind.contextBase, unwind.target}
			continue
		}
		return fmt.Errorf("execute KTF AOT class initializer %s.<clinit>: %w", metadata.Name, callErr)
	}
}

func (runtime *initializationRuntime) newAOTInstance(thread *armcore.Thread) (uint32, error) {
	classAddress, err := thread.Register(0)
	if err != nil {
		return 0, err
	}
	address, _, err := runtime.allocateAOTInstance(classAddress)
	return address, err
}

func (runtime *initializationRuntime) allocateAOTInstance(classAddress uint32) (uint32, *jvm.Object, error) {
	metadata, err := runtime.resolveAOTClass(classAddress)
	if err != nil {
		return 0, nil, err
	}
	object, err := runtime.client.vm.NewAOTInstance(classAddress)
	if err != nil {
		return 0, nil, fmt.Errorf("create KTF AOT instance of %s: %w", metadata.Name, err)
	}
	address, err := runtime.allocateAOTObject(metadata, make([]byte, metadata.InstanceSize), object)
	if err != nil {
		return 0, nil, err
	}
	runtime.recordDiagnostic(diagEvent{kind: diagNew, name: metadata.Name, site: address, hasSite: true})
	return address, object, nil
}

func (runtime *initializationRuntime) newAOTArray(thread *armcore.Thread) (uint32, error) {
	elementType, err := thread.Register(0)
	if err != nil {
		return 0, err
	}
	count, err := thread.Register(1)
	if err != nil {
		return 0, err
	}
	if count > maxJavaArrayElements {
		return 0, fmt.Errorf("KTF Java array length %d exceeds %d", count, maxJavaArrayElements)
	}
	metadata, err := runtime.resolveAOTArrayClass(elementType)
	if err != nil {
		return 0, err
	}
	address, err := runtime.allocateAOTArrayObject(metadata, count)
	if err != nil {
		return 0, err
	}
	lr, _ := thread.Register(armcore.RegisterLR)
	runtime.recordDiagnostic(diagEvent{
		kind:    diagNewArray,
		name:    metadata.Name,
		nums:    [5]uint32{uint32(count), address},
		site:    lr,
		hasSite: true,
	})
	return address, nil
}

func (runtime *initializationRuntime) allocateAOTArrayObject(metadata jvm.AOTClassMetadata, count uint32) (uint32, error) {
	elementSize, err := aotArrayElementSize(metadata.Name)
	if err != nil {
		return 0, err
	}
	payloadSize := uint64(javaArrayLengthSize) + uint64(count)*elementSize
	totalSize := uint64(javaInstanceSize+javaInstanceHeader) + payloadSize
	if totalSize > maxPlatformAllocation {
		return 0, fmt.Errorf("KTF Java array layout %d exceeds allocation limit %d", totalSize, maxPlatformAllocation)
	}
	object, err := runtime.client.vm.NewAOTArray(metadata.Address, count)
	if err != nil {
		return 0, fmt.Errorf("create KTF AOT array %s: %w", metadata.Name, err)
	}
	payload := make([]byte, int(payloadSize))
	binary.LittleEndian.PutUint32(payload, count)
	return runtime.allocateAOTObject(metadata, payload, object)
}

func (runtime *initializationRuntime) resolveAOTArrayClass(elementType uint32) (jvm.AOTClassMetadata, error) {
	if elementType <= 0x100 {
		var descriptor string
		switch byte(elementType) {
		case 'Z', 'B', 'C', 'S', 'I', 'J', 'F', 'D':
			descriptor = "[" + string(rune(elementType))
		default:
			return jvm.AOTClassMetadata{}, fmt.Errorf("KTF Java array element type %#x is invalid", elementType)
		}
		classAddress, err := runtime.ensureJavaClass(descriptor)
		if err != nil {
			return jvm.AOTClassMetadata{}, err
		}
		metadata, ok := runtime.client.vm.AOTClassAt(classAddress)
		if !ok {
			return jvm.AOTClassMetadata{}, fmt.Errorf("KTF AOT array class %s is not registered", descriptor)
		}
		return metadata, nil
	}

	metadata, err := runtime.resolveAOTClass(elementType)
	if err != nil {
		return jvm.AOTClassMetadata{}, err
	}
	typeInfo, parseErr := jvm.ParseFieldDescriptor(metadata.Name)
	if parseErr != nil || typeInfo.Kind != jvm.TypeArray || typeInfo.Component == nil {
		return jvm.AOTClassMetadata{}, fmt.Errorf("KTF Java array class pointer %#x names non-array %q", elementType, metadata.Name)
	}
	return metadata, nil
}

// aotHierarchyLimit bounds a superclass walk over guest records. A chain this
// deep is a malformed image rather than a class hierarchy.
const aotHierarchyLimit = 64

func (runtime *initializationRuntime) resolveAOTClass(address uint32) (jvm.AOTClassMetadata, error) {
	metadata, err := runtime.registerAOTClassRecord(address)
	if err != nil {
		return jvm.AOTClassMetadata{}, err
	}
	runtime.registerAOTAncestors(address)
	return metadata, nil
}

// registerAOTAncestors registers the superclass chain a class record points at.
// KTF hands the runtime one class at a time — GetClass answers the class it was
// asked for, and the guest registers a class as it initializes it — so a method
// or field a class inherits had nowhere for the lookup to walk to: a title
// whose Jlet does not override startApp reported its own startApp missing, and
// a field miss reported the parent record as a bare address because nothing had
// ever named it.
//
// The chain is in the image already. A class descriptor holds its parent's
// record address, and reading that record is metadata only, with no guest code
// behind it and no class initializer run — the guest still registers the parent
// itself when it initializes it, and that registration refreshes what is here.
//
// A record that will not read is not fatal. This is enrichment of a lookup that
// worked before without it, so an unreadable ancestor stops the walk and is
// logged rather than failing the class that asked.
func (runtime *initializationRuntime) registerAOTAncestors(address uint32) {
	visited := map[uint32]bool{address: true}
	for depth := 0; depth < aotHierarchyLimit; depth++ {
		parent, err := runtime.aotSuperAddress(address)
		if err != nil {
			runtime.client.log("KTF AOT parent record unreadable", "class", fmt.Sprintf("%#x", address), "error", err)
			return
		}
		if parent == 0 || visited[parent] {
			return
		}
		visited[parent] = true
		if _, ok := runtime.client.vm.AOTClassAt(parent); !ok {
			if _, err := runtime.registerAOTClassRecord(parent); err != nil {
				runtime.client.log("KTF AOT parent class not registered", "class", fmt.Sprintf("%#x", address), "parent", fmt.Sprintf("%#x", parent), "error", err)
				return
			}
		}
		address = parent
	}
	runtime.client.log("KTF AOT class hierarchy is deeper than the limit", "class", fmt.Sprintf("%#x", address), "limit", aotHierarchyLimit)
}

// aotSuperAddress answers the record address of a class record's parent, or
// zero at the root of the hierarchy.
func (runtime *initializationRuntime) aotSuperAddress(address uint32) (uint32, error) {
	if address&3 != 0 {
		return 0, fmt.Errorf("class address %#x is not word-aligned", address)
	}
	class, err := runtime.readAOTBytes(address, javaClassSize, "class")
	if err != nil {
		return 0, err
	}
	descriptorAddress := binary.LittleEndian.Uint32(class[8:])
	if descriptorAddress&3 != 0 {
		return 0, fmt.Errorf("class descriptor address %#x is not word-aligned", descriptorAddress)
	}
	descriptor, err := runtime.readAOTBytes(descriptorAddress, javaDescriptorSize, "class descriptor")
	if err != nil {
		return 0, err
	}
	return binary.LittleEndian.Uint32(descriptor[8:]), nil
}

func (runtime *initializationRuntime) registerAOTClassRecord(address uint32) (jvm.AOTClassMetadata, error) {
	if metadata, ok := runtime.client.vm.AOTClassAt(address); ok {
		return metadata, nil
	}
	metadata, err := runtime.readAOTClass(address)
	if err != nil {
		return jvm.AOTClassMetadata{}, fmt.Errorf("read KTF AOT class at %#x for allocation: %w", address, err)
	}
	if existing, ok := runtime.client.vm.AOTClass(metadata.Name); ok {
		// The guest materialized an already-known class at a second record.
		// The first registration stays canonical; the new address becomes an
		// alias so later guest references resolve to the same class.
		if err := runtime.client.vm.RegisterAOTAddressAlias(address, metadata.Name); err != nil {
			return jvm.AOTClassMetadata{}, fmt.Errorf("alias KTF AOT class %s at %#x: %w", metadata.Name, address, err)
		}
		return existing, nil
	}
	if err := runtime.client.vm.RegisterAOTClass(metadata); err != nil {
		return jvm.AOTClassMetadata{}, fmt.Errorf("register KTF AOT class %s for allocation: %w", metadata.Name, err)
	}
	return metadata, nil
}

func (runtime *initializationRuntime) allocateAOTObject(metadata jvm.AOTClassMetadata, payload []byte, object *jvm.Object) (uint32, error) {
	// Strings publish their real content through the guest field layout;
	// client helpers read the UTF-16 array and count directly.
	if metadata.Name == "java/lang/String" && len(payload) >= 12 {
		if text, ok := jvm.StringText(object); ok {
			units := utf16.Encode([]rune(text))
			arrayAddress, err := runtime.allocateGuestCharArray(units)
			if err != nil {
				return 0, fmt.Errorf("allocate KTF string content: %w", err)
			}
			binary.LittleEndian.PutUint32(payload[0:], arrayAddress)
			binary.LittleEndian.PutUint32(payload[4:], 0)
			binary.LittleEndian.PutUint32(payload[8:], uint32(len(units)))
		}
	}
	header, err := runtime.aotVTableHeader(metadata)
	if err != nil {
		return 0, err
	}
	totalSize := uint64(javaInstanceSize+javaInstanceHeader) + uint64(len(payload))
	address, err := runtime.allocate(totalSize)
	if err != nil {
		return 0, err
	}
	fieldsAddress := address + javaInstanceSize
	data := make([]byte, int(totalSize))
	binary.LittleEndian.PutUint32(data[0:], fieldsAddress)
	binary.LittleEndian.PutUint32(data[4:], metadata.Address)
	binary.LittleEndian.PutUint32(data[javaInstanceSize:], header)
	copy(data[javaInstanceSize+javaInstanceHeader:], payload)
	if err := runtime.client.core.Memory().Write(address, data); err != nil {
		return 0, fmt.Errorf("write KTF AOT object %s at %#x: %w", metadata.Name, address, err)
	}
	if err := runtime.client.vm.BindAOTObject(address, object); err != nil {
		return 0, fmt.Errorf("bind KTF AOT object %s at %#x: %w", metadata.Name, address, err)
	}
	runtime.trackObject(address, totalSize)
	// From here the array's elements live in the guest payload alone. Whatever
	// the Host had put in them moves across as the storage is bound, and every
	// later read or write on either side goes to the same bytes.
	if component, length, ok := jvm.ArrayComponent(object); ok {
		storage, storageErr := runtime.newGuestArrayStorage(address, component, length)
		if storageErr != nil {
			return 0, storageErr
		}
		if err := jvm.BindArrayStorage(object, storage); err != nil {
			return 0, fmt.Errorf("bind KTF array %s storage at %#x: %w", metadata.Name, address, err)
		}
	}
	return address, nil
}

// aotVTableHeader builds the object-header word read by guest virtual
// dispatch and array-store checks. Real clients decode it as a class-record
// offset relative to the JVM context: dispatch reads the vtable through
// header>>5 plus 12 and type checks read the descriptor through header>>5
// plus 8. Client class records live in the native image, beyond the shifted
// offset range, so every class gets one bounded dispatch-alias record inside
// the platform arena and the header encodes that alias.
// aotArrayElementBytes reads or writes one element of a bound guest array.
// Values follow the primitive sizes; references store the paired guest
// address word.
func aotArrayElementBytes(component jvm.Type) (int, error) {
	switch component.Kind {
	case jvm.TypeBoolean, jvm.TypeByte:
		return 1, nil
	case jvm.TypeChar, jvm.TypeShort:
		return 2, nil
	case jvm.TypeInt, jvm.TypeFloat, jvm.TypeReference, jvm.TypeArray:
		return 4, nil
	case jvm.TypeLong, jvm.TypeDouble:
		return 8, nil
	default:
		return 0, fmt.Errorf("invalid array component %s", component.Descriptor())
	}
}

// allocateGuestCharArray builds a bound [C guest array holding the supplied
// UTF-16 units and fills the paired JVM array with the same values.
func (runtime *initializationRuntime) allocateGuestCharArray(units []uint16) (uint32, error) {
	if uint64(len(units)) > uint64(maxJavaStringUnits) {
		return 0, fmt.Errorf("KTF string content %d units exceeds %d", len(units), maxJavaStringUnits)
	}
	classAddress, err := runtime.ensureJavaClass("[C")
	if err != nil {
		return 0, err
	}
	metadata, ok := runtime.client.vm.AOTClassAt(classAddress)
	if !ok {
		return 0, fmt.Errorf("KTF [C class at %#x is not registered", classAddress)
	}
	address, err := runtime.allocateAOTArrayObject(metadata, uint32(len(units)))
	if err != nil {
		return 0, err
	}
	if len(units) == 0 {
		return address, nil
	}
	values := make([]jvm.Value, len(units))
	for index, unit := range units {
		values[index] = jvm.IntValue(int32(unit))
	}
	object, ok := runtime.client.vm.AOTObject(address)
	if !ok {
		return 0, fmt.Errorf("KTF string array at %#x is not bound", address)
	}
	// The array is guest-backed, so filling it through the JVM boundary is
	// what puts the units in the memory the client helpers read.
	if err := jvm.SetArrayRange(object, 0, values); err != nil {
		return 0, fmt.Errorf("fill KTF string array: %w", err)
	}
	return address, nil
}

func (runtime *initializationRuntime) aotVTableHeader(metadata jvm.AOTClassMetadata) (uint32, error) {
	if runtime.jvmContext == 0 {
		return 0, fmt.Errorf("KTF JVM context is not prepared")
	}
	alias, ok := runtime.classAliases[metadata.Address]
	if !ok {
		record, err := runtime.readAOTBytes(metadata.Address, javaClassSize, "class record for dispatch alias")
		if err != nil {
			return 0, fmt.Errorf("read KTF class %s for dispatch alias: %w", metadata.Name, err)
		}
		aliasData := make([]byte, javaClassSize)
		copy(aliasData, record)
		alias, err = runtime.allocateBytes(aliasData)
		if err != nil {
			return 0, fmt.Errorf("allocate KTF dispatch alias for %s: %w", metadata.Name, err)
		}
		var next [4]byte
		binary.LittleEndian.PutUint32(next[:], alias+4)
		if err := runtime.client.core.Memory().Write(alias, next[:]); err != nil {
			return 0, fmt.Errorf("write KTF dispatch alias for %s: %w", metadata.Name, err)
		}
		// An alias is a class record in every respect the guest can see, so it
		// is one the guest passes back: it reaches a virtual call through the
		// object header and then hands the same word to the method and field
		// lookups. Registering the address is what makes those lookups answer
		// about the class rather than report an unregistered record — a title
		// stopped on the first paint of its card that way.
		if err := runtime.client.vm.RegisterAOTAddressAlias(alias, metadata.Name); err != nil {
			return 0, fmt.Errorf("register KTF dispatch alias for %s: %w", metadata.Name, err)
		}
		runtime.classAliases[metadata.Address] = alias
	}
	if alias <= runtime.jvmContext {
		return 0, fmt.Errorf("KTF dispatch alias %#x precedes the JVM context", alias)
	}
	offset := alias - runtime.jvmContext
	if offset >= 1<<26 {
		return 0, fmt.Errorf("KTF dispatch alias offset %#x exceeds the header range", offset)
	}
	return offset << 5, nil
}

func aotArrayElementSize(name string) (uint64, error) {
	typeInfo, err := jvm.ParseFieldDescriptor(name)
	if err != nil || typeInfo.Kind != jvm.TypeArray || typeInfo.Component == nil {
		return 0, fmt.Errorf("KTF AOT class %q is not an array descriptor", name)
	}
	switch typeInfo.Component.Kind {
	case jvm.TypeBoolean, jvm.TypeByte:
		return 1, nil
	case jvm.TypeChar, jvm.TypeShort:
		return 2, nil
	case jvm.TypeInt, jvm.TypeFloat, jvm.TypeReference, jvm.TypeArray:
		return 4, nil
	case jvm.TypeLong, jvm.TypeDouble:
		return 8, nil
	default:
		return 0, fmt.Errorf("KTF AOT array %s has invalid component type", name)
	}
}

// describeAOTOperands names the bound arrays among a set of candidate guest
// words, reporting for each the length the runtime recorded and the length
// word the guest reads from the header. The two disagreeing is the runtime's
// bug; both agreeing points the investigation back at the index instead.
func (runtime *initializationRuntime) describeAOTOperands(candidates []uint32) string {
	seen := make(map[uint32]bool, len(candidates))
	described := make([]string, 0, 4)
	for _, address := range candidates {
		if address == 0 || seen[address] {
			continue
		}
		seen[address] = true
		object, ok := runtime.client.vm.AOTObject(address)
		if !ok || object == nil {
			continue
		}
		if _, values, arrayErr := jvm.ArraySnapshot(object); arrayErr == nil {
			guestLength := "unreadable"
			if words, err := runtime.readAOTWords(address+javaInstanceSize+javaInstanceHeader, 1, "array length probe"); err == nil {
				guestLength = fmt.Sprintf("%d", words[0])
			}
			described = append(described, fmt.Sprintf("%#x %s runtime=%d guest=%s",
				address, object.ClassName, len(values), guestLength))
			continue
		}
		// Guest dispatch reads an object's first word and jumps through it, so
		// an object whose dispatch word names a method with the wrong shape is
		// how a well-formed call site lands in a method expecting more
		// arguments than the caller prepared.
		described = append(described, fmt.Sprintf("%#x %s%s", address, object.ClassName, runtime.describeDispatchWord(address)))
	}
	return strings.Join(described, "; ")
}

// describeDispatchWord decodes the class an object dispatches through and
// checks it against the class the object was allocated as. Guest virtual calls
// find their vtable at jvmContext plus the header's arithmetic-shifted offset,
// so a header naming the wrong class sends a call site into another class's
// vtable slot — landing on a method with an unrelated signature, which reads
// arguments the caller never prepared. The two names disagreeing is that bug;
// agreeing rules it out.
func (runtime *initializationRuntime) describeDispatchWord(address uint32) string {
	words, err := runtime.readAOTWords(address, 3, "object header")
	if err != nil {
		return " header=unreadable"
	}
	payload, boundClass, header := words[0], words[1], words[2]
	if payload != address+javaInstanceSize {
		return fmt.Sprintf(" payload=%#x (expected %#x)", payload, address+javaInstanceSize)
	}
	// The guest recovers the alias with an arithmetic shift, so the offset is
	// read back signed exactly as the guest reads it.
	alias := uint32(int64(runtime.jvmContext) + int64(int32(header)>>5))
	bound := "unbound"
	if metadata, ok := runtime.client.vm.AOTClassAt(boundClass); ok {
		bound = metadata.Name
	}
	dispatched := "unknown"
	for classAddress, candidate := range runtime.classAliases {
		if candidate != alias {
			continue
		}
		if metadata, ok := runtime.client.vm.AOTClassAt(classAddress); ok {
			dispatched = metadata.Name
		}
		break
	}
	agreement := "ok"
	if dispatched != bound {
		agreement = "MISMATCH"
	}
	return fmt.Sprintf(" bound=%s dispatch=%s alias=%#x %s", bound, dispatched, alias, agreement)
}

// throwStackWords is how much of the guest stack a throw records.
const throwStackWords = 48

// describeGuestFault turns a guest instruction fault into the same evidence a
// guest throw already carries. A fault comes back as an address and an opcode
// — "read guest memory at 0x4" — and the address is never the question: what
// was being dispatched, on which object, and from which Java method are. The
// registers at the faulting instruction hold all three, so they are named the
// way every other boundary event names a guest word.
//
// It reads only, and every read is best effort: a fault has already left the
// guest somewhere unexpected, so a probe that fails says so and the rest of
// the report still arrives.
func (runtime *initializationRuntime) describeGuestFault(faultContext armcore.Context, fault *armcore.InstructionError) string {
	registers := faultContext.Registers[:armcore.RegisterSP]
	stackPointer := faultContext.Registers[armcore.RegisterSP]
	linkRegister := faultContext.Registers[armcore.RegisterLR]
	stack := runtime.readStackWindow(stackPointer, throwStackWords)
	parts := []string{fmt.Sprintf("regs=%#x sp=%#x lr=%#x", registers, stackPointer, linkRegister)}
	symbols := runtime.aotSymbolIndex()
	// The engine has already advanced the program counter past the faulting
	// instruction, so the address that means anything is the one the fault
	// itself carries.
	if name, ok := symbolizeAOTAddress(symbols, fault.PC); ok {
		parts = append(parts, "pc="+name)
	}
	if name, ok := symbolizeAOTAddress(symbols, linkRegister); ok {
		parts = append(parts, "lr="+name)
	}
	// A dispatch fault is a question about an object's header, which is what
	// describeAOTOperands answers for every guest word the frame was holding.
	if operands := runtime.describeAOTOperands(append(append([]uint32{}, registers...), stack...)); operands != "" {
		parts = append(parts, "operands="+operands)
	}
	if frames := runtime.symbolizeAOTStack(stack); frames != "" {
		parts = append(parts, "frames="+frames)
	}
	// A guest stack grows down into nothing, so running off the end of one is
	// reported as an access to unmapped memory a few bytes below a mapping —
	// which reads like a wild pointer and is not one. Saying so is the whole
	// difference between "this title dereferences garbage" and "this title
	// recursed", and the two are investigated in opposite directions.
	faultAddress := stackPointer
	var access *armcore.AccessError
	if errors.As(fault.Cause, &access) {
		faultAddress = access.Address
	}
	if note := runtime.client.describeStackOverflow(faultAddress, stackPointer); note != "" {
		parts = append(parts, note)
	}
	return strings.Join(parts, " ")
}

// guestFault reports whether an error from a guest run is an instruction fault
// rather than one of the two control-flow errors the AOT bridge raises through
// the same return.
func guestFault(err error) (*armcore.InstructionError, bool) {
	var unwind *aotExceptionUnwind
	if errors.As(err, &unwind) {
		return nil, false
	}
	var uncaught *UncaughtAOTException
	if errors.As(err, &uncaught) {
		return nil, false
	}
	var fault *armcore.InstructionError
	if errors.As(err, &fault) {
		return fault, true
	}
	return nil, false
}

// readStackWindow reads up to words from the stack pointer, shrinking until
// the read fits the mapping. A thread whose stack is nearly empty has fewer
// words above SP than the window asks for, and the whole probe is best effort:
// returning the words that do exist beats returning none.
func (runtime *initializationRuntime) readStackWindow(stackPointer uint32, words int) []uint32 {
	for count := words; count > 0; count /= 2 {
		if window, err := runtime.readAOTWords(stackPointer, uint32(count), "throw stack probe"); err == nil {
			return window
		}
	}
	return nil
}

// symbolizeAOTStack annotates a stack window word by word. A window holds dead
// residue as well as live frames and the two are indistinguishable from the
// values alone, so each word keeps its slot number and nothing is presented as
// a call chain: the slots say where a name was found, and reading a chain out
// of them is the analyst's job, not this function's guess.
func (runtime *initializationRuntime) symbolizeAOTStack(stack []uint32) string {
	imageEnd := ImageBase + uint32(runtime.client.image.MappedSize())
	symbols := runtime.aotSymbolIndex()
	annotated := make([]string, 0, 16)
	for slot, word := range stack {
		switch {
		case word&^1 >= ImageBase && word&^1 < imageEnd:
			symbol, ok := symbolizeAOTAddress(symbols, word)
			if !ok {
				symbol = "code?"
			}
			annotated = append(annotated, fmt.Sprintf("[%d]=%#x %s", slot, word, symbol))
		case word >= platformDataBase:
			if object, ok := runtime.client.vm.AOTObject(word); ok && object != nil {
				annotated = append(annotated, fmt.Sprintf("[%d]=%#x %s", slot, word, object.ClassName))
			}
		}
	}
	return strings.Join(annotated, " ")
}

// checkAOTType answers the guest's instanceof and checkcast primitive.
//
// Answering it unconditionally true is not a harmless simplification: the guest
// compiles `if (x instanceof T) ((T)x).m()` into a check followed by a virtual
// call that takes T's method-table index and applies it to x's own vtable. A
// wrong yes therefore does not fail as a cast error — it dispatches through a
// different class's slot, landing on a method with an unrelated signature that
// reads arguments the call site never pushed.
//
// A wrong no would break working games just as badly, so a negative answer is
// given only when the whole hierarchy is known. Anything undecidable — an
// interface target, an unregistered class — keeps the permissive answer and
// records why.
func (runtime *initializationRuntime) checkAOTType(thread *armcore.Thread) (uint32, error) {
	classAddress, err := thread.Register(0)
	if err != nil {
		return 0, err
	}
	objectAddress, err := thread.Register(1)
	if err != nil {
		return 0, err
	}
	if objectAddress == 0 {
		// Guest checkcast guards null before reaching here, so this is
		// instanceof, which is false for null.
		return 0, nil
	}
	target, err := runtime.resolveAOTClass(classAddress)
	if err != nil {
		// This is the permissive answer the comment above warns about, so the
		// call site goes in the event: knowing a wrong yes was given is only
		// half of it, and the other half is which guest code was told.
		event := diagEvent{
			kind: diagCheckUnresolved,
			name: runtime.describeOperandClass(objectAddress),
			nums: [5]uint32{classAddress},
		}
		if lr, lrErr := thread.Register(armcore.RegisterLR); lrErr == nil {
			event.site, event.hasSite = lr, true
		}
		runtime.recordDiagnostic(event)
		return 1, nil
	}
	object, ok := runtime.client.vm.AOTObject(objectAddress)
	if !ok || object == nil || object.ClassName == "" {
		runtime.countDiagnostic("checktype object unbound for " + target.Name)
		return 1, nil
	}
	assignable, decided := runtime.aotAssignable(object.ClassName, objectAddress, target)
	if !decided {
		runtime.recordDiagnostic(diagEvent{kind: diagCheckUndecided, name: object.ClassName, target: target.Name})
		return 1, nil
	}
	if !assignable {
		runtime.recordDiagnostic(diagEvent{kind: diagCheckReject, name: object.ClassName, target: target.Name})
		return 0, nil
	}
	return 1, nil
}

// describeOperandClass names a guest word as whichever of a bound object or a
// registered class it turns out to be, so an unexpected operand order shows up
// as a name rather than a number.
func (runtime *initializationRuntime) describeOperandClass(address uint32) string {
	if address == 0 {
		return "null"
	}
	if object, ok := runtime.client.vm.AOTObject(address); ok && object != nil {
		return "object:" + object.ClassName
	}
	if metadata, ok := runtime.client.vm.AOTClassAt(address); ok {
		return "class:" + metadata.Name
	}
	return fmt.Sprintf("%#x", address)
}

// aotAssignable reports whether a value of className may be treated as target,
// and whether that could be decided at all. The hierarchy is walked through the
// guest's own class records rather than the Host's registry, because a game
// asks about classes long before it registers them with the runtime, and an
// undecidable answer has to stay permissive — which is how a wrong yes reaches
// the guest in the first place.
func (runtime *initializationRuntime) aotAssignable(className string, objectAddress uint32, target jvm.AOTClassMetadata) (bool, bool) {
	// The same primitive backs array stores, where the common case is an exact
	// type match, so that is settled before anything else.
	if className == target.Name {
		return true, true
	}
	if target.Name == "java/lang/Object" {
		return true, true
	}
	sourceArray := strings.HasPrefix(className, "[")
	targetArray := strings.HasPrefix(target.Name, "[")
	switch {
	case sourceArray && targetArray:
		// Array covariance turns on component assignability, which the
		// metadata does not describe beyond the exact match settled above.
		return false, false
	case targetArray:
		// A plain class is never an array.
		return false, true
	case sourceArray:
		// An array is unrelated to a plain class once Object is excluded —
		// except for the two interfaces every array type implements, which no
		// class record of this platform's making declares.
		if target.Name == "java/lang/Cloneable" || target.Name == "java/io/Serializable" {
			return true, true
		}
		return false, true
	}
	if classAddress, err := runtime.aotObjectClassAddress(objectAddress); err == nil {
		if assignable, decided := runtime.aotAssignableFrom(classAddress, target); decided {
			return assignable, true
		}
	}
	// Without the object's own class record the registry is all there is, and
	// it only reaches as far as the classes already registered.
	for current, depth := className, 0; depth < maxAOTHierarchyDepth; depth++ {
		if current == target.Name {
			return true, true
		}
		if current == "java/lang/Object" || current == "" {
			return false, true
		}
		metadata, ok := runtime.client.vm.AOTClass(current)
		if !ok || metadata.SuperName == current {
			return false, false
		}
		current = metadata.SuperName
	}
	return false, false
}

// aotObjectClassAddress reads the class record a guest object points at. The
// instance layout is the fields pointer followed by the class.
func (runtime *initializationRuntime) aotObjectClassAddress(objectAddress uint32) (uint32, error) {
	if objectAddress == 0 || objectAddress&3 != 0 {
		return 0, fmt.Errorf("object address %#x is null or unaligned", objectAddress)
	}
	words, err := runtime.readAOTWords(objectAddress, 2, "object class")
	if err != nil {
		return 0, err
	}
	if words[1] == 0 {
		return 0, fmt.Errorf("object at %#x has no class", objectAddress)
	}
	return words[1], nil
}

// aotAssignableFrom walks a guest class record's superclass chain, checking the
// interfaces each level declares along the way, and reports whether the walk
// settled the question.
func (runtime *initializationRuntime) aotAssignableFrom(classAddress uint32, target jvm.AOTClassMetadata) (bool, bool) {
	for current, depth := classAddress, 0; current != 0 && depth < maxAOTHierarchyDepth; depth++ {
		summary, err := runtime.aotClassSummary(current)
		if err != nil {
			return false, false
		}
		if summary.name == target.Name {
			return true, true
		}
		// 0x0200 is ACC_INTERFACE. Only an interface target can be reached
		// through the implements lists rather than the superclass chain.
		if target.AccessFlags&0x0200 != 0 {
			implements, decided := runtime.aotImplements(summary.interfaces, target.Name, 0)
			if !decided {
				return false, false
			}
			if implements {
				return true, true
			}
		}
		current = summary.parent
	}
	if classAddress == 0 {
		return false, false
	}
	// The chain ran out without a match, which is a definite no.
	return false, true
}

// aotImplements reports whether any of the interface records, or the interfaces
// those extend, is the named one.
func (runtime *initializationRuntime) aotImplements(interfaces []uint32, name string, depth int) (bool, bool) {
	if depth >= maxAOTHierarchyDepth {
		return false, false
	}
	for _, address := range interfaces {
		summary, err := runtime.aotClassSummary(address)
		if err != nil {
			return false, false
		}
		if summary.name == name {
			return true, true
		}
		extends, decided := runtime.aotImplements(summary.interfaces, name, depth+1)
		if !decided {
			return false, false
		}
		if extends {
			return true, true
		}
	}
	return false, true
}

// aotClassSummary is the part of a guest class record the type check needs:
// its name, its superclass record, and the interfaces it declares. Class
// records never change once the guest has built them, so summaries are kept.
type aotClassSummary struct {
	name       string
	parent     uint32
	interfaces []uint32
}

func (runtime *initializationRuntime) aotClassSummary(address uint32) (aotClassSummary, error) {
	if summary, ok := runtime.classSummaries[address]; ok {
		return summary, nil
	}
	if address&3 != 0 {
		return aotClassSummary{}, fmt.Errorf("class address %#x is not word-aligned", address)
	}
	class, err := runtime.readAOTBytes(address, javaClassSize, "class")
	if err != nil {
		return aotClassSummary{}, err
	}
	if next := binary.LittleEndian.Uint32(class[0:]); next != address+4 {
		return aotClassSummary{}, fmt.Errorf("class next pointer %#x does not identify class %#x", next, address)
	}
	descriptorAddress := binary.LittleEndian.Uint32(class[8:])
	if descriptorAddress&3 != 0 {
		return aotClassSummary{}, fmt.Errorf("class descriptor address %#x is not word-aligned", descriptorAddress)
	}
	descriptor, err := runtime.readAOTBytes(descriptorAddress, javaDescriptorSize, "class descriptor")
	if err != nil {
		return aotClassSummary{}, err
	}
	name, err := runtime.readAOTName(binary.LittleEndian.Uint32(descriptor[0:]), "class name")
	if err != nil {
		return aotClassSummary{}, err
	}
	summary := aotClassSummary{name: name, parent: binary.LittleEndian.Uint32(descriptor[8:])}
	// The implements list is a plain pointer table whose length the descriptor
	// carries beside it; it has no terminator to walk to.
	if table := binary.LittleEndian.Uint32(descriptor[16:]); table != 0 {
		count := binary.LittleEndian.Uint16(descriptor[30:])
		if count > maxAOTInterfaces {
			return aotClassSummary{}, fmt.Errorf("class %s declares %d interfaces", name, count)
		}
		if count > 0 {
			summary.interfaces, err = runtime.readAOTWords(table, uint32(count), "interface table")
			if err != nil {
				return aotClassSummary{}, err
			}
		}
	}
	if runtime.classSummaries == nil {
		runtime.classSummaries = make(map[uint32]aotClassSummary)
	}
	runtime.classSummaries[address] = summary
	return summary, nil
}

// aotMethodFromGuestRecords resolves a method by walking the guest's own class
// records instead of the Host's registry.
//
// The registry only holds classes the title has registered, and a title
// registers a class when it first loads it — long after it has been using
// instances of it. A card whose superclass is not registered yet therefore has
// no chain to walk, and an inherited method the platform itself implements
// (keyNotify, say) is reported missing on a class that plainly has one. The
// guest's records are always there, because the class it is calling had to be
// built to be called at all.
func (runtime *initializationRuntime) aotMethodFromGuestRecords(classAddress uint32, name, descriptor string) (jvm.AOTMethodMetadata, bool, error) {
	for depth := 0; depth < maxAOTHierarchyDepth && classAddress != 0; depth++ {
		if classAddress&3 != 0 {
			return jvm.AOTMethodMetadata{}, false, fmt.Errorf("class address %#x is not word-aligned", classAddress)
		}
		class, err := runtime.readAOTBytes(classAddress, javaClassSize, "class")
		if err != nil {
			return jvm.AOTMethodMetadata{}, false, err
		}
		descriptorRecord, err := runtime.readAOTBytes(binary.LittleEndian.Uint32(class[8:]), javaDescriptorSize, "class descriptor")
		if err != nil {
			return jvm.AOTMethodMetadata{}, false, err
		}
		methodPointers, err := runtime.readAOTPointerTable(binary.LittleEndian.Uint32(descriptorRecord[12:]), "method table")
		if err != nil {
			return jvm.AOTMethodMetadata{}, false, err
		}
		for _, pointer := range methodPointers {
			method, _, err := runtime.readAOTMethod(pointer)
			if err != nil {
				return jvm.AOTMethodMetadata{}, false, err
			}
			if method.Name == name && method.Descriptor == descriptor {
				return method, true, nil
			}
		}
		classAddress = binary.LittleEndian.Uint32(descriptorRecord[8:])
	}
	return jvm.AOTMethodMetadata{}, false, nil
}

// maxAOTInterfaces bounds the declared implements list so a corrupt descriptor
// cannot ask for an unbounded read.
const maxAOTInterfaces = 64

// maxAOTHierarchyDepth bounds the superclass walk so guest metadata cannot spin
// the check.
const maxAOTHierarchyDepth = 64

func (runtime *initializationRuntime) throwAOTException(thread *armcore.Thread) (uint32, error) {
	nameAddress, err := thread.Register(0)
	if err != nil {
		return 0, err
	}
	name, err := runtime.readAOTName(nameAddress, "Java exception class name")
	if err != nil {
		return 0, err
	}
	if _, err := jvm.ParseFieldDescriptor("L" + name + ";"); err != nil {
		return 0, fmt.Errorf("invalid KTF Java exception class %q: %w", name, err)
	}
	// The guest's AOT code compiles its own bounds and cast checks, so a throw
	// arriving here is usually the game reacting to something the runtime
	// handed it. The site is the only way back to which access that was, so it
	// travels with the exception as well as into the diagnostics.
	site := ""
	if lr, lrErr := thread.Register(armcore.RegisterLR); lrErr == nil {
		registers := make([]uint32, 8)
		for index := range registers {
			registers[index], _ = thread.Register(index)
		}
		stackPointer, _ := thread.Register(armcore.RegisterSP)
		// A frame here spans tens of bytes — the throwing method's own saved
		// link register sits past the first sixteen words — so the window has
		// to be wide enough to reach the caller rather than stopping inside
		// the callee. It also has to stop at the end of the stack mapping: a
		// shallow guest stack leaves fewer words above SP than the window
		// wants, and asking for them all reads nothing at all.
		stack := runtime.readStackWindow(stackPointer, throwStackWords)
		site = fmt.Sprintf("thrown by guest code at %#x sp=%#x regs=%#x stack=%#x", lr, stackPointer, registers, stack)
		runtime.countDiagnostic(fmt.Sprintf("throw %s @%#x sp=%#x regs=%#x stack=%#x", name, lr, stackPointer, registers, stack))
		// The guest compiles its own bounds check against the length word this
		// runtime wrote into the array header, so an out-of-range throw is
		// first of all a question about that word. Naming every array the
		// throwing frame had in hand, with the length the runtime believes and
		// the length the guest can actually read, answers it directly.
		if name == "java/lang/ArrayIndexOutOfBoundsException" {
			operands := runtime.describeAOTOperands(append(append([]uint32{}, registers...), stack...))
			if operands != "" {
				site += " arrays=" + operands
				runtime.countDiagnostic("throw arrays " + operands)
			}
		}
		// Guest frames are bare addresses until the AOT metadata names them,
		// and a crash is only actionable once it says which Java methods were
		// on the stack.
		if frames := runtime.symbolizeAOTStack(stack); frames != "" {
			site += " frames=" + frames
			runtime.countDiagnostic("throw frames " + frames)
		}
	}
	classAddress, err := runtime.ensureJavaClass(name)
	if err != nil {
		return 0, fmt.Errorf("resolve KTF Java exception class %s: %w", name, err)
	}
	metadata, ok := runtime.client.vm.AOTClassAt(classAddress)
	if !ok {
		return 0, fmt.Errorf("KTF Java exception class %s at %#x is not registered", name, classAddress)
	}
	object, err := runtime.client.vm.NewAOTInstance(classAddress)
	if err != nil {
		return 0, fmt.Errorf("create KTF Java exception %s: %w", name, err)
	}
	address, err := runtime.allocateAOTObject(metadata, make([]byte, metadata.InstanceSize), object)
	if err != nil {
		return 0, fmt.Errorf("allocate KTF Java exception %s: %w", name, err)
	}
	runtime.callbacks.Allocations++

	unwind, err := runtime.findAOTExceptionHandler(thread, name, address)
	if err != nil {
		return 0, err
	}
	if unwind == nil {
		return 0, &UncaughtAOTException{
			Address:   address,
			Exception: &jvm.GuestException{Object: object, Message: site},
		}
	}
	return 0, unwind
}

// throwAOTExceptionObject is the callbacks table's slot 2: the guest hands over
// an exception it built itself and asks for it to be thrown. It is what a
// title's own `throw e` compiles to, and leaving the slot at zero is what a
// title reads as a method pointer of zero and calls — the failure it produced
// was "jump target is null" in the middle of the title's own error handling,
// with nothing to say the platform had refused a throw.
//
// The class of the throw is the object's own, so nothing is created here and
// nothing is looked up by name; the handler search and the uncaught path are
// the ones every other throw takes.
func (runtime *initializationRuntime) throwAOTExceptionObject(thread *armcore.Thread) (uint32, error) {
	address, err := thread.Register(0)
	if err != nil {
		return 0, err
	}
	if address == 0 {
		// Throwing a null is a NullPointerException, which is what the
		// language says and what a title that lost track of what it caught
		// depends on.
		return 0, runtime.raiseGuestException(thread, &jvm.GuestException{
			Object:  &jvm.Object{ClassName: "java/lang/NullPointerException", Native: "throw of a null exception"},
			Message: "throw of a null exception",
		})
	}
	object, bound := runtime.client.vm.AOTObject(address)
	if !bound || object == nil {
		second, _ := thread.Register(1)
		return 0, fmt.Errorf("KTF Java throw of %s, which is not bound to a JVM object (second word %s)%s",
			runtime.describeGuestWord(address), runtime.describeGuestWord(second), runtime.callerSite(thread))
	}
	name := object.ClassName
	runtime.countDiagnostic("throw object " + name + runtime.callerMark(thread))
	unwind, err := runtime.findAOTExceptionHandler(thread, name, address)
	if err != nil {
		return 0, err
	}
	if unwind == nil {
		site := ""
		if lr, lrErr := thread.Register(armcore.RegisterLR); lrErr == nil {
			site = fmt.Sprintf("thrown by guest code at %#x", lr)
		}
		return 0, &UncaughtAOTException{
			Address:   address,
			Exception: &jvm.GuestException{Object: object, Message: site},
		}
	}
	return 0, unwind
}

// raiseGuestException transports a JVM-raised exception into the guest's raw
// handler chain, so guest try/catch regions observe runtime Java failures the
// same way they observe their own throws.
func (runtime *initializationRuntime) raiseGuestException(thread *armcore.Thread, guest *jvm.GuestException) error {
	if guest == nil || guest.Object == nil || guest.Object.ClassName == "" {
		return fmt.Errorf("KTF guest exception has no object")
	}
	name := guest.Object.ClassName
	runtime.countDiagnostic("raise " + name)
	classAddress, err := runtime.ensureJavaClass(name)
	if err != nil {
		return fmt.Errorf("resolve KTF exception class %s: %w", name, err)
	}
	metadata, ok := runtime.client.vm.AOTClassAt(classAddress)
	if !ok {
		return fmt.Errorf("KTF exception class %s at %#x is not registered", name, classAddress)
	}
	address, ok := runtime.client.vm.AOTAddress(guest.Object)
	if !ok {
		address, err = runtime.allocateAOTObject(metadata, make([]byte, metadata.InstanceSize), guest.Object)
		if err != nil {
			return fmt.Errorf("allocate KTF exception %s: %w", name, err)
		}
	}
	unwind, err := runtime.findAOTExceptionHandler(thread, name, address)
	if err != nil {
		return err
	}
	if unwind == nil {
		return &UncaughtAOTException{Address: address, Exception: guest}
	}
	return unwind
}

func (runtime *initializationRuntime) findAOTExceptionHandler(thread *armcore.Thread, exceptionClass string, exceptionAddress uint32) (*aotExceptionUnwind, error) {
	if runtime.exceptionContext == 0 {
		return nil, fmt.Errorf("KTF JVM exception context is not prepared")
	}
	headAddress := runtime.exceptionContext + javaExceptionHead
	handlerAddress, err := runtime.client.core.ThreadLocalWord(thread, headAddress)
	if err != nil {
		return nil, fmt.Errorf("read KTF Java exception handler head: %w", err)
	}
	visited := make(map[uint32]bool)
	for depth := 0; handlerAddress != 0; depth++ {
		if depth >= maxExceptionHandlers {
			return nil, fmt.Errorf("KTF Java exception handler chain exceeds %d", maxExceptionHandlers)
		}
		if handlerAddress&3 != 0 {
			return nil, fmt.Errorf("KTF Java exception handler address %#x is not word-aligned", handlerAddress)
		}
		if visited[handlerAddress] {
			return nil, fmt.Errorf("KTF Java exception handler chain cycles at %#x", handlerAddress)
		}
		visited[handlerAddress] = true

		handler, err := runtime.readAOTBytes(handlerAddress, javaExceptionHandler, "Java exception handler")
		if err != nil {
			return nil, err
		}
		methodAddress := binary.LittleEndian.Uint32(handler[0:])
		oldHandler := binary.LittleEndian.Uint32(handler[8:])
		currentPC := binary.LittleEndian.Uint32(handler[12:])
		functionsAddress := binary.LittleEndian.Uint32(handler[20:])
		method, err := runtime.readAOTBytes(methodAddress, javaMethodSize, "Java exception method")
		if err != nil {
			return nil, err
		}
		entryCount := uint32(binary.LittleEndian.Uint16(method[16:]))
		entryPointers, err := runtime.readAOTWords(binary.LittleEndian.Uint32(method[8:]), entryCount, "Java exception table")
		if err != nil {
			return nil, err
		}
		for _, entryAddress := range entryPointers {
			if entryAddress&3 != 0 {
				return nil, fmt.Errorf("KTF Java exception entry address %#x is not word-aligned", entryAddress)
			}
			entry, err := runtime.readAOTBytes(entryAddress, javaExceptionEntry, "Java exception entry")
			if err != nil {
				return nil, err
			}
			fromPC := binary.LittleEndian.Uint32(entry[0:])
			toPC := binary.LittleEndian.Uint32(entry[4:])
			target := binary.LittleEndian.Uint32(entry[8:])
			catchAddress := binary.LittleEndian.Uint32(entry[12:])
			if fromPC >= toPC {
				return nil, fmt.Errorf("KTF Java exception range [%#x, %#x) is invalid", fromPC, toPC)
			}
			if currentPC < fromPC || currentPC >= toPC {
				continue
			}
			matches, err := runtime.aotExceptionMatches(exceptionClass, catchAddress)
			if err != nil {
				return nil, err
			}
			if !matches {
				continue
			}
			if target == 0 {
				return nil, fmt.Errorf("KTF Java exception handler target is null")
			}
			functions, err := runtime.readAOTWords(functionsAddress, 2, "Java exception restore functions")
			if err != nil {
				return nil, err
			}
			nextPC := functions[1]
			if nextPC == 0 {
				return nil, fmt.Errorf("KTF Java exception restore function is null")
			}
			probe := armcore.NewContext()
			if err := probe.SetPC(nextPC); err != nil {
				return nil, fmt.Errorf("KTF Java exception restore function %#x: %w", nextPC, err)
			}
			if err := runtime.client.core.SetThreadLocalWord(thread, headAddress, handlerAddress); err != nil {
				return nil, fmt.Errorf("write KTF Java exception handler head: %w", err)
			}
			// The handler record is where the catch block reads what it
			// caught. It lives on the guest stack, so the word is whatever
			// the frame underneath left there until a throw writes it: one
			// title's `catch (e) { close(); throw e; }` re-threw a Thumb code
			// address that had been on the stack, and the platform could only
			// report that the word it was handed was not an object.
			var caught [4]byte
			binary.LittleEndian.PutUint32(caught[:], exceptionAddress)
			if err := runtime.client.core.Memory().Write(handlerAddress+javaExceptionObject, caught[:]); err != nil {
				return nil, fmt.Errorf("write KTF Java caught exception at %#x: %w", handlerAddress+javaExceptionObject, err)
			}
			return &aotExceptionUnwind{
				contextBase: handlerAddress + javaExceptionContext,
				target:      target,
				nextPC:      nextPC,
			}, nil
		}
		handlerAddress = oldHandler
	}
	return nil, nil
}

func (runtime *initializationRuntime) aotExceptionMatches(exceptionClass string, catchAddress uint32) (bool, error) {
	if catchAddress == 0 {
		return true, nil
	}
	catchClass, err := runtime.resolveAOTClass(catchAddress)
	if err != nil {
		return false, fmt.Errorf("resolve KTF Java exception catch class at %#x: %w", catchAddress, err)
	}
	matches, err := runtime.client.vm.IsSubclassOf(exceptionClass, catchClass.Name)
	if err != nil {
		return false, fmt.Errorf("match KTF Java exception %s against %s: %w", exceptionClass, catchClass.Name, err)
	}
	return matches, nil
}

func (runtime *initializationRuntime) getAOTMethod(thread *armcore.Thread) (uint32, error) {
	classAddress, err := thread.Register(0)
	if err != nil {
		return 0, err
	}
	nameAddress, err := thread.Register(1)
	if err != nil {
		return 0, err
	}
	name, descriptor, err := runtime.readAOTFullName(nameAddress)
	if err != nil {
		return 0, err
	}
	runtime.countDiagnostic("getmethod " + name + descriptor)
	first, err := runtime.readAOTWords(classAddress, 1, "class or vtable reference")
	if err != nil {
		return 0, err
	}
	if first[0] != classAddress+4 {
		methodPointers, err := runtime.readAOTPointerTable(first[0], "vtable method table")
		if err != nil {
			return 0, err
		}
		for _, pointer := range methodPointers {
			method, _, err := runtime.readAOTMethod(pointer)
			if err != nil {
				return 0, err
			}
			if method.Name == name && method.Descriptor == descriptor {
				return method.Address, nil
			}
		}
		return 0, fmt.Errorf("KTF AOT method %s%s was not found in vtable %#x", name, descriptor, first[0])
	}
	method, ok, err := runtime.client.vm.FindAOTMethod(classAddress, name, descriptor)
	if err != nil {
		return 0, err
	}
	if !ok {
		return 0, fmt.Errorf("KTF AOT method %s%s was not found from %s", name, descriptor, runtime.describeAOTClassAt(classAddress))
	}
	return method.Address, nil
}

func (runtime *initializationRuntime) getAOTField(thread *armcore.Thread) (uint32, error) {
	classAddress, err := thread.Register(0)
	if err != nil {
		return 0, err
	}
	nameAddress, err := thread.Register(1)
	if err != nil {
		return 0, err
	}
	name, descriptor, err := runtime.readAOTFullName(nameAddress)
	if err != nil {
		return 0, err
	}
	runtime.countDiagnostic("getfield " + name + ":" + descriptor)
	field, ok, err := runtime.client.vm.FindAOTField(classAddress, name, descriptor)
	if err != nil {
		return 0, err
	}
	if !ok {
		return 0, fmt.Errorf("KTF AOT field %s:%s was not found from %s", name, descriptor, runtime.describeAOTClassAt(classAddress))
	}
	return field.Address, nil
}

// describeAOTClassAt names the class a failed lookup was made against. A record
// the registry has never heard of is still a record, and the name in it is the
// whole of what makes the miss actionable: without it the report is an address,
// and which class was being asked is exactly the question. Both lookups say it
// the same way, because a field miss is investigated the same way a method miss
// is.
func (runtime *initializationRuntime) describeAOTClassAt(address uint32) string {
	if class, ok := runtime.client.vm.AOTClassAt(address); ok {
		return fmt.Sprintf("class %s at %#x", class.Name, address)
	}
	if class, err := runtime.readAOTClass(address); err == nil {
		return fmt.Sprintf("unregistered class %s at %#x", class.Name, address)
	}
	return fmt.Sprintf("unregistered class %#x", address)
}

func (runtime *initializationRuntime) readAOTClass(address uint32) (jvm.AOTClassMetadata, error) {
	if address&3 != 0 {
		return jvm.AOTClassMetadata{}, fmt.Errorf("class address %#x is not word-aligned", address)
	}
	class, err := runtime.readAOTBytes(address, javaClassSize, "class")
	if err != nil {
		return jvm.AOTClassMetadata{}, err
	}
	if next := binary.LittleEndian.Uint32(class[0:]); next != address+4 {
		return jvm.AOTClassMetadata{}, fmt.Errorf("class next pointer %#x does not identify class %#x", next, address)
	}
	descriptorAddress := binary.LittleEndian.Uint32(class[8:])
	if descriptorAddress&3 != 0 {
		return jvm.AOTClassMetadata{}, fmt.Errorf("class descriptor address %#x is not word-aligned", descriptorAddress)
	}
	descriptor, err := runtime.readAOTBytes(descriptorAddress, javaDescriptorSize, "class descriptor")
	if err != nil {
		return jvm.AOTClassMetadata{}, err
	}
	name, err := runtime.readAOTName(binary.LittleEndian.Uint32(descriptor[0:]), "class name")
	if err != nil {
		return jvm.AOTClassMetadata{}, err
	}

	var superName string
	if parent := binary.LittleEndian.Uint32(descriptor[8:]); parent != 0 {
		superName, err = runtime.readAOTClassName(parent)
		if err != nil {
			return jvm.AOTClassMetadata{}, fmt.Errorf("parent of %s: %w", name, err)
		}
	}

	methodPointers, err := runtime.readAOTPointerTable(binary.LittleEndian.Uint32(descriptor[12:]), "method table")
	if err != nil {
		return jvm.AOTClassMetadata{}, fmt.Errorf("class %s: %w", name, err)
	}
	methods := make([]jvm.AOTMethodMetadata, 0, len(methodPointers))
	for _, pointer := range methodPointers {
		method, owner, err := runtime.readAOTMethod(pointer)
		if err != nil {
			return jvm.AOTClassMetadata{}, fmt.Errorf("class %s method at %#x: %w", name, pointer, err)
		}
		if owner == address {
			methods = append(methods, method)
		}
	}

	var fields []jvm.AOTFieldMetadata
	if !strings.HasPrefix(name, "[") {
		fieldPointers, err := runtime.readAOTPointerTable(binary.LittleEndian.Uint32(descriptor[20:]), "field table")
		if err != nil {
			return jvm.AOTClassMetadata{}, fmt.Errorf("class %s: %w", name, err)
		}
		fields = make([]jvm.AOTFieldMetadata, 0, len(fieldPointers))
		for _, pointer := range fieldPointers {
			field, owner, err := runtime.readAOTField(pointer)
			if err != nil {
				return jvm.AOTClassMetadata{}, fmt.Errorf("class %s field at %#x: %w", name, pointer, err)
			}
			if owner == address {
				fields = append(fields, field)
			}
		}
	}

	vtableCount := binary.LittleEndian.Uint16(class[16:])
	vtable, err := runtime.readAOTWords(binary.LittleEndian.Uint32(class[12:]), uint32(vtableCount), "vtable")
	if err != nil {
		return jvm.AOTClassMetadata{}, fmt.Errorf("class %s: %w", name, err)
	}
	return jvm.AOTClassMetadata{
		Address:       address,
		Name:          name,
		SuperName:     superName,
		AccessFlags:   binary.LittleEndian.Uint16(descriptor[28:]),
		InstanceSize:  binary.LittleEndian.Uint16(descriptor[26:]),
		VTableAddress: binary.LittleEndian.Uint32(class[12:]),
		VTable:        vtable,
		Methods:       methods,
		Fields:        fields,
	}, nil
}

func (runtime *initializationRuntime) readAOTClassName(address uint32) (string, error) {
	if address&3 != 0 {
		return "", fmt.Errorf("parent class address %#x is not word-aligned", address)
	}
	class, err := runtime.readAOTBytes(address, javaClassSize, "parent class")
	if err != nil {
		return "", err
	}
	if next := binary.LittleEndian.Uint32(class[0:]); next != address+4 {
		return "", fmt.Errorf("parent class next pointer %#x does not identify class %#x", next, address)
	}
	descriptorAddress := binary.LittleEndian.Uint32(class[8:])
	if descriptorAddress&3 != 0 {
		return "", fmt.Errorf("parent class descriptor address %#x is not word-aligned", descriptorAddress)
	}
	descriptor, err := runtime.readAOTBytes(descriptorAddress, javaDescriptorSize, "parent class descriptor")
	if err != nil {
		return "", err
	}
	return runtime.readAOTName(binary.LittleEndian.Uint32(descriptor[0:]), "parent class name")
}

func (runtime *initializationRuntime) readAOTMethod(address uint32) (jvm.AOTMethodMetadata, uint32, error) {
	if address&3 != 0 {
		return jvm.AOTMethodMetadata{}, 0, fmt.Errorf("method address %#x is not word-aligned", address)
	}
	method, err := runtime.readAOTBytes(address, javaMethodSize, "method")
	if err != nil {
		return jvm.AOTMethodMetadata{}, 0, err
	}
	name, descriptor, err := runtime.readAOTFullName(binary.LittleEndian.Uint32(method[12:]))
	if err != nil {
		return jvm.AOTMethodMetadata{}, 0, err
	}
	return jvm.AOTMethodMetadata{
		Address:          address,
		Name:             name,
		Descriptor:       descriptor,
		Body:             binary.LittleEndian.Uint32(method[0:]),
		NativeBody:       binary.LittleEndian.Uint32(method[8:]),
		ExceptionEntries: binary.LittleEndian.Uint16(method[16:]),
		VTableIndex:      binary.LittleEndian.Uint16(method[20:]),
		AccessFlags:      binary.LittleEndian.Uint16(method[22:]),
	}, binary.LittleEndian.Uint32(method[4:]), nil
}

func (runtime *initializationRuntime) readAOTField(address uint32) (jvm.AOTFieldMetadata, uint32, error) {
	if address&3 != 0 {
		return jvm.AOTFieldMetadata{}, 0, fmt.Errorf("field address %#x is not word-aligned", address)
	}
	field, err := runtime.readAOTBytes(address, javaFieldSize, "field")
	if err != nil {
		return jvm.AOTFieldMetadata{}, 0, err
	}
	name, descriptor, err := runtime.readAOTFullName(binary.LittleEndian.Uint32(field[8:]))
	if err != nil {
		return jvm.AOTFieldMetadata{}, 0, err
	}
	return jvm.AOTFieldMetadata{
		Address:     address,
		Name:        name,
		Descriptor:  descriptor,
		Offset:      binary.LittleEndian.Uint32(field[12:]),
		AccessFlags: binary.LittleEndian.Uint32(field[0:]),
	}, binary.LittleEndian.Uint32(field[4:]), nil
}

func (runtime *initializationRuntime) readAOTFullName(address uint32) (string, string, error) {
	if address == ^uint32(0) {
		return "", "", fmt.Errorf("Java full-name address overflows")
	}
	if _, err := runtime.readAOTBytes(address, 1, "Java full-name tag"); err != nil {
		return "", "", err
	}
	value, err := runtime.readCString(address+1, maxAOTNameBytes)
	if err != nil {
		return "", "", fmt.Errorf("read Java full name: %w", err)
	}
	separator := strings.IndexByte(value, '+')
	if separator <= 0 || separator == len(value)-1 {
		return "", "", fmt.Errorf("Java full name %q has no descriptor/name separator", value)
	}
	return value[separator+1:], value[:separator], nil
}

func (runtime *initializationRuntime) readAOTName(address uint32, label string) (string, error) {
	name, err := runtime.readCString(address, maxAOTNameBytes)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", label, err)
	}
	if !utf8.ValidString(name) {
		return "", fmt.Errorf("%s is not valid UTF-8", label)
	}
	return name, nil
}

func (runtime *initializationRuntime) readAOTPointerTable(address uint32, label string) ([]uint32, error) {
	if address == 0 {
		return nil, nil
	}
	if address&3 != 0 {
		return nil, fmt.Errorf("%s address %#x is not word-aligned", label, address)
	}
	result := make([]uint32, 0, 16)
	for index := uint32(0); index < maxAOTMemberPointers; index++ {
		current := uint64(address) + uint64(index)*4
		if current+4 > uint64(1)<<32 {
			return nil, fmt.Errorf("%s address overflows guest memory", label)
		}
		word, err := runtime.readAOTWords(uint32(current), 1, label)
		if err != nil {
			return nil, err
		}
		if word[0] == 0 {
			return result, nil
		}
		result = append(result, word[0])
	}
	return nil, fmt.Errorf("%s exceeds %d entries", label, maxAOTMemberPointers)
}

func (runtime *initializationRuntime) readAOTWords(address, count uint32, label string) ([]uint32, error) {
	if count == 0 {
		return nil, nil
	}
	if address == 0 || address&3 != 0 {
		return nil, fmt.Errorf("%s address %#x is null or unaligned", label, address)
	}
	data, err := runtime.readAOTBytes(address, uint64(count)*4, label)
	if err != nil {
		return nil, err
	}
	words := make([]uint32, count)
	for index := range words {
		words[index] = binary.LittleEndian.Uint32(data[index*4:])
	}
	return words, nil
}

func (runtime *initializationRuntime) readAOTBytes(address uint32, size uint64, label string) ([]byte, error) {
	if address == 0 || size == 0 || size > maxPlatformAllocation {
		return nil, fmt.Errorf("%s range at %#x has invalid size %d", label, address, size)
	}
	if uint64(address)+size > uint64(1)<<32 {
		return nil, fmt.Errorf("%s range at %#x overflows guest memory", label, address)
	}
	data := make([]byte, int(size))
	if err := runtime.client.core.Memory().Read(address, data); err != nil {
		return nil, fmt.Errorf("read %s at %#x: %w", label, address, err)
	}
	return data, nil
}
