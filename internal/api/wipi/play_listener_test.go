package wipi

import (
	"strings"
	"testing"

	"github.com/movingwoo/wfeature/internal/jvm"
)

func TestPlayListenerDeclaresStandardCallback(t *testing.T) {
	machine := newMachine(t)
	definition := playListenerDefinition()
	if want := uint16(jvm.AccessPublic | jvm.AccessInterface | jvm.AccessAbstract); definition.Access != want {
		t.Errorf("PlayListener access = %#x, want %#x", definition.Access, want)
	}
	const descriptor = "(Lorg/kwis/msp/media/Clip;II)V"
	declared := false
	for _, method := range definition.Methods {
		if method.Name == "playDone" {
			t.Error("PlayListener still declares the unsupported playDone callback")
		}
		if method.Name == "playUpdate" && method.Descriptor == descriptor {
			declared = true
			if want := uint16(jvm.AccessPublic | jvm.AccessAbstract); method.Access != want {
				t.Errorf("playUpdate access = %#x, want %#x", method.Access, want)
			}
		}
	}
	if !declared {
		t.Error("PlayListener does not declare playUpdate(Clip, int, int)")
	}
	// Resolve through the installed VM too. An abstract declaration has no
	// body, but it must resolve before the VM reports that missing body.
	listener := &jvm.Object{ClassName: PlayListenerClass}
	_, err := machine.InvokeSpecial(listener, PlayListenerClass, "playUpdate", descriptor,
		jvm.ReferenceValue(nil), jvm.IntValue(2), jvm.IntValue(0))
	if err == nil || !strings.Contains(err.Error(), "method has no code: "+PlayListenerClass+".playUpdate"+descriptor) {
		t.Errorf("installed playUpdate declaration did not resolve: %v", err)
	}
}

func TestPlayListenerConstantsAreReadable(t *testing.T) {
	machine := newMachine(t)
	fields := make(map[string]jvm.FieldDefinition)
	for _, field := range playListenerDefinition().Fields {
		fields[field.Name] = field
	}
	for _, test := range []struct {
		name string
		want int32
	}{
		{"ERROR", -1},
		{"END_OF_DATA", 1},
		{"START", 2},
		{"STOP", 3},
		{"PAUSE", 4},
		{"RESUME", 5},
		{"RECORD", 6},
		{"FULL_OF_DATA", 7},
	} {
		t.Run(test.name, func(t *testing.T) {
			field, ok := fields[test.name]
			if !ok || field.Descriptor != "I" || field.Access != jvm.AccessPublic|jvm.AccessStatic|jvm.AccessFinal {
				t.Errorf("PlayListener.%s is not declared public static final int", test.name)
			}
			// Read the VM field instead of a Java compiler's inlined constant.
			value, err := machine.StaticField(PlayListenerClass, test.name, "I")
			if err != nil {
				t.Fatalf("PlayListener.%s: %v", test.name, err)
			}
			if got, err := value.Int32(); err != nil || got != test.want {
				t.Errorf("PlayListener.%s = %v, %v; want %d", test.name, value, err, test.want)
			}
		})
	}
}
