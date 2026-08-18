package ktf

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"github.com/movingwoo/wfeature/internal/armcore"
	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/cheat"
)

// NativeSession drives one game from the earlier KTF package the way a Host
// wants to hold it: start it, tick it, take the frame, send it keys.
//
// It is a different shape underneath from the descriptor package's Session and
// the same shape on the outside, because what a Host does with a game does not
// depend on which generation of the carrier's download package it came in.
// The differences that remain are two:
//
//   - The title owns its own pace. It hands the platform an interval and a
//     function at the end of its start-up, so a tick here is "run the frame if
//     it is due" rather than "run a service round".
//   - There is no descriptor, so the game's identity comes out of the module
//     information file: its name for a label and its application number for
//     the directory its saves belong in.
type NativeSession struct {
	Archive  *NativeArchive
	Client   *NativeClient
	platform *NativePlatform
	clock    Clock
	started  time.Time
	logger   *slog.Logger

	cheat        *cheat.Session
	cheatConsole *cheat.Console
}

// NativeSessionOptions bound and equip one session.
type NativeSessionOptions struct {
	// MaxSteps caps the ARM instructions one bounded guest run may retire.
	// Zero selects a ceiling the local title's start-up fits inside.
	MaxSteps uint64
	// SaveStore persists what the title writes. Nil keeps writes for the
	// session only, which is what a probe wants and not what a player does.
	SaveStore SaveStore
	// Clock is the time source the title's frame interval is measured
	// against. Nil selects the wall clock, which runs the game at the speed it
	// was written for; a Host running ticks in a batch passes a ManualClock.
	Clock Clock
	// Logger receives session lifecycle events at debug level. Nil is silent.
	Logger *slog.Logger
	// AudioSink is where the title's music goes. Nil is silent, which is what
	// a Host without an audio device wants.
	AudioSink backend.AudioSink
}

// nativeSessionDefaultMaxSteps covers the title's own start-up, which loads
// every file it ships before it draws anything.
const nativeSessionDefaultMaxSteps = 200_000_000

// IsNativeArchive reports whether an archive is the earlier package rather
// than the descriptor one. It answers from entry names, so it costs the
// central directory a Host has already read.
func IsNativeArchive(data []byte) bool {
	files, err := readOuterZIP(data)
	if err != nil {
		return false
	}
	return IsNativePackage(files)
}

// NativeSaveOwner names the directory a title's saves belong in. The earlier
// package carries no PID, so the module information file's application number
// is what identifies the title — it is the same number the module's own class
// factory is asked for, so a package whose number does not match its module
// would not start at all.
func NativeSaveOwner(info NativeInfo) string {
	if info.ApplicationID == 0 {
		return ""
	}
	return strconv.FormatUint(uint64(info.ApplicationID), 10)
}

// StartNativeSession loads a package and runs the title up to the point where
// it is waiting for its first frame.
func StartNativeSession(ctx context.Context, data []byte, options NativeSessionOptions) (*NativeSession, error) {
	archive, err := OpenNative(data)
	if err != nil {
		return nil, err
	}
	steps := options.MaxSteps
	if steps == 0 {
		steps = nativeSessionDefaultMaxSteps
	}
	client, err := LoadNativeClient(archive, armcore.CoreOptions{MaxSteps: steps})
	if err != nil {
		return nil, err
	}
	platform := NewNativePlatform(client, archive, options.Clock)
	platform.AttachSaves(options.SaveStore)
	platform.AttachAudio(options.AudioSink)
	if err := platform.Boot(ctx); err != nil {
		return nil, err
	}
	session := &NativeSession{
		Archive:  archive,
		Client:   client,
		platform: platform,
		clock:    platform.clock,
		started:  platform.clock.Now(),
		logger:   options.Logger,
	}
	if options.Logger != nil {
		options.Logger.Debug("KTF native session started",
			"name", archive.Info.Name, "application", archive.Info.ApplicationID,
			"frame", platform.FrameInterval())
	}
	return session, nil
}

// Platform exposes the served surface, for the probes that drive it directly.
func (session *NativeSession) Platform() *NativePlatform {
	if session == nil {
		return nil
	}
	return session.platform
}

