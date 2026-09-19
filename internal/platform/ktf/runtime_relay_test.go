package ktf

import (
	"bytes"
	"errors"
	"testing"
	"time"

	"github.com/movingwoo/wfeature/internal/jvm"
)

func enableTestSlotRelay(client *Client) {
	client.appProperties = map[string]string{"REQLIB": relayDependency + "|" + relayDependencyVersion + "`"}
	client.image.Data = append([]byte("com/vdigm/billcom/relay/a\x00"), relayHostConstant()...)
}

func TestLocalRelayDependencyRequiresProviderContract(t *testing.T) {
	client, runtime := newTestRuntime(t)
	lookup := func() int {
		t.Helper()
		value, err := runtimeKernelGetExecNames(runtime, client.vm, []jvm.Value{jvm.ReferenceValue(client.vm.NewString(relayDependency)), jvm.ReferenceValue(nil), jvm.ReferenceValue(nil)})
		if err != nil {
			t.Fatal(err)
		}
		array, _ := value.Reference()
		_, count, ok := jvm.ArrayComponent(array)
		if !ok {
			t.Fatal("not an array")
		}
		return count
	}
	client.appProperties = map[string]string{"REQLIB": relayDependency + "|" + relayDependencyVersion}
	if lookup() != 0 {
		t.Fatal("descriptor alone installed a service")
	}
	enableTestSlotRelay(client)
	if lookup() != 1 {
		t.Fatal("recognized local provider missing")
	}
	client.appProperties["REQLIB"] = relayDependency + "|99.00.00"
	if lookup() != 0 {
		t.Fatal("unsupported provider version installed")
	}
	enableTestSlotRelay(client)
	client.image.Data = []byte("com/vdigm/billcom/relay/a")
	if lookup() != 0 {
		t.Fatal("missing endpoint accepted")
	}
}

