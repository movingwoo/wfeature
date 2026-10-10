package jvm

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
)

func internTestVM(t *testing.T) *VM {
	t.Helper()
	vm := New(nil, Options{})
	t.Cleanup(vm.Close)
	return vm
}

func requireInternedString(t *testing.T, vm *VM, text string) *Object {
	t.Helper()
	object, err := vm.InternString(text)
	if err != nil {
		t.Fatal(err)
	}
	return object
}

func requireInternOOME(t *testing.T, vm *VM, err error) {
	t.Helper()
	if !vm.IsGuestException(err, "java/lang/OutOfMemoryError") {
		t.Fatalf("intern overflow = %v, want guest OutOfMemoryError", err)
	}
}

func internHeapRoundTrip(t *testing.T, saved HeapState) HeapState {
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

func TestStringInternConcurrentCanonicalObject(t *testing.T) {
	vm := internTestVM(t)
	const text = "concurrent event"
	const workers = 32
	var wait sync.WaitGroup
	start := make(chan struct{})
	objects := make([]*Object, workers)
	errors := make([]error, workers)
	for worker := range workers {
		wait.Go(func() {
			receiver := vm.NewString(text)
			<-start
			for attempt := range 32 {
				var object *Object
				var err error
				if (worker+attempt)%2 == 0 {
					object, err = vm.InternString(text)
				} else {
					var value Value
					value, err = vm.InvokeVirtual(receiver, "intern", "()Ljava/lang/String;")
					if err == nil {
						object, err = value.Reference()
					}
				}
				if err != nil {
					errors[worker] = err
					return
				}
				if attempt > 0 && objects[worker] != object {
					errors[worker] = fmt.Errorf("canonical object changed within worker %d", worker)
					return
				}
				objects[worker] = object
			}
		})
	}
	close(start)
	wait.Wait()
	canonical := requireInternedString(t, vm, text)
	for worker, object := range objects {
		if errors[worker] != nil || object != canonical {
			t.Fatalf("worker %d: object = %p, error = %v; want %p", worker, object, errors[worker], canonical)
		}
	}
	first, second := vm.NewString(text), vm.NewString(text)
	if first == second || first == canonical || second == canonical {
		t.Fatal("NewString reused an interned or previously constructed object")
	}
	if requireInternedString(t, internTestVM(t), text) == canonical {
		t.Fatal("separate VMs shared their interned object")
	}
}

func TestStringInternCountBudget(t *testing.T) {
	vm := internTestVM(t)
	var first *Object
	for index := range maxInternedStrings {
		object := requireInternedString(t, vm, strconv.Itoa(index))
		if index == 0 {
			first = object
		}
	}
	object, err := vm.InternString("one too many")
	requireInternOOME(t, vm, err)
	if object != nil {
		t.Fatal("failed interning returned an object")
	}
	if requireInternedString(t, vm, "0") != first {
		t.Fatal("full pool lost an existing canonical object")
	}
	receiver := vm.NewString("another new value")
	_, err = vm.InvokeVirtual(receiver, "intern", "()Ljava/lang/String;")
	requireInternOOME(t, vm, err)
	value, err := vm.InvokeVirtual(first, "intern", "()Ljava/lang/String;")
	if err != nil {
		t.Fatal(err)
	}
	if object, _ := value.Reference(); object != first {
		t.Fatal("String.intern refused an existing string at the count limit")
	}
}

func TestStringInternTextBudgetSurvivesRestore(t *testing.T) {
	vm := internTestVM(t)
	firstText := strings.Repeat("a", maxInternedStringBytes/2)
	secondText := strings.Repeat("z", maxInternedStringBytes-len(firstText))
	first := requireInternedString(t, vm, firstText)
	second := requireInternedString(t, vm, secondText)
	_, err := vm.InternString("x")
	requireInternOOME(t, vm, err)
	if requireInternedString(t, vm, firstText) != first || requireInternedString(t, vm, secondText) != second {
		t.Fatal("full text budget prevented an existing lookup")
	}
	saved, err := vm.CaptureHeapState([]*Object{first, second}, HeapCodec{})
	if err != nil {
		t.Fatal(err)
	}
	fresh := internTestVM(t)
	roots, err := fresh.RestoreHeapState(saved, HeapCodec{})
	if err != nil {
		t.Fatal(err)
	}
	if requireInternedString(t, fresh, firstText) != roots[0] || requireInternedString(t, fresh, secondText) != roots[1] {
		t.Fatal("restored full pool lost an existing canonical object")
	}
	_, err = fresh.InternString("x")
	requireInternOOME(t, fresh, err)
	_, err = fresh.InvokeVirtual(fresh.NewString("y"), "intern", "()Ljava/lang/String;")
	requireInternOOME(t, fresh, err)
}

func TestStringInternEmptyAndNULHeapRoundTrip(t *testing.T) {
	vm := internTestVM(t)
	texts := []string{"z", "a\x00b", "", "\x00", "a"}
	for _, text := range texts {
		requireInternedString(t, vm, text)
	}
	empty := requireInternedString(t, vm, "")
	nul := requireInternedString(t, vm, "a\x00b")
	copy := vm.NewString("a\x00b")
	saved, err := vm.CaptureHeapState([]*Object{nul, nil, copy, empty, nul}, HeapCodec{})
	if err != nil {
		t.Fatal(err)
	}
	fresh := internTestVM(t)
	roots, err := fresh.RestoreHeapState(internHeapRoundTrip(t, saved), HeapCodec{})
	if err != nil {
		t.Fatal(err)
	}
	if len(roots) != 5 || roots[0] == nul || roots[0] != roots[4] || roots[1] != nil || roots[2] == roots[0] {
		t.Fatal("intern restoration changed explicit root order or constructed-string identity")
	}
	if requireInternedString(t, fresh, "") != roots[3] || requireInternedString(t, fresh, "a\x00b") != roots[0] {
		t.Fatal("empty or NUL string lost its canonical object")
	}
	for _, text := range texts {
		object := requireInternedString(t, fresh, text)
		if got, ok := StringText(object); !ok || got != text {
			t.Fatalf("restored text = %q, %v; want %q", got, ok, text)
		}
	}
	after, err := fresh.CaptureHeapState(roots, HeapCodec{})
	if err != nil {
		t.Fatal(err)
	}
	if after.NextObject != saved.NextObject || len(after.InternedStrings) != len(texts) {
		t.Fatal("looking up restored pool-only strings allocated replacement objects")
	}
}

func TestStringInternHeapRejectsMalformedPoolWithoutMutation(t *testing.T) {
	source := internTestVM(t)
	requireInternedString(t, source, "alpha")
	requireInternedString(t, source, "omega")
	saved, err := source.CaptureHeapState(nil, HeapCodec{})
	if err != nil {
		t.Fatal(err)
	}
	firstObject := func(state *HeapState) *HeapObjectState {
		return &state.Objects[state.InternedStrings[0]-1]
	}
	firstPayload := func(state *HeapState) *HeapPayloadState {
		return &state.Payloads[firstObject(state).Native-1]
	}
	tests := []struct {
		name   string
		mutate func(*HeapState)
	}{
		{"null reference", func(state *HeapState) { state.InternedStrings[0] = 0 }},
		{"out of range reference", func(state *HeapState) { state.InternedStrings[0] = uint32(len(state.Objects) + 1) }},
		{"non-string object", func(state *HeapState) { firstObject(state).Class = ObjectClass }},
		{"missing identity", func(state *HeapState) { firstObject(state).Identity = 0 }},
		{"missing payload", func(state *HeapState) { firstObject(state).Native = 0 }},
		{"out of range payload", func(state *HeapState) { firstObject(state).Native = uint32(len(state.Payloads) + 1) }},
		{"non-string payload", func(state *HeapState) {
			firstPayload(state).Kind = "int32"
			firstPayload(state).Data = []byte{1, 0, 0, 0}
		}},
		{"duplicate object", func(state *HeapState) { state.InternedStrings[1] = state.InternedStrings[0] }},
		{"duplicate text", func(state *HeapState) {
			second := state.Objects[state.InternedStrings[1]-1]
			state.Payloads[second.Native-1].Data = []byte("alpha")
		}},
		{"reversed order", func(state *HeapState) {
			state.InternedStrings[0], state.InternedStrings[1] = state.InternedStrings[1], state.InternedStrings[0]
		}},
		{"count limit", func(state *HeapState) {
			// Every entry is otherwise valid and unique, so duplicate checks
			// cannot conceal a missing count bound.
			count := maxInternedStrings + 1
			state.Objects = make([]HeapObjectState, count)
			state.Payloads = make([]HeapPayloadState, count)
			state.InternedStrings = make([]uint32, count)
			state.NextObject = uint32(count)
			for index := range count {
				ref := uint32(index + 1)
				state.Objects[index] = HeapObjectState{Class: StringClass, Identity: ref, Native: ref}
				state.Payloads[index] = HeapPayloadState{Kind: "string", Data: []byte(fmt.Sprintf("%05d", index))}
				state.InternedStrings[index] = ref
			}
		}},
		{"text limit", func(state *HeapState) {
			firstPayload(state).Data = []byte(strings.Repeat("a", maxInternedStringBytes+1))
		}},
		{"aggregate text limit", func(state *HeapState) {
			firstPayload(state).Data = []byte(strings.Repeat("a", maxInternedStringBytes/2))
			second := state.Objects[state.InternedStrings[1]-1]
			state.Payloads[second.Native-1].Data = []byte(strings.Repeat("z", maxInternedStringBytes/2+1))
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			damaged := internHeapRoundTrip(t, saved)
			test.mutate(&damaged)
			destination := internTestVM(t)
			retained := requireInternedString(t, destination, "retained")
			constructed := destination.NewString("retained")
			if err := destination.BindAOTObject(0x4000, constructed); err != nil {
				t.Fatal(err)
			}
			before, err := destination.CaptureHeapState([]*Object{retained, constructed}, HeapCodec{})
			if err != nil {
				t.Fatal(err)
			}
			if roots, err := destination.RestoreHeapState(damaged, HeapCodec{}); err == nil || roots != nil {
				t.Fatalf("malformed pool returned roots=%v, error=%v", roots, err)
			}
			if requireInternedString(t, destination, "retained") != retained {
				t.Fatal("refused restore changed the destination's canonical object")
			}
			after, err := destination.CaptureHeapState([]*Object{retained, constructed}, HeapCodec{})
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(before, after) {
				t.Fatal("refused restore changed destination heap state or counters")
			}
		})
	}
}
