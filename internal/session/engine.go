package session

import (
	"context"
	"fmt"
	"time"

	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/cheat"
	"github.com/movingwoo/wfeature/internal/platform/ktf"
	"github.com/movingwoo/wfeature/internal/platform/lgt"
	"github.com/movingwoo/wfeature/internal/platform/skt"
	"github.com/movingwoo/wfeature/internal/platform/skt/clet"
)

// engine is the platform session running a game, as this package asks it
// things. Each kind of platform session has an adapter here, and every question
// a session asks of its platform is a method of this interface, so a question
// added for one platform does not compile until every adapter has answered it.
// That is what a branch left out of a per-platform switch never did: a Clet's
// ending and its authentication were both missed that way, on every path but
// the one that had been written for it.
//
// A platform with nothing to say answers so in its own adapter, which is also
// where the reason is written down.
type engine interface {
	// tick runs one Host tick of guest execution. A guest that ended itself
	// through its platform's exit call answers with that platform's sentinel,
	// which guestExited recognizes; a platform that learns of an ending some
	// other way reports it in the result.
	tick(ctx context.Context, budget time.Duration) (tickResult, error)
	flushes() uint64
	// sendKey delivers one Host key press or release, in the MIDP-style codes
	// the browser Host sends, to a session that is running.
	sendKey(s *Session, ctx context.Context, action string, code int32) error
	// sendPointer delivers one touch, already in WIPI event values;
	// ErrNoPointer is the answer of a platform with no touch surface.
	sendPointer(ctx context.Context, eventType, x, y int32) error
	hasPointer() bool
	// heldKey and repeat are the handset's key repeat: what the guest believes
	// is down, and one repeat of it on a platform that has the event.
	heldKey(s *Session) (int32, bool)
	repeat(ctx context.Context, code int32) error
	pause(ctx context.Context) error
	resume(ctx context.Context) error
	close()

	// frame answers the current picture. owned reports a platform that owns
	// its surface's size, whose picture is shown as drawn.
	frame() (rgba []byte, width, height int, owned bool)
	// screen is the guest's screen, and false where the platform has no answer
	// of its own before its first frame.
	screen() (width, height int, ok bool)
	vibration() (backend.Vibration, bool)
	guestElapsed() (time.Duration, bool)
	// setSpeed answers the multiplier the platform accepted.
	setSpeed(multiplier float64) float64
	speed() float64
	setFrameSink(sink backend.FrameSink)
	skipToNextDeadline() bool
	virtualClock() bool

	cheat() *cheat.Session
	cheatConsole() *cheat.Console
	authentication() backend.AuthenticationStatus
	uncaughtCallbacks() (uint64, string)
	textInput(ctx context.Context) (*backend.TextInput, error)

	canCheckpoint() bool
	captureCheckpoint(ctx context.Context, captureShared func() ([]byte, error)) (backend.Checkpoint, error)
	resumeCheckpointOutput()
}

// tickResult is what one platform tick reported.
type tickResult struct {
	progressed bool
	wait       time.Duration
	// exited reports an ending the platform learned of from a state rather
	// than from its exit call, and reason is what it knows of it.
	exited bool
	reason string
}

// sendWIPIKey is a key on a platform that numbers keys the WIPI way. pad says
// whether the repeat clock follows the key: it does on the platforms whose
// key events it can repeat, and on a Clet, whose pad the checkpoint records.
func (s *Session) sendWIPIKey(ctx context.Context, action string, code int32, pad bool, send func(context.Context, int32, int32) error) error {
	eventType, ok := ktfKeyEventType(action)
	if !ok {
		return fmt.Errorf("session: unknown key action %q", action)
	}
	if err := s.checkKeyHold(action, code); err != nil {
		return err
	}
	key := ktfKeyCode(code)
	if pad {
		// The repeat clock names the key the guest was given, which is this
		// one: the WIPI Java path delivers what the Host reports.
		s.held.Key(action == KeyPress, key)
	}
	if err := s.endedOrFailed(send(ctx, eventType, key)); err != nil {
		return err
	}
	s.noteKeyHold(action, code)
	return nil
}

