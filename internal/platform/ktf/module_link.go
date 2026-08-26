package ktf

import (
	"context"
	"encoding/binary"
	"fmt"
	"unicode/utf16"

	"github.com/movingwoo/wfeature/internal/armcore"
	"github.com/movingwoo/wfeature/internal/jvm"
)

// An older relocatable module carries the same Java metadata the current
// generation does — twenty-byte class records, twenty-eight-byte method
// records, sixteen-byte field records, the same object layout and the same
// virtual-dispatch arithmetic — and it arrives in the same unlinked state:
// a class descriptor's parent is a reference cell rather than a pointer, and
// its vtable word is zero.
//
// What differs is who links it. The current generation's guest resolves its
// own names through the initialization callbacks and hands each finished class
// back through `registerClass`; a module has no such callback and publishes a
// descriptor for the platform to read instead. `seg[0]` points at it:
//
//	desc[0]  the class table, one pointer per bucket, null where empty
//	desc[1]  how many of those buckets are filled
//	desc[2]  how many buckets there are, a power of two
//	desc[3]  a thunk
//	desc[4]  the same thunk
//	desc[5]  the descriptor's own address, which is what identifies it
//
// Nothing in a module's own code reads that descriptor — the only two words
// that point at it are `seg[0]` and its own self-reference — so it exists for
// this side to read, and linking the records is this side's job.
const (
	moduleDescriptorWords = 6
	// moduleDescriptorSelf is the word that holds the descriptor's own
	// address. Three modules agree on it and it is what tells a descriptor
	// apart from any other six words.
	moduleDescriptorSelf = 5
	// maxModuleClasses bounds a table read out of guest data.
	maxModuleClasses = 4096
	// moduleNameTableOffset is the segment header word that points at the
	// module's own name table, which every reference cell indexes.
	moduleNameTableOffset = 4 * 4
	// moduleUnresolved is the low bit a reference cell carries while it still
	// names an index rather than an address. The module's own resolvers test
	// exactly this bit before following the word.
	moduleUnresolved = 1
	// javaDescriptorElement is where a class descriptor names the class of an
	// array's elements. See "The array store check was asked about a null
	// class" in docs/ktf.md for what reads it.
	javaDescriptorElement = 0x14
)

// resolveModuleDescriptorCell replaces a reference cell in a class descriptor
// with the class it names. A word that is already a pointer is left alone,
// which is what makes this safe to call twice.
func (runtime *initializationRuntime) resolveModuleDescriptorCell(address uint32, depth int) error {
	cell, err := runtime.readWord(address)
	if err != nil {
		return err
	}
	if cell&moduleUnresolved == 0 {
		return nil
	}
	name, err := runtime.moduleName(cell)
	if err != nil {
		return err
	}
	resolved, err := runtime.resolveModuleClass(name, depth+1)
	if err != nil {
		return fmt.Errorf("%q: %w", name, err)
	}
	return runtime.writeWord(address, resolved)
}

// moduleDescriptor is the class table a module publishes for the platform.
type moduleDescriptor struct {
	address    uint32
	classTable uint32
	classCount uint32
	tableSize  uint32
}

func (runtime *initializationRuntime) readModuleDescriptor() (moduleDescriptor, error) {
	segment := runtime.client.moduleSegment
	if segment == 0 {
		return moduleDescriptor{}, fmt.Errorf("KTF client is not a relocatable module")
	}
	address, err := runtime.readWord(segment)
	if err != nil {
		return moduleDescriptor{}, fmt.Errorf("read KTF module descriptor pointer: %w", err)
	}
	words, err := runtime.readAOTWords(address, moduleDescriptorWords, "module descriptor")
	if err != nil {
		return moduleDescriptor{}, err
	}
	if words[moduleDescriptorSelf] != address {
		return moduleDescriptor{}, fmt.Errorf("KTF module descriptor at %#x does not name itself (%#x)", address, words[moduleDescriptorSelf])
	}
	descriptor := moduleDescriptor{address: address, classTable: words[0], classCount: words[1], tableSize: words[2]}
	if descriptor.tableSize == 0 || descriptor.tableSize > maxModuleClasses || descriptor.classCount > descriptor.tableSize {
		return moduleDescriptor{}, fmt.Errorf("KTF module descriptor names %d classes in %d buckets", descriptor.classCount, descriptor.tableSize)
	}
	return descriptor, nil
}

