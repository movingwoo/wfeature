package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/png"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/filter/hqx"
	"github.com/movingwoo/wfeature/internal/gdbstub"
	"github.com/movingwoo/wfeature/internal/jvm"
	"github.com/movingwoo/wfeature/internal/licenses"
	"github.com/movingwoo/wfeature/internal/platform/ktf"
	"golang.org/x/text/encoding/korean"

	"github.com/movingwoo/wfeature/internal/platform/lgt"
	"github.com/movingwoo/wfeature/internal/platform/skt"
	"github.com/movingwoo/wfeature/internal/route"
	"github.com/movingwoo/wfeature/internal/session"
	"github.com/movingwoo/wfeature/internal/wipic"
)

// idlePollCeiling bounds one idle wait in the interactive tick loop, so a
// guest sleeping for a second still leaves an interrupt or a typed cheat
// command a prompt answer.
const idlePollCeiling = 50 * time.Millisecond

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		printUsage(stderr)
		return 2
	}
	// The handset's subscriber number is a decision rather than a fact — see
	// wipic.SetSubscriberNumber — so both hosts read the same variable for it
	// before anything is loaded.
	if number := os.Getenv("WFEATURE_PHONE_NUMBER"); number != "" {
		if err := wipic.SetSubscriberNumber(number); err != nil {
			fmt.Fprintln(stderr, err)
			return 2
		}
	}
	switch args[0] {
	case "inspect":
		if len(args) != 2 {
			printUsage(stderr)
			return 2
		}
		return inspect(args[1], stdout, stderr)
	case "runskt":
		if len(args) < 2 {
			printUsage(stderr)
			return 2
		}
		return runSKT(args[1], args[2:], stdout, stderr)
	case "licenses":
		// A release is one executable, so the notices have to be reachable from
		// it rather than only from the repository.
		fmt.Fprint(stdout, licenses.Project, "\n\n", licenses.ThirdParty)
		return 0
	case "runlgt":
		if len(args) < 2 {
			printUsage(stderr)
			return 2
		}
		return runLGT(args[1], args[2:], stdout, stderr)
	case "runktf":
		if len(args) < 2 {
			printUsage(stderr)
			return 2
		}
		return runKTF(args[1], args[2:], stdout, stderr)
	case "invoke":
		if len(args) < 4 {
			printUsage(stderr)
			return 2
		}
		return invoke(args[1], args[2], args[3], args[4:], stdout, stderr)
	case "importsaves":
		if len(args) < 2 {
			printUsage(stderr)
			return 2
		}
		return importSaves(args[1], args[2:], stdout, stderr)
	case "checkgames":
		return checkGames(args[1:], stdout, stderr)
	case "provision":
		if len(args) < 2 {
			printUsage(stderr)
			return 2
		}
		return provision(args[1], args[2:], stdout, stderr)
	case "contactsheet":
		return contactSheet(args[1:], stdout, stderr)
	case "framediff":
		return frameDiff(args[1:], stdout, stderr)
	case "zoom":
		return zoomFrame(args[1:], stdout, stderr)
	default:
		printUsage(stderr)
		return 2
	}
}

// defaultSaveRoot names this binary's save tree. Each build profile owns one,
// so playing a debug build never moves a release build's progress — and a
// debug session, where a half-finished API is most likely to write a save the
// game cannot read back, cannot damage the saves the release build reads. The
// server derives the same path from its own build tag, so `make run` and the
// browser agree. `runktf -save <dir>` overrides it.
func defaultSaveRoot() string {
	return platformSaveRoot("ktf")
}

// platformSaveRoot is the same tree for any platform; the platform segment
// keeps two emulated systems that happen to share a title name apart.
func platformSaveRoot(platform string) string {
	return filepath.Join("var", "savedata", backend.BuildProfile(), platform)
}

// sktTickInterval is how long one runskt tick takes. A MIDlet has no tick of
// its own: its threads sleep against the wall clock and its frame loop is
// whatever those threads and the serial queue produce, so the run is paced at
// a frame rather than stepped as fast as the Host can go. It is the interval
// the server session drives the same runtime with.
const sktTickInterval = 16 * time.Millisecond

