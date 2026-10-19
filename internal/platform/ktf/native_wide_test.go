package ktf

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/movingwoo/wfeature/internal/armcore"
	"github.com/movingwoo/wfeature/internal/jvm"
)

// nativeCallIsWideByWalk is how a native call's width was found before the
// stubs were indexed by address: by walking every stub, kept as the answer
// the index has to give.
func nativeCallIsWideByWalk(runtime *initializationRuntime, address uint32) bool {
	for key, stub := range runtime.stubs {
		if stub != address || uint32(key>>32) != svcCategoryRuntimeJava {
			continue
		}
		invocation, ok := runtime.nativeMethods[uint32(key)]
		if !ok {
			return false
		}
		methodType, err := jvm.ParseMethodDescriptor(invocation.method.descriptor)
		if err != nil {
			return false
		}
		return methodType.Return.Slots() == 2
	}
	return false
}

// describeNativeCallByWalk is how a native call was named before the stubs
// were indexed by address, kept as the answer the index has to give.
func describeNativeCallByWalk(runtime *initializationRuntime, address uint32) string {
	for key, stub := range runtime.stubs {
		if stub != address || uint32(key>>32) != svcCategoryRuntimeJava {
			continue
		}
		if invocation, ok := runtime.nativeMethods[uint32(key)]; ok {
			return fmt.Sprintf("%s.%s%s at %#x", invocation.method.class, invocation.method.name, invocation.method.descriptor, address)
		}
	}
	return fmt.Sprintf("at %#x", address)
}

// A native call's width and name are found by its stub's address, and answer
// as the walk over every stub did: for natives answering a long, a double, an
// int and nothing, for a stub of another category at an address of its own,
// for addresses that are no stub, and for a runtime restored from a
// checkpoint, whose stubs come back without the stub calls that made them.
func TestANativeCallsWidthIsFoundByItsStub(t *testing.T) {
	client, runtime := newTestRuntime(t)
	var addresses []uint32
	for _, descriptor := range []string{"()J", "(I)I", "(J)D", "(JJ)V", "()Ljava/lang/String;"} {
		stub, err := runtime.runtimeJavaStub(runtimeJavaMethod{class: "probe/Wide", name: "call", descriptor: descriptor, accessFlags: 9}, false)
		if err != nil {
			t.Fatal(err)
		}
		addresses = append(addresses, stub, stub+2, stub&^1)
	}
	other, err := runtime.stub(svcCategoryRuntimeJava+1, 7)
	if err != nil {
		t.Fatal(err)
	}
	addresses = append(addresses, other, 0, ^uint32(0))
	want := map[uint32]bool{}
	for _, address := range addresses {
		want[address] = nativeCallIsWideByWalk(runtime, address)
		if got := runtime.nativeCallIsWide(address); got != want[address] {
			t.Fatalf("nativeCallIsWide(%#x) = %t, the walk over every stub said %t", address, got, want[address])
		}
		if got, walked := runtime.describeNativeCall(address), describeNativeCallByWalk(runtime, address); got != walked {
			t.Fatalf("describeNativeCall(%#x) = %q, the walk over every stub said %q", address, got, walked)
		}
	}
	if got := runtime.describeNativeCall(addresses[0]); got != fmt.Sprintf("probe/Wide.call()J at %#x", addresses[0]) {
		t.Fatalf("describeNativeCall() = %q", got)
	}
	if !want[addresses[0]] || want[addresses[3]] || !want[addresses[6]] || want[addresses[9]] {
		t.Fatalf("the widths are not the descriptors' own: %v", want)
	}

	saved, err := runtime.captureMetadataState()
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(saved)
	if err != nil {
		t.Fatal(err)
	}
	var decoded runtimeMetadataState
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	coreState, err := client.core.CaptureState()
	if err != nil {
		t.Fatal(err)
	}
	freshClient, fresh := newTestRuntime(t)
	if freshClient.core, err = armcore.NewCoreFromState(coreState, armcore.CoreOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := fresh.restoreMetadataState(decoded); err != nil {
		t.Fatal(err)
	}
	for _, address := range addresses {
		if got := fresh.nativeCallIsWide(address); got != want[address] {
			t.Fatalf("after a restore nativeCallIsWide(%#x) = %t, want %t", address, got, want[address])
		}
	}
	// A stub made after the restore is indexed too.
	stub, err := fresh.runtimeJavaStub(runtimeJavaMethod{class: "probe/Wide", name: "later", descriptor: "()J", accessFlags: 9}, false)
	if err != nil {
		t.Fatal(err)
	}
	if !fresh.nativeCallIsWide(stub) {
		t.Fatal("a stub made after the restore is not found")
	}
}
