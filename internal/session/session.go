// Package session drives one emulation session without knowing what its Host
// intends to do with the frames. Every Host needs the same four moves —
// inspect an archive, start whatever it turns out to be, tick it, send it keys
// — and the differences between the platforms behind those moves belong here
// rather than in each entry point.
//
// The platforms are not the same shape underneath. KTF and LGT hand the Host a
// finished frame and count their own flushes; the MIDP runtimes draw into a
// framebuffer the Host supplies and have no tick of their own. A Host that
// wants a picture should not have to know which it is talking to, so this
// package presents one model: tick, ask whether the flush counter moved, take
// the frame if it did.
package session

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"sync"
	"time"

	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/cheat"
	"github.com/movingwoo/wfeature/internal/filter/hqx"
	"github.com/movingwoo/wfeature/internal/jvm"
	"github.com/movingwoo/wfeature/internal/keypad"
	"github.com/movingwoo/wfeature/internal/platform/detect"
	"github.com/movingwoo/wfeature/internal/platform/ktf"
	"github.com/movingwoo/wfeature/internal/platform/lgt"
	"github.com/movingwoo/wfeature/internal/platform/skt"
)

// DefaultWidth and DefaultHeight are the handset screen every platform here
// was written against.
const (
	DefaultWidth  = 240
	DefaultHeight = 320
)

// FramePace is what a tick asks for on the MIDP runtimes, which only run what
// a Host callback deferred and so never report themselves idle. In a browser
// that went unnoticed — the page ticked them from its frame clock — but a Host
// that believes "no wait" and ticks again immediately spins a core. One frame
// at sixty hertz is the clock they were written against.
//
// It is a poll interval and not a pace: those runtimes read the wall clock
// directly, so ticking them early or late changes only how promptly deferred
// work runs. LGT is the platform where that distinction matters, and it does
// not use this — a fixed wait there sets the game's speed rather than its
// responsiveness, which is why it paces itself instead.
const FramePace = 16 * time.Millisecond

// Summary names a game well enough to load its saves and label a run. Every
// platform answers with the same keys, so a Host can act on it without a
// switch of its own.
type Summary struct {
	Platform  string `json:"platform"`
	AID       string `json:"aid,omitempty"`
	PID       string `json:"pid,omitempty"`
	Name      string `json:"name,omitempty"`
	SaveOwner string `json:"save_owner,omitempty"`
	MainClass string `json:"main_class,omitempty"`
}

// Inspect reads an archive far enough to name the game and the directory its
// saves belong in. A broken archive still reports its platform: "this is a KTF
// archive and it is damaged" is a different problem from "I cannot tell what
// this is", and a Host should be able to say which.
func Inspect(archive []byte) (Summary, error) {
	platform, err := detect.Archive(archive)
	if err != nil {
		return Summary{}, err
	}
	summary := Summary{Platform: string(platform)}
	switch platform {
	case detect.KTF:
		// The vendor ships two generations of package, and only one of them
		// has a descriptor. The earlier one names itself in its module
		// information file instead. See docs/ktf.md, "An earlier KTF package".
		if ktf.IsNativeArchive(archive) {
			opened, err := ktf.OpenNative(archive)
			if err != nil {
				return summary, err
			}
			summary.AID = strconv.FormatUint(uint64(opened.Info.ApplicationID), 10)
			summary.Name = opened.Info.Name
			summary.SaveOwner = ktf.NativeSaveOwner(opened.Info)
			return summary, nil
		}
		opened, err := ktf.Open(archive)
		if err != nil {
			return summary, err
		}
		summary.AID = opened.Descriptor.AID
		summary.PID = opened.Descriptor.PID
		// The owner is the PID rather than the AID: distinct games ship the
		// same AID, and a save wins over packaged data, so an AID-keyed
		// directory would let one game shadow another's shipped files.
		summary.SaveOwner = ktf.SaveOwner(opened.Descriptor)
		summary.MainClass = opened.Descriptor.MainClass
	case detect.LGT:
		opened, err := lgt.Open(archive)
		if err != nil {
			return summary, err
		}
		summary.AID = opened.Descriptor.AID
		summary.PID = opened.Descriptor.PID
		summary.SaveOwner = lgt.SaveOwner(opened.Descriptor)
		summary.MainClass = opened.Descriptor.MClass
	case detect.SKT:
		opened, err := skt.Open(archive)
		if err != nil {
			return summary, err
		}
		summary.Name = opened.Descriptor.Name
		summary.SaveOwner = skt.SaveOwner(opened.Descriptor)
		summary.MainClass = opened.Descriptor.MainClass
	default:
		return summary, ErrUnsupportedArchive
	}
	return summary, nil
}

