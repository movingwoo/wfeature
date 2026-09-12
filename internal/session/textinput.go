package session

import (
	"context"

	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/platform/skt"
)

type textInputProvider interface {
	TextInput(context.Context) (*backend.TextInput, error)
}

// TextInputRequest identifies a native platform dialog that the Host must
// present automatically. Java fields deliberately return zero: they remain
// available through the user's text-input control without opening themselves.
func (s *Session) TextInputRequest() uint64 {
	if s == nil || s.paused || s.script == nil {
		return 0
	}
	return s.script.TextInputRequest()
}

// TextInput obtains a short-lived edit for the current guest field. Hosts call
// this method and the returned completion on their serialized session loop.
func (s *Session) TextInput(ctx context.Context) (edit *backend.TextInput, err error) {
	err = s.guarded("session text input", func() error {
		if s.paused {
			return backend.ErrNoTextInput
		}
		var platform any
		switch {
		case s.ktf != nil:
			platform = s.ktf
		case s.lgt != nil:
			platform = s.lgt
		case s.runtime != nil:
			platform = s.runtime
		case s.script != nil:
			platform = s.script
		default:
			return backend.ErrNoTextInput
		}
		provider, ok := platform.(textInputProvider)
		if !ok {
			return backend.ErrNoTextInput
		}
		var failure error
		edit, failure = provider.TextInput(ctx)
		if failure != nil {
			return failure
		}
		if edit == nil || edit.Commit == nil {
			return backend.ErrNoTextInput
		}
		commit := edit.Commit
		edit.Commit = func(ctx context.Context, text string) error {
			return s.commitTextInput(ctx, platform, commit, text)
		}
		if edit.Cancel != nil {
			cancel := edit.Cancel
			edit.Cancel = func(ctx context.Context) error {
				return s.cancelTextInput(ctx, platform, cancel)
			}
		}
		return nil
	})
	return edit, err
}

func (s *Session) commitTextInput(ctx context.Context, platform any, commit func(context.Context, string) error, text string) error {
	if err := backend.ValidateTextInput(text); err != nil {
		return err
	}
	return s.completeTextInput(ctx, platform, "session text commit", func(ctx context.Context) error {
		return commit(ctx, text)
	})
}

func (s *Session) cancelTextInput(ctx context.Context, platform any, cancel func(context.Context) error) error {
	return s.completeTextInput(ctx, platform, "session text cancel", cancel)
}

func (s *Session) completeTextInput(ctx context.Context, platform any, where string, complete func(context.Context) error) error {
	return s.guarded(where, func() error {
		if s.paused || ctx.Err() != nil || !s.ownsTextInput(platform) {
			return backend.ErrTextInputChanged
		}
		if err := s.endedOrFailed(complete(ctx)); err != nil {
			return err
		}
		// SGS exit is a state rather than an error. Detect it at this boundary so
		// a callback that exits settles this Host request as the game ending.
		if script, ok := platform.(*skt.ScriptSession); ok && script.Exited() {
			s.exitReason = "the script requested exit"
			s.Close()
			return ErrExited
		}
		return nil
	})
}

func (s *Session) ownsTextInput(platform any) bool {
	return platform == s.ktf || platform == s.lgt || platform == s.runtime || platform == s.script
}
