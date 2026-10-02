package jvm

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"math"
	"testing"
)

//go:embed testdata/HeapCheckpointProbe.class
var heapCheckpointClass []byte

func TestHeapStateResumesAuthoredJavaWithoutInitializationReplay(t *testing.T) {
	source := mapClassSource{"HeapCheckpointProbe": heapCheckpointClass}
	vm := New(source, Options{})
	if _, err := vm.InvokeStatic("HeapCheckpointProbe", "prepare", "()V"); err != nil {
		t.Fatal(err)
	}
	if _, err := vm.InvokeStatic("HeapCheckpointProbe", "tick", "()I"); err != nil {
		t.Fatal(err)
	}
	saved, err := vm.CaptureHeapState(nil, HeapCodec{})
	if err != nil {
		t.Fatal(err)
	}
	want, err := vm.InvokeStatic("HeapCheckpointProbe", "tick", "()I")
	if err != nil {
		t.Fatal(err)
	}
	fresh := New(source, Options{})
	if _, err := fresh.RestoreHeapState(saved, HeapCodec{}); err != nil {
		t.Fatal(err)
	}
	got, err := fresh.InvokeStatic("HeapCheckpointProbe", "tick", "()I")
	if err != nil || got != want {
		t.Fatalf("restored Java tick=%v, %v; uninterrupted=%v", got, err, want)
	}
	if value, _ := got.Int32(); value != 54 {
		t.Fatalf("fixture tick=%d", value)
	}
}

type checkpointArrayStorage struct{ ArrayStorage }

func (*checkpointArrayStorage) Len() int { return 2 }

type checkpointNativeLink struct{ object *Object }

func TestHeapStateUsesExplicitExternalCodecsWithoutReadingGuestArrays(t *testing.T) {
	vm := New(nil, Options{})
	array := &Object{ClassName: "[I", Native: &Array{Component: Type{Kind: TypeInt}, storage: &checkpointArrayStorage{}}}
	link := &checkpointNativeLink{object: array}
	left, right := &Object{ClassName: ObjectClass, Native: link}, &Object{ClassName: ObjectClass, Native: link}
	array.Fields = map[string]Value{"owner": ReferenceValue(left)}
	codec := HeapCodec{
		CaptureArray: func(storage ArrayStorage) (HeapExternalPayload, error) {
			if _, ok := storage.(*checkpointArrayStorage); !ok {
				t.Fatal("wrong array storage")
			}
			return HeapExternalPayload{Kind: "fixture-array", Data: []byte{42}}, nil
		},
		RestoreArray: func(component Type, length int, data HeapExternalPayload) (ArrayStorage, error) {
			if component.Kind != TypeInt || length != 2 || data.Kind != "fixture-array" || len(data.Data) != 1 || data.Data[0] != 42 {
				return nil, fmt.Errorf("invalid fixture array")
			}
			return &checkpointArrayStorage{}, nil
		},
		CaptureNative: func(native any) (HeapExternalPayload, error) {
			return HeapExternalPayload{Kind: "fixture-link", References: []*Object{native.(*checkpointNativeLink).object}}, nil
		},
		RestoreNative: func(data HeapExternalPayload) (any, error) {
			if data.Kind != "fixture-link" || len(data.References) != 1 {
				return nil, fmt.Errorf("invalid fixture link")
			}
			return &checkpointNativeLink{object: data.References[0]}, nil
		},
	}
	saved, err := vm.CaptureHeapState([]*Object{left, right}, codec)
	if err != nil {
		t.Fatal(err)
	}
	fresh := New(nil, Options{})
	roots, err := fresh.RestoreHeapState(saved, codec)
	if err != nil {
		t.Fatal(err)
	}
	if roots[0].Native != roots[1].Native || roots[0].Native == link {
		t.Fatal("external payload sharing was lost")
	}
	restoredArray := roots[0].Native.(*checkpointNativeLink).object
	owner, _ := restoredArray.Fields["owner"].Reference()
	if owner != roots[0] || restoredArray == array {
		t.Fatal("external payload cycle was lost")
	}
	old := fresh.NewString("keep")
	if err := fresh.BindAOTObject(0x1234, old); err != nil {
		t.Fatal(err)
	}
	codec.RestoreArray = func(Type, int, HeapExternalPayload) (ArrayStorage, error) {
		return nil, fmt.Errorf("fixture rejected array")
	}
	if _, err := fresh.RestoreHeapState(saved, codec); err == nil {
		t.Fatal("refused adapter was accepted")
	}
	if got, _ := fresh.AOTObjectAt(0x1234); got != old {
		t.Fatal("adapter refusal changed the destination")
	}
}

