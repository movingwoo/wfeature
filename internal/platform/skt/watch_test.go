package skt

import (
	_ "embed"
	"strings"
	"testing"

	"github.com/movingwoo/wfeature/internal/cheat"
	"github.com/movingwoo/wfeature/internal/jvm"
)

//go:embed testdata/Watched.class
var watchedClass []byte

// watchFixture is a runtime with one class of its own, its heap mapped, and
// the cheat target over it. It is the smallest thing that can answer "what
// wrote this address", which needs all three: an address space, an object the
// interpreter can store into, and a session holding the watch.
func watchFixture(t *testing.T) (*Runtime, cheatTarget, *jvm.Object) {
	t.Helper()
	vm := jvm.New(mapSource{"Watched": watchedClass}, jvm.Options{})
	object, err := vm.NewObject("Watched", "()V")
	if err != nil {
		t.Fatalf("NewObject() error = %v", err)
	}
	runtime := &Runtime{VM: vm}
	runtime.heap = newHeapMap(vm, func() []*jvm.Object { return []*jvm.Object{object} })
	runtime.heap.refresh()
	return runtime, cheatTarget{runtime: runtime}, object
}

type mapSource map[string][]byte

func (source mapSource) ClassBytes(name string) ([]byte, bool) {
	data, ok := source[name]
	return data, ok
}

// The panel offers write watching on this platform now, so the thing it offers
// has to work: a store from guest code lands as a hit on the address a search
// would have found, and it names the method that did it.
func TestAGuestStoreIsRecordedAgainstTheAddressItWrote(t *testing.T) {
	runtime, target, object := watchFixture(t)

	address := fieldAddressOf(t, runtime.heap, object, "gold")
	target.Watch(address)

	if _, err := runtime.VM.InvokeVirtual(object, "spend", "(I)V", jvm.IntValue(7)); err != nil {
		t.Fatalf("InvokeVirtual() error = %v", err)
	}

	hits := target.WatchHits()
	if len(hits) != 1 {
		t.Fatalf("%d writers recorded, want one", len(hits))
	}
	hit := hits[0]
	if hit.Address != address {
		t.Errorf("hit address %#x, want %#x", hit.Address, address)
	}
	if hit.Value != 7 {
		t.Errorf("hit value %d, want 7", hit.Value)
	}
	if hit.Count != 1 {
		t.Errorf("hit count %d, want 1", hit.Count)
	}
	// A pc means nothing here, so the writer is named. This is what the panel
	// and the console show in its place.
	if !strings.HasPrefix(hit.Site, "Watched.spend+") {
		t.Errorf("hit site %q, want it to name Watched.spend", hit.Site)
	}
	if hit.Origin != cheat.OriginGuest {
		t.Errorf("hit origin %v, want the guest", hit.Origin)
	}

	// Writing again is the same writer rather than a second one, which is what
	// makes the list readable on a field written every frame.
	if _, err := runtime.VM.InvokeVirtual(object, "spend", "(I)V", jvm.IntValue(9)); err != nil {
		t.Fatal(err)
	}
	if hits := target.WatchHits(); len(hits) != 1 || hits[0].Count != 2 {
		t.Errorf("a second write from the same site made %d writers", len(hits))
	}
}

// A store somewhere else is not this address's business, and neither is any
// store at all once the watch is gone.
func TestOnlyTheWatchedAddressIsRecorded(t *testing.T) {
	runtime, target, object := watchFixture(t)

	target.Watch(fieldAddressOf(t, runtime.heap, object, "gold"))
	if _, err := runtime.VM.InvokeVirtual(object, "elsewhere", "(I)V", jvm.IntValue(3)); err != nil {
		t.Fatal(err)
	}
	if hits := target.WatchHits(); len(hits) != 0 {
		t.Errorf("a store to another field was recorded: %+v", hits)
	}

	target.ClearWatches()
	if _, err := runtime.VM.InvokeVirtual(object, "spend", "(I)V", jvm.IntValue(1)); err != nil {
		t.Fatal(err)
	}
	if hits := target.WatchHits(); len(hits) != 0 {
		t.Errorf("a cleared watch still recorded %d writers", len(hits))
	}
}

// The engine asks this before a Host offers the control, and it used to answer
// no on this platform.
func TestThisPlatformNowSaysItCanWatchWrites(t *testing.T) {
	runtime, _, _ := watchFixture(t)
	session := cheat.NewSession(cheatTarget{runtime: runtime})
	if !session.CanWatch() {
		t.Error("the MIDlet cheat target says it cannot watch writes")
	}
}

func fieldAddressOf(t *testing.T, heap *heapMap, object *jvm.Object, name string) uint32 {
	t.Helper()
	entry := heap.byIdentity[heap.vm.Identity(object)]
	if entry == nil {
		t.Fatalf("%s is not mapped", object.ClassName)
	}
	for _, slot := range entry.shape.slots {
		if slot.field.Name == name {
			return entry.base + slot.offset
		}
	}
	t.Fatalf("%s has no field %q", object.ClassName, name)
	return 0
}
