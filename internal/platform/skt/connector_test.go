package skt

import (
	_ "embed"
	"testing"

	"github.com/movingwoo/wfeature/internal/api/midp"
	"github.com/movingwoo/wfeature/internal/jvm"
)

//go:embed testdata/connector.jar
var connectorJAR []byte

// connectorChecks is every bit ConnectorMIDlet.run sets when the whole
// connection surface refuses. Comparing against the exact value rather than a
// threshold means a newly broken check fails even if another starts passing.
const connectorChecks = int32(1)<<11 - 1

func TestConnectorRefusesEveryConnection(t *testing.T) {
	runtime := startConnectorFixture(t)
	flags, err := runtime.VM.InvokeStatic("ConnectorMIDlet", "run", "()I")
	if err != nil {
		t.Fatalf("run() error = %v", err)
	}
	value, err := flags.Int32()
	if err != nil {
		t.Fatal(err)
	}
	if value != connectorChecks {
		t.Fatalf("run() = %#x, want %#x (missing %#x): %s",
			value, connectorChecks, connectorChecks&^value,
			fixtureString(t, runtime, "ConnectorMIDlet", "failure"))
	}
}

// TestConnectionClassesResolve loads every class of the Generic Connection
// Framework. The refusal is only worth having if the names around it resolve:
// a game that declares a field of one of these types, or catches on one, must
// reach its own handler instead of the loader's "class not found", which no
// guest code can catch.
func TestConnectionClassesResolve(t *testing.T) {
	runtime := startConnectorFixture(t)
	classes := []string{
		midp.ConnectorClass,
		midp.ConnectionClass,
		midp.InputConnectionClass,
		midp.OutputConnectionClass,
		midp.StreamConnectionClass,
		midp.ContentConnectionClass,
		midp.StreamConnectionNotifierClass,
		midp.HTTPConnectionClass,
		midp.SocketConnectionClass,
		midp.ConnectionNotFoundExceptionClass,
	}
	for _, name := range classes {
		// Walking to Object is what makes the runtime resolve the class, so a
		// name the library stopped declaring fails here rather than inside a
		// game.
		loaded, err := runtime.VM.IsSubclassOf(name, "java/lang/Object")
		if err != nil {
			t.Errorf("IsSubclassOf(%q, Object) error = %v", name, err)
			continue
		}
		if !loaded {
			t.Errorf("%q does not reach java/lang/Object", name)
		}
	}

	// The exception's own parent is the whole reason a game catches it.
	extendsIOException, err := runtime.VM.IsSubclassOf(midp.ConnectionNotFoundExceptionClass, jvm.IOExceptionClass)
	if err != nil {
		t.Fatalf("IsSubclassOf(ConnectionNotFoundException, IOException) error = %v", err)
	}
	if !extendsIOException {
		t.Fatalf("ConnectionNotFoundException does not extend %s", jvm.IOExceptionClass)
	}
}

// TestConnectionRefusalIsAnIOException pins the one relationship a game
// depends on. Most titles never name ConnectionNotFoundException; they wrap
// the attempt in a single catch of IOException, so a refusal that did not
// reach that handler would leave the game stopped rather than offline.
func TestConnectionRefusalIsAnIOException(t *testing.T) {
	runtime := startConnectorFixture(t)
	refusal := newGuestException(midp.ConnectionNotFoundExceptionClass, "http://ranking.invalid/")
	if !runtime.VM.IsGuestException(refusal, jvm.IOExceptionClass) {
		t.Fatalf("a connection refusal is not catchable as %s", jvm.IOExceptionClass)
	}
}

func startConnectorFixture(t *testing.T) *Runtime {
	t.Helper()
	archive, err := Open(connectorJAR)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	runtime, err := Start(archive, testRuntimeOptions(t))
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	return runtime
}
