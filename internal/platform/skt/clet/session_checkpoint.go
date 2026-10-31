package clet

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/movingwoo/wfeature/internal/armcore"
	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/platform/wipicrt"
)

// Quick save and quick load
//
// A checkpoint is the program's execution state and nothing else. It holds no
// save, and taking or loading one never reverts or removes a save: the
// restored program reads its files as the store has them when the load
// commits, and whatever it writes afterwards lands on top of them. What makes
// that hold is that a file has one home. The file table stores every write
// before the call that made it returns, so a record never has to carry one
// and a load never has an older copy of a file to put back. See
// checkpoint_storage.go.
//
// A Clet is called and returns, so a checkpoint is taken between two whole
// service rounds, where nothing of it is running, and a load runs none of it:
// not the start entry, not destroy on the session it displaces. The record is
// checkpoint_state.go.

// ErrCheckpointBusy reports a capture or a load that met the program inside a
// call. The Host retries at the next boundary.
var ErrCheckpointBusy = errors.New("SKT Clet checkpoint requires an idle session boundary")

// CanCheckpoint reports that a Clet has a quick-save codec. A capture can
// still refuse a session that is mid-call, is entering text, has no save
// store, or holds more than a record may.
func (session *Session) CanCheckpoint() bool {
	return session != nil && session.Client != nil && session.Client.IsClet()
}

func checkpointTimerLimit(options SessionOptions) int {
	if options.TimerLimit <= 0 {
		return wipicrt.SessionDefaultTimerLimit
	}
	return options.TimerLimit
}

// CaptureCheckpoint must run between whole service rounds on the Host's
// session goroutine, serialized with keys, clocks, lifecycle and save imports.
// It runs no guest code and makes no save store call.
func (session *Session) CaptureCheckpoint(ctx context.Context) (backend.Checkpoint, error) {
	return session.CaptureCheckpointWithSession(ctx, nil)
}

// CaptureCheckpointWithSession obtains the shared session record at the same
// instant as the program's clock. captureSession must only encode state that
// is already parked; it may not run the guest, send input or move a clock.
func (session *Session) CaptureCheckpointWithSession(ctx context.Context, captureSession func() ([]byte, error)) (backend.Checkpoint, error) {
	if err := ctx.Err(); err != nil {
		return backend.Checkpoint{}, err
	}
	if session == nil || session.Client == nil || session.archiveIdentity == ([32]byte{}) {
		return backend.Checkpoint{}, fmt.Errorf("SKT Clet checkpoint session is not started")
	}
	client := session.Client
	if !client.Run.TryLock() {
		return backend.Checkpoint{}, ErrCheckpointBusy
	}
	defer client.Run.Unlock()
	// A slot carries no save, so a load has to find the saves in a store. A
	// session that has none could be captured and never loaded.
	if client.SaveStore == nil {
		return backend.Checkpoint{}, fmt.Errorf("SKT Clet checkpoint requires a save store")
	}
	if err := client.checkpointIdle(); err != nil {
		return backend.Checkpoint{}, err
	}
	now := client.Now()
	var shared []byte
	if captureSession != nil {
		var err error
		if shared, err = captureSession(); err != nil {
			return backend.Checkpoint{}, err
		}
	}
	saved, runtime, storage, err := client.captureCheckpointState(now)
	if err != nil {
		return backend.Checkpoint{}, err
	}
	record, err := backend.EncodeCheckpointRecord(checkpointState{Version: checkpointVersion, TimerLimit: checkpointTimerLimit(session.options),
		DisableAuthentication: session.options.DisableAuthentication, Client: saved, Runtime: runtime, Storage: storage})
	if err != nil {
		return backend.Checkpoint{}, err
	}
	if err := ctx.Err(); err != nil {
		return backend.Checkpoint{}, err
	}
	return backend.Checkpoint{Identity: session.archiveIdentity, Variant: backend.CheckpointSKTClet, Session: shared, Runtime: record}, nil
}

// PreparedSession owns a fully validated, dormant replacement. It was built on
// a placeholder store that answers nothing, its open files have no bytes yet,
// and it is cut off from every output until Commit. The caller must Discard it
// when abandoning a load; Discard after a successful Commit is harmless.
type PreparedSession struct {
	session     *Session
	storage     *restoredStorage
	vibration   backend.VibrationState
	placeholder *backend.DetachedSaveStore
	// constructed is the instant the record's deadlines were laid against;
	// Commit moves them by the time that has passed since.
	constructed time.Time
}

// PreparationStoreCalls reports how many save store calls validation made
// against its placeholder, and the first of them. Validation is meant to make
// none: the saves are read when the load commits, from the live store.
func (prepared *PreparedSession) PreparationStoreCalls() (int, string) {
	if prepared == nil || prepared.placeholder == nil {
		return 0, ""
	}
	return prepared.placeholder.Calls(), prepared.placeholder.FirstCall()
}