// TickFor runs the title's frame as often as it is due, for at most budget of
// guest time, and reports how long a Host should wait before the next tick.
//
// The title asked to be called back on an interval, so the wait is what is
// left of that interval rather than a number this platform chose. A Host that
// honours it runs the game at the speed it was written for.
func (session *NativeSession) TickFor(ctx context.Context, budget time.Duration) (bool, time.Duration, error) {
	if session == nil || session.platform == nil {
		return false, 0, fmt.Errorf("KTF native session is not started")
	}
	deadline := session.clock.Now().Add(budget)
	progressed := false
	for {
		ran, err := session.platform.Tick(ctx)
		if err != nil {
			return progressed, 0, err
		}
		if !ran {
			break
		}
		progressed = true
		if err := session.serviceCheat(); err != nil {
			return progressed, 0, err
		}
		if budget <= 0 || !session.clock.Now().Before(deadline) {
			break
		}
	}
	return progressed, session.platform.UntilNextFrame(), nil
}

// Tick runs at most one frame.
func (session *NativeSession) Tick(ctx context.Context) (bool, error) {
	progressed, _, err := session.TickFor(ctx, 0)
	return progressed, err
}

// Frame answers the last picture the title drew, with its size and the number
// of frames it has ended. The bytes belong to the caller.
func (session *NativeSession) Frame() ([]byte, int, int, uint32) {
	if session == nil || session.platform == nil {
		return nil, 0, 0, 0
	}
	frame, presents := session.platform.Frame()
	if frame == nil {
		return nil, 0, 0, 0
	}
	bounds := frame.Bounds()
	return append([]byte(nil), frame.Pix...), bounds.Dx(), bounds.Dy(), uint32(presents)
}

// Flushes reports how many frames the title has ended, without copying one.
func (session *NativeSession) Flushes() uint32 {
	if session == nil || session.platform == nil {
		return 0
	}
	_, presents := session.platform.Frame()
	return uint32(presents)
}

// SendKey delivers one key event, in the WIPI codes every Host here already
// speaks. See nativeKeyCode for what they become.
func (session *NativeSession) SendKey(ctx context.Context, eventType, key int32) error {
	if session == nil || session.platform == nil {
		return fmt.Errorf("KTF native session is not started")
	}
	code, ok := nativeKeyCode(key)
	if !ok {
		// A key this handset has no code for is dropped rather than sent as
		// itself: an unmapped value is not a key the title can act on, and
		// sending it would be indistinguishable from a mapping that is wrong.
		return nil
	}
	switch eventType {
	case KeyPressed, KeyRepeated:
		return session.platform.Key(ctx, code, true)
	case KeyReleased:
		return session.platform.Key(ctx, code, false)
	}
	return fmt.Errorf("KTF native key event type %d is not one of pressed, released or repeated", eventType)
}

// GuestElapsed reports how much time the title has seen pass.
func (session *NativeSession) GuestElapsed() time.Duration {
	if session == nil {
		return 0
	}
	return session.clock.Now().Sub(session.started)
}

// NextDeadline reports when the title's frame is next due, which is what a
// Host on a manual clock jumps to.
func (session *NativeSession) NextDeadline() (time.Time, bool) {
	if session == nil || session.platform == nil {
		return time.Time{}, false
	}
	return session.platform.NextFrame()
}

// SkipToNextDeadline moves a manual clock to the next frame. It answers false
// on a wall clock, which cannot be moved, and on a title with no frame.
func (session *NativeSession) SkipToNextDeadline() bool {
	if session == nil {
		return false
	}
	manual, ok := session.clock.(*ManualClock)
	if !ok {
		return false
	}
	deadline, ok := session.NextDeadline()
	if !ok {
		return false
	}
	manual.Set(deadline)
	return true
}

// Name is what the module information file calls the title.
func (session *NativeSession) Name() string {
	if session == nil || session.Archive == nil {
		return ""
	}
	return session.Archive.Info.Name
}

// SaveOwner names the directory this title's saves belong in.
func (session *NativeSession) SaveOwner() string {
	if session == nil || session.Archive == nil {
		return ""
	}
	return NativeSaveOwner(session.Archive.Info)
}

// Close releases the session. There are no guest threads here — the title runs
// only inside the frame it asked for — so this only drops the platform.
func (session *NativeSession) Close() {
	if session == nil {
		return
	}
	session.platform = nil
}
