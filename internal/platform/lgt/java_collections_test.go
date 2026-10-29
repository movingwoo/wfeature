package lgt

import (
	"context"
	"reflect"
	"strings"
	"testing"
)

func newTestHashtable(t *testing.T, client *Client, capacity ...uint32) uint32 {
	t.Helper()
	table, err := newTestObject(t, client, javaHashtableClass)
	if err != nil {
		t.Fatal(err)
	}
	if len(capacity) == 0 {
		mustJavaCall(t, client, "java/util/Hashtable.<init>()V", table)
	} else {
		mustJavaCall(t, client, "java/util/Hashtable.<init>(I)V", table, capacity[0])
	}
	return table
}

func newTestInteger(t *testing.T, client *Client, value int32) uint32 {
	t.Helper()
	object, err := newTestObject(t, client, javaIntegerClass)
	if err != nil {
		t.Fatal(err)
	}
	mustJavaCall(t, client, "java/lang/Integer.<init>(I)V", object, uint32(value))
	return object
}

// javaEnumerated walks an enumeration to its end through its two slots, and
// checks that one more step is the NoSuchElementException the language throws.
func javaEnumerated(t *testing.T, client *Client, enumeration uint32) []uint32 {
	t.Helper()
	class, ok := client.javaClassOfObject(enumeration)
	if !ok {
		t.Fatal("the enumeration is not an object issued here")
	}
	items := []uint32{}
	for mustSlot(t, client, class.Name, 10, enumeration) == javaTrue {
		items = append(items, mustSlot(t, client, class.Name, 11, enumeration))
		if len(items) > 64 {
			t.Fatal("the enumeration does not end")
		}
	}
	if _, err := slotCall(t, client, class.Name, 11, enumeration); thrownClass(err) != javaNoSuchElementClass {
		t.Errorf("a step past the end = %v, want NoSuchElementException", err)
	}
	return items
}

func javaIntegers(t *testing.T, client *Client, objects []uint32) []int64 {
	t.Helper()
	values := make([]int64, len(objects))
	for index, object := range objects {
		value, err := client.javaNumberOf(object)
		if err != nil {
			t.Fatal(err)
		}
		values[index] = value
	}
	return values
}

// The table is the class library's, bucket for bucket: a key is filed at
// `(hash & 0x7fffffff) % buckets`, a new entry heads its chain, a table three
// quarters full rehashes into `2n + 1` buckets walking the old ones from the
// last, and an enumeration visits the buckets from the last. Each order below is
// that arithmetic worked by hand.
func TestJavaHashtableIsTheClassLibrarysTable(t *testing.T) {
	client := fixtureClient(t)
	table := newTestHashtable(t, client)
	for _, pair := range [][2]string{{"a", "1"}, {"b", "2"}, {"c", "3"}} {
		mustSlot(t, client, javaHashtableClass, 18, table, newTestString(t, client, pair[0]), newTestString(t, client, pair[1]))
	}
	// 'a', 'b' and 'c' hash to 97, 98 and 99: buckets 9, 10 and 0 of 11.
	text, err := client.javaToStringOf(context.Background(), client.thread, table)
	if err != nil || text != "{b=2, a=1, c=3}" {
		t.Errorf("toString() = %q, %v, want {b=2, a=1, c=3}", text, err)
	}

	// Two buckets rehash at the second key into five; 5 then shares bucket 0
	// with 0 and heads its chain; 2 rehashes five into eleven.
	small := newTestHashtable(t, client, 2)
	put := func(key int32) {
		mustSlot(t, client, javaHashtableClass, 18, small, newTestInteger(t, client, key), newTestString(t, client, "v"))
	}
	put(0)
	put(1)
	put(5)
	if got := javaIntegers(t, client, javaEnumerated(t, client, mustSlot(t, client, javaHashtableClass, 12, small))); !reflect.DeepEqual(got, []int64{1, 5, 0}) {
		t.Errorf("keys in five buckets = %v, want [1 5 0]", got)
	}
	put(2)
	if got := javaIntegers(t, client, javaEnumerated(t, client, mustSlot(t, client, javaHashtableClass, 12, small))); !reflect.DeepEqual(got, []int64{5, 2, 1, 0}) {
		t.Errorf("keys in eleven buckets = %v, want [5 2 1 0]", got)
	}
	if buckets := len(client.javaRuntimeState().hashtables[small].buckets); buckets != 11 {
		t.Errorf("the table has %d buckets, want 11", buckets)
	}
}

