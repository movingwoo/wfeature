package ktf

import (
	"context"
	"errors"
	"fmt"

	"github.com/movingwoo/wfeature/internal/backend"
)

var ErrCheckpointBusy = errors.New("KTF checkpoint requires an idle session boundary")

// sessionCheckpointVersion is the layout of sessionCheckpointState and of the
// records inside it. Version 1 carried the content of every storage table.
const sessionCheckpointVersion = 2

type sessionCheckpointState struct {
	Version               uint32
	TimerLimit            int
	DisableAuthentication bool
	Client                clientState
	Adapters              saveAdapterState
}

func checkpointTimerLimit(options SessionOptions) int {
	if options.TimerLimit <= 0 {
		return sessionDefaultTimerLimit
	}
	return options.TimerLimit
}

func checkpointVariant(client *Client) uint16 {
	if client.IsModule() {
		return backend.CheckpointKTFModule
	}
	return backend.CheckpointKTFJava
}

// CaptureCheckpoint must run between whole session rounds on the Host's session
// goroutine, serialized with keys, cheats, clocks, lifecycle and save imports.
// It never invokes pauseApp. The additional client lock refuses immediately if
// a low-level caller still owns execution; capture does not queue a later save.
// The enclosing shared session supplies Checkpoint.Session before encoding.
//
// It makes no save store call. Every write this runtime takes is stored before
// the call that made it returns, so nothing is waiting for the store when a
// checkpoint is taken, and the record names what the title has open without
// holding any of it. See storage_rebind.go.
func (session *Session) CaptureCheckpoint(ctx context.Context) (backend.Checkpoint, error) {
	return session.CaptureCheckpointWithSession(ctx, nil)
}

// CaptureCheckpointWithSession obtains the shared session record at the same
// boundary as the platform clock, before copying a large heap or reading files.
// captureSession must only encode already parked state; it may not execute the
// guest, send input, or move its clock.
func (session *Session) CaptureCheckpointWithSession(ctx context.Context, captureSession func() ([]byte, error)) (backend.Checkpoint, error) {
	if err := ctx.Err(); err != nil {
		return backend.Checkpoint{}, err
	}
	if session == nil || session.Client == nil || session.Archive == nil || session.archiveIdentity == ([32]byte{}) {
		return backend.Checkpoint{}, fmt.Errorf("KTF checkpoint session is not started")
	}
	if session.cheat != nil && (session.cheat.Freezes().Len() != 0 || len(session.cheat.Patches()) != 0) {
		return backend.Checkpoint{}, fmt.Errorf("KTF checkpoint cannot capture active cheat freezes or patches")
	}
	client := session.Client
	if !client.run.TryLock() {
		return backend.Checkpoint{}, ErrCheckpointBusy
	}
	defer client.run.Unlock()
	now := client.now()
	var shared []byte
	if captureSession != nil {
		var err error
		shared, err = captureSession()
		if err != nil {
			return backend.Checkpoint{}, err
		}
	}
	saved, err := client.captureClientStateAt(now)
	if err != nil {
		return backend.Checkpoint{}, err
	}
	adapters, base, err := captureSaveAdapters(client.saveStore)
	if err != nil {
		return backend.Checkpoint{}, err
	}
	// A slot carries no save, so a load has to find the saves in a store. A
	// session that has none could be captured and never loaded.
	if base == nil {
		return backend.Checkpoint{}, fmt.Errorf("KTF checkpoint requires a save store")
	}
	record, err := backend.EncodeCheckpointRecord(sessionCheckpointState{Version: sessionCheckpointVersion, TimerLimit: checkpointTimerLimit(session.options),
		DisableAuthentication: session.options.DisableAuthentication, Client: saved, Adapters: adapters})
	if err != nil {
		return backend.Checkpoint{}, err
	}
	if err := ctx.Err(); err != nil {
		return backend.Checkpoint{}, err
	}
	return backend.Checkpoint{Identity: session.archiveIdentity, Variant: checkpointVariant(client), Session: shared, Runtime: record}, nil
}

