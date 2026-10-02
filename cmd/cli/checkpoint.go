package main

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/platform/ktf"
	"github.com/movingwoo/wfeature/internal/serve"
	"github.com/movingwoo/wfeature/internal/session"
)

// runShared drives the same session and checkpoint API as the browser Host.
// Platform-specific diagnostic commands remain available as runktf/runlgt/runskt.
func runShared(ctx context.Context, path string, args []string, in io.Reader, out, diagnostic io.Writer) int {
	flags := flag.NewFlagSet("run", flag.ContinueOnError)
	flags.SetOutput(diagnostic)
	root := flags.String("save", "", "platform save directory; defaults to var/savedata/<profile>/<platform>")
	ticks := flags.Int("ticks", defaultProbeTicks, "number of ticks, including zero")
	pipe := flags.Bool("serve", false, "read JSON commands from standard input")
	load := flags.Bool("quickload", false, "restore the archive's checkpoint without guest startup")
	save := flags.Bool("quicksave", false, "save a checkpoint after the final tick")
	frame := flags.String("frame", "", "write the final screen to a PNG file")
	play := flags.Bool("play", false, "pace the game against wall time instead of running each tick as soon as the last ends")
	speed := flags.Float64("speed", 1, "guest speed multiplier")
	screen := flags.String("screen", "", "handset size, WxH")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if flags.NArg() != 0 || *ticks < 0 || *ticks > 1_000_000 || math.IsNaN(*speed) || math.IsInf(*speed, 0) || *speed < backend.SpeedFloor || *speed > backend.SpeedCeiling {
		fmt.Fprintln(diagnostic, "run expects valid options, 0..1000000 ticks and speed 0.1..16")
		return 2
	}
	ticksChosen := false
	flags.Visit(func(f *flag.Flag) { ticksChosen = ticksChosen || f.Name == "ticks" })
	if *pipe && (ticksChosen || *save) {
		fmt.Fprintln(diagnostic, "-serve takes step and quicksave commands instead of -ticks or -quicksave")
		return 2
	}
	options := session.Options{Speed: *speed, Logger: backend.NewLogger(diagnostic)}
	if *screen != "" {
		var err error
		options.Width, options.Height, err = parseScreenSize(*screen)
		if err != nil {
			fmt.Fprintln(diagnostic, err)
			return 2
		}
	}
	archive, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintln(diagnostic, err)
		return 1
	}
	summary, err := session.Inspect(archive)
	if err != nil {
		fmt.Fprintln(diagnostic, err)
		return 1
	}
	if *root == "" {
		*root = platformSaveRoot(summary.Platform)
	}
	directory, release, err := claimArchiveSaves(archive, *root)
	if err != nil {
		fmt.Fprintln(diagnostic, err)
		return 1
	}
	defer release()
	var store *backend.DirectorySaveStore
	if directory != "" {
		store = backend.NewDirectorySaveStore(directory)
		options.SaveStore = store
	}
	if backend.DebugBuild() {
		options.TraceLimit = ktf.DefaultTraceLimit
	}
	if summary.Platform == "ktf" && !*play {
		options.Clock = ktf.NewManualClock(time.Time{})
	}
	identity := backend.SaveIdentity(archive)
	readSlot := func() ([]byte, error) {
		if store == nil {
			return nil, fmt.Errorf("this archive has no persistent save directory")
		}
		data, exists, err := store.LoadCheckpoint(identity)
		if err == nil && !exists {
			err = fmt.Errorf("no checkpoint is saved for this archive")
		}
		return data, err
	}
	var game *session.Session
	if *load {
		var data []byte
		data, err = readSlot()
		if err == nil {
			game, err = session.RestoreCheckpoint(ctx, archive, data, options)
		}
	} else {
		game, err = session.Start(ctx, archive, options)
	}
	if err != nil {
		fmt.Fprintln(diagnostic, err)
		if errors.Is(err, session.ErrExited) {
			return 0
		}
		return 1
	}
	defer game.Close()
	driver := sharedServeDriver(game, *play)
	driver.QuickSave = func(ctx context.Context) error {
		if store == nil {
			return fmt.Errorf("this archive has no persistent save directory")
		}
		data, err := game.CaptureCheckpoint(ctx)
		if err != nil {
			return err
		}
		return store.StoreCheckpoint(identity, data)
	}
	driver.QuickLoad = func(ctx context.Context) error {
		current, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		data, err := readSlot()
		if err != nil {
			return err
		}
		return game.LoadCheckpoint(ctx, current, data)
	}
	if *pipe {
		err = serve.Serve(ctx, driver, in, out)
	} else {
		count, ended := 0, false
		for count < *ticks && ctx.Err() == nil {
			_, err = driver.Advance(ctx)
			count++
			if errors.Is(err, session.ErrExited) {
				ended, err = true, nil
				break
			}
			if err != nil {
				break
			}
		}
		if err == nil && *save {
			err = driver.QuickSave(ctx)
		}
		if err == nil {
			err = json.NewEncoder(out).Encode(map[string]any{"platform": summary.Platform, "ticks": count, "exited": ended,
				"restored": *load, "quick_saved": *save, "digest": fmt.Sprintf("%016x", driver.Digest())})
		}
	}
	if err == nil && *frame != "" {
		err = driver.Shot(*frame)
	}
	if err != nil {
		fmt.Fprintln(diagnostic, err)
		return 1
	}
	return 0
}

