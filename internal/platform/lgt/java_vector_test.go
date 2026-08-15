package lgt

import "testing"

// newFixtureVector builds a vector the way the module does: an object of the
// class, then the constructor that gives it its list.
func newFixtureVector(t *testing.T, client *Client) uint32 {
	t.Helper()
	class, err := client.preparePlatformJavaClass(javaVectorClass)
	if err != nil {
		t.Fatal(err)
	}
	vector, err := client.allocateJavaObject(class)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := javaVectorConstructor(client, nil, nil, []uint32{vector}); err != nil {
		t.Fatalf("the constructor failed: %v", err)
	}
	return vector
}

// `indexOf` answers where a reference sits and -1 when it is not held, which is
// the answer a title uses as a count of the rows it is about to allocate.
func TestJavaVectorIndexOfAnswersThePositionOrMinusOne(t *testing.T) {
	client := fixtureClient(t)
	vector := newFixtureVector(t, client)

	for _, element := range []uint32{0x1000, 0x2000, 0x3000} {
		if _, err := javaVectorAdd(client, nil, nil, []uint32{vector, element}); err != nil {
			t.Fatalf("addElement(%#x) failed: %v", element, err)
		}
	}

	for _, want := range []struct {
		element uint32
		index   uint32
	}{
		{0x1000, 0},
		{0x3000, 2},
		{0x4000, ^uint32(0)},
	} {
		index, err := javaVectorIndexOf(client, nil, nil, []uint32{vector, want.element})
		if err != nil {
			t.Fatalf("indexOf(%#x) error = %v", want.element, err)
		}
		if index != want.index {
			t.Errorf("indexOf(%#x) = %d, want %d", want.element, int32(index), int32(want.index))
		}
	}

	// The first of two equal references is the one, the way the language says.
	if _, err := javaVectorAdd(client, nil, nil, []uint32{vector, 0x1000}); err != nil {
		t.Fatal(err)
	}
	if index, err := javaVectorIndexOf(client, nil, nil, []uint32{vector, 0x1000}); err != nil || index != 0 {
		t.Errorf("indexOf of a repeated reference = %d (%v), want 0", int32(index), err)
	}

	if _, err := javaVectorIndexOf(client, nil, nil, []uint32{0xdeadbeef, 0x1000}); err == nil {
		t.Error("indexOf on an object that is no vector is not reported")
	}
}

// `firstElement` and `removeElementAt(0)` are the pair a title drains a vector
// with, and an empty vector is reported rather than answered with a null the
// caller would fault on later.
func TestJavaVectorFirstElementDrainsWithRemoveAt(t *testing.T) {
	client := fixtureClient(t)
	vector := newFixtureVector(t, client)

	if _, err := javaVectorFirst(client, nil, nil, []uint32{vector}); err == nil {
		t.Error("the first element of an empty vector is not reported")
	}

	for _, element := range []uint32{0x1000, 0x2000} {
		if _, err := javaVectorAdd(client, nil, nil, []uint32{vector, element}); err != nil {
			t.Fatal(err)
		}
	}
	for _, want := range []uint32{0x1000, 0x2000} {
		first, err := javaVectorFirst(client, nil, nil, []uint32{vector})
		if err != nil {
			t.Fatalf("firstElement() error = %v", err)
		}
		if first != want {
			t.Errorf("firstElement() = %#x, want %#x", first, want)
		}
		if _, err := javaVectorRemoveAt(client, nil, nil, []uint32{vector, 0}); err != nil {
			t.Fatalf("removeElementAt(0) error = %v", err)
		}
	}

	size, err := javaVectorSize(client, nil, nil, []uint32{vector})
	if err != nil {
		t.Fatal(err)
	}
	if size != 0 {
		t.Errorf("the drained vector holds %d, want 0", size)
	}
}

