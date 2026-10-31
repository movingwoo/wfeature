package wipicrt

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/movingwoo/wfeature/internal/armcore"
	"github.com/movingwoo/wfeature/internal/backend"
)

// Client is the Host side of the WIPI-C engine: the core and the thread guest
// code runs on, the host-service budget and its pacing, the screen and the
// frame plumbing, the storage the guest's files and databases live in, and the
// audio. A platform's own client embeds it and adds what only it has — on KTF
// the Java VM, the worker threads and the card state.
type Client struct {
	// Platform names the platform and fixes its identifier layout, and Hooks
	// are its parts of a service round; see platform.go.
	Platform Platform
	Hooks    ClientHooks

	SubscriberNumber string
	core             *armcore.Core
	Thread           *armcore.Thread
	// imageSize is how much guest memory the program's image occupies from
	// ImageBase, which bounds where a pixel operation may point.
	imageSize uint64

	Run      sync.Mutex
	Runtime  *Runtime
	Prepared *Runtime

	Resources   map[string][]byte
	Files       map[string][]byte
	SaveStore   SaveStore
	ProgramName string

	// Frame plumbing: the converted RGBA frame, its size, and the sink the Host
	// collects it through. See the frame methods below.
	FrameRGBA        []byte
	FrameWidth       int
	FrameHeight      int
	FlushCount       uint32
	FramePending     bool
	PartialFrame     bool
	FrameSink        backend.FrameSink
	FrameSampleAt    time.Time
	FrameSampleClock func() time.Time
	LastSample       []byte

	// ScreenWidth and screenHeight are the handset size this program is told it
	// runs on; zero means the platform's own. See SetScreen.
	ScreenWidth  int
	ScreenHeight int

	ThreadSliceSteps uint64

	// Costs tallies a tick's time and boundary crossings; timePhases enables the
	// per-phase durations. See hostcost.go.
	Costs      HostCosts
	timePhases bool

	// NextRoundPaint is when a platform's Host-driven round paint is next due,
	// such as KTF's card paint; a program that drives its own paint entry
	// leaves it zero.
	NextRoundPaint time.Time

	// The paint cost bookkeeping the host-cost report reads, for a platform
	// with a round paint; they stay at zero for one without.
	PaintsDrop uint64
	PaintLoad  float64
	PaintCost  time.Duration
	EntryCost  time.Duration

	// WorkBaseline and servingDue anchor the frame-period charge; activeTimer is
	// the timer whose callback is running. See clock.go and serviceTimersLocked.
	WorkBaseline uint64
	ServingDue   time.Time
	ActiveTimer  *WIPICTimer

	// The host-service budget: how much guest execution a service call is given
	// and how it is charged. See service_budget.go.
	ServiceSteps       uint64
	ServiceWait        time.Duration
	ServiceRemaining   uint64
	ServiceDepth       int
	serviceStartedAt   time.Time
	serviceWindowAt    time.Time
	serviceWindowReads uint64
	serviceStepMark    uint64
	GuestClockReads    uint64

	// Clock is the time source guest waits are measured against and speed scales
	// them; clientWakeAt carries a wait declared on the client thread. See
	// clock.go.
	Clock           Clock
	SpeedMultiplier float64
	ClientWakeAt    time.Time

	TraceLimit int
	Logger     *slog.Logger

	// Audio holds the sounds the game loaded, advanced on the guest's clock.
	Audio *backend.Audio
	// Vibrator holds what the guest asked the handset's motor to do.
	Vibrator backend.Vibrator
}

// ServiceTimers runs the timers whose callbacks are due, up to limit.
func (client *Client) ServiceTimers(ctx context.Context, limit int) (int, error) {
	if client == nil || client.core == nil || client.Thread == nil {
		return 0, fmt.Errorf("%s client is not initialized", client.platformName())
	}
	client.Run.Lock()
	defer client.Run.Unlock()
	return client.ServiceTimersLocked(ctx, limit)
}

