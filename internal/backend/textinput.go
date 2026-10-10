package backend

import (
	"context"
	"errors"
	"unicode/utf8"

	"golang.org/x/text/encoding/korean"
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
	// MaxLength is the field limit in platform character units; zero is unlimited.
	// Java fields count UTF-16 code units, so a supplementary character uses two.
	MaxLength int
	Multiline bool
	Password  bool
	// Append means the platform can insert complete text at the active guest
	// cursor but cannot read or replace the field's existing value.
	Append bool
	// InputMode is a browser keyboard hint, not a substitute for validation.
	InputMode string
	Commit    func(context.Context, string) error
}

// ValidateTextInput checks transport-level invariants. Platforms enforce their
// own field constraints as well, even if the browser already checked them.
func ValidateTextInput(text string) error {
	if len(text) > MaxTextInputBytes || !utf8.ValidString(text) {
		return ErrInvalidTextInput
	}
	return nil
}

// EncodeKSC5601 encodes text a person typed for a title and reports whether
// every character has a code in the handsets' string encoding. The WIPI
// specification names it — a String becomes a C string as ISO8859 for English
// and KSC5601 for Hangul — and all three carriers' handsets were EUC-KR.
//
// The Go encoder is the Microsoft extension of KSC5601, which also gives codes
// to the 8,822 Hangul syllables KSC5601 has none for: a lead byte below 0xA1, or
// a trail byte below 0xA1. Titles draw text with fonts of their own laid out
// for KSC5601 and index the glyph table by the bytes they are given. One title
// that saved a name holding such a syllable overran that table drawing its
// slot screen, and the slot stayed blank for as long as the save held the name,
// so text like that is refused before it reaches a title rather than handed
// over.
func EncodeKSC5601(text string) ([]byte, bool) {
	encoded, err := korean.EUCKR.NewEncoder().Bytes([]byte(text))
	if err != nil {
		return nil, false
	}
	for index := 0; index < len(encoded); index++ {
		if encoded[index] < 0x80 {
			continue
		}
		if index+1 == len(encoded) || encoded[index] < 0xa1 || encoded[index+1] < 0xa1 {
			return nil, false
		}
		index++
	}
	return encoded, true
}
