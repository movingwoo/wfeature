package lgt

import (
	"context"
	"encoding/binary"
	"strings"
	"testing"
	"time"
)

// A module says what each of its classes extends, and nothing stops it saying
// that a class extends itself. These records are planted to say exactly that,
// in the ways a chain can be closed: a class over itself, two over each other,
// and a class registered under the name the platform's own hierarchy ends at.
const (
	chainClassStrings uint32 = fixtureDataBase + 0x400
	chainClassFirst   uint32 = fixtureDataBase + 0x500
	chainClassSecond  uint32 = fixtureDataBase + 0x580
)

type chainFixture struct {
	t      *testing.T
	client *Client
	next   uint32
}

func newChainFixture(t *testing.T) *chainFixture {
	return &chainFixture{t: t, client: fixtureClient(t), next: chainClassStrings}
}

func (fixture *chainFixture) text(text string) uint32 {
	fixture.t.Helper()
	at := fixture.next
	if err := fixture.client.core.Memory().Write(at, append([]byte(text), 0)); err != nil {
		fixture.t.Fatal(err)
	}
	fixture.next += uint32(len(text)+1+3) &^ 3
	return at
}

// class plants the smallest record the reader accepts — a name, the word that
// says what it extends, no members and no dispatch table of its own — and
// answers its handle. The word is another record's handle or a name.
func (fixture *chainFixture) class(header uint32, name string, super uint32) uint32 {
	fixture.t.Helper()
	words := make([]uint32, javaClassHeader/4+3)
	words[0] = 0x21
	words[2] = fixture.text(name)
	words[4] = super
	words[17] = javaClassSentinel
	words[javaClassHeader/4+2] = header // the back-pointer that pairs handle and header
	data := make([]byte, len(words)*4)
	for index, word := range words {
		binary.LittleEndian.PutUint32(data[index*4:], word)
	}
	if err := fixture.client.core.Memory().Write(header, data); err != nil {
		fixture.t.Fatal(err)
	}
	return header + javaClassHeader
}

// chainEnds walks a class's recorded superclasses and reports whether the walk
// reaches a class with none, without trusting it to.
func chainEnds(class *javaRuntimeClass) bool {
	for steps := 0; steps < 64; steps++ {
		if class == nil {
			return true
		}
		class = class.Super
	}
	return false
}

// within runs something that is expected to return, and fails the test rather
// than hanging it if it does not.
func within(t *testing.T, what string, run func()) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		defer close(done)
		run()
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatalf("%s did not return", what)
	}
}

// A record whose superclass word is its own handle.
func TestJavaClassThatExtendsItselfIsRefused(t *testing.T) {
	fixture := newChainFixture(t)
	handle := fixture.class(chainClassFirst, "fixture/Self", chainClassFirst+javaClassHeader)

	_, err := fixture.client.prepareJavaClass(context.Background(), nil, handle)
	if err == nil || !strings.Contains(err.Error(), "fixture/Self") {
		t.Fatalf("a class that extends itself was prepared: %v", err)
	}
	for _, class := range fixture.client.javaRun.byHandle {
		if !chainEnds(class) {
			t.Fatalf("%s was left above itself", class.Name)
		}
	}
}

// Two records that name each other. The first attempt at either has to fail,
// because a title that got an answer would go on to use the class; and what the
// attempt registered on its way, which a later request is answered with as it
// is, has to be a chain that ends.
func TestJavaClassesThatExtendEachOtherAreRefused(t *testing.T) {
	fixture := newChainFixture(t)
	first := fixture.class(chainClassFirst, "fixture/First", chainClassSecond+javaClassHeader)
	second := fixture.class(chainClassSecond, "fixture/Second", chainClassFirst+javaClassHeader)

	_, err := fixture.client.prepareJavaClass(context.Background(), nil, first)
	if err == nil || !strings.Contains(err.Error(), "fixture/First") {
		t.Fatalf("a class in a closed chain was prepared: %v", err)
	}
	_, _ = fixture.client.prepareJavaClass(context.Background(), nil, second)
	if len(fixture.client.javaRun.byHandle) != 2 {
		t.Fatalf("the fixture's records were not both read: %d classes", len(fixture.client.javaRun.byHandle))
	}
	for _, class := range fixture.client.javaRun.byHandle {
		if !chainEnds(class) {
			t.Fatalf("%s was left above itself", class.Name)
		}
	}
}

