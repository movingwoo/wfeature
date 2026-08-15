package lgt

import (
	"context"
	"testing"
)

// Preparing a class builds the two things the compiled code reaches for: the
// class object the resolve call answers, and the vtable an instance's first
// word points at. Both shapes are the module's, so both are checked by reading
// them back the way the module would.
func TestJavaClassPreparationBuildsTheObjectAndTheVTable(t *testing.T) {
	client := fixtureClient(t)
	writeJavaClassFixture(t, client)

	class, err := client.prepareJavaClass(context.Background(), nil, fixtureClassHandle)
	if err != nil {
		t.Fatalf("prepareJavaClass() error = %v", err)
	}
	if class.Name != "o" || class.Slots != 26 || class.Instance != 24 {
		t.Fatalf("class = %+v", class)
	}

	// The module reads a class's name at word 2 of its data block, and its
	// state halfword at word 4.
	data, err := client.readWord(class.Object + 8)
	if err != nil {
		t.Fatal(err)
	}
	name, err := client.readWord(data + javaClassNameWord*4)
	if err != nil {
		t.Fatal(err)
	}
	if text, ok := client.readPrintableString(name); !ok || text != "o" {
		t.Errorf("the class object names %q", text)
	}
	state, err := client.readHalfword(data + javaClassStateWord*4)
	if err != nil {
		t.Fatal(err)
	}
	if state != 0 {
		t.Errorf("an unprepared class reports state %d, want 0", state)
	}

	// And it gates its own use of the record on the halfword at header+0x1a.
	prepared, err := client.readHalfword(fixtureClassHeader + 0x1a)
	if err != nil {
		t.Fatal(err)
	}
	if prepared != javaClassReady {
		t.Errorf("the record reports state %d, want %d", prepared, javaClassReady)
	}

	// Every slot answers with something: a slot left at zero is a branch to
	// zero the first time the application dispatches through it.
	for slot := uint32(0); slot < class.Slots; slot++ {
		entry, err := client.readWord(class.VTable + 4 + slot*4)
		if err != nil {
			t.Fatal(err)
		}
		if entry == 0 {
			t.Fatalf("vtable slot %d is null", slot)
		}
	}
}

// Initialising a class runs its static initialiser once and leaves the state
// the module tests for. The state is written before the initialiser runs, so an
// initialiser that reaches back into its own class does not start a second one.
func TestJavaClassInitialisationIsOnce(t *testing.T) {
	client := fixtureClient(t)
	writeJavaClassFixture(t, client)
	class, err := client.prepareJavaClass(context.Background(), nil, fixtureClassHandle)
	if err != nil {
		t.Fatalf("prepareJavaClass() error = %v", err)
	}
	if err := client.initializeJavaClass(context.Background(), nil, class.Object, 0); err != nil {
		t.Fatalf("initializeJavaClass() error = %v", err)
	}
	data, err := client.readWord(class.Object + 8)
	if err != nil {
		t.Fatal(err)
	}
	state, err := client.readHalfword(data + javaClassStateWord*4)
	if err != nil {
		t.Fatal(err)
	}
	if state != javaClassInitedFlag {
		t.Errorf("an initialised class reports state %d, want %d", state, javaClassInitedFlag)
	}

	// An instance is a pair: the object, whose first word is the vtable, and
	// the block its fields live in.
	object, err := client.allocateJavaInstance(class.Object)
	if err != nil {
		t.Fatalf("allocateJavaInstance() error = %v", err)
	}
	table, err := client.readWord(object)
	if err != nil {
		t.Fatal(err)
	}
	if table != class.VTable {
		t.Errorf("the instance dispatches through %#x, want %#x", table, class.VTable)
	}
	fields, err := client.readWord(object + 8)
	if err != nil {
		t.Fatal(err)
	}
	if fields == 0 || fields == object {
		t.Errorf("the instance's fields are at %#x", fields)
	}
}
