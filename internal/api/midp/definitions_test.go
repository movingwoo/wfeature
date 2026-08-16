package midp

import (
	"strings"
	"testing"

	"github.com/movingwoo/wfeature/internal/jvm"
)

// The definitions are a table, so a mistake in one — a descriptor that does
// not parse, a method declared twice — is the same mistake in every session.
// Installing them is what catches it here rather than at a game's first call.
func TestDefineInstallsTheSurface(t *testing.T) {
	machine := jvm.New(nil, jvm.Options{})
	if err := Define(machine); err != nil {
		t.Fatalf("Define() error = %v", err)
	}
	if err := Define(machine); err == nil {
		t.Fatal("defining the library twice succeeded")
	}

	chains := []struct {
		class, parent string
	}{
		{CanvasClass, DisplayableClass},
		{GameCanvasClass, CanvasClass},
		{ListClass, ScreenClass},
		{TextFieldClass, ItemClass},
		{ConnectionNotFoundExceptionClass, jvm.IOExceptionClass},
		{RecordStoreNotFoundExceptionClass, RecordStoreExceptionClass},
	}
	for _, chain := range chains {
		related, err := machine.IsSubclassOf(chain.class, chain.parent)
		if err != nil {
			t.Errorf("IsSubclassOf(%q, %q) error = %v", chain.class, chain.parent, err)
			continue
		}
		if !related {
			t.Errorf("%s does not extend %s", chain.class, chain.parent)
		}
	}
}

// A constant a game reads out of a class has to be there before anything
// reads it, including the ones that are objects the runtime makes.
func TestDefinedConstantsAreReadable(t *testing.T) {
	machine := jvm.New(nil, jvm.Options{})
	if err := Define(machine); err != nil {
		t.Fatalf("Define() error = %v", err)
	}

	value, err := machine.StaticField(CommandClass, "SCREEN", "I")
	if err != nil {
		t.Fatalf("Command.SCREEN error = %v", err)
	}
	if number, _ := value.Int32(); number != CommandScreen {
		t.Errorf("Command.SCREEN = %d, want %d", number, CommandScreen)
	}

	locator, err := machine.StaticField(ManagerClass, "TONE_DEVICE_LOCATOR", "Ljava/lang/String;")
	if err != nil {
		t.Fatalf("Manager.TONE_DEVICE_LOCATOR error = %v", err)
	}
	object, err := locator.Reference()
	if err != nil {
		t.Fatal(err)
	}
	if text, _ := jvm.StringText(object); text != ToneDeviceLocator {
		t.Errorf("Manager.TONE_DEVICE_LOCATOR = %q, want %q", text, ToneDeviceLocator)
	}

	// The alert types are compared by identity, so each has to be its own
	// object and none of them may be null.
	seen := make(map[*jvm.Object]string)
	for _, name := range []string{"INFO", "WARNING", "ERROR", "ALARM", "CONFIRMATION"} {
		value, err := machine.StaticField(AlertTypeClass, name, "Ljavax/microedition/lcdui/AlertType;")
		if err != nil {
			t.Fatalf("AlertType.%s error = %v", name, err)
		}
		object, err := value.Reference()
		if err != nil {
			t.Fatal(err)
		}
		if object == nil {
			t.Fatalf("AlertType.%s is null", name)
		}
		if other, duplicate := seen[object]; duplicate {
			t.Errorf("AlertType.%s is the same object as AlertType.%s", name, other)
		}
		seen[object] = name
	}

	// List.SELECT_COMMAND is created by the class initializer, and a game
	// compares the command it is handed against it. Making one needs the
	// platform's Command state, which a bare VM does not have, so this stands
	// in for the platform the way skt's registration does.
	if err := machine.RegisterNative(CommandClass, "init", "(Ljava/lang/String;Ljava/lang/String;II)V",
		func(*jvm.VM, []jvm.Value) (jvm.Value, error) { return jvm.VoidValue(), nil }); err != nil {
		t.Fatalf("RegisterNative(Command.init) error = %v", err)
	}
	selected, err := machine.StaticField(ListClass, "SELECT_COMMAND", "Ljavax/microedition/lcdui/Command;")
	if err != nil {
		t.Fatalf("List.SELECT_COMMAND error = %v", err)
	}
	command, err := selected.Reference()
	if err != nil {
		t.Fatal(err)
	}
	if command == nil || command.ClassName != CommandClass {
		t.Fatalf("List.SELECT_COMMAND = %v, want a Command", command)
	}
}

// getGameAction is the one input mapping this layer owns rather than the
// platform: a game reads the action, not the key.
func TestGameActionMapping(t *testing.T) {
	machine := jvm.New(nil, jvm.Options{})
	if err := Define(machine); err != nil {
		t.Fatalf("Define() error = %v", err)
	}
	canvas := &jvm.Object{ClassName: CanvasClass, Fields: map[string]jvm.Value{}}
	cases := map[int32]int32{
		141: gameActionUp,
		'2': gameActionUp,
		146: gameActionDown,
		'8': gameActionDown,
		148: gameActionFire,
		'1': gameActionA,
		'0': 0,
	}
	for code, want := range cases {
		result, err := machine.InvokeVirtual(canvas, "getGameAction", "(I)I", jvm.IntValue(code))
		if err != nil {
			t.Fatalf("getGameAction(%d) error = %v", code, err)
		}
		if action, _ := result.Int32(); action != want {
			t.Errorf("getGameAction(%d) = %d, want %d", code, action, want)
		}
	}
}

// A method the platform has to fill in must be declared native and left
// without a body, or the platform's registration collides with this layer's.
func TestPlatformNativesAreLeftOpen(t *testing.T) {
	machine := jvm.New(nil, jvm.Options{})
	if err := Define(machine); err != nil {
		t.Fatalf("Define() error = %v", err)
	}
	registrations := []struct {
		class, name, descriptor string
	}{
		{CanvasClass, "repaint", "(IIII)V"},
		{DisplayableClass, "getWidth", "()I"},
		{RecordStoreClass, "getRecord", "(I)[B"},
		{MIDletClass, "notifyDestroyed", "()V"},
	}
	for _, registration := range registrations {
		err := machine.RegisterNative(registration.class, registration.name, registration.descriptor,
			func(*jvm.VM, []jvm.Value) (jvm.Value, error) { return jvm.VoidValue(), nil })
		if err != nil {
			t.Errorf("RegisterNative(%s.%s%s) error = %v",
				registration.class, registration.name, registration.descriptor, err)
		}
	}

	// And one this layer does implement stays this layer's.
	err := machine.RegisterNative(CanvasClass, "getGameAction", "(I)I",
		func(*jvm.VM, []jvm.Value) (jvm.Value, error) { return jvm.IntValue(0), nil })
	if err == nil || !strings.Contains(err.Error(), "already registered") {
		t.Fatalf("registering over a library body error = %v, want a refusal", err)
	}
}
