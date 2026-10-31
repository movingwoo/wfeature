package ktf

import (
	"errors"
	"fmt"
	"time"

	"github.com/movingwoo/wfeature/internal/armcore"
	"github.com/movingwoo/wfeature/internal/jvm"
	"github.com/movingwoo/wfeature/internal/platform/wipicrt"
)

// KTF titles run their WIPI-C calls on the engine SKT Clets run on
// (internal/platform/wipicrt). What this file holds is KTF's side of that
// boundary: what the engine is told about the platform, and the answers the
// engine asks the rest of this runtime for — the Java side a KTF title has
// beside its C one.

// ktfPlatform is what the engine is told about KTF. A memory identifier's
// first word points at the size word four bytes on, and MC_GETDPTR is eight
// bytes past that word.
var ktfPlatform = wipicrt.Platform{
	Name:            "KTF",
	CName:           "KTF WIPI C",
	CallName:        "KTF AOT",
	ExitError:       ErrGuestExited,
	HandleFirstWord: 4,
	HandleData:      8,
}

// guestValue and guestObject carry a Java object through a field the engine
// only holds for the platform. A missing object stays the untyped nil the
// engine tests for, so a nil object never reads as a task or a card.
func guestValue(object *jvm.Object) any {
	if object == nil {
		return nil
	}
	return object
}

func guestObject(value any) *jvm.Object {
	object, _ := value.(*jvm.Object)
	return object
}

// engineHooks are KTF's answers to what the engine does not own.
func (runtime *initializationRuntime) engineHooks() wipicrt.RuntimeHooks {
	return wipicrt.RuntimeHooks{
		Supervisor:         runtime.handleSupervisorCall,
		SyncClips:          runtime.syncJavaClips,
		StorageUsed:        runtime.platformStorageUsed,
		DescribeGuestFault: runtime.describeAOTFault,
		DescribeGuestWord:  runtime.describeAOTWord,
		NotAFault:          notAGuestFault,
		NoteRead:           runtime.noteAOTRead,
		WorkerThread:       runtime.workerThread,
		InputCardCurrent:   func() bool { return runtime.cInputCardOwner == runtime.cInputCard() },
		InputActivated:     func() { runtime.cInputCardOwner = runtime.cInputCard() },
		RepaintCard:        func() any { return guestValue(runtime.topCard()) },
		RecordDatabase:     runtime.handleWIPICRecordDatabaseCall,
		ReservedKernelSlot: func() uint32 { return runtime.wipicInterface },
		Freed:              runtime.forgetImageSurface,
		SaveBlocked:        runtime.storageRestoredUnbound,
		// The collector runs for an allocation the arena cannot satisfy, and
		// collected objects give back the surfaces and clips they held.
		CollectForAllocation: runtime.collectOnFailedAllocation,
		CollectHostResources: runtime.collectHostObjects,
		ScreenFlushed:        runtime.markWorkerFrame,
	}
}

// notAGuestFault reports the errors that end a guest call without a fault in
// it: an exception unwinding through it, or one nothing caught.
func notAGuestFault(err error) bool {
	var unwind *aotExceptionUnwind
	if errors.As(err, &unwind) {
		return true
	}
	var uncaught *UncaughtAOTException
	return errors.As(err, &uncaught)
}

// noteAOTRead records the bytes a class read takes while one is in progress.
func (runtime *initializationRuntime) noteAOTRead(address uint32, data []byte, cString bool) {
	if runtime.aotReading != nil {
		runtime.aotReading.note(address, data, cString)
	}
}

// workerThread is the guest thread a worker is running a call on.
func (runtime *initializationRuntime) workerThread() (*armcore.Thread, bool) {
	if worker := runtime.client.activeWorker; worker != nil {
		return worker.armThread, true
	}
	return nil, false
}

// forgetImageSurface drops the Java images whose surface was the identifier
// given back.
func (runtime *initializationRuntime) forgetImageSurface(id uint32) {
	for owner, handle := range runtime.imageSurfaces {
		if handle == id {
			delete(runtime.imageSurfaces, owner)
		}
	}
}

// storageRestoredUnbound refuses to store from a runtime a checkpoint brought
// back before a store has filled it.
func (runtime *initializationRuntime) storageRestoredUnbound() error {
	if runtime.restoredStorage != nil {
		return fmt.Errorf("KTF storage was restored from a checkpoint and is not bound to a save store")
	}
	return nil
}

// markWorkerFrame makes the publishing worker's next declared wait a frame
// period rather than any other kind of sleep. See frameLoopPeriod, which is
// the only reader and the only thing that clears it.
func (runtime *initializationRuntime) markWorkerFrame() {
	if worker := runtime.client.activeWorker; worker != nil {
		worker.publishedFrame = true
	}
}

// engineHooks are KTF's parts of a service round.
func (client *Client) engineHooks() wipicrt.ClientHooks {
	return wipicrt.ClientHooks{
		Deadlines:        client.platformDeadlines,
		TimerTaskRunning: client.timerTaskRunning,
		StartTimerTask: func(timer wipicrt.WIPICTimer) error {
			return client.startTimerTask(guestObject(timer.Owner), guestObject(timer.Task))
		},
	}
}

