package ktf

import (
	"encoding/binary"
	"testing"
)

// writeModuleClass lays out one class record of a published module class
// table: a descriptor naming the class and its superclass, and a method table
// holding one method that claims the vtable slot it is given. The slot is the
// point of the helper — a module ships slot numbers and no table, so a slot
// number above what the superclass fills is what leaves a gap in the middle.
func writeModuleClass(t *testing.T, runtime *initializationRuntime, name string, parent uint32, method, descriptor string, slot uint16) uint32 {
	t.Helper()
	class := writeGuestClass(t, runtime, name, parent, nil, 0x21)
	fullName, err := runtime.allocateBytes(append([]byte{0}, append([]byte(descriptor+"+"+method), 0)...))
	if err != nil {
		t.Fatal(err)
	}
	methodAddress, err := runtime.allocate(javaMethodSize)
	if err != nil {
		t.Fatal(err)
	}
	record := make([]byte, javaMethodSize)
	binary.LittleEndian.PutUint32(record[0:], 0x1000)
	binary.LittleEndian.PutUint32(record[4:], class)
	binary.LittleEndian.PutUint32(record[12:], fullName)
	binary.LittleEndian.PutUint16(record[20:], slot)
	binary.LittleEndian.PutUint16(record[22:], 0x0001)
	if err := runtime.client.core.Memory().Write(methodAddress, record); err != nil {
		t.Fatal(err)
	}
	table, err := runtime.allocateWords([]uint32{methodAddress, 0})
	if err != nil {
		t.Fatal(err)
	}
	descriptorAddress, err := runtime.readAOTWords(class+8, 1, "class descriptor")
	if err != nil {
		t.Fatal(err)
	}
	var pointer [4]byte
	binary.LittleEndian.PutUint32(pointer[:], table)
	if err := runtime.client.core.Memory().Write(descriptorAddress[0]+12, pointer[:]); err != nil {
		t.Fatal(err)
	}
	return class
}

// A module publishes its classes in one table and the platform links them all,
// but what a class inherits is read from what its superclass registered. The
// walk used to be a map range, so the order changed on every run and a class
// linked before its superclass inherited nothing at all: one local title
// reported two different failures for one archive depending on the draw. A
// superclass is now linked before the subclass that needs it, whatever order
// the table is walked in.
//
// The loop is what makes this a test rather than a coincidence: Go randomises
// map order per range, so twenty rounds over three classes see both orders.
func TestAModuleClassLinksItsSuperclassFirst(t *testing.T) {
	for round := 0; round < 20; round++ {
		_, runtime := newTestRuntime(t)
		parent := writeModuleClass(t, runtime, "game/Parent", 0, "tick", "()V", 5)
		child := writeModuleClass(t, runtime, "game/Child", parent, "draw", "()V", 7)
		runtime.moduleClassByName = map[string]uint32{
			"game/Child":  child,
			"game/Parent": parent,
		}
		if err := runtime.linkModuleClasses(); err != nil {
			t.Fatalf("round %d: linkModuleClasses() error = %v", round, err)
		}
		metadata, ok := runtime.client.vm.AOTClassAt(child)
		if !ok {
			t.Fatalf("round %d: the subclass was not registered", round)
		}
		// Slot 5 is the superclass's method. A subclass that linked before its
		// parent inherits an empty table and leaves that slot null, which is a
		// virtual call that branches to zero the first time the guest makes it.
		if len(metadata.VTable) <= 7 {
			t.Fatalf("round %d: subclass vtable holds %d slots, want 8", round, len(metadata.VTable))
		}
		if metadata.VTable[5] == 0 {
			t.Fatalf("round %d: subclass vtable slot 5 is null, want the superclass method", round)
		}
		if metadata.VTable[7] == 0 {
			t.Fatalf("round %d: subclass vtable slot 7 is null, want its own method", round)
		}
	}
}

// A module numbers its own methods from the first slot past the table its
// superclass fills, so a platform class that publishes fewer methods than the
// runtime the module was compiled against leaves a gap in the middle of the
// table. The gap is padded with zeroes to keep the numbering, and reading one
// of those back as a method record dereferenced null: a title whose superclass
// was seven slots short failed with an invalid method range at 0x0, which
// named neither the class nor the slot.
func TestAnEmptyInheritedVTableSlotIsNotAMethod(t *testing.T) {
	_, runtime := newTestRuntime(t)
	parent := writeModuleClass(t, runtime, "game/Base", 0, "tick", "()V", 5)
	child := writeModuleClass(t, runtime, "game/Middle", parent, "draw", "()V", 9)
	leaf := writeModuleClass(t, runtime, "game/Leaf", child, "paint", "()V", 11)
	runtime.moduleClassByName = map[string]uint32{
		"game/Base":   parent,
		"game/Middle": child,
		"game/Leaf":   leaf,
	}
	if err := runtime.linkModuleClasses(); err != nil {
		t.Fatalf("linkModuleClasses() error = %v", err)
	}
	metadata, ok := runtime.client.vm.AOTClassAt(leaf)
	if !ok {
		t.Fatal("the leaf class was not registered")
	}
	if len(metadata.VTable) <= 11 {
		t.Fatalf("leaf vtable holds %d slots, want 12", len(metadata.VTable))
	}
	// The gaps stay gaps: the slots below a method are what its number means,
	// so filling one would move every method under it.
	for _, slot := range []int{0, 6, 10} {
		if metadata.VTable[slot] != 0 {
			t.Fatalf("leaf vtable slot %d = %#x, want an empty slot", slot, metadata.VTable[slot])
		}
	}
	for _, slot := range []int{5, 9, 11} {
		if metadata.VTable[slot] == 0 {
			t.Fatalf("leaf vtable slot %d is null, want a method", slot)
		}
	}
}

