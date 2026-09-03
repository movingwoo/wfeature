package skt_test

import (
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/platform/skt"
)

// TestLocalSKTArchiveCost runs one ignored local archive for a while so a Go
// profiler has something to look at. It is opt-in twice over: real games are
// local data rather than fixtures, and a profiling run is not a check.
//
//	WFEATURE_SKT_COST_ARCHIVE=var/games/test_skt/<title>.zip \
//	  go test -run TestLocalSKTArchiveCost -memprofile alloc.out ./internal/platform/skt
//
// **A tick here is paced the way the Host paces one**, because a MIDlet has no
// tick of its own and its threads sleep against the wall: run the loop flat out
// and the guest threads get a different share of the machine than they ever do
// in a session, which is a different program to profile. WFEATURE_SKT_COST_FLAT
// takes the pacing away for a run that only wants sample density.
//
// Read the result against "Do not read a macOS Go CPU profile" in
// docs/testing.md — the allocation profile is the cheaper clue here.
func TestLocalSKTArchiveCost(t *testing.T) {
	path := os.Getenv("WFEATURE_SKT_COST_ARCHIVE")
	if path == "" {
		t.Skip("set WFEATURE_SKT_COST_ARCHIVE to an archive to profile")
	}
	ticks := 300
	if value, err := strconv.Atoi(os.Getenv("WFEATURE_SKT_COST_TICKS")); err == nil && value > 0 {
		ticks = value
	}
	paced := os.Getenv("WFEATURE_SKT_COST_FLAT") != "1"
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read archive: %v", err)
	}
	archive, err := skt.Open(data)
	if err != nil {
		t.Fatalf("open archive: %v", err)
	}
	framebuffer, err := backend.NewMemoryFramebuffer(240, 320)
	if err != nil {
		t.Fatalf("framebuffer: %v", err)
	}
	session, err := skt.Start(archive, skt.Options{
		Framebuffer: framebuffer,
		SaveStore:   backend.NewDirectorySaveStore(t.TempDir()),
	})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	defer session.Destroy(true)

	started := time.Now()
	ran := 0
	for ; ran < ticks; ran++ {
		state := session.State()
		if state == skt.StateDestroyed || state == skt.StateError {
			break
		}
		session.AdvanceAudio()
		if err := session.RunPending(); err != nil {
			t.Fatalf("tick %d: %v", ran, err)
		}
		if paced {
			time.Sleep(16 * time.Millisecond)
		}
	}
	elapsed := time.Since(started)
	t.Logf("%d ticks in %s (%.2fms a tick), state %v",
		ran, elapsed.Round(time.Millisecond), float64(elapsed.Microseconds())/1000/float64(max(ran, 1)), session.State())
}
