package ktf

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"golang.org/x/text/encoding/korean"

	"github.com/movingwoo/wfeature/internal/armcore"
	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/jvm"
	"github.com/movingwoo/wfeature/internal/textinput"
)

const (
	ImageBase       uint32 = 0x00100000
	EntryAddress           = ImageBase + 1
	ReturnAddress   uint32 = 0x7f000000
	ThreadStackBase uint32 = 0x20000000
	ThreadStackSize uint64 = 1 << 20
)

// svcStubSize is how many bytes one supervisor-call stub occupies.
const svcStubSize = 16

// svcStub builds the Thumb stub a guest reaches the platform through. It
// stashes the identifier in r12, crosses the supervisor-call boundary and
// returns through lr, so the guest calls it like any other function and a
// handler that answers with nothing still returns to the caller.
//
// Three places build one and they had drifted into three copies of the same
// twelve bytes: the platform's callback tables, the hooks written over a
// recognized C routine in the guest image, and the trap tables the native
// package's loader plants. The identifier is what tells them apart, so the
// bytes are shared and only the category and the identifier differ.
func svcStub(category, id uint32) []byte {
	return []byte{
		0x10, 0xb4, // push {r4}
		0x02, 0x4c, // ldr r4, [pc, #8]
		0xa4, 0x46, // mov r12, r4
		0x10, 0xbc, // pop {r4}
		byte(category), 0xdf, // svc #category
		0x70, 0x47, // bx lr
		byte(id), byte(id >> 8), byte(id >> 16), byte(id >> 24),
	}
}

type Client struct {
	core   *armcore.Core
	thread *armcore.Thread
	image  ClientImage
	// mapped is what the image occupies in guest memory, entry is the address
	// ExecuteEntry calls, and argument is the single word it is called with.
	// The current generation of client images enters at its first byte with
	// the BSS size; the older relocatable modules name their own entry and
	// take the platform's callback table, which ExecuteModuleEntry fills in.
	// See client_relocatable.go.
	mapped   uint64
	entry    uint32
	argument uint32
	// module is set for the older relocatable images. Their entry is handed
	// the platform's own callback table rather than a size, so the runtime
	// that owns that table has to exist before the entry runs. See
	// client_relocatable.go and ExecuteModuleEntry. moduleSegment is where
	// the relocated segment begins, which is what its own header's offsets
	// are relative to.
	module                bool
	moduleSegment         uint32
	vm                    *jvm.VM
	run                   sync.Mutex
	initializationStarted bool
	runtime               *initializationRuntime
	// prepared is the runtime once its arena, stubs and interface tables
	// exist, which is before it becomes the client's live runtime. The two
	// differ only for the older modules, whose entry needs the callback table
	// prepare builds. initParameters are the five words the client's own
	// initialization function takes.
	prepared       *initializationRuntime
	initParameters []uint32
	executable     Executable
	resources      map[string][]byte
	files          map[string][]byte
	saveStore      SaveStore

	// The lwc text component that currently has the keypad, and the editor
	// driving it. See runtime_lwc_input.go.
	textMu        sync.Mutex
	focusedText   *jvm.Object
	textEditor    *textinput.State
	appProperties map[string]string
	programName   string
	frame         []byte
	frameWidth    int
	frameHeight   int
	flushCount    uint32
	// screenWidth and screenHeight are the handset this game is told it runs
	// on. Zero means the platform's own, which is what all but a title
	// packaged for a smaller phone wants; see SetScreen.
	screenWidth  int
	screenHeight int
	// framePending records that the guest flushed since the last conversion,
	// so Frame knows it has work to do. See presentScreen.
	framePending     bool
	workers          []*guestWorker
	activeWorker     *guestWorker
	workerStackCount int
	freeWorkerStacks []uint32
	// runningTimerTasks maps a java/util/Timer to the worker running one of
	// its tasks. A Timer has one background thread and runs its tasks on it
	// one after another, so a second task — or the next run of a repeating
	// one — waits for the first to return instead of starting a thread of its
	// own. Without that a title that schedules a task per frame, whose run()
	// is its whole scene loop, reaches the worker limit in a second.
	runningTimerTasks map[*jvm.Object]*guestWorker
	workersStopped    bool
	threadSliceSteps  uint64
	// unwindBound overrides how long StopThreads waits for one aborted worker
	// to return; zero means stopUnwindBound. Only a test that stops a worker
	// which cannot answer sets it, so the bound's own path is exercised
	// without a test that takes seconds.
	unwindBound time.Duration
	// costs is where a tick's time and boundary crossings are tallied, and
	// timePhases whether the per-phase durations are collected at all. See
	// hostcost.go.
	costs      HostCosts
	timePhases bool
	// skipPaint drops this round's card paint, and lastPaint is when one last
	// happened. A host that has fallen behind spends its rounds on the guest's
	// logic instead of its screen; see behindOnPaint.
	skipPaint  bool
	lastPaint  time.Time
	paintsDrop uint64
	// uncaughtCallbacks counts the guest exceptions that ended a callback this
	// Host started, and uncaughtFirst keeps what the first one said. Absorbing
	// one keeps the session alive, so without a count a title that fails every
	// paint reads exactly like a title that plays. See uncaught.go.
	uncaughtCallbacks uint64
	uncaughtFirst     string
	// paintLoad is the running ratio of what an entry costs to the wait the
	// guest asks for after it; above one the host is oversubscribed.
	paintLoad float64
	// paintCost is what one card paint runs to, and entryCost what one entry
	// does. Their ratio is the other half of the drop decision: paintLoad says
	// the host is over its schedule, and these say whether the paint is what
	// it is over on. Both are running averages, and paintCost is only moved by
	// a paint that actually ran, so dropping paints cannot walk its own input
	// down and oscillate.
	paintCost        time.Duration
	entryCost        time.Duration
	serviceSteps     uint64
	serviceWait      time.Duration
	serviceRemaining uint64
	serviceDepth     int
	// A service call that is waiting on the clock is charged differently from
	// one that is computing: serviceStartedAt and serviceWindowAt are where
	// the session clock stood when the call and the current step window began,
	// and serviceWindowReads is what guestClockReads held at the same moment.
	// See continueHostService.
	serviceStartedAt   time.Time
	serviceWindowAt    time.Time
	serviceWindowReads uint64
	// guestClockReads counts the times the guest has asked what time it is,
	// through any of the calls that answer from the session clock. It only
	// ever grows; the difference across a step window is what says the guest
	// spent that window waiting.
	guestClockReads uint64
	// clock is the time source guest waits are measured against, and speed
	// scales those waits. clientWakeAt carries a wait the guest declared on
	// the client thread, which has nothing to park. See clock.go.
	clock        Clock
	speed        float64
	clientWakeAt time.Time
	traceLimit   int
	logger       *slog.Logger
	// audio holds the sounds the game loaded and advances them on the same
	// clock the guest runs on, so a Host batching ticks hears the same
	// sequence a real-time Host does, only faster. See serviceAudio.
	audio *backend.Audio
}

