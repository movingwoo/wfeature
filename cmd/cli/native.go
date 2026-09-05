package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/platform/ktf"
	"github.com/movingwoo/wfeature/internal/route"
	"github.com/movingwoo/wfeature/internal/serve"
)

// The carrier shipped two generations of download package, and `runktf` takes
// both. The earlier one has no descriptor, no JAR and no lifecycle to drive:
// the title hands the platform an interval and a function during its start-up
// and everything after that happens inside that function. So this loop is
// "run the frame when it is due" where the other is "run a service round",
// and the options that only mean something to the other are refused by name
// rather than ignored. See docs/ktf.md, "An earlier KTF package".
type nativeRun struct {
	// archivePath is what the package was read from, which is the second half
	// of a cheat table's key beside the hash of the module itself.
	archivePath string
	ticks       int
	// ticksChosen says the count came from -ticks rather than from the probe
	// default, which is what decides whether a cheat session may run past it.
	ticksChosen  bool
	framePath    string
	frameDir     string
	play         bool
	saveRoot     string
	keyEvents    map[int][]int32
	keyHold      int
	audioPrefix  string
	cheatConsole bool
	serveSession bool
	script       *route.Route
	screenWidth  int
	screenHeight int
	logger       *slog.Logger
}

func runKTFNative(ctx context.Context, data []byte, run nativeRun, stdout, stderr io.Writer) int {
	options := ktf.NativeSessionOptions{
		Logger: run.logger,
		Width:  run.screenWidth,
		Height: run.screenHeight,
	}
	if run.saveRoot != "" {
		owner, err := nativeSaveOwner(data)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		options.SaveStore = ktf.NewDirectorySaveStore(filepath.Join(run.saveRoot, owner))
	}
	// The recording sink timestamps with guest time, which the session only
	// answers once it exists, so the clock is attached just after the start.
	var audioSink *backend.RecordingSink
	if run.audioPrefix != "" {
		audioSink = backend.NewRecordingSink(nil)
		options.AudioSink = audioSink
	}
	// A game paces itself with the interval it asked for, and what that should
	// cost depends on who is watching. -play shows the game to a person, so it
	// runs on the wall clock; a probe is measuring what the guest computes, so
	// it runs a manual clock it jumps to each next frame.
	var probeClock *ktf.ManualClock
	if !run.play {
		probeClock = ktf.NewManualClock(time.Time{})
		options.Clock = probeClock
	}
	session, err := ktf.StartNativeSession(ctx, data, options)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	defer session.Close()
	if audioSink != nil {
		audioSink.Clock = session.GuestElapsed
	}

	var cheatCommands chan string
	if run.cheatConsole {
		// Reading stdin on its own goroutine keeps the frame loop paced; the
		// commands run between frames so a scan observes the guest at a frame
		// boundary rather than mid-instruction.
		cheatCommands = make(chan string, 16)
		go func() {
			lines := bufio.NewScanner(os.Stdin)
			for lines.Scan() {
				cheatCommands <- lines.Text()
			}
			close(cheatCommands)
		}()
		// A cheat session runs until it is interrupted, unless the caller asked
		// for a specific number of ticks. Without this the console attaches to
		// a run that is already 64 frames from ending, and the invitation to
		// type a command is answered by the summary.
		if !run.ticksChosen {
			run.ticks = 1 << 30
		}
		keyCheatTable(session.CheatConsole(), run.archivePath)
		fmt.Fprintln(stdout, "cheat: attached. `help` for commands, ctrl-c to quit.")
	}
	ran := 0
	dumpedFlush := uint32(0)
	keyReleases := map[int][]int32{}
	var tickError error
	var routeResult route.Result
	if run.serveSession {
		// The earlier package has no pointer, no lifecycle to park and no
		// diagnostics, which is the same list `runktf` already refuses for it
		// by name. Those three commands answer that they do not apply here.
		driver := &serve.Driver{
			Advance: func(ctx context.Context) (bool, error) {
				progressed, err := session.Tick(ctx)
				if err != nil {
					return progressed, err
				}
				ran++
				nativePace(ctx, session, probeClock)
				return progressed, nil
			},
			Frame: func() ([]byte, int, int) {
				frame, width, height, _ := session.Frame()
				return frame, width, height
			},
			Digest:    session.FrameDigest,
			Flushes:   func() uint64 { return uint64(session.Flushes()) },
			LookupKey: ktf.KeyCodeByName,
			SendKey: func(ctx context.Context, pressed bool, key int32) error {
				eventType := ktf.KeyPressed
				if !pressed {
					eventType = ktf.KeyReleased
				}
				return session.SendKey(ctx, eventType, key)
			},
			Stalled: func() bool {
				_, pending := session.NextDeadline()
				return !pending
			},
			Shot: func(path string) error {
				frame, width, height, _ := session.Frame()
				return shootFrame(path, frame, width, height)
			},
			RunRoute: func(ctx context.Context, script *route.Route) (route.Result, error) {
				return runNativeRoute(ctx, session, script, nativeRouteRun{
					frameDir: run.frameDir, hold: run.keyHold, probeClock: probeClock, stderr: stderr,
				})
			},
			DefaultHold: run.keyHold,
		}
		if err := serve.Serve(ctx, driver, os.Stdin, stdout); err != nil {
			fmt.Fprintf(stderr, "serve: %v\n", err)
			return 1
		}
	}
	if run.script != nil {
		// A route runs until it arrives, so it does not inherit the tick count
		// that bounds an unscripted probe; an explicit -ticks still caps it.
		budget := run.ticks
		if !run.ticksChosen {
			budget = 0
		}
		routeResult, tickError = runNativeRoute(ctx, session, run.script, nativeRouteRun{
			frameDir:   run.frameDir,
			hold:       run.keyHold,
			probeClock: probeClock,
			maxTicks:   budget,
			stderr:     stderr,
		})
		ran = routeResult.Ticks
		if tickError != nil && !errors.Is(tickError, context.Canceled) {
			fmt.Fprintf(stderr, "route: %v\n", tickError)
		}
	}
	for ; !run.serveSession && run.script == nil && ran < run.ticks; ran++ {
		if ctx.Err() != nil {
			break
		}
		flushes := session.Flushes()
		if flushes > 0 && run.frameDir != "" && flushes != dumpedFlush {
			frame, width, height, _ := session.Frame()
			if width > 0 && height > 0 {
				dumpedFlush = flushes
				name := filepath.Join(run.frameDir, fmt.Sprintf("tick%04d.png", ran))
				if err := writePNG(name, frame, width, height); err != nil {
					fmt.Fprintf(stderr, "write frame: %v\n", err)
					return 1
				}
			}
		}
		// A scripted key is pressed on its tick and released -hold ticks
		// later, for the same reason it is on the other package: a title that
		// samples the keypad once a frame never sees a press and a release
		// delivered in the same tick.
		for _, key := range run.keyEvents[ran] {
			if err := session.SendKey(ctx, ktf.KeyPressed, key); err != nil {
				tickError = err
				break
			}
			keyReleases[ran+run.keyHold] = append(keyReleases[ran+run.keyHold], key)
		}
		for _, key := range keyReleases[ran] {
			if err := session.SendKey(ctx, ktf.KeyReleased, key); err != nil {
				tickError = err
				break
			}
		}
		delete(keyReleases, ran)
		if tickError != nil {
			break
		}
		if cheatCommands != nil {
			for done := false; !done; {
				select {
				case line, ok := <-cheatCommands:
					if !ok {
						cheatCommands = nil
						done = true
						break
					}
					if output := session.CheatConsole().Execute(line); output != "" {
						fmt.Fprintln(stdout, output)
					}
				default:
					done = true
				}
			}
		}
		if _, err := session.Tick(ctx); err != nil {
			tickError = err
			break
		}
		nativePace(ctx, session, probeClock)
	}

	frame, width, height, flushes := session.Frame()
	lit := frameLit(frame)
	if run.framePath != "" && width > 0 && height > 0 {
		if err := writePNG(run.framePath, frame, width, height); err != nil {
			fmt.Fprintf(stderr, "write frame: %v\n", err)
			return 1
		}
	}
	summary := map[string]any{
		"package":    "native",
		"aid":        session.SaveOwner(),
		"name":       session.Name(),
		"ticks":      ran,
		"flushes":    flushes,
		"width":      width,
		"height":     height,
		"lit_pixels": lit,
	}
	if run.script != nil {
		summary["route_completed"] = routeResult.Completed
		marks := make([]map[string]any, 0, len(routeResult.Marks))
		for _, mark := range routeResult.Marks {
			marks = append(marks, map[string]any{"label": mark.Label, "tick": mark.Tick})
		}
		summary["route_marks"] = marks
		if !routeResult.Completed {
			summary["route_stopped_at"] = routeResult.StoppedAt + 1
			summary["route_reason"] = routeResult.Reason
		}
	}
	if platform := session.Platform(); platform != nil {
		summary["draws"] = platform.Draws()
		summary["images"] = platform.Images()
		if failures := platform.StoreFailures(); failures > 0 {
			summary["save_store_failures"] = failures
		}
	}
	if audioSink != nil {
		messages, samples := audioSink.Summary()
		summary["audio_midi_messages"] = messages
		summary["audio_wave_samples"] = samples
		written, err := audioSink.Write(run.audioPrefix)
		if err != nil {
			fmt.Fprintf(stderr, "write audio: %v\n", err)
			return 1
		}
		summary["audio"] = written
	}
	if platform := session.Platform(); platform != nil && platform.ClipRefusals() > 0 {
		summary["clips_refused"] = platform.ClipRefusals()
	}
	if tickError != nil && !errors.Is(tickError, context.Canceled) {
		summary["tick_error"] = tickError.Error()
	}
	// A serve session owns stdout: one line on it is one answer to one
	// command, and a summary after the last of them is a line nothing asked
	// for.
	if !run.serveSession {
		encoder := json.NewEncoder(stdout)
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(summary); err != nil {
			fmt.Fprintf(stderr, "write result: %v\n", err)
			return 1
		}
	}
	return 0
}

