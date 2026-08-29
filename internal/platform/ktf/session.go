package ktf

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"time"

	"github.com/movingwoo/wfeature/internal/armcore"
	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/cheat"
	"github.com/movingwoo/wfeature/internal/jvm"
)

// ErrGuestExited reports that guest code called MC_knlExit: the game ended on
// its own terms and the Host should close the session, not report a failure.
var ErrGuestExited = errors.New("KTF guest requested exit")

// Session drives one KTF game from archive bytes to a running lifecycle. It
// composes the load, initialization, and startApp sequence every Host shares,
// then exposes the cooperative service loop as single ticks.
type Session struct {
	Archive      *Archive
	Client       *Client
	options      SessionOptions
	cheat        *cheat.Session
	cheatConsole *cheat.Console
}

// SessionOptions bound guest execution for one session.
type SessionOptions struct {
	// MaxSteps caps the ARM instructions of each bounded guest run. Zero
	// selects the startApp acceptance ceiling real games require.
	MaxSteps uint64
	// TimerLimit caps timer callbacks per tick. Zero selects a default.
	TimerLimit int
	// ThreadSliceSteps caps the ARM steps one guest thread runs per tick
	// before parking. Zero selects a default.
	ThreadSliceSteps uint64
	// ServiceSteps caps the total ARM steps one Host service call — a timer
	// round, a key delivery, a paint — may spend before it fails with
	// ErrServiceStepLimit. It is a ceiling on a call that never returns, not
	// on one that takes a while: MaxSteps still bounds each window, and
	// exhausting a window only renews it. Zero selects a default that covers
	// the longest save load these games perform.
	ServiceSteps uint64
	// ServiceWait caps how long one Host service call may spend waiting on the
	// session clock before it fails with ErrServiceWaitLimit. A guest that
	// waits by polling the clock inside a delay loop — which is how a title
	// paces its opening sequence with no scheduler to yield to — spends steps
	// without doing work, so those windows are charged here instead of against
	// ServiceSteps. Zero selects a default.
	ServiceWait time.Duration
	// SaveStore persists guest saves. When nil and SaveRoot is set, a
	// directory store scoped by the archive PID is attached instead.
	SaveStore SaveStore
	// SaveRoot is the directory Hosts with filesystem access use for saves;
	// each game persists under <SaveRoot>/<PID>. See SaveOwner for why the
	// PID and not the AID.
	SaveRoot string
	// Clock is the time source the waits a game asks for are measured
	// against. Nil selects the wall clock, which is what an interactive Host
	// wants: the game then runs at the speed it was written for. A Host that
	// runs ticks in a batch passes a ManualClock and drives it with
	// SkipToNextDeadline so the waits cost no real time.
	Clock Clock
	// Speed scales the game's pace. Zero and 1 both select the original
	// speed; see Client.SetSpeed for the range.
	Speed float64
	// TraceLimit is how many recent runtime boundary events Session.Diagnostics
	// reports in order. Zero keeps only the counted totals. Hosts enable this
	// in debug builds; release builds leave it at zero.
	TraceLimit int
	// Logger receives session lifecycle events at debug level. Nil is silent.
	Logger *slog.Logger
	// AudioSink is where sound the game plays goes. Nil is silent, which is
	// what a Host without an audio device wants; the game still runs its
	// playback calls and still gets the answers it expects.
	AudioSink backend.AudioSink
	// Width and Height name the handset the game is told it runs on. Zero for
	// either selects the platform's own 240x320, which is what all but a
	// title packaged for a smaller phone wants. It is what the guest is told
	// rather than how large the Host draws the result; see Client.SetScreen.
	Width, Height int
	// Debug is handed the ARM core once it exists and before any guest code
	// has run. It is for the local investigation probes, and what it is for is
	// the horizon: a debugger attached to a finished session cannot see the
	// client's own initialization or `startApp`, which is exactly where a
	// title that never reaches a session fails. Breakpoints and watches armed
	// here do see them. No Host sets it.
	Debug func(*armcore.Core)
}

