package ktf

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/movingwoo/wfeature/internal/jvm"
)

func TestRelayHeapContinuesPartialFramesAndSharedStreams(t *testing.T) {
	_, runtime := newTestRuntime(t)
	socket := &relaySocket{}
	receiver := &jvm.Object{ClassName: runtimeRelaySocketClass, Native: socket}
	input, err := runtimeRelayInput(runtime, runtime.client.vm, []jvm.Value{jvm.ReferenceValue(receiver)})
	if err != nil {
		t.Fatal(err)
	}
	output, err := runtimeRelayOutput(runtime, runtime.client.vm, []jvm.Value{jvm.ReferenceValue(receiver)})
	if err != nil {
		t.Fatal(err)
	}
	inputObject, _ := input.Reference()
	outputObject, _ := output.Reference()
	runtime.relaySocket, runtime.relayOnline = socket, true
	handshake, _ := encodeRelayFrame(relayFrame{payload: slotMessage(1, 1000, []byte{30})})
	if err := socket.write(handshake); err != nil {
		t.Fatal(err)
	}
	if _, err := runtimeRelayRead(runtime, runtime.client.vm, []jvm.Value{input}); err != nil {
		t.Fatal(err)
	}
	identity, _ := encodeRelayFrame(relayFrame{payload: slotMessage(5, 1400, []byte{3, 11, 22, 33})})
	if err := socket.write(identity[:9]); err != nil {
		t.Fatal(err)
	}
	saved, err := runtime.captureHeapState([]*jvm.Object{receiver, inputObject, outputObject})
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(saved)
	if err != nil {
		t.Fatal(err)
	}
	var decoded runtimeHeapState
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	_, fresh := newTestRuntime(t)
	roots, err := fresh.restoreHeapState(decoded)
	if err != nil {
		t.Fatal(err)
	}
	restored := fresh.relaySocket
	if len(roots) != 3 || !fresh.relayOnline || restored == nil || restored == socket || roots[0].Native != restored || roots[1].Native != restored || roots[2].Native != restored || restored.input != roots[1] || restored.output != roots[2] {
		t.Fatal("relay socket or stream sharing was lost")
	}
	if restored.service.phase != 1 || !bytes.Equal(restored.pending, identity[:9]) || !bytes.Equal(restored.incoming, socket.incoming) {
		t.Fatal("relay request or reply cursor changed")
	}
	if err := restored.write(identity[9:]); err != nil {
		t.Fatal(err)
	}
	if restored.service.phase != 2 || !bytes.Equal(restored.service.identity, []byte{11, 22, 33}) || len(restored.pending) != 0 || socket.service.phase != 1 || !bytes.Equal(socket.pending, identity[:9]) {
		t.Fatal("restored request did not continue independently")
	}
	wantReply, _ := encodeRelayFrame(relayFrame{payload: slotMessage(5, 1400, nil)})
	want := append(bytes.Clone(socket.incoming), wantReply...)
	for _, b := range want {
		value, err := runtimeRelayRead(fresh, fresh.client.vm, []jvm.Value{jvm.ReferenceValue(roots[1])})
		got, _ := value.Int32()
		if err != nil || got != int32(b) {
			t.Fatalf("continued relay read = %d, %v; want %d", got, err, b)
		}
	}
	if _, err := runtimeRelayClose(fresh, fresh.client.vm, []jvm.Value{jvm.ReferenceValue(roots[0])}); err != nil {
		t.Fatal(err)
	}
	if restored.closed || !restored.socketClosed || restored.inputClosed || restored.outputClosed {
		t.Fatal("closing the socket closed retained streams")
	}
	again, err := fresh.captureHeapState(roots)
	if err != nil {
		t.Fatal(err)
	}
	_, next := newTestRuntime(t)
	if _, err := next.restoreHeapState(again); err != nil {
		t.Fatal(err)
	}
	if !next.relaySocket.socketClosed || next.relaySocket.closed || next.relaySocket.inputClosed || next.relaySocket.outputClosed {
		t.Fatal("restoration changed the lifetime of retained streams")
	}
	keepalive, _ := encodeRelayFrame(relayFrame{payload: slotMessage(0, 10, nil)})
	if err := next.relaySocket.write(keepalive); err != nil {
		t.Fatal(err)
	}
}

