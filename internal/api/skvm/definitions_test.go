package skvm

import (
	"testing"

	"github.com/movingwoo/wfeature/internal/api/midp"
	"github.com/movingwoo/wfeature/internal/jvm"
)

func defineLibraries(t *testing.T) *jvm.VM {
	t.Helper()
	machine := jvm.New(nil, jvm.Options{})
	// SKVM is built on MIDP — Graphics2D wraps a MIDP Graphics — so both go on
	// the VM in the order the platform installs them.
	if err := midp.Define(machine); err != nil {
		t.Fatalf("midp.Define() error = %v", err)
	}
	if err := Define(machine); err != nil {
		t.Fatalf("Define() error = %v", err)
	}
	return machine
}

func TestDefineInstallsTheSurface(t *testing.T) {
	machine := defineLibraries(t)
	chains := []struct {
		class, parent string
	}{
		{FileInputStreamClass, jvm.InputStreamClass},
		{FileOutputStreamClass, jvm.OutputStreamClass},
		{UnsupportedFormatExceptionClass, "java/lang/Exception"},
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

	// The clip the runtime hands back has to be the interface a title's own
	// code declares, or its variable will not hold what it is given.
	clip := &jvm.Object{ClassName: RuntimeAudioClipClass, Fields: map[string]jvm.Value{}}
	if !machine.IsInstance(clip, AudioClipClass) {
		t.Errorf("%s is not an %s", RuntimeAudioClipClass, AudioClipClass)
	}
}

// A title opens its save by mode bits it reads out of the class, so the
// constants have to be the handset's.
func TestFileConstants(t *testing.T) {
	machine := defineLibraries(t)
	cases := map[string]int32{
		"READ":       1,
		"WRITE":      2,
		"READ_WRITE": 3,
		"SEEK_SET":   0,
		"SEEK_CUR":   1,
		"SEEK_END":   2,
	}
	for name, want := range cases {
		value, err := machine.StaticField(XFileClass, name, "I")
		if err != nil {
			t.Fatalf("XFile.%s error = %v", name, err)
		}
		if number, _ := value.Int32(); number != want {
			t.Errorf("XFile.%s = %d, want %d", name, number, want)
		}
	}
}

// A stream constructor opens a file through XFile, and the mode is what
// decides whether a title's save is readable or gets truncated. The file
// natives belong to the platform, so this stands in for them and records what
// the library asked for.
func TestStreamsOpenThroughXFile(t *testing.T) {
	machine := defineLibraries(t)
	var openedName string
	var openedMode int32
	if err := machine.RegisterNative(XFileClass, "initName", "(Ljava/lang/String;I)V",
		func(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
			name, err := arguments[1].Reference()
			if err != nil {
				return jvm.VoidValue(), err
			}
			openedName, _ = jvm.StringText(name)
			openedMode, err = arguments[2].Int32()
			return jvm.VoidValue(), err
		}); err != nil {
		t.Fatalf("RegisterNative(XFile.initName) error = %v", err)
	}

	if _, err := machine.NewObject(FileInputStreamClass, "(Ljava/lang/String;)V",
		jvm.ReferenceValue(machine.NewString("save.dat"))); err != nil {
		t.Fatalf("new FileInputStream(String) error = %v", err)
	}
	if openedName != "save.dat" || openedMode != xfileRead {
		t.Errorf("input stream opened %q mode %d, want %q mode %d", openedName, openedMode, "save.dat", xfileRead)
	}

	if _, err := machine.NewObject(FileOutputStreamClass, "(Ljava/lang/String;)V",
		jvm.ReferenceValue(machine.NewString("save.dat"))); err != nil {
		t.Fatalf("new FileOutputStream(String) error = %v", err)
	}
	if openedMode != xfileWrite {
		t.Errorf("output stream opened mode %d, want %d", openedMode, xfileWrite)
	}

	// The textual mode is the C-style spelling, and it has to reach the same
	// bits.
	if _, err := machine.NewObject(XFileClass, "(Ljava/lang/String;Ljava/lang/String;)V",
		jvm.ReferenceValue(machine.NewString("save.dat")), jvm.ReferenceValue(machine.NewString("rw"))); err != nil {
		t.Fatalf("new XFile(String, String) error = %v", err)
	}
	if openedMode != xfileRead|xfileWrite {
		t.Errorf(`XFile(name, "rw") opened mode %d, want %d`, openedMode, xfileRead|xfileWrite)
	}
}