// Options configures a session. The zero value runs a silent session at the
// original speed on a handset-sized screen.
type Options struct {
	SaveStore backend.SaveStore
	AudioSink backend.AudioSink
	Logger    *slog.Logger

	// Speed scales the pace of the platforms that own a clock. Zero and 1 both
	// mean the speed the game was written for.
	Speed float64
	// Scale magnifies a finished frame with the hqx filter. It applies only to
	// the platforms that hand the Host a frame; the MIDP runtimes draw into
	// the surface they were given and enforce its size, so a magnified surface
	// there would reject every frame they produce.
	Scale int
	// Width and Height size the screen. Zero selects the handset default.
	Width, Height int
	// TraceLimit is how many recent boundary events the diagnostics keep in
	// order. Hosts pass a limit in debug builds and zero in release.
	TraceLimit int
}

func (o Options) width() int {
	if o.Width > 0 {
		return o.Width
	}
	return DefaultWidth
}

func (o Options) height() int {
	if o.Height > 0 {
		return o.Height
	}
	return DefaultHeight
}

func (o Options) scale() int {
	if o.Scale > 1 {
		return o.Scale
	}
	return 1
}

// Session is one running game. It is driven from a single goroutine — Tick,
// SendKey and Frame are not safe to call concurrently — which is what every
// Host does anyway, because guest code is not re-entrant.
type Session struct {
	platform detect.Platform
	summary  Summary
	options  Options

	ktf       *ktf.Session
	ktfNative *ktf.NativeSession
	lgt       *lgt.Session
	runtime   *skt.Runtime

	// surface receives frames on the platforms that draw into one. It also
	// gives those platforms a flush counter they do not otherwise have.
	surface *captureFramebuffer

	// startedAt anchors the guest audio timeline for the MIDP runtimes, which
	// are advanced by the Host rather than by a clock of their own.
	startedAt time.Time

	// repeat is the handset's key repeat, which this layer makes rather than
	// forwards. See repeatDue.
	repeat keypad.Repeat
	// held tracks what the WIPI Java path told the guest is down, so a repeat
	// can name it. That path delivers what the Host reports — the pad rule is
	// the other two platforms', for the reason `docs/ktf.md` measured — so the
	// zero value, which passes every key through, is the whole of it here. The
	// MIDP runtime has a pad of its own and answers for itself.
	held keypad.Pad
	// speed is the multiplier the Host last asked for, which the repeat clock
	// runs at. Zero is the speed the game was written for.
	speed float64
	// lastTick is when the repeat clock was last advanced, and now is that
	// clock — a seam a test moves by hand, because a cadence measured in
	// hundreds of milliseconds is not something to wait out.
	lastTick time.Time
	now      func() time.Time

	// scaler holds the magnification filter's working buffers between frames.
	// It is here rather than in the filter because a session is what has a
	// stream of frames, and one is what a Scaler belongs to; frames are taken
	// on one goroutine, which is the same discipline the platforms behind them
	// already require.
	scaler hqx.Scaler
}