// moduleClasses reads the class records the descriptor's table holds, in
// bucket order. The count the descriptor declares is checked against what the
// table actually holds, because the two agreeing is what says the table was
// read correctly.
func (runtime *initializationRuntime) moduleClasses(descriptor moduleDescriptor) ([]uint32, error) {
	buckets, err := runtime.readAOTWords(descriptor.classTable, descriptor.tableSize, "module class table")
	if err != nil {
		return nil, err
	}
	classes := make([]uint32, 0, descriptor.classCount)
	for _, class := range buckets {
		if class != 0 {
			classes = append(classes, class)
		}
	}
	if uint32(len(classes)) != descriptor.classCount {
		return nil, fmt.Errorf("KTF module class table holds %d classes, descriptor says %d", len(classes), descriptor.classCount)
	}
	return classes, nil
}

// moduleName reads the name a reference cell's class half indexes. The cell's
// low halfword is the index shifted up by one with the unresolved bit in its
// place, which is the same decode the module's own resolvers do:
// `(word >> 1) * 4` into the table `seg[4]` points at.
func (runtime *initializationRuntime) moduleName(cell uint32) (string, error) {
	table, err := runtime.readWord(runtime.client.moduleSegment + moduleNameTableOffset)
	if err != nil {
		return "", fmt.Errorf("read KTF module name table: %w", err)
	}
	pointer, err := runtime.readWord(table + (cell&0xffff)>>1*4)
	if err != nil {
		return "", fmt.Errorf("read KTF module name %#x: %w", cell, err)
	}
	return runtime.readAOTName(pointer, "module name")
}

// linkModuleClasses turns every class the module publishes into a record this
// platform's own reader accepts, and registers it. A class is linked before
// anything the guest does, because the guest reaches its superclass through
// the record rather than through a call.
func (runtime *initializationRuntime) linkModuleClasses() error {
	runtime.client.log("KTF module classes", "count", len(runtime.moduleClassByName))
	for _, class := range runtime.moduleClassByName {
		if _, err := runtime.linkModuleClass(class, 0); err != nil {
			return err
		}
	}
	return nil
}

// maxModuleLinkDepth bounds the superclass chain a link follows. A module's
// own hierarchy is shallow; the bound is against a cycle in guest data rather
// than against a real depth.
const maxModuleLinkDepth = 64

// linkModuleClass resolves a class record's superclass, builds its vtable and
// registers it. It is idempotent: the record itself is where "already linked"
// is recorded, because that is the state the guest reads too.
func (runtime *initializationRuntime) linkModuleClass(class uint32, depth int) (uint32, error) {
	if depth > maxModuleLinkDepth {
		return 0, fmt.Errorf("KTF module class %#x exceeds the superclass depth limit", class)
	}
	if runtime.linkedModuleClasses == nil {
		runtime.linkedModuleClasses = make(map[uint32]bool)
	}
	if runtime.linkedModuleClasses[class] {
		return class, nil
	}
	runtime.linkedModuleClasses[class] = true

	record, err := runtime.readAOTWords(class, javaClassSize/4, "module class record")
	if err != nil {
		return 0, err
	}
	if record[0] != class+4 {
		return 0, fmt.Errorf("KTF module class record %#x does not identify itself", class)
	}
	descriptorAddress := record[2]
	if err := runtime.resolveModuleDescriptorCell(descriptorAddress+8, depth); err != nil {
		return 0, fmt.Errorf("KTF module class %#x superclass: %w", class, err)
	}
	parent, err := runtime.readWord(descriptorAddress + 8)
	if err != nil {
		return 0, err
	}

	// An array class names its element the same way, and the guest reads it
	// straight off the descriptor when it checks an array store — so a cell
	// left there is a check asked about a number. Four of one module's
	// classes carry one.
	if err := runtime.resolveModuleDescriptorCell(descriptorAddress+javaDescriptorElement, depth); err != nil {
		return 0, fmt.Errorf("KTF module class %#x element class: %w", class, err)
	}
	if err := runtime.buildModuleVTable(class, descriptorAddress, parent); err != nil {
		return 0, err
	}
	metadata, err := runtime.readAOTClass(class)
	if err != nil {
		return 0, fmt.Errorf("read linked KTF module class %#x: %w", class, err)
	}
	if err := runtime.client.vm.RegisterAOTClass(metadata); err != nil {
		return 0, fmt.Errorf("register KTF module class %s: %w", metadata.Name, err)
	}
	runtime.classes[metadata.Name] = class
	runtime.callbacks.RegisteredClasses++
	return class, nil
}

