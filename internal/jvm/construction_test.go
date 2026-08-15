package jvm

import (
	_ "embed"
	"strings"
	"testing"
)

//go:embed testdata/Constructed.class
var constructedClass []byte

func TestNewObjectInvokesConstructor(t *testing.T) {
	vm := New(mapClassSource{"Constructed": constructedClass}, Options{})
	object, err := vm.NewObject("Constructed", "(I)V", IntValue(42))
	if err != nil {
		t.Fatalf("NewObject() error = %v", err)
	}

	result, err := vm.InvokeVirtual(object, "value", "()I")
	if err != nil {
		t.Fatalf("InvokeVirtual() error = %v", err)
	}
	value, err := result.Int32()
	if err != nil {
		t.Fatal(err)
	}
	if value != 42 {
		t.Fatalf("value() = %d, want 42", value)
	}

	isObject, err := vm.IsSubclassOf("Constructed", "java/lang/Object")
	if err != nil {
		t.Fatalf("IsSubclassOf() error = %v", err)
	}
	if !isObject {
		t.Fatal("Constructed is not recognized as a java/lang/Object subclass")
	}
	isString, err := vm.IsSubclassOf("Constructed", "java/lang/String")
	if err != nil {
		t.Fatalf("IsSubclassOf() unrelated class error = %v", err)
	}
	if isString {
		t.Fatal("Constructed is reported as a java/lang/String subclass")
	}
}

func TestNewObjectRejectsInvalidConstructor(t *testing.T) {
	vm := New(mapClassSource{"Constructed": constructedClass}, Options{})
	_, err := vm.NewObject("Constructed", "()I")
	if err == nil || !strings.Contains(err.Error(), "must return void") {
		t.Fatalf("NewObject() error = %v", err)
	}
}
