package jvm

import (
	"runtime"
	"strconv"
	"testing"
)

// A Hashtable grows its keys and values together, and the second array can
// be refused after the first was made. A table that kept grown keys over the
// old values never grew again: its next new key landed past the values' end.
// Refused halfway, the table stays as it was and grows on the next put.
func TestAHashtableRefusedHalfwayThroughGrowingGrowsLater(t *testing.T) {
	// Sampled finely enough that both arrays are charged exactly.
	vm := New(nil, Options{})
	vm.heap = newGuestHeap(DefaultMaxHeapBytes, 1<<10, nil)
	table, err := vm.NewObject(HashtableClass, "()V")
	if err != nil {
		t.Fatal(err)
	}
	put := func(index int) error {
		key := vm.NewString("key" + strconv.Itoa(index))
		_, err := vm.InvokeVirtual(table, "put", "(Ljava/lang/Object;Ljava/lang/Object;)Ljava/lang/Object;",
			ReferenceValue(key), ReferenceValue(vm.NewString(strconv.Itoa(index))))
		return err
	}
	// 143 entries fill the capacity the table reaches from 8, so the next new
	// key grows both arrays to 287 elements.
	const full = 143
	for index := range full {
		if err := put(index); err != nil {
			t.Fatalf("put %d: %v", index, err)
		}
	}
	// Room for one grown array and a quarter: the arrays it replaces stay
	// charged until both are stored, so the second is refused.
	grownArray := arrayHeapBytes + 287*valueHeapBytes
	runtime.GC()
	charged, limit := vm.HeapUsage()
	vm.heap.mu.Lock()
	vm.heap.limit = charged + grownArray + grownArray/4
	vm.heap.mu.Unlock()
	if err := put(full); !vm.IsGuestException(err, "java/lang/OutOfMemoryError") {
		t.Fatalf("a put that needs two grown arrays where one fits: %v, want OutOfMemoryError", err)
	}
	keys, values, size, err := hashtableState(vm, table)
	if err != nil {
		t.Fatal(err)
	}
	if keys.Length() != values.Length() || size != full {
		t.Fatalf("after the refused growth the table holds %d entries in %d keys and %d values", size, keys.Length(), values.Length())
	}
	vm.heap.mu.Lock()
	vm.heap.limit = limit
	vm.heap.mu.Unlock()
	for index := full; index < full+3; index++ {
		if err := put(index); err != nil {
			t.Fatalf("put %d once the budget has room: %v", index, err)
		}
	}
	value, err := vm.InvokeVirtual(table, "get", "(Ljava/lang/Object;)Ljava/lang/Object;", ReferenceValue(vm.NewString("key144")))
	if err != nil {
		t.Fatal(err)
	}
	if object, _ := value.Reference(); object == nil {
		t.Fatal("the entry put after the growth is missing")
	} else if text, _ := StringText(object); text != "144" {
		t.Fatalf("get(key144) = %q", text)
	}
}