const (
	// The ceiling is what a real game's startApp needs, and two local titles
	// spend between fifty and a hundred million instructions in one native
	// call there — decompressing what they load before they draw anything.
	// Both finish in under a second of Host time, so the old fifty million
	// was not protecting against a runaway; it was cutting off a loader.
	sessionDefaultMaxSteps   = 100_000_000
	sessionDefaultTimerLimit = 4
	// startupSliceSteps bounds guest execution between PendingSession pumps.
	startupSliceSteps = 8_000_000
)

var errStartCancelled = fmt.Errorf("KTF session start was cancelled")

// PendingSession is a session start running on a background goroutine. The
// startup guest execution parks after every slice of steps; each Pump grants
// the next slice, so a browser Host can keep its event loop alive during the
// tens of seconds a real game's startApp takes.
type PendingSession struct {
	grants  chan struct{}
	done    chan struct{}
	stopped bool
	session *Session
	err     error
}

// StartSessionAsync begins StartSession in the background. The returned
// PendingSession must be pumped until it reports completion.
func StartSessionAsync(ctx context.Context, data []byte, options SessionOptions) *PendingSession {
	pending := &PendingSession{grants: make(chan struct{}), done: make(chan struct{})}
	go func() {
		defer close(pending.done)
		// A start runs the archive's own initialization and its startApp, so
		// this goroutine is guest code and a panic here would be an unsupported
		// archive taking the process down instead of reporting itself. It is
		// the start's error either way; see backend.GuestPanic.
		defer func() {
			if recovered := recover(); recovered != nil {
				pending.session = nil
				pending.err = backend.GuestPanic(options.Logger, "KTF session start", recovered)
			}
		}()
		pending.session, pending.err = startSession(ctx, data, options, func(context.Context) error {
			if _, ok := <-pending.grants; !ok {
				return errStartCancelled
			}
			return nil
		})
	}()
	return pending
}

// Pump grants one startup slice and reports whether the start finished. When
// finished it returns the start error, if any; Session then holds the result.
func (pending *PendingSession) Pump() (bool, error) {
	if pending == nil {
		return true, fmt.Errorf("KTF pending session is nil")
	}
	if pending.stopped {
		return true, errStartCancelled
	}
	select {
	case <-pending.done:
		return true, pending.err
	case pending.grants <- struct{}{}:
		select {
		case <-pending.done:
			return true, pending.err
		default:
			return false, nil
		}
	}
}

// Session returns the started session once Pump reported completion without
// an error.
func (pending *PendingSession) Session() *Session {
	if pending == nil {
		return nil
	}
	return pending.session
}

// Close cancels a start that has not finished; the background goroutine fails
// its next park and exits.
func (pending *PendingSession) Close() {
	if pending == nil || pending.stopped {
		return
	}
	pending.stopped = true
	close(pending.grants)
}

// StartSession opens a KTF archive, executes the client's entry and
// initialization functions, constructs the ADF main class, and calls
// startApp with an empty argument array like the original launcher.
func StartSession(ctx context.Context, data []byte, options SessionOptions) (*Session, error) {
	return startSession(ctx, data, options, nil)
}

// StartFailure is what a start reports once guest code has run. The interesting
// half of a start that fails is what the game was doing when it stopped, and
// until this existed that half was thrown away: there is no session to ask, so
// `-diag` wrote nothing for exactly the archives whose failure is hardest to
// read. The error is unchanged — every caller that only tests it sees what it
// always saw — and a Host that wants the trace unwraps this.
type StartFailure struct {
	// Err is the failure itself.
	Err error
	// Diagnostics is the boundary trace of the run that failed. It is empty
	// when the failure came before the runtime existed, and when the Host did
	// not ask for tracing.
	Diagnostics Diagnostics
}

func (failure *StartFailure) Error() string {
	if failure == nil || failure.Err == nil {
		return "KTF session start failed"
	}
	return failure.Err.Error()
}

