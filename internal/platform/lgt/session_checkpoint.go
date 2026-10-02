package lgt

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/keypad"
)

// Quick save and quick load
//
// A checkpoint is taken between two ticks and loaded between two ticks, which
// is the one place on this platform where nothing of the guest's is in flight
// on the Host's own goroutine: a Clet has returned from every entry point it
// was called through, and every guest thread of a Java title is parked. What
// is recorded there is the memory image, the registers, the tables in
// checkpoint_state.go and, for a Java title, what each parked thread still
// owes (checkpoint_worker.go).
//
// **Taking one runs no guest code and tells the title nothing.** A pause would
// call the title's own pauseClet, and a title is free to change what it holds
// when it is paused — which is the state being recorded. A load is the same:
// the title is not started, initialised or resumed, it is simply there again.
//
// **A load replaces the ordinary saves as well as the memory.** The two were
// one moment when the checkpoint was taken, and a title restored beside saves
// from a later moment reads a save file that describes a game it has not
// played yet. The save set is replaced first and whole; only once that has
// happened is the restored session published, and from there nothing can fail.

// sessionCheckpointState is the runtime section of an LGT checkpoint.
type sessionCheckpointState struct {
	Version               uint32
	DisableAuthentication bool
	Authentication        string
	Tick                  time.Duration
	MaxSteps              uint64
	Speed                 float64
	Pad                   keypad.PadState
	Client                clientState
	Adapters              adapterState
}

const sessionCheckpointVersion = 1

func checkpointMaxSteps(options SessionOptions) uint64 {
	if options.MaxSteps == 0 {
		return sessionDefaultMaxSteps
	}
	return options.MaxSteps
}

func checkpointTick(options SessionOptions) time.Duration {
	if options.Tick <= 0 {
		return defaultTick
	}
	return options.Tick
}

func checkpointVariant(client *Client) uint16 {
	if client.javaRun != nil || client.javaApplication {
		return backend.CheckpointLGTJava
	}
	return backend.CheckpointLGTClet
}

func validAuthentication(status string) bool {
	switch backend.AuthenticationStatus(status) {
	case backend.AuthenticationOff, backend.AuthenticationUnsupported, backend.AuthenticationLGTOptions,
		backend.AuthenticationLGTCertificate100, backend.AuthenticationLGTCertificate58,
		backend.AuthenticationLGTNotification, backend.AuthenticationLGTHandshake,
		backend.AuthenticationLGTSaveSubscriber, backend.AuthenticationLGTCertificateMessage:
		return true
	}
	return false
}

// CanCheckpoint reports whether this session can be asked for a checkpoint at
// all. A request can still be refused: see CaptureCheckpoint.
func (session *Session) CanCheckpoint() bool {
	return session != nil && session.client != nil && session.archiveIdentity != ([32]byte{})
}

// CaptureCheckpoint records the session between two ticks. It must be called
// from the goroutine that ticks the session, and it runs no guest code.
//
// A refusal leaves the session exactly as it was. ErrCheckpointBusy is the
// one worth retrying; any other names something a checkpoint cannot hold — an
// active cheat, or a guest thread parked inside a platform call this platform
// has no way to finish later.
func (session *Session) CaptureCheckpoint(ctx context.Context) (backend.Checkpoint, error) {
	return session.CaptureCheckpointWithSession(ctx, nil)
}

// CaptureCheckpointWithSession also takes the enclosing Host session's record
// at the same boundary, before the memory image is copied. captureSession may
// only encode state that is already parked.
func (session *Session) CaptureCheckpointWithSession(
	ctx context.Context, captureSession func() ([]byte, error),
) (backend.Checkpoint, error) {
	if err := ctx.Err(); err != nil {
		return backend.Checkpoint{}, err
	}
	if !session.CanCheckpoint() {
		return backend.Checkpoint{}, fmt.Errorf("LGT checkpoint session is not started")
	}
	if session.cheat != nil && (session.cheat.Freezes().Len() != 0 || len(session.cheat.Patches()) != 0) {
		return backend.Checkpoint{}, fmt.Errorf("LGT checkpoint cannot capture active cheat freezes or patches")
	}
	client := session.client
	if err := client.checkpointIdle(); err != nil {
		return backend.Checkpoint{}, err
	}
	var shared []byte
	if captureSession != nil {
		var err error
		if shared, err = captureSession(); err != nil {
			return backend.Checkpoint{}, err
		}
	}
	pad, err := session.pad.CaptureState()
	if err != nil {
		return backend.Checkpoint{}, err
	}
	saved := sessionCheckpointState{
		Version: sessionCheckpointVersion, DisableAuthentication: session.options.DisableAuthentication,
		Authentication: string(session.Authentication()), Tick: session.tick,
		MaxSteps: client.core.MaxSteps(), Speed: session.speedOrDefault(), Pad: pad,
	}
	if saved.Client, err = client.captureClientState(); err != nil {
		return backend.Checkpoint{}, err
	}
	adapters, base, err := client.captureAdapters()
	if err != nil {
		return backend.Checkpoint{}, err
	}
	saved.Adapters = adapters
	entries, err := snapshotCheckpointSaves(base)
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
	return backend.Checkpoint{
		Identity: session.archiveIdentity, Variant: checkpointVariant(client),
		Session: shared, Runtime: record, Saves: entries,
	}, nil
}

