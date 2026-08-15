// Package route reads and replays a scripted way back to a scene in a running
// game. It belongs to no platform: what it needs of a session arrives as
// functions and a key table, because the platform sessions spell those
// differently while a route reads the same either way.
package route

import (
	"context"
	"fmt"
	"strconv"
	"strings"
)

// A route is a scripted way back to a scene. Some of what these games do only
// happens well inside a save — a battle, a shop, the frame rate under a
// special attack — and none of it is reachable from a fresh boot without
// playing there. Writing the route down makes the scene a thing that can be
// re-entered on demand, by a profile run, by a regression check, or by whoever
// is looking at a reported bug.
//
// Absolute tick numbers are what a route must not be built from. They are
// exactly what breaks: fixing the type check that made enemies stand still
// changed which branch the game took, and every hand-tuned key timing written
// before it stopped landing on the menu it was aimed at. So a route waits on
// what the screen is doing instead — steady, or changed — and only falls back
// to counting ticks where nothing better is available.

// Action is what one step of a route does.
type Action uint8

const (
	// Wait advances a fixed number of ticks.
	Wait Action = iota
	// WaitIdle advances until the screen has been unchanged for Count
	// consecutive ticks — the game has finished whatever it was animating and
	// is waiting for input — giving up after Limit ticks.
	WaitIdle
	// WaitChange advances until the screen differs from what it was when
	// the step began.
	WaitChange
	// Key presses and releases a key Count times.
	Key
	// Press and Release send one half of a key event, for the games
	// that act on a held key.
	Press
	Release
	// Mark is a checkpoint: the Host is told the route arrived somewhere
	// worth naming, which is where a profile is reset and a frame captured.
	Mark
	// Shot asks the Host to capture a frame without resetting anything.
	Shot
)

// Step is one line of a route.
type Step struct {
	Action Action
	Key    int32
	Count  int
	// Limit bounds a wait that is looking for something rather than counting:
	// a screen that settles, a screen that changes. Every such wait is bounded,
	// because an unbounded one silently eats the whole run's tick budget and
	// then reports the step after it as the failure.
	Limit int
	Label string
	Line  int
}

// Route is a parsed script.
type Route struct {
	Steps []Step
}

// MarkPoint records where a checkpoint fell.
type MarkPoint struct {
	Label string
	Tick  int
}

// Result reports what the run reached.
type Result struct {
	Ticks     int
	Marks     []MarkPoint
	Completed bool
	// StoppedAt is the index of the step that did not finish, and Reason says
	// why. A route that ran out of ticks in its first wait has gone somewhere
	// different from where it was written, which is worth saying plainly
	// rather than reporting as a run that simply ended.
	StoppedAt int
	Reason    string
}

// LookupKey resolves a key name to the code the platform's games compare
// against. The names are shared — a route reads the same way whichever
// platform it drives — and only the codes behind them belong to a platform.
type LookupKey func(name string) (int32, bool)

// Parse reads a route script. Blank lines and everything from a `#` are
// comments, except where the `#` is the key name a key verb is taking: it is a
// key on the handset as well as the comment marker, and cutting the line at
// the character left `key #` unwritable — the one key the table could name but
// a route could never press.
func Parse(text string, lookupKey LookupKey) (*Route, error) {
	if lookupKey == nil {
		return nil, fmt.Errorf("route parsing needs a key table")
	}
	route := &Route{}
	for number, line := range strings.Split(text, "\n") {
		number++
		fields := strings.Fields(line)
		for index, field := range fields {
			if field == "#" && index == 1 && isKeyVerb(fields[0]) {
				continue
			}
			cut := strings.IndexByte(field, '#')
			if cut < 0 {
				continue
			}
			if cut > 0 {
				fields = append(fields[:index], field[:cut])
			} else {
				fields = fields[:index]
			}
			break
		}
		if len(fields) == 0 {
			continue
		}
		step, err := parseStep(fields, lookupKey)
		if err != nil {
			return nil, fmt.Errorf("route line %d: %w", number, err)
		}
		step.Line = number
		route.Steps = append(route.Steps, step)
	}
	if len(route.Steps) == 0 {
		return nil, fmt.Errorf("route has no steps")
	}
	return route, nil
}

// defaultWaitLimitFactor is how much longer than the settling run a wait-idle
// will keep looking before giving up. A screen that has not settled in twenty
// times the run asked for is animating, not settling.
const defaultWaitLimitFactor = 20