// serviceAudio moves audio playback to where the guest's clock now is. It runs
// at the end of a tick, after the guest has had its slice, so a sound started
// this tick is not also advanced by it.
func (client *Client) serviceAudio() {
	if client == nil || client.audio == nil || client.runtime == nil {
		return
	}
	client.audio.Advance(client.runtime.guestElapsed())
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

// SetScreen names the handset the game is told it runs on. Zero for either
// side selects the platform's own 240x320, which is what every local title
// but one packaged for a smaller phone wants.
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

// ServiceThreads grants one step slice to up to limit live guest Java
// threads in queue order and reports how many made progress. Each guest
// thread runs run() on a dedicated worker goroutine with a private ARM stack;
// a worker whose slice ends (step budget or guest sleep) parks with its whole
// call stack intact and rejoins the back of the queue, so an infinite main
// loop keeps progressing across ticks instead of dropping at the step limit.
func (client *Client) ServiceThreads(ctx context.Context, limit int) (int, error) {
	if client == nil || client.core == nil || client.thread == nil {
		return 0, fmt.Errorf("KTF client is not initialized")
	}
	client.run.Lock()
	defer client.run.Unlock()
	if client.runtime == nil {
		return 0, fmt.Errorf("KTF client initialization has not completed")
	}
	if client.workersStopped {
		return 0, errWorkersStopped
	}
	runtime := client.runtime
	// Adopt newly started guest threads as workers in start order.
	for _, javaThread := range runtime.pendingThreads {
		worker, err := client.newGuestWorker(javaThread)
		if err != nil {
			return 0, err
		}
		client.workers = append(client.workers, worker)
	}
	runtime.pendingThreads = nil
	// A serial Runnable is idle-time work: one comes off the queue per pass,
	// and the next pass is a frame away. Taking the whole queue here is what
	// turns a Runnable that re-queues itself into a spin — one title's entire
	// frame loop is a Runnable that re-queues itself, reads the clock, and
	// returns without drawing until its interval is up.
	//
	// A queue that has grown past one entry is not that pattern: the title is
	// queueing faster than a frame drains, and holding a backlog to one a
	// frame only ends at the queue's limit with the run over. A backlog is
	// therefore drained a Runnable per round rather than per frame.
	//
	// The Runnable runs here, on the client thread, rather than on a guest
	// thread of its own. That is what the name says — the original runtime
	// runs these one after another on its event thread — and a thread each was
	// worse than slow: one title queues a Runnable from inside its paint, so
	// they arrived faster than threads could finish and the run ended on the
	// thread-stack limit rather than on anything the title did.
	//
	// **A Runnable waits like everything else on the client thread, and a
	// repaint queued before it goes first.** Both follow from where it runs:
	// the original loop takes repaints and Runnables off one queue in the
	// order they were posted, and a wait declared inside a Runnable is a wait
	// on the thread that would have done the next paint. A title whose frame
	// loop is a Runnable that repaints, re-queues itself and sleeps needs both
	// — without the wait, the Runnable is dispatched every sixteen
	// milliseconds while its own sleep pushes the paint further away each
	// time, and without the ordering the next Runnable takes the round the
	// paint was finally due in. It ran four hundred frames of its loop and
	// drew nothing.
	serialRan := 0
	if len(runtime.pendingSerial) > 0 && client.clientThreadDue() && !runtime.repaintQueued() &&
		(len(runtime.pendingSerial) > 1 || !client.now().Before(runtime.serialDueAt)) {
		runnable := runtime.pendingSerial[0]
		runtime.pendingSerial = runtime.pendingSerial[1:]
		runtime.serialDueAt = client.waitDeadline(serialDispatchInterval)
		if err := client.runSerialRunnable(ctx, runnable); err != nil {
			return 0, err
		}
		serialRan = 1
	}
	round := len(client.workers)
	if limit < round {
		round = limit
	}
	serviced := serialRan
	// `limit` shares out the grants that end with the worker still running:
	// those are the ones competing for the next round too. A worker that
	// returned from run() is leaving, so it does not spend the round's share
	// and the queue behind it is not held back for a round it will not be in.
	// A title that starts a thread per sound effect starts them faster than
	// one a round retires them, and the queue only ever grew — see
	// "A thread that finished is not a slice" in docs/ktf.md.
	//
	// What bounds the drain is steps rather than a count: a round grants at
	// most one slice's worth of execution beyond the grant it was already
	// entitled to, so retiring a hundred threads that each cost a few thousand
	// steps is allowed and one that spends a whole slice before returning ends
	// the round.
	slices := serialRan
	spent := uint64(0)
	allowance := client.threadSliceSteps
	if allowance == 0 {
		allowance = defaultThreadSliceSteps
	}
	previousThread, previousContext := runtime.currentThread, runtime.currentContext
	defer func() {
		runtime.currentThread, runtime.currentContext = previousThread, previousContext
	}()
	// A worker that asked to sleep is not eligible until its deadline passes,
	// which is what paces a game to the speed it was written for. Skipped
	// workers rejoin the queue, and one pass over the queue bounds the search
	// so a round where every worker is sleeping ends instead of spinning.
	now := client.now()
	for attempts := len(client.workers); attempts > 0 && slices < round && spent < allowance; attempts-- {
		worker := client.workers[0]
		client.workers = client.workers[1:]
		if now.Before(worker.wakeAt) {
			client.workers = append(client.workers, worker)
			continue
		}
		before := client.core.Steps()
		runtime.currentThread, runtime.currentContext = worker.armThread, ctx
		client.activeWorker = worker
		worker.grant <- struct{}{}
		event := <-worker.events
		client.activeWorker = nil
		serviced++
		spent += client.core.Steps() - before
		if !event.done {
			slices++
			client.workers = append(client.workers, worker)
			continue
		}
		client.freeWorkerStacks = append(client.freeWorkerStacks, worker.stackBase)
		if client.runningTimerTasks[worker.timerOwner] == worker {
			delete(client.runningTimerTasks, worker.timerOwner)
		}
		if event.err != nil {
			return serviced, fmt.Errorf("run KTF guest thread %s: %w", worker.javaThread.ClassName, event.err)
		}
	}
	return serviced, nil
}

// ServicePaint invokes paint on the top pushed display card with a Graphics
// targeting the screen framebuffer, then presents the frame. It reports
// whether a card painted.
//
// Painting is the frame loop for the games that do their work in paint and
// pace themselves with a sleep there, so it waits out a wait the client thread
// declared instead of repainting as fast as the Host asks.
func (client *Client) ServicePaint(ctx context.Context) (bool, error) {
	if client == nil || client.core == nil || client.thread == nil {
		return false, fmt.Errorf("KTF client is not initialized")
	}
	client.run.Lock()
	defer client.run.Unlock()
	if client.runtime == nil {
		return false, fmt.Errorf("KTF client initialization has not completed")
	}
	if !client.clientThreadDue() {
		return false, nil
	}
	runtime := client.runtime
	defer client.beginHostService(ctx)()
	previousThread, previousContext := runtime.currentThread, runtime.currentContext
	runtime.currentThread, runtime.currentContext = client.thread, ctx
	defer func() {
		runtime.currentThread, runtime.currentContext = previousThread, previousContext
	}()
	return runtime.paintTopCard()
}

// ServiceEvents delivers the events a game posted to its own queue and reports
// how many were delivered.
//
// It is the Host's half of a queue that otherwise has no reader. A game driving
// its own WIPI loop drains the queue itself with getNextEvent, and everything
// the Host originates — a key, a pointer, a repaint — is dispatched to the card
// stack directly rather than queued. What is left is the event a game posts to
// itself from inside its own code with MC_grpPostEvent, which a handset would
// hand back to it on a later turn around the event loop. Without a drain here
// the game waits for a message it sent itself.
//
// The round delivers what was waiting when it started. A handler that posts
// while it runs is queuing for the next round, which is what a queue does and
// what keeps a game that answers every event with another event from holding
// this round for ever.
func (client *Client) ServiceEvents(ctx context.Context) (int, error) {
	if client == nil || client.core == nil || client.thread == nil {
		return 0, fmt.Errorf("KTF client is not initialized")
	}
	client.run.Lock()
	defer client.run.Unlock()
	if client.runtime == nil {
		return 0, fmt.Errorf("KTF client initialization has not completed")
	}
	runtime := client.runtime
	if runtime.guestEventLoop || len(runtime.events) == 0 {
		return 0, nil
	}
	defer client.beginHostService(ctx)()
	previousThread, previousContext := runtime.currentThread, runtime.currentContext
	runtime.currentThread, runtime.currentContext = client.thread, ctx
	defer func() {
		runtime.currentThread, runtime.currentContext = previousThread, previousContext
	}()
	delivered := 0
	for waiting := len(runtime.events); waiting > 0; waiting-- {
		event, ok := runtime.nextGuestEvent()
		if !ok {
			break
		}
		delivered++
		if err := runtime.dispatchGuestEvent(client.vm, event); err != nil {
			return delivered, err
		}
	}
	return delivered, nil
}

// ServiceTimers runs up to limit due WIPI C timer callbacks in registration
// order and reports how many ran. A timer is due when its delay has elapsed on
// the session clock; the rest stay pending, which is what keeps a game whose
// frame loop is a repeating timer running at its own rate. Callbacks may
// register new timers; those wait for the next call.
func (client *Client) ServiceTimers(ctx context.Context, limit int) (int, error) {
	if client == nil || client.core == nil || client.thread == nil {
		return 0, fmt.Errorf("KTF client is not initialized")
	}
	client.run.Lock()
	defer client.run.Unlock()
	if client.runtime == nil {
		return 0, fmt.Errorf("KTF client initialization has not completed")
	}
	// Timers run on the client thread too, so a wait declared there holds them
	// as well; a timer that is due meanwhile stays pending rather than being
	// dropped.
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
			// Preserve the rest for the next service round.
			client.runtime.pendingTimers = append(client.runtime.pendingTimers, pending[index:]...)
			break
		}
		if timer.callback == 0 && timer.task == nil {
			continue
		}
		if now.Before(timer.due) {
			client.runtime.pendingTimers = append(client.runtime.pendingTimers, timer)
			continue
		}
		if timer.task != nil {
			// A java/util/Timer task runs on a thread of its own, which is
			// what the specification says and what a task that never returns
			// requires: one title's whole battle is a loop inside run(), and
			// running it here blocked the Host inside a single call — no
			// frame collected, no key delivered, and the run ending on the
			// step ceiling. A worker parks on its budget instead, so the loop
			// progresses tick after tick like a title's own thread does.
			if _, running := client.runningTimerTasks[timer.owner]; running {
				client.runtime.pendingTimers = append(client.runtime.pendingTimers, timer)
				continue
			}
			if err := client.startTimerTask(timer.owner, timer.task); err != nil {
				return serviced, err
			}
			if timer.period > 0 {
				timer.due = client.waitDeadline(timer.period)
				client.runtime.pendingTimers = append(client.runtime.pendingTimers, timer)
			}
			serviced++
			continue
		}
		run, err := client.core.Call(
			ctx,
			client.thread,
			timer.callback,
			ReturnAddress,
			[]uint32{timer.pointer, timer.param},
			client.runtime.handleSupervisorCall,
		)
		if err != nil {
			// A fault inside a timer callback gets the same evidence a fault
			// inside an AOT call gets. The address a fault names is never the
			// answer on its own, and a title whose whole loop is a timer would
			// otherwise report less about where it died than one that faults
			// in a method.
			if fault, isFault := guestFault(err); isFault {
				if report := client.runtime.describeGuestFault(run.Context, fault); report != "" {
					client.runtime.countDiagnostic("guest fault " + report)
					return serviced, fmt.Errorf("service KTF timer at %#x: %w: %s", timer.callback, err, report)
				}
			}
			return serviced, fmt.Errorf("service KTF timer at %#x: %w", timer.callback, err)
		}
		serviced++
	}
	return serviced, nil
}