// ktfEngine is a KTF title packaged with a descriptor: WIPI Java, with the
// WIPI-C module some of them carry.
type ktfEngine struct{ session *ktf.Session }

func (e ktfEngine) tick(ctx context.Context, budget time.Duration) (tickResult, error) {
	progressed, wait, err := e.session.TickFor(ctx, budget)
	return tickResult{progressed: progressed, wait: wait}, err
}

func (e ktfEngine) flushes() uint64 { return uint64(e.session.Flushes()) }

func (e ktfEngine) sendKey(s *Session, ctx context.Context, action string, code int32) error {
	return s.sendWIPIKey(ctx, action, code, true, e.session.SendKey)
}

func (e ktfEngine) sendPointer(ctx context.Context, eventType, x, y int32) error {
	return e.session.SendPointer(ctx, eventType, x, y)
}

// hasPointer is the title's answer and not the platform's; see
// Session.HasPointer.
func (e ktfEngine) hasPointer() bool { return e.session.HasPointer() }

func (e ktfEngine) heldKey(s *Session) (int32, bool) { return s.held.Held() }

func (e ktfEngine) repeat(ctx context.Context, code int32) error {
	return e.session.SendKey(ctx, ktf.KeyRepeated, code)
}

func (e ktfEngine) pause(ctx context.Context) error  { return e.session.Pause(ctx) }
func (e ktfEngine) resume(ctx context.Context) error { return e.session.Resume(ctx) }
func (e ktfEngine) close()                           { e.session.Close() }

func (e ktfEngine) frame() ([]byte, int, int, bool) {
	rgba, width, height, _ := e.session.Frame()
	return rgba, width, height, false
}

// screen is the frame once there is one, because it is built from the screen
// the game was actually given rather than from what was asked for. Before
// that, the platform's own: a start that named no screen took the one the
// archive's descriptor declares, which the Host's default does not know. The
// platform answers both without copying the frame.
func (e ktfEngine) screen() (int, int, bool) {
	width, height := e.session.Screen()
	return width, height, true
}

func (e ktfEngine) vibration() (backend.Vibration, bool) { return e.session.Vibration(), true }
func (e ktfEngine) guestElapsed() (time.Duration, bool)  { return e.session.GuestElapsed(), true }

// setSpeed answers the rate KTF took: an origin outside the duration range
// cannot be rebased, and input repeat and checkpoint metadata stay at the
// accepted guest rate.
func (e ktfEngine) setSpeed(multiplier float64) float64 {
	e.session.SetSpeed(multiplier)
	return e.session.Speed()
}

func (e ktfEngine) speed() float64 { return e.session.Speed() }

func (e ktfEngine) setFrameSink(sink backend.FrameSink) { e.session.Client.SetFrameSink(sink) }
func (e ktfEngine) skipToNextDeadline() bool            { return e.session.SkipToNextDeadline() }
func (e ktfEngine) virtualClock() bool                  { return false }
func (e ktfEngine) cheat() *cheat.Session               { return e.session.Cheat() }
func (e ktfEngine) cheatConsole() *cheat.Console        { return e.session.CheatConsole() }

func (e ktfEngine) authentication() backend.AuthenticationStatus {
	return e.session.Authentication()
}

func (e ktfEngine) uncaughtCallbacks() (uint64, string) { return e.session.Client.UncaughtCallbacks() }

func (e ktfEngine) textInput(ctx context.Context) (*backend.TextInput, error) {
	return e.session.TextInput(ctx)
}

func (e ktfEngine) canCheckpoint() bool { return true }

func (e ktfEngine) captureCheckpoint(ctx context.Context, captureShared func() ([]byte, error)) (backend.Checkpoint, error) {
	return e.session.CaptureCheckpointWithSession(ctx, captureShared)
}

func (e ktfEngine) resumeCheckpointOutput() { e.session.ResumeCheckpointOutput() }

// ktfNativeEngine is the earlier KTF package, a module entered through one
// event callback. It paces itself: it hands the platform an interval and a
// function, and what is left of that interval is the wait. See
// ktf.NativeSession.TickFor.
type ktfNativeEngine struct{ session *ktf.NativeSession }