// A platform class with no superclass yet is given one by name the first time
// a type check walks through it. A module that registers one of its own classes
// under that name, above a subclass of the platform class, would have the link
// close the chain — so the link is not made, and the check ends.
func TestJavaTypeCheckEndsWhenAModuleTakesAPlatformName(t *testing.T) {
	fixture := newChainFixture(t)
	client := fixture.client
	base, err := client.preparePlatformJavaClass("fixture/Base")
	if err != nil {
		t.Fatal(err)
	}
	// An application class named as the root of the platform's hierarchy,
	// extending the platform class whose own superclass that name is.
	usurper := fixture.class(chainClassFirst, "java/lang/Object", fixture.text("fixture/Base"))
	prepared, err := client.prepareJavaClass(context.Background(), nil, usurper)
	if err != nil {
		t.Fatal(err)
	}
	if prepared.Super != base || client.javaRun.byName["java/lang/Object"] != prepared {
		t.Fatalf("the fixture did not put the module's class above the platform's: super %v", prepared.Super)
	}
	absent := fixture.text("fixture/Absent")
	var answer uint32
	within(t, "a type check through the taken name", func() {
		answer, err = client.javaTypeCheck(base.Object, absent)
	})
	if err != nil || answer != 0 {
		t.Fatalf("the check answered %d, %v", answer, err)
	}
	if !chainEnds(base) || !chainEnds(prepared) {
		t.Fatal("the check left a chain that does not end")
	}
	// What the chain does hold is still found.
	within(t, "a type check against the class itself", func() {
		answer, err = client.javaTypeCheck(usurper, fixture.text("fixture/Base"))
	})
	if err != nil || answer != 1 {
		t.Fatalf("the module's class is no longer a fixture/Base: %d, %v", answer, err)
	}
}

// The layout is walked upwards by name, by a loop and by a recursion, so a
// class may not be laid out above itself there either.
func TestJavaLayoutRefusesAClassAboveItself(t *testing.T) {
	surface := &javaSurface{}
	lay := func(layout *javaLayout, name, super string) error {
		_, err := layout.layoutApplicationClass(javaClass{Name: name, SuperName: super, SuperHandle: 0x1000}, javaClassLayoutEntry{Name: name}, surface)
		return err
	}
	for _, test := range []struct {
		name    string
		classes [][2]string // name and superclass, in the order they are laid out
		refused bool
	}{
		{"a class over a platform class", [][2]string{{"Game", "java/util/Vector"}}, false},
		{"a class over another of the module's", [][2]string{{"Base", "java/lang/Object"}, {"Game", "Base"}}, false},
		{"a class laid out before the class it extends", [][2]string{{"Game", "Base"}, {"Base", "java/lang/Object"}}, false},
		{"a class over itself", [][2]string{{"Game", "Game"}}, true},
		{"two classes over each other", [][2]string{{"Game", "Base"}, {"Base", "Game"}}, true},
		{"three classes in a ring", [][2]string{{"A", "B"}, {"B", "C"}, {"C", "A"}}, true},
		// Stack is not laid out, so the walk leaves it by the specification's
		// own link, which is back to Vector.
		{"a platform name over its own subclass", [][2]string{{"java/util/Vector", "java/util/Stack"}}, true},
		{"the root given a superclass beneath it", [][2]string{{"Game", "java/lang/Object"}, {"java/lang/Object", "Game"}}, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			layout := newJavaLayout()
			var err error
			for _, class := range test.classes {
				if err = lay(layout, class[0], class[1]); err != nil {
					break
				}
			}
			if (err != nil) != test.refused {
				t.Fatalf("the layout answered %v", err)
			}
			// Whatever was accepted can be walked: by the loop that looks for
			// an inherited slot, and by the recursion that sizes a vtable.
			within(t, "a walk up the layout", func() {
				for name := range layout.classes {
					layout.findVirtual(name, "absent()V")
				}
				for _, name := range []string{"Game", "Base", "A", "java/util/Vector", "java/lang/Object"} {
					if _, laid := layout.classes[name]; laid {
						layout.vtableSize(name)
					}
				}
			})
		})
	}
}

// Several walks go up the specification's hierarchy by name with nothing to
// stop them but its end, so the table that holds it has to have one.
func TestJavaPlatformHierarchyEnds(t *testing.T) {
	for name := range javaPlatformSupers {
		steps := 0
		for current := name; current != ""; current = javaPlatformSuper(current) {
			if steps++; steps > len(javaPlatformSupers)+2 {
				t.Fatalf("the hierarchy above %s does not end", name)
			}
		}
	}
}