func TestRelayHeapRetainsRuntimeOnlySocket(t *testing.T) {
	_, runtime := newTestRuntime(t)
	runtime.relaySocket = &relaySocket{}
	runtime.relayOnline = true
	saved, err := runtime.captureHeapState(nil)
	if err != nil {
		t.Fatal(err)
	}
	_, fresh := newTestRuntime(t)
	roots, err := fresh.restoreHeapState(saved)
	if err != nil || len(roots) != 0 || fresh.relaySocket == nil || !fresh.relayOnline || fresh.relaySocket.input != nil || fresh.relaySocket.output != nil {
		t.Fatalf("runtime-only relay restore failed: %v", err)
	}
	if fresh.relaySocket == runtime.relaySocket {
		t.Fatal("runtime-only socket was not copied")
	}
	after, err := fresh.captureHeapState(nil)
	if err != nil || after.JVM.NextObject != saved.JVM.NextObject {
		t.Fatalf("capturing a native owner assigned a guest identity: %v", err)
	}
}

func TestRelayHeapRejectsMalformedOwnershipBeforeAdoption(t *testing.T) {
	_, runtime := newTestRuntime(t)
	socket := &relaySocket{}
	socket.input = &jvm.Object{ClassName: runtimeRelayInputClass, Native: socket}
	runtime.relaySocket = socket
	saved, err := runtime.captureHeapState(nil)
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(saved)
	if err != nil {
		t.Fatal(err)
	}
	sentinelAddress, err := runtime.allocate(4)
	if err != nil {
		t.Fatal(err)
	}
	sentinel := &jvm.Object{ClassName: jvm.ObjectClass}
	if err := runtime.client.vm.BindAOTObject(sentinelAddress, sentinel); err != nil {
		t.Fatal(err)
	}
	for _, damage := range []func(*runtimeHeapState, int){
		func(s *runtimeHeapState, i int) { s.JVM.Payloads[i].Data = s.JVM.Payloads[i].Data[:3] },
		func(s *runtimeHeapState, i int) { s.JVM.Payloads[i].Data[0] = 99 },
		func(s *runtimeHeapState, i int) { s.JVM.Payloads[i].Data[4] = 255 },
		func(s *runtimeHeapState, i int) { s.JVM.Payloads[i].References = nil },
		func(s *runtimeHeapState, i int) { s.JVM.Objects[s.JVM.Payloads[i].References[0]-1].Native = 0 },
		func(s *runtimeHeapState, i int) {
			s.JVM.Objects[s.JVM.Payloads[i].References[0]-1].Class = jvm.ObjectClass
		},
		func(s *runtimeHeapState, _ int) { s.JVM.Objects[s.JVM.Roots[s.Roots.RelaySocket-1]-1].Native = 0 },
	} {
		var bad runtimeHeapState
		if err := json.Unmarshal(data, &bad); err != nil {
			t.Fatal(err)
		}
		for i, payload := range bad.JVM.Payloads {
			if payload.ExternalKind == "ktf-relay-v1" {
				damage(&bad, i)
				break
			}
		}
		vm := runtime.client.vm
		if _, err := runtime.restoreHeapState(bad); err == nil {
			t.Fatal("malformed relay was accepted")
		}
		binding, _ := vm.AOTObjectAt(sentinelAddress)
		if runtime.client.vm != vm || binding != sentinel || runtime.relaySocket != socket || socket.input.Native != socket {
			t.Fatal("refused relay changed its target")
		}
	}
}