// tick ends the way a descriptor package's does, even though no slot this
// package serves asks to exit yet: the day one does, a Host that sorted the
// ending from a failure for one generation and not the other would report a
// game that finished as a game that broke.
func (e ktfNativeEngine) tick(ctx context.Context, budget time.Duration) (tickResult, error) {
	progressed, wait, err := e.session.TickFor(ctx, budget)
	return tickResult{progressed: progressed, wait: wait}, err
}

func (e ktfNativeEngine) flushes() uint64 { return uint64(e.session.Flushes()) }

func (e ktfNativeEngine) sendKey(s *Session, ctx context.Context, action string, code int32) error {
	return s.sendWIPIKey(ctx, action, code, false, e.session.SendKey)
}

// sendPointer has nothing to deliver to: the module's one event callback has
// no pointer in it.
func (e ktfNativeEngine) sendPointer(context.Context, int32, int32, int32) error {
	return ErrNoPointer
}

func (e ktfNativeEngine) hasPointer() bool { return false }

// heldKey is false and repeat delivers nothing: this package's events are a
// press and a release.
func (e ktfNativeEngine) heldKey(*Session) (int32, bool)      { return 0, false }
func (e ktfNativeEngine) repeat(context.Context, int32) error { return nil }

// pause and resume have no lifecycle to call: the module is entered through
// one event callback and declares no pause entry.
func (e ktfNativeEngine) pause(context.Context) error  { return nil }
func (e ktfNativeEngine) resume(context.Context) error { return nil }
func (e ktfNativeEngine) close()                       { e.session.Close() }

func (e ktfNativeEngine) frame() ([]byte, int, int, bool) {
	rgba, width, height, _ := e.session.Frame()
	return rgba, width, height, false
}

// screen is the frame: the title asks for its size once and draws into what
// it is given, so the frame is what it actually got.
func (e ktfNativeEngine) screen() (int, int, bool) {
	_, width, height, _ := e.session.Frame()
	return width, height, width > 0 && height > 0
}

// vibration is not answered: the native table has no vibrator call at all,
// and saying so is what keeps a Host from buzzing on a request nobody made.
func (e ktfNativeEngine) vibration() (backend.Vibration, bool) { return backend.Vibration{}, false }

func (e ktfNativeEngine) guestElapsed() (time.Duration, bool) {
	return e.session.GuestElapsed(), true
}

func (e ktfNativeEngine) setSpeed(multiplier float64) float64 {
	e.session.SetSpeed(multiplier)
	return multiplier
}

func (e ktfNativeEngine) speed() float64                 { return e.session.Speed() }
func (e ktfNativeEngine) setFrameSink(backend.FrameSink) {}
func (e ktfNativeEngine) skipToNextDeadline() bool       { return e.session.SkipToNextDeadline() }
func (e ktfNativeEngine) virtualClock() bool             { return false }
func (e ktfNativeEngine) cheat() *cheat.Session          { return e.session.Cheat() }
func (e ktfNativeEngine) cheatConsole() *cheat.Console   { return e.session.CheatConsole() }

// authentication is unsupported: the earlier package has no check to adapt.
func (e ktfNativeEngine) authentication() backend.AuthenticationStatus {
	return backend.AuthenticationUnsupported
}

// uncaughtCallbacks is none: the earlier package runs no Java.
func (e ktfNativeEngine) uncaughtCallbacks() (uint64, string) { return 0, "" }

// textInput is never offered: the earlier package has no input method.
func (e ktfNativeEngine) textInput(context.Context) (*backend.TextInput, error) {
	return nil, backend.ErrNoTextInput
}

func (e ktfNativeEngine) canCheckpoint() bool { return true }

func (e ktfNativeEngine) captureCheckpoint(ctx context.Context, captureShared func() ([]byte, error)) (backend.Checkpoint, error) {
	return e.session.CaptureCheckpointWithSession(ctx, captureShared)
}

func (e ktfNativeEngine) resumeCheckpointOutput() { e.session.ResumeCheckpointOutput() }

// cletEngine is a native SKT program. It compares against the same key values
// a KTF game does, and runs on the same WIPI-C engine.
type cletEngine struct{ session *clet.Session }

