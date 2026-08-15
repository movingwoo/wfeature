package lgt

import (
	"context"
	"os"
	"strconv"
	"testing"
	"time"
)

// TestLGTLoadCostProbe is the LGT counterpart of the KTF load probe: it runs a
// real archive for a fixed number of ticks and reports what the run cost the
// host. It exists to be run under `-cpuprofile`, because "the emulator is
// slower here than on the other platform" is a question about where the host's
// time goes rather than about what the guest computes.
//
//	WFEATURE_PERF_ARCHIVE=var/games/lgt/game.zip \
//	  go test ./internal/platform/lgt -run LGTLoadCost -cpuprofile cpu.out
func TestLGTLoadCostProbe(t *testing.T) {
	path := os.Getenv("WFEATURE_PERF_ARCHIVE")
	if path == "" {
		t.Skip("set WFEATURE_PERF_ARCHIVE to a local game archive")
	}
	ticks := 600
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
	opened := time.Now()
	options := SessionOptions{SaveRoot: t.TempDir()}
	if value := os.Getenv("WFEATURE_TICK_MS"); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil || parsed <= 0 {
			t.Fatalf("invalid WFEATURE_TICK_MS %q", value)
		}
		options.Tick = time.Duration(parsed) * time.Millisecond
	}
	session, err := StartSession(ctx, data, options)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close(ctx)
	startup := time.Since(opened)

	// Tick rather than TickFor: TickFor's answer is how long the Host should
	// wait before the next tick, not what this one cost, and a probe measuring
	// cost has no business waiting at all. Reading the wait as the cost is how
	// an emulator running comfortably ahead of real time reported seconds-long
	// "slowest ticks" that were entirely idle.
	// WFEATURE_PACED runs the session the way a Host on a real clock does,
	// through TickFor and its wait. That is the only way to see the two
	// numbers a player feels: frames delivered per second of real time, and
	// whether guest time is keeping pace with real time or running away from
	// it. Unpaced is for throughput, where waiting is noise.
	paced := os.Getenv("WFEATURE_PACED") != ""
	start := time.Now()
	slowest := time.Duration(0)
	for tick := 0; tick < ticks; tick++ {
		began := time.Now()
		var wait time.Duration
		var err error
		if paced {
			wait, err = session.TickFor(ctx)
		} else {
			err = session.Tick(ctx)
		}
		if err != nil {
			t.Fatalf("tick %d: %v", tick, err)
		}
		if cost := time.Since(began); cost > slowest {
			slowest = cost
		}
		if paced && wait > 0 {
			time.Sleep(wait)
		}
	}
	host := time.Since(start)

	// Host time per tick is not comparable across a change that alters how
	// much guest work a tick contains, so the instruction count comes with it:
	// nanoseconds per instruction retired is the number a throughput change
	// has to move.
	steps := session.client.core.Steps()
	guest := session.GuestElapsed()
	t.Logf("real=%v guest=%v speed=%.2fx flushes=%d real_fps=%.1f guest_fps=%.1f",
		host.Round(time.Millisecond), guest.Round(time.Millisecond),
		guest.Seconds()/max(host.Seconds(), 1e-9), session.Flushes(),
		float64(session.Flushes())/max(host.Seconds(), 1e-9),
		float64(session.Flushes())/max(guest.Seconds(), 1e-9))
	// The decode cache's cost is per code page, not per hot loop, so widening
	// its entry multiplies this number rather than the working set. It is
	// reported here so the two halves of that trade are measured in one run.
	cachePages, cacheBytes := session.client.core.Memory().DecodeCacheStats()
	t.Logf("decode_cache_pages=%d decode_cache_bytes=%d (%.1f MiB)",
		cachePages, cacheBytes, float64(cacheBytes)/(1<<20))
	t.Logf("startup=%v host=%v ticks=%d per_tick=%v slowest_tick=%v flushes=%d steps=%d ns_per_step=%.2f",
		startup.Round(time.Millisecond), host.Round(time.Millisecond), ticks,
		(host / time.Duration(ticks)).Round(time.Microsecond),
		slowest.Round(time.Millisecond), session.Flushes(), steps,
		float64(host.Nanoseconds())/float64(max(steps, 1)))
}
