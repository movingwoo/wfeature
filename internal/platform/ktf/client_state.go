package ktf

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"math"
	"slices"
	"time"

	"github.com/movingwoo/wfeature/internal/armcore"
	"github.com/movingwoo/wfeature/internal/backend"
)

// clientState joins the execution, heap, platform and logical device records.
// Immutable archive resources, external saves and Host output are owned by the
// enclosing session checkpoint. A caller must serialize whole service rounds,
// input and clock changes as well as holding client.run during capture.
type clientState struct {
	Version                                 uint32
	ImageHash                               [32]byte
	ImageName                               []byte
	ImageBSS                                uint32
	Core                                    armcore.CoreState
	Thread                                  armcore.RootThreadState
	Metadata                                runtimeMetadataState
	Heap                                    runtimeHeapState
	Workers                                 []guestWorkerState
	WorkerStackCount                        int
	FreeWorkerStacks                        []uint32
	Initialized, Prepared                   bool
	InitParameters                          []uint32
	Argument                                uint32
	Executable                              Executable
	Subscriber                              string
	Authentication                          backend.AuthenticationStatus
	Frame                                   []byte
	FrameWidth, FrameHeight                 int
	ScreenWidth, ScreenHeight               int
	FlushCount                              uint32
	FramePending, PartialFrame, ForcedSlice bool
	Speed                                   float64
	ClientWake, LastPaint, NextRoundPaint   heapDeadline
	PaintLoad                               float64
	PaintCost, EntryCost                    time.Duration
	WorkBaseline                            uint64
	ThreadSliceSteps                        uint64
	ServiceSteps                            uint64
	ServiceWait                             time.Duration
	Audio                                   *backend.AudioState
	Vibration                               backend.VibrationState
}

func (client *Client) captureClientState() (clientState, error) {
	return client.captureClientStateAt(client.now())
}

func (client *Client) captureClientStateAt(now time.Time) (clientState, error) {
	if client == nil || client.core == nil || client.thread == nil || client.runtime == nil || client.workersStopped || client.activeWorker != nil || client.serviceDepth != 0 || client.activeTimer != nil || !client.servingDue.IsZero() || client.skipPaint {
		return clientState{}, fmt.Errorf("KTF client capture requires an idle, running session")
	}
	if client.prepared != nil && client.prepared != client.runtime {
		return clientState{}, fmt.Errorf("KTF client initialization is incomplete")
	}
	// Every guest-clock offset, including editor cycles and worker waits, uses
	// this instant. Copying a large heap must not shorten the saved waits.
	saved := clientState{Version: 1, ImageHash: sha256.Sum256(client.image.Data), ImageName: []byte(client.image.Name), ImageBSS: client.image.BSSSize,
		WorkerStackCount: client.workerStackCount, FreeWorkerStacks: slices.Clone(client.freeWorkerStacks),
		Initialized: client.initializationStarted, Prepared: client.prepared != nil, InitParameters: slices.Clone(client.initParameters),
		Argument: client.argument, Executable: client.executable, Subscriber: client.subscriberNumber, Authentication: client.authentication,
		Frame: bytes.Clone(client.frame), FrameWidth: client.frameWidth, FrameHeight: client.frameHeight,
		ScreenWidth: client.screenWidth, ScreenHeight: client.screenHeight, FlushCount: client.flushCount,
		FramePending: client.framePending, PartialFrame: client.partialFrame, ForcedSlice: client.forcedSlice,
		Speed: client.speedOrDefault(), ClientWake: captureHeapDeadline(client.clientWakeAt, now), LastPaint: captureHeapDeadline(client.lastPaint, now),
		NextRoundPaint: captureHeapDeadline(client.nextRoundPaint, now), PaintLoad: client.paintLoad, PaintCost: client.paintCost, EntryCost: client.entryCost,
		WorkBaseline: client.workBaseline, ThreadSliceSteps: client.threadSliceSteps, ServiceSteps: client.serviceSteps, ServiceWait: client.serviceWait}
	workers, roots, err := client.captureWorkerStates(now)
	if err != nil {
		return clientState{}, err
	}
	saved.Workers = workers
	saved.Vibration, err = client.vibrator.CaptureState()
	if err != nil {
		return clientState{}, err
	}
	saved.Thread, err = client.core.CaptureRootThread(client.thread)
	if err != nil {
		return clientState{}, err
	}
	saved.Metadata, err = client.runtime.captureMetadataState()
	if err != nil {
		return clientState{}, err
	}
	saved.Heap, err = client.runtime.captureHeapStateAt(roots, now)
	if err != nil {
		return clientState{}, err
	}
	saved.Core, err = client.core.CaptureState()
	if err != nil {
		return clientState{}, err
	}
	if client.audio != nil {
		audio, err := client.audio.CaptureStateAt(now)
		if err != nil {
			return clientState{}, err
		}
		saved.Audio = &audio
	}
	if err := client.validateClientState(saved); err != nil {
		return clientState{}, err
	}
	return saved, nil
}