// timerTaskRunning reports a java/util/Timer whose worker is still running one
// of its tasks. ServiceTimers runs one task per Timer at a time.
func (client *Client) timerTaskRunning(timer wipicrt.WIPICTimer) bool {
	_, running := client.runningTimerTasks[guestObject(timer.Owner)]
	return running
}

// platformDeadlines offers the work only KTF has to the Host's next deadline.
// The caller holds the run lock.
func (client *Client) platformDeadlines(consider func(time.Time)) {
	// The Host's own round paint is work the guest has waiting, so the frame it
	// is next due at is a deadline like any other: leaving it out reports a
	// title whose only work is that paint as having nothing left to do, and a
	// batch Host stops at the first gated frame.
	//
	// **Unlike a client wait it counts when it is already past**, because it
	// is the one deadline here that a round clears by itself: a batch Host
	// skips to a deadline and then asks again, so a paint reported only while
	// it is still ahead disappears exactly when it comes due. That is safe
	// only while the round paint could actually run, which is why it waits on
	// the client thread — reported through a client wait that has not elapsed,
	// it is a deadline level with the clock that no round can clear, and the
	// clock never moves again.
	if client.runtime != nil && len(client.runtime.displayCards) > 0 && client.ClientThreadDue() {
		consider(client.NextRoundPaint)
	}
	for _, worker := range client.workers {
		consider(worker.wakeAt)
	}
	if client.runtime == nil {
		return
	}
	client.runtime.mediaDeadlines(client.Now(), consider)
	// A guest thread that has been started but not yet adopted as a worker
	// is runnable right now, and the next round is what adopts it. Leaving
	// it out reports the game as idle until whatever else is parked comes
	// due, and a Host that believes that sleeps through work that was
	// ready — one title started a thread from its paint and then sat for
	// the hundred seconds its only other thread had asked to sleep. It is
	// due rather than parked, so the deadline is the past.
	if len(client.runtime.pendingThreads) > 0 {
		consider(time.Time{})
	}
	// A queued serial Runnable is due on the next idle pass, not now —
	// and never before the wait the client thread declared is over,
	// because that is the thread it runs on. Answering the earlier of the
	// two reported work as due at an instant the dispatch would refuse, so
	// a Host on a manual clock never advanced it and the wait never ended.
	if len(client.runtime.pendingSerial) > 0 {
		due := client.runtime.serialDueAt
		if client.ClientWakeAt.After(due) {
			due = client.ClientWakeAt
		}
		consider(due)
	}
}

// setRuntimes records the running and the prepared runtime, here and in the
// engine, which reads both.
func (client *Client) setRuntimes(runtime, prepared *initializationRuntime) {
	client.runtime, client.prepared = runtime, prepared
	client.Client.Runtime, client.Client.Prepared = runtime.engine(), prepared.engine()
}

func (runtime *initializationRuntime) engine() *wipicrt.Runtime {
	if runtime == nil {
		return nil
	}
	return runtime.Runtime
}

// The engine's names a KTF caller has always reached through this package.
const (
	ImageBase         = wipicrt.ImageBase
	DefaultTraceLimit = wipicrt.DefaultTraceLimit

	KeyPressed    = wipicrt.KeyPressed
	KeyReleased   = wipicrt.KeyReleased
	KeyRepeated   = wipicrt.KeyRepeated
	KeyUp         = wipicrt.KeyUp
	KeyDown       = wipicrt.KeyDown
	KeyLeft       = wipicrt.KeyLeft
	KeyRight      = wipicrt.KeyRight
	KeyFire       = wipicrt.KeyFire
	KeyLeftSoft   = wipicrt.KeyLeftSoft
	KeyRightSoft  = wipicrt.KeyRightSoft
	KeyThirdSoft  = wipicrt.KeyThirdSoft
	KeyCall       = wipicrt.KeyCall
	KeyHangup     = wipicrt.KeyHangup
	KeyVolumeUp   = wipicrt.KeyVolumeUp
	KeyVolumeDown = wipicrt.KeyVolumeDown
	KeyClear      = wipicrt.KeyClear
	KeyNum0       = wipicrt.KeyNum0
	KeyNum9       = wipicrt.KeyNum9
	KeyHash       = wipicrt.KeyHash
	KeyStar       = wipicrt.KeyStar
)

type (
	Diagnostics = wipicrt.Diagnostics
	ManualClock = wipicrt.ManualClock
	HostCosts   = wipicrt.HostCosts
)

// ErrServiceStepLimit and ErrServiceWaitLimit end a Host service call that
// spent its allowance; see the engine.
var (
	ErrServiceStepLimit = wipicrt.ErrServiceStepLimit
	ErrServiceWaitLimit = wipicrt.ErrServiceWaitLimit
)

// NewManualClock is a clock that moves only when the Host moves it.
func NewManualClock(start time.Time) *ManualClock { return wipicrt.NewManualClock(start) }

// NewDirectorySaveStore stores saves in a directory on disk.
func NewDirectorySaveStore(root string) *wipicrt.DirectorySaveStore {
	return wipicrt.NewDirectorySaveStore(root)
}

// KeyCodeByName is the key code a route or script names.
func KeyCodeByName(name string) (int32, bool) { return wipicrt.KeyCodeByName(name) }