func TestHeapStateNativeListSharingAndOverlappingViewRefusal(t *testing.T) {
	vm := New(nil, Options{})
	list := []*Object{vm.NewString("visible"), vm.NewString("retained")}
	left := &Object{ClassName: ObjectClass, Native: list[:1]}
	right := &Object{ClassName: ObjectClass, Native: list[:1]}
	saved, err := vm.CaptureHeapState([]*Object{left, right}, HeapCodec{})
	if err != nil {
		t.Fatal(err)
	}
	roots, err := New(nil, Options{}).RestoreHeapState(saved, HeapCodec{})
	if err != nil {
		t.Fatal(err)
	}
	a, b := roots[0].Native.([]*Object), roots[1].Native.([]*Object)
	if len(a) != 1 || cap(a) != 2 || len(b) != 1 || cap(b) != 2 {
		t.Fatal("native list shape changed")
	}
	a[:2][1] = nil
	if b[:2][1] != nil || list[1] == nil {
		t.Fatal("native list sharing or independence changed")
	}
	right.Native = list[1:]
	if _, err := vm.CaptureHeapState([]*Object{left, right}, HeapCodec{}); err == nil {
		t.Fatal("overlapping native list views were accepted")
	}
}

func TestHeapStatePreservesCyclesAliasesAndWeakBindings(t *testing.T) {
	vm := New(nil, Options{})
	buffer := &stringBufferData{units: []uint16{'a', 0xd800, 'b'}}
	left := &Object{ClassName: ObjectClass, Native: buffer, Fields: make(map[string]Value)}
	right := &Object{ClassName: ObjectClass, Native: buffer, Fields: make(map[string]Value)}
	left.Fields["peer"], right.Fields["peer"] = ReferenceValue(right), ReferenceValue(left)
	left.Fields["nan"] = Value{kind: ValueDouble, bits: 0x7ff8000000000123}
	left.Fields["zero"] = FloatValue(float32(math.Copysign(0, -1)))
	if err := vm.BindAOTObject(0x1000, left); err != nil {
		t.Fatal(err)
	}
	if err := vm.BindAOTObject(0x2000, right); err != nil {
		t.Fatal(err)
	}
	vm.RetainAOTGraph(0x1000, []*Object{right})
	vm.ReleaseAOTObject(0x2000)
	identity := right.identity.Load()
	saved, err := vm.CaptureHeapState([]*Object{left, left, nil}, HeapCodec{})
	if err != nil {
		t.Fatal(err)
	}
	if vm.AOTObjectPinned(0x2000) {
		t.Fatal("capture pinned a released binding")
	}
	encoded, err := json.Marshal(saved)
	if err != nil {
		t.Fatal(err)
	}
	var decoded HeapState
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	fresh := New(nil, Options{})
	roots, err := fresh.RestoreHeapState(decoded, HeapCodec{})
	if err != nil {
		t.Fatal(err)
	}
	if roots[0] == left || roots[0] != roots[1] || roots[2] != nil {
		t.Fatal("heap roots lost identity")
	}
	a := roots[0]
	b, _ := a.Fields["peer"].Reference()
	back, _ := b.Fields["peer"].Reference()
	if back != a || a.Native != b.Native || a.Native == buffer || b.identity.Load() != identity {
		t.Fatal("heap graph or native sharing changed")
	}
	if fresh.AOTObjectPinned(0x2000) || !fresh.AOTObjectPinned(0x1000) {
		t.Fatal("restored binding strength changed")
	}
	if len(a.aotRetain) != 1 || a.aotRetain[0] != b {
		t.Fatal("retained guest graph changed")
	}
	if a.Fields["nan"].bits != 0x7ff8000000000123 || a.Fields["zero"].bits != 0x80000000 {
		t.Fatal("floating point bits changed")
	}
	if units := a.Native.(*stringBufferData).units; len(units) != 3 || units[1] != 0xd800 {
		t.Fatal("UTF-16 units changed")
	}
	if next := fresh.objectIdentity(&Object{}); next <= identity {
		t.Fatal("restored identity counter went backwards")
	}
}