// ServiceTimersLocked fires the due timers: the WIPI-C callbacks a title arms
// with MC_knlSetTimer, and any task the platform queued beside them. The
// caller holds the run lock.
func (client *Client) ServiceTimersLocked(ctx context.Context, limit int) (int, error) {
	if client.Runtime == nil {
		return 0, fmt.Errorf("%s client initialization has not completed", client.platformName())
	}
	// Timers run on the client thread too, so a wait declared there holds them
	// as well; a timer that is due meanwhile stays pending rather than being
	// dropped.
	if !client.ClientThreadDue() {
		return 0, nil
	}
	defer client.BeginHostService(ctx)()
	// A network failure owed to a caller of MC_netConnect is delivered here for
	// the same reason a timer callback is: the guest is between its own calls.
	// It goes first, because a title waiting on one is waiting to start doing
	// anything at all.
	netServiced, err := client.ServiceNetCallbacks(ctx)
	if err != nil {
		return 0, err
	}
	pending := client.Runtime.PendingTimers
	client.Runtime.PendingTimers = nil
	now := client.Now()
	serviced := netServiced
	for index, timer := range pending {
		if serviced >= limit {
			// Preserve the rest for the next service round.
			client.Runtime.requeueTimers(pending[index:])
			break
		}
		if timer.Callback == 0 && timer.Task == nil {
			continue
		}
		if now.Before(timer.Due) {
			client.Runtime.PendingTimers = append(client.Runtime.PendingTimers, timer)
			continue
		}
		if timer.Task != nil {
			// A platform's task runs on a thread of its own, which is what a
			// task that never returns requires: running it here would block
			// the Host inside a single call. The platform parks it on its own
			// budget instead, and one task per owner runs at a time.
			if client.Hooks.TimerTaskRunning != nil && client.Hooks.TimerTaskRunning(timer) {
				client.Runtime.PendingTimers = append(client.Runtime.PendingTimers, timer)
				continue
			}
			if client.Hooks.StartTimerTask == nil {
				return serviced, fmt.Errorf("%s has no runner for a timer task", client.platformName())
			}
			if err := client.Hooks.StartTimerTask(timer); err != nil {
				return serviced, err
			}
			if timer.Period > 0 {
				// From the deadline this run was due at, so a repeating task
				// keeps its own rate rather than drifting by what each run
				// cost. See framePeriodDeadline.
				client.ServingDue = timer.Due
				timer.Due = client.FramePeriodDeadline(timer.Period)
				client.ServingDue = time.Time{}
				client.Runtime.PendingTimers = append(client.Runtime.PendingTimers, timer)
			}
			serviced++
			continue
		}
		// The frame period this callback arms for its next frame runs from the
		// deadline this one was due at, wherever in the callback it is armed.
		client.ServingDue = timer.Due
		client.ActiveTimer = &timer
		// The entries after this one are what a cancel or a re-arm from the
		// callback has to find; see roundTimers.
		previousRound := client.Runtime.RoundTimers
		client.Runtime.RoundTimers = pending[index+1:]
		run, err := client.core.Call(
			ctx,
			client.Thread,
			timer.Callback,
			ReturnAddress,
			[]uint32{timer.Pointer, timer.Param},
			client.Runtime.supervisor,
		)
		client.Runtime.RoundTimers = previousRound
		client.ServingDue = time.Time{}
		client.ActiveTimer = nil
		if input := &client.Runtime.CInput; input.TimerPointer == timer.Pointer && input.TimerCallback == timer.Callback {
			input.ClearPending = false
		}
		// Only the same rearmed callback inherits what it painted.
		for i := range client.Runtime.PendingTimers {
			next := &client.Runtime.PendingTimers[i]
			if next.Task == nil && next.Pointer == timer.Pointer && next.Callback == timer.Callback {
				next.PaintedCard = timer.PaintedCard
			}
		}
		if err != nil {
			// The callback that failed did not happen, and the timers behind
			// it still do: a Host absorbs an uncaught guest exception here and
			// carries on, and the rest of the batch has to be there for it.
			client.Runtime.requeueTimers(pending[index+1:])
			// A fault inside a timer callback gets the same evidence a fault
			// inside any other guest call gets. The address a fault names is
			// never the answer on its own, and a title whose whole loop is a
			// timer would otherwise report less about where it died than one
			// that faults in a call.
			if fault, isFault := client.Runtime.GuestFault(err); isFault {
				if report := client.Runtime.DescribeGuestFault(run.Context, fault); report != "" {
					client.Runtime.CountDiagnostic("guest fault " + report)
					return serviced, fmt.Errorf("service %s timer at %#x: %w: %s", client.platformName(), timer.Callback, err, report)
				}
			}
			return serviced, fmt.Errorf("service %s timer at %#x: %w", client.platformName(), timer.Callback, err)
		}
		serviced++
	}
	return serviced, nil
}

