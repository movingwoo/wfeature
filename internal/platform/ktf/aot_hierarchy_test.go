package ktf

import (
	"encoding/binary"
	"strings"
	"testing"

	"github.com/movingwoo/wfeature/internal/armcore"
)

// writeGuestClassWithMethod lays out a class record whose descriptor names one
// method, which is what a lookup through the hierarchy has to find.
func writeGuestClassWithMethod(t *testing.T, runtime *initializationRuntime, name string, parent uint32, method, descriptor string) uint32 {
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

// KTF hands the runtime one class at a time, so a title whose Jlet inherits
// startApp from a base class in the same image used to report its own startApp
// missing: the lookup walked the chain through the registry, and the base class
// had never been in it. Resolving a class now brings its ancestors with it.
func TestResolvingAClassRegistersItsSuperclassChain(t *testing.T) {
	_, runtime := newTestRuntime(t)
	base := writeGuestClassWithMethod(t, runtime, "game/Base", 0, "startApp", "([Ljava/lang/String;)V")
	middle := writeGuestClass(t, runtime, "game/Middle", base, nil, 0x21)
	leaf := writeGuestClass(t, runtime, "game/Leaf", middle, nil, 0x21)

	if _, err := runtime.resolveAOTClass(leaf); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"game/Middle", "game/Base"} {
		if _, registered := runtime.client.vm.AOTClass(name); !registered {
			t.Fatalf("%s was not registered with its subclass", name)
		}
	}
	method, found, err := runtime.client.vm.FindAOTMethod(leaf, "startApp", "([Ljava/lang/String;)V")
	if err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Fatal("startApp was not found through the inherited chain")
	}
	if method.Name != "startApp" {
		t.Fatalf("found %s, want startApp", method.Name)
	}
}

// A record that will not read stops the walk instead of failing the class that
// asked for it: the ancestors are enrichment of a lookup that worked without
// them.
func TestAnUnreadableAncestorDoesNotFailTheClassThatAskedForIt(t *testing.T) {
	_, runtime := newTestRuntime(t)
	base := writeGuestClass(t, runtime, "game/Base", 0, nil, 0x21)
	leaf := writeGuestClass(t, runtime, "game/Leaf", base, nil, 0x21)
	// The base keeps a readable name, which is what the child's own record
	// needs, and loses its method table, which is what registering it needs.
	descriptorAddress, err := runtime.readAOTWords(base+8, 1, "class descriptor")
	if err != nil {
		t.Fatal(err)
	}
	var pointer [4]byte
	binary.LittleEndian.PutUint32(pointer[:], 0xdeadbee0)
	if err := runtime.client.core.Memory().Write(descriptorAddress[0]+12, pointer[:]); err != nil {
		t.Fatal(err)
	}
	metadata, err := runtime.resolveAOTClass(leaf)
	if err != nil {
		t.Fatalf("resolveAOTClass() error = %v", err)
	}
	if metadata.Name != "game/Leaf" {
		t.Fatalf("resolved %s, want game/Leaf", metadata.Name)
	}
}

// A miss names the class it was made against. The field path used to report a
// bare handle, which is not a thing an investigation can search for — and four
// titles in one local set stopped on exactly that.
func TestAFailedFieldLookupNamesItsClass(t *testing.T) {
	_, runtime := newTestRuntime(t)
	class := writeGuestClass(t, runtime, "game/Holder", 0, nil, 0x21)
	if _, err := runtime.resolveAOTClass(class); err != nil {
		t.Fatal(err)
	}
	nameAddress, err := runtime.allocateBytes(append([]byte{0}, append([]byte("I+missing"), 0)...))
	if err != nil {
		t.Fatal(err)
	}
	context := armcore.NewContext()
	context.Registers[0] = class
	context.Registers[1] = nameAddress
	_, err = runtime.getAOTField(armcore.NewThread(context))
	if err == nil {
		t.Fatal("a missing field was answered")
	}
	if !strings.Contains(err.Error(), "class game/Holder") {
		t.Fatalf("the miss does not name its class: %v", err)
	}
}
