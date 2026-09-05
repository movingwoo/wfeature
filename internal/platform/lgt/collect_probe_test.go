package lgt

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

// What a collection reclaimed is a number, and a number is not evidence that a
// title still draws the same thing. So this probe runs every local archive
// twice out of one build — once with the collector off, once with it on — and
// compares the frame the title presented at every tick of both runs. A
// difference at tick n is a picture the collection changed, which is the only
// failure that matters here; the totals it prints are what says a collection
// happened at all.
//
//	WFEATURE_LGT_COLLECT_PROBE=1 go test -run TestLocalLGTCollectionKeepsTheFrame -v ./internal/platform/lgt
//
// WFEATURE_LGT_COLLECT_TICKS overrides how far each arm runs.
func TestLocalLGTCollectionKeepsTheFrame(t *testing.T) {
	if os.Getenv("WFEATURE_LGT_COLLECT_PROBE") != "1" {
		t.Skip("set WFEATURE_LGT_COLLECT_PROBE=1 to run ignored local LGT archives")
	}
	ticks := 400
	if value := os.Getenv("WFEATURE_LGT_COLLECT_TICKS"); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil || parsed <= 0 {
			t.Fatalf("WFEATURE_LGT_COLLECT_TICKS=%q is not a tick count", value)
		}
		ticks = parsed
	}
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate the probe source")
	}
	directory := filepath.Join(filepath.Dir(source), "..", "..", "..", "var", "games", "lgt")
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatalf("read local LGT game directory: %v", err)
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.EqualFold(filepath.Ext(entry.Name()), ".zip") {
			continue
		}
		name := entry.Name()
		t.Run(name, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join(directory, name))
			if err != nil {
				t.Fatalf("read archive: %v", err)
			}
			off := runCollectionArm(t, data, ticks, true)
			on := runCollectionArm(t, data, ticks, false)
			if on.java == 0 {
				t.Skip("not a Java title: the collector never runs")
			}
			t.Logf("ticks=%d collections=%d tracked=%d marked=%d condemned=%d freed=%d bytes=%d surfaces=%d files=%d",
				len(on.digests), on.collections, on.stats.Tracked, on.stats.Marked,
				on.stats.Condemned, on.stats.Freed, on.stats.Bytes, on.stats.Surfaces,
				on.stats.Files)
			t.Logf("arena %d -> %d bytes, surfaces %d -> %d bytes, collector cost %s",
				off.arena, on.arena, off.surfaces, on.surfaces, on.cost)
			if on.err != "" {
				t.Logf("both runs ended at tick %d: %s", len(on.digests), on.err)
			}
			if off.err != on.err {
				t.Errorf("the run ended differently: off=%q on=%q", off.err, on.err)
			}
			if len(off.digests) != len(on.digests) {
				t.Fatalf("the runs covered %d and %d ticks", len(off.digests), len(on.digests))
			}
			for tick := range off.digests {
				if off.digests[tick] != on.digests[tick] {
					t.Fatalf("the frame changed at tick %d: %#x without collection, %#x with it",
						tick, off.digests[tick], on.digests[tick])
				}
			}
		})
	}
}

type collectionArm struct {
	digests     []uint64
	err         string
	java        int
	collections int
	stats       CollectionStats
	arena       uint64
	surfaces    uint64
	cost        time.Duration
}

// runCollectionArm runs one archive to a tick count and answers the frame it
// presented at every tick. Input is not sent: the two arms have to see the same
// title, and a key delivered on a wall clock would not be the same tick in both.
func runCollectionArm(t *testing.T, data []byte, ticks int, collectorOff bool) collectionArm {
	t.Helper()
	ctx := context.Background()
	session, err := StartSession(ctx, data, SessionOptions{SaveRoot: t.TempDir()})
	if err != nil {
		if errors.Is(err, ErrJavaAppUnsupported) {
			t.Skip("LGT Java app this platform does not run")
		}
		t.Fatalf("start: %v", err)
	}
	defer session.Close(ctx)
	session.client.collectorOff = collectorOff

	arm := collectionArm{digests: make([]uint64, 0, ticks)}
	for tick := 0; tick < ticks; tick++ {
		if err := session.Tick(ctx); err != nil {
			arm.err = err.Error()
			break
		}
		arm.digests = append(arm.digests, session.FrameDigest())
	}
	if run := session.client.javaRun; run != nil {
		arm.java = len(run.objects) + run.collections
		arm.collections = run.collections
		arm.stats = run.collected
	}
	arm.arena = session.client.arena.used()
	arm.surfaces = session.client.surfaces.used()
	arm.cost = time.Duration(session.client.collectNanos)
	return arm
}
