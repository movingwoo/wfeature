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

// **The frame timer of a title doing real work comes due while it is paused
// too.** The pause used to move the clock by the wait to the deadline, and
// that fell short by the smaller of the wait and the work of the last frame:
// a frame of a million and a half instructions is 10 ms of guest time, and
// after it the timer was still armed when the title was resumed. A deadline
// between two milliseconds, which a 1/60 s period makes, fell short by the
// fraction with no work at all. The fixture's frame callback spends the work,
// counts itself and arms itself again through MC_knlSetTimer, driven by real
// ticks; the park comes right after a frame.
func TestParkingACletServesItsFrameTimerAfterRealWork(t *testing.T) {
	for _, frame := range []struct {
		loops  uint32 // two instructions each
		period uint32 // what the callback asks MC_knlSetTimer for, in ms
	}{
		{750_000, 50}, // 1.5 million instructions: 10 ms of work
		{1_000, 1},    // answered at 1/60 s, with next to no work
		{1_080_000, 1},
	} {
		ctx := context.Background()
		session, err := StartSession(ctx, fixtureArchive(t), SessionOptions{Width: 16, Height: 8, MaxSteps: 1 << 26})
		if err != nil {
			t.Fatal(err)
		}
		client := session.client
		setTimer, err := client.importFunction(importTableWIPIC, slotSetTimer)
		if err != nil {
			t.Fatal(err)
		}
		const (
			code      = fixtureDataBase + 0x800
			counter   = fixtureDataBase + 0x400
			structure = fixtureDataBase + 0x200
		)
		a := &assembler{base: code}
		a.emit(armPushLR)
		a.emit(armMovReg(5, 0)) // r5 = the timer's structure
		a.literal(4, frame.loops)
		loop := a.here()
		a.emit(0xe2544001)                                                           // subs r4, r4, #1
		a.emit(0x1a000000 | uint32(int32(int64(loop)-int64(a.here()+8))/4)&0xffffff) // bne loop
		a.literal(7, counter)
		a.emit(armLdr(0, 7, 0), armAddImm(0, 0, 1), armStr(0, 7, 0))
		// MC_knlSetTimer(structure, period, 0, 0), last: the frame arms the next.
		a.emit(armMovReg(0, 5), armMovImm(1, frame.period), armMovImm(2, 0), armMovImm(3, 0))
		a.literal(6, setTimer)
		a.call(6)
		a.emit(armMovImm(0, 0))
		a.emit(armPopPC)
		if err := client.core.Memory().Write(code, a.finish()); err != nil {
			t.Fatal(err)
		}
		frames := func() uint32 {
			var word [4]byte
			if err := client.core.Memory().Read(counter, word[:]); err != nil {
				t.Fatal(err)
			}
			return binary.LittleEndian.Uint32(word[:])
		}
		client.mu.Lock()
		client.timers[structure] = &timer{structure: structure, callback: code,
			dueAt: time.Duration(client.clock.millis()) * time.Millisecond, armed: true}
		client.mu.Unlock()
		// Play until a tick has run a frame, and park right after it.
		before := frames()
		for tick := 0; tick < 50; tick++ {
			if err := session.Tick(ctx); err != nil {
				t.Fatal(err)
			}
			if frames() > before+2 {
				break
			}
		}
		played := frames()
		if played <= before {
			t.Fatalf("%d loops a %d ms frame: no frame ran before the park", frame.loops, frame.period)
		}
		if err := session.Pause(ctx); err != nil {
			t.Fatal(err)
		}
		if frames() == played {
			t.Errorf("%d loops a %d ms frame: the frame timer did not come due while the title was paused", frame.loops, frame.period)
		}
		session.Close(ctx)
	}
}

// **Every timer armed at the pause has its turn.** With a shorter timer armed
// beside the frame timer that carries the pause, the pause used to fire the
// shorter one alone and leave the frame timer armed, so the pause it carries
// never ran. The fixture's pause entry stands in for the shorter timer and its
// resume entry, which writes the running mark, for the frame timer.
func TestParkingACletServesEveryTimerArmedAtThePause(t *testing.T) {
	ctx := context.Background()
	session, err := StartSession(ctx, fixtureArchive(t), SessionOptions{Width: 16, Height: 8, MaxSteps: 1 << 20})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close(ctx)
	client := session.client
	const frameTimer, shorter = fixtureDataBase + 0x200, fixtureDataBase + 0x240
	before := client.clock.millis()
	client.mu.Lock()
	client.timers[frameTimer] = &timer{structure: frameTimer, callback: client.clet.Resume,
		dueAt: time.Duration(before+70) * time.Millisecond, armed: true}
	client.timers[shorter] = &timer{structure: shorter, callback: client.clet.Pause,
		dueAt: time.Duration(before+30) * time.Millisecond, armed: true}
	client.mu.Unlock()

	if err := session.Pause(ctx); err != nil {
		t.Fatal(err)
	}
	if got := lifecycleMark(t, session); got != fixtureLifecycleRunning {
		t.Fatalf("the Clet is %d after a pause, want %d: the frame timer did not run while it was paused", got, fixtureLifecycleRunning)
	}
	if client.timers[frameTimer].armed || client.timers[shorter].armed {
		t.Fatal("a timer armed at the pause is still armed after it")
	}
}

// **A timer far ahead is left for after the resume.** The pause used to move
// the clock to the earliest deadline however far off — a timeout is a guest's
// 64-bit number of milliseconds — and fire it there: ten minutes of guest
// time, or days, gone in a park. Nothing further than a second off is fired,
// and the clock does not move for it.
func TestParkingACletLeavesATimerFarAheadForTheResume(t *testing.T) {
	ctx := context.Background()
	session, err := StartSession(ctx, fixtureArchive(t), SessionOptions{Width: 16, Height: 8, MaxSteps: 1 << 20})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close(ctx)
	client := session.client
	const structure = fixtureDataBase + 0x200
	before := client.clock.millis()
	client.mu.Lock()
	client.timers[structure] = &timer{structure: structure, callback: client.clet.Resume,
		dueAt: time.Duration(before+600_000) * time.Millisecond, armed: true}
	client.mu.Unlock()

	if err := session.Pause(ctx); err != nil {
		t.Fatal(err)
	}
	if got := client.clock.millis(); got != before {
		t.Fatalf("the clock moved from %d to %d ms for a timer ten minutes off", before, got)
	}
	if !client.timers[structure].armed || lifecycleMark(t, session) != fixtureLifecyclePaused {
		t.Fatal("a timer ten minutes off fired in the pause")
	}
}
