package lgt

import (
	"context"
	"testing"
)

// newTestBoolean is a Boolean built the way a title builds one, through the
// constructor it links against.
func newTestBoolean(t *testing.T, client *Client, value bool) uint32 {
	t.Helper()
	object, err := newTestObject(t, client, javaBooleanClass)
	if err != nil {
		t.Fatal(err)
	}
	mustJavaCall(t, client, "java/lang/Boolean.<init>(Z)V", object, javaBoolean(value))
	return object
}

// A Boolean answers what the class page and the KTF and SKT runtimes answer:
// its flag, 1231 or 1237, "true" or "false", and equality with another Boolean
// holding the same flag — which is what lets one key a Hashtable.
func TestJavaBooleanBoxesAFlag(t *testing.T) {
	client := fixtureClient(t)
	ctx := context.Background()
	yes, alsoYes, no := newTestBoolean(t, client, true), newTestBoolean(t, client, true), newTestBoolean(t, client, false)

	if got := mustSlot(t, client, javaBooleanClass, 10, yes); got != javaTrue {
		t.Errorf("booleanValue() of true = %d", got)
	}
	if got := mustSlot(t, client, javaBooleanClass, 10, no); got != javaFalse {
		t.Errorf("booleanValue() of false = %d", got)
	}
	for _, probe := range []struct {
		object uint32
		hash   int32
		text   string
	}{{yes, 1231, "true"}, {no, 1237, "false"}} {
		if hash, err := client.javaHashCodeOf(ctx, client.thread, probe.object); err != nil || hash != probe.hash {
			t.Errorf("hashCode() = %d, %v, want %d", hash, err, probe.hash)
		}
		if text, err := client.javaToStringOf(ctx, client.thread, probe.object); err != nil || text != probe.text {
			t.Errorf("toString() = %q, %v, want %q", text, err, probe.text)
		}
	}
	one := newTestInteger(t, client, 1)
	for _, probe := range []struct {
		name  string
		other uint32
		want  bool
	}{{"another true", alsoYes, true}, {"false", no, false}, {"an Integer of 1", one, false}, {"null", 0, false}} {
		if equal, err := client.javaEqualsOf(ctx, client.thread, yes, probe.other); err != nil || equal != probe.want {
			t.Errorf("true.equals(%s) = %v, %v, want %v", probe.name, equal, err, probe.want)
		}
	}
	// A flag built again finds the entry filed under the first.
	table := newTestHashtable(t, client)
	value := newTestString(t, client, "kept")
	mustSlot(t, client, javaHashtableClass, 18, table, yes, value)
	if got := mustSlot(t, client, javaHashtableClass, 16, table, alsoYes); got != value {
		t.Errorf("get(another true) = %#x, want %#x", got, value)
	}
	// Any word the caller hands the constructor that is not zero is true, as a
	// boolean is.
	odd, err := newTestObject(t, client, javaBooleanClass)
	if err != nil {
		t.Fatal(err)
	}
	mustJavaCall(t, client, "java/lang/Boolean.<init>(Z)V", odd, 2)
	if equal, _ := client.javaEqualsOf(ctx, client.thread, odd, yes); !equal {
		t.Error("Boolean(2) is not Boolean(true)")
	}
}

// TRUE and FALSE are built when the class is laid out and written where the
// module was told they sit: two objects, one of each flag, the same ones every
// read, and kept by the collector because the class's storage is a root.
func TestJavaBooleanStaticsAreTheTwoInstances(t *testing.T) {
	client := fixtureClient(t)
	surface := &javaSurface{
		StaticFields: []javaMemberRef{
			{Name: "FALSE", Descriptor: "Ljava/lang/Boolean;"},
			{Name: "TRUE", Descriptor: "Ljava/lang/Boolean;"},
		},
		Classes: []javaAPIClass{{Name: javaBooleanClass, StaticFields: javaRun{Start: 0, Count: 2}}},
	}
	layout := newJavaLayout()
	answers, err := layout.layoutPlatformStatics(surface)
	if err != nil {
		t.Fatal(err)
	}
	client.javaLink = &javaLink{surface: surface, layout: layout}
	class, err := client.preparePlatformJavaClass(javaBooleanClass)
	if err != nil {
		t.Fatal(err)
	}
	read := func(entry uint32) uint32 {
		t.Helper()
		object, err := client.readWord(class.dataBlock + (javaClassDataWords+answers[entry])*4)
		if err != nil {
			t.Fatal(err)
		}
		return object
	}
	no, yes := read(0), read(1)
	if yes == 0 || no == 0 || yes == no {
		t.Fatalf("TRUE = %#x and FALSE = %#x, want two objects", yes, no)
	}
	for _, probe := range []struct {
		object, want uint32
	}{{yes, javaTrue}, {no, javaFalse}} {
		if held, ok := client.javaClassOfObject(probe.object); !ok || held.Name != javaBooleanClass {
			t.Fatalf("the static at %#x is a %v, want a Boolean", probe.object, held)
		}
		if got := mustSlot(t, client, javaBooleanClass, 10, probe.object); got != probe.want {
			t.Errorf("booleanValue() of the static at %#x = %d, want %d", probe.object, got, probe.want)
		}
	}
	client.releaseJavaPins(0)
	collect(t, client)
	collect(t, client)
	if !tracked(client, yes) || !tracked(client, no) {
		t.Fatal("a collection freed TRUE or FALSE, which only the class's storage names")
	}
}