func (e cletEngine) tick(ctx context.Context, budget time.Duration) (tickResult, error) {
	progressed, wait, err := e.session.TickFor(ctx, budget)
	return tickResult{progressed: progressed, wait: wait}, err
}

func (e cletEngine) flushes() uint64 { return uint64(e.session.Flushes()) }

func (e cletEngine) sendKey(s *Session, ctx context.Context, action string, code int32) error {
	return s.sendWIPIKey(ctx, action, code, true, e.session.SendKey)
}

// sendPointer has nothing to deliver to: a Clet's event kinds are keys.
func (e cletEngine) sendPointer(context.Context, int32, int32, int32) error { return ErrNoPointer }
func (e cletEngine) hasPointer() bool                                       { return false }

// heldKey is false and repeat delivers nothing: a Clet's events are a press
// and a release, with no repeat event among them.
func (e cletEngine) heldKey(*Session) (int32, bool)      { return 0, false }
func (e cletEngine) repeat(context.Context, int32) error { return nil }

func (e cletEngine) pause(ctx context.Context) error  { return e.session.Pause(ctx) }
func (e cletEngine) resume(ctx context.Context) error { return e.session.Resume(ctx) }
func (e cletEngine) close()                           { e.session.Close() }

func (e cletEngine) frame() ([]byte, int, int, bool) {
	rgba, width, height, _ := e.session.Frame()
	return rgba, width, height, false
}

func (e cletEngine) screen() (int, int, bool) {
	width, height := e.session.Screen()
	return width, height, true
}

func (e cletEngine) vibration() (backend.Vibration, bool) { return e.session.Vibration(), true }
func (e cletEngine) guestElapsed() (time.Duration, bool)  { return e.session.GuestElapsed(), true }

func (e cletEngine) setSpeed(multiplier float64) float64 {
	e.session.SetSpeed(multiplier)
	return multiplier
}

func (e cletEngine) speed() float64                      { return e.session.Speed() }
func (e cletEngine) setFrameSink(sink backend.FrameSink) { e.session.Client.SetFrameSink(sink) }
func (e cletEngine) skipToNextDeadline() bool            { return e.session.SkipToNextDeadline() }
func (e cletEngine) virtualClock() bool                  { return false }

// cheat is nil: the cheat console is not wired to a native SKT program yet.
func (e cletEngine) cheat() *cheat.Session        { return nil }
func (e cletEngine) cheatConsole() *cheat.Console { return nil }

func (e cletEngine) authentication() backend.AuthenticationStatus {
	return e.session.Authentication()
}

// uncaughtCallbacks is none: a Clet runs no Java.
func (e cletEngine) uncaughtCallbacks() (uint64, string) { return 0, "" }

func (e cletEngine) textInput(ctx context.Context) (*backend.TextInput, error) {
	return e.session.TextInput(ctx)
}

func (e cletEngine) canCheckpoint() bool { return e.session.CanCheckpoint() }

func (e cletEngine) captureCheckpoint(ctx context.Context, captureShared func() ([]byte, error)) (backend.Checkpoint, error) {
	return e.session.CaptureCheckpointWithSession(ctx, captureShared)
}

func (e cletEngine) resumeCheckpointOutput() { e.session.ResumeCheckpointOutput() }

// lgtEngine is an LGT title, a Clet or a Java application on the same module
// loader.
type lgtEngine struct{ session *lgt.Session }

// tick paces against the wall clock rather than reporting a frame's worth of
// wait: the guest clock only moves when a tick moves it, so the wait is what
// decides the game's speed. See lgt.TickFor.
func (e lgtEngine) tick(ctx context.Context, _ time.Duration) (tickResult, error) {
	wait, err := e.session.TickFor(ctx)
	return tickResult{progressed: true, wait: wait}, err
}

func (e lgtEngine) flushes() uint64 { return e.session.Flushes() }

