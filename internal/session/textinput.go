package session

import (
	"context"

	"github.com/movingwoo/wfeature/internal/backend"
)

type textInputProvider interface {
	TextInput(context.Context) (*backend.TextInput, error)
}

// TextInput obtains a short-lived edit for the current guest field. Hosts call
// both this method and the returned Commit on their serialized session loop.
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
		return nil
	})
	return edit, err
}

func (s *Session) commitTextInput(ctx context.Context, platform any, commit func(context.Context, string) error, text string) error {
	return s.guarded("session text commit", func() error {
		if s.paused || ctx.Err() != nil ||
			(platform != s.ktf && platform != s.lgt && platform != s.runtime) {
			return backend.ErrTextInputChanged
		}
		if err := backend.ValidateTextInput(text); err != nil {
			return err
		}
		return s.endedOrFailed(commit(ctx, text))
	})
}
