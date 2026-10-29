package lgt

import (
	"context"
	"slices"
	"testing"

	"github.com/movingwoo/wfeature/internal/backend"
)

// joinFixture starts a loader that counts three sleeps in word 0 and ends, and
// answers its Thread.
func joinFixture(t *testing.T) (*javaThreadFixture, uint32) {
	t.Helper()
	store, _ := backend.NewMemorySaveStore(nil)
	fixture := newJavaThreadFixture(t, store)
	join, err := fixture.client.stub(svcCategoryJava, javaVirtualSlot(javaThreadClass, 17))
	if err != nil {
		t.Fatal(err)
	}
	fixture.stubs["join"] = join
	loader := fixture.install(func(a *assembler) {
		a.emit(armPushLR)
		for range 3 {
			fixture.increment(a, 0)
			fixture.call(a, "sleep", 30, 0)
		}
		a.emit(armPopPC)
	})
	return fixture, fixture.start("Loader", loader)
}

// A guest thread that joins another waits until that one has ended, a slice at
// a time, and a checkpoint taken while it waits restores to the same wait.
func TestJavaThreadJoinWaitsForTheThreadToEnd(t *testing.T) {
	fixture, loaded := joinFixture(t)
	client := fixture.client
	if dispatch, ok := client.javaPlatformVirtualDispatch(javaVirtualSlot(javaThreadClass, 17)); !ok || dispatch.Called != "join()V" {
		t.Fatalf("the join stub resolves to %+v", dispatch)
	}
	waiter := fixture.install(func(a *assembler) {
		fixture.call(a, "join", loaded)
		top := a.here()
		fixture.increment(a, 1)
		fixture.call(a, "sleep", 30, 0)
		fixture.loop(a, top)
	})
	fixture.start("Waiter", waiter)
	fixture.settle()

	joined, sawJoin := false, false
	for tick := 0; tick < 40 && !joined; tick++ {
		done := client.javaRun.threads[loaded].worker.done
		words := fixture.words(fixture.session)
		if words[1] != 0 && !done {
			t.Fatalf("tick %d: the waiter ran on while the loader was alive (%v)", tick, words)
		}
		if !done {
			checkpoint := roundTripCheckpoint(t, fixture.archive, fixture.session)
			if chains := remainders(decodeRuntime(t, checkpoint)); slices.Contains(chains, "join") {
				sawJoin = true
				fixture.compare(fixture.restore(checkpoint), 8)
			}
		}
		joined = words[1] != 0
		tickSession(t, fixture.session, 1)
	}
	if !joined {
		t.Fatalf("the waiter never got past the join (%v)", fixture.words(fixture.session))
	}
	if !sawJoin {
		t.Error("no checkpoint was taken with the waiter parked in its join")
	}
	if words := fixture.words(fixture.session); words[0] != 3 {
		t.Errorf("the loader counted %d, want 3", words[0])
	}
}

// The platform's own thread has no slice to end, so it gives the other threads
// theirs until the one it joins has ended; a thread nothing started is not
// alive and is not waited for.
func TestJavaThreadJoinFromThePlatformsThread(t *testing.T) {
	fixture, loaded := joinFixture(t)
	client := fixture.client
	fixture.settle()
	if _, err := javaThreadJoin(client, context.Background(), client.thread, []uint32{loaded}); err != nil {
		t.Fatal(err)
	}
	if !client.javaRun.threads[loaded].worker.done || fixture.words(fixture.session)[0] != 3 {
		t.Fatalf("join returned with the loader at %v", fixture.words(fixture.session))
	}
	idle, err := newTestObject(t, client, javaThreadClass)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := javaThreadJoin(client, context.Background(), client.thread, []uint32{idle}); err != nil {
		t.Errorf("join on a thread nothing started = %v", err)
	}
}
