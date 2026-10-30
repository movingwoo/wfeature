package clet

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

// Client runs one Clet on an ARM core. It is the WIPI-C half of the KTF client:
// the core, the thread, the host-service budget and the frame plumbing, without
// the Java VM, the worker goroutines or the LCDUI state a Clet has none of. The
// pacing (clock.go, hostcost.go, service_budget.go) is the KTF engine's,
// brought over so a Clet is paced exactly as a KTF WIPI-C title is.
type Client struct {
	subscriberNumber string
	authentication   backend.AuthenticationStatus
	core             *armcore.Core
	thread           *armcore.Thread
	image            ClientImage
	// mapped is what the image occupies in guest memory and entry is the start
	// address callClet runs; a Clet enters through its own six entry points.
	mapped uint64
	entry  uint32
	// clet is the Clet state a LoadCletClient built; see clet.go.
	clet *cletState

	run                   sync.Mutex
	initializationStarted bool
	workersStopped        bool
	runtime               *initializationRuntime
	prepared              *initializationRuntime

	resources   map[string][]byte
	files       map[string][]byte
	saveStore   SaveStore
	programName string

	// Frame plumbing: the converted RGBA frame, its size, and the sink the Host
	// collects it through. See the frame methods below (extracted from KTF).
	frame            []byte
	frameWidth       int
	frameHeight      int
	flushCount       uint32
	framePending     bool
	partialFrame     bool
	frameSink        backend.FrameSink
	frameSampleAt    time.Time
	frameSampleClock func() time.Time
	lastSample       []byte

	// screenWidth and screenHeight are the handset size this program is told it
	// runs on; zero means the platform's own. See SetScreen.
	screenWidth  int
	screenHeight int

	threadSliceSteps uint64

	// costs tallies a tick's time and boundary crossings; timePhases enables the
	// per-phase durations. See hostcost.go.
	costs      HostCosts
	timePhases bool

	// nextRoundPaint is unused by a Clet (it has no Host-driven round paint) but
	// kept so the pacing code reads the same.
	nextRoundPaint time.Time

	// paint cost bookkeeping the host-cost report reads; a Clet drives its own
	// paint entry, so these only ever stay at zero.
	paintsDrop uint64
	paintLoad  float64
	paintCost  time.Duration
	entryCost  time.Duration

	// workBaseline and servingDue anchor the frame-period charge; activeTimer is
	// the timer whose callback is running. See clock.go and serviceTimersLocked.
	workBaseline uint64
	servingDue   time.Time
	activeTimer  *wipicTimer

	// The host-service budget: how much guest execution a service call is given
	// and how it is charged. See service_budget.go.
	serviceSteps       uint64
	serviceWait        time.Duration
	serviceRemaining   uint64
	serviceDepth       int
	serviceStartedAt   time.Time
	serviceWindowAt    time.Time
	serviceWindowReads uint64
	serviceStepMark    uint64
	guestClockReads    uint64

	// clock is the time source guest waits are measured against and speed scales
	// them; clientWakeAt carries a wait declared on the client thread. See
	// clock.go.
	clock        Clock
	speed        float64
	clientWakeAt time.Time

	traceLimit int
	logger     *slog.Logger

	// audio holds the sounds the game loaded, advanced on the guest's clock.
	audio *backend.Audio
	// vibrator holds what the guest asked the handset's motor to do.
	vibrator backend.Vibrator
}

// ServiceTimers runs the timers whose callbacks are due, up to limit.
func (client *Client) ServiceTimers(ctx context.Context, limit int) (int, error) {
	if client == nil || client.core == nil || client.thread == nil {
		return 0, fmt.Errorf("SKT Clet client is not initialized")
	}
	client.run.Lock()
	defer client.run.Unlock()
	return client.serviceTimersLocked(ctx, limit)
}

