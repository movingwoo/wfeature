package ktf

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/movingwoo/wfeature/internal/armcore"
	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/jvm"
	"github.com/movingwoo/wfeature/internal/platform/wipicrt"
	"github.com/movingwoo/wfeature/internal/textinput"
)

const (
	EntryAddress = wipicrt.ImageBase + 1
)

type Client struct {
	*wipicrt.Client

	authentication backend.AuthenticationStatus
	image          ClientImage
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

	// The lwc text component that currently has the keypad, and the editor
	// driving it. See runtime_lwc_input.go.
	textMu           sync.Mutex
	focusedText      *jvm.Object
	textEditor       *textinput.State
	appProperties    map[string]string
	forcedSlice      bool // Last thread service paused unfinished computation.
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
	// unwindBound overrides how long StopThreads waits for one aborted worker
	// to return; zero means stopUnwindBound. Only a test that stops a worker
	// which cannot answer sets it, so the bound's own path is exercised
	// without a test that takes seconds.
	unwindBound time.Duration
	// skipPaint drops this round's card paint, and lastPaint is when one last
	// happened. A host that has fallen behind spends its rounds on the guest's
	// logic instead of its screen; see behindOnPaint.
	skipPaint bool
	lastPaint time.Time
	// uncaught counts the guest exceptions that ended a callback this Host
	// started, and keeps what the first one said. Absorbing one keeps the
	// session alive, so without a count a title that fails every paint reads
	// exactly like a title that plays. See uncaught.go.
	uncaught backend.UncaughtCallbacks
}

