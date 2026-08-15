package lgt

import (
	"strings"
	"testing"
)

// The layout fixture is one application class extending a platform class, laid
// out the way a real module's is: the record declares how large the object and
// the vtable came out, the layout entry says how many of each the class itself
// declares, and the platform's job is to number the difference. One of the
// class's virtual entries repeats a method the platform class declares, which
// is the case counting alone gets wrong.
func javaLayoutFixture() (javaClass, javaClassLayoutEntry, *javaSurface) {
	surface := &javaSurface{
		VirtualMethods: []javaMemberRef{
			{Name: "getHeight", Descriptor: "()I"},
			{Name: "repaint", Descriptor: "()V"},
			{Name: "paint", Descriptor: "(Lorg/kwis/msp/lcdui/Graphics;)V"},
			{Name: "repaint", Descriptor: "()V"},
			{Name: "a", Descriptor: "(I)V"},
		},
		Classes: []javaAPIClass{{
			Name:           "org/kwis/msp/lcdui/Card",
			VirtualMethods: javaRun{Start: 0, Count: 2},
		}},
	}
	record := javaClass{
		Name: "o", Super: "org/kwis/msp/lcdui/Card",
		// Eleven fields in an object of 24 words, and three virtual entries in
		// a vtable of 26 — one of the three is an override, so only two of them
		// are new.
		InstanceSize: 24, VTableSize: 26,
	}
	entry := javaClassLayoutEntry{
		Name:           "o",
		Fields:         javaRun{Start: 0, Count: 11},
		VirtualMethods: javaRun{Start: 2, Count: 3},
	}
	return record, entry, surface
}

// A field's slot is counted back from the instance size, which is what reads
// the platform superclass's own size out of the module.
func TestJavaFieldSlotsAreCountedBackFromTheInstanceSize(t *testing.T) {
	record, entry, surface := javaLayoutFixture()
	layout := newJavaLayout()
	if _, err := layout.layoutPlatformClasses(surface); err != nil {
		t.Fatalf("layoutPlatformClasses() error = %v", err)
	}
	answers, err := layout.layoutApplicationClass(record, entry, surface)
	if err != nil {
		t.Fatalf("layoutApplicationClass() error = %v", err)
	}
	if len(answers.Fields) != 11 {
		t.Fatalf("answered %d fields, want 11", len(answers.Fields))
	}
	for index, want := range map[uint32]uint32{0: 13, 10: 23} {
		if slot := answers.Fields[index]; slot != want {
			t.Errorf("field entry %d was given slot %d, want %d", index, slot, want)
		}
	}
}

// An override shares its superclass's slot, and only the new methods take new
// ones. Counting the whole run as new would put every slot two too low and
// send each call into the wrong method.
func TestJavaOverridesShareTheSuperclassSlot(t *testing.T) {
	record, entry, surface := javaLayoutFixture()
	layout := newJavaLayout()
	if _, err := layout.layoutPlatformClasses(surface); err != nil {
		t.Fatalf("layoutPlatformClasses() error = %v", err)
	}
	// The platform class numbers its own from Object's eleven.
	card := layout.classes["org/kwis/msp/lcdui/Card"]
	if slot := card.Virtual["repaint()V"]; slot != javaObjectVTableSize+1 {
		t.Fatalf("the platform's repaint()V took slot %d", slot)
	}
	answers, err := layout.layoutApplicationClass(record, entry, surface)
	if err != nil {
		t.Fatalf("layoutApplicationClass() error = %v", err)
	}
	for index, want := range map[uint32]uint32{
		2: 24,                       // paint, new
		3: javaObjectVTableSize + 1, // repaint, the superclass's slot
		4: 25,                       // a(I)V, new
	} {
		if slot := answers.Virtual[index]; slot != want {
			t.Errorf("virtual entry %d was given slot %d, want %d", index, slot, want)
		}
	}
	// The superclass's size is what the class's own methods start at, and the
	// module is the only thing that says what it is.
	if card.VTableSize != 24 {
		t.Errorf("the platform class was measured at %d slots, want 24", card.VTableSize)
	}
}