// Start loads the archive, works out what it is, and runs it up to the point
// where it is waiting for its first tick.
//
// KTF's sliced start is not used here. It exists so a browser can keep
// painting during the tens of seconds a real startApp takes; a Host that can
// run this on its own goroutine has nothing to keep alive.
func Start(ctx context.Context, archive []byte, options Options) (*Session, error) {
	platform, err := detect.Archive(archive)
	if err != nil {
		return nil, err
	}
	summary, err := Inspect(archive)
	if err != nil {
		return nil, err
	}
	// The repeat clock takes the speed the same way SetSpeed gives it to it: a
	// session started at a multiplier is a guest already running at that pace.
	session := &Session{platform: platform, summary: summary, options: options, speed: options.Speed}

	switch platform {
	case detect.KTF:
		if ktf.IsNativeArchive(archive) {
			started, err := ktf.StartNativeSession(ctx, archive, ktf.NativeSessionOptions{
				SaveStore: options.SaveStore,
				AudioSink: options.AudioSink,
				Logger:    options.Logger,
				Speed:     options.Speed,
				Width:     options.width(),
				Height:    options.height(),
			})
			if err != nil {
				return nil, err
			}
			session.ktfNative = started
			session.startedAt = time.Now()
			return session, nil
		}
		started, err := ktf.StartSession(ctx, archive, ktf.SessionOptions{
			AudioSink:  options.AudioSink,
			SaveStore:  options.SaveStore,
			Speed:      options.Speed,
			TraceLimit: options.TraceLimit,
			Logger:     options.Logger,
			Width:      options.width(),
			Height:     options.height(),
		})
		if err != nil {
			return nil, err
		}
		started.TimeHostPhases(options.TraceLimit > 0)
		session.ktf = started
	case detect.LGT:
		started, err := lgt.StartSession(ctx, archive, lgt.SessionOptions{
			Logger:    options.Logger,
			AudioSink: options.AudioSink,
			SaveStore: options.SaveStore,
			Width:     options.width(),
			Height:    options.height(),
			Speed:     options.Speed,
		})
		if err != nil {
			return nil, err
		}
		session.lgt = started
	case detect.SKT:
		surface, err := newCaptureFramebuffer(options.width(), options.height())
		if err != nil {
			return nil, err
		}
		session.surface = surface
		opened, err := skt.Open(archive)
		if err != nil {
			return nil, err
		}
		runtime, err := skt.Start(opened, skt.Options{
			JVM:         jvm.Options{Logger: options.Logger},
			Framebuffer: surface,
			SaveStore:   options.SaveStore,
			Speed:       options.Speed,
		})
		// A failed start still hands back the runtime it got to, and closing
		// it is the Host's job either way.
		session.runtime = runtime
		if err != nil {
			session.Close()
			return nil, err
		}
		if options.AudioSink != nil {
			runtime.AttachAudioSink(options.AudioSink)
		}
	default:
		// Inspect already refused this, so reaching here means the two switches
		// disagree about which platforms exist. Returning a started-looking
		// session with nothing in it would be the worse failure.
		return nil, ErrUnsupportedArchive
	}
	session.startedAt = time.Now()
	return session, nil
}

// Platform reports which platform answered for the archive.
func (s *Session) Platform() string { return string(s.platform) }

// Summary reports the game's identity.
func (s *Session) Summary() Summary { return s.summary }

// KTF exposes the KTF session for the surfaces that only exist there — the
// profiler and the diagnostics report. It is nil on every other platform,
// which is the answer a Host needs anyway.
func (s *Session) KTF() *ktf.Session { return s.ktf }

// Cheat exposes the attached cheat engine. Every platform has one now. The two
// ARM platforms give the guest a flat address space a scan can sweep; the MIDP
// runtime has no address space at all, so it builds one over its object graph
// and answers that (`docs/skvm.md`, "A heap with addresses in it").
//
// A Host asks this rather than reaching through KTF(), which is what kept the
// engine LGT already had from ever being reachable from the browser.
func (s *Session) Cheat() *cheat.Session {
	switch {
	case s == nil:
		return nil
	case s.ktf != nil:
		return s.ktf.Cheat()
	case s.ktfNative != nil:
		return s.ktfNative.Cheat()
	case s.lgt != nil:
		return s.lgt.Cheat()
	case s.runtime != nil:
		return s.runtime.Cheat()
	default:
		return nil
	}
}

// CheatConsole is the text command console bound to Cheat(), and is nil
// wherever that is.
func (s *Session) CheatConsole() *cheat.Console {
	switch {
	case s == nil:
		return nil
	case s.ktf != nil:
		return s.ktf.CheatConsole()
	case s.ktfNative != nil:
		return s.ktfNative.CheatConsole()
	case s.lgt != nil:
		return s.lgt.CheatConsole()
	case s.runtime != nil:
		return s.runtime.CheatConsole()
	default:
		return nil
	}
}

// Progress is what one tick did.
type Progress struct {
	// Progressed reports whether guest code ran.
	Progressed bool
	// Wait is how long the guest itself asked to be left alone. A Host that
	// honours it runs the game at the pace it was written for; one that rounds
	// it up to a frame boundary loses the difference out of every wait, and a
	// game takes one between every pair of frames.
	Wait time.Duration
	// Flushes counts frames the game has finished. A Host takes a new frame
	// only when this moves.
	Flushes uint64
	// Exited reports that the game ended. The session is closed by then.
	Exited bool
}

// ErrNotRunning is returned once the session has ended.
var ErrNotRunning = errors.New("session: no game is running")