// resolveModuleClass answers a name with a class record: one of the module's
// own if its table holds it, and this platform's otherwise. A module class is
// linked on the way, so a subclass never sees an unlinked parent.
func (runtime *initializationRuntime) resolveModuleClass(name string, depth int) (uint32, error) {
	if class, ok := runtime.moduleClassByName[name]; ok {
		return runtime.linkModuleClass(class, depth)
	}
	return runtime.ensureJavaClass(name)
}

// buildModuleVTable writes the class's virtual dispatch table. A module ships
// each method's slot number and no table, so the table is the parent's with
// the class's own methods placed at the numbers they declare — the same
// numbering the current generation's guest builds for itself, which is why a
// subclass of a platform class lands on this platform's slots.
func (runtime *initializationRuntime) buildModuleVTable(class, descriptorAddress, parent uint32) error {
	inherited, err := runtime.inheritedVTable(parent)
	if err != nil {
		return err
	}
	slots := inherited.pointers()
	methodTable, err := runtime.readWord(descriptorAddress + 12)
	if err != nil {
		return err
	}
	pointers, err := runtime.readAOTPointerTable(methodTable, "module method table")
	if err != nil {
		return err
	}
	for _, pointer := range pointers {
		method, owner, err := runtime.readAOTMethod(pointer)
		if err != nil {
			return fmt.Errorf("read KTF module method at %#x: %w", pointer, err)
		}
		// A static method has no receiver to dispatch on, and the slot it
		// would claim belongs to whatever the hierarchy already put there.
		if owner != class || method.AccessFlags&0x0008 != 0 {
			continue
		}
		index := int(method.VTableIndex)
		if index >= maxAOTMemberPointers {
			return fmt.Errorf("KTF module method %s claims vtable slot %d", method.Name, index)
		}
		for len(slots) <= index {
			slots = append(slots, 0)
		}
		slots[index] = pointer
	}
	if len(slots) == 0 {
		return nil
	}
	address, err := runtime.allocateWords(slots)
	if err != nil {
		return err
	}
	// Only the two words the file leaves empty are written. The halfword
	// after the vtable count is a flag field the file fills in itself — one
	// local module carries 0x8000 there where a current-generation image
	// carries 8 — and the bit the guest tests before it asks for a class
	// initializer lives in it, so overwriting it would tell every class it
	// had already been initialized.
	var tail [6]byte
	binary.LittleEndian.PutUint32(tail[0:], address)
	binary.LittleEndian.PutUint16(tail[4:], uint16(len(slots)))
	if err := runtime.client.core.Memory().Write(class+12, tail[:]); err != nil {
		return fmt.Errorf("write KTF module vtable for class %#x: %w", class, err)
	}
	return nil
}

// The module's runtime glue reaches everything it needs through `fp`, which
// this side has to build and hold: the current generation is handed the same
// things as initialization parameters instead. Only five words of the block
// are ever read, and the module's own helpers are where each one is named —
// they save the caller's stack pointer, switch to a stack of their own, walk a
// chain of handler records, and find a class record from an object header.
const (
	moduleContextSize = 0x40
	// moduleContextStackSave is where a helper parks the caller's stack
	// pointer while it runs on the runtime stack.
	moduleContextStackSave = 0x24
	// moduleContextHandlers heads the chain of sixty-four-byte handler
	// records a protected region pushes. It is this generation's place for
	// what the current one keeps at javaExceptionHead.
	moduleContextHandlers = 0x2c
	// moduleContextFrame is copied into every handler record it pushes.
	moduleContextFrame = 0x30
	// moduleContextStack is the stack the runtime glue switches to before it
	// calls anything of its own, so a helper never runs on the guest frame
	// that called it.
	moduleContextStack = 0x34
	// moduleContextClasses is the base a virtual call adds the object
	// header's shifted alias to, which is the JVM context by another name:
	// `[[obj] >> 5] + [fp + 0x38]`, then the vtable at `+0xc`.
	moduleContextClasses = 0x38
	// moduleRuntimeStackSize is the stack the glue runs its own helpers on.
	moduleRuntimeStackSize = 32 << 10
	// moduleContextSlotOffset is the segment header word that holds the
	// context for code that has no `fp` to hand. It carries the loader's
	// marker constant until this side writes the block's address over it.
	moduleContextSlotOffset = 0x20
)

