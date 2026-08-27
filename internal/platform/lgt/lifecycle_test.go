package lgt

import (
	"context"
	"encoding/binary"
	"testing"
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
