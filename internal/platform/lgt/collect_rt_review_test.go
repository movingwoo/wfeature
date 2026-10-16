package lgt

import "testing"

// A wrapper over a title's own stream reads through that stream's object and
// hands its `read([BII)` a buffer this platform allocated once. Neither is
// named by any guest word once the wrapper is built — the wrapper's constructor
// is native — so the wrapper is the only thing that keeps them, and freeing
// them under it hands the title's `read` a zeroed receiver and a buffer some
// other object now owns.
func TestJavaCollectorKeepsAWrappedGuestStreamsSourceAndBuffer(t *testing.T) {
	client := fixtureClient(t)
	runtime := client.javaRuntimeState()
	wrapper := newCollectableObject(t, client, "java/lang/Object")
	source := newCollectableObject(t, client, "java/lang/Object")
	buffer, err := client.newJavaByteArray(make([]byte, javaStreamPull))
	if err != nil {
		t.Fatal(err)
	}
	client.releaseJavaPins(0)
	stream := &javaStream{Name: "s", Source: &javaStreamSource{Object: source, Buffer: buffer}}
	runtime.streams[wrapper] = stream
	runtime.streams[source] = stream
	runtime.jlet = wrapper

	collect(t, client)
	collect(t, client)
	if !tracked(client, source) {
		t.Fatal("the title's own stream behind a live wrapper was freed")
	}
	if !tracked(client, buffer) {
		t.Fatal("the buffer a live wrapper reads its source through was freed")
	}

	// Dropping the wrapper lets all three go: the edge is the wrapper's, not a
	// root of its own.
	runtime.jlet = 0
	collect(t, client)
	collect(t, client)
	for name, object := range map[string]uint32{"wrapper": wrapper, "source": source, "buffer": buffer} {
		if tracked(client, object) {
			t.Errorf("the %s outlived the wrapper that held it", name)
		}
	}
}
