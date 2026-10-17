package jvm

import "testing"

// The heap budget sits on every allocation, so these measure what it adds to
// the two the guest makes most: a small array and a String. An allocation
// that is not a sample costs one atomic subtraction; see guestHeap.
// BenchmarkGuestCallLoopAllocating covers an object `new` makes.

var benchmarkSink *Object

func BenchmarkHeapBudgetSmallArray(b *testing.B) {
	vm := New(nil, Options{})
	component := Type{Kind: TypeInt}
	b.ReportAllocs()
	for b.Loop() {
		array, err := vm.NewArray(component, 64)
		if err != nil {
			b.Fatal(err)
		}
		benchmarkSink = array
	}
}

func BenchmarkHeapBudgetLibraryString(b *testing.B) {
	vm := New(nil, Options{})
	b.ReportAllocs()
	for b.Loop() {
		object, err := vm.newStringObject("a library string")
		if err != nil {
			b.Fatal(err)
		}
		benchmarkSink = object
	}
}