func parseCount(what, text string) (int, error) {
	count, err := strconv.Atoi(text)
	if err != nil || count <= 0 {
		return 0, fmt.Errorf("%s %q is not a positive number", what, text)
	}
	return count, nil
}

// isKeyVerb reports whether a word takes a key name as its next argument.
func isKeyVerb(field string) bool {
	switch strings.ToLower(field) {
	case "key", "press", "release":
		return true
	}
	return false
}

func parseStep(fields []string, lookupKey LookupKey) (Step, error) {
	step := Step{Count: 1}
	switch strings.ToLower(fields[0]) {
	case "wait":
		if len(fields) != 2 {
			return step, fmt.Errorf("wait expects a tick count")
		}
		count, err := parseCount(fields[0], fields[1])
		if err != nil {
			return step, err
		}
		step.Action, step.Count = Wait, count
		return step, nil
	case "wait-idle", "wait-change":
		if len(fields) < 2 || len(fields) > 3 {
			return step, fmt.Errorf("%s expects a tick count and an optional limit", fields[0])
		}
		count, err := parseCount(fields[0], fields[1])
		if err != nil {
			return step, err
		}
		step.Count = count
		step.Limit = count * defaultWaitLimitFactor
		if len(fields) == 3 {
			limit, err := parseCount(fields[0]+" limit", fields[2])
			if err != nil {
				return step, err
			}
			step.Limit = limit
		}
		if step.Limit < step.Count {
			return step, fmt.Errorf("%s limit %d is below its tick count %d", fields[0], step.Limit, step.Count)
		}
		if strings.EqualFold(fields[0], "wait-idle") {
			step.Action = WaitIdle
		} else {
			step.Action = WaitChange
		}
		return step, nil
	case "key", "press", "release":
		if len(fields) < 2 || len(fields) > 3 {
			return step, fmt.Errorf("%s expects a key name and an optional repeat count", fields[0])
		}
		key, ok := lookupKey(fields[1])
		if !ok {
			return step, fmt.Errorf("unknown key %q", fields[1])
		}
		step.Key = key
		if len(fields) == 3 {
			count, err := strconv.Atoi(fields[2])
			if err != nil || count <= 0 {
				return step, fmt.Errorf("repeat count %q is not a positive number", fields[2])
			}
			step.Count = count
		}
		switch strings.ToLower(fields[0]) {
		case "key":
			step.Action = Key
		case "press":
			step.Action = Press
		default:
			step.Action = Release
		}
		return step, nil
	case "mark", "shot":
		if len(fields) != 2 {
			return step, fmt.Errorf("%s expects a label", fields[0])
		}
		step.Label = fields[1]
		if strings.EqualFold(fields[0], "mark") {
			step.Action = Mark
		} else {
			step.Action = Shot
		}
		return step, nil
	}
	return step, fmt.Errorf("unknown route command %q", fields[0])
}

// Runner drives a route against a running session.
//
// What it needs of a platform is four things, and they arrive as functions
// rather than as an interface a session has to implement: the platforms here
// spell their sessions differently — one takes a key event type, the other a
// pressed flag — and a route is the same script either way.
type Runner struct {
	// Advance performs one tick with the Host's own pacing — a probe jumps its
	// clock to the next deadline, an interactive run waits it out — and reports
	// whether the guest made progress. The runner does not tick the session
	// itself, so a route replays at whatever speed the Host is already running.
	Advance func(context.Context) (bool, error)
	// Digest identifies what is on the screen. Two calls answering the same
	// value mean the screen did not change; nothing else about the value is
	// used, so a hash of the frame is exactly enough.
	Digest func() uint64
	// SendKey delivers one half of a key event.
	SendKey func(ctx context.Context, pressed bool, key int32) error
	// Stalled reports that a tick which made no progress will never be
	// followed by one that does — nothing is parked and no deadline is due, so
	// ticking on only burns the budget. A platform that cannot tell leaves it
	// nil, and the route runs to its budget instead.
	Stalled func() bool
	// Checkpoint is called for `mark` and `shot`. reset says whether the step
	// was a mark, which is what tells a Host to restart its profile here.
	Checkpoint func(label string, tick int, reset bool) error
	// MaxTicks stops a route that never arrives. Zero selects a default.
	MaxTicks int
	// Hold is how many ticks a `key` step holds the press before releasing
	// it. Zero releases in the same tick, which is what a route wants when the
	// game reads its keys from a queue; a game that samples the keypad once a
	// frame needs a number here or it never sees the press at all.
	Hold int
}

const defaultMaxTicks = 200_000

