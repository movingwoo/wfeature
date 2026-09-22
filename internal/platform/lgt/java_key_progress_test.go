package lgt

import (
	"context"
	"fmt"
	"testing"
)

func TestJavaKeyCallbackAllowsWorkerToClearBusyFlag(t *testing.T) {
	client := fixtureClient(t)
	writeJavaClassFixture(t, client)
	class, err := client.prepareJavaClass(t.Context(), client.thread, fixtureClassHandle)
	if err != nil {
		t.Fatal(err)
	}
	// A key callback spins at a compiler boundary until a worker clears a field.
	body := installThumb(t, client, 0xb510, 0x6884, 0x6820, 0x2800, 0xd003, 0x2355, 0x469c, 0xdf05, 0xe7f8, 0x2001, 0xbd10)
	class.Record.Methods = append(class.Record.Methods, javaMember{Name: "keyNotify", Descriptor: "(II)Z", Body: body})
	object, err := client.allocateJavaInstance(class.Object)
	if err != nil {
		t.Fatal(err)
	}
	data, err := client.readWord(object + 8)
	if err != nil {
		t.Fatal(err)
	}
	if err := client.writeWord(data, 1); err != nil {
		t.Fatal(err)
	}
	runtime := client.javaRuntimeState()
	runtime.card = object
	worker := &javaWorker{grant: make(chan context.Context), events: make(chan javaWorkerEvent, 1)}
	runtime.workers = []*javaWorker{worker}
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-worker.grant:
			err := client.writeWord(data, 0)
			worker.events <- javaWorkerEvent{done: true, err: err}
		case <-done:
		}
	}()
	if err := client.deliverJavaKey(t.Context(), false, uint32(5)); err != nil {
		t.Fatal(err)
	}
	if !worker.done {
		t.Fatal("key callback returned without worker progress")
	}
}

func TestJavaWorkerTryRegionsSurviveInterleavedCallback(t *testing.T) {
	client := fixtureClient(t)
	client.javaRuntimeState()
	client.javaCallDepth = 2
	mainBuffer, err := client.enterJavaTry()
	if err != nil {
		t.Fatal(err)
	}
	worker := &javaWorker{grant: make(chan context.Context), events: make(chan javaWorkerEvent, 1)}
	var workerBuffer uint32
	go func() {
		<-worker.grant
		client.javaCallDepth = 1
		var err error
		workerBuffer, err = client.enterJavaTry()
		worker.events <- javaWorkerEvent{err: err}
		<-worker.grant
		if len(client.javaTry) != 1 || client.javaTry[0].Buffer != workerBuffer || client.javaCallDepth != 1 {
			worker.events <- javaWorkerEvent{done: true, err: fmt.Errorf("worker exception state was replaced")}
			return
		}
		err = client.leaveJavaTry()
		client.javaCallDepth = 0
		worker.events <- javaWorkerEvent{done: true, err: err}
	}()
	event, err := client.grantJavaSlice(t.Context(), worker)
	if err != nil || event.err != nil {
		t.Fatalf("first slice: %v, %v", err, event.err)
	}
	if len(client.javaTry) != 1 || client.javaTry[0].Buffer != mainBuffer || client.javaCallDepth != 2 {
		t.Error("callback exception state was replaced")
	}
	if workerBuffer == mainBuffer {
		t.Error("worker and callback share a jump buffer")
	}
	client.dropJavaTryFrames(0)
	client.javaCallDepth = 0
	event, err = client.grantJavaSlice(t.Context(), worker)
	if err != nil || event.err != nil {
		t.Fatalf("resumed slice: %v, %v", err, event.err)
	}
	if len(client.javaTry) != 0 || client.javaCallDepth != 0 {
		t.Fatal("completed worker leaked exception state")
	}
}

func TestJavaKeyCheckpointPreservesPlatformMonitor(t *testing.T) {
	client := fixtureClient(t)
	runtime := client.javaRuntimeState()
	runtime.keyCallback = true
	runtime.workers = []*javaWorker{{grant: make(chan context.Context), events: make(chan javaWorkerEvent)}}
	if err := client.javaMonitorEnter(t.Context(), 0x1234); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 128; i++ {
		if err := client.serviceJavaKeyWorkers(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
	if runtime.monitors[0x1234].count != 1 {
		t.Fatal("checkpoint changed the held monitor")
	}
}
