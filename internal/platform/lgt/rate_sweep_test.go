package lgt

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"
)

// TestRateSweep is a measurement harness, not a check: it reports, per local
// archive, the frame period the title asks for against the one the work clock
// lets it have. Run it with WFEATURE_GUEST_MIPS very high and the answer is
// the schedule the title wants, because computation then costs no guest time.
//
//	WFEATURE_LGT_CORPUS=var/games/lgt WFEATURE_GUEST_MIPS=50000 \
//	  go test -v -count=1 -run RateSweep ./internal/platform/lgt -timeout 60m
func TestRateSweep(t *testing.T) {
	corpus := os.Getenv("WFEATURE_LGT_CORPUS")
	if corpus == "" {
		t.Skip("set WFEATURE_LGT_CORPUS to a directory of local archives")
	}
	if value := os.Getenv("WFEATURE_GUEST_MIPS"); value != "" {
		parsed, err := strconv.ParseUint(value, 10, 64)
		if err != nil || parsed == 0 {
			t.Fatalf("invalid WFEATURE_GUEST_MIPS %q", value)
		}
		restore := guestInstructionsPerMillisecond
		guestInstructionsPerMillisecond = parsed
		defer func() { guestInstructionsPerMillisecond = restore }()
	}
	ticks := 3000
	if value := os.Getenv("WFEATURE_SWEEP_TICKS"); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil || parsed <= 0 {
			t.Fatalf("invalid WFEATURE_SWEEP_TICKS %q", value)
		}
		ticks = parsed
	}
	entries, err := os.ReadDir(corpus)
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".zip") {
			names = append(names, entry.Name())
		}
	}
	sort.Strings(names)
	t.Logf("rate=%d inst/ms  ticks=%d  archives=%d", guestInstructionsPerMillisecond, ticks, len(names))
	for _, name := range names {
		data, err := os.ReadFile(filepath.Join(corpus, name))
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		ctx := context.Background()
		session, err := StartSession(ctx, data, SessionOptions{SaveRoot: t.TempDir()})
		if err != nil {
			t.Logf("%-44s start failed: %v", name, err)
			continue
		}
		gaps := make([]time.Duration, 0, 512)
		lastPaint := time.Duration(0)
		lastFlushes := uint64(0)
		host := time.Now()
		var runErr error
		for step := 0; step < ticks; step++ {
			if err := session.Tick(ctx); err != nil {
				runErr = err
				break
			}
			if f := uint64(session.Flushes()); f != lastFlushes {
				lastFlushes = f
				now := session.GuestElapsed()
				if lastPaint > 0 {
					gaps = append(gaps, now-lastPaint)
				}
				lastPaint = now
			}
		}
		elapsed := time.Since(host)
		guest := session.GuestElapsed()
		session.Close(ctx)
		note := ""
		if runErr != nil {
			note = " err=" + runErr.Error()
			if len(note) > 60 {
				note = note[:60]
			}
		}
		if len(gaps) < 8 {
			t.Logf("%-44s paints=%d (too few to read)%s", name, len(gaps)+1, note)
			continue
		}
		// The last three quarters of the run: the first paints are the boot
		// screen and a load, which is not the rate the title settles at.
		settled := gaps[len(gaps)/4:]
		sorted := slices.Clone(settled)
		slices.Sort(sorted)
		at := func(f float64) time.Duration { return sorted[int(float64(len(sorted)-1)*f)] }
		mode, modeCount := time.Duration(0), 0
		counts := map[time.Duration]int{}
		for _, gap := range settled {
			r := gap.Round(time.Millisecond)
			counts[r]++
			if counts[r] > modeCount {
				mode, modeCount = r, counts[r]
			}
		}
		fmt.Printf("%-44s paints=%3d mode=%-7v (%2d%%) p50=%-7v p90=%-7v guest=%-8v host=%-8v ratio=%.2f%s\n",
			name, len(gaps)+1, mode, modeCount*100/len(settled),
			at(0.50).Round(time.Millisecond), at(0.90).Round(time.Millisecond),
			guest.Round(time.Millisecond), elapsed.Round(time.Millisecond),
			guest.Seconds()/max(elapsed.Seconds(), 1e-9), note)
		_ = fmt.Sprint()
	}
}