// ErrUnsupportedArchive is returned for an archive no vendor claimed. The
// vendors are KTF, LGT and SKT; a bare MIDlet with no carrier behind it is not
// one of them, and saying so is more use to a Host than trying to run it and
// failing somewhere further in.
var ErrUnsupportedArchive = errors.New("session: the archive is not a KTF, LGT or SKT game")

// Tick advances the game by at most budget of guest execution. The budget
// bounds how many service rounds are started rather than how long one takes:
// guest code cannot be suspended mid-call, so a tick routinely runs past it by
// the length of its last round.
func (s *Session) Tick(ctx context.Context, budget time.Duration) (Progress, error) {
	if progress, ended, err := s.repeatDue(ctx); ended || err != nil {
		return progress, err
	}
	switch {
	case s.ktf != nil:
		progressed, wait, err := s.ktf.TickFor(ctx, budget)
		progress := Progress{Progressed: progressed, Wait: wait, Flushes: uint64(s.ktf.Flushes())}
		if errors.Is(err, ktf.ErrGuestExited) {
			s.Close()
			progress.Exited = true
			return progress, nil
		}
		return progress, err
	case s.ktfNative != nil:
		// The earlier KTF package's title paces itself: it hands the platform
		// an interval and a function, and what is left of that interval is the
		// wait. See ktf.NativeSession.TickFor.
		progressed, wait, err := s.ktfNative.TickFor(ctx, budget)
		progress := Progress{Progressed: progressed, Wait: wait, Flushes: uint64(s.ktfNative.Flushes())}
		// The two generations end the same way for a Host, even though no slot
		// this package serves asks to exit yet: the day one does, a Host that
		// sorted the ending from a failure for one and not the other would
		// report a game that finished as a game that broke.
		if errors.Is(err, ktf.ErrGuestExited) {
			s.Close()
			progress.Exited = true
			return progress, nil
		}
		return progress, err
	case s.lgt != nil:
		// LGT paces itself against the wall clock rather than reporting a
		// frame's worth of wait: its guest clock only moves when a tick moves
		// it, so the wait is what decides the game's speed. See lgt.TickFor.
		wait, err := s.lgt.TickFor(ctx)
		progress := Progress{Progressed: true, Wait: wait, Flushes: s.lgt.Flushes()}
		if errors.Is(err, lgt.ErrGuestExited) {
			s.Close()
			progress.Exited = true
			return progress, nil
		}
		return progress, err
	case s.runtime != nil:
		// A MIDlet has no tick of its own: it runs on the callbacks the Host
		// makes and whatever those deferred. Its audio timeline is advanced by
		// the Host for the same reason, and on the guest's clock rather than
		// the wall so that a game running fast hears its music at the pace it
		// is playing at.
		s.runtime.AdvanceAudio(s.runtime.GuestElapsed())
		err := s.runtime.RunPending()
		// The pace is what the Host comes back at, so it carries the speed the
		// same way every other platform's wait does.
		progress := Progress{Progressed: true, Wait: guestPace(FramePace, s.runtime.Speed()), Flushes: s.surface.Flushes()}
		if state := s.runtime.State(); state == skt.StateDestroyed || state == skt.StateError {
			progress.Exited = true
		}
		return progress, err
	}
	return Progress{Exited: true}, ErrNotRunning
}

// Key actions, named the way a Host names them rather than the way any one
// platform numbers them.
const (
	KeyPress   = "press"
	KeyRelease = "release"
	KeyRepeat  = "repeat"
)

// ErrExited reports that the game ended while a Host call was running it. It
// is not a failure: a guest exits when the player picks the option that quits,
// and the key that picked it is the call that carries the news.
var ErrExited = errors.New("session: the game exited")