// runSerialRunnable invokes one Display.callSerially Runnable's run() on the
// client thread. The caller holds the run lock.
func (client *Client) runSerialRunnable(ctx context.Context, runnable *jvm.Object) error {
	runtime := client.runtime
	previousThread, previousContext := runtime.currentThread, runtime.currentContext
	runtime.currentThread, runtime.currentContext = client.thread, ctx
	defer func() {
		runtime.currentThread, runtime.currentContext = previousThread, previousContext
	}()
	if _, err := client.vm.InvokeVirtual(runnable, "run", "()V"); err != nil {
		return fmt.Errorf("run KTF serial runnable %s: %w", runnable.ClassName, err)
	}
	return nil
}

// runTimerTask invokes one java/util/Timer task's run(). The caller holds the
// run lock and has already opened the Host service window.
// startTimerTask adopts one due java/util/Timer task as a guest worker, the
// same machinery Thread.start uses, and records it as that Timer's thread.
// The caller holds the run lock.
func (client *Client) startTimerTask(owner, task *jvm.Object) error {
	if client.workersStopped {
		return errWorkersStopped
	}
	worker, err := client.newGuestWorker(task)
	if err != nil {
		return fmt.Errorf("start KTF timer task %s: %w", task.ClassName, err)
	}
	worker.timerOwner = owner
	if client.runningTimerTasks == nil {
		client.runningTimerTasks = make(map[*jvm.Object]*guestWorker)
	}
	client.runningTimerTasks[owner] = worker
	client.workers = append(client.workers, worker)
	return nil
}