// Speed exposes the validated pace so the shared session can check its own
// record against it before the load commits.
func (prepared *PreparedSession) Speed() float64 {
	if prepared == nil || prepared.session == nil {
		return 0
	}
	return prepared.session.Speed()
}

// PrepareCheckpoint rebuilds a Clet session from a checkpoint without running
// any of it. program is the Clet the archive lays out, under the same
// authentication setting the session is started with. It reads no save:
// options.SaveStore and options.SaveRoot are ignored, and the live store
// reaches the restored runtime only as an argument of Commit.
func PrepareCheckpoint(program CletProgram, checkpoint backend.Checkpoint, options SessionOptions) (*PreparedSession, error) {
	if checkpoint.Identity != program.Identity {
		return nil, backend.ErrCheckpointIdentity
	}
	if checkpoint.Variant != backend.CheckpointSKTClet {
		return nil, backend.ErrCheckpointVersion
	}
	var saved checkpointState
	if err := backend.DecodeCheckpointRecord(checkpoint.Runtime, &saved); err != nil {
		return nil, err
	}
	if saved.Version != checkpointVersion {
		return nil, fmt.Errorf("SKT Clet checkpoint record is version %d and this build reads %d: %w", saved.Version, checkpointVersion, backend.ErrCheckpointVersion)
	}
	if saved.TimerLimit != checkpointTimerLimit(options) || saved.DisableAuthentication != options.DisableAuthentication {
		return nil, fmt.Errorf("SKT Clet checkpoint was taken under another timer limit or authentication setting: %w", backend.ErrCheckpointVersion)
	}
	prepared := &PreparedSession{placeholder: backend.NewDetachedSaveStore(), vibration: saved.Client.Vibration}
	detached := options
	detached.SaveStore, detached.SaveRoot = prepared.placeholder, ""
	detached.AudioSink, detached.FrameSink, detached.Debug = nil, backend.FrameSink{}, nil
	client, err := newCletSessionClient(program, detached)
	if err != nil {
		return nil, err
	}
	if err := saved.Client.validate(client, options); err != nil {
		return nil, err
	}
	if saved.Client.Audio == nil {
		return nil, fmt.Errorf("SKT Clet checkpoint has no audio record")
	}
	// The guest clock is what the audio catches up over, so a record whose
	// clock or repeat origin could make that unbounded is refused before any
	// of it is built.
	elapsed := float64(max(saved.Runtime.ClockAge, 0)) * saved.Client.Speed
	if elapsed >= float64(maxDeadlineOffset) {
		return nil, fmt.Errorf("SKT Clet guest clock exceeds the audio duration range")
	}
	if err := backend.ValidateAudioCatchup(*saved.Client.Audio, time.Duration(elapsed)); err != nil {
		return nil, fmt.Errorf("SKT Clet checkpoint: %w", err)
	}
	core, err := armcore.NewCoreFromState(saved.Client.Core, sessionCoreOptions(options))
	if err != nil {
		return nil, err
	}
	thread, err := core.RestoreRootThread(saved.Client.Thread, 0)
	if err != nil {
		return nil, err
	}
	client.AttachCore(core, thread)
	if options.Debug != nil {
		options.Debug(core)
	}
	constructed := client.Now()
	client.SpeedMultiplier = saved.Client.Speed
	runtime, err := restoreRuntimeState(client, saved.Runtime, saved.Client.Audio, constructed)
	if err != nil {
		return nil, err
	}
	storage, err := runtime.restoreStorageState(saved.Storage)
	if err != nil {
		return nil, err
	}
	audio, err := backend.NewAudioFromStateWithClock(*saved.Client.Audio, nil, client.Now)
	if err != nil {
		return nil, err
	}
	audio.SetLogger(options.Logger)
	if err := audio.RebasePlaybackClock(time.Duration(elapsed), saved.Client.Speed); err != nil {
		return nil, err
	}
	if err := client.Vibrator.RestoreState(saved.Client.Vibration); err != nil {
		return nil, err
	}
	state := saved.Client
	client.setRuntimes(runtime, runtime)
	client.initializationStarted = true
	client.clet.root, client.clet.repaint = state.Root, state.Repaint
	client.clet.timeRecord, client.clet.tokenNext = state.TimeRecord, state.TokenNext
	client.SubscriberNumber = state.Subscriber
	client.FrameRGBA, client.FrameWidth, client.FrameHeight = state.Frame, state.FrameWidth, state.FrameHeight
	client.ScreenWidth, client.ScreenHeight, client.FlushCount = state.ScreenWidth, state.ScreenHeight, state.FlushCount
	client.FramePending, client.PartialFrame = state.FramePending, state.PartialFrame
	client.ClientWakeAt, client.NextRoundPaint = state.ClientWake.restore(constructed), state.NextRoundPaint.restore(constructed)
	client.PaintLoad, client.PaintCost, client.EntryCost, client.WorkBaseline = state.PaintLoad, state.PaintCost, state.EntryCost, state.WorkBaseline
	client.Audio = audio
	prepared.session = &Session{Client: client, options: options, archiveIdentity: program.Identity}
	prepared.storage, prepared.constructed = storage, constructed
	return prepared, nil
}

