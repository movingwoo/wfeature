package jvm

import (
	"testing"
	"time"
)

// bareClass is a class file with no members whose superclass is whatever the
// caller names, including itself: the shape a corrupted or crafted archive can
// carry and no compiler emits.
func bareClass(name, super string) []byte {
	return classImplementing(name, super, 0x0021) // public super
}

// A superclass chain that comes back on itself is refused where it is walked,
// rather than walked for ever: method resolution, the native lookup behind it,
// and a catch clause's class test all stop with an answer.
func TestCyclicSuperclassChainsAreBounded(t *testing.T) {
	source := mapClassSource{
		"Self":  bareClass("Self", "Self"),
		"Left":  bareClass("Left", "Right"),
		"Right": bareClass("Right", "Left"),
	}
	vm := New(source, Options{})
	defer vm.Close()
	for _, className := range []string{"Self", "Left"} {
		state := vm.newExecution()
		object, err := vm.newObject(state, className)
		if err != nil {
			t.Fatalf("new %s error = %v", className, err)
		}
		if _, err := invokeWithin(t, 5*time.Second, func() (Value, error) {
			return vm.InvokeVirtual(object, "toString", "()Ljava/lang/String;")
		}); err == nil {
			t.Fatalf("%s.toString() resolved through a cyclic chain", className)
		}
		if _, err := invokeWithin(t, 5*time.Second, func() (Value, error) {
			return vm.InvokeStatic(className, "missing", "()V")
		}); err == nil {
			t.Fatalf("%s.missing() resolved through a cyclic chain", className)
		}
		if _, err := invokeWithin(t, 5*time.Second, func() (Value, error) {
			return VoidValue(), boolError(vm.HasMethodBody(className, "missing", "()V"))
		}); err != nil {
			t.Fatalf("HasMethodBody(%s.missing) = true", className)
		}
		if _, err := invokeWithin(t, 5*time.Second, func() (Value, error) {
			return VoidValue(), boolError(vm.IsGuestException(guestException(className, ""), "java/lang/Throwable"))
		}); err != nil {
			t.Fatalf("a %s exception was caught as Throwable through a cyclic chain", className)
		}
	}
}

// boolError turns a boolean the test expects to be false into an error.
func boolError(value bool) error {
	if value {
		return ErrStepLimit
	}
	return nil
}
