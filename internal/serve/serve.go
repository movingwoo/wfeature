// Package serve drives a running game one command at a time, over a pipe.
//
// A route is a script: it is written before the run and it replays the same
// way every time, which is what makes it a repro. What it cannot do is look at
// the screen and decide the next move — the decision was taken when the file
// was written. Investigating a title nobody has a route for is the other
// shape: press something, look, press something else, and only once the way
// through is known does it get written down as a route.
//
// That loop needed a person watching a window. This is the same loop with the
// screen answered as a number, so whatever is driving the investigation can be
// a program: one JSON command per line in, one JSON answer per line out.
//
//	route = replay, serve = explore.
//
// The two share everything below them. A serve session ticks the same session
// the rest of the subcommand does, sends keys through the same table `-key` and
// a route script read, and can run a route script itself.
package serve

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/movingwoo/wfeature/internal/route"
)

// maxCommandLine bounds one input line. Everything this protocol accepts is
// short — a key name, a path, a few numbers — so a line past this is a
// desynchronized stream rather than a command, and there is no way to resume
// reading in the middle of one. It is the only input that ends the session:
// every other bad command is answered and the session carries on.
const maxCommandLine = 1 << 20

// maxStepTicks bounds one step. A step is how a caller spends guest time on
// purpose; a step of a hundred million is a typo, and the session would be
// unreachable until it finished.
const maxStepTicks = 1_000_000

// Request is one command line.
type Request struct {
	// ID is echoed back on the answer, whatever it is. A caller that pipelines
	// commands needs to pair them up, and the protocol has no opinion about
	// how it numbers them.
	ID  json.RawMessage `json:"id,omitempty"`
	Cmd string          `json:"cmd"`

	// Ticks is how far `step` advances. Zero means one tick.
	Ticks int `json:"ticks,omitempty"`
	// Screen asks `step` to include the full screen report. It is opt-in
	// because the report hashes the whole frame, and a caller stepping a tick
	// at a time usually only needs the cheap digest.
	Screen bool `json:"screen,omitempty"`

	// Key is a name from the same table `-key` and a route script read.
	Key string `json:"key,omitempty"`
	// Hold is how many ticks `key` holds the press before releasing it. A
	// press and its release in the same tick is not a press to a title that
	// samples the keypad once a frame, which reads exactly like a title that
	// has stopped.
	Hold int `json:"hold,omitempty"`
	// Repeat is how many times `key` presses. Zero means once.
	Repeat int `json:"repeat,omitempty"`
	// Action is `press`, `release` or `tap` for `key` — a hold that has to
	// span other commands needs its two halves separately — and `press`,
	// `drag` or `release` for `touch`.
	Action string `json:"action,omitempty"`

	// X and Y are the guest's own screen coordinates, for `touch` and `pixel`.
	X int `json:"x,omitempty"`
	Y int `json:"y,omitempty"`

	// MS is how long `park` leaves the game parked.
	MS int `json:"ms,omitempty"`

	// Path is where `shot` and `diag` write, and which script `route` reads.
	Path string `json:"path,omitempty"`
}

// Response is one answer line.
type Response struct {
	ID json.RawMessage `json:"id,omitempty"`
	OK bool            `json:"ok"`
	// Error says what went wrong with the command. It is reported here rather
	// than by ending the session: an exploratory caller mistypes a key name
	// and needs the run it spent minutes reaching to still be there.
	Error string `json:"error,omitempty"`
	Cmd   string `json:"cmd,omitempty"`

	// Ticks is what this command spent; Total is what the serve session has
	// spent altogether.
	Ticks int `json:"ticks,omitempty"`
	Total int `json:"total_ticks"`
	// Progressed says the last tick ran guest work, and Stalled says nothing
	// is left that would: no deadline is due and nothing is parked, so further
	// ticks cannot change anything.
	Progressed bool `json:"progressed,omitempty"`
	Stalled    bool `json:"stalled,omitempty"`
	// Digest identifies the picture cheaply, which is all a caller needs to
	// ask "did anything change". It is the same digest a route waits on.
	Digest string `json:"digest,omitempty"`

	// Ended and EndReason report a guest that has stopped. The session stays
	// open afterwards, because a screen, a shot and a diagnostics report are
	// exactly what is wanted about a run that just ended.
	Ended     bool   `json:"ended,omitempty"`
	EndReason string `json:"end_reason,omitempty"`

	Screen *ScreenReport `json:"screen,omitempty"`
	Pixel  *PixelReport  `json:"pixel,omitempty"`
	Route  *RouteReport  `json:"route,omitempty"`
	Path   string        `json:"path,omitempty"`
}

