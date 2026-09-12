package lgt

import (
	"context"
	"strings"
	"testing"
)

func TestDataInputStreamResetsWrappedByteArray(t *testing.T) {
	client := fixtureClient(t)
	arrayType, err := client.javaArrayType(1, "B", 1)
	if err != nil {
		t.Fatal(err)
	}
	array, err := client.allocateJavaArray(arrayType.Object, 3)
	if err != nil {
		t.Fatal(err)
	}
	block, err := client.readWord(array + 8)
	if err != nil {
		t.Fatal(err)
	}
	if err := client.core.Memory().Write(block+4, []byte{11, 22, 33}); err != nil {
		t.Fatal(err)
	}
	newObject := func(name string) uint32 {
		t.Helper()
		class, err := client.preparePlatformJavaClass(name)
		if err != nil {
			t.Fatal(err)
		}
		object, err := client.allocateJavaObject(class)
		if err != nil {
			t.Fatal(err)
		}
		return object
	}
	input := newObject("java/io/ByteArrayInputStream")
	if _, err := javaByteStreamConstructor(client, nil, client.thread, []uint32{input, array}); err != nil {
		t.Fatal(err)
	}
	wrapper := newObject(javaDataInputStreamClass)
	if _, err := javaWrapStream(client, nil, nil, []uint32{wrapper, input}); err != nil {
		t.Fatal(err)
	}
	call := func(object, slot uint32, args ...uint32) uint32 {
		t.Helper()
		for index, value := range append([]uint32{object}, args...) {
			if err := client.thread.SetRegister(index, value); err != nil {
				t.Fatal(err)
			}
		}
		class, _ := client.javaClassOfObject(object)
		served, err := client.callJavaPlatformVirtual(context.Background(), client.thread, javaVirtualSlot(class.Name, slot))
		if err != nil || !served {
			t.Fatalf("slot %d: served=%v error=%v", slot, served, err)
		}
		value, err := client.thread.Register(0)
		if err != nil {
			t.Fatal(err)
		}
		return value
	}
	// Startup may reset before any explicit mark. Both objects share a cursor.
	call(wrapper, 17)
	if got := call(wrapper, 10); got != 11 {
		t.Fatalf("first byte = %d", got)
	}
	call(wrapper, 17)
	if got := call(input, 10); got != 11 {
		t.Fatalf("reset byte = %d", got)
	}
	if got := call(wrapper, 18); got != 1 {
		t.Fatalf("markSupported = %d", got)
	}
	call(input, 16, 0)
	if got := call(wrapper, 10); got != 22 {
		t.Fatalf("marked byte = %d", got)
	}
	call(wrapper, 10)
	if got := call(wrapper, 10); got != ^uint32(0) {
		t.Fatalf("EOF = %d", got)
	}
	call(wrapper, 17)
	if got := call(input, 10); got != 22 {
		t.Fatalf("byte after marked reset = %d", got)
	}
}

func TestJavaStreamWithoutMarkSupportRejectsReset(t *testing.T) {
	client := fixtureClient(t)
	client.archive = &Archive{Resources: map[string][]byte{"data": {11, 22}}}
	name, err := client.newJavaString("data")
	if err != nil {
		t.Fatal(err)
	}
	object, err := javaGetResourceAsStream(client, nil, nil, []uint32{0, name})
	if err != nil {
		t.Fatal(err)
	}
	args := []uint32{object, 100}
	if got, err := javaStreamMarkSupported(client, nil, nil, args); err != nil || got != 0 {
		t.Fatalf("markSupported = %d, %v", got, err)
	}
	if _, err := javaStreamMark(client, nil, nil, args); err != nil {
		t.Fatal(err)
	}
	if _, err := javaStreamReset(client, nil, client.thread, args); err == nil || !strings.Contains(err.Error(), javaIOExceptionClass) {
		t.Fatalf("reset error = %v, want IOException", err)
	}
}
