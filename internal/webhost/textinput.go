package webhost

import (
	"errors"

	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/session"
)

func (r *sessionRunner) clearTextInput() {
	r.textInput = nil
	r.textInputGame = nil
}

func (r *sessionRunner) handleTextInput(message clientMessage) {
	fail := func(err error) {
		r.send(serverMessage{Kind: serverError, ID: message.ID, Message: err.Error()})
	}
	if r.game == nil {
		r.clearTextInput()
		fail(backend.ErrNoTextInput)
		return
	}
	switch message.Action {
	case "open":
		r.clearTextInput()
		r.releaseHeldInput()
		edit, err := r.game.TextInput(r.gameCtx)
		if err != nil {
			fail(err)
			return
		}
		r.textInputID++
		r.textInput, r.textInputGame = edit, r.game
		r.send(serverMessage{Kind: serverResult, ID: message.ID, TextInput: &textInputMessage{
			Edit: r.textInputID, Text: edit.Text, MaxLength: edit.MaxLength,
			Multiline: edit.Multiline, Password: edit.Password, InputMode: edit.InputMode,
		}})
	case "commit":
		if r.textInput == nil || r.textInputGame != r.game || message.Edit != r.textInputID {
			fail(backend.ErrTextInputChanged)
			return
		}
		if err := backend.ValidateTextInput(message.Text); err != nil {
			fail(err)
			return
		}
		if err := r.textInput.Commit(r.gameCtx, message.Text); err != nil {
			switch {
			case errors.Is(err, session.ErrExited):
				reason := r.game.ExitReason()
				r.endGame(endedByExit(reason), true)
				// The event moves the page out of its playing state before the
				// request rejection reaches the dialog and tries to restore it.
				r.send(serverMessage{Kind: serverExited, Message: reason})
				r.send(serverMessage{Kind: serverError, ID: message.ID, Message: endedByExit(reason), Exited: true})
			case errors.Is(err, backend.ErrTextInputChanged):
				// Once an edit has been proven stale it must never become usable
				// again if the guest later restores the same focus and contents.
				r.clearTextInput()
				fail(err)
			default:
				fail(err)
			}
			return
		}
		r.clearTextInput()
		r.send(serverMessage{Kind: serverResult, ID: message.ID})
	case "cancel":
		if message.Edit == r.textInputID {
			r.clearTextInput()
		}
		r.send(serverMessage{Kind: serverResult, ID: message.ID})
	default:
		fail(backend.ErrInvalidTextInput)
	}
}
