package skt

import (
	_ "embed"
	"testing"

	"github.com/movingwoo/wfeature/internal/api/midp"
	"github.com/movingwoo/wfeature/internal/jvm"
)

//go:embed testdata/bytecode-checkpoint.jar
var bytecodeCheckpointJAR []byte

func TestPackagedMIDletResumesBytecodeWithoutRepeatingStartup(t *testing.T) {
	archive, err := Open(bytecodeCheckpointJAR)
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := Start(archive, Options{Framebuffer: newTestFramebuffer(t, 32, 24)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtime.Destroy(true) })
	var execution jvm.BytecodeExecutionState
	var heap jvm.HeapState
	if err := runtime.VM.RegisterContextNative("BytecodeCheckpointProbe", "checkpoint", "(ILjava/lang/Object;)J", func(call *jvm.Invocation, args []jvm.Value) (jvm.Value, error) {
		stage, _ := args[0].Int32()
		if stage == 1 {
			var roots []*jvm.Object
			var err error
			execution, roots, err = call.CaptureBytecodeState(1)
			if err != nil {
				return jvm.VoidValue(), err
			}
			heap, err = runtime.VM.CaptureHeapState(append(roots, runtime.MIDlet), jvm.HeapCodec{})
			if err != nil {
				return jvm.VoidValue(), err
			}
		}
		return jvm.LongValue(int64(stage) * 10), nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := runtime.Pause(); err != nil {
		t.Fatal(err)
	}
	if err := runtime.Resume(); err != nil {
		t.Fatal(err)
	}
	fresh := jvm.New(archive, jvm.Options{})
	if err := midp.Define(fresh); err != nil {
		t.Fatal(err)
	}
	if err := fresh.RegisterContextNative("BytecodeCheckpointProbe", "checkpoint", "(ILjava/lang/Object;)J", func(_ *jvm.Invocation, args []jvm.Value) (jvm.Value, error) {
		stage, _ := args[0].Int32()
		if stage != 2 {
			t.Fatalf("restoration repeated checkpoint prefix at stage %d", stage)
		}
		return jvm.LongValue(20), nil
	}); err != nil {
		t.Fatal(err)
	}
	roots, err := fresh.RestoreHeapState(heap, jvm.HeapCodec{})
	if err != nil {
		t.Fatal(err)
	}
	if roots[len(roots)-1] == runtime.MIDlet || roots[len(roots)-1].ClassName != "BytecodeCheckpointMIDlet" {
		t.Fatal("MIDlet was not restored independently")
	}
	if _, err := fresh.ResumeBytecodeExecution(execution, roots[:execution.RootCount], func(*jvm.Invocation) (jvm.Value, error) {
		return jvm.LongValue(10), nil
	}); err != nil {
		t.Fatal(err)
	}
	for _, field := range []struct {
		class, name string
		want        int32
	}{
		{"BytecodeCheckpointMIDlet", "starts", 2},
		{"BytecodeCheckpointMIDlet", "result", 84},
		{"BytecodeCheckpointProbe", "before", 1},
		{"BytecodeCheckpointProbe", "after", 1},
	} {
		value, err := fresh.StaticField(field.class, field.name, "I")
		if got, _ := value.Int32(); err != nil || got != field.want {
			t.Fatalf("restored %s.%s = %d, %v; want %d", field.class, field.name, got, err, field.want)
		}
	}
}
