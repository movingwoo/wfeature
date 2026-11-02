package jvm

import (
	"fmt"
	"testing"
)

func TestIsGuestExceptionMatchesRuntimeHierarchyThroughWrappers(t *testing.T) {
	vm := New(emptyClassSource{}, Options{})
	err := fmt.Errorf("invoke callback: %w", guestException("java/lang/ArithmeticException", "/ by zero"))

	if !vm.IsGuestException(err, RuntimeExceptionClass) {
		t.Fatal("IsGuestException() did not match ArithmeticException as RuntimeException")
	}
	if !vm.IsGuestException(err, "java/lang/Throwable") {
		t.Fatal("IsGuestException() did not match ArithmeticException as Throwable")
	}
	if vm.IsGuestException(err, "java/lang/Error") {
		t.Fatal("IsGuestException() matched ArithmeticException as Error")
	}
	if vm.IsGuestException(fmt.Errorf("host failure"), RuntimeExceptionClass) {
		t.Fatal("IsGuestException() matched a host error")
	}
}

type emptyClassSource struct{}

func (emptyClassSource) ClassBytes(string) ([]byte, bool) {
	return nil, false
}

// An empty message is a message: getMessage answers "" and toString keeps
// the separator, where a missing one answers null.
func TestThrowableKeepsAnEmptyMessage(t *testing.T) {
	vm := New(libraryContractsSource(), Options{})
	if got := contractInt(t, vm, "emptyMessageLength"); got != 0 {
		t.Fatalf("new Exception(\"\").getMessage().length() = %d, want 0", got)
	}
	if got := contractString(t, vm, "emptyMessageText"); got != "java.lang.Exception: " {
		t.Fatalf("new Exception(\"\").toString() = %q", got)
	}
	empty, err := vm.NewObject("java/lang/Exception", "()V")
	if err != nil {
		t.Fatal(err)
	}
	message, err := vm.InvokeVirtual(empty, "getMessage", "()Ljava/lang/String;")
	if err != nil {
		t.Fatal(err)
	}
	if object, _ := message.Reference(); object != nil {
		t.Fatalf("new Exception().getMessage() = %v, want null", object)
	}
}
