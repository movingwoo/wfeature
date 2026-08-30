package ktf

import (
	"context"
	"os"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/movingwoo/wfeature/internal/route"
)

// What a Host does with a scene, measured on the scene rather than near it.
//
// The probes in local_perf_test.go warm up by ticking blind for a few thousand
// ticks and then measure whatever the game happens to be showing, which for a
// title with a long boot is its menu. The cost worth measuring is in a fight or
// a crowded map, and nothing reaches those but playing there — so this one
// takes a route, drives it to the scene, and only then starts counting.
//
// It also measures through the call the page actually uses. A route runner
// drives Session.Tick, and the paint-drop decision lives one level up in
// TickFor: skipPaint is never set on the route's path, so a run that only
// routes cannot see a dropped paint at all. Everything about how a saturated
// Host trades the picture for the logic is invisible to every probe we had.
//
// The two phases want opposite clocks, which is the whole trick here. Reaching
// the scene has to repeat exactly, so the route runs on a manual clock jumped
// to each next deadline. Measuring has to charge the host what it really
// spends, so the measurement runs on the wall. A clock that does both is a few
// lines, and switching it in the middle costs nothing — where asking the
// session to switch would have meant a production seam that exists for a test.
type probeClock struct {
	mutex sync.Mutex
	// manual is the instant the route phase moves by hand.
	manual time.Time
	// live says the measurement has started, after which the clock reads the
	// wall's own progress from where the route left it.
	live   bool
	origin time.Time
	base   time.Time
}

// hostTickBudget is what the page gives one entry, from webhost's tickBudget.
// It is repeated rather than exported because a probe that measured a budget
// the browser does not use would be measuring a Host nobody runs; if that
// constant moves, this one moves with it.
const hostTickBudget = 32 * time.Millisecond

// blindWarmupTicks is how far a run with no route ticks before measuring, which
// is the same 3000 the older probes settled on: past a title's loading and into
// whatever it shows once it is up.
const blindWarmupTicks = 3000

func newProbeClock() *probeClock {
	return &probeClock{manual: time.UnixMilli(manualClockEpochMillis).UTC()}
}

func (clock *probeClock) Now() time.Time {
	clock.mutex.Lock()
	defer clock.mutex.Unlock()
	if !clock.live {
		return clock.manual
	}
	return clock.base.Add(time.Since(clock.origin))
}

// jumpTo is SkipToNextDeadline by hand. The session's own version type-asserts
// its clock to *ManualClock and this is not one, so the probe does what that
// method does — advance to the guest's next deadline, never backwards — from
// out here, where NextDeadline and the clock are both already in reach.
func (clock *probeClock) jumpTo(deadline time.Time) {
	clock.mutex.Lock()
	defer clock.mutex.Unlock()
	if !clock.live && deadline.After(clock.manual) {
		clock.manual = deadline
	}
}

// golive hands the clock to the wall, continuing from the instant the route
// finished rather than jumping to today: a guest that has been told it is 2007
// should not have the year change under it between two ticks.
func (clock *probeClock) golive() {
	clock.mutex.Lock()
	defer clock.mutex.Unlock()
	clock.live = true
	clock.base = clock.manual
	clock.origin = time.Now()
}

