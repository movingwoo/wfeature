package ktf

import (
	"testing"

	"github.com/movingwoo/wfeature/internal/armcore"
	"github.com/movingwoo/wfeature/internal/jvm"
)

func TestFieldlessModuleSuperclassPreservesPayloadAndAliasAncestry(t *testing.T) {
	_, runtime := newTestRuntime(t)
	base := writeModuleClassFields(t, runtime, "test/PayloadBase", 0, "base", "I", 0, 8)
	middle := writeGuestClass(t, runtime, "test/Fieldless", base, nil, 0x21)
	if err := runtime.writeModuleInstanceSize(middle, 0); err != nil {
		t.Fatal(err)
	}
	leaf := writeModuleClassFields(t, runtime, "test/PayloadLeaf", middle, "leaf", "I", 0, 4)
	runtime.moduleClassByName = map[string]uint32{"test/PayloadBase": base, "test/Fieldless": middle, "test/PayloadLeaf": leaf}
	if err := runtime.linkModuleClasses(); err != nil {
		t.Fatal(err)
	}
	mid, _ := runtime.client.vm.AOTClassAt(middle)
	metadata, _ := runtime.client.vm.AOTClassAt(leaf)
	if mid.InstanceSize != 8 || metadata.InstanceSize != 12 || metadata.Fields[0].Offset != 8 {
		t.Fatalf("payload sizes: middle %d, leaf %d, field %d", mid.InstanceSize, metadata.InstanceSize, metadata.Fields[0].Offset)
	}
	object, err := runtime.client.vm.NewAOTInstance(leaf)
	if err != nil {
		t.Fatal(err)
	}
	address, err := runtime.allocateAOTObject(metadata, make([]byte, metadata.InstanceSize), object)
	if err != nil {
		t.Fatal(err)
	}
	payload := address + javaInstanceSize + javaInstanceHeader
	writeWord(t, runtime, payload, 17)
	writeWord(t, runtime, payload+metadata.Fields[0].Offset, 42)
	if got, _ := runtime.readWord(payload); got != 17 {
		t.Fatal("child field overwrote ancestor payload")
	}
	header, err := runtime.readWord(address + javaInstanceSize)
	if err != nil {
		t.Fatal(err)
	}
	alias := runtime.jvmContext + header>>5
	canonical, ok := runtime.client.vm.AOTClassAt(alias)
	if !ok || canonical.Address != leaf {
		t.Fatalf("dispatch alias %#x lost canonical class", alias)
	}
	if checkGuestType(t, runtime, base, address) != 1 || checkGuestType(t, runtime, middle, address) != 1 {
		t.Fatal("alias ancestry lost a superclass")
	}
}

func TestModuleHelperStacksRemainPrivateWhileWorkersExist(t *testing.T) {
	client, runtime := newTestRuntime(t)
	context, err := runtime.allocate(moduleContextSize)
	if err != nil {
		t.Fatal(err)
	}
	runtime.moduleContext = context
	if err := runtime.registerModuleThreadWords(); err != nil {
		t.Fatal(err)
	}
	first, second := armcore.NewThread(armcore.NewContext()), armcore.NewThread(armcore.NewContext())
	if err := runtime.prepareModuleThread(first); err != nil {
		t.Fatal(err)
	}
	firstTop, err := client.core.ThreadLocalWord(first, context+moduleContextStack)
	if err != nil {
		t.Fatal(err)
	}
	writeWord(t, runtime, firstTop-4, 0x12345678)
	if err := runtime.prepareModuleThread(second); err != nil {
		t.Fatal(err)
	}
	secondTop, err := client.core.ThreadLocalWord(second, context+moduleContextStack)
	if err != nil {
		t.Fatal(err)
	}
	if firstTop == secondTop {
		t.Fatal("workers share a helper stack")
	}
	collectTwice(t, runtime)
	writeWord(t, runtime, secondTop-4, 0x87654321)
	if value, _ := runtime.readWord(firstTop - 4); value != 0x12345678 {
		t.Fatal("live helper stack was reused")
	}
}

func TestNativeArgumentContainerSurvivesNestedNativeCall(t *testing.T) {
	client, runtime := newTestRuntime(t)
	innerBody := uint32(runtime.codeCursor)
	runtime.codeCursor += 4
	// mov r0,r1; bx lr: return the argument container for inspection.
	if err := client.core.Memory().Load(innerBody, []byte{0x08, 0x46, 0x70, 0x47}); err != nil {
		t.Fatal(err)
	}
	inner := jvm.AOTMethodMetadata{Name: "inner", Descriptor: "(I)I", AccessFlags: 0x109, NativeBody: innerBody | 1}
	typeInfo, err := jvm.ParseMethodDescriptor("(I)I")
	if err != nil {
		t.Fatal(err)
	}
	if err := client.vm.RegisterNative("test/Scratch", "nested", "(I)I", func(_ *jvm.VM, args []jvm.Value) (jvm.Value, error) {
		outer, err := runtime.currentThread.Register(1)
		if err != nil {
			return jvm.VoidValue(), err
		}
		value, _, err := runtime.runAOTMethod(runtime.currentContext, runtime.currentThread, inner, typeInfo, []uint32{99})
		if err != nil {
			return jvm.VoidValue(), err
		}
		innerAddress, _ := value.Int32()
		if uint32(innerAddress) == outer {
			t.Fatal("nested call reused its caller's arguments")
		}
		if got, _ := runtime.readWord(outer); got != 55 {
			t.Fatal("nested call changed its caller's argument")
		}
		if got, _ := runtime.readWord(uint32(innerAddress)); got != 99 {
			t.Fatal("inner argument container was lost")
		}
		return args[0], nil
	}); err != nil {
		t.Fatal(err)
	}
	outerBody, err := runtime.runtimeJavaStub(runtimeJavaMethod{class: "test/Scratch", name: "nested", descriptor: "(I)I", accessFlags: 9}, true)
	if err != nil {
		t.Fatal(err)
	}
	outer := jvm.AOTMethodMetadata{Name: "outer", Descriptor: "(I)I", AccessFlags: 0x109, NativeBody: outerBody}
	runtime.currentThread, runtime.currentContext = client.thread, t.Context()
	value, _, err := runtime.runAOTMethod(t.Context(), client.thread, outer, typeInfo, []uint32{55})
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := value.Int32(); got != 55 {
		t.Fatalf("native round trip = %d", got)
	}
}