// SendKey delivers one key event. The code is the MIDP-style value the browser
// Host has always sent; the WIPI platforms translate it.
//
// A key runs guest code, so a game can end inside one — a menu whose confirm
// key is "quit" does exactly that. That ending is reported as ErrExited rather
// than as a failure, because a Host that shows it as a failure leaves the
// player looking at an error over the last frame of a game that simply
// finished. Tick has always reported this; the input paths had not.
func (s *Session) SendKey(ctx context.Context, action string, code int32) error {
	if action == KeyRepeat {
		// A Host's repeat is the operating system repeating a held keyboard
		// key, thirty a second at a cadence the user configured, and no
		// handset ever sent that. It is dropped on every platform: the two
		// that have a repeat event of their own are given the handset's
		// instead, on this session's clock rather than the browser's, and the
		// page has stopped sending these at all. See repeatDue.
		if !s.Running() {
			return ErrNotRunning
		}
		return nil
	}
	switch {
	case s.ktf != nil:
		eventType, ok := ktfKeyEventType(action)
		if !ok {
			return fmt.Errorf("session: unknown key action %q", action)
		}
		// The repeat clock names the key the guest was given, which is this
		// one: the WIPI Java path delivers what the Host reports.
		key := ktfKeyCode(code)
		s.held.Key(action == KeyPress, key)
		return s.endedOrFailed(s.ktf.SendKey(ctx, eventType, key))
	case s.ktfNative != nil:
		eventType, ok := ktfKeyEventType(action)
		if !ok {
			return fmt.Errorf("session: unknown key action %q", action)
		}
		return s.endedOrFailed(s.ktfNative.SendKey(ctx, eventType, ktfKeyCode(code)))
	case s.lgt != nil:
		// A Clet compares against the same key values a KTF game does, so the
		// translation is shared.
		pressed, deliver, ok := lgtKeyEvent(action)
		if !ok {
			return fmt.Errorf("session: unknown key action %q", action)
		}
		if !deliver {
			return nil
		}
		s.lgt.SendKey(pressed, uint32(ktfKeyCode(code)))
		return nil
	case s.runtime != nil:
		// The MIDP runtime takes the page's codes unchanged: they are the MIDP
		// values, and translating them is the WIPI path's business.
		return s.runtime.SendKey(skt.KeyEventType(action), code)
	}
	return ErrNotRunning
}

// repeatDue makes the handset's key repeat and delivers the one it owes, which
// is what a tick does before it runs the guest.
//
// **This layer makes the repeat rather than passing one on.** The two runtimes
// with a repeat event of their own — WIPI Java's `keyNotify` and a MIDP
// Canvas's `keyRepeated` — used to be handed the browser's, which is the
// operating system repeating a held keyboard key at whatever cadence the user
// configured. A handset's is `KEYREPEAT`, "600:250": the first after 600ms,
// then one every 250ms (`docs/lgt.md`, "A Host repeat is not a second press",
// which measured what the two cadences do to a title). Making it here also
// gives the on-screen keypad one, which the page never sent: a thumb holding a
// button on a touchscreen is a key held down like any other.
//
// It returns a Progress and true only where the repeat ended the game, which a
// key can do on any platform that has a menu with "quit" in it.
func (s *Session) repeatDue(ctx context.Context) (Progress, bool, error) {
	elapsed := s.guestSinceLastTick()
	s.repeat.Holding(s.heldKey())
	code, due := s.repeat.Due(elapsed)
	if !due {
		return Progress{}, false, nil
	}
	// Taken before the delivery: a guest that exits inside it leaves nothing
	// to ask afterwards, and the frame it exited on is the one to draw.
	flushes := s.Flushes()
	switch err := s.deliverRepeat(ctx, code); {
	case err == nil:
		return Progress{}, false, nil
	case errors.Is(err, ErrExited):
		return Progress{Flushes: flushes, Exited: true}, true, nil
	default:
		return Progress{Flushes: flushes}, true, err
	}
}

// heldKey answers what the guest believes is down, which is the pad's answer
// rather than the Host's: a thumb that rolled off one direction onto another
// is holding a key the Host never pressed a second time. The platforms with no
// repeat event answer false — LGT's Clet events are a press and a release, and
// so are the earlier KTF package's.
func (s *Session) heldKey() (int32, bool) {
	switch {
	case s.ktf != nil:
		return s.held.Held()
	case s.runtime != nil:
		return s.runtime.HeldKey()
	}
	return 0, false
}

// deliverRepeat hands one repeat to a platform that has the event.
func (s *Session) deliverRepeat(ctx context.Context, code int32) error {
	switch {
	case s.ktf != nil:
		return s.endedOrFailed(s.ktf.SendKey(ctx, ktf.KeyRepeated, code))
	case s.runtime != nil:
		return s.runtime.SendKey(skt.KeyRepeated, code)
	}
	return nil
}

