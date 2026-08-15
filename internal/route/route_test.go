package route

import (
	"context"
	"strings"
	"testing"
)

// testKeys is a key table standing in for a platform's. The names are the
// shared ones; the codes are the WIPI ones, which is what makes a route
// written for one platform readable against another.
func testKeys(name string) (int32, bool) {
	codes := map[string]int32{
		"up": -1, "down": -2, "left": -3, "right": -4, "fire": -5, "ok": -5,
		"soft1": -6, "soft2": -7, "soft3": -8, "ez": -8,
		"call": -10, "hangup": -11, "clear": -16,
	}
	if code, ok := codes[name]; ok {
		return code, true
	}
	if len(name) == 1 && (name[0] >= '0' && name[0] <= '9' || name[0] == '*' || name[0] == '#') {
		return int32(name[0]), true
	}
	return 0, false
}

func TestParseReadsEveryCommand(t *testing.T) {
	route, err := parseForTest(`
# reach the first battle
wait 30
wait-idle 40      # the title has settled
key fire
key down 3
press left
release left
mark battle
shot after
wait-change 200
`)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	want := []Step{
		{Action: Wait, Count: 30, Line: 3},
		{Action: WaitIdle, Count: 40, Limit: 800, Line: 4},
		{Action: Key, Key: -5, Count: 1, Line: 5},
		{Action: Key, Key: -2, Count: 3, Line: 6},
		{Action: Press, Key: -3, Count: 1, Line: 7},
		{Action: Release, Key: -3, Count: 1, Line: 8},
		{Action: Mark, Count: 1, Label: "battle", Line: 9},
		{Action: Shot, Count: 1, Label: "after", Line: 10},
		{Action: WaitChange, Count: 200, Limit: 4000, Line: 11},
	}
	if len(route.Steps) != len(want) {
		t.Fatalf("parsed %d steps, want %d", len(route.Steps), len(want))
	}
	for index, step := range route.Steps {
		if step != want[index] {
			t.Errorf("step %d = %+v, want %+v", index, step, want[index])
		}
	}
}

// `#` is both the comment marker and a key on the handset. A route that cannot
// write the second one cannot reach the menus a game puts behind it.
func TestParseWritesTheHashKey(t *testing.T) {
	route, err := parseForTest("key #\npress # 2   # and still a comment after it\n")
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	hash, _ := testKeys("#")
	want := []Step{
		{Action: Key, Key: hash, Count: 1, Line: 1},
		{Action: Press, Key: hash, Count: 2, Line: 2},
	}
	if len(route.Steps) != len(want) {
		t.Fatalf("parsed %d steps, want %d", len(route.Steps), len(want))
	}
	for index, step := range route.Steps {
		if step != want[index] {
			t.Errorf("step %d = %+v, want %+v", index, step, want[index])
		}
	}
}

func TestParseRejectsBadScripts(t *testing.T) {
	for name, script := range map[string]string{
		"unknown command": "jump 3",
		"unknown key":     "key sideways",
		"missing count":   "wait",
		"zero count":      "wait 0",
		"bad repeat":      "key fire zero",
		"missing label":   "mark",
		"limit below run": "wait-idle 40 10",
		"bad limit":       "wait-idle 40 zero",
		"empty":           "# nothing but a comment\n",
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := parseForTest(script); err == nil {
				t.Fatalf("Parse(%q) succeeded, want an error", script)
			}
		})
	}
}

// The line number has to survive parsing: a route that stops halfway is
// diagnosed by which line stopped it.
func TestParseErrorNamesTheLine(t *testing.T) {
	_, err := parseForTest("wait 5\n\n# comment\nkey nowhere\n")
	if err == nil || !strings.Contains(err.Error(), "line 4") {
		t.Fatalf("error = %v, want it to name line 4", err)
	}
}

// The runner's own logic — the waits, the budget, the stop reasons — is
// tested against a stand-in tick rather than a game, so what it does when a
// game stalls or a wait never settles is pinned without needing a game that
// stalls. A zero Session answers a constant frame digest, which is exactly a
// screen that never changes.
type routeHarness struct {
	ticks    int
	marks    []string
	resets   []string
	advance  func(tick int) (bool, error)
	maxTicks int
}

