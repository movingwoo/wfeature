package jvm

import (
	_ "embed"
	"testing"
)

//go:embed testdata/SuperinterfaceInit.class
var superinterfaceInitClass []byte

//go:embed testdata/SuperinterfaceInit$Plain.class
var superinterfaceInitPlainClass []byte

//go:embed testdata/SuperinterfaceInit$Statics.class
var superinterfaceInitStaticsClass []byte

//go:embed testdata/SuperinterfaceInit$Root.class
var superinterfaceInitRootClass []byte

//go:embed testdata/SuperinterfaceInit$Left.class
var superinterfaceInitLeftClass []byte

//go:embed testdata/SuperinterfaceInit$Right.class
var superinterfaceInitRightClass []byte

//go:embed testdata/SuperinterfaceInit$Base.class
var superinterfaceInitBaseClass []byte

//go:embed testdata/SuperinterfaceInit$Implementation.class
var superinterfaceInitImplementationClass []byte

// Initializing a class initializes its superclass and then the superinterfaces
// that declare a default method, an interface's own superinterfaces first
// (JVMS 5.5). The fixture's initializers append B for the superclass, R and G
// for the two interfaces with a default method and C for the class itself; P,
// S and L belong to interfaces that declare none and must not appear.
func TestClassInitializationInitializesDefaultMethodSuperinterfaces(t *testing.T) {
	vm := New(mapClassSource{
		"SuperinterfaceInit":                superinterfaceInitClass,
		"SuperinterfaceInit$Plain":          superinterfaceInitPlainClass,
		"SuperinterfaceInit$Statics":        superinterfaceInitStaticsClass,
		"SuperinterfaceInit$Root":           superinterfaceInitRootClass,
		"SuperinterfaceInit$Left":           superinterfaceInitLeftClass,
		"SuperinterfaceInit$Right":          superinterfaceInitRightClass,
		"SuperinterfaceInit$Base":           superinterfaceInitBaseClass,
		"SuperinterfaceInit$Implementation": superinterfaceInitImplementationClass,
	}, Options{})
	result, err := vm.InvokeStatic("SuperinterfaceInit", "order", "()Ljava/lang/String;")
	if err != nil {
		t.Fatalf("order() error = %v", err)
	}
	object, err := result.Reference()
	if err != nil {
		t.Fatal(err)
	}
	order, ok := StringText(object)
	if !ok {
		t.Fatalf("order() = %v, want a String", object)
	}
	if order != "BRGC" {
		t.Fatalf("initialization order = %q, want %q", order, "BRGC")
	}
}

// An interface the class names but nobody can load is passed over, as it was
// before interfaces were walked: the loader resolves them lazily and a title
// may implement one its platform does not declare.
func TestClassInitializationPassesOverAnUnloadableInterface(t *testing.T) {
	vm := New(mapClassSource{
		"SuperinterfaceInit":                superinterfaceInitClass,
		"SuperinterfaceInit$Base":           superinterfaceInitBaseClass,
		"SuperinterfaceInit$Implementation": superinterfaceInitImplementationClass,
	}, Options{})
	result, err := vm.InvokeStatic("SuperinterfaceInit", "order", "()Ljava/lang/String;")
	if err != nil {
		t.Fatalf("order() error = %v", err)
	}
	object, _ := result.Reference()
	if order, _ := StringText(object); order != "BC" {
		t.Fatalf("initialization order = %q, want %q", order, "BC")
	}
}