func (failure *StartFailure) Unwrap() error {
	if failure == nil {
		return nil
	}
	return failure.Err
}

// StartDiagnostics reports the boundary trace carried by a failed start, if the
// error is one. A caller that has only an error uses this rather than reaching
// for the type.
func StartDiagnostics(err error) (Diagnostics, bool) {
	var failure *StartFailure
	if errors.As(err, &failure) && failure != nil {
		return failure.Diagnostics, true
	}
	return Diagnostics{}, false
}

func startSession(ctx context.Context, data []byte, options SessionOptions, startupHook func(context.Context) error) (*Session, error) {
	archive, err := Open(data)
	if err != nil {
		return nil, err
	}
	maxSteps := options.MaxSteps
	if maxSteps == 0 {
		maxSteps = sessionDefaultMaxSteps
	}
	client, err := LoadClient(archive.JAR.Client, armcore.CoreOptions{MaxSteps: maxSteps})
	if err != nil {
		return nil, err
	}
	// Before the clock, the screen or a single guest instruction: a watch on an
	// address a title writes once during initialization has to be armed before
	// that initialization, or it reports no writer at all.
	if options.Debug != nil {
		options.Debug(client.core)
	}
	// The clock has to be in place before anything guest-visible runs: the
	// runtime anchors the guest's timeline to it when initialization builds
	// the runtime.
	client.clock = options.Clock
	if client.clock == nil {
		client.clock = wallClock{}
	}
	client.SetSpeed(options.Speed)
	// The screen has to be named before any guest code runs: the framebuffer
	// is built on the game's first request for it and never resized.
	client.SetScreen(options.Width, options.Height)
	client.SetDiagnostics(options.TraceLimit, options.Logger)
	client.audio = backend.NewAudio(options.AudioSink)
	client.log("KTF session loading",
		"aid", archive.Descriptor.AID,
		"main_class", archive.Descriptor.MainClass,
		"client", archive.JAR.Client.Name,
		"max_steps", maxSteps)
	if startupHook != nil {
		// Startup guest runs park every slice so a Host event loop stays
		// responsive during the long DRM/initialization phase; the hook and
		// budget come off before the session's normal service loop begins.
		client.thread.SetStepBudget(startupSliceSteps)
		client.thread.SetLimitHook(startupHook)
		defer func() {
			client.thread.SetStepBudget(0)
			client.thread.SetLimitHook(nil)
		}()
	}
	client.threadSliceSteps = options.ThreadSliceSteps
	client.serviceSteps = options.ServiceSteps
	client.serviceWait = options.ServiceWait
	client.SetProgramName(ProgramNameForAID(archive.Descriptor.AID))
	client.AttachAppProperties(archive.Descriptor.Properties)
	client.AttachResources(archive.JAR.Entries)
	client.AttachFilesystem(archive.GuestFiles())
	if options.SaveStore != nil {
		client.AttachSaveStore(options.SaveStore)
	} else if options.SaveRoot != "" {
		client.AttachSaveStore(NewDirectorySaveStore(filepath.Join(options.SaveRoot, SaveOwner(archive.Descriptor))))
	}
	// Everything from here on has run guest code, so a failure carries the
	// trace of what the game was doing when it stopped.
	failed := func(err error) (*Session, error) {
		return nil, &StartFailure{Err: err, Diagnostics: (&Session{Client: client}).Diagnostics()}
	}
	// The two generations reach the same runtime by different routes. An
	// older module is handed the platform's callback table and publishes its
	// classes for this side to link; the current one relocates itself, hands
	// back an executable descriptor and registers its own. Everything after
	// this point is shared. See module_link.go.
	if client.IsModule() {
		entrySummary, err := client.ExecuteModuleEntry(ctx)
		if err != nil {
			client.log("KTF module entry failed", "error", err)
			return failed(err)
		}
		client.log("KTF module started", "steps", entrySummary.Steps)
	} else {
		entrySummary, err := client.ExecuteEntry(ctx, nil)
		if err != nil {
			client.log("KTF entry failed", "error", err)
			return failed(err)
		}
		client.log("KTF entry executed", "steps", entrySummary.Steps)
		if _, err := client.Initialize(ctx, entrySummary.Context.Registers[0]); err != nil {
			client.log("KTF initialization failed", "error", err)
			return failed(err)
		}
		client.log("KTF initialized")
	}
	object, _, err := client.NewObject(ctx, archive.Descriptor.MainClass, "()V")
	if err != nil {
		client.log("KTF main class construction failed", "class", archive.Descriptor.MainClass, "error", err)
		return failed(fmt.Errorf("construct KTF main class %s: %w", archive.Descriptor.MainClass, err))
	}
	arguments, err := client.newStringArrayObject()
	if err != nil {
		return failed(err)
	}
	if _, err := client.InvokeVirtual(ctx, object, "startApp", "([Ljava/lang/String;)V", jvm.ReferenceValue(arguments)); err != nil {
		client.log("KTF startApp failed", "class", archive.Descriptor.MainClass, "error", err)
		return failed(fmt.Errorf("start KTF main class %s: %w", archive.Descriptor.MainClass, err))
	}
	client.log("KTF startApp returned", "class", archive.Descriptor.MainClass)
	return &Session{Archive: archive, Client: client, options: options}, nil
}

