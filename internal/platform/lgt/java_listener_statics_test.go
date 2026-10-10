package lgt

import "testing"

func TestLGTWIPIListenerImportedConstants(t *testing.T) {
	f := newLGTWIPIListenerFixture(t)
	want := []struct {
		name  string
		value int32
	}{
		{"ERROR", -1}, {"END_OF_DATA", 1}, {"START", 2}, {"STOP", 3},
		{"PAUSE", 4}, {"RESUME", 5}, {"RECORD", 6}, {"FULL_OF_DATA", 7},
	}
	// Exercise the same field-import numbering and class allocation as a
	// compiled module that loads constants instead of inlining them.
	surface := &javaSurface{Classes: []javaAPIClass{{Name: javaPlayListenerClass, StaticFields: javaRun{Count: uint32(len(want))}}}}
	for _, field := range want {
		surface.StaticFields = append(surface.StaticFields, javaMemberRef{Name: field.name, Descriptor: "I"})
	}
	layout := f.client.javaLink.layout
	answers, err := layout.layoutPlatformStatics(surface)
	if err != nil {
		t.Fatal(err)
	}
	class, err := f.client.preparePlatformJavaClass(javaPlayListenerClass)
	if err != nil {
		t.Fatal(err)
	}
	for index, field := range want {
		address := class.dataBlock + (javaClassDataWords+answers[uint32(index)])*4
		if got := int32(f.word(address)); got != field.value {
			t.Errorf("imported PlayListener.%s = %d, want %d", field.name, got, field.value)
		}
	}
}
