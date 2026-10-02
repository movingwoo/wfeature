package ktf

import (
	"encoding/binary"
	"encoding/json"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/movingwoo/wfeature/internal/armcore"
	"github.com/movingwoo/wfeature/internal/audio/smaf"
	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/jvm"
)

func captureClientForTest(t *testing.T, client *Client) clientState {
	t.Helper()
	client.run.Lock()
	defer client.run.Unlock()
	saved, err := client.captureClientState()
	if err != nil {
		t.Fatal(err)
	}
	return saved
}

func roundTripClientState(t *testing.T, saved clientState) clientState {
	t.Helper()
	data, err := backend.EncodeCheckpointRecord(saved)
	if err != nil {
		t.Fatal(err)
	}
	var decoded clientState
	if err := backend.DecodeCheckpointRecord(data, &decoded); err != nil {
		t.Fatal(err)
	}
	return decoded
}

func restoreClientForTest(t *testing.T, saved clientState, epoch int64, options continuationFixtureOptions) continuationFixture {
	t.Helper()
	fresh := newContinuationRestoreFixture(t, epoch, options)
	fresh.client.vibrator.SetClock(fresh.clock.Now)
	if err := fresh.client.restoreClientState(saved, armcore.CoreOptions{}, nil); err != nil {
		t.Fatal(err)
	}
	if len(fresh.client.workers) != 0 {
		fresh.object = fresh.client.workers[0].javaThread
	}
	return fresh
}

