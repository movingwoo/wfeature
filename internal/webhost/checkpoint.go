package webhost

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/session"
)

func timelineCommand(kind string) bool {
	switch kind {
	case clientKey, clientPointer, clientText, clientCheat, clientSpeed, clientScale, clientQuickSave, clientQuickLoad, clientAudioResume:
		return true
	}
	return false
}

func checkpointExists(directory string, identity [32]byte) bool {
	if directory == "" {
		return false
	}
	exists, err := backend.NewDirectorySaveStore(directory).HasCheckpoint(identity)
	return exists && err == nil
}

func loadCheckpointSlot(directory string, identity [32]byte) ([]byte, error) {
	if directory == "" {
		return nil, fmt.Errorf("this game has no checkpoint directory")
	}
	data, found, err := backend.NewDirectorySaveStore(directory).LoadCheckpoint(identity)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, fmt.Errorf("no quick save exists for this archive")
	}
	return data, nil
}

// checkpointStep is where a refusal reaches the person. One cause is worded
// for the step that was asked for, and the log names the step.
type checkpointStep int

const (
	checkpointSaving checkpointStep = iota
	checkpointLoading
	checkpointStarting
)

func (step checkpointStep) String() string {
	return [...]string{"quick save", "quick load", "start from the quick save"}[step]
}

// checkpointRefusal is what the page shows for a refused quick save or quick
// load. Four refusals are ones a person can act on, and they are said the way
// the rest of the page speaks to them: a slot an earlier build wrote, writes
// the running game had issued that the disk would not take, saves a load could
// not read, and a game that is not running, which starting it again answers.
// Every other refusal passes through as it is.
//
// The sentence takes the place of the cause, and the cause names the file and
// what the store answered, so it goes to the log.
func (r *sessionRunner) checkpointRefusal(step checkpointStep, game string, err error) string {
	var key string
	switch {
	case errors.Is(err, backend.ErrCheckpointLegacy):
		key = "checkpoint.refused.legacy"
	case errors.Is(err, backend.ErrCheckpointSaveWrite) && step == checkpointSaving:
		key = "checkpoint.refused.save-write"
	case errors.Is(err, backend.ErrCheckpointSaveWrite):
		key = "checkpoint.refused.load-write"
	case errors.Is(err, backend.ErrCheckpointSaveRead):
		key = "checkpoint.refused.load-read"
	case errors.Is(err, backend.ErrCheckpointNotRunning) && step == checkpointSaving:
		key = "checkpoint.refused.save-not-running"
	case errors.Is(err, backend.ErrCheckpointNotRunning):
		key = "checkpoint.refused.load-not-running"
	default:
		return err.Error()
	}
	r.server.logger.Warn("checkpoint refused", "game", game, "step", step.String(), "error", err)
	return r.server.phrase(key)
}

// quickLoadRefusal reads the slot a start from the quick save would restore,
// before the caller stops the game it is running. It answers the refusals that
// are already certain: no slot, a slot an earlier build wrote, a damaged one,
// one taken from a different archive. A start that cannot succeed then leaves
// the running game alone.
//
// It only reads. Everything else, an archive that cannot be read among them,
// is left to the start itself, which words those as it always has; and the
// start reads the slot again once it holds the save directory, because this
// answer was taken without the claim.
func (s *Server) quickLoadRefusal(game string) error {
	archive, _, err := s.readGameArchive(game)
	if err != nil {
		return nil
	}
	summary, err := session.Inspect(archive)
	if err != nil {
		return nil
	}
	identity := backend.SaveIdentity(archive)
	// The saves an earlier build kept under another owner name are moved by
	// the start, with the claim; until then the slot is read where it is.
	directory := ""
	if s.saveDirectory(summary.Platform, summary.SaveOwner) != "" {
		directory = session.SaveDirectory(summary, s.platformSaveRoot(summary.Platform))
	}
	data, err := loadCheckpointSlot(directory, identity)
	if err != nil {
		return err
	}
	_, err = backend.DecodeCheckpoint(data, identity)
	return err
}

// reportEarlierLeftovers says in the log, once per start, what an earlier build
// left beside this game's saves. This build writes neither: a quick save in the
// earlier slot format is refused when it is loaded and stays where it is, and
// the saves an earlier quick load set aside are never restored or removed,
// because nothing knows whether they are newer than the saves in use. A person
// who wants either has to be told that it exists.
func (r *sessionRunner) reportEarlierLeftovers(directory string, identity [32]byte, game string) {
	if directory == "" {
		return
	}
	store := backend.NewDirectorySaveStore(directory)
	slot, slotErr := store.LegacyCheckpoint(identity)
	displaced, displacedErr := store.DisplacedGeneration()
	if slotErr != nil || displacedErr != nil {
		r.server.logger.Debug("earlier build leftovers could not be checked", "game", game, "error", errors.Join(slotErr, displacedErr))
	}
	if slot || displaced {
		r.server.logger.Info("an earlier build left files beside this game's saves; they are kept and not used",
			"game", game, "saves", directory, "earlier_quick_save", slot, "saves_set_aside_by_a_quick_load", displaced)
	}
}