func (harness *routeHarness) run(t *testing.T, script string) Result {
	t.Helper()
	route, err := parseForTest(script)
	if err != nil {
		t.Fatal(err)
	}
	budget := harness.maxTicks
	if budget == 0 {
		budget = 500
	}
	runner := &Runner{
		Digest:  func() uint64 { return 0 },
		SendKey: func(context.Context, bool, int32) error { return nil },
		// Nothing is ever parked in the harness, so a tick that made no
		// progress is a stall — the same answer a session with no deadline
		// pending gives.
		Stalled:  func() bool { return true },
		MaxTicks: budget,
		Advance: func(context.Context) (bool, error) {
			harness.ticks++
			if harness.advance != nil {
				return harness.advance(harness.ticks)
			}
			return true, nil
		},
		Checkpoint: func(label string, _ int, reset bool) error {
			harness.marks = append(harness.marks, label)
			if reset {
				harness.resets = append(harness.resets, label)
			}
			return nil
		},
	}
	result, err := runner.Run(context.Background(), route)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	return result
}

func TestRunnerAdvancesTheTicksAWaitAsksFor(t *testing.T) {
	harness := &routeHarness{}
	result := harness.run(t, "wait 12\nwait 8\n")
	if harness.ticks != 20 || result.Ticks != 20 {
		t.Fatalf("ticks = %d (result %d), want 20", harness.ticks, result.Ticks)
	}
	if !result.Completed {
		t.Fatalf("route did not complete: %s", result.Reason)
	}
}

// A settled screen is what wait-idle is looking for, so it stops as soon as
// the digest has held for the requested run of ticks rather than burning the
// whole budget.
func TestRunnerWaitIdleStopsOnceTheScreenHolds(t *testing.T) {
	harness := &routeHarness{}
	result := harness.run(t, "wait-idle 5\n")
	if result.Ticks != 5 {
		t.Fatalf("ticks = %d, want 5", result.Ticks)
	}
	if !result.Completed {
		t.Fatalf("route did not complete: %s", result.Reason)
	}
}

// A screen that never changes is a route that has gone somewhere else, and
// saying so beats reporting a run that merely ended.
func TestRunnerReportsAWaitChangeThatNeverArrives(t *testing.T) {
	harness := &routeHarness{}
	result := harness.run(t, "wait 1\nwait-change 20 20\nwait 5\n")
	if result.Completed {
		t.Fatal("route completed, want it stopped at the wait-change")
	}
	if result.StoppedAt != 1 {
		t.Fatalf("stopped at step %d, want 1", result.StoppedAt)
	}
	if !strings.Contains(result.Reason, "line 2") || !strings.Contains(result.Reason, "did not change") {
		t.Fatalf("reason = %q, want it to name line 2 and the unchanged screen", result.Reason)
	}
	// The step after the failed one must not have run.
	if result.Ticks != 21 {
		t.Fatalf("ticks = %d, want 21 — the trailing wait should not have run", result.Ticks)
	}
}

// A guest that stops running with nothing parked will never resume, so ticking
// at it until the budget runs out only wastes the run.
func TestRunnerStopsWhenTheGuestStallsWithNothingParked(t *testing.T) {
	harness := &routeHarness{advance: func(tick int) (bool, error) {
		return tick < 4, nil
	}}
	result := harness.run(t, "wait 100\n")
	if result.Completed {
		t.Fatal("route completed, want it stopped on the stall")
	}
	if result.Ticks != 4 {
		t.Fatalf("ticks = %d, want 4", result.Ticks)
	}
	if !strings.Contains(result.Reason, "stopped making progress") {
		t.Fatalf("reason = %q, want it to name the stall", result.Reason)
	}
}

func TestRunnerStopsAtTheTickBudget(t *testing.T) {
	harness := &routeHarness{maxTicks: 30}
	result := harness.run(t, "wait 100\n")
	if result.Completed {
		t.Fatal("route completed, want it stopped at the budget")
	}
	if result.Ticks != 30 {
		t.Fatalf("ticks = %d, want 30", result.Ticks)
	}
	if !strings.Contains(result.Reason, "budget") {
		t.Fatalf("reason = %q, want it to name the budget", result.Reason)
	}
}

// A mark is a measurement checkpoint and a shot is only a picture, so only the
// mark asks the Host to restart anything.
func TestRunnerTellsMarksApartFromShots(t *testing.T) {
	harness := &routeHarness{}
	result := harness.run(t, "wait 3\nshot title\nwait 2\nmark battle\n")
	if got := harness.marks; len(got) != 2 || got[0] != "title" || got[1] != "battle" {
		t.Fatalf("checkpoints = %v, want [title battle]", got)
	}
	if got := harness.resets; len(got) != 1 || got[0] != "battle" {
		t.Fatalf("resets = %v, want [battle]", got)
	}
	if len(result.Marks) != 1 || result.Marks[0].Label != "battle" || result.Marks[0].Tick != 5 {
		t.Fatalf("marks = %+v, want battle at tick 5", result.Marks)
	}
}