// newStringArrayObject allocates the empty [Ljava/lang/String; guest array
// startApp receives from the original launcher.
func (client *Client) newStringArrayObject() (*jvm.Object, error) {
	classAddress, err := client.runtime.ensureJavaClass("[Ljava/lang/String;")
	if err != nil {
		return nil, err
	}
	metadata, ok := client.JVM().AOTClassAt(classAddress)
	if !ok {
		return nil, fmt.Errorf("[Ljava/lang/String; is not registered")
	}
	address, err := client.runtime.allocateAOTArrayObject(metadata, 0)
	if err != nil {
		return nil, err
	}
	object, ok := client.JVM().AOTObject(address)
	if !ok {
		return nil, fmt.Errorf("startApp arguments array is not bound")
	}
	return object, nil
}

// Tick performs one cooperative service round: pending WIPI C timers, one
// queued guest thread — plus any workers that finish, which is not what the
// limit shares out — and a card paint. It reports whether any service ran, so
// Hosts can idle when the game is fully waiting.
func (session *Session) Tick(ctx context.Context) (bool, error) {
	if session == nil || session.Client == nil {
		return false, fmt.Errorf("KTF session is not started")
	}
	timerLimit := session.options.TimerLimit
	if timerLimit <= 0 {
		timerLimit = sessionDefaultTimerLimit
	}
	client := session.Client
	client.costs.Rounds++

	phase := client.phaseClock()
	ranTimers, timerErr := client.ServiceTimers(ctx, timerLimit)
	client.sincePhase(phase, &client.costs.Timers)
	if timerErr = client.absorbUncaughtCallback("timer", timerErr); timerErr != nil {
		client.log("KTF timer service failed", "error", timerErr)
		return ranTimers > 0, timerErr
	}

	phase = client.phaseClock()
	ranThreads, threadErr := client.ServiceThreads(ctx, 1)
	client.sincePhase(phase, &client.costs.Threads)
	if threadErr = client.absorbUncaughtCallback("thread", threadErr); threadErr != nil {
		client.log("KTF thread service failed", "error", threadErr)
		return ranTimers+ranThreads > 0, threadErr
	}

	// Skipping a paint is a trade — the picture for the logic in the same round
	// — and a round with no logic in it has nothing to trade. Not all games put
	// their frame loop in a timer or a thread: one runs it from its card's
	// paint, so a skip there drops the only work the round had. And because the
	// client thread advances its own next wake-up inside that paint, skipping
	// left it perpetually due, which the Host reads as work waiting and spins
	// on. The screen kept updating, at a cost of a busy core and a million
	// empty rounds a minute.
	if ranTimers+ranThreads == 0 {
		client.skipPaint = false
	}
	painted := false
	if !client.skipPaint {
		phase = client.phaseClock()
		var paintErr error
		painted, paintErr = client.ServicePaint(ctx)
		client.sincePhase(phase, &client.costs.Paint)
		if paintErr = client.absorbUncaughtCallback("paint", paintErr); paintErr != nil {
			client.log("KTF paint failed", "error", paintErr)
			return true, paintErr
		}
	}

	phase = client.phaseClock()
	cheatErr := session.serviceCheat()
	client.sincePhase(phase, &client.costs.Cheat)
	if cheatErr != nil {
		return true, cheatErr
	}

	phase = client.phaseClock()
	client.serviceAudio()
	client.sincePhase(phase, &client.costs.Audio)

	phase = client.phaseClock()
	collectErr := session.collectGuestObjects()
	client.sincePhase(phase, &client.costs.Collect)
	if collectErr != nil {
		return true, collectErr
	}
	return ranTimers > 0 || ranThreads > 0 || painted, nil
}