// nativeSaveOwner reads the package far enough to name the directory its saves
// belong in, which is what the store is rooted at before the title runs.
func nativeSaveOwner(data []byte) (string, error) {
	archive, err := ktf.OpenNative(data)
	if err != nil {
		return "", err
	}
	owner := ktf.NativeSaveOwner(archive.Info)
	if owner == "" {
		return "", fmt.Errorf("KTF native package names no application, so its saves have no owner")
	}
	return owner, nil
}

// nativePace waits out what is left of the interval the title asked for. A
// probe jumps its clock there instead, which costs no real time.
func nativePace(ctx context.Context, session *ktf.NativeSession, probeClock *ktf.ManualClock) {
	if probeClock != nil {
		session.SkipToNextDeadline()
		return
	}
	deadline, pending := session.NextDeadline()
	if !pending {
		return
	}
	wait := min(time.Until(deadline), idlePollCeiling)
	if wait <= 0 {
		return
	}
	select {
	case <-ctx.Done():
	case <-time.After(wait):
	}
}

// nativeRouteRun is what replaying a script against the earlier package needs
// beyond the script itself.
type nativeRouteRun struct {
	frameDir string
	// hold is how many ticks a scripted press is held before its release, for
	// the reason it is on the other package: a title that samples the keypad
	// once a frame never sees a press and a release delivered together.
	hold       int
	probeClock *ktf.ManualClock
	maxTicks   int
	stderr     io.Writer
}

