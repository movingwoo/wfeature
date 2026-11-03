package ktf

import (
	"testing"

	"github.com/movingwoo/wfeature/internal/api/wipi"
	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/jvm"
	"github.com/movingwoo/wfeature/internal/platform/wipicrt"
	"github.com/movingwoo/wfeature/internal/platform/wipicrt/wipicrttest"
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
	if got := string(runtime.clip(receiver).Data); got != "\x09\x09" {
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
	if got := len(runtime.clip(receiver).Data); got != 4 {
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
	if got := len(runtime.clip(receiver).Data); got != 0 {
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
	if got := string(runtime.clip(packaged).Data); got != "MMMDsound" {
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
	if data := runtime.clip(missing).Data; len(data) != 0 {
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

// TestClearDataAnsweringAnOutlivedStopKeepsTheNewSound pins what clearing a
// clip does from a listener answering a STOP the clip has outlived. A title
// keeps one Clip for every sound and changes music with `Player.stop`,
// `putData` and `Player.play`, and its PlayListener answers STOP by calling
// `clearData`. Listener events wait for the next round, so the STOP of the
// old sound arrives after the new one has started, and clearing the clip
// closed the new title music in the same millisecond it began: the title
// screen stayed silent. The STOP reports a sound that is already gone, so the
// call clears nothing; anywhere else clearData still stops the clip's sound.
func TestClearDataAnsweringAnOutlivedStopKeepsTheNewSound(t *testing.T) {
	client, runtime := newTestRuntime(t)
	clock := wipicrt.NewManualClock(runtime.ClockBase)
	client.Clock = clock
	client.Audio = backend.NewAudioWithClock(&audioPauseProbe{}, clock.Now)
	clip := newJavaAudioPauseClip(t, client, runtime)
	vm := client.JVM()
	clearData := func() {
		t.Helper()
		if _, err := runtimeClipClearData(runtime, vm, []jvm.Value{jvm.ReferenceValue(clip)}); err != nil {
			t.Fatalf("clearData: %v", err)
		}
	}
	stop := clipEvent{clip: clip, code: wipi.PlayEventStop}

	// The clip is playing when its STOP is delivered: the STOP is outlived.
	javaAudioPauseCall(t, client, clip, "Clip", "play", 1, 1)
	playing := runtime.clip(clip).Handle
	if !runtime.outlivedStop(stop) {
		t.Fatal("a STOP delivered while its clip plays again was not recognized")
	}
	for _, other := range []int32{wipi.PlayEventStart, wipi.PlayEventEndOfData} {
		if runtime.outlivedStop(clipEvent{clip: clip, code: other}) {
			t.Fatalf("event %d was taken for an outlived STOP", other)
		}
	}
	held := len(runtime.clip(clip).Data)
	runtime.replacedStop = clip
	clearData()
	runtime.replacedStop = nil
	if !client.Audio.Playing(playing) || len(runtime.clip(clip).Data) != held {
		t.Fatal("clearData answering an outlived STOP touched the new sound or its data")
	}

	// Outside that answer clearData empties the clip and stops its sound.
	clearData()
	if client.Audio.Playing(playing) || len(runtime.clip(clip).Data) != 0 || runtime.clip(clip).Loaded {
		t.Fatal("clearData did not stop the playing sound and empty the clip")
	}
	// A STOP for a clip that stays stopped is not outlived.
	sound := wipicrttest.OneNoteSMAF()
	fill := []jvm.Value{jvm.ReferenceValue(clip), jvm.ReferenceValue(newByteArray(t, client, sound)), jvm.IntValue(0), jvm.IntValue(int32(len(sound)))}
	if _, err := runtimeClipPutData(runtime, vm, fill); err != nil {
		t.Fatalf("putData: %v", err)
	}
	javaAudioPauseCall(t, client, clip, "Clip", "play", 1, 0)
	javaAudioPauseCall(t, client, clip, "Clip", "stop", 1)
	if runtime.outlivedStop(stop) {
		t.Fatal("a STOP for a stopped clip was taken for an outlived one")
	}
}
