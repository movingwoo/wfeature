package jvm

import "testing"

// A Class object a title kept — in a field, or as the lock it synchronizes on —
// is still the one getClass answers after a heap restore. The table of class
// objects is not part of the saved heap; it is rebuilt from the objects the
// heap holds.
func TestClassObjectsStayCanonicalAcrossHeapRestore(t *testing.T) {
	source := libraryContractsSource()
	vm := New(source, Options{})
	key, err := vm.NewObject("LibraryContracts$Key", "(I)V", IntValue(3))
	if err != nil {
		t.Fatal(err)
	}
	classValue, err := vm.InvokeVirtual(key, "getClass", "()Ljava/lang/Class;")
	if err != nil {
		t.Fatal(err)
	}
	class, err := classValue.Reference()
	if err != nil || class == nil {
		t.Fatalf("getClass() = %v/%v", class, err)
	}
	saved, err := vm.CaptureHeapState([]*Object{key, class}, HeapCodec{})
	if err != nil {
		t.Fatal(err)
	}
	fresh := New(source, Options{})
	roots, err := fresh.RestoreHeapState(saved, HeapCodec{})
	if err != nil {
		t.Fatal(err)
	}
	again, err := fresh.InvokeVirtual(roots[0], "getClass", "()Ljava/lang/Class;")
	if err != nil {
		t.Fatal(err)
	}
	if object, _ := again.Reference(); object != roots[1] {
		t.Fatalf("getClass() after restore = %p, want the restored class object %p", object, roots[1])
	}
}
