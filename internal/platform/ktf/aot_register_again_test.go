package ktf

import (
	"context"
	"encoding/binary"
	"slices"
	"testing"

	"github.com/movingwoo/wfeature/internal/armcore"
)

// registeredAOTFixture builds one class record in guest memory, with a method,
// a field and a vtable of one word, and answers the runtime, the class
// record's address and the vtable's.
func registeredAOTFixture(t *testing.T) (*initializationRuntime, *Client, uint32, uint32) {
	t.Helper()
	client, err := LoadClient(ClientImage{Name: "client.bin0", Data: syntheticInitializableClient()}, armcore.CoreOptions{})
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := newInitializationRuntime(client)
	if err != nil {
		t.Fatal(err)
	}
	classAddress, err := runtime.allocate(20)
	if err != nil {
		t.Fatal(err)
	}
	className, _ := runtime.allocateBytes([]byte("game/Again\x00"))
	methodName, _ := runtime.allocateBytes(append([]byte{0}, []byte("(I)I+run\x00")...))
	fieldName, _ := runtime.allocateBytes(append([]byte{0}, []byte("I+value\x00")...))
	methodAddress, _ := runtime.allocate(28)
	fieldAddress, _ := runtime.allocate(16)
	methodTable, _ := runtime.allocateWords([]uint32{methodAddress, 0})
	fieldTable, _ := runtime.allocateWords([]uint32{fieldAddress, 0})
	vtable, _ := runtime.allocateWords([]uint32{methodAddress})
	descriptor, _ := runtime.allocate(36)
	writeTestWords(t, client, methodAddress, []uint32{0x101001, classAddress, 0, methodName, 0, 0x00010000, 0})
	writeTestWords(t, client, fieldAddress, []uint32{0x0001, classAddress, fieldName, 4})
	descriptorData := make([]byte, 36)
	binary.LittleEndian.PutUint32(descriptorData[0:], className)
	binary.LittleEndian.PutUint32(descriptorData[12:], methodTable)
	binary.LittleEndian.PutUint32(descriptorData[20:], fieldTable)
	binary.LittleEndian.PutUint16(descriptorData[24:], 1)
	binary.LittleEndian.PutUint16(descriptorData[26:], 8)
	binary.LittleEndian.PutUint16(descriptorData[28:], 0x21)
	if err := client.Core().Memory().Write(descriptor, descriptorData); err != nil {
		t.Fatal(err)
	}
	classData := make([]byte, 20)
	binary.LittleEndian.PutUint32(classData[0:], classAddress+4)
	binary.LittleEndian.PutUint32(classData[8:], descriptor)
	binary.LittleEndian.PutUint32(classData[12:], vtable)
	binary.LittleEndian.PutUint16(classData[16:], 1)
	if err := client.Core().Memory().Write(classAddress, classData); err != nil {
		t.Fatal(err)
	}
	return runtime, client, classAddress, vtable
}

// A title registers the same class record again and again. When nothing it
// says has changed, registering it again reads the bytes it was read from and
// parses and registers nothing; when its vtable has changed, the class's
// vtable snapshot follows, as it did when every registration read the record
// whole.
func TestRegisteringAnUnchangedClassAgainReadsNoMoreThanItsBytes(t *testing.T) {
	runtime, client, classAddress, vtable := registeredAOTFixture(t)
	register := func() {
		t.Helper()
		context_ := armcore.NewContext()
		context_.Registers[0] = classAddress
		if _, err := runtime.registerAOTClass(context.Background(), armcore.NewThread(context_)); err != nil {
			t.Fatalf("registerAOTClass() error = %v", err)
		}
	}
	register()
	first, ok := client.JVM().AOTClass("game/Again")
	if !ok || first.Address != classAddress || len(first.VTable) != 1 {
		t.Fatalf("registered class = %+v (%t)", first, ok)
	}
	whole := testing.AllocsPerRun(20, func() {
		read := runtime.aotClassReads[classAddress]
		delete(runtime.aotClassReads, classAddress)
		register()
		if runtime.aotClassReads[classAddress] == nil {
			runtime.aotClassReads[classAddress] = read
		}
	})
	again := testing.AllocsPerRun(20, register)
	t.Logf("allocations: %.0f registering again, %.0f reading the record whole", again, whole)
	if again*4 > whole {
		t.Fatalf("registering an unchanged class again allocated %.0f times, a whole read %.0f", again, whole)
	}
	if held, ok := client.JVM().AOTClass("game/Again"); !ok || !slices.Equal(held.VTable, first.VTable) {
		t.Fatalf("the class after registering it again = %+v", held)
	}

	writeTestWords(t, client, vtable, []uint32{0x123450})
	register()
	refreshed, ok := client.JVM().AOTClass("game/Again")
	if !ok || !slices.Equal(refreshed.VTable, []uint32{0x123450}) {
		t.Fatalf("the vtable snapshot after the title changed its vtable = %+v", refreshed.VTable)
	}
	// AllocsPerRun runs its function once more than it is asked to.
	if registered := runtime.callbacks.RegisteredClasses; registered != 1+21+21+1 {
		t.Fatalf("RegisteredClasses = %d, want every registration counted", registered)
	}
}
