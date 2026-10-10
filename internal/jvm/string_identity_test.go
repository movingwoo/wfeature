package jvm

import (
	_ "embed"
	"encoding/json"
	"testing"
)

//go:embed testdata/StringIdentity.class
var stringIdentityClass []byte

//go:embed testdata/StringIdentity$Peer.class
var stringIdentityPeerClass []byte

func stringIdentityVM(t *testing.T) *VM {
	t.Helper()
	vm := New(mapClassSource{"StringIdentity": stringIdentityClass, "StringIdentity$Peer": stringIdentityPeerClass}, Options{})
	t.Cleanup(vm.Close)
	return vm
}

func identityString(t *testing.T, vm *VM, method string) *Object {
	t.Helper()
	value, err := vm.InvokeStatic("StringIdentity", method, "()Ljava/lang/String;")
	if err != nil {
		t.Fatal(err)
	}
	object, err := value.Reference()
	if err != nil || object == nil {
		t.Fatalf("%s = %v, %v", method, object, err)
	}
	return object
}

func requireStringIdentity(t *testing.T, vm *VM, method string) {
	t.Helper()
	value, err := vm.InvokeStatic("StringIdentity", method, "()Z")
	if err != nil {
		t.Fatal(err)
	}
	if got, err := value.Int32(); err != nil || got != 1 {
		t.Fatalf("%s = %v, %v; want true", method, got, err)
	}
}

func TestStringLiteralIdentityAcrossLoadsClassesAndConstants(t *testing.T) {
	vm := stringIdentityVM(t)
	first := identityString(t, vm, "literal")
	if identityString(t, vm, "literal") != first || identityString(t, vm, "peerLiteral") != first {
		t.Fatal("equal literals did not share their object")
	}
	value, err := vm.StaticField("StringIdentity", "CONSTANT", "Ljava/lang/String;")
	if err != nil {
		t.Fatal(err)
	}
	constant, _ := value.Reference()
	if constant != first {
		t.Fatal("ConstantValue field did not share its literal")
	}
	if err := vm.DefineClass(ClassDefinition{Name: "NativeStringConstant", SuperName: ObjectClass, Access: AccessPublic,
		Fields: []FieldDefinition{{Name: "EVENT", Descriptor: "Ljava/lang/String;", Access: AccessPublic | AccessStatic | AccessFinal, Constant: StringValue("started")}},
	}); err != nil {
		t.Fatal(err)
	}
	value, err = vm.StaticField("NativeStringConstant", "EVENT", "Ljava/lang/String;")
	if err != nil {
		t.Fatal(err)
	}
	constant, _ = value.Reference()
	if constant != first {
		t.Fatal("native constant did not share its literal")
	}
	if vm.NewString("started") == first || identityString(t, stringIdentityVM(t), "literal") == first {
		t.Fatal("constructed strings or different VMs shared literal identity")
	}
}

func TestStringInternRetainsCanonicalReceiver(t *testing.T) {
	vm := stringIdentityVM(t)
	requireStringIdentity(t, vm, "internBeforeLiteral")
	requireStringIdentity(t, vm, "internLiteral")
}

func TestStringIdentitySurvivesHeapRestore(t *testing.T) {
	vm := stringIdentityVM(t)
	if _, err := vm.InvokeStatic("StringIdentity", "retain", "()V"); err != nil {
		t.Fatal(err)
	}
	requireStringIdentity(t, vm, "retainedIdentity")
	saved, err := vm.CaptureHeapState(nil, HeapCodec{})
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(saved)
	if err != nil {
		t.Fatal(err)
	}
	var decoded HeapState
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	fresh := stringIdentityVM(t)
	// Bootstrapping the destination must not override the restored pool.
	old := identityString(t, fresh, "literal")
	if _, err := fresh.RestoreHeapState(decoded, HeapCodec{}); err != nil {
		t.Fatal(err)
	}
	requireStringIdentity(t, fresh, "retainedIdentity")
	if identityString(t, fresh, "literal") == old {
		t.Fatal("restore retained a bootstrap string")
	}
	second, err := fresh.CaptureHeapState(nil, HeapCodec{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := stringIdentityVM(t).RestoreHeapState(second, HeapCodec{}); err != nil {
		t.Fatal(err)
	}
}