// ScreenReport identifies what is on the screen.
//
// The hash is the point. A PNG of a frame is not an identity — the encoder
// chooses filters and a compression level, and two runs that drew the same
// picture can write different files — so a regression assertion written
// against a screenshot is asserting something about the encoder as well as
// about the game. This hashes the pixels: the geometry as two little-endian
// 64-bit words, then the normalized RGBA bytes row by row. A fully transparent
// pixel contributes four zero bytes whatever colour was left under it, because
// what is under a transparent pixel is not part of the picture.
type ScreenReport struct {
	Width  int `json:"width"`
	Height int `json:"height"`
	// RGBASHA256 is that hash, in lower-case hex.
	RGBASHA256 string `json:"rgba_sha256"`
	// NonBlackPixels is how much of the screen is neither transparent nor
	// black, which is the signal that separates a title that painted
	// something from one that painted nothing.
	NonBlackPixels uint64 `json:"non_black_pixels"`
	// VisiblePixels is how much of it is not transparent.
	VisiblePixels uint64 `json:"visible_pixels"`
	// Flushes is how many times the guest has presented the screen.
	Flushes uint64 `json:"flushes,omitempty"`
	// Digest is the cheap identity, carried beside the hash so a caller can
	// use one report for both questions.
	Digest string `json:"digest"`
}

// PixelReport is one pixel in the guest's own coordinates.
type PixelReport struct {
	X    int    `json:"x"`
	Y    int    `json:"y"`
	R    uint8  `json:"r"`
	G    uint8  `json:"g"`
	B    uint8  `json:"b"`
	A    uint8  `json:"a"`
	RGBA string `json:"rgba"`
}

// RouteReport is what a replayed script reached.
type RouteReport struct {
	Completed bool `json:"completed"`
	Ticks     int  `json:"ticks"`
	// StoppedAt is the one-based step that did not finish, and Reason says
	// why. A route that goes somewhere else is an answer rather than a crash.
	StoppedAt int         `json:"stopped_at,omitempty"`
	Reason    string      `json:"reason,omitempty"`
	Marks     []MarkPoint `json:"marks,omitempty"`
}

// MarkPoint is where a checkpoint fell.
type MarkPoint struct {
	Label string `json:"label"`
	Tick  int    `json:"tick"`
}

