package webhost

import (
	"context"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/platform/ktf"
	"github.com/movingwoo/wfeature/internal/session"
)

// TestSessionCostProbe runs a real archive the way a page's session does — the
// shared session, then the picture encoder for every new frame and the sound
// encoding for every batch — and reports what that cost the host. The
// platform probes stop at the session, so a profile of them never sees the
// encoder, and SKT has no probe of its own; this one exists to be run under
// -cpuprofile for either reason:
//
//	WFEATURE_PERF_ARCHIVE=var/games/skt/game.zip \
//	  go test ./internal/webhost -run SessionCostProbe -cpuprofile cpu.out
//
// WFEATURE_LOAD_TICKS sets the length. WFEATURE_PERF_KEYS=mash presses a fixed
// cycle of keys, one every 24 ticks, so a title that waits on its menus gets
// past them and into the work a player sees.
func TestSessionCostProbe(t *testing.T) {
	path := os.Getenv("WFEATURE_PERF_ARCHIVE")
	if path == "" {
		t.Skip("set WFEATURE_PERF_ARCHIVE to a local game archive")
	}
	ticks := 3000
	if value := os.Getenv("WFEATURE_LOAD_TICKS"); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil || parsed <= 0 {
			t.Fatalf("invalid WFEATURE_LOAD_TICKS %q", value)
		}
		ticks = parsed
	}
	archive, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	summary, err := session.Inspect(archive)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	collector := &audioCollector{pcmChannels: true}
	intermediate := make(chan pendingFrame, 1)
	options := session.Options{
		SaveStore:    backend.NewDirectorySaveStore(t.TempDir()),
		AudioSink:    collector,
		FrameUpdates: intermediate,
	}
	// As the CLI runs it: a KTF title's waits are skipped on a manual clock,
	// and an LGT title's clock is moved by its ticks.
	if summary.Platform == "ktf" {
		options.Clock = ktf.NewManualClock(time.Time{})
	}
	opened := time.Now()
	game, err := session.Start(ctx, archive, options)
	if err != nil {
		t.Fatal(err)
	}
	defer game.Close()
	startup := time.Since(opened)

	encoder := newPictureEncoder(protocolStream, true)
	var definitions audioDefinitions
	var frames, pictureBytes, soundBytes int
	encode := func(frame pendingFrame) {
		encoded, err := encoder.encode(frame)
		if err != nil {
			t.Fatal(err)
		}
		if encoded != nil {
			frames++
			pictureBytes += len(encoded)
			encoder.accept()
		}
	}
	mash := os.Getenv("WFEATURE_PERF_KEYS") == "mash"
	keys := []int32{148, 141, 148, 145, '5', 146, 148, 142}
	presented := game.Flushes()
	start := time.Now()
	ran := 0
	for ; ran < ticks; ran++ {
		if mash && ran%24 == 0 {
			_ = game.SendKey(ctx, session.KeyPress, keys[ran/24%len(keys)])
		} else if mash && ran%24 == 4 {
			_ = game.SendKey(ctx, session.KeyRelease, keys[ran/24%len(keys)])
		}
		progress, err := game.Tick(ctx, tickBudget)
		if progress.Exited {
			t.Logf("the title ended itself at tick %d", ran)
			break
		}
		if err != nil {
			t.Fatalf("tick %d: %v", ran, err)
		}
		for drained := false; !drained; {
			select {
			case frame := <-intermediate:
				encode(frame)
			default:
				drained = true
			}
		}
		if progress.Flushes != presented {
			presented = progress.Flushes
			if frame, ok := game.FrameUpdate(); ok {
				encode(frame)
			}
		}
		if events, _ := collector.takeBatch(); len(events) != 0 {
			soundBytes += len(definitions.encode(events))
		}
		if !game.SkipToNextDeadline() && !game.VirtualClock() && progress.Wait > 0 {
			time.Sleep(min(progress.Wait, 50*time.Millisecond))
		}
	}
	elapsed := time.Since(start)
	t.Logf("%s: startup %v, %d ticks in %v, %d pictures (%d bytes), %d sound bytes",
		summary.Platform, startup.Round(time.Millisecond), ran, elapsed.Round(time.Millisecond), frames, pictureBytes, soundBytes)
}
