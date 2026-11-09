package lgt

import (
	"strconv"
	"testing"
)

// intern answers one String for each text: the first String interned with a
// text is the one every later String with that text interns to, and it stays
// so after a collection, which nothing but the pool holds it through.
func TestJavaStringInternPoolsOneStringPerText(t *testing.T) {
	client := fixtureClient(t)
	first, second, other := newTestString(t, client, "아이템"), newTestString(t, client, "아이템"), newTestString(t, client, "item")
	if first == second {
		t.Fatal("two Strings built apart are one object")
	}
	if got := mustSlot(t, client, javaStringClass, 35, first); got != first {
		t.Errorf("the first intern of a text answered %#x, want the String itself %#x", got, first)
	}
	if got := mustSlot(t, client, javaStringClass, 35, second); got != first {
		t.Errorf("a later intern of the same text answered %#x, want %#x", got, first)
	}
	if got := mustSlot(t, client, javaStringClass, 35, other); got != other {
		t.Errorf("another text interned to %#x, want %#x", got, other)
	}
	client.releaseJavaPins(0)
	collect(t, client)
	collect(t, client)
	if !tracked(client, first) {
		t.Fatal("a collection freed a pooled String")
	}
	again := newTestString(t, client, "아이템")
	if got := mustSlot(t, client, javaStringClass, 35, again); got != first {
		t.Errorf("an intern after a collection answered %#x, want %#x", got, first)
	}
}

// A pool that is full is the OutOfMemoryError the KTF and SKT runtimes throw
// for one, and a text already in it is still answered.
func TestJavaStringInternPoolIsBounded(t *testing.T) {
	client := fixtureClient(t)
	kept := newTestString(t, client, "kept")
	mustSlot(t, client, javaStringClass, 35, kept)
	runtime := client.javaRuntimeState()
	for index := 1; index < maxJavaInternedStrings; index++ {
		runtime.singletons[javaInternKey+strconv.Itoa(index)] = kept
	}
	if _, err := slotCall(t, client, javaStringClass, 35, newTestString(t, client, "one more")); thrownClass(err) != javaOutOfMemoryClass {
		t.Errorf("an intern past the pool's bound = %v, want OutOfMemoryError", err)
	}
	if got := mustSlot(t, client, javaStringClass, 35, newTestString(t, client, "kept")); got != kept {
		t.Errorf("a text already pooled answered %#x, want %#x", got, kept)
	}
}