// ServiceThreads grants one step slice to up to limit live guest Java
// threads in queue order and reports how many made progress. Each guest
// thread runs run() on a dedicated worker goroutine with a private ARM stack;
// a worker whose slice ends (step budget or guest sleep) parks with its whole
// call stack intact and rejoins the back of the queue, so an infinite main
// loop keeps progressing across ticks instead of dropping at the step limit.
func (client *Client) ServiceThreads(ctx context.Context, limit int) (int, error) {
	if client == nil || client.Core() == nil || client.Thread == nil {
		return 0, fmt.Errorf("KTF client is not initialized")
	}
	client.Run.Lock()
	defer client.Run.Unlock()
	client.forcedSlice = false
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
	if len(runtime.pendingSerial) > 0 && client.ClientThreadDue() && !runtime.repaintQueued() &&
		(len(runtime.pendingSerial) > 1 || !client.Now().Before(runtime.serialDueAt)) {
		runnable := runtime.pendingSerial[0]
		runtime.pendingSerial = runtime.pendingSerial[1:]
		runtime.serialDueAt = client.WaitDeadline(serialDispatchInterval)
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
	// "A thread that finished is not a slice" in docs/history/ktf.md.
	//
	// What bounds the drain is steps rather than a count: a round grants at
	// most one slice's worth of execution beyond the grant it was already
	// entitled to, so retiring a hundred threads that each cost a few thousand
	// steps is allowed and one that spends a whole slice before returning ends
	// the round.
	// The serial callback runs on the client thread. It does not spend a
	// guest worker grant: a callback that requeues itself every round would
	// otherwise starve all workers when the host supplies a limit of one.
	slices := 0
	spent := uint64(0)
	allowance := client.ThreadSliceSteps
	if allowance == 0 {
		allowance = defaultThreadSliceSteps
	}
	previousThread, previousContext := runtime.CurrentThread, runtime.CurrentContext
	defer func() {
		runtime.CurrentThread, runtime.CurrentContext = previousThread, previousContext
	}()
	// A worker that asked to sleep is not eligible until its deadline passes,
	// which is what paces a game to the speed it was written for. Skipped
	// workers rejoin the queue, and one pass over the queue bounds the search
	// so a round where every worker is sleeping ends instead of spinning.
	now := client.Now()
	for attempts := len(client.workers); attempts > 0 && slices < round && spent < allowance; attempts-- {
		worker := client.workers[0]
		client.workers = client.workers[1:]
		if now.Before(worker.wakeAt) {
			client.workers = append(client.workers, worker)
			continue
		}
		before := client.Core().Steps()
		runtime.CurrentThread, runtime.CurrentContext = worker.armThread, ctx
		client.activeWorker = worker
		worker.grant <- struct{}{}
		event := <-worker.events
		client.forcedSlice = event.forced
		// A worker can stop inside a DataOutputStream call without another
		// platform call, so what that call holds is stored before anything
		// else runs; see storeHeldGuestFiles.
		runtime.storeHeldGuestFiles()
		client.activeWorker = nil
		serviced++
		spent += client.Core().Steps() - before
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
		// A repeating task's worker ends after each run, but the scheduled
		// task still owns its requested frame cadence until cancellation.
		if worker.timerOwner != nil && worker.paintedCard != nil {
			for i := range runtime.PendingTimers {
				timer := &runtime.PendingTimers[i]
				if guestObject(timer.Owner) == worker.timerOwner && guestObject(timer.Task) == worker.javaThread {
					timer.PaintedCard = guestValue(worker.paintedCard)
				}
			}
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
	if client == nil || client.Core() == nil || client.Thread == nil {
		return false, fmt.Errorf("KTF client is not initialized")
	}
	client.Run.Lock()
	defer client.Run.Unlock()
	if client.runtime == nil {
		return false, fmt.Errorf("KTF client initialization has not completed")
	}
	if !client.ClientThreadDue() {
		return false, nil
	}
	runtime := client.runtime
	// **The rate belongs to the paint nobody asked for.** A repaint the guest
	// requested is the guest asking for a frame now, and holding one back
	// stalls more than the screen: the serial queue waits on an outstanding
	// repaint, and the repaint is cleared by the paint, so a gated one leaves
	// both parked on each other. See NextDeadline.
	if !runtime.repaintQueued() && (client.forcedSlice || client.Now().Before(client.NextRoundPaint)) {
		return false, nil
	}
	defer client.BeginHostService(ctx)()
	previousThread, previousContext := runtime.CurrentThread, runtime.CurrentContext
	runtime.CurrentThread, runtime.CurrentContext = client.Thread, ctx
	defer func() {
		runtime.CurrentThread, runtime.CurrentContext = previousThread, previousContext
	}()
	painted, err := runtime.paintTopCard()
	// **The next frame is armed whether or not this one drew.** paintTopCard
	// declines a round while the guest is publishing its own frames, and a
	// declined round is still a round the Host has to come back from: the rule
	// that declined it counts rounds, so without a deadline to come back at, a
	// title with nothing else running reports no work pending and a batch Host
	// stops on the frame before the handover. Only the absence of a card means
	// there is no frame to arm.
	if len(runtime.displayCards) > 0 {
		client.NextRoundPaint = client.WaitDeadline(wipicrt.MinGuestFramePeriod)
	}
	return painted, err
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
	if client == nil || client.Core() == nil || client.Thread == nil {
		return 0, fmt.Errorf("KTF client is not initialized")
	}
	client.Run.Lock()
	defer client.Run.Unlock()
	if client.runtime == nil {
		return 0, fmt.Errorf("KTF client initialization has not completed")
	}
	runtime := client.runtime
	if len(runtime.mediaEvents) == 0 && (runtime.GuestEventLoop || len(runtime.Events) == 0) {
		return 0, nil
	}
	defer client.BeginHostService(ctx)()
	previousThread, previousContext := runtime.CurrentThread, runtime.CurrentContext
	runtime.CurrentThread, runtime.CurrentContext = client.Thread, ctx
	defer func() {
		runtime.CurrentThread, runtime.CurrentContext = previousThread, previousContext
	}()
	delivered := 0
	waitingMedia := len(runtime.mediaEvents)
	// Drain the existing generic prefix first. Media callbacks can fill that
	// queue and drop its oldest entries; counting it before media delivery
	// alone would then let newly posted events enter this same round. Keep
	// media queued until this succeeds: the session absorbs guest exceptions
	// from generic handlers and must not lose its waiting notifications.
	if !runtime.GuestEventLoop {
		for waiting := len(runtime.Events); waiting > 0; waiting-- {
			event, ok := runtime.NextGuestEvent()
			if !ok {
				break
			}
			delivered++
			if err := runtime.dispatchGuestEvent(client.vm, event); err != nil {
				return delivered, err
			}
		}
	}
	media := runtime.mediaEvents[:waitingMedia]
	// Copy only the new suffix, leaving the delivered prefix collectible.
	runtime.mediaEvents = append([]clipEvent(nil), runtime.mediaEvents[waitingMedia:]...)
	for _, event := range media {
		delivered++
		if err := runtime.deliverClipEvent(event); err != nil {
			return delivered, err
		}
	}
	return delivered, nil
}

type serialPaintOwner struct {
	card *jvm.Object
}

// runSerialRunnable invokes one Display.callSerially Runnable's run() on the
// client thread. The caller holds the run lock.
func (client *Client) runSerialRunnable(ctx context.Context, runnable *jvm.Object) error {
	runtime := client.runtime
	previousThread, previousContext := runtime.CurrentThread, runtime.CurrentContext
	previousPaintOwner := runtime.activeSerialPaint
	paintOwner := &serialPaintOwner{card: runtime.serialPaintOwners[runnable]}
	delete(runtime.serialPaintOwners, runnable)
	runtime.activeSerialPaint = paintOwner
	runtime.CurrentThread, runtime.CurrentContext = client.Thread, ctx
	defer func() {
		runtime.CurrentThread, runtime.CurrentContext = previousThread, previousContext
		runtime.activeSerialPaint = previousPaintOwner
	}()
	if _, err := client.vm.InvokeVirtual(runnable, "run", "()V"); err != nil {
		return fmt.Errorf("run KTF serial runnable %s: %w", runnable.ClassName, err)
	}
	// A self-queued loop can poll many times between requested frames.
	// Keep its cadence while the same Runnable remains queued, and release
	// it when that loop ends. Unrelated queued work inherits nothing.
	if paintOwner.card != nil {
		for _, pending := range runtime.pendingSerial {
			if pending == runnable {
				if runtime.serialPaintOwners == nil {
					runtime.serialPaintOwners = make(map[*jvm.Object]*jvm.Object)
				}
				runtime.serialPaintOwners[runnable] = paintOwner.card
				break
			}
		}
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
	keyEventMinType = wipicrt.KeyPressed
	keyEventMaxType = wipicrt.KeyRepeated
)

// SendKey delivers one key event. A game that drives its own WIPI event loop
// receives it through the event queue; every other game gets the Host's direct
// card-stack dispatch. Delivering both ways would deliver the key twice.
func (client *Client) SendKey(ctx context.Context, eventType, key int32) error {
	if client == nil || client.Core() == nil || client.Thread == nil {
		return fmt.Errorf("KTF client is not initialized")
	}
	if eventType < keyEventMinType || eventType > keyEventMaxType {
		return fmt.Errorf("KTF key event type %d is out of range", eventType)
	}
	client.Run.Lock()
	defer client.Run.Unlock()
	if client.runtime == nil {
		return fmt.Errorf("KTF client initialization has not completed")
	}
	runtime := client.runtime
	if runtime.GuestEventLoop {
		runtime.PostGuestEvent(wipicrt.GuestEvent{Kind: wipicrt.EventKindKey, Param1: eventType, Param2: key})
		return nil
	}
	defer client.BeginHostService(ctx)()
	previousThread, previousContext := runtime.CurrentThread, runtime.CurrentContext
	runtime.CurrentThread, runtime.CurrentContext = client.Thread, ctx
	defer func() {
		runtime.CurrentThread, runtime.CurrentContext = previousThread, previousContext
	}()
	// A key is a callback like a paint: what the title's keyNotify throws and
	// does not catch ends that call, not the game. See uncaught.go.
	return client.absorbUncaughtCallback("key", runtime.dispatchKeyToCards(eventType, key))
}

// ImageBytes reads back the client image as it now stands in guest memory.
// After the entry function has run that is the relocated image, which is the
// form a disassembler needs: the bytes in the archive are not the bytes that
// execute. Load the result at ImageBase.
func (client *Client) ImageBytes() ([]byte, error) {
	if client == nil || client.Core() == nil {
		return nil, fmt.Errorf("KTF client is not initialized")
	}
	size := client.mapped
	if size == 0 {
		return nil, fmt.Errorf("KTF client image is not mapped")
	}
	image := make([]byte, size)
	if err := client.Core().Memory().Read(wipicrt.ImageBase, image); err != nil {
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
	if client == nil || client.Core() == nil || client.Thread == nil {
		return fmt.Errorf("KTF client is not initialized")
	}
	if eventType < PointerPressed || eventType > PointerDragged {
		return fmt.Errorf("KTF pointer event type %d is out of range", eventType)
	}
	client.Run.Lock()
	defer client.Run.Unlock()
	if client.runtime == nil {
		return fmt.Errorf("KTF client initialization has not completed")
	}
	runtime := client.runtime
	if runtime.GuestEventLoop {
		runtime.PostGuestEvent(wipicrt.GuestEvent{Kind: wipicrt.EventKindPointer, Param1: eventType, Param2: x, Param3: y})
		return nil
	}
	defer client.BeginHostService(ctx)()
	previousThread, previousContext := runtime.CurrentThread, runtime.CurrentContext
	runtime.CurrentThread, runtime.CurrentContext = client.Thread, ctx
	defer func() {
		runtime.CurrentThread, runtime.CurrentContext = previousThread, previousContext
	}()
	return client.absorbUncaughtCallback("pointer", runtime.dispatchPointerToCards(eventType, x, y))
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
	jletPauseApp   = "pauseApp"
	jletResumeApp  = "resumeApp"
	jletDestroyApp = "destroyApp"
)

// PauseApp and ResumeApp run the Jlet's own lifecycle callbacks.
//
// **They are entered the way a key is**, and that is the whole design. Guest
// code cannot be entered on top of a parked worker's nested Go stack — see
// "Guest workers unwound together" in docs/history/ktf.md for what that costs — so
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
	return client.invokeJletLifecycle(ctx, jletPauseApp, "()V")
}

func (client *Client) ResumeApp(ctx context.Context) error {
	return client.invokeJletLifecycle(ctx, jletResumeApp, "()V")
}

// DestroyApp tells the active Jlet that an unconditional Host teardown has
// begun. It must run before StopThreads: after that point guest code cannot be
// entered safely.
func (client *Client) DestroyApp(ctx context.Context) error {
	return client.invokeJletLifecycle(ctx, jletDestroyApp, "(Z)V", jvm.IntValue(1))
}

func (client *Client) invokeJletLifecycle(ctx context.Context, name, descriptor string, arguments ...jvm.Value) error {
	if client == nil || client.Core() == nil || client.Thread == nil {
		return fmt.Errorf("KTF client is not initialized")
	}
	client.Run.Lock()
	defer client.Run.Unlock()
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
	if name == jletDestroyApp {
		if runtime.destroyCallbackStarted {
			return nil
		}
		// The transition is unconditional and one-way. Mark it before guest
		// entry: an exit, exception or exhausted instruction allowance must
		// not make a later Close repeat partially completed cleanup.
		runtime.destroyCallbackStarted = true
	}
	defer client.BeginHostService(ctx)()
	previousThread, previousContext := runtime.CurrentThread, runtime.CurrentContext
	runtime.CurrentThread, runtime.CurrentContext = client.Thread, ctx
	defer func() {
		runtime.CurrentThread, runtime.CurrentContext = previousThread, previousContext
	}()
	if !runtime.hasAOTMethod(jlet.ClassName, name, descriptor) {
		return nil
	}
	arguments = append([]jvm.Value{jvm.ReferenceValue(jlet)}, arguments...)
	_, err := runtime.invokeAOTFromJVM(jlet.ClassName, name, descriptor, arguments)
	return err
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
	if uint64(wipicrt.ImageBase)+mappedSize > uint64(wipicrt.ThreadStackBase) {
		return nil, fmt.Errorf("KTF client image %q overlaps the guest stack", image.Name)
	}

	// An older module is relocated before it is mapped, and names its own
	// entry rather than starting at one. See client_relocatable.go.
	loaded, entry, argument := image.Data, EntryAddress, image.BSSSize
	relocatable, segmentBase := false, uint32(0)
	if module, ok := parseRelocatableClient(image.Data); ok {
		relocated, relocateErr := module.relocate(image.Data, wipicrt.ImageBase)
		if relocateErr != nil {
			return nil, fmt.Errorf("relocate KTF client image %q: %w", image.Name, relocateErr)
		}
		// The entry takes the platform's callback table rather than a size:
		// it reads that table's first word and calls it to resolve an
		// interface by name. The argument is filled in by ExecuteModuleEntry,
		// which is the only caller that has a table to hand it.
		loaded, entry, argument, relocatable = relocated, module.entryAddress(relocated), 0, true
		segmentBase = wipicrt.ImageBase + uint32(module.segmentStart)
	}

	core := armcore.NewCore(options)
	if err := core.Memory().Map(wipicrt.ImageBase, mappedSize, armcore.PermissionReadWriteExecute); err != nil {
		return nil, fmt.Errorf("map KTF client image %q: %w", image.Name, err)
	}
	if err := core.Memory().Load(wipicrt.ImageBase, loaded); err != nil {
		return nil, fmt.Errorf("load KTF client image %q: %w", image.Name, err)
	}
	if err := core.Memory().Map(wipicrt.ThreadStackBase, wipicrt.ThreadStackSize, armcore.PermissionReadWrite); err != nil {
		return nil, fmt.Errorf("map KTF client stack: %w", err)
	}
	initial := armcore.NewContext()
	initial.Registers[armcore.RegisterSP] = wipicrt.ThreadStackBase + uint32(wipicrt.ThreadStackSize)
	engine := wipicrt.NewClient(ktfPlatform, core, armcore.NewThread(initial), image.MappedSize())
	engine.SubscriberNumber = HandsetNumber()
	client := &Client{
		Client:   engine,
		image:    image,
		mapped:   mappedSize,
		entry:    entry,
		argument: argument,
		module:   relocatable,

		moduleSegment: segmentBase,
	}
	engine.Hooks = client.engineHooks()
	// KTF handsets decode byte content as EUC-KR; games index parsed strings
	// by character position, so the platform charset must match. Guest Java
	// threads share the single cooperative ARM core, so every Thread.start
	// queues for the Host service loop instead of spawning a goroutine. The
	// clock is the guest's own, so System.currentTimeMillis and the WIPI
	// kernel's MC_knlCurrentTime read one timeline; a game that measures a
	// frame with one and waits with the other stays consistent.
	client.vm = jvm.New(nil, jvm.Options{
		ByteDecoder: backend.DecodeEUCKR,
		ByteEncoder: backend.EncodeEUCKR,
		Clock: func() int64 {
			if client.runtime == nil {
				return time.Now().UnixMilli()
			}
			return client.runtime.GuestMillis()
		},
		GuestThreadStarter: func(thread *jvm.Object) error {
			if client.runtime == nil {
				return fmt.Errorf("KTF guest thread started before initialization")
			}
			if len(client.runtime.pendingThreads) >= maxPendingThreads {
				return fmt.Errorf("KTF pending thread count exceeds %d", maxPendingThreads)
			}
			client.runtime.CountDiagnostic("thread start " + thread.ClassName)
			client.runtime.pendingThreads = append(client.runtime.pendingThreads, thread)
			return nil
		},
	})
	return client, nil
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
	if client == nil || client.Core() == nil || client.Thread == nil {
		return armcore.RunSummary{}, fmt.Errorf("KTF client is not initialized")
	}
	client.Run.Lock()
	defer client.Run.Unlock()
	result, err := client.Core().Call(ctx, client.Thread, client.entry, wipicrt.ReturnAddress, []uint32{client.argument}, handler)
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
	if client == nil || client.Core() == nil || client.Thread == nil {
		return armcore.RunSummary{}, fmt.Errorf("KTF client is not initialized")
	}
	if !client.module {
		return armcore.RunSummary{}, fmt.Errorf("KTF client image %q is not a relocatable module", client.image.Name)
	}
	client.Run.Lock()
	defer client.Run.Unlock()
	if client.initializationStarted {
		return armcore.RunSummary{}, fmt.Errorf("KTF client initialization already started")
	}
	client.initializationStarted = true
	runtime, parameters, err := client.prepareInitialization()
	if err != nil {
		return armcore.RunSummary{}, err
	}
	client.setRuntimes(runtime, client.prepared)
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
	result, err := client.Core().Call(ctx, client.Thread, client.entry, wipicrt.ReturnAddress, []uint32{client.argument}, runtime.handleSupervisorCall)
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