func TestClientCheckpointResumesWorkersAndLogicalDevices(t *testing.T) {
	options := continuationFixtureOptions{Runnable: true, Graphics: true, Storage: true, Platform: true, Relay: true}
	source := newContinuationFixture(t, 1700000000, options)
	worker := source.start(t)
	worker.publishedFrame, worker.paintedCard = true, source.object
	source.client.SetSpeed(1.25)
	source.clock.Advance(8 * time.Millisecond)
	source.client.clientWakeAt = source.clock.Now().Add(12 * time.Millisecond)
	source.client.lastPaint = source.clock.Now().Add(-7 * time.Millisecond)
	source.client.nextRoundPaint = source.clock.Now().Add(9 * time.Millisecond)
	source.client.paintLoad, source.client.paintCost, source.client.entryCost = 1.5, 3*time.Millisecond, 4*time.Millisecond
	source.client.workBaseline = source.client.core.Steps() - 1
	if err := source.runtime.convertScreen(); err != nil {
		t.Fatal(err)
	}
	source.client.frame[0], source.client.partialFrame = 17, true
	source.client.framePending, source.client.flushCount, source.client.forcedSlice = false, 19, true
	source.client.vibrator.SetClock(source.clock.Now)
	source.client.vibrator.Vibrate(60, 150)
	source.client.audio = backend.NewAudio(nil)
	handle, err := source.client.audio.LoadEvents([]smaf.Event{{Time: 0, Type: smaf.EventNoteOn, Channel: 0, Note: 60, Velocity: 100}, {Time: 100, Type: smaf.EventEnd}})
	if err != nil {
		t.Fatal(err)
	}
	source.client.audio.Play(handle, 0, true)
	source.client.audio.Advance(10 * time.Millisecond)
	source.client.audio.SetVolume(25)
	saved := roundTripClientState(t, captureClientForTest(t, source.client))
	if len(saved.Workers) != 1 || saved.Workers[0].Continuation == nil || saved.Workers[0].Wake.Remaining != 22*time.Millisecond {
		t.Fatal("worker continuation or relative wake was lost")
	}
	wantTime := source.runtime.guestElapsed()
	wantVibration := source.client.vibrator.State()
	source.client.StopThreads()
	fresh := restoreClientForTest(t, saved, 1800000000, options)
	if fresh.runtime.guestElapsed() != wantTime || fresh.client.Speed() != 1.25 || fresh.client.vibrator.State() != wantVibration {
		t.Fatal("restored clocks or vibration changed")
	}
	if fresh.client.workBaseline != saved.WorkBaseline || fresh.client.paintLoad != 1.5 || fresh.client.paintCost != 3*time.Millisecond || fresh.client.entryCost != 4*time.Millisecond || fresh.client.clientWakeAt.Sub(fresh.clock.Now()) != 12*time.Millisecond || fresh.client.lastPaint.Sub(fresh.clock.Now()) != -7*time.Millisecond || fresh.client.nextRoundPaint.Sub(fresh.clock.Now()) != 9*time.Millisecond {
		t.Fatal("restored pacing differs")
	}
	frame, width, height, flushes := fresh.client.Frame()
	if len(frame) == 0 || frame[0] != 17 || width != saved.FrameWidth || height != saved.FrameHeight || flushes != 19 || !fresh.client.partialFrame || !fresh.client.forcedSlice {
		t.Fatal("restoration exposed unflushed pixels or lost frame state")
	}
	if got, err := fresh.client.audio.CaptureState(); err != nil || !reflect.DeepEqual(got, *saved.Audio) {
		t.Fatalf("restored logical audio differs: %v", err)
	}
	if fresh.client.workers[0].paintedCard != fresh.object || !fresh.client.workers[0].publishedFrame {
		t.Fatal("worker paint ownership was lost")
	}
	fresh.checkGraphics(t)
	fresh.checkStorage(t)
	fresh.checkPlatformRoots(t)
	fresh.checkRelay(t)
	// A second save before the first restored grant must retain the dormant
	// continuation. It cannot classify it as a never-started Thread.run.
	recaptured := captureClientForTest(t, fresh.client)
	if !reflect.DeepEqual(recaptured.Workers, saved.Workers) {
		t.Fatal("recapture before first restored grant changed the continuation")
	}
	fresh.clock.Advance(21 * time.Millisecond)
	if ran, err := fresh.client.ServiceThreads(t.Context(), 1); err != nil || ran != 0 {
		t.Fatalf("early worker grant = %d, %v", ran, err)
	}
	fresh.clock.Advance(time.Millisecond)
	if ran, err := fresh.client.ServiceThreads(t.Context(), 1); err != nil || ran != 1 {
		t.Fatalf("resumed worker = %d, %v", ran, err)
	}
	probe := readTestBytes(t, fresh.client, source.probe, 16)
	if binary.LittleEndian.Uint32(probe) != 1 || binary.LittleEndian.Uint32(probe[8:]) != 1 || binary.LittleEndian.Uint32(probe[12:]) != 1 {
		t.Fatalf("worker prefix repeated or wait was lost: %x", probe)
	}
	savedAgain := roundTripClientState(t, captureClientForTest(t, fresh.client))
	fresh.client.StopThreads()
	last := restoreClientForTest(t, savedAgain, 1900000000, options)
	last.clock.Advance(24 * time.Millisecond)
	if ran, err := last.client.ServiceThreads(t.Context(), 1); err != nil || ran != 1 || len(last.client.workers) != 0 {
		t.Fatalf("worker completion = %d, %v", ran, err)
	}
	probe = readTestBytes(t, last.client, source.probe, 16)
	if binary.LittleEndian.Uint32(probe[4:]) != 42 || binary.LittleEndian.Uint32(probe[12:]) != 2 {
		t.Fatalf("restored result or second wait differs: %x", probe)
	}
	if len(last.runtime.nativeReturnScopes) != 0 || len(last.runtime.aotCallDepth) != 0 {
		t.Fatal("completed client kept active call ownership")
	}
}

