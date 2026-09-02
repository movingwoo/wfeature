package skt

import (
	"testing"

	"github.com/movingwoo/wfeature/internal/jvm"
)

// The handset resolved a resource name from the root of the archive whether or
// not it began with a slash, and titles shipped against that. Three shapes
// appear in the local corpus and all three have to answer: the name the
// specification resolves against the asking class's package, the same name
// spelled from the root by a class that has a package of its own, and a name
// asked through a platform class that has no package a game's data could sit
// in.
func TestResourceNamesResolveAgainstThePackageAndTheRoot(t *testing.T) {
	runtime := startLifecycleFixture(t)
	defer runtime.Destroy(true)

	runtime.Archive.Entries["tk/images/Map0.lbm"] = []byte("root")
	runtime.Archive.Entries["tk/local/near.lbm"] = []byte("package")
	runtime.Archive.Entries["table.gft"] = []byte("bare")

	for _, probe := range []struct {
		name      string
		askedFrom string
		asked     string
		want      string
	}{
		{
			name:      "the package answers a relative name",
			askedFrom: "tk/local/Loader",
			asked:     "near.lbm",
			want:      "package",
		},
		{
			name:      "a relative name spelled from the root is found there",
			askedFrom: "tk/Kingdoms",
			asked:     "tk/images/Map0.lbm",
			want:      "root",
		},
		{
			name:      "a platform class with no package of its own falls back to the bare name",
			askedFrom: "java/lang/Runtime",
			asked:     "table.gft",
			want:      "bare",
		},
		{
			name:      "an absolute name is the root",
			askedFrom: "tk/Kingdoms",
			asked:     "/tk/images/Map0.lbm",
			want:      "root",
		},
	} {
		t.Run(probe.name, func(t *testing.T) {
			if got := readResource(t, runtime, probe.askedFrom, probe.asked); got != probe.want {
				t.Fatalf("%s asking for %q read %q, want %q", probe.askedFrom, probe.asked, got, probe.want)
			}
		})
	}

	// A name nothing answers is still a null rather than an error, because the
	// callers here catch their own exception and would hide one.
	if stream := openResource(t, runtime, "tk/Kingdoms", "nothing/here.lbm"); stream != nil {
		t.Fatalf("a name no entry answers opened %v", stream)
	}
}

func openResource(t *testing.T, runtime *Runtime, className, name string) *jvm.Object {
	t.Helper()
	classObject, err := runtime.VM.NewClassObject(className)
	if err != nil {
		t.Fatalf("NewClassObject(%q) error = %v", className, err)
	}
	result, err := runtime.VM.InvokeVirtual(classObject, "getResourceAsStream",
		"(Ljava/lang/String;)Ljava/io/InputStream;", jvm.ReferenceValue(runtime.VM.NewString(name)))
	if err != nil {
		t.Fatalf("getResourceAsStream(%q) error = %v", name, err)
	}
	stream, err := result.Reference()
	if err != nil {
		t.Fatal(err)
	}
	return stream
}

func readResource(t *testing.T, runtime *Runtime, className, name string) string {
	t.Helper()
	stream := openResource(t, runtime, className, name)
	if stream == nil {
		t.Fatalf("%s asking for %q got no stream", className, name)
	}
	var read []byte
	for {
		result, err := runtime.VM.InvokeVirtual(stream, "read", "()I")
		if err != nil {
			t.Fatalf("read() error = %v", err)
		}
		value, err := result.Int32()
		if err != nil {
			t.Fatal(err)
		}
		if value < 0 {
			return string(read)
		}
		read = append(read, byte(value))
	}
}
