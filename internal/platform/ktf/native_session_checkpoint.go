package ktf

import (
	"context"
	"fmt"

	"github.com/movingwoo/wfeature/internal/backend"
)

// CaptureCheckpoint runs between complete native rounds. It records data only;
// there is no suspended Go worker in this execution variant.
func (session *NativeSession) CaptureCheckpoint(ctx context.Context) (backend.Checkpoint, error) {
	return session.CaptureCheckpointWithSession(ctx, nil)
}

func (session *NativeSession) CaptureCheckpointWithSession(ctx context.Context, captureSession func() ([]byte, error)) (backend.Checkpoint, error) {
	if err := ctx.Err(); err != nil {
		return backend.Checkpoint{}, err
	}
	if session == nil {
		return backend.Checkpoint{}, fmt.Errorf("KTF native checkpoint session is not started")
	}
	if !session.run.TryLock() {
		return backend.Checkpoint{}, ErrCheckpointBusy
	}
	defer session.run.Unlock()
	if session.Client == nil || session.platform == nil || session.archiveIdentity == ([32]byte{}) {
		return backend.Checkpoint{}, fmt.Errorf("KTF native checkpoint session is not started")
	}
	if session.cheat != nil && (session.cheat.Freezes().Len() != 0 || len(session.cheat.Patches()) != 0) {
		return backend.Checkpoint{}, fmt.Errorf("KTF native checkpoint cannot capture active cheat freezes or patches")
	}
	if !session.Client.run.TryLock() {
		return backend.Checkpoint{}, ErrCheckpointBusy
	}
	defer session.Client.run.Unlock()
	now := session.clock.Now()
	var shared []byte
	if captureSession != nil {
		var err error
		shared, err = captureSession()
		if err != nil {
			return backend.Checkpoint{}, err
		}
	}
	saved, err := session.captureNativeState(now)
	if err != nil {
		return backend.Checkpoint{}, err
	}
	entries, err := snapshotCheckpointSaves(session.platform.saves)
	if err != nil {
		return backend.Checkpoint{}, err
	}
	record, err := backend.EncodeCheckpointRecord(saved)
	if err != nil {
		return backend.Checkpoint{}, err
	}
	if err := ctx.Err(); err != nil {
		return backend.Checkpoint{}, err
	}
	return backend.Checkpoint{Identity: session.archiveIdentity, Variant: backend.CheckpointKTFNative, Session: shared, Runtime: record, Saves: entries}, nil
}

// PreparedNativeSession keeps outputs and saves detached through validation.
// Commit replaces ordinary saves once, then adopts without running guest code.
type PreparedNativeSession struct {
	session    *NativeSession
	state      nativeState
	store      backend.SaveSnapshotStore
	finalStore backend.SaveStore
	saves      []backend.SaveEntry
}

func (prepared *PreparedNativeSession) Speed() float64 {
	if prepared == nil || prepared.session == nil {
		return 0
	}
	return prepared.session.Speed()
}

func PrepareNativeSessionCheckpoint(archive []byte, checkpoint backend.Checkpoint, options NativeSessionOptions) (*PreparedNativeSession, error) {
	identity := backend.SaveIdentity(archive)
	if checkpoint.Identity != identity {
		return nil, backend.ErrCheckpointIdentity
	}
	if checkpoint.Variant != backend.CheckpointKTFNative {
		return nil, backend.ErrCheckpointVersion
	}
	var saved nativeState
	if err := backend.DecodeCheckpointRecord(checkpoint.Runtime, &saved); err != nil {
		return nil, err
	}
	opened, err := OpenNative(archive)
	if err != nil {
		return nil, err
	}
	isolated, err := backend.NewMemorySaveStore(checkpoint.Saves)
	if err != nil {
		return nil, err
	}
	prepared := &PreparedNativeSession{state: saved, finalStore: options.SaveStore}
	if options.SaveStore != nil {
		var ok bool
		prepared.store, ok = options.SaveStore.(backend.SaveSnapshotStore)
		if !ok {
			return nil, fmt.Errorf("KTF native checkpoint requires a replaceable save snapshot store")
		}
	} else if len(checkpoint.Saves) != 0 {
		return nil, fmt.Errorf("KTF native checkpoint has saves but the destination has no save store")
	}
	prepared.saves, err = isolated.SnapshotSaves()
	if err != nil {
		return nil, err
	}
	detached := options
	detached.SaveStore, detached.AudioSink = isolated, nil
	prepared.session, err = restoreNativeState(opened, saved, detached)
	if err != nil {
		return nil, err
	}
	prepared.session.archiveIdentity = identity
	prepared.session.options = options
	prepared.session.platform.AttachSaves(isolated)
	return prepared, nil
}

func (prepared *PreparedNativeSession) Discard() {
	if prepared == nil || prepared.session == nil {
		return
	}
	prepared.session.platform.saves = nil
	prepared.session.platform.audio.SetSink(nil)
	prepared.session.platform = nil
	*prepared = PreparedNativeSession{}
}

func (prepared *PreparedNativeSession) Commit(ctx context.Context, previous *NativeSession) (*NativeSession, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if prepared == nil || prepared.session == nil {
		return nil, fmt.Errorf("KTF native checkpoint replacement is not prepared")
	}
	if previous != nil {
		if !previous.run.TryLock() {
			return nil, ErrCheckpointBusy
		}
		defer previous.run.Unlock()
		if previous.Client == nil || previous.platform == nil || previous.archiveIdentity != prepared.session.archiveIdentity {
			return nil, backend.ErrCheckpointIdentity
		}
		old := previous.Client
		if !old.run.TryLock() {
			return nil, ErrCheckpointBusy
		}
		defer old.run.Unlock()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if prepared.store != nil {
		if err := prepared.store.ReplaceSaves(prepared.saves); err != nil {
			return nil, err
		}
	}
	// No fallible work follows the durable replacement. In particular, Close
	// would flush discarded pending writes into the restored generation.
	if previous != nil {
		previous.platform.saves = nil
		previous.platform.audio.SetSink(nil)
		previous.platform, previous.Client, previous.cheat, previous.cheatConsole = nil, nil, nil, nil
	}
	session, saved := prepared.session, prepared.state
	platform := session.platform
	now := platform.clock.Now()
	platform.started, session.started = now.Add(-saved.Elapsed), now.Add(-saved.SessionElapsed)
	platform.timedDue = saved.TimedDue.restore(now)
	if saved.Frame != nil {
		platform.frame.due = now.Add(saved.Frame.Remaining)
	}
	platform.audio.ActivateOutputClock()
	platform.saves = prepared.finalStore
	platform.audio.SetSink(session.options.AudioSink)
	*prepared = PreparedNativeSession{}
	return session, nil
}

func (session *NativeSession) ResumeCheckpointOutput() {
	if session != nil && session.platform != nil && session.platform.audio != nil {
		session.platform.audio.ResumeOutput()
	}
}