// prepareModuleContext builds the block `fp` points at and installs it on the
// client thread, where every call into the module inherits it.
func (runtime *initializationRuntime) prepareModuleContext() (uint32, error) {
	block := make([]byte, moduleContextSize)
	address, err := runtime.allocateBytes(block)
	if err != nil {
		return 0, err
	}
	stack, err := runtime.allocate(moduleRuntimeStackSize)
	if err != nil {
		return 0, err
	}
	if err := runtime.writeWord(address+moduleContextStack, stack+moduleRuntimeStackSize); err != nil {
		return 0, err
	}
	if err := runtime.writeWord(address+moduleContextClasses, runtime.jvmContext); err != nil {
		return 0, err
	}
	// Every handler record a protected region pushes copies this word, and
	// what the platform reads back out of the record is the restore routine
	// the catch block resumes through. A module publishes no such table, so
	// this side writes one over a routine of its own making.
	restore, err := runtime.loadModuleCode(moduleRestoreStub())
	if err != nil {
		return 0, err
	}
	functions, err := runtime.allocateWords([]uint32{0, restore})
	if err != nil {
		return 0, err
	}
	if err := runtime.writeWord(address+moduleContextFrame, functions); err != nil {
		return 0, err
	}
	if err := runtime.client.thread.SetRegister(armcore.RegisterFP, address); err != nil {
		return 0, fmt.Errorf("install KTF module context: %w", err)
	}
	// The same block has to be reachable without a register, because compiled
	// code restores the handler chain head through a global rather than
	// through `fp`: `[[seg + 0x20] + 0x2c] = head`. That word is one of the
	// two the segment header carries the module's own constant in — the pair
	// the loader reads as markers — and the only one anything points at.
	if err := runtime.writeWord(runtime.client.moduleSegment+moduleContextSlotOffset, address); err != nil {
		return 0, err
	}
	runtime.moduleContext = address
	if err := runtime.registerModuleThreadWords(); err != nil {
		return 0, err
	}
	return address, nil
}

// loadModuleCode places a Thumb routine of this platform's own in the stub
// region and answers its Thumb address.
func (runtime *initializationRuntime) loadModuleCode(code []byte) (uint32, error) {
	size := uint64(len(code)+3) &^ 3
	if runtime.codeCursor+size > uint64(platformCodeBase)+platformCodeSize {
		return 0, fmt.Errorf("KTF platform callback stub space exhausted")
	}
	address := uint32(runtime.codeCursor)
	if err := runtime.client.core.Memory().Load(address, code); err != nil {
		return 0, fmt.Errorf("load KTF module routine: %w", err)
	}
	runtime.codeCursor += size
	return address | 1, nil
}

func (runtime *initializationRuntime) readWord(address uint32) (uint32, error) {
	words, err := runtime.readAOTWords(address, 1, "module word")
	if err != nil {
		return 0, err
	}
	return words[0], nil
}

func (runtime *initializationRuntime) writeWord(address, value uint32) error {
	var word [4]byte
	binary.LittleEndian.PutUint32(word[:], value)
	if err := runtime.client.core.Memory().Write(address, word[:]); err != nil {
		return fmt.Errorf("write KTF guest word at %#x: %w", address, err)
	}
	return nil
}

// indexModuleClasses reads the published table once and keys it by name, so
// that a superclass reference can be answered from the module before it is
// looked for on this platform.
func (runtime *initializationRuntime) indexModuleClasses() error {
	descriptor, err := runtime.readModuleDescriptor()
	if err != nil {
		return err
	}
	classes, err := runtime.moduleClasses(descriptor)
	if err != nil {
		return err
	}
	runtime.moduleClassByName = make(map[string]uint32, len(classes))
	for _, class := range classes {
		name, err := runtime.readAOTClassName(class)
		if err != nil {
			return fmt.Errorf("read KTF module class name at %#x: %w", class, err)
		}
		runtime.moduleClassByName[name] = class
	}
	return nil
}

// A module reaches six routines it does not carry by tail-jumping through a
// table its segment header names at `seg[5]`. All three local modules leave
// exactly six zero words there, followed by the same code in each, and the
// glue reads them 8, 6, 2, 2, 1 and 1 times — so the table is the platform's
// to fill, and it is filled with the same supervisor-call stubs every other
// table here uses. A tail jump leaves the caller's return address in `lr`, and
// a stub returns with `bx lr`, so the two fit without a shim.
const moduleJumpSlots = 6

