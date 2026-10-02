package skt

import (
	_ "embed"
	"testing"
	"time"

	"github.com/movingwoo/wfeature/internal/api/midp"
	"github.com/movingwoo/wfeature/internal/jvm"
)

//go:embed testdata/native-checkpoint.jar
var nativeCheckpointJAR []byte

func TestPackagedMIDletHeapRestoresWithoutStartup(t *testing.T) {
	archive, err := Open(nativeCheckpointJAR)
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := Start(archive, Options{Framebuffer: newTestFramebuffer(t, 32, 24)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtime.Destroy(true) })
	const calendarMillis = int64(3000000000000)
	if _, err := runtime.VM.InvokeStatic("HeapCheckpointProbe", "prepareCalendar", "(J)V", jvm.LongValue(calendarMillis)); err != nil {
		t.Fatal(err)
	}
	saved, err := runtime.VM.CaptureHeapState([]*jvm.Object{runtime.MIDlet}, jvm.HeapCodec{})
	if err != nil {
		t.Fatal(err)
	}
	want, err := runtime.VM.InvokeStatic("HeapCheckpointProbe", "tick", "()I")
	if err != nil {
		t.Fatal(err)
	}
	fresh := jvm.New(archive, jvm.Options{})
	if err := midp.Define(fresh); err != nil {
		t.Fatal(err)
	}
	roots, err := fresh.RestoreHeapState(saved, jvm.HeapCodec{})
	if err != nil {
		t.Fatal(err)
	}
	if len(roots) != 1 || roots[0] == runtime.MIDlet || roots[0].ClassName != "NativeCheckpointMIDlet" {
		t.Fatal("MIDlet root was not restored independently")
	}
	starts, err := fresh.StaticField("NativeCheckpointMIDlet", "starts", "I")
	if got, _ := starts.Int32(); err != nil || got != 1 {
		t.Fatalf("restored starts=%d, %v", got, err)
	}
	got, err := fresh.InvokeStatic("HeapCheckpointProbe", "tick", "()I")
	if err != nil || got != want {
		t.Fatalf("restored JAR tick=%v, %v, want %v", got, err, want)
	}
	for _, millis := range []int64{-100000000000, calendarMillis} {
		want, err := runtime.VM.InvokeStatic("HeapCheckpointProbe", "calendarMinute", "(J)I", jvm.LongValue(millis))
		if err != nil {
			t.Fatal(err)
		}
		got, err := fresh.InvokeStatic("HeapCheckpointProbe", "calendarMinute", "(J)I", jvm.LongValue(millis))
		if err != nil || got != want {
			t.Fatalf("restored JAR Calendar at %s=%v, %v; want %v", time.UnixMilli(millis).UTC(), got, err, want)
		}
	}
}

func TestPackagedMIDletSurvivesRefusedNativeExecutionCapture(t *testing.T) {
	archive, err := Open(nativeCheckpointJAR)
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := Start(archive, Options{Framebuffer: newTestFramebuffer(t, 32, 24)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtime.Destroy(true) })
	refused := false
	if err := runtime.VM.RegisterContextNative("NativeCheckpointProbe", "checkpoint", "()I", func(call *jvm.Invocation, _ []jvm.Value) (jvm.Value, error) {
		_, err := call.CaptureNativeState(1)
		refused = err != nil
		return jvm.IntValue(42), nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := runtime.Pause(); err != nil {
		t.Fatal(err)
	}
	if err := runtime.Resume(); err != nil {
		t.Fatal(err)
	}
	if !refused || runtime.State() != StateActive {
		t.Fatalf("refused=%v lifecycle=%s", refused, runtime.State())
	}
	for _, field := range []struct {
		class, name string
		want        int32
	}{
		{"NativeCheckpointMIDlet", "result", 43},
		{"NativeCheckpointProbe", "before", 1},
		{"NativeCheckpointProbe", "after", 1},
	} {
		value, err := runtime.VM.StaticField(field.class, field.name, "I")
		if got, _ := value.Int32(); err != nil || got != field.want {
			t.Fatalf("%s.%s = %d, %v", field.class, field.name, got, err)
		}
	}
}
