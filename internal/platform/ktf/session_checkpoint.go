package ktf

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"

	"github.com/movingwoo/wfeature/internal/backend"
)

var ErrCheckpointBusy = errors.New("KTF checkpoint requires an idle session boundary")

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
	entries, err := snapshotCheckpointSaves(base)
	if err != nil {
		return backend.Checkpoint{}, err
	}
	record, err := backend.EncodeCheckpointRecord(sessionCheckpointState{Version: 1, TimerLimit: checkpointTimerLimit(session.options),
		DisableAuthentication: session.options.DisableAuthentication, Client: saved, Adapters: adapters})
	if err != nil {
		return backend.Checkpoint{}, err
	}
	if err := ctx.Err(); err != nil {
		return backend.Checkpoint{}, err
	}
	return backend.Checkpoint{Identity: session.archiveIdentity, Variant: checkpointVariant(client), Session: shared, Runtime: record, Saves: entries}, nil
}

func snapshotCheckpointSaves(store backend.SaveStore) ([]backend.SaveEntry, error) {
	if store == nil {
		return nil, nil
	}
	complete, ok := store.(backend.SaveSnapshotStore)
	if !ok {
		return nil, fmt.Errorf("KTF checkpoint requires a complete save snapshot store")
	}
	return complete.SnapshotSaves()
}

// PreparedSession owns a fully validated, dormant replacement. Its guest saves
// and outputs are isolated until Commit succeeds. The caller must Discard it
// when abandoning a load; Discard after successful Commit is harmless.
type PreparedSession struct {
	session    *Session
	activation clientActivation
	store      backend.SaveSnapshotStore
	finalStore backend.SaveStore
	saves      []backend.SaveEntry
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
// The caller checks the envelope before calling and retains exclusive ownership
// of the save directory through Commit. Native packages use a separate runtime.
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
	if saved.Version != 1 || saved.TimerLimit != checkpointTimerLimit(options) || saved.DisableAuthentication != options.DisableAuthentication {
		return nil, fmt.Errorf("KTF checkpoint session version or policy is incompatible")
	}
	isolated, err := backend.NewMemorySaveStore(checkpoint.Saves)
	if err != nil {
		return nil, err
	}
	base := options.SaveStore
	if base == nil && options.SaveRoot != "" {
		base = backend.NewDirectorySaveStore(filepath.Join(options.SaveRoot, SaveOwner(archive.Descriptor)))
	}
	var store backend.SaveSnapshotStore
	if base != nil {
		var ok bool
		store, ok = base.(backend.SaveSnapshotStore)
		if !ok {
			return nil, fmt.Errorf("KTF checkpoint requires a replaceable save snapshot store")
		}
	} else if len(checkpoint.Saves) != 0 {
		return nil, fmt.Errorf("KTF checkpoint has saves but the destination has no save store")
	}
	finalStore, err := restoreSaveAdapters(saved.Adapters, base)
	if err != nil {
		return nil, err
	}
	prepared := &PreparedSession{store: store, finalStore: finalStore}
	// Own the generation independently of caller buffers before validation.
	prepared.saves, err = isolated.SnapshotSaves()
	if err != nil {
		return nil, err
	}
	detached := options
	detached.SaveStore, detached.SaveRoot = isolated, ""
	detached.AudioSink, detached.FrameSink = nil, backend.FrameSink{}
	client, err := restoreSessionClientForActivation(archive, saved.Client, saved.Adapters, detached, &prepared.activation)
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

// Commit replaces the durable generation before publishing any live runtime.
// A failure leaves previous and its saves usable. Once replacement succeeds,
// adoption has no fallible step: displaced workers unwind against their own
// memory generation with outputs detached, without invoking destroyApp.
func (prepared *PreparedSession) Commit(ctx context.Context, previous *Session) (*Session, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if prepared == nil || prepared.session == nil {
		return nil, fmt.Errorf("KTF checkpoint replacement is not prepared")
	}
	var old *Client
	var discardedStore backend.SaveStore
	if previous != nil {
		if previous.Client == nil || previous.archiveIdentity != prepared.session.archiveIdentity {
			return nil, backend.ErrCheckpointIdentity
		}
		old = previous.Client
		if !old.run.TryLock() {
			return nil, ErrCheckpointBusy
		}
		locked := true
		defer func() {
			if locked {
				old.run.Unlock()
			}
		}()
		adapters, base, err := captureSaveAdapters(old.saveStore)
		if err != nil {
			return nil, err
		}
		entries, err := snapshotCheckpointSaves(base)
		if err != nil {
			return nil, err
		}
		isolated, err := backend.NewMemorySaveStore(entries)
		if err != nil {
			return nil, err
		}
		discardedStore, err = restoreSaveAdapters(adapters, isolated)
		if err != nil {
			return nil, err
		}
		if err := prepared.replaceSaves(ctx); err != nil {
			return nil, err
		}
		old.saveStore, old.frameSink = discardedStore, backend.FrameSink{}
		old.audio.SetSink(nil)
		old.run.Unlock()
		locked = false
		old.StopThreads()
		previous.Client, previous.cheat, previous.cheatConsole = nil, nil, nil
	} else if err := prepared.replaceSaves(ctx); err != nil {
		return nil, err
	}
	session := prepared.session
	prepared.activation.activate()
	session.Client.saveStore, session.Client.frameSink = prepared.finalStore, session.options.FrameSink
	session.Client.audio.SetSink(session.options.AudioSink)
	*prepared = PreparedSession{}
	return session, nil
}

func (prepared *PreparedSession) replaceSaves(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if prepared.store != nil {
		return prepared.store.ReplaceSaves(prepared.saves)
	}
	return nil
}

// ResumeCheckpointOutput is called after the Host resets its previous device
// output and queues. Construction and save replacement themselves stay silent.
func (session *Session) ResumeCheckpointOutput() {
	if session != nil && session.Client != nil {
		session.Client.audio.ResumeOutput()
	}
}