// ServiceAudio moves audio playback to where the guest's clock now is. It runs
// at the end of a tick, after the guest has had its slice, so a sound started
// this tick is not also advanced by it.
func (client *Client) ServiceAudio() error {
	if client == nil {
		return nil
	}
	client.Run.Lock()
	defer client.Run.Unlock()
	if client.Audio == nil || client.Runtime == nil {
		return nil
	}
	now := client.Runtime.GuestElapsed()
	if err := client.Runtime.SyncClipCompletions(now); err != nil {
		return err
	}
	client.Runtime.CollectHostResources()
	return nil
}

// SetDiagnostics configures how much boundary history the client retains and
// where it reports lifecycle events. Hosts pass a trace limit only in debug
// builds; a zero limit keeps the counted diagnostics and drops the ordered
// trace. It must be called before initialization builds the runtime.
func (client *Client) SetDiagnostics(traceLimit int, logger *slog.Logger) {
	if client == nil {
		return
	}
	if traceLimit < 0 {
		traceLimit = 0
	}
	if traceLimit > TraceLimitCeiling {
		traceLimit = TraceLimitCeiling
	}
	client.TraceLimit = traceLimit
	client.Logger = logger
}

// Log reports a lifecycle event when the Host attached a logger. Debug builds
// keep these; release loggers drop them by level.
func (client *Client) Log(message string, arguments ...any) {
	if client == nil || client.Logger == nil {
		return
	}
	client.Logger.Debug(message, arguments...)
}

// GuestPrint reports one line the guest wrote through MC_knlPrintk. This is
// the game's own commentary on what it is doing, so it is logged at info
// level: it survives the release logger, unlike the Host's own debug tracing.
func (client *Client) GuestPrint(line string) {
	if client == nil || client.Logger == nil {
		return
	}
	client.Logger.Info(client.platformName()+" guest printk", "text", strings.TrimRight(line, "\r\n"))
}

// SetScreen names the handset the game is told it runs on. Anything but two
// positive sides selects the platform's own 240x320, and is kept as zero for
// both, so a quick save never records half a handset; a session picks the
// pair from its Host or its descriptor before this, see sessionScreen.
//
// **It is an emulation input rather than a view setting.** Nothing here
// scales a picture: how large the frame is drawn belongs to the Host, and
// this changes what the guest is told and therefore which artwork it loads
// and how it lays a screen out. A title that carries no artwork for the size
// it is given draws nothing usable, so this is a choice about the archive
// rather than a preference.
//
// **Both answers have to agree or the picture tears.** A game reads the size
// from MC_grpGetDisplayInfo and the row stride from the framebuffer record,
// and two local titles write into the buffer directly with the stride they
// were told. Answering one and not the other lays every row at the wrong
// offset — see docs/ktf.md. Both read the same fields here for that reason.
//
// It must be set before guest code runs: the screen framebuffer is built once,
// on the game's first request for it, and never resized.
func (client *Client) SetScreen(width, height int) {
	if client == nil {
		return
	}
	if width <= 0 || height <= 0 {
		width, height = 0, 0
	}
	client.ScreenWidth = width
	client.ScreenHeight = height
}

// ScreenSize is the handset to answer with, with the platform's own standing
// in for whatever was not chosen.
func (client *Client) ScreenSize() (int, int) {
	width, height := RuntimeDisplayPixelWidth, RuntimeDisplayPixelHeight
	if client != nil && client.ScreenWidth > 0 && client.ScreenHeight > 0 {
		width, height = client.ScreenWidth, client.ScreenHeight
	}
	return width, height
}

// Screen answers the screen the title is running on, which a Host lays out
// against. Once there is a frame it is the frame's size, because the frame is
// built from the screen the title was actually given; before that it is the
// handset the title is told it runs on, which for a start that chose nothing
// is the descriptor's or the platform's rather than the Host's default.
//
// It answers the size alone, under one lock, so a Host that asks at every
// start and every load pays for neither a conversion nor a copy of the frame.
func (client *Client) Screen() (int, int) {
	if client == nil {
		return RuntimeDisplayPixelWidth, RuntimeDisplayPixelHeight
	}
	client.Run.Lock()
	defer client.Run.Unlock()
	if client.FrameWidth > 0 && client.FrameHeight > 0 {
		return client.FrameWidth, client.FrameHeight
	}
	return client.ScreenSize()
}

