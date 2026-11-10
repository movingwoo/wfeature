package lgt

import (
	"context"
	"encoding/binary"
	"errors"
	"testing"
	"time"
)

// lifecycleMark reads the word the fixture's Clet writes when the platform
// calls one of its lifecycle entry points.
func lifecycleMark(t *testing.T, session *Session) uint32 {
	t.Helper()
	var word [4]byte
	if err := session.client.core.Memory().Read(fixtureLifecycle, word[:]); err != nil {
		t.Fatalf("read the lifecycle mark: %v", err)
	}
	return binary.LittleEndian.Uint32(word[:])
}

// A Host parks a game whose page has gone away rather than closing it, and a
// handset did the same thing when a call arrived. Both halves have to reach
// the Clet: the pause on the way out, so a title can stop what it was doing,
// and the resume on the way back, so it can notice that the clock moved while
// nobody was watching.
//
// This is the test that would have failed for as long as the platform had
// PauseClet and ResumeClet and no Host called either.
func TestParkingACletTellsIt(t *testing.T) {
	ctx := context.Background()
	session, err := StartSession(ctx, fixtureArchive(t), SessionOptions{
		Width: 16, Height: 8, MaxSteps: 1 << 20,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close(ctx)

	if got := lifecycleMark(t, session); got != fixtureLifecycleRunning {
		t.Fatalf("the Clet is %d after starting, want %d", got, fixtureLifecycleRunning)
	}
	if err := session.Pause(ctx); err != nil {
		t.Fatalf("Pause() error = %v", err)
	}
	if got := lifecycleMark(t, session); got != fixtureLifecyclePaused {
		t.Fatalf("the Clet is %d after a pause, want %d", got, fixtureLifecyclePaused)
	}
	if err := session.Resume(ctx); err != nil {
		t.Fatalf("Resume() error = %v", err)
	}
	if got := lifecycleMark(t, session); got != fixtureLifecycleRunning {
		t.Fatalf("the Clet is %d after a resume, want %d", got, fixtureLifecycleRunning)
	}
	// A paused game that came back is a game that still runs. The point of
	// parking is that the session survives it.
	if err := session.Tick(ctx); err != nil {
		t.Fatalf("Tick() after a resume: %v", err)
	}
}

// Half the local titles declare a lifecycle entry that is a prologue and a
// return, and some declare none at all — the module's table simply holds a
// zero. That is the title saying it has nothing to do, not an error, and a
// Host that treated it as one would refuse to park a game that is perfectly
// well.
func TestACletThatDeclaresNoLifecycleIsNotAFailure(t *testing.T) {
	ctx := context.Background()
	session, err := StartSession(ctx, fixtureArchive(t), SessionOptions{
		Width: 16, Height: 8, MaxSteps: 1 << 20,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close(ctx)

	session.client.clet.Pause, session.client.clet.Resume = 0, 0
	if err := session.Pause(ctx); err != nil {
		t.Fatalf("Pause() over a Clet with no pause entry = %v", err)
	}
	if err := session.Resume(ctx); err != nil {
		t.Fatalf("Resume() over a Clet with no resume entry = %v", err)
	}
}

// A start that fails after the title started a thread ends that thread: the
// session is never returned, so nothing would close it.
func TestAFailedStartStopsTheThreadsItStarted(t *testing.T) {
	fixture := newJavaThreadFixture(t, nil)
	run := fixture.install(func(a *assembler) {
		top := a.here()
		fixture.call(a, "yield")
		fixture.loop(a, top)
	})
	fixture.start("Runner", run)
	client := fixture.client
	if len(client.javaRun.workers) != 1 {
		t.Fatalf("the fixture started %d threads", len(client.javaRun.workers))
	}
	worker := client.javaRun.workers[0]
	cause := errors.New("the title stopped")
	failure := startFailure(client, cause)
	if !errors.Is(failure, cause) {
		t.Fatalf("the failure reports %v", failure)
	}
	if !worker.done || !client.javaThreadsStopped || len(client.javaRun.workers) != 0 {
		t.Fatal("a failed start left the title's thread waiting for a slice")
	}
}

// A handset kept time while a title was paused, so the timer a Clet had armed
// came due during the pause, and a title that does its pausing in that
// callback depends on it: one stopped its sound and its frame timer there, for
// resumeClet to start them again, and a park that served no timer before the
// resume froze it for good. The fixture's resume entry stands in for such a
// callback: it writes the running mark, so the mark after the pause says
// whether the timer ran while the title was paused.
func TestParkingACletServesTheTimerThatComesDueWhilePaused(t *testing.T) {
	ctx := context.Background()
	session, err := StartSession(ctx, fixtureArchive(t), SessionOptions{
		Width: 16, Height: 8, MaxSteps: 1 << 20,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close(ctx)
	client := session.client

	const structure = fixtureDataBase + 0x200
	before := client.clock.millis()
	client.mu.Lock()
	client.timers[structure] = &timer{structure: structure, callback: client.clet.Resume,
		dueAt: time.Duration(before+70) * time.Millisecond, armed: true}
	client.mu.Unlock()

	if err := session.Pause(ctx); err != nil {
		t.Fatalf("Pause() error = %v", err)
	}
	if got := lifecycleMark(t, session); got != fixtureLifecycleRunning {
		t.Fatalf("the Clet is %d after a pause, want %d: the timer that came due while it was paused never ran", got, fixtureLifecycleRunning)
	}
	if after := client.clock.millis(); after < before+70 {
		t.Fatalf("the clock reads %d ms after the pause, want at least %d: it has to reach the timer's deadline", after, before+70)
	}
	if client.timers[structure].armed {
		t.Fatal("the timer is still armed after it fired")
	}

	// With nothing armed a pause moves no clock.
	if err := session.Resume(ctx); err != nil {
		t.Fatal(err)
	}
	still := client.clock.millis()
	if err := session.Pause(ctx); err != nil {
		t.Fatal(err)
	}
	if got := client.clock.millis(); got != still {
		t.Fatalf("a pause with no timer armed moved the clock from %d to %d ms", still, got)
	}
}
