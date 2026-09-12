package skt

import (
	_ "embed"
	"testing"
)

//go:embed testdata/streams.jar
var streamInterfacesJAR []byte

// A class-file fixture proves the VM operation itself; this packages the same
// contract as a MIDlet and reaches it through the SKT archive and lifecycle.
func TestPackagedMIDletUsesCLDCStreamInterfaces(t *testing.T) {
	archive, err := Open(streamInterfacesJAR)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	runtime, err := Start(archive, Options{Framebuffer: newTestFramebuffer(t, 32, 24)})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	t.Cleanup(func() { _ = runtime.Destroy(true) })
	result, err := runtime.VM.InvokeStatic("StreamMIDlet", "result", "()I")
	if err != nil {
		t.Fatalf("result() error = %v", err)
	}
	value, err := result.Int32()
	if err != nil {
		t.Fatal(err)
	}
	if value != 5669 {
		t.Fatalf("result() = %d, want 5669", value)
	}
}
