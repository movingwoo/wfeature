package lgt

import (
	"context"
	"testing"
)

// What a Java title's byte sinks, vectors and clips hold lives in host slices
// that grow on every call. Each is bounded by what the language's own storage
// for it could hold here — a byte array, an Object array — so a runaway loop
// stops with a report instead of growing the host until the process dies.

func TestJavaByteSinkStopsAtTheLargestArrayItCouldBecome(t *testing.T) {
	client := fixtureClient(t)
	runtime := client.javaRuntimeState()
	const sink = 0x7ffff000
	if _, err := javaByteSinkConstructor(client, nil, nil, []uint32{sink}); err != nil {
		t.Fatal(err)
	}
	array, err := client.newJavaByteArray([]byte{1, 2, 3, 4})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	writes := map[string]func() (uint32, error){
		"write(I)":    func() (uint32, error) { return javaByteSinkWrite(client, ctx, nil, []uint32{sink, 7}) },
		"write([BII)": func() (uint32, error) { return javaByteSinkWriteRange(client, ctx, nil, []uint32{sink, array, 0, 4}) },
		"write([B)":   func() (uint32, error) { return javaByteSinkWriteAll(client, ctx, nil, []uint32{sink, array}) },
		"writeInt":    func() (uint32, error) { return javaSinkAppend(4)(client, ctx, nil, []uint32{sink, 7}) },
		"writeLong":   func() (uint32, error) { return javaSinkWriteLong(client, ctx, nil, []uint32{sink, 7, 0}) },
		"writeUTF": func() (uint32, error) {
			return javaSinkWriteUTF(client, ctx, nil, []uint32{sink, newTestString(t, client, "ab")})
		},
	}
	for name, write := range writes {
		// Room for exactly one byte: a write of one fits, a wider one does not.
		runtime.sinks[sink] = make([]byte, maxJavaSinkBytes-1)
		_, err := write()
		if name == "write(I)" {
			if err != nil {
				t.Errorf("%s into the last free byte: %v", name, err)
			}
			if len(runtime.sinks[sink]) != maxJavaSinkBytes {
				t.Errorf("%s left %d bytes, want %d", name, len(runtime.sinks[sink]), maxJavaSinkBytes)
			}
			continue
		}
		if err == nil {
			t.Errorf("%s past the bound was taken", name)
		}
		if len(runtime.sinks[sink]) != maxJavaSinkBytes-1 {
			t.Errorf("%s past the bound changed the sink to %d bytes", name, len(runtime.sinks[sink]))
		}
	}
	runtime.sinks[sink] = make([]byte, maxJavaSinkBytes)
	if _, err := javaByteSinkWrite(client, ctx, nil, []uint32{sink, 7}); err == nil {
		t.Error("a byte past a full sink was taken")
	}
}

func TestJavaVectorStopsAtTheLargestArrayItCouldBecome(t *testing.T) {
	client := fixtureClient(t)
	runtime := client.javaRuntimeState()
	const vector = 0x7ffff000
	runtime.vectors[vector] = make([]uint32, maxJavaArrayLength-1)
	if _, err := javaVectorAdd(client, nil, nil, []uint32{vector, 1}); err != nil {
		t.Fatalf("the last element that fits: %v", err)
	}
	if _, err := javaVectorAdd(client, nil, nil, []uint32{vector, 1}); err == nil {
		t.Error("addElement past the bound was taken")
	}
	if _, err := javaVectorInsertAt(client, nil, nil, []uint32{vector, 1, 0}); err == nil {
		t.Error("insertElementAt past the bound was taken")
	}
	if len(runtime.vectors[vector]) != maxJavaArrayLength {
		t.Errorf("the vector holds %d elements, want %d", len(runtime.vectors[vector]), maxJavaArrayLength)
	}
}

// putData answers how many bytes the clip took, so a clip that is full takes
// what fits and says so, the same as the C media slot does.
func TestJavaClipPutDataTakesOnlyWhatFits(t *testing.T) {
	client := fixtureClient(t)
	const object = 0x7ffff000
	clip := &mediaClip{java: true, data: make([]byte, maxJavaClipBytes-2)}
	client.clips[object] = clip
	array, err := client.newJavaByteArray([]byte{1, 2, 3, 4, 5, 6, 7, 8})
	if err != nil {
		t.Fatal(err)
	}
	taken, err := javaClipPutData(client, nil, nil, []uint32{object, array, 1, 5})
	if err != nil {
		t.Fatal(err)
	}
	if taken != 2 || len(clip.data) != maxJavaClipBytes {
		t.Fatalf("putData took %d bytes into %d, want 2 into %d", taken, len(clip.data), maxJavaClipBytes)
	}
	if clip.data[len(clip.data)-2] != 2 || clip.data[len(clip.data)-1] != 3 {
		t.Fatal("putData took the wrong bytes")
	}
	if taken, err = javaClipPutData(client, nil, nil, []uint32{object, array, 0, 8}); err != nil || taken != 0 {
		t.Fatalf("putData into a full clip = %d (%v), want 0", taken, err)
	}
	if len(clip.data) != maxJavaClipBytes {
		t.Fatalf("a full clip grew to %d bytes", len(clip.data))
	}
}
