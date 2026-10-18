package ktf

import (
	"runtime"
	"testing"

	"github.com/movingwoo/wfeature/internal/jvm"
)

// The character the Host composed for a title's field is taken only once the
// array that carries it to the listener exists. Making the array can be
// refused by the heap budget, and a character taken before it was lost with
// the refusal; it now waits for the title's next key.
func TestAComposedCharacterOutlivesARefusedDelivery(t *testing.T) {
	vm := jvm.New(nil, jvm.Options{MaxHeapBytes: 8 << 20})
	const listenerClass = "test/BudgetListener"
	if err := vm.DefineClass(jvm.ClassDefinition{Name: listenerClass, SuperName: jvm.ObjectClass, Access: jvm.AccessPublic,
		Methods: []jvm.MethodDefinition{{Name: "notifyTextChanged", Descriptor: "([CII)V", Access: jvm.AccessPublic | jvm.AccessNative}}}); err != nil {
		t.Fatal(err)
	}
	delivered := 0
	if err := vm.RegisterNative(listenerClass, "notifyTextChanged", "([CII)V", func(*jvm.VM, []jvm.Value) (jvm.Value, error) {
		delivered++
		return jvm.VoidValue(), nil
	}); err != nil {
		t.Fatal(err)
	}
	listener := &jvm.Object{ClassName: listenerClass, Fields: make(map[string]jvm.Value)}
	// Arrays up to the budget, then small ones until one is refused, and a
	// string the host holds past the limit: from there every allocation is
	// weighed and none fits.
	var held []*jvm.Object
	for {
		array, err := vm.NewArray(jvm.Type{Kind: jvm.TypeInt}, 64<<10)
		if err != nil {
			break
		}
		held = append(held, array)
	}
	for refused := false; !refused; {
		array, err := vm.NewArray(jvm.Type{Kind: jvm.TypeInt}, 4)
		if err != nil {
			if !vm.IsGuestException(err, "java/lang/OutOfMemoryError") {
				t.Fatal(err)
			}
			refused = true
		}
		held = append(held, array)
	}
	held = append(held, vm.NewString("held past the limit"))
	taken := 0
	take := func() { taken++ }
	if err := notifyInputMethodListenerAfter(vm, listener, []uint16{'A'}, inputMethodInsert, take); !vm.IsGuestException(err, "java/lang/OutOfMemoryError") {
		t.Fatalf("a delivery whose array the full budget refuses: %v, want OutOfMemoryError", err)
	}
	if taken != 0 || delivered != 0 {
		t.Fatalf("the refused delivery took the character %d times and delivered it %d", taken, delivered)
	}
	runtime.KeepAlive(held)
	held = nil
	runtime.GC()
	if err := notifyInputMethodListenerAfter(vm, listener, []uint16{'A'}, inputMethodInsert, take); err != nil {
		t.Fatalf("the delivery once the budget has room: %v", err)
	}
	if taken != 1 || delivered != 1 {
		t.Fatalf("the delivery took the character %d times and delivered it %d, want once each", taken, delivered)
	}
}