// Driver is what a serve session needs of a platform.
//
// It arrives as functions rather than as an interface a session implements,
// for the reason the route runner takes the same shape: the platforms spell
// their sessions differently — one takes a key event type, another a pressed
// flag, a third has no pointer at all — and none of that is a property of the
// protocol. A capability a platform does not have is left nil and the command
// that needs it answers so by name.
type Driver struct {
	// Advance performs one tick with the Host's own pacing and reports whether
	// the guest made progress. The subcommand keeps its own pacing that way: a
	// probe jumps its clock, an interactive run waits the interval out.
	Advance func(ctx context.Context) (bool, error)
	// Frame is the screen as RGBA in the guest's own size.
	Frame func() (rgba []byte, width, height int)
	// Digest identifies the picture cheaply. It is the same one a route waits
	// on, so a serve caller and a route agree about what "changed" means.
	Digest func() uint64
	// Flushes is how many times the guest has presented the screen. Optional.
	Flushes func() uint64
	// LookupKey resolves a key name against the platform's codes, from the
	// same table `-key` and a route script read.
	LookupKey route.LookupKey
	// SendKey delivers one half of a key event.
	SendKey func(ctx context.Context, pressed bool, key int32) error
	// Stalled reports that a tick which made no progress will never be
	// followed by one that does. A platform that cannot tell leaves it nil.
	Stalled func() bool
	// SendTouch delivers one pointer event; nil on a platform with no pointer.
	SendTouch func(ctx context.Context, action string, x, y int) error
	// Park runs the lifecycle a Host runs when the page goes away and comes
	// back, holding the game parked in between; nil where there is none.
	Park func(ctx context.Context, hold time.Duration) error
	// Diag writes the runtime-boundary diagnostics; nil where there are none.
	Diag func(path string) error
	// Shot writes the current frame as a PNG, through whatever the subcommand
	// already does that with, so a serve capture and a `-frame` capture are
	// the same file.
	Shot func(path string) error
	// RunRoute replays a parsed script against the session.
	RunRoute func(ctx context.Context, script *route.Route) (route.Result, error)
	// DefaultHold is the subcommand's `-hold`, used when a `key` command does
	// not name one.
	DefaultHold int
}

// session is the mutable half: what the run has spent and how it ended.
type session struct {
	driver *Driver
	total  int
	ended  error
}

// Serve reads commands until the stream ends, `quit` arrives, or a line
// arrives that is too long to be one. Everything else — an unknown command, a
// key name that is not in the table, a path that cannot be written — is
// answered in band and the run stays alive.
func Serve(ctx context.Context, driver *Driver, in io.Reader, out io.Writer) error {
	if driver == nil || driver.Advance == nil || driver.Frame == nil || driver.Digest == nil {
		return errors.New("a serve session needs a way to advance the run and read its screen")
	}
	state := &session{driver: driver}
	reader := bufio.NewScanner(in)
	reader.Buffer(make([]byte, 0, 64*1024), maxCommandLine)
	writer := bufio.NewWriter(out)
	encoder := json.NewEncoder(writer)

	answer := func(response Response) error {
		response.Total = state.total
		if err := encoder.Encode(response); err != nil {
			return err
		}
		return writer.Flush()
	}

	for reader.Scan() {
		line := strings.TrimSpace(reader.Text())
		if line == "" {
			continue
		}
		var request Request
		if err := json.Unmarshal([]byte(line), &request); err != nil {
			// The id lived in the line that did not parse, so there is none to
			// echo. Saying that plainly beats inventing one.
			if err := answer(Response{OK: false, Error: fmt.Sprintf("this line is not a command object: %v", err)}); err != nil {
				return err
			}
			continue
		}
		if strings.EqualFold(request.Cmd, "quit") {
			return answer(Response{ID: request.ID, OK: true, Cmd: "quit"})
		}
		response := state.run(ctx, request)
		response.ID, response.Cmd = request.ID, request.Cmd
		if err := answer(response); err != nil {
			return err
		}
		if ctx.Err() != nil {
			return nil
		}
	}
	if err := reader.Err(); err != nil {
		if errors.Is(err, bufio.ErrTooLong) {
			_ = answer(Response{OK: false, Error: fmt.Sprintf("a command line was longer than %d bytes, so the stream cannot be resynchronized", maxCommandLine)})
			return fmt.Errorf("serve: a command line was longer than %d bytes", maxCommandLine)
		}
		return err
	}
	return nil
}

