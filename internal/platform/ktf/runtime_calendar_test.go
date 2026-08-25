package ktf

import (
	"testing"
)

// `java/util/GregorianCalendar` has to extend `java/util/Calendar` here, not
// `java/lang/Object`. A title that constructs one directly rather than through
// the factory then dispatches `Calendar.get` on it, and a virtual dispatch
// indexes the *receiver's* vtable at the slot the declaring class numbered: on
// the loader's Object-only fallback that slot is past the end of the table, and
// the bytes there are the name string the record was allocated with. One title
// followed those characters as a pointer and faulted.
func TestGregorianCalendarExtendsCalendarAndCarriesItsMethods(t *testing.T) {
	client, runtime := newTestRuntime(t)

	record, err := runtime.ensureJavaClass("java/util/GregorianCalendar")
	if err != nil {
		t.Fatalf("ensureJavaClass() error = %v", err)
	}
	metadata, ok := client.JVM().AOTClassAt(record)
	if !ok {
		t.Fatal("the class this platform registered is not a class record")
	}
	if metadata.SuperName != "java/util/Calendar" {
		t.Errorf("super = %q, want java/util/Calendar", metadata.SuperName)
	}
	calendar, err := runtime.ensureJavaClass("java/util/Calendar")
	if err != nil {
		t.Fatal(err)
	}
	parent, ok := client.JVM().AOTClassAt(calendar)
	if !ok {
		t.Fatal("java/util/Calendar is not a class record")
	}
	// Every slot the superclass numbered has to be in the subclass's table, or
	// a dispatch through one of them reads past its end.
	if len(metadata.VTable) < len(parent.VTable) {
		t.Fatalf("vtable slots = %d, want at least the %d java/util/Calendar has",
			len(metadata.VTable), len(parent.VTable))
	}
}