func (runtime *initializationRuntime) installModuleJumps() error {
	table, err := runtime.readWord(runtime.client.moduleSegment + 5*4)
	if err != nil {
		return fmt.Errorf("read KTF module jump table pointer: %w", err)
	}
	words := make([]byte, moduleJumpSlots*4)
	for slot := 0; slot < moduleJumpSlots; slot++ {
		existing, err := runtime.readWord(table + uint32(slot)*4)
		if err != nil {
			return err
		}
		if existing != 0 {
			return fmt.Errorf("KTF module jump slot %d at %#x is not empty (%#x)", slot, table+uint32(slot)*4, existing)
		}
		stub, err := runtime.stub(svcCategoryModuleJump, uint32(slot))
		if err != nil {
			return err
		}
		binary.LittleEndian.PutUint32(words[slot*4:], stub)
	}
	if err := runtime.client.core.Memory().Write(table, words); err != nil {
		return fmt.Errorf("write KTF module jump table at %#x: %w", table, err)
	}
	runtime.moduleJumpTable = table
	return nil
}

// The two slots the invoke helpers reach are the two calling forms this
// platform already runs a method in. The helper checks the method record's
// ACC_NATIVE bit itself and picks between them, which is what identifies each:
//
//	slot 0  a compiled body, entered with the reserved word, the receiver and
//	        the arguments
//	slot 1  a native body, entered with the reserved word and a container
//	        holding the same arguments
//
// Both are reached by a tail jump with the method record in `r0`, the receiver
// in `r1`, and the caller's `r2` and `r3` pushed on the guest stack — so the
// arguments have to be gathered from three places, and the pair the helper
// pushed has to be dropped before the jump returns to its caller.
const (
	moduleJumpInvoke       = 0
	moduleJumpInvokeNative = 1
	// moduleJumpPoll is reached by a bare `bl` with nothing set up and
	// nothing read back — its caller loads neither argument before the call
	// nor a result after it, and the registers it happens to hold are the
	// leftovers of the call before. A guest polling the runtime between
	// iterations of its own loop is the only thing that shape can be, and
	// this side has nothing to do at one.
	moduleJumpPoll = 4
	// moduleJumpMonitorEnter and moduleJumpMonitorExit bracket a synchronized
	// region: each is entered with the object in r0, from a pair of call
	// sites that differ only in whether the class reference had been resolved
	// yet, and each is the first thing after a region entry that pushed a
	// handler for the implicit finally. This platform runs one guest thread
	// at a time and answers the current generation's monitor calls the same
	// way. See handleJavaCall.
	moduleJumpMonitorEnter = 2
	moduleJumpMonitorExit  = 3
	// moduleJumpWait is reached twice in one local module and from nowhere
	// else: both times inside a sound thread's `run`, with the object in r0
	// and inside a protected region — which is `wait()` and the interruption
	// it declares. It is answered the way this platform answers the current
	// generation's `Object.wait`: the worker gives up its slice and comes
	// back, because nothing here holds a monitor to be released.
	moduleJumpWait = 5
)

func (runtime *initializationRuntime) handleModuleJumpCall(ctx context.Context, thread *armcore.Thread, slot uint32) (uint32, error) {
	switch slot {
	case moduleJumpInvoke, moduleJumpInvokeNative:
		return runtime.invokeModuleMethod(ctx, thread, slot == moduleJumpInvokeNative)
	case moduleJumpPoll:
		runtime.countDiagnostic("module poll")
		return 0, nil
	case moduleJumpMonitorEnter, moduleJumpMonitorExit:
		return 0, nil
	case moduleJumpWait:
		return 0, runtime.sleepCurrentWorker(waitSliceWithoutTimeout)
	}
	registers := make([]uint32, 4)
	for index := range registers {
		value, err := thread.Register(index)
		if err != nil {
			return 0, err
		}
		registers[index] = value
	}
	return 0, fmt.Errorf("KTF module jump %d is not implemented (r0=%#x r1=%#x r2=%#x r3=%#x)%s",
		slot, registers[0], registers[1], registers[2], registers[3], runtime.callerSite(thread))
}

// moduleJumpSpill is how many argument words the invoke helper pushed before
// it jumped: the caller's r2 and r3.
const moduleJumpSpill = 2