func (state *session) run(ctx context.Context, request Request) Response {
	switch strings.ToLower(strings.TrimSpace(request.Cmd)) {
	case "":
		return failure("a command object needs a `cmd`")
	case "step":
		return state.step(ctx, request)
	case "key":
		return state.key(ctx, request)
	case "touch":
		return state.touch(ctx, request)
	case "park":
		return state.park(ctx, request)
	case "screen":
		return state.screen()
	case "pixel":
		return state.pixel(request)
	case "diag":
		return state.write(request, state.driver.Diag, "diagnostics", "-diag")
	case "shot":
		return state.write(request, state.driver.Shot, "frame", "-frame")
	case "route":
		return state.route(ctx, request)
	}
	return failure("unknown command %q; the commands are step, key, touch, park, screen, pixel, diag, shot, route and quit", request.Cmd)
}

func failure(format string, arguments ...any) Response {
	return Response{OK: false, Error: fmt.Sprintf(format, arguments...)}
}

// step advances the run. It stops early on a guest that ended, on a tick that
// failed, and on a tick that did nothing when nothing is left to run — the
// last is what keeps a step of ten thousand from spending ten thousand ticks
// on a session that is already over.
func (state *session) step(ctx context.Context, request Request) Response {
	ticks := request.Ticks
	if ticks == 0 {
		ticks = 1
	}
	if ticks < 0 || ticks > maxStepTicks {
		return failure("step takes 1 to %d ticks, not %d", maxStepTicks, ticks)
	}
	if state.ended != nil {
		return state.endedResponse()
	}
	spent, progressed, stalled, err := state.advance(ctx, ticks)
	response := Response{
		OK: err == nil, Ticks: spent, Progressed: progressed, Stalled: stalled,
		Digest: formatDigest(state.driver.Digest()),
	}
	if err != nil {
		state.ended = err
		response.OK, response.Ended, response.EndReason = true, true, err.Error()
	}
	if request.Screen {
		report, screenErr := state.screenReport()
		if screenErr != nil {
			return failure("%v", screenErr)
		}
		response.Screen = &report
	}
	return response
}

// advance runs up to ticks ticks, reporting what it spent.
func (state *session) advance(ctx context.Context, ticks int) (spent int, progressed, stalled bool, err error) {
	for ; spent < ticks; spent++ {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return spent, progressed, stalled, nil
		}
		progressed, err = state.driver.Advance(ctx)
		state.total++
		if err != nil {
			return spent + 1, progressed, stalled, err
		}
		if !progressed && state.driver.Stalled != nil && state.driver.Stalled() {
			return spent + 1, progressed, true, nil
		}
	}
	return spent, progressed, stalled, nil
}

func (state *session) endedResponse() Response {
	return Response{
		OK: true, Ended: true, EndReason: state.ended.Error(),
		Digest: formatDigest(state.driver.Digest()),
	}
}

// key presses a key from the shared table. `tap` holds the press for the
// requested number of ticks and those ticks are the caller's, the way a
// route's are; `press` and `release` are the two halves on their own, for a
// hold that has to span other commands.
func (state *session) key(ctx context.Context, request Request) Response {
	if state.driver.LookupKey == nil || state.driver.SendKey == nil {
		return failure("this platform takes no keys")
	}
	name := strings.TrimSpace(request.Key)
	if name == "" {
		return failure("key expects a key name")
	}
	code, ok := state.driver.LookupKey(name)
	if !ok {
		return failure("unknown key %q", name)
	}
	action := strings.ToLower(strings.TrimSpace(request.Action))
	if action == "" {
		action = "tap"
	}
	repeat := request.Repeat
	if repeat == 0 {
		repeat = 1
	}
	if repeat < 1 || repeat > 1000 {
		return failure("key repeat must be between 1 and 1000, not %d", repeat)
	}
	hold := request.Hold
	if hold == 0 {
		hold = state.driver.DefaultHold
	}
	if hold < 0 || hold > maxStepTicks {
		return failure("key hold must be between 0 and %d ticks, not %d", maxStepTicks, hold)
	}
	if state.ended != nil && action != "release" {
		return state.endedResponse()
	}

	spent := 0
	switch action {
	case "press", "release":
		pressed := action == "press"
		for index := 0; index < repeat; index++ {
			if err := state.driver.SendKey(ctx, pressed, code); err != nil {
				return failure("send %s %s: %v", action, name, err)
			}
		}
	case "tap":
		for index := 0; index < repeat; index++ {
			if err := state.driver.SendKey(ctx, true, code); err != nil {
				return failure("press %s: %v", name, err)
			}
			if hold > 0 {
				held, _, _, err := state.advance(ctx, hold)
				spent += held
				if err != nil {
					state.ended = err
					// The release still goes out: a guest that ended mid-hold
					// should not be left holding a key down.
					_ = state.driver.SendKey(ctx, false, code)
					return Response{OK: true, Ticks: spent, Ended: true, EndReason: err.Error(),
						Digest: formatDigest(state.driver.Digest())}
				}
			}
			if err := state.driver.SendKey(ctx, false, code); err != nil {
				return failure("release %s: %v", name, err)
			}
		}
	default:
		return failure("key action %q is not press, release or tap", request.Action)
	}
	return Response{OK: true, Ticks: spent, Digest: formatDigest(state.driver.Digest())}
}