func TestHeapStateRebuildsCooperativeThreadNotifications(t *testing.T) {
	options := Options{GuestThreadStarter: func(*Object) error { return nil }}
	vm := New(nil, options)
	thread, err := vm.NewObject(ThreadClass, "()V")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := vm.InvokeVirtual(thread, "start", "()V"); err != nil {
		t.Fatal(err)
	}
	if _, err := vm.InvokeVirtual(thread, "interrupt", "()V"); err != nil {
		t.Fatal(err)
	}
	sourceState := thread.thread.Load()
	saved, err := vm.CaptureHeapState([]*Object{thread}, HeapCodec{})
	if err != nil {
		t.Fatal(err)
	}
	if len(sourceState.wake) != 1 {
		t.Fatal("capture consumed a pending notification")
	}
	fresh := New(nil, options)
	roots, err := fresh.RestoreHeapState(saved, HeapCodec{})
	if err != nil {
		t.Fatal(err)
	}
	state := roots[0].thread.Load()
	if !state.started || !state.alive || !state.interrupted || len(state.wake) != 1 || state.wake == sourceState.wake || state.done == sourceState.done {
		t.Fatal("thread flags or fresh channels were lost")
	}
	if _, err := fresh.InvokeVirtual(roots[0], "start", "()V"); err == nil {
		t.Fatal("restored thread started twice")
	}
	fresh.EndGuestThread(roots[0])
	value, err := fresh.InvokeVirtual(roots[0], "isAlive", "()Z")
	if got, _ := value.Int32(); err != nil || got != 0 {
		t.Fatalf("completed thread alive=%d, %v", got, err)
	}
	if _, err := New(nil, Options{}).RestoreHeapState(saved, HeapCodec{}); err == nil {
		t.Fatal("thread ownership policy mismatch was accepted")
	}
}

func TestHeapStateRefusesUnknownPayloadAndPreservesDestination(t *testing.T) {
	vm := New(nil, Options{})
	object := &Object{ClassName: ObjectClass, Native: make(chan struct{})}
	if _, err := vm.CaptureHeapState([]*Object{object}, HeapCodec{}); err == nil {
		t.Fatal("unknown native payload was accepted")
	}
	object.Native = nil
	object.monitor.owner, object.monitor.depth = 1, 1
	if _, err := vm.CaptureHeapState([]*Object{object}, HeapCodec{}); err == nil {
		t.Fatal("held monitor was accepted")
	}
	object.monitor.owner, object.monitor.depth = 0, 0
	saved, err := vm.CaptureHeapState([]*Object{object}, HeapCodec{})
	if err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*HeapState){
		func(s *HeapState) { s.Version++ },
		func(s *HeapState) { s.Roots[0] = uint32(len(s.Objects) + 1) },
		func(s *HeapState) { s.Objects[0].Native = 1 },
		func(s *HeapState) { s.Objects[0].Class = "[" },
		func(s *HeapState) { s.NextObject = ^uint32(0) },
	} {
		data, _ := json.Marshal(saved)
		var bad HeapState
		if err := json.Unmarshal(data, &bad); err != nil {
			t.Fatal(err)
		}
		change(&bad)
		fresh := New(nil, Options{})
		old := fresh.NewString("destination")
		if err := fresh.BindAOTObject(0x4000, old); err != nil {
			t.Fatal(err)
		}
		before := fresh.nextObject.Load()
		if _, err := fresh.RestoreHeapState(bad, HeapCodec{}); err == nil {
			t.Fatal("invalid heap accepted")
		}
		got, _ := fresh.AOTObjectAt(0x4000)
		if got != old || fresh.nextObject.Load() != before {
			t.Fatal("refusal changed the destination")
		}
	}
}
