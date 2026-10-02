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
	case clientKey, clientPointer, clientText, clientCheat, clientSpeed, clientScale, clientQuickSave, clientQuickLoad:
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
		r.send(serverMessage{Kind: serverError, ID: message.ID, Message: err.Error()})
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
		r.send(serverMessage{Kind: serverError, ID: message.ID, Message: err.Error()})
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
	r.game.ResumeCheckpointOutput()
	r.sendAudio(r.audio.take(), false)
	r.presented = r.game.Flushes()
	r.forceFrame = true
	r.pushFrame()
	r.flushVibration()
	r.server.logger.Debug("checkpoint restored", "game", r.label, "epoch", epoch)
}