func claimArchiveSaves(archive []byte, root string) (string, func(), error) {
	summary, err := session.Inspect(archive)
	if err != nil {
		return "", nil, err
	}
	if root == "" || summary.SaveOwner == "" {
		return "", func() {}, nil
	}
	owner := summary.SaveOwner
	if !filepath.IsLocal(owner) || filepath.Base(owner) != owner || strings.ContainsAny(owner, `/\`) || owner == "." || owner == ".wfeature-quicksave" {
		return "", nil, fmt.Errorf("archive has an invalid save owner")
	}
	directory := filepath.Join(root, owner)
	release, err := backend.ClaimSaveDirectory(directory)
	return directory, release, err
}

func sharedServeDriver(game *session.Session, play bool) *serve.Driver {
	frame := func() ([]byte, int, int) {
		pixels, width, height, _ := game.Frame()
		return pixels, width, height
	}
	return &serve.Driver{
		Advance: func(ctx context.Context) (bool, error) {
			if game.Paused() {
				return false, nil
			}
			progress, err := game.Tick(ctx, 0)
			if progress.Exited {
				return progress.Progressed, session.ErrExited
			}
			if err != nil {
				return progress.Progressed, err
			}
			// Without -play nothing is watching, so the wait between ticks is
			// skipped where skipping it changes nothing the guest can see: a
			// manual clock is moved to the next deadline, and a clock that only
			// a tick moves has already been moved by the tick.
			if !play && (game.SkipToNextDeadline() || game.VirtualClock()) {
				return progress.Progressed, nil
			}
			if progress.Wait > 0 {
				select {
				case <-time.After(min(progress.Wait, idlePollCeiling)):
				case <-ctx.Done():
					return progress.Progressed, ctx.Err()
				}
			}
			return progress.Progressed, nil
		},
		Frame: frame, Flushes: game.Flushes,
		Stalled: game.Paused,
		Digest: func() uint64 {
			pixels, _, _ := frame()
			digest := sha256.Sum256(pixels)
			return binary.LittleEndian.Uint64(digest[:8])
		},
		LookupKey: sharedKeyCode,
		SendKey: func(ctx context.Context, pressed bool, code int32) error {
			action := session.KeyRelease
			if pressed {
				action = session.KeyPress
			}
			return game.SendKey(ctx, action, code)
		},
		SendTouch: func(ctx context.Context, action string, x, y int) error {
			return game.SendPointer(ctx, action, int32(x), int32(y))
		},
		Park: func(ctx context.Context, hold time.Duration) error {
			return parkFor(ctx, hold, game.Pause, game.Resume)
		},
		Shot: func(path string) error {
			pixels, width, height := frame()
			return shootFrame(path, pixels, width, height)
		},
	}
}

func sharedKeyCode(name string) (int32, bool) {
	if len(name) == 1 && (name[0] >= '0' && name[0] <= '9' || name == "*" || name == "#") {
		return int32(name[0]), true
	}
	code, ok := map[string]int32{"up": 141, "down": 146, "left": 142, "right": 145, "fire": 148, "ok": 148,
		"soft1": 6, "soft2": 7, "soft3": 9, "ez": 9, "clear": 8, "call": 10, "hangup": -1}[strings.ToLower(name)]
	return code, ok
}
