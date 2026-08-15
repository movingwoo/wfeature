package lgt

import (
	"encoding/binary"
	"fmt"
)

// Answering the load call: the platform decides where a field sits in an
// object, which vtable slot a virtual method takes, and what address a static
// method is entered at, and writes those answers back for the compiled code to
// index with. See docs/lgt.md, "The Java interface table".
//
// The numbering is the identity map — entry *n* of a member table is answered
// with *n* — and that is a decision rather than a shortcut, because it is what
// makes the tables answerable at all. An entry carries a name and a descriptor
// and no class, so two entries can read `j:I` and three classes can declare
// `a(I)V`:
//
//   - A **virtual** entry names a slot, not a method body. A subclass that
//     overrides a method shares its superclass's slot, so every class that
//     answers to a name and descriptor answers at one index, and handing out
//     one index per entry is exactly right.
//   - A **field** entry names one field of one class. Giving each entry a slot
//     of its own is right for the same reason it is wasteful: an object then
//     carries a slot for every field reference in the title. That is the price
//     of a table that does not say which class it meant, and it is paid in
//     object size rather than in correctness.
//
// What the platform is not free to choose is the layout of a class the
// compiler already laid out — one rooted at `java/lang/Object`, whose vtable
// arrives inside its own record. Those are untouched; see java_class.go.

const (
	// javaSlotStaticMethod marks an SVC slot as "platform static method entry
	// n". The auxiliary-table encoding takes bit 24, so this takes bit 25 and
	// the two never collide.
	javaSlotStaticMethod uint32 = 1 << 25
	// javaSlotVirtual marks an SVC slot as "the vtable slot of a class this
	// platform has not implemented", which is what an unfilled dispatch entry
	// answers with.
	javaSlotVirtual uint32 = 1 << 26
	// A module that asks for more entries than this is misread rather than
	// large: the stub region is finite and one stub is spent per entry.
	maxJavaStaticMethods = 2048
)

func javaStaticMethodSlot(index uint32) uint32 { return javaSlotStaticMethod | index }

func javaStaticMethodParts(slot uint32) (uint32, bool) {
	if slot&javaSlotStaticMethod == 0 {
		return 0, false
	}
	return slot &^ javaSlotStaticMethod, true
}

// javaLink is what the load call left behind: the surface, and the answers.
type javaLink struct {
	surface *javaSurface
	layout  *javaLayout
}

// linkJavaSurface answers the load call. **Only the entries a class claims are
// written.** The member tables are shared between the platform's classes and
// the application's, and the application's half is not this call's to answer:
// it arrives one class at a time, on the per-class call, with the class record
// that says where its members go. Writing the whole table here would also
// overrun the array beside it — the field array of one local title has room for
// 150 entries where the table's own bounds suggest 179, and the extra 29 land
// in the static-method addresses.
func (client *Client) linkJavaSurface(surface *javaSurface, layout *javaLayout) error {
	if len(surface.StaticMethods) > maxJavaStaticMethods {
		return fmt.Errorf("java static method table has %d entries", len(surface.StaticMethods))
	}
	slots, err := layout.layoutPlatformClasses(surface)
	if err != nil {
		return err
	}
	if err := client.writeJavaSlots(surface.VirtualMethodsOut, slots); err != nil {
		return fmt.Errorf("write the java virtual method answers at %#x: %w",
			surface.VirtualMethodsOut, err)
	}
	// The static fields and the non-virtual methods are answered with their own
	// index. Which array a static field's number indexes, and what a
	// non-virtual method's stands for, are both still open; an index of its own
	// per entry is the answer that claims the least.
	for _, table := range []struct {
		label string
		out   uint32
		pick  func(javaAPIClass) javaRun
	}{
		{"static field", surface.StaticFieldsOut, func(class javaAPIClass) javaRun { return class.StaticFields }},
		{"method", surface.MethodsOut, func(class javaAPIClass) javaRun { return class.Methods }},
	} {
		identity := map[uint32]uint32{}
		for _, class := range surface.Classes {
			run := table.pick(class)
			for offset := uint32(0); offset < run.Count; offset++ {
				identity[run.Start+offset] = run.Start + offset
			}
		}
		if err := client.writeJavaSlots(table.out, identity); err != nil {
			return fmt.Errorf("write the java %s answers at %#x: %w", table.label, table.out, err)
		}
	}
	return client.writeJavaStaticMethodArray(surface)
}

