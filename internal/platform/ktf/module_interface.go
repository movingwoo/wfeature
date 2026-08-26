package ktf

import (
	"context"
	"fmt"
	"strings"

	"github.com/movingwoo/wfeature/internal/armcore"
	"github.com/movingwoo/wfeature/internal/jvm"
)

// The older relocatable images of this platform do not hand the Host an
// executable descriptor the way the current generation does. Their entry is
// handed the platform's own callback table, reads its first word — the same
// `getInterface(name, major, minor)` the current generation reaches through
// its initialization parameters — and asks it for one interface by name. The
// name is the same in all three local modules, the version asked for is `any`,
// and the module keeps the answer in a static and reports success only when it
// is not null. Everything the module does afterwards goes through that table.
//
// What the table holds is found out one call at a time, which is why every
// slot that is not answered is a stub that counts itself and names the guest
// address that called it: a table reached only through a register never
// mentions a slot number in the guest's own code, so a count with no address
// cannot be investigated at all.
//
// The fourteen slots the local modules reach are found in one static sweep for
// the veneer every interface call goes through: the instruction two before it
// is the one that loads the slot. Each of the fourteen turned out to be a call
// this platform already answers for the current generation of images under
// another name, and two needed a translation rather than a forward — see
// modulePrimitiveTypes and moduleNewMultiArray.
//
// A module caches each answer by writing it over the reference cell that
// asked, which is the same lazily-linked constant pool the current generation
// carries; see module_link.go for the half of it this side has to link.
//
// See docs/ktf.md, "The older modules run under the platform".
const moduleInterfaceName = "MNInterface"

// moduleInterfaceFunctions bounds the table. The highest slot any local module
// reaches is `0x74`, and the same bound the WIPI C tables use covers it with
// room for the ones not yet seen.
const moduleInterfaceFunctions = 64

func (runtime *initializationRuntime) makeModuleInterface() (uint32, error) {
	if runtime.moduleInterface != 0 {
		return runtime.moduleInterface, nil
	}
	entries := make([]uint32, moduleInterfaceFunctions)
	for function := range entries {
		stub, err := runtime.stub(svcCategoryModuleInterface, uint32(function))
		if err != nil {
			return 0, err
		}
		entries[function] = stub
	}
	table, err := runtime.allocateWords(entries)
	if err != nil {
		return 0, err
	}
	runtime.moduleInterface = table
	return table, nil
}

// The slot numbers are byte offsets divided by four, because that is how the
// module indexes the table. Fourteen of the sixty-four are reached by the
// local modules and each one is named by what its caller does with the answer:
// a static sweep of the module's own code for the veneer every interface call
// goes through finds them all at once, and the instruction that loads the slot
// is two before it.
const (
	// moduleInterfaceThrow is the failure path of every resolver here.
	moduleInterfaceThrow = 0x20 / 4
	// moduleInterfaceFailed is what an allocation that answered null reports.
	// Its two call sites pass no arguments at all, so there is nothing to say
	// about it beyond that the guest could not go on.
	moduleInterfaceFailed = 0x24 / 4
	moduleInterfaceNew    = 0x38 / 4
	// moduleInterfaceNewArray takes an array class, and
	// moduleInterfaceNewPrimitiveArray takes an element type code. The
	// element-class-to-array-class step in front of the first is
	// moduleInterfaceArrayClass.
	moduleInterfaceNewArray          = 0x3c / 4
	moduleInterfaceLoadClass         = 0x40 / 4
	moduleInterfaceObjectClass       = 0x44 / 4
	moduleInterfaceCheckType         = 0x48 / 4
	moduleInterfaceFindField         = 0x54 / 4
	moduleInterfaceInitializeClass   = 0x60 / 4
	moduleInterfaceFindMethod        = 0x64 / 4
	moduleInterfaceArrayClass        = 0x6c / 4
	moduleInterfaceNewPrimitiveArray = 0x70 / 4
	// moduleInterfaceNewMultiArray takes the outermost array class, how many
	// dimensions it has, and a pointer to that many lengths. The helper in
	// front of it pushes the lengths its caller passed in registers and hands
	// on the stack pointer, which is what makes the third argument a pointer
	// where the other allocators take a count.
	moduleInterfaceNewMultiArray = 0x74 / 4
)