// A nil session is rejected rather than panicked on, since a Host that forgot
// to start one would otherwise see a crash instead of a message.
func TestRunnerRejectsAnUnstartedSession(t *testing.T) {
	runner := &Runner{}
	route, err := parseForTest("wait 1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runner.Run(context.Background(), route); err == nil {
		t.Fatal("Run() with no session succeeded, want an error")
	}
}

func TestRunnerRejectsAnEmptyRoute(t *testing.T) {
	runner := &Runner{Advance: func(context.Context) (bool, error) { return true, nil }, Digest: func() uint64 { return 0 }, SendKey: func(context.Context, bool, int32) error { return nil }}
	if _, err := runner.Run(context.Background(), &Route{}); err == nil {
		t.Fatal("Run() with no steps succeeded, want an error")
	}
}

// parseForTest reads a script against the stand-in key table, so the tests
// read the way a route does.
func parseForTest(text string) (*Route, error) {
	return Parse(text, testKeys)
}

// A press and its release in the same tick is not a press to a game that
// samples the keypad once a frame, so a route holds one for as many ticks as
// the caller asked for — and the hold costs the route those ticks.
func TestRunnerHoldsAKeyForTheTicksItWasGiven(t *testing.T) {
	var events []string
	ticks := 0
	runner := &Runner{
		MaxTicks: 100,
		Hold:     4,
		Digest:   func() uint64 { return 0 },
		Advance: func(context.Context) (bool, error) {
			ticks++
			events = append(events, "tick")
			return true, nil
		},
		SendKey: func(_ context.Context, pressed bool, key int32) error {
			if pressed {
				events = append(events, "press")
			} else {
				events = append(events, "release")
			}
			return nil
		},
	}
	script, err := parseForTest("key fire\n")
	if err != nil {
		t.Fatal(err)
	}
	result, err := runner.Run(context.Background(), script)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if !result.Completed {
		t.Fatalf("route did not complete: %s", result.Reason)
	}
	if ticks != 4 || result.Ticks != 4 {
		t.Fatalf("the hold advanced %d ticks (result %d), want 4", ticks, result.Ticks)
	}
	if got := len(events); got != 6 || events[0] != "press" || events[5] != "release" {
		t.Fatalf("events = %v, want a press, four ticks and a release", events)
	}
}

// Zero is the other answer, and it is the one a game that reads its keys from
// a queue wants: the press and the release arrive together and no tick is
// spent on them.
func TestRunnerWithoutAHoldReleasesInTheSameTick(t *testing.T) {
	ticks := 0
	runner := &Runner{
		MaxTicks: 100,
		Digest:   func() uint64 { return 0 },
		Advance:  func(context.Context) (bool, error) { ticks++; return true, nil },
		SendKey:  func(context.Context, bool, int32) error { return nil },
	}
	script, err := parseForTest("key fire 3\n")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runner.Run(context.Background(), script); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if ticks != 0 {
		t.Fatalf("three unheld presses advanced %d ticks, want 0", ticks)
	}
}

// A runner with no way to advance or observe the session is a programming
// mistake, and saying so beats a nil dereference at the first step.
func TestRunnerRejectsAnIncompleteRunner(t *testing.T) {
	script, err := parseForTest("wait 1\n")
	if err != nil {
		t.Fatal(err)
	}
	for name, runner := range map[string]*Runner{
		"no advance": {Digest: func() uint64 { return 0 }, SendKey: func(context.Context, bool, int32) error { return nil }},
		"no digest":  {Advance: func(context.Context) (bool, error) { return true, nil }, SendKey: func(context.Context, bool, int32) error { return nil }},
		"no keys":    {Advance: func(context.Context) (bool, error) { return true, nil }, Digest: func() uint64 { return 0 }},
	} {
		if _, err := runner.Run(context.Background(), script); err == nil {
			t.Fatalf("Run() with %s succeeded, want an error", name)
		}
	}
}

// The key table is the platform's, not the route's. A script naming a key the
// platform has no code for is a script that cannot run, and it is rejected
// where it is read rather than at the step that would have pressed it.
func TestParseTakesItsKeyCodesFromTheTableItIsGiven(t *testing.T) {
	route, err := Parse("key ok\n", func(name string) (int32, bool) {
		if name == "ok" {
			return 8, true
		}
		return 0, false
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := route.Steps[0].Key; got != 8 {
		t.Fatalf("key = %d, want the table's 8", got)
	}
	if _, err := Parse("key fire\n", func(string) (int32, bool) { return 0, false }); err == nil {
		t.Fatal("Parse() with a table that knows no keys succeeded, want an error")
	}
	if _, err := Parse("wait 1\n", nil); err == nil {
		t.Fatal("Parse() with no key table succeeded, want an error")
	}
}
