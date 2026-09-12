package lgt

import (
	"context"
	"testing"
)

func TestJavaDataInputStreamReadLongSlot(t *testing.T) {
	for _, data := range [][]byte{{0x81, 2, 3, 4, 5, 6, 7, 8}, {1, 2, 3}} {
		client := fixtureClient(t)
		class, err := client.preparePlatformJavaClass(javaDataInputStreamClass)
		if err != nil {
			t.Fatal(err)
		}
		object, err := client.allocateJavaObject(class)
		if err != nil {
			t.Fatal(err)
		}
		stream := &javaStream{Name: "record", Data: data}
		client.javaRuntimeState().streams[object] = stream
		if err := client.thread.SetRegister(0, object); err != nil {
			t.Fatal(err)
		}
		served, err := client.callJavaPlatformVirtual(context.Background(), client.thread, javaVirtualSlot(javaDataInputStreamClass, 29))
		if !served {
			t.Fatal("readLong slot is not served")
		}
		if len(data) < 8 {
			if err == nil {
				t.Fatal("truncated long accepted")
			}
			if stream.Read != 0 {
				t.Fatal("truncated read moved cursor")
			}
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		low, _ := client.thread.Register(0)
		high, _ := client.thread.Register(1)
		if low != 0x05060708 || high != 0x81020304 || stream.Read != 8 {
			t.Fatalf("long = %#x:%08x, cursor=%d", high, low, stream.Read)
		}
	}
}

func TestJavaDataInputStreamSkipBytesSlot(t *testing.T) {
	client := fixtureClient(t)
	class, err := client.preparePlatformJavaClass(javaDataInputStreamClass)
	if err != nil {
		t.Fatal(err)
	}
	object, err := client.allocateJavaObject(class)
	if err != nil {
		t.Fatal(err)
	}
	stream := &javaStream{Name: "record", Data: []byte{11, 22, 33}}
	client.javaRuntimeState().streams[object] = stream
	for _, step := range []struct{ count, want uint32 }{{^uint32(0), 0}, {0, 0}, {2, 2}, {100, 1}, {1, 0}} {
		if err := client.thread.SetRegister(0, object); err != nil {
			t.Fatal(err)
		}
		if err := client.thread.SetRegister(1, step.count); err != nil {
			t.Fatal(err)
		}
		served, err := client.callJavaPlatformVirtual(context.Background(), client.thread, javaVirtualSlot(javaDataInputStreamClass, 21))
		if !served || err != nil {
			t.Fatalf("skipBytes: served=%v error=%v", served, err)
		}
		if got, _ := client.thread.Register(0); got != step.want {
			t.Fatalf("skipBytes(%d) = %d, want %d", int32(step.count), got, step.want)
		}
	}
}