// The first subclass of a platform class is what measures it, so its own
// numbering cannot be checked against anything — but the platform class's
// declared methods have to fit below where the subclass starts, and a size that
// leaves no room for them is a misreading. Saying so is worth more than a title
// that dispatches into the wrong slot.
func TestJavaLayoutRefusesASizeThePlatformClassDoesNotFitIn(t *testing.T) {
	record, entry, surface := javaLayoutFixture()
	record.VTableSize = 13 // two new methods leaves 11, and the platform uses 11 and 12
	layout := newJavaLayout()
	if _, err := layout.layoutPlatformClasses(surface); err != nil {
		t.Fatalf("layoutPlatformClasses() error = %v", err)
	}
	if _, err := layout.layoutApplicationClass(record, entry, surface); err == nil {
		t.Fatal("a vtable one slot short was accepted")
	}
}

// A class the application extends twice has to agree with itself: the second
// subclass counts its methods from the size the first one measured.
func TestJavaApplicationSuperclassSizesAreChecked(t *testing.T) {
	record, entry, surface := javaLayoutFixture()
	layout := newJavaLayout()
	if _, err := layout.layoutPlatformClasses(surface); err != nil {
		t.Fatalf("layoutPlatformClasses() error = %v", err)
	}
	if _, err := layout.layoutApplicationClass(record, entry, surface); err != nil {
		t.Fatalf("layoutApplicationClass() error = %v", err)
	}
	// A subclass of that class, declaring one field and no methods.
	child := javaClass{Name: "d", SuperName: "o", InstanceSize: 25, VTableSize: 26}
	childEntry := javaClassLayoutEntry{Name: "d", Fields: javaRun{Start: 11, Count: 1}}
	answers, err := layout.layoutApplicationClass(child, childEntry, surface)
	if err != nil {
		t.Fatalf("layoutApplicationClass() error = %v", err)
	}
	if slot := answers.Fields[11]; slot != 24 {
		t.Errorf("the subclass's field took slot %d, want 24", slot)
	}
	child.InstanceSize = 30 // a gap between the superclass's fields and its own
	if _, err := layout.layoutApplicationClass(child, childEntry, surface); err == nil {
		t.Fatal("a field slot that does not follow the superclass was accepted")
	}
}

// A platform class the module extends twice has to leave both subclasses the
// same room. The first subclass is what measures it; the second is the only
// thing that can check the measurement, and disagreeing means one of the two
// records was read wrong.
func TestJavaPlatformSuperclassIsMeasuredOnce(t *testing.T) {
	record, entry, surface := javaLayoutFixture()
	layout := newJavaLayout()
	if _, err := layout.layoutPlatformClasses(surface); err != nil {
		t.Fatalf("layoutPlatformClasses() error = %v", err)
	}
	if _, err := layout.layoutApplicationClass(record, entry, surface); err != nil {
		t.Fatalf("layoutApplicationClass() error = %v", err)
	}
	// A second class extending the same platform class, whose own record puts
	// the platform's vtable one slot further along.
	sibling := javaClass{Name: "e", Super: "org/kwis/msp/lcdui/Card", InstanceSize: 13, VTableSize: 27}
	siblingEntry := javaClassLayoutEntry{
		Name:           "e",
		VirtualMethods: javaRun{Start: 2, Count: 3},
	}
	_, err := layout.layoutApplicationClass(sibling, siblingEntry, surface)
	if err == nil {
		t.Fatal("two subclasses measuring the platform class differently were accepted")
	}
	if !strings.Contains(err.Error(), "org/kwis/msp/lcdui/Card was measured") {
		t.Errorf("the disagreement was reported as %v", err)
	}
}