// sendKey shares the WIPI translation: a Clet compares against the same key
// values a KTF game does. It delivers no repeat, because this platform has no
// event kind for one; see lgtKeyEvent.
func (e lgtEngine) sendKey(s *Session, ctx context.Context, action string, code int32) error {
	pressed, deliver, ok := lgtKeyEvent(action)
	if !ok {
		return fmt.Errorf("session: unknown key action %q", action)
	}
	if !deliver {
		return nil
	}
	if err := s.checkKeyHold(action, code); err != nil {
		return err
	}
	e.session.SendKey(pressed, uint32(ktfKeyCode(code)))
	s.noteKeyHold(action, code)
	return nil
}

func (e lgtEngine) sendPointer(context.Context, int32, int32, int32) error { return ErrNoPointer }
func (e lgtEngine) hasPointer() bool                                       { return false }

// heldKey is false and repeat delivers nothing: a Clet's events here are a
// press and a release.
func (e lgtEngine) heldKey(*Session) (int32, bool)      { return 0, false }
func (e lgtEngine) repeat(context.Context, int32) error { return nil }

func (e lgtEngine) pause(ctx context.Context) error  { return e.session.Pause(ctx) }
func (e lgtEngine) resume(ctx context.Context) error { return e.session.Resume(ctx) }

// close gives an LGT title's destroyClet closeWait: a close ends in bounded
// time on every platform.
func (e lgtEngine) close() {
	ctx, cancel := context.WithTimeout(context.Background(), closeWait)
	defer cancel()
	_ = e.session.Close(ctx)
}

func (e lgtEngine) frame() ([]byte, int, int, bool) {
	rgba, width, height, _ := e.session.Frame()
	return rgba, width, height, false
}

func (e lgtEngine) screen() (int, int, bool) {
	_, width, height, _ := e.session.Frame()
	return width, height, width > 0 && height > 0
}

func (e lgtEngine) vibration() (backend.Vibration, bool) { return e.session.Vibration(), true }
func (e lgtEngine) guestElapsed() (time.Duration, bool)  { return e.session.GuestElapsed(), true }

func (e lgtEngine) setSpeed(multiplier float64) float64 {
	e.session.SetSpeed(multiplier)
	return multiplier
}

func (e lgtEngine) speed() float64                 { return e.session.Speed() }
func (e lgtEngine) setFrameSink(backend.FrameSink) {}
func (e lgtEngine) skipToNextDeadline() bool       { return false }

// virtualClock is true: a tick stands for a span of guest time whether or not
// the Host waits that long before the next one. See Session.VirtualClock.
func (e lgtEngine) virtualClock() bool           { return true }
func (e lgtEngine) cheat() *cheat.Session        { return e.session.Cheat() }
func (e lgtEngine) cheatConsole() *cheat.Console { return e.session.CheatConsole() }

func (e lgtEngine) authentication() backend.AuthenticationStatus {
	return e.session.Authentication()
}

func (e lgtEngine) uncaughtCallbacks() (uint64, string) { return e.session.UncaughtCallbacks() }

func (e lgtEngine) textInput(ctx context.Context) (*backend.TextInput, error) {
	return e.session.TextInput(ctx)
}

func (e lgtEngine) canCheckpoint() bool { return e.session.CanCheckpoint() }

func (e lgtEngine) captureCheckpoint(ctx context.Context, captureShared func() ([]byte, error)) (backend.Checkpoint, error) {
	return e.session.CaptureCheckpointWithSession(ctx, captureShared)
}

func (e lgtEngine) resumeCheckpointOutput() { e.session.ResumeCheckpointOutput() }

// sktJavaEngine is an SKT MIDlet on the MIDP runtime. It draws into a surface
// this package keeps, which owns the picture's size.
type sktJavaEngine struct {
	runtime *skt.Runtime
	surface *captureFramebuffer
}

// tick has no tick of the MIDlet's own to call: it runs on the callbacks the
// Host makes and whatever those deferred. Its audio timeline is advanced by
// the Host for the same reason, and on the guest's clock rather than the wall
// so that a game running fast hears its music at the pace it is playing at.
// The pace is what the Host comes back at, so it carries the speed the same
// way every other platform's wait does.
func (e sktJavaEngine) tick(context.Context, time.Duration) (tickResult, error) {
	e.runtime.AdvanceAudio()
	err := e.runtime.RunPending()
	result := tickResult{progressed: true, wait: guestPace(FramePace, e.runtime.Speed())}
	if state := e.runtime.State(); state == skt.StateDestroyed || state == skt.StateError {
		// A MIDlet has no exit call to name: it is in a terminal state and
		// which one is the whole of what this platform knows.
		result.exited, result.reason = true, fmt.Sprintf("the MIDlet reached state %v", state)
	}
	return result, err
}