// PreparedSession owns a fully validated, dormant replacement. It was built on
// a placeholder store that answers nothing and is cut off from every output
// until Commit succeeds. The caller must Discard it when abandoning a load;
// Discard after successful Commit is harmless.
type PreparedSession struct {
	session     *Session
	activation  clientActivation
	adapters    saveAdapterState
	placeholder *backend.DetachedSaveStore
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

// Speed exposes the validated guest pace so the shared session can check its
// repeat clock before committing saves. It does not grant any worker execution.
func (prepared *PreparedSession) Speed() float64 {
	if prepared == nil || prepared.session == nil {
		return 0
	}
	return prepared.session.Speed()
}

// PrepareSessionCheckpoint opens the original archive and reconstructs a Java
// or earlier-module session without running entry, initialization or startApp.
// It reads no save: options.SaveStore and options.SaveRoot are ignored, and the
// live store reaches the restored runtime only as an argument of Commit. The
// caller checks the envelope before calling and retains exclusive ownership of
// the save directory through Commit. Native packages use a separate runtime.
func PrepareSessionCheckpoint(archive []byte, checkpoint backend.Checkpoint, options SessionOptions) (*PreparedSession, error) {
	identity := backend.SaveIdentity(archive)
	if checkpoint.Identity != identity {
		return nil, backend.ErrCheckpointIdentity
	}
	opened, err := Open(archive)
	if err != nil {
		return nil, err
	}
	return prepareSessionCheckpoint(opened, identity, checkpoint, options)
}

func prepareSessionCheckpoint(archive *Archive, identity [32]byte, checkpoint backend.Checkpoint, options SessionOptions) (*PreparedSession, error) {
	if checkpoint.Identity != identity {
		return nil, backend.ErrCheckpointIdentity
	}
	if checkpoint.Variant != backend.CheckpointKTFJava && checkpoint.Variant != backend.CheckpointKTFModule {
		return nil, backend.ErrCheckpointVersion
	}
	var saved sessionCheckpointState
	if err := backend.DecodeCheckpointRecord(checkpoint.Runtime, &saved); err != nil {
		return nil, err
	}
	if saved.Version != sessionCheckpointVersion {
		return nil, fmt.Errorf("KTF checkpoint session record is version %d and this build reads %d: %w", saved.Version, sessionCheckpointVersion, backend.ErrCheckpointVersion)
	}
	if saved.TimerLimit != checkpointTimerLimit(options) || saved.DisableAuthentication != options.DisableAuthentication {
		return nil, fmt.Errorf("KTF checkpoint was taken under another timer limit or authentication setting: %w", backend.ErrCheckpointVersion)
	}
	// The adapter record is checked here, where nothing is read, and built
	// when the load commits, where the store is.
	if err := saved.Adapters.validate(); err != nil {
		return nil, err
	}
	prepared := &PreparedSession{adapters: saved.Adapters, placeholder: backend.NewDetachedSaveStore()}
	detached := options
	detached.SaveStore, detached.SaveRoot = prepared.placeholder, ""
	detached.AudioSink, detached.FrameSink = nil, backend.FrameSink{}
	client, err := restoreSessionClientForActivation(archive, saved.Client, detached, &prepared.activation)
	if err != nil {
		return nil, err
	}
	if checkpoint.Variant != checkpointVariant(client) {
		client.StopThreads()
		return nil, backend.ErrCheckpointVersion
	}
	prepared.session = &Session{Archive: archive, Client: client, options: options, archiveIdentity: identity}
	return prepared, nil
}

func (prepared *PreparedSession) Discard() {
	if prepared == nil || prepared.session == nil {
		return
	}
	prepared.session.Client.StopThreads()
	*prepared = PreparedSession{}
}

// Commit publishes the restored runtime over the live save store and replaces
// no save. previous is the running session being displaced, or nil for a load
// into a fresh process. A failure leaves previous running and the saves as
// they were.
//
// The order is what keeps the title's own saves first:
//
//  1. What can be refused without the store is refused.
//  2. The restored runtime is bound to the store: its adapters are built over
//     it and every storage object the record names is filled from it. Only
//     reads are made, and a save that cannot be read refuses the load here.
//  3. The displaced runtime has nothing to store. Every write it took was
//     stored before the call that made it returned, and with its run lock
//     held no call is in progress.
//  4. The displaced runtime is cut off and the restored one adopted. Nothing
//     in this step can fail: the displaced workers unwind against a store of
//     their own with outputs detached, without invoking destroyApp.
func (prepared *PreparedSession) Commit(ctx context.Context, previous *Session, live backend.SaveStore) (*Session, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if prepared == nil || prepared.session == nil {
		return nil, fmt.Errorf("KTF checkpoint replacement is not prepared")
	}
	if live == nil {
		return nil, fmt.Errorf("KTF checkpoint load requires a save store")
	}
	var old *Client
	if previous != nil {
		if previous.Client == nil || previous.archiveIdentity != prepared.session.archiveIdentity {
			return nil, backend.ErrCheckpointIdentity
		}
		old = previous.Client
		if !old.run.TryLock() {
			return nil, ErrCheckpointBusy
		}
	}
	// The running client's lock is given back on every way out, a panic
	// among them: a session that failed here is closed by its Host, and its
	// close takes this lock.
	locked := old != nil
	unlock := func() {
		if locked {
			locked = false
			old.run.Unlock()
		}
	}
	defer unlock()
	client := prepared.session.Client
	if err := client.bindRestoredStorage(prepared.adapters, live); err != nil {
		unlock()
		return nil, fmt.Errorf("KTF checkpoint load: %w", err)
	}
	// A displaced worker can still issue a storage call while it unwinds, and
	// one that does not unwind in time can issue one later. It gets a store of
	// its own, so nothing it does reaches the saves the restored runtime now
	// reads.
	sink, err := backend.NewMemorySaveStore(nil)
	if err == nil {
		err = ctx.Err()
	}
	if err != nil {
		// The restored runtime goes back to answering nothing: it is not
		// adopted, and what is discarded must not hold the saves.
		client.saveStore = prepared.placeholder
		unlock()
		return nil, err
	}
	// Nothing after this point can fail.
	client.runtime.restoredStorage = nil
	if old != nil {
		old.saveStore, old.frameSink = rebaseSaveAdapters(old.saveStore, sink), backend.FrameSink{}
		old.audio.SetSink(nil)
		unlock()
		old.StopThreads()
		previous.Client, previous.cheat, previous.cheatConsole = nil, nil, nil
	}
	session := prepared.session
	session.options.SaveStore, session.options.SaveRoot = live, ""
	prepared.activation.activate()
	session.Client.frameSink = session.options.FrameSink
	session.Client.audio.SetSink(session.options.AudioSink)
	*prepared = PreparedSession{}
	return session, nil
}

// ResumeCheckpointOutput is called after the Host resets its previous device
// output and queues. Construction and adoption themselves stay silent.
func (session *Session) ResumeCheckpointOutput() {
	if session != nil && session.Client != nil {
		session.Client.audio.ResumeOutput()
	}
}
