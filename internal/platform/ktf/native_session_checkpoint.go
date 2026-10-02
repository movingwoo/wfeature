package ktf

import (
	"context"
	"fmt"

	"github.com/movingwoo/wfeature/internal/backend"
)

// Quick save and quick load
//
// A checkpoint is the title's execution state and nothing else. The slot
// carries no save and a load replaces none: what the title saved after the
// checkpoint is still there after a load, and the restored title reads and
// writes the files as the store has them then.
//
// What makes that hold is that a file has one home. Whatever the title has
// written and the store has not been given is given to the store first, by a
// quick save and by a quick load alike, and either is refused when the store
// refuses. So a record never has to carry a write, and a load never has an
// older copy of a file to put back.

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
	// A slot carries no save, so a load has to find the saves in a store. A
	// session that has none could be captured and never loaded.
	if session.platform.saves == nil {
		return backend.Checkpoint{}, fmt.Errorf("KTF native checkpoint requires a save store")
	}
	// Everything that can be refused without asking the store is refused
	// first, so a quick save that is not going to happen stores nothing.
	if err := session.checkpointRefusal(); err != nil {
		return backend.Checkpoint{}, err
	}
	// The title's own writes come before the checkpoint: the store is given
	// what it has not been given, and a store that will not take it refuses
	// the quick save. This is what an ordinary boundary does, made now.
	if _, err := session.platform.storePending(); err != nil {
		return backend.Checkpoint{}, fmt.Errorf("KTF native checkpoint: %w", err)
	}
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
	record, err := backend.EncodeCheckpointRecord(saved)
	if err != nil {
		return backend.Checkpoint{}, err
	}
	if err := ctx.Err(); err != nil {
		return backend.Checkpoint{}, err
	}
	return backend.Checkpoint{Identity: session.archiveIdentity, Variant: backend.CheckpointKTFNative, Session: shared, Runtime: record}, nil
}

// PreparedNativeSession keeps outputs detached through validation and is built
// on a placeholder store that answers nothing. Its open files have no bytes
// yet. Commit reads them from the live store, binds the session to that store
// and adopts it without running guest code; it replaces no save.
type PreparedNativeSession struct {
	session     *NativeSession
	state       nativeState
	placeholder *backend.DetachedSaveStore
}

// PreparationStoreCalls reports how many save store calls validation made
// against its placeholder, and the first of them. Validation is meant to make
// none: the saves are read when the load commits, from the live store.
func (prepared *PreparedNativeSession) PreparationStoreCalls() (int, string) {
	if prepared == nil || prepared.placeholder == nil {
		return 0, ""
	}
	return prepared.placeholder.Calls(), prepared.placeholder.FirstCall()
}

func (prepared *PreparedNativeSession) Speed() float64 {
	if prepared == nil || prepared.session == nil {
		return 0
	}
	return prepared.session.Speed()
}

// PrepareNativeSessionCheckpoint rebuilds a native session from a checkpoint
// without running any of it. It reads no save: options.SaveStore is ignored,
// and the live store reaches the restored runtime only as an argument of
// Commit.
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
	if saved.Version != nativeStateVersion {
		return nil, fmt.Errorf("KTF native checkpoint record is version %d and this build reads %d: %w", saved.Version, nativeStateVersion, backend.ErrCheckpointVersion)
	}
	opened, err := OpenNative(archive)
	if err != nil {
		return nil, err
	}
	prepared := &PreparedNativeSession{state: saved, placeholder: backend.NewDetachedSaveStore()}
	detached := options
	detached.SaveStore, detached.AudioSink = prepared.placeholder, nil
	prepared.session, err = restoreNativeState(opened, saved, detached)
	if err != nil {
		return nil, err
	}
	prepared.session.archiveIdentity = identity
	prepared.session.options = options
	prepared.session.platform.AttachSaves(prepared.placeholder)
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

// Commit publishes the restored runtime over the live save store and replaces
// no save. previous is the running session being displaced, or nil for a load
// into a fresh process. A failure leaves previous running, and never reverts
// or removes a save.
//
// The order is what keeps the title's own writes first:
//
//  1. What can be refused without the store is refused.
//  2. The restored title's open files are read from the store, and nothing is
//     written. A file that cannot be read, or files that are too large between
//     them, refuse the load here, with the store untouched.
//  3. The displaced session's pending writes are given to the store. They are
//     writes its title issued, and the session that would have stored them at
//     its next boundary is about to go. A store that refuses refuses the load.
//  4. If that stored anything the files are read again, so the restored title
//     opens on what the displaced one wrote.
//  5. The displaced session is cut off and the restored one adopted. Nothing
//     in this step can fail.
//
// Close is not called on the displaced session: its flush forgets a write the
// store refuses, and the load would go on past a lost save.
func (prepared *PreparedNativeSession) Commit(ctx context.Context, previous *NativeSession, live backend.SaveStore) (*NativeSession, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if prepared == nil || prepared.session == nil {
		return nil, fmt.Errorf("KTF native checkpoint replacement is not prepared")
	}
	if live == nil {
		return nil, fmt.Errorf("KTF native checkpoint load requires a save store")
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
	platform := prepared.session.platform
	reopened, err := platform.reopenFiles(live, prepared.state.Files, nativeStateStorageLimit)
	if err != nil {
		return nil, fmt.Errorf("KTF native checkpoint load: %w", err)
	}
	if previous != nil {
		stored, err := previous.platform.storePending()
		if err != nil {
			return nil, fmt.Errorf("KTF native checkpoint load: %w", err)
		}
		if stored != 0 {
			if reopened, err = platform.reopenFiles(live, prepared.state.Files, nativeStateStorageLimit); err != nil {
				return nil, fmt.Errorf("KTF native checkpoint load: %w", err)
			}
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	// Nothing after this point can fail.
	if previous != nil {
		previous.platform.saves = nil
		previous.platform.audio.SetSink(nil)
		previous.platform, previous.Client, previous.cheat, previous.cheatConsole = nil, nil, nil, nil
	}
	session, saved := prepared.session, prepared.state
	platform.adoptReopened(reopened)
	platform.saves, session.options.SaveStore = live, live
	now := platform.clock.Now()
	platform.started, session.started = now.Add(-saved.Elapsed), now.Add(-saved.SessionElapsed)
	platform.timedDue = saved.TimedDue.restore(now)
	if saved.Frame != nil {
		platform.frame.due = now.Add(saved.Frame.Remaining)
	}
	platform.audio.ActivateOutputClock()
	platform.audio.SetSink(session.options.AudioSink)
	*prepared = PreparedNativeSession{}
	return session, nil
}

func (session *NativeSession) ResumeCheckpointOutput() {
	if session != nil && session.platform != nil && session.platform.audio != nil {
		session.platform.audio.ResumeOutput()
	}
}
