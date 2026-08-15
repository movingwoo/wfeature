package ktf

import (
	"encoding/binary"
	"testing"
	"unicode/utf16"

	"github.com/movingwoo/wfeature/internal/jvm"
)

// newGuestString allocates a bound instance of a runtime-owned class the way a
// guest `new` does: the payload exists before the constructor runs.
func newGuestString(t *testing.T, runtime *initializationRuntime) (uint32, *jvm.Object) {
	t.Helper()
	classAddress, err := runtime.ensureJavaClass("java/lang/String")
	if err != nil {
		t.Fatal(err)
	}
	address, object, err := runtime.allocateAOTInstance(classAddress)
	if err != nil {
		t.Fatal(err)
	}
	return address, object
}

// constructGuestString runs the constructor a guest would run over an instance
// it allocated itself, which is the only place a string's characters are
// decided after its payload already exists.
func constructGuestString(t *testing.T, client *Client, object *jvm.Object, text string) {
	t.Helper()
	bytes, err := client.JVM().NewArray(jvm.Type{Kind: jvm.TypeByte}, int32(len(text)))
	if err != nil {
		t.Fatal(err)
	}
	values := make([]jvm.Value, len(text))
	for index := range values {
		values[index] = jvm.IntValue(int32(text[index]))
	}
	if err := jvm.SetArrayRange(bytes, 0, values); err != nil {
		t.Fatal(err)
	}
	if _, err := client.JVM().InvokeSpecial(object, "java/lang/String", "<init>", "([B)V", jvm.ReferenceValue(bytes)); err != nil {
		t.Fatal(err)
	}
}

func readGuestStringText(t *testing.T, runtime *initializationRuntime, address uint32) string {
	t.Helper()
	value, offset, count, err := runtime.readGuestStringFields(address)
	if err != nil {
		t.Fatal(err)
	}
	units, valid, err := runtime.readGuestStringUnits(value, offset, count)
	if err != nil {
		t.Fatal(err)
	}
	if !valid {
		t.Fatalf("guest string view value=%#x offset=%d count=%d is not readable", value, offset, count)
	}
	return string(utf16.Decode(units))
}

// A guest that constructs a string itself reads its characters through the
// published fields, and the constructor is what decides them.
func TestConstructorPublishesGuestStringFields(t *testing.T) {
	client, runtime := newTestRuntime(t)
	address, object := newGuestString(t, runtime)

	if value, _, count, err := runtime.readGuestStringFields(address); err != nil {
		t.Fatal(err)
	} else if value != 0 || count != 0 {
		t.Fatalf("fresh instance published value=%#x count=%d, want an empty view", value, count)
	}

	constructGuestString(t, client, object, "abc")
	constructor := runtimeJavaMethod{class: "java/lang/String", name: "<init>", descriptor: "([B)V"}
	if err := runtime.publishGuestFields(object, constructor); err != nil {
		t.Fatal(err)
	}
	if text := readGuestStringText(t, runtime, address); text != "abc" {
		t.Fatalf("published guest string = %q, want %q", text, "abc")
	}
}

// Publishing is not repeated for a payload that already spells the same
// characters, so a constructor called on an already-published string does not
// allocate a second character array.
func TestPublishIsSkippedWhenGuestFieldsAlreadyAgree(t *testing.T) {
	client, runtime := newTestRuntime(t)
	address, object := newGuestString(t, runtime)
	constructGuestString(t, client, object, "abc")
	constructor := runtimeJavaMethod{class: "java/lang/String", name: "<init>", descriptor: "([B)V"}

	written, err := publishGuestString(runtime, address, object)
	if err != nil {
		t.Fatal(err)
	}
	if !written {
		t.Fatal("the first publish wrote nothing")
	}
	before, _, _, err := runtime.readGuestStringFields(address)
	if err != nil {
		t.Fatal(err)
	}
	if err := runtime.publishGuestFields(object, constructor); err != nil {
		t.Fatal(err)
	}
	after, _, _, err := runtime.readGuestStringFields(address)
	if err != nil {
		t.Fatal(err)
	}
	if before != after {
		t.Fatalf("second publish moved the content array from %#x to %#x", before, after)
	}
}

// A method that cannot change a published field does not pay for a publish.
func TestPublishIgnoresMethodsThatCannotMutate(t *testing.T) {
	client, runtime := newTestRuntime(t)
	address, object := newGuestString(t, runtime)
	constructGuestString(t, client, object, "abc")

	reader := runtimeJavaMethod{class: "java/lang/String", name: "charAt", descriptor: "(I)C"}
	if err := runtime.publishGuestFields(object, reader); err != nil {
		t.Fatal(err)
	}
	if value, _, _, err := runtime.readGuestStringFields(address); err != nil {
		t.Fatal(err)
	} else if value != 0 {
		t.Fatalf("charAt published a content array at %#x", value)
	}
}