func (state *session) touch(ctx context.Context, request Request) Response {
	if state.driver.SendTouch == nil {
		return failure("this platform has no pointer")
	}
	action := strings.ToLower(strings.TrimSpace(request.Action))
	switch action {
	case "press", "drag", "release":
	default:
		return failure("touch action %q is not press, drag or release", request.Action)
	}
	if err := state.driver.SendTouch(ctx, action, request.X, request.Y); err != nil {
		return failure("touch: %v", err)
	}
	return Response{OK: true, Digest: formatDigest(state.driver.Digest())}
}

func (state *session) park(ctx context.Context, request Request) Response {
	if state.driver.Park == nil {
		return failure("this platform has no park to run")
	}
	if request.MS < 0 || request.MS > 10*60*1000 {
		return failure("park holds for 0 to 600000 ms, not %d", request.MS)
	}
	if err := state.driver.Park(ctx, time.Duration(request.MS)*time.Millisecond); err != nil {
		return failure("park: %v", err)
	}
	return Response{OK: true, Digest: formatDigest(state.driver.Digest())}
}

func (state *session) screen() Response {
	report, err := state.screenReport()
	if err != nil {
		return failure("%v", err)
	}
	return Response{OK: true, Screen: &report, Digest: report.Digest}
}

func (state *session) screenReport() (ScreenReport, error) {
	rgba, width, height := state.driver.Frame()
	flushes := uint64(0)
	if state.driver.Flushes != nil {
		flushes = state.driver.Flushes()
	}
	report, err := Screen(rgba, width, height, flushes)
	if err != nil {
		return ScreenReport{}, err
	}
	report.Digest = formatDigest(state.driver.Digest())
	return report, nil
}

func (state *session) pixel(request Request) Response {
	rgba, width, height := state.driver.Frame()
	if width <= 0 || height <= 0 {
		return failure("nothing has been drawn yet, so there is no pixel to read")
	}
	if request.X < 0 || request.X >= width || request.Y < 0 || request.Y >= height {
		return failure("(%d,%d) is outside the %dx%d screen", request.X, request.Y, width, height)
	}
	offset := (request.Y*width + request.X) * 4
	if offset+4 > len(rgba) {
		return failure("the frame buffer is smaller than %dx%d", width, height)
	}
	red, green, blue, alpha := rgba[offset], rgba[offset+1], rgba[offset+2], rgba[offset+3]
	return Response{OK: true, Pixel: &PixelReport{
		X: request.X, Y: request.Y, R: red, G: green, B: blue, A: alpha,
		RGBA: fmt.Sprintf("#%02x%02x%02x%02x", red, green, blue, alpha),
	}}
}

