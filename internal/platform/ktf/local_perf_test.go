package ktf

import (
	"context"
	"os"
	"strconv"
	"testing"
	"time"
)

// These measure the emulator against a real local game, which is why they are
// env-gated like the other local probes: the archives are not in the tree.
//
// WFEATURE_PERF_ARCHIVE names one. TestPerfProbe answers how much host CPU a
// second of guest time costs, on a manual clock so the same guest work is
// measured every run. The scheduling probes answer what a Host's own loop does
// with that, on the wall clock, and are the ones to compare when the page's
// tick loop changes.

// TestPerfProbe measures how much host CPU one second of guest time costs.
func TestPerfProbe(t *testing.T) {
	path := os.Getenv("WFEATURE_PERF_ARCHIVE")
	if path == "" {
		t.Skip("set WFEATURE_PERF_ARCHIVE to a local game archive")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	clock := NewManualClock(time.Time{})
	session, err := StartSession(ctx, data, SessionOptions{Clock: clock})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()

	// Warm through the loading phase so the measurement covers a live scene.
	for tick := 0; tick < 3000; tick++ {
		if _, err := session.Tick(ctx); err != nil {
			t.Fatal(err)
		}
		session.SkipToNextDeadline()
	}

	startGuest := session.GuestElapsed()
	startFlushes := session.Flushes()
	startWall := time.Now()
	for tick := 0; tick < 6000; tick++ {
		if _, err := session.Tick(ctx); err != nil {
			t.Fatal(err)
		}
		session.SkipToNextDeadline()
	}
	wall := time.Since(startWall)
	guest := session.GuestElapsed() - startGuest
	flushes := session.Flushes() - startFlushes

	t.Logf("guest=%v host=%v realtime_factor=%.2fx flushes=%d guest_fps=%.1f",
		guest, wall, guest.Seconds()/wall.Seconds(), flushes, float64(flushes)/guest.Seconds())
}

// TestPageSchedulingProbe replays the page's own reschedule rule against a
// real session: a tick that reported the guest waiting, or that returned in
// under 4ms, goes back to the frame clock; anything else re-enters at once.
func TestPageSchedulingProbe(t *testing.T) {
	path := os.Getenv("WFEATURE_PERF_ARCHIVE")
	if path == "" {
		t.Skip("set WFEATURE_PERF_ARCHIVE to a local game archive")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	session, err := StartSession(ctx, data, SessionOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()

	warm := time.Now()
	for time.Since(warm) < 20*time.Second {
		if _, err := session.Tick(ctx); err != nil {
			t.Fatal(err)
		}
	}

	const frame = 16667 * time.Microsecond
	startFlushes := session.Flushes()
	ticks, parked := 0, 0
	busy, worst := time.Duration(0), time.Duration(0)
	start := time.Now()
	for time.Since(start) < 20*time.Second {
		tickStart := time.Now()
		if _, err := session.Tick(ctx); err != nil {
			t.Fatal(err)
		}
		elapsed := time.Since(tickStart)
		busy += elapsed
		if elapsed > worst {
			worst = elapsed
		}
		ticks++
		deadline, pending := session.NextDeadline()
		waiting := pending && deadline.After(time.Now())
		if waiting || elapsed < 4*time.Millisecond {
			// The page waits for the next animation frame here.
			parked++
			time.Sleep(frame)
		}
	}
	wall := time.Since(start)
	flushes := session.Flushes() - startFlushes
	t.Logf("wall=%v ticks=%d parked=%d flushes=%d fps=%.1f tick_avg=%v tick_max=%v busy=%.1f%%",
		wall, ticks, parked, flushes, float64(flushes)/wall.Seconds(),
		busy/time.Duration(max(ticks, 1)), worst, 100*busy.Seconds()/wall.Seconds())
}

// TestBudgetedSchedulingProbe replays what a Host does with TickFor: one entry
// covers as many rounds as the guest has due, and the Host then sleeps exactly
// as long as the guest asked rather than rounding up to the frame clock.
func TestBudgetedSchedulingProbe(t *testing.T) {
	path := os.Getenv("WFEATURE_PERF_ARCHIVE")
	if path == "" {
		t.Skip("set WFEATURE_PERF_ARCHIVE to a local game archive")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	session, err := StartSession(ctx, data, SessionOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()

	warm := time.Now()
	for time.Since(warm) < 20*time.Second {
		if _, err := session.Tick(ctx); err != nil {
			t.Fatal(err)
		}
	}

	const budget = 8 * time.Millisecond
	startFlushes := session.Flushes()
	entries := 0
	busy, worst := time.Duration(0), time.Duration(0)
	start := time.Now()
	for time.Since(start) < 20*time.Second {
		entryStart := time.Now()
		_, wait, err := session.TickFor(ctx, budget)
		if err != nil {
			t.Fatal(err)
		}
		elapsed := time.Since(entryStart)
		busy += elapsed
		if elapsed > worst {
			worst = elapsed
		}
		entries++
		if wait > 0 {
			time.Sleep(wait)
		}
	}
	wall := time.Since(start)
	flushes := session.Flushes() - startFlushes
	t.Logf("wall=%v entries=%d flushes=%d fps=%.1f entry_avg=%v entry_max=%v busy=%.1f%%",
		wall, entries, flushes, float64(flushes)/wall.Seconds(),
		busy/time.Duration(max(entries, 1)), worst, 100*busy.Seconds()/wall.Seconds())
}

// TestSlowHostProbe asks what a host far slower than this machine gets. The
// slowness is imposed rather than measured — every round pays an extra delay —
// which is the only way to reach a phone's cost from a desktop. What matters is
// the ratio between the guest's own logic rate and the wall clock: a host that
// keeps that near 1 is running the game at the right speed and merely showing
// fewer frames, which is playable, and one that does not is running the game in
// slow motion, which is not.
func TestSlowHostProbe(t *testing.T) {
	path := os.Getenv("WFEATURE_PERF_ARCHIVE")
	if path == "" {
		t.Skip("set WFEATURE_PERF_ARCHIVE to a local game archive")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	// Running the guest faster than real time is the same demand on the host
	// as running it on a machine that much slower, and it is the only way to
	// reach a phone's cost from a desktop. WFEATURE_PERF_SPEED sets the factor.
	speed := 12.0
	if value := os.Getenv("WFEATURE_PERF_SPEED"); value != "" {
		parsed, parseErr := strconv.ParseFloat(value, 64)
		if parseErr != nil {
			t.Fatalf("invalid WFEATURE_PERF_SPEED %q", value)
		}
		speed = parsed
	}
	ctx := context.Background()
	session, err := StartSession(ctx, data, SessionOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()

	warm := time.Now()
	for time.Since(warm) < 20*time.Second {
		if _, err := session.Tick(ctx); err != nil {
			t.Fatal(err)
		}
	}
	session.SetSpeed(speed)

	// The guest's own elapsed time against the wall clock is the measure: at
	// this speed the game is meant to advance `speed` seconds per second, so
	// the ratio to that is how much of its schedule the host is actually
	// delivering.
	startGuest := session.GuestElapsed()
	startFlushes := session.Flushes()
	start := time.Now()
	for time.Since(start) < 20*time.Second {
		_, wait, tickErr := session.TickFor(ctx, 8*time.Millisecond)
		if tickErr != nil {
			t.Fatal(tickErr)
		}
		// A real host sleeps the wait it is given; spinning through it makes
		// every entry look free and the load measurement meaningless.
		if wait > 0 {
			time.Sleep(wait)
		}
	}
	wall := time.Since(start)
	guest := session.GuestElapsed() - startGuest
	flushes := session.Flushes() - startFlushes
	costs := session.HostCosts()
	t.Logf("speed=%.0fx wall=%v guest=%v schedule_kept=%.0f%% fps=%.1f rounds=%d paints_dropped=%d",
		speed, wall.Round(time.Millisecond), guest.Round(time.Millisecond),
		100*guest.Seconds()/(wall.Seconds()*speed), float64(flushes)/wall.Seconds(),
		costs.Rounds, costs.PaintsDropped)
}

// TestWallClockProbe runs the session the way the page does: the real clock,
// ticked in a tight loop. It reports the frame rate that reaches a Host which
// never stalls, so anything slower in the browser is the page's scheduling
// rather than the emulator's speed.
func TestWallClockProbe(t *testing.T) {
	path := os.Getenv("WFEATURE_PERF_ARCHIVE")
	if path == "" {
		t.Skip("set WFEATURE_PERF_ARCHIVE to a local game archive")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	session, err := StartSession(ctx, data, SessionOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()

	// Let the title get through loading before the frame rate is measured.
	warm := time.Now()
	for time.Since(warm) < 20*time.Second {
		if _, err := session.Tick(ctx); err != nil {
			t.Fatal(err)
		}
	}

	startFlushes := session.Flushes()
	ticks, busy := 0, time.Duration(0)
	start := time.Now()
	for time.Since(start) < 20*time.Second {
		tickStart := time.Now()
		if _, err := session.Tick(ctx); err != nil {
			t.Fatal(err)
		}
		busy += time.Since(tickStart)
		ticks++
	}
	wall := time.Since(start)
	flushes := session.Flushes() - startFlushes

	t.Logf("wall=%v ticks=%d flushes=%d fps=%.1f tick_avg=%v busy=%.1f%%",
		wall, ticks, flushes, float64(flushes)/wall.Seconds(),
		busy/time.Duration(max(ticks, 1)), 100*busy.Seconds()/wall.Seconds())
}

// TestLoadCostProbe measures what a title's load costs the host, which is the
// one scene the wall clock cannot report on: a game that sleeps through its
// splash screen takes seconds of wall time with the emulator barely working,
// so a run under -play measures the game's pacing rather than this. The manual
// clock skipped to each deadline removes the pacing and leaves host CPU
// against the guest time it bought. A load that costs more host seconds than
// the guest seconds it delivers is a load the user waits through.
//
// WFEATURE_LOAD_TICKS covers a longer load; two hundred is where the local
// archives are still loading and the slowest of them has not yet caught up.
func TestLoadCostProbe(t *testing.T) {
	path := os.Getenv("WFEATURE_PERF_ARCHIVE")
	if path == "" {
		t.Skip("set WFEATURE_PERF_ARCHIVE to a local game archive")
	}
	ticks := 200
	if value := os.Getenv("WFEATURE_LOAD_TICKS"); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil {
			t.Fatalf("invalid WFEATURE_LOAD_TICKS %q", value)
		}
		ticks = parsed
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	clock := NewManualClock(time.Time{})
	opened := time.Now()
	session, err := StartSession(ctx, data, SessionOptions{Clock: clock})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	startup := time.Since(opened)

	session.EnableProfile(1000)
	start := time.Now()
	for tick := 0; tick < ticks; tick++ {
		if _, err := session.Tick(ctx); err != nil {
			t.Fatal(err)
		}
		session.SkipToNextDeadline()
	}
	host := time.Since(start)
	guest := session.GuestElapsed()
	profile := session.Profile()

	// See the LGT probe: the decode cache costs one array per code page, so a
	// wider entry multiplies this rather than the working set.
	cachePages, cacheBytes := session.Client.core.Memory().DecodeCacheStats()
	t.Logf("decode_cache_pages=%d decode_cache_bytes=%d (%.1f MiB)",
		cachePages, cacheBytes, float64(cacheBytes)/(1<<20))
	t.Logf("startup=%v host=%v guest=%v ticks=%d instructions=%d host_per_guest=%.2f ns_per_step=%.2f",
		startup.Round(time.Millisecond), host.Round(time.Millisecond), guest,
		ticks, profile.Steps, host.Seconds()/max(guest.Seconds(), 1e-9),
		float64(host.Nanoseconds())/float64(max(profile.Steps, 1)))
}