func (runtime *initializationRuntime) invokeModuleMethod(ctx context.Context, thread *armcore.Thread, native bool) (uint32, error) {
	record, err := thread.Register(0)
	if err != nil {
		return 0, err
	}
	method, _, err := runtime.readAOTMethod(record)
	if err != nil {
		return 0, fmt.Errorf("read KTF module method record %#x: %w", record, err)
	}
	methodType, err := jvm.ParseMethodDescriptor(method.Descriptor)
	if err != nil {
		return 0, fmt.Errorf("KTF module method %s%s: %w", method.Name, method.Descriptor, err)
	}
	words := 0
	if method.AccessFlags&0x0008 == 0 {
		words++
	}
	for _, parameter := range methodType.Parameters {
		if parameter.Kind == jvm.TypeLong || parameter.Kind == jvm.TypeDouble {
			words += 2
			continue
		}
		words++
	}
	stackPointer, err := thread.Register(armcore.RegisterSP)
	if err != nil {
		return 0, err
	}
	// The first word is in r1; the next two are the pair the helper spilled;
	// anything past that is where the caller's own stack arguments already
	// sit, immediately above the spill.
	arguments := make([]uint32, 0, words)
	for index := 0; index < words; index++ {
		switch {
		case index == 0:
			value, registerErr := thread.Register(1)
			if registerErr != nil {
				return 0, registerErr
			}
			arguments = append(arguments, value)
		default:
			value, readErr := runtime.readWord(stackPointer + uint32(index-1)*4)
			if readErr != nil {
				return 0, fmt.Errorf("read KTF module call argument %d: %w", index, readErr)
			}
			arguments = append(arguments, value)
		}
	}
	body := method.Body
	call := append([]uint32{0}, arguments...)
	if native {
		body = method.NativeBody
		container, allocationErr := runtime.allocateWords(arguments)
		if allocationErr != nil {
			return 0, fmt.Errorf("allocate KTF module native argument container: %w", allocationErr)
		}
		call = []uint32{0, container}
	}
	if body == 0 {
		return 0, fmt.Errorf("KTF module method %s%s has no %s body%s",
			method.Name, method.Descriptor, map[bool]string{true: "native", false: "compiled"}[native], runtime.callerSite(thread))
	}
	if err := runtime.enterAOTCall(); err != nil {
		return 0, err
	}
	defer runtime.leaveAOTCall()
	summary, err := runtime.client.core.Call(ctx, thread, body, ReturnAddress, call, runtime.handleSupervisorCall)
	if err != nil {
		return 0, fmt.Errorf("execute KTF module method %s%s: %w", method.Name, method.Descriptor, err)
	}
	// The helper's spilled pair belongs to the helper, and the jump returns
	// straight to its caller, so it is dropped here rather than by anyone
	// else — and only once the call has returned. Dropping it first would
	// enter the call above the frame that owns any handler in it, and a long
	// jump out of the call would then resume in the wrong one.
	if err := thread.SetRegister(armcore.RegisterSP, stackPointer+moduleJumpSpill*4); err != nil {
		return 0, err
	}
	// A long or a double comes back in both result registers, and the call ran
	// on a context of its own; see callAOTJump for the title that found this.
	if err := thread.SetRegister(1, summary.Context.Registers[1]); err != nil {
		return 0, err
	}
	return summary.Context.Registers[0], nil
}

// A module's constant objects carry their dispatch header already baked in,
// and every one of them names a class by a fixed offset from the base a
// virtual call adds: 208 character arrays at offset 0, the 208 strings over
// them at offset 20, and all 58 class records at offset 40. Three classes at
// three fixed places is the whole of it, and the offsets are twenty bytes
// apart because a class record is twenty bytes — so the base is an array of
// them, and this side has to lay the first three out where the image already
// expects them.
var moduleWellKnownClasses = []struct {
	name   string
	offset uint32
}{
	{name: "[C", offset: 0},
	{name: "java/lang/String", offset: javaClassSize},
	{name: "java/lang/Class", offset: 2 * javaClassSize},
}

