package clet

import (
	"context"
	"fmt"
	"time"

	"github.com/movingwoo/wfeature/internal/backend"
)

// The session driver is the Host-facing API that runs one Clet. It mirrors the
// KTF session's driving surface — TickFor, SendKey, Frame — so a Host runs a
// Clet the same way it runs a KTF title. A Clet has no cards, guest threads or
// pointer, so the driver is the timer-and-event service round and nothing else.

// Tick runs one service round: the timers the frames come from, the events the
// program posted, a repaint it asked for, and the audio each round does.
func (session *Session) Tick(ctx context.Context) (bool, error) {
	if session == nil || session.Client == nil {
		return false, fmt.Errorf("SKT Clet session is not started")
	}
	return session.tickClet(ctx)
}

// TickFor runs rounds until the program parks on its own next deadline or the
// budget is spent, and answers how long the program asked to wait. A Clet does
// not drop paints the way a card-driven KTF title does: it draws from its own
// timer, so there is no Host round paint to give up.
func (session *Session) TickFor(ctx context.Context, budget time.Duration) (bool, time.Duration, error) {
	if session == nil || session.Client == nil {
		return false, 0, fmt.Errorf("SKT Clet session is not started")
	}
	progressed := false
	client := session.Client
	entered := client.now()
	deadline := entered.Add(budget)
	for {
		ran, err := session.Tick(ctx)
		progressed = progressed || ran
		if err != nil {
			return progressed, 0, err
		}
		if wait, parked := session.waitBeforeNextRound(); parked {
			return progressed, wait, nil
		}
		if !client.now().Before(deadline) {
			return progressed, 0, nil
		}
	}
}

// waitBeforeNextRound answers the wait until the program's next parked work,
// which a Host idles for instead of polling.
func (session *Session) waitBeforeNextRound() (time.Duration, bool) {
	deadline, parked := session.NextDeadline()
	if !parked {
		return 0, false
	}
	wait := deadline.Sub(session.Client.now())
	if wait <= 0 {
		return 0, false
	}
	return wait, true
}

// NextDeadline reports when the program's next parked work falls due.
func (session *Session) NextDeadline() (time.Time, bool) {
	if session == nil || session.Client == nil {
		return time.Time{}, false
	}
	return session.Client.NextDeadline()
}

// SkipToNextDeadline jumps a manual clock to the next deadline, for a batch Host.
func (session *Session) SkipToNextDeadline() bool {
	if session == nil || session.Client == nil {
		return false
	}
	return session.Client.SkipToNextDeadline()
}

// SendKey delivers one key to the program's event entry.
func (session *Session) SendKey(ctx context.Context, eventType, key int32) error {
	if session == nil || session.Client == nil {
		return fmt.Errorf("SKT Clet session is not started")
	}
	return session.Client.sendCletKey(ctx, eventType, key)
}

// SendPointer is a no-op: a Clet's event entry takes keys only.
func (session *Session) SendPointer(ctx context.Context, eventType, x, y int32) error { return nil }

// HasPointer reports that a Clet takes no pointer input.
func (session *Session) HasPointer() bool { return false }

// Pause and Resume run the program's own pause and resume entries.
func (session *Session) Pause(ctx context.Context) error {
	if session == nil || session.Client == nil {
		return nil
	}
	return session.Client.cletLifecycle(ctx, session.Client.clet.program.Entries.Pause, false)
}

func (session *Session) Resume(ctx context.Context) error {
	if session == nil || session.Client == nil {
		return nil
	}
	return session.Client.cletLifecycle(ctx, session.Client.clet.program.Entries.Resume, false)
}

// Close runs the program's destroy entry and stops whatever was sounding.
func (session *Session) Close() {
	if session == nil || session.Client == nil {
		return
	}
	if session.Client.audio != nil {
		defer session.Client.audio.StopAll()
	}
	ctx, cancel := context.WithTimeout(context.Background(), backend.EndingBound)
	defer cancel()
	_ = session.Client.cletLifecycle(ctx, session.Client.clet.program.Entries.Destroy, true)
}

// Frame is the converted RGBA frame and its size.
func (session *Session) Frame() ([]byte, int, int, uint32) {
	if session == nil || session.Client == nil {
		return nil, 0, 0, 0
	}
	return session.Client.Frame()
}

// FrameDigest is a stable digest of the current frame, for the probes.
func (session *Session) FrameDigest() uint64 {
	if session == nil || session.Client == nil {
		return 0
	}
	return session.Client.FrameDigest()
}

// Screen is the handset size the program draws on.
func (session *Session) Screen() (int, int) {
	if session == nil || session.Client == nil {
		return runtimeDisplayPixelWidth, runtimeDisplayPixelHeight
	}
	return session.Client.Screen()
}

// Flushes counts the frames the program put on the screen.
func (session *Session) Flushes() uint32 {
	if session == nil || session.Client == nil {
		return 0
	}
	return session.Client.Flushes()
}

// GuestElapsed is how much guest time has passed, which the audio runs on.
func (session *Session) GuestElapsed() time.Duration {
	if session == nil || session.Client == nil || session.Client.runtime == nil {
		return 0
	}
	return session.Client.runtime.guestElapsed()
}

// Vibration is what the program last asked the handset's motor to do.
func (session *Session) Vibration() backend.Vibration {
	if session == nil || session.Client == nil {
		return backend.Vibration{}
	}
	return session.Client.vibrator.State()
}

// SetSpeed scales the pace, and Speed reports the multiplier in force.
func (session *Session) SetSpeed(multiplier float64) {
	if session == nil || session.Client == nil {
		return
	}
	session.Client.SetSpeed(multiplier)
}

func (session *Session) Speed() float64 {
	if session == nil || session.Client == nil {
		return 1
	}
	return session.Client.Speed()
}
