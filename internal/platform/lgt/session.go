package lgt

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"time"

	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/cheat"
	"github.com/movingwoo/wfeature/internal/keypad"
)

// Session is the driver a Host runs an LGT game through: start it, tick it,
// take frames, send keys. It is the same shape KTF's session has, so a Host
// that already drives one drives the other.
type Session struct {
	client  *Client
	archive *Archive
	tick    time.Duration
	// speed is how fast the game runs against the wall. The guest clock here
	// is virtual and moves a tick at a time, so what a multiplier changes is
	// what a tick of guest time is allowed to cost — see SetSpeed.
	speed float64

	// nextDue is when the next tick should start, on the wall clock. It is
	// only used by TickFor; a Host stepping ticks by hand never sets it.
	nextDue time.Time

	cheat        *cheat.Session
	cheatConsole *cheat.Console

	// pad makes a keyboard's overlapping holds look like a thumb on a handset
	// pad; see SendKey.
	pad keypad.Pad
}

// SessionOptions configures a session.
type SessionOptions struct {
	Logger    *slog.Logger
	SaveStore backend.SaveStore
	// AudioSink is where the media block's sounds go. Nil is silent.
	AudioSink backend.AudioSink
	SaveRoot  string
	Width     int
	Height    int
	MaxSteps  uint64
	// TraceSVC keeps the most recent platform calls so a failed start or tick
	// can say what the game was told just before it went wrong.
	TraceSVC int
	// TraceLive streams matching calls to TraceOut as they happen, for the
	// question the ring cannot answer. See lgt.Options.
	TraceLive string
	TraceOut  io.Writer
	// Speed is how fast the game runs against the wall. Zero and 1 are both
	// the speed it was written for; see Session.SetSpeed.
	Speed float64
	// Tick is how much guest time one Tick advances. The guest clock is
	// virtual, so a Host batching ticks sees the same sequence as one running
	// in real time, only faster.
	Tick time.Duration
}

const defaultTick = 50 * time.Millisecond

// minTimerPeriod is the shortest interval a timer is actually given. A title
// that asks for a one millisecond timer is not asking for a thousand frames a
// second — it is saying "as soon as you can", and on a handset the answer was
// its own frame time rounded up by whatever the timer could resolve. The
// specification says exactly that where it defines the call: a timer is
// accurate to "the timer resolution the operating system underneath supports",
// and error is to be expected.
//
// The guest's own work is the first bound and the honest one — the work clock
// measures it and `advance` takes the larger of the two — but it answers
// nearly zero for a title idling on a menu behind a one millisecond heartbeat,
// and the Host would then turn hundreds of rounds a second producing frames
// nothing shows. This is the second bound, and it is deliberately not a claim
// about any handset: it is one frame of a sixty hertz display, the fastest a
// screen anyone plays this on can present. It applies to the period rather
// than to the tick, so a title asking for 46ms still gets 46 — flooring the
// tick instead would round every interval up to a multiple of the floor, which
// is the same mistake as the fixed tick in miniature.
const minTimerPeriod = time.Second / 60

// sessionDefaultMaxSteps bounds one Clet call. A Clet does its whole startup —
// decompressing resources, building tables — inside startClet, which is more
// instructions than the core's own default allows, and a title stopped there
// looks like a hang rather than a budget.
//
// Entering the world is heavier still, and it happens inside a single timer
// callback: the title that loads the most here decompresses over a thousand
// resources into freshly allocated images, and it does the whole of that in one
// call. Fifty million — the other ARM platform's number — stopped it part way
// and read as a hang.
//
// **The number is measured, not chosen.** The heaviest local load falls between
// 1.1e9 and 1.2e9 instructions, bisected with `runlgt -steps`, so an earlier
// 1e9 stopped that title a little short of the world and reported an
// instruction limit against a callback that was doing real work. This is about
// two and a half times the measured worst case, which is the headroom a title
// heavier than any here would need; what it costs is that a runaway loop burns
// about a minute of guest work before it is reported rather than twenty
// seconds. That report is the only thing the ceiling buys — a spin does not
// stop at any budget, it only takes longer to say so.
const sessionDefaultMaxSteps uint64 = 3_000_000_000