// A key is found by its own equals, so a String built again finds the entry;
// a key already held keeps its object and takes the new value; null keys and
// values are the NullPointerException the class library throws.
func TestJavaHashtableMembers(t *testing.T) {
	client := fixtureClient(t)
	table := newTestHashtable(t, client)
	first, again := newTestString(t, client, "score"), newTestString(t, client, "score")
	one, two := newTestString(t, client, "1"), newTestString(t, client, "2")

	if old := mustSlot(t, client, javaHashtableClass, 18, table, first, one); old != 0 {
		t.Errorf("the first put answered %#x, want null", old)
	}
	if old := mustSlot(t, client, javaHashtableClass, 18, table, again, two); old != one {
		t.Errorf("replacing put answered %#x, want the old value %#x", old, one)
	}
	if keys := javaEnumerated(t, client, mustSlot(t, client, javaHashtableClass, 12, table)); len(keys) != 1 || keys[0] != first {
		t.Errorf("keys = %v, want the first key object only", keys)
	}
	if got := mustSlot(t, client, javaHashtableClass, 16, table, newTestString(t, client, "score")); got != two {
		t.Errorf("get = %#x, want %#x", got, two)
	}
	if got := mustSlot(t, client, javaHashtableClass, 15, table, newTestString(t, client, "score")); got != javaTrue {
		t.Error("containsKey of an equal key is false")
	}
	if got := mustSlot(t, client, javaHashtableClass, 14, table, newTestString(t, client, "2")); got != javaTrue {
		t.Error("contains of an equal value is false")
	}
	if got := mustSlot(t, client, javaHashtableClass, 16, table, newTestString(t, client, "missing")); got != 0 {
		t.Errorf("get of a missing key = %#x, want null", got)
	}
	if got := mustSlot(t, client, javaHashtableClass, 10, table); got != 1 {
		t.Errorf("size() = %d", got)
	}
	if _, err := slotCall(t, client, javaHashtableClass, 18, table, 0, one); thrownClass(err) != javaThrowNullClass {
		t.Errorf("put(null, v) = %v, want NullPointerException", err)
	}
	if _, err := slotCall(t, client, javaHashtableClass, 18, table, first, 0); thrownClass(err) != javaThrowNullClass {
		t.Errorf("put(k, null) = %v, want NullPointerException", err)
	}
	if got := mustSlot(t, client, javaHashtableClass, 19, table, again); got != two {
		t.Errorf("remove = %#x, want %#x", got, two)
	}
	if got := mustSlot(t, client, javaHashtableClass, 11, table); got != javaTrue {
		t.Error("isEmpty() after the last remove is false")
	}
	mustSlot(t, client, javaHashtableClass, 18, table, first, one)
	mustSlot(t, client, javaHashtableClass, 20, table)
	if got := mustSlot(t, client, javaHashtableClass, 10, table); got != 0 {
		t.Errorf("size() after clear = %d", got)
	}
	if _, err := javaCall(t, client, "java/util/Hashtable.<init>(I)V", newTestHashtable(t, client), uint32(0xffffffff)); thrownClass(err) != javaIllegalArgumentClass {
		t.Errorf("Hashtable(-1) = %v, want IllegalArgumentException", err)
	}
}