// serviceTimersLocked fires the due WIPI-C timer callbacks. A Clet has no
// java/util/Timer tasks and no cards, so only the callback timers a title arms
// with MC_knlSetTimer run here. The caller holds the run lock.
func (client *Client) serviceTimersLocked(ctx context.Context, limit int) (int, error) {
	if client.runtime == nil {
		return 0, fmt.Errorf("SKT Clet initialization has not completed")
	}
	// Timers run on the client thread, so a wait declared there holds them too;
	// a timer due meanwhile stays pending rather than being dropped.
	if !client.clientThreadDue() {
		return 0, nil
	}
	defer client.beginHostService(ctx)()
	// A network failure owed to a caller of MC_netConnect is delivered here for
	// the same reason a timer callback is: the guest is between its own calls.
	// It goes first, because a title waiting on one is waiting to start doing
	// anything at all.
	netServiced, err := client.serviceNetCallbacks(ctx)
	if err != nil {
		return 0, err
	}
	pending := client.runtime.pendingTimers
	client.runtime.pendingTimers = nil
	now := client.now()
	serviced := netServiced
	for index, timer := range pending {
		if serviced >= limit {
			client.runtime.requeueTimers(pending[index:])
			break
		}
		if timer.callback == 0 {
			continue
		}
		if now.Before(timer.due) {
			client.runtime.pendingTimers = append(client.runtime.pendingTimers, timer)
			continue
		}
		// The frame period this callback arms for its next frame runs from the
		// deadline this one was due at, wherever in the callback it is armed.
		client.servingDue = timer.due
		client.activeTimer = &timer
		// The entries after this one are what a cancel or a re-arm from the
		// callback has to find; see roundTimers.
		previousRound := client.runtime.roundTimers
		client.runtime.roundTimers = pending[index+1:]
		run, err := client.core.Call(
			ctx,
			client.thread,
			timer.callback,
			ReturnAddress,
			[]uint32{timer.pointer, timer.param},
			client.runtime.handleSupervisorCall,
		)
		client.runtime.roundTimers = previousRound
		client.servingDue = time.Time{}
		client.activeTimer = nil
		if input := &client.runtime.cInput; input.timerPointer == timer.pointer && input.timerCallback == timer.callback {
			input.clearPending = false
		}
		if err != nil {
			// The callback that failed did not happen; the timers behind it
			// still do, so a Host that absorbs an uncaught guest exception
			// carries on with the rest of the batch.
			client.runtime.requeueTimers(pending[index+1:])
			if fault, isFault := guestFault(err); isFault {
				if report := client.runtime.describeGuestFault(run.Context, fault); report != "" {
					client.runtime.countDiagnostic("guest fault " + report)
					return serviced, fmt.Errorf("service SKT Clet timer at %#x: %w: %s", timer.callback, err, report)
				}
			}
			return serviced, fmt.Errorf("service SKT Clet timer at %#x: %w", timer.callback, err)
		}
		serviced++
	}
	return serviced, nil
}

// EXTRACTED-CLIENT-METHODS-BELOW
// serviceAudio moves audio playback to where the guest's clock now is. It runs
// at the end of a tick, after the guest has had its slice, so a sound started
// this tick is not also advanced by it.
func (client *Client) serviceAudio() error {
	if client == nil {
		return nil
	}
	client.run.Lock()
	defer client.run.Unlock()
	if client.audio == nil || client.runtime == nil {
		return nil
	}
	now := client.runtime.guestElapsed()
	if err := client.runtime.syncClipCompletions(now); err != nil {
		return err
	}
	client.runtime.collectHostResources()
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
	if traceLimit > traceLimitCeiling {
		traceLimit = traceLimitCeiling
	}
	client.traceLimit = traceLimit
	client.logger = logger
}

// log reports a lifecycle event when the Host attached a logger. Debug builds
// keep these; release loggers drop them by level.
func (client *Client) log(message string, arguments ...any) {
	if client == nil || client.logger == nil {
		return
	}
	client.logger.Debug(message, arguments...)
}