// placeModuleAliases writes the three class records the image's own constants
// dispatch through into the base, and registers each as the alias for its
// class so that everything downstream — a method lookup, a type check, a field
// read — answers about the class rather than about an unregistered record.
func (runtime *initializationRuntime) placeModuleAliases() error {
	if runtime.jvmContext == 0 {
		return fmt.Errorf("KTF JVM context is not prepared")
	}
	for _, wellKnown := range moduleWellKnownClasses {
		class, err := runtime.ensureJavaClass(wellKnown.name)
		if err != nil {
			return fmt.Errorf("resolve KTF module class %s: %w", wellKnown.name, err)
		}
		record, err := runtime.readAOTBytes(class, javaClassSize, "well-known class record")
		if err != nil {
			return err
		}
		alias := runtime.jvmContext + wellKnown.offset
		copied := make([]byte, javaClassSize)
		copy(copied, record)
		binary.LittleEndian.PutUint32(copied[0:], alias+4)
		if err := runtime.client.core.Memory().Write(alias, copied); err != nil {
			return fmt.Errorf("write KTF module alias for %s: %w", wellKnown.name, err)
		}
		if err := runtime.client.vm.RegisterAOTAddressAlias(alias, wellKnown.name); err != nil {
			return fmt.Errorf("register KTF module alias for %s: %w", wellKnown.name, err)
		}
		runtime.classAliases[class] = alias
	}
	return nil
}

// An image string is a pair of objects the module built at compile time: an
// array holding the UTF-16 units, and a string over it. Neither is allocated
// at run time, so neither is registered the way the current generation's
// guest registers the strings it builds — this side walks the image once and
// binds what it finds, which is what makes a constant usable as an argument.
const (
	moduleObjectHeader = 4
	moduleArrayLength  = 8
	moduleArrayData    = 12
	moduleStringValue  = 8
	moduleStringLength = 16
)

func (runtime *initializationRuntime) bindModuleStrings() error {
	image, err := runtime.client.ImageBytes()
	if err != nil {
		return err
	}
	stringHeader := (runtime.jvmContext + javaClassSize - runtime.jvmContext) << 5
	bound := 0
	for offset := 0; offset+moduleStringLength+4 <= len(image); offset += 4 {
		address := ImageBase + uint32(offset)
		if binary.LittleEndian.Uint32(image[offset:]) != address+4 {
			continue
		}
		if binary.LittleEndian.Uint32(image[offset+moduleObjectHeader:]) != stringHeader {
			continue
		}
		text, err := runtime.readModuleStringUnits(
			binary.LittleEndian.Uint32(image[offset+moduleStringValue:]),
			binary.LittleEndian.Uint32(image[offset+moduleStringLength:]),
		)
		if err != nil {
			return fmt.Errorf("read KTF module string at %#x: %w", address, err)
		}
		if err := runtime.client.vm.BindAOTObject(address, runtime.client.vm.NewString(text)); err != nil {
			return fmt.Errorf("bind KTF module string at %#x: %w", address, err)
		}
		bound++
	}
	runtime.callbacks.RegisteredStrings += uint32(bound)
	runtime.client.log("KTF module strings bound", "count", bound)
	return nil
}

func (runtime *initializationRuntime) readModuleStringUnits(array, length uint32) (string, error) {
	if length > maxJavaStringUnits {
		return "", fmt.Errorf("length %d exceeds %d", length, maxJavaStringUnits)
	}
	stored, err := runtime.readWord(array + moduleArrayLength)
	if err != nil {
		return "", err
	}
	if stored < length {
		return "", fmt.Errorf("array at %#x holds %d units, string names %d", array, stored, length)
	}
	data := make([]byte, uint64(length)*2)
	if err := runtime.client.core.Memory().Read(array+moduleArrayData, data); err != nil {
		return "", err
	}
	units := make([]uint16, length)
	for index := range units {
		units[index] = binary.LittleEndian.Uint16(data[index*2:])
	}
	return string(utf16.Decode(units)), nil
}

// moduleClassInitialized is the bit the guest tests in the halfword after a
// class record's vtable count before it asks for an initializer.
const moduleClassInitialized = 0x0008

func (runtime *initializationRuntime) setModuleClassInitialized(class uint32) error {
	var flags [2]byte
	if err := runtime.client.core.Memory().Read(class+18, flags[:]); err != nil {
		return fmt.Errorf("read KTF module class flags at %#x: %w", class, err)
	}
	binary.LittleEndian.PutUint16(flags[:], binary.LittleEndian.Uint16(flags[:])|moduleClassInitialized)
	if err := runtime.client.core.Memory().Write(class+18, flags[:]); err != nil {
		return fmt.Errorf("write KTF module class flags at %#x: %w", class, err)
	}
	return nil
}