// writeModuleClassFields lays out a class record whose descriptor names one
// field at the offset it is given, which is what a rebase has to move.
func writeModuleClassFields(t *testing.T, runtime *initializationRuntime, name string, parent uint32, field, descriptor string, offset uint32, instanceSize uint16) uint32 {
	t.Helper()
	class := writeGuestClass(t, runtime, name, parent, nil, 0x21)
	fullName, err := runtime.allocateBytes(append([]byte{0}, append([]byte(descriptor+"+"+field), 0)...))
	if err != nil {
		t.Fatal(err)
	}
	fieldAddress, err := runtime.allocate(javaFieldSize)
	if err != nil {
		t.Fatal(err)
	}
	record := make([]byte, javaFieldSize)
	binary.LittleEndian.PutUint32(record[4:], class)
	binary.LittleEndian.PutUint32(record[8:], fullName)
	binary.LittleEndian.PutUint32(record[12:], offset)
	if err := runtime.client.core.Memory().Write(fieldAddress, record); err != nil {
		t.Fatal(err)
	}
	table, err := runtime.allocateWords([]uint32{fieldAddress, 0})
	if err != nil {
		t.Fatal(err)
	}
	descriptorAddress, err := runtime.readAOTWords(class+8, 1, "class descriptor")
	if err != nil {
		t.Fatal(err)
	}
	var pointer [4]byte
	binary.LittleEndian.PutUint32(pointer[:], table)
	if err := runtime.client.core.Memory().Write(descriptorAddress[0]+moduleDescriptorFieldTable, pointer[:]); err != nil {
		t.Fatal(err)
	}
	var size [2]byte
	binary.LittleEndian.PutUint16(size[:], instanceSize)
	if err := runtime.client.core.Memory().Write(descriptorAddress[0]+moduleDescriptorInstanceSize, size[:]); err != nil {
		t.Fatal(err)
	}
	return class
}

// A module ships a field's offset inside its own class and the guest indexes
// the object with it directly, so a subclass of another of the module's own
// classes writes its first field over its superclass's first field. One local
// title's two constructors wrote four reference fields to the same four words
// and the canvas the framework had pushed to the display was replaced, which
// is why it painted a null screen every frame. The platform is what knows the
// whole chain, so it is what moves the subclass's fields past the superclass's
// — in the guest's own records, which is where the guest reads them.
func TestAModuleSubclassFieldMovesPastItsSuperclass(t *testing.T) {
	_, runtime := newTestRuntime(t)
	parent := writeModuleClassFields(t, runtime, "game/Base", 0, "first", "Ljava/lang/Object;", 0, 24)
	child := writeModuleClassFields(t, runtime, "game/Leaf", parent, "own", "Ljava/lang/Object;", 0, 8)
	runtime.moduleClassByName = map[string]uint32{"game/Base": parent, "game/Leaf": child}
	if err := runtime.linkModuleClasses(); err != nil {
		t.Fatalf("linkModuleClasses() error = %v", err)
	}

	metadata, ok := runtime.client.vm.AOTClassAt(child)
	if !ok {
		t.Fatal("the subclass was not registered")
	}
	if len(metadata.Fields) != 1 {
		t.Fatalf("subclass holds %d fields, want 1", len(metadata.Fields))
	}
	if metadata.Fields[0].Offset != 24 {
		t.Fatalf("subclass field offset = %d, want 24 past its superclass", metadata.Fields[0].Offset)
	}
	// The object has to be large enough for both halves, or the fields that
	// did move land outside it.
	if metadata.InstanceSize != 32 {
		t.Fatalf("subclass instance size = %d, want 32", metadata.InstanceSize)
	}
	// The superclass keeps what it had: its own offsets are already right,
	// and moving them would take its fields away from the constructor that
	// writes them.
	base, ok := runtime.client.vm.AOTClassAt(parent)
	if !ok {
		t.Fatal("the superclass was not registered")
	}
	if base.Fields[0].Offset != 0 || base.InstanceSize != 24 {
		t.Fatalf("superclass field offset = %d size = %d, want 0 and 24", base.Fields[0].Offset, base.InstanceSize)
	}
}

// A class whose superclass is one of this platform's is the case the guest is
// already right about: those fields are not in the guest payload at all, so
// adding the platform class's size would move every field past the end of the
// object the guest allocated.
func TestAModuleClassUnderAPlatformClassKeepsItsOffsets(t *testing.T) {
	_, runtime := newTestRuntime(t)
	card, err := runtime.ensureJavaClass("org/kwis/msp/lcdui/Card")
	if err != nil {
		t.Fatal(err)
	}
	class := writeModuleClassFields(t, runtime, "game/Canvas", card, "own", "Ljava/lang/Object;", 0, 12)
	runtime.moduleClassByName = map[string]uint32{"game/Canvas": class}
	if err := runtime.linkModuleClasses(); err != nil {
		t.Fatalf("linkModuleClasses() error = %v", err)
	}
	metadata, ok := runtime.client.vm.AOTClassAt(class)
	if !ok {
		t.Fatal("the class was not registered")
	}
	if metadata.Fields[0].Offset != 0 || metadata.InstanceSize != 12 {
		t.Fatalf("offset = %d size = %d, want 0 and 12", metadata.Fields[0].Offset, metadata.InstanceSize)
	}
}
