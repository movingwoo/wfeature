package session

import (
	"context"

	"github.com/movingwoo/wfeature/internal/backend"
)

// TextInput obtains a short-lived edit for the current guest field. Hosts call
// both this method and the returned Commit on their serialized session loop.
func (s *Session) TextInput(ctx context.Context) (edit *backend.TextInput, err error) {
	err = s.guarded("session text input", func() error {
		if s.paused || s.engine == nil {
			return backend.ErrNoTextInput
		}
		platform := s.engine
		var failure error
		edit, failure = platform.textInput(ctx)
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

// commitTextInput commits an edit only to the platform session that offered
// it: a load or a close since then has put another one in its place, or none.
func (s *Session) commitTextInput(ctx context.Context, platform engine, commit func(context.Context, string) error, text string) error {
	return s.guarded("session text commit", func() error {
		if s.paused || ctx.Err() != nil || s.engine != platform {
			return backend.ErrTextInputChanged
		}
		if err := backend.ValidateTextInput(text); err != nil {
			return err
		}
		return s.endedOrFailed(commit(ctx, text))
	})
}