func (e sktJavaEngine) flushes() uint64 { return e.surface.Flushes() }

// sendKey takes the page's codes unchanged: they are the MIDP values, and
// translating them is the WIPI path's business.
func (e sktJavaEngine) sendKey(s *Session, _ context.Context, action string, code int32) error {
	if err := s.checkKeyHold(action, code); err != nil {
		return err
	}
	if err := e.runtime.SendKey(skt.KeyEventType(action), code); err != nil {
		return err
	}
	s.noteKeyHold(action, code)
	return nil
}

// sendPointer has nothing to deliver to: a MIDlet's pointerPressed is a
// surface this runtime does not offer.
func (e sktJavaEngine) sendPointer(context.Context, int32, int32, int32) error { return ErrNoPointer }
func (e sktJavaEngine) hasPointer() bool                                       { return false }

// heldKey is the runtime's own: the MIDP runtime has a pad of its own and
// answers for itself.
func (e sktJavaEngine) heldKey(*Session) (int32, bool) { return e.runtime.HeldKey() }

func (e sktJavaEngine) repeat(_ context.Context, code int32) error {
	return e.runtime.SendKey(skt.KeyRepeated, code)
}

func (e sktJavaEngine) pause(context.Context) error  { return e.runtime.Pause() }
func (e sktJavaEngine) resume(context.Context) error { return e.runtime.Resume() }
func (e sktJavaEngine) close()                       { _ = e.runtime.Destroy(true) }

func (e sktJavaEngine) frame() ([]byte, int, int, bool) {
	rgba, width, height := e.surface.Frame()
	return rgba, width, height, true
}

func (e sktJavaEngine) screen() (int, int, bool) {
	width, height := e.surface.Dimensions()
	return width, height, true
}

func (e sktJavaEngine) vibration() (backend.Vibration, bool) { return e.runtime.Vibration(), true }

// guestElapsed is not answered: the MIDP runtime reads the wall clock directly
// rather than being advanced by a Host, so there is no second clock to compare.
func (e sktJavaEngine) guestElapsed() (time.Duration, bool) { return 0, false }

func (e sktJavaEngine) setSpeed(multiplier float64) float64 {
	e.runtime.SetSpeed(multiplier)
	return multiplier
}

func (e sktJavaEngine) speed() float64                 { return e.runtime.Speed() }
func (e sktJavaEngine) setFrameSink(backend.FrameSink) {}
func (e sktJavaEngine) skipToNextDeadline() bool       { return false }
func (e sktJavaEngine) virtualClock() bool             { return false }
func (e sktJavaEngine) cheat() *cheat.Session          { return e.runtime.Cheat() }
func (e sktJavaEngine) cheatConsole() *cheat.Console   { return e.runtime.CheatConsole() }

func (e sktJavaEngine) authentication() backend.AuthenticationStatus {
	return e.runtime.Authentication()
}

func (e sktJavaEngine) uncaughtCallbacks() (uint64, string) { return e.runtime.UncaughtCallbacks() }

func (e sktJavaEngine) textInput(ctx context.Context) (*backend.TextInput, error) {
	return e.runtime.TextInput(ctx)
}

func (e sktJavaEngine) canCheckpoint() bool { return e.runtime.CanCheckpoint() }

func (e sktJavaEngine) captureCheckpoint(ctx context.Context, captureShared func() ([]byte, error)) (backend.Checkpoint, error) {
	return e.runtime.CaptureCheckpointWithSession(ctx, captureShared)
}

func (e sktJavaEngine) resumeCheckpointOutput() { e.runtime.ResumeCheckpointOutput() }