func (client *Client) validateClientState(saved clientState) error {
	if saved.Version != 1 || !bytes.Equal(saved.ImageName, []byte(client.image.Name)) || saved.ImageBSS != client.image.BSSSize || saved.ImageHash != sha256.Sum256(client.image.Data) {
		return fmt.Errorf("KTF client checkpoint belongs to another image or version")
	}
	if len(saved.Workers) > maxGuestWorkers || saved.WorkerStackCount < 0 || saved.WorkerStackCount > maxGuestWorkers || len(saved.FreeWorkerStacks) > maxGuestWorkers || len(saved.InitParameters) != 0 && len(saved.InitParameters) != 5 || !saved.Prepared && len(saved.InitParameters) != 0 {
		return fmt.Errorf("KTF client tables exceed limits or initialization is inconsistent")
	}
	if saved.Thread.StepBudget != 0 || !stackContains(ThreadStackBase, saved.Thread.Context.Registers[armcore.RegisterSP]) || saved.WorkBaseline > saved.Core.Steps {
		return fmt.Errorf("KTF client root thread or instruction baseline is invalid")
	}
	if saved.ThreadSliceSteps != client.threadSliceSteps || saved.ServiceSteps != client.serviceSteps || saved.ServiceWait != client.serviceWait {
		return fmt.Errorf("KTF client service policy is incompatible")
	}
	if len(saved.Subscriber) > 64 || !bytes.Equal([]byte(saved.Subscriber), bytes.ToValidUTF8([]byte(saved.Subscriber), nil)) {
		return fmt.Errorf("KTF client subscriber is invalid")
	}
	switch saved.Authentication {
	case "", backend.AuthenticationOff, backend.AuthenticationUnsupported, backend.AuthenticationKTFCertificate23, backend.AuthenticationKTFCertificate52, backend.AuthenticationKTFSubscriber:
	default:
		return fmt.Errorf("KTF client authentication state is invalid")
	}
	if saved.ScreenWidth < 0 || saved.ScreenHeight < 0 || saved.ScreenWidth > maxWIPICFramebufferSide || saved.ScreenHeight > maxWIPICFramebufferSide || (saved.ScreenWidth == 0) != (saved.ScreenHeight == 0) {
		return fmt.Errorf("KTF client screen shape is invalid")
	}
	if len(saved.Frame) == 0 {
		if saved.FrameWidth != 0 || saved.FrameHeight != 0 {
			return fmt.Errorf("KTF client retained screen is missing")
		}
	} else if !validHeapImageSize(saved.FrameWidth, saved.FrameHeight) || uint64(len(saved.Frame)) != uint64(saved.FrameWidth)*uint64(saved.FrameHeight)*4 {
		return fmt.Errorf("KTF client retained screen shape is invalid")
	}
	if math.IsNaN(saved.Speed) || math.IsInf(saved.Speed, 0) || saved.Speed < 0.1 || saved.Speed > 16 || math.IsNaN(saved.PaintLoad) || math.IsInf(saved.PaintLoad, 0) || saved.PaintLoad < 0 || saved.PaintCost < 0 || saved.EntryCost < 0 {
		return fmt.Errorf("KTF client pacing state is invalid")
	}
	for _, deadline := range []heapDeadline{saved.ClientWake, saved.LastPaint, saved.NextRoundPaint} {
		if !deadline.Set && deadline.Remaining != 0 {
			return fmt.Errorf("KTF client deadline is invalid")
		}
	}
	if err := saved.Vibration.Validate(); err != nil {
		return err
	}
	return validateClientAudio(saved)
}

// Reject a corrupted repeat origin before Audio.Advance could spend unbounded
// time replaying cycles. Ordinary pending events remain pending; capture never
// emits, stops or advances a sound to make a record acceptable.
func validateClientAudio(saved clientState) error {
	elapsed := float64(max(time.Duration(0), saved.Heap.Control.ClockAge)) * saved.Speed
	if elapsed >= float64(math.MaxInt64) {
		return fmt.Errorf("KTF guest clock exceeds the audio duration range")
	}
	handles := make(map[backend.AudioHandle]bool)
	const maxPendingAudioEvents = 1 << 20
	work := uint64(0)
	if saved.Audio != nil {
		if len(saved.Audio.Sounds) > 256 {
			return fmt.Errorf("KTF audio sound count exceeds limit")
		}
		for _, sound := range saved.Audio.Sounds {
			handles[sound.Handle] = true
			if !sound.Playing {
				continue
			}
			if sound.StartedAt < 0 || sound.Length < 0 {
				return fmt.Errorf("KTF audio has a negative playback origin or length")
			}
			cycles := uint64(1)
			if sound.Repeat && sound.Length > 0 && time.Duration(elapsed) > sound.StartedAt {
				cycles += uint64((time.Duration(elapsed) - sound.StartedAt) / sound.Length)
			}
			if cycles > maxPendingAudioEvents || uint64(len(sound.Events)) > (maxPendingAudioEvents-work)/cycles {
				return fmt.Errorf("KTF audio catch-up exceeds its event limit")
			}
			work += cycles * uint64(len(sound.Events))
		}
	}
	for _, clip := range saved.Heap.Roots.Clips {
		if clip.Loaded && !handles[clip.Handle] {
			return fmt.Errorf("KTF Java clip has no loaded audio handle")
		}
	}
	for _, clip := range saved.Heap.Control.Clips {
		if clip.Loaded && !handles[clip.Handle] {
			return fmt.Errorf("KTF C clip has no loaded audio handle")
		}
	}
	return nil
}