func (runtime *initializationRuntime) handleModuleInterfaceCall(ctx context.Context, thread *armcore.Thread, function uint32) (uint32, error) {
	switch function {
	case moduleInterfaceThrow:
		return runtime.throwAOTException(thread)
	case moduleInterfaceFailed:
		return 0, fmt.Errorf("KTF module reported a failed allocation%s", runtime.callerSite(thread))
	case moduleInterfaceNew:
		runtime.callbacks.Allocations++
		return runtime.newAOTInstance(thread)
	case moduleInterfaceNewArray:
		runtime.callbacks.Allocations++
		return runtime.newAOTArray(thread)
	case moduleInterfaceNewPrimitiveArray:
		runtime.callbacks.Allocations++
		return runtime.moduleNewPrimitiveArray(thread)
	case moduleInterfaceLoadClass:
		return runtime.loadJavaClass(thread)
	case moduleInterfaceObjectClass:
		return runtime.moduleObjectClass(thread)
	case moduleInterfaceCheckType:
		return runtime.checkAOTType(thread)
	case moduleInterfaceFindField:
		return runtime.getAOTField(thread)
	case moduleInterfaceInitializeClass:
		return runtime.moduleInitializeClass(ctx, thread)
	case moduleInterfaceFindMethod:
		return runtime.getAOTMethod(thread)
	case moduleInterfaceArrayClass:
		return runtime.moduleArrayClass(thread)
	case moduleInterfaceNewMultiArray:
		runtime.callbacks.Allocations++
		return runtime.moduleNewMultiArray(thread)
	}
	return 0, fmt.Errorf("KTF module interface function %d (%#x) is not implemented%s",
		function, function*4, runtime.callerSite(thread))
}

// modulePrimitiveTypes is the table a module names a primitive by. The
// current generation is handed the same eight characters as one of its five
// initialization parameters — four reserved words and then `ZCFDBSIJ` — and
// what a module passes is the byte offset into that array rather than the
// character: `0x28` is `4 + 6` words in, which is `I`. A module has no such
// parameter to be handed, so the table is here instead.
var modulePrimitiveTypes = [...]byte{4: 'Z', 5: 'C', 6: 'F', 7: 'D', 8: 'B', 9: 'S', 10: 'I', 11: 'J'}

// moduleNewPrimitiveArray allocates the array a type code names. The code is
// translated rather than passed on, because everything downstream of here —
// the array class, its element size, the name it registers under — is keyed on
// the descriptor character.
func (runtime *initializationRuntime) moduleNewPrimitiveArray(thread *armcore.Thread) (uint32, error) {
	code, err := thread.Register(0)
	if err != nil {
		return 0, err
	}
	index := code / 4
	if code%4 != 0 || index >= uint32(len(modulePrimitiveTypes)) || modulePrimitiveTypes[index] == 0 {
		return 0, fmt.Errorf("KTF module primitive array type code %#x is not one of the eight%s", code, runtime.callerSite(thread))
	}
	if err := thread.SetRegister(0, uint32(modulePrimitiveTypes[index])); err != nil {
		return 0, err
	}
	return runtime.newAOTArray(thread)
}

// moduleObjectClass answers an object's own class record, which the guest then
// reads the descriptor and the element class out of on its way to an array
// store check.
func (runtime *initializationRuntime) moduleObjectClass(thread *armcore.Thread) (uint32, error) {
	object, err := thread.Register(0)
	if err != nil {
		return 0, err
	}
	if object == 0 {
		return 0, fmt.Errorf("KTF module asked for the class of a null object%s", runtime.callerSite(thread))
	}
	class, err := runtime.readWord(object + 4)
	if err != nil {
		return 0, fmt.Errorf("read KTF module object class at %#x: %w", object, err)
	}
	return class, nil
}

// moduleArrayClass turns an element class into the array class of it, which is
// the step the guest takes before allocating a reference array.
func (runtime *initializationRuntime) moduleArrayClass(thread *armcore.Thread) (uint32, error) {
	element, err := thread.Register(0)
	if err != nil {
		return 0, err
	}
	metadata, err := runtime.resolveAOTClass(element)
	if err != nil {
		return 0, fmt.Errorf("resolve KTF module array element class %#x: %w", element, err)
	}
	name := "[L" + metadata.Name + ";"
	if strings.HasPrefix(metadata.Name, "[") {
		name = "[" + metadata.Name
	}
	return runtime.ensureJavaClass(name)
}