// guestPrint reports one line the guest wrote through MC_knlPrintk. This is
// the game's own commentary on what it is doing, so it is logged at info
// level: it survives the release logger, unlike the Host's own debug tracing.
func (client *Client) guestPrint(line string) {
	if client == nil || client.logger == nil {
		return
	}
	client.logger.Info("KTF guest printk", "text", strings.TrimRight(line, "\r\n"))
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
	client.screenWidth = width
	client.screenHeight = height
}

// screenSize is the handset to answer with, with the platform's own standing
// in for whatever was not chosen.
func (client *Client) screenSize() (int, int) {
	width, height := runtimeDisplayPixelWidth, runtimeDisplayPixelHeight
	if client != nil && client.screenWidth > 0 && client.screenHeight > 0 {
		width, height = client.screenWidth, client.screenHeight
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
		return runtimeDisplayPixelWidth, runtimeDisplayPixelHeight
	}
	client.run.Lock()
	defer client.run.Unlock()
	if client.frameWidth > 0 && client.frameHeight > 0 {
		return client.frameWidth, client.frameHeight
	}
	return client.screenSize()
}

// Frame returns a copy of the last flushed RGBA screen frame with its size
// and the number of flushes performed so far. The conversion from the guest's
// RGB565 screen happens here rather than at each flush, so a Host that asks
// once pays for one conversion however many times the guest flushed.
func (client *Client) Frame() ([]byte, int, int, uint32) {
	if client == nil {
		return nil, 0, 0, 0
	}
	client.run.Lock()
	defer client.run.Unlock()
	if client.framePending && client.runtime != nil {
		if err := client.runtime.convertScreen(); err != nil {
			client.log("KTF screen conversion failed", "error", err)
		}
	}
	return append([]byte(nil), client.frame...), client.frameWidth, client.frameHeight, client.flushCount
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
	client.run.Lock()
	defer client.run.Unlock()
	if client.framePending && client.runtime != nil {
		if err := client.runtime.convertScreen(); err != nil {
			client.log("KTF screen conversion failed", "error", err)
		}
	}
	// FNV-1a over the frame bytes.
	const offset64, prime64 = uint64(14695981039346656037), uint64(1099511628211)
	digest := offset64
	for _, value := range client.frame {
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
	client.run.Lock()
	defer client.run.Unlock()
	return client.flushCount
}

// AttachSaveStore supplies the persistence boundary DataBase, the WIPI C
// database, and File writes use. Without one, saves stay in memory.
func (client *Client) AttachSaveStore(store SaveStore) {
	if client == nil {
		return
	}
	client.saveStore = store
}

// AttachFilesystem supplies the guest filesystem entries File and FileSystem
// read. The original runtime mounts every outer-archive file by bare name
// with its private "P/" or "p/" prefix removed.
func (client *Client) AttachFilesystem(entries map[string][]byte) {
	if client == nil || len(entries) == 0 {
		return
	}
	if client.files == nil {
		client.files = make(map[string][]byte, len(entries))
	}
	for name, data := range entries {
		client.files[name] = data
	}
}

// SetProgramName supplies the identifier MC_knlGetProgramName reports, which
// KTF clients use to compose database and resource names.
func (client *Client) SetProgramName(name string) {
	if client == nil {
		return
	}
	client.programName = name
}

// AttachResources supplies the archive entries guest code can read through
// Class.getResourceAsStream. Inner JAR entries should be layered over outer
// archive files by the caller; later attachments override earlier names.
func (client *Client) AttachResources(entries map[string][]byte) {
	if client == nil || len(entries) == 0 {
		return
	}
	if client.resources == nil {
		client.resources = make(map[string][]byte, len(entries))
	}
	for name, data := range entries {
		client.resources[name] = data
	}
}

func (client *Client) Core() *armcore.Core {
	return client.core
}

func (client *Client) Image() ClientImage {
	return client.image
}
