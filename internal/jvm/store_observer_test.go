package jvm

import (
	_ "embed"
	"testing"
)

//go:embed testdata/Stores.class
var storesClass []byte

// A platform whose stores are interpreter assignments rather than instructions
// has nothing to trap, so the interpreter is what tells it what was written.
// All three shapes have to arrive, and each has to name the code that did it.
func TestTheInterpreterReportsEveryShapeOfStore(t *testing.T) {
	vm := New(mapClassSource{"Stores": storesClass}, Options{})

	var seen []StoreEvent
	vm.SetStoreObserver(func(event StoreEvent) { seen = append(seen, event) })

	object, err := vm.NewObject("Stores", "()V")
	if err != nil {
		t.Fatalf("NewObject() error = %v", err)
	}
	// The constructor's own stores are reported too; what this test is about
	// starts here.
	seen = nil
	if _, err := vm.InvokeVirtual(object, "spend", "(I)V", IntValue(40)); err != nil {
		t.Fatalf("InvokeVirtual() error = %v", err)
	}

	var field, static, element *StoreEvent
	for index := range seen {
		event := &seen[index]
		switch {
		case event.Key == "Stores.gold:I":
			field = event
		case event.Key == "Stores.party:I":
			static = event
		case event.Index == 2 && event.Key == "":
			element = event
		}
	}

	if field == nil {
		t.Fatal("the instance field store was not reported")
	}
	if field.Object != object {
		t.Error("the instance store named an object other than the one written")
	}
	if value, _ := field.Value.Int32(); value != 40 {
		t.Errorf("the instance store carried %d, want 40", value)
	}

	if static == nil {
		t.Fatal("the static store was not reported")
	}
	// A static belongs to its class, so there is no object to name.
	if static.Object != nil {
		t.Error("the static store named an object")
	}
	if static.Class != "Stores" {
		t.Errorf("the static store named class %q, want Stores", static.Class)
	}
	if value, _ := static.Value.Int32(); value != 41 {
		t.Errorf("the static store carried %d, want 41", value)
	}

	if element == nil {
		t.Fatal("the array store was not reported")
	}
	if value, _ := element.Value.Int32(); value != 42 {
		t.Errorf("the array store carried %d, want 42", value)
	}

	// The writer is named by the code that ran, which is the whole point of
	// reporting at all: an address without a writer answers nothing.
	for _, event := range []*StoreEvent{field, static, element} {
		if event.SiteClass != "Stores" || event.SiteMethod != "spend" {
			t.Errorf("a store was attributed to %s.%s, want Stores.spend", event.SiteClass, event.SiteMethod)
		}
		if event.Site() == "" {
			t.Error("a store had no assembled site name")
		}
	}
}

// Nobody watching means nobody pays. The observer is what a platform installs
// while it is investigating, and clearing it has to actually stop the reports.
func TestAClearedStoreObserverHearsNothing(t *testing.T) {
	vm := New(mapClassSource{"Stores": storesClass}, Options{})
	object, err := vm.NewObject("Stores", "()V")
	if err != nil {
		t.Fatalf("NewObject() error = %v", err)
	}

	count := 0
	vm.SetStoreObserver(func(StoreEvent) { count++ })
	if _, err := vm.InvokeVirtual(object, "spend", "(I)V", IntValue(1)); err != nil {
		t.Fatal(err)
	}
	if count == 0 {
		t.Fatal("an installed observer heard nothing")
	}

	vm.SetStoreObserver(nil)
	before := count
	if _, err := vm.InvokeVirtual(object, "spend", "(I)V", IntValue(2)); err != nil {
		t.Fatal(err)
	}
	if count != before {
		t.Errorf("a cleared observer still heard %d stores", count-before)
	}
}
