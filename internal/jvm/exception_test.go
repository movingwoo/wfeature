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