func snapshotCheckpointSaves(store backend.SaveStore) ([]backend.SaveEntry, error) {
	if store == nil {
		return nil, nil
	}
	complete, ok := store.(backend.SaveSnapshotStore)
	if !ok {
		return nil, fmt.Errorf("LGT checkpoint requires a complete save snapshot store")
	}
	return complete.SnapshotSaves()
}

// PreparedSession is a restored session that has not been published. Its
// saves are an isolated copy and it has no audio device, so discarding it
// leaves no trace. The caller must Discard it when abandoning a load; a
// Discard after a successful Commit does nothing.
type PreparedSession struct {
	session   *Session
	store     backend.SaveSnapshotStore
	base      backend.SaveStore
	saves     []backend.SaveEntry
	vibration backend.VibrationState
}

// Speed is the pace the checkpoint was taken at, which the Host checks against
// its own record before it commits.
func (prepared *PreparedSession) Speed() float64 {
	if prepared == nil || prepared.session == nil {
		return 0
	}
	return prepared.session.Speed()
}

// PrepareSessionCheckpoint rebuilds a session from a checkpoint without
// running any of it. Everything the record claims is checked here, before the
// saves it would replace are touched.
func PrepareSessionCheckpoint(archive []byte, checkpoint backend.Checkpoint, options SessionOptions) (*PreparedSession, error) {
	identity := backend.SaveIdentity(archive)
	if checkpoint.Identity != identity {
		return nil, backend.ErrCheckpointIdentity
	}
	if checkpoint.Variant != backend.CheckpointLGTClet && checkpoint.Variant != backend.CheckpointLGTJava {
		return nil, backend.ErrCheckpointVersion
	}
	var saved sessionCheckpointState
	if err := backend.DecodeCheckpointRecord(checkpoint.Runtime, &saved); err != nil {
		return nil, err
	}
	if saved.Version != sessionCheckpointVersion || saved.DisableAuthentication != options.DisableAuthentication ||
		saved.Tick != checkpointTick(options) || saved.MaxSteps != checkpointMaxSteps(options) ||
		!validAuthentication(saved.Authentication) ||
		saved.DisableAuthentication != (backend.AuthenticationStatus(saved.Authentication) == backend.AuthenticationOff) {
		return nil, fmt.Errorf("LGT checkpoint session version or policy is incompatible")
	}
	if math.IsNaN(saved.Speed) || math.IsInf(saved.Speed, 0) || saved.Speed < backend.SpeedFloor || saved.Speed > backend.SpeedCeiling {
		return nil, fmt.Errorf("LGT checkpoint has an invalid speed")
	}
	// The pad's rule is attached with the first key a session is sent, so a
	// session that was never sent one has none and is restored without one.
	var isPad func(int32) bool
	if saved.Pad.UsesPad {
		isPad = func(code int32) bool { return isPadKey(uint32(code)) }
	}
	pad, err := keypad.RestorePad(saved.Pad, isPad)
	if err != nil {
		return nil, err
	}
	opened, err := Open(archive)
	if err != nil {
		return nil, err
	}
	isolated, err := backend.NewMemorySaveStore(checkpoint.Saves)
	if err != nil {
		return nil, err
	}
	base := options.SaveStore
	if base == nil && options.SaveRoot != "" {
		base = backend.NewDirectorySaveStore(options.SaveRoot)
	}
	prepared := &PreparedSession{base: base, vibration: saved.Client.Vibration}
	if base != nil {
		var ok bool
		if prepared.store, ok = base.(backend.SaveSnapshotStore); !ok {
			return nil, fmt.Errorf("LGT checkpoint requires a replaceable save snapshot store")
		}
	} else if len(checkpoint.Saves) != 0 {
		return nil, fmt.Errorf("LGT checkpoint has saves but the destination has no save store")
	}
	// Own the generation independently of the caller's buffers.
	if prepared.saves, err = isolated.SnapshotSaves(); err != nil {
		return nil, err
	}
	client, err := restoreClientState(opened, saved.Client, Options{
		Logger: options.Logger, SaveStore: isolated, MaxSteps: checkpointMaxSteps(options),
		TraceSVC: options.TraceSVC, TraceLive: options.TraceLive, TraceOut: options.TraceOut,
	})
	if err != nil {
		return nil, err
	}
	if checkpoint.Variant != checkpointVariant(client) {
		return nil, backend.ErrCheckpointVersion
	}
	if err := client.restoreAdapters(opened, saved.Adapters, isolated); err != nil {
		return nil, err
	}
	if err := client.startRestoredJavaWorkers(); err != nil {
		return nil, err
	}
	prepared.session = &Session{
		client: client, archive: opened, tick: saved.Tick, speed: saved.Speed, pad: pad,
		authentication: backend.AuthenticationStatus(saved.Authentication),
		options:        options, archiveIdentity: identity,
	}
	return prepared, nil
}