// runNativeRoute replays a script against a started session of the earlier
// package. The runner is the one the descriptor package uses and the script is
// the same file: a route is written against what is on the screen and which
// keys are pressed, and neither of those is a property of how the title was
// packaged. What differs is only how a tick is spent and where the frame comes
// from, which is what these four functions carry.
//
// Ticking stays here rather than in the runner so a route replays at whatever
// speed the Host is already running at — a probe jumps its clock, a -play run
// waits the interval out.
func runNativeRoute(ctx context.Context, session *ktf.NativeSession, script *route.Route, options nativeRouteRun) (route.Result, error) {
	var runError error
	runner := &route.Runner{
		MaxTicks: options.maxTicks,
		Hold:     options.hold,
		Digest:   session.FrameDigest,
		SendKey: func(ctx context.Context, pressed bool, key int32) error {
			eventType := ktf.KeyPressed
			if !pressed {
				eventType = ktf.KeyReleased
			}
			return session.SendKey(ctx, eventType, key)
		},
		Stalled: func() bool {
			// The title has no loop of its own: everything it does happens
			// inside the frame it asked to be called back on. With no deadline
			// pending there is nothing left to run it.
			_, pending := session.NextDeadline()
			return !pending
		},
		Advance: func(ctx context.Context) (bool, error) {
			progressed, err := session.Tick(ctx)
			if err != nil {
				return progressed, err
			}
			nativePace(ctx, session, options.probeClock)
			return progressed, nil
		},
		Checkpoint: func(label string, tick int, reset bool) error {
			// There is no profiler on this package, so a mark is a place on
			// the way rather than somewhere to start measuring from.
			if options.frameDir == "" {
				return nil
			}
			frame, width, height, _ := session.Frame()
			if width <= 0 || height <= 0 {
				return nil
			}
			return writePNG(filepath.Join(options.frameDir, fmt.Sprintf("%s.png", label)), frame, width, height)
		},
	}
	result, err := runner.Run(ctx, script)
	if err != nil {
		runError = err
	}
	if !result.Completed && runError == nil {
		fmt.Fprintf(options.stderr, "route stopped at step %d (%s)\n", result.StoppedAt+1, result.Reason)
	}
	return result, runError
}
