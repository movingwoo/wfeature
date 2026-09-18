package ktf

import (
	"github.com/movingwoo/wfeature/internal/jvm"
	"testing"
)

func TestInputStreamSharesFileCursorAndWrites(t *testing.T) {
	client, runtime := newTestRuntime(t)
	vm := client.JVM()
	state := &runtimeGuestFile{name: "cursor", data: []byte("abcd"), position: 1}
	file := &jvm.Object{ClassName: "org/kwis/msp/io/File", Native: state}
	streamValue, err := runtimeFileOpenDataStream(runtime, vm, []jvm.Value{jvm.ReferenceValue(file)})
	if err != nil {
		t.Fatal(err)
	}
	stream, _ := streamValue.Reference()
	value, err := vm.InvokeVirtual(stream, "read", "()I")
	got, _ := value.Int32()
	if err != nil || got != 'b' || state.position != 2 {
		t.Fatalf("stream read = %d, cursor %d, %v", got, state.position, err)
	}
	value, err = runtimeFileReadByte(runtime, vm, []jvm.Value{jvm.ReferenceValue(file)})
	got, _ = value.Int32()
	if err != nil || got != 'c' || state.position != 3 {
		t.Fatalf("direct read = %d, %v", got, err)
	}
	if _, err := runtimeFileSeek(runtime, vm, []jvm.Value{jvm.ReferenceValue(file), jvm.IntValue(1)}); err != nil {
		t.Fatal(err)
	}
	if _, err := runtimeFileWriteByte(runtime, vm, []jvm.Value{jvm.ReferenceValue(file), jvm.IntValue('X')}); err != nil {
		t.Fatal(err)
	}
	state.position = 1
	value, err = vm.InvokeVirtual(stream, "read", "()I")
	got, _ = value.Int32()
	if err != nil || got != 'X' {
		t.Fatalf("stream saw stale bytes: %d, %v", got, err)
	}
}

func TestUserEventPreservesTypeAndBothArguments(t *testing.T) {
	client, runtime := newTestRuntime(t)
	vm := client.JVM()
	var seen [3]int32
	if err := vm.RegisterNative("test/UserListener", "notifyEvent", "(III)V", func(_ *jvm.VM, args []jvm.Value) (jvm.Value, error) {
		for i := range seen {
			seen[i], _ = args[i+1].Int32()
		}
		return jvm.VoidValue(), nil
	}); err != nil {
		t.Fatal(err)
	}
	runtime.jletListeners = []*jvm.Object{{ClassName: "test/UserListener"}}
	for _, kind := range []int32{0x5000, 0xa600, 0x7fffffff} {
		runtime.postGuestEvent(guestEvent{kind: kind, param1: -7, param2: 42})
		event, _ := runtime.nextGuestEvent()
		if err := runtime.dispatchGuestEvent(vm, event); err != nil {
			t.Fatal(err)
		}
		if seen != [3]int32{kind, -7, 42} {
			t.Fatalf("payload = %v", seen)
		}
	}
	if err := runtime.dispatchGuestEvent(vm, guestEvent{kind: 0x4fff}); err == nil {
		t.Fatal("reserved event accepted")
	}
}