// WIPI key event types and key codes match the original runtime values games
// compare against in Card.keyNotify.
const (
	KeyPressed  int32 = 1
	KeyReleased int32 = 2
	KeyRepeated int32 = 3

	KeyUp        int32 = -1
	KeyDown      int32 = -2
	KeyLeft      int32 = -3
	KeyRight     int32 = -4
	KeyFire      int32 = -5
	KeyLeftSoft  int32 = -6
	KeyRightSoft int32 = -7
	// MH_KEY_SOFT3 is the third soft key, which these handsets carry as a
	// labelled key of its own beside the two under the screen. One LGT title's
	// party screen asks for it by that label — "press the EZ key for the
	// submenu" — and it is the only key on the pad a game can name that this
	// table was missing.
	KeyThirdSoft    int32 = -8
	KeyCall         int32 = -10
	KeyHangup       int32 = -11
	KeyVolumeUp     int32 = -13
	KeyVolumeDown   int32 = -14
	KeyClear        int32 = -16
	KeyNum0         int32 = '0'
	KeyNum9         int32 = '9'
	KeyHash         int32 = '#'
	KeyStar         int32 = '*'
	keyEventMinType       = KeyPressed
	keyEventMaxType       = KeyRepeated
)

// SendKey delivers one key event. A game that drives its own WIPI event loop
// receives it through the event queue; every other game gets the Host's direct
// card-stack dispatch. Delivering both ways would deliver the key twice.
func (client *Client) SendKey(ctx context.Context, eventType, key int32) error {
	if client == nil || client.core == nil || client.thread == nil {
		return fmt.Errorf("KTF client is not initialized")
	}
	if eventType < keyEventMinType || eventType > keyEventMaxType {
		return fmt.Errorf("KTF key event type %d is out of range", eventType)
	}
	client.run.Lock()
	defer client.run.Unlock()
	if client.runtime == nil {
		return fmt.Errorf("KTF client initialization has not completed")
	}
	runtime := client.runtime
	if runtime.guestEventLoop {
		runtime.postGuestEvent(guestEvent{kind: eventKindKey, param1: eventType, param2: key})
		return nil
	}
	defer client.beginHostService(ctx)()
	previousThread, previousContext := runtime.currentThread, runtime.currentContext
	runtime.currentThread, runtime.currentContext = client.thread, ctx
	defer func() {
		runtime.currentThread, runtime.currentContext = previousThread, previousContext
	}()
	return runtime.dispatchKeyToCards(eventType, key)
}

