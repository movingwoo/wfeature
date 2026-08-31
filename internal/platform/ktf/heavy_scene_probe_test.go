package ktf

import (
	"context"
	"fmt"
	"os"
	"runtime/pprof"
	"sort"
	"testing"
	"time"

	"github.com/movingwoo/wfeature/internal/route"
)

// TestHeavySceneProbe replays a route on a manual clock and reports what each
// tick cost the host, so a scene inside the route can be read on its own.
//
// The routed load probe measures where a route ends; a scene that is a burst in
// the middle of one — a full-screen effect that lasts a second — is over before
// that probe starts counting. This walks the whole replay, keeps the wall cost
// and the instructions retired for every tick, and reports the window between
// two marks beside the run as a whole.
//
// It runs on a manual clock, so two runs of one binary agree to the tenth of a
// millisecond and an A/B is readable without repeating it. `-count=1` is not
// optional: without it the second arm of an A/B comes back from the test cache.
//
// WFEATURE_PERF_DIGESTS writes the frame digest and the instruction count of
// every tick. Two runs whose digests agree line for line and whose counts agree
// to the instruction have changed nothing the guest can see, and a diff of the
// two files names the first tick where that stops being true. WFEATURE_PERF_CPU
// starts a Go CPU profile at the route's first mark.
//
//	WFEATURE_PERF_ARCHIVE=/abs/game.zip WFEATURE_PERF_ROUTE=/abs/scene.route \
//	  go test ./internal/platform/ktf -run TestHeavySceneProbe -v -timeout 30m
func TestHeavySceneProbe(t *testing.T) {
	archivePath := os.Getenv("WFEATURE_PERF_ARCHIVE")
	routePath := os.Getenv("WFEATURE_PERF_ROUTE")
	if archivePath == "" || routePath == "" {
		t.Skip("set WFEATURE_PERF_ARCHIVE and WFEATURE_PERF_ROUTE")
	}
	archive, err := os.ReadFile(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	text, err := os.ReadFile(routePath)
	if err != nil {
		t.Fatal(err)
	}
	script, err := route.Parse(string(text), KeyCodeByName)
	if err != nil {
		t.Fatal(err)
	}

	var cpuProfile *os.File
	profiling := false
	if path := os.Getenv("WFEATURE_PERF_CPU"); path != "" {
		file, createErr := os.Create(path)
		if createErr != nil {
			t.Fatal(createErr)
		}
		cpuProfile = file
		defer file.Close()
	}

	var digests *os.File
	if path := os.Getenv("WFEATURE_PERF_DIGESTS"); path != "" {
		file, createErr := os.Create(path)
		if createErr != nil {
			t.Fatal(createErr)
		}
		digests = file
		defer file.Close()
	}

	ctx := context.Background()
	clock := NewManualClock(time.Time{})
	session, err := StartSession(ctx, archive, SessionOptions{Clock: clock, SaveRoot: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()

	type tickCost struct {
		cost  time.Duration
		steps uint64
	}
	costs := make([]tickCost, 0, 4096)
	marks := map[string]int{}
	lastSteps := session.Client.core.Steps()

	runner := &route.Runner{
		Digest: session.FrameDigest,
		SendKey: func(ctx context.Context, pressed bool, key int32) error {
			eventType := KeyPressed
			if !pressed {
				eventType = KeyReleased
			}
			return session.SendKey(ctx, eventType, key)
		},
		Stalled: func() bool {
			_, pending := session.NextDeadline()
			return !pending
		},
		Checkpoint: func(label string, tick int, reset bool) error {
			marks[label] = len(costs)
			// A profile of the whole replay is a profile of the boot that
			// preceded the scene. Starting it at the mark is what makes the
			// ranking the scene's own.
			if reset && cpuProfile != nil {
				if err := pprof.StartCPUProfile(cpuProfile); err != nil {
					t.Fatal(err)
				}
				profiling = true
			}
			return nil
		},
		Advance: func(ctx context.Context) (bool, error) {
			entered := time.Now()
			progressed, tickErr := session.Tick(ctx)
			spent := time.Since(entered)
			steps := session.Client.core.Steps()
			costs = append(costs, tickCost{cost: spent, steps: steps - lastSteps})
			if digests != nil {
				fmt.Fprintf(digests, "%d %016x %d\n", len(costs)-1, session.FrameDigest(), steps)
			}
			lastSteps = steps
			if tickErr != nil {
				return progressed, tickErr
			}
			session.SkipToNextDeadline()
			return progressed, nil
		},
	}
	started := time.Now()
	result, err := runner.Run(ctx, script)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Completed {
		t.Fatalf("route stopped at step %d: %s", result.StoppedAt+1, result.Reason)
	}
	if profiling {
		pprof.StopCPUProfile()
	}
	t.Logf("route replayed %d ticks in %v", len(costs), time.Since(started).Round(time.Millisecond))

	report := func(name string, from, to int) {
		if from < 0 || to > len(costs) || from >= to {
			return
		}
		window := costs[from:to]
		total := time.Duration(0)
		steps := uint64(0)
		sorted := make([]time.Duration, 0, len(window))
		for _, entry := range window {
			total += entry.cost
			steps += entry.steps
			sorted = append(sorted, entry.cost)
		}
		sort.Slice(sorted, func(a, b int) bool { return sorted[a] < sorted[b] })
		p := func(q float64) time.Duration {
			index := int(q * float64(len(sorted)-1))
			return sorted[index].Round(time.Microsecond)
		}
		nsPerStep := 0.0
		if steps > 0 {
			nsPerStep = float64(total.Nanoseconds()) / float64(steps)
		}
		t.Logf("%-14s ticks=%-5d wall=%-10v mean=%-9v p50=%-9v p90=%-9v p99=%-9v steps=%.1fM (%.2fM/tick) ns_per_step=%.3f",
			name, len(window), total.Round(time.Millisecond),
			(total / time.Duration(len(window))).Round(time.Microsecond),
			p(0.50), p(0.90), p(0.99),
			float64(steps)/1e6, float64(steps)/float64(len(window))/1e6, nsPerStep)
	}

	report("whole route", 0, len(costs))
	names := make([]string, 0, len(marks))
	for name := range marks {
		names = append(names, name)
	}
	sort.Slice(names, func(a, b int) bool { return marks[names[a]] < marks[names[b]] })
	for index, name := range names {
		end := len(costs)
		if index+1 < len(names) {
			end = marks[names[index+1]]
		}
		report(fmt.Sprintf("from %s", name), marks[name], end)
	}
	// The costliest ticks say where a burst is even when the route carries no
	// mark for it, which is how the stretch this probe was written for was
	// found: it sits between two of the route's marks and not on one.
	worst := make([]int, len(costs))
	for index := range worst {
		worst[index] = index
	}
	sort.Slice(worst, func(a, b int) bool { return costs[worst[a]].cost > costs[worst[b]].cost })
	head := worst[:min(20, len(worst))]
	sort.Ints(head)
	for _, index := range head {
		t.Logf("  costly tick %d: %v (%.2fM steps)", index, costs[index].cost.Round(time.Microsecond), float64(costs[index].steps)/1e6)
	}
}