// write is `shot` and `diag`: both take a path, both are a capability a
// platform may not have, and both answer with the path they wrote.
func (state *session) write(request Request, writeTo func(string) error, what, flag string) Response {
	if writeTo == nil {
		return failure("this platform writes no %s, the way %s does not apply to it", what, flag)
	}
	path := strings.TrimSpace(request.Path)
	if path == "" {
		return failure("%s expects a path", what)
	}
	if err := writeTo(path); err != nil {
		return failure("write %s: %v", what, err)
	}
	return Response{OK: true, Path: path}
}

// route replays a script from here. Exploring is how a route gets written, and
// replaying one is how the exploration resumes from where it left off, so both
// belong on the same connection.
func (state *session) route(ctx context.Context, request Request) Response {
	if state.driver.RunRoute == nil || state.driver.LookupKey == nil {
		return failure("this platform cannot replay a route")
	}
	path := strings.TrimSpace(request.Path)
	if path == "" {
		return failure("route expects a script path")
	}
	if state.ended != nil {
		return state.endedResponse()
	}
	text, err := os.ReadFile(path)
	if err != nil {
		return failure("read route: %v", err)
	}
	script, err := route.Parse(string(text), state.driver.LookupKey)
	if err != nil {
		return failure("%v", err)
	}
	result, runErr := state.driver.RunRoute(ctx, script)
	state.total += result.Ticks
	report := RouteReport{Completed: result.Completed, Ticks: result.Ticks, Reason: result.Reason}
	if !result.Completed {
		report.StoppedAt = result.StoppedAt + 1
	}
	for _, mark := range result.Marks {
		report.Marks = append(report.Marks, MarkPoint{Label: mark.Label, Tick: mark.Tick})
	}
	response := Response{OK: true, Ticks: result.Ticks, Route: &report, Digest: formatDigest(state.driver.Digest())}
	if runErr != nil {
		state.ended = runErr
		response.Ended, response.EndReason = true, runErr.Error()
	}
	return response
}

// Screen builds the report for one frame. It is exported because it is the
// identity a regression assertion is written against, and whoever writes one
// should be able to compute the same number from a captured buffer.
func Screen(rgba []byte, width, height int, flushes uint64) (ScreenReport, error) {
	report := ScreenReport{Width: max(width, 0), Height: max(height, 0), Flushes: flushes}
	pixels := report.Width * report.Height
	if pixels > 0 && len(rgba) < pixels*4 {
		return ScreenReport{}, fmt.Errorf("the frame buffer is smaller than %dx%d", width, height)
	}
	// The geometry goes in first, so two runs that drew the same bytes into
	// different-shaped screens are not the same screen.
	var geometry [16]byte
	binary.LittleEndian.PutUint64(geometry[0:8], uint64(report.Width))
	binary.LittleEndian.PutUint64(geometry[8:16], uint64(report.Height))
	digest := sha256.New()
	digest.Write(geometry[:])

	normalized := make([]byte, pixels*4)
	for index := 0; index < pixels; index++ {
		offset := index * 4
		if rgba[offset+3] == 0 {
			// What is under a transparent pixel is whatever the surface last
			// held there, which is not part of the picture and must not be
			// part of its identity.
			continue
		}
		copy(normalized[offset:offset+4], rgba[offset:offset+4])
		report.VisiblePixels++
		if rgba[offset]|rgba[offset+1]|rgba[offset+2] != 0 {
			report.NonBlackPixels++
		}
	}
	digest.Write(normalized)
	report.RGBASHA256 = hex.EncodeToString(digest.Sum(nil))
	return report, nil
}

// formatDigest writes the frame digest as fixed-width hex. It is a string
// rather than a number because a 64-bit value does not survive a JSON reader
// that keeps numbers as doubles, and a digest that quietly loses its low bits
// is worse than no digest.
func formatDigest(value uint64) string { return fmt.Sprintf("%016x", value) }