// ImageBytes reads back the client image as it now stands in guest memory.
// After the entry function has run that is the relocated image, which is the
// form a disassembler needs: the bytes in the archive are not the bytes that
// execute. Load the result at ImageBase.
func (client *Client) ImageBytes() ([]byte, error) {
	if client == nil || client.core == nil {
		return nil, fmt.Errorf("KTF client is not initialized")
	}
	size := client.mapped
	if size == 0 {
		return nil, fmt.Errorf("KTF client image is not mapped")
	}
	image := make([]byte, size)
	if err := client.core.Memory().Read(ImageBase, image); err != nil {
		return nil, fmt.Errorf("read KTF client image: %w", err)
	}
	return image, nil
}

// Pointer event types, as the specification's EventQueue names them:
// POINT_PRESSED is 1 and POINT_RELEASED is 2, the same two values a key press
// and release take, and **POINT_DRAGGED is 5** rather than the 3 that follows
// them — 3 and 4 belong to the key repeat and the typed key. The value was 3
// here until the specification was read, which is what reading a constant off
// its neighbours costs.
const (
	PointerPressed  int32 = 1
	PointerReleased int32 = 2
	PointerDragged  int32 = 5
)

// SendPointer delivers one pointer event to the card stack, the same traversal
// SendKey uses: the top card first, and downward while a card answers true.
//
// It takes the same two roads SendKey does, for the same reason: a title that
// drives its own getNextEvent loop is given a queued event and everything else
// is dispatched to the card stack directly. The queue's pointer event is the
// specification's — POINTER_EVENT is 2, with the type in event[1] and the
// coordinates in event[2] and event[3]. See docs/ktf.md.
func (client *Client) SendPointer(ctx context.Context, eventType, x, y int32) error {
	if client == nil || client.core == nil || client.thread == nil {
		return fmt.Errorf("KTF client is not initialized")
	}
	if eventType < PointerPressed || eventType > PointerDragged {
		return fmt.Errorf("KTF pointer event type %d is out of range", eventType)
	}
	client.run.Lock()
	defer client.run.Unlock()
	if client.runtime == nil {
		return fmt.Errorf("KTF client initialization has not completed")
	}
	runtime := client.runtime
	if runtime.guestEventLoop {
		runtime.postGuestEvent(guestEvent{kind: eventKindPointer, param1: eventType, param2: x, param3: y})
		return nil
	}
	defer client.beginHostService(ctx)()
	previousThread, previousContext := runtime.currentThread, runtime.currentContext
	runtime.currentThread, runtime.currentContext = client.thread, ctx
	defer func() {
		runtime.currentThread, runtime.currentContext = previousThread, previousContext
	}()
	return runtime.dispatchPointerToCards(eventType, x, y)
}

