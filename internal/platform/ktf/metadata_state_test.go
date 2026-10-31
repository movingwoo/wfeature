package ktf

import (
	"encoding/json"
	"reflect"
	goruntime "runtime"
	"testing"

	"github.com/movingwoo/wfeature/internal/armcore"
	"github.com/movingwoo/wfeature/internal/jvm"
	"github.com/movingwoo/wfeature/internal/platform/wipicrt"
)

func TestRuntimeMetadataContinuesDispatchAllocationAndCollection(t *testing.T) {
	client, runtime := newTestRuntime(t)
	method := runtimeJavaMethod{class: "java/lang/Math", name: "abs", descriptor: "(I)I", accessFlags: 9}
	stub, err := runtime.runtimeJavaStub(method, false)
	if err != nil {
		t.Fatal(err)
	}
	methodID := runtime.nextNativeMethod
	class, err := runtime.ensureJavaClass(jvm.ObjectClass)
	if err != nil {
		t.Fatal(err)
	}
	objectAddress, object, err := runtime.allocateAOTInstance(class)
	if err != nil {
		t.Fatal(err)
	}
	runtime.initializedClasses = map[uint32]bool{class: true}
	runtime.loadedClasses = map[string]uint32{"saved/Object": class, "saved/\xff": class}
	runtime.classSummaries = map[uint32]aotClassSummary{class: {name: jvm.ObjectClass}}
	runtime.objects[objectAddress] = objectRecord{size: runtime.objects[objectAddress].size, released: true}
	client.vm.ReleaseAOTObject(objectAddress)
	runtime.CollectAt = 123456
	first, err := runtime.AllocateWIPIC(17)
	if err != nil {
		t.Fatal(err)
	}
	second, err := runtime.AllocateWIPIC(29)
	if err != nil {
		t.Fatal(err)
	}
	runtime.FreeWIPIC(first)
	// Reuse leaves older order entries in place, including duplicates. The
	// diagnostic set and its eviction order must be restored independently.
	if reused, err := runtime.AllocateWIPIC(17); err != nil || reused != first {
		t.Fatalf("reuse = %#x, %v", reused, err)
	}
	runtime.FreeWIPIC(first)
	runtime.ImageSourceBuffers = map[uint32]uint32{second: second}
	runtime.UserMemoryPools = map[uint32]*wipicrt.UserMemoryPool{second + 12: {Base: second + 12, Size: 29, Cursor: 3, FreeCalls: 7}}
	runtime.PixelOps = &wipicrt.PixelOpCache{Function: stub, Param: 81, Results: map[uint32]uint16{0x11112222: 0x4321}}
	runtime.Leds = 5
	runtime.menuTextCompatibility = true
	heap, err := runtime.captureHeapState([]*jvm.Object{object})
	if err != nil {
		t.Fatal(err)
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
	coreState, err := client.Core().CaptureState()
	if err != nil {
		t.Fatal(err)
	}
	freshClient, fresh := newTestRuntime(t)
	restoredCore, err := armcore.NewCoreFromState(coreState, armcore.CoreOptions{})
	freshClient.AttachCore(restoredCore, freshClient.Thread)
	if err != nil {
		t.Fatal(err)
	}
	if err := fresh.restoreMetadataState(decoded); err != nil {
		t.Fatal(err)
	}
	roots, err := fresh.restoreHeapState(heap)
	if err != nil {
		t.Fatal(err)
	}
	recaptured, err := fresh.captureMetadataState()
	if err != nil || !reflect.DeepEqual(saved, recaptured) {
		t.Fatalf("metadata recapture differs: %v", err)
	}
	thread := armcore.NewThread(armcore.NewContext())
	if err := thread.SetRegister(1, 0xffffffdb); err != nil {
		t.Fatal(err)
	}
	if value, err := fresh.handleRuntimeJavaCall(thread, methodID); err != nil || value != 37 {
		t.Fatalf("restored native dispatch = %d, %v", value, err)
	}
	if value, err := fresh.ApplyPixelOp(wipicrt.WIPICPixelOp{Function: stub, Param: 81}, 0x1111, 0x2222); err != nil || value != 0x4321 || freshClient.Core().Steps() != 0 {
		t.Fatalf("restored pixel cache = %#x, %v", value, err)
	}
	for _, target := range []*initializationRuntime{runtime, fresh} {
		if got, err := target.AllocateWIPIC(17); err != nil || got != first {
			t.Fatalf("next WIPI allocation = %#x, %v; want %#x", got, err, first)
		}
		if err := thread.SetRegister(0, second+12); err != nil {
			t.Fatal(err)
		}
		if err := thread.SetRegister(1, 5); err != nil {
			t.Fatal(err)
		}
		if got, err := target.HandleUserMemoryCall(thread, wipicrt.UserMemoryAlloc); err != nil || got != second+20 {
			t.Fatalf("next pool allocation = %#x, %v", got, err)
		}
		if got, err := target.runtimeJavaStub(method, true); err != nil || got != uint32(saved.CodeCursor)|1 || target.nextNativeMethod != saved.NextNativeMethod+1 {
			t.Fatalf("next native stub = %#x, %v", got, err)
		}
	}
	if stats, err := fresh.collectGuestObjects([]uint32{objectAddress}); err != nil || stats.Tracked != 1 || stats.Marked != 1 || stats.Lost != 0 || fresh.objects[objectAddress].released {
		t.Fatalf("restored collector = %+v, %v", stats, err)
	}
	if !runtime.objects[objectAddress].released || runtime.PixelOps == fresh.PixelOps || runtime.UserMemoryPools[second+12] == fresh.UserMemoryPools[second+12] {
		t.Fatal("restored metadata shares mutable source state")
	}
	goruntime.KeepAlive(roots)
}

func TestRuntimeMetadataRestoresModuleClassLinksWithoutEntry(t *testing.T) {
	client := loadSyntheticModule(t)
	if _, err := client.ExecuteModuleEntry(t.Context()); err != nil {
		t.Fatal(err)
	}
	runtime := client.runtime
	parent := writeModuleClass(t, runtime, "saved/Base", 0, "tick", "()V", 5)
	child := writeModuleClass(t, runtime, "saved/Child", parent, "draw", "()V", 7)
	runtime.moduleClassByName = map[string]uint32{"saved/Base": parent, "saved/Child": child}
	if err := runtime.linkModuleClasses(); err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.aotClassSummary(child); err != nil {
		t.Fatal(err)
	}
	saved, err := runtime.captureMetadataState()
	if err != nil {
		t.Fatal(err)
	}
	if saved.ClassArena != nil || saved.Bindings.Module == 0 {
		t.Fatal("fixture did not use the older module layout")
	}
	heap, err := runtime.captureHeapState(nil)
	if err != nil {
		t.Fatal(err)
	}
	coreState, err := client.Core().CaptureState()
	if err != nil {
		t.Fatal(err)
	}
	freshClient := loadSyntheticModule(t)
	fresh, err := newInitializationRuntime(freshClient)
	if err != nil {
		t.Fatal(err)
	}
	freshClient.setRuntimes(fresh, freshClient.prepared)
	restoredCore, err := armcore.NewCoreFromState(coreState, armcore.CoreOptions{MaxSteps: 1000})
	if err != nil {
		t.Fatal(err)
	}
	freshClient.AttachCore(restoredCore, freshClient.Thread)
	if err := fresh.restoreMetadataState(saved); err != nil {
		t.Fatal(err)
	}
	if _, err := fresh.restoreHeapState(heap); err != nil {
		t.Fatal(err)
	}
	if got, err := fresh.resolveModuleClass("saved/Child", 0); err != nil || got != child {
		t.Fatalf("restored module class = %#x, %v", got, err)
	}
	if got := freshClient.Core().Steps(); got != client.Core().Steps() {
		t.Fatalf("module restore or lookup executed additional guest instructions: %d", got)
	}
	if after, err := fresh.captureMetadataState(); err != nil || !reflect.DeepEqual(saved, after) {
		t.Fatalf("module lookup changed its restored links or allocations: %v", err)
	}
}

func TestRuntimeMetadataRejectsMalformedStateBeforeAdoption(t *testing.T) {
	_, runtime := newTestRuntime(t)
	if _, err := runtime.runtimeJavaStub(runtimeJavaMethod{class: "java/lang/Math", name: "abs", descriptor: "(I)I", accessFlags: 9}, false); err != nil {
		t.Fatal(err)
	}
	allocation, err := runtime.AllocateWIPIC(17)
	if err != nil {
		t.Fatal(err)
	}
	saved, err := runtime.captureMetadataState()
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(saved)
	if err != nil {
		t.Fatal(err)
	}
	for name, change := range map[string]func(*runtimeMetadataState){
		"arena policy":       func(s *runtimeMetadataState) { s.Arena.Limit++ },
		"class arena policy": func(s *runtimeMetadataState) { s.ClassArena.Base += 4 },
		"code cursor":        func(s *runtimeMetadataState) { s.CodeCursor = uint64(wipicrt.PlatformCodeBase) - 4 },
		"stub address":       func(s *runtimeMetadataState) { s.Stubs[0].Address = 1 },
		"duplicate stub":     func(s *runtimeMetadataState) { s.Stubs = append(s.Stubs, s.Stubs[0]) },
		"native id":          func(s *runtimeMetadataState) { s.NativeMethods[0].ID = s.NextNativeMethod + 1 },
		"native descriptor":  func(s *runtimeMetadataState) { s.NativeMethods[0].Descriptor = []byte("(I") },
		"allocation size":    func(s *runtimeMetadataState) { s.WIPICAllocations[0].Size = 1 << 32 },
		"allocation overlaps free space": func(s *runtimeMetadataState) {
			s.Arena.Free = []arenaBlockState{{Start: uint64(allocation), End: uint64(allocation) + 4}}
			s.Arena.Freed = 4
		},
		"overlapping allocations": func(s *runtimeMetadataState) { s.Objects = []runtimeObjectState{{Address: allocation, Size: 8}} },
		"pool overflow": func(s *runtimeMetadataState) {
			s.UserMemoryPools = []runtimeUserPoolState{{Base: 0xfffffff0, Size: 32}}
		},
		"collector trigger": func(s *runtimeMetadataState) { s.CollectAt = 1 << 32 },
		"exception address": func(s *runtimeMetadataState) { s.Bindings.ExceptionContext = 0xfffffffc },
	} {
		t.Run(name, func(t *testing.T) {
			var bad runtimeMetadataState
			if err := json.Unmarshal(data, &bad); err != nil {
				t.Fatal(err)
			}
			change(&bad)
			arena := runtime.Arena
			if err := runtime.restoreMetadataState(bad); err == nil {
				t.Fatal("malformed metadata was accepted")
			}
			after, err := runtime.captureMetadataState()
			if err != nil || runtime.Arena != arena || !reflect.DeepEqual(saved, after) {
				t.Fatalf("refused state changed the target: %v", err)
			}
		})
	}
}