// A type check whose second argument is a class object rather than a name is
// answered against that class. A title testing the elements it drained out of a
// vector resolves the array type it wants first and passes the type object, and
// reading that word as a name finds no name at all.
func TestJavaTypeCheckTakesAClassObjectOrAName(t *testing.T) {
	client := fixtureClient(t)
	characters, err := client.javaArrayType(1, "C", 2)
	if err != nil {
		t.Fatal(err)
	}
	array, err := client.allocateJavaArray(characters.Object, 4)
	if err != nil {
		t.Fatal(err)
	}
	// The identity is where the title reads it: the word in front of the
	// object's vtable, which is two hops from the object.
	vtable, err := client.readWord(array)
	if err != nil {
		t.Fatal(err)
	}
	identity, err := client.readWord(vtable)
	if err != nil {
		t.Fatal(err)
	}

	answer, err := client.javaTypeCheck(identity, characters.Object)
	if err != nil {
		t.Fatalf("the check against a class object failed: %v", err)
	}
	if answer != 1 {
		t.Errorf("a [C against the [C type answered %d, want 1", answer)
	}

	bytes, err := client.javaArrayType(1, "B", 1)
	if err != nil {
		t.Fatal(err)
	}
	if answer, err := client.javaTypeCheck(identity, bytes.Object); err != nil || answer != 0 {
		t.Errorf("a [C against the [B type answered %d (%v), want 0", answer, err)
	}

	// The name form still answers, because both kinds arrive at this call.
	name, err := client.allocateBytes([]byte("[C\x00"))
	if err != nil {
		t.Fatal(err)
	}
	if answer, err := client.javaTypeCheck(identity, name); err != nil || answer != 1 {
		t.Errorf("a [C against the name %q answered %d (%v), want 1", "[C", answer, err)
	}
}

// A title's save stops on slot 23. It is `elementAt(int)`, and the two
// independent readings that say so are what this pins: the class's own
// numbering, and what its caller does with the answer.
//
// The numbering is the same one that put every other slot here — a class's own
// methods run from 10 in declaration order — and this class's declaration
// order is fixed by the anchors already in the table. `elementAt` is the
// fourteenth method it declares, which is 23, and every neighbour agrees:
// `size` is the sixth at 15, `contains` the ninth at 18, `indexOf` the tenth
// at 19, `firstElement` the fifteenth at 24, `removeElementAt` the eighteenth
// at 27, `addElement` the twentieth at 29.
//
// The caller settles what kind of method it is. The instruction after the
// dispatch tests the answer against zero and the surviving branch loads a
// vtable out of it — so the answer is a reference, which no int or boolean
// method on this class can be, and of the reference-answering ones this is the
// one that takes an index.
func TestJavaVectorElementAtIsTheSlotASaveStopsOn(t *testing.T) {
	const elementAtSlot = 23
	slot, ok := javaBakedVirtualSlots[javaVectorClass][elementAtSlot]
	if !ok {
		t.Fatalf("slot %d is not registered on %s", elementAtSlot, javaVectorClass)
	}
	if slot.Called != "elementAt(I)Ljava/lang/Object;" {
		t.Fatalf("slot %d is registered as %q", elementAtSlot, slot.Called)
	}
	// The receiver and one index: a slot that took only the receiver would
	// read the index out of whatever the caller left behind.
	if slot.Method.Words != 2 {
		t.Fatalf("slot %d takes %d words, want the receiver and an index", elementAtSlot, slot.Method.Words)
	}

	// The neighbours the numbering rests on, so a change to any of them fails
	// here rather than in a game.
	for number, want := range map[uint32]string{
		15: "size()I",
		19: "indexOf(Ljava/lang/Object;)I",
		24: "firstElement()Ljava/lang/Object;",
		27: "removeElementAt(I)V",
		29: "addElement(Ljava/lang/Object;)V",
	} {
		if got := javaBakedVirtualSlots[javaVectorClass][number].Called; got != want {
			t.Errorf("slot %d is %q, want %q — the numbering elementAt was read from", number, got, want)
		}
	}

	client := fixtureClient(t)
	vector := newFixtureVector(t, client)
	for _, element := range []uint32{0x1000, 0x2000, 0x3000} {
		if _, err := javaVectorAdd(client, nil, nil, []uint32{vector, element}); err != nil {
			t.Fatal(err)
		}
	}
	for index, want := range []uint32{0x1000, 0x2000, 0x3000} {
		got, err := slot.Method.Implementat(client, nil, nil, []uint32{vector, uint32(index)})
		if err != nil {
			t.Fatalf("elementAt(%d) failed: %v", index, err)
		}
		if got != want {
			t.Errorf("elementAt(%d) = %#x, want %#x", index, got, want)
		}
	}
	// Past the end is reported rather than answered with a null the caller
	// would fault on somewhere else entirely.
	if _, err := slot.Method.Implementat(client, nil, nil, []uint32{vector, 3}); err == nil {
		t.Error("elementAt(3) of a vector of three answered rather than failing")
	}
}