// guestSinceLastTick is how much of the guest's time has passed since the last
// tick asked. It is the wall's, scaled by the speed the Host set, because that
// is what a multiplier means everywhere else here: the guest's clock runs at
// that rate and the waits between its callbacks cost that much less. A handset
// running twice as fast repeated a held key twice as often.
func (s *Session) guestSinceLastTick() time.Duration {
	now := time.Now
	if s.now != nil {
		now = s.now
	}
	at := now()
	if s.lastTick.IsZero() {
		s.lastTick = at
		return 0
	}
	elapsed := at.Sub(s.lastTick)
	s.lastTick = at
	if elapsed <= 0 {
		return 0
	}
	if s.speed > 0 {
		return time.Duration(float64(elapsed) * s.speed)
	}
	return elapsed
}

// endedOrFailed sorts a guest's ending from a real failure. Both arrive as an
// error from whatever Host call was running guest code at the time, and a Host
// that cannot tell them apart shows the player a failure over the last frame of
// a game that simply finished.
func (s *Session) endedOrFailed(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, ktf.ErrGuestExited) || errors.Is(err, lgt.ErrGuestExited) {
		s.Close()
		return ErrExited
	}
	return err
}

// Frame answers the current picture, magnified if the Host asked for it. ok is
// false when there is nothing to show yet.
//
// **The bytes belong to the caller.** Every platform behind this answers with a
// picture of its own rather than the one it is still drawing into, so a Host
// may hold a frame across ticks, hand it to another goroutine to encode, or
// keep it as the last thing shown — none of which is safe against a live
// buffer, and all of which is what a Host does with a frame. It also means a
// Host has no reason to copy one on the way out.
func (s *Session) Frame() (rgba []byte, width, height int, ok bool) {
	switch {
	case s.ktf != nil:
		frame, frameWidth, frameHeight, _ := s.ktf.Frame()
		return s.magnify(frame, frameWidth, frameHeight)
	case s.ktfNative != nil:
		frame, frameWidth, frameHeight, _ := s.ktfNative.Frame()
		return s.magnify(frame, frameWidth, frameHeight)
	case s.lgt != nil:
		frame, frameWidth, frameHeight, _ := s.lgt.Frame()
		return s.magnify(frame, frameWidth, frameHeight)
	case s.surface != nil:
		frame, frameWidth, frameHeight := s.surface.Frame()
		// The MIDP runtimes own their surface's size, so what they drew is
		// what is shown.
		if len(frame) == 0 || frameWidth <= 0 || frameHeight <= 0 {
			return nil, 0, 0, false
		}
		return frame, frameWidth, frameHeight, true
	}
	return nil, 0, 0, false
}

// Screen is the guest's own screen, before any magnification. A Host lays out
// around it and reports it to whatever is drawing the picture, so what it has
// to answer is the size the platform actually took rather than the size the
// Host asked for. All three honour the request now, and a platform may still
// answer with a size of its own: what the guest took is the only answer a Host
// can lay out against.
//
// It answers the requested size before the first frame exists, because a Host
// that has just started a game asks then.
func (s *Session) Screen() (width, height int) {
	switch {
	case s.ktf != nil:
		// The frame is the answer once there is one, because it is built from
		// the screen the game was actually given rather than from what was
		// asked for. Before that, the request stands.
		if _, frameWidth, frameHeight, _ := s.ktf.Frame(); frameWidth > 0 && frameHeight > 0 {
			return frameWidth, frameHeight
		}
		return s.options.width(), s.options.height()
	case s.ktfNative != nil:
		// The earlier package answers the same way: the title asks for its
		// size once and draws into what it is given, so the frame is what it
		// actually got.
		if _, frameWidth, frameHeight, _ := s.ktfNative.Frame(); frameWidth > 0 && frameHeight > 0 {
			return frameWidth, frameHeight
		}
		return s.options.width(), s.options.height()
	case s.lgt != nil:
		if _, frameWidth, frameHeight, _ := s.lgt.Frame(); frameWidth > 0 && frameHeight > 0 {
			return frameWidth, frameHeight
		}
	case s.surface != nil:
		return s.surface.Dimensions()
	}
	return s.options.width(), s.options.height()
}

