package lgt

import (
	"testing"
)

// What a Java title's byte sinks, vectors and clips hold lives in host slices
// that grow on every call. Each is bounded by what the language's own storage
// for it could hold here — a byte array, an Object array — so a runaway loop
// stops with a report instead of growing the host until the process dies.

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
