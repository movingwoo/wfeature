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

// flushTextInputRequest turns the script runtime's standing native-dialog
// request into one page event. The page opens through the ordinary request path
// afterwards, so the edit token and snapshot still have one owner.
func (r *sessionRunner) flushTextInputRequest() {
	if r.game == nil {
		return
	}
	request := r.game.TextInputRequest()
	if request == 0 || request == r.textInputRequest {
		return
	}
	r.textInputRequest = request
	r.send(serverMessage{Kind: serverTextInput})
}

func (r *sessionRunner) sendTextInput(message clientMessage) {
	edit := r.textInput
	r.send(serverMessage{Kind: serverResult, ID: message.ID, TextInput: &textInputMessage{
		Edit: r.textInputID, Text: edit.Text, Prompt: edit.Prompt,
		MaxLength: edit.MaxLength, MaxBytes: edit.MaxBytes,
		Multiline: edit.Multiline, Password: edit.Password, InputMode: edit.InputMode,
	}})
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
		if err := r.releaseHeldInput(); errors.Is(err, session.ErrExited) {
			r.endTextInputOnExit(message)
			return
		}
		edit, err := r.game.TextInput(r.gameCtx)
		if err != nil {
			fail(err)
			return
		}
		r.textInputID++
		r.textInput, r.textInputGame = edit, r.game
		r.sendTextInput(message)
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
				r.endTextInputOnExit(message)
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
		// Late cancellation of an old page token is deliberately harmless. It
		// must not consume a newer native dialog that now owns the session.
		if r.textInput == nil || r.textInputGame != r.game || message.Edit != r.textInputID {
			r.send(serverMessage{Kind: serverResult, ID: message.ID})
			return
		}
		if r.textInput.Cancel != nil {
			if err := r.textInput.Cancel(r.gameCtx); err != nil {
				r.clearTextInput()
				switch {
				case errors.Is(err, session.ErrExited):
					r.endTextInputOnExit(message)
				default:
					fail(err)
				}
				return
			}
		}
		r.clearTextInput()
		r.send(serverMessage{Kind: serverResult, ID: message.ID})
	default:
		fail(backend.ErrInvalidTextInput)
	}
}

func (r *sessionRunner) endTextInputOnExit(message clientMessage) {
	reason := r.game.ExitReason()
	r.endGame(endedByExit(reason), true)
	// The event moves the page out of its playing state before the request
	// rejection reaches the dialog and tries to restore it.
	r.send(serverMessage{Kind: serverExited, Message: reason})
	r.send(serverMessage{Kind: serverError, ID: message.ID, Message: endedByExit(reason), Exited: true})
}