// writeJavaSlots answers a run of an index table, one int16 per entry. Entries
// nothing claims are left alone: they belong to a class that has not been laid
// out yet.
func (client *Client) writeJavaSlots(out uint32, slots map[uint32]uint32) error {
	if len(slots) == 0 {
		return nil
	}
	if out == 0 {
		return fmt.Errorf("the module passed no array for %d entries", len(slots))
	}
	for index, slot := range slots {
		if slot > 0x7fff {
			return fmt.Errorf("entry %d was given slot %d, which is not an int16", index, slot)
		}
		data := make([]byte, 2)
		binary.LittleEndian.PutUint16(data, uint16(slot))
		if err := client.core.Memory().Write(out+index*2, data); err != nil {
			return err
		}
	}
	return nil
}

// writeJavaStaticMethodArray answers the one table that takes addresses rather
// than indices. Every entry gets a stub of its own, including the two null
// entries each class's run opens with: a stub that is never called costs
// sixteen bytes, and one that is called reports which method the module
// reached for, which is the whole value of answering this table before any of
// it is implemented.
func (client *Client) writeJavaStaticMethodArray(surface *javaSurface) error {
	count := len(surface.StaticMethods)
	if count == 0 {
		return nil
	}
	if surface.StaticMethodsOut == 0 {
		return fmt.Errorf("the module passed no static method array for %d entries", count)
	}
	data := make([]byte, count*4)
	for index := 0; index < count; index++ {
		stub, err := client.stub(svcCategoryJava, javaStaticMethodSlot(uint32(index)))
		if err != nil {
			return err
		}
		binary.LittleEndian.PutUint32(data[index*4:], stub)
	}
	return client.core.Memory().Write(surface.StaticMethodsOut, data)
}

// describeJavaStaticMethod names the entry a static-method stub stands for,
// which is what a failure at one has to report.
func (link *javaLink) describeJavaStaticMethod(index uint32) string {
	if link == nil || link.surface == nil || int(index) >= len(link.surface.StaticMethods) {
		return fmt.Sprintf("static method entry %d", index)
	}
	member := link.surface.StaticMethods[index]
	owner, known := link.surface.ownerOf(
		func(class javaAPIClass) javaRun { return class.StaticMethods }, index)
	name := member.String()
	if name == "" {
		// The two entries every class's run opens with carry no name. What
		// they are is not known; reporting the class and the position in its
		// run is what a later reading would start from.
		name = "the unnamed entry"
	}
	if !known {
		return fmt.Sprintf("%s (static method entry %d)", name, index)
	}
	return fmt.Sprintf("%s.%s (static method entry %d)", owner, name, index)
}

// unnamedStaticEntryOwner answers the class an unnamed static entry belongs to.
// Every platform class's run opens with two of them, and what they are is
// settled by what the module does with the answer: the class itself, which one
// call site hands straight to the allocator. See docs/lgt.md, "The two entries
// every class's run opens with".
func (link *javaLink) unnamedStaticEntry(index uint32) (string, uint32, bool) {
	if link == nil || link.surface == nil || int(index) >= len(link.surface.StaticMethods) {
		return "", 0, false
	}
	member := link.surface.StaticMethods[index]
	if member.Name != "" || member.Descriptor != "" {
		return "", 0, false
	}
	for _, class := range link.surface.Classes {
		if class.StaticMethods.contains(index) {
			return class.Name, index - class.StaticMethods.Start, true
		}
	}
	return "", 0, false
}