// sktScriptEngine is an SKT script title. It draws into a surface this package
// keeps, which owns the picture's size.
type sktScriptEngine struct {
	script  *skt.ScriptSession
	surface *captureFramebuffer
}

func (e sktScriptEngine) tick(ctx context.Context, _ time.Duration) (tickResult, error) {
	wait, err := e.script.Tick(ctx)
	result := tickResult{progressed: true, wait: wait, exited: e.script.Exited()}
	if result.exited {
		result.reason = "the script requested exit"
	}
	return result, err
}

func (e sktScriptEngine) flushes() uint64 { return e.surface.Flushes() }

func (e sktScriptEngine) sendKey(s *Session, ctx context.Context, action string, code int32) error {
	if action != KeyPress && action != KeyRelease {
		return fmt.Errorf("session: unknown key action %q", action)
	}
	if err := s.checkKeyHold(action, code); err != nil {
		return err
	}
	if err := e.script.SendKey(ctx, action, code); err != nil {
		return err
	}
	s.noteKeyHold(action, code)
	return nil
}

func (e sktScriptEngine) sendPointer(context.Context, int32, int32, int32) error { return ErrNoPointer }
func (e sktScriptEngine) hasPointer() bool                                       { return false }
func (e sktScriptEngine) heldKey(*Session) (int32, bool)                         { return 0, false }
func (e sktScriptEngine) repeat(context.Context, int32) error                    { return nil }

func (e sktScriptEngine) pause(context.Context) error {
	e.script.Pause()
	return nil
}

func (e sktScriptEngine) resume(context.Context) error {
	e.script.Resume()
	return nil
}

func (e sktScriptEngine) close() { _ = e.script.Close() }

func (e sktScriptEngine) frame() ([]byte, int, int, bool) {
	rgba, width, height := e.surface.Frame()
	return rgba, width, height, true
}

func (e sktScriptEngine) screen() (int, int, bool) {
	width, height := e.surface.Dimensions()
	return width, height, true
}

func (e sktScriptEngine) vibration() (backend.Vibration, bool) { return e.script.Vibration(), true }
func (e sktScriptEngine) guestElapsed() (time.Duration, bool)  { return e.script.GuestElapsed(), true }

func (e sktScriptEngine) setSpeed(multiplier float64) float64 {
	e.script.SetSpeed(multiplier)
	return multiplier
}

func (e sktScriptEngine) speed() float64                 { return e.script.Speed() }
func (e sktScriptEngine) setFrameSink(backend.FrameSink) {}
func (e sktScriptEngine) skipToNextDeadline() bool       { return false }
func (e sktScriptEngine) virtualClock() bool             { return false }

// cheat is nil: a script title has neither an address space nor an object
// graph a search could sweep.
func (e sktScriptEngine) cheat() *cheat.Session        { return nil }
func (e sktScriptEngine) cheatConsole() *cheat.Console { return nil }

// authentication is unsupported: a script title carries no check to adapt.
func (e sktScriptEngine) authentication() backend.AuthenticationStatus {
	return backend.AuthenticationUnsupported
}

func (e sktScriptEngine) uncaughtCallbacks() (uint64, string) { return 0, "" }

// textInput is never offered: the script machine has no text field.
func (e sktScriptEngine) textInput(context.Context) (*backend.TextInput, error) {
	return nil, backend.ErrNoTextInput
}

func (e sktScriptEngine) canCheckpoint() bool { return e.script.CanCheckpoint() }

func (e sktScriptEngine) captureCheckpoint(ctx context.Context, captureShared func() ([]byte, error)) (backend.Checkpoint, error) {
	return e.script.CaptureCheckpointWithSession(ctx, captureShared)
}

func (e sktScriptEngine) resumeCheckpointOutput() { e.script.ResumeCheckpointOutput() }

// displaced is the running platform session a load replaces, which has to be
// the kind the checkpoint restores; the zero adapter where nothing is
// displaced, which hands the platform a nil session.
func displaced[E engine](previous *Session) (E, error) {
	var none E
	if previous == nil {
		return none, nil
	}
	old, ok := previous.engine.(E)
	if !ok {
		return none, ErrCheckpointUnsupported
	}
	return old, nil
}