// StartSession loads an archive, runs its initializer, and starts the Clet.
func StartSession(ctx context.Context, data []byte, options SessionOptions) (*Session, error) {
	archive, err := Open(data)
	if err != nil {
		return nil, err
	}
	store := options.SaveStore
	if store == nil && options.SaveRoot != "" {
		store = backend.NewDirectorySaveStore(options.SaveRoot)
	}
	maxSteps := options.MaxSteps
	if maxSteps == 0 {
		maxSteps = sessionDefaultMaxSteps
	}
	client, err := Load(archive, Options{
		Logger:    options.Logger,
		SaveStore: store,
		AudioSink: options.AudioSink,
		Width:     options.Width,
		Height:    options.Height,
		MaxSteps:  maxSteps,
		TraceSVC:  options.TraceSVC,
		TraceLive: options.TraceLive,
		TraceOut:  options.TraceOut,
	})
	if err != nil {
		return nil, err
	}
	// A title that fails during startup takes its client with it, and the
	// client is what holds the trace — which is exactly the run worth reading.
	// So the trace travels on the error.
	if err := client.Start(ctx); err != nil {
		return nil, &StartFailure{Err: err, Trace: client.SVCTrace()}
	}
	if err := client.StartClet(ctx); err != nil && !errors.Is(err, ErrGuestExited) {
		return nil, &StartFailure{Err: err, Trace: client.SVCTrace()}
	}
	tick := options.Tick
	if tick <= 0 {
		tick = defaultTick
	}
	return &Session{client: client, archive: archive, tick: tick, speed: backend.ClampSpeed(options.Speed)}, nil
}

// StartFailure is a startup that failed with a platform call trace attached.
// It only carries a trace when the caller asked for one.
type StartFailure struct {
	Err   error
	Trace []SVCCall
}

func (failure *StartFailure) Error() string { return failure.Err.Error() }
func (failure *StartFailure) Unwrap() error { return failure.Err }

// Archive is the loaded game.
func (session *Session) Archive() *Archive { return session.archive }

// SVCTrace returns the platform calls recorded for this session, oldest
// first, or nil when the session was started without a trace.
func (session *Session) SVCTrace() []SVCCall {
	if session == nil || session.client == nil {
		return nil
	}
	return session.client.SVCTrace()
}

// Tick advances the guest clock, fires whatever that made due, delivers
// queued events, and asks the game to paint.
func (session *Session) Tick(ctx context.Context) error {
	_, err := session.tickOnce(ctx)
	return err
}

// tickOnce is Tick, and also answers how much guest time the tick stood for.
// TickFor needs that number: it is what the Host then waits on the wall clock,
// and it is not the session's tick unless the guest had nothing sooner to do.
func (session *Session) tickOnce(ctx context.Context) (time.Duration, error) {
	if session == nil || session.client == nil {
		return 0, fmt.Errorf("LGT session is not started")
	}
	client := session.client
	// The tick stands for what the clock actually moved, which is the span it
	// set out to stand for unless the guest's own work overran it. `TickFor`
	// charges the wall clock with this number, so reporting the intent instead
	// of the outcome hands a title's computation back for free — see
	// `docs/lgt.md`, "The wall has to be charged for the work, not for the
	// intent".
	span := client.clock.advance(session.tickSpan())
	if err := client.serviceTimers(ctx); err != nil {
		return span, err
	}
	if err := client.serviceNetConnects(ctx); err != nil {
		return span, err
	}
	if err := client.deliverEvents(ctx); err != nil {
		return span, err
	}
	if err := client.PaintClet(ctx); err != nil {
		return span, err
	}
	// A Java title's own threads run before its frame is drawn: that is where
	// its work is done, and painting after it is what shows the result of this
	// tick rather than the last one. See java_thread.go.
	if _, err := client.ServiceJavaThreads(ctx); err != nil {
		return span, err
	}
	// A Java title has no Clet to paint: its frame is a call into the card the
	// application pushed. A title is one kind or the other, and the loop it is
	// not does nothing. See java_frame.go.
	if err := client.PaintJava(ctx); err != nil {
		return span, err
	}
	client.serviceAudio()
	// Frozen values are rewritten after the game has drawn, so a cheat wins
	// over whatever the tick just wrote rather than racing it.
	return span, session.serviceCheat()
}

// tickSpan is how much guest time the next tick stands for: the wait until the
// guest's own next scheduled work, bounded by the session tick.
//
// A title's frame loop here is a timer that re-arms itself at the end of every
// frame, so the interval it asks for is the rate it wants. A tick that always
// stands for the same span rounds that interval up to a multiple of itself,
// and the local titles ask for 1ms, 10ms, 46ms, 60ms and 83ms — every one of
// which becomes 50 or 100 under a fixed 50ms tick. That is the whole of the
// "everything is slow" report on this platform, and the error per title is the
// size of its own rounding: 46ms to 100 is half speed, 60 to 100 is two
// thirds, 83 to 100 is the one that reads as "slightly off".
//
// The bound matters in both directions. The session tick is the ceiling
// because a Host still has to take frames and deliver keys while a game waits
// out a long timer, and one tick of input latency is the same as before.
// The floor is the guest's own work: a title that asks for 1ms means "as soon
// as you can", and on a handset that is however long its frame takes — which
// is what the work clock already measures, so `advance` taking the larger of
// the two is what answers it.
func (session *Session) tickSpan() time.Duration {
	if session.client == nil {
		return session.tick
	}
	wait, pending := session.client.nextTimerDue()
	// A Java title arms no timer — its frame loop is a thread that sleeps —
	// so the same question has to be asked of the threads, and the tick
	// stands for whichever comes first. See nextJavaThreadDue for what the
	// rounding costs a title that sleeps 100ms on a 50ms tick.
	if threadWait, sleeping := session.client.nextJavaThreadDue(); sleeping &&
		(!pending || threadWait < wait) {
		wait, pending = threadWait, true
	}
	if !pending || wait > session.tick {
		return session.tick
	}
	if wait < 0 {
		// Already due: this tick is only carrying it to the callback, so it
		// stands for no time of its own.
		return 0
	}
	return wait
}