// Discard drops a prepared session that will not be committed.
func (prepared *PreparedSession) Discard() {
	if prepared == nil || prepared.session == nil {
		return
	}
	client := prepared.session.client
	client.saveStore = nil
	client.audio.SetSink(nil)
	client.StopJavaThreads()
	*prepared = PreparedSession{}
}

// Commit replaces the ordinary saves and publishes the restored session.
//
// previous is the live session being replaced, or nil for a load into a fresh
// process. A failure before the saves are replaced leaves previous running
// with its saves untouched. After that point nothing can fail: the displaced
// session is cut off from the store and the audio device and its guest threads
// are ended — **without closing it**, because closing flushes its open files
// and calls its destroyClet, and both would write the old session's state over
// the saves that were just restored.
func (prepared *PreparedSession) Commit(ctx context.Context, previous *Session) (*Session, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if prepared == nil || prepared.session == nil {
		return nil, fmt.Errorf("LGT checkpoint replacement is not prepared")
	}
	if previous != nil {
		if previous.client == nil || previous.archiveIdentity != prepared.session.archiveIdentity {
			return nil, backend.ErrCheckpointIdentity
		}
		if err := previous.client.checkpointBoundary(); err != nil {
			return nil, err
		}
	}
	if prepared.store != nil {
		if err := prepared.store.ReplaceSaves(prepared.saves); err != nil {
			return nil, err
		}
	}
	if previous != nil {
		old := previous.client
		old.saveStore = nil
		old.audio.SetSink(nil)
		old.StopJavaThreads()
		previous.client, previous.cheat, previous.cheatConsole = nil, nil, nil
	}
	session := prepared.session
	client := session.client
	client.rebaseAdapters(prepared.base)
	// The motor's deadline is measured on the Host's clock, so it is set again
	// here: the time a load spent being checked is not time the motor ran.
	_ = client.vibrator.RestoreState(prepared.vibration)
	client.audio.ActivateOutputClock()
	client.audio.SetSink(session.options.AudioSink)
	*prepared = PreparedSession{}
	return session, nil
}

// checkpointBoundary reports whether a live session can be replaced now. It is
// the part of checkpointIdle that is about what is running rather than about
// whether the state could be recorded: a session that has exited or lost a
// save can still be loaded over.
func (client *Client) checkpointBoundary() error {
	if client.activeJavaWorker != nil || client.javaCallDepth != 0 || client.collecting ||
		len(client.thread.LiveContexts()) != 1 {
		return ErrCheckpointBusy
	}
	if client.javaRun != nil && client.javaRun.keyCallback {
		return ErrCheckpointBusy
	}
	return nil
}

// ResumeCheckpointOutput rebuilds the sound that was playing when the
// checkpoint was taken. The Host calls it once, after it has cleared its own
// queues: a load is silent until then.
func (session *Session) ResumeCheckpointOutput() {
	if session != nil && session.client != nil && session.client.audio != nil {
		session.client.audio.ResumeOutput()
	}
}
