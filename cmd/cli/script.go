package main

import (
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"time"

	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/platform/skt"
	"github.com/movingwoo/wfeature/internal/route"
	"github.com/movingwoo/wfeature/internal/serve"
)

type scriptCLIRun struct {
	ticks, width, height, hold             int
	frame, frameDir, saveRoot, diag, audio string
	keys                                   map[int][]int32
	route                                  *route.Route
	serve                                  bool
}

// runScript drives the same SGS core as the browser host, using a manual
// clock so diagnostic ticks are reproducible without wall-clock sleeps.
func runScript(archive *skt.Archive, run scriptCLIRun, stdout, stderr io.Writer) int {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	if run.width == 0 {
		run.width, run.height = archive.Script.Width, archive.Script.Height
	}
	fb, err := backend.NewMemoryFramebuffer(run.width, run.height)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if run.saveRoot == "" {
		run.saveRoot = filepath.Join(platformSaveRoot("skt"), archive.ScriptSaveOwner)
	}
	var elapsed time.Duration
	var recording *backend.RecordingSink
	var sink backend.AudioSink
	if run.audio != "" {
		recording = backend.NewRecordingSink(func() time.Duration { return elapsed })
		sink = recording
	}
	core, err := skt.StartScript(ctx, archive, skt.ScriptOptions{Framebuffer: fb, SaveStore: backend.NewDirectorySaveStore(run.saveRoot), AudioSink: sink, Logger: backend.NewLogger(stderr)})
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	defer core.Close()
	ticks := 0
	frame := func() ([]byte, int, int) { f, _ := fb.Snapshot(); return f.RGBA, f.Width, f.Height }
	digest := func() uint64 { f, _, _ := frame(); h := fnv.New64a(); _, _ = h.Write(f); return h.Sum64() }
	send := func(ctx context.Context, pressed bool, key int32) error {
		action := "release"
		if pressed {
			action = "press"
		}
		return core.SendKey(ctx, action, key)
	}
	advance := func(ctx context.Context) (bool, error) {
		for _, key := range run.keys[ticks] {
			if err := send(ctx, true, key); err != nil {
				return false, err
			}
		}
		_, err := core.Advance(ctx, 16*time.Millisecond)
		ticks++
		elapsed = core.GuestElapsed()
		if run.frameDir != "" {
			rgba, w, h := frame()
			if writeErr := writePNG(filepath.Join(run.frameDir, fmt.Sprintf("%06d.png", ticks)), rgba, w, h); writeErr != nil {
				return false, writeErr
			}
		}
		return !core.Exited(), err
	}
	shot := func(path string) error { rgba, w, h := frame(); return shootFrame(path, rgba, w, h) }
	summary := func() map[string]any {
		_, flushes := fb.Snapshot()
		return map[string]any{"runtime": "sgs", "name": archive.Script.Name, "ticks": ticks, "flushes": flushes, "exited": core.Exited()}
	}
	diag := func(path string) error {
		data, err := json.MarshalIndent(summary(), "", "  ")
		if err != nil {
			return err
		}
		return os.WriteFile(path, append(data, '\n'), 0o644)
	}
	runner := &route.Runner{Advance: advance, Digest: digest, SendKey: send, Stalled: core.Exited, MaxTicks: run.ticks, Checkpoint: func(label string, _ int, _ bool) error {
		if run.frameDir == "" {
			return nil
		}
		return shot(filepath.Join(run.frameDir, label+".png"))
	}}
	if run.serve {
		driver := &serve.Driver{Advance: advance, Frame: frame, Digest: digest, Flushes: func() uint64 { _, n := fb.Snapshot(); return n }, LookupKey: skt.KeyCodeByName, SendKey: send, Stalled: core.Exited, DefaultHold: run.hold, Shot: shot, Diag: diag, RunRoute: runner.Run, Park: func(ctx context.Context, hold time.Duration) error {
			return parkFor(ctx, hold, func(context.Context) error { core.Pause(); return nil }, func(context.Context) error { core.Resume(); return nil })
		}}
		err = serve.Serve(ctx, driver, os.Stdin, stdout)
	} else if run.route != nil {
		_, err = runner.Run(ctx, run.route)
	} else {
		for ticks < run.ticks && !core.Exited() {
			if _, err = advance(ctx); err != nil {
				break
			}
		}
	}
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if run.frame != "" {
		if err := shot(run.frame); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
	}
	if run.diag != "" {
		if err := diag(run.diag); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
	}
	if recording != nil {
		if _, err := recording.Write(run.audio); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
	}
	if !run.serve {
		if err := json.NewEncoder(stdout).Encode(summary()); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
	}
	return 0
}