// Discard abandons a prepared load.
func (prepared *PreparedSession) Discard() {
	if prepared == nil || prepared.session == nil {
		return
	}
	if client := prepared.session.Client; client != nil && client.Audio != nil {
		client.Audio.SetSink(nil)
	}
	*prepared = PreparedSession{}
}

// Commit publishes the restored program over the live save store and replaces
// no save. previous is the running session being displaced, or nil for a load
// into a fresh process. A failure leaves previous running and the saves as
// they were.
//
// The order is what keeps the program's own saves first:
//
//  1. What can be refused without the store is refused.
//  2. The restored program is bound to the store: every file the record names
//     is filled from it. Only reads are made, and a save that cannot be read
//     refuses the load here.
//  3. The displaced program has nothing to store: every write it made was
//     stored before the call that made it returned, and with its run lock
//     held no call is in progress.
//  4. The displaced program is cut off and the restored one adopted. Nothing in
//     this step can fail, and neither program runs: the displaced one is not
//     destroyed, because its destroy entry is guest code that may write.
func (prepared *PreparedSession) Commit(ctx context.Context, previous *Session, live wipicrt.SaveStore) (*Session, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if prepared == nil || prepared.session == nil {
		return nil, fmt.Errorf("SKT Clet checkpoint replacement is not prepared")
	}
	if live == nil {
		return nil, fmt.Errorf("SKT Clet checkpoint load requires a save store")
	}
	var old *Client
	if previous != nil {
		if previous.Client == nil || previous.archiveIdentity != prepared.session.archiveIdentity {
			return nil, backend.ErrCheckpointIdentity
		}
		old = previous.Client
		if !old.Run.TryLock() {
			return nil, ErrCheckpointBusy
		}
	}
	// The running client's lock is given back on every way out, a panic among
	// them: a session that failed here is closed by its Host, and its close
	// takes this lock.
	locked := old != nil
	unlock := func() {
		if locked {
			locked = false
			old.Run.Unlock()
		}
	}
	defer unlock()
	client := prepared.session.Client
	if err := client.bindRestoredStorage(prepared.storage, live); err != nil {
		return nil, fmt.Errorf("SKT Clet checkpoint load: %w", err)
	}
	if err := ctx.Err(); err != nil {
		// The restored program goes back to answering nothing: it is not
		// adopted, and what is discarded must not hold the saves.
		client.SaveStore = prepared.placeholder
		return nil, err
	}
	// Nothing after this point can fail.
	if old != nil {
		// A store of its own, so nothing the displaced client is still asked
		// to do reaches the saves the restored one now reads.
		sink, _ := backend.NewMemorySaveStore(nil)
		old.SaveStore, old.FrameSink, old.workersStopped = sink, backend.FrameSink{}, true
		if old.Audio != nil {
			old.Audio.SetSink(nil)
		}
		unlock()
		previous.Client = nil
	}
	session := prepared.session
	session.options.SaveStore, session.options.SaveRoot = live, ""
	// The record's deadlines were laid against the instant the replacement
	// was built; the time the load took since then moves them all alike.
	shift := client.Now().Sub(prepared.constructed)
	rebase := func(instant time.Time) time.Time {
		if instant.IsZero() {
			return instant
		}
		return instant.Add(shift)
	}
	runtime := client.runtime
	runtime.ClockBase = runtime.ClockBase.Add(shift)
	for index := range runtime.PendingTimers {
		runtime.PendingTimers[index].Due = rebase(runtime.PendingTimers[index].Due)
	}
	client.ClientWakeAt, client.NextRoundPaint = rebase(client.ClientWakeAt), rebase(client.NextRoundPaint)
	client.Audio.ActivateOutputClock()
	// This is the already validated record applied to the motor's own clock.
	_ = client.Vibrator.RestoreState(prepared.vibration)
	client.FrameSink = session.options.FrameSink
	client.Audio.SetSink(session.options.AudioSink)
	*prepared = PreparedSession{}
	return session, nil
}

// ResumeCheckpointOutput is called after the Host resets its previous device
// output and queues. Construction and adoption themselves stay silent.
func (session *Session) ResumeCheckpointOutput() {
	if session != nil && session.Client != nil && session.Client.Audio != nil {
		session.Client.Audio.ResumeOutput()
	}
}
