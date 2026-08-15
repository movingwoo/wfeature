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