// The runner owns the save claim and handles these commands between whole
// rounds. Other pages and save imports cannot access a different generation.
func (r *sessionRunner) quickSave(ctx context.Context, message clientMessage) {
	if r.game == nil || !r.started.CanCheckpoint {
		r.send(serverMessage{Kind: serverError, ID: message.ID, Message: session.ErrCheckpointUnsupported.Error()})
		return
	}
	data, err := r.game.CaptureCheckpoint(ctx)
	if err == nil {
		err = backend.NewDirectorySaveStore(r.saveDirectory).StoreCheckpoint(r.game.ArchiveIdentity(), data)
	}
	if err != nil {
		r.send(serverMessage{Kind: serverError, ID: message.ID, Message: r.checkpointRefusal(checkpointSaving, r.label, err)})
		return
	}
	r.started.HasCheckpoint = true
	r.server.logger.Debug("checkpoint saved", "game", r.label, "bytes", len(data))
	r.send(serverMessage{Kind: serverResult, ID: message.ID, Message: "Checkpoint saved."})
}

func (r *sessionRunner) quickLoad(ctx context.Context, message clientMessage) {
	if r.game == nil || !r.started.CanCheckpoint {
		r.send(serverMessage{Kind: serverError, ID: message.ID, Message: session.ErrCheckpointUnsupported.Error()})
		return
	}
	archive, _, err := r.server.readGameArchive(r.started.Game)
	if err == nil && backend.SaveIdentity(archive) != r.game.ArchiveIdentity() {
		err = backend.ErrCheckpointIdentity
	}
	var data []byte
	if err == nil {
		data, err = loadCheckpointSlot(r.saveDirectory, r.game.ArchiveIdentity())
	}
	if err == nil {
		err = r.game.LoadCheckpoint(ctx, archive, data)
	}
	if err != nil {
		r.send(serverMessage{Kind: serverError, ID: message.ID, Message: r.checkpointRefusal(checkpointLoading, r.label, err)})
		return
	}
	r.finishCheckpointLoad(message)
}

func (r *sessionRunner) finishCheckpointLoad(message clientMessage) {
	// No old runtime can emit after LoadCheckpoint has returned. The encoder
	// and writer reject anything already queued under the previous epoch.
	epoch := r.outputEpoch.Add(1)
	r.game.SetOutputEpoch(epoch)
	r.clearTextInput()
	clear(r.heldKeys)
	r.heldPointer = nil
	r.audio.take()
	r.audioDefinitions = audioDefinitions{}
	r.vibrationRequest = 0
	r.guestMarked = false
	r.ticks, r.tickTotal, r.skipped = 0, 0, 0
	r.statsSince = time.Now()
	r.started.HasCheckpoint, r.started.Restored = true, true
	r.started.Speed = r.game.Speed()
	r.started.Width, r.started.Height = r.game.Screen()
	identity := r.started
	// Reset is non-droppable and precedes every new sound/picture. The page
	// closes its old async decoder before accepting a new complete frame.
	r.send(serverMessage{Kind: serverRestored, ID: message.ID, Started: &identity})
	err := r.game.ReleaseHeldInput(r.gameCtx)
	// The snapshot preserves pause state; a visible browser is a new Host
	// observation and resumes through the same lifecycle as reconnection.
	if err == nil && r.game.Paused() {
		err = r.game.Resume(r.gameCtx)
	}
	if err != nil {
		if errors.Is(err, session.ErrExited) {
			reason := r.game.ExitReason()
			r.endGame(endedByExit(reason), true)
			r.send(serverMessage{Kind: serverExited, Message: reason})
		} else {
			r.send(serverMessage{Kind: serverError, Message: err.Error()})
		}
		return
	}
	events, overflow := r.audio.collectReplay(r.game.ResumeCheckpointOutput)
	r.audioNeedsReset = !r.sendAudio(events, false) || overflow
	r.presented = r.game.Flushes()
	r.forceFrame = true
	r.pushFrame()
	r.flushVibration()
	r.server.logger.Debug("checkpoint restored", "game", r.label, "epoch", epoch)
}