// An enumeration is reached the way a title reaches it, through the
// interface: the interface table this platform gives its class puts
// Enumeration's two methods at the class's slots 10 and 11.
func TestJavaEnumerationAnswersThroughItsInterface(t *testing.T) {
	client := fixtureClient(t)
	vector := newFixtureVector(t, client)
	walk := mustSlot(t, client, javaVectorClass, 17, vector)
	name, err := client.allocateBytes(append([]byte(javaEnumerationInterface), 0))
	if err != nil {
		t.Fatal(err)
	}
	methods, err := client.javaInterfaceTable(walk, name)
	if err != nil {
		t.Fatal(err)
	}
	for index, want := range []string{"hasMoreElements()Z", "nextElement()Ljava/lang/Object;"} {
		entry, err := client.readWord(methods + 4 + uint32(index)*4)
		if err != nil {
			t.Fatal(err)
		}
		slot, ok := client.javaStubSlot(entry)
		if !ok {
			t.Fatalf("method %d of the interface is not a platform stub", index)
		}
		dispatch, ok := client.javaPlatformVirtualDispatch(slot)
		if !ok || dispatch.Called != want {
			t.Errorf("method %d of the interface is %q, want %q", index, dispatch.Called, want)
		}
	}
	// A vector's enumeration is live: what is added before the walk ends is
	// seen.
	element := newTestString(t, client, "late")
	mustSlot(t, client, javaVectorClass, 29, vector, element)
	if items := javaEnumerated(t, client, walk); len(items) != 1 || items[0] != element {
		t.Errorf("the walk handed out %v, want the element added after it began", items)
	}
}

// A vector searches by the argument's own equals, keeps the capacity the class
// library would, and writes itself the way the library does.
func TestJavaVectorMembers(t *testing.T) {
	client := fixtureClient(t)
	vector := newFixtureVector(t, client)
	a, b := newTestString(t, client, "a"), newTestString(t, client, "b")
	for _, element := range []uint32{a, b, a} {
		mustSlot(t, client, javaVectorClass, 29, vector, element)
	}
	equalA := newTestString(t, client, "a")
	for _, probe := range []struct {
		slot      uint32
		arguments []uint32
		want      int32
	}{
		{19, []uint32{equalA}, 0},
		{20, []uint32{equalA, 1}, 2},
		{21, []uint32{equalA}, 2},
		{22, []uint32{equalA, 1}, 0},
		{19, []uint32{newTestString(t, client, "z")}, -1},
	} {
		got := mustSlot(t, client, javaVectorClass, probe.slot, append([]uint32{vector}, probe.arguments...)...)
		if int32(got) != probe.want {
			t.Errorf("slot %d%v = %d, want %d", probe.slot, probe.arguments, int32(got), probe.want)
		}
	}
	if _, err := slotCall(t, client, javaVectorClass, 22, vector, equalA, 3); thrownClass(err) != javaIndexClass {
		t.Errorf("lastIndexOf from the size = %v, want IndexOutOfBoundsException", err)
	}
	if got := mustSlot(t, client, javaVectorClass, 18, vector, newTestString(t, client, "b")); got != javaTrue {
		t.Error("contains of an equal element is false")
	}
	if got := mustSlot(t, client, javaVectorClass, 30, vector, newTestString(t, client, "b")); got != javaTrue {
		t.Error("removeElement of an equal element is false")
	}
	if got := mustSlot(t, client, javaVectorClass, 25, vector); got != a {
		t.Errorf("lastElement() = %#x, want %#x", got, a)
	}
	mustSlot(t, client, javaVectorClass, 13, vector, 4)
	if held := client.javaRuntimeState().vectors[vector]; !reflect.DeepEqual(held, []uint32{a, a, 0, 0}) {
		t.Errorf("setSize(4) left %v", held)
	}
	mustSlot(t, client, javaVectorClass, 28, vector, newTestInteger(t, client, 5), 1)
	if text, _ := client.javaToStringOf(context.Background(), client.thread, vector); text != "[a, 5, a, null, null]" {
		t.Errorf("toString() = %q", text)
	}

	// 10 to start with, doubling; a step when one is given; trimToSize to the
	// size.
	grown := newFixtureVector(t, client)
	for range 11 {
		mustSlot(t, client, javaVectorClass, 29, grown, a)
	}
	if got := mustSlot(t, client, javaVectorClass, 14, grown); got != 20 {
		t.Errorf("capacity after 11 adds = %d, want 20", got)
	}
	stepped, err := newTestObject(t, client, javaVectorClass)
	if err != nil {
		t.Fatal(err)
	}
	mustJavaCall(t, client, "java/util/Vector.<init>(II)V", stepped, 3, 2)
	for range 4 {
		mustSlot(t, client, javaVectorClass, 29, stepped, a)
	}
	if got := mustSlot(t, client, javaVectorClass, 14, stepped); got != 5 {
		t.Errorf("capacity of Vector(3, 2) after 4 adds = %d, want 5", got)
	}
	mustSlot(t, client, javaVectorClass, 12, stepped, 30)
	if got := mustSlot(t, client, javaVectorClass, 14, stepped); got != 30 {
		t.Errorf("ensureCapacity(30) = %d, want 30", got)
	}
	mustSlot(t, client, javaVectorClass, 11, stepped)
	if got := mustSlot(t, client, javaVectorClass, 14, stepped); got != 4 {
		t.Errorf("trimToSize() = %d, want 4", got)
	}

	empty := newFixtureVector(t, client)
	if _, err := slotCall(t, client, javaVectorClass, 25, empty); thrownClass(err) != javaNoSuchElementClass {
		t.Errorf("lastElement of an empty vector = %v, want NoSuchElementException", err)
	}
}