func TestClientActivationExcludesDetachedPreparationTime(t *testing.T) {
	options := continuationFixtureOptions{Runnable: true, Platform: true}
	source := newContinuationFixture(t, 1700000000, options)
	source.start(t)
	source.client.SetSpeed(1.25)
	source.clock.Advance(8 * time.Millisecond)
	source.client.clientWakeAt = source.clock.Now().Add(12 * time.Millisecond)
	source.client.lastPaint = source.clock.Now().Add(-7 * time.Millisecond)
	source.client.nextRoundPaint = source.clock.Now().Add(9 * time.Millisecond)
	source.client.vibrator.SetClock(source.clock.Now)
	source.client.vibrator.Vibrate(60, 150)
	saved := roundTripClientState(t, captureClientForTest(t, source.client))
	source.client.StopThreads()
	fresh := newContinuationRestoreFixture(t, 1800000000, options)
	fresh.client.vibrator.SetClock(fresh.clock.Now)
	var activation clientActivation
	if err := fresh.client.restoreClientStateForActivation(saved, armcore.CoreOptions{}, nil, &activation); err != nil {
		t.Fatal(err)
	}
	// Parsing and the durable save swap may take much longer than a wait or
	// multi-tap cycle. None of that interval belongs to the restored guest.
	fresh.clock.Advance(10 * time.Second)
	activation.activate()
	recaptured := captureClientForTest(t, fresh.client)
	if !reflect.DeepEqual(recaptured.Workers, saved.Workers) ||
		!reflect.DeepEqual(recaptured.Heap.Editors, saved.Heap.Editors) ||
		!reflect.DeepEqual(recaptured.Heap.Roots, saved.Heap.Roots) ||
		recaptured.Heap.Control.ClockAge != saved.Heap.Control.ClockAge ||
		recaptured.ClientWake != saved.ClientWake || recaptured.LastPaint != saved.LastPaint ||
		recaptured.NextRoundPaint != saved.NextRoundPaint || recaptured.Vibration != saved.Vibration {
		t.Fatal("detached preparation consumed guest time")
	}
	fresh.clock.Advance(21 * time.Millisecond)
	activation.activate() // Accidental repeated adoption must not move time again.
	if ran, err := fresh.client.ServiceThreads(t.Context(), 1); err != nil || ran != 0 {
		t.Fatalf("early worker grant = %d, %v", ran, err)
	}
	fresh.clock.Advance(time.Millisecond)
	if ran, err := fresh.client.ServiceThreads(t.Context(), 1); err != nil || ran != 1 {
		t.Fatalf("resumed worker = %d, %v", ran, err)
	}
}

func TestClientCheckpointKeepsInitialGrantAndStackReuseOrder(t *testing.T) {
	source := newContinuationFixture(t, 1700000000, continuationFixtureOptions{})
	owner := &jvm.Object{ClassName: "java/util/Timer"}
	if err := source.client.startTimerTask(owner, source.object); err != nil {
		t.Fatal(err)
	}
	first := source.client.workers[0]
	first.paintedCard = source.object
	// Finished workers return their stacks in this LIFO order. Construct two
	// extra initial workers and retire them without invoking guest code.
	var retired []*guestWorker
	for i := 0; i < 2; i++ {
		worker, err := source.client.newGuestWorker(source.object)
		if err != nil {
			t.Fatal(err)
		}
		close(worker.grant)
		source.client.waitForWorker(worker)
		retired = append(retired, worker)
	}
	for _, worker := range retired {
		source.client.freeWorkerStacks = append(source.client.freeWorkerStacks, worker.stackBase)
	}
	saved := roundTripClientState(t, captureClientForTest(t, source.client))
	if saved.Workers[0].Continuation != nil || first.started {
		t.Fatal("worker ran before its first grant")
	}
	source.client.StopThreads()
	fresh := restoreClientForTest(t, saved, 1800000000, continuationFixtureOptions{})
	restored := fresh.client.workers[0]
	if restored.timerOwner == nil || fresh.client.runningTimerTasks[restored.timerOwner] != restored || restored.paintedCard != restored.javaThread {
		t.Fatal("initial Timer worker ownership differs")
	}
	for i := 1; i >= 0; i-- {
		worker, err := fresh.client.newGuestWorker(fresh.object)
		if err != nil {
			t.Fatal(err)
		}
		fresh.client.workers = append(fresh.client.workers, worker)
		if worker.stackBase != saved.FreeWorkerStacks[i] {
			t.Fatal("worker stack reuse order changed")
		}
	}
	if ran, err := fresh.client.ServiceThreads(t.Context(), 1); err != nil || ran != 1 || !restored.started {
		t.Fatalf("initial grant = %d, %v", ran, err)
	}
	if got := binary.LittleEndian.Uint32(readTestBytes(t, fresh.client, source.probe, 4)); got != 1 {
		t.Fatalf("initial worker prefix count = %d", got)
	}
}