// TickFor runs one tick and reports how long the Host should wait before the
// next one is due. A Host on a real clock must use this rather than Tick.
//
// The guest clock here is virtual: a tick moves it by the session's tick and
// nothing else does. So the guest's speed is decided entirely by how often the
// Host ticks, and a Host that waits a fixed span between ticks runs the game at
// whatever ratio its own tick cost happens to produce — too fast on a cheap
// scene, slow motion on an expensive one, with nothing in the numbers saying
// so. Anchoring to the wall clock instead means one tick of guest time is asked
// to take one tick of real time, which is the only pace the game was written
// for.
//
// The wait is zero when the tick has already overrun; the game then runs slow,
// because a tick that costs more than it represents cannot be bought back. Debt
// is capped at a single tick so that a long stall — a world load inside one
// call — is not repaid by sprinting through the scene that follows it.
func (session *Session) TickFor(ctx context.Context) (time.Duration, error) {
	if session == nil || session.client == nil {
		return 0, fmt.Errorf("LGT session is not started")
	}
	if session.nextDue.IsZero() {
		session.nextDue = time.Now()
	}
	// The tick reports the guest time it stood for, and that is what is owed on
	// the wall clock. A tick that carried a timer already due stands for no
	// time at all, so the next one is due immediately — which is how the guest
	// gets a callback and its frame in the same instant rather than a tick
	// apart.
	span, err := session.tickOnce(ctx)
	now := time.Now()
	// The speed setting is applied here and nowhere else. The guest's clock is
	// virtual, so running the game faster is not a matter of telling it a
	// different time — it is a matter of buying its time more cheaply: at
	// twice the speed a tick standing for 20ms of guest time is owed 10ms of
	// wall time, and the Host comes back for the next one twice as soon.
	session.nextDue = session.nextDue.Add(session.realTime(span))
	if floor := now.Add(-session.realTime(session.tick)); session.nextDue.Before(floor) {
		session.nextDue = floor
	}
	wait := session.nextDue.Sub(now)
	if wait < 0 {
		wait = 0
	}
	return wait, err
}

// realTime is what a stretch of guest time costs on the wall at the speed in
// force.
func (session *Session) realTime(guest time.Duration) time.Duration {
	return time.Duration(float64(guest) / session.speedOrDefault())
}

func (session *Session) speedOrDefault() float64 {
	if session == nil || session.speed <= 0 {
		return 1
	}
	return session.speed
}

// SetSpeed scales how fast the game runs. It takes the same numbers the other
// platforms here take and means the same thing by them; what differs is only
// which clock underneath moves, and on this platform the guest's clock is the
// tick, so what moves is how often a tick is bought. See backend.ClampSpeed.
func (session *Session) SetSpeed(multiplier float64) {
	if session == nil {
		return
	}
	session.speed = backend.ClampSpeed(multiplier)
}

// Speed reports the multiplier in force.
func (session *Session) Speed() float64 {
	if session == nil {
		return 1
	}
	return session.speedOrDefault()
}

// GuestElapsed is how much time has passed on the guest's own clock. The clock
// is virtual here, so this is the only way to see the speed a Host is actually
// running the game at: comparing it against the wall says whether a tick of
// guest time is costing a tick of real time. The KTF session answers the same
// question under the same name.
func (session *Session) GuestElapsed() time.Duration {
	if session == nil || session.client == nil {
		return 0
	}
	return session.client.clock.now()
}

// Steps is how many guest instructions the run has retired. Host time per tick
// is not comparable across a change that alters how much guest work a tick
// holds, so a throughput change is judged on host nanoseconds per step — and
// that needs this beside the busy time a run already reports. The load probe
// answers the same question for a run with no route; this is how a run that
// was driven to a scene answers it, which is the only place a title is doing
// the work worth measuring.
func (session *Session) Steps() uint64 {
	if session == nil || session.client == nil || session.client.core == nil {
		return 0
	}
	return session.client.core.Steps()
}