// Frame returns a copy of the last flushed RGBA screen frame with its size
// and the number of flushes performed so far. The conversion from the guest's
// RGB565 screen happens here rather than at each flush, so a Host that asks
// once pays for one conversion however many times the guest flushed.
func (client *Client) Frame() ([]byte, int, int, uint32) {
	if client == nil {
		return nil, 0, 0, 0
	}
	client.Run.Lock()
	defer client.Run.Unlock()
	if client.FramePending && client.Runtime != nil {
		if err := client.Runtime.ConvertScreen(); err != nil {
			client.Log(client.platformName()+" screen conversion failed", "error", err)
		}
	}
	return append([]byte(nil), client.FrameRGBA...), client.FrameWidth, client.FrameHeight, client.FlushCount
}

// FrameDigest fingerprints the current screen. A route waits on what the
// screen is doing — steady, or changed — and asking that question every tick
// through Frame would copy the whole frame out each time only to throw it
// away, so the hash is taken over the frame in place.
//
// It is a fingerprint and not an identity: two different screens can collide.
// For deciding whether a game is still animating that is harmless, and it
// keeps the check to one pass over the pixels.
func (client *Client) FrameDigest() uint64 {
	if client == nil {
		return 0
	}
	client.Run.Lock()
	defer client.Run.Unlock()
	if client.FramePending && client.Runtime != nil {
		if err := client.Runtime.ConvertScreen(); err != nil {
			client.Log(client.platformName()+" screen conversion failed", "error", err)
		}
	}
	// FNV-1a over the frame bytes.
	const offset64, prime64 = uint64(14695981039346656037), uint64(1099511628211)
	digest := offset64
	for _, value := range client.FrameRGBA {
		digest = (digest ^ uint64(value)) * prime64
	}
	return digest
}

// Flushes reports how many frames have been flushed. A Host polls this every
// tick to decide whether a new frame is worth presenting, so it deliberately
// does not copy the frame the way Frame does.
func (client *Client) Flushes() uint32 {
	if client == nil {
		return 0
	}
	client.Run.Lock()
	defer client.Run.Unlock()
	return client.FlushCount
}

// AttachSaveStore supplies the persistence boundary DataBase, the WIPI C
// database, and File writes use. Without one, saves stay in memory.
func (client *Client) AttachSaveStore(store SaveStore) {
	if client == nil {
		return
	}
	client.SaveStore = store
}

// AttachFilesystem supplies the guest filesystem entries File and FileSystem
// read. The original runtime mounts every outer-archive file by bare name
// with its private "P/" or "p/" prefix removed.
func (client *Client) AttachFilesystem(entries map[string][]byte) {
	if client == nil || len(entries) == 0 {
		return
	}
	if client.Files == nil {
		client.Files = make(map[string][]byte, len(entries))
	}
	for name, data := range entries {
		client.Files[name] = data
	}
}

// SetProgramName supplies the identifier MC_knlGetProgramName reports, which
// KTF clients use to compose database and resource names.
func (client *Client) SetProgramName(name string) {
	if client == nil {
		return
	}
	client.ProgramName = name
}

// AttachResources supplies the archive entries guest code can read through
// Class.getResourceAsStream. Inner JAR entries should be layered over outer
// archive files by the caller; later attachments override earlier names.
func (client *Client) AttachResources(entries map[string][]byte) {
	if client == nil || len(entries) == 0 {
		return
	}
	if client.Resources == nil {
		client.Resources = make(map[string][]byte, len(entries))
	}
	for name, data := range entries {
		client.Resources[name] = data
	}
}

func (client *Client) Core() *armcore.Core {
	return client.core
}

// NewClient is a client for a program already mapped into core, which runs on
// thread and whose image occupies imageSize bytes from ImageBase. The platform
// attaches its clock, audio, storage and screen to it before building the
// runtime.
func NewClient(platform Platform, core *armcore.Core, thread *armcore.Thread, imageSize uint64) *Client {
	return &Client{Platform: platform, core: core, Thread: thread, imageSize: imageSize}
}

// AttachCore puts the client on a core and thread restored from a checkpoint,
// in place of the ones it was built with.
func (client *Client) AttachCore(core *armcore.Core, thread *armcore.Thread) {
	client.core, client.Thread = core, thread
}
