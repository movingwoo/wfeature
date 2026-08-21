package jvm

import (
	_ "embed"
	"testing"
)

//go:embed testdata/Streams.class
var streamsClass []byte

// A title may write its save through java.io.DataOutput and read it back
// through java.io.DataInput, naming the interfaces and passing the streams.
// Neither interface is declared in this runtime, and neither has to be:
// invokeinterface dispatches on the class of the receiver, so what answers is
// the DataOutputStream the title handed over. This is what makes those two
// names appear in a scan of a title's link surface and never reach the loader.
func TestInterfaceCallsLandOnTheStreamThatWasPassed(t *testing.T) {
	vm := New(mapClassSource{"Streams": streamsClass}, Options{})
	result, err := vm.InvokeStatic("Streams", "roundTrip", "()I")
	if err != nil {
		t.Fatalf("roundTrip() error = %v", err)
	}
	value, err := result.Int32()
	if err != nil {
		t.Fatal(err)
	}
	// 4660 + 5 + 1 + 1000, written and read back in that order.
	if value != 5666 {
		t.Fatalf("roundTrip() = %d, want 5666", value)
	}
}
