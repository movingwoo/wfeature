package lgt

import (
	"context"
	"os"
	"slices"
	"strconv"
	"testing"
	"time"

	// The key table a route is written against is one table for every
	// platform, and it lives beside the platform that needed it first — the
	// CLI reaches for the same one when it runs an LGT route.
	"github.com/movingwoo/wfeature/internal/platform/ktf"
	"github.com/movingwoo/wfeature/internal/route"
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
	// A fresh save directory by default, so a run is repeatable; but the scene
	// a route is written for is usually inside a save — a field, a battle — and
	// a route replayed from a fresh boot stops at the title screen having
	// measured nothing. WFEATURE_SAVE_ROOT points the session at a copy of a
	// real one. Copy it rather than naming the live directory: the run plays,
	// and a probe that writes the save it depends on measures something
	// different every time.
	saveRoot := os.Getenv("WFEATURE_SAVE_ROOT")
	if saveRoot == "" {
		saveRoot = t.TempDir()
	}
	options := SessionOptions{SaveRoot: saveRoot}
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
	// WFEATURE_PROFILE samples where the guest spends its instructions. It is
	// off by default because sampling costs a stack walk, and on a run of this
	// length that is 5% of the wall clock — enough to move the tick
	// percentiles this probe also reports.
	if os.Getenv("WFEATURE_PROFILE") != "" {
		session.EnableProfile(0)
	}
	start := time.Now()
	slowest := time.Duration(0)
	// The mean tick says nothing about this platform: a title computes almost
	// nothing on most ticks and a frame on a few, so p50 is microseconds while
	// p90 is the frame. **p90 against the guest's own tick is what says whether
	// the emulator keeps up**, and it is the number a "combat is slow" report
	// is about. Kept per tick rather than as a running estimate because the
	// run is a few thousand ticks and exactness is free at that size.
	costs := make([]time.Duration, 0, ticks)
	tick := func() error {
		began := time.Now()
		var wait time.Duration
		var err error
		if paced {
			wait, err = session.TickFor(ctx)
		} else {
			err = session.Tick(ctx)
		}
		if err != nil {
			return err
		}
		cost := time.Since(began)
		costs = append(costs, cost)
		if cost > slowest {
			slowest = cost
		}
		if paced && wait > 0 {
			time.Sleep(wait)
		}
		return nil
	}
	// **WFEATURE_PERF_ROUTE is what makes this probe measure a game.** Without
	// one a title spends its first thousands of ticks on a title screen it
	// barely computes for, and the profile is of a session waiting. A route
	// drives it to where it is doing the work the report is about, which is
	// the only place a throughput number means anything.
	if script := os.Getenv("WFEATURE_PERF_ROUTE"); script != "" {
		text, err := os.ReadFile(script)
		if err != nil {
			t.Fatal(err)
		}
		parsed, err := route.Parse(string(text), ktf.KeyCodeByName)
		if err != nil {
			t.Fatalf("route %s: %v", script, err)
		}
		stopped := false
		runner := &route.Runner{
			MaxTicks: ticks,
			Hold:     20,
			Digest:   session.FrameDigest,
			Advance: func(context.Context) (bool, error) {
				if err := tick(); err != nil {
					stopped = true
					return false, err
				}
				return true, nil
			},
			SendKey: func(_ context.Context, pressed bool, key int32) error {
				session.SendKey(pressed, uint32(key))
				return nil
			},
			Stalled: func() bool { return stopped },
		}
		result, err := runner.Run(ctx, parsed)
		if err != nil {
			t.Fatal(err)
		}
		if !result.Completed {
			t.Logf("route stopped at step %d (%s)", result.StoppedAt+1, result.Reason)
		}
	} else {
		for step := 0; step < ticks; step++ {
			if err := tick(); err != nil {
				t.Fatalf("tick %d: %v", step, err)
			}
		}
	}
	host := time.Since(start)
	busy := time.Duration(0)
	for _, cost := range costs {
		busy += cost
	}
	slices.Sort(costs)
	percentile := func(fraction float64) time.Duration {
		if len(costs) == 0 {
			return 0
		}
		return costs[int(float64(len(costs)-1)*fraction)]
	}

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
	t.Logf("busy=%v tick_p50=%v tick_p90=%v tick_p99=%v",
		busy.Round(time.Millisecond), percentile(0.50).Round(time.Microsecond),
		percentile(0.90).Round(time.Microsecond), percentile(0.99).Round(time.Microsecond))
	if os.Getenv("WFEATURE_PROFILE") != "" {
		t.Logf("\n%s", session.Profile().Report(25))
	}
	t.Logf("startup=%v host=%v ticks=%d per_tick=%v slowest_tick=%v flushes=%d steps=%d ns_per_step=%.2f",
		startup.Round(time.Millisecond), host.Round(time.Millisecond), ticks,
		(host / time.Duration(ticks)).Round(time.Microsecond),
		slowest.Round(time.Millisecond), session.Flushes(), steps,
		float64(host.Nanoseconds())/float64(max(steps, 1)))
}
