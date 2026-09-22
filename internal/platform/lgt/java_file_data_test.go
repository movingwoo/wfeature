package lgt

import (
	"context"
	"testing"
)

func TestFileDataInputStreamReadsAndReleasesFile(t *testing.T) {
	client := fixtureClient(t)
	client.saveStore = newMemorySaveStore()
	client.writeFile("record.dat", []byte{0xff, 0x12, 0x34, 0x56, 0x78, 9})
	name, err := client.newJavaString("record.dat")
	if err != nil {
		t.Fatal(err)
	}
	file := uint32(0x1100)
	if _, err := javaFileOpen(client, nil, nil, []uint32{file, name, 1, 1}); err != nil {
		t.Fatal(err)
	}
	handle := client.javaRuntimeState().files[file]
	client.files[handle].cursor = 1
	method, ok := javaPlatformMethods[javaFileClass+".openDataInputStream()Ljava/io/DataInputStream;"]
	if !ok {
		t.Fatal("File.openDataInputStream is not implemented")
	}
	if err := client.thread.SetRegister(0, file); err != nil {
		t.Fatal(err)
	}
	if err := client.callJavaMethod(context.Background(), client.thread, javaFileClass, "openDataInputStream", method); err != nil {
		t.Fatal(err)
	}
	stream, err := client.thread.Register(0)
	if err != nil {
		t.Fatal(err)
	}
	if class, ok := client.javaClassOfObject(stream); !ok || class.Name != javaDataInputStreamClass {
		t.Fatal("opener did not return a DataInputStream object")
	}
	if _, err := javaFileOpenInputStream(client, nil, nil, []uint32{file}); err == nil {
		t.Fatal("second input stream accepted")
	}
	// Reach the returned object's typed read through the native dispatch boundary.
	served, err := client.callJavaPlatformVirtual(context.Background(), client.thread, javaVirtualSlot(javaDataInputStreamClass, 28))
	if !served || err != nil {
		t.Fatalf("readInt: served=%v error=%v", served, err)
	}
	if got, _ := client.thread.Register(0); got != 0x12345678 {
		t.Fatalf("readInt = %#x", got)
	}
	for i := 0; i < 2; i++ {
		if _, err := javaStreamClose(client, nil, nil, []uint32{stream}); err != nil {
			t.Fatal(err)
		}
		if got := client.files[handle].cursor; got != 5 {
			t.Fatalf("cursor after close %d = %d, want 5", i, got)
		}
	}
	next, err := method.Implementat(client, nil, nil, []uint32{file})
	if err != nil {
		t.Fatal(err)
	}
	if got, err := javaStreamRead(client, nil, nil, []uint32{next}); err != nil || got != 9 {
		t.Fatalf("reopened read = %d, %v", got, err)
	}
	if _, err := javaFileClose(client, nil, nil, []uint32{file}); err != nil {
		t.Fatal(err)
	}
	if _, err := method.Implementat(client, nil, nil, []uint32{file}); err == nil {
		t.Fatal("stream opened on closed file")
	}
}