// GuestElapsed is how much time has passed on the guest's own clock, and ok
// reports whether the platform has one to answer with.
//
// It is the only way a Host sees slow motion. A session that cannot keep up
// drops no frames and reports no error — it simply gives the game less time
// than the wall gave it, and every other number a Host has stays plausible
// while it happens. Comparing this against the wall over the same window is
// what says so: three quarters of a second of guest time in a second of real
// time is a game running at three quarters speed.
//
// The MIDP runtimes answer false. They read the wall clock directly rather
// than being advanced by a Host, so there is no second clock to compare.
func (s *Session) GuestElapsed() (time.Duration, bool) {
	switch {
	case s.ktf != nil:
		return s.ktf.GuestElapsed(), true
	case s.ktfNative != nil:
		return s.ktfNative.GuestElapsed(), true
	case s.lgt != nil:
		return s.lgt.GuestElapsed(), true
	}
	return 0, false
}

// Flushes reports how many frames the game has finished, which is what a Host
// compares against to decide whether Frame is worth calling.
func (s *Session) Flushes() uint64 {
	switch {
	case s.ktf != nil:
		return uint64(s.ktf.Flushes())
	case s.ktfNative != nil:
		return uint64(s.ktfNative.Flushes())
	case s.lgt != nil:
		return s.lgt.Flushes()
	case s.surface != nil:
		return s.surface.Flushes()
	}
	return 0
}

func (s *Session) magnify(frame []byte, width, height int) ([]byte, int, int, bool) {
	if len(frame) == 0 || width <= 0 || height <= 0 {
		return nil, 0, 0, false
	}
	scale := s.options.scale()
	if scale == 1 {
		return frame, width, height, true
	}
	// Magnifying here rather than in the Host keeps one implementation of the
	// filter and hands the display pixels it can draw without resampling. A
	// filter that refuses the frame gives back the original: a smaller picture
	// beats no picture, and the size travels with every frame anyway.
	scaled, scaledWidth, scaledHeight, err := s.scaler.ScaleRGBA(frame, width, height, scale)
	if err != nil {
		return frame, width, height, true
	}
	return scaled, scaledWidth, scaledHeight, true
}

// SetSpeed changes the pace of a running game. Every platform here answers it,
// and every one means the same thing by a multiplier — the guest's clock runs
// at that rate and the waits between its callbacks cost that much less — even
// though what is underneath differs: a virtual clock the tick moves on LGT, a
// scaled wall clock on KTF, and the VM's own sleeps on a MIDlet.
func (s *Session) SetSpeed(multiplier float64) {
	// The repeat clock is scaled by hand because it is this layer's own; every
	// platform below scales its clock for itself.
	s.speed = multiplier
	if s.ktf != nil {
		s.ktf.SetSpeed(multiplier)
	}
	if s.ktfNative != nil {
		s.ktfNative.SetSpeed(multiplier)
	}
	if s.lgt != nil {
		s.lgt.SetSpeed(multiplier)
	}
	if s.runtime != nil {
		s.runtime.SetSpeed(multiplier)
	}
}

// guestPace is what an interval of guest time costs a Host at a given speed.
func guestPace(interval time.Duration, speed float64) time.Duration {
	if speed <= 0 {
		return interval
	}
	return time.Duration(float64(interval) / speed)
}

// SetScale changes the magnification applied to later frames.
func (s *Session) SetScale(scale int) {
	if scale < 1 {
		scale = 1
	}
	s.options.Scale = scale
}

// Scale reports the magnification in effect.
func (s *Session) Scale() int { return s.options.scale() }

// Running reports whether there is still a game to tick.
func (s *Session) Running() bool {
	return s.ktf != nil || s.ktfNative != nil || s.lgt != nil || s.runtime != nil
}

// Close ends the session. It is safe to call more than once, and safe to call
// on a session whose game already exited.
func (s *Session) Close() {
	if s.ktf != nil {
		s.ktf.Close()
		s.ktf = nil
	}
	if s.ktfNative != nil {
		s.ktfNative.Close()
		s.ktfNative = nil
	}
	if s.lgt != nil {
		_ = s.lgt.Close(context.Background())
		s.lgt = nil
	}
	if s.runtime != nil {
		_ = s.runtime.Destroy(true)
		s.runtime = nil
	}
	// Nothing is held once there is no game to hold it, and a repeat that
	// outlived its session would name a key on a platform that is gone.
	s.repeat.Forget()
	s.held.Forget()
	// The captured surface goes with it, so a closed session answers the same
	// way on every platform: there is no frame, because there is no game. A
	// Host that wants the last picture on screen already has it — it was sent
	// when it was flushed.
	s.surface = nil
}

