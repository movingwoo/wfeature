package ktf

import (
	"testing"

	"github.com/movingwoo/wfeature/internal/jvm"
)

func newByteArray(t *testing.T, client *Client, data []byte) *jvm.Object {
	t.Helper()
	array, err := client.JVM().NewArray(jvm.Type{Kind: jvm.TypeByte}, int32(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	values := make([]jvm.Value, len(data))
	for index, value := range data {
		values[index] = jvm.IntValue(int32(int8(value)))
	}
	if err := jvm.SetArrayRange(array, 0, values); err != nil {
		t.Fatal(err)
	}
	return array
}

// TestSetBufferReplacesTheClipRatherThanAppending is the difference between
// setBuffer and putData, and getting it wrong is silent: a game that refills
// one clip per sound effect would play every effect it had ever loaded, joined
// end to end.
func TestSetBufferReplacesTheClipRatherThanAppending(t *testing.T) {
	client, runtime := newTestRuntime(t)
	receiver := &jvm.Object{ClassName: "org/kwis/msp/media/Clip"}

	first := newByteArray(t, client, []byte{1, 2, 3, 4})
	second := newByteArray(t, client, []byte{9, 9})
	arguments := []jvm.Value{jvm.ReferenceValue(receiver), jvm.ReferenceValue(first), jvm.IntValue(4)}
	if _, err := runtimeClipSetBuffer(runtime, client.JVM(), arguments); err != nil {
		t.Fatalf("setBuffer error = %v", err)
	}
	arguments = []jvm.Value{jvm.ReferenceValue(receiver), jvm.ReferenceValue(second), jvm.IntValue(2)}
	if _, err := runtimeClipSetBuffer(runtime, client.JVM(), arguments); err != nil {
		t.Fatalf("setBuffer error = %v", err)
	}
	if got := string(runtime.clip(receiver).data); got != "\x09\x09" {
		t.Fatalf("clip holds %q after the second setBuffer, want only the second buffer", got)
	}
}

// TestSetBufferReportsWhetherTheDataWasTaken pins the boolean form that one
// title's base class carries. The caller branches on it, so answering true for
// a buffer that was refused would have the game play silence and never notice.
func TestSetBufferReportsWhetherTheDataWasTaken(t *testing.T) {
	client, runtime := newTestRuntime(t)
	receiver := &jvm.Object{ClassName: "org/kwis/msp/media/BaseClip"}
	data := newByteArray(t, client, []byte{1, 2, 3, 4})

	result, err := runtimeClipSetBufferChecked(runtime, client.JVM(), []jvm.Value{
		jvm.ReferenceValue(receiver), jvm.ReferenceValue(data), jvm.IntValue(4),
	})
	if err != nil {
		t.Fatalf("setBuffer error = %v", err)
	}
	if taken, _ := result.Int32(); taken != 1 {
		t.Fatalf("setBuffer of four bytes = %d, want true", taken)
	}
	if got := len(runtime.clip(receiver).data); got != 4 {
		t.Fatalf("clip holds %d bytes, want 4", got)
	}

	result, err = runtimeClipSetBufferChecked(runtime, client.JVM(), []jvm.Value{
		jvm.ReferenceValue(receiver), jvm.ReferenceValue(data), jvm.IntValue(0),
	})
	if err != nil {
		t.Fatalf("setBuffer error = %v", err)
	}
	if taken, _ := result.Int32(); taken != 0 {
		t.Fatalf("setBuffer of nothing = %d, want false", taken)
	}
	if got := len(runtime.clip(receiver).data); got != 0 {
		t.Fatalf("clip holds %d bytes after an empty setBuffer, want 0", got)
	}
}

// A title builds its whole sound set in startApp from a numbering its own
// archive is sparse in, so a missing name reached the constructor thirteen
// times in one start. The specification declares this constructor no
// exception, which leaves an empty clip as the only answer there is — and the
// alternative, which is what this used to do, was to end the session before
// the title's first frame.
func TestAClipNamingAResourceTheArchiveLacksIsEmptyRatherThanFatal(t *testing.T) {
	client, runtime := newTestRuntime(t)
	client.AttachResources(map[string][]byte{"1.mmf": []byte("MMMDsound")})

	packaged := &jvm.Object{ClassName: "org/kwis/msp/media/Clip"}
	arguments := []jvm.Value{
		jvm.ReferenceValue(packaged),
		jvm.ReferenceValue(client.JVM().NewString("audio/x-mmf")),
		jvm.ReferenceValue(client.JVM().NewString("/1.mmf")),
	}
	if _, err := runtimeClipConstructor(runtime, client.JVM(), arguments); err != nil {
		t.Fatalf("a packaged clip failed: %v", err)
	}
	if got := string(runtime.clip(packaged).data); got != "MMMDsound" {
		t.Fatalf("the packaged clip holds %q, want the resource's bytes", got)
	}

	missing := &jvm.Object{ClassName: "org/kwis/msp/media/Clip"}
	arguments = []jvm.Value{
		jvm.ReferenceValue(missing),
		jvm.ReferenceValue(client.JVM().NewString("audio/x-mmf")),
		jvm.ReferenceValue(client.JVM().NewString("/13.mmf")),
	}
	if _, err := runtimeClipConstructor(runtime, client.JVM(), arguments); err != nil {
		t.Fatalf("a clip naming an absent resource failed: %v", err)
	}
	if data := runtime.clip(missing).data; len(data) != 0 {
		t.Fatalf("the clip holds %d bytes, want none", len(data))
	}

	// A name that climbs out of the archive is still refused: that is a
	// malformed path rather than a gap in the archive, and nothing in the
	// specification asks this platform to follow it. A relative name cannot
	// reach one — it is resolved against the class's own package first, which
	// absorbs the climb — so this is the absolute form.
	escaping := &jvm.Object{ClassName: "org/kwis/msp/media/Clip"}
	arguments = []jvm.Value{
		jvm.ReferenceValue(escaping),
		jvm.ReferenceValue(client.JVM().NewString("audio/x-mmf")),
		jvm.ReferenceValue(client.JVM().NewString("/../outside.mmf")),
	}
	if _, err := runtimeClipConstructor(runtime, client.JVM(), arguments); err == nil {
		t.Fatal("a clip naming a resource outside the package was accepted")
	}
}