// Flushes counts the presentations the game has asked for. A Host compares it
// before taking a frame, so an unchanged screen costs no conversion.
func (session *Session) Flushes() uint64 {
	if session == nil || session.client == nil {
		return 0
	}
	session.client.mu.Lock()
	defer session.client.mu.Unlock()
	return session.client.flushes
}

// Frame converts the LCD to RGBA for the Host. The conversion happens here
// rather than on every guest write because a Clet writes the framebuffer
// directly and would otherwise pay for a conversion per pixel.
//
// The bytes are the caller's own, as they are on every other platform here. It
// used to answer the buffer it converts into, which meant the next frame was
// written over the last one wherever a Host had put it — safe only for a Host
// that finished with a picture before asking for another, and Hosts do not:
// this one hands the frame to another goroutine to encode.
func (session *Session) Frame() ([]byte, int, int, bool) {
	if session == nil || session.client == nil {
		return nil, 0, 0, false
	}
	client := session.client
	client.mu.Lock()
	defer client.mu.Unlock()
	if !client.framePending {
		return append([]byte(nil), client.frameRGBA...), client.screen.width, client.screen.height, false
	}
	for index, pixel := range client.screen.pixels {
		red, green, blue := unpack565(pixel)
		client.frameRGBA[index*4] = byte(red)
		client.frameRGBA[index*4+1] = byte(green)
		client.frameRGBA[index*4+2] = byte(blue)
		client.frameRGBA[index*4+3] = 0xff
	}
	client.framePending = false
	return append([]byte(nil), client.frameRGBA...), client.screen.width, client.screen.height, true
}

// FrameDigest hashes what is on the screen, so a caller can tell a settled
// screen from an animating one without copying the frame. It reads the LCD
// itself rather than the converted RGBA, because the conversion only happens
// when a Host asks for a frame and a route asks far more often than that.
func (session *Session) FrameDigest() uint64 {
	if session == nil || session.client == nil {
		return 0
	}
	client := session.client
	client.mu.Lock()
	defer client.mu.Unlock()
	// FNV-1a over the pixels.
	const offset64, prime64 = uint64(14695981039346656037), uint64(1099511628211)
	digest := offset64
	for _, pixel := range client.screen.pixels {
		digest = (digest ^ uint64(pixel)) * prime64
	}
	return digest
}

// Clet event kinds. A Clet reads these from its handleCletEvent.
//
// They are not 1 and 2. A key arrives at a Clet through the same path a WIPI
// Java card's keyNotify(type, key) takes, with 501 added to the type — so the
// press and release types 1 and 2 reach the game as 502 and 503. Both engines
// here agree: one subtracts 0x1f6 and 0x1f7 from the kind and branches on zero,
// the other compares it against 0xfb<<1. Sending 1 and 2 delivers events every
// title ignores, which looks exactly like input not being wired up at all.
const (
	EventKeyPressed  uint32 = 502
	EventKeyReleased uint32 = 503
	EventKeyRepeated uint32 = 504
)

// The direction pad, in the WIPI values a Clet compares against. The four
// navigation keys are the pad a handset has; 2, 4, 6 and 8 are the same pad
// under the digits, which is how these titles were played on a keypad and how
// half the local routes drive them. The other digits are not: one title draws
// its skill shortcuts on 1, 3, 7, 9 and 0, and those are actions.
func isPadKey(keyCode uint32) bool {
	switch keyCode {
	case 0xFFFFFFFF, 0xFFFFFFFE, 0xFFFFFFFD, 0xFFFFFFFC: // up, down, left, right
		return true
	case '2', '4', '6', '8':
		return true
	}
	return false
}

// SendKey queues one key event for the game.
//
// The events are what the pad reports rather than what the keyboard did; see
// `internal/keypad` for the rule and `docs/lgt.md` "A release stops a
// character the pad has moved on from" for the screens that produced it.
func (session *Session) SendKey(pressed bool, keyCode uint32) {
	if session == nil || session.client == nil {
		return
	}
	if session.pad.IsPad == nil {
		session.pad.IsPad = func(code int32) bool { return isPadKey(uint32(code)) }
	}
	for _, event := range session.pad.Key(pressed, int32(keyCode)) {
		kind := EventKeyReleased
		if event.Pressed {
			kind = EventKeyPressed
		}
		session.client.SendEvent(kind, uint32(event.Code), 0)
	}
}

// Close ends the game.
func (session *Session) Close(ctx context.Context) error {
	if session == nil || session.client == nil {
		return nil
	}
	// A Java title's threads are goroutines parked inside guest code, and
	// nothing else will ever wake them: they have to be told the session is
	// over or they stay parked for the life of the process.
	session.client.StopJavaThreads()
	if err := session.client.DestroyClet(ctx); err != nil && !errors.Is(err, ErrGuestExited) {
		return err
	}
	return nil
}