// peek is the top without taking it off, search counts from the top as 1, and
// an empty stack is EmptyStackException.
func TestJavaStackPeekAndSearch(t *testing.T) {
	client := fixtureClient(t)
	stack, err := newTestObject(t, client, javaStackClass)
	if err != nil {
		t.Fatal(err)
	}
	mustJavaCall(t, client, "java/util/Stack.<init>()V", stack)
	if _, err := slotCall(t, client, javaStackClass, 34, stack); thrownClass(err) != javaEmptyStackClass {
		t.Errorf("peek of an empty stack = %v, want EmptyStackException", err)
	}
	a, b := newTestString(t, client, "a"), newTestString(t, client, "b")
	mustSlot(t, client, javaStackClass, 32, stack, a)
	mustSlot(t, client, javaStackClass, 32, stack, b)
	if got := mustSlot(t, client, javaStackClass, 34, stack); got != b {
		t.Errorf("peek() = %#x, want %#x", got, b)
	}
	if got := mustSlot(t, client, javaStackClass, 36, stack, newTestString(t, client, "a")); got != 2 {
		t.Errorf("search(a) = %d, want 2", got)
	}
	if got := mustSlot(t, client, javaStackClass, 36, stack, newTestString(t, client, "z")); int32(got) != -1 {
		t.Errorf("search(z) = %d, want -1", int32(got))
	}
}

// A vector that holds itself, or two tables that hold each other, ask their
// own toString for ever. The class library would run out of stack; here the
// nesting is counted and the title stops, rather than the Go stack running out
// under the whole process.
func TestJavaContainersThatHoldThemselvesStopTheirToString(t *testing.T) {
	client := fixtureClient(t)
	ctx := context.Background()
	vector := newFixtureVector(t, client)
	mustSlot(t, client, javaVectorClass, 29, vector, newTestString(t, client, "a"))
	mustSlot(t, client, javaVectorClass, 29, vector, vector)
	if _, err := client.javaToStringOf(ctx, client.thread, vector); err == nil || !strings.Contains(err.Error(), "nested") {
		t.Errorf("toString of a vector that holds itself = %v, want the nesting stopped", err)
	}
	first, second := newTestHashtable(t, client), newTestHashtable(t, client)
	key := newTestString(t, client, "k")
	mustSlot(t, client, javaHashtableClass, 18, first, key, second)
	mustSlot(t, client, javaHashtableClass, 18, second, key, first)
	if _, err := client.javaToStringOf(ctx, client.thread, first); err == nil || !strings.Contains(err.Error(), "nested") {
		t.Errorf("toString of two tables that hold each other = %v, want the nesting stopped", err)
	}
	if client.javaVirtualNesting != 0 {
		t.Errorf("the nesting count is %d after the calls returned", client.javaVirtualNesting)
	}

	// Nesting short of the bound answers, as the class library writes it.
	outer, inner := newFixtureVector(t, client), newFixtureVector(t, client)
	mustSlot(t, client, javaVectorClass, 29, inner, newTestString(t, client, "x"))
	mustSlot(t, client, javaVectorClass, 29, outer, inner)
	if text, err := client.javaToStringOf(ctx, client.thread, outer); err != nil || text != "[[x]]" {
		t.Errorf("toString of a vector of a vector = %q, %v, want [[x]]", text, err)
	}
}
