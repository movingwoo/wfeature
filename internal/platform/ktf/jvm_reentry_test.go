package ktf

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/movingwoo/wfeature/internal/armcore"
	"github.com/movingwoo/wfeature/internal/jvm"
	"github.com/movingwoo/wfeature/internal/platform/wipicrt"
)

type reentryClassSource []byte

func (source reentryClassSource) ClassBytes(name string) ([]byte, bool) {
	return source, name == "ReentryProbe"
}

func TestBytecodeARMRoundTripPreservesInitializationMonitorAndBudget(t *testing.T) {
	class, err := os.ReadFile("testdata/ReentryProbe.class")
	if err != nil {
		t.Fatal(err)
	}
	for _, budget := range []uint64{10000, 300} {
		client, runtime := newTestRuntime(t)
		client.vm = jvm.New(reentryClassSource(class), jvm.Options{MaxSteps: budget})
		client.vm.SetContextAOTInvoker(runtime.invokeAOTFromJVMContext)
		stub, err := runtime.runtimeJavaStub(runtimeJavaMethod{class: "ReentryProbe", name: "inner", descriptor: "()I", accessFlags: 9}, false)
		if err != nil {
			t.Fatal(err)
		}
		address, err := runtime.Allocate(4)
		if err != nil {
			t.Fatal(err)
		}
		if err := client.vm.RegisterAOTClass(jvm.AOTClassMetadata{Address: address, Name: "ReentryBridge", SuperName: jvm.ObjectClass,
			Methods: []jvm.AOTMethodMetadata{{Address: address + 4, Name: "through", Descriptor: "()I", AccessFlags: 9, Body: stub}}}); err != nil {
			t.Fatal(err)
		}
		runtime.CurrentThread, runtime.CurrentContext = client.Thread, t.Context()
		result, err := client.vm.InvokeStatic("ReentryProbe", "outer", "()I")
		if err != nil {
			t.Fatal(err)
		}
		if got, _ := result.Int32(); got != 45 {
			t.Fatalf("round trip = %d", got)
		}
		calls, err := client.vm.StaticField("ReentryProbe", "calls", "I")
		if err != nil {
			t.Fatal(err)
		}
		if got, _ := calls.Int32(); got != 2 {
			t.Fatalf("initializer or callback repeated: %d", got)
		}
		_, err = client.vm.InvokeStatic("ReentryProbe", "spend", "()I")
		if budget == 300 && !errors.Is(err, jvm.ErrStepLimit) {
			t.Fatalf("shared instruction budget = %v", err)
		}
		if budget == 10000 && err != nil {
			t.Fatal(err)
		}
		if len(runtime.AOTCallDepth) != 0 {
			t.Fatalf("round trip leaked depth: %v", runtime.AOTCallDepth)
		}
	}
}

func TestRuntimeJavaReentryRetainsSynchronizedOwner(t *testing.T) {
	client, runtime := newTestRuntime(t)
	vm := client.JVM()
	const class = "test/Reentry"
	runtime.nativeMethods[991] = runtimeJavaInvocation{method: runtimeJavaMethod{class: class, name: "inner", descriptor: "()I", accessFlags: 0x0009}}
	if err := vm.DefineClass(jvm.ClassDefinition{Name: class, SuperName: jvm.ObjectClass, Access: jvm.AccessPublic,
		Methods: []jvm.MethodDefinition{
			{Name: "inner", Descriptor: "()I", Access: jvm.AccessPublic | jvm.AccessStatic | jvm.AccessSynchronized, Body: func(*jvm.Invocation, []jvm.Value) (jvm.Value, error) { return jvm.IntValue(42), nil }},
			{Name: "outer", Descriptor: "()I", Access: jvm.AccessPublic | jvm.AccessStatic | jvm.AccessSynchronized, Body: func(call *jvm.Invocation, _ []jvm.Value) (jvm.Value, error) {
				runtime.CurrentContext = context.WithValue(t.Context(), jvmInvocationKey{}, call)
				defer func() { runtime.CurrentContext = nil }()
				result, err := runtime.handleRuntimeJavaCall(armcore.NewThread(armcore.NewContext()), 991)
				return jvm.IntValue(int32(result)), err
			}},
		}}); err != nil {
		t.Fatal(err)
	}
	value, err := vm.InvokeStatic(class, "outer", "()I")
	got, _ := value.Int32()
	if err != nil || got != 42 {
		t.Fatalf("reentry = %d, %v", got, err)
	}
}

func TestAOTDepthLeavesTheEnteringThread(t *testing.T) {
	_, runtime := newTestRuntime(t)
	first, second := armcore.NewThread(armcore.NewContext()), armcore.NewThread(armcore.NewContext())
	runtime.CurrentThread = first
	runtime.client.activeWorker = &guestWorker{armThread: first}
	if err := runtime.EnterAOTCall(); err != nil {
		t.Fatal(err)
	}
	owner := runtime.CurrentThread
	runtime.CurrentThread = second
	runtime.client.activeWorker = &guestWorker{armThread: second}
	if err := runtime.EnterAOTCall(); err != nil {
		t.Fatal(err)
	}
	runtime.LeaveAOTCall(owner)
	if runtime.AOTCallDepth[first] != 0 || runtime.AOTCallDepth[second] != 1 {
		t.Fatalf("depth ownership = %v", runtime.AOTCallDepth)
	}
	runtime.LeaveAOTCall(second)
	for i := 0; i < int(wipicrt.MaxAOTCallDepth); i++ {
		runtime.CurrentThread = armcore.NewThread(armcore.NewContext())
		if err := runtime.EnterAOTCall(); err != nil {
			t.Fatal(err)
		}
	}
	if err := runtime.EnterAOTCall(); err == nil {
		t.Fatal("derived frames bypassed the worker depth limit")
	}
	for i := 0; i < int(wipicrt.MaxAOTCallDepth); i++ {
		runtime.LeaveAOTCall(second)
	}
}
