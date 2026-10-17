package jvm

import (
	_ "embed"
	"testing"
)

//go:embed testdata/LoneSurrogate.class
var loneSurrogateClass []byte

// The class file format lets a string constant carry half a surrogate pair,
// and javac writes one for a literal such as "\uD800". The class loads; the
// half reads back as one replacement character, which is what this runtime's
// strings already make of a lone surrogate built at run time.
func TestClassWithALoneSurrogateConstantLoads(t *testing.T) {
	vm := New(mapClassSource{"LoneSurrogate": loneSurrogateClass}, Options{})
	result, err := vm.InvokeStatic("LoneSurrogate", "text", "()Ljava/lang/String;")
	if err != nil {
		t.Fatalf("text() error = %v", err)
	}
	object, err := result.Reference()
	if err != nil {
		t.Fatal(err)
	}
	if text, _ := StringText(object); text != "a�b" {
		t.Fatalf("text() = %q, want %q", text, "a�b")
	}
	length, err := vm.InvokeStatic("LoneSurrogate", "length", "()I")
	if err != nil {
		t.Fatalf("length() error = %v", err)
	}
	if got, _ := length.Int32(); got != 3 {
		t.Fatalf("length() = %d, want 3", got)
	}
}