func TestClientCheckpointRejectsInvalidStateBeforeLaunchingWorkers(t *testing.T) {
	source := newContinuationFixture(t, 1700000000, continuationFixtureOptions{})
	source.start(t)
	saved := captureClientForTest(t, source.client)
	for _, test := range []struct {
		name string
		edit func(*clientState)
	}{
		{"version", func(s *clientState) { s.Version++ }},
		{"image", func(s *clientState) { s.ImageHash[0]++ }},
		{"frame", func(s *clientState) { s.Frame = []byte{1} }},
		{"speed", func(s *clientState) { s.Speed = math.NaN() }},
		{"policy", func(s *clientState) { s.ThreadSliceSteps++ }},
		{"baseline", func(s *clientState) { s.WorkBaseline = s.Core.Steps + 1 }},
		{"root budget", func(s *clientState) { s.Thread.StepBudget = 1 }},
		{"stack partition", func(s *clientState) { s.WorkerStackCount++ }},
		{"stack alias", func(s *clientState) { s.WorkerStackCount++; s.FreeWorkerStacks = []uint32{s.Workers[0].StackBase} }},
		{"stack pointer", func(s *clientState) { s.Workers[0].Root.Context.Registers[armcore.RegisterSP] = ThreadStackBase }},
		{"call budget", func(s *clientState) { s.Workers[0].Continuation.Calls[0].ARM.Budget++ }},
		{"execution ID", func(s *clientState) { s.Workers[0].Continuation.Execution.ID = s.Heap.JVM.NextExecution + 1 }},
		{"JVM policy", func(s *clientState) { s.Workers[0].Continuation.Execution.MaxSteps++ }},
		{"method", func(s *clientState) { s.Workers[0].Continuation.Method.Body += 4 }},
		{"roots", func(s *clientState) { s.Heap.Roots.CallerCount-- }},
		{"vibration", func(s *clientState) { s.Vibration.Level = 101 }},
		{"audio catchup", func(s *clientState) {
			s.Heap.Control.ClockAge = 365 * 24 * time.Hour
			s.Audio = &backend.AudioState{Version: 1, MaxSounds: 256, Next: 1, Volume: 100, Sounds: []backend.AudioSoundState{{Handle: 1, Playing: true, Repeat: true, Length: time.Millisecond, Events: []smaf.Event{{Time: 1, Type: smaf.EventEnd}}}}}
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			candidate := roundTripClientState(t, saved)
			test.edit(&candidate)
			fresh := newContinuationRestoreFixture(t, 1800000000, continuationFixtureOptions{})
			if err := fresh.client.restoreClientState(candidate, armcore.CoreOptions{}, nil); err == nil {
				t.Fatal("accepted invalid client checkpoint")
			}
			if len(fresh.client.workers) != 0 {
				t.Fatal("invalid state launched workers")
			}
		})
	}
	// Every rejection leaves the original parked execution usable.
	source.clock.Advance(30 * time.Millisecond)
	if ran, err := source.client.ServiceThreads(t.Context(), 1); err != nil || ran != 1 {
		t.Fatalf("source after rejected loads = %d, %v", ran, err)
	}
}

