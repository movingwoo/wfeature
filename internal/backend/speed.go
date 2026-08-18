package backend

import (
	"sync"
	"time"
)

// A Host lets a person change how fast a game runs, and every platform here
// answers that question with the same numbers. The range lives here rather
// than in one of them because it is the Host's contract: the browser's control
// carries 0.25 through 4, a Host that keeps the setting across sessions stores
// what it would actually run at, and a platform that clamped differently would
// make the same setting mean two things.
//
// What a multiplier means is the same everywhere too. 1 is the speed the game
// was written for. 2 runs the guest's clock twice as fast and halves what a
// Host waits between its callbacks, so a game that measures elapsed time
// itself speeds up by the same factor instead of fighting the change.
const (
	// SpeedFloor keeps a mistyped value from stalling a game into apparent
	// death, and SpeedCeiling is the practical "as fast as it goes": past it
	// the emulator's own throughput, not the guest's waits, is what limits the
	// frame rate.
	SpeedFloor   = 0.1
	SpeedCeiling = 16.0
)

// ClampSpeed answers the multiplier a session would actually run at. A zero or
// negative value selects the speed the game was written for, because "no
// setting" and "the normal setting" are the same thing to a game.
func ClampSpeed(multiplier float64) float64 {
	switch {
	case multiplier <= 0:
		return 1
	case multiplier < SpeedFloor:
		return SpeedFloor
	case multiplier > SpeedCeiling:
		return SpeedCeiling
	}
	return multiplier
}

// SpeedClock is a clock that runs at a multiple of another one's rate.
//
// A game that measures its own elapsed time decides how far to step from what
// the clock tells it, so running one faster is not a matter of calling it more
// often: called twice as often against an unchanged clock, it sees half as
// much time pass per call and steps half as far, and the game does not speed
// up at all. What has to move is the clock it reads. What a Host waits between
// callbacks is the other half of the same setting, and SourceDuration converts
// it back.
//
// Changing the rate rebases rather than jumps: the time already seen is kept
// and only what follows runs at the new rate, so a game that has been running
// for an hour does not find itself two hours old because a person moved a
// control.
//
// It is safe for concurrent use, because a guest thread reading the clock and
// a Host changing the setting are not the same goroutine.
type SpeedClock struct {
	mu     sync.Mutex
	source func() time.Time
	// sourceAt and scaledAt are the instant the current rate started at, on
	// each clock.
	sourceAt time.Time
	scaledAt time.Time
	speed    float64
}

// NewSpeedClock scales the clock source reads. A nil source reads the wall.
func NewSpeedClock(source func() time.Time) *SpeedClock {
	if source == nil {
		source = time.Now
	}
	now := source()
	return &SpeedClock{source: source, sourceAt: now, scaledAt: now, speed: 1}
}

// Now is the time the game sees.
func (clock *SpeedClock) Now() time.Time {
	clock.mu.Lock()
	defer clock.mu.Unlock()
	return clock.now()
}

func (clock *SpeedClock) now() time.Time {
	return clock.scaledAt.Add(time.Duration(float64(clock.source().Sub(clock.sourceAt)) * clock.speed))
}

// SetSpeed changes the rate. See ClampSpeed for what a value outside the range
// becomes.
func (clock *SpeedClock) SetSpeed(multiplier float64) {
	clock.mu.Lock()
	defer clock.mu.Unlock()
	clock.scaledAt = clock.now()
	clock.sourceAt = clock.source()
	clock.speed = ClampSpeed(multiplier)
}

// Speed reports the multiplier in force.
func (clock *SpeedClock) Speed() float64 {
	clock.mu.Lock()
	defer clock.mu.Unlock()
	return clock.speed
}

// SourceDuration answers what a stretch of the game's time costs on the clock
// underneath, which is what a Host waiting out a callback interval waits.
func (clock *SpeedClock) SourceDuration(scaled time.Duration) time.Duration {
	clock.mu.Lock()
	defer clock.mu.Unlock()
	return time.Duration(float64(scaled) / clock.speed)
}

// SourceInstant answers when an instant on the game's clock arrives on the one
// underneath, which is what a Host waiting for a deadline waits until.
func (clock *SpeedClock) SourceInstant(scaled time.Time) time.Time {
	clock.mu.Lock()
	defer clock.mu.Unlock()
	return clock.sourceAt.Add(time.Duration(float64(scaled.Sub(clock.scaledAt)) / clock.speed))
}
