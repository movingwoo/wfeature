package jvm

import (
	_ "embed"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"slices"
	"strings"
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
	buffer := &stringBufferData{units: []uint16{'a', 0xd800, 'b'}, capacity: 21}
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
	if capacity := a.Native.(*stringBufferData).capacity; capacity != 21 {
		t.Fatalf("buffer capacity = %d, want the 21 it was captured with", capacity)
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

// A platform's defined constant holds the value this build defines after a
// heap restore, whatever the snapshot carried: a quick save from an earlier
// build restored that build's numbers, or none for a constant it did not have.
func TestDefinedConstantsHoldThisBuildsValuesAfterARestore(t *testing.T) {
	const class = "test/SoftKeys"
	define := func(vm *VM, constants []FieldDefinition) {
		t.Helper()
		if err := vm.DefineClass(ClassDefinition{Name: class, SuperName: ObjectClass, Access: AccessPublic, Fields: constants}); err != nil {
			t.Fatal(err)
		}
	}
	constant := func(name string, value int32) FieldDefinition {
		return FieldDefinition{Name: name, Descriptor: "I", Access: AccessPublic | AccessStatic | AccessFinal, Constant: IntValue(value)}
	}
	earlier := New(nil, Options{})
	define(earlier, []FieldDefinition{constant("SOFT1", 21), constant("SOFT2", 22)})
	// Read as a title reads it, which initializes the class.
	if _, err := earlier.StaticField(class, "SOFT1", "I"); err != nil {
		t.Fatal(err)
	}
	saved, err := earlier.CaptureHeapState(nil, HeapCodec{})
	if err != nil {
		t.Fatal(err)
	}
	current := New(nil, Options{})
	define(current, []FieldDefinition{constant("SOFT1", 90), constant("SOFT2", 91), constant("SOFT3", 92)})
	if _, err := current.RestoreHeapState(saved, HeapCodec{}); err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]int32{"SOFT1": 90, "SOFT2": 91, "SOFT3": 92} {
		value, err := current.StaticField(class, name, "I")
		if err != nil {
			t.Fatal(err)
		}
		if got, _ := value.Int32(); got != want {
			t.Errorf("%s = %d after the restore, want %d", name, got, want)
		}
	}
}

// A Class object a title kept — in a field, or as the lock it synchronizes on —
// is still the one getClass answers after a heap restore. The table of class
// objects is not part of the saved heap; it is rebuilt from the objects the
// heap holds.
func TestClassObjectsStayCanonicalAcrossHeapRestore(t *testing.T) {
	source := libraryContractsSource()
	vm := New(source, Options{})
	key, err := vm.NewObject("LibraryContracts$Key", "(I)V", IntValue(3))
	if err != nil {
		t.Fatal(err)
	}
	classValue, err := vm.InvokeVirtual(key, "getClass", "()Ljava/lang/Class;")
	if err != nil {
		t.Fatal(err)
	}
	class, err := classValue.Reference()
	if err != nil || class == nil {
		t.Fatalf("getClass() = %v/%v", class, err)
	}
	saved, err := vm.CaptureHeapState([]*Object{key, class}, HeapCodec{})
	if err != nil {
		t.Fatal(err)
	}
	fresh := New(source, Options{})
	roots, err := fresh.RestoreHeapState(saved, HeapCodec{})
	if err != nil {
		t.Fatal(err)
	}
	again, err := fresh.InvokeVirtual(roots[0], "getClass", "()Ljava/lang/Class;")
	if err != nil {
		t.Fatal(err)
	}
	if object, _ := again.Reference(); object != roots[1] {
		t.Fatalf("getClass() after restore = %p, want the restored class object %p", object, roots[1])
	}
}

// heapThroughJSON is a heap record as a checkpoint carries it.
func heapThroughJSON(t *testing.T, saved HeapState) HeapState {
	t.Helper()
	encoded, err := json.Marshal(saved)
	if err != nil {
		t.Fatal(err)
	}
	var decoded HeapState
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	return decoded
}

// A StringBuffer's payload carries its capacity ahead of its units. One a
// checkpoint from before capacities were kept wrote, "buffer", carries the
// units alone and still restores: the buffer is given the capacity its text
// would have grown an empty buffer to — what LGT gives a buffer it restores
// without one — and grows on from there by the same rule.
func TestHeapStateRestoresABufferWrittenBeforeCapacitiesWereKept(t *testing.T) {
	vm := New(nil, Options{})
	short := newBuffer(t, vm, "(Ljava/lang/String;)V", ReferenceValue(vm.NewString("abc")))
	roomy := newBuffer(t, vm, "()V")
	bufferCall(t, vm, roomy, "ensureCapacity", "(I)V", IntValue(100))
	long := newBuffer(t, vm, "(Ljava/lang/String;)V", ReferenceValue(vm.NewString(strings.Repeat("x", 40))))
	bufferCall(t, vm, long, "setLength", "(I)V", IntValue(40))
	saved, err := vm.CaptureHeapState([]*Object{short, roomy, long}, HeapCodec{})
	if err != nil {
		t.Fatal(err)
	}
	buffers := 0
	for _, payload := range saved.Payloads {
		if payload.Kind == heapBufferKind {
			buffers++
		}
	}
	if buffers != 3 {
		t.Fatalf("%d buffer payloads carry a capacity, want 3", buffers)
	}
	capacities := func(vm *VM, roots []*Object) []int32 {
		t.Helper()
		return []int32{
			bufferInt(t, vm, roots[0], "capacity", "()I"),
			bufferInt(t, vm, roots[1], "capacity", "()I"),
			bufferInt(t, vm, roots[2], "capacity", "()I"),
		}
	}
	current := New(nil, Options{})
	roots, err := current.RestoreHeapState(heapThroughJSON(t, saved), HeapCodec{})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := capacities(current, roots), []int32{19, 100, 56}; !slices.Equal(got, want) {
		t.Fatalf("restored capacities = %v, want the captured %v", got, want)
	}

	earlier := heapThroughJSON(t, saved)
	for index := range earlier.Payloads {
		if payload := &earlier.Payloads[index]; payload.Kind == heapBufferKind {
			payload.Kind, payload.Data = "buffer", payload.Data[4:]
		}
	}
	fresh := New(nil, Options{})
	roots, err = fresh.RestoreHeapState(earlier, HeapCodec{})
	if err != nil {
		t.Fatalf("a buffer from before capacities were kept: %v", err)
	}
	if got, want := capacities(fresh, roots), []int32{16, 16, 40}; !slices.Equal(got, want) {
		t.Fatalf("capacities without a record = %v, want what the text grows an empty buffer to: %v", got, want)
	}
	if got := bufferText(t, fresh, roots[0]); got != "abc" {
		t.Fatalf("the restored text = %q", got)
	}
	bufferCall(t, fresh, roots[2], "append", "(C)Ljava/lang/StringBuffer;", IntValue('!'))
	if got := bufferInt(t, fresh, roots[2], "capacity", "()I"); got != 82 {
		t.Fatalf("growth after the restore = %d, want (40+1)*2", got)
	}
}

// A buffer payload capture could not have written is refused: a capacity that
// does not hold the units, a negative one, and a record too short to carry
// one or not a whole number of units.
func TestHeapStateRefusesABufferCapacityCaptureCannotWrite(t *testing.T) {
	vm := New(nil, Options{})
	buffer := newBuffer(t, vm, "(Ljava/lang/String;)V", ReferenceValue(vm.NewString("abc")))
	saved, err := vm.CaptureHeapState([]*Object{buffer}, HeapCodec{})
	if err != nil {
		t.Fatal(err)
	}
	for name, damage := range map[string]func([]byte) []byte{
		"short of its units": func(data []byte) []byte { binary.LittleEndian.PutUint32(data, 2); return data },
		"negative":           func(data []byte) []byte { binary.LittleEndian.PutUint32(data, 0x80000000); return data },
		"without a capacity": func([]byte) []byte { return []byte{1, 0} },
		"half a unit":        func(data []byte) []byte { return data[:len(data)-1] },
	} {
		damaged := heapThroughJSON(t, saved)
		for index := range damaged.Payloads {
			if payload := &damaged.Payloads[index]; payload.Kind == heapBufferKind {
				payload.Data = damage(payload.Data)
			}
		}
		if _, err := New(nil, Options{}).RestoreHeapState(damaged, HeapCodec{}); err == nil {
			t.Errorf("a buffer capacity %s was restored", name)
		}
	}
}

// A reader from before Reader kept a lock and InputStreamReader kept what it
// held at a mark restores with neither: the lock reads null, as it did, and a
// reset returns the stream to its own mark holding nothing — what a reader
// that was never marked held at the stream's start.
func TestHeapStateRestoresAReaderWrittenBeforeItsMarkWasKept(t *testing.T) {
	vm := New(nil, Options{})
	unmarked := readerOver(t, vm, []byte("xy"))
	readUnits(t, vm, unmarked, 1)
	// A reader marked between two whole characters held nothing at its mark,
	// so a record without what it held answers what it would have.
	marked := readerOver(t, vm, []byte("xy"))
	readUnits(t, vm, marked, 1)
	streamCall(t, vm, marked, "mark", "(I)V", IntValue(4))
	readUnits(t, vm, marked, 1)
	saved, err := vm.CaptureHeapState([]*Object{unmarked, marked}, HeapCodec{})
	if err != nil {
		t.Fatal(err)
	}
	earlier := heapThroughJSON(t, saved)
	dropped := map[string]int{}
	for index := range earlier.Objects {
		object := &earlier.Objects[index]
		kept := object.Fields[:0]
		for _, field := range object.Fields {
			if strings.Contains(field.Name, ".lock:") || strings.Contains(field.Name, ".marked") {
				dropped[field.Name[strings.LastIndexByte(field.Name, '.')+1:]]++
				continue
			}
			kept = append(kept, field)
		}
		object.Fields = kept
	}
	if dropped["lock:Ljava/lang/Object;"] != 2 || dropped["markedByte:I"] != 1 || dropped["markedChar:C"] != 1 {
		t.Fatalf("dropped %v, want both locks and the marked reader's two fields", dropped)
	}
	fresh := New(nil, Options{})
	roots, err := fresh.RestoreHeapState(earlier, HeapCodec{})
	if err != nil {
		t.Fatal(err)
	}
	for _, restored := range roots {
		if lock, _ := fresh.Field(restored, ReaderClass, "lock", "Ljava/lang/Object;"); mustObject(lock) != nil {
			t.Fatal("an earlier reader gained a lock")
		}
		if got := streamCall(t, fresh, restored, "markSupported", "()Z"); got != IntValue(1) {
			t.Fatal("the restored reader supports no mark")
		}
		streamCall(t, fresh, restored, "reset", "()V")
	}
	if got := readUnits(t, fresh, roots[0], 3); !slices.Equal(got, []int32{'x', 'y', -1}) {
		t.Fatalf("the unmarked reader after reset = %x, want the stream from its start", got)
	}
	if got := readUnits(t, fresh, roots[1], 2); !slices.Equal(got, []int32{'y', -1}) {
		t.Fatalf("the marked reader after reset = %x, want the stream from its mark", got)
	}
}