func TestClientCheckpointMultipleParkedWorkers(t *testing.T) {
	source := newContinuationFixture(t, 1700000000, continuationFixtureOptions{})
	first := source.start(t)
	secondObject := &jvm.Object{ClassName: source.object.ClassName}
	second, err := source.client.newGuestWorker(secondObject)
	if err != nil {
		t.Fatal(err)
	}
	source.client.workers = append(source.client.workers, second)
	if ran, err := source.client.ServiceThreads(t.Context(), 1); err != nil || ran != 1 {
		t.Fatalf("second worker park = %d, %v", ran, err)
	}
	if !first.started || !second.started {
		t.Fatal("both workers must be parked after their first grant")
	}
	saved := roundTripClientState(t, captureClientForTest(t, source.client))
	if saved.Workers[0].Continuation.Execution.ID == saved.Workers[1].Continuation.Execution.ID {
		t.Fatal("worker execution owners collide")
	}
	source.client.StopThreads()
	fresh := restoreClientForTest(t, saved, 1800000000, continuationFixtureOptions{})
	for round := 0; round < 2; round++ {
		fresh.clock.Advance(30 * time.Millisecond)
		if ran, err := fresh.client.ServiceThreads(t.Context(), 2); err != nil || ran != 2 {
			t.Fatalf("restored worker round = %d, %v", ran, err)
		}
	}
	probe := readTestBytes(t, fresh.client, source.probe, 16)
	if len(fresh.client.workers) != 0 || len(fresh.client.freeWorkerStacks) != 2 || binary.LittleEndian.Uint32(probe) != 2 || binary.LittleEndian.Uint32(probe[8:]) != 2 || binary.LittleEndian.Uint32(probe[12:]) != 4 || len(fresh.runtime.nativeReturnScopes) != 0 || len(fresh.runtime.aotCallDepth) != 0 {
		t.Fatalf("multiple-worker continuation or cleanup differs: %x", probe)
	}
}

func TestClientCheckpointSubprocess(t *testing.T) {
	if path := os.Getenv("WFEATURE_CLIENT_CHECKPOINT_FIXTURE"); path != "" {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var record struct {
			State clientState
			Probe uint32
		}
		if err := json.Unmarshal(data, &record); err != nil {
			t.Fatal(err)
		}
		fresh := restoreClientForTest(t, record.State, 1900000000, continuationFixtureOptions{Runnable: true})
		for i := 0; i < 2; i++ {
			fresh.clock.Advance(30 * time.Millisecond)
			if ran, err := fresh.client.ServiceThreads(t.Context(), 1); err != nil || ran != 1 {
				t.Fatalf("restored process slice = %d, %v", ran, err)
			}
		}
		probe := readTestBytes(t, fresh.client, record.Probe, 16)
		if binary.LittleEndian.Uint32(probe) != 1 || binary.LittleEndian.Uint32(probe[4:]) != 42 || binary.LittleEndian.Uint32(probe[12:]) != 2 || len(fresh.client.workers) != 0 {
			t.Fatalf("restored process continuation differs: %x", probe)
		}
		return
	}
	source := newContinuationFixture(t, 1700000000, continuationFixtureOptions{Runnable: true})
	source.start(t)
	saved := captureClientForTest(t, source.client)
	source.client.StopThreads()
	data, err := json.Marshal(struct {
		State clientState
		Probe uint32
	}{saved, source.probe})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "client.json")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	binary := os.Getenv("WFEATURE_CALL_CHECKPOINT_RESTORE_BINARY")
	if binary == "" {
		binary = os.Args[0]
	}
	process := exec.CommandContext(t.Context(), binary, "-test.run=^TestClientCheckpointSubprocess$", "-test.timeout=20s")
	process.Env = append(os.Environ(), "WFEATURE_CLIENT_CHECKPOINT_FIXTURE="+path)
	if output, err := process.CombinedOutput(); err != nil {
		t.Fatalf("restore client process: %v\n%s", err, output)
	}
}
