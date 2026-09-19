package ktf

import (
	"encoding/binary"
	"testing"

	"github.com/movingwoo/wfeature/internal/jvm"
)

func TestImageClassHeaderPreservesCanonicalIdentity(t *testing.T) {
	client, runtime := newTestRuntime(t)
	source := writeGuestClass(t, runtime, "test/Child", 0, nil, 0x21)
	record := readTestBytes(t, client, source, javaClassSize)
	address := ImageBase + 0x80
	binary.LittleEndian.PutUint32(record, address+4)
	if err := client.core.Memory().Write(address, record); err != nil {
		t.Fatal(err)
	}
	metadata, err := runtime.readAOTClass(address)
	if err != nil {
		t.Fatal(err)
	}
	if err := client.vm.RegisterAOTClass(metadata); err != nil {
		t.Fatal(err)
	}
	header, err := runtime.aotVTableHeader(metadata)
	if err != nil {
		t.Fatal(err)
	}
	// The compiled protected-member check compares the receiver's decoded
	// class address directly against the accessing class's original record.
	decoded := uint32(int64(runtime.jvmContext) + int64(int32(header)>>5))
	if decoded != address {
		t.Fatalf("decoded class = %#x, want canonical %#x", decoded, address)
	}
}

func TestFieldlessRuntimeSubclassRetainsInheritedPayload(t *testing.T) {
	client, runtime := newTestRuntime(t)
	for _, name := range []string{runtimeTextFieldComponentClass, runtimeTextBoxComponentClass} {
		address, err := runtime.ensureJavaClass(name)
		if err != nil {
			t.Fatal(err)
		}
		metadata, ok := client.vm.AOTClassAt(address)
		if !ok {
			t.Fatal("missing class")
		}
		if metadata.InstanceSize < textComponentFieldsSize {
			t.Fatalf("%s payload = %d, want at least %d", name, metadata.InstanceSize, textComponentFieldsSize)
		}
	}
}

func TestTextComponentPublishesConstructedHandler(t *testing.T) {
	client, runtime := newTestRuntime(t)
	class, err := runtime.ensureJavaClass(runtimeTextFieldComponentClass)
	if err != nil {
		t.Fatal(err)
	}
	address, object, err := runtime.allocateAOTInstance(class)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtimeTextComponentConstructorWithText(runtime, client.vm, []jvm.Value{jvm.ReferenceValue(object), jvm.ReferenceValue(client.vm.NewString("")), jvm.IntValue(0)}); err != nil {
		t.Fatal(err)
	}
	if err := runtime.publishGuestFields(object, runtimeJavaMethod{class: runtimeTextFieldComponentClass, name: "<init>"}); err != nil {
		t.Fatal(err)
	}
	handler, err := runtime.readWord(address + javaInstanceSize + javaInstanceHeader)
	if err != nil {
		t.Fatal(err)
	}
	if handler == 0 {
		t.Fatal("constructed input handler was not published")
	}
	bound, ok := client.vm.AOTObject(handler)
	if !ok || bound.ClassName != runtimeInputMethodHandlerClass {
		t.Fatal("payload does not name the input handler")
	}
}

func TestClassContextRejectsOverlapAndExhaustion(t *testing.T) {
	client, runtime := newTestRuntime(t)
	client.mapped = uint64(ThreadStackBase - ImageBase)
	if _, err := runtime.prepareClassContext(); err == nil {
		t.Fatal("context overlapping the stack accepted")
	}
	runtime.classArena = newGuestArena(runtime.jvmContext, 4)
	if _, err := runtime.allocateClassAlias(make([]byte, 8)); err == nil {
		t.Fatal("class arena overflow accepted")
	}
}
