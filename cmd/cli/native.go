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
)

// The carrier shipped two generations of download package, and `runktf` takes
// both. The earlier one has no descriptor, no JAR and no lifecycle to drive:
// the title hands the platform an interval and a function during its start-up
// and everything after that happens inside that function. So this loop is
// "run the frame when it is due" where the other is "run a service round",
// and the options that only mean something to the other are refused by name
// rather than ignored. See docs/ktf.md, "An earlier KTF package".
type nativeRun struct {
	ticks        int
	framePath    string
	frameDir     string
	play         bool
	saveRoot     string
	keyEvents    map[int][]int32
	keyHold      int
	audioPrefix  string
	cheatConsole bool
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
		fmt.Fprintln(stdout, "cheat: attached. `help` for commands, ctrl-c to quit.")
	}
	ran := 0
	dumpedFlush := uint32(0)
	keyReleases := map[int][]int32{}
	var tickError error
	for ; ran < run.ticks; ran++ {
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
	encoder := json.NewEncoder(stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(summary); err != nil {
		fmt.Fprintf(stderr, "write result: %v\n", err)
		return 1
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