// HasPointer reports whether a touch would reach code the title wrote, which is
// a different question from whether this platform has pointer events at all.
//
// WIPI carries them because the last handsets of the era had a panel, and the
// local library shows what that was worth: of 264 archives, 27 carry a
// `pointerNotify` body and every one of those bodies belongs to the same
// middleware base card, in two builds of it. The other 237 never override the
// method, so a touch resolves to the platform's own body — `runtimeComponentZero`,
// which answers false and does nothing.
//
// **A Host has to know the difference, because offering a touch is not free.**
// The page turns the canvas into a touch surface when this is true, and the
// finger it takes is the one that was starting a keypad slide. Answering true
// everywhere would spend that gesture on 237 titles and hand back nothing.
//
// What this cannot see is a title that reads pointer events out of its own
// `EventQueue` loop rather than overriding the method. None is known here, and
// nothing static tells them apart — a queue kind is a number the title compares
// at runtime. `SendPointer` therefore still delivers wherever it is called:
// `runktf -touch` is how such a title would be found, and a probe that the Host
// had already decided against would find nothing.
func (client *Client) HasPointer() bool {
	if client == nil || client.vm == nil {
		return false
	}
	return aotDeclaresPointerNotify(client.vm.AOTClasses())
}

// aotDeclaresPointerNotify answers the question above over class metadata alone,
// so the rule is testable without a module around it. A native body does not
// count: that is the platform's own registration standing in the same table,
// and it is exactly what a title inherits when it wrote nothing.
func aotDeclaresPointerNotify(classes []jvm.AOTClassMetadata) bool {
	for _, class := range classes {
		for _, method := range class.Methods {
			if method.Name != "pointerNotify" || method.Descriptor != "(III)Z" {
				continue
			}
			if method.Body != 0 && method.NativeBody == 0 {
				return true
			}
		}
	}
	return false
}

// Jlet lifecycle methods. A handset called these when something took the
// screen away from the application — a call arriving, the user switching to
// the menu — and gave it back afterwards. A Host that parks a game whose page
// has gone away is in exactly that position.
const (
	jletPauseApp  = "pauseApp"
	jletResumeApp = "resumeApp"
)

// PauseApp and ResumeApp run the Jlet's own lifecycle callbacks.
//
// **They are entered the way a key is**, and that is the whole design. Guest
// code cannot be entered on top of a parked worker's nested Go stack — see
// "Guest workers unwound together" in docs/ktf.md for what that costs — so
// what these needed was a moment when the Host holds no guest stack at all.
// That moment already existed and already had two callers: SendKey and
// SendPointer enter guest code from the Host between ticks, taking the run
// lock and standing the client thread up as the current thread. A Host drives
// its session from one goroutine, so "between ticks" is every moment it is not
// inside Tick, and a park happens there by construction.
//
// This is therefore not the teardown problem. A close stops the workers and
// unwinds them; a pause leaves every one of them exactly where it is, parked
// with its stack intact, and runs one short method on the client thread. The
// game is still running afterwards, which is the point.
//
// A title that declares no such method is not an error. The callback is
// optional — half the local titles compile theirs to a prologue and a return,
// which says the same thing more expensively — so a missing method is nothing
// to call rather than something to report.
func (client *Client) PauseApp(ctx context.Context) error {
	return client.invokeJletLifecycle(ctx, jletPauseApp)
}

func (client *Client) ResumeApp(ctx context.Context) error {
	return client.invokeJletLifecycle(ctx, jletResumeApp)
}

func (client *Client) invokeJletLifecycle(ctx context.Context, name string) error {
	if client == nil || client.core == nil || client.thread == nil {
		return fmt.Errorf("KTF client is not initialized")
	}
	client.run.Lock()
	defer client.run.Unlock()
	if client.runtime == nil {
		return fmt.Errorf("KTF client initialization has not completed")
	}
	runtime := client.runtime
	if client.workersStopped {
		// The session is being torn down. Whatever this was going to tell the
		// application, the application is about to stop existing.
		return nil
	}
	jlet := runtime.activeJlet
	if jlet == nil || jlet.ClassName == "" {
		// Nothing constructed a Jlet, so there is nothing with a lifecycle.
		return nil
	}
	defer client.beginHostService(ctx)()
	previousThread, previousContext := runtime.currentThread, runtime.currentContext
	runtime.currentThread, runtime.currentContext = client.thread, ctx
	defer func() {
		runtime.currentThread, runtime.currentContext = previousThread, previousContext
	}()
	if !runtime.hasAOTMethod(jlet.ClassName, name, "()V") {
		return nil
	}
	_, err := runtime.invokeAOTFromJVM(jlet.ClassName, name, "()V", []jvm.Value{jvm.ReferenceValue(jlet)})
	return err
}