// ktfKeyEventType maps the Host event names shared with the MIDP path to the
// WIPI keyNotify types.
// A Host's repeat is not one of them: SendKey drops it before it gets here,
// and the repeat this platform does deliver is made by repeatDue.
func ktfKeyEventType(action string) (int32, bool) {
	switch action {
	case KeyPress:
		return ktf.KeyPressed, true
	case KeyRelease:
		return ktf.KeyReleased, true
	}
	return 0, false
}

// lgtKeyEvent says what one Host key action becomes on the Clet platform, and
// deliver is what a repeat needs: nothing is sent for one.
//
// A held keyboard key is repeated by the operating system, the page forwards
// those keydowns as repeats, and this platform has no event kind for them —
// its Clet events are pressed and released, and it generates no
// `MV_KEY_REPEAT_EVENT` because no title has asked for the cadence
// (`docs/lgt.md` "Nothing repeats a held key"). Sending one on as a press is
// therefore not a translation but an invention: a second press with no release
// between is a thing no handset delivers, and titles read a press as a fresh
// tap. Two of the local ones dash on that tap, so holding a direction dashed
// again every time the operating system repeated the key.
func lgtKeyEvent(action string) (pressed, deliver, ok bool) {
	switch action {
	case KeyPress:
		return true, true, true
	case KeyRelease:
		return false, true, true
	}
	return false, false, false
}

// ktfKeyCode converts the browser Host's MIDP-style key codes to the WIPI
// values a WIPI game compares against; digits, star, and hash pass through.
func ktfKeyCode(code int32) int32 {
	switch code {
	case 141:
		return ktf.KeyUp
	case 146:
		return ktf.KeyDown
	case 142:
		return ktf.KeyLeft
	case 145:
		return ktf.KeyRight
	case 148:
		return ktf.KeyFire
	case 6:
		return ktf.KeyLeftSoft
	case 7:
		return ktf.KeyRightSoft
	case 9:
		return ktf.KeyThirdSoft
	case 8:
		return ktf.KeyClear
	case 10:
		return ktf.KeyCall
	case -1:
		return ktf.KeyHangup
	case 13:
		return ktf.KeyVolumeUp
	case 14:
		return ktf.KeyVolumeDown
	}
	return code
}

// captureFramebuffer is the surface the MIDP runtimes draw into. It keeps the
// most recent frame and counts them, which is what gives those platforms the
// same "has anything changed?" answer the WIPI ones report themselves.
type captureFramebuffer struct {
	width, height int

	mutex   sync.Mutex
	rgba    []byte
	flushes uint64
}

func newCaptureFramebuffer(width, height int) (*captureFramebuffer, error) {
	length, err := backend.RGBAByteLength(width, height)
	if err != nil {
		return nil, err
	}
	return &captureFramebuffer{width: width, height: height, rgba: make([]byte, length)}, nil
}

func (f *captureFramebuffer) Dimensions() (int, int) { return f.width, f.height }

func (f *captureFramebuffer) Present(frame backend.Frame) error {
	if frame.Width != f.width || frame.Height != f.height {
		return fmt.Errorf("frame %dx%d does not match the surface %dx%d",
			frame.Width, frame.Height, f.width, f.height)
	}
	length, err := backend.RGBAByteLength(frame.Width, frame.Height)
	if err != nil {
		return err
	}
	if len(frame.RGBA) != length {
		return fmt.Errorf("RGBA frame length is %d, want %d", len(frame.RGBA), length)
	}
	// The frame is only valid for the duration of Present, so the bytes are
	// copied rather than retained.
	f.mutex.Lock()
	copy(f.rgba, frame.RGBA)
	f.flushes++
	f.mutex.Unlock()
	return nil
}

func (f *captureFramebuffer) Flushes() uint64 {
	f.mutex.Lock()
	defer f.mutex.Unlock()
	return f.flushes
}

// Frame answers a copy of the retained picture, which is what the WIPI
// platforms' own Frame calls hand back too.
//
// It used to answer the retained bytes themselves, on the understanding that
// the caller reads them before ticking again. That understanding does not hold
// here: a MIDlet's own threads are goroutines, so one of them can paint — and
// Present can copy into these very bytes — while the Host is still reading the
// last picture. Copying under the lock is what makes the answer a picture
// rather than a view of one being redrawn.
func (f *captureFramebuffer) Frame() ([]byte, int, int) {
	f.mutex.Lock()
	defer f.mutex.Unlock()
	if f.flushes == 0 {
		return nil, 0, 0
	}
	return append([]byte(nil), f.rgba...), f.width, f.height
}