// restoreClientState is for a fresh, detached LoadClient with registered native
// implementations. The caller discards it on any error. No guest startup,
// callback, output or save write runs, and no worker starts before validation.
func (client *Client) restoreClientState(saved clientState, limits armcore.CoreOptions, sink backend.AudioSink) error {
	return client.restoreClientStateForActivation(saved, limits, sink, nil)
}

func (client *Client) restoreClientStateForActivation(saved clientState, limits armcore.CoreOptions, sink backend.AudioSink, activation *clientActivation) error {
	if client == nil || client.runtime == nil || client.core.Steps() != 0 || client.prepared != nil || len(client.workers) != 0 || client.initializationStarted {
		return fmt.Errorf("KTF client restoration requires a fresh detached runtime")
	}
	if err := client.validateClientState(saved); err != nil {
		return err
	}
	core, err := armcore.NewCoreFromState(saved.Core, limits)
	if err != nil {
		return err
	}
	thread, err := core.RestoreRootThread(saved.Thread, 0)
	if err != nil {
		return err
	}
	client.core, client.thread = core, thread
	if err := client.validateWorkerStacks(saved.Workers, saved.WorkerStackCount, saved.FreeWorkerStacks); err != nil {
		return err
	}
	if saved.Executable != (Executable{}) {
		executable, err := client.readExecutable(saved.Executable.Address)
		if err != nil {
			return err
		}
		if executable != saved.Executable {
			return fmt.Errorf("KTF executable descriptor differs from restored memory")
		}
	}
	if err := client.runtime.restoreMetadataState(saved.Metadata); err != nil {
		return err
	}
	var audio *backend.Audio
	if saved.Audio != nil {
		audio, err = backend.NewAudioFromStateWithClock(*saved.Audio, sink, client.now)
		if err != nil {
			return err
		}
	}
	now := client.now()
	heapContext := &heapNativeContext{runtime: client.runtime, now: now}
	roots, err := client.runtime.restoreHeapWithContext(saved.Heap, heapContext)
	if err != nil {
		return err
	}
	workers, timers, err := client.decodeWorkerStates(saved.Workers, roots, now, saved.Heap.JVM.NextExecution)
	if err != nil {
		return err
	}
	if err := client.vibrator.RestoreState(saved.Vibration); err != nil {
		return err
	}
	client.initializationStarted = saved.Initialized
	if saved.Prepared {
		client.prepared = client.runtime
	}
	client.initParameters = slices.Clone(saved.InitParameters)
	client.argument, client.executable = saved.Argument, saved.Executable
	client.subscriberNumber, client.authentication = saved.Subscriber, saved.Authentication
	client.frame, client.frameWidth, client.frameHeight = bytes.Clone(saved.Frame), saved.FrameWidth, saved.FrameHeight
	client.screenWidth, client.screenHeight, client.flushCount = saved.ScreenWidth, saved.ScreenHeight, saved.FlushCount
	client.framePending, client.partialFrame, client.forcedSlice = saved.FramePending, saved.PartialFrame, saved.ForcedSlice
	client.speed, client.clientWakeAt = saved.Speed, saved.ClientWake.restore(now)
	client.lastPaint, client.nextRoundPaint = saved.LastPaint.restore(now), saved.NextRoundPaint.restore(now)
	client.paintLoad, client.paintCost, client.entryCost, client.workBaseline = saved.PaintLoad, saved.PaintCost, saved.EntryCost, saved.WorkBaseline
	client.audio = audio
	client.workerStackCount, client.freeWorkerStacks = saved.WorkerStackCount, slices.Clone(saved.FreeWorkerStacks)
	client.workers, client.runningTimerTasks = workers, timers
	// Frame sampling and diagnostic caches start empty. The retained LCD above
	// remains exact even when the back buffer contains unflushed drawing.
	client.frameSampleAt, client.lastSample = time.Time{}, nil
	if activation != nil {
		*activation = clientActivation{client: client, constructed: now, editors: heapContext.restoredEditors, vibration: saved.Vibration}
	}
	for _, worker := range workers {
		go worker.run(client)
	}
	return nil
}
