package skt

import (
	_ "embed"
	"testing"
	"time"

	"github.com/movingwoo/wfeature/internal/backend"
)

//go:embed testdata/jvm-contracts.jar
var jvmContractsJAR []byte

// jvmContractsChecks is every bit JVMContractsMIDlet.run sets when each
// contract holds.
const jvmContractsChecks = int32(1)<<9 - 1

// The interpreter and core-library contracts the source review corrected,
// reached through a MIDlet JAR the way a title reaches them: an interface's
// array read through its implementing class, a synchronized toString inside a
// synchronized concatenation, EOFException at the end of a data stream, strict
// number parsing, an empty exception message, a catchable null String
// argument, one Class object per class, wait(0, n) as a timed wait, and one
// lock for a static synchronized method and its class literal. Two of them
// hung for ever before, so the run has a deadline.
func TestJVMContractsHoldInAMIDletJAR(t *testing.T) {
	archive, err := Open(jvmContractsJAR)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	options := testRuntimeOptions(t)
	options.SaveStore = backend.NewDirectorySaveStore(t.TempDir())
	runtime, err := Start(archive, options)
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	type answer struct {
		value int32
		err   error
	}
	done := make(chan answer, 1)
	go func() {
		flags, err := runtime.VM.InvokeStatic("JVMContractsMIDlet", "run", "()I")
		if err != nil {
			done <- answer{err: err}
			return
		}
		value, err := flags.Int32()
		done <- answer{value, err}
	}()
	select {
	case got := <-done:
		if got.err != nil {
			t.Fatalf("run() error = %v", got.err)
		}
		if got.value != jvmContractsChecks {
			t.Fatalf("run() = %#x, want %#x (missing %#x): %s", got.value, jvmContractsChecks,
				jvmContractsChecks&^got.value, fixtureString(t, runtime, "JVMContractsMIDlet", "failure"))
		}
	case <-time.After(10 * time.Second):
		t.Fatal("run() still running after 10s")
	}
}