// A module keeps its handler chain and its saved frame in the same records the
// current generation uses, at the same offsets for everything the platform
// reads — the method, the previous head, the label, the caught object and the
// restore-function table — and differs in two places: the head is a field of
// the context rather than a fixed offset into a block of its own, and the
// register block at +24 is saved in another order, with the stack pointer
// first instead of tenth.
func (runtime *initializationRuntime) exceptionHead() uint32 {
	if runtime.client.module {
		if runtime.moduleContext == 0 {
			return 0
		}
		return runtime.moduleContext + moduleContextHandlers
	}
	if runtime.exceptionContext == 0 {
		return 0
	}
	return runtime.exceptionContext + javaExceptionHead
}

// A module keeps the label at +16 and what it caught at +12; the current
// generation keeps them the other way round.
func (runtime *initializationRuntime) exceptionLabel() uint32 {
	if runtime.client.module {
		return javaExceptionObject
	}
	return javaExceptionCurrentPC
}

func (runtime *initializationRuntime) exceptionObject() uint32 {
	if runtime.client.module {
		return javaExceptionCurrentPC
	}
	return javaExceptionObject
}

func (runtime *initializationRuntime) exceptionFrameStack() int {
	if runtime.client.module {
		return javaExceptionContext
	}
	return javaExceptionFrameStack
}

// moduleRestoreStub is the Thumb routine a caught exception resumes through.
// The current generation's modules carry one of their own and publish it in
// the two-word table every handler record copies from `fp + 0x30`; a module
// publishes no such table, so this side writes one, and the routine it names
// has to unpack the module's own register order:
//
//	+24 sp, +28 lr, +32..44 r4-r7, +48 r8, +52 r9, +60 r10
//
// It is entered the way the rest of this platform enters a restore — the
// record's register block in r0 and the catch label in r1 — and it leaves the
// way a `setjmp` does: back to where the region was entered, with the label in
// r0. That is what the compiled code expects, because a protected region here
// is a call that answers zero on the way in and a label on the way back, and
// the method switches on the answer.
func moduleRestoreStub() []byte {
	halfwords := []uint16{
		0x468c, // mov  ip, r1          the label to resume with
		0x6802, // ldr  r2, [r0, #0]    sp
		0x4695, // mov  sp, r2
		0x6842, // ldr  r2, [r0, #4]    lr, which is where the region entry
		0x4696, // mov  lr, r2          returned to when it was pushed
		0x6a42, // ldr  r2, [r0, #36]   r10
		0x4692, // mov  sl, r2
		0x6982, // ldr  r2, [r0, #24]   r8
		0x4690, // mov  r8, r2
		0x69c2, // ldr  r2, [r0, #28]   r9
		0x4691, // mov  r9, r2
		0x3008, // adds r0, #8
		0xc8f0, // ldmia r0!, {r4, r5, r6, r7}
		0x4660, // mov  r0, ip          the label is the answer
		0x4770, // bx   lr
	}
	code := make([]byte, len(halfwords)*2)
	for index, halfword := range halfwords {
		binary.LittleEndian.PutUint16(code[index*2:], halfword)
	}
	return code
}

// The three context words a thread cannot share are its handler chain, the
// stack its runtime glue runs helpers on, and the slot that glue parks the
// caller's stack pointer in while it does. They live at fixed offsets in one
// block, because that is what `fp` points at, so each is registered as a
// thread-local word and given a value per thread instead of a block per
// thread — which also keeps the class table and the restore routine beside
// them shared, as they should be.
var moduleThreadWords = []uint32{moduleContextStackSave, moduleContextHandlers, moduleContextStack}

func (runtime *initializationRuntime) registerModuleThreadWords() error {
	for _, offset := range moduleThreadWords {
		if err := runtime.client.core.RegisterThreadLocalWord(runtime.moduleContext + offset); err != nil {
			return fmt.Errorf("register KTF module context word %#x: %w", offset, err)
		}
	}
	return nil
}

// prepareModuleThread gives a guest worker its own runtime stack and an empty
// handler chain.
func (runtime *initializationRuntime) prepareModuleThread(thread *armcore.Thread) error {
	stack, err := runtime.allocate(moduleRuntimeStackSize)
	if err != nil {
		return err
	}
	values := map[uint32]uint32{
		moduleContextStackSave: 0,
		moduleContextHandlers:  0,
		moduleContextStack:     stack + moduleRuntimeStackSize,
	}
	for offset, value := range values {
		if err := runtime.client.core.SetThreadLocalWord(thread, runtime.moduleContext+offset, value); err != nil {
			return fmt.Errorf("set KTF module context word %#x: %w", offset, err)
		}
	}
	return nil
}