// The detector is what names the moment a title starts writing a published
// field, so it has to see a guest write that a boundary crossing then reads.
func TestAdoptReportsAGuestWriteToAPublishedField(t *testing.T) {
	client, runtime := newTestRuntime(t)
	address, object := newGuestString(t, runtime)
	constructGuestString(t, client, object, "abc")
	if _, err := publishGuestString(runtime, address, object); err != nil {
		t.Fatal(err)
	}
	if diverged, err := adoptGuestString(runtime, address, object); err != nil {
		t.Fatal(err)
	} else if diverged {
		t.Fatal("a freshly published string reported a divergence")
	}

	text := client.JVM().NewString("hello")
	if err := runtime.ensureResultBound(text); err != nil {
		t.Fatal(err)
	}
	textAddress, ok := client.JVM().AOTAddress(text)
	if !ok {
		t.Fatal("the string was not bound")
	}
	// The guest points the view at another string's characters, which is the
	// shape of a write this cannot see any other way.
	value, _, _, err := runtime.readGuestStringFields(textAddress)
	if err != nil {
		t.Fatal(err)
	}
	payload := make([]byte, 12)
	binary.LittleEndian.PutUint32(payload[0:], value)
	binary.LittleEndian.PutUint32(payload[8:], 5)
	if err := client.core.Memory().Write(address+javaInstanceSize+javaInstanceHeader, payload); err != nil {
		t.Fatal(err)
	}
	if diverged, err := adoptGuestString(runtime, address, object); err != nil {
		t.Fatal(err)
	} else if !diverged {
		t.Fatal("a guest write to a published field went unreported")
	}
}

// A view the character array cannot hold is refused rather than read, because
// a length this cannot trust is exactly what must not be treated as content.
func TestGuestStringUnitsRefuseAViewOutsideTheArray(t *testing.T) {
	client, runtime := newTestRuntime(t)
	address, object := newGuestString(t, runtime)
	constructGuestString(t, client, object, "abc")
	if _, err := publishGuestString(runtime, address, object); err != nil {
		t.Fatal(err)
	}
	value, _, _, err := runtime.readGuestStringFields(address)
	if err != nil {
		t.Fatal(err)
	}
	for _, view := range []struct {
		name          string
		offset, count uint32
	}{
		{"count past the array", 0, 4096},
		{"offset past the array", 4096, 0},
		{"view crossing the end", 1, 4096},
	} {
		if _, valid, err := runtime.readGuestStringUnits(value, view.offset, view.count); err != nil {
			t.Fatalf("%s: %v", view.name, err)
		} else if valid {
			t.Fatalf("%s was accepted", view.name)
		}
	}
}

// Every class that publishes an instance field to the guest has two storages
// for it, so every one of them needs an entry in the synchronization table.
// This is the tripwire the deliberately incomplete note in docs/ktf.md points
// at: adding a published field without deciding how it stays in step fails
// here rather than diverging in a game.
func TestFieldSyncTableCoversPublishedInstanceFields(t *testing.T) {
	for name, class := range runtimeJavaClasses {
		published := make([]string, 0, len(class.fields))
		for _, field := range class.fields {
			if field.accessFlags&0x0008 != 0 {
				// A static field's value word lives inside its own record,
				// which is one storage rather than two.
				continue
			}
			published = append(published, field.name)
		}
		if len(published) == 0 {
			continue
		}
		sync, ok := fieldSyncs[name]
		if !ok {
			t.Errorf("%s publishes instance fields %v with no entry in fieldSyncs", name, published)
			continue
		}
		if sync.publish == nil {
			t.Errorf("%s has no publish for its instance fields %v", name, published)
		}
		if sync.adopt == nil {
			t.Errorf("%s has no adopt for its instance fields %v", name, published)
		}
		if len(sync.mutators) == 0 {
			t.Errorf("%s names no method after which its fields %v are republished", name, published)
		}
	}
	for name := range fieldSyncs {
		class, ok := runtimeJavaClasses[name]
		if !ok {
			t.Errorf("fieldSyncs names %s, which is not a runtime Java class", name)
			continue
		}
		if class.instanceSize == 0 {
			t.Errorf("fieldSyncs names %s, whose instances publish no payload", name)
		}
	}
}