func runSKT(path string, args []string, stdout, stderr io.Writer) int {
	logger := backend.NewLogger(stderr)
	ticks := 64
	framePath := ""
	frameDir := ""
	saveRoot := ""
	keyEvents := map[int][]int32{}
	keyHold := 1
	diagPath := ""
	cheatConsole := false
	ticksChosen := false
	// The screen is the handset's, and on this vendor it is not the same
	// handset for every title: one local archive ships its artwork only in the
	// 120 and 176 wide sets and asks for a 240 one it does not contain, which
	// is a title packaged for a smaller phone than the default.
	screenWidth, screenHeight := session.DefaultWidth, session.DefaultHeight
	for index := 0; index < len(args); index++ {
		switch args[index] {
		case "-ticks":
			if index+1 >= len(args) {
				fmt.Fprintln(stderr, "-ticks expects a count")
				return 2
			}
			index++
			parsed, err := strconv.Atoi(args[index])
			if err != nil || parsed <= 0 {
				fmt.Fprintf(stderr, "invalid -ticks %q\n", args[index])
				return 2
			}
			ticks = parsed
			ticksChosen = true
		case "-cheat":
			cheatConsole = true
		case "-frame":
			if index+1 >= len(args) {
				fmt.Fprintln(stderr, "-frame expects a path")
				return 2
			}
			index++
			framePath = args[index]
		case "-framedir":
			if index+1 >= len(args) {
				fmt.Fprintln(stderr, "-framedir expects a directory")
				return 2
			}
			index++
			frameDir = args[index]
		case "-save":
			if index+1 >= len(args) {
				fmt.Fprintln(stderr, "-save expects a directory")
				return 2
			}
			index++
			saveRoot = args[index]
		case "-key":
			if index+1 >= len(args) {
				fmt.Fprintln(stderr, "-key expects <tick>:<name>")
				return 2
			}
			index++
			tick, key, err := parseSKTKeyEvent(args[index])
			if err != nil {
				fmt.Fprintln(stderr, err)
				return 2
			}
			keyEvents[tick] = append(keyEvents[tick], key)
		case "-diag":
			if index+1 >= len(args) {
				fmt.Fprintln(stderr, "-diag expects a report path")
				return 2
			}
			index++
			diagPath = args[index]
		case "-screen":
			if index+1 >= len(args) {
				fmt.Fprintln(stderr, "-screen expects <width>x<height>")
				return 2
			}
			index++
			width, height, err := parseScreenSize(args[index])
			if err != nil {
				fmt.Fprintln(stderr, err)
				return 2
			}
			screenWidth, screenHeight = width, height
		case "-hold":
			if index+1 >= len(args) {
				fmt.Fprintln(stderr, "-hold expects a tick count")
				return 2
			}
			index++
			parsed, err := strconv.Atoi(args[index])
			if err != nil || parsed <= 0 {
				fmt.Fprintf(stderr, "invalid -hold %q\n", args[index])
				return 2
			}
			keyHold = parsed
		default:
			fmt.Fprintf(stderr, "unknown runskt option %q\n", args[index])
			return 2
		}
	}

	logger.Debug("starting SKT title", "profile", backend.BuildProfile(), "path", path)
	archive, err := openSKT(path)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	framebuffer, err := backend.NewMemoryFramebuffer(screenWidth, screenHeight)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	// RMS record stores and com.xce.io files persist under the same tree KTF
	// saves use, so one save root holds everything a user would think of as
	// their progress.
	if saveRoot == "" {
		saveRoot = filepath.Join(platformSaveRoot("skt"), skt.SaveOwner(archive.Descriptor))
	}
	runtime, err := skt.Start(archive, skt.Options{
		JVM:         jvm.Options{Logger: logger},
		Framebuffer: framebuffer,
		SaveStore:   backend.NewDirectorySaveStore(saveRoot),
	})
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	logger.Debug("SKT startup dispatch completed", "main_class", archive.Descriptor.MainClass, "state", runtime.State())

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	// Reading stdin on its own goroutine keeps the tick loop paced; the
	// commands run between Host passes, which is the only time reading and
	// freezing the object graph is safe.
	var cheatCommands chan string
	if cheatConsole {
		cheatCommands = make(chan string, 16)
		go func() {
			lines := bufio.NewScanner(os.Stdin)
			for lines.Scan() {
				cheatCommands <- lines.Text()
			}
			close(cheatCommands)
		}()
		// A cheat session runs until it is interrupted, unless the caller asked
		// for a specific number of ticks.
		if !ticksChosen {
			ticks = 1 << 30
		}
	}
	startedAt := time.Now()
	// A scripted key is released -hold ticks after its press for the reason it
	// is on the WIPI paths: a Canvas that samples the keypad once a frame never
	// sees a press and a release delivered together.
	keyReleases := map[int][]int32{}
	ran := 0
	runErr := error(nil)
	dumpedPresents := uint64(0)
	for ; ran < ticks; ran++ {
		if ctx.Err() != nil {
			break
		}
		if frameDir != "" {
			if frame, presents := framebuffer.Snapshot(); presents != dumpedPresents {
				dumpedPresents = presents
				name := filepath.Join(frameDir, fmt.Sprintf("tick%04d.png", ran))
				if err := writePNG(name, frame.RGBA, frame.Width, frame.Height); err != nil {
					fmt.Fprintf(stderr, "write frame: %v\n", err)
					return 1
				}
			}
		}
		for _, key := range keyEvents[ran] {
			if err := runtime.SendKey(skt.KeyPressed, key); err != nil {
				runErr = err
				break
			}
			keyReleases[ran+keyHold] = append(keyReleases[ran+keyHold], key)
		}
		for _, key := range keyReleases[ran] {
			if err := runtime.SendKey(skt.KeyReleased, key); err != nil {
				runErr = err
				break
			}
		}
		delete(keyReleases, ran)
		if cheatConsole {
			for done := false; !done; {
				select {
				case line, ok := <-cheatCommands:
					if !ok {
						cheatCommands = nil
						done = true
						break
					}
					if output := runtime.CheatConsole().Execute(line); output != "" {
						fmt.Fprintln(stdout, output)
					}
				default:
					done = true
				}
			}
		}
		if runErr == nil {
			runtime.AdvanceAudio(time.Since(startedAt))
			runErr = runtime.RunPending()
		}
		if runErr != nil {
			fmt.Fprintf(stderr, "tick %d: %v\n", ran, runErr)
			break
		}
		if state := runtime.State(); state == skt.StateDestroyed || state == skt.StateError {
			break
		}
		select {
		case <-ctx.Done():
		case <-time.After(sktTickInterval):
		}
	}

	frame, _ := framebuffer.Snapshot()
	if framePath != "" {
		if err := writePNG(framePath, frame.RGBA, frame.Width, frame.Height); err != nil {
			fmt.Fprintf(stderr, "write frame: %v\n", err)
			return 1
		}
	}
	summary := struct {
		skt.RuntimeSummary
		Ticks int `json:"ticks"`
		// Lit is the count of non-black pixels, the same first-frame signal the
		// acceptance probes read: a MIDlet that reached its title screen and one
		// that painted nothing are otherwise the same JSON.
		Lit int `json:"lit"`
	}{RuntimeSummary: runtime.Summary(), Ticks: ran, Lit: frameLit(frame.RGBA)}

	if diagPath != "" {
		if err := writeSKTDiagnostics(diagPath, runtime); err != nil {
			fmt.Fprintf(stderr, "write diagnostics: %v\n", err)
			return 1
		}
	}

	encoder := json.NewEncoder(stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(summary); err != nil {
		fmt.Fprintf(stderr, "write result: %v\n", err)
		return 1
	}
	if runErr != nil {
		return 1
	}
	return 0
}

// parseSKTKeyEvent splits "<tick>:<name>" the way parseKeyEvent does, against
// the MIDP key codes a MIDlet compares with.
// parseScreenSize reads a `<width>x<height>` screen. The bounds are the ones a
// framebuffer of that size has to be worth allocating from an argument.
func parseScreenSize(text string) (int, int, error) {
	width, height, found := strings.Cut(strings.ToLower(text), "x")
	if !found {
		return 0, 0, fmt.Errorf("invalid -screen %q, want <width>x<height>", text)
	}
	parsedWidth, widthErr := strconv.Atoi(width)
	parsedHeight, heightErr := strconv.Atoi(height)
	if widthErr != nil || heightErr != nil ||
		parsedWidth < 32 || parsedHeight < 32 || parsedWidth > 1024 || parsedHeight > 1024 {
		return 0, 0, fmt.Errorf("invalid -screen %q, want two sizes between 32 and 1024", text)
	}
	return parsedWidth, parsedHeight, nil
}

func parseSKTKeyEvent(spec string) (int, int32, error) {
	colon := strings.IndexByte(spec, ':')
	if colon <= 0 || colon == len(spec)-1 {
		return 0, 0, fmt.Errorf("invalid key event %q, expected <tick>:<name>", spec)
	}
	tick, err := strconv.Atoi(spec[:colon])
	if err != nil || tick < 0 {
		return 0, 0, fmt.Errorf("invalid key event tick %q", spec[:colon])
	}
	name := spec[colon+1:]
	key, ok := skt.KeyCodeByName(name)
	if !ok {
		return 0, 0, fmt.Errorf("unknown key name %q", name)
	}
	return tick, key, nil
}

func inspect(path string, stdout, stderr io.Writer) int {
	logger := backend.NewLogger(stderr)
	logger.Debug("starting archive inspection", "profile", backend.BuildProfile(), "path", path)
	archive, err := openSKT(path)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	logger.Debug("archive inspection complete", "entries", len(archive.Entries), "main_class", archive.MainClass.Name)

	encoder := json.NewEncoder(stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(archive.Summary()); err != nil {
		fmt.Fprintf(stderr, "write result: %v\n", err)
		return 1
	}
	return 0
}

func invoke(path, method, descriptor string, argumentTexts []string, stdout, stderr io.Writer) int {
	logger := backend.NewLogger(stderr)
	archive, err := openSKT(path)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	methodType, err := jvm.ParseMethodDescriptor(descriptor)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if len(argumentTexts) != len(methodType.Parameters) {
		fmt.Fprintf(stderr, "expected %d arguments, got %d\n", len(methodType.Parameters), len(argumentTexts))
		return 2
	}
	arguments := make([]jvm.Value, len(argumentTexts))
	for index, text := range argumentTexts {
		arguments[index], err = parseTextValue(methodType.Parameters[index], text)
		if err != nil {
			fmt.Fprintf(stderr, "argument %d: %v\n", index, err)
			return 2
		}
	}

	machine := jvm.New(archive, jvm.Options{Logger: logger})
	result, err := machine.InvokeStatic(archive.Descriptor.MainClass, method, descriptor, arguments...)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	output, err := invocationOutput(archive.Descriptor.MainClass, method, descriptor, result)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	encoder := json.NewEncoder(stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(output); err != nil {
		fmt.Fprintf(stderr, "write result: %v\n", err)
		return 1
	}
	return 0
}

// defaultProbeTicks is how far an unscripted run goes when -ticks is absent.
// It doubles as the sentinel for "the caller did not choose", which -cheat and
// -route both use to take a budget of their own instead.
const defaultProbeTicks = 64

// runKTF drives a KTF archive through startApp and the cooperative service
// loop until a first frame flushes or the tick budget runs out. It reports a
// JSON summary and can save the presented frame as PNG. With -play it keeps
// ticking past the first frame, injecting -key events, so in-game progress is
// observable from the command line; with -route it replays a script instead.
func runKTF(path string, extra []string, stdout, stderr io.Writer) int {
	ticks := defaultProbeTicks
	ticksChosen := false
	framePath := ""
	frameDir := ""
	play := false
	speed := 1.0
	cheatConsole := false
	diagPath := ""
	profilePath := ""
	profileFoldedPath := ""
	profileFrom := 0
	routePath := ""
	audioPrefix := ""
	keyEvents := map[int][]int32{}
	keyHold := 1
	saveRoot := defaultSaveRoot()
	gdbAddress := ""
	screenWidth, screenHeight := 0, 0
	for index := 0; index < len(extra); index++ {
		switch extra[index] {
		case "-save":
			if index+1 >= len(extra) {
				fmt.Fprintln(stderr, "-save expects a directory")
				return 2
			}
			saveRoot = extra[index+1]
			index++
		case "-screen":
			if index+1 >= len(extra) {
				fmt.Fprintln(stderr, "-screen expects <width>x<height>")
				return 2
			}
			index++
			width, height, err := parseScreenSize(extra[index])
			if err != nil {
				fmt.Fprintln(stderr, err)
				return 2
			}
			screenWidth, screenHeight = width, height
		case "-ticks":
			if index+1 >= len(extra) {
				fmt.Fprintln(stderr, "-ticks expects a count")
				return 2
			}
			parsed, err := strconv.Atoi(extra[index+1])
			if err != nil || parsed <= 0 {
				fmt.Fprintf(stderr, "invalid tick count %q\n", extra[index+1])
				return 2
			}
			ticks = parsed
			ticksChosen = true
			index++
		case "-frame":
			if index+1 >= len(extra) {
				fmt.Fprintln(stderr, "-frame expects a PNG path")
				return 2
			}
			framePath = extra[index+1]
			index++
		case "-framedir":
			if index+1 >= len(extra) {
				fmt.Fprintln(stderr, "-framedir expects a directory")
				return 2
			}
			frameDir = extra[index+1]
			index++
		case "-gdb":
			if index+1 >= len(extra) {
				fmt.Fprintln(stderr, "-gdb expects a listen address")
				return 2
			}
			index++
			gdbAddress = extra[index]
		case "-diag":
			if index+1 >= len(extra) {
				fmt.Fprintln(stderr, "-diag expects a report path")
				return 2
			}
			diagPath = extra[index+1]
			index++
		case "-profile":
			if index+1 >= len(extra) {
				fmt.Fprintln(stderr, "-profile expects a report path")
				return 2
			}
			profilePath = extra[index+1]
			index++
		case "-profile-folded":
			if index+1 >= len(extra) {
				fmt.Fprintln(stderr, "-profile-folded expects a path")
				return 2
			}
			profileFoldedPath = extra[index+1]
			index++
		case "-profile-from":
			if index+1 >= len(extra) {
				fmt.Fprintln(stderr, "-profile-from expects a tick")
				return 2
			}
			parsed, err := strconv.Atoi(extra[index+1])
			if err != nil || parsed < 0 {
				fmt.Fprintf(stderr, "invalid profile start tick %q\n", extra[index+1])
				return 2
			}
			profileFrom = parsed
			index++
		case "-route":
			if index+1 >= len(extra) {
				fmt.Fprintln(stderr, "-route expects a script path")
				return 2
			}
			// A route deliberately stays on the manual clock unless -play is
			// also given: jumping to each next deadline makes the replay
			// deterministic and as fast as the guest can be driven, which is
			// what a repro run wants. -play forces the wall clock when the
			// point is to watch it.
			routePath = extra[index+1]
			index++
		case "-scale":
			if index+1 >= len(extra) {
				fmt.Fprintln(stderr, "-scale expects 2, 3, or 4")
				return 2
			}
			parsed, err := strconv.Atoi(extra[index+1])
			if err != nil || parsed < 2 || parsed > 4 {
				fmt.Fprintf(stderr, "invalid hqx scale %q, expected 2, 3, or 4\n", extra[index+1])
				return 2
			}
			frameScale = parsed
			index++
		case "-audio":
			if index+1 >= len(extra) {
				fmt.Fprintln(stderr, "-audio expects an output path prefix")
				return 2
			}
			audioPrefix = extra[index+1]
			index++
		case "-play":
			play = true
		case "-speed":
			if index+1 >= len(extra) {
				fmt.Fprintln(stderr, "-speed expects a multiplier")
				return 2
			}
			parsed, err := strconv.ParseFloat(extra[index+1], 64)
			if err != nil || parsed <= 0 {
				fmt.Fprintf(stderr, "invalid speed %q\n", extra[index+1])
				return 2
			}
			speed = parsed
			play = true
			index++
		case "-cheat":
			cheatConsole = true
			play = true
		case "-key":
			if index+1 >= len(extra) {
				fmt.Fprintln(stderr, "-key expects <tick>:<name>")
				return 2
			}
			tick, key, err := parseKeyEvent(extra[index+1])
			if err != nil {
				fmt.Fprintln(stderr, err)
				return 2
			}
			keyEvents[tick] = append(keyEvents[tick], key)
			play = true
			index++
		case "-hold":
			if index+1 >= len(extra) {
				fmt.Fprintln(stderr, "-hold expects a tick count")
				return 2
			}
			parsed, err := strconv.Atoi(extra[index+1])
			if err != nil || parsed < 1 {
				fmt.Fprintf(stderr, "invalid -hold %q\n", extra[index+1])
				return 2
			}
			keyHold = parsed
			index++
		default:
			fmt.Fprintf(stderr, "unknown runktf option %q\n", extra[index])
			return 2
		}
	}
	// The route is parsed before the archive is even read: a typo in a script
	// should be reported now, not after the minutes of guest execution it takes
	// to reach the step that contains it.
	var script *route.Route
	if routePath != "" {
		text, err := os.ReadFile(routePath)
		if err != nil {
			fmt.Fprintf(stderr, "read route: %v\n", err)
			return 1
		}
		script, err = route.Parse(string(text), ktf.KeyCodeByName)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 2
		}
	}
	logger := backend.NewLogger(stderr)
	logger.Debug("starting KTF archive", "profile", backend.BuildProfile(), "path", path)
	data, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintf(stderr, "read archive: %v\n", err)
		return 1
	}
	// An interrupt cancels the context rather than killing the process, which
	// is what reaches a guest call already running: the KTF client renews a
	// service call's step budget only while its Host context is live, so a
	// long guest run stops at the next window instead of at the next tick.
	// The session still closes normally afterwards.
	ctx, stopInterrupts := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stopInterrupts()
	// The vendor shipped two generations of package and this subcommand takes
	// both, because a person with a game in their hand should not have to know
	// which one it is. What the earlier one cannot do it refuses by name: it
	// has no descriptor to inspect, no AOT methods to profile or symbolize,
	// and no repro route runner yet. The cheat engine it does have.
	if ktf.IsNativeArchive(data) {
		for name, unsupported := range map[string]bool{
			"-diag":           diagPath != "",
			"-gdb":            gdbAddress != "",
			"-profile":        profilePath != "",
			"-profile-folded": profileFoldedPath != "",
			"-route":          routePath != "",
		} {
			if unsupported {
				fmt.Fprintf(stderr, "%s is not available for the earlier KTF package\n", name)
				return 2
			}
		}
		return runKTFNative(ctx, data, nativeRun{
			ticks:        ticks,
			ticksChosen:  ticksChosen,
			framePath:    framePath,
			frameDir:     frameDir,
			play:         play,
			saveRoot:     saveRoot,
			keyEvents:    keyEvents,
			keyHold:      keyHold,
			audioPrefix:  audioPrefix,
			cheatConsole: cheatConsole,
			screenWidth:  screenWidth,
			screenHeight: screenHeight,
			logger:       logger,
		}, stdout, stderr)
	}
	// The ordered boundary trace is a debug-profile cost; release runs keep
	// only the counted totals.
	traceLimit := 0
	if backend.DebugBuild() {
		traceLimit = ktf.DefaultTraceLimit
	}
	// A game paces itself with the waits it asks for, and what those waits
	// should cost depends on who is watching. -play shows the game to a
	// person, so its waits run on the wall clock and the game moves at the
	// speed it was written for. A frame probe is measuring what the guest
	// computes, not how long it takes, so it runs a manual clock it jumps to
	// each next deadline: the same sequence of guest work, at no real cost.
	options := ktf.SessionOptions{
		SaveRoot:   saveRoot,
		TraceLimit: traceLimit,
		Logger:     logger,
		Speed:      speed,
		Width:      screenWidth,
		Height:     screenHeight,
	}
	// The recording sink timestamps with guest time, which the session only
	// answers once it exists, so the clock is attached just after the start.
	var audioSink *backend.RecordingSink
	if audioPrefix != "" {
		audioSink = backend.NewRecordingSink(nil)
		options.AudioSink = audioSink
	}
	var probeClock *ktf.ManualClock
	if !play {
		probeClock = ktf.NewManualClock(time.Time{})
		options.Clock = probeClock
	}
	session, err := ktf.StartSession(ctx, data, options)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	defer session.Close()

	// A debugger attaches to the loaded core. Attaching slows execution to
	// one instruction per quantum, which is why it is opt-in rather than
	// always listening.
	if gdbAddress != "" {
		gdbTarget := gdbstub.NewCoreTarget(session.Client.Core())
		listener, listenErr := gdbstub.ListenAndServe(gdbAddress, gdbTarget, logger)
		if listenErr != nil {
			fmt.Fprintln(stderr, listenErr)
			return 1
		}
		defer func() {
			listener.Close()
			gdbTarget.Finished()
			gdbTarget.Detach()
		}()
		fmt.Fprintf(stderr, "gdb stub listening on %s (target remote %s)\n", gdbAddress, gdbAddress)
	}
	if audioSink != nil {
		audioSink.Clock = session.GuestElapsed
	}
	profiling := profilePath != "" || profileFoldedPath != ""
	if profiling {
		session.EnableProfile(0)
	}
	var cheatCommands chan string
	if cheatConsole {
		// Reading stdin on its own goroutine keeps the tick loop paced; the
		// commands run between ticks so a scan observes the guest at a frame
		// boundary rather than mid-instruction.
		cheatCommands = make(chan string, 16)
		go func() {
			lines := bufio.NewScanner(os.Stdin)
			for lines.Scan() {
				cheatCommands <- lines.Text()
			}
			close(cheatCommands)
		}()
		if !ticksChosen {
			ticks = 1 << 30
		}
		fmt.Fprintln(stdout, "cheat: attached. `help` for commands, ctrl-c to quit.")
	}
	ran := 0
	dumpedFlush := uint32(0)
	keyReleases := map[int][]int32{}
	var tickError error
	var routeResult route.Result
	if script != nil {
		// A route runs until it arrives, so it does not inherit the default
		// tick count that bounds an unscripted probe; an explicit -ticks still
		// caps it.
		budget := ticks
		if budget == defaultProbeTicks {
			budget = 0
		}
		routeResult, tickError = runRoute(ctx, session, script, routeRun{
			frameDir:   frameDir,
			profiling:  profiling,
			hold:       keyHold,
			probeClock: probeClock,
			maxTicks:   budget,
			stderr:     stderr,
		})
		ran = routeResult.Ticks
		if tickError != nil && !errors.Is(tickError, ktf.ErrGuestExited) && !errors.Is(tickError, context.Canceled) {
			fmt.Fprintf(stderr, "route: %v\n", tickError)
		}
	}
	for ; script == nil && ran < ticks; ran++ {
		if ctx.Err() != nil {
			break
		}
		// A game spends its first thousands of ticks loading, and that would
		// dominate a profile of the scene actually being investigated.
		// -profile-from throws those samples away on arrival.
		if profiling && profileFrom > 0 && ran == profileFrom {
			session.ResetProfile()
		}
		// Frame copies the whole RGBA buffer, so the loop only takes one when
		// the flush counter says there is something new to look at.
		flushes := session.Flushes()
		if flushes > 0 && (!play || (frameDir != "" && flushes != dumpedFlush)) {
			frame, width, height, _ := session.Frame()
			if !play && frameLit(frame) > 0 {
				break
			}
			if frameDir != "" && width > 0 && height > 0 && flushes != dumpedFlush {
				dumpedFlush = flushes
				name := filepath.Join(frameDir, fmt.Sprintf("tick%04d.png", ran))
				if err := writePNG(name, frame, width, height); err != nil {
					fmt.Fprintf(stderr, "write frame: %v\n", err)
					return 1
				}
			}
		}
		// A scripted key is pressed on its tick and released -hold ticks later.
		// A title that samples the keypad once a frame never sees a press and a
		// release delivered in the same tick: two of the local ones sit on
		// their title screen for a whole run that way, which reads exactly like
		// a title that has stopped.
		for _, key := range keyEvents[ran] {
			if err := session.SendKey(ctx, ktf.KeyPressed, key); err != nil {
				tickError = err
				break
			}
			keyReleases[ran+keyHold] = append(keyReleases[ran+keyHold], key)
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
		if cheatConsole {
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
		progressed, err := session.Tick(ctx)
		if err != nil {
			tickError = err
			break
		}
		pace(ctx, session, probeClock, cheatConsole)
		if !progressed && !cheatConsole {
			// A round that did nothing is not an idle session while something
			// is still due: a title whose whole loop is one repeating timer
			// spends every round before the first one due doing nothing at
			// all, and stopping there ends the run before it draws.
			if _, pending := session.NextDeadline(); !pending {
				break
			}
		}
	}
	frame, width, height, flushes := session.Frame()
	lit := frameLit(frame)
	if framePath != "" && width > 0 && height > 0 {
		if err := writePNG(framePath, frame, width, height); err != nil {
			fmt.Fprintf(stderr, "write frame: %v\n", err)
			return 1
		}
	}
	summary := map[string]any{
		"aid":        session.Archive.Descriptor.AID,
		"main_class": session.Archive.Descriptor.MainClass,
		"ticks":      ran,
		"flushes":    flushes,
		"width":      width,
		"height":     height,
		"lit_pixels": lit,
	}
	if errors.Is(tickError, ktf.ErrGuestExited) {
		summary["exited"] = true
	} else if tickError != nil {
		summary["tick_error"] = tickError.Error()
	}
	if script != nil {
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
	if diagPath != "" {
		if err := writeDiagnostics(diagPath, session, summary); err != nil {
			fmt.Fprintf(stderr, "write diagnostics: %v\n", err)
			return 1
		}
		summary["diagnostics"] = diagPath
	}
	if profiling {
		profile := session.Profile()
		if profilePath != "" {
			if err := os.WriteFile(profilePath, []byte(profile.Report(30)), 0o644); err != nil {
				fmt.Fprintf(stderr, "write profile: %v\n", err)
				return 1
			}
			summary["profile"] = profilePath
		}
		if profileFoldedPath != "" {
			if err := os.WriteFile(profileFoldedPath, []byte(profile.Folded()), 0o644); err != nil {
				fmt.Fprintf(stderr, "write folded profile: %v\n", err)
				return 1
			}
			summary["profile_folded"] = profileFoldedPath
		}
		summary["profile_samples"] = profile.Samples
		summary["profile_steps"] = profile.Steps
	}
	if audioSink != nil {
		messages, samples := audioSink.Summary()
		summary["audio_midi_messages"] = messages
		summary["audio_wave_samples"] = samples
		written, err := audioSink.Write(audioPrefix)
		if err != nil {
			fmt.Fprintf(stderr, "write audio: %v\n", err)
			return 1
		}
		summary["audio"] = written
	}
	encoder := json.NewEncoder(stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(summary); err != nil {
		fmt.Fprintf(stderr, "write result: %v\n", err)
		return 1
	}
	return 0
}

// writeDiagnostics saves the session's runtime boundary report next to the run
// summary. The counted totals answer what the game exercised; the ordered
// trace, present only in debug builds, answers what it was doing last.
func writeDiagnostics(path string, session *ktf.Session, summary map[string]any) error {
	diagnostics := session.Diagnostics()
	report := map[string]any{
		"profile":     backend.BuildProfile(),
		"summary":     summary,
		"counts":      diagnostics.Counts,
		"counts_text": diagnostics.FormatCounts(0),
		"trace":       diagnostics.Trace,
		"traced":      diagnostics.Traced,
	}
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	if directory := filepath.Dir(path); directory != "" {
		if err := os.MkdirAll(directory, 0o755); err != nil {
			return err
		}
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}

// writeSKTDiagnostics saves what an SKT run used. Unlike the KTF report there
// is no ordered trace: the question this one exists to answer is which of the
// runtime's Java surface a title reached, and that is a total rather than a
// sequence.
func writeSKTDiagnostics(path string, runtime *skt.Runtime) error {
	diagnostics := runtime.Diagnostics()
	report := map[string]any{
		"profile":      backend.BuildProfile(),
		"classes":      diagnostics.Classes,
		"missing":      diagnostics.Missing,
		"natives":      diagnostics.Natives,
		"natives_text": diagnostics.FormatCounts(0),
	}
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	if directory := filepath.Dir(path); directory != "" {
		if err := os.MkdirAll(directory, 0o755); err != nil {
			return err
		}
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}

// parseKeyEvent splits "<tick>:<name>" into the tick to fire at and the WIPI
// key code. Names cover the phone keypad and navigation keys.
// routeRun carries what a route replay needs from the command line.
type routeRun struct {
	frameDir  string
	profiling bool
	// hold is how many ticks a route's `key` step holds its press. It is the
	// same -hold a scripted key takes, for the same reason: a title that
	// samples the keypad once a frame never sees a press and a release
	// delivered together.
	hold       int
	probeClock *ktf.ManualClock
	maxTicks   int
	stderr     io.Writer
}

// runRoute replays a script against a started session. Ticking, and the pacing
// that goes with it, stays here rather than in the runner: a route should
// replay at whatever speed the Host is already running at, so the same script
// serves a fast deterministic probe and a watchable -play run.
func runRoute(ctx context.Context, session *ktf.Session, script *route.Route, options routeRun) (route.Result, error) {
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
			_, pending := session.NextDeadline()
			return !pending
		},
		Advance: func(ctx context.Context) (bool, error) {
			progressed, err := session.Tick(ctx)
			if err != nil {
				return progressed, err
			}
			pace(ctx, session, options.probeClock, false)
			return progressed, nil
		},
		Checkpoint: func(label string, tick int, reset bool) error {
			// A mark says the route arrived somewhere worth measuring, so the
			// profile starts over here and covers the scene rather than
			// everything it took to get here.
			if reset && options.profiling {
				session.ResetProfile()
			}
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

// pace holds a tick loop to the game's own speed. Whatever the game is waiting
// for, the loop must not busy-poll it. A probe jumps its clock to the end of
// the wait, so its tick budget buys guest work rather than repeats; an
// interactive run waits it out, which is what holds the game to the speed it
// was written for. The ceiling keeps an interrupt or a typed cheat command from
// waiting out a long guest sleep, and stands in as the poll interval when the
// guest declared no wait at all — the cheat console still has to read stdin.
func pace(ctx context.Context, session *ktf.Session, probeClock *ktf.ManualClock, cheatConsole bool) {
	if probeClock != nil {
		session.SkipToNextDeadline()
		return
	}
	wait := idlePollCeiling
	if deadline, pending := session.NextDeadline(); pending {
		wait = min(time.Until(deadline), idlePollCeiling)
	} else if !cheatConsole {
		wait = 0
	}
	if wait > 0 {
		select {
		case <-ctx.Done():
		case <-time.After(wait):
		}
	}
}

func parseKeyEvent(spec string) (int, int32, error) {
	colon := strings.IndexByte(spec, ':')
	if colon <= 0 || colon == len(spec)-1 {
		return 0, 0, fmt.Errorf("invalid key event %q, expected <tick>:<name>", spec)
	}
	tick, err := strconv.Atoi(spec[:colon])
	if err != nil || tick < 0 {
		return 0, 0, fmt.Errorf("invalid key event tick %q", spec[:colon])
	}
	name := spec[colon+1:]
	// The names live with the key codes in the platform package, so -key and a
	// route script cannot drift apart on what "fire" means.
	key, ok := ktf.KeyCodeByName(name)
	if !ok {
		return 0, 0, fmt.Errorf("unknown key name %q", name)
	}
	return tick, key, nil
}

// frameLit counts non-black RGBA pixels, the same first-frame signal the
// acceptance probe uses.
func frameLit(frame []byte) int {
	lit := 0
	for offset := 0; offset+3 < len(frame); offset += 4 {
		if frame[offset] != 0 || frame[offset+1] != 0 || frame[offset+2] != 0 {
			lit++
		}
	}
	return lit
}

// frameScale magnifies saved frames with hqx. Zero and one save the frame as
// the guest drew it.
var frameScale = 0

func writePNG(path string, frame []byte, width, height int) error {
	if len(frame) < width*height*4 {
		return fmt.Errorf("frame buffer is smaller than %dx%d", width, height)
	}
	if frameScale > 1 {
		scaled, scaledWidth, scaledHeight, err := hqx.ScaleRGBA(frame[:width*height*4], width, height, frameScale)
		if err != nil {
			return err
		}
		frame, width, height = scaled, scaledWidth, scaledHeight
	}
	target := image.NewRGBA(image.Rect(0, 0, width, height))
	copy(target.Pix, frame)
	// A -framedir that does not exist yet is an ordinary way to ask for one,
	// and failing the run at the first painted frame is a poor answer to it.
	if directory := filepath.Dir(path); directory != "" && directory != "." {
		if err := os.MkdirAll(directory, 0o755); err != nil {
			return err
		}
	}
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()
	if err := png.Encode(file, target); err != nil {
		return err
	}
	return file.Close()
}

// openSKT reads what runskt, inspect and invoke are pointed at. An SKT title
// normally arrives as the archive a handset was sent — the JAR beside the .msd
// naming it — rather than as a JAR that names itself, so both shapes are read.
func openSKT(path string) (*skt.Archive, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read archive: %w", err)
	}
	return skt.Open(data)
}

func parseTextValue(typeInfo jvm.Type, text string) (jvm.Value, error) {
	switch typeInfo.Kind {
	case jvm.TypeBoolean:
		if strings.EqualFold(text, "true") {
			return jvm.IntValue(1), nil
		}
		if strings.EqualFold(text, "false") {
			return jvm.IntValue(0), nil
		}
		return jvm.VoidValue(), fmt.Errorf("expected true or false")
	case jvm.TypeByte, jvm.TypeChar, jvm.TypeShort, jvm.TypeInt:
		value, err := strconv.ParseInt(text, 0, 32)
		if err != nil {
			return jvm.VoidValue(), fmt.Errorf("parse int %q: %w", text, err)
		}
		return jvm.IntValue(int32(value)), nil
	case jvm.TypeLong:
		value, err := strconv.ParseInt(text, 0, 64)
		if err != nil {
			return jvm.VoidValue(), fmt.Errorf("parse long %q: %w", text, err)
		}
		return jvm.LongValue(value), nil
	case jvm.TypeFloat:
		value, err := strconv.ParseFloat(text, 32)
		if err != nil {
			return jvm.VoidValue(), fmt.Errorf("parse float %q: %w", text, err)
		}
		return jvm.FloatValue(float32(value)), nil
	case jvm.TypeDouble:
		value, err := strconv.ParseFloat(text, 64)
		if err != nil {
			return jvm.VoidValue(), fmt.Errorf("parse double %q: %w", text, err)
		}
		return jvm.DoubleValue(value), nil
	case jvm.TypeReference:
		if text == "null" {
			return jvm.ReferenceValue(nil), nil
		}
		if typeInfo.ClassName == "java/lang/String" {
			return jvm.ReferenceValue(&jvm.Object{ClassName: typeInfo.ClassName, Native: text}), nil
		}
		return jvm.VoidValue(), fmt.Errorf("only null and java/lang/String references are supported")
	default:
		return jvm.VoidValue(), fmt.Errorf("array arguments are not supported")
	}
}

type invokeResult struct {
	Class      string `json:"class"`
	Method     string `json:"method"`
	Descriptor string `json:"descriptor"`
	Kind       string `json:"kind"`
	Value      any    `json:"value"`
}

func invocationOutput(class, method, descriptor string, value jvm.Value) (invokeResult, error) {
	result := invokeResult{Class: class, Method: method, Descriptor: descriptor, Kind: value.Kind().String()}
	var err error
	switch value.Kind() {
	case jvm.ValueVoid:
		result.Value = nil
	case jvm.ValueInt:
		result.Value, err = value.Int32()
	case jvm.ValueLong:
		result.Value, err = value.Int64()
	case jvm.ValueFloat:
		result.Value, err = value.Float32()
	case jvm.ValueDouble:
		result.Value, err = value.Float64()
	case jvm.ValueReference:
		var object *jvm.Object
		object, err = value.Reference()
		if object == nil {
			result.Value = nil
		} else if stringValue, ok := object.Native.(string); ok {
			result.Value = stringValue
		} else {
			result.Value = map[string]string{"class": object.ClassName}
		}
	default:
		err = fmt.Errorf("cannot render JVM value kind %s", value.Kind())
	}
	return result, err
}

// importSaves converts another emulator's save tree into this project's save
// layout. The source keys databases by PID and guest files by AID, one file
// per record; wfeature keys both by PID under db/jdb/fs scopes, so the trees
// are not interchangeable by copying and the archives resolve the owners.
// displayName converts a title name out of the descriptor for a terminal.
// These were Korean handsets and the descriptors are EUC-KR, but the decoding
// belongs here rather than in the parsers: a KTF guest reads those same bytes
// back through getAppProperty and expects the encoding it shipped with.
// Bytes that are not EUC-KR are left alone, because a repacked archive can
// carry an ASCII or UTF-8 descriptor.
func displayName(name string) string {
	decoded, err := korean.EUCKR.NewDecoder().String(name)
	if err != nil {
		return name
	}
	return decoded
}

// checkGames reports the save directories that more than one title claims.
//
// A save owner is the PID the archive declares, and a repacked archive can
// declare one that belongs to another game. Nothing about that shows up while
// a game runs — the two titles simply open the same directory and overwrite
// each other — so this is the check to run when archives are added to the
// library, rather than waiting for a save that will not load.
func checkGames(extra []string, stdout, stderr io.Writer) int {
	gameRoot := filepath.Join("var", "games")
	for index := 0; index < len(extra); index++ {
		switch extra[index] {
		case "-games":
			if index+1 >= len(extra) {
				fmt.Fprintln(stderr, "-games expects a directory")
				return 2
			}
			index++
			gameRoot = extra[index]
		default:
			fmt.Fprintf(stderr, "unknown checkgames option %q\n", extra[index])
			return 2
		}
	}

	ktfCollisions, err := ktf.SaveOwnerCollisions(gameRoot)
	if err != nil {
		fmt.Fprintf(stderr, "scan KTF archives: %v\n", err)
		return 1
	}
	lgtCollisions, err := lgt.SaveOwnerCollisions(gameRoot)
	if err != nil {
		fmt.Fprintf(stderr, "scan LGT archives: %v\n", err)
		return 1
	}
	if len(ktfCollisions) == 0 && len(lgtCollisions) == 0 {
		fmt.Fprintf(stdout, "no save owner is claimed by more than one title under %s\n", gameRoot)
		return 0
	}

	report := func(platform string, owner string, claims []struct{ path, aid, name string }) {
		fmt.Fprintf(stdout, "%s save owner %s is claimed by %d titles:\n", platform, owner, len(claims))
		for _, claim := range claims {
			fmt.Fprintf(stdout, "  AID %s  %s  %s\n", claim.aid, displayName(claim.name), claim.path)
		}
	}
	for _, collision := range ktfCollisions {
		claims := make([]struct{ path, aid, name string }, 0, len(collision.Claims))
		for _, claim := range collision.Claims {
			claims = append(claims, struct{ path, aid, name string }{claim.Path, claim.Descriptor.AID, claim.Descriptor.Properties["NAME"]})
		}
		report("KTF", collision.Owner, claims)
	}
	for _, collision := range lgtCollisions {
		claims := make([]struct{ path, aid, name string }, 0, len(collision.Claims))
		for _, claim := range collision.Claims {
			claims = append(claims, struct{ path, aid, name string }{claim.Path, claim.Descriptor.AID, claim.Descriptor.Fields["Name"]})
		}
		report("LGT", collision.Owner, claims)
	}
	fmt.Fprintln(stdout, "\nThese titles share one save directory and will overwrite each other.")
	return 1
}

func importSaves(source string, extra []string, stdout, stderr io.Writer) int {
	saveRoot := defaultSaveRoot()
	gameRoot := filepath.Join("var", "games")
	dryRun := false
	for index := 0; index < len(extra); index++ {
		switch extra[index] {
		case "-save":
			if index+1 >= len(extra) {
				fmt.Fprintln(stderr, "-save expects a directory")
				return 2
			}
			saveRoot = extra[index+1]
			index++
		case "-games":
			if index+1 >= len(extra) {
				fmt.Fprintln(stderr, "-games expects a directory")
				return 2
			}
			gameRoot = extra[index+1]
			index++
		case "-dry-run":
			dryRun = true
		default:
			fmt.Fprintf(stderr, "unknown importsaves option %q\n", extra[index])
			return 2
		}
	}
	identities, err := ktf.GameIdentities(gameRoot)
	if err != nil {
		fmt.Fprintf(stderr, "read games from %s: %v\n", gameRoot, err)
		return 1
	}
	report, err := ktf.ImportExternalSaves(ktf.ImportOptions{
		SourceRoot: source,
		SaveRoot:   saveRoot,
		Identities: identities,
		DryRun:     dryRun,
	})
	for _, entry := range report.Imported {
		fmt.Fprintf(stdout, "%s -> %s/%s (%d bytes)\n", entry.Source, entry.Owner, entry.Key, entry.Bytes)
	}
	for _, skipped := range report.Skipped {
		fmt.Fprintf(stdout, "skipped %s\n", skipped)
	}
	if err != nil {
		fmt.Fprintf(stderr, "import saves: %v\n", err)
		return 1
	}
	verb := "imported"
	if dryRun {
		verb = "would import"
	}
	fmt.Fprintf(stdout, "%s %d entries, skipped %d\n", verb, len(report.Imported), len(report.Skipped))
	return 0
}

func printUsage(output io.Writer) {
	fmt.Fprintln(output, "usage:")
	fmt.Fprintln(output, "  wfeature inspect <game.jar>")
	fmt.Fprintln(output, "  wfeature runskt <game.jar|game.zip> [-ticks N] [-frame out.png] [-framedir dir] [-key tick:name] [-hold N] [-save dir] [-diag report.json]")
	fmt.Fprintln(output, "                            [-screen WxH] [-cheat]")
	fmt.Fprintln(output, "  wfeature runlgt <game.zip> [-ticks N] [-frame out.png] [-framedir dir] [-key tick:name] [-hold N] [-steps N] [-save dir] [-cheat]")
	fmt.Fprintln(output, "                            [-trace N] [-trace-live filter] [-route script]")
	fmt.Fprintln(output, "                            [-profile report.txt] [-profile-folded stacks.txt] [-profile-from tick]")
	fmt.Fprintln(output, "  wfeature runktf <game.zip> [-ticks N] [-frame out.png] [-save dir] [-play] [-speed N] [-key tick:name] [-framedir dir] [-cheat] [-diag report.json] [-audio out] [-scale N] [-screen WxH]")
	fmt.Fprintln(output, "                            [-gdb host:port]")
	fmt.Fprintln(output, "                            [-profile report.txt] [-profile-folded stacks.txt] [-profile-from tick] [-route script]")
	fmt.Fprintln(output, "  wfeature invoke <game.jar> <method> <descriptor> [arguments...]")
	fmt.Fprintln(output, "  wfeature importsaves <external savedata dir> [-save dir] [-games dir] [-dry-run]")
	fmt.Fprintln(output, "  wfeature checkgames [-games dir]")
	fmt.Fprintln(output, "  wfeature provision <game.zip> [-save dir] [-number N] [-dry-run]")
	fmt.Fprintln(output, "  wfeature contactsheet <framedir> <out.png> [-every N] [-columns N] [-shrink N] [-from tick] [-to tick]")
	fmt.Fprintln(output, "  wfeature framediff <dirA> <dirB> [-limit N]")
	fmt.Fprintln(output, "  wfeature zoom <frame.png> <out.png> [-x N] [-y N] [-width N] [-height N] [-scale N]")
	fmt.Fprintln(output, "  wfeature licenses")
}

// lgtCheatTickInterval paces an interactive LGT run. It matches the session's
// own default tick so the guest clock advances at about real time.
const lgtCheatTickInterval = 50 * time.Millisecond

// runLGT loads an LGT archive, runs its Clet for a while, and optionally
// writes the frame it produced.
func runLGT(path string, args []string, stdout, stderr io.Writer) int {
	logger := backend.NewLogger(stderr)
	ticks := 64
	framePath := ""
	frameDir := ""
	saveRoot := ""
	cheatConsole := false
	ticksChosen := false
	traceSVC := 0
	traceLive := ""
	traceLiveChosen := false
	routePath := ""
	keyEvents := map[int][]int32{}
	keyHold := 1
	maxSteps := uint64(0)
	audioPrefix := ""
	profilePath := ""
	profileFoldedPath := ""
	profileFrom := 0
	for index := 0; index < len(args); index++ {
		switch args[index] {
		case "-cheat":
			cheatConsole = true
		case "-profile":
			if index+1 >= len(args) {
				fmt.Fprintln(stderr, "-profile expects a report path")
				return 2
			}
			index++
			profilePath = args[index]
		case "-profile-folded":
			if index+1 >= len(args) {
				fmt.Fprintln(stderr, "-profile-folded expects a path")
				return 2
			}
			index++
			profileFoldedPath = args[index]
		case "-profile-from":
			if index+1 >= len(args) {
				fmt.Fprintln(stderr, "-profile-from expects a tick")
				return 2
			}
			index++
			parsed, err := strconv.Atoi(args[index])
			if err != nil || parsed < 0 {
				fmt.Fprintf(stderr, "invalid profile start tick %q\n", args[index])
				return 2
			}
			profileFrom = parsed
		case "-audio":
			if index+1 >= len(args) {
				fmt.Fprintln(stderr, "-audio expects an output path prefix")
				return 2
			}
			index++
			audioPrefix = args[index]
		case "-framedir":
			if index+1 >= len(args) {
				fmt.Fprintln(stderr, "-framedir expects a directory")
				return 2
			}
			index++
			frameDir = args[index]
		case "-key":
			if index+1 >= len(args) {
				fmt.Fprintln(stderr, "-key expects <tick>:<name>")
				return 2
			}
			index++
			tick, key, err := parseKeyEvent(args[index])
			if err != nil {
				fmt.Fprintln(stderr, err)
				return 2
			}
			keyEvents[tick] = append(keyEvents[tick], key)
		case "-steps":
			if index+1 >= len(args) {
				fmt.Fprintln(stderr, "-steps expects an instruction budget")
				return 2
			}
			index++
			parsed, err := strconv.ParseUint(args[index], 10, 64)
			if err != nil || parsed == 0 {
				fmt.Fprintf(stderr, "invalid -steps %q\n", args[index])
				return 2
			}
			maxSteps = parsed
		case "-hold":
			if index+1 >= len(args) {
				fmt.Fprintln(stderr, "-hold expects a tick count")
				return 2
			}
			index++
			parsed, err := strconv.Atoi(args[index])
			if err != nil || parsed <= 0 {
				fmt.Fprintf(stderr, "invalid -hold %q\n", args[index])
				return 2
			}
			keyHold = parsed
		case "-trace":
			if index+1 >= len(args) {
				fmt.Fprintln(stderr, "-trace expects a count")
				return 2
			}
			index++
			parsed, err := strconv.Atoi(args[index])
			if err != nil || parsed <= 0 {
				fmt.Fprintf(stderr, "invalid -trace %q\n", args[index])
				return 2
			}
			traceSVC = parsed
		case "-trace-live":
			if index+1 >= len(args) {
				fmt.Fprintln(stderr, "-trace-live expects a filter; use \"\" for every call")
				return 2
			}
			index++
			traceLive = args[index]
			traceLiveChosen = true
		case "-ticks":
			if index+1 >= len(args) {
				fmt.Fprintln(stderr, "-ticks expects a count")
				return 2
			}
			index++
			parsed, err := strconv.Atoi(args[index])
			if err != nil || parsed <= 0 {
				fmt.Fprintf(stderr, "invalid -ticks %q\n", args[index])
				return 2
			}
			ticks = parsed
			ticksChosen = true
		case "-frame":
			if index+1 >= len(args) {
				fmt.Fprintln(stderr, "-frame expects a path")
				return 2
			}
			index++
			framePath = args[index]
		case "-save":
			if index+1 >= len(args) {
				fmt.Fprintln(stderr, "-save expects a directory")
				return 2
			}
			index++
			saveRoot = args[index]
		case "-route":
			if index+1 >= len(args) {
				fmt.Fprintln(stderr, "-route expects a script path")
				return 2
			}
			index++
			routePath = args[index]
		default:
			fmt.Fprintf(stderr, "unknown runlgt option %q\n", args[index])
			return 2
		}
	}
	// The script is parsed before the archive is read, so a typo is reported
	// now rather than after the minutes of guest execution it takes to reach
	// the step holding it.
	var script *route.Route
	if routePath != "" {
		text, err := os.ReadFile(routePath)
		if err != nil {
			fmt.Fprintf(stderr, "read route: %v\n", err)
			return 1
		}
		script, err = route.Parse(string(text), ktf.KeyCodeByName)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 2
		}
		if !ticksChosen {
			// A route runs until it arrives, so it does not inherit the
			// default tick count a bare run gets.
			ticks = 0
		}
	}

	data, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	archive, err := lgt.Open(data)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if saveRoot == "" {
		saveRoot = filepath.Join(platformSaveRoot("lgt"), lgt.SaveOwner(archive.Descriptor))
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	sessionOptions := lgt.SessionOptions{
		Logger:    logger,
		SaveRoot:  saveRoot,
		TraceSVC:  traceSVC,
		TraceLive: traceLive,
		MaxSteps:  maxSteps,
	}
	// The recording sink timestamps with guest time, which the session only
	// answers once it exists, so the clock is attached just after the start.
	var audioSink *backend.RecordingSink
	if audioPrefix != "" {
		audioSink = backend.NewRecordingSink(nil)
		sessionOptions.AudioSink = audioSink
	}
	if traceLiveChosen {
		// The stream goes to stderr so a run's frames, its JSON summary and
		// its trace can each be redirected on their own.
		sessionOptions.TraceOut = stderr
	}
	session, err := lgt.StartSession(ctx, data, sessionOptions)
	if err != nil {
		fmt.Fprintln(stderr, err)
		var failure *lgt.StartFailure
		if errors.As(err, &failure) && len(failure.Trace) > 0 {
			fmt.Fprintf(stderr, "\nlast %d platform calls before the failure:\n%s",
				len(failure.Trace), lgt.FormatSVCTrace(failure.Trace))
		}
		return 1
	}
	if audioSink != nil {
		audioSink.Clock = session.GuestElapsed
	}
	// Reading stdin on its own goroutine keeps the tick loop paced; the
	// commands run between ticks so a scan observes the guest at a frame
	// boundary rather than mid-instruction.
	var cheatCommands chan string
	if cheatConsole {
		cheatCommands = make(chan string, 16)
		go func() {
			lines := bufio.NewScanner(os.Stdin)
			for lines.Scan() {
				cheatCommands <- lines.Text()
			}
			close(cheatCommands)
		}()
		// A cheat session runs until it is interrupted, unless the caller
		// asked for a specific number of ticks.
		if !ticksChosen {
			ticks = 1 << 30
		}
		fmt.Fprintln(stdout, "cheat: attached. `help` for commands, ctrl-c to quit.")
	}
	// A scripted key is pressed on its tick and released -hold ticks later. A
	// game that samples the keypad once a frame never sees a press and release
	// delivered in the same tick, so the release is scheduled rather than
	// queued behind the press.
	keyReleases := map[int][]int32{}
	ran := 0
	dumpedFlush := uint64(0)
	busy := time.Duration(0)
	slowestTick, slowestTickCost := 0, time.Duration(0)
	profiling := profilePath != "" || profileFoldedPath != ""
	if profiling {
		session.EnableProfile(0)
	}
	stopped := false
	// One tick, whatever is driving it. A plain run counts them off against
	// -ticks and a route asks for them a step at a time, and both have to dump
	// frames, deliver scheduled keys and read the cheat console the same way —
	// so the tick is written once and the two callers differ only in when they
	// ask for it.
	advance := func(ctx context.Context) (bool, error) {
		if ctx.Err() != nil {
			stopped = true
			return false, nil
		}
		if frameDir != "" {
			if flushes := session.Flushes(); flushes != dumpedFlush {
				dumpedFlush = flushes
				frame, width, height, _ := session.Frame()
				if width > 0 && height > 0 {
					name := filepath.Join(frameDir, fmt.Sprintf("tick%04d.png", ran))
					if err := writePNG(name, frame, width, height); err != nil {
						return false, fmt.Errorf("write frame: %w", err)
					}
				}
			}
		}
		for _, key := range keyEvents[ran] {
			session.SendKey(true, uint32(key))
			keyReleases[ran+keyHold] = append(keyReleases[ran+keyHold], key)
		}
		for _, key := range keyReleases[ran] {
			session.SendKey(false, uint32(key))
		}
		delete(keyReleases, ran)
		if cheatConsole {
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
		// A title spends its first thousands of ticks loading, and that would
		// dominate a profile of the scene actually being investigated.
		// -profile-from throws those samples away on arrival.
		if profiling && profileFrom > 0 && ran == profileFrom {
			session.ResetProfile()
		}
		tickStarted := time.Now()
		tickErr := session.Tick(ctx)
		if cost := time.Since(tickStarted); cost > slowestTickCost {
			slowestTickCost, slowestTick = cost, ran
		}
		busy += time.Since(tickStarted)
		ran++
		if tickErr != nil {
			// The tick did not run, so it did not progress; stopped is what
			// tells both callers the run is over.
			stopped = true
			if errors.Is(tickErr, lgt.ErrGuestExited) {
				return false, nil
			}
			fmt.Fprintf(stderr, "tick %d: %v\n", ran-1, tickErr)
			if trace := session.SVCTrace(); len(trace) > 0 {
				fmt.Fprintf(stderr, "\nlast %d platform calls before the failure:\n%s",
					len(trace), lgt.FormatSVCTrace(trace))
			}
			return false, nil
		}
		if cheatConsole {
			// An interactive run is held to the speed the game was written
			// for. Without the wait the tick budget is spent before a typed
			// command has been read, and the console never gets a turn.
			select {
			case <-ctx.Done():
			case <-time.After(lgtCheatTickInterval):
			}
		}
		return true, nil
	}
	var routeResult route.Result
	if script != nil {
		runner := &route.Runner{
			MaxTicks: ticks,
			Hold:     keyHold,
			Digest:   session.FrameDigest,
			Advance:  advance,
			SendKey: func(_ context.Context, pressed bool, key int32) error {
				session.SendKey(pressed, uint32(key))
				return nil
			},
			// A tick that failed, or a guest that exited, is the end of the
			// run whatever step the route was on: without this the route
			// keeps asking a dead session for ticks until its budget is
			// gone, and reports the budget instead of the failure.
			Stalled: func() bool { return stopped },
			Checkpoint: func(label string, _ int, _ bool) error {
				if frameDir == "" {
					return nil
				}
				frame, width, height, _ := session.Frame()
				if width <= 0 || height <= 0 {
					return nil
				}
				return writePNG(filepath.Join(frameDir, label+".png"), frame, width, height)
			},
		}
		result, err := runner.Run(ctx, script)
		routeResult = result
		if err != nil {
			fmt.Fprintf(stderr, "route: %v\n", err)
			return 1
		}
		if !result.Completed {
			fmt.Fprintf(stderr, "route stopped at step %d (%s)\n", result.StoppedAt+1, result.Reason)
		}
	} else {
		for !stopped && ran < ticks {
			if _, err := advance(ctx); err != nil {
				fmt.Fprintln(stderr, err)
				return 1
			}
		}
	}
	frame, width, height, _ := session.Frame()
	if framePath != "" && width > 0 && height > 0 {
		if err := writePNG(framePath, frame, width, height); err != nil {
			fmt.Fprintf(stderr, "write frame: %v\n", err)
			return 1
		}
	}
	// A run that ends without failing is worth reading too: a title that
	// reaches a screen it should not have is answering a call wrongly
	// somewhere, and the ring is the only record of what it asked.
	if traceSVC > 0 {
		if trace := session.SVCTrace(); len(trace) > 0 {
			fmt.Fprintf(stderr, "\nlast %d platform calls:\n%s",
				len(trace), lgt.FormatSVCTrace(trace))
		}
	}
	summary := map[string]any{
		"aid":     archive.Descriptor.AID,
		"pid":     archive.Descriptor.PID,
		"mclass":  archive.Descriptor.MClass,
		"ticks":   ran,
		"flushes": session.Flushes(),
		"width":   width,
		"height":  height,
		// What the run cost the host, and where its worst moment was. A world
		// load happens inside one tick, so an average hides it entirely and
		// the slowest tick is the load.
		"busy_ms":         busy.Milliseconds(),
		"slowest_tick":    slowestTick,
		"slowest_tick_ms": slowestTickCost.Milliseconds(),
		// Steps and the host nanoseconds each one cost. A throughput change
		// moves `ns_per_step` and nothing else here reliably does: busy time
		// alone moves when a change alters how much guest work a tick holds,
		// which is exactly what a scene reached by a route does from run to
		// run.
		"steps":       session.Steps(),
		"ns_per_step": float64(busy.Nanoseconds()) / float64(max(session.Steps(), 1)),
		// The guest's own clock. A tick here stands for the time until the
		// guest's next scheduled work rather than a fixed span, so the tick
		// count no longer says how much guest time a run covered — and guest
		// time against frames is what says the rate a title is being given.
		"guest_ms": session.GuestElapsed().Milliseconds(),
	}
	if script != nil {
		summary["route_completed"] = routeResult.Completed
		summary["route_marks"] = routeResult.Marks
		if !routeResult.Completed {
			summary["route_stopped_at"] = routeResult.StoppedAt + 1
			summary["route_reason"] = routeResult.Reason
		}
	}
	if profiling {
		profile := session.Profile()
		if profilePath != "" {
			if err := os.WriteFile(profilePath, []byte(profile.Report(30)), 0o644); err != nil {
				fmt.Fprintf(stderr, "write profile: %v\n", err)
				return 1
			}
			summary["profile"] = profilePath
		}
		if profileFoldedPath != "" {
			if err := os.WriteFile(profileFoldedPath, []byte(profile.Folded()), 0o644); err != nil {
				fmt.Fprintf(stderr, "write folded profile: %v\n", err)
				return 1
			}
			summary["profile_folded"] = profileFoldedPath
		}
		summary["profile_samples"] = profile.Samples
		summary["profile_steps"] = profile.Steps
	}
	if audioSink != nil {
		messages, samples := audioSink.Summary()
		summary["audio_midi_messages"] = messages
		summary["audio_wave_samples"] = samples
		written, err := audioSink.Write(audioPrefix)
		if err != nil {
			fmt.Fprintf(stderr, "write audio: %v\n", err)
			return 1
		}
		summary["audio"] = written
	}
	encoder := json.NewEncoder(stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(summary); err != nil {
		fmt.Fprintf(stderr, "write result: %v\n", err)
		return 1
	}
	return 0
}

// provision writes the certificate one title will not start without.
//
// The title checks a certificate a server issued to a handset, once, years
// ago; see internal/platform/ktf/provision.go for the check and for what this
// is. It is a deliberate act, so it is a command a person runs rather than
// anything the emulator does on its own, and it says exactly what it wrote.
func provision(path string, extra []string, stdout, stderr io.Writer) int {
	saveRoot := defaultSaveRoot()
	number := ""
	dryRun := false
	for index := 0; index < len(extra); index++ {
		switch extra[index] {
		case "-save":
			if index+1 >= len(extra) {
				printUsage(stderr)
				return 2
			}
			index++
			saveRoot = extra[index]
		case "-number":
			if index+1 >= len(extra) {
				printUsage(stderr)
				return 2
			}
			index++
			number = extra[index]
		case "-dry-run":
			dryRun = true
		default:
			fmt.Fprintf(stderr, "unknown provision option %q\n", extra[index])
			return 2
		}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	archive, err := ktf.Open(data)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	certificate, err := ktf.ProvisionCertificate(archive, number)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	owner := ktf.SaveOwner(archive.Descriptor)
	store := backend.NewDirectorySaveStore(filepath.Join(saveRoot, owner))
	fmt.Fprintf(stdout, "%s (%s): certificate for handset %s -> %s/%s/%s\n",
		archive.Descriptor.AID, owner, certificate.Number, saveRoot, owner, certificate.SaveKey)
	if dryRun {
		fmt.Fprintln(stdout, "dry run: nothing written")
		return 0
	}
	if err := store.StoreSave(certificate.SaveKey, certificate.Data); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	// A run that rejected the packaged certificate deleted it, and that
	// deletion is remembered. Clearing the list is what lets the entry just
	// written be read at all.
	if removed, exists := store.LoadSave(ktf.CertificateRemovalKey); exists && len(removed) > 0 {
		kept := make([]string, 0)
		for _, line := range strings.Split(string(removed), "\n") {
			if name := strings.TrimSpace(line); name != "" && name != "cert.c2s" {
				kept = append(kept, name)
			}
		}
		if err := store.StoreSave(ktf.CertificateRemovalKey, []byte(strings.Join(kept, "\n"))); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		fmt.Fprintln(stdout, "cleared the earlier deletion of cert.c2s")
	}
	return 0
}
