package clet

import (
	"errors"
	"log/slog"
	"time"

	"github.com/movingwoo/wfeature/internal/armcore"
	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/platform/wipicrt"
	"github.com/movingwoo/wfeature/internal/wipic"
)

// ErrGuestExited reports that guest code called MC_knlExit: the program ended
// on its own rather than failing.
var ErrGuestExited = errors.New("SKT Clet guest requested exit")

const sessionDefaultMaxSteps = 100_000_000

// Session is a running Clet and the Host boundaries attached to it. A Clet has
// no archive and no cheat console wired yet, so it keeps only the client, the
// options and the save identity a diagnostic report names.
type Session struct {
	Client          *Client
	options         SessionOptions
	archiveIdentity [32]byte
}

// serviceCheat is a no-op for a Clet: the cheat console is not wired to a native
// program. It exists so the service round reads the same as a KTF title's.
func (session *Session) serviceCheat() error { return nil }

// SessionOptions configures a Clet session. The fields mirror the KTF session's
// so a Host drives both the same way.
type SessionOptions struct {
	// DisableAuthentication opts out of automatic compatibility for diagnostics.
	DisableAuthentication bool
	// MaxSteps caps the ARM instructions of each bounded guest run.
	MaxSteps uint64
	// TimerLimit caps timer callbacks per tick. Zero selects a default.
	TimerLimit int
	// ThreadSliceSteps caps the ARM steps one guest thread runs per tick.
	ThreadSliceSteps uint64
	// ServiceSteps caps the total ARM steps one Host service call may spend.
	ServiceSteps uint64
	// ServiceWait caps how long one Host service call may wait on the clock.
	ServiceWait time.Duration
	// SaveStore persists guest saves; when nil and SaveRoot is set, a directory
	// store scoped by the program number is attached instead.
	SaveStore wipicrt.SaveStore
	// SaveRoot is the directory a filesystem Host uses for saves.
	SaveRoot string
	// Clock is the time source waits are measured against; nil selects the wall.
	Clock wipicrt.Clock
	// Speed scales the game's pace; zero and 1 both select the original speed.
	Speed float64
	// TraceLimit is how many recent boundary events Diagnostics reports.
	TraceLimit int
	// Logger receives session lifecycle events at debug level.
	Logger *slog.Logger
	// AudioSink is where sound the game plays goes; nil is silent.
	AudioSink backend.AudioSink
	FrameSink backend.FrameSink
	// Width and Height name the handset the game is told it runs on.
	Width, Height int
	// Debug is handed the ARM core before any guest code runs, for probes.
	Debug func(*armcore.Core)
}

// cletQuantum is how many instructions the ARM core retires before it yields
// to the Go scheduler. A Clet is one thread that runs long stretches of guest
// code between platform calls, so a yield every thousand instructions, the
// default, buys it nothing and costs it a scheduler wakeup each time: at this
// figure, which is LGT's Clet quantum for the same reason, both local titles
// drew the same frames for about a sixth less of the process's CPU. No count
// the guest or a service allowance sees depends on it, since both count steps
// rather than quanta. See docs/skt-native.md, "Running".
const cletQuantum = uint32(16384)

func sessionCoreOptions(options SessionOptions) armcore.CoreOptions {
	steps := options.MaxSteps
	if steps == 0 {
		steps = sessionDefaultMaxSteps
	}
	return armcore.CoreOptions{MaxSteps: steps, Quantum: cletQuantum}
}

// StartFailure is what a start reports once guest code has run: the failure and
// the boundary trace of the run that failed.
type StartFailure struct {
	Err         error
	Diagnostics Diagnostics
}

func (failure *StartFailure) Error() string {
	if failure == nil || failure.Err == nil {
		return "SKT Clet session start failed"
	}
	return failure.Err.Error()
}

func (failure *StartFailure) Unwrap() error {
	if failure == nil {
		return nil
	}
	return failure.Err
}

// ClientImage is the code a client runs, named for diagnostics and the cheat
// panel. A Clet's image is its code alone.
type ClientImage struct {
	Name    string
	Data    []byte
	BSSSize uint32
}

// HandsetNumber is the subscriber number the platform reports, from the shared
// WIPI system properties.
func HandsetNumber() string { return wipic.SystemProperties["PHONENUMBER"] }
