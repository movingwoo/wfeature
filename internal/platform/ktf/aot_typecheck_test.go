package ktf

import (
	"encoding/binary"
	"testing"

	"github.com/movingwoo/wfeature/internal/armcore"
)

// writeGuestClass lays out one guest class record and its descriptor the way a
// relocated client image does, so the type check has a real hierarchy to walk.
// Only the parts the check reads are filled in: the name, the superclass
// record, the declared interfaces, and the access flags.
func writeGuestClass(t *testing.T, runtime *initializationRuntime, name string, parent uint32, interfaces []uint32, accessFlags uint16) uint32 {
	t.Helper()
	nameAddress, err := runtime.allocateBytes(append([]byte(name), 0))
	if err != nil {
		t.Fatal(err)
	}
	var interfaceTable uint32
	if len(interfaces) > 0 {
		if interfaceTable, err = runtime.allocateWords(interfaces); err != nil {
			t.Fatal(err)
		}
	}
	descriptorAddress, err := runtime.allocate(javaDescriptorSize)
	if err != nil {
		t.Fatal(err)
	}
	descriptor := make([]byte, javaDescriptorSize)
	binary.LittleEndian.PutUint32(descriptor[0:], nameAddress)
	binary.LittleEndian.PutUint32(descriptor[8:], parent)
	binary.LittleEndian.PutUint32(descriptor[16:], interfaceTable)
	binary.LittleEndian.PutUint16(descriptor[26:], 4)
	binary.LittleEndian.PutUint16(descriptor[28:], accessFlags)
	binary.LittleEndian.PutUint16(descriptor[30:], uint16(len(interfaces)))
	if err := runtime.client.core.Memory().Write(descriptorAddress, descriptor); err != nil {
		t.Fatal(err)
	}
	classAddress, err := runtime.allocate(javaClassSize)
	if err != nil {
		t.Fatal(err)
	}
	class := make([]byte, javaClassSize)
	binary.LittleEndian.PutUint32(class[0:], classAddress+4)
	binary.LittleEndian.PutUint32(class[8:], descriptorAddress)
	if err := runtime.client.core.Memory().Write(classAddress, class); err != nil {
		t.Fatal(err)
	}
	return classAddress
}

func checkGuestType(t *testing.T, runtime *initializationRuntime, target, object uint32) uint32 {
	t.Helper()
	context := armcore.NewContext()
	context.Registers[0] = target
	context.Registers[1] = object
	result, err := runtime.checkAOTType(armcore.NewThread(context))
	if err != nil {
		t.Fatalf("checkAOTType() error = %v", err)
	}
	return result
}

// A game asks about classes long before it registers them with the runtime, so
// deciding assignability from the Host's registry alone gives up on the middle
// of a hierarchy and falls back to answering yes. The guest then takes a branch
// meant for an unrelated type, which is how enemies stop acting on a map whose
// entity list the game filters by class.
func TestCheckTypeDecidesThroughAnUnregisteredGuestSuperclass(t *testing.T) {
	_, runtime := newTestRuntime(t)
	base := writeGuestClass(t, runtime, "game/Base", 0, nil, 0x21)
	middle := writeGuestClass(t, runtime, "game/Middle", base, nil, 0x21)
	leaf := writeGuestClass(t, runtime, "game/Leaf", middle, nil, 0x21)
	unrelated := writeGuestClass(t, runtime, "game/Unrelated", 0, nil, 0x21)

	object, _, err := runtime.allocateAOTInstance(leaf)
	if err != nil {
		t.Fatal(err)
	}
	if _, registered := runtime.client.vm.AOTClass("game/Middle"); registered {
		t.Fatal("the middle of the hierarchy is registered, so the test proves nothing")
	}
	if result := checkGuestType(t, runtime, unrelated, object); result != 0 {
		t.Errorf("game/Leaf instanceof game/Unrelated = %d, want 0", result)
	}
	if result := checkGuestType(t, runtime, base, object); result != 1 {
		t.Errorf("game/Leaf instanceof game/Base = %d, want 1", result)
	}
}

// The class descriptor names the interfaces a class implements, so an interface
// target is answerable too rather than being waved through.
func TestCheckTypeAnswersInterfaceTargetsFromTheDeclaredList(t *testing.T) {
	_, runtime := newTestRuntime(t)
	// 0x0200 is ACC_INTERFACE.
	base := writeGuestClass(t, runtime, "game/IBase", 0, nil, 0x601)
	derived := writeGuestClass(t, runtime, "game/IDerived", 0, []uint32{base}, 0x601)
	implementor := writeGuestClass(t, runtime, "game/Implementor", 0, []uint32{derived}, 0x21)
	inheritor := writeGuestClass(t, runtime, "game/Inheritor", implementor, nil, 0x21)
	plain := writeGuestClass(t, runtime, "game/Plain", 0, nil, 0x21)

	implementorObject, _, err := runtime.allocateAOTInstance(implementor)
	if err != nil {
		t.Fatal(err)
	}
	inheritorObject, _, err := runtime.allocateAOTInstance(inheritor)
	if err != nil {
		t.Fatal(err)
	}
	plainObject, _, err := runtime.allocateAOTInstance(plain)
	if err != nil {
		t.Fatal(err)
	}
	if result := checkGuestType(t, runtime, derived, implementorObject); result != 1 {
		t.Errorf("game/Implementor instanceof game/IDerived = %d, want 1", result)
	}
	// An interface the class reaches only through the one it declares.
	if result := checkGuestType(t, runtime, base, implementorObject); result != 1 {
		t.Errorf("game/Implementor instanceof game/IBase = %d, want 1", result)
	}
	// An interface inherited from the superclass rather than declared.
	if result := checkGuestType(t, runtime, derived, inheritorObject); result != 1 {
		t.Errorf("game/Inheritor instanceof game/IDerived = %d, want 1", result)
	}
	if result := checkGuestType(t, runtime, derived, plainObject); result != 0 {
		t.Errorf("game/Plain instanceof game/IDerived = %d, want 0", result)
	}
}
