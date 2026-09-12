package backend

import (
	"context"
	"errors"
	"unicode/utf8"
)

var (
	ErrNoTextInput      = errors.New("no supported text field is active")
	ErrTextInputChanged = errors.New("the active text field changed; open text input again")
	ErrInvalidTextInput = errors.New("text does not satisfy the active field constraints")
)

// MaxTextInputBytes bounds committed Host text independently of a guest's
// advertised field size. The boundary never truncates a composition silently.
const MaxTextInputBytes = 64 << 10

// TextInput is a snapshot of one active guest editor. The Host composes text
// using its own keyboard/IME and commits the finished value. Commit must check
// that the same field is still active and unchanged, then apply guest limits
// and notification semantics. It must not redirect a stale edit to a new field.
//
// Snapshots are short-lived and must not be retained across session ownership
// changes. Commit is called on the same serialized Host path as key events;
// platforms remain responsible for their guest-thread synchronization.
type TextInput struct {
	Text string
	// Prompt is optional guest text describing the requested value. Hosts must
	// render it as text, never as markup.
	Prompt string
	// MaxLength is the field limit in platform character units; zero is unlimited.
	// Java fields count UTF-16 code units, so a supplementary character uses two.
	MaxLength int
	// MaxBytes is a limit in the guest platform's encoded bytes, not UTF-8
	// transport bytes. A Host may describe it, but the platform must enforce it.
	MaxBytes  int
	Multiline bool
	Password  bool
	// InputMode is a browser keyboard hint, not a substitute for validation.
	InputMode string
	Commit    func(context.Context, string) error
	// Cancel is optional. Platforms whose native dialog reports cancellation
	// use it to complete the guest callback without changing the field.
	Cancel func(context.Context) error
}

// ValidateTextInput checks transport-level invariants. Platforms enforce their
// own field constraints as well, even if the browser already checked them.
func ValidateTextInput(text string) error {
	if len(text) > MaxTextInputBytes || !utf8.ValidString(text) {
		return ErrInvalidTextInput
	}
	return nil
}