// TickFor runs service rounds until the guest has nothing due, a frame is
// ready, or the budget is spent. It reports whether anything progressed and
// how long the Host should wait before coming back.
//
// A Host that can only re-enter on a timer needs this. One round services a
// single thread slice, and a game asks for thousands of those a second, so a
// Host clocked at sixty entries a second delivers a fraction of the execution
// the guest asked for and the game runs slow rather than badly. Doing the
// rounds here instead means one entry covers as many as the guest has due.
//
// The budget is what keeps that from becoming the opposite problem. On a page
// this runs on the thread that also has to draw, so a round that overruns is
// a dropped frame; the budget bounds one entry's share of it. A round already
// running when the budget goes is not cut short, because guest code cannot be
// suspended mid-call, so the overrun is one round rather than none.
func (session *Session) TickFor(ctx context.Context, budget time.Duration) (bool, time.Duration, error) {
	if session == nil || session.Client == nil {
		return false, 0, fmt.Errorf("KTF session is not started")
	}
	progressed := false
	client := session.Client
	entered := client.now()
	deadline := entered.Add(budget)
	for {
		client.skipPaint = session.behindOnPaint()
		ran, err := session.Tick(ctx)
		if client.skipPaint {
			client.paintsDrop++
		} else {
			client.lastPaint = client.now()
		}
		client.skipPaint = false
		progressed = progressed || ran
		if err != nil {
			return progressed, 0, err
		}
		// Nothing is due until the guest's own next deadline, so the Host can
		// sleep exactly that long instead of polling until it arrives.
		if wait, parked := session.waitBeforeNextRound(); parked {
			client.noteEntryLoad(client.now().Sub(entered), wait)
			return progressed, wait, nil
		}
		if !client.now().Before(deadline) {
			// The entry stopped on its budget with work still due, which is
			// saturation by definition: there was no wait to spend.
			client.noteEntryLoad(client.now().Sub(entered), budget)
			return progressed, 0, nil
		}
	}
}