// Run plays the route and reports where it got to. It returns an error only
// for a failure of the session or the Host; a route that does not arrive comes
// back as a result with Completed false and a reason, because "the game went
// somewhere else" is an answer, not a crash.
func (runner *Runner) Run(ctx context.Context, route *Route) (Result, error) {
	result := Result{StoppedAt: -1}
	if runner == nil || runner.Advance == nil || runner.Digest == nil || runner.SendKey == nil {
		return result, fmt.Errorf("route runner has no way to advance the session")
	}
	if route == nil || len(route.Steps) == 0 {
		return result, fmt.Errorf("route is empty")
	}
	budget := runner.MaxTicks
	if budget <= 0 {
		budget = defaultMaxTicks
	}

	for index, step := range route.Steps {
		stop, err := runner.runStep(ctx, step, budget, &result)
		if err != nil {
			return result, err
		}
		if stop != "" {
			result.StoppedAt = index
			result.Reason = fmt.Sprintf("line %d: %s", step.Line, stop)
			return result, nil
		}
	}
	result.Completed = true
	return result, nil
}

func (runner *Runner) runStep(ctx context.Context, step Step, budget int, result *Result) (string, error) {
	switch step.Action {
	case Wait:
		return runner.tickFor(ctx, step.Count, budget, result, nil)
	case WaitIdle:
		steady, previous := 0, runner.Digest()
		settled := false
		stop, err := runner.tickFor(ctx, step.Limit, budget, result, func() bool {
			digest := runner.Digest()
			if digest == previous {
				steady++
			} else {
				steady, previous = 0, digest
			}
			settled = steady >= step.Count
			return settled
		})
		if err != nil || stop != "" {
			return stop, err
		}
		if !settled {
			return fmt.Sprintf("the screen never held still for %d ticks within %d — it is animating, not settling",
				step.Count, step.Limit), nil
		}
		return "", nil
	case WaitChange:
		start := runner.Digest()
		reached := false
		stop, err := runner.tickFor(ctx, step.Limit, budget, result, func() bool {
			reached = runner.Digest() != start
			return reached
		})
		if err != nil || stop != "" {
			return stop, err
		}
		if !reached {
			return fmt.Sprintf("the screen did not change within %d ticks", step.Limit), nil
		}
		return "", nil
	case Key:
		for repeat := 0; repeat < step.Count; repeat++ {
			if err := runner.SendKey(ctx, true, step.Key); err != nil {
				return "", err
			}
			// A press and its release in the same tick is not a press to a
			// game that samples the keypad once a frame: two titles sat on
			// their own screen for a whole run that way, which reads exactly
			// like a title that has stopped. Holding costs the route the ticks
			// it holds for, which is why it is the caller's number.
			if runner.Hold > 0 {
				if stop, err := runner.tickFor(ctx, runner.Hold, budget, result, nil); err != nil || stop != "" {
					return stop, err
				}
			}
			if err := runner.SendKey(ctx, false, step.Key); err != nil {
				return "", err
			}
		}
		return "", nil
	case Press, Release:
		pressed := step.Action == Press
		for repeat := 0; repeat < step.Count; repeat++ {
			if err := runner.SendKey(ctx, pressed, step.Key); err != nil {
				return "", err
			}
		}
		return "", nil
	case Mark, Shot:
		reset := step.Action == Mark
		if reset {
			result.Marks = append(result.Marks, MarkPoint{Label: step.Label, Tick: result.Ticks})
		}
		if runner.Checkpoint != nil {
			if err := runner.Checkpoint(step.Label, result.Ticks, reset); err != nil {
				return "", err
			}
		}
		return "", nil
	}
	return fmt.Sprintf("unknown route action %d", step.Action), nil
}

// tickFor advances up to count ticks, stopping early when done reports true.
// It returns a non-empty reason when the route cannot continue.
func (runner *Runner) tickFor(ctx context.Context, count, budget int, result *Result, done func() bool) (string, error) {
	for ran := 0; ran < count; ran++ {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		if result.Ticks >= budget {
			return fmt.Sprintf("route tick budget of %d was exhausted", budget), nil
		}
		progressed, err := runner.Advance(ctx)
		if err != nil {
			return "", err
		}
		result.Ticks++
		if !progressed && runner.Stalled != nil && runner.Stalled() {
			// Nothing ran, and nothing is parked either, so no amount of
			// further ticking will change anything: the guest is waiting on
			// something the clock will never deliver.
			return "the guest stopped making progress with nothing parked", nil
		}
		if done != nil && done() {
			return "", nil
		}
	}
	return "", nil
}