// TestRoutedLoadProbe drives WFEATURE_PERF_ROUTE into WFEATURE_PERF_ARCHIVE and
// then runs the Host's own loop over the scene it arrives at.
//
// WFEATURE_PERF_SPEED is the multiplier to measure at, defaulting to the 0.25
// the reports that prompted this were taken at; WFEATURE_PERF_ENTRIES is how
// many TickFor entries to measure over. Entries rather than seconds because the
// measurement is on the wall and so does not repeat exactly: fixing the count
// at least fixes how much of the Host's loop each side of an A/B ran.
func TestRoutedLoadProbe(t *testing.T) {
	archivePath := os.Getenv("WFEATURE_PERF_ARCHIVE")
	if archivePath == "" {
		t.Skip("set WFEATURE_PERF_ARCHIVE")
	}
	archive, err := os.ReadFile(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	// The route is what makes this a measurement of a scene. Without one the
	// probe still runs — ticking blind for a while reaches whatever the title
	// shows on its own, which is what a sweep across many titles can afford —
	// but a number taken that way is a menu's number as often as a game's.
	var script *route.Route
	if routePath := os.Getenv("WFEATURE_PERF_ROUTE"); routePath != "" {
		text, readErr := os.ReadFile(routePath)
		if readErr != nil {
			t.Fatal(readErr)
		}
		script, err = route.Parse(string(text), KeyCodeByName)
		if err != nil {
			t.Fatal(err)
		}
	}
	speed := 0.25
	if value := os.Getenv("WFEATURE_PERF_SPEED"); value != "" {
		parsed, parseErr := strconv.ParseFloat(value, 64)
		if parseErr != nil || parsed <= 0 {
			t.Fatalf("invalid WFEATURE_PERF_SPEED %q", value)
		}
		speed = parsed
	}
	entries := 300
	if value := os.Getenv("WFEATURE_PERF_ENTRIES"); value != "" {
		parsed, parseErr := strconv.Atoi(value)
		if parseErr != nil || parsed <= 0 {
			t.Fatalf("invalid WFEATURE_PERF_ENTRIES %q", value)
		}
		entries = parsed
	}

	ctx := context.Background()
	clock := newProbeClock()
	session, err := StartSession(ctx, archive, SessionOptions{Clock: clock})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()

	routeStarted := time.Now()
	runner := &route.Runner{
		Digest: session.FrameDigest,
		SendKey: func(ctx context.Context, pressed bool, key int32) error {
			eventType := KeyPressed
			if !pressed {
				eventType = KeyReleased
			}
			return session.SendKey(ctx, eventType, key)
		},
		Stalled: func() bool {
			_, pending := session.NextDeadline()
			return !pending
		},
		Advance: func(ctx context.Context) (bool, error) {
			progressed, tickErr := session.Tick(ctx)
			if tickErr != nil {
				return progressed, tickErr
			}
			if deadline, pending := session.NextDeadline(); pending {
				clock.jumpTo(deadline)
			}
			return progressed, nil
		},
	}
	if script != nil {
		result, runErr := runner.Run(ctx, script)
		if runErr != nil {
			t.Fatalf("route: %v", runErr)
		}
		if !result.Completed {
			t.Fatalf("route stopped at step %d: %s", result.StoppedAt+1, result.Reason)
		}
		t.Logf("route reached the scene in %v of host time", time.Since(routeStarted).Round(time.Millisecond))
	} else {
		for tick := 0; tick < blindWarmupTicks; tick++ {
			if _, tickErr := runner.Advance(ctx); tickErr != nil {
				t.Fatal(tickErr)
			}
		}
		t.Logf("no route: warmed %d ticks in %v of host time",
			blindWarmupTicks, time.Since(routeStarted).Round(time.Millisecond))
	}

	// Everything the route spent is the cost of getting here, not of the scene.
	// Reading the counters now and subtracting is what keeps a long boot out of
	// the numbers the A/B compares.
	before := session.HostCosts()
	beforeFlushes := session.Flushes()
	session.SetSpeed(speed)
	clock.golive()

	startGuest := session.GuestElapsed()
	started := time.Now()
	busy := time.Duration(0)
	for entry := 0; entry < entries; entry++ {
		entered := time.Now()
		_, wait, tickErr := session.TickFor(ctx, hostTickBudget)
		busy += time.Since(entered)
		if tickErr != nil {
			t.Fatal(tickErr)
		}
		// A Host that finishes early sleeps out the rest, and skipping that
		// would make every entry look free — the load ratio the paint-drop
		// decision reads is cost against exactly this wait.
		if wait > 0 {
			time.Sleep(wait)
		}
	}
	wall := time.Since(started)
	guest := session.GuestElapsed() - startGuest
	costs := session.HostCosts()

	rounds := costs.Rounds - before.Rounds
	dropped := costs.PaintsDropped - before.PaintsDropped
	painted := session.Flushes() - beforeFlushes
	droppedShare := 0.0
	if rounds > 0 {
		droppedShare = 100 * float64(dropped) / float64(rounds)
	}
	t.Logf("speed=%gx entries=%d wall=%v busy=%.0f%% of a core",
		speed, entries, wall.Round(time.Millisecond), 100*busy.Seconds()/wall.Seconds())
	t.Logf("rounds=%d (%.1f/s) paints_dropped=%d (%.0f%% of rounds) paint_load=%.2f",
		rounds, float64(rounds)/wall.Seconds(), dropped, droppedShare, costs.PaintLoad)
	// The share is what separates a title the drop can rescue from one it
	// cannot, so a sweep reads this column to see where a title falls.
	paintShare := 0.0
	if session.Client.entryCost > 0 {
		paintShare = session.Client.paintCost.Seconds() / session.Client.entryCost.Seconds()
	}
	t.Logf("paint=%v entry=%v share=%.4f (floor %.2f)",
		session.Client.paintCost.Round(time.Microsecond),
		session.Client.entryCost.Round(time.Microsecond), paintShare, paintShareFloor)
	t.Logf("fps=%.1f schedule_kept=%.0f%% guest=%v",
		float64(painted)/wall.Seconds(),
		100*guest.Seconds()/(wall.Seconds()*speed), guest.Round(time.Millisecond))
}