// Painting is what a slow host has to give up first.
//
// A round is a card paint and the game logic that led to it, and the paint is
// almost all of it — measured at 96% against 3% for the guest threads. A host
// that can afford three rounds a second is therefore not running the game at
// three frames a second; it is running the game's *logic* at three ticks a
// second, which is a game in slow motion rather than a game with a low frame
// rate. Everything the guest paces off its own clock — animation, timers,
// enemy movement — drifts against a wall clock that keeps going.
//
// Dropping the paint gives that back. The logic keeps its rounds at 4% of the
// cost, and the screen updates as often as the host can manage. A game at ten
// frames a second running at the right speed is playable; the same game at
// three frames a second running at a fifth of its speed is not.
// The signal has to be saturation, not lateness.
//
// The obvious test — "is this round starting after the deadline it was due" —
// fires on a host that is keeping up perfectly well, because a round that
// finishes a frame's work naturally lands past the deadline that scheduled it.
// Measured, that dropped a desktop from 104 frames a second to 9 while gaining
// nothing, which is the opposite of the point.
//
// What actually distinguishes a host in trouble is that an entry costs more
// than the wait the guest asked for afterwards. A desktop spends 16ms and is
// then asked to wait 32ms: it has half its time spare and must never skip. A
// phone spends 282ms against the same 32ms wait: it is nine times oversubscribed
// and every paint it drops buys back a round of logic. Both numbers are already
// on hand at the end of an entry, and averaging them over recent entries keeps
// one slow frame from triggering the behaviour.
const (
	// paintSaturationBias is how much of its schedule a host must be using
	// before it gives up paints. Slightly over one keeps a host that is merely
	// close to its budget from oscillating.
	paintSaturationBias = 1.1
	// maxPaintInterval is the longest the screen may go without an update, no
	// matter how far behind the host is. Without a floor a host that never
	// catches up would stop painting altogether.
	maxPaintInterval = 100 * time.Millisecond
	// paintLoadSmoothing weights the newest entry against the running average.
	paintLoadSmoothing = 0.25
)

// noteEntryLoad records what one entry cost against the wait the guest asked
// for afterwards, which is the ratio behindOnPaint reads.
func (client *Client) noteEntryLoad(cost, wait time.Duration) {
	if wait <= 0 {
		// The guest had work due immediately, so this entry says nothing about
		// how much slack the host has.
		return
	}
	load := cost.Seconds() / wait.Seconds()
	if client.paintLoad == 0 {
		client.paintLoad = load
		return
	}
	client.paintLoad += paintLoadSmoothing * (load - client.paintLoad)
}

// behindOnPaint answers whether this round should skip its paint: the host is
// using more time than the guest's own schedule leaves it, and the screen has
// been updated recently enough that skipping one more will not read as a
// freeze.
func (session *Session) behindOnPaint() bool {
	client := session.Client
	if client.paintLoad < paintSaturationBias {
		return false
	}
	now := client.now()
	return !client.lastPaint.IsZero() && now.Sub(client.lastPaint) < maxPaintInterval
}

// waitBeforeNextRound answers how long until the guest has work due, and
// whether it is parked at all. A session with nothing parked is not waiting
// for the clock — it is waiting on something the clock will not deliver — so
// the Host keeps ticking rather than sleeping.
func (session *Session) waitBeforeNextRound() (time.Duration, bool) {
	deadline, parked := session.NextDeadline()
	if !parked {
		return 0, false
	}
	wait := deadline.Sub(session.Client.now())
	if wait <= 0 {
		return 0, false
	}
	return wait, true
}

// GuestElapsed is how much guest time has passed since the session started.
// Hosts recording audio timestamp with it so a batched run records the guest's
// timeline rather than the Host's.
func (session *Session) GuestElapsed() time.Duration {
	if session == nil || session.Client == nil {
		return 0
	}
	return session.Client.runtime.guestElapsed()
}

// Pause and Resume are the Jlet lifecycle a Host drives when the page that was
// watching goes away and comes back. See Client.PauseApp for why running guest
// code here is safe and the teardown's problem is not this one.
//
// **A callback that fails does not end the game.** This is the one rule that
// had to be measured rather than reasoned: driving the pair across the local
// 266-archive set killed five titles that had run perfectly well without it —
// two threw NullPointerException inside their own resumeApp, one reached a
// kernel slot this platform does not implement, and two asked for a sound file
// their archive does not carry. Every one of them was a title that had been
// running fine, and the callback is a courtesy the Host pays rather than
// something the game asked for. So a failure here is logged and counted and
// the session carries on exactly as it did before the calls existed. The
// alternative is a phone that ends somebody's game every time it backgrounds
// the page.
//
// A guest that *exits* inside one is different: that is the title deciding to
// end, and it reaches the Host as it does from anywhere else.
func (session *Session) Pause(ctx context.Context) error {
	if session == nil || session.Client == nil {
		return nil
	}
	return session.lifecycleResult(session.Client.PauseApp(ctx), "pauseApp")
}

