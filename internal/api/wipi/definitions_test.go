package wipi

import (
	"testing"

	"github.com/movingwoo/wfeature/internal/api/midp"
	"github.com/movingwoo/wfeature/internal/jvm"
)

// The definitions are a table, so a mistake in one — a descriptor that does
// not parse, a method declared twice, a superclass that is not installed — is
// the same mistake in every session. Installing them is what catches it here
// rather than at a title's first call.
func TestDefineInstallsTheSurfaceOverMIDP(t *testing.T) {
	machine := newMachine(t)
	if err := Define(machine); err == nil {
		t.Fatal("defining the library twice succeeded")
	}
}

// The whole design is that a WIPI class is its MIDP counterpart with the
// standard's own additions on top: the display path, the paint scheduling and
// the pixels are the ones already built. If one of these chains breaks, a Jlet
// stops reaching them and nothing else says so.
func TestEachClassExtendsItsMIDPCounterpart(t *testing.T) {
	machine := newMachine(t)
	for _, probe := range []struct{ class, parent string }{
		{JletClass, midp.MIDletClass},
		{CardClass, midp.CanvasClass},
		{CardClass, midp.DisplayableClass},
		{DisplayClass, midp.DisplayClass},
		{GraphicsClass, midp.GraphicsClass},
		{ImageClass, midp.ImageClass},
		{FontClass, midp.FontClass},
	} {
		subclass, err := machine.IsSubclassOf(probe.class, probe.parent)
		if err != nil {
			t.Fatalf("IsSubclassOf(%s, %s) error = %v", probe.class, probe.parent, err)
		}
		if !subclass {
			t.Fatalf("%s does not extend %s", probe.class, probe.parent)
		}
	}
}

// IsJlet is what a Host asks of an archive's main class, and the two kinds of
// application are packaged identically: only the class answers.
func TestIsJletSeparatesTheTwoApplicationClasses(t *testing.T) {
	machine := newMachine(t)
	jlet, err := IsJlet(machine, JletClass)
	if err != nil || !jlet {
		t.Fatalf("IsJlet(%s) = %v, %v", JletClass, jlet, err)
	}
	midlet, err := IsJlet(machine, midp.MIDletClass)
	if err != nil || midlet {
		t.Fatalf("IsJlet(%s) = %v, %v", midp.MIDletClass, midlet, err)
	}
	// A class nobody loaded is not a Jlet, and asking must not be an error: a
	// Host asks before it knows what the archive holds.
	if _, err := IsJlet(machine, ""); err != nil {
		t.Fatalf("IsJlet(\"\") error = %v", err)
	}
}

func newMachine(t *testing.T) *jvm.VM {
	t.Helper()
	machine := jvm.New(nil, jvm.Options{})
	if err := midp.Define(machine); err != nil {
		t.Fatalf("midp.Define() error = %v", err)
	}
	if err := Define(machine); err != nil {
		t.Fatalf("Define() error = %v", err)
	}
	return machine
}