// AttachSaveStore supplies the persistence boundary DataBase, the WIPI C
// database, and File writes use. Without one, saves stay in memory.
func (client *Client) AttachSaveStore(store SaveStore) {
	if client == nil {
		return
	}
	client.saveStore = store
}

// AttachAppProperties supplies the application descriptor fields
// Jlet.getAppProperty answers from. Keys are upper-case, as the descriptor
// parser stores them.
func (client *Client) AttachAppProperties(properties map[string]string) {
	if client == nil || len(properties) == 0 {
		return
	}
	client.appProperties = properties
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

// programNameOverrides maps ADF AIDs to the program name baked into the
// client binary when they differ. One title compares MC_knlGetProgramName
// against "010100D5" in client.bin and refuses to start its card otherwise.
var programNameOverrides = map[string]string{
	"0102DD43": "010100D5",
}

// ProgramNameForAID resolves the identifier MC_knlGetProgramName should
// report for an archive's AID, honoring known binary-baked overrides.
func ProgramNameForAID(aid string) string {
	if name, ok := programNameOverrides[aid]; ok {
		return name
	}
	return aid
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

func LoadClient(image ClientImage, options armcore.CoreOptions) (*Client, error) {
	bssSize, err := parseBSSSize(image.Name)
	if err != nil {
		return nil, err
	}
	if bssSize != image.BSSSize {
		return nil, fmt.Errorf("KTF client image %q suffix names BSS %d but image specifies %d", image.Name, bssSize, image.BSSSize)
	}
	if len(image.Data) == 0 {
		return nil, fmt.Errorf("KTF client image %q is empty", image.Name)
	}
	mappedSize := image.MappedSize()
	if mappedSize > maxClientMappedSize {
		return nil, fmt.Errorf("KTF client image %q maps %d bytes, limit %d", image.Name, mappedSize, maxClientMappedSize)
	}
	if uint64(ImageBase)+mappedSize > uint64(ThreadStackBase) {
		return nil, fmt.Errorf("KTF client image %q overlaps the guest stack", image.Name)
	}

	// An older module is relocated before it is mapped, and names its own
	// entry rather than starting at one. See client_relocatable.go.
	loaded, entry, argument := image.Data, EntryAddress, image.BSSSize
	relocatable, segmentBase := false, uint32(0)
	if module, ok := parseRelocatableClient(image.Data); ok {
		relocated, relocateErr := module.relocate(image.Data, ImageBase)
		if relocateErr != nil {
			return nil, fmt.Errorf("relocate KTF client image %q: %w", image.Name, relocateErr)
		}
		// The entry takes the platform's callback table rather than a size:
		// it reads that table's first word and calls it to resolve an
		// interface by name. The argument is filled in by ExecuteModuleEntry,
		// which is the only caller that has a table to hand it.
		loaded, entry, argument, relocatable = relocated, module.entryAddress(relocated), 0, true
		segmentBase = ImageBase + uint32(module.segmentStart)
	}

	core := armcore.NewCore(options)
	if err := core.Memory().Map(ImageBase, mappedSize, armcore.PermissionReadWriteExecute); err != nil {
		return nil, fmt.Errorf("map KTF client image %q: %w", image.Name, err)
	}
	if err := core.Memory().Load(ImageBase, loaded); err != nil {
		return nil, fmt.Errorf("load KTF client image %q: %w", image.Name, err)
	}
	if err := core.Memory().Map(ThreadStackBase, ThreadStackSize, armcore.PermissionReadWrite); err != nil {
		return nil, fmt.Errorf("map KTF client stack: %w", err)
	}
	initial := armcore.NewContext()
	initial.Registers[armcore.RegisterSP] = ThreadStackBase + uint32(ThreadStackSize)
	client := &Client{
		core:     core,
		thread:   armcore.NewThread(initial),
		image:    image,
		mapped:   mappedSize,
		entry:    entry,
		argument: argument,
		module:   relocatable,

		moduleSegment: segmentBase,
	}
	// KTF handsets decode byte content as EUC-KR; games index parsed strings
	// by character position, so the platform charset must match. Guest Java
	// threads share the single cooperative ARM core, so every Thread.start
	// queues for the Host service loop instead of spawning a goroutine. The
	// clock is the guest's own, so System.currentTimeMillis and the WIPI
	// kernel's MC_knlCurrentTime read one timeline; a game that measures a
	// frame with one and waits with the other stays consistent.
	client.vm = jvm.New(nil, jvm.Options{
		ByteDecoder: decodeEUCKR,
		ByteEncoder: encodeEUCKR,
		Clock: func() int64 {
			if client.runtime == nil {
				return time.Now().UnixMilli()
			}
			return client.runtime.guestMillis()
		},
		GuestThreadStarter: func(thread *jvm.Object) error {
			if client.runtime == nil {
				return fmt.Errorf("KTF guest thread started before initialization")
			}
			if len(client.runtime.pendingThreads) >= maxPendingThreads {
				return fmt.Errorf("KTF pending thread count exceeds %d", maxPendingThreads)
			}
			client.runtime.countDiagnostic("thread start " + thread.ClassName)
			client.runtime.pendingThreads = append(client.runtime.pendingThreads, thread)
			return nil
		},
	})
	return client, nil
}

func decodeEUCKR(data []byte) string {
	decoded, err := korean.EUCKR.NewDecoder().Bytes(data)
	if err != nil {
		return strings.ToValidUTF8(string(data), "�")
	}
	return string(decoded)
}

func encodeEUCKR(value string) []byte {
	encoded, err := korean.EUCKR.NewEncoder().Bytes([]byte(value))
	if err != nil {
		return []byte(value)
	}
	return encoded
}

func (client *Client) Core() *armcore.Core {
	return client.core
}

func (client *Client) Image() ClientImage {
	return client.image
}

// JVM returns the shared Java execution layer that owns KTF AOT metadata and
// guest-address object bindings.
func (client *Client) JVM() *jvm.VM {
	if client == nil {
		return nil
	}
	return client.vm
}

// ExecuteEntry runs the KTF Thumb entry point with the decimal BSS suffix in
// r0. Every Host shares this platform-composed path.
func (client *Client) ExecuteEntry(ctx context.Context, handler armcore.SupervisorCallHandler) (armcore.RunSummary, error) {
	if client == nil || client.core == nil || client.thread == nil {
		return armcore.RunSummary{}, fmt.Errorf("KTF client is not initialized")
	}
	client.run.Lock()
	defer client.run.Unlock()
	result, err := client.core.Call(ctx, client.thread, client.entry, ReturnAddress, []uint32{client.argument}, handler)
	if err != nil {
		return result, fmt.Errorf("execute KTF client entry %q: %w", client.image.Name, err)
	}
	return result, nil
}

// IsModule reports whether this image is one of the older relocatable modules,
// whose entry takes the platform's callback table rather than a size.
func (client *Client) IsModule() bool {
	return client != nil && client.module
}

// ExecuteModuleEntry runs an older module's entry with the platform callback
// table it expects. The runtime that owns that table has to exist first, which
// is the whole difference from ExecuteEntry: the current generation relocates
// itself with no platform underneath it and only meets one in Initialize.
func (client *Client) ExecuteModuleEntry(ctx context.Context) (armcore.RunSummary, error) {
	if client == nil || client.core == nil || client.thread == nil {
		return armcore.RunSummary{}, fmt.Errorf("KTF client is not initialized")
	}
	if !client.module {
		return armcore.RunSummary{}, fmt.Errorf("KTF client image %q is not a relocatable module", client.image.Name)
	}
	client.run.Lock()
	defer client.run.Unlock()
	if client.initializationStarted {
		return armcore.RunSummary{}, fmt.Errorf("KTF client initialization already started")
	}
	client.initializationStarted = true
	runtime, parameters, err := client.prepareInitialization()
	if err != nil {
		return armcore.RunSummary{}, err
	}
	client.runtime = runtime
	// The module's runtime glue reaches its context through `fp` and expects
	// a caller to be holding it, so it is installed before any of the
	// module's code runs. See module_link.go.
	if _, err := runtime.prepareModuleContext(); err != nil {
		return armcore.RunSummary{}, err
	}
	if err := runtime.placeModuleAliases(); err != nil {
		return armcore.RunSummary{}, err
	}
	if err := runtime.indexModuleClasses(); err != nil {
		return armcore.RunSummary{}, err
	}
	if err := runtime.installModuleJumps(); err != nil {
		return armcore.RunSummary{}, err
	}
	// The callback table is the last of the five words the current
	// generation's initialization function takes, and its first word is the
	// `getInterface` this entry calls.
	client.argument = parameters[len(parameters)-1]
	result, err := client.core.Call(ctx, client.thread, client.entry, ReturnAddress, []uint32{client.argument}, runtime.handleSupervisorCall)
	if err != nil {
		return result, fmt.Errorf("execute KTF module entry %q: %w", client.image.Name, err)
	}
	if answer := result.Context.Registers[0]; answer != 0 {
		return result, fmt.Errorf("KTF module entry %q returned %#x", client.image.Name, answer)
	}
	// The module publishes its classes rather than registering them, so this
	// side links and registers them once the interface it asked for exists.
	if err := runtime.linkModuleClasses(); err != nil {
		return result, fmt.Errorf("link KTF module %q: %w", client.image.Name, err)
	}
	if err := runtime.bindModuleStrings(); err != nil {
		return result, fmt.Errorf("bind KTF module %q strings: %w", client.image.Name, err)
	}
	return result, nil
}