func (session *Session) Resume(ctx context.Context) error {
	if session == nil || session.Client == nil {
		return nil
	}
	return session.lifecycleResult(session.Client.ResumeApp(ctx), "resumeApp")
}

func (session *Session) lifecycleResult(err error, callback string) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, ErrGuestExited):
		return err
	}
	session.Client.log("KTF lifecycle callback failed", "callback", callback, "error", err)
	if runtime := session.Client.runtime; runtime != nil {
		runtime.countDiagnostic("lifecycle callback failed")
	}
	return nil
}

// HasPointer reports whether this title wrote anything a touch would reach;
// see Client.HasPointer for why a Host is told rather than left to guess.
func (session *Session) HasPointer() bool {
	if session == nil || session.Client == nil {
		return false
	}
	return session.Client.HasPointer()
}

// SendPointer delivers a pointer event to the card stack. See
// Client.SendPointer for why this does not go through the event queue.
func (session *Session) SendPointer(ctx context.Context, eventType, x, y int32) error {
	if session == nil || session.Client == nil {
		return fmt.Errorf("KTF session is not started")
	}
	return session.Client.SendPointer(ctx, eventType, x, y)
}

// SetSpeed scales how fast the game runs; see Client.SetSpeed. A Host may
// change it between ticks while the game is running.
func (session *Session) SetSpeed(multiplier float64) {
	if session == nil {
		return
	}
	session.Client.SetSpeed(multiplier)
}

// Speed reports the current multiplier.
func (session *Session) Speed() float64 {
	if session == nil {
		return 1
	}
	return session.Client.Speed()
}

// NextDeadline reports when the earliest parked guest work — a sleeping
// thread, a pending timer — becomes due, and whether any is parked. A Tick
// that reports no progress has found nothing due yet; a Host idles until this
// instant rather than polling, and stops ticking when nothing is parked at
// all, because then the guest is waiting on something the clock will not
// deliver.
func (session *Session) NextDeadline() (time.Time, bool) {
	if session == nil {
		return time.Time{}, false
	}
	return session.Client.NextDeadline()
}

// SkipToNextDeadline jumps a manual clock forward to the next deadline and
// reports whether it moved; see Client.SkipToNextDeadline. A session on the
// wall clock always reports false.
func (session *Session) SkipToNextDeadline() bool {
	if session == nil {
		return false
	}
	return session.Client.SkipToNextDeadline()
}

// Close aborts every parked guest thread worker so their goroutines exit.
// The session cannot tick afterwards.
func (session *Session) Close() {
	// Whatever was still sounding stops with the session; a Host recording to
	// a file otherwise ends with notes that never get their note off.
	if session != nil && session.Client != nil {
		session.Client.audio.StopAll()
	}
	if session == nil || session.Client == nil {
		return
	}
	session.Client.StopThreads()
}

// Frame exposes the last flushed RGBA frame with its size and flush count.
func (session *Session) Frame() ([]byte, int, int, uint32) {
	if session == nil {
		return nil, 0, 0, 0
	}
	return session.Client.Frame()
}

// FrameDigest fingerprints the current screen without copying it; see
// Client.FrameDigest. A route uses it to tell a settled screen from an
// animating one.
func (session *Session) FrameDigest() uint64 {
	if session == nil {
		return 0
	}
	return session.Client.FrameDigest()
}

// Flushes reports how many frames the session has flushed, without copying
// the frame itself.
func (session *Session) Flushes() uint32 {
	if session == nil {
		return 0
	}
	return session.Client.Flushes()
}

// SendKey forwards one WIPI key event to the pushed display cards.
func (session *Session) SendKey(ctx context.Context, eventType, key int32) error {
	if session == nil || session.Client == nil {
		return fmt.Errorf("KTF session is not started")
	}
	return session.Client.SendKey(ctx, eventType, key)
}