func TestLocalRelayRuntimeStreamRoundTripAndDisconnect(t *testing.T) {
	client, runtime := newTestRuntime(t)
	enableTestSlotRelay(client)
	vm := client.vm
	invoke := func(receiver *jvm.Object, name, descriptor string, args ...jvm.Value) jvm.Value {
		t.Helper()
		value, err := vm.InvokeVirtual(receiver, name, descriptor, args...)
		if err != nil {
			t.Fatal(err)
		}
		return value
	}
	socketURL := jvm.ReferenceValue(vm.NewString(relayAddress))
	if _, err := runtimeURLFind(runtime, vm, []jvm.Value{socketURL}); err == nil {
		t.Fatal("opened before connect")
	}
	value, err := runtimeNetworkConnect(runtime, vm, nil)
	if err != nil {
		t.Fatal(err)
	}
	connected, _ := value.Int32()
	if connected != 0 {
		t.Fatal("local connect failed")
	}
	if _, err := runtimeURLFind(runtime, vm, []jvm.Value{jvm.ReferenceValue(vm.NewString("socket://example.invalid:17096"))}); err == nil {
		t.Fatal("unknown endpoint accepted")
	}
	for _, class := range []string{runtimeRelaySocketClass, runtimeRelayInputClass, runtimeRelayOutputClass} {
		if _, err := runtime.ensureJavaClass(class); err != nil {
			t.Fatal(err)
		}
	}
	socketValue, err := runtimeURLFind(runtime, vm, []jvm.Value{socketURL})
	if err != nil {
		t.Fatal(err)
	}
	socket, _ := socketValue.Reference()
	input, _ := invoke(socket, "getInputStream", "()Ljava/io/InputStream;").Reference()
	output, _ := invoke(socket, "getOutputStream", "()Ljava/io/OutputStream;").Reference()
	wire, _ := encodeRelayFrame(relayFrame{payload: slotMessage(1, 1000, []byte{30})})
	// Three write shapes feed one frame. Reads also exercise all overloads.
	invoke(output, "write", "(I)V", jvm.IntValue(int32(wire[0])))
	invoke(output, "write", "([B)V", jvm.ReferenceValue(jvm.NewByteArray(wire[1:5])))
	padded := append([]byte{99}, wire[5:]...)
	invoke(output, "write", "([BII)V", jvm.ReferenceValue(jvm.NewByteArray(padded)), jvm.IntValue(1), jvm.IntValue(int32(len(padded)-1)))
	first, _ := invoke(input, "read", "()I").Int32()
	if first != 0 {
		t.Fatal("bad envelope marker")
	}
	prefix := jvm.NewByteArray(make([]byte, 6))
	n, _ := invoke(input, "read", "([B)I", jvm.ReferenceValue(prefix)).Int32()
	if n != 6 {
		t.Fatalf("prefix read=%d", n)
	}
	result := jvm.NewByteArray(make([]byte, 15))
	n, _ = invoke(input, "read", "([BII)I", jvm.ReferenceValue(result), jvm.IntValue(1), jvm.IntValue(13)).Int32()
	if n != 13 {
		t.Fatalf("body read=%d", n)
	}
	data, err := jvm.ByteArraySnapshot(result)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(data[1:14], slotMessage(1, 1000, []byte{0})) {
		t.Fatalf("body=%x", data)
	}
	if n, _ := invoke(input, "available", "()I").Int32(); n != 0 {
		t.Fatal("unexpected pending bytes")
	}
	if n, _ := invoke(input, "read", "([BII)I", jvm.ReferenceValue(result), jvm.IntValue(0), jvm.IntValue(0)).Int32(); n != 0 {
		t.Fatal("empty read blocked")
	}
	if _, err := vm.InvokeVirtual(input, "read", "([BII)I", jvm.ReferenceValue(result), jvm.IntValue(-1), jvm.IntValue(1)); err == nil {
		t.Fatal("negative offset accepted")
	}
	if _, err := vm.InvokeVirtual(output, "write", "([BII)V", jvm.ReferenceValue(result), jvm.IntValue(14), jvm.IntValue(2)); err == nil {
		t.Fatal("range overrun accepted")
	}
	if _, err := runtimeNetworkDisconnect(runtime, vm, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := vm.InvokeVirtual(input, "read", "()I"); err == nil {
		t.Fatal("closed read accepted")
	}
	if _, err := vm.InvokeVirtual(output, "flush", "()V"); err == nil {
		t.Fatal("closed flush accepted")
	}
	invoke(socket, "close", "()V")
	if runtime.relayOnline || runtime.relaySocket != nil {
		t.Fatal("disconnect retained connection")
	}
}

func TestRelayReaderParksAndDisconnectReleasesWorker(t *testing.T) {
	clock := NewManualClock(time.Unix(1700000000, 0))
	client, runtime := newPacedTestRuntime(t, clock, 1)
	socket := &relaySocket{}
	runtime.relaySocket = socket
	runtime.relayOnline = true
	receiver := &jvm.Object{ClassName: runtimeRelayInputClass, Native: socket}
	returned := false
	if err := client.vm.RegisterNative("test/RelayReader", "run", "()V", func(*jvm.VM, []jvm.Value) (jvm.Value, error) {
		value, err := runtimeRelayRead(runtime, client.vm, []jvm.Value{jvm.ReferenceValue(receiver)})
		returned = true
		return value, err
	}); err != nil {
		t.Fatal(err)
	}
	runtime.pendingThreads = []*jvm.Object{{ClassName: "test/RelayReader"}}
	t.Cleanup(client.StopThreads)
	if _, err := client.ServiceThreads(t.Context(), 1); err != nil {
		t.Fatal(err)
	}
	if returned || len(client.workers) != 1 {
		t.Fatal("empty transport did not park")
	}
	if _, err := runtimeNetworkDisconnect(runtime, client.vm, nil); err != nil {
		t.Fatal(err)
	}
	clock.Advance(time.Second)
	_, err := client.ServiceThreads(t.Context(), 1)
	var guest *jvm.GuestException
	if !errors.As(err, &guest) || guest.Object.ClassName != "java/io/IOException" || !returned || len(client.workers) != 0 {
		t.Fatalf("worker disconnect returned=%v workers=%d error=%v", returned, len(client.workers), err)
	}
}

func TestRelaySocketClosePreservesReturnedStreams(t *testing.T) {
	client, runtime := newTestRuntime(t)
	socket := &relaySocket{}
	owner := &jvm.Object{ClassName: runtimeRelaySocketClass, Native: socket}
	args := []jvm.Value{jvm.ReferenceValue(owner)}
	in, err := runtimeRelayInput(runtime, client.vm, args)
	if err != nil {
		t.Fatal(err)
	}
	out, err := runtimeRelayOutput(runtime, client.vm, args)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtimeRelayClose(runtime, client.vm, args); err != nil {
		t.Fatal(err)
	}
	if socket.closed {
		t.Fatal("Socket.close invalidated returned streams")
	}
	if _, err := runtimeRelayInput(runtime, client.vm, args); err == nil {
		t.Fatal("opened new stream after Socket.close")
	}
	wire, _ := encodeRelayFrame(relayFrame{payload: slotMessage(1, 1000, []byte{30})})
	if _, err := runtimeRelayWrite(runtime, client.vm, []jvm.Value{out, jvm.ReferenceValue(jvm.NewByteArray(wire))}); err != nil {
		t.Fatal(err)
	}
	if _, err := runtimeRelayRead(runtime, client.vm, []jvm.Value{in}); err != nil {
		t.Fatal(err)
	}
	if _, err := runtimeRelayClose(runtime, client.vm, []jvm.Value{in}); err != nil {
		t.Fatal(err)
	}
	if socket.closed {
		t.Fatal("input close invalidated the remaining output stream")
	}
	if _, err := runtimeRelayRead(runtime, client.vm, []jvm.Value{in}); err == nil {
		t.Fatal("read from closed input succeeded")
	}
	if _, err := runtimeRelayClose(runtime, client.vm, []jvm.Value{out}); err != nil {
		t.Fatal(err)
	}
	if !socket.closed || socket.pending != nil || socket.incoming != nil {
		t.Fatal("last close retained transport buffers")
	}
}

func TestKernelLoadOnlyStartsRecognizedLocalProvider(t *testing.T) {
	client, runtime := newTestRuntime(t)
	args := []jvm.Value{jvm.ReferenceValue(client.vm.NewString(relayDependency)), jvm.ReferenceValue(nil)}
	load := func() int32 {
		t.Helper()
		value, err := runtimeKernelLoad(runtime, client.vm, args)
		if err != nil {
			t.Fatal(err)
		}
		id, err := value.Int32()
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	if load() >= 0 {
		t.Fatal("missing provider loaded")
	}
	enableTestSlotRelay(client)
	id := load()
	if id <= 0 || load() != id {
		t.Fatal("local provider has no stable program ID")
	}
	args[0] = jvm.ReferenceValue(client.vm.NewString("unknown-library"))
	if load() >= 0 {
		t.Fatal("unknown library loaded")
	}
	args[0] = jvm.ReferenceValue(client.vm.NewString(relayDependency))
	array, err := client.vm.NewArray(jvm.Type{Kind: jvm.TypeReference, ClassName: "java/lang/String"}, 1)
	if err != nil {
		t.Fatal(err)
	}
	args[1] = jvm.ReferenceValue(array)
	if load() >= 0 {
		t.Fatal("unsupported load arguments ignored")
	}
}
