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
// **A load leaves the ordinary saves alone.** A checkpoint is the game's
// execution state and nothing else: the slot carries no save, and a load never
// replaces, reverts or removes one. The title's own saves come first, so what
// it saved after the checkpoint is still there after a load, and the restored
// title reads and writes the saves as they are now. checkpoint_storage.go has
// what that takes on this platform: the writes a title has issued are stored
// before either step, and what a restored title has open is read again from
// the store when a load commits.

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

// sessionCheckpointVersion is the layout of sessionCheckpointState and of the
// records inside it. Version 1 carried the content of every open file.
const sessionCheckpointVersion = 2

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
	// A slot carries no save, so a load has to find the saves in a store. A
	// session that has none could be captured and never loaded.
	if _, base, err := client.captureAdapters(); err != nil {
		return backend.Checkpoint{}, err
	} else if base == nil {
		return backend.Checkpoint{}, fmt.Errorf("LGT checkpoint requires a save store")
	}
	// Everything that can be refused without asking the store is refused
	// first, so a quick save that is not going to happen stores nothing.
	if held := client.storageBytes(); held > maxStateBytes {
		return backend.Checkpoint{}, fmt.Errorf("LGT checkpoint: the open files and databases hold %d bytes, more than the %d a checkpoint restores", held, maxStateBytes)
	}
	// The title's own writes come before the checkpoint: the store is given
	// what it has not been given, and a store that will not take it refuses
	// the quick save. It runs before the memory image is copied, because an
	// authentication adapter can write into guest memory when it stores.
	if _, err := client.storeIssuedWrites(); err != nil {
		return backend.Checkpoint{}, fmt.Errorf("LGT checkpoint: %w", err)
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
	adapters, _, err := client.captureAdapters()
	if err != nil {
		return backend.Checkpoint{}, err
	}
	saved.Adapters = adapters
	record, err := backend.EncodeCheckpointRecord(saved)
	if err != nil {
		return backend.Checkpoint{}, err
	}
	if err := ctx.Err(); err != nil {
		return backend.Checkpoint{}, err
	}
	return backend.Checkpoint{
		Identity: session.archiveIdentity, Variant: checkpointVariant(client),
		Session: shared, Runtime: record,
	}, nil
}

// PreparedSession is a restored session that has not been published. It was
// built on a placeholder store that answers nothing and it has no audio
// device, so discarding it leaves no trace. What the title has open in it is
// there by name and holds nothing yet. The caller must Discard it when
// abandoning a load; a Discard after a successful Commit does nothing.
type PreparedSession struct {
	session     *Session
	archive     *Archive
	adapters    adapterState
	placeholder *backend.DetachedSaveStore
	vibration   backend.VibrationState
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

// Speed is the pace the checkpoint was taken at, which the Host checks against
// its own record before it commits.
func (prepared *PreparedSession) Speed() float64 {
	if prepared == nil || prepared.session == nil {
		return 0
	}
	return prepared.session.Speed()
}

// PrepareSessionCheckpoint rebuilds a session from a checkpoint without
// running any of it. Everything the record claims is checked here, and no save
// is read: options.SaveStore and options.SaveRoot are ignored, and the live
// store reaches the restored runtime only as an argument of Commit.
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
	if saved.Version != sessionCheckpointVersion {
		return nil, fmt.Errorf("LGT checkpoint session record is version %d and this build reads %d: %w", saved.Version, sessionCheckpointVersion, backend.ErrCheckpointVersion)
	}
	if saved.DisableAuthentication != options.DisableAuthentication ||
		saved.Tick != checkpointTick(options) || saved.MaxSteps != checkpointMaxSteps(options) ||
		!validAuthentication(saved.Authentication) ||
		saved.DisableAuthentication != (backend.AuthenticationStatus(saved.Authentication) == backend.AuthenticationOff) {
		return nil, fmt.Errorf("LGT checkpoint was taken under another tick, instruction budget or authentication setting: %w", backend.ErrCheckpointVersion)
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
	prepared := &PreparedSession{archive: opened, adapters: saved.Adapters, placeholder: backend.NewDetachedSaveStore(), vibration: saved.Client.Vibration}
	client, err := restoreClientState(opened, saved.Client, Options{
		Logger: options.Logger, SaveStore: prepared.placeholder, MaxSteps: checkpointMaxSteps(options),
		TraceSVC: options.TraceSVC, TraceLive: options.TraceLive, TraceOut: options.TraceOut,
	})
	if err != nil {
		return nil, err
	}
	if checkpoint.Variant != checkpointVariant(client) {
		return nil, backend.ErrCheckpointVersion
	}
	if err := client.checkAdapters(opened, saved.Adapters); err != nil {
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

// Commit publishes the restored session over the live save store and replaces
// no save.
//
// previous is the live session being replaced, or nil for a load into a fresh
// process. A failure leaves previous running, and never reverts or removes a
// save.
//
// The order is what keeps the title's own writes first:
//
//  1. What can be refused without the store is refused.
//  2. The restored session is bound to the store: its adapter is built over it
//     and everything it has open is read from it. Nothing is written. A save
//     that cannot be read refuses the load here, with the store untouched.
//  3. The displaced session's unstored writes are given to the store. They are
//     writes its title issued, and the session that would have stored them at
//     its close is about to go. A store that refuses refuses the load.
//  4. If that stored anything the restored session is bound again, so what it
//     has open is what the displaced one wrote.
//  5. The displaced session is cut off from the store and the audio device
//     and its guest threads are ended, **without closing it**, because closing
//     calls its destroyClet and a load runs no guest code of either session.
//     Nothing in this step can fail.
func (prepared *PreparedSession) Commit(ctx context.Context, previous *Session, live backend.SaveStore) (*Session, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if prepared == nil || prepared.session == nil {
		return nil, fmt.Errorf("LGT checkpoint replacement is not prepared")
	}
	if live == nil {
		return nil, fmt.Errorf("LGT checkpoint load requires a save store")
	}
	if previous != nil {
		if previous.client == nil || previous.archiveIdentity != prepared.session.archiveIdentity {
			return nil, backend.ErrCheckpointIdentity
		}
		if err := previous.client.checkpointBoundary(); err != nil {
			return nil, err
		}
	}
	client := prepared.session.client
	if err := client.bindRestoredStorage(prepared.archive, prepared.adapters, live); err != nil {
		return nil, fmt.Errorf("LGT checkpoint load: %w", err)
	}
	// From here the restored client reads the store. On any refusal it goes
	// back to answering nothing: it is not adopted, and what is discarded must
	// not hold the saves.
	detach := func() { client.saveStore = prepared.placeholder }
	if previous != nil {
		stored, err := previous.client.storeIssuedWrites()
		if err != nil {
			detach()
			return nil, fmt.Errorf("LGT checkpoint load: %w", err)
		}
		if stored != 0 {
			if err := client.bindRestoredStorage(prepared.archive, prepared.adapters, live); err != nil {
				detach()
				return nil, fmt.Errorf("LGT checkpoint load: %w", err)
			}
		}
	}
	if err := ctx.Err(); err != nil {
		detach()
		return nil, err
	}
	// Nothing after this point can fail.
	client.restoredStorage = nil
	if previous != nil {
		old := previous.client
		old.saveStore = nil
		old.audio.SetSink(nil)
		old.StopJavaThreads()
		previous.client, previous.cheat, previous.cheatConsole = nil, nil, nil
	}
	session := prepared.session
	session.options.SaveStore, session.options.SaveRoot = live, ""
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
