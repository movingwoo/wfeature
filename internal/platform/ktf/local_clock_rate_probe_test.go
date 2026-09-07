package ktf

import (
	"context"
	"os"
	"strconv"
	"testing"
	"time"
)

// How fast does the guest's clock run against the wall, and how many frames
// does the guest publish for each one a Host collects?
//
// Those are the two numbers that separate the two ways a title can look
// broken to a person. A title that reads the clock to decide when its next
// frame is due spins if the clock runs ahead of it and waits far too long if
// it runs behind, so a rate that is not 1.0 on a wall-clock session explains
// both at once. A title that never reads the clock cannot be explained that
// way at all, and then the frames-per-round figure is what says whether it is
// pacing itself or running flat out.
//
// The probe measures both on the path a person actually plays on — no manual
// clock, no deadline skipping — and drives the rounds the way the interactive
// CLI does: a round, then a wait as long as the guest's own next deadline.
//
//	WFEATURE_KTF_CLOCK_RATE_PROBE=1 \
//	WFEATURE_KTF_CLOCK_RATE_ARCHIVE=/absolute/path.zip \
//	go test -run TestLocalKTFGuestClockRate -v ./internal/platform/ktf
//
// WFEATURE_KTF_CLOCK_RATE_TICKS sets how many rounds are run. The elapsed
// times it prints are wall-clock and therefore noise if anything else is
// running; the ratio between them is not, and neither is the flush count.
func TestLocalKTFGuestClockRate(t *testing.T) {
	if os.Getenv("WFEATURE_KTF_CLOCK_RATE_PROBE") != "1" {
		t.Skip("set WFEATURE_KTF_CLOCK_RATE_PROBE=1 to measure a title's guest clock against the wall")
	}
	archive := os.Getenv("WFEATURE_KTF_CLOCK_RATE_ARCHIVE")
	if archive == "" {
		t.Skip("set WFEATURE_KTF_CLOCK_RATE_ARCHIVE to an absolute archive path")
	}
	ticks := 600
	if value := os.Getenv("WFEATURE_KTF_CLOCK_RATE_TICKS"); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil || parsed <= 0 {
			t.Fatalf("WFEATURE_KTF_CLOCK_RATE_TICKS = %q, want a positive count", value)
		}
		ticks = parsed
	}
	data, err := os.ReadFile(archive)
	if err != nil {
		t.Fatal(err)
	}
	// No Clock: the wall clock, which is what an interactive Host gets and
	// what both reports this was written for are about.
	session, err := StartSession(context.Background(), data, SessionOptions{
		SaveRoot: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	defer session.Close()
	ctx := context.Background()
	started := time.Now()
	guestAtStart := session.GuestElapsed()
	ran := 0
	for ; ran < ticks; ran++ {
		if _, err := session.Tick(ctx); err != nil {
			t.Logf("round %d refused: %v", ran, err)
			break
		}
		// The interactive CLI's pacing: idle exactly as long as the guest
		// asked to wait, and not at all when it asked for nothing.
		if deadline, pending := session.NextDeadline(); pending {
			if wait := time.Until(deadline); wait > 0 {
				time.Sleep(min(wait, 50*time.Millisecond))
			}
		}
	}
	real := time.Since(started)
	guest := session.GuestElapsed() - guestAtStart
	flushes := session.Flushes()
	rate := 0.0
	if real > 0 {
		rate = float64(guest) / float64(real)
	}
	perRound := 0.0
	if ran > 0 {
		perRound = float64(flushes) / float64(ran)
	}
	t.Logf("rounds %d, guest %v against wall %v, rate %.4f, flushes %d (%.1f per round)",
		ran, guest.Round(time.Millisecond), real.Round(time.Millisecond), rate, flushes, perRound)
}