// moduleInitializeClass runs a class's own initializer the first time the
// guest asks. The guest tests a bit in the class record before it asks, and
// that bit is the file's own, so this is reached once per class.
func (runtime *initializationRuntime) moduleInitializeClass(ctx context.Context, thread *armcore.Thread) (uint32, error) {
	class, err := thread.Register(0)
	if err != nil {
		return 0, err
	}
	metadata, err := runtime.resolveAOTClass(class)
	if err != nil {
		return 0, fmt.Errorf("resolve KTF module class %#x for initialization: %w", class, err)
	}
	runtime.countDiagnostic("module initialize " + metadata.Name)
	if err := runtime.runAOTClassInitializer(ctx, thread, metadata); err != nil {
		return 0, err
	}
	// The guest tests this bit before it asks, so setting it is what stops it
	// asking again — the current generation's guest sets the same bit on the
	// records it registers itself.
	return 0, runtime.setModuleClassInitialized(class)
}

// maxModuleArrayDimensions bounds a dimension count read out of guest code.
// The language allows 255; nothing here has needed more than two.
const maxModuleArrayDimensions = 255

func (runtime *initializationRuntime) moduleNewMultiArray(thread *armcore.Thread) (uint32, error) {
	class, err := thread.Register(0)
	if err != nil {
		return 0, err
	}
	dimensions, err := thread.Register(1)
	if err != nil {
		return 0, err
	}
	lengthsAddress, err := thread.Register(2)
	if err != nil {
		return 0, err
	}
	if dimensions == 0 || dimensions > maxModuleArrayDimensions {
		return 0, fmt.Errorf("KTF module array of %d dimensions%s", dimensions, runtime.callerSite(thread))
	}
	lengths, err := runtime.readAOTWords(lengthsAddress, dimensions, "module array lengths")
	if err != nil {
		return 0, err
	}
	return runtime.allocateModuleArray(class, lengths)
}

// allocateModuleArray builds an array and, for every dimension past the first,
// the arrays inside it. The element class comes off the descriptor rather than
// from the name, because that word is the one the guest's own array-store
// check reads and the two have to agree.
func (runtime *initializationRuntime) allocateModuleArray(class uint32, lengths []uint32) (uint32, error) {
	metadata, err := runtime.resolveAOTClass(class)
	if err != nil {
		return 0, fmt.Errorf("resolve KTF module array class %#x: %w", class, err)
	}
	if lengths[0] > maxJavaArrayElements {
		return 0, fmt.Errorf("KTF module array length %d exceeds %d", lengths[0], maxJavaArrayElements)
	}
	address, err := runtime.allocateAOTArrayObject(metadata, lengths[0])
	if err != nil {
		return 0, err
	}
	if len(lengths) == 1 || lengths[0] == 0 {
		return address, nil
	}
	descriptor, err := runtime.readWord(metadata.Address + 8)
	if err != nil {
		return 0, err
	}
	element, err := runtime.readWord(descriptor + javaDescriptorElement)
	if err != nil {
		return 0, err
	}
	if element == 0 {
		return 0, fmt.Errorf("KTF module array class %s names no element class", metadata.Name)
	}
	values := make([]jvm.Value, lengths[0])
	for index := range values {
		child, childErr := runtime.allocateModuleArray(element, lengths[1:])
		if childErr != nil {
			return 0, childErr
		}
		object, ok := runtime.client.vm.AOTObject(child)
		if !ok {
			return 0, fmt.Errorf("KTF module array element at %#x is not bound", child)
		}
		values[index] = jvm.ReferenceValue(object)
	}
	object, ok := runtime.client.vm.AOTObject(address)
	if !ok {
		return 0, fmt.Errorf("KTF module array at %#x is not bound", address)
	}
	if err := jvm.SetArrayRange(object, 0, values); err != nil {
		return 0, fmt.Errorf("fill KTF module array %s: %w", metadata.Name, err)
	}
	return address, nil
}
