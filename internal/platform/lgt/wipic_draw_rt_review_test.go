package lgt

import (
	"runtime"
	"testing"
)

// MC_grpDrawString and MC_grpGetStringWidth hand over a counted string, and
// the count is the guest's own int32. A count past any line of text is
// refused before anything is allocated for it, rather than allocating up to
// two gigabytes and then finding the guest memory is not there.
func TestACountedDrawStringIsBoundedBeforeItIsRead(t *testing.T) {
	client := fixtureClient(t)
	pointer, err := client.allocateBytes([]byte("hello"))
	if err != nil {
		t.Fatal(err)
	}
	if text, err := client.readString(pointer, 5); err != nil || text != "hello" {
		t.Fatalf("readString(5) = %q, %v", text, err)
	}
	if _, err := client.readString(pointer, maxCStringLength+1); err == nil {
		t.Fatal("a string one byte past the bound was read")
	}
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	if _, err := client.readString(pointer, 0x7ffffff0); err == nil {
		t.Fatal("a string of two gigabytes was read")
	}
	runtime.ReadMemStats(&after)
	if grown := after.TotalAlloc - before.TotalAlloc; grown > 64<<20 {
		t.Fatalf("refusing the string allocated %d bytes first", grown)
	}
}
